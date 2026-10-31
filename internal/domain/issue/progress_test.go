package issue_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
)

func TestSubIssueProgress(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	parent := e.create(t, "Parent")
	lonely := e.create(t, "No children")
	child := func(state string) issue.Issue {
		t.Helper()
		st := e.state(t, e.ylk.ID, state)
		c, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ParentID: &parent.ID, Title: state, StateID: &st.ID})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	child("Done")
	child("Todo")
	child("Canceled")
	deleted := child("Done")
	if err := e.issues.Delete(ctx, e.users.Member, deleted.ID); err != nil {
		t.Fatal(err)
	}
	got, err := e.issues.SubIssueProgress(ctx, []uuid.UUID{parent.ID, lonely.ID})
	if err != nil {
		t.Fatal(err)
	}
	if p := got[parent.ID]; p != (issue.Progress{Completed: 1, Total: 2}) {
		t.Errorf("parent progress = %+v, want {1 2}", p)
	}
	if p := got[lonely.ID]; p != (issue.Progress{}) {
		t.Errorf("no children progress = %+v, want {0 0}", p)
	}
}
