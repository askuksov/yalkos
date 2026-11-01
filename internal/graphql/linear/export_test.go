package linear

import (
	"context"
	"time"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// Present runs an error through the /graphql error presenter.
func Present(err error) *gqlerror.Error {
	return presenter(func(context.Context, error) {})(context.Background(), err)
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
