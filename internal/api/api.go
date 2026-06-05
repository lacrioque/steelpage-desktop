package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"

	"github.com/alexedwards/scs/v2"

	"github.com/markusfluer/steelpage-desktop/internal/auth"
	"github.com/markusfluer/steelpage-desktop/internal/comments"
	"github.com/markusfluer/steelpage-desktop/internal/config"
	"github.com/markusfluer/steelpage-desktop/internal/configsvc"
	"github.com/markusfluer/steelpage-desktop/internal/docs"
	"github.com/markusfluer/steelpage-desktop/internal/gitstore"
	"github.com/markusfluer/steelpage-desktop/internal/groups"
	"github.com/markusfluer/steelpage-desktop/internal/mailer"
	"github.com/markusfluer/steelpage-desktop/internal/permissions"
	"github.com/markusfluer/steelpage-desktop/internal/render"
	"github.com/markusfluer/steelpage-desktop/internal/search"
	"github.com/markusfluer/steelpage-desktop/internal/tokens"
	"github.com/markusfluer/steelpage-desktop/internal/users"
)

type API struct {
	Cfg         *config.Config
	Renderer    *render.Renderer
	Git         *gitstore.Store
	Users       *users.Store
	Groups      *groups.Store
	Comments    *comments.Store
	Indexer     *search.Indexer
	SearchStore *search.Store
	Sessions    *scs.SessionManager
	Auth        *auth.Service
	Permissions *permissions.Store
	Tokens      *tokens.Store
	Mailer      mailer.Mailer
	Configsvc   *configsvc.Service

	saveMu  sync.Mutex
	saveLks map[string]*sync.Mutex
}

func New(
	cfg *config.Config,
	r *render.Renderer,
	g *gitstore.Store,
	u *users.Store,
	gs *groups.Store,
	c *comments.Store,
	idx *search.Indexer,
	ss *search.Store,
	sm *scs.SessionManager,
	authSvc *auth.Service,
	perms *permissions.Store,
	toks *tokens.Store,
	mail mailer.Mailer,
	cfgsvc *configsvc.Service,
) *API {
	return &API{
		Cfg:         cfg,
		Renderer:    r,
		Git:         g,
		Users:       u,
		Groups:      gs,
		Comments:    c,
		Indexer:     idx,
		SearchStore: ss,
		Sessions:    sm,
		Auth:        authSvc,
		Permissions: perms,
		Tokens:      toks,
		Mailer:      mail,
		Configsvc:   cfgsvc,
		saveLks:     make(map[string]*sync.Mutex),
	}
}

// cfg returns the current effective config (YAML + overrides). Handlers that
// need live values should prefer this over a.Cfg, which is the immutable
// cold-start baseline.
func (a *API) cfg() *config.Config {
	return a.Configsvc.Snapshot()
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
