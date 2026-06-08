// Package control holds the desktop "control plane" — the handful of local
// endpoints that work the same in both local and server mode: preferences
// (prefs.json), the connection status/test, and the local identity. They
// don't depend on the document/comment API, so the loopback can mount them
// whether or not the local services are running.
package control

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/markusfluer/steelpage-desktop/internal/localidentity"
	"github.com/markusfluer/steelpage-desktop/internal/prefs"
	"github.com/markusfluer/steelpage-desktop/internal/remote"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// --- preferences -----------------------------------------------------------

// prefsResponse is the SPA-facing view of prefs.json. Secrets (push/server
// tokens) are never echoed back — only whether they are set.
type prefsResponse struct {
	ContentDir     string `json:"content_dir"`
	Theme          string `json:"theme"`
	Font           string `json:"font"`
	LastDoc        string `json:"last_doc"`
	PushRemote     string `json:"push_remote"`
	PushTokenSet   bool   `json:"push_token_set"`
	ServerURL      string `json:"server_url"`
	ServerTokenSet bool   `json:"server_token_set"`
}

type patchPrefsRequest struct {
	Theme       *string `json:"theme,omitempty"`
	Font        *string `json:"font,omitempty"`
	LastDoc     *string `json:"last_doc,omitempty"`
	PushRemote  *string `json:"push_remote,omitempty"`
	PushToken   *string `json:"push_token,omitempty"`
	ServerURL   *string `json:"server_url,omitempty"`
	ServerToken *string `json:"server_token,omitempty"`
}

func toPrefsResponse(p prefs.Prefs) prefsResponse {
	return prefsResponse{
		ContentDir:     p.ContentDir,
		Theme:          p.Theme,
		Font:           p.Font,
		LastDoc:        p.LastDoc,
		PushRemote:     p.PushRemote,
		PushTokenSet:   p.PushToken != "",
		ServerURL:      p.ServerURL,
		ServerTokenSet: p.ServerToken != "",
	}
}

func GetPrefs(w http.ResponseWriter, _ *http.Request) {
	p, err := prefs.Load()
	if err != nil {
		log.Printf("prefs load: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load preferences")
		return
	}
	writeJSON(w, http.StatusOK, toPrefsResponse(p))
}

// PatchPrefs updates prefs.json. Font/theme apply immediately in the SPA;
// content dir, push remote, and the server connection take effect on next
// launch.
func PatchPrefs(w http.ResponseWriter, r *http.Request) {
	var req patchPrefsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	p, err := prefs.Load()
	if err != nil {
		log.Printf("prefs load: %v", err)
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
	if req.ServerURL != nil {
		p.ServerURL = *req.ServerURL
	}
	if req.ServerToken != nil {
		p.ServerToken = *req.ServerToken
	}
	if err := p.Save(); err != nil {
		log.Printf("prefs save: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to save preferences")
		return
	}
	writeJSON(w, http.StatusOK, toPrefsResponse(p))
}

// --- connection ------------------------------------------------------------

type connectionResponse struct {
	Mode      string       `json:"mode"` // "local" | "server"
	ServerURL string       `json:"server_url,omitempty"`
	User      *remote.User `json:"user,omitempty"`
}

// Connection reports the current mode and, in server mode, the connected
// user (best-effort — a probe failure still reports mode=server).
func Connection(w http.ResponseWriter, r *http.Request) {
	p, err := prefs.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load preferences")
		return
	}
	if !p.Remote() {
		id := localidentity.Default()
		writeJSON(w, http.StatusOK, connectionResponse{
			Mode: "local",
			User: &remote.User{ID: id.ID, DisplayName: id.DisplayName},
		})
		return
	}
	resp := connectionResponse{Mode: "server", ServerURL: p.ServerURL}
	if u, err := remote.TestConnection(r.Context(), p.ServerURL, p.ServerToken); err == nil {
		resp.User = &u
	}
	writeJSON(w, http.StatusOK, resp)
}

type testConnectionRequest struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

type testConnectionResponse struct {
	OK    bool         `json:"ok"`
	User  *remote.User `json:"user,omitempty"`
	Error string       `json:"error,omitempty"`
}

// TestConnection probes an arbitrary url+token (so the Preferences modal can
// verify before saving/relaunching).
func TestConnection(w http.ResponseWriter, r *http.Request) {
	var req testConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	u, err := remote.TestConnection(context.Background(), req.URL, req.Token)
	if err != nil {
		writeJSON(w, http.StatusOK, testConnectionResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, testConnectionResponse{OK: true, User: &u})
}

// --- identity --------------------------------------------------------------

// LocalMe returns the single local identity (local mode only; in server mode
// /api/me is proxied to the remote server instead).
func LocalMe(w http.ResponseWriter, _ *http.Request) {
	id := localidentity.Default()
	writeJSON(w, http.StatusOK, remote.User{ID: id.ID, DisplayName: id.DisplayName})
}
