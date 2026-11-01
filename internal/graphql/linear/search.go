package linear

import (
	"context"
	"errors"
	"reflect"
	"regexp"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/page"
)

// searchResultOut maps an issue to a search result: the same fields (copied
// by name from issueOut) and empty metadata.
func (r *Resolver) searchResultOut(i issue.Issue) IssueSearchResult {
	var out IssueSearchResult
	src := reflect.ValueOf(r.issueOut(i)).Elem()
	dst := reflect.ValueOf(&out).Elem()
	for k := range dst.NumField() {
		f := dst.Type().Field(k)
		if v := src.FieldByName(f.Name); v.IsValid() && v.Type() == f.Type {
			dst.Field(k).Set(v)
		}
	}
	return out
}

// searchIssues runs a word search with the caller's filter, within one team
// when teamRef is set.
func (r *Resolver) searchIssues(ctx context.Context, term string, teamRef *string, f *IssueFilter, includeArchived *bool, by *PaginationOrderBy, a page.Args) (*IssueSearchPayload, error) {
	scope := issueScope(includeArchived)
	scope.Search = &term
	if teamRef != nil {
		p, err := r.projectByRef(ctx, "teamId", *teamRef)
		if err != nil {
			return nil, err
		}
		scope.Project = &issue.IDCond{Eq: &p.ID}
	}
	user, err := r.issueFilter(ctx, f, scope.IncludeArchived)
	if err != nil {
		return nil, err
	}
	order, err := issueOrder(by, nil)
	if err != nil {
		return nil, err
	}
	all := scoped(scope, user)
	res, err := r.d.Issues.List(ctx, all, order, a)
	if err != nil {
		return nil, err
	}
	n, err := r.d.Issues.Count(ctx, all)
	if err != nil {
		return nil, err
	}
	out := &IssueSearchPayload{
		PageInfo: infoOut(res.Info), TotalCount: float64(n), Nodes: make([]IssueSearchResult, len(res.Items)),
		ArchivePayload: &ArchiveResponse{IncludesDependencies: []string{}},
	}
	for i, it := range res.Items {
		out.Nodes[i] = r.searchResultOut(it)
	}
	return out, nil
}

var branchIssueRe = regexp.MustCompile(`(?i)\b([a-z][a-z0-9]{0,6})-(\d+)\b`)

// maxBranchCandidates bounds the lookups one branch name can cause.
const maxBranchCandidates = 10

// issueByBranch returns the first issue whose KEY-N appears in the branch
// name (among its first maxBranchCandidates), or nil.
func (r *Resolver) issueByBranch(ctx context.Context, branch string) (*Issue, error) {
	if _, err := r.actor(ctx); err != nil {
		return nil, err
	}
	for _, m := range branchIssueRe.FindAllString(branch, maxBranchCandidates) {
		i, err := r.d.Issues.Get(ctx, m)
		if errors.Is(err, kit.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return r.issueOut(i), nil
	}
	return nil, nil
}
