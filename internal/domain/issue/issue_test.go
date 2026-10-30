package issue_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/testfx"
)

func ptr[T any](v T) *T { return &v }

type env struct {
	pool     *pgxpool.Pool
	users    testfx.Users
	clock    func() time.Time
	projects *project.Service
	issues   *issue.Service
	audit    *audit.Service
	ylk      project.Project
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool, users, clock := testfx.New(t)
	projects := project.New(pool, clock)
	e := env{pool: pool, users: users, clock: clock, projects: projects, issues: issue.New(pool, projects, clock), audit: audit.New(pool)}
	e.ylk = e.project(t, "YLK")
	return e
}

func (e env) project(t *testing.T, key string) project.Project {
	t.Helper()
	p, err := e.projects.Create(context.Background(), e.users.Admin, project.CreateInput{Key: key, Name: key})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (e env) state(t *testing.T, projectID uuid.UUID, name string) project.State {
	t.Helper()
	states, err := e.projects.States(context.Background(), projectID)
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

func (e env) create(t *testing.T, title string) issue.Issue {
	t.Helper()
	i, err := e.issues.Create(context.Background(), e.users.Member, issue.CreateInput{ProjectID: &e.ylk.ID, Title: title})
	if err != nil {
		t.Fatalf("Create %q: %v", title, err)
	}
	return i
}

func (e env) history(t *testing.T, id uuid.UUID) []audit.Entry {
	t.Helper()
	r, err := e.audit.List(context.Background(), id, page.Args{First: ptr(250)})
	if err != nil {
		t.Fatal(err)
	}
	return r.Items
}

func wantInput(t *testing.T, err error, field string, reason kit.Reason) {
	t.Helper()
	_ = asInput(t, err, field, reason)
}

func asInput(t *testing.T, err error, field string, reason kit.Reason) *kit.InputError {
	t.Helper()
	var ie *kit.InputError
	if !errors.As(err, &ie) {
		t.Fatalf("err = %v, want InputError field %q reason %q", err, field, reason)
	}
	if ie.Field != field || ie.Reason != reason {
		t.Fatalf("InputError = {%s %s %q}, want {%s %s}", ie.Field, ie.Reason, ie.Msg, field, reason)
	}
	return ie
}

func wantNotFound(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, kit.ErrNotFound) {
		t.Fatalf("err = %v, want not found", err)
	}
}

func TestCreateIssue(t *testing.T) {
	e := newEnv(t)
	todo := e.state(t, e.ylk.ID, "Todo")
	first := e.create(t, "  First  ")
	if first.Number != 1 || first.Identifier != "YLK-1" || first.Title != "First" || first.StateID != todo.ID ||
		first.Priority != 0 || first.SortOrder != 0 || first.CreatorID != e.users.Member.ID || first.ProjectID != e.ylk.ID {
		t.Fatalf("first = %+v", first)
	}
	if first.CompletedAt != nil || first.CanceledAt != nil || first.ParentID != nil || first.AssigneeID != nil {
		t.Fatalf("first optional fields = %+v", first)
	}
	second := e.create(t, "Second")
	if second.Identifier != "YLK-2" || second.SortOrder != -1000 {
		t.Fatalf("second = %+v", second)
	}
	h := e.history(t, first.ID)
	if len(h) != 1 || h[0].Action != "create" || *h[0].ActorID != e.users.Member.ID {
		t.Fatalf("history = %+v", h)
	}
	explicit, err := e.issues.Create(context.Background(), e.users.Member, issue.CreateInput{ProjectID: &e.ylk.ID, Title: "x", SortOrder: ptr(5.5), Priority: 3})
	if err != nil || explicit.SortOrder != 5.5 || explicit.Priority != 3 {
		t.Fatalf("explicit = %+v, %v", explicit, err)
	}
}

func TestCreateValidation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	web := e.project(t, "WEB")
	webTodo := e.state(t, web.ID, "Todo")
	disabled := testfx.DisabledUser(t, e.pool, e.users.Owner)
	missing := uuid.New()
	in := func(mod func(*issue.CreateInput)) issue.CreateInput {
		c := issue.CreateInput{ProjectID: &e.ylk.ID, Title: "ok"}
		mod(&c)
		return c
	}
	cases := []struct {
		in     issue.CreateInput
		field  string
		reason kit.Reason
	}{
		{in(func(c *issue.CreateInput) { c.Title = "  " }), "title", kit.ReasonRequired},
		{in(func(c *issue.CreateInput) { c.Title = strings.Repeat("я", 256) }), "title", kit.ReasonTooLong},
		{in(func(c *issue.CreateInput) { c.Title = "a\nb" }), "title", kit.ReasonInvalidFormat},
		{in(func(c *issue.CreateInput) { c.Title = "a\rb" }), "title", kit.ReasonInvalidFormat},
		{in(func(c *issue.CreateInput) { c.Description = strings.Repeat("d", 100001) }), "description", kit.ReasonTooLong},
		{in(func(c *issue.CreateInput) { c.Priority = 5 }), "priority", kit.ReasonOutOfRange},
		{in(func(c *issue.CreateInput) { c.Priority = -1 }), "priority", kit.ReasonOutOfRange},
		{in(func(c *issue.CreateInput) { c.AssigneeID = &disabled.ID }), "assigneeId", kit.ReasonNotAllowed},
		{in(func(c *issue.CreateInput) { c.AssigneeID = &missing }), "assigneeId", kit.ReasonNotFound},
		{in(func(c *issue.CreateInput) { c.StateID = &webTodo.ID }), "stateId", kit.ReasonWrongProject},
		{in(func(c *issue.CreateInput) { c.StateID = &missing }), "stateId", kit.ReasonNotFound},
		{in(func(c *issue.CreateInput) { c.ProjectID = nil }), "projectId", kit.ReasonRequired},
		{in(func(c *issue.CreateInput) { c.ProjectID = &missing }), "projectId", kit.ReasonNotFound},
	}
	for _, c := range cases {
		_, err := e.issues.Create(ctx, e.users.Member, c.in)
		wantInput(t, err, c.field, c.reason)
	}
	_, err := e.issues.Create(ctx, e.users.Member, in(func(c *issue.CreateInput) { c.Title = strings.Repeat("я", 256) }))
	if ie := asInput(t, err, "title", kit.ReasonTooLong); ie.Params["max"] != 255 {
		t.Fatalf("params = %v", ie.Params)
	}
	_, err = e.issues.Create(ctx, e.users.Member, in(func(c *issue.CreateInput) { c.Priority = 5 }))
	if ie := asInput(t, err, "priority", kit.ReasonOutOfRange); ie.Params["min"] != 0 || ie.Params["max"] != 4 {
		t.Fatalf("params = %v", ie.Params)
	}
	// Nothing was created by the failed attempts.
	if i := e.create(t, "real"); i.Number != 1 {
		t.Fatalf("number after failures = %d", i.Number)
	}
}

func TestCreateInCompletedState(t *testing.T) {
	e := newEnv(t)
	done := e.state(t, e.ylk.ID, "Done")
	before := e.clock()
	i, err := e.issues.Create(context.Background(), e.users.Member, issue.CreateInput{ProjectID: &e.ylk.ID, Title: "x", StateID: &done.ID})
	if err != nil {
		t.Fatal(err)
	}
	want := before.Add(time.Second)
	if i.CompletedAt == nil || !i.CompletedAt.Equal(want) || i.CanceledAt != nil || !i.CreatedAt.Equal(want) {
		t.Fatalf("completedAt = %v createdAt %v, want %v", i.CompletedAt, i.CreatedAt, want)
	}
}

func TestCreateConcurrentNumbers(t *testing.T) {
	e := newEnv(t)
	var wg sync.WaitGroup
	nums := make([]int, 20)
	errs := make([]error, 20)
	for g := range 20 {
		wg.Go(func() {
			i, err := e.issues.Create(context.Background(), e.users.Member, issue.CreateInput{ProjectID: &e.ylk.ID, Title: "c"})
			nums[g], errs[g] = i.Number, err
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	slices.Sort(nums)
	for i, n := range nums {
		if n != i+1 {
			t.Fatalf("numbers = %v", nums)
		}
	}
}

func TestGetByIdentifier(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	i := e.create(t, "x")
	for _, ref := range []string{"ylk-1", "YLK-1", i.ID.String()} {
		got, err := e.issues.Get(ctx, ref)
		if err != nil || got.ID != i.ID || got.Identifier != "YLK-1" {
			t.Fatalf("Get(%q) = %+v, %v", ref, got, err)
		}
	}
	for _, ref := range []string{"YLK-99", "YLK-0", "nope", "YLK-99999999999", "WEB-1", uuid.NewString()} {
		_, err := e.issues.Get(ctx, ref)
		wantNotFound(t, err)
	}
	got, err := e.issues.ByIDs(ctx, []uuid.UUID{uuid.New(), i.ID})
	if err != nil || len(got) != 1 || got[0].ID != i.ID {
		t.Fatalf("ByIDs = %+v, %v", got, err)
	}
}

func TestUpdateFields(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	i := e.create(t, "x")
	due := time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)
	before := e.clock()
	u, err := e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{
		Title: ptr(" New title "), Description: ptr("# md"),
		AssigneeID: kit.Opt[uuid.UUID]{Set: true, Value: &e.users.Admin.ID},
		DueDate:    kit.Opt[time.Time]{Set: true, Value: &due},
		SortOrder:  ptr(42.5),
	})
	if err != nil {
		t.Fatal(err)
	}
	if u.Title != "New title" || u.Description != "# md" || *u.AssigneeID != e.users.Admin.ID ||
		!u.DueDate.Equal(due) || u.SortOrder != 42.5 || !u.UpdatedAt.Equal(before.Add(time.Second)) {
		t.Fatalf("updated = %+v", u)
	}
	c, err := e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{
		AssigneeID: kit.Opt[uuid.UUID]{Set: true}, DueDate: kit.Opt[time.Time]{Set: true},
	})
	if err != nil || c.AssigneeID != nil || c.DueDate != nil || c.Title != "New title" {
		t.Fatalf("cleared = %+v, %v", c, err)
	}
	_, err = e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{Title: ptr("")})
	wantInput(t, err, "title", kit.ReasonRequired)
	_, err = e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{Priority: ptr(9)})
	wantInput(t, err, "priority", kit.ReasonOutOfRange)
	_, err = e.issues.Update(ctx, e.users.Member, uuid.New(), issue.Patch{Title: ptr("x")})
	wantNotFound(t, err)
}

func TestUpdateStateTimestamps(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	i := e.create(t, "x")
	done, canceled, todo := e.state(t, e.ylk.ID, "Done"), e.state(t, e.ylk.ID, "Canceled"), e.state(t, e.ylk.ID, "Todo")
	d, err := e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{StateID: &done.ID})
	if err != nil || d.CompletedAt == nil || d.CanceledAt != nil || d.StateID != done.ID {
		t.Fatalf("→Done: %+v, %v", d, err)
	}
	c, err := e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{StateID: &canceled.ID})
	if err != nil || c.CompletedAt != nil || c.CanceledAt == nil {
		t.Fatalf("→Canceled: %+v, %v", c, err)
	}
	b, err := e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{StateID: &todo.ID})
	if err != nil || b.CompletedAt != nil || b.CanceledAt != nil {
		t.Fatalf("→Todo: %+v, %v", b, err)
	}
	web := e.project(t, "WEB")
	webTodo := e.state(t, web.ID, "Todo")
	_, err = e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{StateID: &webTodo.ID})
	wantInput(t, err, "stateId", kit.ReasonWrongProject)
}

func TestUpdateAudit(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	i := e.create(t, "x")
	if _, err := e.issues.Update(ctx, e.users.Admin, i.ID, issue.Patch{Priority: ptr(2)}); err != nil {
		t.Fatal(err)
	}
	h := e.history(t, i.ID)
	if len(h) != 2 || h[1].Action != "update" || *h[1].ActorID != e.users.Admin.ID || len(h[1].Changes) != 1 {
		t.Fatalf("history = %+v", h)
	}
	if c := h[1].Changes[0]; c.Field != "priority" || string(c.From) != "0" || string(c.To) != "2" {
		t.Fatalf("change = %+v", c)
	}
	same, err := e.issues.Update(ctx, e.users.Admin, i.ID, issue.Patch{Priority: ptr(2), Title: ptr("x")})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(e.history(t, i.ID)); n != 2 {
		t.Fatalf("no-op patch recorded: %d events", n)
	}
	got, err := e.issues.Get(ctx, i.ID.String())
	if err != nil || !got.UpdatedAt.Equal(same.UpdatedAt) {
		t.Fatalf("updatedAt moved on no-op: %v vs %v", got.UpdatedAt, same.UpdatedAt)
	}
}

func TestArchivedProjectIssuesReadOnly(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	i := e.create(t, "x")
	if _, err := e.projects.SetArchived(ctx, e.users.Admin, e.ylk.ID, true); err != nil {
		t.Fatal(err)
	}
	_, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ProjectID: &e.ylk.ID, Title: "y"})
	wantInput(t, err, "projectId", kit.ReasonArchived)
	_, err = e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{Title: ptr("y")})
	wantInput(t, err, "projectId", kit.ReasonArchived)
	err = e.issues.Delete(ctx, e.users.Member, i.ID)
	wantInput(t, err, "projectId", kit.ReasonArchived)
	if _, err := e.issues.Get(ctx, "YLK-1"); err != nil {
		t.Fatalf("Get in archived project: %v", err)
	}
}
