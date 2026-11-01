package linear

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/project"
)

// filterCtx resolves relation sub-filters to id sets, reading each list once
// per request.
type filterCtx struct {
	r   *Resolver
	ctx context.Context
	me  uuid.UUID
	now time.Time
}

// issueFilter turns an issue filter into the domain filter. Relation
// filters become id sets; one that matches nothing matches no issue.
func (r *Resolver) issueFilter(ctx context.Context, f *IssueFilter) (issue.Filter, error) {
	me, err := r.actor(ctx)
	if err != nil {
		return issue.Filter{}, err
	}
	fc := filterCtx{r: r, ctx: ctx, me: me.ID, now: r.d.now()}
	return fc.issue(f)
}

var matchNothing = issue.Filter{ID: &issue.IDCond{In: []uuid.UUID{}}}

func (fc filterCtx) issue(f *IssueFilter) (issue.Filter, error) {
	var out issue.Filter
	if f == nil {
		return out, nil
	}
	var err error
	for _, sub := range f.And {
		s, err := fc.issue(&sub)
		if err != nil {
			return out, err
		}
		out.All = append(out.All, s)
	}
	for _, sub := range f.Or {
		s, err := fc.issue(&sub)
		if err != nil {
			return out, err
		}
		out.Any = append(out.Any, s)
	}
	if out.ID, err = idCond("id", f.ID); err != nil {
		return out, err
	}
	if out.Number, err = intCond("number", f.Number); err != nil {
		return out, err
	}
	if f.Priority != nil {
		if f.Priority.Null != nil && *f.Priority.Null {
			// Every issue has a priority (0 = none).
			out.All = append(out.All, matchNothing)
		}
		p := f.Priority
		if out.Priority, err = intCond("priority", &NumberComparator{Eq: p.Eq, Neq: p.Neq, Lt: p.Lt, Lte: p.Lte, Gt: p.Gt, Gte: p.Gte, In: p.In, Nin: p.Nin}); err != nil {
			return out, err
		}
	}
	out.Title = textCond(f.Title)
	if f.Description != nil {
		d := f.Description
		out.Description = textCond(&StringComparator{
			Eq: d.Eq, Neq: d.Neq, In: d.In, Nin: d.Nin, EqIgnoreCase: d.EqIgnoreCase, NeqIgnoreCase: d.NeqIgnoreCase,
			Contains: d.Contains, NotContains: d.NotContains, ContainsIgnoreCase: d.ContainsIgnoreCase,
			NotContainsIgnoreCase: d.NotContainsIgnoreCase, StartsWith: d.StartsWith, StartsWithIgnoreCase: d.StartsWithIgnoreCase,
			NotStartsWith: d.NotStartsWith, EndsWith: d.EndsWith, NotEndsWith: d.NotEndsWith,
		})
		out.Description.Null = d.Null
	}
	for _, t := range []struct {
		field string
		ops   dateOps
		dst   **issue.TimeCond
	}{
		{"createdAt", dateOpsOf(f.CreatedAt), &out.CreatedAt},
		{"updatedAt", dateOpsOf(f.UpdatedAt), &out.UpdatedAt},
		{"completedAt", nullableDateOpsOf(f.CompletedAt), &out.CompletedAt},
		{"canceledAt", nullableDateOpsOf(f.CanceledAt), &out.CanceledAt},
	} {
		if *t.dst, err = fc.timeCond(t.field, t.ops); err != nil {
			return out, err
		}
	}
	if out.DueDate, err = fc.dateCond(timelessOpsOf(f.DueDate)); err != nil {
		return out, err
	}
	if out.Assignee, err = fc.userCond(f.Assignee); err != nil {
		return out, err
	}
	if out.Creator, err = fc.userCond(f.Creator); err != nil {
		return out, err
	}
	if f.State != nil {
		ids, err := fc.stateIDs(f.State)
		if err != nil {
			return out, err
		}
		out.State = &issue.IDCond{In: ids}
	}
	if f.Team != nil {
		ids, err := fc.r.teamIDs(fc.ctx, f.Team)
		if err != nil {
			return out, err
		}
		out.Project = &issue.IDCond{In: ids}
	}
	if f.Labels != nil {
		if out.Label, err = fc.labelCond(f.Labels); err != nil {
			return out, err
		}
	}
	if f.Parent != nil {
		if out.Parent, err = idCond("id", f.Parent.ID); err != nil {
			return out, err
		}
		if f.Parent.Null != nil {
			if out.Parent == nil {
				out.Parent = &issue.IDCond{}
			}
			out.Parent.Null = f.Parent.Null
		}
	}
	return out, nil
}

func idCond(field string, c *IDComparator) (*issue.IDCond, error) {
	if c == nil {
		return nil, nil
	}
	var out issue.IDCond
	var err error
	for _, p := range []struct {
		src *string
		dst **uuid.UUID
	}{{c.Eq, &out.Eq}, {c.Neq, &out.Neq}} {
		if p.src != nil {
			id, err := parseID(field, *p.src)
			if err != nil {
				return nil, err
			}
			*p.dst = &id
		}
	}
	if c.In != nil {
		if out.In, err = parseIDs(field, c.In); err != nil {
			return nil, err
		}
	}
	if c.Nin != nil {
		if out.Nin, err = parseIDs(field, c.Nin); err != nil {
			return nil, err
		}
	}
	return &out, nil
}

func whole(field string, v float64) (int, error) {
	if v != math.Trunc(v) || math.Abs(v) > math.MaxInt32 {
		return 0, badArg(field, "Invalid "+field+": must be a whole number")
	}
	return int(v), nil
}

func intCond(field string, c *NumberComparator) (*issue.IntCond, error) {
	if c == nil {
		return nil, nil
	}
	var out issue.IntCond
	for _, p := range []struct {
		src *float64
		dst **int
	}{{c.Eq, &out.Eq}, {c.Neq, &out.Neq}, {c.Lt, &out.Lt}, {c.Lte, &out.Lte}, {c.Gt, &out.Gt}, {c.Gte, &out.Gte}} {
		if p.src != nil {
			n, err := whole(field, *p.src)
			if err != nil {
				return nil, err
			}
			*p.dst = &n
		}
	}
	for _, p := range []struct {
		src []float64
		dst *[]int
	}{{c.In, &out.In}, {c.Nin, &out.Nin}} {
		if p.src == nil {
			continue
		}
		*p.dst = make([]int, len(p.src))
		for i, v := range p.src {
			n, err := whole(field, v)
			if err != nil {
				return nil, err
			}
			(*p.dst)[i] = n
		}
	}
	return &out, nil
}

func textCond(c *StringComparator) *issue.TextCond {
	if c == nil {
		return nil
	}
	return &issue.TextCond{
		Eq: c.Eq, Neq: c.Neq, EqFold: c.EqIgnoreCase, NeqFold: c.NeqIgnoreCase,
		Contains: c.Contains, NotContains: c.NotContains, ContainsFold: c.ContainsIgnoreCase, NotContainsFold: c.NotContainsIgnoreCase,
		StartsWith: c.StartsWith, StartsWithFold: c.StartsWithIgnoreCase, NotStartsWith: c.NotStartsWith,
		EndsWith: c.EndsWith, NotEndsWith: c.NotEndsWith, In: c.In, Nin: c.Nin,
	}
}

func (o dateOps) empty() bool {
	return o.eq == nil && o.neq == nil && o.lt == nil && o.lte == nil && o.gt == nil && o.gte == nil && o.in == nil && o.nin == nil && o.null == nil
}

func (fc filterCtx) timeCond(field string, o dateOps) (*issue.TimeCond, error) {
	if o.empty() {
		return nil, nil
	}
	c, err := o.parse(field, func(f, s string) (time.Time, error) { return parseDateOrDuration(f, s, fc.now) })
	if err != nil {
		return nil, err
	}
	return &issue.TimeCond{Eq: c.eq, Neq: c.neq, Lt: c.lt, Lte: c.lte, Gt: c.gt, Gte: c.gte, In: c.in, Nin: c.nin, Null: c.null}, nil
}

func (fc filterCtx) dateCond(o dateOps) (*issue.DateCond, error) {
	if o.empty() {
		return nil, nil
	}
	c, err := o.parse("dueDate", func(f, s string) (time.Time, error) { return parseTimelessOrDuration(f, s, fc.now) })
	if err != nil {
		return nil, err
	}
	return &issue.DateCond{Eq: c.eq, Neq: c.neq, Lt: c.lt, Lte: c.lte, Gt: c.gt, Gte: c.gte, In: c.in, Nin: c.nin, Null: c.null}, nil
}

// userFilterOf drops null from a nullable user filter (and its groups).
func userFilterOf(f *NullableUserFilter) *UserFilter {
	if f == nil {
		return nil
	}
	out := &UserFilter{
		ID: f.ID, Name: f.Name, DisplayName: f.DisplayName, Email: f.Email,
		Active: f.Active, Admin: f.Admin, Owner: f.Owner, IsMe: f.IsMe,
	}
	for _, s := range f.And {
		out.And = append(out.And, *userFilterOf(&s))
	}
	for _, s := range f.Or {
		out.Or = append(out.Or, *userFilterOf(&s))
	}
	return out
}

func (f *NullableUserFilter) hasConds() bool {
	return f.And != nil || f.Or != nil || f.ID != nil || f.Name != nil || f.DisplayName != nil || f.Email != nil ||
		f.Active != nil || f.Admin != nil || f.Owner != nil || f.IsMe != nil
}

// userCond: null tests for no user; other conditions match the users they
// select (AND-ed with null).
func (fc filterCtx) userCond(f *NullableUserFilter) (*issue.IDCond, error) {
	if f == nil {
		return nil, nil
	}
	out := &issue.IDCond{Null: f.Null}
	if f.hasConds() {
		users, err := loadersFrom(fc.ctx).allUsers.get(fc.ctx, func(ctx context.Context) ([]account.User, error) {
			return fc.r.d.Accounts.ListMembers(ctx, account.User{})
		})
		if err != nil {
			return nil, err
		}
		uf := userFilterOf(f)
		out.In = []uuid.UUID{}
		for _, u := range users {
			ok, err := userMatch(uf, u, fc.me)
			if err != nil {
				return nil, err
			}
			if ok {
				out.In = append(out.In, u.ID)
			}
		}
	}
	return out, nil
}

// stateIDs are the states (of all teams) matching f.
func (fc filterCtx) stateIDs(f *WorkflowStateFilter) ([]uuid.UUID, error) {
	ps, err := fc.r.everyProject(fc.ctx)
	if err != nil {
		return nil, err
	}
	states, err := loadersFrom(fc.ctx).allStates.get(fc.ctx, func(ctx context.Context) ([]project.State, error) {
		ids := make([]uuid.UUID, len(ps))
		for i, p := range ps {
			ids[i] = p.ID
		}
		byProject, err := fc.r.d.Projects.StatesByProjects(ctx, ids)
		if err != nil {
			return nil, err
		}
		var out []project.State
		for _, id := range ids {
			out = append(out, byProject[id]...)
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	teams := teamIndex(ps...)
	out := []uuid.UUID{}
	for _, s := range states {
		ok, err := stateMatch(f, s, teams)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, s.ID)
		}
	}
	return out, nil
}

// issueOrder maps orderBy and sort (sort wins) to the domain order.
func issueOrder(by *PaginationOrderBy, sort []IssueSortInput) (issue.Order, error) {
	if len(sort) > 1 {
		return issue.Order{}, tooManySortKeys()
	}
	if len(sort) == 1 {
		s := sort[0]
		type key struct {
			field issue.OrderField
			nulls *PaginationNulls
			order *PaginationSortOrder
		}
		var keys []key
		if s.Priority != nil {
			if s.Priority.NoPriorityFirst != nil && *s.Priority.NoPriorityFirst {
				return issue.Order{}, badArg("sort", "Invalid sort: noPriorityFirst is not supported")
			}
			keys = append(keys, key{issue.PriorityNoneLast, s.Priority.Nulls, s.Priority.Order})
		}
		if s.CreatedAt != nil {
			keys = append(keys, key{issue.CreatedAt, s.CreatedAt.Nulls, s.CreatedAt.Order})
		}
		if s.UpdatedAt != nil {
			keys = append(keys, key{issue.UpdatedAt, s.UpdatedAt.Nulls, s.UpdatedAt.Order})
		}
		if s.DueDate != nil {
			keys = append(keys, key{issue.DueDate, s.DueDate.Nulls, s.DueDate.Order})
		}
		if s.Manual != nil {
			keys = append(keys, key{issue.SortOrder, s.Manual.Nulls, s.Manual.Order})
		}
		if len(keys) > 1 {
			return issue.Order{}, tooManySortKeys()
		}
		if len(keys) == 1 {
			desc, err := sortKey(keys[0].nulls, keys[0].order)
			return issue.Order{Field: keys[0].field, Desc: desc}, err
		}
	}
	if by != nil && *by == PaginationOrderByUpdatedAt {
		return issue.Order{Field: issue.UpdatedAt, Desc: true}, nil
	}
	return issue.Order{Field: issue.CreatedAt, Desc: true}, nil
}

// labelCond: issues with a label matching all of id, name and some; null
// tests for no labels.
func (fc filterCtx) labelCond(c *IssueLabelCollectionFilter) (*issue.LabelCond, error) {
	out := &issue.LabelCond{Null: c.Null}
	if c.ID != nil || c.Name != nil || c.Some != nil {
		f := &IssueLabelFilter{ID: c.ID, Name: c.Name}
		if c.Some != nil {
			f.And = []IssueLabelFilter{*c.Some}
		}
		ids, err := fc.r.labelIDs(fc.ctx, f)
		if err != nil {
			return nil, err
		}
		out.In = ids
	}
	return out, nil
}
