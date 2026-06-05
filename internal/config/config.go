package config

import (
	"path/filepath"

	"github.com/markusfluer/steelpage-desktop/internal/localidentity"
	"github.com/markusfluer/steelpage-desktop/internal/prefs"
)

type Config struct {
	Repo   Repo
	Server Server
	DB     DB
	Render Render
}

type Repo struct {
	Path              string
	Branch            string
	CommitAuthorName  string
	CommitAuthorEmail string
	AutoPush          bool
	PushRemote        string
}

type Server struct {
	Bind string
}

type DB struct {
	Path string
}

type Render struct {
	AllowRawHTML     bool
	Mermaid          bool
	CodeHighlighting bool
	SanitizeHTML     bool
}

// FromPrefs synthesizes the runtime config from user prefs + platform paths.
// The desktop build has no YAML config file: everything either comes from
// prefs.json or is a fixed sane default.
func FromPrefs(p prefs.Prefs, configDir string) *Config {
	id := localidentity.Default()
	return &Config{
		Repo: Repo{
			Path:              p.ContentDir,
			Branch:            "main",
			CommitAuthorName:  id.DisplayName,
			CommitAuthorEmail: id.Email,
			AutoPush:          p.PushRemote != "",
			PushRemote:        "origin",
		},
		Server: Server{
			// Loopback only, OS-assigned port. Never a public bind.
			Bind: "127.0.0.1:0",
		},
		DB: DB{
			Path: filepath.Join(configDir, "steelpage.db"),
		},
		Render: Render{
			AllowRawHTML:     false,
			Mermaid:          true,
			CodeHighlighting: true,
			SanitizeHTML:     true,
		},
	}
}
