package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/store/q"
)

// dateTimeLayout is the payload date-time format: UTC, milliseconds, truncated.
const dateTimeLayout = "2006-01-02T15:04:05.000Z"

// Built is a payload built for one change. UpdatedFrom nil means the key is
// absent; URL nil is stored as JSON null.
type Built struct {
	Type                     string
	Data, UpdatedFrom, Actor json.RawMessage
	URL                      *string
}

// Builder builds payload parts from the database inside the change's transaction.
type Builder interface {
	Build(ctx context.Context, db *q.Queries, c event.Change) (Built, error)
	Actor(ctx context.Context, db *q.Queries, userID uuid.UUID) (json.RawMessage, error)
}

// Inserter queues River jobs in a caller's transaction; *river.Client[pgx.Tx]
// satisfies it.
type Inserter interface {
	InsertManyTx(ctx context.Context, tx pgx.Tx, params []river.InsertManyParams) ([]*rivertype.JobInsertResult, error)
}

// envelope is the stored payload; webhookTimestamp is added per attempt.
type envelope struct {
	Action         event.Action    `json:"action"`
	Type           string          `json:"type"`
	Actor          json.RawMessage `json:"actor"`
	CreatedAt      string          `json:"createdAt"`
	Data           json.RawMessage `json:"data"`
	URL            *string         `json:"url"`
	UpdatedFrom    json.RawMessage `json:"updatedFrom,omitempty"`
	OrganizationID uuid.UUID       `json:"organizationId"`
	WebhookID      uuid.UUID       `json:"webhookId"`
}

// Emit records a change for every matching webhook: one delivery row and one
// River job each, in the caller's transaction. It implements event.Emitter.
// An error must fail the change so that no event is lost.
func (s *Service) Emit(ctx context.Context, tx pgx.Tx, c event.Change) error {
	db := s.q.WithTx(tx)
	hooks, err := db.MatchWebhooks(ctx, q.MatchWebhooksParams{
		Resource:   string(c.Resource),
		ProjectIds: c.ProjectIDs,
		// Labels are workspace-wide: their events go to every scope.
		AnyScope: c.Resource == event.Label,
	})
	if err != nil {
		return fmt.Errorf("match webhooks: %w", err)
	}
	if len(hooks) == 0 {
		return nil
	}
	if s.opts.Builder == nil || s.opts.Queue == nil {
		return errors.New("webhook: builder and queue are required")
	}
	built, err := s.opts.Builder.Build(ctx, db, c)
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}
	jobs := make([]river.InsertManyParams, 0, len(hooks))
	for _, h := range hooks {
		payload, err := json.Marshal(envelope{
			Action: c.Action, Type: built.Type, Actor: built.Actor, CreatedAt: c.At.UTC().Format(dateTimeLayout),
			Data: built.Data, URL: built.URL, UpdatedFrom: built.UpdatedFrom,
			OrganizationID: h.WorkspaceID, WebhookID: h.ID,
		})
		if err != nil {
			return fmt.Errorf("encode payload: %w", err)
		}
		id, err := db.InsertDelivery(ctx, q.InsertDeliveryParams{
			WebhookID: h.ID, Resource: string(c.Resource), Action: string(c.Action),
			EntityID: &c.EntityID, Payload: payload, CreatedAt: c.At,
		})
		if err != nil {
			return fmt.Errorf("insert delivery: %w", err)
		}
		jobs = append(jobs, river.InsertManyParams{Args: DeliveryArgs{DeliveryID: id}})
	}
	if _, err := s.opts.Queue.InsertManyTx(ctx, tx, jobs); err != nil {
		return fmt.Errorf("queue deliveries: %w", err)
	}
	return nil
}
