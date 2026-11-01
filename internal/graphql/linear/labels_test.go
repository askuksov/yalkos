package linear_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
)

type labelNode struct {
	ID          string
	Name        string
	Color       string
	Description *string
	IsGroup     bool
	GroupType   *string
	Parent      *struct{ ID, Name string }
	Team        *struct{ ID string }
	Creator     *struct{ ID string }
	CreatedAt   string
	UpdatedAt   string
}

func labelNames(nodes []labelNode) []string {
	out := []string{}
	for _, n := range nodes {
		out = append(out, n.Name)
	}
	return out
}

const labelFields = `id name color description isGroup groupType parent { id name } team { id } creator { id } createdAt updatedAt`

func TestIssueLabelsList(t *testing.T) {
	f := newIssueFix(t)
	types, _ := f.deps.Labels.TypeInfo(context.Background())
	free := f.label(t, "Backend")
	q := `query($filter: IssueLabelFilter) { issueLabels(filter: $filter) { nodes { ` + labelFields + ` } } }`
	list := func(vars map[string]any) []labelNode {
		var out struct{ IssueLabels struct{ Nodes []labelNode } }
		f.gql(t, f.ownerKey, q, vars).ok(t, &out)
		return out.IssueLabels.Nodes
	}
	all := list(nil)
	set(t, "all", labelNames(all), "Type", "Bug", "Feature", "Task", "Improvement", "Backend")
	for _, l := range all {
		switch l.Name {
		case "Type":
			if !l.IsGroup || l.GroupType == nil || *l.GroupType != "singleSelect" || l.Parent != nil || l.ID != types.GroupID.String() {
				t.Fatalf("Type = %+v", l)
			}
		case "Backend":
			if l.IsGroup || l.GroupType != nil || l.Parent != nil || l.Color != "#aabbcc" || l.Creator == nil || l.Creator.ID != f.owner.ID.String() || l.ID != free.ID.String() {
				t.Fatalf("Backend = %+v", l)
			}
		default:
			if l.IsGroup || l.Parent == nil || l.Parent.Name != "Type" || l.Team != nil {
				t.Fatalf("type label = %+v", l)
			}
		}
		if l.CreatedAt == "" || l.UpdatedAt == "" {
			t.Fatalf("timestamps %+v", l)
		}
	}
	set(t, "name eq", labelNames(list(map[string]any{"filter": map[string]any{"name": cmp("eq", "Bug")}})), "Bug")
	set(t, "isGroup", labelNames(list(map[string]any{"filter": map[string]any{"isGroup": cmp("eq", true)}})), "Type")
	set(t, "parent name", labelNames(list(map[string]any{"filter": map[string]any{"parent": map[string]any{"name": cmp("eq", "Type")}}})),
		"Bug", "Feature", "Task", "Improvement")
	set(t, "or", labelNames(list(map[string]any{"filter": map[string]any{"or": []any{
		map[string]any{"name": cmp("eq", "Bug")}, map[string]any{"name": cmp("eq", "Backend")},
	}}})), "Bug", "Backend")
	var one struct{ IssueLabel labelNode }
	f.gql(t, f.ownerKey, `query($id: String!) { issueLabel(id: $id) { id name } }`, map[string]any{"id": free.ID.String()}).ok(t, &one)
	if one.IssueLabel.Name != "Backend" {
		t.Fatalf("issueLabel = %+v", one.IssueLabel)
	}
	f.gql(t, f.ownerKey, `query($id: String!) { issueLabel(id: $id) { id } }`, map[string]any{"id": f.ylk.ID.String()}).fails(t).
		want(t, "NOT_FOUND", "invalid input", "IssueLabel not found", "")
}

func TestIssueAndTeamLabels(t *testing.T) {
	f := newIssueFix(t)
	bug := f.label(t, "Backend")
	if _, err := f.deps.Issues.Update(context.Background(), f.owner, f.i["YLK-1"].ID, issue.Patch{AddLabelIDs: []uuid.UUID{bug.ID}}); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Issue struct{ Labels, Named struct{ Nodes []labelNode } }
		Team  struct{ Labels struct{ Nodes []labelNode } }
	}
	f.gql(t, f.ownerKey, `{ issue(id: "YLK-1") { labels { nodes { name } } named: labels(filter: { name: { eq: "Backend" } }) { nodes { name } } }
		team(id: "WEB") { labels(first: 250) { nodes { name } } } }`, nil).ok(t, &out)
	set(t, "issue labels", labelNames(out.Issue.Labels.Nodes), "Task", "Backend")
	set(t, "issue labels filtered", labelNames(out.Issue.Named.Nodes), "Backend")
	set(t, "team labels", labelNames(out.Team.Labels.Nodes), "Type", "Bug", "Feature", "Task", "Improvement", "Backend")
}

func TestIssueFilterLabels(t *testing.T) {
	f := newIssueFix(t)
	ctx := context.Background()
	be := f.label(t, "Backend")
	if _, err := f.deps.Issues.Update(ctx, f.owner, f.i["YLK-2"].ID, issue.Patch{AddLabelIDs: []uuid.UUID{be.ID}}); err != nil {
		t.Fatal(err)
	}
	f.update(t, "YLK-3", map[string]any{"labelIds": []string{}})
	set(t, "labels name", f.list(t, filter("labels", map[string]any{"name": cmp("eq", "Backend")})), "YLK-2")
	set(t, "labels some id", f.list(t, filter("labels", map[string]any{"some": map[string]any{"id": cmp("in", []string{be.ID.String()})}})), "YLK-2")
	set(t, "labels some parent", f.list(t, filter("labels", map[string]any{"some": map[string]any{"parent": map[string]any{"name": cmp("eq", "Type")}}})),
		"YLK-1", "YLK-2", "YLK-4", "WEB-1")
	set(t, "labels null", f.list(t, filter("labels", cmp("null", true))), "YLK-3")
	set(t, "labels not null", f.list(t, filter("labels", cmp("null", false))), "YLK-1", "YLK-2", "YLK-4", "WEB-1")
	set(t, "labels unknown name", f.list(t, filter("labels", map[string]any{"name": cmp("eq", "Nope")})))
	set(t, "labels in or", f.list(t, filter("or", []any{
		map[string]any{"labels": map[string]any{"name": cmp("eq", "Nope")}}, map[string]any{"number": cmp("eq", 3)},
	})), "YLK-3")
}

type labelPayload struct {
	Success    bool
	LastSyncID float64 `json:"lastSyncId"`
	IssueLabel labelNode
	EntityID   string `json:"entityId"`
}

func TestLabelCreateUpdateDelete(t *testing.T) {
	f := newIssueFix(t)
	create := `mutation($input: IssueLabelCreateInput!, $r: Boolean) { issueLabelCreate(input: $input, replaceTeamLabels: $r) { success lastSyncId issueLabel { ` + labelFields + ` } } }`
	var c struct{ IssueLabelCreate labelPayload }
	f.gql(t, f.ownerKey, create, map[string]any{"input": map[string]any{"name": "Area", "isGroup": true, "groupType": "singleSelect"}, "r": true}).ok(t, &c)
	group := c.IssueLabelCreate.IssueLabel
	if !c.IssueLabelCreate.Success || !group.IsGroup || group.GroupType == nil || *group.GroupType != "singleSelect" || c.IssueLabelCreate.LastSyncID <= 0 {
		t.Fatalf("group = %+v", c.IssueLabelCreate)
	}
	f.gql(t, f.ownerKey, create, map[string]any{"input": map[string]any{"name": "Multi", "isGroup": true, "groupType": "multiSelect"}}).ok(t, &c)
	if g := c.IssueLabelCreate.IssueLabel; g.GroupType == nil || *g.GroupType != "multiSelect" {
		t.Fatalf("multi = %+v", g)
	}
	f.gql(t, f.ownerKey, create, map[string]any{"input": map[string]any{"name": "API", "color": "#112233", "description": "api work", "parentId": group.ID}}).ok(t, &c)
	l := c.IssueLabelCreate.IssueLabel
	if l.Name != "API" || l.Color != "#112233" || *l.Description != "api work" || l.Parent == nil || l.Parent.ID != group.ID || l.GroupType != nil {
		t.Fatalf("label = %+v", l)
	}
	f.gql(t, f.ownerKey, create, map[string]any{"input": map[string]any{"name": "Plain"}}).ok(t, &c)
	if c.IssueLabelCreate.IssueLabel.Color == "" {
		t.Fatalf("default colour: %+v", c.IssueLabelCreate.IssueLabel)
	}
	update := `mutation($id: String!, $input: IssueLabelUpdateInput!) { issueLabelUpdate(id: $id, input: $input) { success lastSyncId issueLabel { ` + labelFields + ` } } }`
	var u struct{ IssueLabelUpdate labelPayload }
	f.gql(t, f.ownerKey, update, map[string]any{"id": l.ID, "input": map[string]any{"name": "Api", "parentId": nil}}).ok(t, &u)
	if u.IssueLabelUpdate.IssueLabel.Name != "Api" || u.IssueLabelUpdate.IssueLabel.Parent != nil {
		t.Fatalf("updated = %+v", u.IssueLabelUpdate.IssueLabel)
	}
	f.gql(t, f.ownerKey, update, map[string]any{"id": group.ID, "input": map[string]any{"groupType": "multiSelect"}}).ok(t, &u)
	if g := u.IssueLabelUpdate.IssueLabel.GroupType; g == nil || *g != "multiSelect" {
		t.Fatalf("groupType = %v", g)
	}
	var d struct{ IssueLabelDelete labelPayload }
	f.gql(t, f.ownerKey, `mutation($id: String!) { issueLabelDelete(id: $id) { success lastSyncId entityId } }`, map[string]any{"id": l.ID}).ok(t, &d)
	if !d.IssueLabelDelete.Success || d.IssueLabelDelete.EntityID != l.ID || d.IssueLabelDelete.LastSyncID <= 0 {
		t.Fatalf("delete = %+v", d.IssueLabelDelete)
	}
	f.gql(t, f.ownerKey, `query($id: String!) { issueLabel(id: $id) { id } }`, map[string]any{"id": l.ID}).fails(t).
		want(t, "NOT_FOUND", "invalid input", "IssueLabel not found", "")
}

func TestLabelCreateWithTeamRejected(t *testing.T) {
	f := newIssueFix(t)
	before, _ := f.deps.Labels.List(context.Background())
	f.gql(t, f.ownerKey, `mutation { issueLabelCreate(input: { name: "Team label", teamId: "YLK" }) { success } }`, nil).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid teamId: labels are workspace-wide; leave teamId out", "teamId")
	after, _ := f.deps.Labels.List(context.Background())
	if len(after) != len(before) {
		t.Fatal("a label was created")
	}
	// teamId: null is accepted.
	f.gql(t, f.ownerKey, `mutation { issueLabelCreate(input: { name: "Workspace label", teamId: null }) { success } }`, nil).ok(t, nil)
}

func TestLabelPermissionMessage(t *testing.T) {
	f := newIssueFix(t)
	types, _ := f.deps.Labels.TypeInfo(context.Background())
	f.gql(t, f.miaKey, `mutation($p: String) { issueLabelCreate(input: { name: "Chore", parentId: $p }) { success } }`,
		map[string]any{"p": types.GroupID.String()}).fails(t).want(t, "FORBIDDEN", "forbidden", "You do not have permission to do this", "")
	// Positive control: a member may create a free label.
	f.gql(t, f.miaKey, `mutation { issueLabelCreate(input: { name: "Mine" }) { success } }`, nil).ok(t, nil)
}
