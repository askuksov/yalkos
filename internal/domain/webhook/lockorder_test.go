package webhook_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/webhook"
)

// Domain writers hold their project row (FOR UPDATE) when they emit an event,
// and the delivery insert then key-shares the webhook row. A webhook edit must
// therefore not lock the webhook row harder than FOR NO KEY UPDATE, or an edit
// that moves the webhook to a project (a key share on the project row) and an
// event write in that project wait for each other.

// writer is a transaction that behaves like a domain write: it holds a project
// row lock and emits an event before it commits.
type writer struct {
	t  *testing.T
	tx pgx.Tx
}

func startWriter(t *testing.T, e emitEnv, projectID uuid.UUID) writer {
	t.Helper()
	tx, err := e.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, err := tx.Exec(context.Background(), `SET LOCAL lock_timeout = '5s'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `SELECT id FROM projects WHERE id = $1 FOR UPDATE`, projectID); err != nil {
		t.Fatal(err)
	}
	return writer{t: t, tx: tx}
}

// emitAndCommit emits an issue event of the project and commits.
func (w writer) emitAndCommit(e emitEnv, projectID uuid.UUID) error {
	c := event.Change{
		Resource: event.Issue, Action: event.Create, EntityID: uuid.New(), ProjectIDs: []uuid.UUID{projectID},
		ActorID: e.users.Admin.ID, At: at,
	}
	if err := e.svc.Emit(context.Background(), w.tx, c); err != nil {
		return err
	}
	return w.tx.Commit(context.Background())
}

// waiters counts the other sessions of this database that wait for a lock.
func waiters(t *testing.T, e emitEnv) int {
	t.Helper()
	return e.count(t, `SELECT count(*) FROM pg_stat_activity
		WHERE datname = current_database() AND wait_event_type = 'Lock' AND pid <> pg_backend_pid()`)
}

func waitForWaiters(t *testing.T, e emitEnv, n int) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if waiters(t, e) >= n {
			return
		}
	}
	t.Fatalf("fewer than %d sessions wait for a lock", n)
}

func result(t *testing.T, ch <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not finish", what)
		return nil
	}
}

func isDeadlock(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "40P01"
}

// Moving a webhook to a project while an event write holds that project and
// emits to the webhook must not deadlock.
func TestUpdateScopeDoesNotDeadlockWithEmit(t *testing.T) {
	e := newEmitEnv(t)
	p := e.project(t, "AAA")
	w := e.create(t, webhook.CreateInput{Enabled: true})

	wr := startWriter(t, e, p.ID)
	edited := make(chan error, 1)
	go func() {
		_, err := e.svc.Update(context.Background(), e.users.Admin, w.ID, webhook.Patch{ProjectID: kit.Opt[uuid.UUID]{Set: true, Value: &p.ID}})
		edited <- err
	}()
	// The edit holds the webhook and waits for the project row.
	waitForWaiters(t, e, 1)

	emitted := make(chan error, 1)
	go func() { emitted <- wr.emitAndCommit(e, p.ID) }()

	for _, r := range []struct {
		what string
		ch   <-chan error
	}{{"event write", emitted}, {"webhook edit", edited}} {
		if err := result(t, r.ch, r.what); err != nil {
			if isDeadlock(err) {
				t.Fatalf("%s: deadlock: %v", r.what, err)
			}
			t.Fatalf("%s: %v", r.what, err)
		}
	}
	if n := e.count(t, `SELECT count(*) FROM webhook_deliveries WHERE webhook_id = $1`, w.ID); n != 1 {
		t.Errorf("deliveries = %d, want 1", n)
	}
	got, err := e.svc.Get(context.Background(), e.users.Admin, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID == nil || *got.ProjectID != p.ID {
		t.Errorf("webhook project = %v, want %v", got.ProjectID, p.ID)
	}
}

// Positive control: the same sequence with an edit that does not touch the
// scope never waits for the project row, so nothing can deadlock either.
func TestUpdateNameWithEmitControl(t *testing.T) {
	e := newEmitEnv(t)
	p := e.project(t, "AAA")
	w := e.create(t, webhook.CreateInput{Enabled: true})

	wr := startWriter(t, e, p.ID)
	if _, err := e.svc.Update(context.Background(), e.users.Admin, w.ID, webhook.Patch{Name: ptr("Renamed")}); err != nil {
		t.Fatalf("webhook edit: %v", err)
	}
	if err := wr.emitAndCommit(e, p.ID); err != nil {
		t.Fatalf("event write: %v", err)
	}
	if n := e.count(t, `SELECT count(*) FROM webhook_deliveries WHERE webhook_id = $1`, w.ID); n != 1 {
		t.Errorf("deliveries = %d, want 1", n)
	}
}

// keyShare holds a key share on the webhook row, as a delivery insert does.
func keyShare(t *testing.T, e emitEnv, id uuid.UUID) pgx.Tx {
	t.Helper()
	tx, err := e.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, err := tx.Exec(context.Background(), `SELECT id FROM webhooks WHERE id = $1 FOR KEY SHARE`, id); err != nil {
		t.Fatal(err)
	}
	return tx
}

// An edit leaves the webhook's key alone, so it does not wait for a key share.
func TestEditsDoNotWaitForKeyShare(t *testing.T) {
	e := newEmitEnv(t)
	w := e.create(t, webhook.CreateInput{Enabled: true})
	tx := keyShare(t, e, w.ID)

	done := make(chan error, 1)
	go func() {
		_, err := e.svc.Update(context.Background(), e.users.Admin, w.ID, webhook.Patch{Name: ptr("Renamed")})
		if err == nil {
			_, err = e.svc.RotateSecret(context.Background(), e.users.Admin, w.ID)
		}
		done <- err
	}()
	if err := result(t, done, "edit"); err != nil {
		t.Fatalf("edit while a key share is held: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// Delete removes the key, so it waits for a key share to go (positive control
// for the test above: the same holder, a different operation).
func TestDeleteWaitsForKeyShare(t *testing.T) {
	e := newEmitEnv(t)
	w := e.create(t, webhook.CreateInput{Enabled: true})
	tx := keyShare(t, e, w.ID)

	done := make(chan error, 1)
	go func() { done <- e.svc.Delete(context.Background(), e.users.Admin, w.ID) }()
	waitForWaiters(t, e, 1)
	select {
	case err := <-done:
		t.Fatalf("delete finished while a key share was held (err %v)", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := result(t, done, "delete"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n := e.count(t, `SELECT count(*) FROM webhooks WHERE id = $1`, w.ID); n != 0 {
		t.Errorf("webhook rows = %d, want 0", n)
	}
}
