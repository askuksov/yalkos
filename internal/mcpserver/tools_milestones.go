package mcpserver

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/project"
)

type createMilestoneIn struct {
	Project     string `json:"project" jsonschema:"Project key, e.g. YLK"`
	Name        string `json:"name" jsonschema:"Up to 100 characters, unique in the project"`
	Description string `json:"description,omitempty" jsonschema:"Markdown"`
	TargetDate  string `json:"targetDate,omitempty" jsonschema:"YYYY-MM-DD"`
	Lead        string `json:"lead,omitempty" jsonschema:"\"me\", an email or a name"`
}

type updateMilestoneIn struct {
	Project     string  `json:"project" jsonschema:"Project key, e.g. YLK"`
	Milestone   string  `json:"milestone" jsonschema:"Name or id"`
	Name        string  `json:"name,omitempty" jsonschema:"Up to 100 characters, unique in the project"`
	Description *string `json:"description,omitempty" jsonschema:"Markdown; replaces the whole text"`
	TargetDate  string  `json:"targetDate,omitempty" jsonschema:"YYYY-MM-DD, or \"none\""`
	Lead        string  `json:"lead,omitempty" jsonschema:"\"me\", an email, a name or \"none\""`
	Status      string  `json:"status,omitempty" jsonschema:"planned, in_progress, done, canceled, or automatic to follow the issues"`
}

var milestoneStatuses = []milestone.Status{milestone.Planned, milestone.InProgress, milestone.Done, milestone.Canceled}

// parseStatus maps a status name to a patch: a pinned status, or nil for automatic.
func parseStatus(s string) (kit.Opt[milestone.Status], error) {
	if same(s, "automatic") {
		return kit.Opt[milestone.Status]{Set: true}, nil
	}
	for _, st := range milestoneStatuses {
		if same(s, string(st)) {
			return kit.Opt[milestone.Status]{Set: true, Value: &st}, nil
		}
	}
	return kit.Opt[milestone.Status]{}, errf("Unknown status %q. Use one of: planned, in_progress, done, canceled, automatic.", strings.TrimSpace(s))
}

// milestoneResult is the milestone object with fresh progress.
func (c *call) milestoneResult(ctx context.Context, p project.Project, m milestone.Milestone) (any, error) {
	prog, err := c.d.Milestones.Progress(ctx, []uuid.UUID{m.ID})
	if err != nil {
		return nil, err
	}
	return c.milestoneOut(ctx, p, m, prog[m.ID])
}

func registerMilestoneTools(srv *mcp.Server, d *Deps) {
	addTool(srv, d, &mcp.Tool{
		Name: "create_milestone", Title: "Create milestone", Annotations: writes("Create milestone", false),
		Description: "Create a milestone in a project: a goal or feature that groups issues, with an optional target date. Its status follows its issues until it is set by hand.",
	}, func(ctx context.Context, c *call, in createMilestoneIn) (any, error) {
		p, err := c.project(ctx, in.Project)
		if err != nil {
			return nil, err
		}
		ci := milestone.CreateInput{Name: in.Name, Description: in.Description}
		if given(in.TargetDate) {
			t, err := parseDate("targetDate", in.TargetDate)
			if err != nil {
				return nil, err
			}
			ci.TargetDate = &t
		}
		if given(in.Lead) {
			if ci.LeadID, err = c.person(ctx, in.Lead); err != nil {
				return nil, err
			}
		}
		m, err := c.d.Milestones.Create(ctx, c.actor, p.ID, ci)
		if err != nil {
			return nil, err
		}
		return c.milestoneResult(ctx, p, m)
	})

	addTool(srv, d, &mcp.Tool{
		Name: "update_milestone", Title: "Update milestone", Annotations: writes("Update milestone", true),
		Description: `Change a milestone of a project, found by its name or id. status planned, in_progress, done or canceled pins the status; automatic lets it follow the issues again. "none" clears targetDate or lead.`,
	}, func(ctx context.Context, c *call, in updateMilestoneIn) (any, error) {
		p, err := c.project(ctx, in.Project)
		if err != nil {
			return nil, err
		}
		m, err := c.milestoneNamed(ctx, p, in.Milestone)
		if err != nil {
			return nil, err
		}
		var pt milestone.Patch
		if given(in.Name) {
			pt.Name = &in.Name
		}
		pt.Description = in.Description
		if given(in.TargetDate) {
			pt.TargetDate = kit.Opt[time.Time]{Set: true}
			if !isNone(in.TargetDate) {
				t, err := parseDate("targetDate", in.TargetDate)
				if err != nil {
					return nil, err
				}
				pt.TargetDate.Value = &t
			}
		}
		if given(in.Lead) {
			id, err := c.person(ctx, in.Lead)
			if err != nil {
				return nil, err
			}
			pt.LeadID = optID(id)
		}
		if given(in.Status) {
			if pt.Status, err = parseStatus(in.Status); err != nil {
				return nil, err
			}
		}
		updated, err := c.d.Milestones.Update(ctx, c.actor, m.ID, pt)
		if err != nil {
			return nil, err
		}
		return c.milestoneResult(ctx, p, updated)
	})
}
