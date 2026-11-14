package audit_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/page"
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

// stmtCounter is a pgx tracer that counts SQL statements.
type stmtCounter struct{ n atomic.Int64 }

func (c *stmtCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (c *stmtCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// Record reads the names of every kind in one statement, however many ids
// each kind has, and none when the changes name nothing.
func TestRecordNamesOneStatement(t *testing.T) {
	base, users, clock := testfx.New(t)
	ctx := context.Background()
	cfg := base.Config()
	counter := &stmtCounter{}
	cfg.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

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
				before := counter.n.Load()
				defer func() { used = counter.n.Load() - before }()
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
