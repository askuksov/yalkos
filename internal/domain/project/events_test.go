package project_test

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
	"github.com/askuksov/yalkos/internal/domain/project"
)

func recorded(t *testing.T, e env) *eventtest.Recorder {
	t.Helper()
	rec := eventtest.NewRecorder(t, e.pool)
	e.svc.SetEmitter(rec)
	return rec
}

func insertMilestone(t *testing.T, e env, projectID uuid.UUID, name string, order int) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO milestones
		(project_id, name, sort_order, created_by, created_at, updated_at) VALUES ($1, $2, $3, $4, now(), now()) RETURNING id`,
		projectID, name, order, e.users.Member.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestProjectDeleteEvents(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.create(t, "YLK")
	todo := stateByName(t, e, p.ID, "Todo")
	live1 := insertIssue(t, e, p.ID, todo.ID, 1, false)
	live2 := insertIssue(t, e, p.ID, todo.ID, 2, false)
	gone := insertIssue(t, e, p.ID, todo.ID, 3, true)
	m1 := insertMilestone(t, e, p.ID, "First", 1000)
	m2 := insertMilestone(t, e, p.ID, "Second", 2000)
	if _, err := e.pool.Exec(ctx, `INSERT INTO comments (issue_id, author_id, body, created_at, updated_at)
		VALUES ($1, $2, 'hi', now(), now())`, live1, e.users.Member.ID); err != nil {
		t.Fatal(err)
	}
	other := e.create(t, "WEB")
	insertIssue(t, e, other.ID, stateByName(t, e, other.ID, "Todo").ID, 1, false)
	insertMilestone(t, e, other.ID, "Elsewhere", 1000)

	rec := recorded(t, e)
	// Every event is emitted while the rows it describes still exist.
	rec.Probe = func(ctx context.Context, tx pgx.Tx, c event.Change) error {
		table := map[event.Resource]string{event.Issue: "issues", event.Milestone: "milestones"}[c.Resource]
		if table == "" {
			return errors.New("unexpected event for " + string(c.Resource))
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE id = $1`, c.EntityID).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return errors.New(string(c.Resource) + " is gone when its event is emitted")
		}
		return nil
	}

	if err := e.svc.Delete(ctx, e.users.Owner, p.ID, "YLK"); err != nil {
		t.Fatal(err)
	}
	events := eventtest.Sorted(rec.Events(t))
	live := []uuid.UUID{live1, live2}
	slices.SortFunc(live, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	wantLines := []string{
		eventtest.Line(event.Issue, event.Remove, live[0]),
		eventtest.Line(event.Issue, event.Remove, live[1]),
		eventtest.Line(event.Milestone, event.Remove, m1),
		eventtest.Line(event.Milestone, event.Remove, m2),
	}
	if got := eventtest.Lines(events); !slices.Equal(got, wantLines) {
		t.Fatalf("events =\n%q\nwant\n%q (soft-deleted issue %v and comments send nothing)", got, wantLines, gone)
	}
	for _, c := range events {
		if !slices.Equal(c.ProjectIDs, []uuid.UUID{p.ID}) || c.ActorID != e.users.Owner.ID || len(c.Diff) != 0 {
			t.Errorf("event %s %v = %+v", c.Resource, c.EntityID, c)
		}
		if purged := c.Resource == event.Issue; c.Purged != purged {
			t.Errorf("%s %v purged = %v, want %v", c.Resource, c.EntityID, c.Purged, purged)
		}
	}
}

func TestProjectDeleteRefusedSendsNothing(t *testing.T) {
	e := newEnv(t)
	p := e.create(t, "YLK")
	insertIssue(t, e, p.ID, stateByName(t, e, p.ID, "Todo").ID, 1, false)
	rec := recorded(t, e)
	if err := e.svc.Delete(context.Background(), e.users.Owner, p.ID, "WEB"); err == nil {
		t.Fatal("delete with a wrong key succeeded")
	}
	if n := len(rec.Events(t)); n != 0 {
		t.Fatalf("%d events after a refused delete", n)
	}
}

func TestStateDeleteEvents(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.create(t, "YLK")
	review, err := e.svc.CreateState(ctx, e.users.Admin, p.ID, project.StateInput{Name: "Review", Type: project.Started, Color: "#ff0000"})
	if err != nil {
		t.Fatal(err)
	}
	progress := stateByName(t, e, p.ID, "In Progress")
	i1 := insertIssue(t, e, p.ID, review.ID, 1, false)
	i2 := insertIssue(t, e, p.ID, review.ID, 2, false)
	insertIssue(t, e, p.ID, review.ID, 3, true)
	prev := map[uuid.UUID]time.Time{}
	for _, id := range []uuid.UUID{i1, i2} {
		var at time.Time
		if err := e.pool.QueryRow(ctx, `SELECT updated_at FROM issues WHERE id = $1`, id).Scan(&at); err != nil {
			t.Fatal(err)
		}
		prev[id] = at
	}
	rec := recorded(t, e)

	if err := e.svc.DeleteState(ctx, e.users.Admin, review.ID, &progress.ID); err != nil {
		t.Fatal(err)
	}
	events := eventtest.Sorted(rec.Events(t))
	ids := []uuid.UUID{i1, i2}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	want := []string{eventtest.Line(event.Issue, event.Update, ids[0]), eventtest.Line(event.Issue, event.Update, ids[1])}
	if got := eventtest.Lines(events); !slices.Equal(got, want) {
		t.Fatalf("events = %q, want %q (the deleted issue sends nothing)", got, want)
	}
	for _, c := range events {
		if got := eventtest.DiffKeys(c); !slices.Equal(got, []string{"stateId"}) {
			t.Errorf("diff keys = %v, want [stateId]", got)
		}
		if c.Diff["stateId"].From != review.ID.String() || c.Diff["stateId"].To != progress.ID.String() {
			t.Errorf("stateId change = %+v", c.Diff["stateId"])
		}
		if !slices.Equal(c.ProjectIDs, []uuid.UUID{p.ID}) || !c.PrevUpdatedAt.Equal(prev[c.EntityID]) || c.ActorID != e.users.Admin.ID {
			t.Errorf("event %v = %+v, want prev %v", c.EntityID, c, prev[c.EntityID])
		}
	}
}

func TestStateAndProjectChangesSendNothingElse(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	rec := recorded(t, e)
	p := e.create(t, "YLK")
	st, err := e.svc.CreateState(ctx, e.users.Admin, p.ID, project.StateInput{Name: "Review", Type: project.Started, Color: "#ff0000"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.UpdateState(ctx, e.users.Admin, st.ID, project.StatePatch{Name: ptr("QA")}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Update(ctx, e.users.Admin, p.ID, project.UpdateInput{Name: ptr("Renamed")}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SetArchived(ctx, e.users.Admin, p.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := rec.Events(t); len(got) != 0 {
		t.Fatalf("events = %v, want none", eventtest.Lines(got))
	}
}
