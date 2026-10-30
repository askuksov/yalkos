package issue_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
)

func (e env) child(t *testing.T, parent uuid.UUID, title string) issue.Issue {
	t.Helper()
	i, err := e.issues.Create(context.Background(), e.users.Member, issue.CreateInput{ParentID: &parent, Title: title})
	if err != nil {
		t.Fatalf("Create child %q: %v", title, err)
	}
	return i
}

func setParent(e env, id uuid.UUID, parent *uuid.UUID) error {
	_, err := e.issues.Update(context.Background(), e.users.Member, id, issue.Patch{ParentID: kit.Opt[uuid.UUID]{Set: true, Value: parent}})
	return err
}

func TestSubIssue(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	parent := e.create(t, "parent")
	c := e.child(t, parent.ID, "child")
	if c.ProjectID != e.ylk.ID || *c.ParentID != parent.ID || c.Identifier != "YLK-2" {
		t.Fatalf("child = %+v", c)
	}
	web := e.project(t, "WEB")
	_, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ProjectID: &web.ID, ParentID: &parent.ID, Title: "x"})
	wantInput(t, err, "parentId", kit.ReasonWrongProject)
	missing := uuid.New()
	_, err = e.issues.Create(ctx, e.users.Member, issue.CreateInput{ParentID: &missing, Title: "x"})
	wantInput(t, err, "parentId", kit.ReasonNotFound)
	same, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ProjectID: &e.ylk.ID, ParentID: &parent.ID, Title: "x"})
	if err != nil || *same.ParentID != parent.ID {
		t.Fatalf("same project explicit: %+v, %v", same, err)
	}
}

func TestParentCycle(t *testing.T) {
	e := newEnv(t)
	a := e.create(t, "A")
	b := e.child(t, a.ID, "B")
	c := e.child(t, b.ID, "C")
	err := setParent(e, a.ID, &c.ID)
	if ie := asInput(t, err, "parentId", kit.ReasonCycle); ie.Msg != "would create a cycle" {
		t.Fatalf("msg = %q", ie.Msg)
	}
	err = setParent(e, a.ID, &a.ID)
	wantInput(t, err, "parentId", kit.ReasonCycle)
	// Re-parenting without a cycle and clearing work.
	if err := setParent(e, c.ID, &a.ID); err != nil {
		t.Fatal(err)
	}
	if err := setParent(e, c.ID, nil); err != nil {
		t.Fatal(err)
	}
	got, err := e.issues.Get(context.Background(), c.ID.String())
	if err != nil || got.ParentID != nil {
		t.Fatalf("cleared parent = %+v, %v", got, err)
	}
}

func TestParentCycleConcurrent(t *testing.T) {
	e := newEnv(t)
	for round := range 10 {
		a := e.create(t, "A")
		b := e.create(t, "B")
		var wg sync.WaitGroup
		var errA, errB error
		wg.Go(func() { errA = setParent(e, a.ID, &b.ID) })
		wg.Go(func() { errB = setParent(e, b.ID, &a.ID) })
		wg.Wait()
		ok := 0
		for _, err := range []error{errA, errB} {
			var ie *kit.InputError
			switch {
			case err == nil:
				ok++
			case errors.As(err, &ie) && ie.Reason == kit.ReasonCycle:
			default:
				t.Fatalf("round %d: unexpected error %v", round, err)
			}
		}
		if ok != 1 {
			t.Fatalf("round %d: %d succeeded (a: %v, b: %v)", round, ok, errA, errB)
		}
	}
}

func TestParentOtherProject(t *testing.T) {
	e := newEnv(t)
	web := e.project(t, "WEB")
	other, err := e.issues.Create(context.Background(), e.users.Member, issue.CreateInput{ProjectID: &web.ID, Title: "w"})
	if err != nil {
		t.Fatal(err)
	}
	i := e.create(t, "x")
	err = setParent(e, i.ID, &other.ID)
	wantInput(t, err, "parentId", kit.ReasonWrongProject)
}

func TestDeleteParentDetachesChildren(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	grand := e.create(t, "grand")
	parent := e.child(t, grand.ID, "parent")
	c1 := e.child(t, parent.ID, "c1")
	c2 := e.child(t, parent.ID, "c2")
	if err := e.issues.Delete(ctx, e.users.Member, parent.ID); err != nil {
		t.Fatal(err)
	}
	for _, c := range []issue.Issue{c1, c2} {
		got, err := e.issues.Get(ctx, c.ID.String())
		if err != nil || got.ParentID != nil {
			t.Fatalf("child %s = %+v, %v", c.Identifier, got, err)
		}
		h := e.history(t, c.ID)
		if len(h) != 2 || h[1].Action != "update" || h[1].Changes[0].Field != "parentId" || string(h[1].Changes[0].To) != "null" {
			t.Fatalf("child history = %+v", h)
		}
	}
	var parentID *uuid.UUID
	if err := e.pool.QueryRow(ctx, `SELECT parent_id FROM issues WHERE id = $1`, parent.ID).Scan(&parentID); err != nil || parentID != nil {
		t.Fatalf("deleted issue parent_id = %v, %v", parentID, err)
	}
	h := e.history(t, parent.ID)
	if len(h) != 2 || h[1].Action != "delete" {
		t.Fatalf("parent history = %+v", h)
	}
}

func TestDeletedIssueInvisible(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	i := e.create(t, "x")
	other := e.create(t, "y")
	if err := e.issues.Delete(ctx, e.users.Member, i.ID); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{i.ID.String(), "YLK-1"} {
		_, err := e.issues.Get(ctx, ref)
		wantNotFound(t, err)
	}
	err := setParent(e, other.ID, &i.ID)
	wantInput(t, err, "parentId", kit.ReasonNotFound)
	_, err = e.issues.Create(ctx, e.users.Member, issue.CreateInput{ParentID: &i.ID, Title: "z"})
	wantInput(t, err, "parentId", kit.ReasonNotFound)
	_, err = e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{Title: ptr("again")})
	wantNotFound(t, err)
	wantNotFound(t, e.issues.Delete(ctx, e.users.Member, i.ID))
	got, err := e.issues.ByIDs(ctx, []uuid.UUID{i.ID})
	if err != nil || len(got) != 0 {
		t.Fatalf("ByIDs = %+v, %v", got, err)
	}
	// The number is never reused.
	if n := e.create(t, "z").Number; n != 3 {
		t.Fatalf("next number = %d", n)
	}
}
