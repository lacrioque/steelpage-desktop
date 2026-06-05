package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/markusfluer/steelpage-desktop/internal/mailer"
	"github.com/markusfluer/steelpage-desktop/internal/users"
)

// ForgotPassword handles POST /api/auth/forgot. The response is always 204
// regardless of whether the email is known, to avoid leaking which addresses
// are registered (enumeration). The actual email is only sent when a local
// user (one with a password_hash) exists for the given address.
func (s *Service) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Even malformed input doesn't reveal anything — still 204.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if req.Email == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	u, err := s.Users.FindByEmail(req.Email)
	if err != nil {
		// Not found (or any lookup error) — silently succeed.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if u.PasswordHash == "" {
		// Only local accounts can reset; OIDC-only users have nothing to reset.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	plaintext, err := newResetToken()
	if err != nil {
		log.Printf("auth.ForgotPassword: token gen failed: %v", err)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	hash := hashResetToken(plaintext)
	now := time.Now().UTC()
	expires := now.Add(1 * time.Hour)

	if err := insertResetToken(s.DB, u.ID, hash, expires, now); err != nil {
		log.Printf("auth.ForgotPassword: insert token failed: %v", err)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	link := s.publicURL("/reset?token=" + plaintext)
	if u.Email != nil {
		text := "We received a request to reset your Steelpage password.\r\n\r\n" +
			"Open the link below within 60 minutes to set a new password:\r\n\r\n" +
			link + "\r\n\r\n" +
			"If you didn't request this, you can safely ignore this email — your password won't change."
		html := `<p>We received a request to reset your Steelpage password.</p>` +
			`<p>Open the link below within 60 minutes to set a new password:</p>` +
			`<p><a href="` + link + `">` + link + `</a></p>` +
			`<p>If you didn't request this, you can safely ignore this email — your password won't change.</p>`

		log.Printf("auth: password reset requested for user_id=%d email=%q (token expires %s)",
			u.ID, *u.Email, expires.Format(time.RFC3339))
		if err := s.Mailer.Send(mailer.Message{
			To:      []string{*u.Email},
			Subject: "Steelpage password reset",
			Text:    text,
			HTML:    html,
		}); err != nil {
			log.Printf("auth: password reset email FAILED for user_id=%d: %v", u.ID, err)
		} else {
			log.Printf("auth: password reset email queued for user_id=%d", u.ID)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// ResetPassword handles POST /api/auth/reset. Validates the token, swaps in a
// fresh bcrypt hash, and marks the token used in a single transaction so a
// successful reset can't be replayed.
func (s *Service) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(req.NewPassword) < minPasswordLen {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "invalid or expired token")
		return
	}

	hash := hashResetToken(req.Token)
	userID, err := lookupResetToken(s.DB, hash)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid or expired token")
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcryptCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	tx, err := s.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(newHash), userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update password")
		return
	}
	res, err := tx.Exec(`UPDATE password_reset_tokens SET used_at = ? WHERE token_hash = ? AND used_at IS NULL`, now, hash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark token used")
		return
	}
	// If the token got consumed between lookup and update (race), refuse.
	n, _ := res.RowsAffected()
	if n == 0 {
		writeError(w, http.StatusBadRequest, "invalid or expired token")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}
	committed = true
	log.Printf("auth: password reset COMPLETED for user_id=%d", userID)

	// Belt-and-braces: ensure the user actually exists. We don't want to leave
	// stale tokens pointing at a deleted user (FK ON DELETE CASCADE handles
	// that), so this is just defensive logging.
	if _, err := s.Users.GetByID(userID); err != nil && !errors.Is(err, users.ErrNotFound) {
		log.Printf("auth.ResetPassword: post-update user lookup failed: %v", err)
	}

	w.WriteHeader(http.StatusNoContent)
}

// newResetToken produces ~32 hex chars (16 random bytes) of unguessable token
// material. Returned as the plaintext we email — the DB only stores its hash.
func newResetToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// hashResetToken returns the sha256 hex digest used for DB storage and lookup.
// Using sha256 (not bcrypt) is fine here because the input has full crypto
// entropy already — there's no password to slow-hash.
func hashResetToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

func insertResetToken(db *sql.DB, userID int64, tokenHash string, expiresAt, createdAt time.Time) error {
	_, err := db.Exec(
		`INSERT INTO password_reset_tokens(user_id, token_hash, expires_at, created_at) VALUES(?, ?, ?, ?)`,
		userID,
		tokenHash,
		expiresAt.Format(time.RFC3339),
		createdAt.Format(time.RFC3339),
	)
	return err
}

func lookupResetToken(db *sql.DB, tokenHash string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	row := db.QueryRow(
		`SELECT user_id FROM password_reset_tokens
		 WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
		tokenHash, now,
	)
	var userID int64
	if err := row.Scan(&userID); err != nil {
		return 0, err
	}
	return userID, nil
}
