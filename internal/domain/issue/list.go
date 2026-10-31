package issue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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
	i.completed_at, i.canceled_at, i.deleted_at, p.key`

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

// List returns live issues matching f in order o, one Relay page at a time.
func (s *Service) List(ctx context.Context, f Filter, o Order, a page.Args) (page.Result[Issue], error) {
	w, err := a.Window()
	if err != nil {
		return page.Result[Issue]{}, err
	}
	if o.Field == "" {
		o.Field = SortOrder
	}
	spec, ok := orders[o.Field]
	if !ok {
		return page.Result[Issue]{}, kit.Input("orderBy", kit.ReasonInvalidFormat, "unknown order field")
	}
	dir := "asc"
	if o.Desc {
		dir = "desc"
	}
	var wh where
	f.build(&wh)
	key := spec.key(o.Desc)
	// Query direction: backward pages read the order in reverse.
	asc := w.Forward != o.Desc
	sqlDir, cmp := "ASC", ">"
	if !asc {
		sqlDir, cmp = "DESC", "<"
	}
	if w.Cursor != nil {
		field := "after"
		if !w.Forward {
			field = "before"
		}
		c, err := page.Decode(*w.Cursor, field)
		if err != nil {
			return page.Result[Issue]{}, err
		}
		bad := kit.Input(field, kit.ReasonInvalidFormat, "cursor belongs to another order")
		if c.F != string(o.Field) || c.D != dir {
			return page.Result[Issue]{}, bad
		}
		v, err := spec.decode(c.V)
		if err != nil {
			return page.Result[Issue]{}, bad
		}
		wh.add(fmt.Sprintf("(%s, i.id) %s (%%s::%s, %%s::uuid)", key, cmp, spec.cast), v, c.ID)
	}
	sql := fmt.Sprintf(`SELECT %s, %s
FROM issues i JOIN projects p ON p.id = i.project_id JOIN states st ON st.id = i.state_id
WHERE %s
ORDER BY %s %s, i.id %s
LIMIT %s`, issueColumns, spec.out(o.Desc), wh.sql(), key, sqlDir, sqlDir, wh.arg(w.Limit+1))

	rows, err := s.pool.Query(ctx, sql, wh.args...)
	if err != nil {
		return page.Result[Issue]{}, fmt.Errorf("list issues: %w", err)
	}
	listed, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (listedIssue, error) {
		var i q.Issue
		var projectKey string
		var k any
		err := r.Scan(&i.ID, &i.ProjectID, &i.Number, &i.Title, &i.Description, &i.StateID, &i.Priority,
			&i.AssigneeID, &i.CreatorID, &i.ParentID, &i.DueDate, &i.SortOrder, &i.CreatedAt, &i.UpdatedAt,
			&i.CompletedAt, &i.CanceledAt, &i.DeletedAt, &projectKey, &k)
		return listedIssue{issue: toIssue(i, projectKey), key: k}, err
	})
	if err != nil {
		return page.Result[Issue]{}, fmt.Errorf("list issues: %w", err)
	}
	res := page.Finish(listed, w, func(l listedIssue) string {
		v, _ := json.Marshal(l.key)
		return page.Encode(page.Cursor{F: string(o.Field), D: dir, V: v, ID: l.issue.ID})
	})
	out := page.Result[Issue]{Cursors: res.Cursors, Info: res.Info, Items: make([]Issue, len(res.Items))}
	for i, l := range res.Items {
		out.Items[i] = l.issue
	}
	return out, nil
}
