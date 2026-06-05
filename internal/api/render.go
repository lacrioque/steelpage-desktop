package api

import (
	"encoding/json"
	"net/http"
)

type RenderRequest struct {
	Path     string `json:"path"`
	Markdown string `json:"markdown"`
}

type RenderResponse struct {
	HTML string `json:"html"`
}

func (a *API) Render(w http.ResponseWriter, r *http.Request) {
	var req RenderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	html, err := a.Renderer.Render([]byte(req.Markdown))
	if err != nil {
		logError("render preview", err)
		writeError(w, http.StatusInternalServerError, "render failed")
		return
	}
	writeJSON(w, http.StatusOK, RenderResponse{HTML: html})
}
