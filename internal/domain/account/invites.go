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

// canInvite: Owner and Admin may invite members; only the Owner may invite admins.
func canInvite(u User, role Role) bool {
	if u.Disabled {
		return false
	}
	switch role {
	case RoleMember:
		return u.IsAdmin()
	case RoleAdmin:
		return u.Role == RoleOwner
	default:
		return false
	}
}

// CreateInvite makes a single-use invite link valid for days (1–30).
func (s *Service) CreateInvite(ctx context.Context, actor User, role Role, days int) (Invite, error) {
	if role != RoleMember && role != RoleAdmin {
		return Invite{}, inputErr("role", "must be admin or member")
	}
	if !canInvite(actor, role) {
		return Invite{}, ErrForbidden
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
	// Cheap check first, so a random token costs no password hash.
	pre, err := s.q.GetInvite(ctx, auth.HashSecret(token))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !s.inviteUsable(pre)) {
		return User{}, "", ErrInviteInvalid
	}
	if err != nil {
		return User{}, "", fmt.Errorf("find invite: %w", err)
	}
	hash, err := hashPassword(ctx, password)
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
		if !s.inviteUsable(inv) {
			return ErrInviteInvalid
		}
		// The creator must still be allowed to issue this invite.
		creator, err := tx.GetUserByID(ctx, inv.CreatedBy)
		if err != nil {
			return err
		}
		if !canInvite(toUser(creator), Role(inv.Role)) {
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
	if errors.Is(err, ErrInviteInvalid) || errors.Is(err, ErrBusy) || errors.As(err, &ie) {
		return User{}, "", err
	}
	if err != nil {
		return User{}, "", fmt.Errorf("accept invite: %w", err)
	}
	return user, session, nil
}

func (s *Service) inviteUsable(inv q.Invite) bool {
	return inv.UsedAt == nil && inv.RevokedAt == nil && inv.ExpiresAt.After(s.now())
}
