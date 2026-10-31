package native

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/label"
)

// Label is the GraphQL Label; parent and children resolve from the request's
// label index.
type Label struct {
	ID          string
	Name        string
	Color       string
	Description string
	IsGroup     bool
	Exclusive   bool
	Position    float64

	uid      uuid.UUID
	parentID *uuid.UUID
}

func labelOut(l label.Label) *Label {
	return &Label{
		ID: l.ID.String(), Name: l.Name, Color: l.Color, Description: l.Description, IsGroup: l.IsGroup,
		Exclusive: l.Exclusive, Position: l.Position, uid: l.ID, parentID: l.ParentID,
	}
}

func labelsOut(in []label.Label) []Label {
	out := make([]Label, 0, len(in))
	for _, l := range in {
		out = append(out, *labelOut(l))
	}
	return out
}

// LabelSettings is the GraphQL LabelSettings.
type LabelSettings struct {
	typeGroupID uuid.UUID
	defaultID   *uuid.UUID
}

// labelIndex is every label of the workspace, read once per request: labels
// are few, so one query serves all label lookups of a page.
type labelIndex struct {
	byID     map[uuid.UUID]label.Label
	children map[uuid.UUID][]label.Label
	top      []label.Label
	types    label.TypeInfo
}

type lazyLabels struct {
	once sync.Once
	idx  *labelIndex
	err  error
	load func(context.Context) (*labelIndex, error)
}

func (l *lazyLabels) get(ctx context.Context) (*labelIndex, error) {
	l.once.Do(func() { l.idx, l.err = l.load(ctx) })
	return l.idx, l.err
}

func loadLabelIndex(svc *label.Service) func(context.Context) (*labelIndex, error) {
	return func(ctx context.Context) (*labelIndex, error) {
		all, err := svc.List(ctx)
		if err != nil {
			return nil, err
		}
		types, err := svc.TypeInfo(ctx)
		if err != nil {
			return nil, err
		}
		idx := &labelIndex{byID: map[uuid.UUID]label.Label{}, children: map[uuid.UUID][]label.Label{}, types: types}
		for _, l := range all { // List is ordered by position
			idx.byID[l.ID] = l
			if l.ParentID == nil {
				idx.top = append(idx.top, l)
			} else {
				idx.children[*l.ParentID] = append(idx.children[*l.ParentID], l)
			}
		}
		return idx, nil
	}
}

func (idx *labelIndex) get(id uuid.UUID) (label.Label, error) {
	l, ok := idx.byID[id]
	if !ok {
		return label.Label{}, kit.NotFoundError{Entity: "Label"}
	}
	return l, nil
}

func (idx *labelIndex) isType(l label.Label) bool {
	return l.ParentID != nil && *l.ParentID == idx.types.GroupID
}

// issueLabels orders an issue's labels: the type first, then by name.
func (idx *labelIndex) issueLabels(ids []uuid.UUID) []label.Label {
	out := make([]label.Label, 0, len(ids))
	for _, id := range ids {
		if l, ok := idx.byID[id]; ok {
			out = append(out, l)
		}
	}
	slices.SortFunc(out, func(a, b label.Label) int {
		if ta, tb := idx.isType(a), idx.isType(b); ta != tb {
			if ta {
				return -1
			}
			return 1
		}
		return cmp.Or(strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), slices.Compare(a.ID[:], b.ID[:]))
	})
	return out
}

// issueLabels returns the issue's labels, type first: from the write that
// produced the issue, or batched per page.
func (r *Resolver) issueLabels(ctx context.Context, obj *Issue) ([]label.Label, error) {
	ids := obj.labelIDs
	if ids == nil {
		var err error
		if ids, err = loadersFrom(ctx).issueLabels.Load(ctx, obj.uid); err != nil {
			return nil, err
		}
	}
	idx, err := loadersFrom(ctx).labels.get(ctx)
	if err != nil {
		return nil, err
	}
	return idx.issueLabels(ids), nil
}

func (r *Resolver) labelSettings(ctx context.Context) (*LabelSettings, error) {
	ti, err := r.Labels.TypeInfo(ctx)
	if err != nil {
		return nil, err
	}
	return &LabelSettings{typeGroupID: ti.GroupID, defaultID: ti.DefaultID}, nil
}

// labelPatch reads labelIds (replace; null is not allowed) and add/remove lists.
func labelPatch(p *issue.Patch, in UpdateIssueInput) error {
	if ids, set := in.LabelIds.ValueOK(); set {
		if ids == nil {
			return kit.Input("labelIds", kit.ReasonRequired, "cannot be null")
		}
		parsed, err := parseIDs("labelIds", ids)
		if err != nil {
			return err
		}
		p.LabelIDs = &parsed
	}
	var err error
	if p.AddLabelIDs, err = optIDs("addLabelIds", in.AddLabelIds.Value()); err != nil {
		return err
	}
	p.RemoveLabelIDs, err = optIDs("removeLabelIds", in.RemoveLabelIds.Value())
	return err
}
