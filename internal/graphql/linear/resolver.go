package linear

// This file will not be regenerated automatically.

import (
	"log/slog"

	"github.com/askuksov/yalkos/internal/domain/account"
)

// Resolver is the root resolver of the Linear-compatible API.
type Resolver struct {
	Accounts *account.Service
	Logger   *slog.Logger
}

// userOut maps a Yalkos user to Linear's User shape (ADR-006 mapping lives here).
func userOut(u account.User) *User {
	return &User{
		ID:          u.ID.String(),
		Name:        u.Name,
		DisplayName: u.Name,
		Email:       u.Email,
		Active:      !u.Disabled,
		Admin:       u.IsAdmin(),
	}
}
