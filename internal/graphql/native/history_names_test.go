package native_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

type historyName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type bool   `json:"type"`
}

type namedEntry struct {
	Action  string `json:"action"`
	Changes []struct {
		Field string `json:"field"`
	} `json:"changes"`
	Names []historyName `json:"names"`
}

func namedHistory(t *testing.T, c *client, issueID string) []namedEntry {
	t.Helper()
	var got struct {
		History struct {
			Nodes []namedEntry `json:"nodes"`
		} `json:"history"`
	}
	c.do(`query($id: String!) { issue(id: $id) { history { nodes { action changes { field } names { id name type } } } } }`,
		map[string]any{"id": issueID}).decode(t, "issue", &got)
	return got.History.Nodes
}

// names renders an entry's names as "Name" or "Name(type)", sorted.
func names(e namedEntry) string {
	out := make([]string, 0, len(e.Names))
	for _, n := range e.Names {
		s := n.Name
		if n.Type {
			s += "(type)"
		}
		out = append(out, s)
	}
	slices.Sort(out)
	return strings.Join(out, ",")
}

func fields(e namedEntry) string {
	out := make([]string, 0, len(e.Changes))
	for _, c := range e.Changes {
		out = append(out, c.Field)
	}
	return strings.Join(out, ",")
}

func TestIssueHistoryKeepsNames(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	a := createProject(t, owner, "AAA", "Alpha")
	b := createProject(t, owner, "BBB", "Bravo")
	beta := createMilestone(t, owner, a.ID, map[string]any{"name": "Beta"})
	perf := createLabel(t, owner, map[string]any{"name": "Perf", "color": "#000000"})
	bug := typeIDs(t, owner)["Bug"]
	in := func(title string) map[string]any {
		return map[string]any{"projectId": a.ID, "title": title, "milestoneId": beta.ID, "labelIds": []string{perf.ID, bug}}
	}
	moved := createIssue(t, owner, in("moved"))
	stays := createIssue(t, owner, in("stays"))
	owner.do(`mutation($id: ID!) { updateIssue(id: $id, input: {priority: 2}) { id } }`, map[string]any{"id": moved.ID}).decode(t, "updateIssue", &struct{}{})
	for _, id := range []string{perf.ID, bug} {
		if r := owner.do(`mutation($id: ID!) { deleteLabel(id: $id) }`, map[string]any{"id": id}); len(r.Errors) > 0 {
			t.Fatalf("delete label: %s", r.raw)
		}
	}
	owner.do(`mutation($id: ID!, $p: ID!) { moveIssue(id: $id, projectId: $p) { id } }`,
		map[string]any{"id": moved.ID, "p": b.ID}).decode(t, "moveIssue", &struct{}{})
	if r := owner.do(`mutation($id: ID!) { deleteMilestone(id: $id) }`, map[string]any{"id": beta.ID}); len(r.Errors) > 0 {
		t.Fatalf("delete milestone: %s", r.raw)
	}

	check := func(issueID string, want []string) {
		t.Helper()
		h := namedHistory(t, owner, issueID)
		got := make([]string, 0, len(h))
		for _, e := range h {
			got = append(got, fmt.Sprintf("%s[%s]", e.Action, names(e)))
		}
		if !slices.Equal(got, want) {
			t.Fatalf("history names:\n got %v\nwant %v", got, want)
		}
		for _, e := range h {
			for _, n := range e.Names {
				if n.ID == "" {
					t.Fatalf("name without id: %+v", e)
				}
			}
		}
	}
	// create; priority (no names); Perf deleted; Bug deleted; moved (milestone cleared).
	check(moved.ID, []string{
		"create[Beta,Bug(type),Perf]", "update[]", "update[Bug(type),Perf]", "update[Bug(type)]", "update[Beta]",
	})
	// create; Perf deleted; Bug deleted; Beta deleted.
	check(stays.ID, []string{
		"create[Beta,Bug(type),Perf]", "update[Bug(type),Perf]", "update[Bug(type)]", "update[Beta]",
	})
	h := namedHistory(t, owner, moved.ID)
	if fields(h[1]) != "priority" || h[1].Names == nil {
		t.Fatalf("priority entry = %+v", h[1])
	}
	if !strings.Contains(fields(h[4]), "milestoneId") {
		t.Fatalf("move entry = %+v", h[4])
	}
	byID := map[string]historyName{}
	for _, n := range h[0].Names {
		byID[n.ID] = n
	}
	if byID[beta.ID].Name != "Beta" || byID[perf.ID].Name != "Perf" || byID[bug].Name != "Bug" || !byID[bug].Type {
		t.Fatalf("create names by id = %+v", byID)
	}
}

func TestIssueHistoryKeepsNameBeforeRename(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	p := createProject(t, owner, "YLK", "Yalkos")
	m := createMilestone(t, owner, p.ID, map[string]any{"name": "Beta"})
	docs := createLabel(t, owner, map[string]any{"name": "Docs", "color": "#000000"})
	i := createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "t", "milestoneId": m.ID, "labelIds": []string{docs.ID}})
	owner.do(`mutation($id: ID!) { updateLabel(id: $id, input: { name: "Guides" }) { id } }`, map[string]any{"id": docs.ID}).decode(t, "updateLabel", &struct{}{})
	updateMilestone(t, owner, m.ID, map[string]any{"name": "Gamma"})
	// The issue's labels changed after the renames: the new entry stores the new name.
	owner.do(`mutation($id: ID!, $in: UpdateIssueInput!) { updateIssue(id: $id, input: $in) { id } }`,
		map[string]any{"id": i.ID, "in": map[string]any{"removeLabelIds": []string{docs.ID}}}).decode(t, "updateIssue", &struct{}{})
	h := namedHistory(t, owner, i.ID)
	if len(h) != 2 || names(h[0]) != "Beta,Docs,Task(type)" || names(h[1]) != "Guides,Task(type)" {
		t.Fatalf("history = %+v", h)
	}
}
