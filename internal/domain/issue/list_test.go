package issue_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/domain/project"
)

type listFixture struct {
	env
	web    project.Project
	all    []issue.Issue // every fixture issue as created/updated
	types  map[uuid.UUID]project.StateType
	done   project.State
	parent issue.Issue
}

func date(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}

// newListFixture: YLK with 20 issues (tied priorities, sort orders and due
// dates, some sub-issues), WEB with 3.
func newListFixture(t *testing.T) listFixture {
	t.Helper()
	e := newEnv(t)
	ctx := context.Background()
	f := listFixture{env: e, web: e.project(t, "WEB"), types: map[uuid.UUID]project.StateType{}}
	for _, p := range []uuid.UUID{e.ylk.ID, f.web.ID} {
		states, err := e.projects.States(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range states {
			f.types[s.ID] = s.Type
		}
	}
	f.done = e.state(t, e.ylk.ID, "Done")
	stateNames := []string{"Done", "Canceled", "Backlog", "In Progress", "Todo", "Todo"}
	dues := []*time.Time{nil, date(2026, 1, 15), date(2026, 3, 1)}
	var ylk []issue.Issue
	for i := range 20 {
		in := issue.CreateInput{
			ProjectID: &e.ylk.ID, Title: fmt.Sprintf("Y%02d", i), Priority: i % 5,
			SortOrder: ptr(float64(i % 4)), DueDate: dues[i%3],
		}
		st := e.state(t, e.ylk.ID, stateNames[i%6])
		in.StateID = &st.ID
		if i%2 == 0 {
			in.AssigneeID = &e.users.Member.ID
		}
		switch i {
		case 5, 6:
			in.ParentID = &ylk[0].ID
		case 7:
			in.ParentID = &ylk[5].ID
		}
		created, err := e.issues.Create(ctx, e.users.Member, in)
		if err != nil {
			t.Fatal(err)
		}
		ylk = append(ylk, created)
	}
	f.parent = ylk[0]
	f.all = append(f.all, ylk...)
	for i := range 3 {
		w, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ProjectID: &f.web.ID, Title: fmt.Sprintf("W%d", i), Priority: 1})
		if err != nil {
			t.Fatal(err)
		}
		f.all = append(f.all, w)
	}
	return f
}

func (f listFixture) list(t *testing.T, flt issue.Filter, o issue.Order, a page.Args) page.Result[issue.Issue] {
	t.Helper()
	r, err := f.issues.List(context.Background(), flt, o, a)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return r
}

func titles(items []issue.Issue) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Title
	}
	return out
}

// check compares List(flt) with the fixture issues matching keep (as sets).
func (f listFixture) check(t *testing.T, name string, flt issue.Filter, keep func(issue.Issue) bool) {
	t.Helper()
	got := titles(f.list(t, flt, issue.Order{}, page.Args{First: ptr(250)}).Items)
	var want []string
	for _, i := range f.all {
		if keep(i) {
			want = append(want, i.Title)
		}
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("%s:\n got %v\nwant %v", name, got, want)
	}
}

func TestFilterStateAndType(t *testing.T) {
	f := newListFixture(t)
	f.check(t, "state in Done", issue.Filter{State: &issue.IDCond{In: []uuid.UUID{f.done.ID}}},
		func(i issue.Issue) bool { return i.StateID == f.done.ID })
	f.check(t, "type in completed,canceled", issue.Filter{StateType: &issue.StateTypeCond{In: []project.StateType{project.Completed, project.Canceled}}},
		func(i issue.Issue) bool {
			tp := f.types[i.StateID]
			return tp == project.Completed || tp == project.Canceled
		})
	f.check(t, "type nin backlog", issue.Filter{StateType: &issue.StateTypeCond{Nin: []project.StateType{project.Backlog}}},
		func(i issue.Issue) bool { return f.types[i.StateID] != project.Backlog })
	f.check(t, "type eq started", issue.Filter{StateType: &issue.StateTypeCond{Eq: ptr(project.Started)}},
		func(i issue.Issue) bool { return f.types[i.StateID] == project.Started })
	f.check(t, "state neq Done", issue.Filter{State: &issue.IDCond{Neq: &f.done.ID}},
		func(i issue.Issue) bool { return i.StateID != f.done.ID })
}

func TestFilterAssignee(t *testing.T) {
	f := newListFixture(t)
	me := f.users.Member.ID
	f.check(t, "assignee eq", issue.Filter{Assignee: &issue.IDCond{Eq: &me}},
		func(i issue.Issue) bool { return i.AssigneeID != nil && *i.AssigneeID == me })
	f.check(t, "assignee null", issue.Filter{Assignee: &issue.IDCond{Null: ptr(true)}},
		func(i issue.Issue) bool { return i.AssigneeID == nil })
	f.check(t, "assignee not null", issue.Filter{Assignee: &issue.IDCond{Null: ptr(false)}},
		func(i issue.Issue) bool { return i.AssigneeID != nil })
	f.check(t, "assignee in {}", issue.Filter{Assignee: &issue.IDCond{In: []uuid.UUID{}}},
		func(issue.Issue) bool { return false })
	f.check(t, "assignee neq (unassigned included)", issue.Filter{Assignee: &issue.IDCond{Neq: &me}},
		func(i issue.Issue) bool { return i.AssigneeID == nil || *i.AssigneeID != me })
	f.check(t, "assignee nin", issue.Filter{Assignee: &issue.IDCond{Nin: []uuid.UUID{me}}},
		func(i issue.Issue) bool { return i.AssigneeID == nil || *i.AssigneeID != me })
}

func TestFilterPriority(t *testing.T) {
	f := newListFixture(t)
	f.check(t, "priority 1..2", issue.Filter{Priority: &issue.IntCond{Gte: ptr(1), Lte: ptr(2)}},
		func(i issue.Issue) bool { return i.Priority == 1 || i.Priority == 2 })
	f.check(t, "priority eq 0", issue.Filter{Priority: &issue.IntCond{Eq: ptr(0)}},
		func(i issue.Issue) bool { return i.Priority == 0 })
	f.check(t, "priority gt 2 lt 4 neq 3", issue.Filter{Priority: &issue.IntCond{Gt: ptr(2), Lt: ptr(4), Neq: ptr(3)}},
		func(issue.Issue) bool { return false })
	f.check(t, "priority in/nin", issue.Filter{Priority: &issue.IntCond{In: []int{1, 4}, Nin: []int{4}}},
		func(i issue.Issue) bool { return i.Priority == 1 })
}

func TestFilterParent(t *testing.T) {
	f := newListFixture(t)
	f.check(t, "top level", issue.Filter{Parent: &issue.IDCond{Null: ptr(true)}},
		func(i issue.Issue) bool { return i.ParentID == nil })
	f.check(t, "children of parent", issue.Filter{Parent: &issue.IDCond{Eq: &f.parent.ID}},
		func(i issue.Issue) bool { return i.ParentID != nil && *i.ParentID == f.parent.ID })
	hasChildren := map[uuid.UUID]bool{}
	for _, i := range f.all {
		if i.ParentID != nil {
			hasChildren[*i.ParentID] = true
		}
	}
	f.check(t, "has children", issue.Filter{HasChildren: ptr(true)},
		func(i issue.Issue) bool { return hasChildren[i.ID] })
	f.check(t, "no children", issue.Filter{HasChildren: ptr(false)},
		func(i issue.Issue) bool { return !hasChildren[i.ID] })
}

func TestFilterDueDate(t *testing.T) {
	f := newListFixture(t)
	cut := date(2026, 2, 1)
	f.check(t, "due lt", issue.Filter{DueDate: &issue.DateCond{Lt: cut}},
		func(i issue.Issue) bool { return i.DueDate != nil && i.DueDate.Before(*cut) })
	f.check(t, "due null", issue.Filter{DueDate: &issue.DateCond{Null: ptr(true)}},
		func(i issue.Issue) bool { return i.DueDate == nil })
	f.check(t, "due eq", issue.Filter{DueDate: &issue.DateCond{Eq: date(2026, 3, 1)}},
		func(i issue.Issue) bool { return i.DueDate != nil && i.DueDate.Equal(*date(2026, 3, 1)) })
	f.check(t, "due gte lte", issue.Filter{DueDate: &issue.DateCond{Gte: date(2026, 1, 15), Lte: date(2026, 1, 15)}},
		func(i issue.Issue) bool { return i.DueDate != nil && i.DueDate.Equal(*date(2026, 1, 15)) })
	f.check(t, "due gt", issue.Filter{DueDate: &issue.DateCond{Gt: date(2026, 1, 15)}},
		func(i issue.Issue) bool { return i.DueDate != nil && i.DueDate.After(*date(2026, 1, 15)) })
}

func TestFilterArchived(t *testing.T) {
	f := newListFixture(t)
	if _, err := f.projects.SetArchived(context.Background(), f.users.Admin, f.web.ID, true); err != nil {
		t.Fatal(err)
	}
	inWeb := func(i issue.Issue) bool { return i.ProjectID == f.web.ID }
	f.check(t, "archived hidden", issue.Filter{}, func(i issue.Issue) bool { return !inWeb(i) })
	f.check(t, "project eq archived", issue.Filter{Project: &issue.IDCond{Eq: &f.web.ID}}, inWeb)
	f.check(t, "project in archived", issue.Filter{Project: &issue.IDCond{In: []uuid.UUID{f.web.ID}}}, inWeb)
	f.check(t, "include archived", issue.Filter{IncludeArchived: true}, func(issue.Issue) bool { return true })
	f.check(t, "project neq (archived still hidden)", issue.Filter{Project: &issue.IDCond{Neq: &f.ylk.ID}}, func(issue.Issue) bool { return false })
}

func TestDeletedNotListed(t *testing.T) {
	f := newListFixture(t)
	gone := f.all[3]
	if err := f.issues.Delete(context.Background(), f.users.Member, gone.ID); err != nil {
		t.Fatal(err)
	}
	f.check(t, "deleted", issue.Filter{}, func(i issue.Issue) bool { return i.ID != gone.ID })
}

func priorities(items []issue.Issue) string {
	var s []string
	for _, i := range items {
		s = append(s, fmt.Sprint(i.Priority))
	}
	return strings.Join(slices.Compact(s), ",")
}

func TestOrderPriorityNoneLast(t *testing.T) {
	f := newListFixture(t)
	asc := f.list(t, issue.Filter{}, issue.Order{Field: issue.Priority}, page.Args{First: ptr(250)}).Items
	if got := priorities(asc); got != "1,2,3,4,0" {
		t.Fatalf("ASC = %s", got)
	}
	desc := f.list(t, issue.Filter{}, issue.Order{Field: issue.Priority, Desc: true}, page.Args{First: ptr(250)}).Items
	if got := priorities(desc); got != "0,4,3,2,1" {
		t.Fatalf("DESC = %s", got)
	}
	rev := slices.Clone(asc)
	slices.Reverse(rev)
	if !slices.Equal(titles(rev), titles(desc)) {
		t.Fatalf("DESC is not the exact reverse of ASC:\n%v\n%v", titles(asc), titles(desc))
	}
}

func TestOrderDueDateNullsLast(t *testing.T) {
	f := newListFixture(t)
	for _, desc := range []bool{false, true} {
		items := f.list(t, issue.Filter{}, issue.Order{Field: issue.DueDate, Desc: desc}, page.Args{First: ptr(250)}).Items
		seenNull := false
		var prev *time.Time
		for _, i := range items {
			if i.DueDate == nil {
				seenNull = true
				continue
			}
			if seenNull {
				t.Fatalf("desc=%v: dated issue %s after a null", desc, i.Title)
			}
			if prev != nil && ((!desc && i.DueDate.Before(*prev)) || (desc && i.DueDate.After(*prev))) {
				t.Fatalf("desc=%v: out of order at %s", desc, i.Title)
			}
			prev = i.DueDate
		}
		if !seenNull {
			t.Fatal("fixture has no null due dates")
		}
	}
}

func TestPaginationWalk(t *testing.T) {
	f := newListFixture(t)
	fields := []issue.OrderField{issue.SortOrder, issue.CreatedAt, issue.UpdatedAt, issue.Priority, issue.DueDate}
	for _, field := range fields {
		for _, desc := range []bool{false, true} {
			o := issue.Order{Field: field, Desc: desc}
			name := fmt.Sprintf("%s desc=%v", field, desc)
			want := titles(f.list(t, issue.Filter{}, o, page.Args{First: ptr(250)}).Items)
			if len(want) != 23 {
				t.Fatalf("%s: %d issues", name, len(want))
			}
			var fwd []string
			var after *string
			for {
				r := f.list(t, issue.Filter{}, o, page.Args{First: ptr(5), After: after})
				fwd = append(fwd, titles(r.Items)...)
				if r.Info.HasPrev != (after != nil) {
					t.Fatalf("%s: hasPreviousPage %v with after %v", name, r.Info.HasPrev, after != nil)
				}
				if !r.Info.HasNext {
					break
				}
				after = r.Info.End
			}
			if !slices.Equal(fwd, want) {
				t.Fatalf("%s forward:\n got %v\nwant %v", name, fwd, want)
			}
			var back []string
			var before *string
			for {
				r := f.list(t, issue.Filter{}, o, page.Args{Last: ptr(5), Before: before})
				back = append(titles(r.Items), back...)
				if r.Info.HasNext != (before != nil) {
					t.Fatalf("%s: hasNextPage %v with before %v", name, r.Info.HasNext, before != nil)
				}
				if !r.Info.HasPrev {
					break
				}
				before = r.Info.Start
			}
			if !slices.Equal(back, want) {
				t.Fatalf("%s backward:\n got %v\nwant %v", name, back, want)
			}
		}
	}
}

func TestCursorOfOtherOrderRejected(t *testing.T) {
	f := newListFixture(t)
	r := f.list(t, issue.Filter{}, issue.Order{}, page.Args{First: ptr(2)})
	_, err := f.issues.List(context.Background(), issue.Filter{}, issue.Order{Field: issue.Priority}, page.Args{First: ptr(2), After: r.Info.End})
	wantInput(t, err, "after", kit.ReasonInvalidFormat)
	_, err = f.issues.List(context.Background(), issue.Filter{}, issue.Order{Desc: true}, page.Args{Last: ptr(2), Before: r.Info.End})
	wantInput(t, err, "before", kit.ReasonInvalidFormat)
	_, err = f.issues.List(context.Background(), issue.Filter{}, issue.Order{}, page.Args{First: ptr(251)})
	wantInput(t, err, "first", kit.ReasonOutOfRange)
}

// Values outside the column type must not reach PostgreSQL as encode errors.
func TestFilterPriorityOutOfRange(t *testing.T) {
	f := newListFixture(t)
	f.check(t, "priority eq 40000", issue.Filter{Priority: &issue.IntCond{Eq: ptr(40000)}},
		func(issue.Issue) bool { return false })
	f.check(t, "priority lt 1<<40", issue.Filter{Priority: &issue.IntCond{Lt: ptr(1 << 40)}},
		func(issue.Issue) bool { return true })
	f.check(t, "priority in [1<<40]", issue.Filter{Priority: &issue.IntCond{In: []int{1 << 40}}},
		func(issue.Issue) bool { return false })
	f.check(t, "priority nin [1<<40]", issue.Filter{Priority: &issue.IntCond{Nin: []int{1 << 40}}},
		func(issue.Issue) bool { return true })
}

func TestCursorGarbageValues(t *testing.T) {
	f := newListFixture(t)
	cases := []struct {
		order issue.Order
		field string
		v     string
	}{
		{issue.Order{Field: issue.DueDate}, "dueDate", `"garbage"`},
		{issue.Order{Field: issue.DueDate}, "dueDate", `"2026-13-01"`},
		{issue.Order{Field: issue.Priority}, "priority", `1000000000000`},
		{issue.Order{Field: issue.Priority}, "priority", `0`},
		{issue.Order{Field: issue.SortOrder}, "sortOrder", `"x"`},
	}
	for _, c := range cases {
		cur := page.Encode(page.Cursor{F: c.field, D: "asc", V: []byte(c.v), ID: uuid.New()})
		_, err := f.issues.List(context.Background(), issue.Filter{}, c.order, page.Args{First: ptr(2), After: &cur})
		wantInput(t, err, "after", kit.ReasonInvalidFormat)
	}
	// Valid edge values still work.
	for _, v := range []string{`"infinity"`, `"2026-01-15"`} {
		cur := page.Encode(page.Cursor{F: "dueDate", D: "asc", V: []byte(v), ID: uuid.New()})
		if _, err := f.issues.List(context.Background(), issue.Filter{}, issue.Order{Field: issue.DueDate}, page.Args{First: ptr(2), After: &cur}); err != nil {
			t.Fatalf("dueDate cursor %s: %v", v, err)
		}
	}
}

func TestOrderPriorityNoneLastBothWays(t *testing.T) {
	f := newListFixture(t)
	asc := f.list(t, issue.Filter{}, issue.Order{Field: issue.PriorityNoneLast}, page.Args{First: ptr(250)}).Items
	if got := priorities(asc); got != "1,2,3,4,0" {
		t.Fatalf("ASC = %s", got)
	}
	desc := f.list(t, issue.Filter{}, issue.Order{Field: issue.PriorityNoneLast, Desc: true}, page.Args{First: ptr(250)}).Items
	if got := priorities(desc); got != "4,3,2,1,0" {
		t.Fatalf("DESC = %s", got)
	}
	// Keyset pages walk the same order.
	var walked []issue.Issue
	a := page.Args{First: ptr(4)}
	for {
		r := f.list(t, issue.Filter{}, issue.Order{Field: issue.PriorityNoneLast, Desc: true}, a)
		walked = append(walked, r.Items...)
		if !r.Info.HasNext {
			break
		}
		a.After = r.Info.End
	}
	if !slices.Equal(titles(walked), titles(desc)) {
		t.Fatalf("paged DESC differs:\n%v\n%v", titles(walked), titles(desc))
	}
}
