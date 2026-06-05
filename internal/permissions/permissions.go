package permissions

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/markusfluer/steelpage-desktop/internal/users"
)

// Subject types.
const (
	SubjectAnonymous     = "anonymous"
	SubjectAuthenticated = "authenticated"
	SubjectRole          = "role"
	SubjectGroup         = "group"
	SubjectUser          = "user"
)

// Permission levels (ordered: read < comment < write).
const (
	PermRead    = "read"
	PermComment = "comment"
	PermWrite   = "write"
)

// rank maps a permission to a numeric level so higher levels imply lower.
func rank(p string) int {
	switch p {
	case PermRead:
		return 1
	case PermComment:
		return 2
	case PermWrite:
		return 3
	default:
		return 0
	}
}

var (
	ErrNotFound  = errors.New("permission rule not found")
	ErrInvalid   = errors.New("invalid permission rule")
	ErrDuplicate = errors.New("permission rule already exists")
)

type Rule struct {
	ID           int64  `json:"id"`
	PathGlob     string `json:"path_glob"`
	SubjectType  string `json:"subject_type"`
	SubjectValue string `json:"subject_value"`
	Permission   string `json:"permission"`
	CreatedAt    string `json:"created_at"`
}

type Store struct {
	DB *sql.DB
}

func New(db *sql.DB) *Store { return &Store{DB: db} }

func validSubject(t string) bool {
	switch t {
	case SubjectAnonymous, SubjectAuthenticated, SubjectRole, SubjectGroup, SubjectUser:
		return true
	default:
		return false
	}
}

func validPermission(p string) bool { return rank(p) > 0 }

func (s *Store) Create(r Rule) (*Rule, error) {
	r.PathGlob = strings.TrimSpace(r.PathGlob)
	r.SubjectType = strings.TrimSpace(r.SubjectType)
	r.SubjectValue = strings.TrimSpace(r.SubjectValue)
	r.Permission = strings.TrimSpace(r.Permission)

	if r.PathGlob == "" || !validSubject(r.SubjectType) || !validPermission(r.Permission) {
		return nil, ErrInvalid
	}
	// Anonymous/authenticated don't carry a value — normalise to ''.
	if r.SubjectType == SubjectAnonymous || r.SubjectType == SubjectAuthenticated {
		r.SubjectValue = ""
	} else if r.SubjectValue == "" {
		return nil, ErrInvalid
	}
	if _, err := doublestar.Match(r.PathGlob, ""); err != nil {
		return nil, fmt.Errorf("%w: bad glob: %v", ErrInvalid, err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.DB.Exec(`
		INSERT INTO permissions(path_glob, subject_type, subject_value, permission, created_at)
		VALUES(?, ?, ?, ?, ?)`,
		r.PathGlob, r.SubjectType, r.SubjectValue, r.Permission, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrDuplicate
		}
		return nil, fmt.Errorf("insert permission: %w", err)
	}
	id, _ := res.LastInsertId()
	return s.GetByID(id)
}

func (s *Store) GetByID(id int64) (*Rule, error) {
	row := s.DB.QueryRow(`
		SELECT id, path_glob, subject_type, subject_value, permission, created_at
		FROM permissions WHERE id = ?`,
		id,
	)
	return scan(row)
}

func (s *Store) List() ([]*Rule, error) {
	rows, err := s.DB.Query(`
		SELECT id, path_glob, subject_type, subject_value, permission, created_at
		FROM permissions
		ORDER BY path_glob ASC, subject_type ASC, subject_value ASC, permission ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Rule{}
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) Delete(id int64) error {
	res, err := s.DB.Exec(`DELETE FROM permissions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Effective returns the subset of rules whose glob matches the given path.
// Useful for the admin "explain why" tool.
func (s *Store) Effective(path string) ([]*Rule, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	out := make([]*Rule, 0, len(all))
	for _, r := range all {
		if ok, _ := doublestar.Match(r.PathGlob, path); ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// Decision describes the outcome of an authorisation check.
type Decision struct {
	// MatchedAnyRule is true when at least one rule's glob matched the path.
	// When false, callers should fall through to defaults (anon-read flag,
	// RequireAuth on writes).
	MatchedAnyRule bool
	// MaxGranted is the highest permission level a subject (or anyone) was
	// granted on this path among matching rules.
	MaxGranted string
	// AllowsUser is the highest permission level the given user is granted.
	// Empty string means "no permission".
	AllowsUser string
}

// Check evaluates whether the given user has at least `required` permission
// on `path`. It returns a Decision so callers can distinguish "no rules at
// all → fall back to defaults" from "rules apply but user not granted".
func (s *Store) Check(path string, user *users.User, required string) (Decision, error) {
	if !validPermission(required) {
		return Decision{}, ErrInvalid
	}

	rules, err := s.Effective(path)
	if err != nil {
		return Decision{}, err
	}
	if len(rules) == 0 {
		return Decision{MatchedAnyRule: false}, nil
	}

	bestAny := ""
	bestUser := ""
	for _, r := range rules {
		if rank(r.Permission) > rank(bestAny) {
			bestAny = r.Permission
		}
		if subjectMatches(r, user) {
			if rank(r.Permission) > rank(bestUser) {
				bestUser = r.Permission
			}
		}
	}
	return Decision{
		MatchedAnyRule: true,
		MaxGranted:     bestAny,
		AllowsUser:     bestUser,
	}, nil
}

// Allows is a convenience wrapper: returns true when the user is allowed
// `required` permission on `path` under the matched rules. Pass mustFallback
// to know whether the caller should consult defaults instead.
func (s *Store) Allows(path string, user *users.User, required string) (allowed, mustFallback bool, err error) {
	d, err := s.Check(path, user, required)
	if err != nil {
		return false, false, err
	}
	if !d.MatchedAnyRule {
		return false, true, nil
	}
	return rank(d.AllowsUser) >= rank(required), false, nil
}

// subjectMatches checks whether a user (possibly nil) satisfies the rule's
// subject. The implicit subjects:
//   - anonymous     → always matches (including signed-in users)
//   - authenticated → matches when user is not nil
//   - role          → user.Role == subject_value
//   - group         → subject_value in user.Groups
//   - user          → strconv(user.ID) == subject_value
func subjectMatches(r *Rule, user *users.User) bool {
	switch r.SubjectType {
	case SubjectAnonymous:
		return true
	case SubjectAuthenticated:
		return user != nil
	case SubjectRole:
		return user != nil && user.Role == r.SubjectValue
	case SubjectGroup:
		if user == nil {
			return false
		}
		for _, g := range user.Groups {
			if g == r.SubjectValue {
				return true
			}
		}
		return false
	case SubjectUser:
		if user == nil {
			return false
		}
		return strconv.FormatInt(user.ID, 10) == r.SubjectValue
	default:
		return false
	}
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scan(row rowScanner) (*Rule, error) {
	var r Rule
	if err := row.Scan(&r.ID, &r.PathGlob, &r.SubjectType, &r.SubjectValue, &r.Permission, &r.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &r, nil
}
