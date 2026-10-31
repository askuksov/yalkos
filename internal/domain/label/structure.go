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
	var out Label
	err := s.inTx(ctx, func(tx *q.Queries) error {
		cur, err := s.lock(ctx, tx, id)
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
		var newGroup *q.Label
		if p.ParentID.Set && !sameID(p.ParentID.Value, cur.ParentID) {
			if cur.IsGroup {
				return kit.Input("parentId", kit.ReasonNotAllowed, "groups cannot be nested")
			}
			if p.ParentID.Value != nil {
				if *p.ParentID.Value == id {
					return kit.Input("parentId", kit.ReasonNotAllowed, "a label cannot contain itself")
				}
				g, err := group(ctx, tx, *p.ParentID.Value)
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
				newGroup = &g
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
			if *p.Exclusive {
				if _, err := tx.LockLabelChildren(ctx, &id); err != nil {
					return fmt.Errorf("lock group labels: %w", err)
				}
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
			if newGroup != nil && newGroup.Exclusive {
				g = &newGroup.ID
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
		return record(ctx, tx, actor, out, "update", changes, now)
	})
	if err != nil {
		return Label{}, err
	}
	return out, nil
}

func sameID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Delete removes a label from every issue (one issue audit entry each) and
// deletes it; a group must be empty, the type group stays. Owner/Admin only.
func (s *Service) Delete(ctx context.Context, actor account.User, id uuid.UUID) error {
	if !actor.IsAdmin() {
		return kit.ErrForbidden
	}
	return s.inTx(ctx, func(tx *q.Queries) error {
		cur, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
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
		for _, set := range sets {
			after := slices.DeleteFunc(slices.Clone(set.LabelIds), func(l uuid.UUID) bool { return l == id })
			if err := audit.Record(ctx, tx, audit.Event{
				EntityType: "issue", Action: "update", EntityID: set.IssueID, ProjectID: &set.ProjectID, ActorID: &actor.ID,
				Changes: map[string]audit.Change{"labelIds": {From: set.LabelIds, To: after}}, At: now,
			}); err != nil {
				return err
			}
		}
		if err := tx.DeleteLabel(ctx, id); err != nil {
			return fmt.Errorf("delete label: %w", err)
		}
		return record(ctx, tx, actor, cur, "delete", nil, now)
	})
}
