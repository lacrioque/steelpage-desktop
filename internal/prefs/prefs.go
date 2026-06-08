// Package prefs is the desktop replacement for the server's YAML config +
// live config service: a small JSON file in the platform config dir holding
// the handful of knobs a single local user actually changes.
package prefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

type Prefs struct {
	// ContentDir is the markdown archive (a git repo, auto-initialized on
	// first launch). Defaults to ~/Documents/Steelpage.
	ContentDir string `json:"content_dir"`
	Theme      string `json:"theme,omitempty"` // "light" for now; dark is a follow-up
	Font       string `json:"font,omitempty"`  // empty = frontend default
	LastDoc    string `json:"last_doc,omitempty"`
	// PushRemote enables opt-in, push-only backup of the archive to an
	// HTTPS git remote. PushToken is sent as basic-auth password.
	// TODO(follow-up): move the token into the OS keychain.
	PushRemote string `json:"push_remote,omitempty"`
	PushToken  string `json:"push_token,omitempty"`
	// ServerURL + ServerToken switch the app into "server mode": instead of
	// the local archive, the loopback reverse-proxies to a remote steelpage
	// server using the bearer token. Empty ServerURL → local mode.
	// TODO(follow-up): move the token into the OS keychain [[steelpage-kyx]].
	ServerURL   string `json:"server_url,omitempty"`
	ServerToken string `json:"server_token,omitempty"`
}

// Remote reports whether the app should run in server mode (proxy to a
// remote steelpage server) rather than against the local archive.
func (p Prefs) Remote() bool {
	return p.ServerURL != "" && p.ServerToken != ""
}

// Dir returns the platform config directory for the app, e.g.
// ~/.config/steelpage-desktop (Linux), ~/Library/Application Support/Steelpage
// (macOS), %APPDATA%\Steelpage (Windows). The SQLite DB lives here too.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	name := "steelpage-desktop"
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		name = "Steelpage"
	}
	return filepath.Join(base, name), nil
}

// DefaultContentDir is ~/Documents/Steelpage on every platform.
func DefaultContentDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, "Documents", "Steelpage"), nil
}

func path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "prefs.json"), nil
}

// Load reads prefs.json, returning defaults when the file does not exist
// yet (first launch).
func Load() (Prefs, error) {
	var p Prefs
	fp, err := path()
	if err != nil {
		return p, err
	}
	body, err := os.ReadFile(fp)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// first launch — fall through to defaults
	case err != nil:
		return p, fmt.Errorf("read %s: %w", fp, err)
	default:
		if err := json.Unmarshal(body, &p); err != nil {
			return p, fmt.Errorf("parse %s: %w", fp, err)
		}
	}
	if p.ContentDir == "" {
		p.ContentDir, err = DefaultContentDir()
		if err != nil {
			return p, err
		}
	}
	return p, nil
}

// Save writes prefs.json, creating the config dir if needed.
func (p Prefs) Save() error {
	fp, err := path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	body, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(fp, append(body, '\n'), 0o600)
}
