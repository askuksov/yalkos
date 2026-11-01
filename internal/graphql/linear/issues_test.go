package linear_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/graphql/linear"
	"github.com/askuksov/yalkos/internal/testdb"
)

// issueFix: YLK and WEB, member Mia, five issues created in this order
// (the test clock makes createdAt increase):
//
//	YLK-1 "Retry bank sync"  P2 assignee Olga
//	YLK-2 "Bank export"      P1 assignee Mia, due 2026-03-01, In Progress, "Export to CSV"
//	YLK-3 "Fix login"        P0 child of YLK-1
//	YLK-4 "Archive old data" P4 due 2026-01-15, created by Mia, Done
//	WEB-1 "Landing sync"     P1
type issueFix struct {
	*env
	ylk, web project.Project
	mia      account.User
	miaKey   string
	i        map[string]issue.Issue
}

func stateNamed(t *testing.T, e *env, p project.Project, name string) project.State {
	t.Helper()
	states, err := e.deps.Projects.States(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no state %q", name)
	return project.State{}
}

func day(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}

func newIssueFix(t *testing.T) issueFix {
	t.Helper()
	return issueFixOn(t, newEnv(t))
}

func issueFixOn(t *testing.T, e *env) issueFix {
	t.Helper()
	ctx := context.Background()
	f := issueFix{env: e, i: map[string]issue.Issue{}}
	f.ylk = e.project(t, "YLK", "Yalkos")
	f.web = e.project(t, "WEB", "Website")
	f.mia = e.user(t, "Mia", "mia@example.com", account.RoleMember)
	f.miaKey = e.key(t, f.mia)
	inProgress := stateNamed(t, e, f.ylk, "In Progress")
	f.i["YLK-1"] = e.issue(t, f.ylk, "Retry bank sync", func(in *issue.CreateInput) { in.Priority = 2; in.AssigneeID = &e.owner.ID })
	f.i["YLK-2"] = e.issue(t, f.ylk, "Bank export", func(in *issue.CreateInput) {
		in.Priority, in.AssigneeID, in.DueDate, in.StateID, in.Description = 1, &f.mia.ID, day(2026, 3, 1), &inProgress.ID, "Export to CSV"
	})
	parent := f.i["YLK-1"].ID
	f.i["YLK-3"] = e.issue(t, f.ylk, "Fix login", func(in *issue.CreateInput) { in.ParentID = &parent })
	y4, err := e.deps.Issues.Create(ctx, f.mia, issue.CreateInput{ProjectID: &f.ylk.ID, Title: "Archive old data", Priority: 4, DueDate: day(2026, 1, 15)})
	if err != nil {
		t.Fatal(err)
	}
	done := stateNamed(t, e, f.ylk, "Done")
	if f.i["YLK-4"], err = e.deps.Issues.Update(ctx, f.mia, y4.ID, issue.Patch{StateID: &done.ID}); err != nil {
		t.Fatal(err)
	}
	f.i["WEB-1"] = e.issue(t, f.web, "Landing sync", func(in *issue.CreateInput) { in.Priority = 1 })
	return f
}

type issueNode struct {
	ID                string
	Identifier        string
	Number            float64
	Title             string
	Description       *string
	Priority          float64
	PriorityLabel     string
	URL               string
	BranchName        string
	LabelIDs          []string `json:"labelIds"`
	SubIssueSortOrder *float64
	SortOrder         float64
	Estimate          *float64
	Reactions         []any
	ReactionData      any
	Team              struct{ ID, Key string }
	State             struct{ ID, Name, Type string }
	Assignee          *struct{ ID string }
	Creator           *struct{ ID string }
	Parent            *struct{ ID, Identifier string }
	SharedAccess      struct {
		IsShared              bool
		SharedWithCount       int
		DisallowedIssueFields []string
		SharedWithUsers       []any
	}
	PreviousIdentifiers  []string
	DueDate              *string
	CompletedAt          *string
	CreatedAt, UpdatedAt string
}

type issueConn struct {
	Nodes    []issueNode
	PageInfo struct {
		HasNextPage bool
		EndCursor   *string
	}
}

func idents(nodes []issueNode) []string {
	out := []string{}
	for _, n := range nodes {
		out = append(out, n.Identifier)
	}
	return out
}

// list runs issues(filter, sort, orderBy, includeArchived) and returns identifiers.
func (f issueFix) list(t *testing.T, vars map[string]any) []string {
	t.Helper()
	var out struct{ Issues issueConn }
	f.gql(t, f.ownerKey, `query($filter: IssueFilter, $sort: [IssueSortInput!], $orderBy: PaginationOrderBy, $includeArchived: Boolean, $first: Int) {
		issues(filter: $filter, sort: $sort, orderBy: $orderBy, includeArchived: $includeArchived, first: $first) { nodes { identifier } } }`, vars).ok(t, &out)
	return idents(out.Issues.Nodes)
}

// set compares identifiers as sets.
func set(t *testing.T, name string, got []string, want ...string) {
	t.Helper()
	g, w := slices.Clone(got), slices.Clone(want)
	slices.Sort(g)
	slices.Sort(w)
	if !slices.Equal(g, w) {
		t.Errorf("%s: %v, want %v", name, got, want)
	}
}

func filter(kv ...any) map[string]any {
	m := map[string]any{}
	for i := 0; i < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return map[string]any{"filter": m}
}

func cmp(op string, v any) map[string]any { return map[string]any{op: v} }

func TestIssueByIDAndKey(t *testing.T) {
	f := newIssueFix(t)
	q := `query($id: String!) { issue(id: $id) { id identifier } }`
	y2 := f.i["YLK-2"]
	for _, ref := range []string{y2.ID.String(), "ylk-2", "YLK-2"} {
		var out struct{ Issue issueNode }
		f.gql(t, f.ownerKey, q, map[string]any{"id": ref}).ok(t, &out)
		if out.Issue.ID != y2.ID.String() || out.Issue.Identifier != "YLK-2" {
			t.Fatalf("%s → %+v", ref, out.Issue)
		}
	}
	if _, err := f.deps.Issues.Move(context.Background(), f.owner, y2.ID, f.web.ID, nil); err != nil {
		t.Fatal(err)
	}
	var out struct{ Issue issueNode }
	f.gql(t, f.ownerKey, q, map[string]any{"id": "YLK-2"}).ok(t, &out)
	if out.Issue.ID != y2.ID.String() || out.Issue.Identifier != "WEB-2" {
		t.Fatalf("former identifier → %+v", out.Issue)
	}
	for _, ref := range []string{"YLK-99", uuid.NewString(), "nonsense"} {
		f.gql(t, f.ownerKey, q, map[string]any{"id": ref}).fails(t).want(t, "NOT_FOUND", "invalid input", "Issue not found", "")
	}
}

const issueFields = `id identifier number title description priority priorityLabel url branchName labelIds subIssueSortOrder sortOrder
	estimate reactions { id } reactionData team { id key } state { id name type } assignee { id } creator { id } parent { id identifier }
	sharedAccess { isShared sharedWithCount disallowedIssueFields sharedWithUsers { id } } previousIdentifiers dueDate completedAt createdAt updatedAt`

func (f issueFix) get(t *testing.T, ref string) issueNode {
	t.Helper()
	var out struct{ Issue issueNode }
	f.gql(t, f.ownerKey, `query($id: String!) { issue(id: $id) { `+issueFields+` } }`, map[string]any{"id": ref}).ok(t, &out)
	return out.Issue
}

func TestIssueFields(t *testing.T) {
	f := newIssueFix(t)
	types, err := f.deps.Labels.TypeInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	y1 := f.get(t, "YLK-1")
	if y1.Identifier != "YLK-1" || y1.Number != 1 || y1.Title != "Retry bank sync" || y1.Priority != 2 || y1.PriorityLabel != "High" ||
		y1.URL != base+"/issue/YLK-1" || y1.BranchName != "ylk-1-retry-bank-sync" || y1.Description != nil || y1.Estimate != nil ||
		y1.SubIssueSortOrder != nil || y1.Team.Key != "YLK" || y1.State.Type != "unstarted" || y1.Assignee == nil ||
		y1.Assignee.ID != f.owner.ID.String() || y1.Creator == nil || y1.Creator.ID != f.owner.ID.String() || y1.Parent != nil ||
		y1.Reactions == nil || len(y1.Reactions) != 0 || y1.SharedAccess.IsShared || y1.SharedAccess.SharedWithCount != 0 ||
		y1.SharedAccess.DisallowedIssueFields == nil || y1.SharedAccess.SharedWithUsers == nil || y1.PreviousIdentifiers == nil ||
		y1.DueDate != nil || y1.CompletedAt != nil {
		t.Fatalf("YLK-1 = %+v", y1)
	}
	if types.DefaultID == nil || !slices.Equal(y1.LabelIDs, []string{types.DefaultID.String()}) {
		t.Fatalf("labelIds = %v, default type %v", y1.LabelIDs, types.DefaultID)
	}
	if rd, _ := json.Marshal(y1.ReactionData); string(rd) != "[]" {
		t.Fatalf("reactionData = %s", rd)
	}
	y3 := f.get(t, "YLK-3")
	if y3.Parent == nil || y3.Parent.Identifier != "YLK-1" || y3.SubIssueSortOrder == nil || *y3.SubIssueSortOrder != y3.SortOrder ||
		y3.Priority != 0 || y3.PriorityLabel != "No priority" || y3.Assignee != nil {
		t.Fatalf("YLK-3 = %+v", y3)
	}
	y2 := f.get(t, "YLK-2")
	if y2.Description == nil || *y2.Description != "Export to CSV" || y2.DueDate == nil || *y2.DueDate != "2026-03-01" ||
		y2.PriorityLabel != "Urgent" || y2.State.Name != "In Progress" {
		t.Fatalf("YLK-2 = %+v", y2)
	}
	if y4 := f.get(t, "YLK-4"); y4.CompletedAt == nil || y4.PriorityLabel != "Low" || y4.Creator.ID != f.mia.ID.String() {
		t.Fatalf("YLK-4 = %+v", y4)
	}
}

func TestBranchNameCut(t *testing.T) {
	long := strings.Repeat("word ", 16) // 80 characters
	got := linear.BranchName("YLK-12", long)
	if len(got) > 60 || strings.HasSuffix(got, "-") || !strings.HasPrefix(got, "ylk-12-word-word") {
		t.Fatalf("branch = %q (%d)", got, len(got))
	}
	if got := linear.BranchName("YLK-1", strings.Repeat("x", 80)); len(got) != 60 {
		t.Fatalf("no dash: %q", got)
	}
	if got := linear.BranchName("WEB-7", "Fix: the  log-in (again)!"); got != "web-7-fix-the-log-in-again" {
		t.Fatalf("slug = %q", got)
	}
}

func TestIssuesDefaultOrder(t *testing.T) {
	f := newIssueFix(t)
	if got := f.list(t, nil); !slices.Equal(got, []string{"WEB-1", "YLK-4", "YLK-3", "YLK-2", "YLK-1"}) {
		t.Fatalf("default = %v", got)
	}
	if _, err := f.deps.Issues.Update(context.Background(), f.owner, f.i["YLK-2"].ID, issue.Patch{Title: ptr("Bank export v2")}); err != nil {
		t.Fatal(err)
	}
	if got := f.list(t, map[string]any{"orderBy": "updatedAt"}); got[0] != "YLK-2" {
		t.Fatalf("updatedAt = %v", got)
	}
	if got := f.list(t, map[string]any{"orderBy": "createdAt"}); got[0] != "WEB-1" {
		t.Fatalf("createdAt = %v", got)
	}
}

func TestIssuesSort(t *testing.T) {
	f := newIssueFix(t)
	prios := func(vars map[string]any) []string {
		var out struct{ Issues issueConn }
		f.gql(t, f.ownerKey, `query($sort: [IssueSortInput!]) { issues(sort: $sort) { nodes { identifier priority } } }`, vars).ok(t, &out)
		var s []string
		for _, n := range out.Issues.Nodes {
			s = append(s, strconv.Itoa(int(n.Priority)))
		}
		return s
	}
	sortBy := func(key string, v map[string]any) map[string]any {
		return map[string]any{"sort": []any{map[string]any{key: v}}}
	}
	if got := prios(sortBy("priority", map[string]any{"order": "Ascending"})); !slices.Equal(got, []string{"1", "1", "2", "4", "0"}) {
		t.Errorf("priority asc = %v", got)
	}
	if got := prios(sortBy("priority", map[string]any{"order": "Descending"})); !slices.Equal(got, []string{"4", "2", "1", "1", "0"}) {
		t.Errorf("priority desc = %v", got)
	}
	if got := f.list(t, sortBy("dueDate", map[string]any{})); !slices.Equal(got[:2], []string{"YLK-4", "YLK-2"}) {
		t.Errorf("dueDate = %v", got)
	}
	if got := f.list(t, sortBy("dueDate", map[string]any{"order": "Descending"})); !slices.Equal(got[:2], []string{"YLK-2", "YLK-4"}) {
		t.Errorf("dueDate desc = %v", got)
	}
	if got := f.list(t, sortBy("createdAt", map[string]any{"order": "Ascending"})); got[0] != "YLK-1" {
		t.Errorf("createdAt asc = %v", got)
	}
	if got := f.list(t, sortBy("manual", map[string]any{})); len(got) != 5 {
		t.Errorf("manual = %v", got)
	}
	q := `query($sort: [IssueSortInput!]) { issues(sort: $sort) { nodes { id } } }`
	bad := []struct {
		vars map[string]any
		msg  string
	}{
		{map[string]any{"sort": []any{map[string]any{"priority": map[string]any{}}, map[string]any{"dueDate": map[string]any{}}}}, "Invalid sort: use one sort key"},
		{map[string]any{"sort": []any{map[string]any{"priority": map[string]any{}, "dueDate": map[string]any{}}}}, "Invalid sort: use one sort key"},
		{sortBy("dueDate", map[string]any{"nulls": "first"}), "Invalid sort: nulls first is not supported"},
		{sortBy("priority", map[string]any{"noPriorityFirst": true}), "Invalid sort: noPriorityFirst is not supported"},
	}
	for _, b := range bad {
		f.gql(t, f.ownerKey, q, b.vars).fails(t).want(t, "INVALID_INPUT", "invalid input", b.msg, "sort")
	}
}

func TestIssueFilterScalars(t *testing.T) {
	f := newIssueFix(t)
	set(t, "title containsIgnoreCase", f.list(t, filter("title", cmp("containsIgnoreCase", "BANK"))), "YLK-1", "YLK-2")
	set(t, "number in", f.list(t, filter("number", cmp("in", []any{1, 3}))), "YLK-1", "YLK-3", "WEB-1")
	set(t, "priority lte 2 (no priority = 0)", f.list(t, filter("priority", cmp("lte", 2))), "YLK-1", "YLK-2", "YLK-3", "WEB-1")
	set(t, "priority gte 1 lt 4", f.list(t, filter("priority", map[string]any{"gte": 1, "lt": 4})), "YLK-1", "YLK-2", "WEB-1")
	set(t, "priority null", f.list(t, filter("priority", cmp("null", true))))
	set(t, "priority not null", f.list(t, filter("priority", cmp("null", false))), "YLK-1", "YLK-2", "YLK-3", "YLK-4", "WEB-1")
	set(t, "description null", f.list(t, filter("description", cmp("null", true))), "YLK-1", "YLK-3", "YLK-4", "WEB-1")
	set(t, "description contains", f.list(t, filter("description", cmp("contains", "CSV"))), "YLK-2")
	set(t, "createdAt gte -P1D", f.list(t, filter("createdAt", cmp("gte", "-P1D"))), "YLK-1", "YLK-2", "YLK-3", "YLK-4", "WEB-1")
	set(t, "createdAt lt -P1D", f.list(t, filter("createdAt", cmp("lt", "-P1D"))))
	set(t, "createdAt lt 2027", f.list(t, filter("createdAt", cmp("lt", "2027-01-01"))), "YLK-1", "YLK-2", "YLK-3", "YLK-4", "WEB-1")
	set(t, "dueDate null", f.list(t, filter("dueDate", cmp("null", true))), "YLK-1", "YLK-3", "WEB-1")
	set(t, "dueDate lt", f.list(t, filter("dueDate", cmp("lt", "2026-02-01"))), "YLK-4")
	set(t, "dueDate neq keeps none", f.list(t, filter("dueDate", cmp("neq", "2026-03-01"))), "YLK-1", "YLK-3", "YLK-4", "WEB-1")
	set(t, "completedAt not null", f.list(t, filter("completedAt", cmp("null", false))), "YLK-4")
	set(t, "canceledAt null", f.list(t, filter("canceledAt", cmp("null", true))), "YLK-1", "YLK-2", "YLK-3", "YLK-4", "WEB-1")
	set(t, "id in", f.list(t, filter("id", cmp("in", []any{f.i["YLK-2"].ID.String()}))), "YLK-2")
	q := `query($filter: IssueFilter) { issues(filter: $filter) { nodes { id } } }`
	f.gql(t, f.ownerKey, q, filter("number", cmp("eq", 1.5))).fails(t).want(t, "INVALID_INPUT", "invalid input", "Invalid number: must be a whole number", "number")
	f.gql(t, f.ownerKey, q, filter("priority", cmp("in", []any{1, 2.5}))).fails(t).want(t, "INVALID_INPUT", "invalid input", "Invalid priority: must be a whole number", "priority")
	f.gql(t, f.ownerKey, q, filter("createdAt", cmp("gt", "soon"))).fails(t).want(t, "INVALID_INPUT", "invalid input", "Invalid createdAt: use an ISO 8601 date, date-time or duration", "createdAt")
	f.gql(t, f.ownerKey, q, filter("dueDate", cmp("gt", "PT1H"))).fails(t).want(t, "INVALID_INPUT", "invalid input", "Invalid dueDate: use an ISO 8601 date, date-time or duration", "dueDate")
	f.gql(t, f.ownerKey, q, filter("id", cmp("eq", "YLK-1"))).fails(t).want(t, "INVALID_INPUT", "invalid input", "Invalid id: not a valid id", "id")
}

func TestIssueFilterRelations(t *testing.T) {
	f := newIssueFix(t)
	set(t, "assignee isMe", f.list(t, filter("assignee", map[string]any{"isMe": cmp("eq", true)})), "YLK-1")
	set(t, "assignee null", f.list(t, filter("assignee", cmp("null", true))), "YLK-3", "YLK-4", "WEB-1")
	set(t, "assignee not null + isMe", f.list(t, filter("assignee", map[string]any{"null": false, "isMe": cmp("eq", true)})), "YLK-1")
	set(t, "assignee null + isMe (AND)", f.list(t, filter("assignee", map[string]any{"null": true, "isMe": cmp("eq", true)})))
	set(t, "creator email", f.list(t, filter("creator", map[string]any{"email": cmp("eq", "mia@example.com")})), "YLK-4")
	set(t, "state type started", f.list(t, filter("state", map[string]any{"type": cmp("eq", "started")})), "YLK-2")
	set(t, "state name todo", f.list(t, filter("state", map[string]any{"name": cmp("eqIgnoreCase", "todo")})), "YLK-1", "YLK-3", "WEB-1")
	set(t, "team key", f.list(t, filter("team", map[string]any{"key": cmp("eq", "WEB")})), "WEB-1")
	set(t, "team id", f.list(t, filter("team", map[string]any{"id": cmp("eq", f.ylk.ID.String())})), "YLK-1", "YLK-2", "YLK-3", "YLK-4")
	set(t, "parent id", f.list(t, filter("parent", map[string]any{"id": cmp("eq", f.i["YLK-1"].ID.String())})), "YLK-3")
	set(t, "parent null", f.list(t, filter("parent", cmp("null", true))), "YLK-1", "YLK-2", "YLK-4", "WEB-1")
	set(t, "parent not null", f.list(t, filter("parent", cmp("null", false))), "YLK-3")
}

func TestIssueFilterOrNested(t *testing.T) {
	f := newIssueFix(t)
	set(t, "or", f.list(t, filter("or", []any{
		map[string]any{"title": cmp("contains", "login")},
		map[string]any{"and": []any{map[string]any{"team": map[string]any{"key": cmp("eq", "WEB")}}, map[string]any{"priority": cmp("eq", 1)}}},
	})), "YLK-3", "WEB-1")
	set(t, "and of or", f.list(t, filter("and", []any{
		map[string]any{"or": []any{map[string]any{"priority": cmp("eq", 1)}, map[string]any{"priority": cmp("eq", 2)}}},
		map[string]any{"team": map[string]any{"key": cmp("eq", "YLK")}},
	})), "YLK-1", "YLK-2")
	set(t, "or with top-level", f.list(t, map[string]any{"filter": map[string]any{
		"team": map[string]any{"key": cmp("eq", "YLK")},
		"or":   []any{map[string]any{"title": cmp("contains", "sync")}, map[string]any{"number": cmp("eq", 3)}},
	}}), "YLK-1", "YLK-3")
}

func TestIssueFilterEmptySubFilterMatchesNothing(t *testing.T) {
	f := newIssueFix(t)
	set(t, "team NOPE", f.list(t, filter("team", map[string]any{"key": cmp("eq", "NOPE")})))
	set(t, "or team NOPE", f.list(t, filter("or", []any{map[string]any{"team": map[string]any{"key": cmp("eq", "NOPE")}}})))
	set(t, "assignee nobody", f.list(t, filter("assignee", map[string]any{"email": cmp("eq", "nobody@x")})))
	set(t, "creator nobody", f.list(t, filter("creator", map[string]any{"name": cmp("eq", "Nobody")})))
	set(t, "state nothing", f.list(t, filter("state", map[string]any{"name": cmp("eq", "Nope")})))
	// Positive control: the matching alternative still matches.
	set(t, "or NOPE or WEB", f.list(t, filter("or", []any{
		map[string]any{"team": map[string]any{"key": cmp("eq", "NOPE")}}, map[string]any{"team": map[string]any{"key": cmp("eq", "WEB")}},
	})), "WEB-1")
}

func TestIssueFilterUnknownFieldRejected(t *testing.T) {
	f := newIssueFix(t)
	r := f.gql(t, f.ownerKey, `query($filter: IssueFilter) { issues(filter: $filter) { nodes { id } } }`, filter("estimate", cmp("eq", 1)))
	g := r.fails(t)
	if g.Extensions["type"] != "graphql error" || g.Message != `Field "estimate" is not defined by type "IssueFilter"` || string(r.Data) != "null" {
		t.Fatalf("estimate filter = %+v %s", g, r.Data)
	}
	g = f.gql(t, f.ownerKey, `query($filter: IssueFilter) { issues(filter: $filter) { nodes { id } } }`,
		filter("or", []any{map[string]any{"assignee": map[string]any{"name": cmp("eq", "x"), "nope": true}}})).fails(t)
	if g.Message != `Field "nope" is not defined by type "NullableUserFilter"` {
		t.Fatalf("nested unknown field = %+v", g)
	}
	g = f.gql(t, f.ownerKey, `{ issues(filter: { cycle: { id: { eq: "x" } } }) { nodes { id } } }`, nil).fails(t)
	if g.Extensions["type"] != "graphql error" || !strings.Contains(g.Message, "cycle") {
		t.Fatalf("cycle filter = %+v", g)
	}
}

func TestIssueFilterArchived(t *testing.T) {
	f := newIssueFix(t)
	if _, err := f.deps.Projects.SetArchived(context.Background(), f.owner, f.web.ID, true); err != nil {
		t.Fatal(err)
	}
	set(t, "hidden", f.list(t, nil), "YLK-1", "YLK-2", "YLK-3", "YLK-4")
	set(t, "includeArchived", f.list(t, map[string]any{"includeArchived": true}), "YLK-1", "YLK-2", "YLK-3", "YLK-4", "WEB-1")
	set(t, "team id", f.list(t, filter("team", map[string]any{"id": cmp("eq", f.web.ID.String())})), "WEB-1")
	set(t, "team key", f.list(t, filter("team", map[string]any{"key": cmp("eq", "WEB")})), "WEB-1")
}

func TestTeamIssuesChildrenAssigned(t *testing.T) {
	f := newIssueFix(t)
	var out struct {
		Team   struct{ Issues issueConn }
		Issue  struct{ Children issueConn }
		Viewer struct{ AssignedIssues issueConn }
		User   struct{ AssignedIssues issueConn }
	}
	f.gql(t, f.ownerKey, `query($mia: String!) {
		team(id: "YLK") { issues(includeSubTeams: true, filter: { priority: { gte: 1 } }) { nodes { identifier } } }
		issue(id: "YLK-1") { children { nodes { identifier } } }
		viewer { assignedIssues { nodes { identifier } } }
		user(id: $mia) { assignedIssues(filter: { state: { type: { eq: "started" } } }) { nodes { identifier } } } }`,
		map[string]any{"mia": f.mia.ID.String()}).ok(t, &out)
	set(t, "team issues", idents(out.Team.Issues.Nodes), "YLK-1", "YLK-2", "YLK-4")
	set(t, "children", idents(out.Issue.Children.Nodes), "YLK-3")
	set(t, "viewer assigned", idents(out.Viewer.AssignedIssues.Nodes), "YLK-1")
	set(t, "mia assigned", idents(out.User.AssignedIssues.Nodes), "YLK-2")
	// Children stay listed when the project is archived.
	if _, err := f.deps.Projects.SetArchived(context.Background(), f.owner, f.ylk.ID, true); err != nil {
		t.Fatal(err)
	}
	var arch struct {
		Issue struct{ Children issueConn }
		Team  struct{ Issues issueConn }
	}
	f.gql(t, f.ownerKey, `{ issue(id: "YLK-1") { children { nodes { identifier } } } team(id: "YLK") { issues { nodes { identifier } } } }`, nil).ok(t, &arch)
	set(t, "children archived", idents(arch.Issue.Children.Nodes), "YLK-3")
	set(t, "archived team issues", idents(arch.Team.Issues.Nodes), "YLK-1", "YLK-2", "YLK-3", "YLK-4")
}

func TestIssueRelationsEmptyAndAttachments(t *testing.T) {
	f := newIssueFix(t)
	y1 := f.i["YLK-1"].ID
	a, err := f.deps.Attachments.Upload(context.Background(), f.mia, "report.txt", strings.NewReader("hello"), &y1)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Issue struct {
			Relations struct {
				Nodes []any
				Edges []any
			}
			Attachments struct {
				Nodes []struct {
					ID, Title, URL, CreatedAt, UpdatedAt string
					Metadata                             map[string]any
					Creator                              struct{ ID string }
					Issue                                struct{ ID string }
					GroupBySource                        bool
					Subtitle                             *string
				}
			}
			Named, Old struct{ Nodes []struct{ ID string } }
		}
	}
	f.gql(t, f.ownerKey, `{ issue(id: "YLK-1") {
		relations { nodes { id type issue { id } relatedIssue { id } } edges { cursor } }
		attachments { nodes { id title url createdAt updatedAt metadata creator { id } issue { id } groupBySource subtitle } }
		named: attachments(filter: { title: { eq: "report.txt" } }) { nodes { id } }
		old: attachments(filter: { createdAt: { lt: "2000-01-01" } }) { nodes { id } } } }`, nil).ok(t, &out)
	is := out.Issue
	if is.Relations.Nodes == nil || len(is.Relations.Nodes) != 0 || len(is.Relations.Edges) != 0 {
		t.Fatalf("relations = %+v", is.Relations)
	}
	if len(is.Attachments.Nodes) != 1 {
		t.Fatalf("attachments = %+v", is.Attachments)
	}
	at := is.Attachments.Nodes[0]
	if at.ID != a.ID.String() || at.Title != "report.txt" || at.URL != base+"/api/attachments/"+a.ID.String()+"/report.txt" ||
		at.Metadata["size"] != float64(5) || at.Metadata["contentType"] != a.ContentType || at.Creator.ID != f.mia.ID.String() ||
		at.Issue.ID != y1.String() || at.GroupBySource || at.Subtitle != nil || at.UpdatedAt != at.CreatedAt {
		t.Fatalf("attachment = %+v", at)
	}
	if len(is.Named.Nodes) != 1 || len(is.Old.Nodes) != 0 {
		t.Fatalf("filtered = %+v / %+v", is.Named, is.Old)
	}
}

// sdkIssueSelection is the field list a client fragment for Issue selects.
const sdkIssueSelection = `__typename trashed reactionData labelIds integrationSourceType url identifier priorityLabel previousIdentifiers
	reactions { __typename comment { id } externalUser { id } initiativeUpdate { id } issue { id } updatedAt emoji projectUpdate { id } archivedAt createdAt id
		user { ...U } }
	customerTicketCount
	sharedAccess { __typename disallowedIssueFields sharedWithCount sharedWithUsers { ...U } viewerHasOnlySharedAccess isShared }
	branchName delegate { id } botActor { __typename avatarUrl subType id name userDisplayName type } sourceComment { id } cycle { id }
	dueDate estimate
	syncedWith { __typename metadata { ... on ExternalEntityInfoGithubMetadata { __typename number owner repo }
		... on ExternalEntityInfoJiraMetadata { __typename issueTypeId projectId issueKey }
		... on ExternalEntitySlackMetadata { __typename messageUrl channelId channelName isFromSlack } } service id }
	externalUserCreator { id } asksExternalUserRequester { id } asksRequester { id } description title number lastAppliedTemplate { id }
	updatedAt boardOrder sortOrder prioritySortOrder subIssueSortOrder parent { id } priority projectMilestone { id } project { id }
	recurringIssueTemplate { id } team { id } archivedAt createdAt startedTriageAt triagedAt addedToCycleAt addedToProjectAt
	addedToTeamAt autoArchivedAt autoClosedAt canceledAt completedAt startedAt slaStartedAt slaBreachesAt slaHighRiskAt
	slaMediumRiskAt snoozedUntilAt slaType id assignee { id } creator { id } snoozedBy { id } favorite { id } state { id }
	inheritsSharedAccess`

const sdkUserFragment = `fragment U on User { __typename description avatarUrl createdIssueCount avatarBackgroundColor statusUntilAt
	statusEmoji initials updatedAt lastSeen timezone disableReason statusLabel archivedAt createdAt id gitHubUserId displayName
	email name title url active isAssignable isMentionable guest admin owner app isMe supportsAgentSessions
	canAccessAnyPublicTeam calendarHash inviteHash }`

func TestIssuesCostSDKSelection(t *testing.T) {
	f := newIssueFix(t)
	q := `query($first: Int) { issues(first: $first) { __typename nodes { ` + sdkIssueSelection + ` } pageInfo { __typename hasPreviousPage hasNextPage startCursor endCursor } } } ` + sdkUserFragment
	r := f.gql(t, f.ownerKey, q, map[string]any{"first": 50})
	var out struct{ Issues issueConn }
	r.ok(t, &out)
	cost, _ := strconv.Atoi(r.Header.Get("X-Complexity"))
	if len(out.Issues.Nodes) != 5 || cost < 1000 || cost >= 10_000 {
		t.Fatalf("first 50: %d nodes, cost %d", len(out.Issues.Nodes), cost)
	}
	g := f.gql(t, f.ownerKey, q, map[string]any{"first": 250}).fails(t)
	if g.Extensions["code"] != "COMPLEXITY_LIMIT_EXCEEDED" || !strings.HasPrefix(g.Message, "Query too complex: ") {
		t.Fatalf("first 250: %+v", g)
	}
	r = f.gql(t, f.ownerKey, `{ teams { nodes { issues(first: 250) { nodes { children(first: 250) { nodes { id } } } } } } }`, nil)
	if g := r.fails(t); g.Extensions["code"] != "COMPLEXITY_LIMIT_EXCEEDED" {
		t.Fatalf("nested: %+v", g)
	}
}

// counter is a pgx tracer that counts SQL statements.
type counter struct{ n atomic.Int64 }

func (c *counter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (c *counter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestIssuesQueryCount(t *testing.T) {
	ctx := context.Background()
	url, _ := testdb.New(t)
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	queries := &counter{}
	cfg.ConnConfig.Tracer = queries
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f := issueFixOn(t, newEnvOn(t, pool))
	for i := range 60 {
		y1 := f.i["YLK-1"].ID
		in := issue.CreateInput{ProjectID: &f.ylk.ID, Title: fmt.Sprintf("bulk %d", i), AssigneeID: &f.mia.ID}
		if i%3 == 0 {
			in.ParentID = &y1
		}
		if _, err := f.deps.Issues.Create(ctx, f.mia, in); err != nil {
			t.Fatal(err)
		}
	}
	count := func(first int) int64 {
		queries.n.Store(0)
		var out struct{ Issues issueConn }
		f.gql(t, f.ownerKey, fmt.Sprintf(`{ issues(first: %d) { nodes { id team { id } state { id } assignee { id } creator { id }
			parent { id } project { id } labelIds attachments { nodes { id } } } } }`, first), nil).ok(t, &out)
		if len(out.Issues.Nodes) != first {
			t.Fatalf("first %d: %d nodes", first, len(out.Issues.Nodes))
		}
		return queries.n.Load()
	}
	// Loader batches can split on a slow run (-race), so take the best of
	// three; one relation loaded per issue would add 45 statements or more.
	best := func(first int) int64 {
		n := count(first)
		for range 2 {
			n = min(n, count(first))
		}
		return n
	}
	small, large := best(5), best(50)
	if large > small+5 {
		t.Fatalf("first 50 ran %d statements, first 5 ran %d: loaders do not batch", large, small)
	}
}
