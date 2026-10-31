package native_test

import (
	"testing"
)

type gqlMilestone struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Status          string `json:"status"`
	StatusAutomatic bool   `json:"statusAutomatic"`
	Overdue         bool   `json:"overdue"`
	TargetDate      *string
	Progress        struct {
		Completed int `json:"completed"`
		Total     int `json:"total"`
	} `json:"progress"`
	Lead *struct {
		ID string `json:"id"`
	} `json:"lead"`
	Project struct {
		Key string `json:"key"`
	} `json:"project"`
}

const milestoneFields = `id name status statusAutomatic overdue targetDate progress { completed total } lead { id } project { key }`

func createMilestone(t *testing.T, c *client, projectID string, in map[string]any) gqlMilestone {
	t.Helper()
	var m gqlMilestone
	c.do(`mutation($p: ID!, $in: CreateMilestoneInput!) { createMilestone(projectId: $p, input: $in) { `+milestoneFields+` } }`,
		map[string]any{"p": projectID, "in": in}).decode(t, "createMilestone", &m)
	return m
}

func getMilestone(t *testing.T, c *client, id string) gqlMilestone {
	t.Helper()
	var m gqlMilestone
	c.do(`query($id: ID!) { milestone(id: $id) { `+milestoneFields+` } }`, map[string]any{"id": id}).decode(t, "milestone", &m)
	return m
}

func updateMilestone(t *testing.T, c *client, id string, in map[string]any) gqlMilestone {
	t.Helper()
	var m gqlMilestone
	c.do(`mutation($id: ID!, $in: UpdateMilestoneInput!) { updateMilestone(id: $id, input: $in) { `+milestoneFields+` } }`,
		map[string]any{"id": id, "in": in}).decode(t, "updateMilestone", &m)
	return m
}

func TestMilestoneAPI(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	me := viewerID(t, owner)
	p := createProject(t, owner, "YLK", "Yalkos")
	m := createMilestone(t, owner, p.ID, map[string]any{"name": "Bank APIs", "targetDate": "2020-01-01", "leadId": me})
	if m.Status != "PLANNED" || !m.StatusAutomatic || !m.Overdue || m.Lead == nil || m.Lead.ID != me || m.Project.Key != "YLK" ||
		m.TargetDate == nil || *m.TargetDate != "2020-01-01" {
		t.Fatalf("created = %+v", m)
	}
	createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "a", "milestoneId": m.ID, "stateId": stateID(p, "In Progress")})
	createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "b", "milestoneId": m.ID, "stateId": stateID(p, "Done")})
	m = getMilestone(t, owner, m.ID)
	if m.Progress.Completed != 1 || m.Progress.Total != 2 || m.Status != "IN_PROGRESS" {
		t.Fatalf("with issues = %+v", m)
	}
	m = updateMilestone(t, owner, m.ID, map[string]any{"status": "CANCELED"})
	if m.Status != "CANCELED" || m.StatusAutomatic || m.Overdue {
		t.Fatalf("manual = %+v", m)
	}
	m = updateMilestone(t, owner, m.ID, map[string]any{"statusAutomatic": true, "targetDate": nil, "leadId": nil})
	if m.Status != "IN_PROGRESS" || !m.StatusAutomatic || m.TargetDate != nil || m.Lead != nil {
		t.Fatalf("automatic again = %+v", m)
	}
	r := owner.do(`mutation($id: ID!) { updateMilestone(id: $id, input: { statusAutomatic: false }) { id } }`, map[string]any{"id": m.ID})
	if r.reason() != "not_allowed" || r.field() != "statusAutomatic" {
		t.Errorf("statusAutomatic false = %s", r.raw)
	}
	var list struct {
		Milestones []gqlMilestone `json:"milestones"`
	}
	other := createMilestone(t, owner, p.ID, map[string]any{"name": "Launch"})
	owner.do(`query($id: String!) { project(id: $id) { milestones { id name } } }`, map[string]any{"id": "YLK"}).decode(t, "project", &list)
	if len(list.Milestones) != 2 || list.Milestones[0].ID != m.ID {
		t.Fatalf("project milestones = %+v", list.Milestones)
	}
	var reordered []gqlMilestone
	owner.do(`mutation($p: ID!, $ids: [ID!]!) { reorderMilestones(projectId: $p, milestoneIds: $ids) { id } }`,
		map[string]any{"p": p.ID, "ids": []string{other.ID, m.ID}}).decode(t, "reorderMilestones", &reordered)
	if len(reordered) != 2 || reordered[0].ID != other.ID {
		t.Fatalf("reorder = %+v", reordered)
	}
	var issue struct {
		Milestone *gqlMilestone `json:"milestone"`
	}
	created := createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "c", "milestoneId": other.ID})
	owner.do(`query($id: String!) { issue(id: $id) { milestone { id name } } }`, map[string]any{"id": created.ID}).decode(t, "issue", &issue)
	if issue.Milestone == nil || issue.Milestone.Name != "Launch" {
		t.Fatalf("issue milestone = %+v", issue.Milestone)
	}
	owner.do(`mutation($id: ID!) { updateIssue(id: $id, input: { milestoneId: null }) { milestone { id } } }`,
		map[string]any{"id": created.ID}).decode(t, "updateIssue", &issue)
	if issue.Milestone != nil {
		t.Errorf("cleared milestone = %+v", issue.Milestone)
	}
	member := userBrowser(t, s, owner, "MEMBER")
	if r := member.do(`mutation($id: ID!) { deleteMilestone(id: $id) }`, map[string]any{"id": m.ID}); r.code() != "FORBIDDEN" {
		t.Errorf("member deletes owner's milestone: %s", r.raw)
	}
	if r := owner.do(`mutation($id: ID!) { deleteMilestone(id: $id) }`, map[string]any{"id": m.ID}); len(r.Errors) > 0 {
		t.Fatalf("delete: %s", r.raw)
	}
	if r := owner.do(`query($id: ID!) { milestone(id: $id) { id } }`, map[string]any{"id": m.ID}); r.code() != "NOT_FOUND" {
		t.Errorf("deleted milestone: %s", r.raw)
	}
}

func TestMilestoneIssuesConnection(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	p := createProject(t, owner, "YLK", "Yalkos")
	m := createMilestone(t, owner, p.ID, map[string]any{"name": "M"})
	a := createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "a", "milestoneId": m.ID})
	createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "b", "milestoneId": m.ID, "priority": 1})
	createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "outside"})
	var got struct {
		Issues struct {
			TotalCount int `json:"totalCount"`
			Nodes      []struct {
				ID string `json:"id"`
			} `json:"nodes"`
		} `json:"issues"`
	}
	owner.do(`query($id: ID!) { milestone(id: $id) { issues(first: 1) { totalCount nodes { id } } } }`, map[string]any{"id": m.ID}).decode(t, "milestone", &got)
	if got.Issues.TotalCount != 2 || len(got.Issues.Nodes) != 1 {
		t.Fatalf("issues = %+v", got.Issues)
	}
	owner.do(`query($id: ID!) { milestone(id: $id) { issues(filter: { priority: { eq: 0 } }) { totalCount nodes { id } } } }`,
		map[string]any{"id": m.ID}).decode(t, "milestone", &got)
	if got.Issues.TotalCount != 1 || got.Issues.Nodes[0].ID != a.ID {
		t.Fatalf("filtered issues = %+v", got.Issues)
	}
	// The connection's own filter cannot widen it to other milestones.
	owner.do(`query($id: ID!) { milestone(id: $id) { issues(filter: { milestone: { null: true } }) { totalCount } } }`,
		map[string]any{"id": m.ID}).decode(t, "milestone", &got)
	if got.Issues.TotalCount != 0 {
		t.Fatalf("widened = %+v", got.Issues)
	}
}
