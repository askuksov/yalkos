package native

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/milestone"
)

// Milestone is the GraphQL Milestone; status, progress and related entities
// resolve through loaders.
type Milestone struct {
	ID              string
	Name            string
	Description     string
	TargetDate      *Date
	StatusAutomatic bool
	CreatedAt       time.Time
	UpdatedAt       time.Time

	m milestone.Milestone
}

func milestoneOut(m milestone.Milestone) *Milestone {
	out := &Milestone{
		ID: m.ID.String(), Name: m.Name, Description: m.Description, StatusAutomatic: m.Manual == nil,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt, m: m,
	}
	if m.TargetDate != nil {
		d := Date(*m.TargetDate)
		out.TargetDate = &d
	}
	return out
}

func milestonesOut(in []milestone.Milestone) []Milestone {
	out := make([]Milestone, 0, len(in))
	for _, m := range in {
		out = append(out, *milestoneOut(m))
	}
	return out
}

func milestoneStatusOut(s milestone.Status) MilestoneStatus {
	return MilestoneStatus(strings.ToUpper(string(s)))
}

func milestoneStatusIn(s MilestoneStatus) milestone.Status {
	return milestone.Status(strings.ToLower(string(s)))
}

func (m *Milestone) uid() uuid.UUID { return m.m.ID }

// statusPatch reads status/statusAutomatic: a status pins it, statusAutomatic:
// true returns to the computed one; anything else is contradictory.
func statusPatch(in UpdateMilestoneInput) (kit.Opt[milestone.Status], error) {
	st, err := notNull("status", in.Status)
	if err != nil {
		return kit.Opt[milestone.Status]{}, err
	}
	auto, err := notNull("statusAutomatic", in.StatusAutomatic)
	if err != nil {
		return kit.Opt[milestone.Status]{}, err
	}
	switch {
	case auto != nil && *auto && st != nil:
		return kit.Opt[milestone.Status]{}, kit.Input("statusAutomatic", kit.ReasonNotAllowed, "set either status or statusAutomatic")
	case auto != nil && !*auto && st == nil:
		return kit.Opt[milestone.Status]{}, kit.Input("statusAutomatic", kit.ReasonNotAllowed, "choose a status to stop automatic status")
	case st != nil:
		s := milestoneStatusIn(*st)
		return kit.Opt[milestone.Status]{Set: true, Value: &s}, nil
	case auto != nil && *auto:
		return kit.Opt[milestone.Status]{Set: true}, nil
	}
	return kit.Opt[milestone.Status]{}, nil
}

// withinMilestone ANDs the caller's milestone condition with "milestone = id",
// so a connection filter can narrow the milestone's issues but never widen them.
func withinMilestone(c *issue.IDCond, id uuid.UUID) *issue.IDCond {
	out := issue.IDCond{}
	if c != nil {
		out = *c
	}
	if out.Eq != nil && *out.Eq != id {
		out.In = []uuid.UUID{} // eq another milestone AND this one: nothing
	}
	out.Eq = &id
	return &out
}
