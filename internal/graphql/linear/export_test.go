package linear

import (
	"context"
	"time"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/domain/project"
)

// Present runs an error through the /graphql error presenter.
func Present(err error) *gqlerror.Error {
	return presenter(nil, func(context.Context, error) {})(context.Background(), err)
}

// CostOf validates query against the schema and returns its cost.
func CostOf(query string, vars map[string]any) (float64, error) {
	doc, errs := gqlparser.LoadQueryWithRules(NewExecutableSchema(Config{Resolvers: &Resolver{}}).Schema(), query, nil)
	if len(errs) > 0 {
		return 0, errs
	}
	return operationCost(doc.Operations[0], doc.Fragments, vars), nil
}

// ParseDateOrDuration is parseDateOrDuration.
func ParseDateOrDuration(field, s string, now time.Time) (time.Time, error) {
	return parseDateOrDuration(field, s, now)
}

// ParseTimelessOrDuration is parseTimelessOrDuration.
func ParseTimelessOrDuration(field, s string, today time.Time) (time.Time, error) {
	return parseTimelessOrDuration(field, s, today)
}

// BranchName is branchName.
func BranchName(identifier, title string) string { return branchName(identifier, title) }

// StateConn is stateConn without a filter; board selects the board order.
func StateConn(states []project.State, by *PaginationOrderBy, a page.Args, board bool) (*WorkflowStateConnection, error) {
	return stateConn(states, nil, nil, by, a, board)
}
