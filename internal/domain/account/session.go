package account

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/auth"
	"github.com/askuksov/yalkos/internal/store/q"
)

const setupCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// SetupNeeded reports whether the instance has no workspace yet.
func (s *Service) SetupNeeded(ctx context.Context) (bool, error) {
	n, err := s.q.CountWorkspaces(ctx)
	if err != nil {
		return false, fmt.Errorf("count workspaces: %w", err)
	}
	return n == 0, nil
}

// RefreshSetupCode creates a new one-time setup code if setup is needed, or clears it.
func (s *Service) RefreshSetupCode(ctx context.Context) error {
	needed, err := s.SetupNeeded(ctx)
	if err != nil {
		return err
	}
	code := ""
	if needed {
		code = newSetupCode()
	}
	s.mu.Lock()
	s.setupCode = code
	s.mu.Unlock()
	return nil
}

// SetupCode returns the current setup code ("" when the instance is set up).
func (s *Service) SetupCode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setupCode
}

func newSetupCode() string {
	var sb strings.Builder
	buf := make([]byte, 1)
	n := byte(len(setupCodeAlphabet))
	limit := 256 - 256%int(n)
	for sb.Len() < 8 {
		_, _ = rand.Read(buf)
		if int(buf[0]) < limit {
			sb.WriteByte(setupCodeAlphabet[buf[0]%n])
		}
	}
	return sb.String()
}

// SetupInput is the first-run form.
type SetupInput struct {
	Code, WorkspaceName, Name, Email, Password string
}

// Setup creates the workspace and its Owner and signs the Owner in.
func (s *Service) Setup(ctx context.Context, in SetupInput, ip string) (User, string, error) {
	if s.setupFails.Blocked(ip) {
		return User{}, "", ErrRateLimited
	}
	code := s.SetupCode()
	if code == "" {
		return User{}, "", ErrAlreadySetUp
	}
	if subtle.ConstantTimeCompare([]byte(strings.ToUpper(strings.TrimSpace(in.Code))), []byte(code)) != 1 {
		s.setupFails.Fail(ip)
		return User{}, "", ErrInvalidSetupCode
	}
	wsName, err := validateText("workspaceName", in.WorkspaceName)
	if err != nil {
		return User{}, "", err
	}
	name, err := validateText("name", in.Name)
	if err != nil {
		return User{}, "", err
	}
	email, err := validateEmail(in.Email)
	if err != nil {
		return User{}, "", err
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		return User{}, "", err
	}

	var user User
	var token string
	err = s.inTx(ctx, func(tx *q.Queries) error {
		ws, err := tx.CreateWorkspace(ctx, wsName)
		if err != nil {
			return err
		}
		u, err := tx.CreateUser(ctx, q.CreateUserParams{
			WorkspaceID: ws.ID, Email: email, Name: name, PasswordHash: hash, Role: string(RoleOwner),
		})
		if err != nil {
			return err
		}
		user = toUser(u)
		token, err = s.newSession(ctx, tx, u.ID)
		return err
	})
	if isUniqueViolation(err, "workspaces_singleton") {
		return User{}, "", ErrAlreadySetUp
	}
	if err != nil {
		return User{}, "", fmt.Errorf("setup: %w", err)
	}
	s.mu.Lock()
	s.setupCode = ""
	s.mu.Unlock()
	return user, token, nil
}

func (s *Service) newSession(ctx context.Context, tx *q.Queries, userID uuid.UUID) (string, error) {
	token, hash := auth.NewToken()
	if err := tx.CreateSession(ctx, q.CreateSessionParams{
		TokenHash: hash, UserID: userID, ExpiresAt: s.now().Add(sessionTTL),
	}); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return token, nil
}

// Login checks email + password and opens a new session.
func (s *Service) Login(ctx context.Context, email, password, ip string) (User, string, error) {
	key := strings.ToLower(strings.TrimSpace(email)) + "|" + ip
	if s.loginFails.Blocked(key) {
		return User{}, "", ErrRateLimited
	}
	if strings.TrimSpace(email) == "" {
		return User{}, "", inputErr("email", "enter your email")
	}
	u, err := s.q.GetUserByEmail(ctx, strings.TrimSpace(email))
	if errors.Is(err, pgx.ErrNoRows) {
		auth.DummyVerify()
		s.loginFails.Fail(key)
		return User{}, "", ErrInvalidCredentials
	}
	if err != nil {
		return User{}, "", fmt.Errorf("find user: %w", err)
	}
	if !auth.VerifyPassword(password, u.PasswordHash) || u.Disabled {
		s.loginFails.Fail(key)
		return User{}, "", ErrInvalidCredentials
	}
	token, err := s.newSession(ctx, s.q, u.ID)
	if err != nil {
		return User{}, "", err
	}
	return toUser(u), token, nil
}

// Logout deletes the session.
func (s *Service) Logout(ctx context.Context, sessionToken string) error {
	if err := s.q.DeleteSession(ctx, auth.HashSecret(sessionToken)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// UserBySession resolves a live session of an enabled user.
func (s *Service) UserBySession(ctx context.Context, sessionToken string) (User, error) {
	if sessionToken == "" {
		return User{}, ErrUnauthenticated
	}
	u, err := s.q.GetSessionUser(ctx, q.GetSessionUserParams{TokenHash: auth.HashSecret(sessionToken), Now: s.now()})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUnauthenticated
	}
	if err != nil {
		return User{}, fmt.Errorf("find session: %w", err)
	}
	return toUser(u), nil
}

// ChangePassword verifies the current password, stores the new one and ends
// every other session of the user.
func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, current, next, keepSessionToken string) error {
	u, err := s.q.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("find user: %w", err)
	}
	if !auth.VerifyPassword(current, u.PasswordHash) {
		return ErrInvalidCredentials
	}
	hash, err := hashPassword(next)
	if err != nil {
		return err
	}
	return s.inTx(ctx, func(tx *q.Queries) error {
		if err := tx.UpdateUserPassword(ctx, q.UpdateUserPasswordParams{ID: userID, PasswordHash: hash}); err != nil {
			return err
		}
		return tx.DeleteUserSessionsExcept(ctx, q.DeleteUserSessionsExceptParams{UserID: userID, Keep: auth.HashSecret(keepSessionToken)})
	})
}
