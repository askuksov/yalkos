package issue

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/project"
)

// IDCond compares a UUID column. In: [] matches nothing.
type IDCond struct {
	Eq, Neq *uuid.UUID
	In, Nin []uuid.UUID
	Null    *bool
}

// IntCond compares an integer column.
type IntCond struct {
	Eq, Neq, Lt, Lte, Gt, Gte *int
	In, Nin                   []int
}

// DateCond compares a date column.
type DateCond struct {
	Eq, Lt, Lte, Gt, Gte *time.Time
	Null                 *bool
}

// StateTypeCond compares the issue's state type.
type StateTypeCond struct {
	Eq      *project.StateType
	In, Nin []project.StateType
}

// Filter selects issues; all set conditions are AND-ed.
type Filter struct {
	Project, State, Assignee, Parent *IDCond
	StateType                        *StateTypeCond
	Priority                         *IntCond
	DueDate                          *DateCond
	HasChildren                      *bool
	// IncludeArchived lists issues of archived projects; they are also listed
	// when Project.Eq or Project.In is set.
	IncludeArchived bool
}

// where collects SQL conditions with numbered placeholders. Column names come
// only from code; every value goes through args.
type where struct {
	conds []string
	args  []any
}

func (w *where) arg(v any) string {
	w.args = append(w.args, v)
	return fmt.Sprintf("$%d", len(w.args))
}

func (w *where) add(format string, vals ...any) {
	ph := make([]any, len(vals))
	for i, v := range vals {
		ph[i] = w.arg(v)
	}
	w.conds = append(w.conds, fmt.Sprintf(format, ph...))
}

func (w *where) sql() string {
	if len(w.conds) == 0 {
		return "TRUE"
	}
	return strings.Join(w.conds, " AND ")
}

// id adds conditions on a nullable or non-null uuid column. neq/nin keep NULLs
// ("not assigned to X" includes unassigned issues).
func (w *where) id(col string, c *IDCond) {
	if c == nil {
		return
	}
	if c.Eq != nil {
		w.add(col+" = %s", *c.Eq)
	}
	if c.Neq != nil {
		w.add(col+" IS DISTINCT FROM %s", *c.Neq)
	}
	if c.In != nil {
		w.add(col+" = ANY(%s::uuid[])", c.In)
	}
	if len(c.Nin) > 0 {
		w.add("("+col+" IS NULL OR "+col+" <> ALL(%s::uuid[]))", c.Nin)
	}
	if c.Null != nil {
		if *c.Null {
			w.conds = append(w.conds, col+" IS NULL")
		} else {
			w.conds = append(w.conds, col+" IS NOT NULL")
		}
	}
}

func (w *where) intCond(col string, c *IntCond) {
	if c == nil {
		return
	}
	for _, op := range []struct {
		v  *int
		op string
	}{{c.Eq, "="}, {c.Neq, "<>"}, {c.Lt, "<"}, {c.Lte, "<="}, {c.Gt, ">"}, {c.Gte, ">="}} {
		if op.v != nil {
			w.add(col+" "+op.op+" %s", *op.v)
		}
	}
	if c.In != nil {
		w.add(col+" = ANY(%s::int[])", c.In)
	}
	if len(c.Nin) > 0 {
		w.add(col+" <> ALL(%s::int[])", c.Nin)
	}
}

func (w *where) dateCond(col string, c *DateCond) {
	if c == nil {
		return
	}
	for _, op := range []struct {
		v  *time.Time
		op string
	}{{c.Eq, "="}, {c.Lt, "<"}, {c.Lte, "<="}, {c.Gt, ">"}, {c.Gte, ">="}} {
		if op.v != nil {
			w.add(col+" "+op.op+" %s::date", op.v.Format(time.DateOnly))
		}
	}
	if c.Null != nil {
		if *c.Null {
			w.conds = append(w.conds, col+" IS NULL")
		} else {
			w.conds = append(w.conds, col+" IS NOT NULL")
		}
	}
}

func typeStrings(ts []project.StateType) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = string(t)
	}
	return out
}

func (w *where) stateType(col string, c *StateTypeCond) {
	if c == nil {
		return
	}
	if c.Eq != nil {
		w.add(col+" = %s", string(*c.Eq))
	}
	if c.In != nil {
		w.add(col+" = ANY(%s::text[])", typeStrings(c.In))
	}
	if len(c.Nin) > 0 {
		w.add(col+" <> ALL(%s::text[])", typeStrings(c.Nin))
	}
}

// build turns the filter into conditions on issues i, projects p, states st.
func (f Filter) build(w *where) {
	w.conds = append(w.conds, "i.deleted_at IS NULL")
	w.id("i.project_id", f.Project)
	w.id("i.state_id", f.State)
	w.id("i.assignee_id", f.Assignee)
	w.id("i.parent_id", f.Parent)
	w.stateType("st.type", f.StateType)
	w.intCond("i.priority", f.Priority)
	w.dateCond("i.due_date", f.DueDate)
	if f.HasChildren != nil {
		exists := "EXISTS (SELECT 1 FROM issues c WHERE c.parent_id = i.id AND c.deleted_at IS NULL)"
		if !*f.HasChildren {
			exists = "NOT " + exists
		}
		w.conds = append(w.conds, exists)
	}
	explicitProject := f.Project != nil && (f.Project.Eq != nil || f.Project.In != nil)
	if !f.IncludeArchived && !explicitProject {
		w.conds = append(w.conds, "p.archived_at IS NULL")
	}
}
