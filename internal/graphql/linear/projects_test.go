package linear_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/milestone"
)

type projectNode struct {
	ID, Name, Description, SlugID, URL, Color, State, PriorityLabel, FrequencyResolution string
	Content                                                                              *string
	TargetDate                                                                           *string
	Progress, Scope, SortOrder                                                           float64
	Priority                                                                             int
	LabelIDs                                                                             []string `json:"labelIds"`
	ScopeHistory                                                                         []float64
	Status                                                                               struct{ ID, Name, Type string }
	Lead, Creator                                                                        *struct{ ID string }
	Teams                                                                                struct{ Nodes []teamNode }
	Issues                                                                               issueConn
	StartDate, CompletedAt                                                               *string
}

type projectFix struct {
	issueFix
	bank, later, web, old milestone.Milestone
}

// newProjectFix: YLK milestones "Bank APIs" (YLK-1 done, YLK-2 in progress,
// YLK-4 canceled; lead Mia, target 2026-06-30) and "Later"; WEB "Web launch";
// OLD (archived) "Old one".
func newProjectFix(t *testing.T) projectFix {
	t.Helper()
	f := projectFix{issueFix: newIssueFix(t)}
	ctx := context.Background()
	mk := func(p uuid.UUID, in milestone.CreateInput) milestone.Milestone {
		m, err := f.deps.Milestones.Create(ctx, f.owner, p, in)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	f.bank = mk(f.ylk.ID, milestone.CreateInput{Name: "Bank APIs", Description: strings.Repeat("d", 2000), LeadID: &f.mia.ID, TargetDate: day(2026, 6, 30)})
	f.later = mk(f.ylk.ID, milestone.CreateInput{Name: "Later"})
	f.web = mk(f.issueFix.web.ID, milestone.CreateInput{Name: "Web launch"})
	old := f.project(t, "OLD", "Old")
	f.old = mk(old.ID, milestone.CreateInput{Name: "Old one"})
	if _, err := f.deps.Projects.SetArchived(ctx, f.owner, old.ID, true); err != nil {
		t.Fatal(err)
	}
	set := func(ref, state string) {
		p := issue.Patch{MilestoneID: kit.Opt[uuid.UUID]{Set: true, Value: &f.bank.ID}}
		if state != "" {
			st := stateNamed(t, f.env, f.ylk, state)
			p.StateID = &st.ID
		}
		if _, err := f.deps.Issues.Update(ctx, f.owner, f.i[ref].ID, p); err != nil {
			t.Fatal(err)
		}
	}
	set("YLK-1", "Done")
	set("YLK-2", "")
	set("YLK-4", "Canceled")
	return f
}

func projectNames(nodes []projectNode) []string {
	out := []string{}
	for _, n := range nodes {
		out = append(out, n.Name)
	}
	return out
}

func (f projectFix) projects(t *testing.T, vars map[string]any) []string {
	t.Helper()
	var out struct{ Projects struct{ Nodes []projectNode } }
	f.gql(t, f.ownerKey, `query($filter: ProjectFilter, $sort: [ProjectSortInput!], $includeArchived: Boolean) {
		projects(filter: $filter, sort: $sort, includeArchived: $includeArchived) { nodes { name } } }`, vars).ok(t, &out)
	return projectNames(out.Projects.Nodes)
}

func TestProjectsList(t *testing.T) {
	f := newProjectFix(t)
	if got := f.projects(t, nil); !slices.Equal(got, []string{"Web launch", "Later", "Bank APIs"}) {
		t.Fatalf("default = %v", got)
	}
	set(t, "includeArchived", f.projects(t, map[string]any{"includeArchived": true}), "Web launch", "Later", "Bank APIs", "Old one")
	set(t, "accessibleTeams some", f.projects(t, map[string]any{"filter": map[string]any{"accessibleTeams": map[string]any{"some": map[string]any{"key": cmp("eq", "WEB")}}}}), "Web launch")
	set(t, "accessibleTeams id", f.projects(t, map[string]any{"filter": map[string]any{"accessibleTeams": map[string]any{"id": cmp("eq", f.ylk.ID.String())}}}), "Later", "Bank APIs")
	set(t, "state started", f.projects(t, map[string]any{"filter": map[string]any{"state": cmp("eq", "started")}}), "Bank APIs")
	set(t, "state planned", f.projects(t, map[string]any{"filter": map[string]any{"state": cmp("eq", "planned")}}), "Later", "Web launch")
	set(t, "name or", f.projects(t, map[string]any{"filter": map[string]any{"or": []any{
		map[string]any{"name": cmp("eq", "Later")}, map[string]any{"id": cmp("eq", f.web.ID.String())},
	}}}), "Later", "Web launch")
	set(t, "targetDate null", f.projects(t, map[string]any{"filter": map[string]any{"targetDate": cmp("null", false)}}), "Bank APIs")
	sortBy := func(key, order string) map[string]any {
		return map[string]any{"sort": []any{map[string]any{key: map[string]any{"order": order}}}}
	}
	if got := f.projects(t, sortBy("name", "Ascending")); !slices.Equal(got, []string{"Bank APIs", "Later", "Web launch"}) {
		t.Fatalf("name asc = %v", got)
	}
	if got := f.projects(t, sortBy("targetDate", "Descending")); got[0] != "Bank APIs" {
		t.Fatalf("targetDate desc = %v", got)
	}
	if got := f.projects(t, sortBy("createdAt", "Ascending")); !slices.Equal(got, []string{"Bank APIs", "Later", "Web launch"}) {
		t.Fatalf("createdAt asc = %v", got)
	}
	if got := f.projects(t, sortBy("manual", "Ascending")); len(got) != 3 {
		t.Fatalf("manual = %v", got)
	}
}

const projectFields = `id name description content slugId url color state priority priorityLabel frequencyResolution targetDate progress scope
	sortOrder labelIds scopeHistory status { id name type } lead { id } creator { id } startDate completedAt
	teams { nodes { key } } issues { nodes { identifier } }`

func TestProjectFields(t *testing.T) {
	f := newProjectFix(t)
	var out struct{ Project projectNode }
	f.gql(t, f.ownerKey, `query($id: String!) { project(id: $id) { `+projectFields+` } }`, map[string]any{"id": f.bank.ID.String()}).ok(t, &out)
	p := out.Project
	full := strings.Repeat("d", 2000)
	if p.Name != "Bank APIs" || p.Description != full || p.Content == nil || *p.Content != full || p.SlugID != p.ID ||
		p.URL != base+"/p/YLK/milestones/"+f.bank.ID.String() || p.Color != "#6B7280" || p.State != "started" || p.Priority != 0 ||
		p.PriorityLabel != "No priority" || p.FrequencyResolution != "weekly" || p.TargetDate == nil || *p.TargetDate != "2026-06-30" ||
		p.Progress != 0.5 || p.Scope != 2 || p.LabelIDs == nil || p.ScopeHistory == nil || p.Lead == nil || p.Lead.ID != f.mia.ID.String() ||
		p.Creator == nil || p.Creator.ID != f.owner.ID.String() || p.StartDate != nil || p.CompletedAt != nil {
		t.Fatalf("project = %+v", p)
	}
	if p.Status.Name != "In Progress" || p.Status.Type != "started" || p.Status.ID != "00000000-0000-7000-8000-000000000002" {
		t.Fatalf("status = %+v", p.Status)
	}
	if got := teamKeys(p.Teams.Nodes); !slices.Equal(got, []string{"YLK"}) {
		t.Fatalf("teams = %v", got)
	}
	set(t, "issues", idents(p.Issues.Nodes), "YLK-1", "YLK-2", "YLK-4")
	f.gql(t, f.ownerKey, `query($id: String!) { project(id: $id) { id } }`, map[string]any{"id": uuid.NewString()}).fails(t).
		want(t, "NOT_FOUND", "invalid input", "Project not found", "")
	var empty struct{ Project projectNode }
	f.gql(t, f.ownerKey, `query($id: String!) { project(id: $id) { content progress scope state } }`, map[string]any{"id": f.later.ID.String()}).ok(t, &empty)
	if empty.Project.Content != nil || empty.Project.Progress != 0 || empty.Project.Scope != 0 || empty.Project.State != "planned" {
		t.Fatalf("empty project = %+v", empty.Project)
	}
}

func TestIssueProjectAndFilter(t *testing.T) {
	f := newProjectFix(t)
	var out struct {
		Issue struct {
			Project *struct{ ID, Name string }
		}
		Other struct {
			Project *struct{ ID string }
		}
	}
	f.gql(t, f.ownerKey, `{ issue(id: "YLK-1") { project { id name } } other: issue(id: "YLK-3") { project { id } } }`, nil).ok(t, &out)
	if out.Issue.Project == nil || out.Issue.Project.Name != "Bank APIs" || out.Other.Project != nil {
		t.Fatalf("issue project = %+v / %+v", out.Issue.Project, out.Other.Project)
	}
	set(t, "project name", f.list(t, filter("project", map[string]any{"name": cmp("eq", "Bank APIs")})), "YLK-1", "YLK-2", "YLK-4")
	set(t, "project id", f.list(t, filter("project", map[string]any{"id": cmp("eq", f.bank.ID.String())})), "YLK-1", "YLK-2", "YLK-4")
	set(t, "project null", f.list(t, filter("project", cmp("null", true))), "YLK-3", "WEB-1")
	set(t, "project nothing", f.list(t, filter("project", map[string]any{"name": cmp("eq", "Nope")})))
}

type projectPayload struct {
	Success    bool
	LastSyncID float64 `json:"lastSyncId"`
	Project    *projectNode
	Entity     *projectNode
}

func TestProjectCreate(t *testing.T) {
	f := newProjectFix(t)
	q := `mutation($input: ProjectCreateInput!) { projectCreate(input: $input, slackChannelName: "x") { success lastSyncId project { ` + projectFields + ` } } }`
	var out struct{ ProjectCreate projectPayload }
	f.gql(t, f.ownerKey, q, map[string]any{"input": map[string]any{
		"teamIds": []string{"YLK"}, "name": "Payments", "content": "# Scope", "targetDate": "2026-09-01", "leadId": f.mia.ID.String(),
	}}).ok(t, &out)
	p := out.ProjectCreate.Project
	if !out.ProjectCreate.Success || out.ProjectCreate.LastSyncID <= 0 || p.Name != "Payments" || p.Description != "# Scope" ||
		*p.TargetDate != "2026-09-01" || p.Lead.ID != f.mia.ID.String() || p.State != "planned" || teamKeys(p.Teams.Nodes)[0] != "YLK" {
		t.Fatalf("created = %+v", out.ProjectCreate)
	}
	f.gql(t, f.ownerKey, q, map[string]any{"input": map[string]any{"teamIds": []string{f.issueFix.web.ID.String()}, "name": "By id", "description": "d"}}).ok(t, &out)
	if out.ProjectCreate.Project.Description != "d" || teamKeys(out.ProjectCreate.Project.Teams.Nodes)[0] != "WEB" {
		t.Fatalf("by id = %+v", out.ProjectCreate.Project)
	}
	for _, ids := range [][]string{{}, {"YLK", "WEB"}} {
		f.gql(t, f.ownerKey, q, map[string]any{"input": map[string]any{"teamIds": ids, "name": "x"}}).fails(t).
			want(t, "INVALID_INPUT", "invalid input", "Invalid teamIds: a project belongs to exactly one team", "teamIds")
	}
	f.gql(t, f.ownerKey, q, map[string]any{"input": map[string]any{"teamIds": []string{"YLK"}, "name": "x", "description": "a", "content": "b"}}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid content: use either description or content", "content")
	f.gql(t, f.ownerKey, q, map[string]any{"input": map[string]any{"teamIds": []string{"YLK"}, "name": "Bank APIs"}}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid name: a project with this name exists in this team", "name")
}

func TestProjectUpdate(t *testing.T) {
	f := newProjectFix(t)
	q := `mutation($id: String!, $input: ProjectUpdateInput!) { projectUpdate(id: $id, input: $input) { success lastSyncId project { ` + projectFields + ` } } }`
	upd := func(input map[string]any) projectNode {
		t.Helper()
		var out struct{ ProjectUpdate projectPayload }
		f.gql(t, f.ownerKey, q, map[string]any{"id": f.bank.ID.String(), "input": input}).ok(t, &out)
		return *out.ProjectUpdate.Project
	}
	if p := upd(map[string]any{"statusId": "00000000-0000-7000-8000-000000000003"}); p.Status.Name != "Done" || p.State != "completed" {
		t.Fatalf("pinned done = %+v", p.Status)
	}
	if p := upd(map[string]any{"statusId": nil}); p.Status.Name != "In Progress" {
		t.Fatalf("automatic = %+v", p.Status)
	}
	if p := upd(map[string]any{"name": "Bank v2", "content": "new text", "targetDate": nil, "leadId": nil}); p.Name != "Bank v2" ||
		p.Description != "new text" || p.TargetDate != nil || p.Lead != nil {
		t.Fatalf("updated = %+v", p)
	}
	f.gql(t, f.ownerKey, q, map[string]any{"id": f.bank.ID.String(), "input": map[string]any{"description": "a", "content": "b"}}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid content: use either description or content", "content")
	f.gql(t, f.ownerKey, q, map[string]any{"id": f.bank.ID.String(), "input": map[string]any{"statusId": uuid.NewString()}}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid statusId: no such project status", "statusId")
	f.gql(t, f.ownerKey, q, map[string]any{"id": f.bank.ID.String(), "input": map[string]any{"teamIds": []string{"WEB"}}}).fails(t)
}

func TestProjectDelete(t *testing.T) {
	f := newProjectFix(t)
	var out struct{ ProjectDelete projectPayload }
	f.gql(t, f.ownerKey, `mutation($id: String!) { projectDelete(id: $id) { success lastSyncId entity { id name } } }`,
		map[string]any{"id": f.later.ID.String()}).ok(t, &out)
	if !out.ProjectDelete.Success || out.ProjectDelete.Entity == nil || out.ProjectDelete.Entity.Name != "Later" || out.ProjectDelete.LastSyncID <= 0 {
		t.Fatalf("delete = %+v", out.ProjectDelete)
	}
	f.gql(t, f.ownerKey, `query($id: String!) { project(id: $id) { id } }`, map[string]any{"id": f.later.ID.String()}).fails(t).
		want(t, "NOT_FOUND", "invalid input", "Project not found", "")
}

func TestProjectStatusByID(t *testing.T) {
	f := newProjectFix(t)
	q := `query($id: String!) { projectStatus(id: $id) { id name type color position indefinite createdAt } }`
	var out struct {
		ProjectStatus struct {
			ID, Name, Type, Color string
			Position              float64
		}
	}
	f.gql(t, f.ownerKey, q, map[string]any{"id": "00000000-0000-7000-8000-000000000003"}).ok(t, &out)
	if s := out.ProjectStatus; s.Name != "Done" || s.Type != "completed" || s.Color != "#22C55E" || s.Position != 2 {
		t.Fatalf("status = %+v", s)
	}
	// The status a project reports resolves through this lookup.
	var p struct{ Project projectNode }
	f.gql(t, f.ownerKey, `query($id: String!) { project(id: $id) { status { id } } }`, map[string]any{"id": f.bank.ID.String()}).ok(t, &p)
	f.gql(t, f.ownerKey, q, map[string]any{"id": p.Project.Status.ID}).ok(t, &out)
	if out.ProjectStatus.Name != "In Progress" {
		t.Fatalf("project's status = %+v", out.ProjectStatus)
	}
	for _, id := range []string{uuid.NewString(), "nope"} {
		f.gql(t, f.ownerKey, q, map[string]any{"id": id}).fails(t).want(t, "NOT_FOUND", "invalid input", "ProjectStatus not found", "")
	}
}
