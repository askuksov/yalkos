package issue_test

import (
	"context"
	"maps"
	"testing"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
)

func TestCountByProjects(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	web, empty := e.project(t, "WEB"), e.project(t, "EMPTY")
	e.create(t, "a")
	e.create(t, "b")
	gone := e.create(t, "c")
	if err := e.issues.Delete(ctx, e.users.Member, gone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ProjectID: &web.ID, Title: "w"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.projects.SetArchived(ctx, e.users.Admin, web.ID, true); err != nil {
		t.Fatal(err)
	}
	got, err := e.issues.CountByProjects(ctx, []uuid.UUID{e.ylk.ID, web.ID, empty.ID})
	if err != nil {
		t.Fatal(err)
	}
	want := map[uuid.UUID]int{e.ylk.ID: 2, web.ID: 1}
	if !maps.Equal(got, want) {
		t.Fatalf("counts = %v, want %v", got, want)
	}
}

func TestCountByCreators(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, title := range []string{"o1", "o2", "o3"} {
		if _, err := e.issues.Create(ctx, e.users.Owner, issue.CreateInput{ProjectID: &e.ylk.ID, Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	m := e.create(t, "m1")
	e.create(t, "m2")
	if err := e.issues.Delete(ctx, e.users.Member, m.ID); err != nil {
		t.Fatal(err)
	}
	got, err := e.issues.CountByCreators(ctx, []uuid.UUID{e.users.Owner.ID, e.users.Member.ID, e.users.Admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	want := map[uuid.UUID]int{e.users.Owner.ID: 3, e.users.Member.ID: 1}
	if !maps.Equal(got, want) {
		t.Fatalf("counts = %v, want %v", got, want)
	}
}
