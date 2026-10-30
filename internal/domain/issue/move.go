package issue

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/store/q"
)

// Move moves an issue with all its live descendants to another project. Each
// moved issue gets the target's next number; old identifiers keep resolving.
// stateID (a target state) applies to the moved issue only; otherwise, and
// always for descendants, the state maps by name, then by type.
func (s *Service) Move(ctx context.Context, actor account.User, id, toProject uuid.UUID, stateID *uuid.UUID) (Issue, error) {
	var out Issue
	err := s.inTx(ctx, func(tx *q.Queries) error {
		root, target, err := s.lockForMove(ctx, tx, id, toProject)
		if err != nil {
			return err
		}
		var explicit *q.State
		if stateID != nil {
			st, err := stateFor(ctx, tx, toProject, stateID)
			if err != nil {
				return err
			}
			explicit = &st
		}
		targetStates, err := tx.ListStates(ctx, toProject)
		if err != nil {
			return fmt.Errorf("list states: %w", err)
		}
		descendants, err := tx.IssueDescendantIDs(ctx, root.ID)
		if err != nil {
			return fmt.Errorf("issue descendants: %w", err)
		}
		now := s.now()
		for n, issueID := range append([]uuid.UUID{root.ID}, descendants...) {
			row, err := tx.GetIssueForUpdate(ctx, issueID)
			if err != nil {
				return fmt.Errorf("lock issue: %w", err)
			}
			before := toIssue(row.Issue, row.ProjectKey)
			from, err := tx.GetState(ctx, before.StateID)
			if err != nil {
				return fmt.Errorf("get state: %w", err)
			}
			to := mapState(from, targetStates)
			if n == 0 && explicit != nil {
				to = *explicit
			}
			number, err := tx.NextIssueNumber(ctx, toProject)
			if err != nil {
				return fmt.Errorf("next issue number: %w", err)
			}
			if err := tx.InsertIssueNumber(ctx, q.InsertIssueNumberParams{ProjectID: toProject, Number: number, IssueID: issueID}); err != nil {
				return fmt.Errorf("issue number: %w", err)
			}
			parent := before.ParentID
			if n == 0 {
				parent = nil
			}
			completed, canceled := project.CompletionTimes(project.StateType(from.Type), project.StateType(to.Type),
				before.CompletedAt, before.CanceledAt, now)
			moved, err := tx.MoveIssue(ctx, q.MoveIssueParams{
				ID: issueID, ProjectID: toProject, Number: number, StateID: to.ID, ParentID: parent,
				CompletedAt: completed, CanceledAt: canceled, Now: now,
			})
			if err != nil {
				return fmt.Errorf("move issue: %w", err)
			}
			after := toIssue(moved, target.Key)
			if err := record(ctx, tx, actor, after, "update", audit.Diff(snapshot(before), snapshot(after)), now); err != nil {
				return err
			}
			if n == 0 {
				out = after
			}
		}
		return nil
	})
	if err != nil {
		return Issue{}, err
	}
	return out, nil
}

// lockForMove locks source and target projects in UUID order (no deadlock
// between opposite moves), then the issue row. Both projects must be writable.
func (s *Service) lockForMove(ctx context.Context, tx *q.Queries, id, toProject uuid.UUID) (q.Issue, q.Project, error) {
	for range 3 {
		row, err := tx.GetIssue(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return q.Issue{}, q.Project{}, errNotFound
		}
		if err != nil {
			return q.Issue{}, q.Project{}, fmt.Errorf("get issue: %w", err)
		}
		source := row.Issue.ProjectID
		if source == toProject {
			return q.Issue{}, q.Project{}, kit.Input("projectId", kit.ReasonNotAllowed, "issue is already in this project")
		}
		ids := []uuid.UUID{source, toProject}
		if bytes.Compare(ids[1][:], ids[0][:]) < 0 {
			ids[0], ids[1] = ids[1], ids[0]
		}
		var target q.Project
		for _, pid := range ids {
			p, err := s.lockProject(ctx, tx, pid)
			if err != nil {
				return q.Issue{}, q.Project{}, err
			}
			if pid == toProject {
				target = p
			}
		}
		locked, err := tx.GetIssueForUpdate(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return q.Issue{}, q.Project{}, errNotFound
		}
		if err != nil {
			return q.Issue{}, q.Project{}, fmt.Errorf("lock issue: %w", err)
		}
		if locked.Issue.ProjectID == source {
			return locked.Issue, target, nil
		}
	}
	return q.Issue{}, q.Project{}, errors.New("issue: moved concurrently, try again")
}

// mapState picks the target state with the same name (case-insensitive),
// else the first (lowest position) state of the same type.
func mapState(from q.State, target []q.State) q.State {
	for _, st := range target {
		if strings.EqualFold(st.Name, from.Name) {
			return st
		}
	}
	for _, st := range target {
		if st.Type == from.Type {
			return st
		}
	}
	// Unreachable: every project keeps at least one state of each type.
	return target[0]
}
