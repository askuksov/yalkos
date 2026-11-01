package mcpserver

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/project"
)

type searchOut struct {
	Issues []struct {
		Identifier string
	}
	TotalCount int
	NextCursor string
}

func (s searchOut) ids() []string {
	out := []string{}
	for _, i := range s.Issues {
		out = append(out, i.Identifier)
	}
	return out
}

func (e *env) search(t *testing.T, args map[string]any) searchOut {
	t.Helper()
	var out searchOut
	decode(t, e.ok(t, "search_issues", args), &out)
	return out
}

func idRange(from, to int) []string {
	var out []string
	for n := from; n >= to; n-- {
		out = append(out, fmt.Sprintf("YLK-%d", n))
	}
	return out
}

func TestSearchDefaults(t *testing.T) {
	e := newEnv(t)
	p := e.project(t, "YLK", "App")
	for n := range 30 {
		e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: fmt.Sprintf("issue %d", n+1)})
	}
	first := e.search(t, nil)
	if !slices.Equal(first.ids(), idRange(30, 6)) || first.TotalCount != 30 || first.NextCursor == "" {
		t.Fatalf("first page = %v total %d cursor %q", first.ids(), first.TotalCount, first.NextCursor)
	}
	second := e.search(t, map[string]any{"cursor": first.NextCursor})
	if !slices.Equal(second.ids(), idRange(5, 1)) || second.TotalCount != 30 || second.NextCursor != "" {
		t.Errorf("second page = %v total %d cursor %q", second.ids(), second.TotalCount, second.NextCursor)
	}
}

// filterFixture: YLK-1 parent (Owner, In Progress, Bug, urgent);
// YLK-2 "Retry bank sync" child of YLK-1 (unassigned, Done, Perf, high);
// YLK-3 without type (Member, milestone Bank APIs, low).
func filterFixture(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	ctx := context.Background()
	p := e.project(t, "YLK", "App")
	c := e.newCall(e.users.Owner)
	bug, err := c.typeLabel(ctx, "Bug")
	if err != nil {
		t.Fatal(err)
	}
	started, done := e.stateID(t, p, "In Progress"), e.stateID(t, p, "Done")
	perf := e.label(t, "Perf", nil)
	m := e.milestone(t, p, milestone.CreateInput{Name: "Bank APIs"})
	i1 := e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "Parent", AssigneeID: &e.users.Owner.ID, StateID: &started, LabelIDs: []uuid.UUID{*bug}, Priority: 1})
	e.issue(t, issue.CreateInput{ParentID: &i1.ID, Title: "Retry bank sync", StateID: &done, LabelIDs: []uuid.UUID{perf.ID}, Priority: 2})
	i3 := e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "Other thing", AssigneeID: &e.users.Member.ID, MilestoneID: &m.ID, Priority: 4})
	if _, err := e.deps.Issues.Update(ctx, e.users.Owner, i3.ID, issue.Patch{RemoveLabelIDs: i3.LabelIDs}); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestSearchFilters(t *testing.T) {
	e := filterFixture(t)
	cases := []struct {
		args map[string]any
		want []string
	}{
		{map[string]any{"assignee": "me"}, []string{"YLK-1"}},
		{map[string]any{"assignee": "none"}, []string{"YLK-2"}},
		{map[string]any{"stateType": []string{"started"}}, []string{"YLK-1"}},
		{map[string]any{"state": []string{"done"}}, []string{"YLK-2"}},
		{map[string]any{"project": "YLK", "state": []string{"In Progress", "Done"}}, []string{"YLK-1", "YLK-2"}},
		{map[string]any{"type": []string{"Bug"}}, []string{"YLK-1"}},
		{map[string]any{"type": []string{"none"}}, []string{"YLK-3"}},
		{map[string]any{"labels": []string{"perf"}}, []string{"YLK-2"}},
		{map[string]any{"milestone": "none"}, []string{"YLK-1", "YLK-2"}},
		{map[string]any{"project": "YLK", "milestone": "bank apis"}, []string{"YLK-3"}},
		{map[string]any{"parent": "YLK-1"}, []string{"YLK-2"}},
		{map[string]any{"parent": "none"}, []string{"YLK-1", "YLK-3"}},
		{map[string]any{"priority": []string{"urgent", "High"}}, []string{"YLK-1", "YLK-2"}},
		{map[string]any{"query": "retry"}, []string{"YLK-2"}},
		{map[string]any{"query": "YLK-3"}, []string{"YLK-3"}},
		{map[string]any{"assignee": "me", "priority": []string{"high"}}, []string{}},
	}
	for _, c := range cases {
		got := e.search(t, c.args).ids()
		slices.Sort(got)
		if !slices.Equal(got, c.want) {
			t.Errorf("%v → %v, want %v", c.args, got, c.want)
		}
	}
}

func TestSearchRejects(t *testing.T) {
	e := filterFixture(t)
	cases := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"type": []string{"Bug", "none"}}, `type is either type names or ["none"], not both.`},
		{map[string]any{"milestone": "Bank APIs"}, `milestone needs project when it is a name.`},
		{map[string]any{"orderBy": "size"}, `Unknown orderBy "size". Use one of: updated, created, priority, dueDate, manual.`},
		{map[string]any{"stateType": []string{"doing"}}, `Unknown stateType "doing". Use one of: backlog, unstarted, started, completed, canceled.`},
		{map[string]any{"cursor": "x"}, `cursor is invalid; repeat the search without it.`},
		{map[string]any{"state": []string{"Doing"}}, `No project has a state named "Doing".`},
		{map[string]any{"project": "YLK", "state": []string{"Doing"}}, `State "Doing" not found in project YLK. States: Backlog, Todo, In Progress, Done, Canceled.`},
		{map[string]any{"parent": "YLK-99"}, `Issue YLK-99 not found.`},
		{map[string]any{"limit": 101}, `limit must be 1–100.`},
		{map[string]any{"limit": -1}, `limit must be 1–100.`},
	}
	for _, c := range cases {
		if got := e.fail(t, "search_issues", c.args); got != c.want {
			t.Errorf("%v → %q, want %q", c.args, got, c.want)
		}
	}
	if got := e.search(t, map[string]any{"limit": 100}); got.TotalCount != 3 {
		t.Errorf("positive control limit 100 = %+v", got)
	}
}

func TestSearchSummaryShape(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.project(t, "YLK", "App")
	c := e.newCall(e.users.Owner)
	bug, err := c.typeLabel(ctx, "Bug")
	if err != nil {
		t.Fatal(err)
	}
	perf := e.label(t, "Perf", nil)
	area := e.group(t, "Area", false)
	api := e.label(t, "API", &area.ID)
	m := e.milestone(t, p, milestone.CreateInput{Name: "Bank APIs"})
	parent := e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "Parent"})
	started := e.stateID(t, p, "In Progress")
	due := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	i := e.issue(t, issue.CreateInput{
		ParentID: &parent.ID, Title: "Retry <bank> & sync", StateID: &started, Priority: 2,
		LabelIDs: []uuid.UUID{api.ID, *bug, perf.ID}, AssigneeID: &e.users.Admin.ID, MilestoneID: &m.ID, DueDate: &due,
	})
	want := `{"issues":[{"identifier":"YLK-2","title":"Retry <bank> & sync","url":"http://yalkos.test/issue/YLK-2","project":"YLK","state":"In Progress","stateType":"started","priority":"high","type":"Bug","labels":["Perf","Area/API"],"assignee":"` + e.users.Admin.Name + `","milestone":"Bank APIs","parent":"YLK-1","dueDate":"2026-12-01","updatedAt":"` + stamp(i.UpdatedAt) + `"}],"totalCount":1}`
	if got := e.ok(t, "search_issues", map[string]any{"query": "YLK-2"}); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := e.ok(t, "search_issues", map[string]any{"query": "YLK-1"}); strings.Contains(got, "description") || strings.Contains(got, `"type":"`) == false {
		t.Errorf("parent summary = %s", got)
	}
}

func TestSearchQueryCount(t *testing.T) {
	e, queries := newTracedEnv(t)
	p := e.project(t, "YLK", "App")
	perf := e.label(t, "Perf", nil)
	m := e.milestone(t, p, milestone.CreateInput{Name: "Bank APIs"})
	var parents []issue.Issue
	for n := range 2 {
		parents = append(parents, e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: fmt.Sprintf("parent %d", n)}))
	}
	assignees := []uuid.UUID{e.users.Owner.ID, e.users.Admin.ID, e.users.Member.ID}
	for n := range 60 {
		e.issue(t, issue.CreateInput{
			ParentID: &parents[n%2].ID, Title: fmt.Sprintf("child %d", n), LabelIDs: []uuid.UUID{perf.ID},
			MilestoneID: &m.ID, AssigneeID: &assignees[n%3],
		})
	}
	count := func(limit int) int64 {
		before := queries.n.Load()
		out := e.search(t, map[string]any{"limit": limit, "orderBy": "updated"})
		if len(out.Issues) != limit {
			t.Fatalf("limit %d: %d issues", limit, len(out.Issues))
		}
		return queries.n.Load() - before
	}
	count(5) // warm up: API key lookup, connections
	small, large := count(5), count(50)
	if small != large {
		t.Errorf("statements: limit 5 → %d, limit 50 → %d", small, large)
	}
	t.Logf("search_issues runs %d statements", small)
}

type issueJSON struct {
	Identifier, URL, Title, Description string
	DescriptionTruncated                bool
	Project                             struct{ Key, Name string }
	State, StateType, Priority, Type    string
	Labels                              []string
	Assignee, Creator                   *struct{ Name, Email string }
	Milestone                           string
	Parent                              *struct{ Identifier, Title string }
	SubIssues                           *struct {
		Completed, Total int
		Items            []struct{ Identifier, Title, State string }
	}
	DueDate, CreatedAt, UpdatedAt, CompletedAt, CanceledAt string
	Attachments                                            []struct {
		Name string
		Size int64
		URL  string
	}
	CommentCount int
	Comments     []commentJSON
}

type commentJSON struct {
	ID, Author, CreatedAt, Body string
	Edited, Truncated           bool
}

func (e *env) getIssue(t *testing.T, ref string) issueJSON {
	t.Helper()
	var out issueJSON
	decode(t, e.ok(t, "get_issue", map[string]any{"issue": ref}), &out)
	return out
}

func (e *env) comment(t *testing.T, i issue.Issue, body string) {
	t.Helper()
	if _, err := e.deps.Comments.Create(context.Background(), e.users.Owner, i.ID, body, nil); err != nil {
		t.Fatal(err)
	}
}

func TestGetIssue(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.project(t, "YLK", "App")
	parent := e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "Parent"})
	i := e.issue(t, issue.CreateInput{ParentID: &parent.ID, Title: "Retry bank sync", AssigneeID: &e.users.Admin.ID, Priority: 2})
	a, err := e.deps.Attachments.Upload(ctx, e.users.Owner, "shot.png", strings.NewReader("png bytes"), &i.ID)
	if err != nil {
		t.Fatal(err)
	}
	desc := "See ![shot](/api/attachments/" + a.ID.String() + "/shot.png)"
	if _, err := e.deps.Issues.Update(ctx, e.users.Owner, i.ID, issue.Patch{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	done, canceled := e.stateID(t, p, "Done"), e.stateID(t, p, "Canceled")
	e.issue(t, issue.CreateInput{ParentID: &i.ID, Title: "child done", StateID: &done})
	e.issue(t, issue.CreateInput{ParentID: &i.ID, Title: "child open"})
	e.issue(t, issue.CreateInput{ParentID: &i.ID, Title: "child canceled", StateID: &canceled})
	for n := range 7 {
		e.comment(t, i, fmt.Sprintf("comment %d", n+1))
	}

	got := e.getIssue(t, "ylk-2")
	if got.Identifier != "YLK-2" || got.URL != "http://yalkos.test/issue/YLK-2" || got.Title != "Retry bank sync" {
		t.Errorf("head = %+v", got)
	}
	if want := "See ![shot](http://yalkos.test/api/attachments/" + a.ID.String() + "/shot.png)"; got.Description != want || got.DescriptionTruncated {
		t.Errorf("description = %q", got.Description)
	}
	if got.Project.Key != "YLK" || got.Project.Name != "App" || got.State != "Todo" || got.StateType != "unstarted" || got.Priority != "high" || got.Type != "Task" || len(got.Labels) != 0 {
		t.Errorf("fields = %+v", got)
	}
	if got.Assignee == nil || got.Assignee.Email != e.users.Admin.Email || got.Creator == nil || got.Creator.Email != "owner@example.com" {
		t.Errorf("people = %+v %+v", got.Assignee, got.Creator)
	}
	if got.Parent == nil || got.Parent.Identifier != "YLK-1" || got.Parent.Title != "Parent" {
		t.Errorf("parent = %+v", got.Parent)
	}
	s := got.SubIssues
	if s == nil || s.Completed != 1 || s.Total != 2 || len(s.Items) != 3 {
		t.Fatalf("subIssues = %+v", s)
	}
	states := map[string]string{}
	for _, it := range s.Items {
		states[it.Identifier+" "+it.Title] = it.State
	}
	if states["YLK-3 child done"] != "Done" || states["YLK-4 child open"] != "Todo" || states["YLK-5 child canceled"] != "Canceled" {
		t.Errorf("subIssues items = %+v", s.Items)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].Name != "shot.png" || got.Attachments[0].Size != 9 ||
		got.Attachments[0].URL != "http://yalkos.test/api/attachments/"+a.ID.String()+"/shot.png" {
		t.Errorf("attachments = %+v", got.Attachments)
	}
	if got.CommentCount != 7 || len(got.Comments) != 5 || got.Comments[0].Body != "comment 3" || got.Comments[4].Body != "comment 7" {
		t.Errorf("comments = %d %+v", got.CommentCount, got.Comments)
	}
	if c := got.Comments[0]; c.Author != "Owner" || c.Edited || len(c.CreatedAt) != len("2026-11-01 11:05") {
		t.Errorf("comment = %+v", c)
	}
	if got.CreatedAt == "" || got.UpdatedAt == "" || got.CompletedAt != "" || got.DueDate != "" {
		t.Errorf("times = %q %q %q %q", got.CreatedAt, got.UpdatedAt, got.CompletedAt, got.DueDate)
	}
}

func TestGetIssueTruncates(t *testing.T) {
	e := newEnv(t)
	p := e.project(t, "YLK", "App")
	i := e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "Big", Description: strings.Repeat("d", 100_000)})
	e.comment(t, i, strings.Repeat("ж", 5_000))
	text := e.ok(t, "get_issue", map[string]any{"issue": "YLK-1"})
	var got issueJSON
	decode(t, text, &got)
	if got.Description != strings.Repeat("d", 20_000)+"…" || !got.DescriptionTruncated {
		t.Errorf("description %d runes, truncated %v", len([]rune(got.Description)), got.DescriptionTruncated)
	}
	if len(got.Comments) != 1 || got.Comments[0].Body != strings.Repeat("ж", 2_000)+"…" || !got.Comments[0].Truncated {
		t.Errorf("comment = %d runes, truncated %v", len([]rune(got.Comments[0].Body)), got.Comments[0].Truncated)
	}
	if len(text) >= 40_000 {
		t.Errorf("answer is %d bytes", len(text))
	}
}

func TestGetIssueNotFound(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.project(t, "YLK", "App")
	e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "one"})
	if got, want := e.fail(t, "get_issue", map[string]any{"issue": "YLK-999"}), `Issue YLK-999 not found.`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	key := "APP"
	if _, err := e.deps.Projects.Update(ctx, e.users.Owner, p.ID, project.UpdateInput{Key: &key}); err != nil {
		t.Fatal(err)
	}
	if got := e.getIssue(t, "YLK-1"); got.Identifier != "APP-1" || got.URL != "http://yalkos.test/issue/APP-1" {
		t.Errorf("former key = %+v", got)
	}
}

func TestListComments(t *testing.T) {
	e := newEnv(t)
	p := e.project(t, "YLK", "App")
	i := e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "Chatty"})
	for n := range 24 {
		e.comment(t, i, fmt.Sprintf("comment %d", n+1))
	}
	e.comment(t, i, strings.Repeat("ж", 12_000))
	type page struct {
		Issue       string
		Comments    []commentJSON
		OlderCursor string
	}
	bodies := func(pg page) []string {
		var out []string
		for _, c := range pg.Comments {
			out = append(out, c.Body)
		}
		return out
	}
	want := func(from, to int) []string {
		var out []string
		for n := from; n <= to; n++ {
			out = append(out, fmt.Sprintf("comment %d", n))
		}
		return out
	}
	var p1, p2, p3 page
	decode(t, e.ok(t, "list_comments", map[string]any{"issue": "YLK-1", "limit": 10}), &p1)
	if p1.Issue != "YLK-1" || p1.OlderCursor == "" || len(p1.Comments) != 10 {
		t.Fatalf("page 1 = %+v", p1)
	}
	last := p1.Comments[9]
	if !slices.Equal(bodies(p1)[:9], want(16, 24)) || last.Body != strings.Repeat("ж", 10_000)+"…" || !last.Truncated {
		t.Errorf("page 1 bodies = %v… (last %d runes)", bodies(p1)[:9], len([]rune(last.Body)))
	}
	decode(t, e.ok(t, "list_comments", map[string]any{"issue": "YLK-1", "limit": 10, "cursor": p1.OlderCursor}), &p2)
	if !slices.Equal(bodies(p2), want(6, 15)) || p2.OlderCursor == "" {
		t.Errorf("page 2 = %v cursor %q", bodies(p2), p2.OlderCursor)
	}
	decode(t, e.ok(t, "list_comments", map[string]any{"issue": "YLK-1", "limit": 10, "cursor": p2.OlderCursor}), &p3)
	if !slices.Equal(bodies(p3), want(1, 5)) || p3.OlderCursor != "" {
		t.Errorf("page 3 = %v cursor %q", bodies(p3), p3.OlderCursor)
	}
	var def page
	decode(t, e.ok(t, "list_comments", map[string]any{"issue": "YLK-1"}), &def)
	if len(def.Comments) != 20 {
		t.Errorf("default limit = %d", len(def.Comments))
	}
	if got := e.fail(t, "list_comments", map[string]any{"issue": "YLK-1", "limit": 51}); got != `limit must be 1–50.` {
		t.Errorf("limit 51 = %q", got)
	}
	if got := e.fail(t, "list_comments", map[string]any{"issue": "YLK-1", "cursor": "x"}); got != `cursor is invalid; repeat the search without it.` {
		t.Errorf("bad cursor = %q", got)
	}
}
