package mcpserver

import (
	"context"
	"slices"
	"testing"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/testfx"
)

func TestResolveExactCaseInsensitive(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.project(t, "YLK", "Yalkos")
	c := e.newCall(e.users.Owner)

	st, err := c.state(ctx, p, " in progress ")
	if err != nil || st.Name != "In Progress" {
		t.Fatalf("state = %+v, %v", st, err)
	}
	_, err = c.state(ctx, p, "Prog")
	if got, want := errText(t, err), `State "Prog" not found in project YLK. States: Backlog, Todo, In Progress, Done, Canceled.`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if _, err := c.project(ctx, "ylk"); err != nil {
		t.Errorf("project ylk: %v", err)
	}
	e.project(t, "WEB", "Web")
	_, err = c.project(ctx, "NOPE")
	if got, want := errText(t, err), `Project NOPE not found. Projects: WEB, YLK.`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolvePerson(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.newCall(e.users.Member)

	if id, err := c.person(ctx, "me"); err != nil || id == nil || *id != e.users.Member.ID {
		t.Errorf("me = %v, %v", id, err)
	}
	if id, err := c.person(ctx, "None"); err != nil || id != nil {
		t.Errorf("none = %v, %v", id, err)
	}
	if id, err := c.person(ctx, "OWNER@example.com"); err != nil || id == nil || *id != e.users.Owner.ID {
		t.Errorf("owner email = %v, %v", id, err)
	}
	if id, err := c.person(ctx, " owner "); err != nil || id == nil || *id != e.users.Owner.ID {
		t.Errorf("owner name = %v, %v", id, err)
	}

	e.user(t, "Olga", "olga1@example.com", account.RoleMember)
	e.user(t, "Olga", "olga2@example.com", account.RoleMember)
	c = e.newCall(e.users.Member)
	_, err := c.person(ctx, "olga")
	if got, want := errText(t, err), `"olga" matches several people: olga1@example.com, olga2@example.com. Use the email.`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if _, err := c.person(ctx, "olga2@example.com"); err != nil {
		t.Errorf("olga2 by email: %v", err)
	}

	d := testfx.DisabledUser(t, e.pool, e.users.Owner)
	c = e.newCall(e.users.Member)
	_, err = c.person(ctx, d.Email)
	if got, want := errText(t, err), `No active person matches "`+d.Email+`". Use list_users.`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveLabels(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	free := e.label(t, "UI", nil)
	area := e.group(t, "Area", false)
	grouped := e.label(t, "UI", &area.ID)
	c := e.newCall(e.users.Owner)

	// A free label is named by its name alone; grouped ones also have Group/Name.
	ids, err := c.labels(ctx, []string{"ui"})
	if err != nil || len(ids) != 1 || ids[0] != free.ID {
		t.Errorf("ui = %v, %v (want the free label %s)", ids, err, free.ID)
	}
	platform := e.group(t, "Platform", false)
	e.label(t, "API", &platform.ID)
	e.label(t, "API", &area.ID)
	c = e.newCall(e.users.Owner)
	_, err = c.labels(ctx, []string{"api"})
	if got, want := errText(t, err), `Label "api" is ambiguous: Platform/API, Area/API.`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	ids, err = c.labels(ctx, []string{"area/ui"})
	if err != nil || len(ids) != 1 || ids[0] != grouped.ID {
		t.Errorf("area/ui = %v, %v (want %s)", ids, err, grouped.ID)
	}
	other := e.label(t, "Perf", nil)
	c = e.newCall(e.users.Owner)
	ids, err = c.labels(ctx, []string{"perf"})
	if err != nil || len(ids) != 1 || ids[0] != other.ID {
		t.Errorf("perf = %v, %v", ids, err)
	}
	_, err = c.labels(ctx, []string{"Task"})
	if got, want := errText(t, err), `"Task" is a type; set it with type.`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	_, err = c.labels(ctx, []string{"nope"})
	if got, want := errText(t, err), `Label "nope" not found. Use list_labels.`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTypeNotFound(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.newCall(e.users.Owner)
	id, err := c.typeLabel(ctx, "bug")
	if err != nil || id == nil {
		t.Fatalf("bug = %v, %v", id, err)
	}
	if id, err := c.typeLabel(ctx, "none"); err != nil || id != nil {
		t.Errorf("none = %v, %v", id, err)
	}
	_, err = c.typeLabel(ctx, "Bugg")
	if got, want := errText(t, err), `Type "Bugg" not found. Types: Bug, Feature, Task, Improvement.`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPriorityAndDate(t *testing.T) {
	if n, err := parsePriority("High"); err != nil || n != 2 {
		t.Errorf("High = %d, %v", n, err)
	}
	for s, want := range map[string]int{"none": 0, "urgent": 1, "high": 2, "medium": 3, "low": 4} {
		if n, err := parsePriority(s); err != nil || n != want || priorityName(want) != s {
			t.Errorf("%s = %d, %v; name %q", s, n, err, priorityName(want))
		}
	}
	_, err := parsePriority("asap")
	if got, want := errText(t, err), `Unknown priority "asap". Use one of: urgent, high, medium, low, none.`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if d, err := parseDate("dueDate", "2026-12-01"); err != nil || day(d) != "2026-12-01" {
		t.Errorf("date = %v, %v", d, err)
	}
	_, err = parseDate("dueDate", "2026-13-01")
	if got, want := errText(t, err), `dueDate must be a date YYYY-MM-DD.`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if !isNone(" NONE ") || isNone("nonee") {
		t.Error("isNone")
	}
}

func TestResolveIssueStatesMilestone(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.project(t, "YLK", "App")
	w := e.project(t, "WEB", "Web")
	i := e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "one"})
	c := e.newCall(e.users.Owner)

	if got, err := c.issue(ctx, " ylk-1 "); err != nil || got.ID != i.ID {
		t.Errorf("issue = %+v, %v", got, err)
	}
	_, err := c.issue(ctx, "YLK-999")
	if got, want := errText(t, err), `Issue YLK-999 not found.`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	ids, err := c.statesNamed(ctx, []string{"done"})
	if err != nil || len(ids) != 2 || !slices.Contains(ids, e.stateID(t, p, "Done")) || !slices.Contains(ids, e.stateID(t, w, "Done")) {
		t.Errorf("statesNamed = %v, %v", ids, err)
	}
	_, err = c.statesNamed(ctx, []string{"Doing"})
	if got, want := errText(t, err), `No project has a state named "Doing".`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	_, err = c.milestone(ctx, p, "Bank APIs")
	if got, want := errText(t, err), `Milestone "Bank APIs" not found in project YLK. Milestones: none.`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	m := e.milestone(t, p, milestone.CreateInput{Name: "Bank APIs"})
	other := e.milestone(t, w, milestone.CreateInput{Name: "Elsewhere"})
	for _, ref := range []string{"bank apis", m.ID.String()} {
		if got, err := c.milestone(ctx, p, ref); err != nil || got == nil || got.ID != m.ID {
			t.Errorf("milestone %q = %v, %v", ref, got, err)
		}
	}
	if got, err := c.milestone(ctx, p, "None"); err != nil || got != nil {
		t.Errorf("none = %v, %v", got, err)
	}
	_, err = c.milestone(ctx, p, other.ID.String())
	if got, want := errText(t, err), `Milestone "`+other.ID.String()+`" not found in project YLK. Milestones: Bank APIs.`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	bug, err := c.typeLabel(ctx, "Bug")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.typeOf(ctx, i.LabelIDs); err != nil || got == nil || *got == *bug {
		t.Errorf("typeOf default = %v, %v (want Task, not Bug)", got, err)
	}
	if got, err := c.typeOf(ctx, nil); err != nil || got != nil {
		t.Errorf("typeOf none = %v, %v", got, err)
	}
}
