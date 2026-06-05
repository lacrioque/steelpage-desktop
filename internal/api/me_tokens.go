package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/markusfluer/steelpage-desktop/internal/middleware"
	"github.com/markusfluer/steelpage-desktop/internal/tokens"
)

// ListMyTokens returns the signed-in user's tokens (no plaintext secrets).
func (a *API) ListMyTokens(w http.ResponseWriter, r *http.Request) {
	u := middleware.FromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	list, err := a.Tokens.ListForUser(u.ID)
	if err != nil {
		logError("list tokens", err)
		writeError(w, http.StatusInternalServerError, "failed to list tokens")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type createTokenRequest struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt *string  `json:"expires_at,omitempty"`
}

// CreateMyToken mints a new token for the current user and returns the
// plaintext secret EXACTLY ONCE.
func (a *API) CreateMyToken(w http.ResponseWriter, r *http.Request) {
	u := middleware.FromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	// Token-authenticated callers can't mint new tokens — too easy to bootstrap
	// a permanent backdoor that way.
	if middleware.TokenScopesFromContext(r.Context()) != nil {
		writeError(w, http.StatusForbidden, "session required to mint tokens")
		return
	}

	var req createTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	var expiry *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "expires_at must be RFC3339")
			return
		}
		expiry = &t
	}

	t, err := a.Tokens.Create(u.ID, req.Name, req.Scopes, expiry)
	if err != nil {
		if errors.Is(err, tokens.ErrInvalid) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		logError("create token", err)
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// DeleteMyToken revokes a token by id (must belong to the current user).
func (a *API) DeleteMyToken(w http.ResponseWriter, r *http.Request) {
	u := middleware.FromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := a.Tokens.Delete(u.ID, id); err != nil {
		if errors.Is(err, tokens.ErrNotFound) {
			writeError(w, http.StatusNotFound, "token not found")
			return
		}
		logError("delete token", err)
		writeError(w, http.StatusInternalServerError, "failed to delete token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
