package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/alexedwards/scs/sqlite3store"
	"github.com/alexedwards/scs/v2"
	"github.com/gorilla/sessions"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/openidConnect"
	"golang.org/x/crypto/bcrypt"

	"github.com/markusfluer/steelpage-desktop"
	"github.com/markusfluer/steelpage-desktop/internal/api"
	"github.com/markusfluer/steelpage-desktop/internal/auth"
	"github.com/markusfluer/steelpage-desktop/internal/comments"
	"github.com/markusfluer/steelpage-desktop/internal/config"
	"github.com/markusfluer/steelpage-desktop/internal/configsvc"
	"github.com/markusfluer/steelpage-desktop/internal/db"
	"github.com/markusfluer/steelpage-desktop/internal/gitstore"
	"github.com/markusfluer/steelpage-desktop/internal/groups"
	"github.com/markusfluer/steelpage-desktop/internal/mailer"
	"github.com/markusfluer/steelpage-desktop/internal/permissions"
	"github.com/markusfluer/steelpage-desktop/internal/render"
	"github.com/markusfluer/steelpage-desktop/internal/search"
	"github.com/markusfluer/steelpage-desktop/internal/server"
	"github.com/markusfluer/steelpage-desktop/internal/tokens"
	"github.com/markusfluer/steelpage-desktop/internal/users"
)

func main() {
	configPath := flag.String("config", "./config.yaml", "path to YAML config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	dbConn, err := db.Open(cfg.DB.Path)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer dbConn.Close()
	if err := db.Migrate(dbConn); err != nil {
		log.Fatalf("db migrate: %v", err)
	}

	dist, err := fs.Sub(steelpage.FrontendFS, "frontend/dist")
	if err != nil {
		log.Fatalf("embed: %v", err)
	}

	// Sessions backed by SQLite for cross-restart persistence.
	sm := scs.New()
	sm.Store = sqlite3store.New(dbConn)
	sm.Cookie.Name = "steelpage_session"
	sm.Cookie.HttpOnly = true
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.Secure = cfg.Auth.Session.Secure
	sm.Cookie.Path = "/"
	sm.Lifetime = 30 * 24 * time.Hour

	ensureSessionSecret()
	configureGothicStore()

	if cfg.Auth.OIDC.Enabled {
		provider, perr := openidConnect.New(
			cfg.Auth.OIDC.ClientID,
			cfg.Auth.OIDC.ClientSecret,
			cfg.Auth.OIDC.RedirectURL,
			cfg.Auth.OIDC.IssuerURL+".well-known/openid-configuration",
			cfg.Auth.OIDC.Scopes...,
		)
		if perr != nil {
			log.Fatalf("oidc provider: %v", perr)
		}
		provider.SetName(auth.ProviderName)
		goth.UseProviders(provider)
		log.Printf("auth: OIDC provider %q registered (issuer=%s)", cfg.Auth.OIDC.Label, cfg.Auth.OIDC.IssuerURL)
	}

	r := render.New(cfg.Render)
	g := gitstore.New(cfg.Repo.Path)
	u := users.New(dbConn)
	gs := groups.New(dbConn)
	c := comments.New(dbConn)
	idx := search.New(dbConn, g)
	ss := search.NewStore(dbConn)
	perms := permissions.New(dbConn)
	toks := tokens.New(dbConn)

	// Live config service: YAML + DB overrides + subscribers.
	cfgsvc, err := configsvc.New(dbConn, cfg)
	if err != nil {
		log.Fatalf("configsvc: %v", err)
	}
	effective := cfgsvc.Snapshot()

	mail := mailer.NewLive(effective.Email)
	if mail.Enabled() {
		log.Printf("mailer: SMTP enabled (%s:%d via %s)", effective.Email.Host, effective.Email.Port, effective.Email.Encryption)
	} else {
		log.Printf("mailer: SMTP not configured — outgoing mail disabled")
	}
	// Rebuild the SMTP transport whenever any email.* override changes.
	cfgsvc.Subscribe("email.", func(snap *config.Config, _ []string) {
		mail.Reload(snap.Email)
		if mail.Enabled() {
			log.Printf("mailer: reloaded (%s:%d via %s)", snap.Email.Host, snap.Email.Port, snap.Email.Encryption)
		} else {
			log.Printf("mailer: reloaded — SMTP now disabled")
		}
	})

	authSvc := auth.New(cfg, u, sm, mail, dbConn, cfgsvc)

	if n, err := idx.IndexAll(cfg.Repo.Path); err != nil {
		log.Printf("search: index walk failed: %v (continuing)", err)
	} else {
		log.Printf("search: indexed %d document(s) on startup", n)
	}

	if err := bootstrapAdmin(u); err != nil {
		log.Printf("auth: bootstrap admin: %v", err)
	}

	a := api.New(cfg, r, g, u, gs, c, idx, ss, sm, authSvc, perms, toks, mail, cfgsvc)
	handler := server.New(cfg, a, sm, u, gs, toks, dist)

	log.Printf("Steelpage listening on http://%s (content: %s, db: %s)", cfg.Server.Bind, cfg.Repo.Path, cfg.DB.Path)
	log.Fatal(http.ListenAndServe(cfg.Server.Bind, handler))
}

// configureGothicStore swaps gothic's default CookieStore for a
// FilesystemStore so OIDC sessions aren't capped by the browser's 4 KB
// cookie limit. Some IdPs (Authentik, Keycloak with rich claims) issue
// id_tokens that, once goth wraps the user record + tokens + state into a
// session, easily exceed 4 KB — the cookie store then throws
// "securecookie: the value is too long" during the OAuth exchange.
//
// We keep the small state cookie on the browser; the fat session payload
// lives in a temp file keyed by that cookie. Files are short-lived (one
// OAuth round-trip) and get cleaned up when the process tears down its
// tmp dir.
func configureGothicStore() {
	secret := os.Getenv("SESSION_SECRET")
	store := sessions.NewFilesystemStore("", []byte(secret))
	// Allow plenty of headroom for id_tokens with many claims. Default is
	// 4096, which is what was biting us.
	store.MaxLength(64 * 1024)
	store.Options.HttpOnly = true
	store.Options.Path = "/"
	gothic.Store = store
}

// ensureSessionSecret guarantees gothic has a SESSION_SECRET to drive its
// OAuth state cookie. If the operator hasn't set one, we generate an
// ephemeral random key for this process; the only downside is that an OIDC
// flow in progress when the binary restarts will lose its state cookie.
func ensureSessionSecret() {
	if os.Getenv("SESSION_SECRET") != "" {
		return
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		log.Printf("auth: failed to seed SESSION_SECRET: %v", err)
		return
	}
	_ = os.Setenv("SESSION_SECRET", hex.EncodeToString(buf))
}

// bootstrapAdmin honors STEELPAGE_BOOTSTRAP_ADMIN=email:password. If no admin
// exists, it creates one with that email/password (bcrypt-hashed) and role=admin.
func bootstrapAdmin(store *users.Store) error {
	raw := os.Getenv("STEELPAGE_BOOTSTRAP_ADMIN")
	if raw == "" {
		return nil
	}
	any, err := store.AnyAdmin()
	if err != nil {
		return err
	}
	if any {
		return nil
	}
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil
	}
	email, password := parts[0], parts[1]
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	display := strings.Split(email, "@")[0]
	if _, err := store.CreateLocal(email, display, string(hash), users.RoleAdmin); err != nil {
		return err
	}
	log.Printf("auth: bootstrap admin user created: %s", email)
	return nil
}
