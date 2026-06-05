// Package configsvc lives between config.yaml (immutable cold-start defaults)
// and the running server. Overrides set by admins are stored in
// config_overrides; calling Snapshot() returns the effective config (YAML +
// overrides applied). Subsystems that cache derived state can Subscribe to a
// prefix and rebuild themselves when relevant keys change.
package configsvc

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/markusfluer/steelpage-desktop/internal/config"
)

// Field describes one editable (or read-only) entry in the live-config UI.
// The exported tags drive both the API response (Schema endpoint) and the
// frontend's per-field component selection.
type Field struct {
	Key       string   `json:"key"`
	Type      string   `json:"type"`           // "string" | "int" | "bool" | "enum" | "string_slice"
	Enum      []string `json:"enum,omitempty"` // for type=enum
	Sensitive bool     `json:"sensitive"`      // hidden in responses, write-only
	ReadOnly  bool     `json:"read_only"`      // YAML-only (restart-required, or not yet wired for live reload)
	Group     string   `json:"group"`          // UI grouping
	Order     int      `json:"order"`          // sort order within group
	Min       *int     `json:"min,omitempty"`  // int validators
	Max       *int     `json:"max,omitempty"`

	// applyTo writes the (already-validated) value into the given config.
	applyTo func(cfg *config.Config, value json.RawMessage) error

	// currentValue extracts the current value for the effective response.
	currentValue func(cfg *config.Config) any
}

func intPtr(v int) *int { return &v }

// allFields enumerates every config key the admin UI knows about. Anything
// not here is implicitly off-limits for live editing.
//
// Adding a new live field = add a row. Don't reflect — the explicit table
// keeps the API stable and audit-friendly.
var allFields = []Field{
	// Repo — per-request reads, no subsystem reload needed.
	{
		Key: "repo.commit_author_name", Type: "string", Group: "Repo", Order: 10,
		applyTo: func(cfg *config.Config, v json.RawMessage) error {
			return json.Unmarshal(v, &cfg.Repo.CommitAuthorName)
		},
		currentValue: func(cfg *config.Config) any { return cfg.Repo.CommitAuthorName },
	},
	{
		Key: "repo.commit_author_email", Type: "string", Group: "Repo", Order: 11,
		applyTo: func(cfg *config.Config, v json.RawMessage) error {
			return json.Unmarshal(v, &cfg.Repo.CommitAuthorEmail)
		},
		currentValue: func(cfg *config.Config) any { return cfg.Repo.CommitAuthorEmail },
	},
	{
		Key: "repo.auto_push", Type: "bool", Group: "Repo", Order: 20,
		applyTo:      func(cfg *config.Config, v json.RawMessage) error { return json.Unmarshal(v, &cfg.Repo.AutoPush) },
		currentValue: func(cfg *config.Config) any { return cfg.Repo.AutoPush },
	},
	{
		Key: "repo.push_remote", Type: "string", Group: "Repo", Order: 21,
		applyTo:      func(cfg *config.Config, v json.RawMessage) error { return json.Unmarshal(v, &cfg.Repo.PushRemote) },
		currentValue: func(cfg *config.Config) any { return cfg.Repo.PushRemote },
	},
	{Key: "repo.path", Type: "string", Group: "Repo", Order: 1, ReadOnly: true,
		currentValue: func(cfg *config.Config) any { return cfg.Repo.Path }},
	{Key: "repo.branch", Type: "string", Group: "Repo", Order: 2, ReadOnly: true,
		currentValue: func(cfg *config.Config) any { return cfg.Repo.Branch }},

	// Server — base_url editable (used for outgoing email links), the rest read-only.
	{
		Key: "server.base_url", Type: "string", Group: "Server", Order: 10,
		applyTo:      func(cfg *config.Config, v json.RawMessage) error { return json.Unmarshal(v, &cfg.Server.BaseURL) },
		currentValue: func(cfg *config.Config) any { return cfg.Server.BaseURL },
	},
	{Key: "server.bind", Type: "string", Group: "Server", Order: 1, ReadOnly: true,
		currentValue: func(cfg *config.Config) any { return cfg.Server.Bind }},

	// DB / frontend — pure runtime infrastructure.
	{Key: "db.path", Type: "string", Group: "Storage", Order: 1, ReadOnly: true,
		currentValue: func(cfg *config.Config) any { return cfg.DB.Path }},
	{Key: "frontend.embedded_dist", Type: "string", Group: "Storage", Order: 2, ReadOnly: true,
		currentValue: func(cfg *config.Config) any { return cfg.Frontend.EmbeddedDist }},

	// Auth flags — read on every authorize() call, so live-editable safely.
	{
		Key: "auth.allow_anonymous_read", Type: "bool", Group: "Auth", Order: 10,
		applyTo: func(cfg *config.Config, v json.RawMessage) error {
			return json.Unmarshal(v, &cfg.Auth.AllowAnonymousRead)
		},
		currentValue: func(cfg *config.Config) any { return cfg.Auth.AllowAnonymousRead },
	},
	{
		Key: "auth.local_enabled", Type: "bool", Group: "Auth", Order: 11,
		applyTo:      func(cfg *config.Config, v json.RawMessage) error { return json.Unmarshal(v, &cfg.Auth.LocalEnabled) },
		currentValue: func(cfg *config.Config) any { return cfg.Auth.LocalEnabled },
	},
	{Key: "auth.mode", Type: "string", Group: "Auth", Order: 1, ReadOnly: true,
		currentValue: func(cfg *config.Config) any { return cfg.Auth.Mode }},
	{Key: "auth.session.secure", Type: "bool", Group: "Auth", Order: 2, ReadOnly: true,
		currentValue: func(cfg *config.Config) any { return cfg.Auth.Session.Secure }},

	// OIDC — read-only in v1. Live reload (re-registering the goth provider)
	// is a follow-up; the schema entries still surface them so admins know
	// what's configured.
	{Key: "auth.oidc.enabled", Type: "bool", Group: "OIDC", Order: 1, ReadOnly: true,
		currentValue: func(cfg *config.Config) any { return cfg.Auth.OIDC.Enabled }},
	{Key: "auth.oidc.label", Type: "string", Group: "OIDC", Order: 2, ReadOnly: true,
		currentValue: func(cfg *config.Config) any { return cfg.Auth.OIDC.Label }},
	{Key: "auth.oidc.issuer_url", Type: "string", Group: "OIDC", Order: 3, ReadOnly: true,
		currentValue: func(cfg *config.Config) any { return cfg.Auth.OIDC.IssuerURL }},
	{Key: "auth.oidc.client_id", Type: "string", Group: "OIDC", Order: 4, ReadOnly: true,
		currentValue: func(cfg *config.Config) any { return cfg.Auth.OIDC.ClientID }},
	{Key: "auth.oidc.client_secret", Type: "string", Group: "OIDC", Order: 5, ReadOnly: true, Sensitive: true,
		currentValue: func(cfg *config.Config) any { return cfg.Auth.OIDC.ClientSecret }},
	{Key: "auth.oidc.redirect_url", Type: "string", Group: "OIDC", Order: 6, ReadOnly: true,
		currentValue: func(cfg *config.Config) any { return cfg.Auth.OIDC.RedirectURL }},
	{Key: "auth.oidc.scopes", Type: "string_slice", Group: "OIDC", Order: 7, ReadOnly: true,
		currentValue: func(cfg *config.Config) any { return cfg.Auth.OIDC.Scopes }},

	// Render — read-only in v1 (the Goldmark instance is built once).
	{Key: "render.allow_raw_html", Type: "bool", Group: "Render", Order: 1, ReadOnly: true,
		currentValue: func(cfg *config.Config) any { return cfg.Render.AllowRawHTML }},
	{Key: "render.mermaid", Type: "bool", Group: "Render", Order: 2, ReadOnly: true,
		currentValue: func(cfg *config.Config) any { return cfg.Render.Mermaid }},
	{Key: "render.code_highlighting", Type: "bool", Group: "Render", Order: 3, ReadOnly: true,
		currentValue: func(cfg *config.Config) any { return cfg.Render.CodeHighlighting }},
	{Key: "render.sanitize_html", Type: "bool", Group: "Render", Order: 4, ReadOnly: true,
		currentValue: func(cfg *config.Config) any { return cfg.Render.SanitizeHTML }},

	// Search — single string for now.
	{
		Key: "search.engine", Type: "string", Group: "Search", Order: 1,
		applyTo:      func(cfg *config.Config, v json.RawMessage) error { return json.Unmarshal(v, &cfg.Search.Engine) },
		currentValue: func(cfg *config.Config) any { return cfg.Search.Engine },
	},

	// Email — full hot-reload via the mailer subsystem subscriber.
	{
		Key: "email.host", Type: "string", Group: "Email", Order: 1,
		applyTo:      func(cfg *config.Config, v json.RawMessage) error { return json.Unmarshal(v, &cfg.Email.Host) },
		currentValue: func(cfg *config.Config) any { return cfg.Email.Host },
	},
	{
		Key: "email.port", Type: "int", Group: "Email", Order: 2, Min: intPtr(0), Max: intPtr(65535),
		applyTo:      func(cfg *config.Config, v json.RawMessage) error { return json.Unmarshal(v, &cfg.Email.Port) },
		currentValue: func(cfg *config.Config) any { return cfg.Email.Port },
	},
	{
		Key: "email.encryption", Type: "enum", Enum: []string{"none", "starttls", "tls"}, Group: "Email", Order: 3,
		applyTo:      func(cfg *config.Config, v json.RawMessage) error { return json.Unmarshal(v, &cfg.Email.Encryption) },
		currentValue: func(cfg *config.Config) any { return cfg.Email.Encryption },
	},
	{
		Key: "email.username", Type: "string", Group: "Email", Order: 4,
		applyTo:      func(cfg *config.Config, v json.RawMessage) error { return json.Unmarshal(v, &cfg.Email.Username) },
		currentValue: func(cfg *config.Config) any { return cfg.Email.Username },
	},
	{
		Key: "email.password", Type: "string", Group: "Email", Order: 5, Sensitive: true,
		applyTo:      func(cfg *config.Config, v json.RawMessage) error { return json.Unmarshal(v, &cfg.Email.Password) },
		currentValue: func(cfg *config.Config) any { return cfg.Email.Password },
	},
	{
		Key: "email.from_address", Type: "string", Group: "Email", Order: 6,
		applyTo:      func(cfg *config.Config, v json.RawMessage) error { return json.Unmarshal(v, &cfg.Email.FromAddress) },
		currentValue: func(cfg *config.Config) any { return cfg.Email.FromAddress },
	},
	{
		Key: "email.from_name", Type: "string", Group: "Email", Order: 7,
		applyTo:      func(cfg *config.Config, v json.RawMessage) error { return json.Unmarshal(v, &cfg.Email.FromName) },
		currentValue: func(cfg *config.Config) any { return cfg.Email.FromName },
	},
	{
		Key: "email.reply_to", Type: "string", Group: "Email", Order: 8,
		applyTo:      func(cfg *config.Config, v json.RawMessage) error { return json.Unmarshal(v, &cfg.Email.ReplyTo) },
		currentValue: func(cfg *config.Config) any { return cfg.Email.ReplyTo },
	},
	{
		Key: "email.insecure_skip_verify", Type: "bool", Group: "Email", Order: 9,
		applyTo: func(cfg *config.Config, v json.RawMessage) error {
			return json.Unmarshal(v, &cfg.Email.InsecureSkipVerify)
		},
		currentValue: func(cfg *config.Config) any { return cfg.Email.InsecureSkipVerify },
	},
}

var fieldsByKey = func() map[string]Field {
	m := make(map[string]Field, len(allFields))
	for _, f := range allFields {
		m[f.Key] = f
	}
	return m
}()

// Schema returns the editable-fields manifest the admin UI reads to render
// itself. Sensitive flags + read-only flags ship to the client.
func Schema() []Field {
	out := make([]Field, len(allFields))
	copy(out, allFields)
	return out
}

// validate checks that `raw` matches the field's type + enum + range. It does
// not mutate the config — applyTo does that.
func (f Field) validate(raw json.RawMessage) error {
	switch f.Type {
	case "string":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return fmt.Errorf("expected string: %w", err)
		}
		return nil
	case "int":
		var n int
		if err := json.Unmarshal(raw, &n); err != nil {
			return fmt.Errorf("expected int: %w", err)
		}
		if f.Min != nil && n < *f.Min {
			return fmt.Errorf("must be >= %d", *f.Min)
		}
		if f.Max != nil && n > *f.Max {
			return fmt.Errorf("must be <= %d", *f.Max)
		}
		return nil
	case "bool":
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return fmt.Errorf("expected bool: %w", err)
		}
		return nil
	case "enum":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return fmt.Errorf("expected string: %w", err)
		}
		for _, allowed := range f.Enum {
			if allowed == s {
				return nil
			}
		}
		return fmt.Errorf("must be one of %s", strings.Join(f.Enum, ", "))
	case "string_slice":
		var ss []string
		if err := json.Unmarshal(raw, &ss); err != nil {
			return fmt.Errorf("expected []string: %w", err)
		}
		return nil
	default:
		return errors.New("unknown field type")
	}
}
