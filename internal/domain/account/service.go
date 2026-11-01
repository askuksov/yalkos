// Package account implements workspace setup, users, sessions, invites and API keys.
package account

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/auth"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/store/q"
)

// Role is a workspace role.
type Role string

// Roles.
const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// User is a workspace member as seen by the rest of the app (no password hash).
type User struct {
	ID        uuid.UUID
	Email     string
	Name      string
	Role      Role
	Disabled  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsAdmin reports Owner or Admin.
func (u User) IsAdmin() bool { return u.Role == RoleOwner || u.Role == RoleAdmin }

const sessionTTL = 30 * 24 * time.Hour

// Service holds account business rules.
type Service struct {
	pool    *pgxpool.Pool
	q       *q.Queries
	now     func() time.Time
	baseURL string

	mu        sync.Mutex
	setupCode string

	// Login: 10 failures per (email, client network) per 15 min, and 100 per email per hour.
	loginPair  *attemptLimiter
	loginEmail *attemptLimiter
	// Setup code: 10 failures per client network per 15 min, 100 in total per hour.
	setupFails  *attemptLimiter
	setupGlobal *attemptLimiter
	// Current-password check in ChangePassword: 10 failures per user per 15 min.
	passwordFails *attemptLimiter
}

// New builds the service; now is the clock (time.Now in production),
// baseURL is the public URL used in invite links.
func New(pool *pgxpool.Pool, now func() time.Time, baseURL string) *Service {
	return &Service{
		pool:          pool,
		q:             q.New(pool),
		now:           now,
		baseURL:       strings.TrimRight(baseURL, "/"),
		loginPair:     newAttemptLimiter(10, 15*time.Minute),
		loginEmail:    newAttemptLimiter(100, time.Hour),
		setupFails:    newAttemptLimiter(10, 15*time.Minute),
		setupGlobal:   newAttemptLimiter(100, time.Hour),
		passwordFails: newAttemptLimiter(10, 15*time.Minute),
	}
}

func toUser(u q.User) User {
	return User{ID: u.ID, Email: u.Email, Name: u.Name, Role: Role(u.Role), Disabled: u.Disabled, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt}
}

// Workspace is the instance's one workspace.
type Workspace struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
}

// Workspace reads the workspace.
func (s *Service) Workspace(ctx context.Context) (Workspace, error) {
	w, err := s.q.GetWorkspace(ctx)
	if err != nil {
		return Workspace{}, fmt.Errorf("get workspace: %w", err)
	}
	return Workspace{ID: w.ID, Name: w.Name, CreatedAt: w.CreatedAt}, nil
}

// inTx runs fn in a transaction.
func (s *Service) inTx(ctx context.Context, fn func(*q.Queries) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return fn(s.q.WithTx(tx)) })
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && (constraint == "" || pgErr.ConstraintName == constraint)
}

func validateEmail(email string) (string, error) {
	email = strings.TrimSpace(email)
	if email == "" || len(email) > 254 || !strings.Contains(email, "@") {
		return "", kit.Input("email", kit.ReasonInvalidFormat, "enter a valid email address")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return "", kit.Input("email", kit.ReasonInvalidFormat, "enter a valid email address")
	}
	return email, nil
}

func validateText(field, v string) (string, error) {
	v = strings.TrimSpace(v)
	switch n := utf8.RuneCountInString(v); {
	case n < 1:
		return "", kit.Input(field, kit.ReasonRequired, "must be 1–100 characters")
	case n > 100:
		return "", kit.Input(field, kit.ReasonTooLong, "must be 1–100 characters").With("max", 100)
	}
	return v, nil
}

func hashPassword(ctx context.Context, pw string) (string, error) {
	h, err := auth.HashPassword(ctx, pw)
	if errors.Is(err, auth.ErrPasswordLength) {
		if utf8.RuneCountInString(pw) < auth.MinPasswordLen {
			return "", kit.Input("password", kit.ReasonTooShort, auth.ErrPasswordLength.Error()).With("min", auth.MinPasswordLen)
		}
		return "", kit.Input("password", kit.ReasonTooLong, auth.ErrPasswordLength.Error()).With("max", auth.MaxPasswordLen)
	}
	if err != nil {
		return "", hashErr(err)
	}
	return h, nil
}

// verifyPassword wraps auth.VerifyPassword, mapping overload to ErrBusy.
func verifyPassword(ctx context.Context, pw, phc string) (bool, error) {
	ok, err := auth.VerifyPassword(ctx, pw, phc)
	if err != nil {
		return false, hashErr(err)
	}
	return ok, nil
}

func hashErr(err error) error {
	if errors.Is(err, auth.ErrBusy) {
		return ErrBusy
	}
	return fmt.Errorf("password hashing: %w", err)
}
