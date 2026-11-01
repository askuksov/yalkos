// Package linear serves the Linear-compatible GraphQL API (/graphql).
package linear

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/transport"
)

// NewHandler serves the schema over POST only, with introspection enabled,
// the query cost limit and per-request loaders.
func NewHandler(d Deps) http.Handler {
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	es := NewExecutableSchema(Config{Resolvers: &Resolver{d: d}})
	srv := handler.New(es)
	srv.AddTransport(transport.POST{})
	srv.Use(extension.Introspection{})
	srv.Use(costLimit{})
	srv.SetErrorPresenter(presenter(es.Schema(), func(_ context.Context, err error) {
		logger.Error("graphql internal error", slog.String("api", "linear"), slog.String("error", err.Error()))
	}))
	return withResponse(withLoaders(d, srv))
}
