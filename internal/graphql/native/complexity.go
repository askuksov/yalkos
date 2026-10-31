package native

import "github.com/askuksov/yalkos/internal/domain/page"

// Query cost: every field costs 1 plus its selection; a connection costs its
// page size times its selection, so nested lists multiply. Credential
// mutations cost authCost, so one request cannot batch many password guesses.
const (
	maxComplexity = 10_000
	authCost      = 500
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

// connCost saturates just above the limit so deep nesting cannot overflow.
func connCost(child int, first, last *int) int {
	n := pageSize(first, last)
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
	c.Mutation.Login = func(child int, _, _ string) int { return authCost + child }
	c.Mutation.Setup = func(child int, _ SetupInput) int { return authCost + child }
	c.Mutation.AcceptInvite = func(child int, _ AcceptInviteInput) int { return authCost + child }
	c.Mutation.ChangePassword = func(child int, _, _ string) int { return authCost + child }
	return c
}
