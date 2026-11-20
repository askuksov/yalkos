package issue_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/project"
)

// Milestone and state deletes lock the project like issue writers do. These
// tests queue a delete and then a write behind a session holding the project:
// the write must see the delete and reject the input instead of failing on a
// foreign key.

// waitQueued waits until at least n other sessions of this database wait for
// a lock. Unlike waitBlockedBy it counts a session queued behind another
// waiter: Postgres reports that waiter, not the holder, as its blocker.
func waitQueued(t *testing.T, e env, n int) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		var got int
		if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock' AND pid <> pg_backend_pid()`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got >= n {
			return
		}
	}
	t.Fatalf("fewer than %d sessions wait for a lock", n)
}

// finished returns what done delivers, failing the test after 10 s.
func finished(t *testing.T, name string, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not finish", name)
		return nil
	}
}

// inBackground runs f in a goroutine and delivers its error.
func inBackground(f func() error) <-chan error {
	done := make(chan error, 1)
	go func() { done <- f() }()
	return done
}

// deletedMeanwhile runs del and then write while a session holds the project,
// so the write waits behind the delete, and returns the write's error.
func deletedMeanwhile(t *testing.T, e env, projectID uuid.UUID, del, write func() error) error {
	t.Helper()
	gate := begin(t, e)
	gate.must(`SELECT 1 FROM projects WHERE id = $1 FOR UPDATE`, projectID)
	deleted := inBackground(del)
	waitQueued(t, e, 1)
	written := inBackground(write)
	waitQueued(t, e, 2)
	gate.commit()
	if err := finished(t, "delete", deleted); err != nil {
		t.Fatalf("delete: %v", err)
	}
	return finished(t, "write", written)
}

func TestMilestoneDeletedMeanwhile(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T) (env, milestone.Milestone, func() error) {
		e := newEnv(t)
		m := e.milestone(t, e.ylk, "M")
		del := func() error { return milestone.New(e.pool, e.projects, e.clock).Delete(ctx, e.users.Admin, m.ID) }
		return e, m, del
	}
	refs := func(t *testing.T, e env, m milestone.Milestone) {
		t.Helper()
		var n int
		if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM issues WHERE milestone_id = $1`, m.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%d issues reference the deleted milestone", n)
		}
	}

	t.Run("create", func(t *testing.T) {
		e, m, del := setup(t)
		err := deletedMeanwhile(t, e, e.ylk.ID, del, func() error {
			_, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ProjectID: &e.ylk.ID, Title: "x", MilestoneID: &m.ID})
			return err
		})
		wantInput(t, err, "milestoneId", kit.ReasonNotFound)
		refs(t, e, m)
	})
	t.Run("update", func(t *testing.T) {
		e, m, del := setup(t)
		i := e.create(t, "x")
		err := deletedMeanwhile(t, e, e.ylk.ID, del, func() error {
			_, err := setMilestone(e, i.ID, &m.ID)
			return err
		})
		wantInput(t, err, "milestoneId", kit.ReasonNotFound)
		refs(t, e, m)
	})
	t.Run("inherited", func(t *testing.T) {
		e, m, del := setup(t)
		parent := e.create(t, "p")
		if _, err := setMilestone(e, parent.ID, &m.ID); err != nil {
			t.Fatal(err)
		}
		var child issue.Issue
		err := deletedMeanwhile(t, e, e.ylk.ID, del, func() error {
			var err error
			child, err = e.issues.Create(ctx, e.users.Member, issue.CreateInput{ParentID: &parent.ID, Title: "c"})
			return err
		})
		if err != nil {
			t.Fatalf("sub-issue: %v", err)
		}
		if child.MilestoneID != nil {
			t.Errorf("sub-issue milestone = %v, want none", *child.MilestoneID)
		}
		refs(t, e, m)
	})
}

func TestStateDeletedMeanwhile(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T) (env, project.State, func() error) {
		e := newEnv(t)
		st, err := e.projects.CreateState(ctx, e.users.Admin, e.ylk.ID, project.StateInput{Name: "Review", Type: project.Started, Color: "#ff0000"})
		if err != nil {
			t.Fatal(err)
		}
		del := func() error { return e.projects.DeleteState(ctx, e.users.Admin, st.ID, nil) }
		return e, st, del
	}

	t.Run("create", func(t *testing.T) {
		e, st, del := setup(t)
		err := deletedMeanwhile(t, e, e.ylk.ID, del, func() error {
			_, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ProjectID: &e.ylk.ID, Title: "x", StateID: &st.ID})
			return err
		})
		wantInput(t, err, "stateId", kit.ReasonNotFound)
	})
	t.Run("update", func(t *testing.T) {
		e, st, del := setup(t)
		i := e.create(t, "x")
		err := deletedMeanwhile(t, e, e.ylk.ID, del, func() error {
			_, err := e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{StateID: &st.ID})
			return err
		})
		wantInput(t, err, "stateId", kit.ReasonNotFound)
	})
}
