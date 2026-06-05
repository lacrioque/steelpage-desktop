package api

import (
	"net/http"
	"strconv"

	"github.com/markusfluer/steelpage-desktop/internal/search"
)

func (a *API) Search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q query parameter required")
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}
	results, err := a.SearchStore.Search(q, limit)
	if err != nil {
		logError("search", err)
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}
	if results == nil {
		results = []search.Result{}
	}
	writeJSON(w, http.StatusOK, results)
}
