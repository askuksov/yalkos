package audit_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/store/q"
	"github.com/askuksov/yalkos/internal/testfx"
)

func ptr[T any](v T) *T { return &v }

func TestDiffOnlyChanged(t *testing.T) {
	id := uuid.New()
	d := audit.Diff(
		map[string]any{"a": 1, "b": "x", "n": nil, "u": id},
		map[string]any{"a": 2, "b": "x", "n": nil, "u": id},
	)
	if len(d) != 1 {
		t.Fatalf("diff = %v, want only a", d)
	}
	if c := d["a"]; c.From != 1 || c.To != 2 {
		t.Fatalf("a = %+v", c)
	}
}

func TestDiffCreate(t *testing.T) {
	d := audit.Diff(nil, map[string]any{"a": 1, "b": nil})
	if len(d) != 2 || d["a"].From != nil || d["a"].To != 1 {
		t.Fatalf("diff = %v", d)
	}
	if c, ok := d["b"]; !ok || c.From != nil || c.To != nil {
		t.Fatalf("b = %+v %v", c, ok)
	}
}

func record(t *testing.T, pool *pgxpool.Pool, e audit.Event) {
	t.Helper()
	ctx := context.Background()
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return audit.Record(ctx, q.New(pool).WithTx(tx), e)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRecordAndList(t *testing.T) {
	pool, users, clock := testfx.New(t)
	ctx := context.Background()
	entity := uuid.New()
	for i := range 3 {
		record(t, pool, audit.Event{
			EntityType: "issue", EntityID: entity, ActorID: &users.Member.ID, Action: "update",
			Changes: map[string]audit.Change{"title": {From: "a", To: "b"}, "priority": {From: i, To: i + 1}},
			At:      clock(),
		})
	}
	svc := audit.New(pool)
	p1, err := svc.List(ctx, entity, page.Args{First: ptr(2)})
	if err != nil {
		t.Fatal(err)
	}
	if len(p1.Items) != 2 || !p1.Info.HasNext || p1.Info.HasPrev {
		t.Fatalf("page 1 = %d items, info %+v", len(p1.Items), p1.Info)
	}
	e := p1.Items[0]
	if e.Action != "update" || *e.ActorID != users.Member.ID || !e.At.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("entry = %+v", e)
	}
	if len(e.Changes) != 2 || e.Changes[0].Field != "priority" || e.Changes[1].Field != "title" {
		t.Fatalf("changes not sorted: %+v", e.Changes)
	}
	if string(e.Changes[0].From) != "0" || string(e.Changes[0].To) != "1" || string(e.Changes[1].To) != `"b"` {
		t.Fatalf("changes = %s→%s, %s", e.Changes[0].From, e.Changes[0].To, e.Changes[1].To)
	}
	p2, err := svc.List(ctx, entity, page.Args{First: ptr(2), After: p1.Info.End})
	if err != nil {
		t.Fatal(err)
	}
	if len(p2.Items) != 1 || p2.Info.HasNext || !p2.Info.HasPrev || string(p2.Items[0].Changes[0].To) != "3" {
		t.Fatalf("page 2 = %+v", p2)
	}
	// Backward from the end gives the two newest in natural order.
	p3, err := svc.List(ctx, entity, page.Args{Last: ptr(2)})
	if err != nil {
		t.Fatal(err)
	}
	if len(p3.Items) != 2 || string(p3.Items[1].Changes[0].To) != "3" || !p3.Info.HasPrev {
		t.Fatalf("last 2 = %+v", p3.Info)
	}
}

func TestRecordRollsBackWithTx(t *testing.T) {
	pool, users, clock := testfx.New(t)
	ctx := context.Background()
	entity := uuid.New()
	boom := errors.New("boom")
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := audit.Record(ctx, q.New(pool).WithTx(tx), audit.Event{
			EntityType: "issue", EntityID: entity, ActorID: &users.Owner.ID, Action: "create",
			Changes: audit.Diff(nil, map[string]any{"title": "x"}), At: clock(),
		}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	r, err := audit.New(pool).List(ctx, entity, page.Args{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Items) != 0 {
		t.Fatalf("events after rollback: %d", len(r.Items))
	}
}

func TestListRejectsForeignCursor(t *testing.T) {
	pool, _, _ := testfx.New(t)
	c := page.Encode(page.Cursor{F: "priority", D: "asc", V: json.RawMessage(`1`), ID: uuid.New()})
	_, err := audit.New(pool).List(context.Background(), uuid.New(), page.Args{After: &c})
	if err == nil {
		t.Fatal("foreign cursor accepted")
	}
}

func TestByIDs(t *testing.T) {
	pool, users, clock := testfx.New(t)
	ctx := context.Background()
	entity := uuid.New()
	for range 2 {
		record(t, pool, audit.Event{EntityType: "issue", EntityID: entity, ActorID: &users.Member.ID, Action: "update",
			Changes: map[string]audit.Change{"title": {From: "a", To: "b"}}, At: clock()})
	}
	svc := audit.New(pool)
	all, err := svc.List(ctx, entity, page.Args{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.ByIDs(ctx, []uuid.UUID{all.Items[1].ID, uuid.New(), all.Items[0].ID})
	if err != nil || len(got) != 2 || got[0].ID != all.Items[1].ID || got[1].ID != all.Items[0].ID || len(got[0].Changes) != 1 {
		t.Fatalf("ByIDs = %+v, %v", got, err)
	}
}

// Record reads the names of every kind in one statement, however many ids
// each kind has, and none when the changes name nothing.
func TestRecordNamesOneStatement(t *testing.T) {
	base, users, clock := testfx.New(t)
	ctx := context.Background()
	counter := &testfx.StmtCounter{}
	pool := testfx.TracedPool(t, base, counter)

	ids := func(n int) []uuid.UUID {
		out := make([]uuid.UUID, n)
		for i := range out {
			out[i] = uuid.New()
		}
		return out
	}
	statements := func(changes map[string]audit.Change) int64 {
		var least int64 = -1
		for range 3 {
			var used int64
			err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
				before := counter.N()
				defer func() { used = counter.N() - before }()
				return audit.Record(ctx, q.New(pool).WithTx(tx), audit.Event{
					EntityType: "issue", EntityID: uuid.New(), ActorID: &users.Member.ID, Action: "update", Changes: changes, At: clock(),
				})
			})
			if err != nil {
				t.Fatal(err)
			}
			if least < 0 || used < least {
				least = used
			}
		}
		return least
	}
	named := func(n int) map[string]audit.Change {
		return map[string]audit.Change{
			"labelIds":    {From: ids(n), To: ids(n)},
			"milestoneId": {From: ids(1)[0], To: ids(1)[0]},
			"stateId":     {From: ids(1)[0], To: ids(1)[0]},
			"projectId":   {From: ids(1)[0], To: ids(1)[0]},
		}
	}
	two, ten := statements(named(2)), statements(named(10))
	// One names statement and one insert.
	if two != 2 || ten != 2 {
		t.Errorf("statements per Record = %d with 2 labels, %d with 10; want 2 and 2", two, ten)
	}
	if none := statements(map[string]audit.Change{"title": {From: "a", To: "b"}}); none != 1 {
		t.Errorf("statements per Record naming nothing = %d, want 1", none)
	}
}

// RecordMany stores what Record stores for each event: the same changes, the
// names of the ids that event's own changes refer to (not the union of all
// events), the event time, and the order of events that share an entity and a
// time.
func TestRecordManyMatchesRecord(t *testing.T) {
	pool, users, clock := testfx.New(t)
	ctx := context.Background()
	projects := project.New(pool, clock)
	labels := label.New(pool, clock)
	milestones := milestone.New(pool, projects, clock)
	p1, err := projects.Create(ctx, users.Admin, project.CreateInput{Key: "ONE", Name: "One"})
	if err != nil {
		t.Fatal(err)
	}
	p2, err := projects.Create(ctx, users.Admin, project.CreateInput{Key: "TWO", Name: "Two"})
	if err != nil {
		t.Fatal(err)
	}
	states, err := projects.States(ctx, p1.ID)
	if err != nil {
		t.Fatal(err)
	}
	lab := func(name string) uuid.UUID {
		l, err := labels.Create(ctx, users.Member, label.CreateInput{Name: name, Color: "#AABBCC"})
		if err != nil {
			t.Fatal(err)
		}
		return l.ID
	}
	l1, l2 := lab("Perf"), lab("Sec")
	m, err := milestones.Create(ctx, users.Admin, p1.ID, milestone.CreateInput{Name: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	gone := uuid.New()
	at := clock()

	type spec struct {
		slot    int // events with equal slot share an entity
		at      time.Time
		changes map[string]audit.Change
	}
	specs := []spec{
		{0, at, map[string]audit.Change{"labelIds": {From: []uuid.UUID{l1}, To: []uuid.UUID{l1, l2}}}},
		{1, at.Add(time.Second), map[string]audit.Change{"stateId": {From: states[0].ID, To: states[1].ID}}},
		{2, at, map[string]audit.Change{"projectId": {From: p1.ID, To: p2.ID}, "title": {From: "a", To: "b"}}},
		{3, at, map[string]audit.Change{"milestoneId": {From: nil, To: m.ID}, "labelIds": {From: []uuid.UUID{l2}, To: []uuid.UUID{}}}},
		{4, at, map[string]audit.Change{"title": {From: "a", To: "b"}}},
		{5, at, map[string]audit.Change{"labelIds": {From: []uuid.UUID{gone, l1}, To: []uuid.UUID{l1}}, "stateId": {From: gone, To: states[0].ID}}},
		{0, at, map[string]audit.Change{"labelIds": {From: []uuid.UUID{l1, l2}, To: []uuid.UUID{l2}}}},
	}
	build := func() ([]audit.Event, []uuid.UUID) {
		entities := make([]uuid.UUID, 6)
		for i := range entities {
			entities[i] = uuid.New()
		}
		events := make([]audit.Event, len(specs))
		for i, s := range specs {
			events[i] = audit.Event{
				EntityType: "issue", Action: "update", EntityID: entities[s.slot], ProjectID: &p1.ID,
				ActorID: &users.Member.ID, Changes: s.changes, At: s.at,
			}
		}
		return events, entities
	}
	control, controlEntities := build()
	batch, batchEntities := build()
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		for _, e := range control {
			if err := audit.Record(ctx, q.New(pool).WithTx(tx), e); err != nil {
				return err
			}
		}
		return audit.RecordMany(ctx, q.New(pool).WithTx(tx), batch)
	}); err != nil {
		t.Fatal(err)
	}
	svc := audit.New(pool)
	for slot := range controlEntities {
		want, err := svc.List(ctx, controlEntities[slot], page.Args{})
		if err != nil {
			t.Fatal(err)
		}
		got, err := svc.List(ctx, batchEntities[slot], page.Args{})
		if err != nil {
			t.Fatal(err)
		}
		if len(want.Items) == 0 || len(got.Items) != len(want.Items) {
			t.Fatalf("slot %d: %d entries, want %d", slot, len(got.Items), len(want.Items))
		}
		for i := range want.Items {
			w, g := want.Items[i], got.Items[i]
			w.ID, g.ID = uuid.Nil, uuid.Nil
			if !reflect.DeepEqual(w, g) {
				t.Errorf("slot %d entry %d:\n got  %+v\n want %+v", slot, i, g, w)
			}
		}
	}
	if err := audit.RecordMany(ctx, q.New(pool), nil); err != nil {
		t.Errorf("RecordMany of nothing: %v", err)
	}
}

// Events without a project or an actor store NULL in both columns, whether
// they are recorded alone or in a batch next to events that have both.
func TestRecordManyStoresNullProjectAndActor(t *testing.T) {
	pool, users, clock := testfx.New(t)
	ctx := context.Background()
	projects := project.New(pool, clock)
	p, err := projects.Create(ctx, users.Admin, project.CreateInput{Key: "ONE", Name: "One"})
	if err != nil {
		t.Fatal(err)
	}
	at := clock()
	event := func(project, actor *uuid.UUID) audit.Event {
		return audit.Event{
			EntityType: "issue", Action: "update", EntityID: uuid.New(), ProjectID: project, ActorID: actor,
			Changes: map[string]audit.Change{"title": {From: "a", To: "b"}}, At: at,
		}
	}
	control := event(nil, nil)
	bare, onlyProject, onlyActor, full := event(nil, nil), event(&p.ID, nil), event(nil, &users.Member.ID), event(&p.ID, &users.Member.ID)
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := audit.Record(ctx, q.New(pool).WithTx(tx), control); err != nil {
			return err
		}
		return audit.RecordMany(ctx, q.New(pool).WithTx(tx), []audit.Event{bare, onlyProject, onlyActor, full})
	}); err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct {
		e                      audit.Event
		nullProject, nullActor bool
	}{
		"Record without both":   {control, true, true},
		"batch without both":    {bare, true, true},
		"batch without actor":   {onlyProject, false, true},
		"batch without project": {onlyActor, true, false},
		"batch with both":       {full, false, false},
	} {
		var nullProject, nullActor bool
		if err := pool.QueryRow(ctx, `SELECT project_id IS NULL, actor_id IS NULL FROM audit_events WHERE entity_id = $1`,
			tc.e.EntityID).Scan(&nullProject, &nullActor); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if nullProject != tc.nullProject || nullActor != tc.nullActor {
			t.Errorf("%s: project_id NULL %v, actor_id NULL %v, want %v, %v",
				name, nullProject, nullActor, tc.nullProject, tc.nullActor)
		}
	}
}

// The names lookup asks for each id once, however many events and fields
// repeat it.
func TestRecordManyLooksUpEachIDOnce(t *testing.T) {
	l1, l2, st, p := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ev := func(changes map[string]audit.Change) audit.Event {
		return audit.Event{EntityType: "issue", EntityID: uuid.New(), Action: "update", Changes: changes}
	}
	got, err := audit.LookupIDs([]audit.Event{
		ev(map[string]audit.Change{"labelIds": {From: []uuid.UUID{l1}, To: []uuid.UUID{l1, l2}}, "stateId": {From: st, To: st}}),
		ev(map[string]audit.Change{"labelIds": {From: nil, To: []uuid.UUID{l2, l1}}, "projectId": {From: p, To: p}}),
		ev(map[string]audit.Change{"stateId": {From: nil, To: st}, "projectId": {From: nil, To: p}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := [4][]uuid.UUID{{l1, l2}, nil, {st}, {p}}
	for i := range want {
		if !slices.Equal(got[i], want[i]) {
			t.Errorf("kind %d: ids %v, want %v", i, got[i], want[i])
		}
	}
}
