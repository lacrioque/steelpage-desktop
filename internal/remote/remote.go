// Package remote turns the desktop into a thin client of a multi-user
// steelpage server: a reverse proxy that injects a bearer token, plus a
// connection probe. The server enforces the token's scopes and permissions;
// the desktop just forwards.
package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// User is the minimal projection of the connected account that the desktop
// shows ("connected as …").
type User struct {
	ID          int64  `json:"id"`
	DisplayName string `json:"display_name"`
}

// Proxy returns an http.Handler that reverse-proxies to serverURL, adding
// `Authorization: Bearer <token>` to every request. serverURL is the base
// origin of the steelpage server (e.g. https://docs.example.com).
func Proxy(serverURL, token string) (http.Handler, error) {
	target, err := normalize(serverURL)
	if err != nil {
		return nil, err
	}
	rp := httputil.NewSingleHostReverseProxy(target)
	base := rp.Director
	rp.Director = func(req *http.Request) {
		base(req)
		req.Host = target.Host
		req.Header.Set("Authorization", "Bearer "+token)
		// The loopback origin has no business forwarding cookies upstream.
		req.Header.Del("Cookie")
	}
	return rp, nil
}

// TestConnection verifies the token against serverURL by calling GET
// /api/me, returning the connected user or a human-readable error.
func TestConnection(ctx context.Context, serverURL, token string) (User, error) {
	var u User
	target, err := normalize(serverURL)
	if err != nil {
		return u, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String()+"/api/me", nil)
	if err != nil {
		return u, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return u, fmt.Errorf("could not reach %s", target.Host)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
			return u, fmt.Errorf("unexpected response from server")
		}
		return u, nil
	case http.StatusUnauthorized, http.StatusNoContent, http.StatusForbidden:
		return u, fmt.Errorf("invalid or expired token")
	default:
		return u, fmt.Errorf("server returned %d", resp.StatusCode)
	}
}

func normalize(serverURL string) (*url.URL, error) {
	s := strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if s == "" {
		return nil, fmt.Errorf("server URL is empty")
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("invalid server URL: missing host")
	}
	return u, nil
}
