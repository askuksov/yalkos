package label_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/event/eventtest"
	"github.com/askuksov/yalkos/internal/domain/label"
)

func recorded(t *testing.T, e env) *eventtest.Recorder {
	t.Helper()
	rec := eventtest.NewRecorder(t, e.pool)
	e.labels.SetEmitter(rec)
	return rec
}

func updatedAt(t *testing.T, e env, issueID uuid.UUID) time.Time {
	t.Helper()
	var at time.Time
	if err := e.pool.QueryRow(context.Background(), `SELECT updated_at FROM issues WHERE id = $1`, issueID).Scan(&at); err != nil {
		t.Fatal(err)
	}
	return at
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLabelEvents(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	rec := recorded(t, e)

	g := e.create(t, e.users.Admin, label.CreateInput{Name: "Area", IsGroup: true, Exclusive: ptr(false)})
	renamed, err := e.labels.Update(ctx, e.users.Admin, g.ID, label.Patch{Name: ptr("Areas")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.labels.Update(ctx, e.users.Admin, g.ID, label.Patch{Exclusive: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.labels.Update(ctx, e.users.Admin, g.ID, label.Patch{Name: ptr("Areas")}); err != nil { // unchanged: no event
		t.Fatal(err)
	}

	events := rec.Events(t)
	want := []string{
		eventtest.Line(event.Label, event.Create, g.ID),
		eventtest.Line(event.Label, event.Update, g.ID),
		eventtest.Line(event.Label, event.Update, g.ID),
	}
	if got := eventtest.Lines(events); !slices.Equal(got, want) {
		t.Fatalf("events = %q, want %q", got, want)
	}
	for n, c := range events {
		if len(c.ProjectIDs) != 0 || c.ActorID != e.users.Admin.ID {
			t.Errorf("event %d: projects %v actor %v", n, c.ProjectIDs, c.ActorID)
		}
	}
	if len(events[0].Diff) != 0 || !events[0].At.Equal(g.CreatedAt) {
		t.Errorf("create event = %+v, label created at %v", events[0], g.CreatedAt)
	}
	if got := eventtest.DiffKeys(events[1]); !slices.Equal(got, []string{"name"}) {
		t.Errorf("rename diff keys = %v", got)
	}
	if got := eventtest.DiffKeys(events[2]); !slices.Equal(got, []string{"exclusive"}) {
		t.Errorf("exclusivity diff keys = %v", got)
	}
	if !events[1].PrevUpdatedAt.Equal(g.UpdatedAt) || !events[1].At.Equal(renamed.UpdatedAt) || !events[2].PrevUpdatedAt.Equal(renamed.UpdatedAt) {
		t.Errorf("rename: prev %v at %v; next prev %v; want %v, %v, %v",
			events[1].PrevUpdatedAt, events[1].At, events[2].PrevUpdatedAt, g.UpdatedAt, renamed.UpdatedAt, renamed.UpdatedAt)
	}
	if c := events[1].Diff["name"]; c.From != "Area" || c.To != "Areas" {
		t.Errorf("name change = %+v", c)
	}
}

func TestLabelDeleteEvents(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	bug := e.create(t, e.users.Member, label.CreateInput{Name: "bug"})
	perf := e.create(t, e.users.Member, label.CreateInput{Name: "perf"})
	i1 := e.issueWith(t, bug)
	i2 := e.issueWith(t, bug, perf)
	i3 := e.issueWith(t, bug)
	e.issueWith(t, perf) // not affected
	before := map[uuid.UUID]time.Time{}
	for _, i := range []uuid.UUID{i1.ID, i2.ID, i3.ID} {
		before[i] = updatedAt(t, e, i)
	}

	rec := recorded(t, e)
	rec.Probe = func(ctx context.Context, tx pgx.Tx, c event.Change) error {
		var hasLabel, touched bool
		switch c.Resource {
		case event.Label:
			// The label is announced while it still exists.
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM labels WHERE id = $1)`, c.EntityID).Scan(&hasLabel); err != nil {
				return err
			}
			if !hasLabel {
				return errors.New("the label is gone when its remove event is emitted")
			}
		case event.Issue:
			// An issue is announced after it lost the label and got a new updated_at.
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM issue_labels WHERE issue_id = $1 AND label_id = $2),
				(SELECT updated_at > $3 FROM issues WHERE id = $1)`, c.EntityID, bug.ID, c.PrevUpdatedAt).Scan(&hasLabel, &touched); err != nil {
				return err
			}
			if hasLabel || !touched {
				return errors.New("issue event emitted before the label left the issue or the issue was touched")
			}
		}
		return nil
	}
	if err := e.labels.Delete(ctx, e.users.Admin, bug.ID); err != nil {
		t.Fatal(err)
	}

	ids := []uuid.UUID{i1.ID, i2.ID, i3.ID}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	events := eventtest.Sorted(rec.Events(t))
	want := []string{
		eventtest.Line(event.Label, event.Remove, bug.ID),
		eventtest.Line(event.Issue, event.Update, ids[0]),
		eventtest.Line(event.Issue, event.Update, ids[1]),
		eventtest.Line(event.Issue, event.Update, ids[2]),
	}
	if got := eventtest.Lines(events); !slices.Equal(got, want) {
		t.Fatalf("events =\n%q\nwant\n%q", got, want)
	}
	if len(events[0].ProjectIDs) != 0 || len(events[0].Diff) != 0 || events[0].Purged {
		t.Errorf("label remove = %+v", events[0])
	}
	for _, c := range events[1:] {
		if got := eventtest.DiffKeys(c); !slices.Equal(got, []string{"labelIds"}) {
			t.Errorf("issue %v diff keys = %v", c.EntityID, got)
		}
		after := updatedAt(t, e, c.EntityID)
		if !after.After(before[c.EntityID]) || !c.At.Equal(after) || !c.PrevUpdatedAt.Equal(before[c.EntityID]) {
			t.Errorf("issue %v: updated_at %v, at %v, prev %v; before %v", c.EntityID, after, c.At, c.PrevUpdatedAt, before[c.EntityID])
		}
		if !slices.Equal(c.ProjectIDs, []uuid.UUID{e.ylk.ID}) || c.ActorID != e.users.Admin.ID {
			t.Errorf("issue %v: projects %v actor %v", c.EntityID, c.ProjectIDs, c.ActorID)
		}
		from, to := []uuid.UUID{bug.ID}, []uuid.UUID{}
		if c.EntityID == i2.ID {
			from, to = []uuid.UUID{bug.ID, perf.ID}, []uuid.UUID{perf.ID}
			slices.SortFunc(from, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
		}
		if got, want := jsonOf(t, c.Diff["labelIds"].From), jsonOf(t, from); got != want {
			t.Errorf("issue %v labelIds from = %s, want %s", c.EntityID, got, want)
		}
		if got, want := jsonOf(t, c.Diff["labelIds"].To), jsonOf(t, to); got != want {
			t.Errorf("issue %v labelIds to = %s, want %s", c.EntityID, got, want)
		}
	}
}

func TestLabelDeleteWithoutIssuesEvents(t *testing.T) {
	e := newEnv(t)
	l := e.create(t, e.users.Member, label.CreateInput{Name: "bug"})
	rec := recorded(t, e)
	if err := e.labels.Delete(context.Background(), e.users.Admin, l.ID); err != nil {
		t.Fatal(err)
	}
	if got, want := eventtest.Lines(rec.Events(t)), []string{eventtest.Line(event.Label, event.Remove, l.ID)}; !slices.Equal(got, want) {
		t.Fatalf("events = %q, want %q", got, want)
	}
}

func TestEmitFailureRollsBackChange(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	l := e.create(t, e.users.Member, label.CreateInput{Name: "before"})
	i := e.issueWith(t, l)
	rec := recorded(t, e)
	boom := errors.New("boom")

	rec.Fail = boom
	if _, err := e.labels.Update(ctx, e.users.Admin, l.ID, label.Patch{Name: ptr("after")}); !errors.Is(err, boom) {
		t.Fatalf("Update with a failing emitter: %v, want %v", err, boom)
	}
	if _, err := e.labels.Create(ctx, e.users.Member, label.CreateInput{Name: "new", Color: "#000000"}); !errors.Is(err, boom) {
		t.Fatalf("Create with a failing emitter: %v, want %v", err, boom)
	}
	issueAt := updatedAt(t, e, i.ID)
	if err := e.labels.Delete(ctx, e.users.Admin, l.ID); !errors.Is(err, boom) {
		t.Fatalf("Delete with a failing emitter: %v, want %v", err, boom)
	}
	if got := e.byName(t, "before"); got.ID != l.ID {
		t.Errorf("label after the failed changes = %+v", got)
	}
	if got := e.issueLabels(t, i.ID); len(got) != 1 {
		t.Errorf("issue labels after the failed delete = %v, want the label kept", got)
	}
	if got := updatedAt(t, e, i.ID); !got.Equal(issueAt) {
		t.Errorf("issue updated_at = %v after the failed delete, want %v", got, issueAt)
	}
	if got := rec.Events(t); len(got) != 0 {
		t.Errorf("events after failed changes = %v", eventtest.Lines(got))
	}

	rec.Fail = nil // positive control
	if _, err := e.labels.Update(ctx, e.users.Admin, l.ID, label.Patch{Name: ptr("after")}); err != nil {
		t.Fatal(err)
	}
	if got := eventtest.Lines(rec.Events(t)); !slices.Equal(got, []string{eventtest.Line(event.Label, event.Update, l.ID)}) {
		t.Errorf("events = %q", got)
	}
}
