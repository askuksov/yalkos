// Package linear serves the Linear-compatible GraphQL API (/graphql).
package linear

import (
	"net/http"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/transport"
)

// NewHandler serves the linear GraphQL schema over POST only, with introspection enabled.
func NewHandler() http.Handler {
	srv := handler.New(NewExecutableSchema(Config{Resolvers: &Resolver{}}))
	srv.AddTransport(transport.POST{})
	srv.Use(extension.Introspection{})
	return srv
}
