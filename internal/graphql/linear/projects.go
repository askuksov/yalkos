package linear

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/domain/project"
)

const projectColor = "#6B7280"

// statusIndex is the position of a milestone status among projectStatuses.
var statusIndex = map[milestone.Status]int{milestone.Planned: 0, milestone.InProgress: 1, milestone.Done: 2, milestone.Canceled: 3}

// statusFor is the project status of a milestone status.
func statusFor(s milestone.Status, ws account.Workspace) ProjectStatus {
	return projectStatuses(ws)[statusIndex[s]]
}

// statusByID reads a project status id.
func statusByID(id string) (milestone.Status, bool) {
	for s, i := range statusIndex {
		if statusRows[i].id == id {
			return s, true
		}
	}
	return "", false
}

// projectOut maps a milestone; status, progress and links are resolved.
func projectOut(m milestone.Milestone) *Project {
	return &Project{
		ID: m.ID.String(), Name: m.Name, Description: m.Description, Content: nonEmpty(m.Description), SlugID: m.ID.String(),
		TargetDate: m.TargetDate, SortOrder: m.SortOrder, PrioritySortOrder: m.SortOrder, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
		Color: projectColor, PriorityLabel: priorityLabels[0], FrequencyResolution: FrequencyResolutionTypeWeekly,
		LabelIds: []string{}, CompletedScopeHistory: []float64{}, CompletedIssueCountHistory: []float64{},
		InProgressScopeHistory: []float64{}, IssueCountHistory: []float64{}, ScopeHistory: []float64{},
		Src: m,
	}
}

func (r *Resolver) workspace(ctx context.Context) (account.Workspace, error) {
	return loadersFrom(ctx).workspace.get(ctx, r.d.Accounts.Workspace)
}

// projectStatus is the status of a milestone with its progress.
func (r *Resolver) projectStatus(ctx context.Context, m milestone.Milestone) (milestone.Status, milestone.Progress, error) {
	p, err := loadersFrom(ctx).milestoneProgress.Load(ctx, m.ID)
	if err != nil {
		return "", p, err
	}
	return m.Status(p), p, nil
}

var stateNames = map[milestone.Status]string{
	milestone.Planned: "planned", milestone.InProgress: "started", milestone.Done: "completed", milestone.Canceled: "canceled",
}

func progressOf(p milestone.Progress) float64 {
	if p.Total == 0 {
		return 0
	}
	return float64(p.Completed) / float64(p.Total)
}

// projectRow is a milestone with what project filters need.
type projectRow struct {
	m      milestone.Milestone
	status milestone.Status
	team   project.Project
}

func projectMatch(f *ProjectFilter, row projectRow, now time.Time) (bool, error) {
	if f == nil {
		return true, nil
	}
	for _, sub := range f.And {
		if ok, err := projectMatch(&sub, row, now); err != nil || !ok {
			return false, err
		}
	}
	if len(f.Or) > 0 {
		matched := false
		for _, sub := range f.Or {
			ok, err := projectMatch(&sub, row, now)
			if err != nil {
				return false, err
			}
			matched = matched || ok
		}
		if !matched {
			return false, nil
		}
	}
	if ok, err := matchID("id", f.ID, row.m.ID); err != nil || !ok {
		return false, err
	}
	if f.TargetDate != nil {
		ok, err := matchNullableTime("targetDate", f.TargetDate, row.m.TargetDate, now)
		if err != nil || !ok {
			return false, err
		}
	}
	if t := f.AccessibleTeams; t != nil {
		if ok, err := matchID("id", t.ID, row.team.ID); err != nil || !ok {
			return false, err
		}
		if ok, err := teamMatch(t.Some, row.team); err != nil || !ok {
			return false, err
		}
	}
	return matchString(f.Name, row.m.Name) && matchString(f.State, stateNames[row.status]), nil
}

// projectRows reads milestones (of archived teams when asked) with status and team.
func (r *Resolver) projectRows(ctx context.Context, includeArchived bool) ([]projectRow, error) {
	ms, err := r.d.Milestones.ListAll(ctx, includeArchived)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(ms))
	for i, m := range ms {
		ids[i] = m.ID
	}
	prog, err := r.d.Milestones.Progress(ctx, ids)
	if err != nil {
		return nil, err
	}
	ps, err := r.everyProject(ctx)
	if err != nil {
		return nil, err
	}
	teams := teamIndex(ps...)
	out := make([]projectRow, len(ms))
	for i, m := range ms {
		out[i] = projectRow{m: m, status: m.Status(prog[m.ID]), team: teams[m.ProjectID]}
	}
	return out, nil
}

func sortProjects(rows []projectRow, sort []ProjectSortInput) error {
	if len(sort) == 0 {
		return nil
	}
	if len(sort) > 1 {
		return tooManySortKeys()
	}
	s := sort[0]
	type key struct {
		nulls *PaginationNulls
		order *PaginationSortOrder
		cmp   func(a, b projectRow) int
	}
	var keys []key
	if s.CreatedAt != nil {
		keys = append(keys, key{s.CreatedAt.Nulls, s.CreatedAt.Order, func(a, b projectRow) int { return a.m.CreatedAt.Compare(b.m.CreatedAt) }})
	}
	if s.UpdatedAt != nil {
		keys = append(keys, key{s.UpdatedAt.Nulls, s.UpdatedAt.Order, func(a, b projectRow) int { return a.m.UpdatedAt.Compare(b.m.UpdatedAt) }})
	}
	if s.Manual != nil {
		keys = append(keys, key{s.Manual.Nulls, s.Manual.Order, func(a, b projectRow) int { return cmp.Compare(a.m.SortOrder, b.m.SortOrder) }})
	}
	if s.Name != nil {
		keys = append(keys, key{s.Name.Nulls, s.Name.Order, func(a, b projectRow) int {
			return strings.Compare(strings.ToLower(a.m.Name), strings.ToLower(b.m.Name))
		}})
	}
	if s.TargetDate != nil {
		keys = append(keys, key{s.TargetDate.Nulls, s.TargetDate.Order, func(a, b projectRow) int {
			return a.m.TargetDate.Compare(*b.m.TargetDate)
		}})
	}
	if len(keys) > 1 {
		return tooManySortKeys()
	}
	if len(keys) == 0 {
		return nil
	}
	desc, err := sortKey(keys[0].nulls, keys[0].order)
	if err != nil {
		return err
	}
	byTarget := s.TargetDate != nil
	slices.SortStableFunc(rows, func(a, b projectRow) int {
		if byTarget && (a.m.TargetDate == nil) != (b.m.TargetDate == nil) {
			// No target date last, in both directions.
			if a.m.TargetDate == nil {
				return 1
			}
			return -1
		}
		if byTarget && a.m.TargetDate == nil {
			return 0
		}
		c := keys[0].cmp(a, b)
		if desc {
			return -c
		}
		return c
	})
	return nil
}

func (r *Resolver) projectConn(ctx context.Context, f *ProjectFilter, includeArchived *bool, by *PaginationOrderBy, sort []ProjectSortInput, a page.Args) (*ProjectConnection, error) {
	if _, err := r.actor(ctx); err != nil {
		return nil, err
	}
	rows, err := r.projectRows(ctx, isTrue(includeArchived))
	if err != nil {
		return nil, err
	}
	now := r.d.now()
	var kept []projectRow
	for _, row := range rows {
		ok, err := projectMatch(f, row, now)
		if err != nil {
			return nil, err
		}
		if ok {
			kept = append(kept, row)
		}
	}
	id := func(p projectRow) uuid.UUID { return p.m.ID }
	orderByTime(kept, id, func(p projectRow) time.Time { return p.m.CreatedAt }, func(p projectRow) time.Time { return p.m.UpdatedAt }, by)
	if err := sortProjects(kept, sort); err != nil {
		return nil, err
	}
	items, info, err := pageOf(kept, id, a)
	if err != nil {
		return nil, err
	}
	out := &ProjectConnection{PageInfo: info, Nodes: make([]Project, len(items)), Edges: make([]ProjectEdge, len(items))}
	for i, row := range items {
		n := projectOut(row.m)
		out.Nodes[i] = *n
		out.Edges[i] = ProjectEdge{Node: n, Cursor: cursorOf(row.m.ID)}
	}
	return out, nil
}

// projectRef loads a milestone for a relation field.
func (r *Resolver) projectRef(ctx context.Context, id *uuid.UUID) (*Project, error) {
	if id == nil {
		return nil, nil
	}
	m, err := loadersFrom(ctx).milestones.Load(ctx, *id)
	if err != nil {
		return nil, err
	}
	return projectOut(m), nil
}

func (r *Resolver) projectByID(ctx context.Context, id string) (*Project, error) {
	if _, err := r.actor(ctx); err != nil {
		return nil, err
	}
	mid, err := parseID("id", id)
	if err != nil {
		return nil, err
	}
	return r.projectRef(ctx, &mid)
}

// milestoneIDs are the milestones (of all teams) matching a nullable project filter.
func (fc filterCtx) milestoneCond(f *NullableProjectFilter) (*issue.IDCond, error) {
	out := &issue.IDCond{Null: f.Null}
	if f.ID == nil && f.Name == nil {
		return out, nil
	}
	ms, err := fc.r.d.Milestones.ListAll(fc.ctx, true)
	if err != nil {
		return nil, err
	}
	out.In = []uuid.UUID{}
	for _, m := range ms {
		ok, err := matchID("id", f.ID, m.ID)
		if err != nil {
			return nil, err
		}
		if ok && matchString(f.Name, m.Name) {
			out.In = append(out.In, m.ID)
		}
	}
	return out, nil
}

func projectPayload(m milestone.Milestone) *ProjectPayload {
	return &ProjectPayload{Success: true, LastSyncID: lastSync(m.UpdatedAt), Project: projectOut(m)}
}

// description takes description or content; both is an error.
func description(desc, content *string) (*string, error) {
	if desc != nil && content != nil {
		return nil, badArg("content", "Invalid content: use either description or content")
	}
	if content != nil {
		return content, nil
	}
	return desc, nil
}

func (r *Resolver) createProject(ctx context.Context, in ProjectCreateInput) (*ProjectPayload, error) {
	actor, err := r.actor(ctx)
	if err != nil {
		return nil, err
	}
	if len(in.TeamIds) != 1 {
		return nil, badArg("teamIds", "Invalid teamIds: a project belongs to exactly one team")
	}
	desc, err := description(in.Description, in.Content)
	if err != nil {
		return nil, err
	}
	team, err := r.projectByRef(ctx, "teamIds", in.TeamIds[0])
	if err != nil {
		return nil, err
	}
	ci := milestone.CreateInput{Name: in.Name, TargetDate: in.TargetDate}
	if desc != nil {
		ci.Description = *desc
	}
	if ci.LeadID, err = optID("leadId", in.LeadID); err != nil {
		return nil, err
	}
	m, err := r.d.Milestones.Create(ctx, actor, team.ID, ci)
	if err != nil {
		return nil, err
	}
	return projectPayload(m), nil
}

func (r *Resolver) updateProject(ctx context.Context, id string, in ProjectUpdateInput) (*ProjectPayload, error) {
	actor, err := r.actor(ctx)
	if err != nil {
		return nil, err
	}
	mid, err := parseID("id", id)
	if err != nil {
		return nil, err
	}
	desc, err := description(in.Description, in.Content)
	if err != nil {
		return nil, err
	}
	p := milestone.Patch{Name: in.Name, Description: desc}
	if v, set := in.TargetDate.ValueOK(); set {
		p.TargetDate = kit.Opt[time.Time]{Set: true, Value: v}
	}
	if p.LeadID, err = optPatch("leadId", in.LeadID); err != nil {
		return nil, err
	}
	if v, set := in.StatusID.ValueOK(); set {
		p.Status = kit.Opt[milestone.Status]{Set: true}
		if v != nil {
			s, ok := statusByID(*v)
			if !ok {
				return nil, badArg("statusId", "Invalid statusId: no such project status")
			}
			p.Status.Value = &s
		}
	}
	m, err := r.d.Milestones.Update(ctx, actor, mid, p)
	if err != nil {
		return nil, err
	}
	return projectPayload(m), nil
}

func (r *Resolver) deleteProject(ctx context.Context, id string) (*ProjectArchivePayload, error) {
	actor, err := r.actor(ctx)
	if err != nil {
		return nil, err
	}
	mid, err := parseID("id", id)
	if err != nil {
		return nil, err
	}
	m, err := r.d.Milestones.Get(ctx, mid)
	if err != nil {
		return nil, err
	}
	if err := r.d.Milestones.Delete(ctx, actor, mid); err != nil {
		return nil, err
	}
	return &ProjectArchivePayload{Success: true, LastSyncID: lastSync(r.d.now()), Entity: projectOut(m)}, nil
}

// projectTeams is the one-team connection of a milestone.
func (r *Resolver) projectTeams(p project.Project, f *TeamFilter, by *PaginationOrderBy, a page.Args) (*TeamConnection, error) {
	return teamList([]project.Project{p}, f, by, a)
}
