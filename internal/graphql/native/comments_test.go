package native_test

import (
	"strings"
	"testing"
)

func TestCommentsAPI(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	member := userBrowser(t, s, owner, "MEMBER")
	p := createProject(t, owner, "YLK", "Yalkos")
	i := createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "t"})
	var c struct {
		ID       string  `json:"id"`
		Body     string  `json:"body"`
		EditedAt *string `json:"editedAt"`
		Author   struct {
			ID string `json:"id"`
		} `json:"author"`
		Issue struct {
			ID string `json:"id"`
		} `json:"issue"`
	}
	owner.do(`mutation($i: ID!) { createComment(issueId: $i, body: "Looks *good*") { id body editedAt author { id } issue { id } } }`,
		map[string]any{"i": i.ID}).decode(t, "createComment", &c)
	if c.Body != "Looks *good*" || c.EditedAt != nil || c.Author.ID != viewerID(t, owner) || c.Issue.ID != i.ID {
		t.Fatalf("created = %+v", c)
	}
	var issue struct {
		CommentCount int `json:"commentCount"`
		Comments     struct {
			Nodes []struct {
				Body string `json:"body"`
			} `json:"nodes"`
		} `json:"comments"`
	}
	owner.do(`query($i: String!) { issue(id: $i) { commentCount comments { nodes { body } } } }`, map[string]any{"i": i.ID}).decode(t, "issue", &issue)
	if issue.CommentCount != 1 || len(issue.Comments.Nodes) != 1 {
		t.Fatalf("issue = %+v", issue)
	}
	if r := member.do(`mutation($id: ID!) { updateComment(id: $id, body: "x") { id } }`, map[string]any{"id": c.ID}); r.code() != "FORBIDDEN" {
		t.Errorf("member update: %s", r.raw)
	}
	owner.do(`mutation($id: ID!) { updateComment(id: $id, body: "edited") { id body editedAt } }`, map[string]any{"id": c.ID}).decode(t, "updateComment", &c)
	if c.Body != "edited" || c.EditedAt == nil {
		t.Errorf("updated = %+v", c)
	}
	if r := owner.do(`mutation($id: ID!) { deleteComment(id: $id) }`, map[string]any{"id": c.ID}); len(r.Errors) > 0 {
		t.Fatalf("delete: %s", r.raw)
	}
	var tl struct {
		Timeline struct {
			Nodes []struct {
				Typename string `json:"__typename"`
				Changes  []struct {
					Field string `json:"field"`
				} `json:"changes"`
			} `json:"nodes"`
		} `json:"timeline"`
	}
	owner.do(`query($i: String!) { issue(id: $i) { timeline(last: 1) { nodes { __typename ... on HistoryEntry { changes { field } } } } } }`,
		map[string]any{"i": i.ID}).decode(t, "issue", &tl)
	n := tl.Timeline.Nodes
	if len(n) != 1 || n[0].Typename != "HistoryEntry" || len(n[0].Changes) != 1 || n[0].Changes[0].Field != "commentDeleted" {
		t.Fatalf("timeline = %+v", n)
	}
	if r := owner.do(`mutation($i: ID!) { createComment(issueId: $i, body: "  ") { id } }`, map[string]any{"i": i.ID}); r.reason() != "required" || r.field() != "body" {
		t.Errorf("empty body: %s", r.raw)
	}
}

func TestTimelineAPI(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	p := createProject(t, owner, "YLK", "Yalkos")
	i := createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "t"})
	owner.do(`mutation($i: ID!) { createComment(issueId: $i, body: "one") { id } }`, map[string]any{"i": i.ID})
	owner.do(`mutation($i: ID!) { updateIssue(id: $i, input: { title: "t2" }) { id } }`, map[string]any{"i": i.ID})
	type node struct {
		Typename string `json:"__typename"`
		Body     string `json:"body"`
		Action   string `json:"action"`
	}
	var tl struct {
		Timeline struct {
			Nodes    []node `json:"nodes"`
			PageInfo struct {
				HasPreviousPage bool    `json:"hasPreviousPage"`
				StartCursor     *string `json:"startCursor"`
			} `json:"pageInfo"`
		} `json:"timeline"`
	}
	q := `query($i: String!, $l: Int, $b: String) { issue(id: $i) { timeline(last: $l, before: $b) {
		nodes { __typename ... on Comment { body } ... on HistoryEntry { action } } pageInfo { hasPreviousPage startCursor } } } }`
	owner.do(q, map[string]any{"i": i.ID}).decode(t, "issue", &tl)
	var kinds []string
	for _, n := range tl.Timeline.Nodes {
		kinds = append(kinds, n.Typename)
	}
	if strings.Join(kinds, ",") != "HistoryEntry,Comment,HistoryEntry" || tl.Timeline.Nodes[1].Body != "one" || tl.Timeline.Nodes[0].Action != "create" {
		t.Fatalf("timeline = %+v", tl.Timeline.Nodes)
	}
	owner.do(q, map[string]any{"i": i.ID, "l": 2}).decode(t, "issue", &tl)
	if len(tl.Timeline.Nodes) != 2 || tl.Timeline.Nodes[0].Typename != "Comment" || !tl.Timeline.PageInfo.HasPreviousPage {
		t.Fatalf("last 2 = %+v", tl.Timeline)
	}
	owner.do(q, map[string]any{"i": i.ID, "l": 2, "b": *tl.Timeline.PageInfo.StartCursor}).decode(t, "issue", &tl)
	if len(tl.Timeline.Nodes) != 1 || tl.Timeline.Nodes[0].Action != "create" || tl.Timeline.PageInfo.HasPreviousPage {
		t.Fatalf("older = %+v", tl.Timeline)
	}
}
