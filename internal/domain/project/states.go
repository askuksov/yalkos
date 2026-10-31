package project

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/store/q"
)

// StateType is one of the five Linear workflow types.
type StateType string

// State types.
const (
	Backlog   StateType = "backlog"
	Unstarted StateType = "unstarted"
	Started   StateType = "started"
	Completed StateType = "completed"
	Canceled  StateType = "canceled"
)

// Valid reports a known type.
func (t StateType) Valid() bool {
	switch t {
	case Backlog, Unstarted, Started, Completed, Canceled:
		return true
	}
	return false
}

// State is a workflow column of a project.
type State struct {
	ID, ProjectID uuid.UUID
	Name          string
	Type          StateType
	Color         string
	Position      int
	IsDefault     bool
}

const (
	maxStateName = 50
	maxStates    = 50
)

func toState(s q.State) State {
	return State{
		ID: s.ID, ProjectID: s.ProjectID, Name: s.Name, Type: StateType(s.Type),
		Color: s.Color, Position: int(s.Position), IsDefault: s.IsDefault,
	}
}

func stateSnapshot(s q.State) map[string]any {
	return map[string]any{"name": s.Name, "type": s.Type, "color": s.Color, "position": s.Position, "isDefault": s.IsDefault}
}

func stateEvent(actor account.User, s q.State, action string, changes map[string]audit.Change, at time.Time) audit.Event {
	return audit.Event{
		EntityType: "state", Action: action, EntityID: s.ID, ProjectID: &s.ProjectID, ActorID: &actor.ID,
		Changes: changes, At: at,
	}
}

func isStateNameTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "states_name"
}

func stateNameTaken() error {
	return kit.Input("name", kit.ReasonTaken, "a state with this name already exists")
}

func stateColor(c string) (string, error) {
	if c == "" {
		return "", kit.Input("color", kit.ReasonRequired, "choose a color")
	}
	return Color("color", c)
}

// States lists a project's states by position.
func (s *Service) States(ctx context.Context, projectID uuid.UUID) ([]State, error) {
	rows, err := s.q.ListStates(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list states: %w", err)
	}
	out := make([]State, len(rows))
	for i, r := range rows {
		out[i] = toState(r)
	}
	return out, nil
}

// StatesByIDs loads states in the order of ids; missing ones are omitted.
func (s *Service) StatesByIDs(ctx context.Context, ids []uuid.UUID) ([]State, error) {
	rows, err := s.q.ListStatesByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("states by ids: %w", err)
	}
	byID := make(map[uuid.UUID]q.State, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	out := make([]State, 0, len(ids))
	for _, id := range ids {
		if st, ok := byID[id]; ok {
			out = append(out, toState(st))
		}
	}
	return out, nil
}

// StateInput is the new state form.
type StateInput struct {
	Name  string
	Type  StateType
	Color string
}

// CreateState appends a state to a project.
func (s *Service) CreateState(ctx context.Context, actor account.User, projectID uuid.UUID, in StateInput) (State, error) {
	if !actor.IsAdmin() {
		return State{}, kit.ErrForbidden
	}
	name, err := Text("name", in.Name, maxStateName)
	if err != nil {
		return State{}, err
	}
	if !in.Type.Valid() {
		return State{}, kit.Input("type", kit.ReasonInvalidFormat, "unknown state type")
	}
	color, err := stateColor(in.Color)
	if err != nil {
		return State{}, err
	}
	var out q.State
	err = s.inTx(ctx, func(tx *q.Queries) error {
		if _, err := s.LockWritable(ctx, tx, projectID); err != nil {
			return err
		}
		states, err := tx.ListStates(ctx, projectID)
		if err != nil {
			return fmt.Errorf("list states: %w", err)
		}
		if len(states) >= maxStates {
			return kit.Input("projectId", kit.ReasonTooMany, "at most 50 states").With("max", maxStates)
		}
		pos := int32(0)
		for _, st := range states {
			pos = max(pos, st.Position+1)
		}
		now := s.now()
		out, err = tx.CreateState(ctx, q.CreateStateParams{
			ProjectID: projectID, Name: name, Type: string(in.Type), Color: color, Position: pos, Now: now,
		})
		if isStateNameTaken(err) {
			return stateNameTaken()
		}
		if err != nil {
			return fmt.Errorf("create state: %w", err)
		}
		return audit.Record(ctx, tx, stateEvent(actor, out, "create", audit.Diff(nil, stateSnapshot(out)), now))
	})
	if err != nil {
		return State{}, err
	}
	return toState(out), nil
}

// lockState loads a state and locks its (writable) project.
func (s *Service) lockState(ctx context.Context, tx *q.Queries, id uuid.UUID) (q.State, error) {
	st, err := tx.GetState(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return q.State{}, kit.NotFoundError{Entity: "State"}
	}
	if err != nil {
		return q.State{}, fmt.Errorf("get state: %w", err)
	}
	if _, err := s.LockWritable(ctx, tx, st.ProjectID); err != nil {
		return q.State{}, err
	}
	// Re-read under the project lock.
	st, err = tx.GetState(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return q.State{}, kit.NotFoundError{Entity: "State"}
	}
	if err != nil {
		return q.State{}, fmt.Errorf("get state: %w", err)
	}
	return st, nil
}

// StatePatch edits a state; the type cannot change.
type StatePatch struct {
	Name, Color *string
	IsDefault   *bool
}

// UpdateState renames, recolors or makes a state the project default.
func (s *Service) UpdateState(ctx context.Context, actor account.User, id uuid.UUID, p StatePatch) (State, error) {
	if !actor.IsAdmin() {
		return State{}, kit.ErrForbidden
	}
	var name, color *string
	if p.Name != nil {
		n, err := Text("name", *p.Name, maxStateName)
		if err != nil {
			return State{}, err
		}
		name = &n
	}
	if p.Color != nil {
		c, err := stateColor(*p.Color)
		if err != nil {
			return State{}, err
		}
		color = &c
	}
	if p.IsDefault != nil && !*p.IsDefault {
		return State{}, kit.Input("isDefault", kit.ReasonNotAllowed, "choose another default state instead")
	}
	var out q.State
	err := s.inTx(ctx, func(tx *q.Queries) error {
		st, err := s.lockState(ctx, tx, id)
		if err != nil {
			return err
		}
		next := q.UpdateStateParams{ID: st.ID, Name: st.Name, Color: st.Color, IsDefault: st.IsDefault}
		if name != nil {
			next.Name = *name
		}
		if color != nil {
			next.Color = *color
		}
		makeDefault := p.IsDefault != nil && !st.IsDefault
		if makeDefault {
			if t := StateType(st.Type); t != Backlog && t != Unstarted {
				return kit.Input("isDefault", kit.ReasonNotAllowed, "the default state must be of type backlog or unstarted")
			}
			next.IsDefault = true
		}
		after := st
		after.Name, after.Color, after.IsDefault = next.Name, next.Color, next.IsDefault
		changes := audit.Diff(stateSnapshot(st), stateSnapshot(after))
		if len(changes) == 0 {
			out = st
			return nil
		}
		now := s.now()
		if makeDefault {
			old, err := tx.ClearDefaultState(ctx, q.ClearDefaultStateParams{ProjectID: st.ProjectID, Now: now})
			if err != nil {
				return fmt.Errorf("clear default state: %w", err)
			}
			if err := audit.Record(ctx, tx, stateEvent(actor, old, "update",
				map[string]audit.Change{"isDefault": {From: true, To: false}}, now)); err != nil {
				return err
			}
		}
		next.Now = now
		out, err = tx.UpdateState(ctx, next)
		if isStateNameTaken(err) {
			return stateNameTaken()
		}
		if err != nil {
			return fmt.Errorf("update state: %w", err)
		}
		return audit.Record(ctx, tx, stateEvent(actor, out, "update", changes, now))
	})
	if err != nil {
		return State{}, err
	}
	return toState(out), nil
}

// ReorderStates sets positions 0..n-1 in the order of ids, which must be
// exactly the project's states.
func (s *Service) ReorderStates(ctx context.Context, actor account.User, projectID uuid.UUID, ids []uuid.UUID) ([]State, error) {
	if !actor.IsAdmin() {
		return nil, kit.ErrForbidden
	}
	var out []q.State
	err := s.inTx(ctx, func(tx *q.Queries) error {
		if _, err := s.LockWritable(ctx, tx, projectID); err != nil {
			return err
		}
		states, err := tx.ListStates(ctx, projectID)
		if err != nil {
			return fmt.Errorf("list states: %w", err)
		}
		byID := make(map[uuid.UUID]q.State, len(states))
		for _, st := range states {
			byID[st.ID] = st
		}
		seen := make(map[uuid.UUID]bool, len(ids))
		for _, id := range ids {
			if _, ok := byID[id]; !ok || seen[id] {
				break
			}
			seen[id] = true
		}
		if len(ids) != len(states) || len(seen) != len(ids) {
			return kit.Input("stateIds", kit.ReasonMismatch, "list every state of the project exactly once")
		}
		now := s.now()
		for i, id := range ids {
			st := byID[id]
			if st.Position == int32(i) {
				continue
			}
			if err := tx.SetStatePosition(ctx, q.SetStatePositionParams{ID: id, Position: int32(i), Now: now}); err != nil {
				return fmt.Errorf("set state position: %w", err)
			}
			if err := audit.Record(ctx, tx, stateEvent(actor, st, "update",
				map[string]audit.Change{"position": {From: st.Position, To: i}}, now)); err != nil {
				return err
			}
		}
		out, err = tx.ListStates(ctx, projectID)
		return err
	})
	if err != nil {
		return nil, err
	}
	res := make([]State, len(out))
	for i, st := range out {
		res[i] = toState(st)
	}
	return res, nil
}

// DeleteState removes a state; its issues (deleted ones too) move to moveTo,
// which is required when the state holds issues.
func (s *Service) DeleteState(ctx context.Context, actor account.User, id uuid.UUID, moveTo *uuid.UUID) error {
	if !actor.IsAdmin() {
		return kit.ErrForbidden
	}
	return s.inTx(ctx, func(tx *q.Queries) error {
		st, err := s.lockState(ctx, tx, id)
		if err != nil {
			return err
		}
		if st.IsDefault {
			return kit.Input("id", kit.ReasonNotAllowed, "the default state cannot be deleted")
		}
		n, err := tx.CountStatesOfType(ctx, q.CountStatesOfTypeParams{ProjectID: st.ProjectID, Type: st.Type})
		if err != nil {
			return fmt.Errorf("count states: %w", err)
		}
		if n <= 1 {
			return kit.Input("id", kit.ReasonNotAllowed, "cannot delete the last state of its type")
		}
		has, err := tx.StateHasIssues(ctx, id)
		if err != nil {
			return fmt.Errorf("state issues: %w", err)
		}
		now := s.now()
		if has {
			if err := s.moveIssues(ctx, tx, actor, st, moveTo, now); err != nil {
				return err
			}
		}
		if err := tx.DeleteState(ctx, id); err != nil {
			return fmt.Errorf("delete state: %w", err)
		}
		return audit.Record(ctx, tx, stateEvent(actor, st, "delete", audit.Diff(stateSnapshot(st), nil), now))
	})
}

func (s *Service) moveIssues(ctx context.Context, tx *q.Queries, actor account.User, from q.State, moveTo *uuid.UUID, now time.Time) error {
	if moveTo == nil {
		return kit.Input("moveIssuesTo", kit.ReasonRequired, "choose a state for the issues of this state")
	}
	if *moveTo == from.ID {
		return kit.Input("moveIssuesTo", kit.ReasonNotAllowed, "choose another state")
	}
	to, err := tx.GetState(ctx, *moveTo)
	if errors.Is(err, pgx.ErrNoRows) {
		return kit.Input("moveIssuesTo", kit.ReasonNotFound, "no such state")
	}
	if err != nil {
		return fmt.Errorf("get state: %w", err)
	}
	if to.ProjectID != from.ProjectID {
		return kit.Input("moveIssuesTo", kit.ReasonWrongProject, "state belongs to another project")
	}
	keep := from.Type == to.Type
	completed, canceled := CompletionTimes(StateType(from.Type), StateType(to.Type), nil, nil, now)
	rows, err := tx.MoveIssuesToState(ctx, q.MoveIssuesToStateParams{
		FromState: from.ID, ToState: to.ID, KeepTimes: keep, CompletedAt: completed, CanceledAt: canceled, Now: now,
	})
	if err != nil {
		return fmt.Errorf("move issues: %w", err)
	}
	for _, r := range rows {
		if r.DeletedAt != nil {
			continue
		}
		changes := audit.Diff(
			map[string]any{"stateId": from.ID, "completedAt": r.OldCompletedAt, "canceledAt": r.OldCanceledAt},
			map[string]any{"stateId": to.ID, "completedAt": r.CompletedAt, "canceledAt": r.CanceledAt},
		)
		if err := audit.Record(ctx, tx, audit.Event{
			EntityType: "issue", Action: "update", EntityID: r.ID, ProjectID: &r.ProjectID, ActorID: &actor.ID,
			Changes: changes, At: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

// CompletionTimes applies the completed/canceled rule on a state change:
// same type keeps the timestamps; completed and canceled set their own and
// clear the other; other types clear both. prevType "" means a new issue.
func CompletionTimes(prevType, newType StateType, prevCompleted, prevCanceled *time.Time, now time.Time) (completed, canceled *time.Time) {
	if prevType == newType {
		return prevCompleted, prevCanceled
	}
	switch newType {
	case Completed:
		return &now, nil
	case Canceled:
		return nil, &now
	default:
		return nil, nil
	}
}

// StatesByProjects returns each project's states by position; projects
// without states (or unknown) are absent from the map.
func (s *Service) StatesByProjects(ctx context.Context, projectIDs []uuid.UUID) (map[uuid.UUID][]State, error) {
	rows, err := s.q.ListStatesByProjectIDs(ctx, projectIDs)
	if err != nil {
		return nil, fmt.Errorf("states by projects: %w", err)
	}
	out := make(map[uuid.UUID][]State, len(projectIDs))
	for _, r := range rows {
		out[r.ProjectID] = append(out[r.ProjectID], toState(r))
	}
	return out, nil
}
