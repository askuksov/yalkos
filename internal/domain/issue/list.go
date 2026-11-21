package issue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/store/q"
)

// OrderField is an issue list order.
type OrderField string

// Issue orders; ties break by id in the same direction.
const (
	SortOrder OrderField = "sortOrder"
	CreatedAt OrderField = "createdAt"
	UpdatedAt OrderField = "updatedAt"
	Priority  OrderField = "priority"
	DueDate   OrderField = "dueDate"
	// PriorityNoneLast is Priority with no priority (0) last in both directions.
	PriorityNoneLast OrderField = "priorityNoneLast"
)

// Order is a list order; the zero value is SortOrder ascending.
type Order struct {
	Field OrderField
	Desc  bool
}

// orderSpec describes a sort key: its SQL expression (per direction), the
// expression selected for the cursor, the cast for the cursor parameter and
// how to decode the cursor value.
type orderSpec struct {
	key    func(desc bool) string
	out    func(desc bool) string
	cast   string
	decode func(json.RawMessage) (any, error)
}

func same(expr string) func(bool) string { return func(bool) string { return expr } }

func decodeAs[T any](raw json.RawMessage) (any, error) {
	var v T
	err := json.Unmarshal(raw, &v)
	return v, err
}

var orders = map[OrderField]orderSpec{
	SortOrder: {key: same("i.sort_order"), out: same("i.sort_order"), cast: "float8", decode: decodeAs[float64]},
	CreatedAt: {key: same("i.created_at"), out: same("i.created_at"), cast: "timestamptz", decode: decodeAs[time.Time]},
	UpdatedAt: {key: same("i.updated_at"), out: same("i.updated_at"), cast: "timestamptz", decode: decodeAs[time.Time]},
	// No priority (0) sorts after low (4) ascending.
	Priority: {
		key:  same("(CASE i.priority WHEN 0 THEN 5 ELSE i.priority END)"),
		out:  same("(CASE i.priority WHEN 0 THEN 5 ELSE i.priority END)::int"),
		cast: "int", decode: decodePriorityKey,
	},
	PriorityNoneLast: {
		key:  priorityNoneLastKey,
		out:  func(desc bool) string { return priorityNoneLastKey(desc) + "::int" },
		cast: "int", decode: decodePriorityNoneLastKey,
	},
	// Issues without a due date come last in both directions.
	DueDate: {
		key:  dueKey,
		out:  func(desc bool) string { return dueKey(desc) + "::text" },
		cast: "date", decode: decodeDueKey,
	},
}

// decodePriorityKey accepts only sort keys the priority order produces (1–5).
func decodePriorityKey(raw json.RawMessage) (any, error) {
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	if v < 1 || v > 5 {
		return nil, fmt.Errorf("priority key %d out of range", v)
	}
	return v, nil
}

func priorityNoneLastKey(desc bool) string {
	if desc {
		return "i.priority"
	}
	return "(CASE i.priority WHEN 0 THEN 5 ELSE i.priority END)"
}

// decodePriorityNoneLastKey accepts the keys of both directions (0–5).
func decodePriorityNoneLastKey(raw json.RawMessage) (any, error) {
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	if v < 0 || v > 5 {
		return nil, fmt.Errorf("priority key %d out of range", v)
	}
	return v, nil
}

// decodeDueKey accepts a YYYY-MM-DD date or ±infinity, the values dueKey produces.
func decodeDueKey(raw json.RawMessage) (any, error) {
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	if v == "infinity" || v == "-infinity" {
		return v, nil
	}
	if _, err := time.Parse(time.DateOnly, v); err != nil {
		return nil, err
	}
	return v, nil
}

func dueKey(desc bool) string {
	if desc {
		return "COALESCE(i.due_date, '-infinity'::date)"
	}
	return "COALESCE(i.due_date, 'infinity'::date)"
}

const issueColumns = `i.id, i.project_id, i.number, i.title, i.description, i.state_id, i.priority,
	i.assignee_id, i.creator_id, i.parent_id, i.due_date, i.sort_order, i.created_at, i.updated_at,
	i.completed_at, i.canceled_at, i.deleted_at, i.milestone_id, p.key`

// Count returns the number of live issues matching f.
func (s *Service) Count(ctx context.Context, f Filter) (int, error) {
	var wh where
	f.build(&wh)
	sql := `SELECT count(*) FROM issues i JOIN projects p ON p.id = i.project_id JOIN states st ON st.id = i.state_id WHERE ` + wh.sql()
	var n int
	if err := s.pool.QueryRow(ctx, sql, wh.args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count issues: %w", err)
	}
	return n, nil
}

type listedIssue struct {
	issue Issue
	key   any
}

// listPlan is a checked list request: its order, sort key and page window,
// with the cursor (if any) decoded.
type listPlan struct {
	o      Order
	spec   orderSpec
	dir    string // "asc" or "desc": the order's direction, kept in cursors
	w      page.Window
	key    string
	sqlDir string // the query's direction: backward pages read the order in reverse
	cmp    string
	// The cursor's sort key and id, when the page starts at a cursor.
	cursorKey any
	cursorID  *uuid.UUID
}

func planList(o Order, a page.Args) (listPlan, error) {
	w, err := a.Window()
	if err != nil {
		return listPlan{}, err
	}
	if o.Field == "" {
		o.Field = SortOrder
	}
	spec, ok := orders[o.Field]
	if !ok {
		return listPlan{}, kit.Input("orderBy", kit.ReasonInvalidFormat, "unknown order field")
	}
	p := listPlan{o: o, spec: spec, dir: "asc", w: w, key: spec.key(o.Desc), sqlDir: "ASC", cmp: ">"}
	if o.Desc {
		p.dir = "desc"
	}
	if w.Forward == o.Desc {
		p.sqlDir, p.cmp = "DESC", "<"
	}
	if w.Cursor != nil {
		field := "after"
		if !w.Forward {
			field = "before"
		}
		c, err := page.Decode(*w.Cursor, field)
		if err != nil {
			return listPlan{}, err
		}
		bad := kit.Input(field, kit.ReasonInvalidFormat, "cursor belongs to another order")
		if c.F != string(o.Field) || c.D != p.dir {
			return listPlan{}, bad
		}
		v, err := spec.decode(c.V)
		if err != nil {
			return listPlan{}, bad
		}
		p.cursorKey, p.cursorID = v, &c.ID
	}
	return p, nil
}

// cursorCond adds the condition that starts the page after (or before) the cursor.
func (p listPlan) cursorCond(wh *where) {
	if p.cursorID != nil {
		wh.add(fmt.Sprintf("(%s, i.id) %s (%%s::%s, %%s::uuid)", p.key, p.cmp, p.spec.cast), p.cursorKey, *p.cursorID)
	}
}

// selectSQL is the query of one page of the issues that wh matches, with the
// sort key last.
func (p listPlan) selectSQL(wh *where) string {
	return fmt.Sprintf(`SELECT %s, %s
FROM issues i JOIN projects p ON p.id = i.project_id JOIN states st ON st.id = i.state_id
WHERE %s
ORDER BY %s %s, i.id %s
LIMIT %s`, issueColumns, p.spec.out(p.o.Desc), wh.sql(), p.key, p.sqlDir, p.sqlDir, wh.arg(p.w.Limit+1))
}

// finish turns the rows of one page (read with one row extra) into the result.
func (p listPlan) finish(listed []listedIssue) page.Result[Issue] {
	res := page.Finish(listed, p.w, func(l listedIssue) string {
		v, _ := json.Marshal(l.key)
		return page.Encode(page.Cursor{F: string(p.o.Field), D: p.dir, V: v, ID: l.issue.ID})
	})
	out := page.Result[Issue]{Cursors: res.Cursors, Info: res.Info, Items: make([]Issue, len(res.Items))}
	for i, l := range res.Items {
		out.Items[i] = l.issue
	}
	return out
}

// scanListed reads an issue and its sort key, after the columns in lead.
func scanListed(r pgx.CollectableRow, lead ...any) (listedIssue, error) {
	var i q.Issue
	var projectKey string
	var k any
	dst := make([]any, 0, len(lead)+20)
	dst = append(dst, lead...)
	dst = append(dst, &i.ID, &i.ProjectID, &i.Number, &i.Title, &i.Description, &i.StateID, &i.Priority,
		&i.AssigneeID, &i.CreatorID, &i.ParentID, &i.DueDate, &i.SortOrder, &i.CreatedAt, &i.UpdatedAt,
		&i.CompletedAt, &i.CanceledAt, &i.DeletedAt, &i.MilestoneID, &projectKey, &k)
	err := r.Scan(dst...)
	return listedIssue{issue: toIssue(i, projectKey), key: k}, err
}

// List returns live issues matching f in order o, one Relay page at a time.
func (s *Service) List(ctx context.Context, f Filter, o Order, a page.Args) (page.Result[Issue], error) {
	p, err := planList(o, a)
	if err != nil {
		return page.Result[Issue]{}, err
	}
	var wh where
	f.build(&wh)
	p.cursorCond(&wh)
	rows, err := s.pool.Query(ctx, p.selectSQL(&wh), wh.args...)
	if err != nil {
		return page.Result[Issue]{}, fmt.Errorf("list issues: %w", err)
	}
	listed, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (listedIssue, error) { return scanListed(r) })
	if err != nil {
		return page.Result[Issue]{}, fmt.Errorf("list issues: %w", err)
	}
	return p.finish(listed), nil
}

// ChildrenOf returns one page of the live sub-issues of each parent (archived
// projects included), as List does for a Parent filter, in one statement: the
// order, the page size and the cursor apply to each parent on its own. Every
// given parent has an entry, empty when it has no sub-issues.
func (s *Service) ChildrenOf(ctx context.Context, parents []uuid.UUID, o Order, a page.Args) (map[uuid.UUID]page.Result[Issue], error) {
	p, err := planList(o, a)
	if err != nil {
		return nil, err
	}
	ids := kit.Distinct(parents)
	var wh where
	parentsArg := wh.arg(ids)
	Filter{IncludeArchived: true}.build(&wh)
	wh.conds = append(wh.conds, "i.parent_id = par.id")
	p.cursorCond(&wh)
	sql := fmt.Sprintf("SELECT par.id, x.* FROM unnest(%s::uuid[]) AS par (id) CROSS JOIN LATERAL (%s) x", parentsArg, p.selectSQL(&wh))
	rows, err := s.pool.Query(ctx, sql, wh.args...)
	if err != nil {
		return nil, fmt.Errorf("list sub-issues: %w", err)
	}
	type parented struct {
		parent uuid.UUID
		listed listedIssue
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (parented, error) {
		var out parented
		var err error
		out.listed, err = scanListed(r, &out.parent)
		return out, err
	})
	if err != nil {
		return nil, fmt.Errorf("list sub-issues: %w", err)
	}
	byParent := make(map[uuid.UUID][]listedIssue, len(ids))
	for _, r := range all {
		byParent[r.parent] = append(byParent[r.parent], r.listed)
	}
	out := make(map[uuid.UUID]page.Result[Issue], len(ids))
	for _, id := range ids {
		out[id] = p.finish(byParent[id])
	}
	return out, nil
}

// CountByProjects counts live issues per project (archived projects
// included); projects without issues are absent from the map.
func (s *Service) CountByProjects(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]int, error) {
	rows, err := s.q.CountIssuesByProjects(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("count issues by projects: %w", err)
	}
	out := make(map[uuid.UUID]int, len(rows))
	for _, r := range rows {
		out[r.ProjectID] = int(r.N)
	}
	return out, nil
}

// CountByCreators counts live issues per creator; users without issues are
// absent from the map.
func (s *Service) CountByCreators(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]int, error) {
	rows, err := s.q.CountIssuesByCreators(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("count issues by creators: %w", err)
	}
	out := make(map[uuid.UUID]int, len(rows))
	for _, r := range rows {
		out[r.CreatorID] = int(r.N)
	}
	return out, nil
}
