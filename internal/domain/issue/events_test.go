package issue_test

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
	"github.com/askuksov/yalkos/internal/domain/issue"
)

// recorded connects a recorder to the issue service; fixtures built before
// the call emit nothing.
func recorded(t *testing.T, e env) *eventtest.Recorder {
	t.Helper()
	rec := eventtest.NewRecorder(t, e.pool)
	e.issues.SetEmitter(rec)
	return rec
}

func wantLines(t *testing.T, rec *eventtest.Recorder, want ...string) []event.Change {
	t.Helper()
	got := eventtest.Sorted(rec.Events(t))
	if lines := eventtest.Lines(got); !slices.Equal(lines, want) {
		t.Fatalf("events =\n%q\nwant\n%q", lines, want)
	}
	return got
}

func wantKeys(t *testing.T, c event.Change, keys ...string) {
	t.Helper()
	if got := eventtest.DiffKeys(c); !slices.Equal(got, keys) {
		t.Errorf("diff keys of %s %s = %v, want %v", c.Resource, c.Action, got, keys)
	}
}

func wantProjects(t *testing.T, c event.Change, ids ...uuid.UUID) {
	t.Helper()
	if !slices.Equal(c.ProjectIDs, ids) {
		t.Errorf("project ids of %s %s = %v, want %v", c.Resource, c.Action, c.ProjectIDs, ids)
	}
}

func TestIssueCreateEvents(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	parent := e.create(t, "parent")
	rec := recorded(t, e)

	i := e.create(t, "one")
	sub, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ParentID: &parent.ID, Title: "sub"})
	if err != nil {
		t.Fatal(err)
	}
	got := eventtest.Lines(rec.Events(t))
	want := []string{eventtest.Line(event.Issue, event.Create, i.ID), eventtest.Line(event.Issue, event.Create, sub.ID)}
	if !slices.Equal(got, want) {
		t.Fatalf("events = %q, want %q", got, want)
	}
	for n, c := range rec.Events(t) {
		wantProjects(t, c, e.ylk.ID)
		wantKeys(t, c)
		if c.ActorID != e.users.Member.ID || !c.At.Equal([]issue.Issue{i, sub}[n].CreatedAt) || c.Purged {
			t.Errorf("event %d = %+v", n, c)
		}
	}
}

func TestIssueUpdateEvents(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	i := e.create(t, "one")
	done := e.state(t, e.ylk.ID, "Done")
	rec := recorded(t, e)

	upd, err := e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{StateID: &done.ID})
	if err != nil {
		t.Fatal(err)
	}
	got := wantLines(t, rec, eventtest.Line(event.Issue, event.Update, i.ID))
	c := got[0]
	wantKeys(t, c, "completedAt", "stateId")
	wantProjects(t, c, e.ylk.ID)
	if !c.PrevUpdatedAt.Equal(i.UpdatedAt) || !c.At.Equal(upd.UpdatedAt) || c.ActorID != e.users.Member.ID {
		t.Errorf("prev %v at %v actor %v, want prev %v at %v", c.PrevUpdatedAt, c.At, c.ActorID, i.UpdatedAt, upd.UpdatedAt)
	}
	if st := c.Diff["stateId"]; st.To != done.ID.String() {
		t.Errorf("stateId change = %+v, want to %v", st, done.ID)
	}

	// Nothing changed: no event.
	if _, err := e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{StateID: &done.ID, Title: ptr("one")}); err != nil {
		t.Fatal(err)
	}
	wantLines(t, rec, eventtest.Line(event.Issue, event.Update, i.ID))
}

func TestIssueMoveEvents(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	web := e.project(t, "WEB")
	e.createIn(t, web.ID, "taken WEB-1") // the moved issues get other numbers than they had
	parent := e.create(t, "parent")
	c1, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ParentID: &parent.ID, Title: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	c2, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ParentID: &parent.ID, Title: "c2"})
	if err != nil {
		t.Fatal(err)
	}
	before := map[uuid.UUID]time.Time{}
	for _, x := range []issue.Issue{parent, c1, c2} {
		cur, err := e.issues.Get(ctx, x.ID.String())
		if err != nil {
			t.Fatal(err)
		}
		before[x.ID] = cur.UpdatedAt
	}
	rec := recorded(t, e)

	if _, err := e.issues.Move(ctx, e.users.Member, parent.ID, web.ID, nil); err != nil {
		t.Fatal(err)
	}
	events := rec.Events(t)
	lines := eventtest.Lines(events)
	if len(lines) != 3 || lines[0] != eventtest.Line(event.Issue, event.Update, parent.ID) {
		t.Fatalf("events = %q, want the parent first, then its children", lines)
	}
	rest := []string{lines[1], lines[2]}
	slices.Sort(rest)
	wantRest := []string{eventtest.Line(event.Issue, event.Update, c1.ID), eventtest.Line(event.Issue, event.Update, c2.ID)}
	slices.Sort(wantRest)
	if !slices.Equal(rest, wantRest) {
		t.Fatalf("moved descendants = %q, want %q", rest, wantRest)
	}
	for _, c := range events {
		wantProjects(t, c, e.ylk.ID, web.ID)
		wantKeys(t, c, "number", "projectId", "stateId")
		if !c.PrevUpdatedAt.Equal(before[c.EntityID]) {
			t.Errorf("prev updated at of %v = %v, want %v", c.EntityID, c.PrevUpdatedAt, before[c.EntityID])
		}
		if c.Diff["projectId"].To != web.ID.String() || c.Diff["projectId"].From != e.ylk.ID.String() {
			t.Errorf("projectId change = %+v", c.Diff["projectId"])
		}
	}
}

func TestIssueDeleteEvents(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	parent := e.create(t, "parent")
	c1, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ParentID: &parent.ID, Title: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	c2, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ParentID: &parent.ID, Title: "c2"})
	if err != nil {
		t.Fatal(err)
	}
	rec := recorded(t, e)
	rec.Probe = func(ctx context.Context, tx pgx.Tx, _ event.Change) error {
		// The issue is soft-deleted and its children detached before any event.
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM issues WHERE id = $1 AND deleted_at IS NOT NULL`, parent.ID).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return errors.New("the parent is not soft-deleted when an event is emitted")
		}
		return nil
	}

	if err := e.issues.Delete(ctx, e.users.Member, parent.ID); err != nil {
		t.Fatal(err)
	}
	got := wantLines(t, rec,
		eventtest.Line(event.Issue, event.Update, c1.ID),
		eventtest.Line(event.Issue, event.Update, c2.ID),
		eventtest.Line(event.Issue, event.Remove, parent.ID))
	for _, c := range got[:2] {
		wantKeys(t, c, "parentId")
		wantProjects(t, c, e.ylk.ID)
		if c.Diff["parentId"].From != parent.ID.String() || c.Diff["parentId"].To != nil {
			t.Errorf("parentId change = %+v", c.Diff["parentId"])
		}
	}
	for n, child := range []issue.Issue{c1, c2} {
		if !got[n].PrevUpdatedAt.Equal(child.UpdatedAt) {
			t.Errorf("prev updated at of child %d = %v, want %v", n, got[n].PrevUpdatedAt, child.UpdatedAt)
		}
	}
	wantKeys(t, got[2])
	wantProjects(t, got[2], e.ylk.ID)
	if got[2].Purged {
		t.Error("a soft delete is not purged")
	}
}

func TestEmitFailureRollsBackChange(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	i := e.create(t, "before")
	rec := recorded(t, e)
	boom := errors.New("boom")

	rec.Fail = boom
	if _, err := e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{Title: ptr("after")}); !errors.Is(err, boom) {
		t.Fatalf("Update with a failing emitter: %v, want %v", err, boom)
	}
	if _, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ProjectID: &e.ylk.ID, Title: "new"}); !errors.Is(err, boom) {
		t.Fatalf("Create with a failing emitter: %v, want %v", err, boom)
	}
	got, err := e.issues.Get(ctx, i.ID.String())
	if err != nil || got.Title != "before" || !got.UpdatedAt.Equal(i.UpdatedAt) {
		t.Fatalf("issue after the failed update = %+v, %v", got, err)
	}
	if h := e.history(t, i.ID); len(h) != 1 {
		t.Errorf("history after the failed update has %d entries, want 1", len(h))
	}
	var n int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM issues`).Scan(&n); err != nil || n != 1 {
		t.Errorf("issues = %d, %v; want 1", n, err)
	}
	wantLines(t, rec)

	rec.Fail = nil // positive control
	if _, err := e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{Title: ptr("after")}); err != nil {
		t.Fatal(err)
	}
	wantLines(t, rec, eventtest.Line(event.Issue, event.Update, i.ID))
}
