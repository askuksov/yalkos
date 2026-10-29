package native

// This file will not be regenerated automatically.

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/authn"
	"github.com/askuksov/yalkos/internal/domain/account"
)

// Resolver is the root resolver.
type Resolver struct {
	Accounts *account.Service
	Logger   *slog.Logger
}

func actor(ctx context.Context) (*authn.Actor, error) {
	a, ok := authn.ActorFrom(ctx)
	if !ok {
		return nil, account.ErrUnauthenticated
	}
	return a, nil
}

// sessionActor is actor() restricted to browser sessions: API keys cannot manage
// credentials (keys, password, invites), so a leaked key cannot entrench itself.
func sessionActor(ctx context.Context) (*authn.Actor, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if a.Via != "session" {
		return nil, account.ErrSessionRequired
	}
	return a, nil
}

func parseID(field, id string) (uuid.UUID, error) {
	u, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, &account.InputError{Field: field, Msg: "invalid ID"}
	}
	return u, nil
}

func roleOut(r account.Role) Role { return Role(strings.ToUpper(string(r))) }
func roleIn(r Role) account.Role  { return account.Role(strings.ToLower(string(r))) }

func userOut(u account.User) *User {
	return &User{ID: u.ID.String(), Name: u.Name, Email: u.Email, Role: roleOut(u.Role), Disabled: u.Disabled, CreatedAt: u.CreatedAt}
}

func inviteOut(i account.Invite) *Invite {
	return &Invite{ID: i.ID.String(), Role: roleOut(i.Role), CreatedAt: i.CreatedAt, ExpiresAt: i.ExpiresAt}
}

func apiKeyOut(k account.APIKey) *APIKey {
	return &APIKey{ID: k.ID.String(), Name: k.Name, DisplayPrefix: k.DisplayPrefix, CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt}
}
