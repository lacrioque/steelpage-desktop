package gitstore

import (
	"os"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
)

func write(t *testing.T, repo, rel, content string) {
	t.Helper()
	abs := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open auto-init: %v", err)
	}
	return s, dir
}

func TestOpenAutoInitAndEmptyRepoReads(t *testing.T) {
	s, _ := openTemp(t)

	if sha, err := s.HeadSHA("nope.md"); err != nil || sha != "" {
		t.Errorf("HeadSHA on empty repo = (%q, %v), want (\"\", nil)", sha, err)
	}
	if ts, err := s.LastModified("nope.md"); err != nil || !ts.IsZero() {
		t.Errorf("LastModified on empty repo = (%v, %v), want (zero, nil)", ts, err)
	}
	entries, err := s.History("nope.md", 10)
	if err != nil || len(entries) != 0 {
		t.Errorf("History on empty repo = (%v, %v), want ([], nil)", entries, err)
	}
}

func TestCommitRoundTrip(t *testing.T) {
	s, dir := openTemp(t)
	write(t, dir, "a.md", "# Hello\n")

	sha, err := s.Commit("a.md", "docs: add a.md", "Tester", "tester@localhost")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if len(sha) != 40 {
		t.Fatalf("Commit returned sha %q", sha)
	}

	if head, _ := s.HeadSHA("a.md"); head != sha {
		t.Errorf("HeadSHA = %q, want %q", head, sha)
	}
	if ts, _ := s.LastModified("a.md"); ts.IsZero() {
		t.Error("LastModified is zero after commit")
	}

	hist, err := s.History("a.md", 10)
	if err != nil || len(hist) != 1 {
		t.Fatalf("History = (%v, %v), want one entry", hist, err)
	}
	if hist[0].AuthorName != "Tester" || hist[0].Message != "docs: add a.md" {
		t.Errorf("History entry = %+v", hist[0])
	}

	body, err := s.ReadAtRef("a.md", sha)
	if err != nil || string(body) != "# Hello\n" {
		t.Errorf("ReadAtRef = (%q, %v)", body, err)
	}
	// Abbreviated SHA resolves too.
	if _, err := s.ReadAtRef("a.md", sha[:8]); err != nil {
		t.Errorf("ReadAtRef short sha: %v", err)
	}

	if br := s.CurrentBranch(); br != "main" {
		t.Errorf("CurrentBranch = %q, want main", br)
	}
}

func TestCommitUnchangedIsNoop(t *testing.T) {
	s, dir := openTemp(t)
	write(t, dir, "a.md", "one\n")
	first, err := s.Commit("a.md", "add", "T", "t@l")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Commit("a.md", "no change", "T", "t@l")
	if err != nil {
		t.Fatalf("no-op commit errored: %v", err)
	}
	if again != first {
		t.Errorf("no-op commit moved head: %q -> %q", first, again)
	}
	hist, _ := s.History("a.md", 10)
	if len(hist) != 1 {
		t.Errorf("expected 1 commit, got %d", len(hist))
	}
}

func TestMoveAndRemove(t *testing.T) {
	s, dir := openTemp(t)
	write(t, dir, "old.md", "content\n")
	if _, err := s.Commit("old.md", "add", "T", "t@l"); err != nil {
		t.Fatal(err)
	}

	sha, err := s.MoveFile("old.md", "sub/new.md", "move", "T", "t@l")
	if err != nil {
		t.Fatalf("MoveFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.md")); !os.IsNotExist(err) {
		t.Error("old.md still exists on disk")
	}
	if body, err := s.ReadAtRef("sub/new.md", sha); err != nil || string(body) != "content\n" {
		t.Errorf("moved file at ref = (%q, %v)", body, err)
	}

	if err := s.RemoveFile("sub/new.md", "remove", "T", "t@l"); err != nil {
		t.Fatalf("RemoveFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub/new.md")); !os.IsNotExist(err) {
		t.Error("removed file still exists on disk")
	}
}

func TestEnsureRemoteAndHasRemote(t *testing.T) {
	s, _ := openTemp(t)
	if s.HasRemote("origin") {
		t.Error("fresh repo claims to have origin")
	}
	if err := s.EnsureRemote("origin", "https://example.com/a.git"); err != nil {
		t.Fatalf("EnsureRemote: %v", err)
	}
	if !s.HasRemote("origin") {
		t.Error("origin missing after EnsureRemote")
	}
	// Repoint is idempotent + updates the URL.
	if err := s.EnsureRemote("origin", "https://example.com/b.git"); err != nil {
		t.Fatalf("EnsureRemote repoint: %v", err)
	}
	r, err := s.repo.Remote("origin")
	if err != nil || r.Config().URLs[0] != "https://example.com/b.git" {
		t.Errorf("remote url after repoint = %v, %v", r, err)
	}
}

func TestPushToLocalBareRemote(t *testing.T) {
	s, dir := openTemp(t)
	write(t, dir, "a.md", "x\n")
	if _, err := s.Commit("a.md", "add", "T", "t@l"); err != nil {
		t.Fatal(err)
	}

	bare := t.TempDir()
	if _, err := git.PlainInit(bare, true); err != nil {
		t.Fatalf("init bare: %v", err)
	}
	if err := s.EnsureRemote("origin", bare); err != nil {
		t.Fatal(err)
	}
	if err := s.Push("origin"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	// Second push is up-to-date — must be nil too.
	if err := s.Push("origin"); err != nil {
		t.Fatalf("Push (up to date): %v", err)
	}
}
