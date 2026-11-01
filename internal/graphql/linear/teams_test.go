package linear_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/project"
)

type teamNode struct {
	ID                  string
	Key                 string
	Name                string
	DisplayName         string
	Description         *string
	Timezone            string
	IssueEstimationType string
	Visibility          string
	Private             bool
	CyclesEnabled       bool
	IssueCount          int
	ArchivedAt          *string
	CreatedAt           string
	DefaultIssueState   *struct{ Name string }
}

func teamKeys(nodes []teamNode) []string {
	var out []string
	for _, n := range nodes {
		out = append(out, n.Key)
	}
	return out
}

// teamsEnv: YLK, WEB and the archived OLD, created in that order.
func teamsEnv(t *testing.T) (*env, project.Project, project.Project, project.Project) {
	t.Helper()
	e := newEnv(t)
	ylk := e.project(t, "YLK", "Yalkos")
	web := e.project(t, "WEB", "Website")
	old := e.project(t, "OLD", "Old stuff")
	if _, err := e.deps.Projects.SetArchived(context.Background(), e.owner, old.ID, true); err != nil {
		t.Fatal(err)
	}
	return e, ylk, web, old
}

func TestTeamsList(t *testing.T) {
	e, _, _, _ := teamsEnv(t)
	q := `query($filter: TeamFilter, $includeArchived: Boolean) { teams(filter: $filter, includeArchived: $includeArchived) { nodes { key archivedAt } } }`
	cases := []struct {
		name string
		vars map[string]any
		want []string
	}{
		{"default", nil, []string{"WEB", "YLK"}},
		{"includeArchived", map[string]any{"includeArchived": true}, []string{"OLD", "WEB", "YLK"}},
		{"key eq", map[string]any{"filter": map[string]any{"key": map[string]any{"eq": "WEB"}}}, []string{"WEB"}},
		{"name containsIgnoreCase", map[string]any{"filter": map[string]any{"name": map[string]any{"containsIgnoreCase": "yal"}}}, []string{"YLK"}},
		{"or", map[string]any{"filter": map[string]any{"or": []any{
			map[string]any{"key": map[string]any{"eq": "WEB"}}, map[string]any{"key": map[string]any{"eq": "YLK"}},
		}}}, []string{"WEB", "YLK"}},
		{"key nothing", map[string]any{"filter": map[string]any{"key": map[string]any{"eq": "NOPE"}}}, nil},
	}
	for _, c := range cases {
		var out struct{ Teams struct{ Nodes []teamNode } }
		e.gql(t, e.ownerKey, q, c.vars).ok(t, &out)
		if got := teamKeys(out.Teams.Nodes); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
		for _, n := range out.Teams.Nodes {
			if (n.Key == "OLD") != (n.ArchivedAt != nil) {
				t.Errorf("%s: %s archivedAt %v", c.name, n.Key, n.ArchivedAt)
			}
		}
	}
}

func TestTeamByIDOrKey(t *testing.T) {
	e, ylk, _, _ := teamsEnv(t)
	q := `query($id: String!) { team(id: $id) { id key } }`
	for _, ref := range []string{ylk.ID.String(), "ylk", "YLK"} {
		var out struct{ Team teamNode }
		e.gql(t, e.ownerKey, q, map[string]any{"id": ref}).ok(t, &out)
		if out.Team.ID != ylk.ID.String() {
			t.Fatalf("%s → %+v", ref, out.Team)
		}
	}
	if _, err := e.deps.Projects.Update(context.Background(), e.owner, ylk.ID, project.UpdateInput{Key: ptr("YAL")}); err != nil {
		t.Fatal(err)
	}
	var out struct{ Team teamNode }
	e.gql(t, e.ownerKey, q, map[string]any{"id": "YLK"}).ok(t, &out)
	if out.Team.ID != ylk.ID.String() || out.Team.Key != "YAL" {
		t.Fatalf("former key → %+v", out.Team)
	}
	for _, ref := range []string{"NOPE", uuid.NewString(), "not a key!"} {
		e.gql(t, e.ownerKey, q, map[string]any{"id": ref}).fails(t).want(t, "NOT_FOUND", "invalid input", "Team not found", "")
	}
}

func ptr[T any](v T) *T { return &v }

func TestTeamFields(t *testing.T) {
	e, ylk, _, _ := teamsEnv(t)
	e.issue(t, ylk, "a")
	e.issue(t, ylk, "b")
	gone := e.issue(t, ylk, "c")
	if err := e.deps.Issues.Delete(context.Background(), e.owner, gone.ID); err != nil {
		t.Fatal(err)
	}
	var out struct{ Team teamNode }
	e.gql(t, e.ownerKey, `{ team(id: "YLK") { key name displayName description timezone issueEstimationType visibility private
		cyclesEnabled issueCount createdAt defaultIssueState { name } } }`, nil).ok(t, &out)
	tm := out.Team
	if tm.Key != "YLK" || tm.Name != "Yalkos" || tm.DisplayName != "Yalkos" || tm.Description != nil || tm.Timezone != "Etc/UTC" ||
		tm.IssueEstimationType != "notUsed" || tm.Visibility != "public" || tm.Private || tm.CyclesEnabled || tm.IssueCount != 2 ||
		tm.DefaultIssueState == nil || tm.DefaultIssueState.Name != "Todo" || tm.CreatedAt == "" {
		t.Fatalf("team = %+v", tm)
	}
}

type stateNode struct {
	ID       string
	Name     string
	Type     string
	Color    string
	Position float64
	Team     struct{ ID, Key string }
}

func TestTeamStatesAndMembers(t *testing.T) {
	e, ylk, _, _ := teamsEnv(t)
	var out struct {
		Team struct {
			States  struct{ Nodes []stateNode }
			Started struct{ Nodes []stateNode }
			Members struct{ Nodes []userNode }
		}
	}
	e.gql(t, e.ownerKey, `{ team(id: "YLK") {
		states { nodes { id name type color position team { id } } }
		started: states(filter: { type: { eq: "started" } }) { nodes { name } }
		members { nodes { id name } } } }`, nil).ok(t, &out)
	st := out.Team.States.Nodes
	if len(st) != 5 {
		t.Fatalf("states = %+v", st)
	}
	types := map[string]bool{}
	for _, s := range st {
		types[s.Type] = true
		if s.Team.ID != ylk.ID.String() || s.Color == "" || s.Name == "" {
			t.Fatalf("state = %+v", s)
		}
	}
	for _, typ := range []string{"backlog", "unstarted", "started", "completed", "canceled"} {
		if !types[typ] {
			t.Fatalf("no %s state in %+v", typ, st)
		}
	}
	if len(out.Team.Started.Nodes) != 1 || out.Team.Started.Nodes[0].Name != "In Progress" {
		t.Fatalf("started = %+v", out.Team.Started.Nodes)
	}
	if m := out.Team.Members.Nodes; len(m) != 1 || m[0].ID != e.owner.ID.String() {
		t.Fatalf("members = %+v", m)
	}
	// A disabled member shows only with includeDisabled.
	ctx := context.Background()
	d := e.user(t, "Dan", "", account.RoleMember)
	if err := e.deps.Projects.AddMember(ctx, e.owner, ylk.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.deps.Accounts.SetMemberDisabled(ctx, e.owner, d.ID, true); err != nil {
		t.Fatal(err)
	}
	var mem struct {
		Team struct {
			A, B struct{ Nodes []userNode }
		}
	}
	e.gql(t, e.ownerKey, `{ team(id: "YLK") { a: members { nodes { name } } b: members(includeDisabled: true) { nodes { name } } } }`, nil).ok(t, &mem)
	if len(mem.Team.A.Nodes) != 1 || len(mem.Team.B.Nodes) != 2 {
		t.Fatalf("members = %+v", mem.Team)
	}
}

func TestWorkflowStates(t *testing.T) {
	e, ylk, _, _ := teamsEnv(t)
	q := `query($filter: WorkflowStateFilter, $includeArchived: Boolean) { workflowStates(filter: $filter, includeArchived: $includeArchived) { nodes { id name team { key } } } }`
	count := func(vars map[string]any) []stateNode {
		var out struct{ WorkflowStates struct{ Nodes []stateNode } }
		e.gql(t, e.ownerKey, q, vars).ok(t, &out)
		return out.WorkflowStates.Nodes
	}
	if n := len(count(nil)); n != 10 {
		t.Fatalf("all states = %d, want 10 (YLK + WEB)", n)
	}
	if n := len(count(map[string]any{"includeArchived": true})); n != 15 {
		t.Fatalf("with archived = %d, want 15", n)
	}
	ylkStates := count(map[string]any{"filter": map[string]any{"team": map[string]any{"key": map[string]any{"eq": "YLK"}}}})
	if len(ylkStates) != 5 {
		t.Fatalf("YLK states = %+v", ylkStates)
	}
	for _, s := range ylkStates {
		if s.Team.Key != "YLK" {
			t.Fatalf("state %+v", s)
		}
	}
	done := count(map[string]any{"filter": map[string]any{"and": []any{
		map[string]any{"name": map[string]any{"eq": "Done"}}, map[string]any{"team": map[string]any{"id": map[string]any{"eq": ylk.ID.String()}}},
	}}})
	if len(done) != 1 {
		t.Fatalf("done = %+v", done)
	}
	var one struct{ WorkflowState stateNode }
	e.gql(t, e.ownerKey, `query($id: String!) { workflowState(id: $id) { id name team { key } } }`, map[string]any{"id": done[0].ID}).ok(t, &one)
	if one.WorkflowState.Name != "Done" || one.WorkflowState.Team.Key != "YLK" {
		t.Fatalf("workflowState = %+v", one.WorkflowState)
	}
	e.gql(t, e.ownerKey, `query($id: String!) { workflowState(id: $id) { id } }`, map[string]any{"id": uuid.NewString()}).fails(t).
		want(t, "NOT_FOUND", "invalid input", "WorkflowState not found", "")
}

func TestOrganization(t *testing.T) {
	e, ylk, _, old := teamsEnv(t)
	e.issue(t, ylk, "a")
	ctx := context.Background()
	if _, err := e.deps.Projects.SetArchived(ctx, e.owner, old.ID, false); err != nil {
		t.Fatal(err)
	}
	e.issue(t, old, "archived but live")
	if _, err := e.deps.Projects.SetArchived(ctx, e.owner, old.ID, true); err != nil {
		t.Fatal(err)
	}
	d := e.user(t, "Dan", "", account.RoleMember)
	e.user(t, "Eve", "", account.RoleMember)
	if err := e.deps.Accounts.SetMemberDisabled(ctx, e.owner, d.ID, true); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Organization struct {
			ID, Name, URLKey, ReleaseChannel, CreatedAt, UpdatedAt  string
			UserCount, CreatedIssueCount                            int
			InitiativeUpdateRemindersDay, ProjectUpdateRemindersDay string
			ProjectUpdatesReminderFrequency, SLADayCount            string
			AllowedAuthServices                                     []string
			AuthSettings                                            map[string]any
			Teams                                                   struct{ Nodes []teamNode }
		}
	}
	e.gql(t, e.ownerKey, `{ organization { id name urlKey releaseChannel createdAt updatedAt userCount createdIssueCount
		initiativeUpdateRemindersDay projectUpdateRemindersDay projectUpdatesReminderFrequency slaDayCount
		allowedAuthServices authSettings teams { nodes { key } } } }`, nil).ok(t, &out)
	o := out.Organization
	if o.Name != "Acme" || o.URLKey != "acme" || o.ReleaseChannel != "public" || o.UserCount != 2 || o.CreatedIssueCount != 2 ||
		o.InitiativeUpdateRemindersDay != "Monday" || o.ProjectUpdateRemindersDay != "Monday" || o.ProjectUpdatesReminderFrequency != "never" ||
		o.SLADayCount != "all" || o.AllowedAuthServices == nil || len(o.AllowedAuthServices) != 0 || o.AuthSettings == nil ||
		o.CreatedAt == "" || o.UpdatedAt != o.CreatedAt {
		t.Fatalf("organization = %+v", o)
	}
	if got := teamKeys(o.Teams.Nodes); !slices.Equal(got, []string{"WEB", "YLK"}) {
		t.Fatalf("teams = %v", got)
	}
}

func TestOrganizationProjectStatuses(t *testing.T) {
	e := newEnv(t)
	var out struct {
		Organization struct {
			CreatedAt       string
			ProjectStatuses []struct {
				ID, Name, Type, Color, CreatedAt, UpdatedAt string
				Position                                    float64
				Indefinite                                  bool
				Description                                 *string
			}
		}
	}
	e.gql(t, e.ownerKey, `{ organization { createdAt projectStatuses { id name type color position indefinite description createdAt updatedAt } } }`, nil).ok(t, &out)
	want := []struct{ id, name, typ, color string }{
		{"00000000-0000-7000-8000-000000000001", "Planned", "planned", "#6B7280"},
		{"00000000-0000-7000-8000-000000000002", "In Progress", "started", "#F59E0B"},
		{"00000000-0000-7000-8000-000000000003", "Done", "completed", "#22C55E"},
		{"00000000-0000-7000-8000-000000000004", "Canceled", "canceled", "#EF4444"},
	}
	ps := out.Organization.ProjectStatuses
	if len(ps) != 4 {
		t.Fatalf("statuses = %+v", ps)
	}
	for i, w := range want {
		s := ps[i]
		if s.ID != w.id || s.Name != w.name || s.Type != w.typ || s.Color != w.color || s.Position != float64(i) || s.Indefinite ||
			s.Description != nil || s.CreatedAt != out.Organization.CreatedAt || s.UpdatedAt != out.Organization.CreatedAt {
			t.Errorf("status %d = %+v", i, s)
		}
	}
}
