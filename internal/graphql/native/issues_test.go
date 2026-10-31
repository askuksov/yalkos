package native_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/db"
	"github.com/askuksov/yalkos/internal/authn"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/graphql/native"
	"github.com/askuksov/yalkos/internal/store"
	"github.com/askuksov/yalkos/internal/testdb"
)

type gqlIssue struct {
	ID          string  `json:"id"`
	Identifier  string  `json:"identifier"`
	Number      int     `json:"number"`
	Title       string  `json:"title"`
	Priority    int     `json:"priority"`
	DueDate     *string `json:"dueDate"`
	CompletedAt *string `json:"completedAt"`
	State       struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"state"`
	Project struct {
		Key string `json:"key"`
	} `json:"project"`
	Assignee *struct {
		ID string `json:"id"`
	} `json:"assignee"`
	Parent *struct {
		ID string `json:"id"`
	} `json:"parent"`
	Children struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	} `json:"children"`
}

const issueFields = `id identifier number title priority dueDate completedAt state { id name } project { key } assignee { id } parent { id }`

func createIssue(t *testing.T, c *client, in map[string]any) gqlIssue {
	t.Helper()
	var i gqlIssue
	c.do(`mutation($in: CreateIssueInput!) { createIssue(input: $in) { `+issueFields+` } }`, map[string]any{"in": in}).decode(t, "createIssue", &i)
	return i
}

func viewerID(t *testing.T, c *client) string {
	t.Helper()
	var v struct {
		ID string `json:"id"`
	}
	c.do(`{ viewer { id } }`, nil).decode(t, "viewer", &v)
	return v.ID
}

func stateID(p gqlProject, name string) string {
	for _, s := range p.States {
		if s.Name == name {
			return s.ID
		}
	}
	return ""
}

func issueIDs(t *testing.T, c *client, filter map[string]any) []string {
	t.Helper()
	var conn struct {
		Nodes []gqlIssue `json:"nodes"`
	}
	c.do(`query($f: IssueFilter) { issues(filter: $f) { nodes { id } } }`, map[string]any{"f": filter}).decode(t, "issues", &conn)
	out := make([]string, 0, len(conn.Nodes))
	for _, i := range conn.Nodes {
		out = append(out, i.ID)
	}
	slices.Sort(out)
	return out
}

func sorted(ids ...string) []string {
	out := slices.Clone(ids)
	slices.Sort(out)
	return out
}

func TestIssueFlowAPI(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	p := createProject(t, owner, "YLK", "Yalkos")
	i := createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "  First  ", "assigneeId": viewerID(t, owner)})
	if i.Identifier != "YLK-1" || i.Number != 1 || i.Title != "First" || i.State.Name != "Todo" || i.Project.Key != "YLK" || i.Assignee == nil {
		t.Fatalf("createIssue: %+v", i)
	}

	var got gqlIssue
	owner.do(`{ issue(id: "ylk-1") { `+issueFields+` } }`, nil).decode(t, "issue", &got)
	if got.ID != i.ID {
		t.Fatalf("issue by identifier: %+v", got)
	}

	var upd gqlIssue
	owner.do(`mutation($id: ID!, $in: UpdateIssueInput!) { updateIssue(id: $id, input: $in) { `+issueFields+` } }`,
		map[string]any{"id": i.ID, "in": map[string]any{"assigneeId": nil, "priority": 2, "stateId": stateID(p, "Done")}}).decode(t, "updateIssue", &upd)
	if upd.Assignee != nil || upd.Priority != 2 || upd.State.Name != "Done" || upd.CompletedAt == nil || upd.Title != "First" {
		t.Fatalf("updateIssue: %+v", upd)
	}
	r := owner.do(`mutation($id: ID!) { updateIssue(id: $id, input: {title: null}) { id } }`, map[string]any{"id": i.ID})
	if r.code() != "INVALID_INPUT" || r.field() != "title" || r.reason() != "required" {
		t.Fatalf("title: null: %s", r.raw)
	}

	if r := owner.do(`mutation($id: ID!) { deleteIssue(id: $id) }`, map[string]any{"id": i.ID}); string(r.Data["deleteIssue"]) != "true" {
		t.Fatalf("deleteIssue: %s", r.raw)
	}
	if r := owner.do(`{ issue(id: "YLK-1") { id } }`, nil); r.code() != "NOT_FOUND" {
		t.Fatalf("deleted issue: %s", r.raw)
	}
}

func TestSubIssuesAPI(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	p := createProject(t, owner, "YLK", "Yalkos")
	parent := createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "Parent"})
	child := createIssue(t, owner, map[string]any{"parentId": parent.ID, "title": "Child"})
	if child.Project.Key != "YLK" || child.Parent == nil || child.Parent.ID != parent.ID {
		t.Fatalf("child: %+v", child)
	}
	var got gqlIssue
	owner.do(`query($id: String!) { issue(id: $id) { id children { nodes { id } } } }`, map[string]any{"id": parent.ID}).decode(t, "issue", &got)
	if len(got.Children.Nodes) != 1 || got.Children.Nodes[0].ID != child.ID {
		t.Fatalf("children: %+v", got)
	}
	r := owner.do(`mutation($id: ID!, $p: ID) { updateIssue(id: $id, input: {parentId: $p}) { id } }`, map[string]any{"id": parent.ID, "p": child.ID})
	if r.code() != "INVALID_INPUT" || r.field() != "parentId" || r.reason() != "cycle" {
		t.Fatalf("cycle: %s", r.raw)
	}
	var detached gqlIssue
	owner.do(`mutation($id: ID!) { updateIssue(id: $id, input: {parentId: null}) { `+issueFields+` } }`, map[string]any{"id": child.ID}).decode(t, "updateIssue", &detached)
	if detached.Parent != nil {
		t.Fatalf("parentId: null: %+v", detached)
	}
}

func TestMoveIssueAPI(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	ylk := createProject(t, owner, "YLK", "Yalkos")
	web := createProject(t, owner, "WEB", "Web")
	i := createIssue(t, owner, map[string]any{"projectId": ylk.ID, "title": "Move me"})
	var moved gqlIssue
	owner.do(`mutation($id: ID!, $p: ID!) { moveIssue(id: $id, projectId: $p) { `+issueFields+` } }`,
		map[string]any{"id": i.ID, "p": web.ID}).decode(t, "moveIssue", &moved)
	if moved.Identifier != "WEB-1" || moved.Project.Key != "WEB" || moved.State.ID != stateID(web, "Todo") {
		t.Fatalf("moveIssue: %+v", moved)
	}
	var old gqlIssue
	owner.do(`{ issue(id: "YLK-1") { `+issueFields+` } }`, nil).decode(t, "issue", &old)
	if old.ID != i.ID || old.Identifier != "WEB-1" {
		t.Fatalf("old identifier: %+v", old)
	}
	r := owner.do(`mutation($id: ID!, $p: ID!) { moveIssue(id: $id, projectId: $p) { id } }`, map[string]any{"id": i.ID, "p": web.ID})
	if r.code() != "INVALID_INPUT" || r.field() != "projectId" || r.reason() != "not_allowed" {
		t.Fatalf("same project: %s", r.raw)
	}
}

func TestIssuesFilterAPI(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	member := userBrowser(t, s, owner, "MEMBER")
	me, other := viewerID(t, owner), viewerID(t, member)
	p := createProject(t, owner, "YLK", "Yalkos")
	mine := createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "Mine", "assigneeId": me, "priority": 1, "dueDate": "2026-01-15"})
	theirs := createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "Theirs", "assigneeId": other, "priority": 3, "dueDate": "2026-03-01"})
	done := createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "Done", "stateId": stateID(p, "Done"), "priority": 2})
	if mine.DueDate == nil || *mine.DueDate != "2026-01-15" {
		t.Fatalf("dueDate: %+v", mine)
	}

	cases := []struct {
		name   string
		filter map[string]any
		want   []string
	}{
		{"isMe", map[string]any{"assignee": map[string]any{"isMe": true}}, sorted(mine.ID)},
		{"not me", map[string]any{"assignee": map[string]any{"isMe": false}}, sorted(theirs.ID, done.ID)},
		{"isMe and eq other", map[string]any{"assignee": map[string]any{"isMe": true, "eq": other}}, []string{}},
		{"unassigned", map[string]any{"assignee": map[string]any{"null": true}}, sorted(done.ID)},
		{"stateType", map[string]any{"stateType": map[string]any{"in": []string{"COMPLETED"}}}, sorted(done.ID)},
		{"dueDate lt", map[string]any{"dueDate": map[string]any{"lt": "2026-02-01"}}, sorted(mine.ID)},
		{"priority range", map[string]any{"priority": map[string]any{"gte": 1, "lte": 2}}, sorted(mine.ID, done.ID)},
		{"project in []", map[string]any{"project": map[string]any{"in": []string{}}}, []string{}},
	}
	for _, c := range cases {
		if got := issueIDs(t, owner, c.filter); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestUnknownFilterFieldRejected(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	r := owner.do(`{ issues(filter: {color: {eq: "x"}}) { nodes { id } } }`, nil)
	if len(r.Errors) == 0 || !strings.Contains(r.Errors[0].Message, "color") || r.Data["issues"] != nil {
		t.Fatalf("got %s", r.raw)
	}
}

func TestBadDateRejected(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	p := createProject(t, owner, "YLK", "Yalkos")
	r := owner.do(`mutation($in: CreateIssueInput!) { createIssue(input: $in) { id } }`,
		map[string]any{"in": map[string]any{"projectId": p.ID, "title": "X", "dueDate": "2026-13-01"}})
	if len(r.Errors) == 0 || r.Errors[0].Message != "invalid date, expected YYYY-MM-DD" {
		t.Fatalf("got %s", r.raw)
	}
	if r.code() != "INVALID_INPUT" || r.field() != "dueDate" || r.reason() != "invalid_format" {
		t.Fatalf("extensions: %s", r.raw)
	}
	if r := owner.do(`{ issues { nodes { id } } }`, nil); strings.Contains(r.raw, `"id"`) {
		t.Fatalf("issue created despite bad date: %s", r.raw)
	}
}

func TestIssueHistoryAPI(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	p := createProject(t, owner, "YLK", "Yalkos")
	i := createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "History"})
	owner.do(`mutation($id: ID!) { updateIssue(id: $id, input: {priority: 2}) { id } }`, map[string]any{"id": i.ID}).decode(t, "updateIssue", &struct{}{})

	type change struct {
		Field string  `json:"field"`
		From  *string `json:"from"`
		To    *string `json:"to"`
	}
	var got struct {
		History struct {
			Nodes []struct {
				Action string `json:"action"`
				Actor  *struct {
					ID string `json:"id"`
				} `json:"actor"`
				Changes []change `json:"changes"`
			} `json:"nodes"`
		} `json:"history"`
	}
	owner.do(`query($id: String!) { issue(id: $id) { history { nodes { action actor { id } changes { field from to } } } } }`,
		map[string]any{"id": i.ID}).decode(t, "issue", &got)
	nodes := got.History.Nodes
	if len(nodes) != 2 || nodes[0].Action != "create" || nodes[1].Action != "update" {
		t.Fatalf("history: %+v", nodes)
	}
	if nodes[1].Actor == nil || nodes[1].Actor.ID != viewerID(t, owner) {
		t.Fatalf("actor: %+v", nodes[1])
	}
	ch := nodes[1].Changes
	if len(ch) != 1 || ch[0].Field != "priority" || ch[0].From == nil || *ch[0].From != "0" || ch[0].To == nil || *ch[0].To != "2" {
		t.Fatalf("changes: %+v", ch)
	}
	for _, c := range nodes[0].Changes {
		if c.From != nil {
			t.Fatalf("create change has from: %+v", c)
		}
		if c.Field == "title" && (c.To == nil || *c.To != `"History"`) {
			t.Fatalf("create title: %+v", c)
		}
	}
}

func TestIssuesPaginationAPI(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	p := createProject(t, owner, "YLK", "Yalkos")
	for n := range 3 {
		createIssue(t, owner, map[string]any{"projectId": p.ID, "title": fmt.Sprintf("Issue %d", n)})
	}
	type conn struct {
		Nodes    []gqlIssue `json:"nodes"`
		PageInfo struct {
			HasNextPage bool   `json:"hasNextPage"`
			EndCursor   string `json:"endCursor"`
		} `json:"pageInfo"`
	}
	q := `query($f: Int, $a: String, $o: IssueOrder) { issues(first: $f, after: $a, orderBy: $o) { nodes { title } pageInfo { hasNextPage endCursor } } }`
	var c1 conn
	owner.do(q, map[string]any{"f": 2}).decode(t, "issues", &c1)
	// New issues go on top: the newest comes first in SORT_ORDER.
	if len(c1.Nodes) != 2 || !c1.PageInfo.HasNextPage || c1.Nodes[0].Title != "Issue 2" {
		t.Fatalf("first page: %+v", c1)
	}
	var c2 conn
	owner.do(q, map[string]any{"f": 2, "a": c1.PageInfo.EndCursor}).decode(t, "issues", &c2)
	if len(c2.Nodes) != 1 || c2.PageInfo.HasNextPage || c2.Nodes[0].Title != "Issue 0" {
		t.Fatalf("second page: %+v", c2)
	}
	var byCreated conn
	owner.do(q, map[string]any{"f": 1, "o": map[string]any{"field": "CREATED_AT", "direction": "DESC"}}).decode(t, "issues", &byCreated)
	if len(byCreated.Nodes) != 1 || byCreated.Nodes[0].Title != "Issue 2" {
		t.Fatalf("CREATED_AT DESC: %+v", byCreated)
	}
	r := owner.do(q, map[string]any{"f": 2, "a": c1.PageInfo.EndCursor, "o": map[string]any{"field": "PRIORITY"}})
	if r.code() != "INVALID_INPUT" || r.field() != "after" || r.reason() != "invalid_format" {
		t.Fatalf("foreign cursor: %s", r.raw)
	}
}

func newHTTPServer(t *testing.T, h http.Handler) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

// counter is a pgx tracer that counts SQL statements.
type counter struct{ n atomic.Int64 }

func (c *counter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (c *counter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// tracedServer is newServer on a pool that counts SQL statements.
func tracedServer(t *testing.T) (server, *counter) {
	t.Helper()
	ctx := context.Background()
	dbURL, setupPool := testdb.New(t)
	if err := store.Migrate(ctx, setupPool, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dbURL)
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

	base, _ := url.Parse("http://yalkos.test")
	svc := account.New(pool, time.Now, base.String())
	if err := svc.RefreshSetupCode(ctx); err != nil {
		t.Fatal(err)
	}
	h := native.NewHandler(testDeps(pool, svc))
	return server{url: newHTTPServer(t, authn.Middleware(svc, base, quiet)(h)), svc: svc, pool: pool}, queries
}

// seedIssues creates 50 issues in two projects: two parents and 48 sub-issues.
func seedIssues(t *testing.T, owner *client) {
	t.Helper()
	me := viewerID(t, owner)
	ylk := createProject(t, owner, "YLK", "Yalkos")
	web := createProject(t, owner, "WEB", "Web")
	parents := map[string]string{
		ylk.ID: createIssue(t, owner, map[string]any{"projectId": ylk.ID, "title": "Parent"}).ID,
		web.ID: createIssue(t, owner, map[string]any{"projectId": web.ID, "title": "Parent"}).ID,
	}
	for n := range 48 {
		pid := ylk.ID
		if n%2 == 1 {
			pid = web.ID
		}
		createIssue(t, owner, map[string]any{"parentId": parents[pid], "title": fmt.Sprintf("Issue %d", n), "assigneeId": me})
	}
}

func countList(t *testing.T, owner *client, queries *counter, selection string) int64 {
	t.Helper()
	queries.n.Store(0)
	var conn struct {
		Nodes []json.RawMessage `json:"nodes"`
	}
	owner.do(`{ issues(first: 50) { nodes { `+selection+` } } }`, nil).decode(t, "issues", &conn)
	if len(conn.Nodes) != 50 {
		t.Fatalf("nodes = %d", len(conn.Nodes))
	}
	n := queries.n.Load()
	t.Logf("%s: %d SQL statements", selection, n)
	return n
}

func TestIssueListQueryCount(t *testing.T) {
	s, queries := tracedServer(t)
	owner := ownerBrowser(t, s)
	seedIssues(t, owner)
	if n := countList(t, owner, queries, `id project { key } state { name } assignee { id } creator { id } parent { id }`); n > 8 {
		t.Fatalf("issue list ran %d SQL statements, want ≤ 8", n)
	}
	// Project states and members batch by project too.
	if n := countList(t, owner, queries, `id project { key states { id } members { id } }`); n > 8 {
		t.Fatalf("issue list with project states/members ran %d SQL statements, want ≤ 8", n)
	}
	// Sub-issue progress batches into one query for the page.
	if n := countList(t, owner, queries, `id subIssueProgress { completed total }`); n > 8 {
		t.Fatalf("issue list with sub-issue progress ran %d SQL statements, want ≤ 8", n)
	}
	// Labels, type and milestone (with its progress) batch per page too.
	if n := countList(t, owner, queries, `id type { id name color } labels { id name color parent { id } }
		milestone { id name status progress { completed total } }`); n > 10 {
		t.Fatalf("issue list with labels and milestone ran %d SQL statements, want ≤ 10", n)
	}
}

// Connections cost their page size times their selection, so nested lists
// cannot multiply into thousands of SQL statements.
func TestQueryCostByPageSize(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	rejected := map[string]string{
		"nested children":  `{ issues(first: 250) { nodes { children(first: 250) { nodes { id } } } } }`,
		"history per item": `{ issues(first: 250) { nodes { history { nodes { id action } } } } }`,
		"deep nesting":     "{ issues { nodes { " + strings.Repeat("children { nodes { ", 8) + "id" + strings.Repeat(" } }", 8) + " } } }",
		"states cycle":     `{ project(id: "YLK") { ` + strings.Repeat("states { project { ", 4) + "id" + strings.Repeat(" } }", 4) + " } }",
		"members fan-out":  `{ projects(first: 250) { nodes { members { id email } } } }`,
		"states cycle x3":  `{ project(id: "YLK") { ` + strings.Repeat("states { project { ", 2) + "states { id }" + strings.Repeat(" } }", 2) + " } }",
		"projects states":  `{ projects(first: 250) { nodes { states { id } members { id } } } issues(first: 250) { nodes { children(first: 50) { nodes { id title } } } } }`,
		// Each count is its own aggregate query, so counts weigh more than a scalar.
		"counts per item": `{ issues(first: 250) { nodes { a: children(first: 1) { totalCount } b: children(first: 1) { totalCount }
			c: children(first: 1) { totalCount } d: children(first: 1) { totalCount } } } }`,
		"milestone issues per item": `{ issues(first: 250) { nodes { milestone { issues(first: 250) { nodes { id } } } } } }`,
		"milestones per project":    `{ projects(first: 250) { nodes { milestones { issues(first: 50) { nodes { id } } } } } }`,
		"labels per label":          `{ labels { children { children { children { id name color description } } } } }`,
	}
	for name, q := range rejected {
		if r := owner.do(q, nil); r.code() != "COMPLEXITY_LIMIT_EXCEEDED" {
			t.Errorf("%s: %s", name, r.raw[:min(len(r.raw), 300)])
		}
	}
	allowed := map[string]string{
		"board page": `{ issues(first: 250) { nodes { id identifier number title priority dueDate sortOrder createdAt updatedAt
			project { id key } state { id name type } assignee { id name } creator { id name } parent { id identifier } }
			pageInfo { hasNextPage endCursor } } }`,
		"issue page": `{ issue(id: "YLK-1") { id title description children { nodes { id identifier title state { name } } }
			history { nodes { id action createdAt actor { name } changes { field from to } } } } }`,
		"projects":                  `{ projects { nodes { id key name states { id name type color position isDefault } members { id name } } } }`,
		"state with project states": `{ project(id: "YLK") { states { id name project { id states { id name } } } } }`,
		"list group": `{ issues(first: 50) { totalCount nodes { id identifier title priority dueDate sortOrder
			state { id name type color } assignee { id name } parent { id identifier } subIssueProgress { completed total }
			project { id key archivedAt } } pageInfo { hasNextPage endCursor } } }`,
		"board page with progress": `{ issues(first: 250) { nodes { id identifier title subIssueProgress { completed total } } } }`,
		"milestones page": `{ project(id: "YLK") { milestones { id name status statusAutomatic progress { completed total } overdue
			targetDate lead { id name } } } }`,
		"milestone page": `{ milestone(id: "00000000-0000-0000-0000-000000000000") { id name description issues(first: 100) { totalCount
			nodes { id identifier title type { id name color } labels { id name color } state { id name type } } } } }`,
		"labels settings": `{ labels { id name color description isGroup exclusive position children { id name color description position parent { id } } }
			labelSettings { typeGroup { id } defaultType { id } } }`,
		"board page with labels": `{ issues(first: 250) { nodes { id identifier title type { id name color } labels { id name color }
			milestone { id name } } } }`,
	}
	for name, q := range allowed {
		if r := owner.do(q, nil); r.code() == "COMPLEXITY_LIMIT_EXCEEDED" {
			t.Errorf("%s rejected: %s", name, r.raw[:min(len(r.raw), 300)])
		}
	}
}

func TestIssuesTotalCountAPI(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	p := createProject(t, owner, "YLK", "Yalkos")
	parent := createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "Fix login redirect"})
	createIssue(t, owner, map[string]any{"parentId": parent.ID, "title": "Child one"})
	createIssue(t, owner, map[string]any{"parentId": parent.ID, "title": "Child two", "stateId": stateID(p, "Done")})

	var conn struct {
		TotalCount int        `json:"totalCount"`
		Nodes      []gqlIssue `json:"nodes"`
	}
	owner.do(`{ issues(first: 1) { totalCount nodes { id } } }`, nil).decode(t, "issues", &conn)
	if conn.TotalCount != 3 || len(conn.Nodes) != 1 {
		t.Fatalf("issues(first: 1) = %+v, want totalCount 3 and one node", conn)
	}
	owner.do(`{ issues(filter: { search: "logi" }) { totalCount nodes { id } } }`, nil).decode(t, "issues", &conn)
	if conn.TotalCount != 1 || len(conn.Nodes) != 1 || conn.Nodes[0].ID != parent.ID {
		t.Fatalf("search logi = %+v", conn)
	}

	var got struct {
		Children struct {
			TotalCount int `json:"totalCount"`
		} `json:"children"`
		SubIssueProgress struct {
			Completed int `json:"completed"`
			Total     int `json:"total"`
		} `json:"subIssueProgress"`
	}
	owner.do(`{ issue(id: "YLK-1") { children(first: 1) { totalCount } subIssueProgress { completed total } } }`, nil).decode(t, "issue", &got)
	if got.Children.TotalCount != 2 || got.SubIssueProgress.Completed != 1 || got.SubIssueProgress.Total != 2 {
		t.Fatalf("children count / progress = %+v", got)
	}
}
