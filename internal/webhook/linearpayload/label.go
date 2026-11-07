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

// labelData is IssueLabelWebhookPayload.
type labelData struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Color           string  `json:"color"`
	Description     *string `json:"description"`
	IsGroup         bool    `json:"isGroup"`
	ParentID        *string `json:"parentId"`
	CreatorID       *string `json:"creatorId"`
	CreatedAt       string  `json:"createdAt"`
	UpdatedAt       string  `json:"updatedAt"`
	TeamID          any     `json:"teamId"`
	InheritedFromID any     `json:"inheritedFromId"`
	ArchivedAt      any     `json:"archivedAt"`
}

// label builds an IssueLabel event; labels have no link.
func (b *Builder) label(ctx context.Context, db *q.Queries, c event.Change) (webhook.Built, error) {
	l, err := db.GetLabel(ctx, c.EntityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return webhook.Built{}, fmt.Errorf("label %s not found", c.EntityID)
	}
	if err != nil {
		return webhook.Built{}, fmt.Errorf("get label: %w", err)
	}
	d := labelData{
		ID: l.ID.String(), Name: l.Name, Color: l.Color, Description: nonEmpty(l.Description), IsGroup: l.IsGroup,
		ParentID: idp(l.ParentID), CreatorID: idp(l.CreatedBy), CreatedAt: ts(l.CreatedAt), UpdatedAt: ts(l.UpdatedAt),
	}
	var uf map[string]any
	if c.Action == event.Update {
		uf = labelUpdatedFrom(c)
	}
	return built("IssueLabel", d, nil, uf)
}
