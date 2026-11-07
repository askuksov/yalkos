package label_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/domain/issue"
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

// waiting counts the other sessions of this database that wait for a lock.
func waiting(t *testing.T, e env) int {
	t.Helper()
	var n int
	err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_stat_activity
		WHERE datname = current_database() AND wait_event_type = 'Lock' AND pid <> pg_backend_pid()`).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// blocked reports whether another session of this database waits for a lock.
func blocked(t *testing.T, e env) bool { return waiting(t, e) > 0 }

// waitBlocked waits until another session of this database waits for a lock.
func waitBlocked(t *testing.T, e env) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if blocked(t, e) {
			return
		}
	}
	t.Fatal("no session waits for a lock")
}

// settle waits until the background update finished or waits for a lock,
// whichever happens first, and returns a channel with its result.
func settle(t *testing.T, e env, done <-chan error) <-chan error {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		select {
		case err := <-done:
			again := make(chan error, 1)
			again <- err
			return again
		default:
		}
		if blocked(t, e) {
			return done
		}
	}
	t.Fatal("update neither finished nor waits for a lock")
	return nil
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
	done := settle(t, e, update(e, g.ID, label.Patch{Exclusive: ptr(true)}))
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

// An issue write that waited for a label moved to another group meanwhile
// locks the new group too, so a concurrent change of that group (for example
// making it exclusive) cannot miss the new issue label.
func TestIssueWriteLocksNewGroupOfMovedLabel(t *testing.T) {
	e := newEnv(t)
	from := e.create(t, e.users.Admin, label.CreateInput{Name: "From", IsGroup: true, Exclusive: ptr(false)})
	to := e.create(t, e.users.Admin, label.CreateInput{Name: "To", IsGroup: true, Exclusive: ptr(false)})
	l := e.create(t, e.users.Admin, label.CreateInput{Name: "Web", ParentID: &from.ID})
	i := e.issueWith(t)

	move := hold(t, e) // a label move in progress
	if err := move.lock(l.ID, "UPDATE"); err != nil {
		t.Fatal(err)
	}
	if _, err := move.tx.Exec(context.Background(), `UPDATE labels SET parent_id = $1 WHERE id = $2`, to.ID, l.ID); err != nil {
		t.Fatal(err)
	}
	// A change of the new group in progress. NO KEY UPDATE, unlike UPDATE, does
	// not wait for the key share the move's foreign key holds on the group, and
	// still blocks the share lock an issue write takes on it.
	change := hold(t, e)
	if err := change.lock(to.ID, "NO KEY UPDATE"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := e.issues.Update(context.Background(), e.users.Member, i.ID, issue.Patch{AddLabelIDs: []uuid.UUID{l.ID}})
		done <- err
	}()
	waitBlocked(t, e)
	move.commit()
	select {
	case err := <-done:
		t.Fatalf("issue write finished while the label's new group was locked (err %v)", err)
	case <-time.After(500 * time.Millisecond):
	}
	change.commit()
	if err := <-done; err != nil {
		t.Fatalf("issue write: %v", err)
	}
	if got := e.issueLabels(t, i.ID); len(got) != 1 || got[0] != "Web:-" {
		t.Errorf("issue labels = %v, want [Web:-]", got)
	}
}

// When the moved label's new group sorts before it and a concurrent label
// change holds that group while waiting for the label, an issue write must
// let the label go before it waits for the group: it starts over.
func TestIssueWriteRestartsWhenMovedLabelGroupIsHeld(t *testing.T) {
	e := newEnv(t)
	to := e.create(t, e.users.Admin, label.CreateInput{Name: "To", IsGroup: true, Exclusive: ptr(false)})
	from := e.create(t, e.users.Admin, label.CreateInput{Name: "From", IsGroup: true, Exclusive: ptr(false)})
	l := e.create(t, e.users.Admin, label.CreateInput{Name: "Web", ParentID: &from.ID})
	if !lower(to.ID, l.ID) {
		t.Fatal("fixture: the new group's id must sort before the label's")
	}
	i := e.issueWith(t)

	move := hold(t, e) // a label move in progress
	if err := move.lock(l.ID, "UPDATE"); err != nil {
		t.Fatal(err)
	}
	if _, err := move.tx.Exec(context.Background(), `UPDATE labels SET parent_id = $1 WHERE id = $2`, to.ID, l.ID); err != nil {
		t.Fatal(err)
	}
	// A label change holding the new group (NO KEY UPDATE: see above) that
	// next wants the label.
	change := hold(t, e)
	if err := change.lock(to.ID, "NO KEY UPDATE"); err != nil {
		t.Fatal(err)
	}
	written := make(chan error, 1)
	go func() {
		_, err := e.issues.Update(context.Background(), e.users.Member, i.ID, issue.Patch{AddLabelIDs: []uuid.UUID{l.ID}})
		written <- err
	}()
	waitBlocked(t, e)
	locked := make(chan error, 1)
	go func() { locked <- change.lock(l.ID, "UPDATE") }()
	for deadline := time.Now().Add(5 * time.Second); waiting(t, e) < 2; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the change does not wait for the label")
		}
	}
	move.commit()
	if err := <-locked; err != nil {
		t.Fatalf("label change locking the label: %v", err)
	}
	change.commit()
	if err := <-written; err != nil {
		t.Fatalf("issue write: %v", err)
	}
	if got := e.issueLabels(t, i.ID); len(got) != 1 || got[0] != "Web:-" {
		t.Errorf("issue labels = %v, want [Web:-]", got)
	}
}

// The same for a label change: when the label moved to a group that sorts
// before it while the change waited, and another change holds that group
// waiting for the label, the change starts over instead of waiting while it
// holds the label.
func TestLabelChangeRestartsWhenMovedMeanwhile(t *testing.T) {
	e := newEnv(t)
	to := e.create(t, e.users.Admin, label.CreateInput{Name: "To", IsGroup: true, Exclusive: ptr(false)})
	from := e.create(t, e.users.Admin, label.CreateInput{Name: "From", IsGroup: true, Exclusive: ptr(false)})
	l := e.create(t, e.users.Admin, label.CreateInput{Name: "Web", ParentID: &from.ID})
	if !lower(to.ID, l.ID) {
		t.Fatal("fixture: the new group's id must sort before the label's")
	}

	move := hold(t, e) // a label move in progress
	if err := move.lock(l.ID, "UPDATE"); err != nil {
		t.Fatal(err)
	}
	if _, err := move.tx.Exec(context.Background(), `UPDATE labels SET parent_id = $1 WHERE id = $2`, to.ID, l.ID); err != nil {
		t.Fatal(err)
	}
	change := hold(t, e) // another change holding the new group (NO KEY UPDATE: see above)
	if err := change.lock(to.ID, "NO KEY UPDATE"); err != nil {
		t.Fatal(err)
	}
	renamed := update(e, l.ID, label.Patch{Name: ptr("Frontend")})
	waitBlocked(t, e)
	locked := make(chan error, 1)
	go func() { locked <- change.lock(l.ID, "UPDATE") }()
	for deadline := time.Now().Add(5 * time.Second); waiting(t, e) < 2; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the other change does not wait for the label")
		}
	}
	move.commit()
	if err := <-locked; err != nil {
		t.Fatalf("other change locking the label: %v", err)
	}
	change.commit()
	if err := <-renamed; err != nil {
		t.Fatalf("renaming the label: %v", err)
	}
	if got := e.byName(t, "Frontend"); got.ParentID == nil || *got.ParentID != to.ID {
		t.Errorf("label parent = %v, want %v", got.ParentID, to.ID)
	}
}
