package issue_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/issue"
)

// moreFixture: five YLK issues with distinct titles, descriptions, creators,
// due dates and timestamps (the test clock ticks one second per call).
//
//	YLK-1 "Retry bank sync"   "Bank fails nightly" member  P2
//	YLK-2 "50%_done report"   ""                   owner
//	YLK-3 "Fix login"         "x"                  member  Done (completed)
//	YLK-4 "BANK export"       "Export bank CSV"    admin   due 2026-03-01
//	YLK-5 "Archive cleanup"   ""                   owner   due 2026-01-15
type moreFixture struct {
	listFixture
	i []issue.Issue // by number - 1, as last written
}

func newMoreFixture(t *testing.T) moreFixture {
	t.Helper()
	e := newEnv(t)
	ctx := context.Background()
	rows := []struct {
		title, desc string
		by          account.User
		prio        int
		due         *time.Time
	}{
		{"Retry bank sync", "Bank fails nightly", e.users.Member, 2, nil},
		{"50%_done report", "", e.users.Owner, 0, nil},
		{"Fix login", "x", e.users.Member, 0, nil},
		{"BANK export", "Export bank CSV", e.users.Admin, 0, date(2026, 3, 1)},
		{"Archive cleanup", "", e.users.Owner, 0, date(2026, 1, 15)},
	}
	f := moreFixture{listFixture: listFixture{env: e}}
	for _, r := range rows {
		it, err := e.issues.Create(ctx, r.by, issue.CreateInput{ProjectID: &e.ylk.ID, Title: r.title, Description: r.desc, Priority: r.prio, DueDate: r.due})
		if err != nil {
			t.Fatal(err)
		}
		f.i = append(f.i, it)
	}
	done := e.state(t, e.ylk.ID, "Done")
	up, err := e.issues.Update(ctx, e.users.Member, f.i[2].ID, issue.Patch{StateID: &done.ID})
	if err != nil {
		t.Fatal(err)
	}
	f.i[2] = up
	f.all = f.i
	return f
}

func (f moreFixture) nums(ns ...int) func(issue.Issue) bool {
	return func(i issue.Issue) bool {
		for _, n := range ns {
			if i.Number == n {
				return true
			}
		}
		return false
	}
}

func (f moreFixture) not(ns ...int) func(issue.Issue) bool {
	in := f.nums(ns...)
	return func(i issue.Issue) bool { return !in(i) }
}

func all(issue.Issue) bool  { return true }
func none(issue.Issue) bool { return false }

func TestFilterTitleOperators(t *testing.T) {
	f := newMoreFixture(t)
	cases := []struct {
		name string
		c    issue.TextCond
		keep func(issue.Issue) bool
	}{
		{"eq", issue.TextCond{Eq: ptr("Fix login")}, f.nums(3)},
		{"neq", issue.TextCond{Neq: ptr("Fix login")}, f.not(3)},
		{"eqFold", issue.TextCond{EqFold: ptr("fix LOGIN")}, f.nums(3)},
		{"neqFold", issue.TextCond{NeqFold: ptr("FIX login")}, f.not(3)},
		{"contains", issue.TextCond{Contains: ptr("bank")}, f.nums(1)},
		{"containsFold", issue.TextCond{ContainsFold: ptr("bank")}, f.nums(1, 4)},
		{"notContains", issue.TextCond{NotContains: ptr("bank")}, f.not(1)},
		{"notContainsFold", issue.TextCond{NotContainsFold: ptr("bank")}, f.not(1, 4)},
		{"startsWith", issue.TextCond{StartsWith: ptr("Fix")}, f.nums(3)},
		{"startsWithFold", issue.TextCond{StartsWithFold: ptr("bank")}, f.nums(4)},
		{"notStartsWith", issue.TextCond{NotStartsWith: ptr("Fix")}, f.not(3)},
		{"endsWith", issue.TextCond{EndsWith: ptr("sync")}, f.nums(1)},
		{"notEndsWith", issue.TextCond{NotEndsWith: ptr("sync")}, f.not(1)},
		{"in", issue.TextCond{In: []string{"Fix login", "Archive cleanup"}}, f.nums(3, 5)},
		{"nin", issue.TextCond{Nin: []string{"Fix login", "Archive cleanup"}}, f.not(3, 5)},
		{"in empty", issue.TextCond{In: []string{}}, none},
		// Without escaping, "%_" would be "any text with at least one character".
		{"contains wildcards", issue.TextCond{Contains: ptr("%_")}, f.nums(2)},
		{"startsWith underscore", issue.TextCond{StartsWith: ptr("_")}, none},
		{"contains backslash", issue.TextCond{Contains: ptr(`\`)}, none},
		{"two operators AND", issue.TextCond{ContainsFold: ptr("bank"), StartsWith: ptr("Retry")}, f.nums(1)},
	}
	for _, c := range cases {
		f.check(t, c.name, issue.Filter{Title: &c.c}, c.keep)
	}
}

func TestFilterDescriptionNull(t *testing.T) {
	f := newMoreFixture(t)
	f.check(t, "null true ('' counts)", issue.Filter{Description: &issue.TextCond{Null: ptr(true)}}, f.nums(2, 5))
	f.check(t, "null false", issue.Filter{Description: &issue.TextCond{Null: ptr(false)}}, f.not(2, 5))
	f.check(t, "neq keeps empty", issue.Filter{Description: &issue.TextCond{Neq: ptr("x")}}, f.not(3))
	f.check(t, "containsFold", issue.Filter{Description: &issue.TextCond{ContainsFold: ptr("BANK")}}, f.nums(1, 4))
	f.check(t, "notContains keeps empty", issue.Filter{Description: &issue.TextCond{NotContains: ptr("bank")}}, f.not(4))
	f.check(t, "nin keeps empty", issue.Filter{Description: &issue.TextCond{Nin: []string{"x"}}}, f.not(3))
}

func TestFilterNumberAndID(t *testing.T) {
	f := newMoreFixture(t)
	f.check(t, "number eq 2", issue.Filter{Number: &issue.IntCond{Eq: ptr(2)}}, f.nums(2))
	f.check(t, "number in", issue.Filter{Number: &issue.IntCond{In: []int{1, 5}}}, f.nums(1, 5))
	a, b := f.i[0].ID, f.i[3].ID
	f.check(t, "id in", issue.Filter{ID: &issue.IDCond{In: []uuid.UUID{a, b}}}, f.nums(1, 4))
	f.check(t, "id nin", issue.Filter{ID: &issue.IDCond{Nin: []uuid.UUID{a}}}, f.not(1))
	f.check(t, "id eq", issue.Filter{ID: &issue.IDCond{Eq: &b}}, f.nums(4))
}

func TestFilterCreator(t *testing.T) {
	f := newMoreFixture(t)
	m := f.users.Member.ID
	f.check(t, "creator eq member", issue.Filter{Creator: &issue.IDCond{Eq: &m}}, f.nums(1, 3))
	f.check(t, "creator nin member", issue.Filter{Creator: &issue.IDCond{Nin: []uuid.UUID{m}}}, f.not(1, 3))
}

func TestFilterTimestamps(t *testing.T) {
	f := newMoreFixture(t)
	from, to := f.i[1].CreatedAt, f.i[3].CreatedAt
	f.check(t, "createdAt window", issue.Filter{CreatedAt: &issue.TimeCond{Gte: &from, Lt: &to}}, f.nums(2, 3))
	f.check(t, "createdAt gt lte", issue.Filter{CreatedAt: &issue.TimeCond{Gt: &from, Lte: &to}}, f.nums(3, 4))
	f.check(t, "createdAt eq", issue.Filter{CreatedAt: &issue.TimeCond{Eq: &from}}, f.nums(2))
	f.check(t, "createdAt neq", issue.Filter{CreatedAt: &issue.TimeCond{Neq: &from}}, f.not(2))
	f.check(t, "completedAt not null", issue.Filter{CompletedAt: &issue.TimeCond{Null: ptr(false)}}, f.nums(3))
	f.check(t, "completedAt null", issue.Filter{CompletedAt: &issue.TimeCond{Null: ptr(true)}}, f.not(3))
	f.check(t, "canceledAt null", issue.Filter{CanceledAt: &issue.TimeCond{Null: ptr(true)}}, all)
	u := f.i[2].UpdatedAt
	f.check(t, "updatedAt in exact", issue.Filter{UpdatedAt: &issue.TimeCond{In: []time.Time{u}}}, f.nums(3))
	f.check(t, "updatedAt nin", issue.Filter{UpdatedAt: &issue.TimeCond{Nin: []time.Time{u}}}, f.not(3))
	c := *f.i[2].CompletedAt
	f.check(t, "completedAt neq keeps nulls", issue.Filter{CompletedAt: &issue.TimeCond{Neq: &c}}, f.not(3))
}

func TestFilterDueDateNeqIn(t *testing.T) {
	f := newMoreFixture(t)
	f.check(t, "due neq keeps no-date", issue.Filter{DueDate: &issue.DateCond{Neq: date(2026, 3, 1)}}, f.not(4))
	f.check(t, "due in", issue.Filter{DueDate: &issue.DateCond{In: []time.Time{*date(2026, 3, 1), *date(2026, 1, 15)}}}, f.nums(4, 5))
	f.check(t, "due nin keeps no-date", issue.Filter{DueDate: &issue.DateCond{Nin: []time.Time{*date(2026, 3, 1)}}}, f.not(4))
}

func TestFilterAnyAll(t *testing.T) {
	f := newMoreFixture(t)
	owner := f.users.Owner.ID
	admin := f.users.Admin.ID
	f.check(t, "any title a or number 3", issue.Filter{Any: []issue.Filter{
		{Title: &issue.TextCond{Contains: ptr("a")}},
		{Number: &issue.IntCond{Eq: ptr(3)}},
	}}, f.nums(1, 3, 5))
	f.check(t, "all inside any", issue.Filter{Any: []issue.Filter{
		{All: []issue.Filter{{Creator: &issue.IDCond{Eq: &owner}}, {DueDate: &issue.DateCond{Null: ptr(false)}}}},
		{Number: &issue.IntCond{Eq: ptr(1)}},
	}}, f.nums(1, 5))
	f.check(t, "empty any adds nothing", issue.Filter{Any: []issue.Filter{}}, all)
	f.check(t, "any: nothing or number 2", issue.Filter{Any: []issue.Filter{
		{ID: &issue.IDCond{In: []uuid.UUID{}}},
		{Number: &issue.IntCond{Eq: ptr(2)}},
	}}, f.nums(2))
	f.check(t, "any of nothing", issue.Filter{Any: []issue.Filter{{Title: &issue.TextCond{Eq: ptr("nope")}}}}, none)
	f.check(t, "all", issue.Filter{All: []issue.Filter{
		{Title: &issue.TextCond{ContainsFold: ptr("bank")}},
		{Creator: &issue.IDCond{Eq: &admin}},
	}}, f.nums(4))
	f.check(t, "any AND top-level", issue.Filter{
		Creator: &issue.IDCond{Eq: &owner},
		Any:     []issue.Filter{{Number: &issue.IntCond{Eq: ptr(1)}}, {Number: &issue.IntCond{Eq: ptr(5)}}},
	}, f.nums(5))
}

func TestFilterSubIgnoresArchivedFlag(t *testing.T) {
	f := newMoreFixture(t)
	ctx := context.Background()
	web := f.project(t, "WEB")
	w, err := f.issues.Create(ctx, f.users.Member, issue.CreateInput{ProjectID: &web.ID, Title: "Web bank"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.projects.SetArchived(ctx, f.users.Admin, web.ID, true); err != nil {
		t.Fatal(err)
	}
	f.all = append(f.all, w)
	isWeb := func(i issue.Issue) bool { return i.ProjectID == web.ID }
	notWeb := func(i issue.Issue) bool { return !isWeb(i) }
	f.check(t, "any include archived", issue.Filter{Any: []issue.Filter{{IncludeArchived: true}}}, notWeb)
	f.check(t, "all include archived", issue.Filter{All: []issue.Filter{{IncludeArchived: true, Title: &issue.TextCond{ContainsFold: ptr("bank")}}}},
		func(i issue.Issue) bool { return notWeb(i) && (i.Number == 1 || i.Number == 4) })
	f.check(t, "top-level include archived", issue.Filter{IncludeArchived: true}, all)
	f.check(t, "top-level include archived + any", issue.Filter{IncludeArchived: true, Any: []issue.Filter{{Title: &issue.TextCond{Eq: ptr("Web bank")}}}}, isWeb)
}
