package server

import (
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/markusfluer/steelpage-desktop/internal/api"
	"github.com/markusfluer/steelpage-desktop/internal/static"
)

// New wires the loopback router for the desktop build: no sessions, no
// identity middleware, no auth/admin surface. Only the webview talks to it.
func New(a *api.API, dist fs.FS) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)

	r.Route("/api", func(r chi.Router) {
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

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/README.md", http.StatusFound)
	})

	r.NotFound(staticHandler.ServeHTTP)

	return r
}
