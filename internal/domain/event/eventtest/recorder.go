// Package eventtest holds an event.Emitter for tests that writes changes in
// the caller's transaction, so the changes of a transaction that rolls back
// or starts over disappear with it.
package eventtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/domain/event"
)

// Recorder stores every emitted change in table test_events.
type Recorder struct {
	pool *pgxpool.Pool
	// Fail, when set, is returned by Emit instead of recording.
	Fail error
	// Probe, when set, runs in the emitting transaction before a change is
	// recorded, to look at the database at that moment; its error fails the
	// change like Fail does.
	Probe func(ctx context.Context, tx pgx.Tx, c event.Change) error
}

// NewRecorder creates the test_events table in pool's database.
func NewRecorder(t *testing.T, pool *pgxpool.Pool) *Recorder {
	t.Helper()
	_, err := pool.Exec(context.Background(), `CREATE TABLE IF NOT EXISTS test_events (
		seq bigserial PRIMARY KEY, resource text NOT NULL, action text NOT NULL, entity_id uuid NOT NULL,
		diff_keys text[] NOT NULL, change jsonb NOT NULL)`)
	if err != nil {
		t.Fatalf("eventtest: create table: %v", err)
	}
	return &Recorder{pool: pool}
}

// Emit implements event.Emitter.
func (r *Recorder) Emit(ctx context.Context, tx pgx.Tx, c event.Change) error {
	if r.Fail != nil {
		return r.Fail
	}
	if r.Probe != nil {
		if err := r.Probe(ctx, tx, c); err != nil {
			return err
		}
	}
	b, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("eventtest: encode change: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO test_events (resource, action, entity_id, diff_keys, change)
		VALUES ($1, $2, $3, $4, $5)`, string(c.Resource), string(c.Action), c.EntityID, DiffKeys(c), b)
	if err != nil {
		return fmt.Errorf("eventtest: record: %w", err)
	}
	return nil
}

// Events returns the committed changes in the order they were emitted.
func (r *Recorder) Events(t *testing.T) []event.Change {
	t.Helper()
	rows, err := r.pool.Query(context.Background(), `SELECT change FROM test_events ORDER BY seq`)
	if err != nil {
		t.Fatalf("eventtest: read: %v", err)
	}
	defer rows.Close()
	var out []event.Change
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			t.Fatalf("eventtest: scan: %v", err)
		}
		var c event.Change
		dec := json.NewDecoder(bytes.NewReader(b))
		if err := dec.Decode(&c); err != nil {
			t.Fatalf("eventtest: decode: %v", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("eventtest: read: %v", err)
	}
	return out
}

// Sorted returns events with every run of the same resource and action ordered
// by entity id, for cascades whose rows come back in no defined order.
func Sorted(events []event.Change) []event.Change {
	out := slices.Clone(events)
	for i := 0; i < len(out); {
		j := i + 1
		for j < len(out) && out[j].Resource == out[i].Resource && out[j].Action == out[i].Action {
			j++
		}
		slices.SortFunc(out[i:j], func(a, b event.Change) int { return slices.Compare(a.EntityID[:], b.EntityID[:]) })
		i = j
	}
	return out
}

// Line renders a change as "resource action entity-id" for exact comparison.
func Line(r event.Resource, a event.Action, id uuid.UUID) string {
	return fmt.Sprintf("%s %s %s", r, a, id)
}

// Lines renders events with Line.
func Lines(events []event.Change) []string {
	out := make([]string, len(events))
	for i, c := range events {
		out[i] = Line(c.Resource, c.Action, c.EntityID)
	}
	return out
}

// DiffKeys returns the sorted field names of c's diff.
func DiffKeys(c event.Change) []string {
	keys := make([]string, 0, len(c.Diff))
	for k := range c.Diff {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
