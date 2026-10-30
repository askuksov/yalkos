package audit_test

import (
	"context"
	"encoding/json"
	"errors"
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
