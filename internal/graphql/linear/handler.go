// Package linear serves the Linear-compatible GraphQL API (/graphql).
package linear

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/transport"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/graphql/gqlerr"
)

const maxComplexity = 200

// NewHandler serves the linear GraphQL schema over POST only, with introspection enabled.
func NewHandler(accounts *account.Service, logger *slog.Logger) http.Handler {
	srv := handler.New(NewExecutableSchema(Config{Resolvers: &Resolver{Accounts: accounts, Logger: logger}}))
	srv.AddTransport(transport.POST{})
	srv.Use(extension.Introspection{})
	// Caps aliases/fields per request (no batching thousands of logins in one body).
	srv.Use(extension.FixedComplexityLimit(maxComplexity))
	srv.SetErrorPresenter(gqlerr.Presenter(func(_ context.Context, err error) {
		logger.Error("graphql internal error", slog.String("api", "linear"), slog.String("error", err.Error()))
	}))
	return srv
}
