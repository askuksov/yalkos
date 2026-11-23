package plane

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

var stateTypes = map[string]string{
	"backlog": "backlog", "unstarted": "unstarted", "started": "started",
	"completed": "completed", "cancelled": "canceled",
}

// importable reports a state of a group Yalkos has; triage states are skipped.
func importable(s State) bool {
	_, ok := stateTypes[s.Group]
	return ok
}

var moduleStatuses = map[string]string{
	"backlog": "planned", "planned": "planned", "paused": "planned",
	"in-progress": "in_progress", "completed": "done", "cancelled": "canceled",
}

var priorities = map[string]int{"urgent": 1, "high": 2, "medium": 3, "low": 4, "none": 0}

// moduleStatus maps a Plane module status to a milestone's manual status;
// an unknown one is planned.
func moduleStatus(s string) string {
	if st, ok := moduleStatuses[s]; ok {
		return st
	}
	return "planned"
}

func issueKey(key string, n int) string { return key + "-" + strconv.Itoa(n) }

// planProject plans one project's states, milestones and issues.
func planProject(pd *ProjectData, key string, ex *Existing, members map[string]bool, lp *labelPlanner, r *Report) (ProjectPlan, error) {
	pp := ProjectPlan{Data: pd, Key: key}
	pp.Name = r.name(pd.Name, maxProjectName, func(string) string { return key + " (project name)" })
	if pp.Name == "" {
		pp.Name = key
	}
	pp.Target, _ = ex.mapped(kindProject, pd.ID)
	for _, id := range pd.MemberIDs {
		if members[id] && !slices.Contains(pp.Members, id) {
			pp.Members = append(pp.Members, id)
		}
	}
	rep := ProjectReport{Key: key}
	if pp.Target != uuid.Nil && ex.Archived[pp.Target] {
		skipArchived(&pp, ex, &rep, r)
		return pp, nil
	}

	if err := planStates(&pp, ex, &rep, r); err != nil {
		return ProjectPlan{}, err
	}
	modules := planMilestones(&pp, ex, &rep, r)
	planIssues(&pp, ex, modules, lp, &rep, r)

	r.Projects = append(r.Projects, rep)
	return pp, nil
}

// skipArchived plans nothing new for a project archived in Yalkos since an
// earlier import: no state, milestone or issue. Its new milestones and issues
// are counted as skipped. Its changed issues are still listed: one moved into
// a writable project gets its new comments there, and nameChanged drops the
// ones still in an archived project.
func skipArchived(pp *ProjectPlan, ex *Existing, rep *ProjectReport, r *Report) {
	r.ArchivedInYalkos = append(r.ArchivedInYalkos, pp.Key)
	for _, s := range pp.Data.States {
		if _, ok := ex.mapped(kindState, s.ID); ok {
			rep.Existing.States++
		}
	}
	for _, m := range pp.Data.Modules {
		if _, ok := ex.mapped(kindMilestone, m.ID); ok {
			rep.Existing.Milestones++
		} else {
			rep.Skipped.Milestones++
		}
	}
	for _, it := range pp.Data.Items {
		if _, ok := ex.mapped(kindIssue, it.ID); ok {
			rep.Existing.Issues++
			addChanged(pp, ex, it, r)
		} else {
			rep.Skipped.Issues++
		}
	}
	r.Projects = append(r.Projects, *rep)
}

// addChanged lists the imported work item it as changed when Plane updated
// it since the updated_at an earlier import recorded.
func addChanged(pp *ProjectPlan, ex *Existing, it WorkItem, r *Report) {
	if rec, ok := ex.Updated[it.ID]; ok && it.UpdatedAt.After(rec) {
		key := issueKey(pp.Key, it.SequenceID)
		pp.Changed = append(pp.Changed, ChangedItem{PlaneID: it.ID, Key: key})
		r.ChangedInPlane = append(r.ChangedInPlane, ChangedIssue{Key: key})
	}
}

// planStates plans the states to create; the Plane default state becomes
// the default of a new project. Triage states (and groups Yalkos does not have)
// are skipped. In a project an earlier import created, a new Plane state
// named like an existing Yalkos state is mapped onto it; a Plane state named
// like an earlier one of the same project gets the next free name.
func planStates(pp *ProjectPlan, ex *Existing, rep *ProjectReport, r *Report) error {
	var states []State
	for _, s := range pp.Data.States {
		if importable(s) {
			states = append(states, s)
		} else {
			rep.Skipped.States++
		}
	}
	if len(states) == 0 {
		return fmt.Errorf("plane project %s has no states to import", pp.Key)
	}
	slices.SortStableFunc(states, func(a, b State) int { return cmp.Compare(a.Sequence, b.Sequence) })

	def := ""
	for _, s := range states {
		if s.Default {
			def = s.ID
			break
		}
	}
	if def == "" {
		def = states[0].ID
		for _, s := range states {
			if s.Group == "backlog" {
				def = s.ID
				break
			}
		}
	}
	existing := ex.States[pp.Target]
	// claimed are the lower-case names the project's Plane states take.
	claimed := map[string]bool{}
	nameOf := make(map[uuid.UUID]string, len(existing))
	for n, id := range existing {
		nameOf[id] = n
	}
	for _, s := range states {
		if t, ok := ex.mapped(kindState, s.ID); ok {
			if n, ok := nameOf[t]; ok {
				claimed[n] = true
			}
		}
	}
	for _, s := range states {
		if _, ok := ex.mapped(kindState, s.ID); ok {
			rep.Existing.States++
			continue
		}
		sp := StatePlan{
			PlaneID: s.ID, Color: validColor(s.Color, fallbackStateColor),
			Type: stateTypes[s.Group], Position: s.Sequence,
			// A project that already exists keeps its default.
			Default: s.ID == def && pp.Target == uuid.Nil,
		}
		sp.Name = r.name(s.Name, maxStateName, func(cut string) string { return pp.Key + ": " + cut + " (state name)" })
		lower := strings.ToLower(sp.Name)
		if id, ok := existing[lower]; ok && !claimed[lower] {
			sp.Target, sp.Default = id, false
			rep.Existing.States++
		} else {
			if claimed[lower] {
				final := freeName(sp.Name, maxStateName, func(n string) bool {
					_, inYalkos := existing[n]
					return claimed[n] || inYalkos
				})
				r.RenamedStates = append(r.RenamedStates, RenamedMilestone{Project: pp.Key, Plane: sp.Name, Yalkos: final})
				sp.Name = final
			}
			rep.Created.States++
		}
		claimed[strings.ToLower(sp.Name)] = true
		pp.States = append(pp.States, sp)
	}
	return nil
}

// planMilestones plans the milestones to create and returns the project's
// modules in Plane order (sort_order). A name taken in the project (by an
// existing milestone or an earlier module) gets the next free name.
func planMilestones(pp *ProjectPlan, ex *Existing, rep *ProjectReport, r *Report) []Module {
	modules := slices.Clone(pp.Data.Modules)
	slices.SortStableFunc(modules, func(a, b Module) int {
		return cmp.Or(cmp.Compare(a.SortOrder, b.SortOrder), a.CreatedAt.Compare(b.CreatedAt))
	})
	used := map[string]bool{}
	for n := range ex.Milestones[pp.Target] {
		used[n] = true
	}
	for _, m := range modules {
		if _, ok := ex.mapped(kindMilestone, m.ID); ok {
			rep.Existing.Milestones++
			continue
		}
		name := r.name(m.Name, maxMilestoneName, func(cut string) string { return pp.Key + ": " + cut + " (milestone name)" })
		final := freeName(name, maxMilestoneName, func(n string) bool { return used[n] })
		used[strings.ToLower(final)] = true
		if final != name {
			r.RenamedMilestones = append(r.RenamedMilestones, RenamedMilestone{Project: pp.Key, Plane: name, Yalkos: final})
		}
		if m.StartDate != nil && *m.StartDate != "" {
			r.Dropped.ModuleStartDates = append(r.Dropped.ModuleStartDates, pp.Key+": "+final)
		}
		pp.Milestones = append(pp.Milestones, MilestonePlan{Module: m, Name: final, Status: moduleStatus(m.Status)})
		rep.Created.Milestones++
	}
	return modules
}

// planIssues plans the issues to create in parent-first order and lists the
// imported ones that changed in Plane since.
func planIssues(pp *ProjectPlan, ex *Existing, modules []Module, lp *labelPlanner, rep *ProjectReport, r *Report) {
	items := slices.Clone(pp.Data.Items)
	slices.SortStableFunc(items, func(a, b WorkItem) int { return cmp.Compare(a.SequenceID, b.SequenceID) })

	states := map[string]bool{}
	for _, s := range pp.Data.States {
		if importable(s) {
			states[s.ID] = true
		}
	}
	// The modules of each issue, earliest first.
	modulesOf := map[string][]string{}
	for _, m := range modules {
		for _, id := range pp.Data.ModuleIssues[m.ID] {
			if !slices.Contains(modulesOf[id], m.ID) {
				modulesOf[id] = append(modulesOf[id], m.ID)
			}
		}
	}
	planned := map[string]bool{}
	for _, it := range items {
		if _, ok := ex.mapped(kindIssue, it.ID); !ok {
			planned[it.ID] = true
		}
	}

	var issues []IssuePlan
	for _, it := range items {
		pp.MaxNumber = max(pp.MaxNumber, it.SequenceID)
		key := issueKey(pp.Key, it.SequenceID)
		if !planned[it.ID] {
			rep.Existing.Issues++
			addChanged(pp, ex, it, r)
			continue
		}
		is := IssuePlan{Item: it, Key: key, Number: it.SequenceID, State: it.State, Priority: priorities[it.Priority]}
		if !states[it.State] {
			// No Plane state: the writer puts the issue in the project's
			// default state in Yalkos at the time it is written.
			is.State = ""
			r.StateFallback = append(r.StateFallback, key)
		}
		if it.Parent != nil && *it.Parent != "" {
			if _, mapped := ex.mapped(kindIssue, *it.Parent); mapped || planned[*it.Parent] {
				is.Parent = *it.Parent
			} else {
				r.Dropped.Parents = append(r.Dropped.Parents, key)
			}
		}
		if ms := modulesOf[it.ID]; len(ms) > 0 {
			is.Milestone = ms[0]
			if len(ms) > 1 {
				r.Dropped.ExtraModules = append(r.Dropped.ExtraModules, key)
			}
		}
		if len(it.Assignees) > 0 {
			is.Assignee = it.Assignees[0]
			if len(it.Assignees) > 1 {
				r.Dropped.ExtraAssignees = append(r.Dropped.ExtraAssignees, key)
			}
		}
		if it.StartDate != nil && *it.StartDate != "" {
			r.Dropped.StartDates = append(r.Dropped.StartDates, key)
		}
		is.Type, is.Labels = lp.forIssue(key, it.Labels)
		issues = append(issues, is)
	}
	pp.Issues = parentFirst(issues, r)
	rep.Created.Issues = len(pp.Issues)
}

// parentFirst orders issues so that each comes after its parent; a parent
// cycle is cut and reported.
func parentFirst(issues []IssuePlan, r *Report) []IssuePlan {
	at := make(map[string]int, len(issues))
	for i, is := range issues {
		at[is.Item.ID] = i
	}
	const (
		unseen = iota
		visiting
		done
	)
	mark := make([]int, len(issues))
	out := make([]IssuePlan, 0, len(issues))
	var visit func(i int)
	visit = func(i int) {
		if mark[i] != unseen {
			return
		}
		mark[i] = visiting
		if j, ok := at[issues[i].Parent]; ok && issues[i].Parent != "" {
			if mark[j] == visiting {
				issues[i].Parent = ""
				r.Dropped.Parents = append(r.Dropped.Parents, issues[i].Key)
			} else {
				visit(j)
			}
		}
		mark[i] = done
		out = append(out, issues[i])
	}
	for i := range issues {
		visit(i)
	}
	return out
}
