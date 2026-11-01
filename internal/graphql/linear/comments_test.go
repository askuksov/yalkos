package linear_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/comment"
)

type commentNode struct {
	ID           string
	Body         string
	IssueID      *string `json:"issueId"`
	Issue        *struct{ ID, Identifier string }
	User         *struct{ ID string }
	URL          string
	EditedAt     *string
	CreatedAt    string
	UpdatedAt    string
	Reactions    []any
	ReactionData any
	Parent       *struct{ ID string }
}

const commentFields = `id body issueId issue { id identifier } user { id } url editedAt createdAt updatedAt reactions { id } reactionData parent { id }`

func (f issueFix) comment(t *testing.T, ref, body string) comment.Comment {
	t.Helper()
	c, err := f.deps.Comments.Create(context.Background(), f.owner, f.i[ref].ID, body, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func bodies(nodes []commentNode) []string {
	out := []string{}
	for _, n := range nodes {
		out = append(out, n.Body)
	}
	return out
}

func TestIssueComments(t *testing.T) {
	f := newIssueFix(t)
	ctx := context.Background()
	first := f.comment(t, "YLK-1", "first x")
	f.comment(t, "YLK-1", "second")
	third := f.comment(t, "YLK-1", "third x")
	f.comment(t, "YLK-2", "elsewhere")
	if _, err := f.deps.Comments.Update(ctx, f.owner, first.ID, "first x edited"); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Issue struct {
			Comments, Xs, Paged struct {
				Nodes    []commentNode
				PageInfo struct{ HasNextPage bool }
			}
		}
	}
	f.gql(t, f.ownerKey, `{ issue(id: "YLK-1") { comments { nodes { `+commentFields+` } }
		xs: comments(filter: { body: { contains: "x" } }) { nodes { body } }
		paged: comments(first: 2, orderBy: updatedAt) { nodes { body } pageInfo { hasNextPage } } } }`, nil).ok(t, &out)
	cs := out.Issue.Comments.Nodes
	if got := bodies(cs); !slices.Equal(got, []string{"first x edited", "second", "third x"}) {
		t.Fatalf("comments = %v", got)
	}
	c := cs[0]
	y1 := f.i["YLK-1"].ID.String()
	if c.ID != first.ID.String() || c.IssueID == nil || *c.IssueID != y1 || c.Issue.Identifier != "YLK-1" || c.User.ID != f.owner.ID.String() ||
		c.URL != base+"/issue/YLK-1" || c.EditedAt == nil || c.Reactions == nil || len(c.Reactions) != 0 || c.Parent != nil {
		t.Fatalf("comment = %+v", c)
	}
	if cs[1].EditedAt != nil {
		t.Fatalf("unedited comment has editedAt: %+v", cs[1])
	}
	if got := bodies(out.Issue.Xs.Nodes); !slices.Equal(got, []string{"first x edited", "third x"}) {
		t.Fatalf("filtered = %v", got)
	}
	if got := bodies(out.Issue.Paged.Nodes); !slices.Equal(got, []string{"first x edited", "second"}) || !out.Issue.Paged.PageInfo.HasNextPage {
		t.Fatalf("paged (oldest first, orderBy ignored) = %v %+v", got, out.Issue.Paged.PageInfo)
	}
	var byUser struct {
		Issue struct{ Comments struct{ Nodes []commentNode } }
	}
	f.gql(t, f.ownerKey, `{ issue(id: "YLK-1") { comments(filter: { user: { isMe: { eq: false } } }) { nodes { body } } } }`, nil).ok(t, &byUser)
	if len(byUser.Issue.Comments.Nodes) != 0 {
		t.Fatalf("by others = %v", byUser.Issue.Comments.Nodes)
	}
	f.gql(t, f.ownerKey, `{ issue(id: "YLK-1") { comments(filter: { issue: { id: { eq: "YLK-2" } } }) { nodes { body } } } }`, nil).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid filter: leave filter.issue out of issue(id).comments", "filter")
	_ = third
}

func TestRootCommentsNeedIssue(t *testing.T) {
	f := newIssueFix(t)
	f.comment(t, "YLK-1", "a")
	f.comment(t, "YLK-2", "b")
	msg := "comments needs filter.issue.id.eq; or use issue(id).comments"
	f.gql(t, f.ownerKey, `{ comments { nodes { id } } }`, nil).fails(t).want(t, "INVALID_INPUT", "invalid input", msg, "filter")
	f.gql(t, f.ownerKey, `{ comments(filter: { body: { eq: "a" } }) { nodes { id } } }`, nil).fails(t).want(t, "INVALID_INPUT", "invalid input", msg, "filter")
	for _, ref := range []string{"YLK-1", f.i["YLK-1"].ID.String()} {
		var out struct{ Comments struct{ Nodes []commentNode } }
		f.gql(t, f.ownerKey, `query($id: ID) { comments(filter: { issue: { id: { eq: $id } } }) { nodes { body issue { identifier } } } }`,
			map[string]any{"id": ref}).ok(t, &out)
		if got := bodies(out.Comments.Nodes); !slices.Equal(got, []string{"a"}) {
			t.Fatalf("%s: %v", ref, got)
		}
	}
	f.gql(t, f.ownerKey, `{ comments(filter: { issue: { id: { eq: "YLK-99" } } }) { nodes { id } } }`, nil).fails(t).
		want(t, "NOT_FOUND", "invalid input", "Issue not found", "")
}

func TestCommentByID(t *testing.T) {
	f := newIssueFix(t)
	c := f.comment(t, "YLK-2", "hello")
	var out struct{ Comment commentNode }
	f.gql(t, f.ownerKey, `query($id: String) { comment(id: $id) { `+commentFields+` } }`, map[string]any{"id": c.ID.String()}).ok(t, &out)
	if out.Comment.Body != "hello" || out.Comment.URL != base+"/issue/YLK-2" || out.Comment.Issue.Identifier != "YLK-2" {
		t.Fatalf("comment = %+v", out.Comment)
	}
	f.gql(t, f.ownerKey, `{ comment(hash: "abc") { id } }`, nil).fails(t).want(t, "INVALID_INPUT", "invalid input", "Invalid id: id is required", "id")
	f.gql(t, f.ownerKey, `query($id: String) { comment(id: $id) { id } }`, map[string]any{"id": uuid.NewString()}).fails(t).
		want(t, "NOT_FOUND", "invalid input", "Comment not found", "")
}

type commentPayload struct {
	Success    bool
	LastSyncID float64 `json:"lastSyncId"`
	Comment    commentNode
	EntityID   string `json:"entityId"`
}

func TestCommentCreateUpdateDelete(t *testing.T) {
	f := newIssueFix(t)
	create := `mutation($input: CommentCreateInput!) { commentCreate(input: $input) { success lastSyncId comment { ` + commentFields + ` } } }`
	var c struct{ CommentCreate commentPayload }
	f.gql(t, f.miaKey, create, map[string]any{"input": map[string]any{"issueId": "YLK-1", "body": "from Mia"}}).ok(t, &c)
	cm := c.CommentCreate.Comment
	if !c.CommentCreate.Success || c.CommentCreate.LastSyncID <= 0 || cm.Body != "from Mia" || cm.User.ID != f.mia.ID.String() || cm.Issue.Identifier != "YLK-1" {
		t.Fatalf("create = %+v", c.CommentCreate)
	}
	f.gql(t, f.ownerKey, create, map[string]any{"input": map[string]any{"issueId": "YLK-1"}}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid body: body is required", "body")
	f.gql(t, f.ownerKey, create, map[string]any{"input": map[string]any{"body": "x"}}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid issueId: issueId is required", "issueId")
	f.gql(t, f.ownerKey, create, map[string]any{"input": map[string]any{"issueId": "YLK-99", "body": "x"}}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid issueId: no such issue", "issueId")
	update := `mutation($id: String!, $input: CommentUpdateInput!, $skip: Boolean) { commentUpdate(id: $id, input: $input, skipEditedAt: $skip) { success lastSyncId comment { ` + commentFields + ` } } }`
	var u struct{ CommentUpdate commentPayload }
	f.gql(t, f.miaKey, update, map[string]any{"id": cm.ID, "input": map[string]any{"body": "edited"}, "skip": true}).ok(t, &u)
	if u.CommentUpdate.Comment.Body != "edited" || u.CommentUpdate.Comment.EditedAt == nil || !u.CommentUpdate.Success {
		t.Fatalf("update = %+v", u.CommentUpdate)
	}
	f.gql(t, f.ownerKey, update, map[string]any{"id": cm.ID, "input": map[string]any{"body": "hijack"}}).fails(t).
		want(t, "FORBIDDEN", "forbidden", "You do not have permission to do this", "")
	var d struct{ CommentDelete commentPayload }
	f.gql(t, f.miaKey, `mutation($id: String!) { commentDelete(id: $id) { success lastSyncId entityId } }`, map[string]any{"id": cm.ID}).ok(t, &d)
	if !d.CommentDelete.Success || d.CommentDelete.EntityID != cm.ID {
		t.Fatalf("delete = %+v", d.CommentDelete)
	}
	f.gql(t, f.ownerKey, `query($id: String) { comment(id: $id) { id } }`, map[string]any{"id": cm.ID}).fails(t).
		want(t, "NOT_FOUND", "invalid input", "Comment not found", "")
}
