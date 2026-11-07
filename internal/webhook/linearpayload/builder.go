// Package linearpayload builds the Linear-compatible payloads of webhook
// events: the entity as `data`, the previous values as `updatedFrom`, the
// entity's link and the actor. It is the one place outside the Linear-compatible
// API where the Linear-compatible names (team, issue label, project for a milestone) appear.
package linearpayload

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/webhook"
	"github.com/askuksov/yalkos/internal/store/q"
)

// Builder implements webhook.Builder.
type Builder struct {
	base string
}

var _ webhook.Builder = (*Builder)(nil)

// New returns a builder whose links start with baseURL (the public URL).
func New(baseURL string) *Builder {
	return &Builder{base: strings.TrimSuffix(baseURL, "/")}
}

// Build builds the payload of a change from the database as the caller's
// transaction sees it. An update reports the diff's previous values, so only
// fields a payload has are looked at. A change of a row about to disappear is
// built from the row as it still is.
func (b *Builder) Build(ctx context.Context, db *q.Queries, c event.Change) (webhook.Built, error) {
	switch c.Resource {
	case event.Issue:
		return b.issue(ctx, db, c)
	case event.Comment:
		return b.comment(ctx, db, c)
	case event.Label:
		return b.label(ctx, db, c)
	case event.Milestone:
		return b.milestone(ctx, db, c)
	}
	return webhook.Built{}, fmt.Errorf("linearpayload: unknown resource %q", c.Resource)
}

// Actor builds the actor object of an event.
func (b *Builder) Actor(ctx context.Context, db *q.Queries, userID uuid.UUID) (json.RawMessage, error) {
	u, err := db.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get actor: %w", err)
	}
	r := b.user(u)
	return json.Marshal(struct {
		userRef
		Type string `json:"type"`
	}{r, "user"})
}

// built encodes the parts of a payload; uf nil (no update) leaves updatedFrom absent.
func built(typ string, data any, url *string, uf map[string]any) (webhook.Built, error) {
	d, err := json.Marshal(data)
	if err != nil {
		return webhook.Built{}, fmt.Errorf("encode %s data: %w", typ, err)
	}
	out := webhook.Built{Type: typ, Data: d, URL: url}
	if uf != nil {
		if out.UpdatedFrom, err = json.Marshal(uf); err != nil {
			return webhook.Built{}, fmt.Errorf("encode %s updatedFrom: %w", typ, err)
		}
	}
	return out, nil
}

func (b *Builder) user(u q.User) userRef {
	return userRef{ID: u.ID.String(), Name: u.Name, Email: u.Email, URL: b.base}
}

// users reads the users with the given ids (nil ones skipped) by id.
func (b *Builder) users(ctx context.Context, db *q.Queries, ids ...*uuid.UUID) (map[uuid.UUID]userRef, error) {
	var want []uuid.UUID
	for _, id := range ids {
		if id != nil {
			want = append(want, *id)
		}
	}
	rows, err := db.ListUsersByIDs(ctx, want)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	out := make(map[uuid.UUID]userRef, len(rows))
	for _, u := range rows {
		out[u.ID] = b.user(u)
	}
	return out, nil
}

// userOf finds a user read by users; nil for none.
func userOf(m map[uuid.UUID]userRef, id *uuid.UUID) *userRef {
	if id == nil {
		return nil
	}
	if u, ok := m[*id]; ok {
		return &u
	}
	return nil
}

// getProject reads a project by id.
func getProject(ctx context.Context, db *q.Queries, id uuid.UUID) (q.Project, error) {
	p, err := db.GetProject(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return q.Project{}, fmt.Errorf("project %s not found", id)
	}
	if err != nil {
		return q.Project{}, fmt.Errorf("get project: %w", err)
	}
	return p, nil
}
