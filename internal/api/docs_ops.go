package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markusfluer/steelpage-desktop/internal/docs"
	"github.com/markusfluer/steelpage-desktop/internal/frontmatter"
	"github.com/markusfluer/steelpage-desktop/internal/localidentity"
)

// DeleteDoc removes the document file, drops its comments + index entries,
// and commits the deletion.
func (a *API) DeleteDoc(w http.ResponseWriter, r *http.Request) {
	docPath := chi.URLParam(r, "*")

	lk := a.pathLock(docPath)
	lk.Lock()
	defer lk.Unlock()

	if _, err := docs.SafeJoin(a.Cfg.Repo.Path, docPath); err != nil {
		writeError(w, httpStatusForDocErr(err), err.Error())
		return
	}
	if _, err := docs.Load(a.Cfg.Repo.Path, docPath); err != nil {
		writeError(w, httpStatusForDocErr(err), err.Error())
		return
	}

	authorName, authorEmail := authorFor(a.currentUser())
	if err := a.Git.RemoveFile(docPath, "docs: delete "+docPath, authorName, authorEmail); err != nil {
		logError("git rm", err)
		writeError(w, http.StatusInternalServerError, "failed to delete file")
		return
	}
	if err := a.Comments.DeletePath(docPath); err != nil {
		logError("comments delete on doc delete", err)
	}
	if err := a.Indexer.Remove(docPath); err != nil {
		logError("indexer remove", err)
	}
	a.maybePush()

	w.WriteHeader(http.StatusNoContent)
}

type moveDocRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// MoveDoc renames (or moves) a document. Requires write on both source and
// destination paths. Comments and search index follow the new path.
func (a *API) MoveDoc(w http.ResponseWriter, r *http.Request) {
	var req moveDocRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.From == "" || req.To == "" || req.From == req.To {
		writeError(w, http.StatusBadRequest, "from and to must be non-empty and different")
		return
	}

	lkFrom := a.pathLock(req.From)
	lkTo := a.pathLock(req.To)
	// Lock in deterministic order to avoid deadlocks under concurrent moves.
	if req.From < req.To {
		lkFrom.Lock()
		lkTo.Lock()
	} else {
		lkTo.Lock()
		lkFrom.Lock()
	}
	defer lkFrom.Unlock()
	defer lkTo.Unlock()

	fromAbs, err := docs.SafeJoin(a.Cfg.Repo.Path, req.From)
	if err != nil {
		writeError(w, httpStatusForDocErr(err), err.Error())
		return
	}
	if _, err := docs.SafeJoin(a.Cfg.Repo.Path, req.To); err != nil {
		writeError(w, httpStatusForDocErr(err), err.Error())
		return
	}
	if _, err := docs.Load(a.Cfg.Repo.Path, req.From); err != nil {
		writeError(w, httpStatusForDocErr(err), err.Error())
		return
	}
	if _, err := docs.Load(a.Cfg.Repo.Path, req.To); err == nil {
		writeError(w, http.StatusConflict, "destination already exists")
		return
	}
	_ = fromAbs

	authorName, authorEmail := authorFor(a.currentUser())
	msg := "docs: move " + req.From + " -> " + req.To
	newSHA, err := a.Git.MoveFile(req.From, req.To, msg, authorName, authorEmail)
	if err != nil {
		logError("git mv", err)
		writeError(w, http.StatusInternalServerError, "failed to move file")
		return
	}

	if err := a.Comments.MovePath(req.From, req.To); err != nil {
		logError("comments move", err)
	}
	if err := a.Indexer.Remove(req.From); err != nil {
		logError("indexer drop source", err)
	}
	// Reload + reindex destination using the on-disk body.
	if raw, err := docs.Load(a.Cfg.Repo.Path, req.To); err == nil {
		_, body, _ := frontmatter.Split(raw)
		_ = a.Indexer.IndexOne(a.Cfg.Repo.Path, req.To, string(body))
	}
	a.maybePush()

	resp, _ := a.buildResponseAfter(req.To, newSHA)
	writeJSON(w, http.StatusOK, resp)
}

type copyDocRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// CopyDoc duplicates a document. The new file gets fresh frontmatter
// (created=now, version=1) so its history starts clean.
func (a *API) CopyDoc(w http.ResponseWriter, r *http.Request) {
	var req copyDocRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.From == "" || req.To == "" || req.From == req.To {
		writeError(w, http.StatusBadRequest, "from and to must be non-empty and different")
		return
	}

	lk := a.pathLock(req.To)
	lk.Lock()
	defer lk.Unlock()

	if _, err := docs.SafeJoin(a.Cfg.Repo.Path, req.From); err != nil {
		writeError(w, httpStatusForDocErr(err), err.Error())
		return
	}
	if _, err := docs.SafeJoin(a.Cfg.Repo.Path, req.To); err != nil {
		writeError(w, httpStatusForDocErr(err), err.Error())
		return
	}
	if _, err := docs.Load(a.Cfg.Repo.Path, req.To); err == nil {
		writeError(w, http.StatusConflict, "destination already exists")
		return
	}
	srcRaw, err := docs.Load(a.Cfg.Repo.Path, req.From)
	if err != nil {
		writeError(w, httpStatusForDocErr(err), err.Error())
		return
	}

	authorName, authorEmail := authorFor(a.currentUser())

	// Strip source frontmatter, give the copy a fresh one.
	_, body, _ := frontmatter.Split(srcRaw)
	fm := map[string]any{}
	frontmatter.ApplyUpdate(fm, authorName, time.Now())
	combined, err := frontmatter.Recombine(fm, body)
	if err != nil {
		logError("recombine on copy", err)
		writeError(w, http.StatusInternalServerError, "failed to build copy")
		return
	}

	if err := docs.Write(a.Cfg.Repo.Path, req.To, combined); err != nil {
		writeError(w, httpStatusForDocErr(err), err.Error())
		return
	}
	msg := "docs: copy " + req.From + " -> " + req.To
	newSHA, err := a.Git.Commit(req.To, msg, authorName, authorEmail)
	if err != nil {
		logError("git commit on copy", err)
		writeError(w, http.StatusInternalServerError, "failed to commit copy")
		return
	}
	if err := a.Indexer.IndexOne(a.Cfg.Repo.Path, req.To, string(body)); err != nil {
		logError("indexer on copy", err)
	}
	a.maybePush()

	resp, _ := a.buildResponseAfter(req.To, newSHA)
	writeJSON(w, http.StatusCreated, resp)
}

// buildResponseAfter is a thin wrapper used by Move/Copy: reload the file
// and assemble a DocumentResponse using the same path as Save's success.
func (a *API) buildResponseAfter(docPath, sha string) (*DocumentResponse, error) {
	raw, err := docs.Load(a.Cfg.Repo.Path, docPath)
	if err != nil {
		return nil, err
	}
	resp, err := a.buildResponse(docPath, raw)
	if err != nil {
		return nil, err
	}
	if sha != "" {
		resp.SHA = sha
	}
	return resp, nil
}

// authorFor returns the (name, email) to record on git operations — always
// the local OS user in the desktop build.
func authorFor(id localidentity.Identity) (string, string) {
	return id.DisplayName, id.Email
}

// maybePush fans the latest commits out to the configured remote in the
// background when auto_push is enabled (opt-in, push-only — no pull/rebase
// in the desktop build).
func (a *API) maybePush() {
	if !a.Cfg.Repo.AutoPush {
		return
	}
	if !a.Git.HasRemote(a.Cfg.Repo.PushRemote) {
		return
	}
	git := a.Git
	remote := a.Cfg.Repo.PushRemote
	go func() {
		if err := git.Push(remote); err != nil {
			logError("git push", err)
		}
	}()
}
