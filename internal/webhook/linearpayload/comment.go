package linearpayload

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/webhook"
	"github.com/askuksov/yalkos/internal/store/q"
)

// commentUnset holds the fields of CommentWebhookPayload a comment has no
// value for: they are present and null.
type commentUnset struct {
	ArchivedAt         any `json:"archivedAt"`
	BotActor           any `json:"botActor"`
	DocumentContent    any `json:"documentContent"`
	DocumentContentID  any `json:"documentContentId"`
	ExternalUser       any `json:"externalUser"`
	ExternalUserID     any `json:"externalUserId"`
	InitiativeID       any `json:"initiativeId"`
	InitiativeUpdate   any `json:"initiativeUpdate"`
	InitiativeUpdateID any `json:"initiativeUpdateId"`
	Parent             any `json:"parent"`
	ParentID           any `json:"parentId"`
	PostID             any `json:"postId"`
	ProjectID          any `json:"projectId"`
	ProjectUpdate      any `json:"projectUpdate"`
	ProjectUpdateID    any `json:"projectUpdateId"`
	QuotedText         any `json:"quotedText"`
	ResolvedAt         any `json:"resolvedAt"`
	ResolvingCommentID any `json:"resolvingCommentId"`
	ResolvingUserID    any `json:"resolvingUserId"`
	SyncedWith         any `json:"syncedWith"`
}

// commentIssue is IssueChildWebhookPayload.
type commentIssue struct {
	ID         string  `json:"id"`
	Identifier string  `json:"identifier"`
	Title      string  `json:"title"`
	URL        string  `json:"url"`
	TeamID     string  `json:"teamId"`
	Team       teamRef `json:"team"`
}

// commentData is CommentWebhookPayload.
type commentData struct {
	ID           string       `json:"id"`
	Body         string       `json:"body"`
	IssueID      string       `json:"issueId"`
	Issue        commentIssue `json:"issue"`
	UserID       string       `json:"userId"`
	User         *userRef     `json:"user"`
	CreatedAt    string       `json:"createdAt"`
	UpdatedAt    string       `json:"updatedAt"`
	EditedAt     *string      `json:"editedAt"`
	ReactionData []any        `json:"reactionData"`
	commentUnset
}

// comment builds a Comment event; its link is the issue's with the comment's anchor.
func (b *Builder) comment(ctx context.Context, db *q.Queries, c event.Change) (webhook.Built, error) {
	cm, err := db.GetComment(ctx, c.EntityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return webhook.Built{}, fmt.Errorf("comment %s not found", c.EntityID)
	}
	if err != nil {
		return webhook.Built{}, fmt.Errorf("get comment: %w", err)
	}
	row, err := getIssue(ctx, db, cm.IssueID)
	if err != nil {
		return webhook.Built{}, err
	}
	people, err := b.users(ctx, db, &cm.AuthorID)
	if err != nil {
		return webhook.Built{}, err
	}
	identifier := fmt.Sprintf("%s-%d", row.ProjectKey, row.Issue.Number)
	issueURL := IssueURL(b.base, identifier)
	teamID := row.Issue.ProjectID.String()
	d := commentData{
		ID: cm.ID.String(), Body: cm.Body, IssueID: cm.IssueID.String(),
		Issue: commentIssue{
			ID: row.Issue.ID.String(), Identifier: identifier, Title: row.Issue.Title, URL: issueURL,
			TeamID: teamID, Team: teamRef{ID: teamID, Key: row.ProjectKey, Name: row.ProjectName},
		},
		UserID: cm.AuthorID.String(), User: userOf(people, &cm.AuthorID),
		CreatedAt: ts(cm.CreatedAt), UpdatedAt: ts(cm.UpdatedAt), EditedAt: tsp(cm.EditedAt),
		ReactionData: []any{},
	}
	url := issueURL + "#comment-" + cm.ID.String()
	var uf map[string]any
	if c.Action == event.Update {
		uf = commentUpdatedFrom(c)
	}
	return built("Comment", d, &url, uf)
}
