package comment_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/event/eventtest"
)

func recorded(t *testing.T, e env) *eventtest.Recorder {
	t.Helper()
	rec := eventtest.NewRecorder(t, e.pool)
	e.comments.SetEmitter(rec)
	e.issues.SetEmitter(rec)
	return rec
}

func TestCommentEvents(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	rec := recorded(t, e)
	// A comment is deleted while its row still exists.
	rec.Probe = func(ctx context.Context, tx pgx.Tx, c event.Change) error {
		if c.Action != event.Remove {
			return nil
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM comments WHERE id = $1`, c.EntityID).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return errors.New("the comment is gone when its remove event is emitted")
		}
		return nil
	}

	c := e.post(t, "first")
	upd, err := e.comments.Update(ctx, e.users.Member, c.ID, "second")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.comments.Update(ctx, e.users.Member, c.ID, "second"); err != nil { // unchanged: no event
		t.Fatal(err)
	}
	if err := e.comments.Delete(ctx, e.users.Member, c.ID); err != nil {
		t.Fatal(err)
	}

	events := rec.Events(t)
	want := []string{
		eventtest.Line(event.Comment, event.Create, c.ID),
		eventtest.Line(event.Comment, event.Update, c.ID),
		eventtest.Line(event.Comment, event.Remove, c.ID),
	}
	if got := eventtest.Lines(events); !slices.Equal(got, want) {
		t.Fatalf("events = %q, want %q (no issue events)", got, want)
	}
	for _, ev := range events {
		if !slices.Equal(ev.ProjectIDs, []uuid.UUID{e.ylk.ID}) || ev.ActorID != e.users.Member.ID {
			t.Errorf("%s %s: projects %v actor %v", ev.Resource, ev.Action, ev.ProjectIDs, ev.ActorID)
		}
	}
	if !events[0].At.Equal(c.CreatedAt) || len(events[0].Diff) != 0 {
		t.Errorf("create event = %+v, comment created at %v", events[0], c.CreatedAt)
	}
	u := events[1]
	if got := eventtest.DiffKeys(u); !slices.Equal(got, []string{"body", "editedAt"}) {
		t.Fatalf("update diff keys = %v, want [body editedAt]", got)
	}
	if b := u.Diff["body"]; b.From != "first" || b.To != "second" {
		t.Errorf("body change = %+v", b)
	}
	if ed := u.Diff["editedAt"]; ed.From != nil || ed.To == nil {
		t.Errorf("editedAt change = %+v, want from nil to a time", ed)
	}
	if !u.PrevUpdatedAt.Equal(c.UpdatedAt) || !u.At.Equal(upd.UpdatedAt) {
		t.Errorf("update prev %v at %v, want prev %v at %v", u.PrevUpdatedAt, u.At, c.UpdatedAt, upd.UpdatedAt)
	}
	if len(events[2].Diff) != 0 {
		t.Errorf("remove diff = %v", events[2].Diff)
	}
}

func TestEmitFailureRollsBackChange(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.post(t, "before")
	rec := recorded(t, e)
	boom := errors.New("boom")

	rec.Fail = boom
	if _, err := e.comments.Update(ctx, e.users.Member, c.ID, "after"); !errors.Is(err, boom) {
		t.Fatalf("Update with a failing emitter: %v, want %v", err, boom)
	}
	if _, err := e.comments.Create(ctx, e.users.Member, e.issue.ID, "new", nil); !errors.Is(err, boom) {
		t.Fatalf("Create with a failing emitter: %v, want %v", err, boom)
	}
	if err := e.comments.Delete(ctx, e.users.Member, c.ID); !errors.Is(err, boom) {
		t.Fatalf("Delete with a failing emitter: %v, want %v", err, boom)
	}
	got, err := e.comments.ByIDs(ctx, []uuid.UUID{c.ID})
	if err != nil || len(got) != 1 || got[0].Body != "before" {
		t.Fatalf("comment after the failed changes = %+v, %v", got, err)
	}
	var n int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM comments`).Scan(&n); err != nil || n != 1 {
		t.Errorf("comments = %d, %v; want 1", n, err)
	}
	if got := rec.Events(t); len(got) != 0 {
		t.Errorf("events after failed changes = %v", eventtest.Lines(got))
	}

	rec.Fail = nil // positive control
	if _, err := e.comments.Update(ctx, e.users.Member, c.ID, "after"); err != nil {
		t.Fatal(err)
	}
	if got := eventtest.Lines(rec.Events(t)); !slices.Equal(got, []string{eventtest.Line(event.Comment, event.Update, c.ID)}) {
		t.Errorf("events = %q", got)
	}
}
