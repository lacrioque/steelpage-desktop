package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/markusfluer/steelpage-desktop/internal/comments"
)

type createCommentRequest struct {
	Path       string `json:"path"`
	LineStart  int    `json:"line_start"`
	LineEnd    int    `json:"line_end"`
	AnchorText string `json:"anchor_text"`
	Body       string `json:"body"`
	ReplyTo    *int64 `json:"reply_to,omitempty"`
}

type updateCommentRequest struct {
	Body   *string `json:"body,omitempty"`
	Status *string `json:"status,omitempty"`
}

func (a *API) ListComments(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path query parameter required")
		return
	}
	if _, status := a.authorize(r, path, "read"); !denyOrContinue(w, status) {
		return
	}
	list, err := a.Comments.ListByPath(path)
	if err != nil {
		logError("list comments", err)
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	if list == nil {
		list = []*comments.Comment{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *API) CreateComment(w http.ResponseWriter, r *http.Request) {
	var req createCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	u, status := a.authorize(r, req.Path, "comment")
	if !denyOrContinue(w, status) {
		return
	}
	if u == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	sha, _ := a.Git.HeadSHA(req.Path)
	c, err := a.Comments.Create(comments.CreateInput{
		Path:        req.Path,
		LineStart:   req.LineStart,
		LineEnd:     req.LineEnd,
		AnchorText:  req.AnchorText,
		DocumentSHA: sha,
		AuthorID:    u.ID,
		Body:        req.Body,
		ReplyTo:     req.ReplyTo,
	})
	if err != nil {
		if errors.Is(err, comments.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "invalid comment payload")
			return
		}
		logError("create comment", err)
		writeError(w, http.StatusInternalServerError, "failed to create comment")
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (a *API) UpdateComment(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	existing, err := a.Comments.GetByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	if _, status := a.authorize(r, existing.Path, "comment"); !denyOrContinue(w, status) {
		return
	}

	var req updateCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	c, err := a.Comments.Update(id, comments.UpdateInput{Body: req.Body, Status: req.Status})
	if err != nil {
		switch {
		case errors.Is(err, comments.ErrNotFound):
			writeError(w, http.StatusNotFound, "comment not found")
		case errors.Is(err, comments.ErrInvalid):
			writeError(w, http.StatusBadRequest, "invalid update payload")
		default:
			logError("update comment", err)
			writeError(w, http.StatusInternalServerError, "failed to update comment")
		}
		return
	}
	writeJSON(w, http.StatusOK, c)
}
