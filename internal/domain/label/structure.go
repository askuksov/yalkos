package label

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/store/q"
)

// Patch edits a label: nil keeps a value; ParentID set with nil moves the
// label out of its group.
type Patch struct {
	Name, Color, Description *string
	ParentID                 kit.Opt[uuid.UUID]
	Exclusive                *bool
	Position                 *float64
}

func conflictErr(field string, n int) error {
	return kit.Input(field, kit.ReasonNotAllowed, fmt.Sprintf("%d issues would get two labels of this group", n)).
		With("conflicts", n)
}

func (s *Service) lock(ctx context.Context, tx *q.Queries, id uuid.UUID) (Label, error) {
	row, err := tx.GetLabelForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Label{}, errNotFound
	}
	if err != nil {
		return Label{}, fmt.Errorf("lock label: %w", err)
	}
	return FromRow(row), nil
}

// Update renames, recolours, regroups or reorders a label, or changes a
// group's exclusivity (Owner/Admin). A change that would leave an issue with
// two labels of an exclusive group is refused with the number of such issues.
func (s *Service) Update(ctx context.Context, actor account.User, id uuid.UUID, p Patch) (Label, error) {
	if !actor.IsAdmin() {
		return Label{}, kit.ErrForbidden
	}
	var name, color, desc *string
	if p.Name != nil {
		n, err := project.Text("name", *p.Name, maxName)
		if err != nil {
			return Label{}, err
		}
		name = &n
	}
	if p.Color != nil {
		c, err := project.Color("color", *p.Color)
		if err != nil {
			return Label{}, err
		}
		color = &c
	}
	if p.Description != nil {
		d, err := validDescription(*p.Description)
		if err != nil {
			return Label{}, err
		}
		desc = &d
	}
	var newGroup *uuid.UUID
	if p.ParentID.Set {
		newGroup = p.ParentID.Value
	}
	var out Label
	err := s.inTxWith(ctx, func(ptx pgx.Tx, tx *q.Queries) error {
		cur, locked, err := s.lockForChange(ctx, tx, id, newGroup)
		if err != nil {
			return err
		}
		st, err := settings(ctx, tx)
		if err != nil {
			return err
		}
		next := cur
		if name != nil {
			next.Name = *name
		}
		if color != nil {
			next.Color = *color
		}
		if desc != nil {
			next.Description = *desc
		}
		if p.Position != nil {
			next.Position = *p.Position
		}
		regrouped := false
		var target *q.Label
		if p.ParentID.Set && !sameID(p.ParentID.Value, cur.ParentID) {
			if cur.IsGroup {
				return kit.Input("parentId", kit.ReasonNotAllowed, "groups cannot be nested")
			}
			if p.ParentID.Value != nil {
				if *p.ParentID.Value == id {
					return kit.Input("parentId", kit.ReasonNotAllowed, "a label cannot contain itself")
				}
				row, found := locked[*p.ParentID.Value]
				g, err := asGroup(row, found)
				if err != nil {
					return err
				}
				if g.Exclusive {
					n, err := tx.CountMoveConflicts(ctx, q.CountMoveConflictsParams{LabelID: id, GroupID: g.ID})
					if err != nil {
						return fmt.Errorf("count conflicts: %w", err)
					}
					if n > 0 {
						return conflictErr("parentId", int(n))
					}
				}
				target = &g
			}
			next.ParentID = p.ParentID.Value
			regrouped = true
			if p.Position == nil {
				pos, err := tx.NextLabelPosition(ctx, next.ParentID)
				if err != nil {
					return fmt.Errorf("label position: %w", err)
				}
				next.Position = pos
			}
		}
		exclusivityChanged := false
		if p.Exclusive != nil && *p.Exclusive != cur.Exclusive {
			switch {
			case !cur.IsGroup:
				return kit.Input("exclusive", kit.ReasonNotAllowed, "only groups can be exclusive")
			case id == *st.TypeGroupID:
				return kit.Input("exclusive", kit.ReasonNotAllowed, "the type group is always exclusive")
			}
			// The group's row lock is enough: moving a label into or out of the
			// group locks the group too, and so does every issue label write.
			if *p.Exclusive {
				n, err := tx.CountGroupConflicts(ctx, id)
				if err != nil {
					return fmt.Errorf("count conflicts: %w", err)
				}
				if n > 0 {
					return conflictErr("exclusive", int(n))
				}
			}
			next.Exclusive = *p.Exclusive
			exclusivityChanged = true
		}
		changes := audit.Diff(snapshot(cur), snapshot(next))
		if len(changes) == 0 {
			out = cur
			return nil
		}
		now := s.now()
		row, err := tx.UpdateLabel(ctx, q.UpdateLabelParams{
			ID: id, Name: next.Name, Description: next.Description, Color: next.Color,
			ParentID: next.ParentID, Exclusive: next.Exclusive, Position: next.Position, Now: now,
		})
		if nameTaken(err) {
			return takenErr()
		}
		if err != nil {
			return fmt.Errorf("update label: %w", err)
		}
		if regrouped {
			var g *uuid.UUID
			if target != nil && target.Exclusive {
				g = &target.ID
			}
			if err := tx.SetLabelExclusiveGroup(ctx, q.SetLabelExclusiveGroupParams{GroupID: g, LabelID: id}); err != nil {
				return fmt.Errorf("issue label groups: %w", err)
			}
		}
		if exclusivityChanged {
			var g *uuid.UUID
			if next.Exclusive {
				g = &id
			}
			if err := tx.SetGroupExclusiveGroup(ctx, q.SetGroupExclusiveGroupParams{ExclusiveGroupID: g, GroupID: id}); err != nil {
				return fmt.Errorf("issue label groups: %w", err)
			}
		}
		out = FromRow(row)
		if err := record(ctx, tx, actor, out, "update", changes, now); err != nil {
			return err
		}
		return s.emit(ctx, ptx, event.Change{
			Resource: event.Label, Action: event.Update, EntityID: id, ActorID: actor.ID, At: now,
			Diff: changes, PrevUpdatedAt: cur.UpdatedAt,
		})
	})
	if err != nil {
		return Label{}, err
	}
	return out, nil
}

// lockForChange locks the label, its current group and newGroup (when set) in
// one statement ordered by id, the order issue label writes use, and returns
// the locked rows by id. When the label moved to another group while this
// waited for its lock, that group is not locked; locking it now could wait for
// a change that waits for the label, so the change starts over instead
// (errMovedMeanwhile, a kit.ErrStartOver) and releases the label first.
func (s *Service) lockForChange(ctx context.Context, tx *q.Queries, id uuid.UUID, newGroup *uuid.UUID) (Label, map[uuid.UUID]q.Label, error) {
	row, err := tx.GetLabel(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Label{}, nil, errNotFound
	}
	if err != nil {
		return Label{}, nil, fmt.Errorf("get label: %w", err)
	}
	ids := []uuid.UUID{id}
	if row.ParentID != nil {
		ids = append(ids, *row.ParentID)
	}
	if newGroup != nil {
		ids = append(ids, *newGroup)
	}
	rows, err := tx.LockLabelsForUpdate(ctx, ids)
	if err != nil {
		return Label{}, nil, fmt.Errorf("lock labels: %w", err)
	}
	locked := make(map[uuid.UUID]q.Label, len(rows))
	for _, r := range rows {
		locked[r.ID] = r
	}
	cur, ok := locked[id]
	if !ok {
		return Label{}, nil, errNotFound
	}
	if !sameID(cur.ParentID, row.ParentID) {
		return Label{}, nil, errMovedMeanwhile
	}
	return FromRow(cur), locked, nil
}

func sameID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Delete removes a label from every issue (one issue audit entry each) and
// deletes it; a group must be empty, the type group stays. Owner/Admin only.
//
// The issues that held the label get a new updated_at, so their writers' locks
// matter: issue writers lock the issue's project, then the issue, then
// share-lock its labels. Delete therefore locks the projects of those issues
// first (in id order), then the label. A project that got an issue with the
// label while Delete waited is not locked, and locking it on top of the label
// could deadlock with a writer that holds it and waits for the label, so the
// transaction starts over instead (kit.ErrStartOver).
func (s *Service) Delete(ctx context.Context, actor account.User, id uuid.UUID) error {
	if !actor.IsAdmin() {
		return kit.ErrForbidden
	}
	return s.inTxWith(ctx, func(ptx pgx.Tx, tx *q.Queries) error {
		if _, err := tx.GetLabel(ctx, id); errors.Is(err, pgx.ErrNoRows) {
			return errNotFound
		} else if err != nil {
			return fmt.Errorf("get label: %w", err)
		}
		projects, err := tx.LabelIssueProjectIDs(ctx, id)
		if err != nil {
			return fmt.Errorf("label projects: %w", err)
		}
		held, err := tx.LockProjectsForUpdate(ctx, projects)
		if err != nil {
			return fmt.Errorf("lock projects: %w", err)
		}
		cur, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		// Writers that wait for the label cannot add it to an issue now; one that
		// finished meanwhile may have, in a project this run does not hold.
		again, err := tx.LabelIssueProjectIDs(ctx, id)
		if err != nil {
			return fmt.Errorf("label projects: %w", err)
		}
		if slices.ContainsFunc(again, func(p uuid.UUID) bool { return !slices.Contains(held, p) }) {
			return errProjectsChanged
		}
		if cur.IsGroup {
			st, err := settings(ctx, tx)
			if err != nil {
				return err
			}
			if id == *st.TypeGroupID {
				return kit.Input("id", kit.ReasonNotAllowed, "the type group cannot be deleted")
			}
			n, err := tx.CountLabelChildren(ctx, &id)
			if err != nil {
				return fmt.Errorf("count group labels: %w", err)
			}
			if n > 0 {
				return kit.Input("id", kit.ReasonNotAllowed, "move or delete the group's labels first").With("labels", int(n))
			}
		}
		now := s.now()
		sets, err := tx.IssueLabelSetsWithLabel(ctx, id)
		if err != nil {
			return fmt.Errorf("issues with label: %w", err)
		}
		changes := make([]map[string]audit.Change, len(sets))
		touched := make([]uuid.UUID, len(sets))
		for n, set := range sets {
			after := slices.DeleteFunc(slices.Clone(set.LabelIds), func(l uuid.UUID) bool { return l == id })
			changes[n] = map[string]audit.Change{"labelIds": {From: set.LabelIds, To: after}}
			touched[n] = set.IssueID
			if err := audit.Record(ctx, tx, audit.Event{
				EntityType: "issue", Action: "update", EntityID: set.IssueID, ProjectID: &set.ProjectID, ActorID: &actor.ID,
				Changes: changes[n], At: now,
			}); err != nil {
				return err
			}
		}
		// The label is reported while it still exists; the issues once they
		// have lost it and carry the new updated_at.
		if err := s.emit(ctx, ptx, event.Change{
			Resource: event.Label, Action: event.Remove, EntityID: id, ActorID: actor.ID, At: now,
		}); err != nil {
			return err
		}
		if err := tx.DeleteLabel(ctx, id); err != nil {
			return fmt.Errorf("delete label: %w", err)
		}
		if len(touched) > 0 {
			if err := tx.TouchIssues(ctx, q.TouchIssuesParams{Ids: touched, Now: now}); err != nil {
				return fmt.Errorf("touch issues: %w", err)
			}
		}
		for n, set := range sets {
			if err := s.emit(ctx, ptx, event.Change{
				Resource: event.Issue, Action: event.Update, EntityID: set.IssueID, ProjectIDs: []uuid.UUID{set.ProjectID},
				ActorID: actor.ID, At: now, Diff: changes[n], PrevUpdatedAt: set.UpdatedAt,
			}); err != nil {
				return err
			}
		}
		return record(ctx, tx, actor, cur, "delete", nil, now)
	})
}
