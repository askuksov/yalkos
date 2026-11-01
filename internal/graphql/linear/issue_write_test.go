package linear_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/domain/milestone"
)

type issuePayload struct {
	Success    bool
	LastSyncID float64 `json:"lastSyncId"`
	Issue      *issueNode
	Entity     *issueNode
}

const createIssue = `mutation($input: IssueCreateInput!) { issueCreate(input: $input) { success lastSyncId issue { ` + issueFields + ` } } }`

func (f issueFix) create(t *testing.T, input map[string]any) issuePayload {
	t.Helper()
	var out struct{ IssueCreate issuePayload }
	f.gql(t, f.ownerKey, createIssue, map[string]any{"input": input}).ok(t, &out)
	return out.IssueCreate
}

const updateIssue = `mutation($id: String!, $input: IssueUpdateInput!) { issueUpdate(id: $id, input: $input) { success lastSyncId issue { ` + issueFields + ` } } }`

func (f issueFix) update(t *testing.T, id string, input map[string]any) issuePayload {
	t.Helper()
	var out struct{ IssueUpdate issuePayload }
	f.gql(t, f.ownerKey, updateIssue, map[string]any{"id": id, "input": input}).ok(t, &out)
	return out.IssueUpdate
}

func (f issueFix) issueCount(t *testing.T) int {
	t.Helper()
	n, err := f.deps.Issues.Count(context.Background(), issue.Filter{IncludeArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (f issueFix) label(t *testing.T, name string) label.Label {
	t.Helper()
	l, err := f.deps.Labels.Create(context.Background(), f.owner, label.CreateInput{Name: name, Color: "#AABBCC"})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestIssueCreate(t *testing.T) {
	f := newIssueFix(t)
	bug := f.label(t, "Bug")
	types, _ := f.deps.Labels.TypeInfo(context.Background())
	p := f.create(t, map[string]any{
		"teamId": "YLK", "title": "New one", "priority": 1, "labelIds": []string{bug.ID.String()},
		"assigneeId": f.mia.ID.String(), "dueDate": "2026-04-01", "description": "Body",
	})
	i := p.Issue
	if !p.Success || p.LastSyncID <= 0 || i == nil || i.Identifier != "YLK-5" || i.State.Name != "Todo" || i.PriorityLabel != "Urgent" ||
		i.Assignee == nil || i.Assignee.ID != f.mia.ID.String() || i.Creator.ID != f.owner.ID.String() || *i.DueDate != "2026-04-01" ||
		*i.Description != "Body" || i.Team.Key != "YLK" {
		t.Fatalf("payload = %+v / %+v", p, i)
	}
	if !slices.Contains(i.LabelIDs, bug.ID.String()) || !slices.Contains(i.LabelIDs, types.DefaultID.String()) {
		t.Fatalf("labelIds = %v", i.LabelIDs)
	}
	got, err := f.deps.Issues.Get(context.Background(), "YLK-5")
	if err != nil || got.Title != "New one" || float64(got.UpdatedAt.UnixMilli()) != p.LastSyncID {
		t.Fatalf("stored = %+v (%v), lastSyncId %v", got, err, p.LastSyncID)
	}
	// By team UUID, with a state.
	done := stateNamed(t, f.env, f.web, "Done")
	p = f.create(t, map[string]any{"teamId": f.web.ID.String(), "title": "Web done", "stateId": done.ID.String(), "sortOrder": 7.5})
	if p.Issue.Identifier != "WEB-2" || p.Issue.State.Name != "Done" || p.Issue.CompletedAt == nil || p.Issue.SortOrder != 7.5 {
		t.Fatalf("web issue = %+v", p.Issue)
	}
}

func TestIssueCreateSubIssueAndProject(t *testing.T) {
	f := newIssueFix(t)
	ctx := context.Background()
	m, err := f.deps.Milestones.Create(ctx, f.owner, f.ylk.ID, milestone.CreateInput{Name: "Bank APIs"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.deps.Milestones.Create(ctx, f.owner, f.ylk.ID, milestone.CreateInput{Name: "Later"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.deps.Issues.Update(ctx, f.owner, f.i["YLK-1"].ID, issue.Patch{MilestoneID: milestoneOpt(m.ID)}); err != nil {
		t.Fatal(err)
	}
	child := f.create(t, map[string]any{"teamId": "YLK", "title": "Child", "parentId": "YLK-1"})
	if child.Issue.Parent == nil || child.Issue.Parent.Identifier != "YLK-1" || child.Issue.Team.Key != "YLK" {
		t.Fatalf("child = %+v", child.Issue)
	}
	got, _ := f.deps.Issues.Get(ctx, child.Issue.ID)
	if got.MilestoneID == nil || *got.MilestoneID != m.ID {
		t.Fatalf("child milestone = %v, want inherited %v", got.MilestoneID, m.ID)
	}
	own := f.create(t, map[string]any{"teamId": "YLK", "title": "Own", "parentId": f.i["YLK-1"].ID.String(), "projectId": other.ID.String()})
	got, _ = f.deps.Issues.Get(ctx, own.Issue.ID)
	if got.MilestoneID == nil || *got.MilestoneID != other.ID {
		t.Fatalf("own milestone = %v, want %v", got.MilestoneID, other.ID)
	}
	// A parent in another team is refused by the domain, in this API's words.
	f.gql(t, f.ownerKey, createIssue, map[string]any{"input": map[string]any{"teamId": "WEB", "title": "x", "parentId": "YLK-1"}}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid parentId: parent belongs to another team", "parentId")
	f.gql(t, f.ownerKey, createIssue, map[string]any{"input": map[string]any{"teamId": "YLK", "title": "x", "parentId": "YLK-99"}}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid parentId: no such issue", "parentId")
	wm, err := f.deps.Milestones.Create(ctx, f.owner, f.web.ID, milestone.CreateInput{Name: "Web M"})
	if err != nil {
		t.Fatal(err)
	}
	f.gql(t, f.ownerKey, createIssue, map[string]any{"input": map[string]any{"teamId": "YLK", "title": "x", "projectId": wm.ID.String()}}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid projectId: project belongs to another team", "projectId")
}

func milestoneOpt(id uuid.UUID) kit.Opt[uuid.UUID] { return kit.Opt[uuid.UUID]{Set: true, Value: &id} }

func TestIssueCreateMissingTitle(t *testing.T) {
	f := newIssueFix(t)
	n := f.issueCount(t)
	f.gql(t, f.ownerKey, createIssue, map[string]any{"input": map[string]any{"teamId": "YLK"}}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid title: title is required", "title")
	f.gql(t, f.ownerKey, createIssue, map[string]any{"input": map[string]any{"teamId": "NOPE", "title": "x"}}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid teamId: no such team", "teamId")
	if f.issueCount(t) != n {
		t.Fatal("an issue was created")
	}
}

func TestIssueCreateUnknownFieldRejected(t *testing.T) {
	f := newIssueFix(t)
	n := f.issueCount(t)
	for field, v := range map[string]any{"estimate": 3, "cycleId": "c", "subscriberIds": []string{}, "createdAt": "2026-01-01T00:00:00Z"} {
		g := f.gql(t, f.ownerKey, createIssue, map[string]any{"input": map[string]any{"teamId": "YLK", "title": "x", field: v}}).fails(t)
		if g.Extensions["type"] != "graphql error" || g.Message != `Field "`+field+`" is not defined by type "IssueCreateInput"` {
			t.Errorf("%s: %+v", field, g)
		}
	}
	g := f.gql(t, f.ownerKey, `mutation { issueCreate(input: { teamId: "YLK", title: "x", estimate: 3 }) { success } }`, nil).fails(t)
	if g.Extensions["type"] != "graphql error" || g.Message != `Field "estimate" is not defined by type "IssueCreateInput".` {
		t.Errorf("literal estimate: %+v", g)
	}
	if f.issueCount(t) != n {
		t.Fatal("an issue was created")
	}
}

func TestIssueCreateWrongStateMessage(t *testing.T) {
	f := newIssueFix(t)
	webTodo := stateNamed(t, f.env, f.web, "Todo")
	g := f.gql(t, f.ownerKey, createIssue, map[string]any{"input": map[string]any{"teamId": "YLK", "title": "x", "stateId": webTodo.ID.String()}}).fails(t)
	g.want(t, "INVALID_INPUT", "invalid input", "Invalid stateId: state belongs to another team", "stateId")
	if g.Extensions["reason"] != "wrong_project" {
		t.Fatalf("reason = %v", g.Extensions["reason"])
	}
	f.gql(t, f.ownerKey, createIssue, map[string]any{"input": map[string]any{"teamId": "YLK", "title": "x", "stateId": "nope"}}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid stateId: not a valid id", "stateId")
}

func TestIssueUpdateFields(t *testing.T) {
	f := newIssueFix(t)
	bug, ux := f.label(t, "Bug"), f.label(t, "UX")
	id := f.i["YLK-2"].ID.String()
	done := stateNamed(t, f.env, f.ylk, "Done")
	p := f.update(t, id, map[string]any{"title": "Renamed", "stateId": done.ID.String(), "assigneeId": nil, "dueDate": nil, "priority": 3})
	i := p.Issue
	if !p.Success || i.Title != "Renamed" || i.State.Name != "Done" || i.Assignee != nil || i.DueDate != nil || i.PriorityLabel != "Medium" ||
		i.Description == nil || *i.Description != "Export to CSV" {
		t.Fatalf("update = %+v", i)
	}
	stored, _ := f.deps.Issues.Get(context.Background(), id)
	if p.LastSyncID != float64(stored.UpdatedAt.UnixMilli()) {
		t.Fatalf("lastSyncId %v, updatedAt %v", p.LastSyncID, stored.UpdatedAt)
	}
	// labelIds replaces all labels, the type included.
	i = f.update(t, id, map[string]any{"labelIds": []string{bug.ID.String()}}).Issue
	if !slices.Equal(i.LabelIDs, []string{bug.ID.String()}) {
		t.Fatalf("labelIds = %v", i.LabelIDs)
	}
	i = f.update(t, id, map[string]any{"addedLabelIds": []string{ux.ID.String()}, "removedLabelIds": []string{bug.ID.String()}}).Issue
	if !slices.Equal(i.LabelIDs, []string{ux.ID.String()}) {
		t.Fatalf("added/removed = %v", i.LabelIDs)
	}
	// parentId sets and null clears; description "" clears.
	i = f.update(t, id, map[string]any{"parentId": "YLK-1"}).Issue
	if i.Parent == nil || i.Parent.Identifier != "YLK-1" {
		t.Fatalf("parent = %+v", i.Parent)
	}
	i = f.update(t, id, map[string]any{"parentId": nil, "description": ""}).Issue
	if i.Parent != nil || i.Description != nil {
		t.Fatalf("cleared = %+v", i)
	}
	// An empty input changes nothing.
	if i2 := f.update(t, id, map[string]any{}).Issue; i2.Title != "Renamed" {
		t.Fatalf("empty update = %+v", i2)
	}
	f.gql(t, f.ownerKey, updateIssue, map[string]any{"id": "YLK-99", "input": map[string]any{"title": "x"}}).fails(t).
		want(t, "NOT_FOUND", "invalid input", "Issue not found", "")
}

func TestIssueUpdateLabelsConflict(t *testing.T) {
	f := newIssueFix(t)
	bug := f.label(t, "Bug")
	f.gql(t, f.ownerKey, updateIssue, map[string]any{"id": "YLK-1", "input": map[string]any{
		"labelIds": []string{bug.ID.String()}, "addedLabelIds": []string{bug.ID.String()},
	}}).fails(t).want(t, "INVALID_INPUT", "invalid input", "Invalid labelIds: use either labelIds or addedLabelIds/removedLabelIds", "labelIds")
	if got := f.get(t, "YLK-1"); slices.Contains(got.LabelIDs, bug.ID.String()) {
		t.Fatal("labels changed")
	}
}

func TestIssueUpdateMove(t *testing.T) {
	f := newIssueFix(t)
	webDone := stateNamed(t, f.env, f.web, "Done")
	p := f.update(t, "YLK-2", map[string]any{"teamId": "WEB", "stateId": webDone.ID.String()})
	if p.Issue.Identifier != "WEB-2" || p.Issue.Team.Key != "WEB" || p.Issue.State.ID != webDone.ID.String() {
		t.Fatalf("moved = %+v", p.Issue)
	}
	if got := f.get(t, "YLK-2"); got.Identifier != "WEB-2" {
		t.Fatalf("old identifier → %s", got.Identifier)
	}
	// Alone: the domain maps the state.
	if p := f.update(t, "YLK-1", map[string]any{"teamId": f.web.ID.String()}); p.Issue.Team.Key != "WEB" {
		t.Fatalf("moved alone = %+v", p.Issue)
	}
	// To its own team: a plain state update.
	ylkDone := stateNamed(t, f.env, f.ylk, "Done")
	if p := f.update(t, "YLK-4", map[string]any{"teamId": "YLK", "stateId": ylkDone.ID.String()}); p.Issue.Identifier != "YLK-4" || p.Issue.State.Name != "Done" {
		t.Fatalf("same team = %+v", p.Issue)
	}
}

func TestIssueUpdateMoveWithOtherFieldsRejected(t *testing.T) {
	f := newIssueFix(t)
	msg := "Invalid teamId: move the issue in its own update (teamId with stateId only)"
	for name, extra := range map[string]any{"title": "x", "assigneeId": nil, "priority": 1, "labelIds": []string{}, "dueDate": nil, "projectId": nil, "sortOrder": 1.0} {
		f.gql(t, f.ownerKey, updateIssue, map[string]any{"id": "YLK-2", "input": map[string]any{"teamId": "WEB", name: extra}}).fails(t).
			want(t, "INVALID_INPUT", "invalid input", msg, "teamId")
	}
	got := f.get(t, "YLK-2")
	if got.Team.Key != "YLK" || got.Title != "Bank export" || got.Assignee == nil {
		t.Fatalf("issue changed: %+v", got)
	}
}

func TestIssueDelete(t *testing.T) {
	f := newIssueFix(t)
	q := `mutation($id: String!, $p: Boolean) { issueDelete(id: $id, permanentlyDelete: $p) { success lastSyncId entity { id identifier } } }`
	f.gql(t, f.ownerKey, q, map[string]any{"id": "YLK-2", "p": true}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "permanentlyDelete is not supported", "permanentlyDelete")
	f.get(t, "YLK-2")
	var out struct{ IssueDelete issuePayload }
	f.gql(t, f.ownerKey, q, map[string]any{"id": "YLK-2"}).ok(t, &out)
	if !out.IssueDelete.Success || out.IssueDelete.Entity == nil || out.IssueDelete.Entity.Identifier != "YLK-2" || out.IssueDelete.LastSyncID <= 0 {
		t.Fatalf("delete = %+v", out.IssueDelete)
	}
	f.gql(t, f.ownerKey, `{ issue(id: "YLK-2") { id } }`, nil).fails(t).want(t, "NOT_FOUND", "invalid input", "Issue not found", "")
	f.gql(t, f.ownerKey, q, map[string]any{"id": "YLK-2"}).fails(t).want(t, "NOT_FOUND", "invalid input", "Issue not found", "")
}

func TestIssueCreateByMember(t *testing.T) {
	f := newIssueFix(t)
	// Members may create issues; the audit actor is the key owner.
	var out struct{ IssueCreate issuePayload }
	f.gql(t, f.miaKey, createIssue, map[string]any{"input": map[string]any{"teamId": "YLK", "title": "By Mia"}}).ok(t, &out)
	if out.IssueCreate.Issue.Creator.ID != f.mia.ID.String() {
		t.Fatalf("creator = %+v", out.IssueCreate.Issue.Creator)
	}
}
