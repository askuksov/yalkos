package label_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/event/eventtest"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/domain/project"
)

// Deleting a label touches the issues that hold it. Issue writers lock their
// project, then the issue, then share-lock its labels, so the delete has to
// lock the projects of those issues before it locks the label. The sessions
// below play issue writers.

// writer is a transaction that locks rows like an issue writer.
type writer struct {
	t   *testing.T
	tx  pgx.Tx
	pid int
}

func beginWriter(t *testing.T, e env) writer {
	t.Helper()
	ctx := context.Background()
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	w := writer{t: t, tx: tx}
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '5s'`); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&w.pid); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w writer) must(sql string, args ...any) {
	w.t.Helper()
	if _, err := w.tx.Exec(context.Background(), sql, args...); err != nil {
		w.t.Fatalf("%s: %v", sql, err)
	}
}

func (w writer) commit() {
	w.t.Helper()
	if err := w.tx.Commit(context.Background()); err != nil {
		w.t.Fatal(err)
	}
}

// blockedQueries waits until some session waits for a lock that w holds and
// returns the statements of the waiting sessions.
func (w writer) blockedQueries(e env) []string {
	w.t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		rows, err := e.pool.Query(context.Background(),
			`SELECT query FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid))`, w.pid)
		if err != nil {
			w.t.Fatal(err)
		}
		var out []string
		for rows.Next() {
			var q string
			if err := rows.Scan(&q); err != nil {
				w.t.Fatal(err)
			}
			out = append(out, q)
		}
		rows.Close()
		if len(out) > 0 {
			return out
		}
	}
	w.t.Fatalf("no session waits for session %d", w.pid)
	return nil
}

func deleteAsync(e env, id uuid.UUID) <-chan error {
	done := make(chan error, 1)
	go func() { done <- e.labels.Delete(context.Background(), e.users.Admin, id) }()
	return done
}

func result(t *testing.T, done <-chan error, what string) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not finish", what)
	}
}

func TestLabelDeleteLockOrder(t *testing.T) {
	e := newEnv(t)
	bug := e.create(t, e.users.Member, label.CreateInput{Name: "bug"})
	i := e.issueWith(t, bug)
	rec := recorded(t, e)

	// An issue writer: its project, then its issue.
	w := beginWriter(t, e)
	w.must(`SELECT id FROM projects WHERE id = $1 FOR UPDATE`, e.ylk.ID)
	w.must(`SELECT id FROM issues WHERE id = $1 FOR UPDATE`, i.ID)

	done := deleteAsync(e, bug.ID)
	queries := w.blockedQueries(e)
	if len(queries) != 1 || !strings.Contains(queries[0], "FROM projects") {
		t.Fatalf("the delete waits with %q, want a wait for the project", queries)
	}
	// The delete holds no lock on the label yet, so the writer can share-lock
	// it (lock_timeout would fire if the delete held it while waiting for us).
	w.must(`SELECT id FROM labels WHERE id = $1 FOR SHARE`, bug.ID)
	w.commit()

	result(t, done, "Delete")
	if got, want := eventtest.Lines(eventtest.Sorted(rec.Events(t))), []string{
		eventtest.Line(event.Label, event.Remove, bug.ID),
		eventtest.Line(event.Issue, event.Update, i.ID),
	}; !slices.Equal(got, want) {
		t.Errorf("events = %q, want %q", got, want)
	}
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM labels WHERE id = $1`, bug.ID).Scan(&n); err != nil || n != 0 {
		t.Errorf("label rows = %d, %v; want 0", n, err)
	}
}

// A project that got an issue with the label while the delete waited for the
// other projects is not locked: the delete starts over instead of locking it
// on top of the label.
func TestLabelDeleteStartsOverForNewProject(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	bug := e.create(t, e.users.Member, label.CreateInput{Name: "bug"})
	a := e.issueWith(t, bug)
	web, err := project.New(e.pool, time.Now).Create(ctx, e.users.Admin, project.CreateInput{Key: "WEB", Name: "Web"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ProjectID: &web.ID, Title: "b"})
	if err != nil {
		t.Fatal(err)
	}
	rec := recorded(t, e)

	holdA := beginWriter(t, e)
	holdA.must(`SELECT id FROM projects WHERE id = $1 FOR UPDATE`, e.ylk.ID)
	done := deleteAsync(e, bug.ID)
	if q := holdA.blockedQueries(e); len(q) != 1 || !strings.Contains(q[0], "FROM projects") {
		t.Fatalf("the delete waits with %q, want a wait for the project", q)
	}
	// Another writer gives the label to an issue of WEB and, still holding
	// WEB, will ask for the label next.
	give := beginWriter(t, e) // its foreign key waits for a delete that already holds the label
	give.must(`INSERT INTO issue_labels (issue_id, label_id) VALUES ($1, $2)`, b.ID, bug.ID)
	give.commit()
	holdB := beginWriter(t, e)
	holdB.must(`SELECT id FROM projects WHERE id = $1 FOR UPDATE`, web.ID)
	holdA.commit()

	// The first run found WEB unlocked and started over; the second waits for
	// WEB and holds no lock on the label, or this share lock would time out.
	if q := holdB.blockedQueries(e); len(q) != 1 || !strings.Contains(q[0], "FROM projects") {
		t.Fatalf("the restarted delete waits with %q, want a wait for the project", q)
	}
	holdB.must(`SELECT id FROM labels WHERE id = $1 FOR SHARE`, bug.ID)
	holdB.commit()

	result(t, done, "Delete")
	ids := []uuid.UUID{a.ID, b.ID}
	slices.SortFunc(ids, func(x, y uuid.UUID) int { return slices.Compare(x[:], y[:]) })
	want := []string{
		eventtest.Line(event.Label, event.Remove, bug.ID),
		eventtest.Line(event.Issue, event.Update, ids[0]),
		eventtest.Line(event.Issue, event.Update, ids[1]),
	}
	if got := eventtest.Lines(eventtest.Sorted(rec.Events(t))); !slices.Equal(got, want) {
		t.Errorf("events =\n%q\nwant\n%q", got, want)
	}
	for _, c := range rec.Events(t) {
		if c.Resource != event.Issue {
			continue
		}
		wantProject := e.ylk.ID
		if c.EntityID == b.ID {
			wantProject = web.ID
		}
		if !slices.Equal(c.ProjectIDs, []uuid.UUID{wantProject}) {
			t.Errorf("issue %v project ids = %v, want %v", c.EntityID, c.ProjectIDs, wantProject)
		}
	}
}
