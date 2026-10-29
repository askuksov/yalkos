package account

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/auth"
	"github.com/askuksov/yalkos/internal/store/q"
)

// Invite is a pending invitation. URL is set only in the CreateInvite result.
type Invite struct {
	ID        uuid.UUID
	Role      Role
	CreatedAt time.Time
	ExpiresAt time.Time
	URL       string
}

func toInvite(i q.Invite) Invite {
	return Invite{ID: i.ID, Role: Role(i.Role), CreatedAt: i.CreatedAt, ExpiresAt: i.ExpiresAt}
}

// CreateInvite makes a single-use invite link valid for days (1–30).
// Owner and Admin may invite members; only the Owner may invite admins.
func (s *Service) CreateInvite(ctx context.Context, actor User, role Role, days int) (Invite, error) {
	switch role {
	case RoleMember:
		if !actor.IsAdmin() {
			return Invite{}, ErrForbidden
		}
	case RoleAdmin:
		if actor.Role != RoleOwner {
			return Invite{}, ErrForbidden
		}
	default:
		return Invite{}, inputErr("role", "must be admin or member")
	}
	if days < 1 || days > 30 {
		return Invite{}, inputErr("expiresInDays", "must be 1–30")
	}
	token, hash := auth.NewToken()
	row, err := s.q.CreateInvite(ctx, q.CreateInviteParams{
		TokenHash: hash, Role: string(role), CreatedBy: actor.ID,
		ExpiresAt: s.now().Add(time.Duration(days) * 24 * time.Hour),
	})
	if err != nil {
		return Invite{}, fmt.Errorf("create invite: %w", err)
	}
	inv := toInvite(row)
	inv.URL = s.baseURL + "/invite/" + token
	return inv, nil
}

// ListInvites returns active invites (Owner and Admin only).
func (s *Service) ListInvites(ctx context.Context, actor User) ([]Invite, error) {
	if !actor.IsAdmin() {
		return nil, ErrForbidden
	}
	rows, err := s.q.ListActiveInvites(ctx, s.now())
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	out := make([]Invite, 0, len(rows))
	for _, r := range rows {
		out = append(out, toInvite(r))
	}
	return out, nil
}

// RevokeInvite cancels an unused invite (Owner and Admin only).
func (s *Service) RevokeInvite(ctx context.Context, actor User, id uuid.UUID) error {
	if !actor.IsAdmin() {
		return ErrForbidden
	}
	if _, err := s.q.RevokeInvite(ctx, id); err != nil {
		return fmt.Errorf("revoke invite: %w", err)
	}
	return nil
}

// AcceptInvite creates the invited user and signs them in.
// Unknown, used, revoked and expired invites all return ErrInviteInvalid.
func (s *Service) AcceptInvite(ctx context.Context, token, name, email, password string) (User, string, error) {
	name, err := validateText("name", name)
	if err != nil {
		return User{}, "", err
	}
	email, err = validateEmail(email)
	if err != nil {
		return User{}, "", err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return User{}, "", err
	}

	var user User
	var session string
	err = s.inTx(ctx, func(tx *q.Queries) error {
		inv, err := tx.GetInviteForUpdate(ctx, auth.HashSecret(token))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInviteInvalid
		}
		if err != nil {
			return err
		}
		if inv.UsedAt != nil || inv.RevokedAt != nil || !inv.ExpiresAt.After(s.now()) {
			return ErrInviteInvalid
		}
		ws, err := tx.GetWorkspace(ctx)
		if err != nil {
			return err
		}
		u, err := tx.CreateUser(ctx, q.CreateUserParams{
			WorkspaceID: ws.ID, Email: email, Name: name, PasswordHash: hash, Role: inv.Role,
		})
		if isUniqueViolation(err, "users_email_lower") {
			return inputErr("email", "this email is already used")
		}
		if err != nil {
			return err
		}
		if err := tx.MarkInviteUsed(ctx, q.MarkInviteUsedParams{ID: inv.ID, UsedBy: &u.ID, Now: s.now()}); err != nil {
			return err
		}
		user = toUser(u)
		session, err = s.newSession(ctx, tx, u.ID)
		return err
	})
	var ie *InputError
	if errors.Is(err, ErrInviteInvalid) || errors.As(err, &ie) {
		return User{}, "", err
	}
	if err != nil {
		return User{}, "", fmt.Errorf("accept invite: %w", err)
	}
	return user, session, nil
}
