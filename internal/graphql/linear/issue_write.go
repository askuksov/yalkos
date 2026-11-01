package linear

import (
	"context"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
)

func optID(field string, s *string) (*uuid.UUID, error) {
	if s == nil {
		return nil, nil
	}
	id, err := parseID(field, *s)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// optPatch maps an omittable id: absent keeps, null clears.
func optPatch(field string, o graphql.Omittable[*string]) (kit.Opt[uuid.UUID], error) {
	v, set := o.ValueOK()
	if !set {
		return kit.Opt[uuid.UUID]{}, nil
	}
	id, err := optID(field, v)
	return kit.Opt[uuid.UUID]{Set: true, Value: id}, err
}

// parentRef resolves a parent issue given by UUID or KEY-N.
func (r *Resolver) parentRef(ctx context.Context, ref *string) (*uuid.UUID, error) {
	if ref == nil {
		return nil, nil
	}
	p, err := r.issueByRef(ctx, "parentId", *ref)
	if err != nil {
		return nil, err
	}
	return &p.ID, nil
}

func (r *Resolver) issuePayload(i issue.Issue) *IssuePayload {
	return &IssuePayload{Success: true, LastSyncID: lastSync(i.UpdatedAt), Issue: r.issueOut(i)}
}

func (r *Resolver) createIssue(ctx context.Context, in IssueCreateInput) (*IssuePayload, error) {
	actor, err := r.actor(ctx)
	if err != nil {
		return nil, err
	}
	if in.Title == nil {
		return nil, badArg("title", "Invalid title: title is required")
	}
	p, err := r.projectByRef(ctx, "teamId", in.TeamID)
	if err != nil {
		return nil, err
	}
	ci := issue.CreateInput{ProjectID: &p.ID, Title: *in.Title, DueDate: in.DueDate, SortOrder: in.SortOrder}
	if in.Description != nil {
		ci.Description = *in.Description
	}
	if in.Priority != nil {
		ci.Priority = *in.Priority
	}
	if ci.AssigneeID, err = optID("assigneeId", in.AssigneeID); err != nil {
		return nil, err
	}
	if ci.StateID, err = optID("stateId", in.StateID); err != nil {
		return nil, err
	}
	if ci.MilestoneID, err = optID("milestoneId", in.ProjectID); err != nil {
		return nil, err
	}
	if in.LabelIds != nil {
		if ci.LabelIDs, err = parseIDs("labelIds", in.LabelIds); err != nil {
			return nil, err
		}
	}
	if ci.ParentID, err = r.parentRef(ctx, in.ParentID); err != nil {
		return nil, err
	}
	i, err := r.d.Issues.Create(ctx, actor, ci)
	if err != nil {
		return nil, err
	}
	return r.issuePayload(i), nil
}

func cannotBeNull(field string) error {
	return badArg(field, "Invalid "+field+": "+field+" cannot be null")
}

// required reads an update field that cannot be cleared: null is an error.
func required[T any](field string, o graphql.Omittable[*T]) (*T, error) {
	v, set := o.ValueOK()
	if set && v == nil {
		return nil, cannotBeNull(field)
	}
	return v, nil
}

// requiredList is required for lists.
func requiredList(field string, o graphql.Omittable[[]string]) ([]string, error) {
	v, set := o.ValueOK()
	if set && v == nil {
		return nil, cannotBeNull(field)
	}
	return v, nil
}

// clearable reads an update text: null clears it.
func clearable(o graphql.Omittable[*string]) *string {
	v, set := o.ValueOK()
	if set && v == nil {
		empty := ""
		return &empty
	}
	return v
}

// movesWithOthers reports fields other than stateId next to teamId.
func movesWithOthers(in IssueUpdateInput) bool {
	return in.Title.IsSet() || in.Description.IsSet() || in.Priority.IsSet() || in.SortOrder.IsSet() ||
		in.LabelIds.IsSet() || in.AddedLabelIds.IsSet() || in.RemovedLabelIds.IsSet() ||
		in.AssigneeID.IsSet() || in.ParentID.IsSet() || in.ProjectID.IsSet() || in.DueDate.IsSet()
}

func (r *Resolver) updateIssue(ctx context.Context, ref string, in IssueUpdateInput) (*IssuePayload, error) {
	actor, err := r.actor(ctx)
	if err != nil {
		return nil, err
	}
	title, err := required("title", in.Title)
	if err != nil {
		return nil, err
	}
	stateRef, err := required("stateId", in.StateID)
	if err != nil {
		return nil, err
	}
	priority, err := required("priority", in.Priority)
	if err != nil {
		return nil, err
	}
	sortOrder, err := required("sortOrder", in.SortOrder)
	if err != nil {
		return nil, err
	}
	teamRef, err := required("teamId", in.TeamID)
	if err != nil {
		return nil, err
	}
	labelIDs, err := requiredList("labelIds", in.LabelIds)
	if err != nil {
		return nil, err
	}
	added, err := requiredList("addedLabelIds", in.AddedLabelIds)
	if err != nil {
		return nil, err
	}
	removed, err := requiredList("removedLabelIds", in.RemovedLabelIds)
	if err != nil {
		return nil, err
	}
	if labelIDs != nil && (added != nil || removed != nil) {
		return nil, badArg("labelIds", "Invalid labelIds: use either labelIds or addedLabelIds/removedLabelIds")
	}
	cur, err := r.issueByRef(ctx, "", ref)
	if err != nil {
		return nil, err
	}
	stateID, err := optID("stateId", stateRef)
	if err != nil {
		return nil, err
	}
	if teamRef != nil {
		p, err := r.projectByRef(ctx, "teamId", *teamRef)
		if err != nil {
			return nil, err
		}
		if p.ID != cur.ProjectID {
			// A move and an update run in separate transactions: a move comes alone.
			if movesWithOthers(in) {
				return nil, badArg("teamId", "Invalid teamId: move the issue in its own update (teamId with stateId only)")
			}
			moved, err := r.d.Issues.Move(ctx, actor, cur.ID, p.ID, stateID)
			if err != nil {
				return nil, err
			}
			return r.issuePayload(moved), nil
		}
	}
	patch := issue.Patch{Title: title, Description: clearable(in.Description), StateID: stateID, Priority: priority, SortOrder: sortOrder}
	if patch.AssigneeID, err = optPatch("assigneeId", in.AssigneeID); err != nil {
		return nil, err
	}
	if patch.MilestoneID, err = optPatch("milestoneId", in.ProjectID); err != nil {
		return nil, err
	}
	if v, set := in.ParentID.ValueOK(); set {
		id, err := r.parentRef(ctx, v)
		if err != nil {
			return nil, err
		}
		patch.ParentID = kit.Opt[uuid.UUID]{Set: true, Value: id}
	}
	if v, set := in.DueDate.ValueOK(); set {
		patch.DueDate = kit.Opt[time.Time]{Set: true, Value: v}
	}
	if labelIDs != nil {
		ids, err := parseIDs("labelIds", labelIDs)
		if err != nil {
			return nil, err
		}
		patch.LabelIDs = &ids
	}
	if patch.AddLabelIDs, err = parseIDs("addedLabelIds", added); err != nil {
		return nil, err
	}
	if patch.RemoveLabelIDs, err = parseIDs("removedLabelIds", removed); err != nil {
		return nil, err
	}
	i, err := r.d.Issues.Update(ctx, actor, cur.ID, patch)
	if err != nil {
		return nil, err
	}
	return r.issuePayload(i), nil
}

func (r *Resolver) deleteIssue(ctx context.Context, ref string, permanentlyDelete *bool) (*IssueArchivePayload, error) {
	actor, err := r.actor(ctx)
	if err != nil {
		return nil, err
	}
	if permanentlyDelete != nil {
		return nil, badArg("permanentlyDelete", "permanentlyDelete is not supported")
	}
	cur, err := r.issueByRef(ctx, "", ref)
	if err != nil {
		return nil, err
	}
	if err := r.d.Issues.Delete(ctx, actor, cur.ID); err != nil {
		return nil, err
	}
	return &IssueArchivePayload{Success: true, LastSyncID: lastSync(r.d.now()), Entity: r.issueOut(cur)}, nil
}
