package project_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/project"
)

func stateByName(t *testing.T, e env, projectID uuid.UUID, name string) project.State {
	t.Helper()
	states, err := e.svc.States(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no state %q", name)
	return project.State{}
}

func TestCreateState(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.create(t, "YLK")
	s, err := e.svc.CreateState(ctx, e.users.Admin, p.ID, project.StateInput{Name: " Review ", Type: project.Started, Color: "#FF0000"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "Review" || s.Position != 5 || s.Type != project.Started || s.Color != "#ff0000" || s.IsDefault || s.ProjectID != p.ID {
		t.Fatalf("state = %+v", s)
	}
	_, err = e.svc.CreateState(ctx, e.users.Admin, p.ID, project.StateInput{Name: "REVIEW", Type: project.Started, Color: "#ff0000"})
	wantInput(t, err, "name", kit.ReasonTaken)
	_, err = e.svc.CreateState(ctx, e.users.Admin, p.ID, project.StateInput{Name: "QA", Type: "done", Color: "#ff0000"})
	wantInput(t, err, "type", kit.ReasonInvalidFormat)
	_, err = e.svc.CreateState(ctx, e.users.Admin, p.ID, project.StateInput{Name: "QA", Type: project.Started, Color: ""})
	wantInput(t, err, "color", kit.ReasonRequired)
	_, err = e.svc.CreateState(ctx, e.users.Admin, p.ID, project.StateInput{Name: strings.Repeat("n", 51), Type: project.Started, Color: "#ff0000"})
	wantInput(t, err, "name", kit.ReasonTooLong)

	for i := 6; i < 50; i++ {
		if _, err := e.svc.CreateState(ctx, e.users.Admin, p.ID, project.StateInput{Name: fmt.Sprintf("S%d", i), Type: project.Started, Color: "#ff0000"}); err != nil {
			t.Fatalf("state %d: %v", i, err)
		}
	}
	_, err = e.svc.CreateState(ctx, e.users.Admin, p.ID, project.StateInput{Name: "S50", Type: project.Started, Color: "#ff0000"})
	if ie := asInput(t, err, "projectId", kit.ReasonTooMany); ie.Msg != "at most 50 states" || ie.Params["max"] != 50 {
		t.Fatalf("51st: %+v", ie)
	}
	_, err = e.svc.CreateState(ctx, e.users.Admin, uuid.New(), project.StateInput{Name: "X", Type: project.Started, Color: "#ff0000"})
	wantNotFound(t, err)
}

func TestUpdateState(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.create(t, "YLK")
	todo := stateByName(t, e, p.ID, "Todo")
	s, err := e.svc.UpdateState(ctx, e.users.Admin, todo.ID, project.StatePatch{Name: ptr("To do"), Color: ptr("#123456")})
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "To do" || s.Color != "#123456" || s.Type != project.Unstarted || s.Position != 1 || !s.IsDefault {
		t.Fatalf("state = %+v", s)
	}
	h := e.history(t, todo.ID)
	if len(h) != 1 || h[0].Action != "update" || changeFields(h[0]) != "color,name" {
		t.Fatalf("history = %+v", h)
	}
	_, err = e.svc.UpdateState(ctx, e.users.Admin, todo.ID, project.StatePatch{Name: ptr("done")})
	wantInput(t, err, "name", kit.ReasonTaken)
	_, err = e.svc.UpdateState(ctx, e.users.Admin, uuid.New(), project.StatePatch{Name: ptr("x")})
	wantNotFound(t, err)
}

func TestChangeDefault(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.create(t, "YLK")
	inProgress := stateByName(t, e, p.ID, "In Progress")
	_, err := e.svc.UpdateState(ctx, e.users.Admin, inProgress.ID, project.StatePatch{IsDefault: ptr(true)})
	wantInput(t, err, "isDefault", kit.ReasonNotAllowed)

	backlog := stateByName(t, e, p.ID, "Backlog")
	s, err := e.svc.UpdateState(ctx, e.users.Admin, backlog.ID, project.StatePatch{IsDefault: ptr(true)})
	if err != nil || !s.IsDefault {
		t.Fatalf("backlog default: %+v, %v", s, err)
	}
	todo := stateByName(t, e, p.ID, "Todo")
	if todo.IsDefault {
		t.Fatal("Todo still default")
	}
	if h := e.history(t, todo.ID); len(h) != 1 || changeFields(h[0]) != "isDefault" {
		t.Fatalf("old default history = %+v", h)
	}
	_, err = e.svc.UpdateState(ctx, e.users.Admin, todo.ID, project.StatePatch{IsDefault: ptr(false)})
	if ie := asInput(t, err, "isDefault", kit.ReasonNotAllowed); ie.Msg != "choose another default state instead" {
		t.Fatalf("msg = %q", ie.Msg)
	}
}

func TestReorderStates(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.create(t, "YLK")
	states, err := e.svc.States(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]uuid.UUID, len(states))
	for i, s := range states {
		ids[len(states)-1-i] = s.ID
	}
	got, err := e.svc.ReorderStates(ctx, e.users.Admin, p.ID, ids)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for i, s := range got {
		if s.Position != i {
			t.Fatalf("position %d = %d", i, s.Position)
		}
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "Canceled,Done,In Progress,Todo,Backlog" {
		t.Fatalf("order = %v", names)
	}
	_, err = e.svc.ReorderStates(ctx, e.users.Admin, p.ID, ids[:4])
	wantInput(t, err, "stateIds", kit.ReasonMismatch)
	dup := append([]uuid.UUID{ids[0]}, ids[:4]...)
	_, err = e.svc.ReorderStates(ctx, e.users.Admin, p.ID, dup)
	wantInput(t, err, "stateIds", kit.ReasonMismatch)
}

// insertIssue adds an issue with raw SQL (the issue service comes later).
func insertIssue(t *testing.T, e env, projectID, stateID uuid.UUID, number int, deleted bool) uuid.UUID {
	t.Helper()
	var deletedAt *time.Time
	if deleted {
		now := time.Now()
		deletedAt = &now
	}
	var id uuid.UUID
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO issues
		(project_id, number, title, state_id, creator_id, sort_order, created_at, updated_at, deleted_at)
		VALUES ($1, $2, 't', $3, $4, 0, now(), now(), $5) RETURNING id`,
		projectID, number, stateID, e.users.Member.ID, deletedAt).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestDeleteStateRules(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.create(t, "YLK")
	todo := stateByName(t, e, p.ID, "Todo")
	err := e.svc.DeleteState(ctx, e.users.Admin, todo.ID, nil)
	wantInput(t, err, "id", kit.ReasonNotAllowed)

	done := stateByName(t, e, p.ID, "Done")
	err = e.svc.DeleteState(ctx, e.users.Admin, done.ID, nil)
	if ie := asInput(t, err, "id", kit.ReasonNotAllowed); !strings.Contains(ie.Msg, "last state of its type") {
		t.Fatalf("msg = %q", ie.Msg)
	}

	review, err := e.svc.CreateState(ctx, e.users.Admin, p.ID, project.StateInput{Name: "Review", Type: project.Started, Color: "#ff0000"})
	if err != nil {
		t.Fatal(err)
	}
	insertIssue(t, e, p.ID, review.ID, 1, false)
	err = e.svc.DeleteState(ctx, e.users.Admin, review.ID, nil)
	wantInput(t, err, "moveIssuesTo", kit.ReasonRequired)
	err = e.svc.DeleteState(ctx, e.users.Admin, review.ID, &review.ID)
	wantInput(t, err, "moveIssuesTo", kit.ReasonNotAllowed)
	other := e.create(t, "WEB")
	otherTodo := stateByName(t, e, other.ID, "Todo")
	err = e.svc.DeleteState(ctx, e.users.Admin, review.ID, &otherTodo.ID)
	wantInput(t, err, "moveIssuesTo", kit.ReasonWrongProject)
	missing := uuid.New()
	err = e.svc.DeleteState(ctx, e.users.Admin, review.ID, &missing)
	wantInput(t, err, "moveIssuesTo", kit.ReasonNotFound)
	err = e.svc.DeleteState(ctx, e.users.Admin, uuid.New(), nil)
	wantNotFound(t, err)
}

func TestDeleteStateMovesIssues(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.create(t, "YLK")
	review, err := e.svc.CreateState(ctx, e.users.Admin, p.ID, project.StateInput{Name: "Review", Type: project.Started, Color: "#ff0000"})
	if err != nil {
		t.Fatal(err)
	}
	live := insertIssue(t, e, p.ID, review.ID, 1, false)
	gone := insertIssue(t, e, p.ID, review.ID, 2, true)
	done := stateByName(t, e, p.ID, "Done")
	before := e.clock()
	if err := e.svc.DeleteState(ctx, e.users.Admin, review.ID, &done.ID); err != nil {
		t.Fatal(err)
	}
	want := before.Add(time.Second)
	var stateID uuid.UUID
	var completed *time.Time
	if err := e.pool.QueryRow(ctx, `SELECT state_id, completed_at FROM issues WHERE id = $1`, live).Scan(&stateID, &completed); err != nil {
		t.Fatal(err)
	}
	if stateID != done.ID || completed == nil || !completed.Equal(want) {
		t.Fatalf("issue state %v completed %v, want %v at %v", stateID, completed, done.ID, want)
	}
	if err := e.pool.QueryRow(ctx, `SELECT state_id FROM issues WHERE id = $1`, gone).Scan(&stateID); err != nil || stateID != done.ID {
		t.Fatalf("deleted issue state = %v, %v", stateID, err)
	}
	h := e.history(t, live)
	if len(h) != 1 || h[0].Action != "update" || changeFields(h[0]) != "completedAt,stateId" {
		t.Fatalf("issue history = %+v", h)
	}
	if n := len(e.history(t, gone)); n != 0 {
		t.Fatalf("deleted issue got %d events", n)
	}
	if h := e.history(t, review.ID); len(h) != 2 || h[1].Action != "delete" {
		t.Fatalf("state history = %+v", h)
	}
	if _, err := e.svc.UpdateState(ctx, e.users.Admin, review.ID, project.StatePatch{Name: ptr("x")}); !errors.Is(err, kit.ErrNotFound) {
		t.Fatalf("deleted state: %v", err)
	}
}

func TestCompletionTimes(t *testing.T) {
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		prev, next             project.StateType
		prevDone, prevCanceled *time.Time
		wantDone, wantCanceled *time.Time
	}{
		{project.Started, project.Completed, nil, nil, &now, nil},
		{project.Completed, project.Completed, &old, nil, &old, nil},
		{project.Completed, project.Canceled, &old, nil, nil, &now},
		{project.Canceled, project.Backlog, nil, &old, nil, nil},
		{"", project.Canceled, nil, nil, nil, &now},
	}
	eq := func(a, b *time.Time) bool { return (a == nil && b == nil) || (a != nil && b != nil && a.Equal(*b)) }
	for _, c := range cases {
		d, x := project.CompletionTimes(c.prev, c.next, c.prevDone, c.prevCanceled, now)
		if !eq(d, c.wantDone) || !eq(x, c.wantCanceled) {
			t.Errorf("%s→%s: got %v %v, want %v %v", c.prev, c.next, d, x, c.wantDone, c.wantCanceled)
		}
	}
}

func TestStatesMemberForbidden(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.create(t, "YLK")
	todo := stateByName(t, e, p.ID, "Todo")
	m := e.users.Member
	errs := []error{
		func() error {
			_, err := e.svc.CreateState(ctx, m, p.ID, project.StateInput{Name: "X", Type: project.Started, Color: "#ff0000"})
			return err
		}(),
		func() error {
			_, err := e.svc.UpdateState(ctx, m, todo.ID, project.StatePatch{Name: ptr("x")})
			return err
		}(),
		func() error { _, err := e.svc.ReorderStates(ctx, m, p.ID, []uuid.UUID{todo.ID}); return err }(),
		e.svc.DeleteState(ctx, m, todo.ID, nil),
	}
	for i, err := range errs {
		if !errors.Is(err, kit.ErrForbidden) {
			t.Errorf("op %d: err = %v, want forbidden", i, err)
		}
	}
}

func TestStatesOfArchivedProject(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.create(t, "YLK")
	todo := stateByName(t, e, p.ID, "Todo")
	if _, err := e.svc.SetArchived(ctx, e.users.Admin, p.ID, true); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.CreateState(ctx, e.users.Admin, p.ID, project.StateInput{Name: "X", Type: project.Started, Color: "#ff0000"})
	wantInput(t, err, "projectId", kit.ReasonArchived)
	_, err = e.svc.UpdateState(ctx, e.users.Admin, todo.ID, project.StatePatch{Name: ptr("x")})
	wantInput(t, err, "projectId", kit.ReasonArchived)
	got, err := e.svc.StatesByIDs(ctx, []uuid.UUID{todo.ID, uuid.New()})
	if err != nil || len(got) != 1 || got[0].ID != todo.ID {
		t.Fatalf("StatesByIDs = %+v, %v", got, err)
	}
}
