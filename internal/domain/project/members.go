package project

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/store/q"
)

// MemberIDs lists the project's members in the order they joined.
func (s *Service) MemberIDs(ctx context.Context, projectID uuid.UUID) ([]uuid.UUID, error) {
	ids, err := s.q.ListProjectMemberIDs(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("project members: %w", err)
	}
	return ids, nil
}

// AddMember adds an active user to the project (idempotent).
func (s *Service) AddMember(ctx context.Context, actor account.User, projectID, userID uuid.UUID) error {
	return s.changeMembers(ctx, actor, projectID, func(tx *q.Queries) error {
		u, err := tx.GetUserByID(ctx, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return kit.Input("userId", kit.ReasonNotFound, "no such user")
		}
		if err != nil {
			return fmt.Errorf("find user: %w", err)
		}
		if u.Disabled {
			return kit.Input("userId", kit.ReasonNotAllowed, "user is disabled")
		}
		return tx.AddProjectMember(ctx, q.AddProjectMemberParams{ProjectID: projectID, UserID: userID, Now: s.now()})
	})
}

// RemoveMember removes a user from the project (idempotent).
func (s *Service) RemoveMember(ctx context.Context, actor account.User, projectID, userID uuid.UUID) error {
	return s.changeMembers(ctx, actor, projectID, func(tx *q.Queries) error {
		return tx.RemoveProjectMember(ctx, q.RemoveProjectMemberParams{ProjectID: projectID, UserID: userID})
	})
}

// changeMembers runs change under the project lock and audits the member list.
func (s *Service) changeMembers(ctx context.Context, actor account.User, projectID uuid.UUID, change func(*q.Queries) error) error {
	if !actor.IsAdmin() {
		return kit.ErrForbidden
	}
	return s.inTx(ctx, func(tx *q.Queries) error {
		if _, err := s.LockWritable(ctx, tx, projectID); err != nil {
			return err
		}
		before, err := tx.ListProjectMemberIDs(ctx, projectID)
		if err != nil {
			return fmt.Errorf("project members: %w", err)
		}
		if err := change(tx); err != nil {
			return err
		}
		after, err := tx.ListProjectMemberIDs(ctx, projectID)
		if err != nil {
			return fmt.Errorf("project members: %w", err)
		}
		if slices.Equal(before, after) {
			return nil
		}
		return audit.Record(ctx, tx, audit.Event{
			EntityType: "project", Action: "update", EntityID: projectID, ProjectID: &projectID, ActorID: &actor.ID,
			Changes: map[string]audit.Change{"members": {From: nonNil(before), To: nonNil(after)}}, At: s.now(),
		})
	})
}

func nonNil(ids []uuid.UUID) []uuid.UUID {
	if ids == nil {
		return []uuid.UUID{}
	}
	return ids
}

// MemberIDsByProjects returns each project's member ids in join order.
func (s *Service) MemberIDsByProjects(ctx context.Context, projectIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	rows, err := s.q.ListProjectMembersByProjectIDs(ctx, projectIDs)
	if err != nil {
		return nil, fmt.Errorf("project members: %w", err)
	}
	out := make(map[uuid.UUID][]uuid.UUID, len(projectIDs))
	for _, r := range rows {
		out[r.ProjectID] = append(out[r.ProjectID], r.UserID)
	}
	return out, nil
}
