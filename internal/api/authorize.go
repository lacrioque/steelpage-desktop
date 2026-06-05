package api

import (
	"net/http"

	"github.com/markusfluer/steelpage-desktop/internal/middleware"
	"github.com/markusfluer/steelpage-desktop/internal/permissions"
	"github.com/markusfluer/steelpage-desktop/internal/tokens"
	"github.com/markusfluer/steelpage-desktop/internal/users"
)

// authorize decides whether the request can perform `action` on `path`.
//
// Semantics ("replace defaults"):
//   - When at least one permission rule matches `path`, only those rules
//     decide: user gets allowed iff some matching rule grants ≥ action.
//   - When no rule matches `path`, defaults apply:
//     read    → honor cfg.Auth.AllowAnonymousRead
//     comment → require an authenticated user
//     write   → require an authenticated user
//
// Returns the user (may be nil), and the HTTP status to write on denial. A
// status of 0 means "allowed".
func (a *API) authorize(r *http.Request, path, action string) (*users.User, int) {
	user := middleware.FromContext(r.Context())

	// Bearer token? Check scopes first — a token-authenticated request
	// can do strictly less than the owner can. We still run the permission
	// check below so the owner's path rules are honored.
	if scopes := middleware.TokenScopesFromContext(r.Context()); scopes != nil {
		if !tokens.AllowsAction(scopes, action, path) {
			return user, http.StatusForbidden
		}
	}

	d, err := a.Permissions.Check(path, user, action)
	if err != nil {
		logError("permissions check", err)
		return user, http.StatusInternalServerError
	}

	if d.MatchedAnyRule {
		if d.AllowsUser != "" && permissionRank(d.AllowsUser) >= permissionRank(action) {
			return user, 0
		}
		if user == nil {
			return user, http.StatusUnauthorized
		}
		return user, http.StatusForbidden
	}

	// Fallback path — no rule mentions this document.
	switch action {
	case permissions.PermRead:
		if a.cfg().Auth.AllowAnonymousRead {
			return user, 0
		}
		if user == nil {
			return user, http.StatusUnauthorized
		}
		return user, 0
	case permissions.PermComment, permissions.PermWrite:
		if user == nil {
			return user, http.StatusUnauthorized
		}
		return user, 0
	}
	return user, http.StatusForbidden
}

func permissionRank(p string) int {
	switch p {
	case permissions.PermRead:
		return 1
	case permissions.PermComment:
		return 2
	case permissions.PermWrite:
		return 3
	default:
		return 0
	}
}

// denyOrContinue writes the chosen status (with a small JSON body) and
// returns false if the caller should bail. If status is 0 it returns true
// to let the handler proceed.
func denyOrContinue(w http.ResponseWriter, status int) bool {
	if status == 0 {
		return true
	}
	switch status {
	case http.StatusUnauthorized:
		writeError(w, status, "authentication required")
	case http.StatusForbidden:
		writeError(w, status, "permission denied")
	default:
		writeError(w, status, "request denied")
	}
	return false
}
