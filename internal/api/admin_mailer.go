package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/markusfluer/steelpage-desktop/internal/mailer"
	"github.com/markusfluer/steelpage-desktop/internal/middleware"
)

type mailerStatusResponse struct {
	Enabled     bool   `json:"enabled"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Encryption  string `json:"encryption"`
	FromAddress string `json:"from_address"`
	FromName    string `json:"from_name"`
}

// AdminMailerStatus exposes the redacted SMTP config so the admin UI can show
// whether SMTP is set up and where it's pointing. Username/password are never
// included.
func (a *API) AdminMailerStatus(w http.ResponseWriter, _ *http.Request) {
	e := a.Configsvc.Snapshot().Email
	writeJSON(w, http.StatusOK, mailerStatusResponse{
		Enabled:     e.Enabled(),
		Host:        e.Host,
		Port:        e.Port,
		Encryption:  e.Encryption,
		FromAddress: e.FromAddress,
		FromName:    e.FromName,
	})
}

type testMailRequest struct {
	To string `json:"to,omitempty"`
}

// AdminMailerTest sends a small test email. The destination defaults to the
// signed-in admin's own email; explicit `to` overrides for delivering to
// another inbox during debugging.
func (a *API) AdminMailerTest(w http.ResponseWriter, r *http.Request) {
	if !a.Mailer.Enabled() {
		writeError(w, http.StatusBadRequest, "SMTP is not configured")
		return
	}
	u := middleware.FromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req testMailRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}

	to := req.To
	if to == "" {
		if u.Email == nil || *u.Email == "" {
			writeError(w, http.StatusBadRequest, "no recipient: set your account email or pass {to}")
			return
		}
		to = *u.Email
	}

	log.Printf("mailer: admin test email requested by user_id=%d → %s", u.ID, to)
	msg := mailer.Message{
		To:      []string{to},
		Subject: "Steelpage test email",
		Text:    "This is a test email from your Steelpage instance. If you received this, SMTP is wired up correctly.\n",
		HTML:    "<p>This is a test email from your Steelpage instance.</p><p>If you received this, SMTP is wired up correctly.</p>",
	}
	if err := a.Mailer.Send(msg); err != nil {
		if errors.Is(err, mailer.ErrNotConfigured) {
			writeError(w, http.StatusBadRequest, "SMTP is not configured")
			return
		}
		logError("mailer test", err)
		writeError(w, http.StatusBadGateway, "failed to send: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": true, "to": to})
}
