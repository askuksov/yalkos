package linear

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/askuksov/yalkos/internal/domain/page"
)

// Query cost: a scalar or enum field costs 0.1, an object field 1 plus its
// selection, and a connection (a field with a first argument) its page size
// times (1 plus its selection). Lists that are not connections count once.
// Fragments on unions and interfaces are all counted.
const (
	maxCost       = 10_000
	maxRootFields = 50
	// Costs are counted in tenths of a point so sums stay exact.
	scalarTenths = 1
	objectTenths = 10
	// capTenths bounds sums so deep nesting cannot overflow.
	capTenths = 1 << 50
)

// costLimit rejects operations over maxCost points or maxRootFields root
// fields before any resolver runs, and reports the cost in X-Complexity.
type costLimit struct{}

var _ interface {
	graphql.HandlerExtension
	graphql.OperationContextMutator
} = costLimit{}

func (costLimit) ExtensionName() string                   { return "QueryCost" }
func (costLimit) Validate(graphql.ExecutableSchema) error { return nil }

func (costLimit) MutateOperationContext(ctx context.Context, oc *graphql.OperationContext) *gqlerror.Error {
	if oc.Operation == nil {
		return nil
	}
	if n := countFields(oc.Operation.SelectionSet, oc.Doc.Fragments); n > maxRootFields {
		return costErr(fmt.Sprintf("Operation has %d root fields, the limit is %d", n, maxRootFields))
	}
	cost := int((costTenths(oc.Operation, oc.Doc.Fragments, oc.Variables) + 9) / 10)
	if w := responseFrom(ctx); w != nil {
		w.Header().Set("X-Complexity", strconv.Itoa(cost))
	}
	if cost > maxCost {
		return costErr(fmt.Sprintf("Query too complex: %d points, the limit is %d", cost, maxCost))
	}
	return nil
}

func costErr(msg string) *gqlerror.Error {
	return &gqlerror.Error{Message: msg, Extensions: map[string]any{"code": "COMPLEXITY_LIMIT_EXCEEDED"}}
}

// operationCost prices a validated operation in points.
func operationCost(op *ast.OperationDefinition, frags ast.FragmentDefinitionList, vars map[string]any) float64 {
	return float64(costTenths(op, frags, vars)) / 10
}

func costTenths(op *ast.OperationDefinition, frags ast.FragmentDefinitionList, vars map[string]any) int64 {
	return selectionCost(op.SelectionSet, frags, vars, 0)
}

// maxDepth stops runaway recursion; validation has rejected fragment cycles.
const maxDepth = 64

func selectionCost(set ast.SelectionSet, frags ast.FragmentDefinitionList, vars map[string]any, depth int) int64 {
	if depth > maxDepth {
		return capTenths
	}
	var sum int64
	for _, sel := range set {
		switch s := sel.(type) {
		case *ast.Field:
			sum += fieldCost(s, frags, vars, depth)
		case *ast.InlineFragment:
			sum += selectionCost(s.SelectionSet, frags, vars, depth+1)
		case *ast.FragmentSpread:
			if def := frags.ForName(s.Name); def != nil {
				sum += selectionCost(def.SelectionSet, frags, vars, depth+1)
			}
		}
		sum = min(sum, capTenths)
	}
	return sum
}

func fieldCost(f *ast.Field, frags ast.FragmentDefinitionList, vars map[string]any, depth int) int64 {
	if len(f.SelectionSet) == 0 {
		return scalarTenths
	}
	c := objectTenths + selectionCost(f.SelectionSet, frags, vars, depth+1)
	if f.Definition != nil && f.Definition.Arguments.ForName("first") != nil {
		c = min(c*int64(costPageSize(f, vars)), capTenths)
	}
	return c
}

// costPageSize is first or last, else the default page size, kept in 1–250.
func costPageSize(f *ast.Field, vars map[string]any) int {
	n := page.Default
	for _, name := range []string{"first", "last"} {
		if a := f.Arguments.ForName(name); a != nil {
			if v, err := a.Value.Value(vars); err == nil && v != nil {
				if i, ok := toInt(v); ok {
					n = i
					break
				}
			}
		}
	}
	return min(max(n, 1), page.Max)
}

func toInt(v any) (int, bool) {
	switch x := v.(type) {
	case int64:
		return int(x), true
	case int:
		return x, true
	case float64:
		return int(x), true
	case json.Number:
		i, err := x.Int64()
		return int(i), err == nil
	}
	return 0, false
}

// countFields counts fields of one selection level, expanding fragments.
func countFields(set ast.SelectionSet, frags ast.FragmentDefinitionList) int {
	n := 0
	for _, sel := range set {
		switch s := sel.(type) {
		case *ast.Field:
			n++
		case *ast.InlineFragment:
			n += countFields(s.SelectionSet, frags)
		case *ast.FragmentSpread:
			if def := frags.ForName(s.Name); def != nil {
				n += countFields(def.SelectionSet, frags)
			}
		}
	}
	return n
}

type responseKey struct{}

// withResponse keeps the response writer in the request context so the cost
// extension can set its header.
func withResponse(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), responseKey{}, w)))
	})
}

func responseFrom(ctx context.Context) http.ResponseWriter {
	w, _ := ctx.Value(responseKey{}).(http.ResponseWriter)
	return w
}
