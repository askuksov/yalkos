package mcpserver

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/project"
)

// summary is the short form of an issue in lists and write results.
type summary struct {
	Identifier string   `json:"identifier"`
	Title      string   `json:"title"`
	URL        string   `json:"url"`
	Project    string   `json:"project"`
	State      string   `json:"state"`
	StateType  string   `json:"stateType"`
	Priority   string   `json:"priority"`
	Type       string   `json:"type,omitempty"`
	Labels     []string `json:"labels,omitempty"`
	Assignee   string   `json:"assignee,omitempty"`
	Milestone  string   `json:"milestone,omitempty"`
	Parent     string   `json:"parent,omitempty"`
	DueDate    string   `json:"dueDate,omitempty"`
	UpdatedAt  string   `json:"updatedAt"`
}

// projectKey is the key part of an identifier KEY-N.
func projectKey(identifier string) string {
	if i := strings.LastIndex(identifier, "-"); i > 0 {
		return identifier[:i]
	}
	return identifier
}

// issueURL is the web link of an issue.
func (c *call) issueURL(identifier string) string { return c.link("/issue/" + identifier) }

// related holds what summaries need about a page of issues, loaded in batches.
type related struct {
	labels     map[uuid.UUID][]uuid.UUID
	states     map[uuid.UUID]project.State
	milestones map[uuid.UUID]string
	parents    map[uuid.UUID]issue.Issue
}

func (c *call) loadRelated(ctx context.Context, is []issue.Issue) (related, error) {
	ids := make([]uuid.UUID, len(is))
	var stateIDs, milestoneIDs, parentIDs []uuid.UUID
	for n, i := range is {
		ids[n] = i.ID
		stateIDs = append(stateIDs, i.StateID)
		if i.MilestoneID != nil {
			milestoneIDs = append(milestoneIDs, *i.MilestoneID)
		}
		if i.ParentID != nil {
			parentIDs = append(parentIDs, *i.ParentID)
		}
	}
	r := related{states: map[uuid.UUID]project.State{}, milestones: map[uuid.UUID]string{}, parents: map[uuid.UUID]issue.Issue{}}
	var err error
	if r.labels, err = c.d.Issues.LabelIDs(ctx, ids); err != nil {
		return related{}, err
	}
	states, err := c.d.Projects.StatesByIDs(ctx, stateIDs)
	if err != nil {
		return related{}, err
	}
	for _, s := range states {
		r.states[s.ID] = s
	}
	ms, err := c.d.Milestones.ByIDs(ctx, milestoneIDs)
	if err != nil {
		return related{}, err
	}
	for _, m := range ms {
		r.milestones[m.ID] = m.Name
	}
	parents, err := c.d.Issues.ByIDs(ctx, parentIDs)
	if err != nil {
		return related{}, err
	}
	for _, p := range parents {
		r.parents[p.ID] = p
	}
	if _, err := c.members(ctx); err != nil {
		return related{}, err
	}
	if _, err := c.labelIdx(ctx); err != nil {
		return related{}, err
	}
	return r, nil
}

// typeAndLabels splits label ids into the type name and the other label
// names: free labels first, then grouped ones, as list_labels shows them.
func (c *call) typeAndLabels(ids []uuid.UUID) (string, []string) {
	x := c.labelX
	var typ string
	var free, grouped []string
	for _, l := range x.all { // by position
		switch {
		case !slices.Contains(ids, l.ID):
		case x.isType[l.ID]:
			typ = l.Name
		case l.ParentID == nil:
			free = append(free, l.Name)
		}
	}
	for _, g := range x.all {
		if !g.IsGroup || g.ID == x.types.GroupID {
			continue
		}
		for _, l := range x.all {
			if l.ParentID != nil && *l.ParentID == g.ID && slices.Contains(ids, l.ID) {
				grouped = append(grouped, labelName(l, x.groups))
			}
		}
	}
	return typ, append(free, grouped...)
}

func (c *call) userName(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	for _, u := range c.users {
		if u.ID == *id {
			return u.Name
		}
	}
	return ""
}

func (c *call) summary(i issue.Issue, r related) summary {
	st := r.states[i.StateID]
	typ, labels := c.typeAndLabels(r.labels[i.ID])
	s := summary{
		Identifier: i.Identifier, Title: i.Title, URL: c.issueURL(i.Identifier), Project: projectKey(i.Identifier),
		State: st.Name, StateType: string(st.Type), Priority: priorityName(i.Priority),
		Type: typ, Labels: labels, Assignee: c.userName(i.AssigneeID),
		DueDate: dayPtr(i.DueDate), UpdatedAt: stamp(i.UpdatedAt),
	}
	if i.MilestoneID != nil {
		s.Milestone = r.milestones[*i.MilestoneID]
	}
	if i.ParentID != nil {
		s.Parent = r.parents[*i.ParentID].Identifier
	}
	return s
}

// summaries builds summaries of a page of issues with a fixed number of queries.
func (c *call) summaries(ctx context.Context, is []issue.Issue) ([]summary, error) {
	r, err := c.loadRelated(ctx, is)
	if err != nil {
		return nil, err
	}
	out := make([]summary, len(is))
	for n, i := range is {
		out[n] = c.summary(i, r)
	}
	return out, nil
}
