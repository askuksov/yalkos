package account

import (
	"errors"

	"github.com/askuksov/yalkos/internal/domain/kit"
)

// Errors returned by the service; the API layer maps them to GraphQL error codes.
var (
	ErrSetupRequired      = errors.New("setup required")
	ErrAlreadySetUp       = errors.New("already set up")
	ErrInvalidSetupCode   = errors.New("invalid setup code")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrRateLimited        = errors.New("too many attempts")
	ErrForbidden          = kit.ErrForbidden
	ErrInviteInvalid      = errors.New("invite is invalid or expired")
	ErrUnauthenticated    = errors.New("authentication required")
	ErrNoSuchUser         = errors.New("no such user")
	ErrBusy               = errors.New("server is busy")
	// ErrSessionRequired: credential management needs a signed-in browser session, not an API key.
	ErrSessionRequired = errors.New("signed-in session required")
)

// InputError reports an invalid input field (shared with other domains).
type InputError = kit.InputError
