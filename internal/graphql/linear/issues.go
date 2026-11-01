package linear

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/attachment"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/domain/project"
)

// memo keeps the first successful result of a read for the request.
type memo[T any] struct {
	mu   sync.Mutex
	done bool
	v    T
}

func (m *memo[T]) get(ctx context.Context, read func(context.Context) (T, error)) (T, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.done {
		return m.v, nil
	}
	v, err := read(ctx)
	if err != nil {
		return v, err
	}
	m.v, m.done = v, true
	return v, nil
}

// everyProject reads all projects, archived ones included, once per request.
func (r *Resolver) everyProject(ctx context.Context) ([]project.Project, error) {
	return loadersFrom(ctx).allProjects.get(ctx, func(ctx context.Context) ([]project.Project, error) {
		return r.allProjects(ctx, true)
	})
}

// teamIDs are the projects (archived ones included) matching f.
func (r *Resolver) teamIDs(ctx context.Context, f *TeamFilter) ([]uuid.UUID, error) {
	ps, err := r.everyProject(ctx)
	if err != nil {
		return nil, err
	}
	out := []uuid.UUID{}
	for _, p := range ps {
		ok, err := teamMatch(f, p)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, p.ID)
		}
	}
	return out, nil
}

// issueConn lists issues; scope is AND-ed with the caller's filter.
func (r *Resolver) issueConn(ctx context.Context, scope issue.Filter, f *IssueFilter, by *PaginationOrderBy, sort []IssueSortInput, a page.Args) (*IssueConnection, error) {
	user, err := r.issueFilter(ctx, f)
	if err != nil {
		return nil, err
	}
	order, err := issueOrder(by, sort)
	if err != nil {
		return nil, err
	}
	res, err := r.d.Issues.List(ctx, scoped(scope, user), order, a)
	if err != nil {
		return nil, err
	}
	out := &IssueConnection{PageInfo: infoOut(res.Info), Nodes: make([]Issue, len(res.Items)), Edges: make([]IssueEdge, len(res.Items))}
	for i, it := range res.Items {
		n := r.issueOut(it)
		out.Nodes[i] = *n
		out.Edges[i] = IssueEdge{Node: n, Cursor: res.Cursors[i]}
	}
	return out, nil
}

// scoped ANDs the caller's filter with a list's scope. Without a project,
// parent or assignee scope the caller's filter is the top level, so its team
// filter decides on archived teams.
func scoped(scope, user issue.Filter) issue.Filter {
	if scope.Project == nil && scope.Parent == nil && scope.Assignee == nil && scope.Milestone == nil {
		user.IncludeArchived = scope.IncludeArchived
		user.Search = scope.Search
		return user
	}
	scope.All = append(scope.All, user)
	return scope
}

// issueByRef finds a live issue by UUID or KEY-N (current or former). With an
// input field a miss is an input error on it; else NOT_FOUND.
func (r *Resolver) issueByRef(ctx context.Context, field, ref string) (issue.Issue, error) {
	i, err := r.d.Issues.Get(ctx, ref)
	if field != "" && errors.Is(err, kit.ErrNotFound) {
		return issue.Issue{}, kit.Input(field, kit.ReasonNotFound, "no such issue")
	}
	return i, err
}

func attachmentMatch(f *AttachmentFilter, a attachment.Attachment, now time.Time) (bool, error) {
	if f == nil {
		return true, nil
	}
	ok, err := matchID("id", f.ID, a.ID)
	if err != nil || !ok {
		return false, err
	}
	if ok, err := matchTime("createdAt", f.CreatedAt, a.CreatedAt, now); err != nil || !ok {
		return false, err
	}
	return matchString(f.Title, a.Name), nil
}

// attachmentConn filters, orders and pages an issue's files in memory.
func (r *Resolver) attachmentConn(files []attachment.Attachment, f *AttachmentFilter, by *PaginationOrderBy, a page.Args) (*AttachmentConnection, error) {
	now := r.d.now()
	var kept []attachment.Attachment
	for _, file := range files {
		ok, err := attachmentMatch(f, file, now)
		if err != nil {
			return nil, err
		}
		if ok {
			kept = append(kept, file)
		}
	}
	id := func(a attachment.Attachment) uuid.UUID { return a.ID }
	created := func(a attachment.Attachment) time.Time { return a.CreatedAt }
	orderByTime(kept, id, created, created, by)
	items, info, err := pageOf(kept, id, a)
	if err != nil {
		return nil, err
	}
	out := &AttachmentConnection{PageInfo: info, Nodes: make([]Attachment, len(items)), Edges: make([]AttachmentEdge, len(items))}
	for i, file := range items {
		n := r.attachmentOut(file)
		out.Nodes[i] = *n
		out.Edges[i] = AttachmentEdge{Node: n, Cursor: cursorOf(file.ID)}
	}
	return out, nil
}

// userRef loads a user for a relation field.
func (r *Resolver) userRef(ctx context.Context, id *uuid.UUID) (*User, error) {
	if id == nil {
		return nil, nil
	}
	me, err := r.actor(ctx)
	if err != nil {
		return nil, err
	}
	u, err := loadersFrom(ctx).users.Load(ctx, *id)
	if err != nil {
		return nil, err
	}
	return r.userOut(u, me.ID), nil
}

// issueRef loads an issue for a relation field; a deleted one reads as null.
func (r *Resolver) issueRef(ctx context.Context, id *uuid.UUID) (*Issue, error) {
	if id == nil {
		return nil, nil
	}
	i, err := loadersFrom(ctx).issues.Load(ctx, *id)
	if errors.Is(err, kit.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r.issueOut(i), nil
}

func idStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// Scopes of issue lists, AND-ed with the caller's filter.
func issueScope(includeArchived *bool) issue.Filter {
	return issue.Filter{IncludeArchived: isTrue(includeArchived)}
}

func teamScope(id uuid.UUID, includeArchived *bool) issue.Filter {
	return issue.Filter{Project: &issue.IDCond{Eq: &id}, IncludeArchived: isTrue(includeArchived)}
}

func childScope(id uuid.UUID) issue.Filter {
	return issue.Filter{Parent: &issue.IDCond{Eq: &id}, IncludeArchived: true}
}

func assigneeScope(id uuid.UUID, includeArchived *bool) issue.Filter {
	return issue.Filter{Assignee: &issue.IDCond{Eq: &id}, IncludeArchived: isTrue(includeArchived)}
}

func (r *Resolver) issueLabelIDs(ctx context.Context, i issue.Issue) ([]string, error) {
	if i.LabelIDs != nil {
		return idStrings(i.LabelIDs), nil
	}
	ids, err := loadersFrom(ctx).issueLabels.Load(ctx, i.ID)
	return idStrings(ids), err
}

func (r *Resolver) issueTeam(ctx context.Context, i issue.Issue) (*Team, error) {
	p, err := loadersFrom(ctx).projects.Load(ctx, i.ProjectID)
	if err != nil {
		return nil, err
	}
	return teamOut(p), nil
}

func (r *Resolver) issueState(ctx context.Context, i issue.Issue) (*WorkflowState, error) {
	st, err := loadersFrom(ctx).states.Load(ctx, i.StateID)
	if err != nil {
		return nil, err
	}
	return stateOut(st), nil
}

func milestoneScope(id uuid.UUID) issue.Filter {
	return issue.Filter{Milestone: &issue.IDCond{Eq: &id}, IncludeArchived: true}
}
