// Package kit holds small types shared by all domain packages: errors the API
// layer maps to GraphQL codes, and Opt for patch inputs.
package kit

import "errors"

// Shared sentinel errors.
var (
	ErrForbidden = errors.New("forbidden")
	ErrNotFound  = errors.New("not found")
)

// Reason is a machine-readable cause of an InputError; the UI translates it.
type Reason string

// Input error reasons.
const (
	ReasonRequired      Reason = "required"
	ReasonTooShort      Reason = "too_short"
	ReasonTooLong       Reason = "too_long"
	ReasonInvalidFormat Reason = "invalid_format"
	ReasonOutOfRange    Reason = "out_of_range"
	ReasonTaken         Reason = "taken"
	ReasonNotFound      Reason = "not_found"
	ReasonWrongProject  Reason = "wrong_project"
	ReasonCycle         Reason = "cycle"
	ReasonArchived      Reason = "archived"
	ReasonNotAllowed    Reason = "not_allowed"
	ReasonMismatch      Reason = "mismatch"
	ReasonTooMany       Reason = "too_many"
	ReasonTooLarge      Reason = "too_large"
)

// InputError reports an invalid input field. Msg is an English fallback;
// clients translate by Reason and Params.
type InputError struct {
	Field  string
	Reason Reason
	Msg    string
	Params map[string]any
}

func (e *InputError) Error() string { return e.Field + ": " + e.Msg }

// Input builds an InputError.
func Input(field string, reason Reason, msg string) *InputError {
	return &InputError{Field: field, Reason: reason, Msg: msg}
}

// With adds a param (e.g. "max": 255) and returns e.
func (e *InputError) With(key string, v any) *InputError {
	if e.Params == nil {
		e.Params = map[string]any{}
	}
	e.Params[key] = v
	return e
}

// NotFoundError reports a missing entity, e.g. NotFoundError{"Issue"}.
type NotFoundError struct{ Entity string }

func (e NotFoundError) Error() string { return e.Entity + " not found" }

// Is makes errors.Is(err, ErrNotFound) true.
func (e NotFoundError) Is(target error) bool { return target == ErrNotFound }

// Opt is a patch field: Set=false keeps the value, Set with nil Value clears it.
type Opt[T any] struct {
	Set   bool
	Value *T
}
