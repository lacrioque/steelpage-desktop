package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/alexedwards/scs/v2"

	"github.com/markusfluer/steelpage-desktop/internal/groups"
	"github.com/markusfluer/steelpage-desktop/internal/tokens"
	"github.com/markusfluer/steelpage-desktop/internal/users"
)

type ctxKey int

const (
	userKey   ctxKey = 1
	scopesKey ctxKey = 2
)

// SessionUserKey is the key under which we stash the user_id inside the scs
// session. Keep it in one place so handlers don't disagree about it.
const SessionUserKey = "user_id"

// Identity attaches a *users.User to the request context. Two sources, checked
// in order:
//
//  1. Bearer token: Authorization: Bearer <plaintext>. Sets the owner user
//     AND a list of token scopes on the context. authorize() will then refuse
//     actions that fall outside those scopes.
//  2. Session cookie: scs-backed; no scope restrictions (the user can do
//     anything their permissions allow).
//
// Unauthenticated requests pass through untouched — handlers / authorize()
// decide whether that's OK.
func Identity(sm *scs.SessionManager, ustore *users.Store, gs *groups.Store, tstore *tokens.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if u, scopes, ok := identifyFromToken(r, ustore, gs, tstore); ok {
				ctx := context.WithValue(r.Context(), userKey, u)
				ctx = context.WithValue(ctx, scopesKey, scopes)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			if u, ok := identifyFromSession(r, sm, ustore, gs); ok {
				ctx := context.WithValue(r.Context(), userKey, u)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func identifyFromToken(r *http.Request, ustore *users.Store, gs *groups.Store, tstore *tokens.Store) (*users.User, []string, bool) {
	if tstore == nil {
		return nil, nil, false
	}
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return nil, nil, false
	}
	plaintext := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if plaintext == "" {
		return nil, nil, false
	}
	tok, err := tstore.FindByPlaintext(plaintext)
	if err != nil {
		return nil, nil, false
	}
	u, err := ustore.GetByID(tok.UserID)
	if err != nil {
		return nil, nil, false
	}
	if names, gerr := gs.GroupsOf(u.ID); gerr == nil {
		u.Groups = names
	}
	return u, tok.Scopes, true
}

func identifyFromSession(r *http.Request, sm *scs.SessionManager, ustore *users.Store, gs *groups.Store) (*users.User, bool) {
	id := sm.GetInt64(r.Context(), SessionUserKey)
	if id <= 0 {
		return nil, false
	}
	u, err := ustore.GetByID(id)
	if err != nil {
		return nil, false
	}
	if names, gerr := gs.GroupsOf(u.ID); gerr == nil {
		u.Groups = names
	}
	return u, true
}

// TokenScopesFromContext returns the scope list when the current request was
// authenticated via a Bearer token; nil for session-based requests or
// anonymous ones.
func TokenScopesFromContext(ctx context.Context) []string {
	if v, ok := ctx.Value(scopesKey).([]string); ok {
		return v
	}
	return nil
}

// FromContext returns the user attached by Identity middleware, or nil.
func FromContext(ctx context.Context) *users.User {
	if v, ok := ctx.Value(userKey).(*users.User); ok {
		return v
	}
	return nil
}

// RequireAuth refuses requests without an attached user.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if FromContext(r.Context()) == nil {
			writeUnauth(w, "authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin enforces role == "admin".
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := FromContext(r.Context())
		if u == nil {
			writeUnauth(w, "authentication required")
			return
		}
		if u.Role != users.RoleAdmin {
			http.Error(w, `{"error":"admin required"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeUnauth(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}

// ErrUnauthenticated kept for API surface compatibility with older callers.
var ErrUnauthenticated = errors.New("authentication required")
