package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markusfluer/steelpage-desktop/internal/comments"
	"github.com/markusfluer/steelpage-desktop/internal/docs"
	"github.com/markusfluer/steelpage-desktop/internal/frontmatter"
)

func splitLines(body string) []string {
	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	return strings.Split(normalized, "\n")
}

type DocumentResponse struct {
	Path        string         `json:"path"`
	Title       string         `json:"title"`
	Frontmatter map[string]any `json:"frontmatter"`
	Markdown    string         `json:"markdown"`
	HTML        string         `json:"html"`
	SHA         string         `json:"sha"`
	Updated     string         `json:"updated"`
	Comments    []any          `json:"comments"`
	ViewingRef  string         `json:"viewing_ref,omitempty"`
}

type putDocRequest struct {
	Markdown string `json:"markdown"`
}

func (a *API) GetDoc(w http.ResponseWriter, r *http.Request) {
	docPath := chi.URLParam(r, "*")

	ref := r.URL.Query().Get("ref")
	var (
		raw []byte
		err error
	)
	if ref != "" {
		raw, err = a.Git.ReadAtRef(docPath, ref)
		if err != nil {
			writeError(w, http.StatusNotFound, "revision not found: "+err.Error())
			return
		}
	} else {
		raw, err = docs.Load(a.Cfg.Repo.Path, docPath)
		if err != nil {
			writeError(w, httpStatusForDocErr(err), err.Error())
			return
		}
	}

	resp, err := a.buildResponse(docPath, raw)
	if err != nil {
		logError("build document response", err)
		writeError(w, http.StatusInternalServerError, "failed to build document")
		return
	}
	if ref != "" {
		resp.ViewingRef = ref
		resp.SHA = ref
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) PutDoc(w http.ResponseWriter, r *http.Request) {
	docPath := chi.URLParam(r, "*")

	var req putDocRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	lk := a.pathLock(docPath)
	lk.Lock()
	defer lk.Unlock()

	authorName, authorEmail := authorFor(a.currentUser())

	fm := map[string]any{}
	if existing, err := docs.Load(a.Cfg.Repo.Path, docPath); err == nil {
		if header, _, hasHeader := frontmatter.Split(existing); hasHeader {
			if parsed, perr := frontmatter.Parse(header); perr == nil {
				fm = parsed
			}
		}
	}

	frontmatter.ApplyUpdate(fm, authorName, time.Now())

	combined, err := frontmatter.Recombine(fm, []byte(req.Markdown))
	if err != nil {
		logError("recombine frontmatter", err)
		writeError(w, http.StatusInternalServerError, "failed to serialize document")
		return
	}

	if err := docs.Write(a.Cfg.Repo.Path, docPath, combined); err != nil {
		writeError(w, httpStatusForDocErr(err), err.Error())
		return
	}

	msg := "docs: update " + docPath
	newSHA, err := a.Git.Commit(docPath, msg, authorName, authorEmail)
	if err != nil {
		logError("git commit", err)
		writeError(w, http.StatusInternalServerError, "failed to commit change")
		return
	}

	// Audit log lives in git: the commit above already records who saved
	// what (display name + email). When auto_push is enabled we also fan
	// the commit out to the configured remote in the background.
	a.maybePush()

	if err := a.Comments.MarkPath(docPath, newSHA, splitLines(req.Markdown)); err != nil {
		logError("reanchor comments", err)
	}

	if err := a.Indexer.IndexOne(a.Cfg.Repo.Path, docPath, req.Markdown); err != nil {
		logError("reindex doc", err)
	}

	resp, err := a.buildResponse(docPath, combined)
	if err != nil {
		logError("build response after save", err)
		writeError(w, http.StatusInternalServerError, "failed to build response")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) BotReady(w http.ResponseWriter, r *http.Request) {
	docPath := chi.URLParam(r, "*")
	raw, err := docs.Load(a.Cfg.Repo.Path, docPath)
	if err != nil {
		writeError(w, httpStatusForDocErr(err), err.Error())
		return
	}

	resp, err := a.buildResponse(docPath, raw)
	if err != nil {
		logError("bot-ready build response", err)
		writeError(w, http.StatusInternalServerError, "failed to build response")
		return
	}
	openComments, _ := a.Comments.ListByPath(docPath)

	if r.URL.Query().Get("format") == "json" {
		writeJSON(w, http.StatusOK, map[string]any{
			"path":        resp.Path,
			"title":       resp.Title,
			"frontmatter": resp.Frontmatter,
			"markdown":    resp.Markdown,
			"html":        resp.HTML,
			"comments":    openComments,
			"git": map[string]string{
				"sha":     resp.SHA,
				"updated": resp.Updated,
			},
		})
		return
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	writeBotReadyMarkdown(w, resp, openComments)
}

func writeBotReadyMarkdown(w http.ResponseWriter, resp *DocumentResponse, allComments []*comments.Comment) {
	fmt.Fprintf(w, "# %s\n\n", resp.Title)
	fmt.Fprintf(w, "Path: %s\n", resp.Path)
	if resp.Updated != "" {
		fmt.Fprintf(w, "Updated: %s\n", resp.Updated)
	}
	if resp.SHA != "" {
		fmt.Fprintf(w, "Git SHA: %s\n", resp.SHA)
	}
	if version, ok := resp.Frontmatter["version"]; ok && version != nil {
		fmt.Fprintf(w, "Version: %v\n", version)
	}
	if tags, ok := resp.Frontmatter["tags"]; ok {
		fmt.Fprintf(w, "Tags: %s\n", joinTags(tags))
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "## Content")
	fmt.Fprintln(w)
	fmt.Fprintln(w, resp.Markdown)

	open := filterActive(allComments)
	if len(open) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "---")
		fmt.Fprintln(w)
		fmt.Fprintf(w, "## Open Comments (%d)\n\n", len(open))
		for _, c := range open {
			tag := "open"
			if c.Status == comments.StatusRelocated {
				tag = "relocated"
			}
			fmt.Fprintf(w, "- Line %d (%s, %s): %s\n", c.LineStart, c.Author.DisplayName, tag, oneLine(c.Body))
		}
	}
}

func joinTags(v any) string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return strings.Join(out, ", ")
	case string:
		return t
	}
	return ""
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func filterActive(list []*comments.Comment) []*comments.Comment {
	out := make([]*comments.Comment, 0, len(list))
	for _, c := range list {
		if c.Status == comments.StatusOpen || c.Status == comments.StatusRelocated {
			out = append(out, c)
		}
	}
	return out
}

func (a *API) buildResponse(docPath string, raw []byte) (*DocumentResponse, error) {
	header, body, hasHeader := frontmatter.Split(raw)

	var fm map[string]any
	if hasHeader {
		parsed, err := frontmatter.Parse(header)
		if err != nil {
			return nil, err
		}
		fm = parsed
	} else {
		fm = map[string]any{}
		body = raw
	}

	htmlOut, err := a.Renderer.Render(body)
	if err != nil {
		return nil, err
	}

	sha, _ := a.Git.HeadSHA(docPath)
	updated := ""
	if t, err := a.Git.LastModified(docPath); err == nil && !t.IsZero() {
		updated = t.Format(time.RFC3339)
	}

	resp := &DocumentResponse{
		Path:        docPath,
		Title:       frontmatter.Title(fm, docPath),
		Frontmatter: fm,
		Markdown:    string(body),
		HTML:        htmlOut,
		SHA:         sha,
		Updated:     updated,
		Comments:    []any{},
	}
	return resp, nil
}
