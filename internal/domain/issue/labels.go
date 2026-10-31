package issue

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/store/q"
)

const maxLabels = 20

func compareIDs(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) }

// resolveLabels computes an issue's new label set: set replaces it, otherwise
// current + add - remove. Adding a label of an exclusive group drops the other
// label of that group; one input naming two labels of an exclusive group, a
// group, or an unknown id is an error. labels holds every referenced label
// and its group. The result is sorted and has no duplicates.
func resolveLabels(current, set, add, remove []uuid.UUID, labels map[uuid.UUID]label.Label, replace bool) ([]uuid.UUID, error) {
	incoming := add
	if replace {
		incoming = set
	}
	groups := map[uuid.UUID]uuid.UUID{} // exclusive group -> incoming label
	for _, id := range incoming {
		l, ok := labels[id]
		if !ok {
			return nil, kit.Input("labelIds", kit.ReasonNotFound, "no such label")
		}
		if l.IsGroup {
			return nil, kit.Input("labelIds", kit.ReasonNotAllowed, "a group cannot be applied to an issue")
		}
		if g := exclusiveGroup(l, labels); g != nil {
			if other, dup := groups[*g]; dup && other != id {
				return nil, kit.Input("labelIds", kit.ReasonNotAllowed, "an issue can have only one label of this group")
			}
			groups[*g] = id
		}
	}
	var out []uuid.UUID
	if !replace {
		for _, id := range current {
			if g := exclusiveGroup(labels[id], labels); g != nil {
				if repl, ok := groups[*g]; ok && repl != id {
					continue // replaced by the incoming label of its group
				}
			}
			out = append(out, id)
		}
	}
	out = append(out, incoming...)
	out = slices.DeleteFunc(out, func(id uuid.UUID) bool { return slices.Contains(remove, id) })
	slices.SortFunc(out, compareIDs)
	out = slices.Compact(out)
	if len(out) > maxLabels {
		return nil, kit.Input("labelIds", kit.ReasonTooMany, "an issue can have at most 20 labels").With("max", maxLabels)
	}
	if out == nil {
		out = []uuid.UUID{}
	}
	return out, nil
}

// exclusiveGroup returns the label's group when that group is exclusive.
func exclusiveGroup(l label.Label, labels map[uuid.UUID]label.Label) *uuid.UUID {
	if l.ParentID == nil {
		return nil
	}
	if g, ok := labels[*l.ParentID]; ok && g.Exclusive {
		return &g.ID
	}
	return nil
}

// lockLabels share-locks the labels (and their groups) referenced by ids.
func lockLabels(ctx context.Context, tx *q.Queries, ids []uuid.UUID) (map[uuid.UUID]label.Label, error) {
	rows, err := tx.LockLabelsForShare(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("lock labels: %w", err)
	}
	out := make(map[uuid.UUID]label.Label, len(rows))
	for _, r := range rows {
		out[r.ID] = label.FromRow(r)
	}
	return out, nil
}

// issueLabelIDs reads the sorted label ids of each issue.
func issueLabelIDs(ctx context.Context, db *q.Queries, ids []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	rows, err := db.IssueLabelIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("issue labels: %w", err)
	}
	out := make(map[uuid.UUID][]uuid.UUID, len(ids))
	for _, id := range ids {
		out[id] = []uuid.UUID{}
	}
	for _, r := range rows {
		out[r.IssueID] = append(out[r.IssueID], r.LabelID)
	}
	return out, nil
}

// LabelIDs returns the sorted label ids of each issue (empty for none).
func (s *Service) LabelIDs(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	return issueLabelIDs(ctx, s.q, issueIDs)
}

// writeLabels replaces the issue's label rows (caller holds the issue lock).
func writeLabels(ctx context.Context, tx *q.Queries, issueID uuid.UUID, ids []uuid.UUID) error {
	if err := tx.DeleteIssueLabels(ctx, issueID); err != nil {
		return fmt.Errorf("delete issue labels: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}
	if err := tx.InsertIssueLabels(ctx, q.InsertIssueLabelsParams{IssueID: issueID, LabelIds: ids}); err != nil {
		return fmt.Errorf("insert issue labels: %w", err)
	}
	return nil
}

// createLabels resolves a new issue's labels and adds the default type when
// none of them is a type.
func createLabels(ctx context.Context, tx *q.Queries, in []uuid.UUID) ([]uuid.UUID, error) {
	ti, err := label.ReadTypeInfo(ctx, tx)
	if err != nil {
		return nil, err
	}
	refs := slices.Clone(in)
	if ti.DefaultID != nil {
		refs = append(refs, *ti.DefaultID)
	}
	labels, err := lockLabels(ctx, tx, refs)
	if err != nil {
		return nil, err
	}
	out, err := resolveLabels(nil, nil, in, nil, labels, false)
	if err != nil {
		return nil, err
	}
	if ti.DefaultID == nil || slices.ContainsFunc(out, func(id uuid.UUID) bool { return slices.Contains(ti.LabelIDs, id) }) {
		return out, nil
	}
	// The default type may have been deleted after ReadTypeInfo; then it is
	// not in labels and stays off.
	if _, ok := labels[*ti.DefaultID]; !ok {
		return out, nil
	}
	return resolveLabels(out, nil, []uuid.UUID{*ti.DefaultID}, nil, labels, false)
}
