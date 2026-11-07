package milestone_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/event/eventtest"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/milestone"
)

func recorded(t *testing.T, e env) *eventtest.Recorder {
	t.Helper()
	rec := eventtest.NewRecorder(t, e.pool)
	e.milestones.SetEmitter(rec)
	return rec
}

func updatedAt(t *testing.T, e env, table string, id uuid.UUID) time.Time {
	t.Helper()
	var at time.Time
	if err := e.pool.QueryRow(context.Background(), `SELECT updated_at FROM `+table+` WHERE id = $1`, id).Scan(&at); err != nil {
		t.Fatal(err)
	}
	return at
}

func TestMilestoneEvents(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	rec := recorded(t, e)
	// The milestone is announced while it still exists, after its issues lost it.
	var removed int
	rec.Probe = func(ctx context.Context, tx pgx.Tx, c event.Change) error {
		if c.Resource != event.Milestone || c.Action != event.Remove {
			return nil
		}
		removed++
		var exists bool
		var issues int
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM milestones WHERE id = $1),
			(SELECT count(*) FROM issues WHERE milestone_id = $1)`, c.EntityID).Scan(&exists, &issues); err != nil {
			return err
		}
		if !exists || issues != 0 {
			return errors.New("the milestone is gone or still has issues when its remove event is emitted")
		}
		return nil
	}

	m := e.create(t, "Launch")
	upd, err := e.milestones.Update(ctx, e.users.Member, m.ID, milestone.Patch{
		Status: kit.Opt[milestone.Status]{Set: true, Value: ptr(milestone.Canceled)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.milestones.Update(ctx, e.users.Member, m.ID, milestone.Patch{Name: ptr("Launch")}); err != nil { // unchanged: no event
		t.Fatal(err)
	}
	i1 := e.issueIn(t, m, "Todo")
	i2 := e.issueIn(t, m, "Todo")
	prev := map[uuid.UUID]time.Time{i1.ID: updatedAt(t, e, "issues", i1.ID), i2.ID: updatedAt(t, e, "issues", i2.ID)}
	if err := e.milestones.Delete(ctx, e.users.Member, m.ID); err != nil {
		t.Fatal(err)
	}

	events := eventtest.Sorted(rec.Events(t))
	ids := []uuid.UUID{i1.ID, i2.ID}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	want := []string{
		eventtest.Line(event.Milestone, event.Create, m.ID),
		eventtest.Line(event.Milestone, event.Update, m.ID),
		eventtest.Line(event.Issue, event.Update, ids[0]),
		eventtest.Line(event.Issue, event.Update, ids[1]),
		eventtest.Line(event.Milestone, event.Remove, m.ID),
	}
	if got := eventtest.Lines(events); !slices.Equal(got, want) || removed != 1 {
		t.Fatalf("events =\n%q\nwant\n%q (remove probed %d times)", got, want, removed)
	}
	for _, c := range events {
		if !slices.Equal(c.ProjectIDs, []uuid.UUID{e.ylk.ID}) || c.ActorID != e.users.Member.ID {
			t.Errorf("%s %s: projects %v actor %v", c.Resource, c.Action, c.ProjectIDs, c.ActorID)
		}
	}
	if len(events[0].Diff) != 0 || !events[0].At.Equal(m.CreatedAt) {
		t.Errorf("create event = %+v, milestone created at %v", events[0], m.CreatedAt)
	}
	u := events[1]
	if got := eventtest.DiffKeys(u); !slices.Equal(got, []string{"status"}) {
		t.Errorf("update diff keys = %v, want [status]", got)
	}
	if s := u.Diff["status"]; s.From != "automatic" || s.To != "canceled" {
		t.Errorf("status change = %+v", s)
	}
	if !u.PrevUpdatedAt.Equal(m.UpdatedAt) || !u.At.Equal(upd.UpdatedAt) {
		t.Errorf("update prev %v at %v, want prev %v at %v", u.PrevUpdatedAt, u.At, m.UpdatedAt, upd.UpdatedAt)
	}
	for _, c := range events[2:4] {
		if got := eventtest.DiffKeys(c); !slices.Equal(got, []string{"milestoneId"}) {
			t.Errorf("issue %v diff keys = %v", c.EntityID, got)
		}
		if ch := c.Diff["milestoneId"]; ch.From != m.ID.String() || ch.To != nil {
			t.Errorf("issue %v milestoneId change = %+v", c.EntityID, ch)
		}
		if !c.PrevUpdatedAt.Equal(prev[c.EntityID]) || !c.At.Equal(updatedAt(t, e, "issues", c.EntityID)) {
			t.Errorf("issue %v: prev %v at %v, want prev %v", c.EntityID, c.PrevUpdatedAt, c.At, prev[c.EntityID])
		}
	}
	if len(events[4].Diff) != 0 {
		t.Errorf("remove diff = %v", events[4].Diff)
	}
}

func TestMilestoneReorderEvents(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a, b, c := e.create(t, "A"), e.create(t, "B"), e.create(t, "C")
	before := map[uuid.UUID]time.Time{}
	for _, m := range []milestone.Milestone{a, b, c} {
		before[m.ID] = updatedAt(t, e, "milestones", m.ID)
	}
	rec := recorded(t, e)

	if err := e.milestones.Reorder(ctx, e.users.Member, e.ylk.ID, []uuid.UUID{b.ID, a.ID, c.ID}); err != nil {
		t.Fatal(err)
	}
	events := eventtest.Sorted(rec.Events(t))
	ids := []uuid.UUID{a.ID, b.ID}
	slices.SortFunc(ids, func(x, y uuid.UUID) int { return slices.Compare(x[:], y[:]) })
	want := []string{eventtest.Line(event.Milestone, event.Update, ids[0]), eventtest.Line(event.Milestone, event.Update, ids[1])}
	if got := eventtest.Lines(events); !slices.Equal(got, want) {
		t.Fatalf("events = %q, want %q (C keeps its place)", got, want)
	}
	for _, ev := range events {
		if got := eventtest.DiffKeys(ev); !slices.Equal(got, []string{"sortOrder"}) {
			t.Errorf("diff keys = %v, want [sortOrder]", got)
		}
		after := updatedAt(t, e, "milestones", ev.EntityID)
		if !after.After(before[ev.EntityID]) || !ev.At.Equal(after) || !ev.PrevUpdatedAt.Equal(before[ev.EntityID]) {
			t.Errorf("milestone %v: updated_at %v, at %v, prev %v; before %v", ev.EntityID, after, ev.At, ev.PrevUpdatedAt, before[ev.EntityID])
		}
		if !slices.Equal(ev.ProjectIDs, []uuid.UUID{e.ylk.ID}) || ev.ActorID != e.users.Member.ID {
			t.Errorf("milestone %v: projects %v actor %v", ev.EntityID, ev.ProjectIDs, ev.ActorID)
		}
		wantFrom, wantTo := a.SortOrder, 2000.0 // A moves from first to second place
		if ev.EntityID == b.ID {
			wantFrom, wantTo = b.SortOrder, 1000.0
		}
		if ch := ev.Diff["sortOrder"]; ch.From != wantFrom || ch.To != wantTo {
			t.Errorf("milestone %v sortOrder change = %+v, want %v to %v", ev.EntityID, ch, wantFrom, wantTo)
		}
	}
	if got := updatedAt(t, e, "milestones", c.ID); !got.Equal(before[c.ID]) {
		t.Errorf("C updated_at = %v, want %v (its order did not change)", got, before[c.ID])
	}

	// The same order again changes nothing.
	if err := e.milestones.Reorder(ctx, e.users.Member, e.ylk.ID, []uuid.UUID{b.ID, a.ID, c.ID}); err != nil {
		t.Fatal(err)
	}
	if got := rec.Events(t); len(got) != 2 {
		t.Errorf("%d events after a reorder that changed nothing, want the 2 from before", len(got))
	}
}

func TestEmitFailureRollsBackChange(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a, b := e.create(t, "A"), e.create(t, "B")
	i := e.issueIn(t, a, "Todo")
	rec := recorded(t, e)
	boom := errors.New("boom")

	rec.Fail = boom
	if _, err := e.milestones.Update(ctx, e.users.Member, a.ID, milestone.Patch{Name: ptr("renamed")}); !errors.Is(err, boom) {
		t.Fatalf("Update with a failing emitter: %v, want %v", err, boom)
	}
	if _, err := e.milestones.Create(ctx, e.users.Member, e.ylk.ID, milestone.CreateInput{Name: "new"}); !errors.Is(err, boom) {
		t.Fatalf("Create with a failing emitter: %v, want %v", err, boom)
	}
	if err := e.milestones.Reorder(ctx, e.users.Member, e.ylk.ID, []uuid.UUID{b.ID, a.ID}); !errors.Is(err, boom) {
		t.Fatalf("Reorder with a failing emitter: %v, want %v", err, boom)
	}
	issueAt := updatedAt(t, e, "issues", i.ID)
	if err := e.milestones.Delete(ctx, e.users.Member, a.ID); !errors.Is(err, boom) {
		t.Fatalf("Delete with a failing emitter: %v, want %v", err, boom)
	}
	got, err := e.milestones.List(ctx, e.ylk.ID, true)
	if err != nil || len(got) != 2 || got[0].ID != a.ID || got[0].Name != "A" || !got[0].UpdatedAt.Equal(a.UpdatedAt) {
		t.Fatalf("milestones after the failed changes = %+v, %v", got, err)
	}
	var milestoneID *uuid.UUID
	var at time.Time
	if err := e.pool.QueryRow(ctx, `SELECT milestone_id, updated_at FROM issues WHERE id = $1`, i.ID).Scan(&milestoneID, &at); err != nil {
		t.Fatal(err)
	}
	if milestoneID == nil || *milestoneID != a.ID || !at.Equal(issueAt) {
		t.Errorf("issue after the failed delete: milestone %v updated_at %v, want %v at %v", milestoneID, at, a.ID, issueAt)
	}
	if got := rec.Events(t); len(got) != 0 {
		t.Errorf("events after failed changes = %v", eventtest.Lines(got))
	}

	rec.Fail = nil // positive control
	if _, err := e.milestones.Update(ctx, e.users.Member, a.ID, milestone.Patch{Name: ptr("renamed")}); err != nil {
		t.Fatal(err)
	}
	if got := eventtest.Lines(rec.Events(t)); !slices.Equal(got, []string{eventtest.Line(event.Milestone, event.Update, a.ID)}) {
		t.Errorf("events = %q", got)
	}
}
