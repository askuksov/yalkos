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

// DateCond compares a date column. Neq and Nin keep rows without a date.
type DateCond struct {
	Eq, Neq, Lt, Lte, Gt, Gte *time.Time
	In, Nin                   []time.Time
	Null                      *bool
}

// TextCond compares a text column. Fold operators ignore case. Negative
// operators (Neq, NeqFold, Nin, Not*) keep rows without a value.
type TextCond struct {
	Eq, Neq, EqFold, NeqFold                  *string
	Contains, NotContains                     *string
	ContainsFold, NotContainsFold             *string
	StartsWith, StartsWithFold, NotStartsWith *string
	EndsWith, NotEndsWith                     *string
	In, Nin                                   []string
	Null                                      *bool
}

// TimeCond compares a timestamp column. Neq and Nin keep rows without a value.
type TimeCond struct {
	Eq, Neq, Lt, Lte, Gt, Gte *time.Time
	In, Nin                   []time.Time
	Null                      *bool
}

// LabelCond matches labels: In = any of, Nin = none of, Null true = no labels.
type LabelCond struct {
	In, Nin []uuid.UUID
	Null    *bool
}

// TypeCond matches the issue type: In = one of these types, Null true = no type.
type TypeCond struct {
	In   []uuid.UUID
	Null *bool
}

// StateTypeCond compares the issue's state type.
type StateTypeCond struct {
	Eq      *project.StateType
	In, Nin []project.StateType
}

// Filter selects issues; all set conditions are AND-ed.
type Filter struct {
	ID, Creator                      *IDCond
	Project, State, Assignee, Parent *IDCond
	Milestone                        *IDCond
	Label                            *LabelCond
	Type                             *TypeCond
	StateType                        *StateTypeCond
	Number, Priority                 *IntCond
	Title, Description               *TextCond
	CreatedAt, UpdatedAt             *TimeCond
	CompletedAt, CanceledAt          *TimeCond
	DueDate                          *DateCond
	HasChildren                      *bool
	// Search matches words of the title and description by prefix; an
	// identifier also matches that issue. Input without words matches all.
	Search *string
	// All adds each sub-filter's conditions; Any matches when at least one
	// sub-filter does (empty Any adds nothing). Sub-filters never decide on
	// deleted or archived rows: their IncludeArchived is ignored.
	All, Any []Filter
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
			// bigint (here and in In/Nin): an untyped or int parameter fails
			// to encode large values.
			w.add(col+" "+op.op+" %s::bigint", *op.v)
		}
	}
	if c.In != nil {
		w.add(col+" = ANY(%s::bigint[])", c.In)
	}
	if len(c.Nin) > 0 {
		w.add(col+" <> ALL(%s::bigint[])", c.Nin)
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
	if c.Neq != nil {
		w.add("("+col+" IS NULL OR "+col+" <> %s::date)", c.Neq.Format(time.DateOnly))
	}
	if c.In != nil {
		w.add(col+" = ANY(%s::date[])", dateStrings(c.In))
	}
	if len(c.Nin) > 0 {
		w.add("("+col+" IS NULL OR "+col+" <> ALL(%s::date[]))", dateStrings(c.Nin))
	}
	if c.Null != nil {
		if *c.Null {
			w.conds = append(w.conds, col+" IS NULL")
		} else {
			w.conds = append(w.conds, col+" IS NOT NULL")
		}
	}
}

func dateStrings(ts []time.Time) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Format(time.DateOnly)
	}
	return out
}

func (w *where) null(col string, null bool) {
	if null {
		w.conds = append(w.conds, col+" IS NULL")
	} else {
		w.conds = append(w.conds, col+" IS NOT NULL")
	}
}

func (w *where) timeCond(col string, c *TimeCond) {
	if c == nil {
		return
	}
	for _, op := range []struct {
		v  *time.Time
		op string
	}{{c.Eq, "="}, {c.Lt, "<"}, {c.Lte, "<="}, {c.Gt, ">"}, {c.Gte, ">="}} {
		if op.v != nil {
			w.add(col+" "+op.op+" %s::timestamptz", *op.v)
		}
	}
	if c.Neq != nil {
		w.add(col+" IS DISTINCT FROM %s::timestamptz", *c.Neq)
	}
	if c.In != nil {
		w.add(col+" = ANY(%s::timestamptz[])", c.In)
	}
	if len(c.Nin) > 0 {
		w.add("("+col+" IS NULL OR "+col+" <> ALL(%s::timestamptz[]))", c.Nin)
	}
	if c.Null != nil {
		w.null(col, *c.Null)
	}
}

// likeEscaper makes a value match literally inside a LIKE pattern (ESCAPE '\').
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// textCond compares a text column; emptyIsNull treats ” as no value.
func (w *where) textCond(col string, c *TextCond, emptyIsNull bool) {
	if c == nil {
		return
	}
	isNull := col + " IS NULL"
	if emptyIsNull {
		isNull = "(" + col + " IS NULL OR " + col + " = '')"
	}
	pos := func(format string, v any) { w.add(format, v) }
	neg := func(format string, v any) { w.add("("+isNull+" OR "+format+")", v) }
	like := func(prefix, v, suffix string) string { return prefix + likeEscaper.Replace(v) + suffix }
	type op struct {
		v      *string
		add    func(string, any)
		format string
		val    func(string) any
	}
	plain := func(s string) any { return s }
	for _, o := range []op{
		{c.Eq, pos, col + " = %s", plain},
		{c.Neq, neg, col + " <> %s", plain},
		{c.EqFold, pos, "lower(" + col + ") = lower(%s)", plain},
		{c.NeqFold, neg, "lower(" + col + ") <> lower(%s)", plain},
		{c.Contains, pos, col + ` LIKE %s ESCAPE '\'`, func(s string) any { return like("%", s, "%") }},
		{c.NotContains, neg, col + ` NOT LIKE %s ESCAPE '\'`, func(s string) any { return like("%", s, "%") }},
		{c.ContainsFold, pos, col + ` ILIKE %s ESCAPE '\'`, func(s string) any { return like("%", s, "%") }},
		{c.NotContainsFold, neg, col + ` NOT ILIKE %s ESCAPE '\'`, func(s string) any { return like("%", s, "%") }},
		{c.StartsWith, pos, col + ` LIKE %s ESCAPE '\'`, func(s string) any { return like("", s, "%") }},
		{c.StartsWithFold, pos, col + ` ILIKE %s ESCAPE '\'`, func(s string) any { return like("", s, "%") }},
		{c.NotStartsWith, neg, col + ` NOT LIKE %s ESCAPE '\'`, func(s string) any { return like("", s, "%") }},
		{c.EndsWith, pos, col + ` LIKE %s ESCAPE '\'`, func(s string) any { return like("%", s, "") }},
		{c.NotEndsWith, neg, col + ` NOT LIKE %s ESCAPE '\'`, func(s string) any { return like("%", s, "") }},
	} {
		if o.v != nil {
			o.add(o.format, o.val(*o.v))
		}
	}
	if c.In != nil {
		w.add(col+" = ANY(%s::text[])", c.In)
	}
	if len(c.Nin) > 0 {
		neg(col+" <> ALL(%s::text[])", c.Nin)
	}
	if c.Null != nil {
		if *c.Null {
			w.conds = append(w.conds, isNull)
		} else {
			w.conds = append(w.conds, "NOT "+isNull)
		}
	}
}

const (
	hasLabel     = "EXISTS (SELECT 1 FROM issue_labels il WHERE il.issue_id = i.id"
	typeGroupSQL = "(SELECT w.type_group_id FROM workspaces w LIMIT 1)"
	hasType      = "EXISTS (SELECT 1 FROM issue_labels il JOIN labels l ON l.id = il.label_id WHERE il.issue_id = i.id AND l.parent_id = " + typeGroupSQL + ")"
)

func (w *where) labels(c *LabelCond) {
	if c == nil {
		return
	}
	if c.In != nil {
		w.add(hasLabel+" AND il.label_id = ANY(%s::uuid[]))", c.In)
	}
	if len(c.Nin) > 0 {
		w.add("NOT "+hasLabel+" AND il.label_id = ANY(%s::uuid[]))", c.Nin)
	}
	if c.Null != nil {
		if *c.Null {
			w.conds = append(w.conds, "NOT "+hasLabel+")")
		} else {
			w.conds = append(w.conds, hasLabel+")")
		}
	}
}

func (w *where) types(c *TypeCond) {
	if c == nil {
		return
	}
	if c.In != nil {
		w.add(hasLabel+" AND il.label_id = ANY(%s::uuid[]))", c.In)
	}
	if c.Null != nil {
		if *c.Null {
			w.conds = append(w.conds, "NOT "+hasType)
		} else {
			w.conds = append(w.conds, hasType)
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
	f.conds(w)
	explicitProject := f.Project != nil && (f.Project.Eq != nil || f.Project.In != nil)
	if !f.IncludeArchived && !explicitProject {
		w.conds = append(w.conds, "p.archived_at IS NULL")
	}
}

// conds adds the field conditions of f and its groups, without the deleted and
// archived rules. Sub-filters share w's placeholder numbering.
func (f Filter) conds(w *where) {
	w.id("i.id", f.ID)
	w.id("i.creator_id", f.Creator)
	w.intCond("i.number", f.Number)
	w.textCond("i.title", f.Title, false)
	w.textCond("i.description", f.Description, true)
	w.timeCond("i.created_at", f.CreatedAt)
	w.timeCond("i.updated_at", f.UpdatedAt)
	w.timeCond("i.completed_at", f.CompletedAt)
	w.timeCond("i.canceled_at", f.CanceledAt)
	w.id("i.project_id", f.Project)
	w.id("i.state_id", f.State)
	w.id("i.assignee_id", f.Assignee)
	w.id("i.parent_id", f.Parent)
	w.id("i.milestone_id", f.Milestone)
	w.labels(f.Label)
	w.types(f.Type)
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
	if f.Search != nil {
		w.search(*f.Search)
	}
	for _, sub := range f.All {
		sub.conds(w)
	}
	if len(f.Any) > 0 {
		alts := make([]string, len(f.Any))
		for i, sub := range f.Any {
			child := where{args: w.args}
			sub.conds(&child)
			w.args = child.args
			alts[i] = "(" + child.sql() + ")"
		}
		w.conds = append(w.conds, "("+strings.Join(alts, " OR ")+")")
	}
}
