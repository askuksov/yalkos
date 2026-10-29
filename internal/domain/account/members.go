package account

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/store/q"
)

// ListMembers returns every user of the workspace (visible to all members).
func (s *Service) ListMembers(ctx context.Context, _ User) ([]User, error) {
	rows, err := s.q.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	out := make([]User, 0, len(rows))
	for _, r := range rows {
		out = append(out, toUser(r))
	}
	return out, nil
}

func (s *Service) target(ctx context.Context, id uuid.UUID) (q.User, error) {
	u, err := s.q.GetUserByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return q.User{}, inputErr("userId", "no such user")
	}
	if err != nil {
		return q.User{}, fmt.Errorf("find user: %w", err)
	}
	return u, nil
}

// UpdateMemberRole switches a user between admin and member. Owner only, not on self.
func (s *Service) UpdateMemberRole(ctx context.Context, actor User, userID uuid.UUID, role Role) error {
	if actor.Role != RoleOwner || userID == actor.ID {
		return ErrForbidden
	}
	if role != RoleAdmin && role != RoleMember {
		return inputErr("role", "must be admin or member")
	}
	t, err := s.target(ctx, userID)
	if err != nil {
		return err
	}
	if Role(t.Role) == RoleOwner {
		return ErrForbidden
	}
	if _, err := s.q.UpdateUserRole(ctx, q.UpdateUserRoleParams{ID: userID, Role: string(role)}); err != nil {
		return fmt.Errorf("update role: %w", err)
	}
	return nil
}

// SetMemberDisabled disables or re-enables a user (Owner and Admin; never the
// Owner, never oneself). Disabling ends the user's sessions; their API keys stop
// working because key lookup skips disabled users.
func (s *Service) SetMemberDisabled(ctx context.Context, actor User, userID uuid.UUID, disabled bool) error {
	if !actor.IsAdmin() || userID == actor.ID {
		return ErrForbidden
	}
	t, err := s.target(ctx, userID)
	if err != nil {
		return err
	}
	if Role(t.Role) == RoleOwner {
		return ErrForbidden
	}
	return s.inTx(ctx, func(tx *q.Queries) error {
		if _, err := tx.SetUserDisabled(ctx, q.SetUserDisabledParams{ID: userID, Disabled: disabled}); err != nil {
			return err
		}
		if disabled {
			return tx.DeleteUserSessions(ctx, userID)
		}
		return nil
	})
}
