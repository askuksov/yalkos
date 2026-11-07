// Package event defines the changes that webhooks are built from. Domain
// services describe each change in these terms and hand it to an Emitter
// inside the transaction that made the change.
package event

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/domain/audit"
)

// Resource is a kind of entity a webhook can subscribe to.
type Resource string

// Resources.
const (
	Issue     Resource = "issue"
	Comment   Resource = "comment"
	Label     Resource = "label"
	Milestone Resource = "milestone"
)

// Resources returns all resources in canonical order.
func Resources() []Resource {
	return []Resource{Issue, Comment, Label, Milestone}
}

// Action is what happened to an entity.
type Action string

// Actions.
const (
	Create Action = "create"
	Update Action = "update"
	Remove Action = "remove"
)

// Change is one change of one entity.
type Change struct {
	Resource Resource
	Action   Action
	EntityID uuid.UUID
	// ProjectIDs: the entity's project, both projects for a move, nil for labels.
	ProjectIDs []uuid.UUID
	ActorID    uuid.UUID
	At         time.Time
	// Diff and PrevUpdatedAt describe an update only.
	Diff          map[string]audit.Change
	PrevUpdatedAt time.Time
	// Purged marks an issue removed for good (its project was deleted).
	Purged bool
}

// Emitter records a change in the caller's transaction.
type Emitter interface {
	Emit(ctx context.Context, tx pgx.Tx, c Change) error
}
