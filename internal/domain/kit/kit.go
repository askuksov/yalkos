// Package kit holds small types shared by all domain packages: errors the API
// layer maps to GraphQL codes, Opt for patch inputs and the transaction runner.
package kit

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/store/q"
)

// Shared sentinel errors.
var (
	ErrForbidden = errors.New("forbidden")
	ErrNotFound  = errors.New("not found")
)

// ErrStartOver ends a transaction that must run again from the start: rows it
// locked changed while it waited for them (an issue moved to another project,
// a label to another group), and locking the new rows while it still holds
// the old ones could deadlock with a writer that locks them in order.
var ErrStartOver = errors.New("changed concurrently, try again")

// attempts bounds how often InTx runs a transaction that ends with ErrStartOver.
const attempts = 3

// InTx runs fn in a transaction on pool with base bound to it. A run that
// ends with ErrStartOver is rolled back, releasing its locks, and runs again,
// up to three times in all.
func InTx(ctx context.Context, pool *pgxpool.Pool, base *q.Queries, fn func(*q.Queries) error) error {
	var err error
	for range attempts {
		err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return fn(base.WithTx(tx)) })
		if !errors.Is(err, ErrStartOver) {
			return err
		}
	}
	return err
}

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
