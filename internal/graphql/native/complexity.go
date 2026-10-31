package native

import (
	"context"
	"fmt"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/askuksov/yalkos/internal/domain/page"
)

// Query cost: every field costs 1 plus its selection; a connection costs its
// page size times its selection, so nested lists multiply. Credential
// mutations cost authCost, so one request cannot batch many password guesses.
// Root fields (aliases included) are capped on their own: the cost budget
// alone would let one request batch thousands of cheap root fields.
const (
	maxComplexity = 10_000
	authCost      = 500
	maxRootFields = 50
	// stateWeight is the typical number of states per project (at most 50);
	// memberWeight the typical number of members.
	stateWeight  = 20
	memberWeight = 20
	// backEdgeCost multiplies State.project: it leads back to Project.states,
	// so states → project → states may nest two levels but not three.
	backEdgeCost = 10
	// countCost weighs IssueConnection.totalCount: each count is its own
	// aggregate query, unlike scalars read from loaded rows.
	countCost = 10
	// Typical sizes of lists that are not paginated: labels and groups at the
	// top level, labels in a group, labels on one issue, milestones of a project.
	labelWeight          = 30
	labelChildrenWeight  = 10
	issueLabelWeight     = 4
	projectMilestoneSize = 20
	// Typical number of files on one issue.
	attachmentWeight = 10
)

// pageSize is the number of items a connection may return.
func pageSize(first, last *int) int {
	n := page.Default
	switch {
	case first != nil:
		n = *first
	case last != nil:
		n = *last
	}
	return min(max(n, 1), page.Max)
}

// connCost is the cost of a connection page.
func connCost(child int, first, last *int) int { return listCost(child, pageSize(first, last)) }

// listCost saturates just above the limit so deep nesting cannot overflow.
func listCost(child, n int) int {
	if child > maxComplexity/n {
		return maxComplexity + 1
	}
	return 1 + n*child
}

func complexity() ComplexityRoot {
	var c ComplexityRoot
	c.Query.Issues = func(child int, _ *IssueFilter, _ *IssueOrder, first *int, _ *string, last *int, _ *string) int {
		return connCost(child, first, last)
	}
	c.Query.Projects = func(child int, _ *bool, first *int, _ *string, last *int, _ *string) int {
		return connCost(child, first, last)
	}
	c.Issue.Children = func(child int, first *int, _ *string, last *int, _ *string, _ *IssueOrder) int {
		return connCost(child, first, last)
	}
	c.Issue.History = func(child int, first *int, _ *string, last *int, _ *string) int {
		return connCost(child, first, last)
	}
	c.IssueConnection.TotalCount = func(int) int { return countCost }
	c.Milestone.Issues = func(child int, _ *IssueFilter, _ *IssueOrder, first *int, _ *string, last *int, _ *string) int {
		return connCost(child, first, last)
	}
	c.Project.Milestones = func(child int, _ *bool) int { return listCost(child, projectMilestoneSize) }
	c.Query.Labels = func(child int) int { return listCost(child, labelWeight) }
	c.Label.Children = func(child int) int { return listCost(child, labelChildrenWeight) }
	c.Issue.Labels = func(child int) int { return listCost(child, issueLabelWeight) }
	c.Issue.Comments = func(child int, first *int, _ *string, last *int, _ *string) int {
		return connCost(child, first, last)
	}
	c.Issue.Timeline = func(child int, first *int, _ *string, last *int, _ *string) int {
		return connCost(child, first, last)
	}
	c.Issue.Attachments = func(child int) int { return listCost(child, attachmentWeight) }
	c.Project.States = func(child int) int { return listCost(child, stateWeight) }
	c.Project.Members = func(child int) int { return listCost(child, memberWeight) }
	c.State.Project = func(child int) int { return listCost(child, backEdgeCost) }
	c.Mutation.Login = func(child int, _, _ string) int { return authCost + child }
	c.Mutation.Setup = func(child int, _ SetupInput) int { return authCost + child }
	c.Mutation.AcceptInvite = func(child int, _ AcceptInviteInput) int { return authCost + child }
	c.Mutation.ChangePassword = func(child int, _, _ string) int { return authCost + child }
	return c
}

// rootFieldLimit rejects operations with more than maxRootFields root fields.
type rootFieldLimit struct{}

var _ interface {
	graphql.HandlerExtension
	graphql.OperationContextMutator
} = rootFieldLimit{}

func (rootFieldLimit) ExtensionName() string                   { return "RootFieldLimit" }
func (rootFieldLimit) Validate(graphql.ExecutableSchema) error { return nil }

func (rootFieldLimit) MutateOperationContext(_ context.Context, oc *graphql.OperationContext) *gqlerror.Error {
	if oc.Operation == nil {
		return nil
	}
	if n := countFields(oc.Operation.SelectionSet, oc.Doc.Fragments); n > maxRootFields {
		return &gqlerror.Error{
			Message:    fmt.Sprintf("operation has %d root fields, the limit is %d", n, maxRootFields),
			Extensions: map[string]any{"code": "COMPLEXITY_LIMIT_EXCEEDED"},
		}
	}
	return nil
}

// countFields counts fields of one selection level, expanding fragments
// (validation has already rejected fragment cycles).
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
