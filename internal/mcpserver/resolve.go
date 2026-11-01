package mcpserver

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/domain/project"
)

// Names are matched case-insensitively on trimmed input, never by prefix.
func same(a, b string) bool { return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) }

// isNone reports the keyword "none" (clears a value or matches an empty one).
func isNone(s string) bool { return same(s, "none") }

func isMe(s string) bool { return same(s, "me") }

// allProjects lists every project by name, archived ones when asked.
func (c *call) allProjects(ctx context.Context, includeArchived bool) ([]project.Project, error) {
	var out []project.Project
	size := page.Max
	var after *string
	for {
		res, err := c.d.Projects.List(ctx, includeArchived, page.Args{First: &size, After: after})
		if err != nil {
			return nil, err
		}
		out = append(out, res.Items...)
		if !res.Info.HasNext {
			return out, nil
		}
		after = res.Info.End
	}
}

// project finds a project by key (current or former) or id.
func (c *call) project(ctx context.Context, ref string) (project.Project, error) {
	ref = strings.TrimSpace(ref)
	p, err := c.d.Projects.Get(ctx, ref)
	if !errors.Is(err, kit.ErrNotFound) {
		return p, err
	}
	ps, err := c.allProjects(ctx, false)
	if err != nil {
		return project.Project{}, err
	}
	keys := make([]string, len(ps))
	for i, p := range ps {
		keys[i] = p.Key
	}
	return project.Project{}, errf("Project %s not found. Projects: %s.", ref, nameList(keys))
}

// issue finds an issue by identifier (also former ones) or id.
func (c *call) issue(ctx context.Context, ref string) (issue.Issue, error) {
	ref = strings.TrimSpace(ref)
	i, err := c.d.Issues.Get(ctx, ref)
	if errors.Is(err, kit.ErrNotFound) {
		return issue.Issue{}, errf("Issue %s not found.", ref)
	}
	return i, err
}

// state finds a state of p by name.
func (c *call) state(ctx context.Context, p project.Project, name string) (project.State, error) {
	states, err := c.d.Projects.States(ctx, p.ID)
	if err != nil {
		return project.State{}, err
	}
	names := make([]string, len(states))
	for i, s := range states {
		if same(s.Name, name) {
			return s, nil
		}
		names[i] = s.Name
	}
	return project.State{}, errf("State %q not found in project %s. States: %s.", strings.TrimSpace(name), p.Key, nameList(names))
}

// statesNamed returns the ids of the states with these names in every project.
func (c *call) statesNamed(ctx context.Context, names []string) ([]uuid.UUID, error) {
	ps, err := c.allProjects(ctx, true)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(ps))
	for i, p := range ps {
		ids[i] = p.ID
	}
	byProject, err := c.d.Projects.StatesByProjects(ctx, ids)
	if err != nil {
		return nil, err
	}
	var out []uuid.UUID
	for _, name := range names {
		found := false
		for _, states := range byProject {
			for _, s := range states {
				if same(s.Name, name) {
					out = append(out, s.ID)
					found = true
				}
			}
		}
		if !found {
			return nil, errf("No project has a state named %q.", strings.TrimSpace(name))
		}
	}
	return out, nil
}

// person resolves "me", "none" (nil), an email or a name among active users.
func (c *call) person(ctx context.Context, ref string) (*uuid.UUID, error) {
	if isNone(ref) {
		return nil, nil
	}
	if isMe(ref) {
		id := c.actor.ID
		return &id, nil
	}
	us, err := c.members(ctx)
	if err != nil {
		return nil, err
	}
	var byName []string
	var match *uuid.UUID
	for _, u := range us {
		if u.Disabled {
			continue
		}
		if same(u.Email, ref) {
			id := u.ID
			return &id, nil
		}
		if same(u.Name, ref) {
			id := u.ID
			match = &id
			byName = append(byName, u.Email)
		}
	}
	switch len(byName) {
	case 0:
		return nil, errf("No active person matches %q. Use list_users.", strings.TrimSpace(ref))
	case 1:
		return match, nil
	}
	return nil, errf("%q matches several people: %s. Use the email.", strings.TrimSpace(ref), nameList(byName))
}

// typeLabel resolves a type name, or "none" (nil).
func (c *call) typeLabel(ctx context.Context, name string) (*uuid.UUID, error) {
	if isNone(name) {
		return nil, nil
	}
	x, err := c.labelIdx(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(x.types.LabelIDs))
	for _, id := range x.types.LabelIDs {
		l := x.byID[id]
		if same(l.Name, name) {
			return &l.ID, nil
		}
		names = append(names, l.Name)
	}
	return nil, errf("Type %q not found. Types: %s.", strings.TrimSpace(name), nameList(names))
}

// typeOf returns the type label among ids, if any.
func (c *call) typeOf(ctx context.Context, ids []uuid.UUID) (*uuid.UUID, error) {
	x, err := c.labelIdx(ctx)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if x.isType[id] {
			return &id, nil
		}
	}
	return nil, nil
}

// labels resolves label names: free labels and labels of groups other than
// the type group; Group/Name picks a grouped label.
func (c *call) labels(ctx context.Context, names []string) ([]uuid.UUID, error) {
	x, err := c.labelIdx(ctx)
	if err != nil {
		return nil, err
	}
	var out []uuid.UUID
	for _, name := range names {
		id, err := c.oneLabel(x, strings.TrimSpace(name))
		if err != nil {
			return nil, err
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

func (c *call) oneLabel(x *labelIndex, name string) (uuid.UUID, error) {
	plain := func(l label.Label) bool { return !l.IsGroup && !x.isType[l.ID] }
	var hits []label.Label
	for _, l := range x.all {
		if plain(l) && same(l.Name, name) {
			hits = append(hits, l)
		}
	}
	// A free label is reachable only by its bare name; grouped ones also
	// answer to Group/Name, so the free one wins a clash.
	var free []label.Label
	for _, l := range hits {
		if l.ParentID == nil {
			free = append(free, l)
		}
	}
	if len(hits) > 1 && len(free) == 1 {
		return free[0].ID, nil
	}
	switch len(hits) {
	case 1:
		return hits[0].ID, nil
	case 0:
	default:
		forms := make([]string, len(hits))
		for i, l := range hits {
			forms[i] = labelName(l, x.groups)
		}
		return uuid.Nil, errf("Label %q is ambiguous: %s.", name, nameList(forms))
	}
	if g, n, ok := strings.Cut(name, "/"); ok {
		for _, l := range x.all {
			if plain(l) && l.ParentID != nil && same(l.Name, n) && same(x.groups[*l.ParentID].Name, g) {
				return l.ID, nil
			}
		}
	}
	for _, id := range x.types.LabelIDs {
		if same(x.byID[id].Name, name) {
			return uuid.Nil, errf("%q is a type; set it with type.", name)
		}
	}
	return uuid.Nil, errf("Label %q not found. Use list_labels.", name)
}

// milestone resolves a milestone of p by name or id, or "none" (nil).
func (c *call) milestone(ctx context.Context, p project.Project, ref string) (*milestone.Milestone, error) {
	if isNone(ref) {
		return nil, nil
	}
	return c.milestoneNamed(ctx, p, ref)
}

// milestoneNamed finds a milestone of p by name or id; "none" is a name here.
func (c *call) milestoneNamed(ctx context.Context, p project.Project, ref string) (*milestone.Milestone, error) {
	ref = strings.TrimSpace(ref)
	if id, err := uuid.Parse(ref); err == nil {
		m, err := c.d.Milestones.Get(ctx, id)
		if err == nil && m.ProjectID == p.ID {
			return &m, nil
		}
		if err != nil && !errors.Is(err, kit.ErrNotFound) {
			return nil, err
		}
	}
	ms, err := c.d.Milestones.List(ctx, p.ID, true)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(ms))
	for i, m := range ms {
		if same(m.Name, ref) {
			return &m, nil
		}
		names[i] = m.Name
	}
	return nil, errf("Milestone %q not found in project %s. Milestones: %s.", ref, p.Key, nameList(names))
}

var priorities = []string{"none", "urgent", "high", "medium", "low"}

// parsePriority maps a priority name to Yalkos 0–4.
func parsePriority(s string) (int, error) {
	for i, p := range priorities {
		if same(s, p) {
			return i, nil
		}
	}
	return 0, errf("Unknown priority %q. Use one of: urgent, high, medium, low, none.", strings.TrimSpace(s))
}

func priorityName(n int) string {
	if n < 0 || n >= len(priorities) {
		return "none"
	}
	return priorities[n]
}

// parseDate reads a strict YYYY-MM-DD date.
func parseDate(field, s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, errf("%s must be a date YYYY-MM-DD.", field)
	}
	return t, nil
}
