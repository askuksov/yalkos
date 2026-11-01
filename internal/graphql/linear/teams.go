package linear

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/domain/project"
)

// allProjects reads every project, archived ones when asked.
func (r *Resolver) allProjects(ctx context.Context, includeArchived bool) ([]project.Project, error) {
	var out []project.Project
	a := page.Args{First: new(page.Max)}
	for {
		res, err := r.d.Projects.List(ctx, includeArchived, a)
		if err != nil {
			return nil, err
		}
		out = append(out, res.Items...)
		if !res.Info.HasNext {
			return out, nil
		}
		a.After = res.Info.End
	}
}

// teamMatch applies a team filter in memory.
func teamMatch(f *TeamFilter, p project.Project) (bool, error) {
	if f == nil {
		return true, nil
	}
	for _, sub := range f.And {
		if ok, err := teamMatch(&sub, p); err != nil || !ok {
			return false, err
		}
	}
	if len(f.Or) > 0 {
		matched := false
		for _, sub := range f.Or {
			ok, err := teamMatch(&sub, p)
			if err != nil {
				return false, err
			}
			matched = matched || ok
		}
		if !matched {
			return false, nil
		}
	}
	ok, err := matchID("id", f.ID, p.ID)
	if err != nil || !ok {
		return false, err
	}
	return matchString(f.Key, p.Key) && matchString(f.Name, p.Name), nil
}

// teams returns the projects matching f.
func (r *Resolver) teams(ctx context.Context, f *TeamFilter, includeArchived bool) ([]project.Project, error) {
	all, err := r.allProjects(ctx, includeArchived)
	if err != nil {
		return nil, err
	}
	var out []project.Project
	for _, p := range all {
		ok, err := teamMatch(f, p)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, p)
		}
	}
	return out, nil
}

func (r *Resolver) teamConn(ctx context.Context, f *TeamFilter, includeArchived *bool, by *PaginationOrderBy, a page.Args) (*TeamConnection, error) {
	if _, err := r.actor(ctx); err != nil {
		return nil, err
	}
	ps, err := r.teams(ctx, f, isTrue(includeArchived))
	if err != nil {
		return nil, err
	}
	return teamList(ps, nil, by, a)
}

// teamList filters, orders and pages projects in memory.
func teamList(all []project.Project, f *TeamFilter, by *PaginationOrderBy, a page.Args) (*TeamConnection, error) {
	var ps []project.Project
	for _, p := range all {
		ok, err := teamMatch(f, p)
		if err != nil {
			return nil, err
		}
		if ok {
			ps = append(ps, p)
		}
	}
	id := func(p project.Project) uuid.UUID { return p.ID }
	orderByTime(ps, id, func(p project.Project) time.Time { return p.CreatedAt }, func(p project.Project) time.Time { return p.UpdatedAt }, by)
	items, info, err := pageOf(ps, id, a)
	if err != nil {
		return nil, err
	}
	out := &TeamConnection{PageInfo: info, Nodes: make([]Team, len(items)), Edges: make([]TeamEdge, len(items))}
	for i, p := range items {
		n := teamOut(p)
		out.Nodes[i] = *n
		out.Edges[i] = TeamEdge{Node: n, Cursor: cursorOf(p.ID)}
	}
	return out, nil
}

func isTrue(b *bool) bool { return b != nil && *b }

// projectByRef finds a project by UUID or key (current or former). With an
// input field, a miss is an input error on that field; else NOT_FOUND.
func (r *Resolver) projectByRef(ctx context.Context, field, ref string) (project.Project, error) {
	p, err := r.d.Projects.Get(ctx, ref)
	if field != "" && errors.Is(err, kit.ErrNotFound) {
		return project.Project{}, kit.Input(field, kit.ReasonNotFound, "no such team")
	}
	return p, err
}

// stateMatch applies a state filter in memory; teams maps project ids.
func stateMatch(f *WorkflowStateFilter, s project.State, teams map[uuid.UUID]project.Project) (bool, error) {
	if f == nil {
		return true, nil
	}
	for _, sub := range f.And {
		if ok, err := stateMatch(&sub, s, teams); err != nil || !ok {
			return false, err
		}
	}
	if len(f.Or) > 0 {
		matched := false
		for _, sub := range f.Or {
			ok, err := stateMatch(&sub, s, teams)
			if err != nil {
				return false, err
			}
			matched = matched || ok
		}
		if !matched {
			return false, nil
		}
	}
	ok, err := matchID("id", f.ID, s.ID)
	if err != nil || !ok {
		return false, err
	}
	if f.Team != nil {
		if ok, err := teamMatch(f.Team, teams[s.ProjectID]); err != nil || !ok {
			return false, err
		}
	}
	return matchString(f.Name, s.Name) && matchString(f.Type, string(s.Type)), nil
}

// stateConn filters, orders and pages states in memory.
func stateConn(states []project.State, teams map[uuid.UUID]project.Project, f *WorkflowStateFilter, by *PaginationOrderBy, a page.Args) (*WorkflowStateConnection, error) {
	var kept []project.State
	for _, s := range states {
		ok, err := stateMatch(f, s, teams)
		if err != nil {
			return nil, err
		}
		if ok {
			kept = append(kept, s)
		}
	}
	id := func(s project.State) uuid.UUID { return s.ID }
	orderByTime(kept, id, func(s project.State) time.Time { return s.CreatedAt }, func(s project.State) time.Time { return s.UpdatedAt }, by)
	items, info, err := pageOf(kept, id, a)
	if err != nil {
		return nil, err
	}
	out := &WorkflowStateConnection{PageInfo: info, Nodes: make([]WorkflowState, len(items)), Edges: make([]WorkflowStateEdge, len(items))}
	for i, s := range items {
		n := stateOut(s)
		out.Nodes[i] = *n
		out.Edges[i] = WorkflowStateEdge{Node: n, Cursor: cursorOf(s.ID)}
	}
	return out, nil
}

// organization reads the workspace with its counts.
func (r *Resolver) organization(ctx context.Context) (*Organization, error) {
	me, err := r.actor(ctx)
	if err != nil {
		return nil, err
	}
	ws, err := r.d.Accounts.Workspace(ctx)
	if err != nil {
		return nil, err
	}
	users, err := r.d.Accounts.ListMembers(ctx, me)
	if err != nil {
		return nil, err
	}
	n, err := r.d.Issues.Count(ctx, issue.Filter{IncludeArchived: true})
	if err != nil {
		return nil, err
	}
	return organizationOut(ws, users, n), nil
}

// teamIndex maps project ids to projects.
func teamIndex(ps ...project.Project) map[uuid.UUID]project.Project {
	out := make(map[uuid.UUID]project.Project, len(ps))
	for _, p := range ps {
		out[p.ID] = p
	}
	return out
}

// workflowStates lists the states of all teams (archived ones when asked).
func (r *Resolver) workflowStates(ctx context.Context, f *WorkflowStateFilter, includeArchived *bool, by *PaginationOrderBy, a page.Args) (*WorkflowStateConnection, error) {
	if _, err := r.actor(ctx); err != nil {
		return nil, err
	}
	ps, err := r.allProjects(ctx, isTrue(includeArchived))
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(ps))
	for i, p := range ps {
		ids[i] = p.ID
	}
	byProject, err := r.d.Projects.StatesByProjects(ctx, ids)
	if err != nil {
		return nil, err
	}
	var states []project.State
	for _, id := range ids {
		states = append(states, byProject[id]...)
	}
	return stateConn(states, teamIndex(ps...), f, by, a)
}
