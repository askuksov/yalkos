package linear_test

import (
	"context"
	"testing"

	"github.com/askuksov/yalkos/internal/domain/label"
)

func labelPatchDescription(d string) label.Patch { return label.Patch{Description: &d} }

// An explicit null in an update input clears what can be empty and is an
// error for what cannot; it is never ignored.
func TestUpdateNullInputs(t *testing.T) {
	f := newProjectFix(t)
	ctx := context.Background()
	// Issues: description null clears it.
	if p := f.update(t, "YLK-2", map[string]any{"description": nil}); p.Issue.Description != nil {
		t.Fatalf("description = %v", *p.Issue.Description)
	}
	for _, field := range []string{"title", "stateId", "priority", "sortOrder", "labelIds", "teamId"} {
		f.gql(t, f.ownerKey, updateIssue, map[string]any{"id": "YLK-1", "input": map[string]any{field: nil}}).fails(t).
			want(t, "INVALID_INPUT", "invalid input", "Invalid "+field+": "+field+" cannot be null", field)
	}
	if got := f.get(t, "YLK-1"); got.Title != "Retry bank sync" || got.Priority != 2 {
		t.Fatalf("issue changed: %+v", got)
	}
	// Projects: description or content null clears; name null is an error.
	pq := `mutation($id: String!, $input: ProjectUpdateInput!) { projectUpdate(id: $id, input: $input) { project { name description content } } }`
	var po struct{ ProjectUpdate struct{ Project projectNode } }
	f.gql(t, f.ownerKey, pq, map[string]any{"id": f.bank.ID.String(), "input": map[string]any{"content": nil}}).ok(t, &po)
	if po.ProjectUpdate.Project.Description != "" || po.ProjectUpdate.Project.Content != nil {
		t.Fatalf("project = %+v", po.ProjectUpdate.Project)
	}
	f.gql(t, f.ownerKey, pq, map[string]any{"id": f.bank.ID.String(), "input": map[string]any{"name": nil}}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid name: name cannot be null", "name")
	f.gql(t, f.ownerKey, pq, map[string]any{"id": f.bank.ID.String(), "input": map[string]any{"description": nil, "content": "x"}}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid content: use either description or content", "content")
	// Labels: description null clears; name, color and groupType null are errors.
	l := f.label(t, "Backend")
	if _, err := f.deps.Labels.Update(ctx, f.owner, l.ID, labelPatchDescription("server side")); err != nil {
		t.Fatal(err)
	}
	lq := `mutation($id: String!, $input: IssueLabelUpdateInput!) { issueLabelUpdate(id: $id, input: $input) { issueLabel { name color description } } }`
	var lo struct {
		IssueLabelUpdate struct{ IssueLabel labelNode }
	}
	f.gql(t, f.ownerKey, lq, map[string]any{"id": l.ID.String(), "input": map[string]any{"description": nil}}).ok(t, &lo)
	if lo.IssueLabelUpdate.IssueLabel.Description != nil || lo.IssueLabelUpdate.IssueLabel.Name != "Backend" {
		t.Fatalf("label = %+v", lo.IssueLabelUpdate.IssueLabel)
	}
	for _, field := range []string{"name", "color", "groupType"} {
		f.gql(t, f.ownerKey, lq, map[string]any{"id": l.ID.String(), "input": map[string]any{field: nil}}).fails(t).
			want(t, "INVALID_INPUT", "invalid input", "Invalid "+field+": "+field+" cannot be null", field)
	}
	// Comments: body null is an error.
	c := f.comment(t, "YLK-1", "keep me")
	f.gql(t, f.ownerKey, `mutation($id: String!) { commentUpdate(id: $id, input: { body: null }) { success } }`, map[string]any{"id": c.ID.String()}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid body: body cannot be null", "body")
}
