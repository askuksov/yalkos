package native

import (
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/project"
)

// Filter inputs: explicit null in a comparator means "no condition".

func idCondIn(field string, c *IDComparator) (*issue.IDCond, error) {
	if c == nil {
		return nil, nil
	}
	return idCond(field, c.Eq.Value(), c.Neq.Value(), c.In.Value(), c.Nin.Value(), c.Null.Value())
}

func idCond(field string, eq, neq *string, in, nin []string, null *bool) (*issue.IDCond, error) {
	var out issue.IDCond
	var err error
	if out.Eq, err = parseOptID(field+".eq", eq); err != nil {
		return nil, err
	}
	if out.Neq, err = parseOptID(field+".neq", neq); err != nil {
		return nil, err
	}
	if in != nil {
		if out.In, err = parseIDs(field+".in", in); err != nil {
			return nil, err
		}
	}
	if nin != nil {
		if out.Nin, err = parseIDs(field+".nin", nin); err != nil {
			return nil, err
		}
	}
	out.Null = null
	return &out, nil
}

// userCondIn adds isMe to the id conditions with AND: true → assigned to me,
// false → not assigned to me (unassigned included, like nin).
func userCondIn(c *UserComparator, me uuid.UUID) (*issue.IDCond, error) {
	if c == nil {
		return nil, nil
	}
	out, err := idCond("filter.assignee", c.Eq.Value(), c.Neq.Value(), c.In.Value(), c.Nin.Value(), c.Null.Value())
	if err != nil {
		return nil, err
	}
	if isMe := c.IsMe.Value(); isMe != nil {
		switch {
		case !*isMe:
			out.Nin = append(out.Nin, me)
		case out.Eq == nil:
			out.Eq = &me
		case *out.Eq != me:
			out.In = []uuid.UUID{} // eq someone else AND me: nothing
		}
	}
	return out, nil
}

func stateTypeCondIn(c *StateTypeComparator) *issue.StateTypeCond {
	if c == nil {
		return nil
	}
	conv := func(in []StateType) []project.StateType {
		if in == nil {
			return nil
		}
		out := make([]project.StateType, 0, len(in))
		for _, t := range in {
			out = append(out, stateTypeIn(t))
		}
		return out
	}
	out := &issue.StateTypeCond{In: conv(c.In.Value()), Nin: conv(c.Nin.Value())}
	if eq := c.Eq.Value(); eq != nil {
		t := stateTypeIn(*eq)
		out.Eq = &t
	}
	return out
}

func intCondIn(c *IntComparator) *issue.IntCond {
	if c == nil {
		return nil
	}
	return &issue.IntCond{
		Eq: c.Eq.Value(), Neq: c.Neq.Value(), Lt: c.Lt.Value(), Lte: c.Lte.Value(), Gt: c.Gt.Value(), Gte: c.Gte.Value(),
		In: c.In.Value(), Nin: c.Nin.Value(),
	}
}

func optIDs(field string, ids []string) ([]uuid.UUID, error) {
	if ids == nil {
		return nil, nil
	}
	return parseIDs(field, ids)
}

func labelCondIn(c *LabelComparator) (*issue.LabelCond, error) {
	if c == nil {
		return nil, nil
	}
	in, err := optIDs("filter.label.in", c.In.Value())
	if err != nil {
		return nil, err
	}
	nin, err := optIDs("filter.label.nin", c.Nin.Value())
	if err != nil {
		return nil, err
	}
	return &issue.LabelCond{In: in, Nin: nin, Null: c.Null.Value()}, nil
}

func typeCondIn(c *TypeComparator) (*issue.TypeCond, error) {
	if c == nil {
		return nil, nil
	}
	in, err := optIDs("filter.type.in", c.In.Value())
	if err != nil {
		return nil, err
	}
	return &issue.TypeCond{In: in, Null: c.Null.Value()}, nil
}

func dateOf(d *Date) *time.Time {
	if d == nil {
		return nil
	}
	t := time.Time(*d)
	return &t
}

func dateCondIn(c *DateComparator) *issue.DateCond {
	if c == nil {
		return nil
	}
	return &issue.DateCond{
		Eq: dateOf(c.Eq.Value()), Lt: dateOf(c.Lt.Value()), Lte: dateOf(c.Lte.Value()),
		Gt: dateOf(c.Gt.Value()), Gte: dateOf(c.Gte.Value()), Null: c.Null.Value(),
	}
}

func issueFilterIn(f *IssueFilter, me uuid.UUID) (issue.Filter, error) {
	var out issue.Filter
	if f == nil {
		return out, nil
	}
	var err error
	if out.Project, err = idCondIn("filter.project", f.Project.Value()); err != nil {
		return out, err
	}
	if out.State, err = idCondIn("filter.state", f.State.Value()); err != nil {
		return out, err
	}
	if out.Parent, err = idCondIn("filter.parent", f.Parent.Value()); err != nil {
		return out, err
	}
	if out.Assignee, err = userCondIn(f.Assignee.Value(), me); err != nil {
		return out, err
	}
	if out.Milestone, err = idCondIn("filter.milestone", f.Milestone.Value()); err != nil {
		return out, err
	}
	if out.Label, err = labelCondIn(f.Label.Value()); err != nil {
		return out, err
	}
	if out.Type, err = typeCondIn(f.Type.Value()); err != nil {
		return out, err
	}
	out.StateType = stateTypeCondIn(f.StateType.Value())
	out.Priority = intCondIn(f.Priority.Value())
	out.DueDate = dateCondIn(f.DueDate.Value())
	out.HasChildren = f.HasChildren.Value()
	out.Search = f.Search.Value()
	if v := f.IncludeArchived.Value(); v != nil {
		out.IncludeArchived = *v
	}
	return out, nil
}

var orderFields = map[IssueOrderField]issue.OrderField{
	IssueOrderFieldSortOrder: issue.SortOrder,
	IssueOrderFieldCreatedAt: issue.CreatedAt,
	IssueOrderFieldUpdatedAt: issue.UpdatedAt,
	IssueOrderFieldPriority:  issue.Priority,
	IssueOrderFieldDueDate:   issue.DueDate,
}

func issueOrderIn(o *IssueOrder) issue.Order {
	if o == nil {
		return issue.Order{Field: issue.SortOrder}
	}
	d := o.Direction.Value()
	return issue.Order{Field: orderFields[o.Field], Desc: d != nil && *d == OrderDirectionDesc}
}
