package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/markusfluer/steelpage-desktop/internal/groups"
	"github.com/markusfluer/steelpage-desktop/internal/users"
)

type adminUser struct {
	*users.User
	Groups []string `json:"groups"`
}

func (a *API) AdminListUsers(w http.ResponseWriter, _ *http.Request) {
	list, err := a.Users.ListAll()
	if err != nil {
		logError("list users", err)
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	out := make([]adminUser, 0, len(list))
	for _, u := range list {
		names, _ := a.Groups.GroupsOf(u.ID)
		if names == nil {
			names = []string{}
		}
		u.Groups = names
		out = append(out, adminUser{User: u, Groups: names})
	}
	writeJSON(w, http.StatusOK, out)
}

type patchUserRequest struct {
	Role *string `json:"role,omitempty"`
}

func (a *API) AdminPatchUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req patchUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Role != nil {
		if err := a.Users.SetRole(id, *req.Role); err != nil {
			switch {
			case errors.Is(err, users.ErrNotFound):
				writeError(w, http.StatusNotFound, "user not found")
			case errors.Is(err, users.ErrInvalidRole):
				writeError(w, http.StatusBadRequest, err.Error())
			default:
				logError("set role", err)
				writeError(w, http.StatusInternalServerError, "failed to update role")
			}
			return
		}
	}
	u, err := a.Users.GetByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if names, gerr := a.Groups.GroupsOf(u.ID); gerr == nil {
		u.Groups = names
	}
	writeJSON(w, http.StatusOK, u)
}

// AdminDisableUserMFA is the emergency unlock for users who lost their
// authenticator app. Admins-only. Strips totp_secret + totp_enabled_at.
func (a *API) AdminDisableUserMFA(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := a.Users.DisableTOTP(id); err != nil {
		if errors.Is(err, users.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		logError("disable user mfa", err)
		writeError(w, http.StatusInternalServerError, "failed to disable MFA")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) AdminListGroups(w http.ResponseWriter, _ *http.Request) {
	list, err := a.Groups.List()
	if err != nil {
		logError("list groups", err)
		writeError(w, http.StatusInternalServerError, "failed to list groups")
		return
	}
	if list == nil {
		list = []*groups.Group{}
	}
	writeJSON(w, http.StatusOK, list)
}

type createGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (a *API) AdminCreateGroup(w http.ResponseWriter, r *http.Request) {
	var req createGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	g, err := a.Groups.Create(req.Name, req.Description)
	if err != nil {
		switch {
		case errors.Is(err, groups.ErrInvalid):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, groups.ErrTaken):
			writeError(w, http.StatusConflict, err.Error())
		default:
			logError("create group", err)
			writeError(w, http.StatusInternalServerError, "failed to create group")
		}
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

func (a *API) AdminDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := a.Groups.Delete(id); err != nil {
		if errors.Is(err, groups.ErrNotFound) {
			writeError(w, http.StatusNotFound, "group not found")
			return
		}
		logError("delete group", err)
		writeError(w, http.StatusInternalServerError, "failed to delete group")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type addMemberRequest struct {
	UserID int64 `json:"user_id"`
}

func (a *API) AdminAddMember(w http.ResponseWriter, r *http.Request) {
	gid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid group id")
		return
	}
	var req addMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if _, err := a.Users.GetByID(req.UserID); err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if _, err := a.Groups.GetByID(gid); err != nil {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}
	if err := a.Groups.AddMember(req.UserID, gid); err != nil {
		logError("add group member", err)
		writeError(w, http.StatusInternalServerError, "failed to add member")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) AdminRemoveMember(w http.ResponseWriter, r *http.Request) {
	gid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid group id")
		return
	}
	uid, err := strconv.ParseInt(chi.URLParam(r, "user_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if err := a.Groups.RemoveMember(uid, gid); err != nil {
		logError("remove member", err)
		writeError(w, http.StatusInternalServerError, "failed to remove member")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
