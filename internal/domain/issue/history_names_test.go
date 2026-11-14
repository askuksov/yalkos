package issue_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/project"
)

// nameOf returns the stored name of id in entry e, or "" when none.
func nameOf(e audit.Entry, id uuid.UUID) string {
	for _, n := range e.Names {
		if n.ID == id {
			return n.Name
		}
	}
	return ""
}

func TestHistoryKeepsDeletedStateName(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	doomed, err := e.projects.CreateState(ctx, e.users.Admin, e.ylk.ID, project.StateInput{Name: "Doomed", Type: project.Started, Color: "#ff0000"})
	if err != nil {
		t.Fatal(err)
	}
	todo := e.state(t, e.ylk.ID, "Todo")
	i := e.create(t, "x")
	if _, err := e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{StateID: &doomed.ID}); err != nil {
		t.Fatal(err)
	}
	if err := e.projects.DeleteState(ctx, e.users.Admin, doomed.ID, &todo.ID); err != nil {
		t.Fatal(err)
	}
	h := e.history(t, i.ID)
	// create, move into Doomed, then the delete moves the issue out of it.
	if len(h) != 3 {
		t.Fatalf("history = %+v", h)
	}
	if got := nameOf(h[0], todo.ID); got != "Todo" {
		t.Errorf("create entry stores state %q, want Todo (names %+v)", got, h[0].Names)
	}
	if got := nameOf(h[1], doomed.ID); got != "Doomed" {
		t.Errorf("state change stores %q, want Doomed (names %+v)", got, h[1].Names)
	}
	if got := nameOf(h[2], doomed.ID); got != "Doomed" {
		t.Errorf("state delete entry stores %q, want Doomed (names %+v)", got, h[2].Names)
	}
}

func TestHistoryKeepsDeletedProjectKey(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := e.project(t, "AAA")
	b := e.project(t, "BBB")
	i := e.createIn(t, a.ID, "x")
	if _, err := e.issues.Move(ctx, e.users.Member, i.ID, b.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := e.projects.Delete(ctx, e.users.Owner, a.ID, "AAA"); err != nil {
		t.Fatal(err)
	}
	h := e.history(t, i.ID)
	if len(h) != 2 {
		t.Fatalf("history = %+v", h)
	}
	// A project is stored by its key, a state by its name.
	if got := nameOf(h[0], a.ID); got != "AAA" {
		t.Errorf("create entry stores project %q, want AAA (names %+v)", got, h[0].Names)
	}
	if got := nameOf(h[0], e.state(t, b.ID, "Todo").ID); got != "" {
		t.Errorf("create entry stores a state of the other project: %q", got)
	}
	if got := nameOf(h[1], a.ID); got != "AAA" {
		t.Errorf("move entry stores the former project %q, want AAA (names %+v)", got, h[1].Names)
	}
	if got := nameOf(h[1], b.ID); got != "BBB" {
		t.Errorf("move entry stores the new project %q, want BBB (names %+v)", got, h[1].Names)
	}
	var stored []string
	for _, n := range h[0].Names {
		stored = append(stored, n.Name)
	}
	slices.Sort(stored)
	if want := "AAA,Task,Todo"; strings.Join(stored, ",") != want {
		t.Errorf("create entry names = %v, want %s", stored, want)
	}
}
