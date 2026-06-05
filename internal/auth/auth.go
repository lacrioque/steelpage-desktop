package auth

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"golang.org/x/crypto/bcrypt"

	"github.com/markusfluer/steelpage-desktop/internal/config"
	"github.com/markusfluer/steelpage-desktop/internal/configsvc"
	"github.com/markusfluer/steelpage-desktop/internal/mailer"
	"github.com/markusfluer/steelpage-desktop/internal/middleware"
	"github.com/markusfluer/steelpage-desktop/internal/users"
)

const (
	bcryptCost     = 12
	minPasswordLen = 8
)

type Service struct {
	Cfg       *config.Config
	Users     *users.Store
	Sessions  *scs.SessionManager
	Mailer    mailer.Mailer
	DB        *sql.DB
	Configsvc *configsvc.Service
}

func New(cfg *config.Config, u *users.Store, sm *scs.SessionManager, m mailer.Mailer, db *sql.DB, cfgsvc *configsvc.Service) *Service {
	return &Service{Cfg: cfg, Users: u, Sessions: sm, Mailer: m, DB: db, Configsvc: cfgsvc}
}

// live returns the current effective config — used for fields the admin can
// override at runtime (LocalEnabled, base_url, …).
func (s *Service) live() *config.Config {
	if s.Configsvc != nil {
		return s.Configsvc.Snapshot()
	}
	return s.Cfg
}

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type providerSummary struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

type capabilitiesResponse struct {
	LocalEnabled       bool              `json:"local_enabled"`
	AllowAnonymousRead bool              `json:"allow_anonymous_read"`
	Providers          []providerSummary `json:"providers"`
}

// Register handles POST /api/auth/register.
func (s *Service) Register(w http.ResponseWriter, r *http.Request) {
	if !s.live().Auth.LocalEnabled {
		writeError(w, http.StatusForbidden, "local sign-up is disabled")
		return
	}

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(req.Password) < minPasswordLen {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("password must be at least %d characters", minPasswordLen))
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	u, err := s.Users.CreateLocal(req.Email, req.DisplayName, string(hash), users.RoleUser)
	if err != nil {
		switch {
		case errors.Is(err, users.ErrEmailTaken):
			writeError(w, http.StatusConflict, "email already registered")
		case errors.Is(err, users.ErrInvalidEmail),
			errors.Is(err, users.ErrInvalidName),
			errors.Is(err, users.ErrInvalidRole):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to create user")
		}
		return
	}

	if err := s.startSession(r, u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start session")
		return
	}
	// Best-effort verification email — failures don't block registration so
	// new accounts still work when SMTP is misconfigured or down.
	s.SendVerificationOnRegister(u)
	writeJSON(w, http.StatusCreated, u)
}

// Login handles POST /api/auth/login.
func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	if !s.live().Auth.LocalEnabled {
		writeError(w, http.StatusForbidden, "local sign-in is disabled")
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	u, err := s.Users.FindByEmail(req.Email)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if u.PasswordHash == "" {
		writeError(w, http.StatusUnauthorized, "this account has no password set")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// MFA gate: when enabled we don't grant a session yet — we stash a
	// pending user_id and ask the client to follow up with /api/auth/login/mfa.
	if requireMFA(u) {
		s.Sessions.Put(r.Context(), SessionMFAPendingKey, u.ID)
		writeJSON(w, http.StatusOK, map[string]any{"mfa_required": true})
		return
	}

	if err := s.startSession(r, u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start session")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// Logout destroys the current session and clears the cookie.
func (s *Service) Logout(w http.ResponseWriter, r *http.Request) {
	if err := s.Sessions.Destroy(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to destroy session")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Capabilities returns the public auth configuration: which providers exist,
// whether local sign-up is enabled, and the anon-read flag. The SPA uses this
// to render the login UI and decide whether to gate routes.
func (s *Service) Capabilities(w http.ResponseWriter, _ *http.Request) {
	live := s.live()
	resp := capabilitiesResponse{
		LocalEnabled:       live.Auth.LocalEnabled,
		AllowAnonymousRead: live.Auth.AllowAnonymousRead,
		Providers:          []providerSummary{},
	}
	if s.Cfg.Auth.OIDC.Enabled {
		label := s.Cfg.Auth.OIDC.Label
		if label == "" {
			label = "OIDC"
		}
		resp.Providers = append(resp.Providers, providerSummary{Name: "openid-connect", Label: label})
	}
	writeJSON(w, http.StatusOK, resp)
}

// OIDCStart kicks off the OAuth dance via goth/gothic.
func (s *Service) OIDCStart(w http.ResponseWriter, r *http.Request) {
	if !s.Cfg.Auth.OIDC.Enabled {
		writeError(w, http.StatusNotFound, "oidc not configured")
		return
	}
	r = withProvider(r, "openid-connect")

	if _, err := gothic.CompleteUserAuth(w, r); err == nil {
		// Already signed in with the provider — fall through to ensure session.
		s.completeOIDC(w, r)
		return
	}
	gothic.BeginAuthHandler(w, r)
}

// OIDCCallback handles the provider redirect, resolves the user, and starts
// a Steelpage session.
func (s *Service) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	if !s.Cfg.Auth.OIDC.Enabled {
		writeError(w, http.StatusNotFound, "oidc not configured")
		return
	}
	r = withProvider(r, "openid-connect")
	s.completeOIDC(w, r)
}

func (s *Service) completeOIDC(w http.ResponseWriter, r *http.Request) {
	gothUser, err := gothic.CompleteUserAuth(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "oidc exchange failed: "+err.Error())
		return
	}
	if gothUser.UserID == "" {
		writeError(w, http.StatusBadRequest, "oidc response missing subject")
		return
	}

	displayName := gothUser.NickName
	if displayName == "" {
		displayName = gothUser.Name
	}
	if displayName == "" {
		displayName = gothUser.FirstName
	}
	if displayName == "" {
		displayName = strings.Split(gothUser.Email, "@")[0]
	}

	u, err := s.Users.EnsureFromOIDC("openid-connect", gothUser.UserID, gothUser.Email, displayName)
	if err != nil {
		if errors.Is(err, users.ErrOIDCMismatch) {
			http.Redirect(w, r, "/login?oidc_error=mismatch", http.StatusFound)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to ensure user")
		return
	}
	if err := s.startSession(r, u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start session")
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// startSession renews the scs token (rotates against fixation) and stores the
// user id.
func (s *Service) startSession(r *http.Request, userID int64) error {
	if err := s.Sessions.RenewToken(r.Context()); err != nil {
		return err
	}
	s.Sessions.Put(r.Context(), middleware.SessionUserKey, userID)
	return nil
}

// withProvider injects ?provider=openid-connect into the URL so gothic knows
// which goth provider to use without forcing the route to carry it.
func withProvider(r *http.Request, name string) *http.Request {
	r2 := r.Clone(r.Context())
	q := r2.URL.Query()
	q.Set("provider", name)
	r2.URL.RawQuery = q.Encode()
	return r2
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ProviderName is the goth provider key we register; exported so the wiring
// code in main.go can reference it without magic strings.
const ProviderName = "openid-connect"

// GothUser is re-exported for tests that want to mock the result.
type GothUser = goth.User
