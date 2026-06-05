package api

import (
	"net/http"

	"github.com/markusfluer/steelpage-desktop/internal/docs"
)

func (a *API) Tree(w http.ResponseWriter, r *http.Request) {
	entries, err := docs.Walk(a.Cfg.Repo.Path)
	if err != nil {
		logError("tree walk", err)
		writeError(w, http.StatusInternalServerError, "tree walk failed")
		return
	}
	writeJSON(w, http.StatusOK, entries)
}
