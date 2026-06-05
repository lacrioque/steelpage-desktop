package server

import (
	"io/fs"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/markusfluer/steelpage-desktop/internal/api"
	"github.com/markusfluer/steelpage-desktop/internal/config"
	"github.com/markusfluer/steelpage-desktop/internal/groups"
	"github.com/markusfluer/steelpage-desktop/internal/middleware"
	"github.com/markusfluer/steelpage-desktop/internal/static"
	"github.com/markusfluer/steelpage-desktop/internal/tokens"
	"github.com/markusfluer/steelpage-desktop/internal/users"
)

func New(
	cfg *config.Config,
	a *api.API,
	sm *scs.SessionManager,
	usersStore *users.Store,
	groupsStore *groups.Store,
	tokensStore *tokens.Store,
	dist fs.FS,
) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(sm.LoadAndSave)
	r.Use(middleware.Identity(sm, usersStore, groupsStore, tokensStore))

	r.Route("/api", func(r chi.Router) {
		// Auth — always public.
		r.Get("/me", a.GetMe)
		r.Patch("/me", a.PatchMe)
		r.Get("/me/tokens", a.ListMyTokens)
		r.Post("/me/tokens", a.CreateMyToken)
		r.Delete("/me/tokens/{id}", a.DeleteMyToken)
		r.Get("/auth/providers", a.Auth.Capabilities)
		r.Post("/auth/register", a.Auth.Register)
		r.Post("/auth/login", a.Auth.Login)
		r.Post("/auth/logout", a.Auth.Logout)
		r.Get("/auth/oidc/start", a.Auth.OIDCStart)
		r.Get("/auth/oidc/callback", a.Auth.OIDCCallback)
		r.Post("/auth/forgot", a.Auth.ForgotPassword)
		r.Post("/auth/reset", a.Auth.ResetPassword)
		r.Post("/auth/verify", a.Auth.VerifyEmail)
		r.Post("/auth/resend-verification", a.Auth.ResendVerification)
		r.Post("/auth/login/mfa", a.Auth.LoginMFA)
		r.Post("/auth/mfa/setup-start", a.Auth.MFASetupStart)
		r.Post("/auth/mfa/setup-confirm", a.Auth.MFASetupConfirm)
		r.Post("/auth/mfa/disable", a.Auth.MFADisable)

		// Reads — handlers call authorize() internally; no route-level auth gate.
		// (path-based permissions can either tighten or honor the anon-read default)
		r.Get("/tree", a.Tree)
		r.Get("/docs/*", a.GetDoc)
		r.Get("/docs-history/*", a.GetDocHistory)
		r.Get("/search", a.Search)
		r.Get("/comments", a.ListComments)
		r.Post("/render", a.Render)

		// Writes — handlers call authorize() with the appropriate action.
		r.Put("/docs/*", a.PutDoc)
		r.Delete("/docs/*", a.DeleteDoc)
		r.Post("/docs-move", a.MoveDoc)
		r.Post("/docs-copy", a.CopyDoc)
		r.Post("/comments", a.CreateComment)
		r.Patch("/comments/{id}", a.UpdateComment)

		// Admin.
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAdmin)
			r.Get("/admin/users", a.AdminListUsers)
			r.Patch("/admin/users/{id}", a.AdminPatchUser)
			r.Post("/admin/users/{id}/mfa/disable", a.AdminDisableUserMFA)
			r.Get("/admin/groups", a.AdminListGroups)
			r.Post("/admin/groups", a.AdminCreateGroup)
			r.Delete("/admin/groups/{id}", a.AdminDeleteGroup)
			r.Post("/admin/groups/{id}/members", a.AdminAddMember)
			r.Delete("/admin/groups/{id}/members/{user_id}", a.AdminRemoveMember)
			r.Get("/admin/permissions", a.AdminListPermissions)
			r.Post("/admin/permissions", a.AdminCreatePermission)
			r.Delete("/admin/permissions/{id}", a.AdminDeletePermission)
			r.Get("/admin/permissions/effective", a.AdminEffectivePermissions)
			r.Get("/admin/git/status", a.AdminGitStatus)
			r.Post("/admin/git/pull", a.AdminGitPull)
			r.Post("/admin/git/push", a.AdminGitPush)
			r.Post("/admin/git/abort", a.AdminGitAbort)
			r.Get("/admin/mailer/status", a.AdminMailerStatus)
			r.Post("/admin/mailer/test", a.AdminMailerTest)
			r.Get("/admin/config/schema", a.AdminConfigSchema)
			r.Get("/admin/config/effective", a.AdminConfigEffective)
			r.Patch("/admin/config", a.AdminConfigPatch)
			r.Get("/admin/config/audit", a.AdminConfigAudit)
			r.Get("/admin/config/export", a.AdminConfigExport)
		})
	})

	staticHandler := static.Handler(dist)

	r.Get("/docs/*", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("botready") == "1" {
			if !cfg.Auth.AllowAnonymousRead && middleware.FromContext(r.Context()) == nil {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
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
