package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/markusfluer/steelpage-desktop/internal/gitstore"
)

// GetDocHistory returns the last N commits that touched the document. Same
// read-permission gate as GetDoc.
func (a *API) GetDocHistory(w http.ResponseWriter, r *http.Request) {
	docPath := chi.URLParam(r, "*")

	limit := 10
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}
	entries, err := a.Git.History(docPath, limit)
	if err != nil {
		logError("doc history", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch history")
		return
	}
	if entries == nil {
		entries = []gitstore.HistoryEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}
