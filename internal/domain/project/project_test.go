package project_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/testfx"
)

func ptr[T any](v T) *T { return &v }

type env struct {
	pool  *pgxpool.Pool
	users testfx.Users
	clock func() time.Time
	svc   *project.Service
	audit *audit.Service
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool, users, clock := testfx.New(t)
	return env{pool: pool, users: users, clock: clock, svc: project.New(pool, clock), audit: audit.New(pool)}
}

func (e env) create(t *testing.T, key string) project.Project {
	t.Helper()
	p, err := e.svc.Create(context.Background(), e.users.Admin, project.CreateInput{Key: key, Name: "Project " + key})
	if err != nil {
		t.Fatalf("Create %s: %v", key, err)
	}
	return p
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

// asInput asserts an InputError and returns it for further checks.
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

func changeFields(e audit.Entry) string {
	var f []string
	for _, c := range e.Changes {
		f = append(f, c.Field)
	}
	return strings.Join(f, ",")
}

func TestCreateProject(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, err := e.svc.Create(ctx, e.users.Admin, project.CreateInput{Key: " ylk ", Name: "Yalkos", Color: ptr("#AABBCC")})
	if err != nil {
		t.Fatal(err)
	}
	if p.Key != "YLK" || p.Name != "Yalkos" || *p.Color != "#aabbcc" || p.Icon != nil || p.ArchivedAt != nil {
		t.Fatalf("project = %+v", p)
	}
	rows, err := e.pool.Query(ctx, `SELECT name, type, color, position, is_default FROM states WHERE project_id = $1 ORDER BY position`, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var name, typ, color string
		var pos int
		var def bool
		if err := rows.Scan(&name, &typ, &color, &pos, &def); err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%d %s %s %s %v", pos, name, typ, color, def))
	}
	want := []string{
		"0 Backlog backlog #bec2c8 false",
		"1 Todo unstarted #e2e2e2 true",
		"2 In Progress started #f2c94c false",
		"3 Done completed #5e6ad2 false",
		"4 Canceled canceled #95a2b3 false",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("states:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	members, err := e.svc.MemberIDs(ctx, p.ID)
	if err != nil || len(members) != 1 || members[0] != e.users.Admin.ID {
		t.Fatalf("members = %v, %v", members, err)
	}
	h := e.history(t, p.ID)
	if len(h) != 1 || h[0].Action != "create" || *h[0].ActorID != e.users.Admin.ID {
		t.Fatalf("history = %+v", h)
	}
	for _, c := range h[0].Changes {
		if string(c.From) != "null" {
			t.Errorf("create change %s from %s, want null", c.Field, c.From)
		}
		if c.Field == "key" && string(c.To) != `"YLK"` {
			t.Errorf("key to %s", c.To)
		}
	}
}

func TestCreateProjectForbidden(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.Create(context.Background(), e.users.Member, project.CreateInput{Key: "YLK", Name: "Y"})
	if !errors.Is(err, kit.ErrForbidden) {
		t.Fatalf("err = %v, want forbidden", err)
	}
}

func TestProjectValidation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	cases := []struct {
		in     project.CreateInput
		field  string
		reason kit.Reason
	}{
		{project.CreateInput{Key: "1AB", Name: "x"}, "key", kit.ReasonInvalidFormat},
		{project.CreateInput{Key: "ABCDEFGH", Name: "x"}, "key", kit.ReasonTooLong},
		{project.CreateInput{Key: "A-B", Name: "x"}, "key", kit.ReasonInvalidFormat},
		{project.CreateInput{Key: " ", Name: "x"}, "key", kit.ReasonRequired},
		{project.CreateInput{Key: "AB", Name: ""}, "name", kit.ReasonRequired},
		{project.CreateInput{Key: "AB", Name: strings.Repeat("я", 101)}, "name", kit.ReasonTooLong},
		{project.CreateInput{Key: "AB", Name: "x", Description: strings.Repeat("d", 100001)}, "description", kit.ReasonTooLong},
		{project.CreateInput{Key: "AB", Name: "x", Color: ptr("red")}, "color", kit.ReasonInvalidFormat},
		{project.CreateInput{Key: "AB", Name: "x", Icon: ptr(strings.Repeat("i", 33))}, "icon", kit.ReasonTooLong},
		{project.CreateInput{Key: "AB", Name: "x", Icon: ptr("")}, "icon", kit.ReasonRequired},
	}
	for _, c := range cases {
		_, err := e.svc.Create(ctx, e.users.Admin, c.in)
		wantInput(t, err, c.field, c.reason)
	}
	_, err := e.svc.Create(ctx, e.users.Admin, project.CreateInput{Key: "AB", Name: strings.Repeat("я", 101)})
	if ie := asInput(t, err, "name", kit.ReasonTooLong); ie.Params["max"] != 100 {
		t.Errorf("params = %v", ie.Params)
	}
}

func TestDuplicateKey(t *testing.T) {
	e := newEnv(t)
	e.create(t, "YLK")
	_, err := e.svc.Create(context.Background(), e.users.Admin, project.CreateInput{Key: "ylk", Name: "Other"})
	if ie := asInput(t, err, "key", kit.ReasonTaken); ie.Msg != "key is already used" {
		t.Fatalf("msg = %q", ie.Msg)
	}
}

func TestFormerKeyResolvesAndIsNotReusable(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.create(t, "YLK")
	up, err := e.svc.Update(ctx, e.users.Admin, p.ID, project.UpdateInput{Key: ptr("yk")})
	if err != nil || up.Key != "YK" {
		t.Fatalf("Update key: %+v, %v", up, err)
	}
	for _, ref := range []string{"ylk", "YLK", "yk", p.ID.String()} {
		got, err := e.svc.Get(ctx, ref)
		if err != nil || got.ID != p.ID || got.Key != "YK" {
			t.Fatalf("Get(%q) = %+v, %v", ref, got, err)
		}
	}
	_, err = e.svc.Create(ctx, e.users.Admin, project.CreateInput{Key: "YLK", Name: "Thief"})
	wantInput(t, err, "key", kit.ReasonTaken)
	other := e.create(t, "WEB")
	_, err = e.svc.Update(ctx, e.users.Admin, other.ID, project.UpdateInput{Key: ptr("YLK")})
	wantInput(t, err, "key", kit.ReasonTaken)

	back, err := e.svc.Update(ctx, e.users.Admin, p.ID, project.UpdateInput{Key: ptr("YLK")})
	if err != nil || back.Key != "YLK" {
		t.Fatalf("back to own former key: %+v, %v", back, err)
	}
	for _, ref := range []string{"nope", "ZZZ", uuid.NewString()} {
		_, err := e.svc.Get(ctx, ref)
		wantNotFound(t, err)
	}
}

func TestArchivedProjectReadOnly(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.create(t, "YLK")
	e.create(t, "WEB")
	a, err := e.svc.SetArchived(ctx, e.users.Admin, p.ID, true)
	if err != nil || a.ArchivedAt == nil {
		t.Fatalf("archive: %+v, %v", a, err)
	}
	_, err = e.svc.Update(ctx, e.users.Admin, p.ID, project.UpdateInput{Name: ptr("New")})
	if ie := asInput(t, err, "projectId", kit.ReasonArchived); ie.Msg != "project is archived" {
		t.Fatalf("msg = %q", ie.Msg)
	}
	err = e.svc.AddMember(ctx, e.users.Admin, p.ID, e.users.Member.ID)
	wantInput(t, err, "projectId", kit.ReasonArchived)

	list, err := e.svc.List(ctx, false, page.Args{})
	if err != nil || len(list.Items) != 1 || list.Items[0].Key != "WEB" {
		t.Fatalf("List = %+v, %v", list.Items, err)
	}
	list, err = e.svc.List(ctx, true, page.Args{})
	if err != nil || len(list.Items) != 2 {
		t.Fatalf("List(includeArchived) = %+v, %v", list.Items, err)
	}
	if _, err := e.svc.Get(ctx, "YLK"); err != nil {
		t.Fatalf("Get archived: %v", err)
	}
	u, err := e.svc.SetArchived(ctx, e.users.Admin, p.ID, false)
	if err != nil || u.ArchivedAt != nil {
		t.Fatalf("unarchive: %+v, %v", u, err)
	}
	if _, err := e.svc.Update(ctx, e.users.Admin, p.ID, project.UpdateInput{Name: ptr("New")}); err != nil {
		t.Fatalf("update after unarchive: %v", err)
	}
	if _, err := e.svc.SetArchived(ctx, e.users.Member, p.ID, true); !errors.Is(err, kit.ErrForbidden) {
		t.Fatalf("member archives: %v", err)
	}
}

func TestDeleteProject(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.create(t, "YLK")
	if err := e.svc.Delete(ctx, e.users.Admin, p.ID, "YLK"); !errors.Is(err, kit.ErrForbidden) {
		t.Fatalf("admin delete: %v", err)
	}
	err := e.svc.Delete(ctx, e.users.Owner, p.ID, "WEB")
	wantInput(t, err, "confirmKey", kit.ReasonMismatch)
	if err := e.svc.Delete(ctx, e.users.Owner, p.ID, "YLK"); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.Get(ctx, p.ID.String())
	wantNotFound(t, err)
	_, err = e.svc.Get(ctx, "YLK")
	wantNotFound(t, err)
	h := e.history(t, p.ID)
	if len(h) != 1 || h[0].Action != "delete" || changeFields(h[0]) != "key,name" {
		t.Fatalf("history after delete = %+v", h)
	}
	var projectID *uuid.UUID
	if err := e.pool.QueryRow(ctx, `SELECT project_id FROM audit_events WHERE entity_id = $1`, p.ID).Scan(&projectID); err != nil || projectID != nil {
		t.Fatalf("delete event project_id = %v, %v", projectID, err)
	}
	// The key is free again once the project is gone.
	e.create(t, "YLK")
}

func TestListProjectsPaged(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, n := range []string{"b", "A", "c"} {
		if _, err := e.svc.Create(ctx, e.users.Admin, project.CreateInput{Key: strings.ToUpper(n) + "K", Name: n}); err != nil {
			t.Fatal(err)
		}
	}
	names := func(r page.Result[project.Project]) string {
		var s []string
		for _, p := range r.Items {
			s = append(s, p.Name)
		}
		return strings.Join(s, ",")
	}
	all, err := e.svc.List(ctx, false, page.Args{})
	if err != nil || names(all) != "A,b,c" {
		t.Fatalf("List = %s, %v", names(all), err)
	}
	p1, err := e.svc.List(ctx, false, page.Args{First: ptr(2)})
	if err != nil || names(p1) != "A,b" || !p1.Info.HasNext {
		t.Fatalf("first 2 = %s %+v %v", names(p1), p1.Info, err)
	}
	p2, err := e.svc.List(ctx, false, page.Args{First: ptr(2), After: p1.Info.End})
	if err != nil || names(p2) != "c" || p2.Info.HasNext || !p2.Info.HasPrev {
		t.Fatalf("after = %s %+v %v", names(p2), p2.Info, err)
	}
	back, err := e.svc.List(ctx, false, page.Args{Last: ptr(2), Before: p2.Info.Start})
	if err != nil || names(back) != "A,b" {
		t.Fatalf("before = %s %v", names(back), err)
	}
}

func TestProjectAudit(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.create(t, "YLK")
	if _, err := e.svc.Update(ctx, e.users.Admin, p.ID, project.UpdateInput{Name: ptr("Renamed"), Description: ptr("")}); err != nil {
		t.Fatal(err)
	}
	h := e.history(t, p.ID)
	if len(h) != 2 || h[1].Action != "update" || changeFields(h[1]) != "name" {
		t.Fatalf("history = %+v", h)
	}
	if string(h[1].Changes[0].From) != `"Project YLK"` || string(h[1].Changes[0].To) != `"Renamed"` {
		t.Fatalf("change = %s → %s", h[1].Changes[0].From, h[1].Changes[0].To)
	}
	// A no-op update records nothing.
	if _, err := e.svc.Update(ctx, e.users.Admin, p.ID, project.UpdateInput{Name: ptr("Renamed")}); err != nil {
		t.Fatal(err)
	}
	if n := len(e.history(t, p.ID)); n != 2 {
		t.Fatalf("no-op update recorded: %d events", n)
	}
}

func TestUpdateProjectClearsOptional(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, err := e.svc.Create(ctx, e.users.Admin, project.CreateInput{Key: "YLK", Name: "Y", Icon: ptr("rocket"), Color: ptr("#000000")})
	if err != nil {
		t.Fatal(err)
	}
	up, err := e.svc.Update(ctx, e.users.Admin, p.ID, project.UpdateInput{Color: kit.Opt[string]{Set: true}})
	if err != nil || up.Color != nil || up.Icon == nil || *up.Icon != "rocket" {
		t.Fatalf("clear color: %+v, %v", up, err)
	}
	_, err = e.svc.Update(ctx, e.users.Member, p.ID, project.UpdateInput{Name: ptr("x")})
	if !errors.Is(err, kit.ErrForbidden) {
		t.Fatalf("member update: %v", err)
	}
	_, err = e.svc.Update(ctx, e.users.Admin, uuid.New(), project.UpdateInput{Name: ptr("x")})
	wantNotFound(t, err)
}
