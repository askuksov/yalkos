package mcpserver

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/project"
)

type createIssueIn struct {
	Project     string   `json:"project,omitempty" jsonschema:"Project key, e.g. YLK; not needed when parent is given"`
	Title       string   `json:"title" jsonschema:"One line, up to 255 characters"`
	Description string   `json:"description,omitempty" jsonschema:"Markdown"`
	State       string   `json:"state,omitempty" jsonschema:"State name of the project, e.g. In Progress"`
	Priority    string   `json:"priority,omitempty" jsonschema:"urgent, high, medium, low or none"`
	Assignee    string   `json:"assignee,omitempty" jsonschema:"\"me\", an email or a name"`
	Type        string   `json:"type,omitempty" jsonschema:"Type name, e.g. Bug"`
	Labels      []string `json:"labels,omitempty" jsonschema:"Label names; Group/Name for a grouped label"`
	Milestone   string   `json:"milestone,omitempty" jsonschema:"Milestone name or id"`
	Parent      string   `json:"parent,omitempty" jsonschema:"Identifier of the parent issue"`
	DueDate     string   `json:"dueDate,omitempty" jsonschema:"YYYY-MM-DD"`
}

type updateIssueIn struct {
	Issue        string   `json:"issue" jsonschema:"Identifier such as YLK-12"`
	Title        string   `json:"title,omitempty" jsonschema:"One line, up to 255 characters"`
	Description  *string  `json:"description,omitempty" jsonschema:"Markdown; replaces the whole text"`
	State        string   `json:"state,omitempty" jsonschema:"State name of the issue's project"`
	Priority     string   `json:"priority,omitempty" jsonschema:"urgent, high, medium, low or none"`
	Assignee     string   `json:"assignee,omitempty" jsonschema:"\"me\", an email, a name or \"none\""`
	Type         string   `json:"type,omitempty" jsonschema:"Type name, or \"none\""`
	Labels       []string `json:"labels,omitempty" jsonschema:"Replaces all labels (the type stays)"`
	AddLabels    []string `json:"addLabels,omitempty" jsonschema:"Labels to add"`
	RemoveLabels []string `json:"removeLabels,omitempty" jsonschema:"Labels to remove"`
	Milestone    string   `json:"milestone,omitempty" jsonschema:"Milestone name or id, or \"none\""`
	Parent       string   `json:"parent,omitempty" jsonschema:"Identifier of the parent issue, or \"none\""`
	DueDate      string   `json:"dueDate,omitempty" jsonschema:"YYYY-MM-DD, or \"none\""`
}

type addCommentIn struct {
	Issue string `json:"issue" jsonschema:"Identifier such as YLK-12"`
	Body  string `json:"body" jsonschema:"Markdown, up to 100 000 characters"`
}

func given(s string) bool { return strings.TrimSpace(s) != "" }

func (c *call) projectByID(ctx context.Context, id uuid.UUID) (project.Project, error) {
	return c.d.Projects.Get(ctx, id.String())
}

func (c *call) createIssue(ctx context.Context, in createIssueIn) (summary, error) {
	for _, f := range []struct{ name, v string }{
		{"type", in.Type}, {"milestone", in.Milestone}, {"assignee", in.Assignee}, {"parent", in.Parent}, {"dueDate", in.DueDate},
	} {
		if isNone(f.v) {
			return summary{}, errf(`Leave %s out instead of "none" when creating an issue; clear it afterwards with update_issue.`, f.name)
		}
	}
	ci := issue.CreateInput{Title: in.Title, Description: in.Description}
	var p project.Project
	var err error
	switch {
	case given(in.Project):
		if p, err = c.project(ctx, in.Project); err != nil {
			return summary{}, err
		}
		ci.ProjectID = &p.ID
	case given(in.Parent):
	default:
		return summary{}, errf("project is required unless parent is given.")
	}
	if given(in.Parent) {
		parent, err := c.issue(ctx, in.Parent)
		if err != nil {
			return summary{}, err
		}
		ci.ParentID = &parent.ID
		if ci.ProjectID == nil {
			if p, err = c.projectByID(ctx, parent.ProjectID); err != nil {
				return summary{}, err
			}
		}
	}
	if given(in.State) {
		s, err := c.state(ctx, p, in.State)
		if err != nil {
			return summary{}, err
		}
		ci.StateID = &s.ID
	}
	if given(in.Priority) {
		if ci.Priority, err = parsePriority(in.Priority); err != nil {
			return summary{}, err
		}
	}
	if given(in.Assignee) {
		if ci.AssigneeID, err = c.person(ctx, in.Assignee); err != nil {
			return summary{}, err
		}
	}
	if len(in.Labels) > 0 {
		if ci.LabelIDs, err = c.labels(ctx, in.Labels); err != nil {
			return summary{}, err
		}
	}
	if given(in.Type) {
		t, err := c.typeLabel(ctx, in.Type)
		if err != nil {
			return summary{}, err
		}
		ci.LabelIDs = append(ci.LabelIDs, *t)
	}
	if given(in.Milestone) {
		m, err := c.milestone(ctx, p, in.Milestone)
		if err != nil {
			return summary{}, err
		}
		ci.MilestoneID = &m.ID
	}
	if given(in.DueDate) {
		d, err := parseDate("dueDate", in.DueDate)
		if err != nil {
			return summary{}, err
		}
		ci.DueDate = &d
	}
	i, err := c.d.Issues.Create(ctx, c.actor, ci)
	if err != nil {
		return summary{}, err
	}
	return c.oneSummary(ctx, i)
}

func (c *call) oneSummary(ctx context.Context, i issue.Issue) (summary, error) {
	s, err := c.summaries(ctx, []issue.Issue{i})
	if err != nil {
		return summary{}, err
	}
	return s[0], nil
}

// optID turns a resolved reference into a patch field; nil clears.
func optID(id *uuid.UUID) kit.Opt[uuid.UUID] { return kit.Opt[uuid.UUID]{Set: true, Value: id} }

// patch resolves every update field before anything is written.
func (c *call) patch(ctx context.Context, i issue.Issue, in updateIssueIn) (issue.Patch, error) {
	var pt issue.Patch
	if in.Labels != nil && (len(in.AddLabels) > 0 || len(in.RemoveLabels) > 0) {
		return pt, errf("Use either labels or addLabels/removeLabels.")
	}
	p, err := c.projectByID(ctx, i.ProjectID)
	if err != nil {
		return pt, err
	}
	if given(in.Title) {
		pt.Title = &in.Title
	}
	pt.Description = in.Description
	if given(in.State) {
		s, err := c.state(ctx, p, in.State)
		if err != nil {
			return pt, err
		}
		pt.StateID = &s.ID
	}
	if given(in.Priority) {
		n, err := parsePriority(in.Priority)
		if err != nil {
			return pt, err
		}
		pt.Priority = &n
	}
	if given(in.Assignee) {
		id, err := c.person(ctx, in.Assignee)
		if err != nil {
			return pt, err
		}
		pt.AssigneeID = optID(id)
	}
	// The type: current one, or the one asked for (nil for "none").
	current, err := c.typeOf(ctx, i.LabelIDs)
	if err != nil {
		return pt, err
	}
	newType, typeSet := current, false
	if given(in.Type) {
		if newType, err = c.typeLabel(ctx, in.Type); err != nil {
			return pt, err
		}
		typeSet = true
	}
	switch {
	case in.Labels != nil:
		ids, err := c.labels(ctx, in.Labels)
		if err != nil {
			return pt, err
		}
		if newType != nil {
			ids = append(ids, *newType)
		}
		pt.LabelIDs = &ids
	default:
		if len(in.AddLabels) > 0 {
			if pt.AddLabelIDs, err = c.labels(ctx, in.AddLabels); err != nil {
				return pt, err
			}
		}
		if len(in.RemoveLabels) > 0 {
			if pt.RemoveLabelIDs, err = c.labels(ctx, in.RemoveLabels); err != nil {
				return pt, err
			}
		}
		switch {
		case typeSet && newType != nil:
			pt.AddLabelIDs = append(pt.AddLabelIDs, *newType)
		case typeSet && current != nil:
			pt.RemoveLabelIDs = append(pt.RemoveLabelIDs, *current)
		}
	}
	if given(in.Milestone) {
		m, err := c.milestone(ctx, p, in.Milestone)
		if err != nil {
			return pt, err
		}
		var id *uuid.UUID
		if m != nil {
			id = &m.ID
		}
		pt.MilestoneID = optID(id)
	}
	if given(in.Parent) {
		var id *uuid.UUID
		if !isNone(in.Parent) {
			parent, err := c.issue(ctx, in.Parent)
			if err != nil {
				return pt, err
			}
			id = &parent.ID
		}
		pt.ParentID = optID(id)
	}
	if given(in.DueDate) {
		pt.DueDate = kit.Opt[time.Time]{Set: true}
		if !isNone(in.DueDate) {
			d, err := parseDate("dueDate", in.DueDate)
			if err != nil {
				return pt, err
			}
			pt.DueDate.Value = &d
		}
	}
	return pt, nil
}

func registerWriteTools(srv *mcp.Server, d *Deps) {
	addTool(srv, d, &mcp.Tool{
		Name: "create_issue", Title: "Create issue", Annotations: writes("Create issue", false),
		Description: "Create an issue. project is required unless parent is given: a sub-issue goes to its parent's project and gets the parent's milestone unless milestone is set. Without state the issue starts in the project's default state; without type it gets the default type. Returns the new issue with its identifier.",
	}, func(ctx context.Context, c *call, in createIssueIn) (any, error) {
		return c.createIssue(ctx, in)
	})

	addTool(srv, d, &mcp.Tool{
		Name: "update_issue", Title: "Update issue", Annotations: writes("Update issue", true),
		Description: `Change an issue. Only the fields you pass change; "none" clears assignee, type, milestone, parent or dueDate. description replaces the whole text. labels replaces all labels (the type stays); addLabels and removeLabels change only those. state is a state name of the issue's project.`,
	}, func(ctx context.Context, c *call, in updateIssueIn) (any, error) {
		i, err := c.issue(ctx, in.Issue)
		if err != nil {
			return nil, err
		}
		pt, err := c.patch(ctx, i, in)
		if err != nil {
			return nil, err
		}
		if i, err = c.d.Issues.Update(ctx, c.actor, i.ID, pt); err != nil {
			return nil, err
		}
		return c.oneSummary(ctx, i)
	})

	addTool(srv, d, &mcp.Tool{
		Name: "add_comment", Title: "Add comment", Annotations: writes("Add comment", false),
		Description: "Add a Markdown comment to an issue.",
	}, func(ctx context.Context, c *call, in addCommentIn) (any, error) {
		i, err := c.issue(ctx, in.Issue)
		if err != nil {
			return nil, err
		}
		cm, err := c.d.Comments.Create(ctx, c.actor, i.ID, in.Body, nil)
		if err != nil {
			return nil, err
		}
		return struct {
			ID        string `json:"id"`
			Issue     string `json:"issue"`
			URL       string `json:"url"`
			CreatedAt string `json:"createdAt"`
		}{cm.ID.String(), i.Identifier, c.issueURL(i.Identifier), stamp(cm.CreatedAt)}, nil
	})
}
