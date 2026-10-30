package account

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/auth"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/store/q"
)

// APIKey describes a personal API key; the secret itself is never stored.
type APIKey struct {
	ID            uuid.UUID
	Name          string
	DisplayPrefix string
	CreatedAt     time.Time
	LastUsedAt    *time.Time
}

func toAPIKey(k q.ApiKey) APIKey {
	return APIKey{ID: k.ID, Name: k.Name, DisplayPrefix: k.DisplayPrefix, CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt}
}

// CreateAPIKey issues a key for actor; the plain key is returned only here.
func (s *Service) CreateAPIKey(ctx context.Context, actor User, name string) (APIKey, string, error) {
	name, err := validateText("name", name)
	if err != nil {
		return APIKey{}, "", err
	}
	plain, hash, prefix := auth.NewAPIKey()
	row, err := s.q.CreateAPIKey(ctx, q.CreateAPIKeyParams{UserID: actor.ID, Name: name, KeyHash: hash, DisplayPrefix: prefix})
	if err != nil {
		return APIKey{}, "", fmt.Errorf("create api key: %w", err)
	}
	return toAPIKey(row), plain, nil
}

// ListAPIKeys returns actor's active keys.
func (s *Service) ListAPIKeys(ctx context.Context, actor User) ([]APIKey, error) {
	rows, err := s.q.ListAPIKeys(ctx, actor.ID)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	out := make([]APIKey, 0, len(rows))
	for _, r := range rows {
		out = append(out, toAPIKey(r))
	}
	return out, nil
}

// RevokeAPIKey revokes one of actor's keys; the Owner may revoke anyone's.
func (s *Service) RevokeAPIKey(ctx context.Context, actor User, id uuid.UUID) error {
	k, err := s.q.GetAPIKey(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return kit.Input("id", kit.ReasonNotFound, "no such API key")
	}
	if err != nil {
		return fmt.Errorf("find api key: %w", err)
	}
	if k.UserID != actor.ID && actor.Role != RoleOwner {
		return ErrForbidden
	}
	if _, err := s.q.RevokeAPIKey(ctx, id); err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	return nil
}

// UserByAPIKey resolves an active key of an enabled user and records its use
// (at most once a minute).
func (s *Service) UserByAPIKey(ctx context.Context, plain string) (User, error) {
	if plain == "" {
		return User{}, ErrUnauthenticated
	}
	row, err := s.q.GetAPIKeyUser(ctx, auth.HashSecret(plain))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUnauthenticated
	}
	if err != nil {
		return User{}, fmt.Errorf("find api key: %w", err)
	}
	if err := s.q.TouchAPIKey(ctx, row.ApiKey.ID); err != nil {
		return User{}, fmt.Errorf("touch api key: %w", err)
	}
	return toUser(row.User), nil
}
