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

// Record writes e using tx (the caller's transaction); see RecordMany.
func Record(ctx context.Context, tx *q.Queries, e Event) error {
	return RecordMany(ctx, tx, []Event{e})
}

// RecordMany writes events using tx (the caller's transaction), in order. The
// names of the labels, milestones, states and projects in each event's
// labelIds, milestoneId, stateId and projectId changes (a project by its key)
// are read in the same transaction, in one statement for all events, and each
// event stores the names of its own ids next to its changes, so the entry
// keeps them after a rename or delete; a caller that deletes one records its
// events first. All rows are inserted in one statement.
func RecordMany(ctx context.Context, tx *q.Queries, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	refs, all, err := eventRefs(events)
	if err != nil {
		return err
	}
	found, err := lookupNames(ctx, tx, all)
	if err != nil {
		return err
	}
	// NULL project and actor ids and absent names are empty strings.
	in := q.InsertAuditEventsParams{
		EntityTypes: make([]string, len(events)), EntityIds: make([]uuid.UUID, len(events)),
		ProjectIds: make([]string, len(events)), ActorIds: make([]string, len(events)),
		Actions: make([]string, len(events)), Changes: make([]string, len(events)),
		Names: make([]string, len(events)), CreatedAts: make([]time.Time, len(events)),
	}
	for i, e := range events {
		changes := e.Changes
		if changes == nil {
			changes = map[string]Change{}
		}
		b, err := json.Marshal(changes)
		if err != nil {
			return fmt.Errorf("audit: encode changes: %w", err)
		}
		names, err := encodeNames(refs[i], found)
		if err != nil {
			return err
		}
		in.EntityTypes[i], in.EntityIds[i], in.Actions[i], in.Changes[i], in.Names[i], in.CreatedAts[i] =
			e.EntityType, e.EntityID, e.Action, string(b), names, e.At
		if e.ProjectID != nil {
			in.ProjectIds[i] = e.ProjectID.String()
		}
		if e.ActorID != nil {
			in.ActorIds[i] = e.ActorID.String()
		}
	}
	if err := tx.InsertAuditEvents(ctx, in); err != nil {
		return fmt.Errorf("audit: record: %w", err)
	}
	return nil
}

// eventRefs returns the named ids of each event and of all events together,
// each id once.
func eventRefs(events []Event) ([]q.AuditNamesParams, q.AuditNamesParams, error) {
	refs := make([]q.AuditNamesParams, len(events))
	var all q.AuditNamesParams
	for i, e := range events {
		var err error
		if refs[i], err = namedIDs(e.Changes); err != nil {
			return nil, all, err
		}
		all.LabelIds = append(all.LabelIds, refs[i].LabelIds...)
		all.MilestoneIds = append(all.MilestoneIds, refs[i].MilestoneIds...)
		all.StateIds = append(all.StateIds, refs[i].StateIds...)
		all.ProjectIds = append(all.ProjectIds, refs[i].ProjectIds...)
	}
	all.LabelIds, all.MilestoneIds = kit.Distinct(all.LabelIds), kit.Distinct(all.MilestoneIds)
	all.StateIds, all.ProjectIds = kit.Distinct(all.StateIds), kit.Distinct(all.ProjectIds)
	return refs, all, nil
}

// storedName is a value of the names column, keyed by id; type is written only
// when true.
type storedName struct {
	Name string `json:"name"`
	Type bool   `json:"type,omitempty"`
}

// listIDs returns the ids that field of changes holds, as lists, in From and To.
func listIDs(changes map[string]Change, field string) ([]uuid.UUID, error) {
	c, ok := changes[field]
	if !ok {
		return nil, nil
	}
	var dst []uuid.UUID
	for _, v := range []any{c.From, c.To} {
		var ids []uuid.UUID
		if err := recode(v, &ids); err != nil {
			return nil, fmt.Errorf("audit: %s: %w", field, err)
		}
		dst = append(dst, ids...)
	}
	return dst, nil
}

// singleIDs returns the ids that field of changes holds, one each, in From and
// To; nil is skipped.
func singleIDs(changes map[string]Change, field string) ([]uuid.UUID, error) {
	c, ok := changes[field]
	if !ok {
		return nil, nil
	}
	var dst []uuid.UUID
	for _, v := range []any{c.From, c.To} {
		var id *uuid.UUID
		if err := recode(v, &id); err != nil {
			return nil, fmt.Errorf("audit: %s: %w", field, err)
		}
		if id != nil {
			dst = append(dst, *id)
		}
	}
	return dst, nil
}

// namedIDs returns the ids of the labels, milestones, states and projects that
// changes refer to.
func namedIDs(changes map[string]Change) (q.AuditNamesParams, error) {
	var ids q.AuditNamesParams
	var err error
	if ids.LabelIds, err = listIDs(changes, "labelIds"); err != nil {
		return ids, err
	}
	if ids.MilestoneIds, err = singleIDs(changes, "milestoneId"); err != nil {
		return ids, err
	}
	if ids.StateIds, err = singleIDs(changes, "stateId"); err != nil {
		return ids, err
	}
	ids.ProjectIds, err = singleIDs(changes, "projectId")
	return ids, err
}

// lookupNames reads the names of ids in one statement for all kinds, or none
// when ids is empty; plain reads: no row locks.
func lookupNames(ctx context.Context, tx *q.Queries, ids q.AuditNamesParams) (map[uuid.UUID]storedName, error) {
	if len(ids.LabelIds)+len(ids.MilestoneIds)+len(ids.StateIds)+len(ids.ProjectIds) == 0 {
		return nil, nil
	}
	rows, err := tx.AuditNames(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("audit: names: %w", err)
	}
	out := make(map[uuid.UUID]storedName, len(rows))
	for _, r := range rows {
		out[r.ID] = storedName{Name: r.Name, Type: r.IsType}
	}
	return out, nil
}

// encodeNames returns the encoded names of the ids in refs that found holds,
// or "" when there are none.
func encodeNames(refs q.AuditNamesParams, found map[uuid.UUID]storedName) (string, error) {
	out := map[uuid.UUID]storedName{}
	for _, list := range [][]uuid.UUID{refs.LabelIds, refs.MilestoneIds, refs.StateIds, refs.ProjectIds} {
		for _, id := range list {
			if n, ok := found[id]; ok {
				out[id] = n
			}
		}
	}
	if len(out) == 0 {
		return "", nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("audit: encode names: %w", err)
	}
	return string(b), nil
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

// Name is a label, milestone, state or project (by its key) named by an event,
// as it was when recorded.
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
	w, at, id, err := window(a)
	if err != nil {
		return page.Result[Entry]{}, err
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
	entries, err := toEntries(rows)
	if err != nil {
		return page.Result[Entry]{}, err
	}
	return page.Finish(entries, w, entryCursor), nil
}

// ListMany is List for each of the given entities in one statement: the page
// size and the cursor apply to each entity on its own. Every given entity has
// an entry, empty when it has no events.
func (s *Service) ListMany(ctx context.Context, entityIDs []uuid.UUID, a page.Args) (map[uuid.UUID]page.Result[Entry], error) {
	w, at, id, err := window(a)
	if err != nil {
		return nil, err
	}
	ids := kit.Distinct(entityIDs)
	var rows []q.AuditEvent
	if w.Forward {
		batch, err := s.q.ListAuditEventsForwardBatch(ctx, q.ListAuditEventsForwardBatchParams{
			EntityIds: ids, AfterAt: at, AfterID: id, Lim: int32(w.Limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("audit: list many: %w", err)
		}
		for _, r := range batch {
			rows = append(rows, r.AuditEvent)
		}
	} else {
		batch, err := s.q.ListAuditEventsBackwardBatch(ctx, q.ListAuditEventsBackwardBatchParams{
			EntityIds: ids, BeforeAt: at, BeforeID: id, Lim: int32(w.Limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("audit: list many: %w", err)
		}
		for _, r := range batch {
			rows = append(rows, r.AuditEvent)
		}
	}
	byEntity := make(map[uuid.UUID][]q.AuditEvent, len(ids))
	for _, r := range rows {
		byEntity[r.EntityID] = append(byEntity[r.EntityID], r)
	}
	out := make(map[uuid.UUID]page.Result[Entry], len(ids))
	for _, e := range ids {
		entries, err := toEntries(byEntity[e])
		if err != nil {
			return nil, err
		}
		out[e] = page.Finish(entries, w, entryCursor)
	}
	return out, nil
}

// window checks the page arguments and decodes the cursor, if any.
func window(a page.Args) (page.Window, *time.Time, *uuid.UUID, error) {
	w, err := a.Window()
	if err != nil {
		return w, nil, nil, err
	}
	if w.Cursor == nil {
		return w, nil, nil, nil
	}
	field := "after"
	if !w.Forward {
		field = "before"
	}
	c, err := page.Decode(*w.Cursor, field)
	if err != nil {
		return w, nil, nil, err
	}
	var t time.Time
	if c.F != cursorField || c.D != "asc" || json.Unmarshal(c.V, &t) != nil {
		return w, nil, nil, kit.Input(field, kit.ReasonInvalidFormat, "invalid cursor")
	}
	return w, &t, &c.ID, nil
}

func toEntries(rows []q.AuditEvent) ([]Entry, error) {
	entries := make([]Entry, 0, len(rows))
	for _, r := range rows {
		e, err := toEntry(r)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
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
