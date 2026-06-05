// Package gitstore versions the markdown archive with an embedded git
// engine (go-git, pure Go) — no git binary required on the user's machine.
//
// The desktop build is local-first: commit/log/read-at-ref always work;
// remote interaction is reduced to an opt-in, push-only backup. There is
// no pull/rebase surface by design.
//
// Known limitation vs the git CLI: history is filtered per path without
// rename tracking (`--follow`), so a moved file's history starts at the
// move commit.
package gitstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	git "github.com/go-git/go-git/v5"
	gitcfg "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

type Store struct {
	RepoPath string

	repo *git.Repository
	// mu serializes worktree mutations (add/commit/move/remove). Reads
	// (log, file-at-ref) work off immutable objects and don't need it.
	mu sync.Mutex
	// pushToken authenticates HTTPS pushes (basic auth password). Empty
	// means anonymous push (e.g. a local bare repo or ssh-agent remote).
	pushToken string
}

// Open opens the repository at repoPath, initializing a fresh one (branch
// "main") when the directory isn't a git repo yet — first launch.
func Open(repoPath string) (*Store, error) {
	repo, err := git.PlainOpen(repoPath)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		repo, err = git.PlainInitWithOptions(repoPath, &git.PlainInitOptions{
			InitOptions: git.InitOptions{DefaultBranch: plumbing.Main},
			Bare:        false,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("open repo %s: %w", repoPath, err)
	}
	return &Store{RepoPath: repoPath, repo: repo}, nil
}

// SetPushToken wires the HTTPS token used by Push. Called at startup from
// prefs; safe to leave unset when no remote is configured.
func (s *Store) SetPushToken(token string) {
	s.pushToken = token
}

// noCommitsYet reports whether the error means the repo has no HEAD yet
// (fresh init, nothing committed). Reads treat that as "empty", not an error.
func noCommitsYet(err error) bool {
	return errors.Is(err, plumbing.ErrReferenceNotFound)
}

// logFor returns a commit iterator filtered to docPath, newest first.
func (s *Store) logFor(docPath string) (object.CommitIter, error) {
	return s.repo.Log(&git.LogOptions{FileName: &docPath})
}

func (s *Store) HeadSHA(docPath string) (string, error) {
	iter, err := s.logFor(docPath)
	if err != nil {
		if noCommitsYet(err) {
			return "", nil
		}
		return "", err
	}
	defer iter.Close()
	c, err := iter.Next()
	if err != nil {
		// No commit touched this path (or the iterator is empty).
		return "", nil
	}
	return c.Hash.String(), nil
}

func (s *Store) LastModified(docPath string) (time.Time, error) {
	iter, err := s.logFor(docPath)
	if err != nil {
		if noCommitsYet(err) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	defer iter.Close()
	c, err := iter.Next()
	if err != nil {
		return time.Time{}, nil
	}
	return c.Committer.When, nil
}

// HistoryEntry is a single commit that touched a document.
type HistoryEntry struct {
	SHA         string `json:"sha"`
	AuthorName  string `json:"author_name"`
	AuthorEmail string `json:"author_email"`
	Date        string `json:"date"`
	Message     string `json:"message"`
}

// History returns up to `limit` recent commits that touched docPath.
func (s *Store) History(docPath string, limit int) ([]HistoryEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 10
	}
	iter, err := s.logFor(docPath)
	if err != nil {
		if noCommitsYet(err) {
			return []HistoryEntry{}, nil
		}
		return nil, err
	}
	defer iter.Close()

	entries := make([]HistoryEntry, 0, limit)
	for len(entries) < limit {
		c, err := iter.Next()
		if err != nil {
			break // io.EOF or storage error — return what we have
		}
		entries = append(entries, HistoryEntry{
			SHA:         c.Hash.String(),
			AuthorName:  c.Author.Name,
			AuthorEmail: c.Author.Email,
			Date:        c.Committer.When.Format(time.RFC3339),
			Message:     firstLine(c.Message),
		})
	}
	return entries, nil
}

func firstLine(msg string) string {
	for i, r := range msg {
		if r == '\n' {
			return msg[:i]
		}
	}
	return msg
}

// ReadAtRef returns the file content at a given commit. `ref` must be a
// hex SHA (7–40 chars); abbreviated hashes are resolved.
func (s *Store) ReadAtRef(docPath, ref string) ([]byte, error) {
	if !isHexSHA(ref) {
		return nil, fmt.Errorf("invalid ref")
	}
	hash, err := s.repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", ref, err)
	}
	commit, err := s.repo.CommitObject(*hash)
	if err != nil {
		return nil, fmt.Errorf("commit %s: %w", ref, err)
	}
	file, err := commit.File(docPath)
	if err != nil {
		return nil, fmt.Errorf("%s at %s: %w", docPath, ref, err)
	}
	content, err := file.Contents()
	if err != nil {
		return nil, err
	}
	return []byte(content), nil
}

func isHexSHA(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

// commitStaged creates a commit with the given author. Returns the new SHA.
func (s *Store) commitStaged(message, authorName, authorEmail string) (string, error) {
	wt, err := s.repo.Worktree()
	if err != nil {
		return "", err
	}
	hash, err := wt.Commit(message, &git.CommitOptions{
		Author: &object.Signature{Name: authorName, Email: authorEmail, When: time.Now()},
	})
	if err != nil {
		return "", err
	}
	return hash.String(), nil
}

// Commit stages docPath and commits it. When the file is unchanged the
// current head SHA for the path is returned without creating a commit.
func (s *Store) Commit(docPath, message, authorName, authorEmail string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	wt, err := s.repo.Worktree()
	if err != nil {
		return "", err
	}
	if _, err := wt.Add(docPath); err != nil {
		return "", fmt.Errorf("git add %s: %w", docPath, err)
	}
	status, err := wt.Status()
	if err != nil {
		return "", err
	}
	if st, ok := status[docPath]; !ok || st.Staging == git.Unmodified {
		// Nothing staged for this path — no-op save.
		return s.HeadSHA(docPath)
	}
	sha, err := s.commitStaged(message, authorName, authorEmail)
	if err != nil {
		if errors.Is(err, git.ErrEmptyCommit) {
			return s.HeadSHA(docPath)
		}
		return "", err
	}
	return sha, nil
}

// MoveFile renames a document and commits the rename. Destination
// directories are created on demand.
func (s *Store) MoveFile(from, to, message, authorName, authorEmail string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if destDir := filepath.Dir(to); destDir != "" && destDir != "." {
		if err := os.MkdirAll(filepath.Join(s.RepoPath, destDir), 0o755); err != nil {
			return "", fmt.Errorf("mkdir dest: %w", err)
		}
	}
	if err := os.Rename(filepath.Join(s.RepoPath, from), filepath.Join(s.RepoPath, to)); err != nil {
		return "", fmt.Errorf("rename: %w", err)
	}
	wt, err := s.repo.Worktree()
	if err != nil {
		return "", err
	}
	// Stage the deletion of the old path and the addition of the new one.
	if _, err := wt.Add(from); err != nil {
		return "", fmt.Errorf("stage removal of %s: %w", from, err)
	}
	if _, err := wt.Add(to); err != nil {
		return "", fmt.Errorf("stage %s: %w", to, err)
	}
	return s.commitStaged(message, authorName, authorEmail)
}

// RemoveFile deletes a document and commits the deletion.
func (s *Store) RemoveFile(docPath, message, authorName, authorEmail string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	wt, err := s.repo.Worktree()
	if err != nil {
		return err
	}
	if _, err := wt.Remove(docPath); err != nil {
		return fmt.Errorf("git rm %s: %w", docPath, err)
	}
	_, err = s.commitStaged(message, authorName, authorEmail)
	return err
}

// Push pushes to the given remote (default "origin") using the configured
// HTTPS token when set. Already-up-to-date is success. Callers usually run
// this in the background and log failures rather than failing a save.
func (s *Store) Push(remote string) error {
	if remote == "" {
		remote = "origin"
	}
	opts := &git.PushOptions{RemoteName: remote}
	if s.pushToken != "" {
		// GitHub & friends accept any username with a token password.
		opts.Auth = &githttp.BasicAuth{Username: "x-access-token", Password: s.pushToken}
	}
	err := s.repo.Push(opts)
	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil
	}
	return err
}

// HasRemote reports whether the repo has the named remote configured. Used
// to skip auto-push when no backup remote is wired up.
func (s *Store) HasRemote(remote string) bool {
	if remote == "" {
		remote = "origin"
	}
	_, err := s.repo.Remote(remote)
	return err == nil
}

// EnsureRemote creates (or repoints) the named remote at url. Idempotent;
// called at startup when prefs configure a push remote.
func (s *Store) EnsureRemote(name, url string) error {
	if name == "" {
		name = "origin"
	}
	existing, err := s.repo.Remote(name)
	switch {
	case err == nil:
		if cfg := existing.Config(); len(cfg.URLs) > 0 && cfg.URLs[0] == url {
			return nil
		}
		if err := s.repo.DeleteRemote(name); err != nil {
			return fmt.Errorf("repoint remote %s: %w", name, err)
		}
	case !errors.Is(err, git.ErrRemoteNotFound):
		return err
	}
	_, err = s.repo.CreateRemote(&gitcfg.RemoteConfig{Name: name, URLs: []string{url}})
	return err
}

// CurrentBranch returns the short name of HEAD's branch, or "HEAD" when
// detached / unborn.
func (s *Store) CurrentBranch() string {
	ref, err := s.repo.Head()
	if err != nil {
		return "HEAD"
	}
	return ref.Name().Short()
}
