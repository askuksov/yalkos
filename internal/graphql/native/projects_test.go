package native_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func (r result) reason() string {
	if len(r.Errors) == 0 {
		return ""
	}
	s, _ := r.Errors[0].Extensions["reason"].(string)
	return s
}

// decode unmarshals data[key] into v.
func (r result) decode(t *testing.T, key string, v any) {
	t.Helper()
	if len(r.Errors) > 0 {
		t.Fatalf("errors: %s", r.raw)
	}
	if err := json.Unmarshal(r.Data[key], v); err != nil {
		t.Fatalf("data.%s: %v (%s)", key, err, r.raw)
	}
}

var userSeq int

// userBrowser invites a user with role through owner and signs them in.
func userBrowser(t *testing.T, s server, owner *client, role string) *client {
	t.Helper()
	r := owner.do(fmt.Sprintf(`mutation { createInvite(role: %s) { url } }`, role), nil)
	url := r.str(t, "createInvite", "url")
	token := url[strings.LastIndex(url, "/")+1:]
	userSeq++
	b := s.browser(t)
	r = b.do(`mutation($in: AcceptInviteInput!) { acceptInvite(input: $in) { id } }`, map[string]any{"in": map[string]any{
		"token": token, "name": fmt.Sprintf("User %d", userSeq), "email": fmt.Sprintf("user%d@example.com", userSeq), "password": "user-password",
	}})
	if len(r.Errors) > 0 {
		t.Fatalf("acceptInvite: %s", r.raw)
	}
	return b
}

type gqlState struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Position  int    `json:"position"`
	IsDefault bool   `json:"isDefault"`
}

type gqlProject struct {
	ID      string     `json:"id"`
	Key     string     `json:"key"`
	Name    string     `json:"name"`
	Color   *string    `json:"color"`
	Icon    *string    `json:"icon"`
	States  []gqlState `json:"states"`
	Members []struct {
		ID string `json:"id"`
	} `json:"members"`
}

const projectFields = `id key name color icon states { id name type position isDefault } members { id }`

func createProject(t *testing.T, c *client, key, name string) gqlProject {
	t.Helper()
	var p gqlProject
	c.do(`mutation($in: CreateProjectInput!) { createProject(input: $in) { `+projectFields+` } }`,
		map[string]any{"in": map[string]any{"key": key, "name": name}}).decode(t, "createProject", &p)
	return p
}

func listKeys(t *testing.T, c *client, includeArchived bool) []string {
	t.Helper()
	var conn struct {
		Nodes []gqlProject `json:"nodes"`
	}
	c.do(`query($a: Boolean) { projects(includeArchived: $a) { nodes { key } } }`, map[string]any{"a": includeArchived}).decode(t, "projects", &conn)
	keys := make([]string, 0, len(conn.Nodes))
	for _, p := range conn.Nodes {
		keys = append(keys, p.Key)
	}
	return keys
}

func TestProjectLifecycleAPI(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	p := createProject(t, owner, "ylk", "Yalkos")
	if p.Key != "YLK" || len(p.States) != 5 || len(p.Members) != 1 {
		t.Fatalf("created: %+v", p)
	}
	for _, st := range p.States {
		if st.IsDefault != (st.Name == "Todo") {
			t.Fatalf("default state: %+v", p.States)
		}
	}
	if p.States[0].Name != "Backlog" || p.States[0].Type != "BACKLOG" || p.States[4].Type != "CANCELED" {
		t.Fatalf("states order: %+v", p.States)
	}

	r := owner.do(`mutation($id: ID!) { updateProject(id: $id, input: {key: "YK"}) { key } }`, map[string]any{"id": p.ID})
	if r.str(t, "updateProject", "key") != "YK" {
		t.Fatalf("updateProject: %s", r.raw)
	}
	r = owner.do(`{ project(id: "ylk") { id key } }`, nil)
	if r.str(t, "project", "key") != "YK" || r.str(t, "project", "id") != p.ID {
		t.Fatalf("project by former key: %s", r.raw)
	}

	r = owner.do(`mutation($id: ID!) { archiveProject(id: $id) { archivedAt } }`, map[string]any{"id": p.ID})
	if r.str(t, "archiveProject", "archivedAt") == "" {
		t.Fatalf("archiveProject: %s", r.raw)
	}
	if keys := listKeys(t, owner, false); len(keys) != 0 {
		t.Fatalf("projects() lists archived: %v", keys)
	}
	if keys := listKeys(t, owner, true); len(keys) != 1 || keys[0] != "YK" {
		t.Fatalf("includeArchived: %v", keys)
	}
	r = owner.do(`mutation($id: ID!) { updateProject(id: $id, input: {name: "X"}) { name } }`, map[string]any{"id": p.ID})
	if r.code() != "INVALID_INPUT" || r.field() != "projectId" || r.reason() != "archived" {
		t.Fatalf("update archived: %s", r.raw)
	}
	r = owner.do(`mutation($id: ID!) { unarchiveProject(id: $id) { archivedAt } }`, map[string]any{"id": p.ID})
	if len(r.Errors) > 0 || string(r.Data["unarchiveProject"]) != `{"archivedAt":null}` {
		t.Fatalf("unarchiveProject: %s", r.raw)
	}
	if keys := listKeys(t, owner, false); len(keys) != 1 {
		t.Fatalf("after unarchive: %v", keys)
	}
}

func TestProjectsForbiddenForMember(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	p := createProject(t, owner, "YLK", "Yalkos")
	member := userBrowser(t, s, owner, "MEMBER")

	cases := map[string]struct {
		q    string
		vars map[string]any
	}{
		"createProject": {`mutation { createProject(input: {key: "WEB", name: "Web"}) { id } }`, nil},
		"createState":   {`mutation($p: ID!) { createState(projectId: $p, input: {name: "Review", type: STARTED, color: "#123456"}) { id } }`, map[string]any{"p": p.ID}},
		// Members may add only themselves (join).
		"addProjectMember": {`mutation($p: ID!, $u: ID!) { addProjectMember(projectId: $p, userId: $u) { id } }`, map[string]any{"p": p.ID, "u": viewerID(t, owner)}},
	}
	for name, c := range cases {
		if r := member.do(c.q, c.vars); r.code() != "FORBIDDEN" {
			t.Errorf("%s: %s", name, r.raw)
		}
	}
	// Reading is open to every user.
	if r := member.do(`{ project(id: "YLK") { key } }`, nil); r.str(t, "project", "key") != "YLK" {
		t.Errorf("member read: %s", r.raw)
	}
}

func TestDeleteProjectAPI(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	admin := userBrowser(t, s, owner, "ADMIN")
	p := createProject(t, owner, "YLK", "Yalkos")
	del := `mutation($id: ID!, $k: String!) { deleteProject(id: $id, confirmKey: $k) }`

	if r := admin.do(del, map[string]any{"id": p.ID, "k": "YLK"}); r.code() != "FORBIDDEN" {
		t.Fatalf("admin delete: %s", r.raw)
	}
	r := owner.do(del, map[string]any{"id": p.ID, "k": "X"})
	if r.code() != "INVALID_INPUT" || r.field() != "confirmKey" || r.reason() != "mismatch" {
		t.Fatalf("wrong confirmKey: %s", r.raw)
	}
	if r := owner.do(del, map[string]any{"id": p.ID, "k": "YLK"}); string(r.Data["deleteProject"]) != "true" {
		t.Fatalf("delete: %s", r.raw)
	}
	if r := owner.do(`query($id: String!) { project(id: $id) { id } }`, map[string]any{"id": p.ID}); r.code() != "NOT_FOUND" {
		t.Fatalf("deleted project: %s", r.raw)
	}
}

func TestStatesAPI(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	p := createProject(t, owner, "YLK", "Yalkos")
	byName := map[string]string{}
	for _, st := range p.States {
		byName[st.Name] = st.ID
	}

	var review gqlState
	owner.do(`mutation($p: ID!) { createState(projectId: $p, input: {name: "Review", type: STARTED, color: "#AABBCC"}) { id name type position isDefault } }`,
		map[string]any{"p": p.ID}).decode(t, "createState", &review)
	if review.Type != "STARTED" || review.Position != 5 || review.IsDefault {
		t.Fatalf("createState: %+v", review)
	}
	r := owner.do(`mutation($p: ID!) { createState(projectId: $p, input: {name: "review", type: STARTED, color: "#aabbcc"}) { id } }`, map[string]any{"p": p.ID})
	if r.code() != "INVALID_INPUT" || r.field() != "name" || r.reason() != "taken" {
		t.Fatalf("duplicate state name: %s", r.raw)
	}

	var backlog struct {
		IsDefault bool `json:"isDefault"`
		Project   struct {
			Key    string     `json:"key"`
			States []gqlState `json:"states"`
		} `json:"project"`
	}
	owner.do(`mutation($id: ID!) { updateState(id: $id, input: {isDefault: true}) { isDefault project { key states { name isDefault } } } }`,
		map[string]any{"id": byName["Backlog"]}).decode(t, "updateState", &backlog)
	if !backlog.IsDefault || backlog.Project.Key != "YLK" {
		t.Fatalf("updateState: %+v", backlog)
	}
	for _, st := range backlog.Project.States {
		if st.IsDefault != (st.Name == "Backlog") {
			t.Fatalf("one default expected: %+v", backlog.Project.States)
		}
	}

	order := []string{review.ID, byName["Done"], byName["In Progress"], byName["Todo"], byName["Backlog"], byName["Canceled"]}
	var reordered []gqlState
	owner.do(`mutation($p: ID!, $ids: [ID!]!) { reorderStates(projectId: $p, stateIds: $ids) { id position } }`,
		map[string]any{"p": p.ID, "ids": order}).decode(t, "reorderStates", &reordered)
	for i, st := range reordered {
		if st.ID != order[i] || st.Position != i {
			t.Fatalf("reorderStates: %+v", reordered)
		}
	}

	r = owner.do(`mutation($id: ID!, $to: ID) { deleteState(id: $id, moveIssuesTo: $to) }`, map[string]any{"id": review.ID, "to": byName["In Progress"]})
	if string(r.Data["deleteState"]) != "true" {
		t.Fatalf("deleteState: %s", r.raw)
	}
	r = owner.do(`mutation($id: ID!) { deleteState(id: $id) }`, map[string]any{"id": byName["Backlog"]})
	if r.code() != "INVALID_INPUT" || r.field() != "id" || r.reason() != "not_allowed" {
		t.Fatalf("delete default state: %s", r.raw)
	}
}

func TestProjectMembersAPI(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	member := userBrowser(t, s, owner, "MEMBER")
	var viewer struct {
		ID string `json:"id"`
	}
	member.do(`{ viewer { id } }`, nil).decode(t, "viewer", &viewer)
	p := createProject(t, owner, "YLK", "Yalkos")

	var added gqlProject
	owner.do(`mutation($p: ID!, $u: ID!) { addProjectMember(projectId: $p, userId: $u) { members { id } } }`,
		map[string]any{"p": p.ID, "u": viewer.ID}).decode(t, "addProjectMember", &added)
	if len(added.Members) != 2 || added.Members[1].ID != viewer.ID {
		t.Fatalf("addProjectMember: %+v", added)
	}
	var removed gqlProject
	owner.do(`mutation($p: ID!, $u: ID!) { removeProjectMember(projectId: $p, userId: $u) { members { id } } }`,
		map[string]any{"p": p.ID, "u": viewer.ID}).decode(t, "removeProjectMember", &removed)
	if len(removed.Members) != 1 {
		t.Fatalf("removeProjectMember: %+v", removed)
	}
	r := owner.do(`mutation($p: ID!) { addProjectMember(projectId: $p, userId: "nope") { id } }`, map[string]any{"p": p.ID})
	if r.code() != "INVALID_INPUT" || r.field() != "userId" || r.reason() != "invalid_format" {
		t.Fatalf("bad userId: %s", r.raw)
	}
}

func TestProjectsConnection(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	for _, k := range []string{"AAA", "BBB", "CCC"} {
		createProject(t, owner, k, "Project "+k)
	}
	var conn struct {
		Nodes []gqlProject `json:"nodes"`
		Edges []struct {
			Node   gqlProject `json:"node"`
			Cursor string     `json:"cursor"`
		} `json:"edges"`
		PageInfo struct {
			HasNextPage     bool    `json:"hasNextPage"`
			HasPreviousPage bool    `json:"hasPreviousPage"`
			StartCursor     *string `json:"startCursor"`
			EndCursor       *string `json:"endCursor"`
		} `json:"pageInfo"`
	}
	q := `query($f: Int, $a: String) { projects(first: $f, after: $a) {
		nodes { key } edges { node { key } cursor } pageInfo { hasNextPage hasPreviousPage startCursor endCursor } } }`
	owner.do(q, map[string]any{"f": 1}).decode(t, "projects", &conn)
	if len(conn.Nodes) != 1 || conn.Nodes[0].Key != "AAA" || len(conn.Edges) != 1 || conn.Edges[0].Node.Key != "AAA" {
		t.Fatalf("first page: %+v", conn)
	}
	if conn.PageInfo.EndCursor == nil || conn.Edges[0].Cursor != *conn.PageInfo.EndCursor || !conn.PageInfo.HasNextPage || conn.PageInfo.HasPreviousPage {
		t.Fatalf("pageInfo: %+v", conn.PageInfo)
	}
	owner.do(q, map[string]any{"f": 5, "a": *conn.PageInfo.EndCursor}).decode(t, "projects", &conn)
	if len(conn.Nodes) != 2 || conn.Nodes[0].Key != "BBB" || conn.PageInfo.HasNextPage || !conn.PageInfo.HasPreviousPage {
		t.Fatalf("second page: %+v", conn)
	}
	r := owner.do(`{ projects(first: 251) { nodes { id } } }`, nil)
	if r.code() != "INVALID_INPUT" || r.field() != "first" || r.reason() != "out_of_range" {
		t.Fatalf("first: 251: %s", r.raw)
	}
}

func TestUpdateProjectClearsColor(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	var p gqlProject
	owner.do(`mutation { createProject(input: {key: "YLK", name: "Yalkos", color: "#AABBCC", icon: "rocket"}) { id color icon } }`, nil).decode(t, "createProject", &p)
	if p.Color == nil || *p.Color != "#aabbcc" {
		t.Fatalf("created color: %+v", p)
	}
	upd := `mutation($id: ID!, $in: UpdateProjectInput!) { updateProject(id: $id, input: $in) { color icon name } }`
	var got gqlProject
	owner.do(upd, map[string]any{"id": p.ID, "in": map[string]any{}}).decode(t, "updateProject", &got)
	if got.Color == nil || *got.Color != "#aabbcc" || got.Icon == nil || got.Name != "Yalkos" {
		t.Fatalf("empty input changed fields: %+v", got)
	}
	owner.do(upd, map[string]any{"id": p.ID, "in": map[string]any{"color": nil}}).decode(t, "updateProject", &got)
	if got.Color != nil || got.Icon == nil {
		t.Fatalf("color: null: %+v", got)
	}
	r := owner.do(upd, map[string]any{"id": p.ID, "in": map[string]any{"name": nil}})
	if r.code() != "INVALID_INPUT" || r.field() != "name" || r.reason() != "required" {
		t.Fatalf("name: null: %s", r.raw)
	}
}

func TestProjectsViaAPIKey(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	key := owner.do(`mutation { createApiKey(name: "CI") { key } }`, nil).str(t, "createApiKey", "key")
	p := createProject(t, s.apiClient(t, key), "YLK", "Yalkos")
	if p.Key != "YLK" {
		t.Fatalf("createProject via key: %+v", p)
	}
}

func TestProjectsUnauthenticated(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	p := createProject(t, owner, "YLK", "Yalkos")
	anon := s.browser(t)
	for name, q := range map[string]string{
		"projects":      `{ projects { nodes { id } } }`,
		"project":       `{ project(id: "YLK") { id } }`,
		"createProject": `mutation { createProject(input: {key: "WEB", name: "Web"}) { id } }`,
		"archive":       fmt.Sprintf(`mutation { archiveProject(id: %q) { id } }`, p.ID),
	} {
		if r := anon.do(q, nil); r.code() != "AUTHENTICATION_ERROR" {
			t.Errorf("%s: %s", name, r.raw)
		}
	}
}

func TestIsMemberAPI(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	member := userBrowser(t, s, owner, "MEMBER")
	p := createProject(t, owner, "YLK", "Yalkos")
	isMember := func(c *client) bool {
		t.Helper()
		var got struct {
			IsMember bool `json:"isMember"`
		}
		c.do(`{ project(id: "YLK") { isMember } }`, nil).decode(t, "project", &got)
		return got.IsMember
	}
	if !isMember(owner) {
		t.Fatal("creator is not a member")
	}
	if isMember(member) {
		t.Fatal("member is a member before joining")
	}
	var me struct {
		ID string `json:"id"`
	}
	member.do(`{ viewer { id } }`, nil).decode(t, "viewer", &me)
	vars := map[string]any{"p": p.ID, "u": me.ID}
	var joined struct {
		IsMember bool `json:"isMember"`
	}
	member.do(`mutation($p: ID!, $u: ID!) { addProjectMember(projectId: $p, userId: $u) { isMember } }`, vars).decode(t, "addProjectMember", &joined)
	if !joined.IsMember || !isMember(member) {
		t.Fatal("member did not join")
	}
	r := member.do(`mutation($p: ID!, $u: ID!) { removeProjectMember(projectId: $p, userId: $u) { id } }`,
		map[string]any{"p": p.ID, "u": viewerID(t, owner)})
	if r.code() != "FORBIDDEN" {
		t.Fatalf("member removes the owner: %s", r.raw)
	}
	member.do(`mutation($p: ID!, $u: ID!) { removeProjectMember(projectId: $p, userId: $u) { id } }`, vars).decode(t, "removeProjectMember", &joined)
	if isMember(member) {
		t.Fatal("member did not leave")
	}
}
