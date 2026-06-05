package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/markusfluer/steelpage-desktop/internal/permissions"
)

func (a *API) AdminListPermissions(w http.ResponseWriter, _ *http.Request) {
	rules, err := a.Permissions.List()
	if err != nil {
		logError("list permissions", err)
		writeError(w, http.StatusInternalServerError, "failed to list rules")
		return
	}
	if rules == nil {
		rules = []*permissions.Rule{}
	}
	writeJSON(w, http.StatusOK, rules)
}

type createPermissionRequest struct {
	PathGlob     string `json:"path_glob"`
	SubjectType  string `json:"subject_type"`
	SubjectValue string `json:"subject_value"`
	Permission   string `json:"permission"`
}

func (a *API) AdminCreatePermission(w http.ResponseWriter, r *http.Request) {
	var req createPermissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	rule, err := a.Permissions.Create(permissions.Rule{
		PathGlob:     req.PathGlob,
		SubjectType:  req.SubjectType,
		SubjectValue: req.SubjectValue,
		Permission:   req.Permission,
	})
	if err != nil {
		switch {
		case errors.Is(err, permissions.ErrInvalid):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, permissions.ErrDuplicate):
			writeError(w, http.StatusConflict, err.Error())
		default:
			logError("create permission", err)
			writeError(w, http.StatusInternalServerError, "failed to create rule")
		}
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

func (a *API) AdminDeletePermission(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := a.Permissions.Delete(id); err != nil {
		if errors.Is(err, permissions.ErrNotFound) {
			writeError(w, http.StatusNotFound, "rule not found")
			return
		}
		logError("delete permission", err)
		writeError(w, http.StatusInternalServerError, "failed to delete rule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) AdminEffectivePermissions(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path query parameter required")
		return
	}
	rules, err := a.Permissions.Effective(path)
	if err != nil {
		logError("effective permissions", err)
		writeError(w, http.StatusInternalServerError, "failed to compute effective rules")
		return
	}
	if rules == nil {
		rules = []*permissions.Rule{}
	}
	writeJSON(w, http.StatusOK, rules)
}
