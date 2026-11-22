package plane

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/store/q"
)

// loadTargets reads the project's states and milestones, which may have
// changed in Yalkos since an earlier import.
func (pw *projectWriter) loadTargets(ctx context.Context) error {
	states, err := pw.w.q.ListStates(ctx, pw.target)
	if err != nil {
		return fmt.Errorf("list states: %w", err)
	}
	if len(states) == 0 {
		return errors.New("the project has no states")
	}
	pw.states = make(map[uuid.UUID]q.State, len(states))
	pw.def = slices.MinFunc(states, func(a, b q.State) int { return cmp.Compare(a.Position, b.Position) })
	for _, s := range states {
		pw.states[s.ID] = s
		if s.IsDefault {
			pw.def = s
		}
	}
	milestones, err := pw.w.q.ListMilestonesByProjects(ctx, []uuid.UUID{pw.target})
	if err != nil {
		return fmt.Errorf("list milestones: %w", err)
	}
	pw.modules = make(map[uuid.UUID]bool, len(milestones))
	for _, m := range milestones {
		pw.modules[m.ID] = true
	}
	return nil
}

// state is the Yalkos state of an issue: the one its Plane state maps to, or
// the project's default when that was deleted in Yalkos.
func (pw *projectWriter) state(planeID string, out *issueOutcome) q.State {
	if id, ok := pw.ex.mapped(kindState, planeID); ok {
		if s, ok := pw.states[id]; ok {
			return s
		}
		if !pw.gone[planeID] {
			pw.gone[planeID] = true
			out.deleted = append(out.deleted, "state "+pw.pp.Key+": "+pw.planeStateName(planeID))
		}
	}
	out.stateFallback = true
	return pw.def
}

func (pw *projectWriter) planeStateName(id string) string {
	for _, s := range pw.pp.Data.States {
		if s.ID == id {
			return s.Name
		}
	}
	return id
}

// milestone is the Yalkos milestone of a Plane module; nil when there is
// none or it was deleted in Yalkos.
func (pw *projectWriter) milestone(planeID string, out *issueOutcome) *uuid.UUID {
	if planeID == "" {
		return nil
	}
	id, ok := pw.ex.mapped(kindMilestone, planeID)
	if ok && pw.modules[id] {
		return &id
	}
	pw.reportMilestone(planeID, out)
	return nil
}

// reportMilestone lists a milestone deleted in Yalkos, once.
func (pw *projectWriter) reportMilestone(planeID string, out *issueOutcome) {
	if pw.gone[planeID] {
		return
	}
	pw.gone[planeID] = true
	name := planeID
	for _, m := range pw.pp.Data.Modules {
		if m.ID == planeID {
			name = m.Name
		}
	}
	out.deleted = append(out.deleted, "milestone "+pw.pp.Key+": "+name)
}

// liveState checks inside the issue's transaction that the state chosen
// before still exists; when it was deleted meanwhile the project's default
// state takes its place.
func (pw *projectWriter) liveState(ctx context.Context, tx *q.Queries, st q.State, planeID string, out *issueOutcome) (q.State, error) {
	row, err := tx.GetState(ctx, st.ID)
	if err == nil && row.ProjectID == pw.target {
		return row, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return q.State{}, fmt.Errorf("state: %w", err)
	}
	delete(pw.states, st.ID)
	if !pw.gone[planeID] {
		pw.gone[planeID] = true
		out.deleted = append(out.deleted, "state "+pw.pp.Key+": "+pw.planeStateName(planeID))
	}
	out.stateFallback = true
	states, err := tx.ListStates(ctx, pw.target)
	if err != nil {
		return q.State{}, fmt.Errorf("list states: %w", err)
	}
	if len(states) == 0 {
		return q.State{}, errors.New("the project has no states")
	}
	def := slices.MinFunc(states, func(a, b q.State) int { return cmp.Compare(a.Position, b.Position) })
	for _, s := range states {
		if s.IsDefault {
			def = s
		}
	}
	pw.def = def
	return def, nil
}

// liveMilestone checks inside the issue's transaction that the milestone
// chosen before still exists; nil when it was deleted meanwhile.
func (pw *projectWriter) liveMilestone(ctx context.Context, tx *q.Queries, id *uuid.UUID, planeID string, out *issueOutcome) (*uuid.UUID, error) {
	if id == nil {
		return nil, nil
	}
	m, err := tx.GetMilestone(ctx, *id)
	if err == nil && m.ProjectID == pw.target {
		return id, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("milestone: %w", err)
	}
	delete(pw.modules, *id)
	pw.reportMilestone(planeID, out)
	return nil, nil
}

// labelIDs are the Yalkos labels of the issue, the Type label first. Labels
// deleted in Yalkos are left out (Base reported them).
func (pw *projectWriter) labelIDs(is *IssuePlan) []uuid.UUID {
	var ids []uuid.UUID
	if is.Type != nil {
		ids = append(ids, *is.Type)
	}
	for _, key := range is.Labels {
		if id, ok := pw.w.labels[key]; ok && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// insertLabels puts the labels that still exist on the issue, at most one
// per exclusive group, and returns them and the ones deleted meanwhile.
func insertLabels(ctx context.Context, tx *q.Queries, issueID uuid.UUID, ids []uuid.UUID) (kept, missing []uuid.UUID, err error) {
	kept = []uuid.UUID{}
	if len(ids) == 0 {
		return kept, nil, nil
	}
	rows, err := tx.LockLabelsForShare(ctx, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("lock labels: %w", err)
	}
	byID := make(map[uuid.UUID]q.Label, len(rows))
	for _, l := range rows {
		byID[l.ID] = l
	}
	groups := map[uuid.UUID]bool{}
	for _, id := range ids {
		l, ok := byID[id]
		if !ok {
			missing = append(missing, id)
			continue
		}
		if l.IsGroup {
			continue
		}
		if l.ParentID != nil && byID[*l.ParentID].Exclusive {
			if groups[*l.ParentID] {
				continue
			}
			groups[*l.ParentID] = true
		}
		kept = append(kept, id)
	}
	if len(kept) == 0 {
		return kept, missing, nil
	}
	if err := tx.InsertIssueLabels(ctx, q.InsertIssueLabelsParams{IssueID: issueID, LabelIds: kept}); err != nil {
		return nil, nil, fmt.Errorf("issue labels: %w", err)
	}
	return kept, missing, nil
}
