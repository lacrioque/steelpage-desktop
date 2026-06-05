package configsvc_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/markusfluer/steelpage-desktop/internal/config"
	"github.com/markusfluer/steelpage-desktop/internal/configsvc"
	"github.com/markusfluer/steelpage-desktop/internal/db"
)

func newService(t *testing.T) *configsvc.Service {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(d); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	base := &config.Config{}
	base.Repo.Path = "/tmp/content"
	base.Repo.CommitAuthorName = "Steelpage"
	base.Email.Host = ""
	base.Email.Port = 587
	base.Email.Encryption = "starttls"
	base.Email.Password = ""

	s, err := configsvc.New(d, base)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return s
}

func TestSnapshotInheritsBase(t *testing.T) {
	s := newService(t)
	got := s.Snapshot()
	if got.Repo.CommitAuthorName != "Steelpage" {
		t.Fatalf("expected base author, got %q", got.Repo.CommitAuthorName)
	}
	if got.Email.Port != 587 {
		t.Fatalf("expected base port, got %d", got.Email.Port)
	}
}

func TestSetThenSnapshot(t *testing.T) {
	s := newService(t)
	if err := s.Set(nil, "email.host", json.RawMessage(`"smtp.example.com"`)); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := s.Set(nil, "email.port", json.RawMessage(`465`)); err != nil {
		t.Fatalf("set port: %v", err)
	}
	snap := s.Snapshot()
	if snap.Email.Host != "smtp.example.com" || snap.Email.Port != 465 {
		t.Fatalf("override not applied: %+v", snap.Email)
	}
}

func TestSetReadOnlyRefused(t *testing.T) {
	s := newService(t)
	err := s.Set(nil, "server.bind", json.RawMessage(`"127.0.0.1:9999"`))
	if err == nil {
		t.Fatalf("expected ErrReadOnly")
	}
}

func TestSetValidatesEnum(t *testing.T) {
	s := newService(t)
	err := s.Set(nil, "email.encryption", json.RawMessage(`"smtps"`))
	if err == nil {
		t.Fatalf("expected validation error for unknown enum")
	}
	if !strings.Contains(err.Error(), "must be one of") {
		t.Fatalf("expected enum message, got %v", err)
	}
}

func TestUnsetRevertsToBase(t *testing.T) {
	s := newService(t)
	_ = s.Set(nil, "email.host", json.RawMessage(`"smtp.example.com"`))
	if got := s.Snapshot().Email.Host; got != "smtp.example.com" {
		t.Fatalf("override didn't stick: %q", got)
	}
	if err := s.Unset(nil, "email.host"); err != nil {
		t.Fatalf("unset: %v", err)
	}
	if got := s.Snapshot().Email.Host; got != "" {
		t.Fatalf("expected base value after unset, got %q", got)
	}
}

func TestSubscribeFiresOnMatchingPrefix(t *testing.T) {
	s := newService(t)
	fired := 0
	s.Subscribe("email.", func(snap *config.Config, changed []string) {
		fired++
		if snap.Email.Host != "smtp.example.com" {
			t.Errorf("listener got stale snapshot: %q", snap.Email.Host)
		}
	})
	s.Subscribe("auth.", func(*config.Config, []string) {
		t.Errorf("auth listener should not fire on email change")
	})
	_ = s.Set(nil, "email.host", json.RawMessage(`"smtp.example.com"`))
	if fired != 1 {
		t.Fatalf("expected 1 callback, got %d", fired)
	}
}

func TestAuditRedactsSensitive(t *testing.T) {
	s := newService(t)
	_ = s.Set(nil, "email.password", json.RawMessage(`"super-secret"`))
	entries, err := s.Audit(10)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit row, got %d", len(entries))
	}
	if entries[0].NewValue == nil || *entries[0].NewValue != `"***"` {
		t.Fatalf("expected redacted new_value, got %v", entries[0].NewValue)
	}
}

func TestEffectiveHidesSensitiveValue(t *testing.T) {
	s := newService(t)
	_ = s.Set(nil, "email.password", json.RawMessage(`"super-secret"`))
	for _, fs := range s.Effective() {
		if fs.Key != "email.password" {
			continue
		}
		if fs.Value != nil {
			t.Fatalf("sensitive value leaked: %v", fs.Value)
		}
		if !fs.HasValue {
			t.Fatalf("expected has_value:true for set sensitive field")
		}
		if !fs.HasOverride {
			t.Fatalf("expected has_override:true")
		}
	}
}
