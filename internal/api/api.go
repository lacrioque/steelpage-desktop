package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"

	"github.com/markusfluer/steelpage-desktop/internal/comments"
	"github.com/markusfluer/steelpage-desktop/internal/config"
	"github.com/markusfluer/steelpage-desktop/internal/docs"
	"github.com/markusfluer/steelpage-desktop/internal/gitstore"
	"github.com/markusfluer/steelpage-desktop/internal/render"
	"github.com/markusfluer/steelpage-desktop/internal/search"
)

type API struct {
	Cfg         *config.Config
	Renderer    *render.Renderer
	Git         *gitstore.Store
	Comments    *comments.Store
	Indexer     *search.Indexer
	SearchStore *search.Store

	saveMu  sync.Mutex
	saveLks map[string]*sync.Mutex
}

func New(
	cfg *config.Config,
	r *render.Renderer,
	g *gitstore.Store,
	c *comments.Store,
	idx *search.Indexer,
	ss *search.Store,
) *API {
	return &API{
		Cfg:         cfg,
		Renderer:    r,
		Git:         g,
		Comments:    c,
		Indexer:     idx,
		SearchStore: ss,
		saveLks:     make(map[string]*sync.Mutex),
	}
}

func (a *API) pathLock(p string) *sync.Mutex {
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	if m, ok := a.saveLks[p]; ok {
		return m
	}
	m := &sync.Mutex{}
	a.saveLks[p] = m
	return m
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func httpStatusForDocErr(err error) int {
	switch {
	case errors.Is(err, docs.ErrOutsideRoot):
		return http.StatusBadRequest
	case errors.Is(err, docs.ErrNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

func logError(prefix string, err error) {
	log.Printf("%s: %v", prefix, err)
}
