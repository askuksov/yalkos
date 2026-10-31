// Package audit records and reads the change log of projects, states and
// issues. Events are written inside the caller's transaction.
package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/store/q"
)

// Change is one field's old and new value.
type Change struct {
	From any `json:"from"`
	To   any `json:"to"`
}

// Event is a change to record. EntityType is project|state|issue,
// Action is create|update|delete.
type Event struct {
	EntityType, Action string
	EntityID           uuid.UUID
	ProjectID, ActorID *uuid.UUID
	Changes            map[string]Change
	At                 time.Time
}

// Record writes e using tx (the caller's transaction).
func Record(ctx context.Context, tx *q.Queries, e Event) error {
	changes := e.Changes
	if changes == nil {
		changes = map[string]Change{}
	}
	b, err := json.Marshal(changes)
	if err != nil {
		return fmt.Errorf("audit: encode changes: %w", err)
	}
	if err := tx.InsertAuditEvent(ctx, q.InsertAuditEventParams{
		EntityType: e.EntityType, EntityID: e.EntityID, ProjectID: e.ProjectID, ActorID: e.ActorID,
		Action: e.Action, Changes: b, CreatedAt: e.At,
	}); err != nil {
		return fmt.Errorf("audit: record: %w", err)
	}
	return nil
}

// Diff returns the fields whose JSON encodings differ between before and after.
// A nil before is a create: every field of after with From nil.
func Diff(before, after map[string]any) map[string]Change {
	out := map[string]Change{}
	if before == nil {
		for k, v := range after {
			out[k] = Change{From: nil, To: v}
		}
		return out
	}
	keys := map[string]struct{}{}
	for k := range before {
		keys[k] = struct{}{}
	}
	for k := range after {
		keys[k] = struct{}{}
	}
	for k := range keys {
		a, _ := json.Marshal(before[k])
		b, _ := json.Marshal(after[k])
		if !bytes.Equal(a, b) {
			out[k] = Change{From: before[k], To: after[k]}
		}
	}
	return out
}

// FieldChange is a stored change; From/To are JSON values.
type FieldChange struct {
	Field    string
	From, To json.RawMessage
}

// Entry is one recorded event.
type Entry struct {
	ID      uuid.UUID
	ActorID *uuid.UUID
	Action  string
	Changes []FieldChange
	At      time.Time
}

// Service reads the log.
type Service struct {
	q *q.Queries
}

// New builds the service.
func New(pool *pgxpool.Pool) *Service { return &Service{q: q.New(pool)} }

const cursorField = "createdAt"

// List returns the events of one entity, oldest first.
func (s *Service) List(ctx context.Context, entityID uuid.UUID, a page.Args) (page.Result[Entry], error) {
	w, err := a.Window()
	if err != nil {
		return page.Result[Entry]{}, err
	}
	var at *time.Time
	var id *uuid.UUID
	if w.Cursor != nil {
		field := "after"
		if !w.Forward {
			field = "before"
		}
		c, err := page.Decode(*w.Cursor, field)
		if err != nil {
			return page.Result[Entry]{}, err
		}
		var t time.Time
		if c.F != cursorField || c.D != "asc" || json.Unmarshal(c.V, &t) != nil {
			return page.Result[Entry]{}, kit.Input(field, kit.ReasonInvalidFormat, "invalid cursor")
		}
		at, id = &t, &c.ID
	}
	var rows []q.AuditEvent
	if w.Forward {
		rows, err = s.q.ListAuditEventsForward(ctx, q.ListAuditEventsForwardParams{
			EntityID: entityID, AfterAt: at, AfterID: id, Lim: int32(w.Limit + 1),
		})
	} else {
		rows, err = s.q.ListAuditEventsBackward(ctx, q.ListAuditEventsBackwardParams{
			EntityID: entityID, BeforeAt: at, BeforeID: id, Lim: int32(w.Limit + 1),
		})
	}
	if err != nil {
		return page.Result[Entry]{}, fmt.Errorf("audit: list: %w", err)
	}
	entries := make([]Entry, 0, len(rows))
	for _, r := range rows {
		e, err := toEntry(r)
		if err != nil {
			return page.Result[Entry]{}, err
		}
		entries = append(entries, e)
	}
	return page.Finish(entries, w, entryCursor), nil
}

func entryCursor(e Entry) string {
	v, _ := json.Marshal(e.At)
	return page.Encode(page.Cursor{F: cursorField, D: "asc", V: v, ID: e.ID})
}

func toEntry(r q.AuditEvent) (Entry, error) {
	var m map[string]struct {
		From json.RawMessage `json:"from"`
		To   json.RawMessage `json:"to"`
	}
	if err := json.Unmarshal(r.Changes, &m); err != nil {
		return Entry{}, fmt.Errorf("audit: decode changes: %w", err)
	}
	changes := make([]FieldChange, 0, len(m))
	for f, c := range m {
		changes = append(changes, FieldChange{Field: f, From: c.From, To: c.To})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Field < changes[j].Field })
	return Entry{ID: r.ID, ActorID: r.ActorID, Action: r.Action, Changes: changes, At: r.CreatedAt}, nil
}

// DeleteProject removes the events of a project, its states and the issues
// currently in it; used inside the project delete transaction.
func DeleteProject(ctx context.Context, tx *q.Queries, projectID uuid.UUID) error {
	if err := tx.DeleteProjectAudit(ctx, projectID); err != nil {
		return fmt.Errorf("audit: delete project events: %w", err)
	}
	return nil
}
