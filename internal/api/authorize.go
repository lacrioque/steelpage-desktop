package api

import (
	"github.com/markusfluer/steelpage-desktop/internal/localidentity"
)

// currentUser returns the single local identity. The desktop build has no
// auth layer — every request acts as the OS user (id=1, seeded in
// migration 001 so the comments FK holds).
func (a *API) currentUser() localidentity.Identity {
	return localidentity.Default()
}
