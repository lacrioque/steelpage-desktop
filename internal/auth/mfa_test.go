package auth_test

// End-to-end test for the MFA flow. Spins up the real auth handlers behind
// httptest, registers a user, walks them through setup → enable → logout →
// login (half-auth) → /api/auth/login/mfa → full session. Uses pquerna/otp
// to generate codes the same way an authenticator app would.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/pquerna/otp/totp"

	"github.com/markusfluer/steelpage-desktop/internal/auth"
	"github.com/markusfluer/steelpage-desktop/internal/config"
	"github.com/markusfluer/steelpage-desktop/internal/configsvc"
	"github.com/markusfluer/steelpage-desktop/internal/db"
	"github.com/markusfluer/steelpage-desktop/internal/groups"
	"github.com/markusfluer/steelpage-desktop/internal/mailer"
	"github.com/markusfluer/steelpage-desktop/internal/middleware"
	"github.com/markusfluer/steelpage-desktop/internal/users"
)

func setupAuthServer(t *testing.T) (*httptest.Server, *http.Client) {
	t.Helper()

	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(d); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	cfg := &config.Config{
		Auth: config.Auth{
			LocalEnabled:       true,
			AllowAnonymousRead: true,
			Session:            config.AuthSession{Secure: false},
		},
		Server: config.Server{Bind: "127.0.0.1:0", BaseURL: "http://test.local"},
	}

	ustore := users.New(d)
	gstore := groups.New(d)
	sm := scs.New()
	sm.Cookie.Name = "steelpage_session"
	sm.Cookie.HttpOnly = true
	sm.Cookie.Path = "/"
	sm.Lifetime = 30 * 24 * time.Hour

	cfgsvc, err := configsvc.New(d, cfg)
	if err != nil {
		t.Fatalf("configsvc: %v", err)
	}
	svc := auth.New(cfg, ustore, sm, mailer.New(cfg.Email), d, cfgsvc)

	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Use(middleware.Identity(sm, ustore, gstore, nil))
	r.Post("/api/auth/register", svc.Register)
	r.Post("/api/auth/login", svc.Login)
	r.Post("/api/auth/login/mfa", svc.LoginMFA)
	r.Post("/api/auth/logout", svc.Logout)
	r.Post("/api/auth/mfa/setup-start", svc.MFASetupStart)
	r.Post("/api/auth/mfa/setup-confirm", svc.MFASetupConfirm)
	r.Get("/api/me", func(w http.ResponseWriter, req *http.Request) {
		u := middleware.FromContext(req.Context())
		if u == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(u)
	})

	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	return ts, client
}

func postJSON(t *testing.T, client *http.Client, url string, body any) *http.Response {
	t.Helper()
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return res
}

func readJSON(t *testing.T, res *http.Response, into any) {
	t.Helper()
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return
	}
	if err := json.Unmarshal(body, into); err != nil {
		t.Fatalf("decode %q: %v", string(body), err)
	}
}

// extractSecretFromOtpauth pulls the base32 secret out of the otpauth:// URL
// goth returns. We don't trust the JSON field on its own because the test
// asserts the whole end-to-end round-trip.
func extractSecretFromOtpauth(t *testing.T, otpauthURL string) string {
	t.Helper()
	idx := strings.Index(otpauthURL, "secret=")
	if idx < 0 {
		t.Fatalf("no secret in otpauth URL %q", otpauthURL)
	}
	rest := otpauthURL[idx+len("secret="):]
	if amp := strings.Index(rest, "&"); amp >= 0 {
		rest = rest[:amp]
	}
	return rest
}

func TestMFA_FullRoundTrip(t *testing.T) {
	ts, client := setupAuthServer(t)

	// 1. Register a fresh user.
	res := postJSON(t, client, ts.URL+"/api/auth/register", map[string]string{
		"email":        "alice@example.com",
		"password":     "correcthorsebattery",
		"display_name": "Alice",
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("register status=%d", res.StatusCode)
	}
	res.Body.Close()

	// 2. Start MFA setup.
	res = postJSON(t, client, ts.URL+"/api/auth/mfa/setup-start", map[string]string{})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("setup-start status=%d", res.StatusCode)
	}
	var setup struct {
		Secret     string `json:"secret"`
		OtpauthURL string `json:"otpauth_url"`
		QRPng      string `json:"qr_png"`
	}
	readJSON(t, res, &setup)
	if setup.Secret == "" || !strings.HasPrefix(setup.QRPng, "data:image/png;base64,") {
		t.Fatalf("setup payload looks wrong: secret=%q qr-prefix=%q", setup.Secret, setup.QRPng[:min(40, len(setup.QRPng))])
	}
	// Also assert the URL-extracted secret matches the explicit field.
	if got := extractSecretFromOtpauth(t, setup.OtpauthURL); got != setup.Secret {
		t.Fatalf("secret mismatch between field (%s) and URL (%s)", setup.Secret, got)
	}

	// 3. Compute a code and confirm setup.
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	res = postJSON(t, client, ts.URL+"/api/auth/mfa/setup-confirm", map[string]string{"code": code})
	if res.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(res.Body)
		t.Fatalf("setup-confirm status=%d body=%s", res.StatusCode, string(bodyBytes))
	}
	res.Body.Close()

	// 4. Log out.
	res = postJSON(t, client, ts.URL+"/api/auth/logout", map[string]string{})
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status=%d", res.StatusCode)
	}
	res.Body.Close()

	// 5. Log back in — should get {mfa_required:true} and no real session.
	res = postJSON(t, client, ts.URL+"/api/auth/login", map[string]string{
		"email":    "alice@example.com",
		"password": "correcthorsebattery",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login status=%d", res.StatusCode)
	}
	var loginBody map[string]any
	readJSON(t, res, &loginBody)
	if v, _ := loginBody["mfa_required"].(bool); !v {
		t.Fatalf("expected mfa_required:true, got %v", loginBody)
	}

	// 6. /api/me should be 204 — we're half-authenticated.
	mereq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/me", nil)
	res, _ = client.Do(mereq)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("/api/me half-auth status=%d (want 204)", res.StatusCode)
	}
	res.Body.Close()

	// 7. Send the TOTP code → full session.
	code, _ = totp.GenerateCode(setup.Secret, time.Now())
	res = postJSON(t, client, ts.URL+"/api/auth/login/mfa", map[string]string{"code": code})
	if res.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(res.Body)
		t.Fatalf("login/mfa status=%d body=%s", res.StatusCode, string(bodyBytes))
	}
	var user users.User
	readJSON(t, res, &user)
	if user.TOTPEnabledAt == nil {
		t.Fatalf("expected totp_enabled_at on returned user")
	}

	// 8. /api/me should now be 200.
	mereq, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/me", nil)
	res, _ = client.Do(mereq)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/api/me after MFA status=%d (want 200)", res.StatusCode)
	}
	res.Body.Close()
}

func TestMFA_LoginRejectsWrongCode(t *testing.T) {
	ts, client := setupAuthServer(t)

	// Register, enable MFA.
	res := postJSON(t, client, ts.URL+"/api/auth/register", map[string]string{
		"email": "bob@example.com", "password": "correcthorsebattery", "display_name": "Bob",
	})
	res.Body.Close()
	res = postJSON(t, client, ts.URL+"/api/auth/mfa/setup-start", map[string]string{})
	var setup struct {
		Secret string `json:"secret"`
	}
	readJSON(t, res, &setup)
	code, _ := totp.GenerateCode(setup.Secret, time.Now())
	res = postJSON(t, client, ts.URL+"/api/auth/mfa/setup-confirm", map[string]string{"code": code})
	res.Body.Close()
	res = postJSON(t, client, ts.URL+"/api/auth/logout", map[string]string{})
	res.Body.Close()

	// Half-auth login.
	res = postJSON(t, client, ts.URL+"/api/auth/login", map[string]string{
		"email": "bob@example.com", "password": "correcthorsebattery",
	})
	res.Body.Close()

	// Wrong code → 401.
	res = postJSON(t, client, ts.URL+"/api/auth/login/mfa", map[string]string{"code": "000000"})
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong code status=%d (want 401)", res.StatusCode)
	}
	res.Body.Close()
}

// min keeps the test compatible with older Go toolchains lacking builtin min.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ = fmt.Sprintf // keep fmt imported for future debug printing without churn
