package account

import "errors"

// Errors returned by the service; the API layer maps them to GraphQL error codes.
var (
	ErrSetupRequired      = errors.New("setup required")
	ErrAlreadySetUp       = errors.New("already set up")
	ErrInvalidSetupCode   = errors.New("invalid setup code")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrRateLimited        = errors.New("too many attempts")
	ErrForbidden          = errors.New("forbidden")
	ErrInviteInvalid      = errors.New("invite is invalid or expired")
	ErrUnauthenticated    = errors.New("authentication required")
)

// InputError reports an invalid input field.
type InputError struct {
	Field string
	Msg   string
}

func (e *InputError) Error() string { return e.Field + ": " + e.Msg }

func inputErr(field, msg string) error { return &InputError{Field: field, Msg: msg} }
