// Package native serves the Yalkos GraphQL API (/api/graphql) used by the web UI.
package native

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

// NewHandler serves the native schema over POST only, with introspection enabled.
func NewHandler(accounts *account.Service, logger *slog.Logger) http.Handler {
	srv := handler.New(NewExecutableSchema(Config{Resolvers: &Resolver{Accounts: accounts, Logger: logger}}))
	srv.AddTransport(transport.POST{})
	srv.Use(extension.Introspection{})
	srv.SetErrorPresenter(gqlerr.Presenter(func(_ context.Context, err error) {
		logger.Error("graphql internal error", slog.String("api", "native"), slog.String("error", err.Error()))
	}))
	return srv
}
