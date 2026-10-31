// Package timeline merges an issue's comments and change events into one
// time-ordered, keyset-paginated list.
package timeline

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/store/q"
)

// Item kinds.
const (
	Comment = "comment"
	Event   = "event"
)

// Item is a comment or an issue audit event, by id.
type Item struct {
	Kind string
	ID   uuid.UUID
	At   time.Time
}

const cursorField = "at"

// List returns one page of the issue's timeline, oldest first.
func List(ctx context.Context, db *q.Queries, issueID uuid.UUID, a page.Args) (page.Result[Item], error) {
	w, err := a.Window()
	if err != nil {
		return page.Result[Item]{}, err
	}
	var at *time.Time
	var id *uuid.UUID
	if w.Cursor != nil {
		field := "after"
		if !w.Forward {
			field = "before"
		}
		c, err := page.Decode(*w.Cursor, field)
		if err != nil {
			return page.Result[Item]{}, err
		}
		var t time.Time
		if c.F != cursorField || c.D != "asc" || json.Unmarshal(c.V, &t) != nil {
			return page.Result[Item]{}, kit.Input(field, kit.ReasonInvalidFormat, "invalid cursor")
		}
		at, id = &t, &c.ID
	}
	var items []Item
	if w.Forward {
		rows, err := db.TimelineForward(ctx, q.TimelineForwardParams{IssueID: issueID, AfterAt: at, AfterID: id, Lim: int32(w.Limit + 1)})
		if err != nil {
			return page.Result[Item]{}, fmt.Errorf("timeline: %w", err)
		}
		for _, r := range rows {
			items = append(items, Item{Kind: r.Kind, ID: r.ID, At: r.At})
		}
	} else {
		rows, err := db.TimelineBackward(ctx, q.TimelineBackwardParams{IssueID: issueID, BeforeAt: at, BeforeID: id, Lim: int32(w.Limit + 1)})
		if err != nil {
			return page.Result[Item]{}, fmt.Errorf("timeline: %w", err)
		}
		for _, r := range rows {
			items = append(items, Item{Kind: r.Kind, ID: r.ID, At: r.At})
		}
	}
	return page.Finish(items, w, func(it Item) string {
		v, _ := json.Marshal(it.At)
		return page.Encode(page.Cursor{F: cursorField, D: "asc", V: v, ID: it.ID})
	}), nil
}
