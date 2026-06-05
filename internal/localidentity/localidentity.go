// Package localidentity provides the single local user identity for the
// desktop build. There is no auth layer: every request acts as this user.
// The ID is always 1 and matches the row seeded by migration 001 so the
// comments.author_id foreign key stays valid.
package localidentity

import "os"

type Identity struct {
	ID          int64
	DisplayName string
	Email       string
}

// Default derives the identity from the OS user. Falls back to "You" when
// no username is available (e.g. stripped-down environments).
func Default() Identity {
	name := os.Getenv("USER")
	if name == "" {
		name = os.Getenv("USERNAME") // Windows
	}
	if name == "" {
		name = "You"
	}
	return Identity{ID: 1, DisplayName: name, Email: name + "@localhost"}
}
