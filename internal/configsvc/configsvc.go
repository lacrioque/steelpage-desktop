package configsvc

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/markusfluer/steelpage-desktop/internal/config"
)

var (
	ErrUnknownKey = errors.New("unknown config key")
	ErrReadOnly   = errors.New("field is config.yaml only")
	ErrValidation = errors.New("invalid value")
)

// AuditEntry mirrors a row in config_audit; secrets are redacted before
// they ever leave the package.
type AuditEntry struct {
	ID           int64   `json:"id"`
	ActorUserID  *int64  `json:"actor_user_id"`
	ActorDisplay *string `json:"actor_display,omitempty"`
	Key          string  `json:"key"`
	OldValue     *string `json:"old_value"`
	NewValue     *string `json:"new_value"`
	At           string  `json:"at"`
}

type listener struct {
	prefix string
	fn     func(snapshot *config.Config, changed []string)
}

type Service struct {
	base      *config.Config
	overrides map[string]json.RawMessage
	listeners []listener
	db        *sql.DB
	mu        sync.RWMutex
}

// New loads any overrides that exist in the DB and returns a ready service.
// `base` should be the result of config.Load() — that becomes the immutable
// cold-start baseline.
func New(db *sql.DB, base *config.Config) (*Service, error) {
	s := &Service{
		base:      base,
		db:        db,
		overrides: map[string]json.RawMessage{},
	}
	if err := s.loadOverrides(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) loadOverrides() error {
	rows, err := s.db.Query(`SELECT key, value FROM config_overrides`)
	if err != nil {
		return fmt.Errorf("load overrides: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return err
		}
		// Silently drop overrides for keys we no longer know about (e.g.,
		// after a schema change). They stay in the DB so the operator can
		// inspect/clean them up.
		if _, ok := fieldsByKey[key]; ok {
			s.overrides[key] = json.RawMessage(value)
		}
	}
	return rows.Err()
}

// Snapshot returns a deep-copy of the effective config: YAML defaults with
// overrides applied. Listeners receive this same shape.
func (s *Service) Snapshot() *config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

func (s *Service) snapshotLocked() *config.Config {
	// Deep-copy via JSON round-trip. The base struct has yaml tags but Go's
	// json package uses field names by default — round-tripping with the
	// same package is internally consistent and avoids aliasing slices like
	// Auth.OIDC.Scopes.
	data, err := json.Marshal(s.base)
	if err != nil {
		// Should never happen for our concrete struct.
		copy := *s.base
		return &copy
	}
	var cloned config.Config
	if err := json.Unmarshal(data, &cloned); err != nil {
		copy := *s.base
		return &copy
	}
	for key, raw := range s.overrides {
		f, ok := fieldsByKey[key]
		if !ok || f.applyTo == nil {
			continue
		}
		_ = f.applyTo(&cloned, raw)
	}
	return &cloned
}

// Set persists a new value for the given key, audits the change, and notifies
// subscribers whose prefix matches. Sensitive keys are redacted in audit.
func (s *Service) Set(actorID *int64, key string, raw json.RawMessage) error {
	field, ok := fieldsByKey[key]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	if field.ReadOnly || field.applyTo == nil {
		return fmt.Errorf("%w: %s", ErrReadOnly, key)
	}
	if err := field.validate(raw); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}

	s.mu.Lock()
	previous, hadPrev := s.overrides[key]
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO config_overrides(key, value, updated_at, updated_by)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at, updated_by=excluded.updated_by`,
		key, string(raw), now, nullInt64(actorID),
	); err != nil {
		_ = tx.Rollback()
		s.mu.Unlock()
		return fmt.Errorf("persist override: %w", err)
	}
	oldRedacted := redactForAudit(field, baseOrPrev(s.base, field, previous, hadPrev))
	newRedacted := redactForAudit(field, raw)
	if _, err := tx.Exec(`
		INSERT INTO config_audit(actor_user_id, key, old_value, new_value, at)
		VALUES(?, ?, ?, ?, ?)`,
		nullInt64(actorID), key, oldRedacted, newRedacted, now,
	); err != nil {
		_ = tx.Rollback()
		s.mu.Unlock()
		return fmt.Errorf("audit: %w", err)
	}
	if err := tx.Commit(); err != nil {
		s.mu.Unlock()
		return err
	}

	s.overrides[key] = raw
	snap := s.snapshotLocked()
	listeners := append([]listener(nil), s.listeners...)
	s.mu.Unlock()

	for _, l := range listeners {
		if strings.HasPrefix(key, l.prefix) {
			l.fn(snap, []string{key})
		}
	}
	return nil
}

// Unset removes an override, falling back to the YAML default. Audited.
func (s *Service) Unset(actorID *int64, key string) error {
	field, ok := fieldsByKey[key]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	if field.ReadOnly {
		return fmt.Errorf("%w: %s", ErrReadOnly, key)
	}

	s.mu.Lock()
	previous, hadPrev := s.overrides[key]
	if !hadPrev {
		// No-op — already at YAML default.
		s.mu.Unlock()
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := s.db.Begin()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM config_overrides WHERE key = ?`, key); err != nil {
		_ = tx.Rollback()
		s.mu.Unlock()
		return err
	}
	oldRedacted := redactForAudit(field, previous)
	if _, err := tx.Exec(`
		INSERT INTO config_audit(actor_user_id, key, old_value, new_value, at)
		VALUES(?, ?, ?, ?, ?)`,
		nullInt64(actorID), key, oldRedacted, sql.NullString{}, now,
	); err != nil {
		_ = tx.Rollback()
		s.mu.Unlock()
		return err
	}
	if err := tx.Commit(); err != nil {
		s.mu.Unlock()
		return err
	}

	delete(s.overrides, key)
	snap := s.snapshotLocked()
	listeners := append([]listener(nil), s.listeners...)
	s.mu.Unlock()

	for _, l := range listeners {
		if strings.HasPrefix(key, l.prefix) {
			l.fn(snap, []string{key})
		}
	}
	return nil
}

// Subscribe registers a callback that fires whenever a key with the given
// prefix changes. Useful for subsystems that cache derived state.
//
// The callback runs on the Set/Unset goroutine, AFTER the override has been
// persisted and the snapshot has been recomputed. If the listener panics or
// returns an error, the override still stands — fail-keeps-running.
func (s *Service) Subscribe(prefix string, fn func(*config.Config, []string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, listener{prefix: prefix, fn: fn})
}

// Effective returns the snapshot plus per-field "has_override" and "sensitive"
// flags so the API can shape its response in one pass. Sensitive values are
// always nil in the result; the frontend reads `has_value` instead.
type FieldState struct {
	Key         string `json:"key"`
	Value       any    `json:"value,omitempty"`
	HasValue    bool   `json:"has_value"`
	HasOverride bool   `json:"has_override"`
	Sensitive   bool   `json:"sensitive"`
	ReadOnly    bool   `json:"read_only"`
}

func (s *Service) Effective() []FieldState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := s.snapshotLocked()
	out := make([]FieldState, 0, len(allFields))
	for _, f := range allFields {
		_, hasOverride := s.overrides[f.Key]
		state := FieldState{
			Key:         f.Key,
			HasOverride: hasOverride,
			Sensitive:   f.Sensitive,
			ReadOnly:    f.ReadOnly,
		}
		if f.currentValue != nil {
			val := f.currentValue(snap)
			state.HasValue = !isZero(val)
			if !f.Sensitive {
				state.Value = val
			}
		}
		out = append(out, state)
	}
	return out
}

// Audit returns the most recent config edits (newest first), capped at limit.
func (s *Service) Audit(limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT a.id, a.actor_user_id, u.display_name, a.key, a.old_value, a.new_value, a.at
		FROM config_audit a
		LEFT JOIN users u ON u.id = a.actor_user_id
		ORDER BY a.id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var (
			e          AuditEntry
			actor      sql.NullInt64
			display    sql.NullString
			oldV, newV sql.NullString
		)
		if err := rows.Scan(&e.ID, &actor, &display, &e.Key, &oldV, &newV, &e.At); err != nil {
			return nil, err
		}
		if actor.Valid {
			v := actor.Int64
			e.ActorUserID = &v
		}
		if display.Valid {
			v := display.String
			e.ActorDisplay = &v
		}
		if oldV.Valid {
			v := oldV.String
			e.OldValue = &v
		}
		if newV.Valid {
			v := newV.String
			e.NewValue = &v
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// baseOrPrev returns the JSON value used as the "old" side of an audit entry:
// the previous override if one existed, otherwise the YAML default.
func baseOrPrev(base *config.Config, f Field, prev json.RawMessage, hadPrev bool) json.RawMessage {
	if hadPrev {
		return prev
	}
	if f.currentValue == nil {
		return nil
	}
	raw, _ := json.Marshal(f.currentValue(base))
	return raw
}

func redactForAudit(f Field, raw json.RawMessage) sql.NullString {
	if raw == nil {
		return sql.NullString{}
	}
	if f.Sensitive {
		return sql.NullString{Valid: true, String: `"***"`}
	}
	return sql.NullString{Valid: true, String: string(raw)}
}

func nullInt64(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

func isZero(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case string:
		return x == ""
	case bool:
		return !x
	case int:
		return x == 0
	case int64:
		return x == 0
	case []string:
		return len(x) == 0
	default:
		return false
	}
}
