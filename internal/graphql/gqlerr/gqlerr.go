// Package gqlerr builds GraphQL errors in the shape Linear's API uses.
package gqlerr

import (
	"context"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// Unauthenticated is returned when a request has no valid credentials.
func Unauthenticated(ctx context.Context) *gqlerror.Error {
	return &gqlerror.Error{
		Path:    graphql.GetPath(ctx),
		Message: "Authentication required",
		Extensions: map[string]any{
			"code":      "AUTHENTICATION_ERROR",
			"type":      "authentication error",
			"userError": true,
		},
	}
}
