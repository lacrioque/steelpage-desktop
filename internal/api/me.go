package api

import (
	"encoding/json"
	"net/http"

	"github.com/markusfluer/steelpage-desktop/internal/middleware"
	"github.com/markusfluer/steelpage-desktop/internal/users"
)

// GetMe returns the currently authenticated user (with groups), or 204.
func (a *API) GetMe(w http.ResponseWriter, r *http.Request) {
	u := middleware.FromContext(r.Context())
	if u == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if names, err := a.Groups.GroupsOf(u.ID); err == nil {
		u.Groups = names
	}
	writeJSON(w, http.StatusOK, u)
}

// patchMeRequest is the body of PATCH /api/me. Pointer fields let the client
// distinguish "not in this request" from "set to empty". For font_family, a
// nil pointer + present "font_family":null clears the preference.
type patchMeRequest struct {
	FontFamily *string `json:"font_family"`
}

// PatchMe updates per-user preferences. Token-authenticated callers are
// refused — there's no good reason an API token should flip its owner's UI
// preferences without their browser being in the loop.
func (a *API) PatchMe(w http.ResponseWriter, r *http.Request) {
	u := middleware.FromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if middleware.TokenScopesFromContext(r.Context()) != nil {
		writeError(w, http.StatusForbidden, "session required")
		return
	}

	// Use a map so we can distinguish "field absent" from "field: null".
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if fontRaw, ok := raw["font_family"]; ok {
		var font *string
		if string(fontRaw) != "null" {
			var s string
			if err := json.Unmarshal(fontRaw, &s); err != nil {
				writeError(w, http.StatusBadRequest, "font_family must be a string or null")
				return
			}
			if !users.IsAllowedFont(s) {
				writeError(w, http.StatusBadRequest, "unknown font")
				return
			}
			font = &s
		}
		if err := a.Users.SetFontFamily(u.ID, font); err != nil {
			logError("set font", err)
			writeError(w, http.StatusInternalServerError, "failed to save preference")
			return
		}
	}

	updated, err := a.Users.GetByID(u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload user")
		return
	}
	if names, gerr := a.Groups.GroupsOf(updated.ID); gerr == nil {
		updated.Groups = names
	}
	writeJSON(w, http.StatusOK, updated)
}

// patchMeRequest stays in the file so future preference fields can append
// without restructuring callers.
var _ = patchMeRequest{}
