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
	"github.com/askuksov/yalkos/internal/domain/attachment"
	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/comment"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/domain/timeline"
	"github.com/askuksov/yalkos/internal/graphql/gqlerr"
)

// Deps are the services behind the native API.
type Deps struct {
	Accounts    *account.Service
	Projects    *project.Service
	Issues      *issue.Service
	Audit       *audit.Service
	Milestones  *milestone.Service
	Labels      *label.Service
	Comments    *comment.Service
	Attachments *attachment.Service
	Timeline    *timeline.Service
	Logger      *slog.Logger
}

// NewHandler serves the native schema over POST only, with introspection enabled.
// Each request gets its own loaders (batched lookups by id).
func NewHandler(d Deps) http.Handler {
	logger := d.Logger
	srv := handler.New(NewExecutableSchema(Config{Resolvers: &Resolver{
		Accounts: d.Accounts, Projects: d.Projects, Issues: d.Issues, Audit: d.Audit,
		Milestones: d.Milestones, Labels: d.Labels, Comments: d.Comments, Attachments: d.Attachments,
		Timeline: d.Timeline, Logger: logger,
	}, Complexity: complexity()}))
	srv.AddTransport(transport.POST{})
	srv.Use(extension.Introspection{})
	// Caps query cost per request (see complexity.go): nested lists and
	// batched logins are rejected before any resolver runs.
	srv.Use(extension.FixedComplexityLimit(maxComplexity))
	srv.Use(rootFieldLimit{})
	srv.SetErrorPresenter(gqlerr.Presenter(func(_ context.Context, err error) {
		logger.Error("graphql internal error", slog.String("api", "native"), slog.String("error", err.Error()))
	}))
	return withLoaders(d, srv)
}
