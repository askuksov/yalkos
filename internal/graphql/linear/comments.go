package linear

import (
	"context"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/comment"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/page"
)

// commentOut maps a comment; its URL is the issue's.
func (r *Resolver) commentOut(c comment.Comment, issueIdentifier string) *Comment {
	issueID := c.IssueID.String()
	return &Comment{
		ID: c.ID.String(), Body: c.Body, IssueID: &issueID, URL: r.d.base() + "/issue/" + issueIdentifier,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, EditedAt: c.EditedAt,
		Reactions: []Reaction{}, ReactionData: JSONObject{V: []any{}}, Src: c,
	}
}

// issueComments reads the whole thread of an issue, oldest first.
func (r *Resolver) issueComments(ctx context.Context, issueID uuid.UUID) ([]comment.Comment, error) {
	var out []comment.Comment
	a := page.Args{First: new(page.Max)}
	for {
		res, err := r.d.Comments.List(ctx, issueID, a)
		if err != nil {
			return nil, err
		}
		out = append(out, res.Items...)
		if !res.Info.HasNext {
			return out, nil
		}
		a.After = res.Info.End
	}
}

func (r *Resolver) commentMatch(ctx context.Context, f *CommentFilter, c comment.Comment, me uuid.UUID) (bool, error) {
	if f == nil {
		return true, nil
	}
	if ok, err := matchID("id", f.ID, c.ID); err != nil || !ok {
		return false, err
	}
	now := r.d.now()
	if ok, err := matchTime("createdAt", f.CreatedAt, c.CreatedAt, now); err != nil || !ok {
		return false, err
	}
	if ok, err := matchTime("updatedAt", f.UpdatedAt, c.UpdatedAt, now); err != nil || !ok {
		return false, err
	}
	if f.User != nil {
		users, err := loadersFrom(ctx).allUsers.get(ctx, func(ctx context.Context) ([]account.User, error) {
			return r.d.Accounts.ListMembers(ctx, account.User{})
		})
		if err != nil {
			return false, err
		}
		var author *account.User
		for i := range users {
			if users[i].ID == c.AuthorID {
				author = &users[i]
			}
		}
		if author == nil {
			return false, nil
		}
		if ok, err := userMatch(f.User, *author, me); err != nil || !ok {
			return false, err
		}
	}
	return matchString(f.Body, c.Body), nil
}

// commentConn filters and pages an issue's thread in memory; comments stay
// oldest first whatever orderBy says.
func (r *Resolver) commentConn(ctx context.Context, it issue.Issue, f *CommentFilter, a page.Args) (*CommentConnection, error) {
	me, err := r.actor(ctx)
	if err != nil {
		return nil, err
	}
	all, err := r.issueComments(ctx, it.ID)
	if err != nil {
		return nil, err
	}
	var kept []comment.Comment
	for _, c := range all {
		ok, err := r.commentMatch(ctx, f, c, me.ID)
		if err != nil {
			return nil, err
		}
		if ok {
			kept = append(kept, c)
		}
	}
	items, info, err := pageOf(kept, func(c comment.Comment) uuid.UUID { return c.ID }, a)
	if err != nil {
		return nil, err
	}
	out := &CommentConnection{PageInfo: info, Nodes: make([]Comment, len(items)), Edges: make([]CommentEdge, len(items))}
	for i, c := range items {
		n := r.commentOut(c, it.Identifier)
		out.Nodes[i] = *n
		out.Edges[i] = CommentEdge{Node: n, Cursor: cursorOf(c.ID)}
	}
	return out, nil
}

// rootComments needs the issue in filter.issue.id.eq.
func (r *Resolver) rootComments(ctx context.Context, f *CommentFilter, a page.Args) (*CommentConnection, error) {
	if _, err := r.actor(ctx); err != nil {
		return nil, err
	}
	if f == nil || f.Issue == nil || f.Issue.ID == nil || f.Issue.ID.Eq == nil {
		return nil, badArg("filter", "comments needs filter.issue.id.eq; or use issue(id).comments")
	}
	it, err := r.issueByRef(ctx, "", *f.Issue.ID.Eq)
	if err != nil {
		return nil, err
	}
	rest := *f
	rest.Issue = nil
	return r.commentConn(ctx, it, &rest, a)
}

// commentByID loads one comment with its issue.
func (r *Resolver) commentByID(ctx context.Context, id *string) (*Comment, error) {
	if _, err := r.actor(ctx); err != nil {
		return nil, err
	}
	if id == nil {
		return nil, badArg("id", "Invalid id: id is required")
	}
	cid, err := parseID("id", *id)
	if err != nil {
		return nil, err
	}
	cs, err := r.d.Comments.ByIDs(ctx, []uuid.UUID{cid})
	if err != nil {
		return nil, err
	}
	if len(cs) == 0 {
		return nil, kit.NotFoundError{Entity: "Comment"}
	}
	it, err := loadersFrom(ctx).issues.Load(ctx, cs[0].IssueID)
	if err != nil {
		return nil, err
	}
	return r.commentOut(cs[0], it.Identifier), nil
}

func (r *Resolver) commentPayload(ctx context.Context, c comment.Comment) (*CommentPayload, error) {
	it, err := loadersFrom(ctx).issues.Load(ctx, c.IssueID)
	if err != nil {
		return nil, err
	}
	return &CommentPayload{Success: true, LastSyncID: lastSync(c.UpdatedAt), Comment: r.commentOut(c, it.Identifier)}, nil
}

func (r *Resolver) createComment(ctx context.Context, in CommentCreateInput) (*CommentPayload, error) {
	actor, err := r.actor(ctx)
	if err != nil {
		return nil, err
	}
	if in.IssueID == nil {
		return nil, badArg("issueId", "Invalid issueId: issueId is required")
	}
	if in.Body == nil {
		return nil, badArg("body", "Invalid body: body is required")
	}
	it, err := r.issueByRef(ctx, "issueId", *in.IssueID)
	if err != nil {
		return nil, err
	}
	c, err := r.d.Comments.Create(ctx, actor, it.ID, *in.Body, nil)
	if err != nil {
		return nil, err
	}
	return r.commentPayload(ctx, c)
}

func (r *Resolver) updateComment(ctx context.Context, id string, in CommentUpdateInput) (*CommentPayload, error) {
	actor, err := r.actor(ctx)
	if err != nil {
		return nil, err
	}
	cid, err := parseID("id", id)
	if err != nil {
		return nil, err
	}
	if in.Body == nil {
		cur, err := r.commentByID(ctx, &id)
		if err != nil {
			return nil, err
		}
		return &CommentPayload{Success: true, LastSyncID: lastSync(cur.UpdatedAt), Comment: cur}, nil
	}
	c, err := r.d.Comments.Update(ctx, actor, cid, *in.Body)
	if err != nil {
		return nil, err
	}
	return r.commentPayload(ctx, c)
}

func (r *Resolver) deleteComment(ctx context.Context, id string) (*DeletePayload, error) {
	actor, err := r.actor(ctx)
	if err != nil {
		return nil, err
	}
	cid, err := parseID("id", id)
	if err != nil {
		return nil, err
	}
	if err := r.d.Comments.Delete(ctx, actor, cid); err != nil {
		return nil, err
	}
	return &DeletePayload{Success: true, EntityID: id, LastSyncID: lastSync(r.d.now())}, nil
}
