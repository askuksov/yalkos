package linear

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/domain/page"
)

// defaultLabelColor is used when a new label comes without a colour.
const defaultLabelColor = "#6B7280"

// labelOut maps a label; only groups have a group type.
func labelOut(l label.Label) *IssueLabel {
	out := &IssueLabel{
		ID: l.ID.String(), Name: l.Name, Color: l.Color, Description: nonEmpty(l.Description), IsGroup: l.IsGroup,
		CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt, Src: l,
	}
	if l.IsGroup {
		gt := LabelGroupTypeMultiSelect
		if l.Exclusive {
			gt = LabelGroupTypeSingleSelect
		}
		out.GroupType = &gt
	}
	return out
}

// workspaceLabels reads all labels once per request.
func (r *Resolver) workspaceLabels(ctx context.Context) ([]label.Label, map[uuid.UUID]label.Label, error) {
	ls, err := loadersFrom(ctx).allLabels.get(ctx, r.d.Labels.List)
	if err != nil {
		return nil, nil, err
	}
	idx := make(map[uuid.UUID]label.Label, len(ls))
	for _, l := range ls {
		idx[l.ID] = l
	}
	return ls, idx, nil
}

// labelMatch applies a label filter in memory; idx resolves parents.
func labelMatch(f *IssueLabelFilter, l label.Label, idx map[uuid.UUID]label.Label) (bool, error) {
	if f == nil {
		return true, nil
	}
	for _, sub := range f.And {
		if ok, err := labelMatch(&sub, l, idx); err != nil || !ok {
			return false, err
		}
	}
	if len(f.Or) > 0 {
		matched := false
		for _, sub := range f.Or {
			ok, err := labelMatch(&sub, l, idx)
			if err != nil {
				return false, err
			}
			matched = matched || ok
		}
		if !matched {
			return false, nil
		}
	}
	ok, err := matchID("id", f.ID, l.ID)
	if err != nil || !ok {
		return false, err
	}
	if f.Parent != nil {
		if l.ParentID == nil {
			return false, nil
		}
		if ok, err := labelMatch(f.Parent, idx[*l.ParentID], idx); err != nil || !ok {
			return false, err
		}
	}
	return matchString(f.Name, l.Name) && matchBool(f.IsGroup, l.IsGroup), nil
}

// labelIDs are the workspace labels matching f.
func (r *Resolver) labelIDs(ctx context.Context, f *IssueLabelFilter) ([]uuid.UUID, error) {
	ls, idx, err := r.workspaceLabels(ctx)
	if err != nil {
		return nil, err
	}
	out := []uuid.UUID{}
	for _, l := range ls {
		ok, err := labelMatch(f, l, idx)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, l.ID)
		}
	}
	return out, nil
}

// labelConn filters, orders and pages labels in memory.
func labelConn(ls []label.Label, idx map[uuid.UUID]label.Label, f *IssueLabelFilter, by *PaginationOrderBy, a page.Args) (*IssueLabelConnection, error) {
	var kept []label.Label
	for _, l := range ls {
		ok, err := labelMatch(f, l, idx)
		if err != nil {
			return nil, err
		}
		if ok {
			kept = append(kept, l)
		}
	}
	id := func(l label.Label) uuid.UUID { return l.ID }
	orderByTime(kept, id, func(l label.Label) time.Time { return l.CreatedAt }, func(l label.Label) time.Time { return l.UpdatedAt }, by)
	items, info, err := pageOf(kept, id, a)
	if err != nil {
		return nil, err
	}
	out := &IssueLabelConnection{PageInfo: info, Nodes: make([]IssueLabel, len(items)), Edges: make([]IssueLabelEdge, len(items))}
	for i, l := range items {
		n := labelOut(l)
		out.Nodes[i] = *n
		out.Edges[i] = IssueLabelEdge{Node: n, Cursor: cursorOf(l.ID)}
	}
	return out, nil
}

// allLabelConn lists every workspace label.
func (r *Resolver) allLabelConn(ctx context.Context, f *IssueLabelFilter, by *PaginationOrderBy, a page.Args) (*IssueLabelConnection, error) {
	if _, err := r.actor(ctx); err != nil {
		return nil, err
	}
	ls, idx, err := r.workspaceLabels(ctx)
	if err != nil {
		return nil, err
	}
	return labelConn(ls, idx, f, by, a)
}

// issueLabelConn lists the labels of one issue.
func (r *Resolver) issueLabelConn(ctx context.Context, issueLabels []string, f *IssueLabelFilter, by *PaginationOrderBy, a page.Args) (*IssueLabelConnection, error) {
	_, idx, err := r.workspaceLabels(ctx)
	if err != nil {
		return nil, err
	}
	var ls []label.Label
	for _, s := range issueLabels {
		if l, ok := idx[uuid.MustParse(s)]; ok {
			ls = append(ls, l)
		}
	}
	return labelConn(ls, idx, f, by, a)
}

func (r *Resolver) labelByID(ctx context.Context, id string) (*IssueLabel, error) {
	if _, err := r.actor(ctx); err != nil {
		return nil, err
	}
	lid, err := parseID("id", id)
	if err != nil {
		return nil, err
	}
	_, idx, err := r.workspaceLabels(ctx)
	if err != nil {
		return nil, err
	}
	l, ok := idx[lid]
	if !ok {
		return nil, kit.NotFoundError{Entity: "Label"}
	}
	return labelOut(l), nil
}

func exclusiveOf(gt *LabelGroupType) *bool {
	if gt == nil {
		return nil
	}
	ex := *gt == LabelGroupTypeSingleSelect
	return &ex
}

func labelPayload(l label.Label) *IssueLabelPayload {
	return &IssueLabelPayload{Success: true, LastSyncID: lastSync(l.UpdatedAt), IssueLabel: labelOut(l)}
}

func (r *Resolver) createLabel(ctx context.Context, in IssueLabelCreateInput) (*IssueLabelPayload, error) {
	actor, err := r.actor(ctx)
	if err != nil {
		return nil, err
	}
	if in.TeamID != nil {
		return nil, badArg("teamId", "Invalid teamId: labels are workspace-wide; leave teamId out")
	}
	ci := label.CreateInput{Name: in.Name, Color: defaultLabelColor, IsGroup: isTrue(in.IsGroup), Exclusive: exclusiveOf(in.GroupType)}
	if in.Color != nil {
		ci.Color = *in.Color
	}
	if in.Description != nil {
		ci.Description = *in.Description
	}
	if ci.ParentID, err = optID("parentId", in.ParentID); err != nil {
		return nil, err
	}
	l, err := r.d.Labels.Create(ctx, actor, ci)
	if err != nil {
		return nil, err
	}
	return labelPayload(l), nil
}

func (r *Resolver) updateLabel(ctx context.Context, id string, in IssueLabelUpdateInput) (*IssueLabelPayload, error) {
	actor, err := r.actor(ctx)
	if err != nil {
		return nil, err
	}
	lid, err := parseID("id", id)
	if err != nil {
		return nil, err
	}
	p := label.Patch{Name: in.Name, Color: in.Color, Description: in.Description, Exclusive: exclusiveOf(in.GroupType)}
	if p.ParentID, err = optPatch("parentId", in.ParentID); err != nil {
		return nil, err
	}
	l, err := r.d.Labels.Update(ctx, actor, lid, p)
	if err != nil {
		return nil, err
	}
	return labelPayload(l), nil
}

func (r *Resolver) deleteLabel(ctx context.Context, id string) (*DeletePayload, error) {
	actor, err := r.actor(ctx)
	if err != nil {
		return nil, err
	}
	lid, err := parseID("id", id)
	if err != nil {
		return nil, err
	}
	if err := r.d.Labels.Delete(ctx, actor, lid); err != nil {
		return nil, err
	}
	return &DeletePayload{Success: true, EntityID: id, LastSyncID: lastSync(r.d.now())}, nil
}
