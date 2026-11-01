package linear_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/account"
)

type userNode struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	DisplayName           string `json:"displayName"`
	Email                 string `json:"email"`
	Active                bool   `json:"active"`
	Admin                 bool   `json:"admin"`
	Owner                 bool   `json:"owner"`
	IsMe                  bool   `json:"isMe"`
	Initials              string `json:"initials"`
	AvatarBackgroundColor string `json:"avatarBackgroundColor"`
	URL                   string `json:"url"`
	CreatedIssueCount     int    `json:"createdIssueCount"`
	CreatedAt             string `json:"createdAt"`
	UpdatedAt             string `json:"updatedAt"`
	Guest                 bool   `json:"guest"`
	App                   bool   `json:"app"`
}

var palette = []string{"#5E6AD2", "#26B5CE", "#F2C94C", "#4CB782", "#F2994A", "#EB5757", "#BB87FC", "#95A2B3"}

func TestViewerFields(t *testing.T) {
	e := newEnv(t)
	p := e.project(t, "YLK", "Yalkos")
	e.issue(t, p, "one")
	e.issue(t, p, "two")
	var out struct{ Viewer userNode }
	e.gql(t, e.ownerKey, `{ viewer { id name displayName email active admin owner isMe initials avatarBackgroundColor url
		createdIssueCount createdAt updatedAt guest app } }`, nil).ok(t, &out)
	v := out.Viewer
	if v.ID != e.owner.ID.String() || v.Name != "Olga" || v.DisplayName != "Olga" || v.Email != "owner@example.com" ||
		!v.Active || !v.Admin || !v.Owner || !v.IsMe || v.Initials != "O" || v.URL != base || v.CreatedIssueCount != 2 ||
		v.Guest || v.App {
		t.Fatalf("viewer = %+v", v)
	}
	if !slices.Contains(palette, v.AvatarBackgroundColor) {
		t.Fatalf("avatar colour %q not in palette", v.AvatarBackgroundColor)
	}
	if len(v.CreatedAt) != len("2006-01-02T15:04:05.000Z") || !strings.HasSuffix(v.CreatedAt, "Z") || v.UpdatedAt == "" {
		t.Fatalf("timestamps %q %q", v.CreatedAt, v.UpdatedAt)
	}
}

func TestViewerMemberAndInitials(t *testing.T) {
	e := newEnv(t)
	m := e.user(t, "mia van dyke", "mia@example.com", account.RoleMember)
	var out struct{ Viewer userNode }
	e.gql(t, "Bearer "+e.key(t, m), `{ viewer { admin owner isMe initials createdIssueCount } }`, nil).ok(t, &out)
	if out.Viewer.Admin || out.Viewer.Owner || !out.Viewer.IsMe || out.Viewer.Initials != "MV" || out.Viewer.CreatedIssueCount != 0 {
		t.Fatalf("member viewer = %+v", out.Viewer)
	}
}

type userConn struct {
	Users struct {
		Nodes    []userNode
		PageInfo struct {
			HasNextPage, HasPreviousPage bool
			StartCursor, EndCursor       *string
		}
	}
}

func names(c userConn) []string {
	var out []string
	for _, n := range c.Users.Nodes {
		out = append(out, n.Name)
	}
	return out
}

func usersEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	e.user(t, "Ann", "ann@member.test", account.RoleMember)
	e.user(t, "Bob", "bob@example.com", account.RoleAdmin)
	d := e.user(t, "Dan", "dan@member.test", account.RoleMember)
	if err := e.deps.Accounts.SetMemberDisabled(context.Background(), e.owner, d.ID, true); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestUsersListAndFilter(t *testing.T) {
	e := usersEnv(t)
	q := `query($filter: UserFilter, $includeDisabled: Boolean, $sort: [UserSortInput!]) {
		users(filter: $filter, includeDisabled: $includeDisabled, sort: $sort) { nodes { name } } }`
	cases := []struct {
		name string
		vars map[string]any
		want []string
	}{
		{"default newest first", nil, []string{"Bob", "Ann", "Olga"}},
		{"include disabled", map[string]any{"includeDisabled": true}, []string{"Dan", "Bob", "Ann", "Olga"}},
		{"email containsIgnoreCase", map[string]any{"filter": map[string]any{"email": map[string]any{"containsIgnoreCase": "MEMBER"}}}, []string{"Ann"}},
		{"isMe", map[string]any{"filter": map[string]any{"isMe": map[string]any{"eq": true}}}, []string{"Olga"}},
		{"or", map[string]any{"filter": map[string]any{"or": []any{
			map[string]any{"name": map[string]any{"eq": "Ann"}}, map[string]any{"name": map[string]any{"eq": "Bob"}},
		}}}, []string{"Bob", "Ann"}},
		{"and", map[string]any{"filter": map[string]any{"and": []any{
			map[string]any{"admin": map[string]any{"eq": true}}, map[string]any{"owner": map[string]any{"eq": false}},
		}}}, []string{"Bob"}},
		{"active false", map[string]any{"includeDisabled": true, "filter": map[string]any{"active": map[string]any{"eq": false}}}, []string{"Dan"}},
		{"sort name asc", map[string]any{"sort": []any{map[string]any{"name": map[string]any{"order": "Ascending"}}}}, []string{"Ann", "Bob", "Olga"}},
		{"sort displayName desc", map[string]any{"sort": []any{map[string]any{"displayName": map[string]any{"order": "Descending"}}}}, []string{"Olga", "Bob", "Ann"}},
		{"id in", nil, nil},
	}
	for _, c := range cases[:len(cases)-1] {
		var out userConn
		e.gql(t, e.ownerKey, q, c.vars).ok(t, &out)
		if got := names(out); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	var out userConn
	e.gql(t, e.ownerKey, q, map[string]any{"filter": map[string]any{"id": map[string]any{"in": []string{e.owner.ID.String()}}}}).ok(t, &out)
	if got := names(out); !slices.Equal(got, []string{"Olga"}) {
		t.Errorf("id in: %v", got)
	}
	e.gql(t, e.ownerKey, q, map[string]any{"filter": map[string]any{"id": map[string]any{"eq": "nope"}}}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid id: not a valid id", "id")
}

func TestUsersPaging(t *testing.T) {
	e := usersEnv(t)
	q := `query($after: String, $first: Int) { users(first: $first, after: $after) { nodes { name } pageInfo { hasNextPage hasPreviousPage startCursor endCursor } } }`
	var p1 userConn
	e.gql(t, e.ownerKey, q, map[string]any{"first": 2}).ok(t, &p1)
	if got := names(p1); !slices.Equal(got, []string{"Bob", "Ann"}) || !p1.Users.PageInfo.HasNextPage || p1.Users.PageInfo.HasPreviousPage {
		t.Fatalf("page 1 = %v %+v", got, p1.Users.PageInfo)
	}
	var p2 userConn
	e.gql(t, e.ownerKey, q, map[string]any{"first": 2, "after": *p1.Users.PageInfo.EndCursor}).ok(t, &p2)
	if got := names(p2); !slices.Equal(got, []string{"Olga"}) || p2.Users.PageInfo.HasNextPage || !p2.Users.PageInfo.HasPreviousPage {
		t.Fatalf("page 2 = %v %+v", got, p2.Users.PageInfo)
	}
	var back userConn
	e.gql(t, e.ownerKey, `query($before: String) { users(last: 1, before: $before) { nodes { name } pageInfo { hasPreviousPage } } }`,
		map[string]any{"before": *p2.Users.PageInfo.StartCursor}).ok(t, &back)
	if got := names(back); !slices.Equal(got, []string{"Ann"}) || !back.Users.PageInfo.HasPreviousPage {
		t.Fatalf("back = %v %+v", got, back.Users.PageInfo)
	}
	e.gql(t, e.ownerKey, q, map[string]any{"after": "garbage"}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid after: cursor is invalid", "after")
	e.gql(t, e.ownerKey, q, map[string]any{"first": 251}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid first: must be 1–250", "first")
}

func TestUserByID(t *testing.T) {
	e := newEnv(t)
	m := e.user(t, "Mia", "mia@example.com", account.RoleMember)
	q := `query($id: String!) { user(id: $id) { id name isMe } }`
	var out struct{ User userNode }
	e.gql(t, e.ownerKey, q, map[string]any{"id": m.ID.String()}).ok(t, &out)
	if out.User.ID != m.ID.String() || out.User.Name != "Mia" || out.User.IsMe {
		t.Fatalf("user = %+v", out.User)
	}
	e.gql(t, e.ownerKey, q, map[string]any{"id": uuid.NewString()}).fails(t).
		want(t, "NOT_FOUND", "invalid input", "User not found", "")
	e.gql(t, e.ownerKey, q, map[string]any{"id": "x"}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid id: not a valid id", "id")
}
