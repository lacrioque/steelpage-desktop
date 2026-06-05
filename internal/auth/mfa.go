package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/png"
	"log"
	"net/http"

	"github.com/pquerna/otp/totp"

	"github.com/markusfluer/steelpage-desktop/internal/middleware"
	"github.com/markusfluer/steelpage-desktop/internal/users"
)

func logTotpError(prefix string, err error) {
	log.Printf("%s: %v", prefix, err)
}

const (
	// SessionMFAPendingKey is set during login between password-check and
	// TOTP-check. The identity middleware ignores it — only `user_id` grants
	// real identity, so this state is half-authenticated by design.
	SessionMFAPendingKey = "mfa_pending_user_id"
)

type setupStartResponse struct {
	Secret     string `json:"secret"`
	OtpauthURL string `json:"otpauth_url"`
	QRPng      string `json:"qr_png"` // data:image/png;base64,…
}

// MFASetupStart generates a fresh TOTP secret (replacing any existing pending
// or enabled one — the user is starting over). Returns the secret + a QR code
// the user can scan in their Authenticator app.
func (s *Service) MFASetupStart(w http.ResponseWriter, r *http.Request) {
	u := middleware.FromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if middleware.TokenScopesFromContext(r.Context()) != nil {
		writeError(w, http.StatusForbidden, "session required")
		return
	}

	account := u.DisplayName
	if u.Email != nil && *u.Email != "" {
		account = *u.Email
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Steelpage",
		AccountName: account,
	})
	if err != nil {
		logTotpError("totp generate", err)
		writeError(w, http.StatusInternalServerError, "failed to generate secret")
		return
	}

	img, err := key.Image(220, 220)
	if err != nil {
		logTotpError("totp image", err)
		writeError(w, http.StatusInternalServerError, "failed to render QR")
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		logTotpError("png encode", err)
		writeError(w, http.StatusInternalServerError, "failed to encode QR")
		return
	}
	qrData := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())

	if err := s.Users.SetTOTPSecret(u.ID, key.Secret()); err != nil {
		logTotpError("totp save secret", err)
		writeError(w, http.StatusInternalServerError, "failed to save secret")
		return
	}

	writeJSON(w, http.StatusOK, setupStartResponse{
		Secret:     key.Secret(),
		OtpauthURL: key.URL(),
		QRPng:      qrData,
	})
}

type setupConfirmRequest struct {
	Code string `json:"code"`
}

// MFASetupConfirm verifies a code against the pending secret and flips
// totp_enabled_at on success.
func (s *Service) MFASetupConfirm(w http.ResponseWriter, r *http.Request) {
	u := middleware.FromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if middleware.TokenScopesFromContext(r.Context()) != nil {
		writeError(w, http.StatusForbidden, "session required")
		return
	}
	if u.TOTPSecret == "" {
		writeError(w, http.StatusBadRequest, "no setup in progress — call setup-start first")
		return
	}
	if u.TOTPEnabledAt != nil {
		writeError(w, http.StatusBadRequest, "MFA already enabled")
		return
	}

	var req setupConfirmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !totp.Validate(req.Code, u.TOTPSecret) {
		writeError(w, http.StatusBadRequest, "invalid code")
		return
	}
	if err := s.Users.ConfirmTOTP(u.ID); err != nil {
		logTotpError("totp confirm", err)
		writeError(w, http.StatusInternalServerError, "failed to enable MFA")
		return
	}
	// Refresh
	fresh, err := s.Users.GetByID(u.ID)
	if err == nil {
		writeJSON(w, http.StatusOK, fresh)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type disableRequest struct {
	Code string `json:"code"`
}

// MFADisable turns off MFA after verifying a current code (or, for admins
// disabling their own account, a fresh code anyway — accidental disable
// without proof is too risky).
func (s *Service) MFADisable(w http.ResponseWriter, r *http.Request) {
	u := middleware.FromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if middleware.TokenScopesFromContext(r.Context()) != nil {
		writeError(w, http.StatusForbidden, "session required")
		return
	}
	if u.TOTPEnabledAt == nil {
		writeError(w, http.StatusBadRequest, "MFA is not enabled")
		return
	}

	var req disableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !totp.Validate(req.Code, u.TOTPSecret) {
		writeError(w, http.StatusBadRequest, "invalid code")
		return
	}
	if err := s.Users.DisableTOTP(u.ID); err != nil {
		logTotpError("totp disable", err)
		writeError(w, http.StatusInternalServerError, "failed to disable MFA")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type mfaLoginRequest struct {
	Code string `json:"code"`
}

// LoginMFA consumes a half-authenticated session (password verified, TOTP
// pending) and completes it on a valid code.
func (s *Service) LoginMFA(w http.ResponseWriter, r *http.Request) {
	pendingID := s.Sessions.GetInt64(r.Context(), SessionMFAPendingKey)
	if pendingID <= 0 {
		writeError(w, http.StatusUnauthorized, "no MFA challenge pending")
		return
	}
	u, err := s.Users.GetByID(pendingID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "challenge expired")
		return
	}

	var req mfaLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if u.TOTPSecret == "" || u.TOTPEnabledAt == nil {
		writeError(w, http.StatusBadRequest, "MFA not enabled on this account")
		return
	}
	if !totp.Validate(req.Code, u.TOTPSecret) {
		writeError(w, http.StatusUnauthorized, "invalid code")
		return
	}

	s.Sessions.Remove(r.Context(), SessionMFAPendingKey)
	if err := s.startSession(r, u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start session")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// requireMFA reports whether the user must complete an MFA challenge before
// their session is fully authenticated.
func requireMFA(u *users.User) bool {
	return u != nil && u.TOTPEnabledAt != nil && u.TOTPSecret != ""
}

var _ = errors.New // keep imports tidy in case future code surfaces typed errors
