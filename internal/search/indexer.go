package search

import (
	"database/sql"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/markusfluer/steelpage-desktop/internal/frontmatter"
	"github.com/markusfluer/steelpage-desktop/internal/gitstore"
)

type Indexer struct {
	DB  *sql.DB
	Git *gitstore.Store
}

func New(db *sql.DB, git *gitstore.Store) *Indexer {
	return &Indexer{DB: db, Git: git}
}

// IndexAll walks the repo and indexes every *.md file. Called on startup.
// Returns the number of indexed documents.
func (i *Indexer) IndexAll(repoPath string) (int, error) {
	rootAbs, err := filepath.Abs(repoPath)
	if err != nil {
		return 0, err
	}
	count := 0
	err = filepath.WalkDir(rootAbs, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || (strings.HasPrefix(name, ".") && path != rootAbs) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			return nil
		}
		rel, rerr := filepath.Rel(rootAbs, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			log.Printf("search: read %s: %v", rel, rerr)
			return nil
		}
		if err := i.indexFromRaw(rel, raw); err != nil {
			log.Printf("search: index %s: %v", rel, err)
		}
		count++
		return nil
	})
	if err != nil {
		return count, err
	}
	return count, nil
}

// IndexOne re-indexes a single document. `body` is the markdown body the
// user just saved (no frontmatter). Frontmatter is loaded from disk so the
// index sees the canonical post-save state.
func (i *Indexer) IndexOne(repoPath, docPath, body string) error {
	full := filepath.Join(repoPath, docPath)
	raw, err := os.ReadFile(full)
	if err != nil {
		return fmt.Errorf("read %s: %w", docPath, err)
	}
	header, _, hasHeader := frontmatter.Split(raw)
	var fm map[string]any
	if hasHeader {
		fm, _ = frontmatter.Parse(header)
	}
	if fm == nil {
		fm = map[string]any{}
	}
	return i.write(docPath, fm, body)
}

func (i *Indexer) indexFromRaw(docPath string, raw []byte) error {
	header, body, hasHeader := frontmatter.Split(raw)
	var fm map[string]any
	if hasHeader {
		fm, _ = frontmatter.Parse(header)
	}
	if fm == nil {
		fm = map[string]any{}
	}
	return i.write(docPath, fm, string(body))
}

// Remove drops a document from both the cache and FTS tables. Used when a
// file is deleted or moved (caller then Indexes the new path).
func (i *Indexer) Remove(docPath string) error {
	tx, err := i.DB.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM documents WHERE path = ?`, docPath); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM documents_fts WHERE path = ?`, docPath); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (i *Indexer) write(docPath string, fm map[string]any, body string) error {
	extracted := FromBody(body, fm, filepath.Base(docPath))
	sha, _ := i.Git.HeadSHA(docPath)
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := i.DB.Begin()
	if err != nil {
		return err
	}

	if _, err := tx.Exec(`
		INSERT INTO documents(path, title, sha, headings, tags, body, indexed_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			title = excluded.title,
			sha = excluded.sha,
			headings = excluded.headings,
			tags = excluded.tags,
			body = excluded.body,
			indexed_at = excluded.indexed_at`,
		docPath, extracted.Title, sha, extracted.Headings, extracted.Tags, extracted.Body, now,
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("upsert documents: %w", err)
	}

	// FTS5 has no UPSERT — delete + insert keeps the row in sync.
	if _, err := tx.Exec(`DELETE FROM documents_fts WHERE path = ?`, docPath); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete fts row: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO documents_fts(path, title, headings, body, tags)
		VALUES(?, ?, ?, ?, ?)`,
		docPath, extracted.Title, extracted.Headings, extracted.Body, extracted.Tags,
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("insert fts row: %w", err)
	}

	return tx.Commit()
}
