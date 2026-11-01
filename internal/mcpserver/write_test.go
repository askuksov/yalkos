package mcpserver

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/page"
)

type summaryJSON struct {
	Identifier, Title, URL, Project, State, StateType, Priority, Type string
	Labels                                                            []string
	Assignee, Milestone, Parent, DueDate, UpdatedAt                   string
}

func (e *env) write(t *testing.T, tool string, args map[string]any) summaryJSON {
	t.Helper()
	var out summaryJSON
	decode(t, e.ok(t, tool, args), &out)
	return out
}

func TestCreateIssue(t *testing.T) {
	e := newEnv(t)
	e.project(t, "YLK", "App")
	e.label(t, "Perf", nil)
	got := e.write(t, "create_issue", map[string]any{
		"project": "ylk", "title": "Retry bank sync", "description": "Body", "type": "bug", "labels": []string{"Perf"},
		"assignee": "me", "priority": "urgent", "dueDate": "2026-12-01", "state": "in progress",
	})
	want := summaryJSON{
		Identifier: "YLK-1", Title: "Retry bank sync", URL: "http://yalkos.test/issue/YLK-1", Project: "YLK",
		State: "In Progress", StateType: "started", Priority: "urgent", Type: "Bug", Labels: []string{"Perf"},
		Assignee: "Owner", DueDate: "2026-12-01", UpdatedAt: got.UpdatedAt,
	}
	if !equalSummary(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	if i := e.getIssue(t, "YLK-1"); i.Description != "Body" {
		t.Errorf("description = %q", i.Description)
	}
	plain := e.write(t, "create_issue", map[string]any{"project": "YLK", "title": "Plain"})
	if plain.Identifier != "YLK-2" || plain.Type != "Task" || plain.State != "Todo" || plain.Priority != "none" || len(plain.Labels) != 0 {
		t.Errorf("defaults = %+v", plain)
	}
	if got := e.fail(t, "create_issue", map[string]any{"title": "Lost"}); got != `project is required unless parent is given.` {
		t.Errorf("no project = %q", got)
	}
}

func equalSummary(a, b summaryJSON) bool { return reflect.DeepEqual(a, b) }

func TestCreateSubIssue(t *testing.T) {
	e := newEnv(t)
	p := e.project(t, "YLK", "App")
	m := e.milestone(t, p, milestone.CreateInput{Name: "Bank APIs"})
	e.milestone(t, p, milestone.CreateInput{Name: "Other"})
	e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "Parent", MilestoneID: &m.ID})

	child := e.write(t, "create_issue", map[string]any{"parent": "YLK-1", "title": "Child", "state": "Done"})
	if child.Project != "YLK" || child.Parent != "YLK-1" || child.Milestone != "Bank APIs" || child.State != "Done" {
		t.Errorf("child = %+v", child)
	}
	other := e.write(t, "create_issue", map[string]any{"parent": "YLK-1", "title": "Child 2", "milestone": "other"})
	if other.Milestone != "Other" || other.Parent != "YLK-1" {
		t.Errorf("child with milestone = %+v", other)
	}
}

func TestCreateRejectsNone(t *testing.T) {
	e := newEnv(t)
	e.project(t, "YLK", "App")
	for _, field := range []string{"type", "milestone", "assignee", "parent", "dueDate"} {
		want := `Leave ` + field + ` out instead of "none" when creating an issue; clear it afterwards with update_issue.`
		if got := e.fail(t, "create_issue", map[string]any{"project": "YLK", "title": "x", field: "None"}); got != want {
			t.Errorf("%s: got %q, want %q", field, got, want)
		}
	}
	if got := e.search(t, nil); got.TotalCount != 0 {
		t.Errorf("issues created: %v", got.ids())
	}
}

func TestUpdateIssue(t *testing.T) {
	e := newEnv(t)
	p := e.project(t, "YLK", "App")
	m := e.milestone(t, p, milestone.CreateInput{Name: "Bank APIs"})
	due := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "One", AssigneeID: &e.users.Owner.ID, MilestoneID: &m.ID, DueDate: &due})
	e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "Two"})

	got := e.write(t, "update_issue", map[string]any{
		"issue": "ylk-1", "title": "One renamed", "state": "in progress", "priority": "low",
		"assignee": "none", "milestone": "none", "dueDate": "none", "parent": "YLK-2",
	})
	if got.Title != "One renamed" || got.State != "In Progress" || got.Priority != "low" || got.Assignee != "" ||
		got.Milestone != "" || got.DueDate != "" || got.Parent != "YLK-2" {
		t.Errorf("updated = %+v", got)
	}
	got = e.write(t, "update_issue", map[string]any{"issue": "YLK-1", "assignee": e.users.Admin.Email, "milestone": "Bank APIs", "dueDate": "2026-12-24", "parent": "none", "description": ""})
	if got.Assignee != e.users.Admin.Name || got.Milestone != "Bank APIs" || got.DueDate != "2026-12-24" || got.Parent != "" || got.Title != "One renamed" {
		t.Errorf("updated again = %+v", got)
	}
	if got := e.fail(t, "update_issue", map[string]any{"issue": "YLK-1", "state": "Doing"}); got != `State "Doing" not found in project YLK. States: Backlog, Todo, In Progress, Done, Canceled.` {
		t.Errorf("bad state = %q", got)
	}
}

func TestUpdateType(t *testing.T) {
	e := newEnv(t)
	p := e.project(t, "YLK", "App")
	e.label(t, "Perf", nil)
	e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "One"})

	got := e.write(t, "update_issue", map[string]any{"issue": "YLK-1", "type": "Bug"})
	if got.Type != "Bug" || len(got.Labels) != 0 {
		t.Errorf("type Bug = %+v", got)
	}
	got = e.write(t, "update_issue", map[string]any{"issue": "YLK-1", "labels": []string{"Perf"}})
	if got.Type != "Bug" || !slices.Equal(got.Labels, []string{"Perf"}) {
		t.Errorf("labels keep type = %+v", got)
	}
	got = e.write(t, "update_issue", map[string]any{"issue": "YLK-1", "type": "none"})
	if got.Type != "" || !slices.Equal(got.Labels, []string{"Perf"}) {
		t.Errorf("type none = %+v", got)
	}
	got = e.write(t, "update_issue", map[string]any{"issue": "YLK-1", "labels": []string{}, "type": "Feature"})
	if got.Type != "Feature" || len(got.Labels) != 0 {
		t.Errorf("labels [] + type = %+v", got)
	}
	got = e.write(t, "update_issue", map[string]any{"issue": "YLK-1", "addLabels": []string{"Perf"}, "type": "Task"})
	if got.Type != "Task" || !slices.Equal(got.Labels, []string{"Perf"}) {
		t.Errorf("add + type = %+v", got)
	}
	got = e.write(t, "update_issue", map[string]any{"issue": "YLK-1", "removeLabels": []string{"perf"}})
	if got.Type != "Task" || len(got.Labels) != 0 {
		t.Errorf("remove = %+v", got)
	}
}

func TestUpdateLabelsConflict(t *testing.T) {
	e := newEnv(t)
	p := e.project(t, "YLK", "App")
	e.label(t, "Perf", nil)
	e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "One"})
	for _, extra := range []string{"addLabels", "removeLabels"} {
		args := map[string]any{"issue": "YLK-1", "labels": []string{"Perf"}, extra: []string{"Perf"}}
		if got := e.fail(t, "update_issue", args); got != `Use either labels or addLabels/removeLabels.` {
			t.Errorf("%s: %q", extra, got)
		}
	}
}

func TestLabelNamedLikeType(t *testing.T) {
	e := newEnv(t)
	p := e.project(t, "YLK", "App")
	e.label(t, "Bug", nil)
	e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "One"})
	got := e.write(t, "update_issue", map[string]any{"issue": "YLK-1", "addLabels": []string{"Bug"}})
	if got.Type != "Task" || !slices.Equal(got.Labels, []string{"Bug"}) {
		t.Errorf("free Bug = %+v", got)
	}
	got = e.write(t, "update_issue", map[string]any{"issue": "YLK-1", "type": "Bug"})
	if got.Type != "Bug" || !slices.Equal(got.Labels, []string{"Bug"}) {
		t.Errorf("type Bug + free Bug = %+v", got)
	}
}

func TestAmbiguousAssigneeChangesNothing(t *testing.T) {
	e := newEnv(t)
	p := e.project(t, "YLK", "App")
	e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "Original"})
	e.user(t, "Olga", "olga1@example.com", account.RoleMember)
	e.user(t, "Olga", "olga2@example.com", account.RoleMember)
	got := e.fail(t, "update_issue", map[string]any{"issue": "YLK-1", "title": "Changed", "assignee": "Olga"})
	if want := `"Olga" matches several people: olga1@example.com, olga2@example.com. Use the email.`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if i := e.getIssue(t, "YLK-1"); i.Title != "Original" || i.Assignee != nil {
		t.Errorf("issue changed: %+v", i)
	}
	if s := e.write(t, "update_issue", map[string]any{"issue": "YLK-1", "assignee": "olga2@example.com"}); s.Assignee != "Olga" {
		t.Errorf("positive control = %+v", s)
	}
}

func TestWriteArchivedProject(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.project(t, "YLK", "App")
	e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "One"})
	if _, err := e.deps.Projects.SetArchived(ctx, e.users.Owner, p.ID, true); err != nil {
		t.Fatal(err)
	}
	const want = `Invalid project: project is archived.`
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"create_issue", map[string]any{"project": "YLK", "title": "Two"}},
		{"update_issue", map[string]any{"issue": "YLK-1", "title": "Renamed"}},
		{"add_comment", map[string]any{"issue": "YLK-1", "body": "Hi"}},
	} {
		if got := e.fail(t, c.tool, c.args); got != want {
			t.Errorf("%s: %q", c.tool, got)
		}
	}
	if i := e.getIssue(t, "YLK-1"); i.Title != "One" || i.CommentCount != 0 {
		t.Errorf("archived issue changed: %+v", i)
	}
}

func TestAddComment(t *testing.T) {
	e := newEnv(t)
	p := e.project(t, "YLK", "App")
	i := e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "One"})
	e.comment(t, i, "first")
	var out struct{ ID, Issue, URL, CreatedAt string }
	decode(t, e.ok(t, "add_comment", map[string]any{"issue": "ylk-1", "body": "Done in abc123"}), &out)
	if out.ID == "" || out.Issue != "YLK-1" || out.URL != "http://yalkos.test/issue/YLK-1" || len(out.CreatedAt) != len("2026-11-01 11:05") {
		t.Errorf("out = %+v", out)
	}
	got := e.getIssue(t, "YLK-1")
	if got.CommentCount != 2 || got.Comments[1].Body != "Done in abc123" || got.Comments[1].ID != out.ID {
		t.Errorf("comments = %+v", got.Comments)
	}
	if got := e.fail(t, "add_comment", map[string]any{"issue": "YLK-1", "body": "  "}); got != `Invalid body: write a comment.` {
		t.Errorf("empty body = %q", got)
	}
}

func TestChangesBelongToKeyOwner(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.project(t, "YLK", "App")
	member := e.client(t, e.key(t, e.users.Member))
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"create_issue", map[string]any{"project": "YLK", "title": "By agent"}},
		{"update_issue", map[string]any{"issue": "YLK-1", "title": "By agent, renamed"}},
		{"add_comment", map[string]any{"issue": "YLK-1", "body": "From the agent"}},
	} {
		if text, isErr := e.call(t, member, c.tool, c.args); isErr {
			t.Fatalf("%s: %s", c.tool, text)
		}
	}
	i, err := e.deps.Issues.Get(ctx, "YLK-1")
	if err != nil {
		t.Fatal(err)
	}
	if i.CreatorID != e.users.Member.ID {
		t.Errorf("creator = %s", i.CreatorID)
	}
	entries, err := audit.New(e.pool).List(ctx, i.ID, page.Args{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries.Items) < 2 {
		t.Fatalf("history = %+v", entries.Items)
	}
	for _, en := range entries.Items {
		if en.ActorID == nil || *en.ActorID != e.users.Member.ID {
			t.Errorf("entry %s by %v, want Member", en.Action, en.ActorID)
		}
	}
	if got := e.getIssue(t, "YLK-1"); len(got.Comments) != 1 || got.Comments[0].Author != e.users.Member.Name {
		t.Errorf("comments = %+v", got.Comments)
	}
}

func TestWriteToolAnnotations(t *testing.T) {
	e := newEnv(t)
	want := map[string]bool{"create_issue": false, "add_comment": false, "update_issue": true}
	seen := 0
	for _, tool := range listTools(t, e).Tools {
		idem, ok := want[tool.Name]
		if !ok {
			continue
		}
		seen++
		a := tool.Annotations
		if a == nil || a.ReadOnlyHint || a.DestructiveHint == nil || *a.DestructiveHint || a.IdempotentHint != idem ||
			a.OpenWorldHint == nil || *a.OpenWorldHint || tool.OutputSchema != nil {
			t.Errorf("%s annotations = %+v", tool.Name, a)
		}
	}
	if seen != len(want) {
		t.Errorf("found %d of %d write tools", seen, len(want))
	}
}
