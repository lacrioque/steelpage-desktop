package api

import (
	"net/http"
	"strconv"

	"github.com/markusfluer/steelpage-desktop/internal/middleware"
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
	// Pull extra in case we filter some out so the requested limit is still
	// satisfied for the common case where many results are visible.
	searchLimit := limit * 3
	if searchLimit < limit {
		searchLimit = limit
	}
	results, err := a.SearchStore.Search(q, searchLimit)
	if err != nil {
		logError("search", err)
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}
	user := middleware.FromContext(r.Context())
	filtered := make([]search.Result, 0, len(results))
	for _, res := range results {
		if a.canRead(res.Path, user) {
			filtered = append(filtered, res)
			if len(filtered) >= limit {
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, filtered)
}
