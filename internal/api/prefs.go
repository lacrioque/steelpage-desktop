package api

import (
	"encoding/json"
	"net/http"

	"github.com/markusfluer/steelpage-desktop/internal/prefs"
)

// prefsResponse is the SPA-facing view of prefs.json. The push token is
// never echoed back — only whether one is set.
type prefsResponse struct {
	ContentDir   string `json:"content_dir"`
	Theme        string `json:"theme"`
	Font         string `json:"font"`
	LastDoc      string `json:"last_doc"`
	PushRemote   string `json:"push_remote"`
	PushTokenSet bool   `json:"push_token_set"`
}

type patchPrefsRequest struct {
	Theme      *string `json:"theme,omitempty"`
	Font       *string `json:"font,omitempty"`
	LastDoc    *string `json:"last_doc,omitempty"`
	PushRemote *string `json:"push_remote,omitempty"`
	PushToken  *string `json:"push_token,omitempty"`
}

func toPrefsResponse(p prefs.Prefs) prefsResponse {
	return prefsResponse{
		ContentDir:   p.ContentDir,
		Theme:        p.Theme,
		Font:         p.Font,
		LastDoc:      p.LastDoc,
		PushRemote:   p.PushRemote,
		PushTokenSet: p.PushToken != "",
	}
}

func (a *API) GetPrefs(w http.ResponseWriter, _ *http.Request) {
	p, err := prefs.Load()
	if err != nil {
		logError("prefs load", err)
		writeError(w, http.StatusInternalServerError, "failed to load preferences")
		return
	}
	writeJSON(w, http.StatusOK, toPrefsResponse(p))
}

// PatchPrefs updates prefs.json. Font and theme apply immediately in the
// SPA; content dir and push remote/token take effect on next launch (the
// git remote is wired at startup).
func (a *API) PatchPrefs(w http.ResponseWriter, r *http.Request) {
	var req patchPrefsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	p, err := prefs.Load()
	if err != nil {
		logError("prefs load", err)
		writeError(w, http.StatusInternalServerError, "failed to load preferences")
		return
	}
	if req.Theme != nil {
		p.Theme = *req.Theme
	}
	if req.Font != nil {
		p.Font = *req.Font
	}
	if req.LastDoc != nil {
		p.LastDoc = *req.LastDoc
	}
	if req.PushRemote != nil {
		p.PushRemote = *req.PushRemote
	}
	if req.PushToken != nil {
		p.PushToken = *req.PushToken
	}
	if err := p.Save(); err != nil {
		logError("prefs save", err)
		writeError(w, http.StatusInternalServerError, "failed to save preferences")
		return
	}
	writeJSON(w, http.StatusOK, toPrefsResponse(p))
}
