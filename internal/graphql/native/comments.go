package native

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/attachment"
	"github.com/askuksov/yalkos/internal/domain/comment"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/domain/timeline"
)

// Comment is the GraphQL Comment; author and issue resolve through loaders.
type Comment struct {
	ID        string
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
	EditedAt  *time.Time

	authorID, issueID uuid.UUID
}

// IsTimelineItem marks Comment as a TimelineItem.
func (Comment) IsTimelineItem() {}

// IsTimelineItem marks HistoryEntry as a TimelineItem.
func (HistoryEntry) IsTimelineItem() {}

func commentOut(c comment.Comment) *Comment {
	return &Comment{
		ID: c.ID.String(), Body: c.Body, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, EditedAt: c.EditedAt,
		authorID: c.AuthorID, issueID: c.IssueID,
	}
}

func commentsOut(res page.Result[comment.Comment]) *CommentConnection {
	conn := &CommentConnection{
		Nodes:    make([]Comment, 0, len(res.Items)),
		Edges:    make([]CommentEdge, 0, len(res.Items)),
		PageInfo: pageInfoOut(res.Info),
	}
	for n, c := range res.Items {
		node := commentOut(c)
		conn.Nodes = append(conn.Nodes, *node)
		conn.Edges = append(conn.Edges, CommentEdge{Node: node, Cursor: res.Cursors[n]})
	}
	return conn
}

// Attachment is the GraphQL Attachment; uploader resolves through the loader.
type Attachment struct {
	ID          string
	Name        string
	Size        int
	ContentType string
	URL         string
	Inline      bool
	CreatedAt   time.Time

	uploaderID uuid.UUID
}

func attachmentOut(a attachment.Attachment) Attachment {
	return Attachment{
		ID: a.ID.String(), Name: a.Name, Size: int(a.Size), ContentType: a.ContentType, URL: attachment.URL(a),
		Inline: attachment.Inline(a.ContentType), CreatedAt: a.CreatedAt, uploaderID: a.UploaderID,
	}
}

// timelineOut resolves a timeline page: comments and events load in one
// query each; an item deleted meanwhile is skipped.
func (r *Resolver) timelineOut(ctx context.Context, res page.Result[timeline.Item]) (*TimelineConnection, error) {
	var commentIDs, eventIDs []uuid.UUID
	for _, it := range res.Items {
		if it.Kind == timeline.Comment {
			commentIDs = append(commentIDs, it.ID)
		} else {
			eventIDs = append(eventIDs, it.ID)
		}
	}
	nodes := map[uuid.UUID]TimelineItem{}
	if len(commentIDs) > 0 {
		cs, err := r.Comments.ByIDs(ctx, commentIDs)
		if err != nil {
			return nil, err
		}
		for _, c := range cs {
			nodes[c.ID] = commentOut(c)
		}
	}
	if len(eventIDs) > 0 {
		es, err := r.Audit.ByIDs(ctx, eventIDs)
		if err != nil {
			return nil, err
		}
		for _, e := range es {
			nodes[e.ID] = historyEntryOut(e)
		}
	}
	conn := &TimelineConnection{
		Nodes:    make([]TimelineItem, 0, len(res.Items)),
		Edges:    make([]TimelineEdge, 0, len(res.Items)),
		PageInfo: pageInfoOut(res.Info),
	}
	for n, it := range res.Items {
		node, ok := nodes[it.ID]
		if !ok {
			continue
		}
		conn.Nodes = append(conn.Nodes, node)
		conn.Edges = append(conn.Edges, TimelineEdge{Node: node, Cursor: res.Cursors[n]})
	}
	return conn, nil
}
