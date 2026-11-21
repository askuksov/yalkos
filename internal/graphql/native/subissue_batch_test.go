package native_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/graphql/native"
)

// seedParents creates a project with n parents, each with three sub-issues
// and two priority changes (three history entries), and returns the parents
// in creation order.
func seedParents(t *testing.T, owner *client, key string, n int) []string {
	t.Helper()
	p := createProject(t, owner, key, key)
	parents := make([]string, n)
	for i := range parents {
		parents[i] = createIssue(t, owner, map[string]any{"projectId": p.ID, "title": fmt.Sprintf("Parent %d", i)}).ID
		for c := range 3 {
			createIssue(t, owner, map[string]any{"parentId": parents[i], "title": fmt.Sprintf("Child %d.%d", i, c)})
		}
		for _, prio := range []int{2, 3} {
			owner.do(`mutation($id: ID!, $p: Int!) { updateIssue(id: $id, input: {priority: $p}) { id } }`,
				map[string]any{"id": parents[i], "p": prio}).decode(t, "updateIssue", &struct{}{})
		}
	}
	return parents
}

type idPage struct {
	Nodes []struct {
		ID string `json:"id"`
	} `json:"nodes"`
	PageInfo struct {
		HasNextPage     bool    `json:"hasNextPage"`
		HasPreviousPage bool    `json:"hasPreviousPage"`
		StartCursor     *string `json:"startCursor"`
		EndCursor       *string `json:"endCursor"`
	} `json:"pageInfo"`
}

func (p idPage) ids() []string {
	out := []string{}
	for _, n := range p.Nodes {
		out = append(out, n.ID)
	}
	return out
}

const pageFields = `nodes { id } pageInfo { hasNextPage hasPreviousPage startCursor endCursor }`

// parentPages lists the parents of a project with the given selection and
// returns each parent's connections by alias.
func parentPages(t *testing.T, c *client, projectID, selection string) map[string]map[string]idPage {
	t.Helper()
	var list struct {
		Nodes []map[string]any `json:"nodes"`
	}
	c.do(`query($p: ID!) { issues(first: 50, filter: {project: {eq: $p}, hasChildren: true}) { nodes { id `+selection+` } } }`,
		map[string]any{"p": projectID}).decode(t, "issues", &list)
	out := map[string]map[string]idPage{}
	for _, n := range list.Nodes {
		id := n["id"].(string)
		out[id] = map[string]idPage{}
		for alias, v := range n {
			if alias == "id" {
				continue
			}
			var pg idPage
			remarshal(t, v, &pg)
			out[id][alias] = pg
		}
	}
	return out
}

func projectIDByKey(t *testing.T, c *client, key string) string {
	t.Helper()
	var p struct {
		ID string `json:"id"`
	}
	c.do(`query($k: String!) { project(id: $k) { id } }`, map[string]any{"k": key}).decode(t, "project", &p)
	return p.ID
}

// wantChildren is one page of a parent's sub-issues through the unbatched List.
func wantChildren(t *testing.T, s server, parent string, o issue.Order, a page.Args) []string {
	t.Helper()
	id := uuid.MustParse(parent)
	res, err := s.deps.Issues.List(context.Background(), issue.Filter{Parent: &issue.IDCond{Eq: &id}, IncludeArchived: true}, o, a)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, i := range res.Items {
		out = append(out, i.ID.String())
	}
	return out
}

// wantHistory is one page of an issue's history through the unbatched List.
func wantHistory(t *testing.T, s server, entity string, a page.Args) []string {
	t.Helper()
	res, err := s.deps.Audit.List(context.Background(), uuid.MustParse(entity), a)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, e := range res.Items {
		out = append(out, e.ID.String())
	}
	return out
}

func intp(n int) *int { return &n }

// Sub-issues and history of a page of parents load in one statement each,
// however many parents the page has.
func TestChildrenAndHistoryQueryCount(t *testing.T) {
	s, queries := tracedServer(t)
	owner := ownerBrowser(t, s)
	seedParents(t, owner, "TWO", 2)
	seedParents(t, owner, "TEN", 10)
	two, ten := projectIDByKey(t, owner, "TWO"), projectIDByKey(t, owner, "TEN")
	native.SetLoaderWait(t, 250*time.Millisecond)

	const selection = `children(first: 2) { ` + pageFields + ` } history(first: 2) { ` + pageFields + ` }`
	count := func(projectID string, parents int) int64 {
		queries.Reset()
		got := parentPages(t, owner, projectID, selection)
		n := queries.N()
		if len(got) != parents {
			t.Fatalf("parents listed = %d, want %d", len(got), parents)
		}
		for id, pages := range got {
			if c := pages["children"]; len(c.Nodes) != 2 || !c.PageInfo.HasNextPage {
				t.Fatalf("parent %s: children page %+v, want 2 of 3", id, c)
			}
			if h := pages["history"]; len(h.Nodes) != 2 || !h.PageInfo.HasNextPage {
				t.Fatalf("parent %s: history page %+v, want 2 of 3", id, h)
			}
		}
		t.Logf("children and history of %d parents: %d SQL statements", parents, n)
		return n
	}
	if a, b := count(two, 2), count(ten, 10); a != b {
		t.Fatalf("SQL statements grow with the parents: %d for 2, %d for 10", a, b)
	}
}

// Each parent gets the page the unbatched list gives it: its own limit, the
// requested order and cursor, and aliases with different arguments apart.
func TestChildrenAndHistoryMatchUnbatchedLists(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	parents := seedParents(t, owner, "YLK", 3)
	projectID := projectIDByKey(t, owner, "YLK")
	native.SetLoaderWait(t, 250*time.Millisecond)

	got := parentPages(t, owner, projectID, `
		first1: children(first: 1) { `+pageFields+` }
		newest2: children(first: 2, orderBy: {field: CREATED_AT, direction: DESC}) { `+pageFields+` }
		last2: children(last: 2) { `+pageFields+` }
		h1: history(first: 1) { `+pageFields+` }
		hLast2: history(last: 2) { `+pageFields+` }`)
	for _, p := range parents {
		pages := got[p]
		checks := []struct {
			alias string
			want  []string
		}{
			{"first1", wantChildren(t, s, p, issue.Order{Field: issue.SortOrder}, page.Args{First: intp(1)})},
			{"newest2", wantChildren(t, s, p, issue.Order{Field: issue.CreatedAt, Desc: true}, page.Args{First: intp(2)})},
			{"last2", wantChildren(t, s, p, issue.Order{Field: issue.SortOrder}, page.Args{Last: intp(2)})},
			{"h1", wantHistory(t, s, p, page.Args{First: intp(1)})},
			{"hLast2", wantHistory(t, s, p, page.Args{Last: intp(2)})},
		}
		for _, c := range checks {
			if g := pages[c.alias].ids(); !slices.Equal(g, c.want) {
				t.Errorf("parent %s %s = %v, want %v", p, c.alias, g, c.want)
			}
		}
		if slices.Equal(pages["first1"].ids(), pages["newest2"].ids()[:1]) && slices.Equal(pages["first1"].ids(), pages["last2"].ids()[1:]) {
			t.Errorf("parent %s: the aliases returned the same rows; the fixture does not tell them apart", p)
		}
	}

	// A cursor pages each parent on its own, forward and backward.
	first := got[parents[0]]["first1"].PageInfo.EndCursor
	hFirst := got[parents[0]]["h1"].PageInfo.EndCursor
	if first == nil || hFirst == nil {
		t.Fatal("no end cursor")
	}
	next := parentPages(t, owner, projectID, fmt.Sprintf(`
		after: children(first: 2, after: %q) { %s }
		before: children(last: 1, before: %q) { %s }
		hAfter: history(first: 5, after: %q) { %s }`, *first, pageFields, *first, pageFields, *hFirst, pageFields))
	for _, p := range parents {
		for alias, want := range map[string][]string{
			"after":  wantChildren(t, s, p, issue.Order{Field: issue.SortOrder}, page.Args{First: intp(2), After: first}),
			"before": wantChildren(t, s, p, issue.Order{Field: issue.SortOrder}, page.Args{Last: intp(1), Before: first}),
			"hAfter": wantHistory(t, s, p, page.Args{First: intp(5), After: hFirst}),
		} {
			if g := next[p][alias].ids(); !slices.Equal(g, want) {
				t.Errorf("parent %s %s = %v, want %v", p, alias, g, want)
			}
		}
	}
	if g := next[parents[0]]["after"].ids(); len(g) != 2 || !next[parents[0]]["after"].PageInfo.HasPreviousPage {
		t.Errorf("first parent, page after its first child = %+v", next[parents[0]]["after"])
	}

	// An issue without sub-issues gets an empty page; a cursor of another
	// order is refused as before.
	var leaf struct {
		Children idPage `json:"children"`
	}
	child := wantChildren(t, s, parents[0], issue.Order{}, page.Args{First: intp(1)})[0]
	owner.do(`query($id: String!) { issue(id: $id) { children(first: 5) { `+pageFields+` } } }`, map[string]any{"id": child}).decode(t, "issue", &leaf)
	if len(leaf.Children.Nodes) != 0 || leaf.Children.PageInfo.HasNextPage {
		t.Errorf("leaf children = %+v, want an empty page", leaf.Children)
	}
	r := owner.do(fmt.Sprintf(`{ issue(id: %q) { children(first: 1, after: %q, orderBy: {field: CREATED_AT}) { nodes { id } } } }`, parents[0], *first), nil)
	if r.code() != "INVALID_INPUT" {
		t.Errorf("cursor of another order: %s", r.raw)
	}
}

// remarshal decodes v (a decoded JSON value) into dst.
func remarshal(t *testing.T, v any, dst any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, dst); err != nil {
		t.Fatal(err)
	}
}
