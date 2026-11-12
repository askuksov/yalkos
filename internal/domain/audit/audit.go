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

// Event is a change to record. EntityType is
// project|state|issue|milestone|label|comment|attachment, Action is
// create|update|delete.
type Event struct {
	EntityType, Action string
	EntityID           uuid.UUID
	ProjectID, ActorID *uuid.UUID
	Changes            map[string]Change
	At                 time.Time
}

// Record writes e using tx (the caller's transaction). The names of the labels
// and milestones in e's labelIds/milestoneId changes are read in the same
// transaction and stored next to the changes, so the entry keeps them after a
// rename or delete; a caller that deletes one records its events first.
func Record(ctx context.Context, tx *q.Queries, e Event) error {
	changes := e.Changes
	if changes == nil {
		changes = map[string]Change{}
	}
	b, err := json.Marshal(changes)
	if err != nil {
		return fmt.Errorf("audit: encode changes: %w", err)
	}
	names, err := resolveNames(ctx, tx, changes)
	if err != nil {
		return err
	}
	if err := tx.InsertAuditEvent(ctx, q.InsertAuditEventParams{
		EntityType: e.EntityType, EntityID: e.EntityID, ProjectID: e.ProjectID, ActorID: e.ActorID,
		Action: e.Action, Changes: b, Names: names, CreatedAt: e.At,
	}); err != nil {
		return fmt.Errorf("audit: record: %w", err)
	}
	return nil
}

// storedName is a value of the names column, keyed by id; type is written only
// when true.
type storedName struct {
	Name string `json:"name"`
	Type bool   `json:"type,omitempty"`
}

// resolveNames returns the encoded names of the labels and milestones that
// changes refer to, or nil when it names none. Plain reads: no row locks.
func resolveNames(ctx context.Context, tx *q.Queries, changes map[string]Change) ([]byte, error) {
	var labels, milestones []uuid.UUID
	if c, ok := changes["labelIds"]; ok {
		for _, v := range []any{c.From, c.To} {
			var ids []uuid.UUID
			if err := recode(v, &ids); err != nil {
				return nil, fmt.Errorf("audit: label ids: %w", err)
			}
			labels = append(labels, ids...)
		}
	}
	if c, ok := changes["milestoneId"]; ok {
		for _, v := range []any{c.From, c.To} {
			var id *uuid.UUID
			if err := recode(v, &id); err != nil {
				return nil, fmt.Errorf("audit: milestone id: %w", err)
			}
			if id != nil {
				milestones = append(milestones, *id)
			}
		}
	}
	out := map[uuid.UUID]storedName{}
	if len(labels) > 0 {
		rows, err := tx.AuditLabelNames(ctx, labels)
		if err != nil {
			return nil, fmt.Errorf("audit: label names: %w", err)
		}
		for _, r := range rows {
			out[r.ID] = storedName{Name: r.Name, Type: r.IsType}
		}
	}
	if len(milestones) > 0 {
		rows, err := tx.AuditMilestoneNames(ctx, milestones)
		if err != nil {
			return nil, fmt.Errorf("audit: milestone names: %w", err)
		}
		for _, r := range rows {
			out[r.ID] = storedName{Name: r.Name}
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("audit: encode names: %w", err)
	}
	return b, nil
}

// recode reads v (whatever type the caller recorded) through its JSON encoding.
func recode(v, dst any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
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

// Name is a label or milestone named by an event, as it was when recorded.
type Name struct {
	ID   uuid.UUID
	Name string
	// Type: a label of the type group.
	Type bool
}

// Entry is one recorded event. Names is empty for events recorded before
// names were stored.
type Entry struct {
	ID      uuid.UUID
	ActorID *uuid.UUID
	Action  string
	Changes []FieldChange
	Names   []Name
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
	var stored map[uuid.UUID]storedName
	if r.Names != nil {
		if err := json.Unmarshal(r.Names, &stored); err != nil {
			return Entry{}, fmt.Errorf("audit: decode names: %w", err)
		}
	}
	names := make([]Name, 0, len(stored))
	for id, n := range stored {
		names = append(names, Name{ID: id, Name: n.Name, Type: n.Type})
	}
	sort.Slice(names, func(i, j int) bool { return names[i].ID.String() < names[j].ID.String() })
	return Entry{ID: r.ID, ActorID: r.ActorID, Action: r.Action, Changes: changes, Names: names, At: r.CreatedAt}, nil
}

// DeleteProject removes the events of a project, its states and the issues
// currently in it; used inside the project delete transaction.
func DeleteProject(ctx context.Context, tx *q.Queries, projectID uuid.UUID) error {
	if err := tx.DeleteProjectAudit(ctx, projectID); err != nil {
		return fmt.Errorf("audit: delete project events: %w", err)
	}
	return nil
}

// ByIDs loads events in the order of ids; missing ones are omitted.
func (s *Service) ByIDs(ctx context.Context, ids []uuid.UUID) ([]Entry, error) {
	rows, err := s.q.ListAuditEventsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("audit: by ids: %w", err)
	}
	byID := make(map[uuid.UUID]Entry, len(rows))
	for _, r := range rows {
		e, err := toEntry(r)
		if err != nil {
			return nil, err
		}
		byID[r.ID] = e
	}
	out := make([]Entry, 0, len(ids))
	for _, id := range ids {
		if e, ok := byID[id]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}
