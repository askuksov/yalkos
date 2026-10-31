package native_test

import (
	"testing"
)

type gqlLabel struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Color     string     `json:"color"`
	IsGroup   bool       `json:"isGroup"`
	Exclusive bool       `json:"exclusive"`
	Children  []gqlLabel `json:"children"`
	Parent    *struct {
		ID string `json:"id"`
	} `json:"parent"`
}

func allLabels(t *testing.T, c *client) []gqlLabel {
	t.Helper()
	var out []gqlLabel
	c.do(`{ labels { id name color isGroup exclusive parent { id } children { id name parent { id } } } }`, nil).decode(t, "labels", &out)
	return out
}

func typeIDs(t *testing.T, c *client) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, l := range allLabels(t, c) {
		if l.Name == "Type" {
			for _, ch := range l.Children {
				out[ch.Name] = ch.ID
			}
		}
	}
	return out
}

func createLabel(t *testing.T, c *client, in map[string]any) gqlLabel {
	t.Helper()
	var l gqlLabel
	c.do(`mutation($in: CreateLabelInput!) { createLabel(input: $in) { id name color isGroup exclusive parent { id } } }`,
		map[string]any{"in": in}).decode(t, "createLabel", &l)
	return l
}

func TestLabelsAPI(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	member := userBrowser(t, s, owner, "MEMBER")
	perf := createLabel(t, member, map[string]any{"name": "Perf", "color": "#AA0000"})
	if perf.Color != "#aa0000" || perf.IsGroup || perf.Parent != nil {
		t.Fatalf("created = %+v", perf)
	}
	labels := allLabels(t, owner)
	if len(labels) != 2 || labels[0].Name != "Type" || !labels[0].IsGroup || !labels[0].Exclusive || len(labels[0].Children) != 4 ||
		labels[0].Children[0].Name != "Bug" || labels[0].Children[0].Parent == nil || labels[0].Children[0].Parent.ID != labels[0].ID ||
		labels[1].Name != "Perf" {
		t.Fatalf("labels = %+v", labels)
	}
	if r := member.do(`mutation($id: ID!) { updateLabel(id: $id, input: { name: "Speed" }) { id } }`, map[string]any{"id": perf.ID}); r.code() != "FORBIDDEN" {
		t.Errorf("member update: %s", r.raw)
	}
	var upd gqlLabel
	owner.do(`mutation($id: ID!) { updateLabel(id: $id, input: { name: "Speed" }) { id name } }`, map[string]any{"id": perf.ID}).decode(t, "updateLabel", &upd)
	if upd.Name != "Speed" {
		t.Errorf("update = %+v", upd)
	}
	var settings struct {
		TypeGroup struct {
			Name string `json:"name"`
		} `json:"typeGroup"`
		DefaultType *struct {
			Name string `json:"name"`
		} `json:"defaultType"`
	}
	owner.do(`{ labelSettings { typeGroup { name } defaultType { name } } }`, nil).decode(t, "labelSettings", &settings)
	if settings.TypeGroup.Name != "Type" || settings.DefaultType == nil || settings.DefaultType.Name != "Task" {
		t.Fatalf("settings = %+v", settings)
	}
	bug := typeIDs(t, owner)["Bug"]
	owner.do(`mutation($id: ID) { setDefaultType(labelId: $id) { defaultType { name } } }`, map[string]any{"id": bug}).decode(t, "setDefaultType", &settings)
	if settings.DefaultType == nil || settings.DefaultType.Name != "Bug" {
		t.Errorf("default after set = %+v", settings.DefaultType)
	}
	if r := owner.do(`mutation($id: ID!) { deleteLabel(id: $id) }`, map[string]any{"id": perf.ID}); len(r.Errors) > 0 {
		t.Errorf("delete: %s", r.raw)
	}
}

func TestIssueTypeAPI(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	p := createProject(t, owner, "YLK", "Yalkos")
	types := typeIDs(t, owner)
	perf := createLabel(t, owner, map[string]any{"name": "Perf", "color": "#000000"})
	type withLabels struct {
		Type *struct {
			Name string `json:"name"`
		} `json:"type"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	var i withLabels
	owner.do(`mutation($in: CreateIssueInput!) { createIssue(input: $in) { type { name } labels { name } } }`,
		map[string]any{"in": map[string]any{"projectId": p.ID, "title": "t", "labelIds": []string{perf.ID}}}).decode(t, "createIssue", &i)
	if i.Type == nil || i.Type.Name != "Task" || len(i.Labels) != 2 || i.Labels[0].Name != "Task" || i.Labels[1].Name != "Perf" {
		t.Fatalf("created = %+v", i)
	}
	created := createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "u"})
	owner.do(`mutation($id: ID!, $in: UpdateIssueInput!) { updateIssue(id: $id, input: $in) { type { name } labels { name } } }`,
		map[string]any{"id": created.ID, "in": map[string]any{"addLabelIds": []string{types["Bug"]}}}).decode(t, "updateIssue", &i)
	if i.Type == nil || i.Type.Name != "Bug" || len(i.Labels) != 1 {
		t.Fatalf("after add Bug = %+v", i)
	}
	owner.do(`mutation($id: ID!, $in: UpdateIssueInput!) { updateIssue(id: $id, input: $in) { type { name } labels { name } } }`,
		map[string]any{"id": created.ID, "in": map[string]any{"removeLabelIds": []string{types["Bug"]}}}).decode(t, "updateIssue", &i)
	if i.Type != nil || len(i.Labels) != 0 {
		t.Fatalf("after remove = %+v", i)
	}
	r := owner.do(`mutation($id: ID!, $in: UpdateIssueInput!) { updateIssue(id: $id, input: $in) { id } }`,
		map[string]any{"id": created.ID, "in": map[string]any{"labelIds": []string{types["Bug"], types["Feature"]}}})
	if r.reason() != "not_allowed" || r.field() != "labelIds" {
		t.Errorf("two types: %s", r.raw)
	}
}

func TestIssueFilterLabelsAPI(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	p := createProject(t, owner, "YLK", "Yalkos")
	types := typeIDs(t, owner)
	m := createMilestone(t, owner, p.ID, map[string]any{"name": "M"})
	bug := createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "bug", "labelIds": []string{types["Bug"]}, "milestoneId": m.ID})
	task := createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "task"})
	owner.do(`mutation { setDefaultType(labelId: null) { typeGroup { id } } }`, nil)
	bare := createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "bare"})
	cases := []struct {
		name   string
		filter map[string]any
		want   []string
	}{
		{"type bug", map[string]any{"type": map[string]any{"in": []string{types["Bug"]}}}, []string{bug.ID}},
		{"no type", map[string]any{"type": map[string]any{"null": true}}, []string{bare.ID}},
		{"no labels", map[string]any{"label": map[string]any{"null": true}}, []string{bare.ID}},
		{"label nin task", map[string]any{"label": map[string]any{"nin": []string{types["Task"]}}}, []string{bug.ID, bare.ID}},
		{"milestone eq", map[string]any{"milestone": map[string]any{"eq": m.ID}}, []string{bug.ID}},
		{"milestone null", map[string]any{"milestone": map[string]any{"null": true}}, []string{task.ID, bare.ID}},
	}
	for _, c := range cases {
		got := issueIDs(t, owner, c.filter)
		want := sorted(c.want...)
		if len(got) != len(want) || (len(got) > 0 && got[0] != want[0]) || (len(got) > 1 && got[1] != want[1]) {
			t.Errorf("%s: got %v want %v", c.name, got, want)
		}
	}
}

func TestLabelConflictReason(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	p := createProject(t, owner, "YLK", "Yalkos")
	g := createLabel(t, owner, map[string]any{"name": "Area", "color": "#000000", "isGroup": true})
	a := createLabel(t, owner, map[string]any{"name": "A", "color": "#000000", "parentId": g.ID})
	b := createLabel(t, owner, map[string]any{"name": "B", "color": "#000000"})
	createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "t", "labelIds": []string{a.ID, b.ID}})
	r := owner.do(`mutation($id: ID!, $g: ID) { updateLabel(id: $id, input: { parentId: $g }) { id } }`, map[string]any{"id": b.ID, "g": g.ID})
	params, _ := r.Errors[0].Extensions["params"].(map[string]any)
	if r.reason() != "not_allowed" || r.field() != "parentId" || params["conflicts"] != float64(1) {
		t.Errorf("conflict: %s", r.raw)
	}
}
