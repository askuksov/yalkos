package webhook

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/store/q"
)

const (
	maxDeliveries = 50

	// KindDelivery is the River job kind of a webhook delivery.
	KindDelivery = "webhook_delivery"
	// QueueDeliveries is the River queue of webhook deliveries.
	QueueDeliveries = "webhooks"
	// MaxAttempts is how many times a delivery is tried.
	MaxAttempts = 4
)

// DeliveryArgs are the arguments of a delivery job.
type DeliveryArgs struct {
	DeliveryID uuid.UUID `json:"deliveryId"`
}

// Kind implements river.JobArgs.
func (DeliveryArgs) Kind() string { return KindDelivery }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (DeliveryArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueDeliveries, MaxAttempts: MaxAttempts}
}

// Delivery is one event sent to one webhook, without its payload.
type Delivery struct {
	ID, WebhookID  uuid.UUID
	Resource       string
	Action         string
	EntityID       *uuid.UUID
	Status         string
	Attempts       int
	ResponseStatus *int
	LastError      *string
	DurationMS     *int
	CreatedAt      time.Time
	LastAttemptAt  *time.Time
	NextAttemptAt  *time.Time
	FinishedAt     *time.Time
}

func intPtr(v *int32) *int {
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}

// Deliveries returns the newest deliveries of a webhook, newest first.
// limit is 1–50.
func (s *Service) Deliveries(ctx context.Context, actor account.User, webhookID uuid.UUID, limit int) ([]Delivery, error) {
	if err := authorize(actor); err != nil {
		return nil, err
	}
	if err := validDeliveryLimit(limit); err != nil {
		return nil, err
	}
	rows, err := s.q.ListDeliveries(ctx, q.ListDeliveriesParams{WebhookID: webhookID, MaxRows: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("list deliveries: %w", err)
	}
	out := make([]Delivery, len(rows))
	for i, r := range rows {
		out[i] = deliveryFromRow(r)
	}
	return out, nil
}

// LatestDeliveries returns the newest deliveries of each given webhook, newest
// first, with the same rule and limit as Deliveries. A webhook without
// deliveries (or one that does not exist) has no entry.
func (s *Service) LatestDeliveries(ctx context.Context, actor account.User, webhookIDs []uuid.UUID, limit int) (map[uuid.UUID][]Delivery, error) {
	if err := authorize(actor); err != nil {
		return nil, err
	}
	if err := validDeliveryLimit(limit); err != nil {
		return nil, err
	}
	rows, err := s.q.LatestDeliveries(ctx, q.LatestDeliveriesParams{WebhookIds: webhookIDs, MaxRows: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("latest deliveries: %w", err)
	}
	out := make(map[uuid.UUID][]Delivery, len(webhookIDs))
	for _, r := range rows {
		d := deliveryFromRow(q.ListDeliveriesRow(r))
		out[d.WebhookID] = append(out[d.WebhookID], d)
	}
	return out, nil
}

// LatestFailures returns up to 50 retrying or failed deliveries of each given
// webhook, the newest failure first (see FailedAt). A webhook without failures
// (or one that does not exist) has no entry.
func (s *Service) LatestFailures(ctx context.Context, actor account.User, webhookIDs []uuid.UUID) (map[uuid.UUID][]Delivery, error) {
	if err := authorize(actor); err != nil {
		return nil, err
	}
	rows, err := s.q.LatestFailures(ctx, q.LatestFailuresParams{WebhookIds: webhookIDs, MaxRows: maxDeliveries})
	if err != nil {
		return nil, fmt.Errorf("latest failures: %w", err)
	}
	out := make(map[uuid.UUID][]Delivery, len(webhookIDs))
	for _, r := range rows {
		d := deliveryFromRow(q.ListDeliveriesRow(r))
		out[d.WebhookID] = append(out[d.WebhookID], d)
	}
	return out, nil
}

// FailedAt is when a retrying or failed delivery last failed: the end of its
// final attempt, the start of its last attempt while it is retried, or the
// moment it was failed as interrupted. LatestFailures orders by it.
func (d Delivery) FailedAt() time.Time {
	switch {
	case d.FinishedAt != nil:
		return *d.FinishedAt
	case d.LastAttemptAt != nil:
		return *d.LastAttemptAt
	}
	return d.CreatedAt
}

func validDeliveryLimit(limit int) error {
	if limit < 1 || limit > maxDeliveries {
		return kit.Input("first", kit.ReasonOutOfRange, "must be 1–50").With("min", 1).With("max", maxDeliveries)
	}
	return nil
}

func deliveryFromRow(r q.ListDeliveriesRow) Delivery {
	return Delivery{
		ID: r.ID, WebhookID: r.WebhookID, Resource: r.Resource, Action: r.Action, EntityID: r.EntityID,
		Status: r.Status, Attempts: int(r.Attempts), ResponseStatus: intPtr(r.ResponseStatus), LastError: r.LastError,
		DurationMS: intPtr(r.DurationMs), CreatedAt: r.CreatedAt, LastAttemptAt: r.LastAttemptAt,
		NextAttemptAt: r.NextAttemptAt, FinishedAt: r.FinishedAt,
	}
}
