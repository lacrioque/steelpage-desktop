package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/markusfluer/steelpage-desktop/internal/mailer"
	"github.com/markusfluer/steelpage-desktop/internal/middleware"
	"github.com/markusfluer/steelpage-desktop/internal/users"
)

const (
	verificationTokenTTL = 24 * time.Hour
)

// VerifyEmailRequest is what POST /api/auth/verify accepts.
type verifyEmailRequest struct {
	Token string `json:"token"`
}

// VerifyEmail consumes a one-shot verification token. Returns 204 on success.
//
// The token store is sha256-hashed (we never store the plaintext). Single-use
// is enforced via `used_at`; expired tokens are rejected even when unused.
func (s *Service) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req verifyEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "token required")
		return
	}

	hash := sha256.Sum256([]byte(req.Token))
	tokenHash := hex.EncodeToString(hash[:])

	row := s.DB.QueryRow(`
		SELECT id, user_id, email
		FROM email_verification_tokens
		WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
		tokenHash, time.Now().UTC().Format(time.RFC3339),
	)
	var (
		id      int64
		userID  int64
		emailAt string
	)
	if err := row.Scan(&id, &userID, &emailAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "invalid or expired token")
			return
		}
		log.Printf("verify lookup: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to verify token")
		return
	}

	tx, err := s.DB.Begin()
	if err != nil {
		log.Printf("verify tx: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to verify token")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`UPDATE email_verification_tokens SET used_at = ? WHERE id = ?`, now, id); err != nil {
		_ = tx.Rollback()
		log.Printf("verify mark used: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to verify token")
		return
	}
	// Set users.email_verified_at AND fill email if previously blank — the
	// verified email is the source of truth for the value.
	if _, err := tx.Exec(`UPDATE users SET email_verified_at = ?, email = COALESCE(email, ?) WHERE id = ?`, now, emailAt, userID); err != nil {
		_ = tx.Rollback()
		log.Printf("verify mark user: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to verify token")
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("verify commit: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to verify token")
		return
	}
	log.Printf("auth: email verified for user_id=%d email=%q", userID, emailAt)
	w.WriteHeader(http.StatusNoContent)
}

// ResendVerification mints a fresh token and emails it. Requires a session
// — token-authenticated callers are refused so a leaked API token can't
// hijack the verification trail.
func (s *Service) ResendVerification(w http.ResponseWriter, r *http.Request) {
	u := middleware.FromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if middleware.TokenScopesFromContext(r.Context()) != nil {
		writeError(w, http.StatusForbidden, "session required")
		return
	}
	if u.Email == nil || *u.Email == "" {
		writeError(w, http.StatusBadRequest, "no email on account")
		return
	}
	if u.EmailVerifiedAt != nil {
		writeError(w, http.StatusBadRequest, "email already verified")
		return
	}
	if !s.Mailer.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "SMTP not configured")
		return
	}
	if err := s.sendVerificationEmail(u); err != nil {
		log.Printf("resend verification: %v", err)
		writeError(w, http.StatusBadGateway, "failed to send verification")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SendVerificationOnRegister is hooked from auth.Register. Best-effort — we
// don't fail registration when SMTP is down or absent.
func (s *Service) SendVerificationOnRegister(u *users.User) {
	if !s.Mailer.Enabled() {
		return
	}
	if u == nil || u.Email == nil || *u.Email == "" {
		return
	}
	if u.EmailVerifiedAt != nil {
		return
	}
	if err := s.sendVerificationEmail(u); err != nil {
		log.Printf("verification on register: %v", err)
	}
}

func (s *Service) sendVerificationEmail(u *users.User) error {
	if u.Email == nil || *u.Email == "" {
		return errors.New("no email")
	}
	plaintext, err := newPlaintextToken()
	if err != nil {
		return err
	}
	hash := sha256.Sum256([]byte(plaintext))
	tokenHash := hex.EncodeToString(hash[:])

	expires := time.Now().Add(verificationTokenTTL).UTC().Format(time.RFC3339)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.DB.Exec(`
		INSERT INTO email_verification_tokens(user_id, email, token_hash, expires_at, created_at)
		VALUES(?, ?, ?, ?, ?)`,
		u.ID, *u.Email, tokenHash, expires, now,
	); err != nil {
		return fmt.Errorf("insert verification token: %w", err)
	}

	link := s.publicURL("/verify?token=" + plaintext)
	displayName := u.DisplayName
	if displayName == "" {
		displayName = "there"
	}
	subject := "Verify your Steelpage email"
	textBody := fmt.Sprintf(`Hi %s,

Please verify the email %s belongs to you by opening this link within 24 hours:

%s

If you didn't create a Steelpage account, you can ignore this email.

— Steelpage
`, displayName, *u.Email, link)
	htmlBody := fmt.Sprintf(`<p>Hi %s,</p>
<p>Please verify the email <strong>%s</strong> belongs to you by opening this link within 24 hours:</p>
<p><a href="%s">%s</a></p>
<p style="color:#6f6a60;font-size:0.9em">If you didn't create a Steelpage account, you can ignore this email.</p>
<p>— Steelpage</p>`, displayName, *u.Email, link, link)

	log.Printf("auth: verification email requested for user_id=%d email=%q (expires in %s)",
		u.ID, *u.Email, verificationTokenTTL)
	if err := s.Mailer.Send(mailer.Message{
		To:      []string{*u.Email},
		Subject: subject,
		Text:    textBody,
		HTML:    htmlBody,
	}); err != nil {
		log.Printf("auth: verification email FAILED for user_id=%d: %v", u.ID, err)
		return err
	}
	log.Printf("auth: verification email queued for user_id=%d", u.ID)
	return nil
}

// newPlaintextToken returns a 32-hex-char random string for use in email links.
func newPlaintextToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
