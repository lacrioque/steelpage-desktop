package api

import (
	"net/http"

	"github.com/markusfluer/steelpage-desktop/internal/docs"
	"github.com/markusfluer/steelpage-desktop/internal/middleware"
	"github.com/markusfluer/steelpage-desktop/internal/permissions"
	"github.com/markusfluer/steelpage-desktop/internal/users"
)

func (a *API) Tree(w http.ResponseWriter, r *http.Request) {
	entries, err := docs.Walk(a.Cfg.Repo.Path)
	if err != nil {
		logError("tree walk", err)
		writeError(w, http.StatusInternalServerError, "tree walk failed")
		return
	}
	user := middleware.FromContext(r.Context())
	filtered := make([]docs.TreeEntry, 0, len(entries))
	for _, e := range entries {
		if a.canRead(e.Path, user) {
			filtered = append(filtered, e)
		}
	}
	writeJSON(w, http.StatusOK, filtered)
}

// canRead is the lightweight predicate used to filter list endpoints (tree,
// search). It mirrors `authorize` but returns a bool so we can drop entries
// instead of failing the whole request.
func (a *API) canRead(path string, user *users.User) bool {
	allowed, mustFallback, err := a.Permissions.Allows(path, user, permissions.PermRead)
	if err != nil {
		return false
	}
	if !mustFallback {
		return allowed
	}
	if a.Cfg.Auth.AllowAnonymousRead {
		return true
	}
	return user != nil
}
