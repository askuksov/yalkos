package mcpserver

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/project"
)

const (
	maxProjectDescription   = 500
	maxMilestoneDescription = 1000
)

type listProjectsIn struct {
	IncludeArchived bool `json:"includeArchived,omitempty" jsonschema:"Also list archived projects"`
}

type projectOut struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Archived    bool   `json:"archived,omitempty"`
	URL         string `json:"url"`
}

type projectIn struct {
	Project string `json:"project" jsonschema:"Project key, e.g. YLK"`
}

type stateOut struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Default bool   `json:"default,omitempty"`
}

type listMilestonesIn struct {
	Project     string `json:"project" jsonschema:"Project key, e.g. YLK"`
	IncludeDone bool   `json:"includeDone,omitempty" jsonschema:"Also list done and canceled milestones"`
}

type progressOut struct {
	Completed int `json:"completed"`
	Total     int `json:"total"`
}

type milestoneOut struct {
	ID              string      `json:"id"`
	Name            string      `json:"name"`
	Status          string      `json:"status"`
	StatusAutomatic bool        `json:"statusAutomatic"`
	Progress        progressOut `json:"progress"`
	TargetDate      string      `json:"targetDate,omitempty"`
	Overdue         bool        `json:"overdue"`
	Lead            string      `json:"lead,omitempty"`
	Description     string      `json:"description,omitempty"`
	URL             string      `json:"url"`
}

// milestoneOut is the milestone object of list_milestones and the milestone tools.
func (c *call) milestoneOut(ctx context.Context, p project.Project, m milestone.Milestone, prog milestone.Progress) (milestoneOut, error) {
	out := milestoneOut{
		ID: m.ID.String(), Name: m.Name, Status: string(m.Status(prog)), StatusAutomatic: m.Manual == nil,
		Progress:    progressOut{Completed: prog.Completed, Total: prog.Total},
		TargetDate:  dayPtr(m.TargetDate),
		Overdue:     m.Overdue(prog, c.d.Milestones.Today()),
		Description: cutText(m.Description, maxMilestoneDescription),
		URL:         c.link("/p/" + p.Key + "/milestones/" + m.ID.String()),
	}
	if m.LeadID != nil {
		u, ok, err := c.userByID(ctx, *m.LeadID)
		if err != nil {
			return milestoneOut{}, err
		}
		if ok {
			out.Lead = u.Name
		}
	}
	return out, nil
}

type labelOut struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type groupOut struct {
	Name      string     `json:"name"`
	Exclusive bool       `json:"exclusive"`
	Labels    []labelOut `json:"labels,omitempty"`
}

type userOut struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
	Me    bool   `json:"me,omitempty"`
}

type noInput struct{}

func registerLookupTools(srv *mcp.Server, d *Deps) {
	addTool(srv, d, &mcp.Tool{
		Name: "list_projects", Title: "List projects", Annotations: readOnly("List projects"),
		Description: "List projects with their keys. Issue identifiers start with the project key (YLK-12). Archived projects are left out unless includeArchived is true.",
	}, func(ctx context.Context, c *call, in listProjectsIn) (any, error) {
		ps, err := c.allProjects(ctx, in.IncludeArchived)
		if err != nil {
			return nil, err
		}
		out := make([]projectOut, len(ps))
		for i, p := range ps {
			out[i] = projectOut{
				Key: p.Key, Name: p.Name, Description: cutText(p.Description, maxProjectDescription),
				Archived: p.ArchivedAt != nil, URL: c.link("/p/" + p.Key),
			}
		}
		slices.SortStableFunc(out, func(a, b projectOut) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
		return map[string]any{"projects": out}, nil
	})

	addTool(srv, d, &mcp.Tool{
		Name: "list_states", Title: "List states", Annotations: readOnly("List states"),
		Description: "List the workflow states of a project in board order, with their type (backlog, unstarted, started, completed, canceled). The default state is where new issues start.",
	}, func(ctx context.Context, c *call, in projectIn) (any, error) {
		p, err := c.project(ctx, in.Project)
		if err != nil {
			return nil, err
		}
		states, err := c.d.Projects.States(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		out := make([]stateOut, len(states))
		for i, s := range states {
			out[i] = stateOut{Name: s.Name, Type: string(s.Type), Default: s.IsDefault}
		}
		return struct {
			Project string     `json:"project"`
			States  []stateOut `json:"states"`
		}{p.Key, out}, nil
	})

	addTool(srv, d, &mcp.Tool{
		Name: "list_milestones", Title: "List milestones", Annotations: readOnly("List milestones"),
		Description: "List the milestones of a project in their manual order, with status, progress (completed of all non-canceled issues), target date and lead. Done and canceled milestones are left out unless includeDone is true.",
	}, func(ctx context.Context, c *call, in listMilestonesIn) (any, error) {
		p, err := c.project(ctx, in.Project)
		if err != nil {
			return nil, err
		}
		ms, err := c.d.Milestones.List(ctx, p.ID, in.IncludeDone)
		if err != nil {
			return nil, err
		}
		ids := make([]uuid.UUID, len(ms))
		for i, m := range ms {
			ids[i] = m.ID
		}
		prog, err := c.d.Milestones.Progress(ctx, ids)
		if err != nil {
			return nil, err
		}
		out := make([]milestoneOut, len(ms))
		for i, m := range ms {
			if out[i], err = c.milestoneOut(ctx, p, m, prog[m.ID]); err != nil {
				return nil, err
			}
		}
		return struct {
			Project    string         `json:"project"`
			Milestones []milestoneOut `json:"milestones"`
		}{p.Key, out}, nil
	})

	addTool(srv, d, &mcp.Tool{
		Name: "list_labels", Title: "List labels", Annotations: readOnly("List labels"),
		Description: "List issue types and labels. Each issue has at most one type. Labels can sit in groups; in an exclusive group an issue has at most one of its labels. Refer to a grouped label as Group/Name when its name alone is not unique.",
	}, func(ctx context.Context, c *call, _ noInput) (any, error) {
		x, err := c.labelIdx(ctx)
		if err != nil {
			return nil, err
		}
		types := make([]string, 0, len(x.types.LabelIDs))
		for _, l := range x.all {
			if x.isType[l.ID] {
				types = append(types, l.Name)
			}
		}
		var defaultType string
		if x.types.DefaultID != nil {
			defaultType = x.byID[*x.types.DefaultID].Name
		}
		var labels []labelOut
		var groups []groupOut
		at := map[uuid.UUID]int{}
		for _, l := range x.all {
			switch {
			case l.IsGroup && l.ID != x.types.GroupID:
				at[l.ID] = len(groups)
				groups = append(groups, groupOut{Name: l.Name, Exclusive: l.Exclusive})
			case !l.IsGroup && l.ParentID == nil:
				labels = append(labels, labelOut{Name: l.Name, Description: l.Description})
			}
		}
		for _, l := range x.all {
			if l.ParentID != nil && !x.isType[l.ID] {
				if i, ok := at[*l.ParentID]; ok {
					groups[i].Labels = append(groups[i].Labels, labelOut{Name: l.Name, Description: l.Description})
				}
			}
		}
		return struct {
			Types       []string   `json:"types"`
			DefaultType string     `json:"defaultType,omitempty"`
			Labels      []labelOut `json:"labels,omitempty"`
			Groups      []groupOut `json:"groups,omitempty"`
		}{types, defaultType, labels, groups}, nil
	})

	addTool(srv, d, &mcp.Tool{
		Name: "list_users", Title: "List people", Annotations: readOnly("List people"),
		Description: `List active people in the workspace with name, email and role. The entry with "me": true is the owner of this API key.`,
	}, func(ctx context.Context, c *call, _ noInput) (any, error) {
		us, err := c.members(ctx)
		if err != nil {
			return nil, err
		}
		active := slices.DeleteFunc(slices.Clone(us), func(u account.User) bool { return u.Disabled })
		slices.SortStableFunc(active, func(a, b account.User) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
		out := make([]userOut, len(active))
		for i, u := range active {
			out[i] = userOut{Name: u.Name, Email: u.Email, Role: string(u.Role), Me: u.ID == c.actor.ID}
		}
		return map[string]any{"users": out}, nil
	})
}
