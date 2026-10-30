// Package gqlerr builds GraphQL errors in the shape Linear's API uses
// (extensions.code / type / userError) for both Yalkos endpoints.
package gqlerr

import (
	"context"
	"errors"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/kit"
)

// CodeInternal marks errors whose details must stay in server logs.
const CodeInternal = "INTERNAL_ERROR"

func newErr(ctx context.Context, msg, code, typ string, user bool) *gqlerror.Error {
	return &gqlerror.Error{
		Path:    graphql.GetPath(ctx),
		Message: msg,
		Extensions: map[string]any{
			"code":      code,
			"type":      typ,
			"userError": user,
		},
	}
}

// Unauthenticated is returned when a request has no valid credentials.
func Unauthenticated(ctx context.Context) *gqlerror.Error {
	return newErr(ctx, "Authentication required", "AUTHENTICATION_ERROR", "authentication error", true)
}

// From maps a domain error to a GraphQL error. Unknown errors become
// INTERNAL_ERROR with a generic message.
func From(ctx context.Context, err error) *gqlerror.Error {
	var ie *kit.InputError
	var nf kit.NotFoundError
	switch {
	case errors.As(err, &ie):
		e := newErr(ctx, ie.Msg, "INVALID_INPUT", "invalid input", true)
		e.Extensions["field"] = ie.Field
		e.Extensions["reason"] = string(ie.Reason)
		if len(ie.Params) > 0 {
			e.Extensions["params"] = ie.Params
		}
		return e
	case errors.As(err, &nf):
		return newErr(ctx, nf.Error(), "NOT_FOUND", "not found", true)
	case errors.Is(err, account.ErrUnauthenticated):
		return Unauthenticated(ctx)
	case errors.Is(err, account.ErrInvalidCredentials):
		return newErr(ctx, "Invalid email or password", "AUTHENTICATION_ERROR", "authentication error", true)
	case errors.Is(err, account.ErrInvalidSetupCode):
		e := newErr(ctx, "Invalid setup code", "INVALID_INPUT", "invalid input", true)
		e.Extensions["field"] = "code"
		e.Extensions["reason"] = string(kit.ReasonMismatch)
		return e
	case errors.Is(err, account.ErrSessionRequired):
		return newErr(ctx, "This action needs a signed-in session; API keys cannot manage keys, passwords or invites", "FORBIDDEN", "forbidden", true)
	case errors.Is(err, kit.ErrForbidden):
		return newErr(ctx, "You do not have permission to do this", "FORBIDDEN", "forbidden", true)
	case errors.Is(err, account.ErrRateLimited):
		return newErr(ctx, "Too many attempts. Try again in a few minutes.", "RATE_LIMITED", "ratelimited", true)
	case errors.Is(err, account.ErrBusy):
		return newErr(ctx, "Server is busy. Try again in a moment.", "RATE_LIMITED", "ratelimited", true)
	case errors.Is(err, account.ErrSetupRequired):
		return newErr(ctx, "Setup required", "SETUP_REQUIRED", "setup required", true)
	case errors.Is(err, account.ErrAlreadySetUp):
		return newErr(ctx, "This instance is already set up", "ALREADY_SET_UP", "invalid input", true)
	case errors.Is(err, account.ErrInviteInvalid):
		return newErr(ctx, "This invite link is invalid or has expired.", "INVITE_INVALID", "invalid input", true)
	default:
		return newErr(ctx, "Internal error", CodeInternal, "internal error", false)
	}
}

// Presenter is a gqlgen error presenter: resolvers return plain errors, this
// turns them into coded GraphQL errors and logs internal ones.
func Presenter(logf func(ctx context.Context, err error)) graphql.ErrorPresenterFunc {
	return func(ctx context.Context, err error) *gqlerror.Error {
		var gerr *gqlerror.Error
		if errors.As(err, &gerr) && gerr.Err == nil {
			// Already a GraphQL error (parse/validation or built by us).
			return gerr
		}
		cause := err
		if gerr != nil && gerr.Err != nil {
			cause = gerr.Err
		}
		out := From(ctx, cause)
		if f, ok := out.Extensions["field"].(string); ok && f == "" && gerr != nil {
			// Scalar errors (e.g. a bad Date) know no field: take it from the input path.
			out.Extensions["field"] = lastName(gerr.Path)
		}
		if out.Extensions["code"] == CodeInternal {
			logf(ctx, cause)
		}
		return out
	}
}

// lastName returns the last field name in path, or "" if it has none.
func lastName(path ast.Path) string {
	for i := len(path) - 1; i >= 0; i-- {
		if n, ok := path[i].(ast.PathName); ok {
			return string(n)
		}
	}
	return ""
}
