package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/markusfluer/steelpage-desktop/internal/configsvc"
	"github.com/markusfluer/steelpage-desktop/internal/middleware"
)

// AdminConfigSchema returns the editable-fields manifest. The frontend uses
// it to render the right input type per field and to decide which keys are
// editable vs. lock-iconned.
func (a *API) AdminConfigSchema(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, configsvc.Schema())
}

// AdminConfigEffective returns the per-field state — current value (redacted
// for sensitive keys), whether an override is set, and whether the field is
// read-only.
func (a *API) AdminConfigEffective(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.Configsvc.Effective())
}

type configPatchRequest struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value,omitempty"`
	Unset bool            `json:"unset,omitempty"`
}

// AdminConfigPatch applies one override. Body: {"key":"email.host","value":"smtp..."}
// or {"key":"email.host","unset":true}.
func (a *API) AdminConfigPatch(w http.ResponseWriter, r *http.Request) {
	var req configPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Key == "" {
		writeError(w, http.StatusBadRequest, "key required")
		return
	}

	var actor *int64
	if u := middleware.FromContext(r.Context()); u != nil {
		v := u.ID
		actor = &v
	}

	var err error
	if req.Unset {
		err = a.Configsvc.Unset(actor, req.Key)
	} else {
		// Empty body for a sensitive field = keep existing. The frontend
		// already gates this client-side, but enforce here too.
		if len(req.Value) == 0 || string(req.Value) == `""` {
			if field, ok := schemaField(req.Key); ok && field.Sensitive {
				writeJSON(w, http.StatusOK, a.Configsvc.Effective())
				return
			}
		}
		err = a.Configsvc.Set(actor, req.Key, req.Value)
	}
	if err != nil {
		switch {
		case errors.Is(err, configsvc.ErrUnknownKey):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, configsvc.ErrReadOnly):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, configsvc.ErrValidation):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			logError("config patch", err)
			writeError(w, http.StatusInternalServerError, "failed to update config")
		}
		return
	}
	writeJSON(w, http.StatusOK, a.Configsvc.Effective())
}

// AdminConfigAudit returns recent config edits.
func (a *API) AdminConfigAudit(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}
	entries, err := a.Configsvc.Audit(limit)
	if err != nil {
		logError("config audit", err)
		writeError(w, http.StatusInternalServerError, "failed to load audit log")
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// AdminConfigExport serves the effective config as a downloadable YAML
// document. Sensitive values are included (this is an admin-only endpoint
// and the operator might want to bake them into config.yaml). The export
// can be saved to disk and used as the new cold-start config.
func (a *API) AdminConfigExport(w http.ResponseWriter, _ *http.Request) {
	snap := a.Configsvc.Snapshot()
	body, err := yaml.Marshal(snap)
	if err != nil {
		logError("config export marshal", err)
		writeError(w, http.StatusInternalServerError, "failed to export")
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="config.yaml"`)
	_, _ = w.Write(body)
}

// schemaField is a tiny helper around configsvc.Schema lookup; kept here so
// the rest of the package doesn't dig into the configsvc internals.
func schemaField(key string) (configsvc.Field, bool) {
	for _, f := range configsvc.Schema() {
		if f.Key == key {
			return f, true
		}
	}
	return configsvc.Field{}, false
}
