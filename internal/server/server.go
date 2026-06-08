package server

import (
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/markusfluer/steelpage-desktop/internal/api"
	"github.com/markusfluer/steelpage-desktop/internal/control"
	"github.com/markusfluer/steelpage-desktop/internal/remote"
	"github.com/markusfluer/steelpage-desktop/internal/static"
)

func baseRouter() *chi.Mux {
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	return r
}

// mountControlPlane registers the local-only endpoints that work the same in
// both modes: preferences and connection status/test.
func mountControlPlane(r chi.Router) {
	r.Get("/prefs", control.GetPrefs)
	r.Patch("/prefs", control.PatchPrefs)
	r.Get("/connection", control.Connection)
	r.Post("/connection/test", control.TestConnection)
}

// NewLocal wires the loopback router against the local archive: no sessions,
// no identity middleware, no auth surface. Only the webview talks to it.
func NewLocal(a *api.API, dist fs.FS) http.Handler {
	r := baseRouter()

	r.Route("/api", func(r chi.Router) {
		mountControlPlane(r)
		r.Get("/me", control.LocalMe)

		// Reads.
		r.Get("/tree", a.Tree)
		r.Get("/docs/*", a.GetDoc)
		r.Get("/docs-history/*", a.GetDocHistory)
		r.Get("/search", a.Search)
		r.Get("/comments", a.ListComments)
		r.Post("/render", a.Render)

		// Writes.
		r.Put("/docs/*", a.PutDoc)
		r.Delete("/docs/*", a.DeleteDoc)
		r.Post("/docs-move", a.MoveDoc)
		r.Post("/docs-copy", a.CopyDoc)
		r.Post("/comments", a.CreateComment)
		r.Patch("/comments/{id}", a.UpdateComment)
	})

	staticHandler := static.Handler(dist)

	r.Get("/docs/*", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("botready") == "1" {
			a.BotReady(w, r)
			return
		}
		staticHandler.ServeHTTP(w, r)
	})

	mountShell(r, staticHandler)
	return r
}

// NewProxy wires the loopback router as a thin client of a remote steelpage
// server: the control plane stays local, everything else under /api is
// reverse-proxied with the bearer token. /docs/* serves the local SPA unless
// it's a ?botready=1 request, which is proxied too.
func NewProxy(serverURL, token string, dist fs.FS) (http.Handler, error) {
	proxy, err := remote.Proxy(serverURL, token)
	if err != nil {
		return nil, err
	}
	r := baseRouter()

	r.Route("/api", func(r chi.Router) {
		mountControlPlane(r)
		// Everything else under /api goes to the server (chi matches the
		// explicit control-plane routes above before this catch-all).
		r.Handle("/*", proxy)
	})

	staticHandler := static.Handler(dist)

	r.Get("/docs/*", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("botready") == "1" {
			proxy.ServeHTTP(w, req)
			return
		}
		staticHandler.ServeHTTP(w, req)
	})

	mountShell(r, staticHandler)
	return r, nil
}

// mountShell adds the SPA entry points shared by both modes.
func mountShell(r chi.Router, staticHandler http.Handler) {
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/README.md", http.StatusFound)
	})
	r.NotFound(staticHandler.ServeHTTP)
}
