package label_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/label"
)

// Issue label writes share-lock an issue's labels and their groups in one
// statement ordered by id. These tests hold such locks from a second
// transaction in the order that used to deadlock with label structure changes.

// holder is a transaction that takes row locks on labels like a concurrent writer.
type holder struct {
	t  *testing.T
	tx pgx.Tx
}

func hold(t *testing.T, e env) holder {
	t.Helper()
	tx, err := e.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, err := tx.Exec(context.Background(), `SET LOCAL lock_timeout = '5s'`); err != nil {
		t.Fatal(err)
	}
	return holder{t: t, tx: tx}
}

func (h holder) lock(id uuid.UUID, mode string) error {
	_, err := h.tx.Exec(context.Background(), `SELECT id FROM labels WHERE id = $1 FOR `+mode, id)
	return err
}

func (h holder) commit() {
	h.t.Helper()
	if err := h.tx.Commit(context.Background()); err != nil {
		h.t.Fatal(err)
	}
}

// update runs a label update in the background.
func update(e env, id uuid.UUID, p label.Patch) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := e.labels.Update(context.Background(), e.users.Admin, id, p)
		done <- err
	}()
	return done
}

// waitBlocked waits until another session of this database waits for a row lock.
func waitBlocked(t *testing.T, e env) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock' AND pid <> pg_backend_pid()`).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no session waits for a lock")
}

func lower(a, b uuid.UUID) bool { return bytes.Compare(a[:], b[:]) < 0 }

func TestExclusivityChangeLockOrder(t *testing.T) {
	e := newEnv(t)
	child := e.create(t, e.users.Admin, label.CreateInput{Name: "Child"})
	g := e.create(t, e.users.Admin, label.CreateInput{Name: "Area", IsGroup: true, Exclusive: ptr(false)})
	if !lower(child.ID, g.ID) {
		t.Fatal("fixture: the child's id must sort before the group's")
	}
	if _, err := e.labels.Update(context.Background(), e.users.Admin, child.ID, label.Patch{
		ParentID: kit.Opt[uuid.UUID]{Set: true, Value: &g.ID},
	}); err != nil {
		t.Fatal(err)
	}

	h := hold(t, e)
	if err := h.lock(child.ID, "SHARE"); err != nil {
		t.Fatal(err)
	}
	done := update(e, g.ID, label.Patch{Exclusive: ptr(true)})
	time.Sleep(300 * time.Millisecond) // let the change take its locks
	if err := h.lock(g.ID, "SHARE"); err != nil {
		t.Fatalf("writer locking the group after the child: %v", err)
	}
	h.commit()
	if err := <-done; err != nil {
		t.Fatalf("making the group exclusive: %v", err)
	}
	if got := e.byName(t, "Area"); !got.Exclusive {
		t.Error("group is not exclusive")
	}
}

func TestRegroupLockOrder(t *testing.T) {
	e := newEnv(t)
	g := e.create(t, e.users.Admin, label.CreateInput{Name: "Area", IsGroup: true, Exclusive: ptr(false)})
	l := e.create(t, e.users.Admin, label.CreateInput{Name: "Web"})
	if !lower(g.ID, l.ID) {
		t.Fatal("fixture: the group's id must sort before the label's")
	}

	h := hold(t, e)
	if err := h.lock(g.ID, "SHARE"); err != nil {
		t.Fatal(err)
	}
	done := update(e, l.ID, label.Patch{ParentID: kit.Opt[uuid.UUID]{Set: true, Value: &g.ID}})
	waitBlocked(t, e)
	if err := h.lock(l.ID, "SHARE"); err != nil {
		t.Fatalf("writer locking the label after the group: %v", err)
	}
	h.commit()
	if err := <-done; err != nil {
		t.Fatalf("moving the label into the group: %v", err)
	}
	if got := e.byName(t, "Web"); got.ParentID == nil || *got.ParentID != g.ID {
		t.Errorf("label parent = %v, want %v", got.ParentID, g.ID)
	}
}

// A label leaving a group waits for a concurrent change of that group (for
// example its exclusivity), so the group's issue labels are updated in order.
func TestRegroupLocksOldGroup(t *testing.T) {
	e := newEnv(t)
	from := e.create(t, e.users.Admin, label.CreateInput{Name: "From", IsGroup: true, Exclusive: ptr(false)})
	to := e.create(t, e.users.Admin, label.CreateInput{Name: "To", IsGroup: true, Exclusive: ptr(false)})
	l := e.create(t, e.users.Admin, label.CreateInput{Name: "Web", ParentID: &from.ID})

	h := hold(t, e)
	if err := h.lock(from.ID, "UPDATE"); err != nil {
		t.Fatal(err)
	}
	done := update(e, l.ID, label.Patch{ParentID: kit.Opt[uuid.UUID]{Set: true, Value: &to.ID}})
	select {
	case err := <-done:
		t.Fatalf("move finished while the old group was locked (err %v)", err)
	case <-time.After(500 * time.Millisecond):
	}
	h.commit()
	if err := <-done; err != nil {
		t.Fatalf("moving the label: %v", err)
	}
	if got := e.byName(t, "Web"); got.ParentID == nil || *got.ParentID != to.ID {
		t.Errorf("label parent = %v, want %v", got.ParentID, to.ID)
	}
}
