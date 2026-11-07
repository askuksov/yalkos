package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/store/q"
)

const (
	// maxErrorChars is the longest delivery last_error.
	maxErrorChars = 1000

	// TestType is the payload type of a test delivery.
	TestType = "WebhookTest"
	// TestResource is the resource and the action of a test delivery.
	TestResource = "test"
	testMessage  = "Test delivery from Yalkos"

	// ErrorDisabled is the last_error of a delivery canceled because its webhook is off.
	ErrorDisabled = "webhook disabled"

	statusSucceeded = "succeeded"
	statusRetrying  = "retrying"
	statusFailed    = "failed"
)

var (
	// ErrGone means there is nothing to send: the delivery or its webhook was
	// deleted, or the delivery is already finished.
	ErrGone = errors.New("delivery gone")
	// ErrDisabled means the webhook is off; the delivery was canceled.
	ErrDisabled = errors.New("webhook disabled")
)

// Attempt is what one send needs. It carries the webhook secret: it has no
// printable form that shows it.
type Attempt struct {
	DeliveryID  uuid.UUID
	URL, Secret string
	Type        string
	Envelope    json.RawMessage
}

// String shows the attempt without its secret.
func (a Attempt) String() string {
	return fmt.Sprintf("attempt{delivery: %s, url: %s, type: %s}", a.DeliveryID, a.URL, a.Type)
}

// GoString is String for %#v.
func (a Attempt) GoString() string { return a.String() }

// Result is the outcome of one send. Status 0 means no response arrived; Err
// is the delivery's last_error text (empty on success).
type Result struct {
	At       time.Time
	Status   int
	Err      string
	Duration time.Duration
}

// OK reports whether the receiver answered 200–299.
func (r Result) OK() bool { return r.Status >= 200 && r.Status <= 299 }

// Sender sends one attempt.
type Sender interface {
	Send(ctx context.Context, a Attempt) Result
}

// SetSender sets how attempts are sent; call it before deliveries are worked
// or tested.
func (s *Service) SetSender(x Sender) { s.sender = x }

// LoadAttempt reads a delivery and its webhook for an attempt. A deleted
// delivery or webhook, or a finished delivery, gives ErrGone. A disabled
// webhook cancels the delivery and gives ErrDisabled.
func (s *Service) LoadAttempt(ctx context.Context, deliveryID uuid.UUID) (Attempt, error) {
	r, err := s.q.GetAttempt(ctx, deliveryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Attempt{}, ErrGone
	}
	if err != nil {
		return Attempt{}, fmt.Errorf("get attempt: %w", err)
	}
	if r.Status != "pending" && r.Status != statusRetrying {
		return Attempt{}, ErrGone
	}
	if !r.Enabled {
		now, reason := s.opts.Now(), ErrorDisabled
		if err := s.q.CancelDelivery(ctx, q.CancelDeliveryParams{ID: deliveryID, Reason: &reason, Now: &now}); err != nil {
			return Attempt{}, fmt.Errorf("cancel delivery: %w", err)
		}
		return Attempt{}, ErrDisabled
	}
	return Attempt{DeliveryID: deliveryID, URL: r.Url, Secret: r.Secret, Type: r.EventType, Envelope: r.Payload}, nil
}

// cleanText cuts s to limit characters and makes it storable as text: invalid
// UTF-8 is replaced and NUL bytes are dropped.
func cleanText(s string, limit int) string {
	s = strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", ""), "�")
	if r := []rune(s); len(r) > limit {
		s = string(r[:limit])
	}
	return s
}

func int32Ptr(n int) *int32 {
	v := int32(n)
	return &v
}

func resultColumns(r Result) (status *int32, lastErr *string, durMS *int32) {
	if r.Status != 0 {
		status = int32Ptr(r.Status)
	}
	if r.Err != "" {
		e := cleanText(r.Err, maxErrorChars)
		lastErr = &e
	}
	return status, lastErr, int32Ptr(int(r.Duration.Milliseconds()))
}

// Record stores the outcome of attempt number attempt: succeeded on a 2xx
// answer, failed when the last attempt fails, retrying (due at next)
// otherwise. A final failure turns the webhook off unless an event delivery
// created after the failed one succeeded. ErrGone: nothing unfinished to
// record on.
func (s *Service) Record(ctx context.Context, deliveryID uuid.UUID, r Result, attempt int, next *time.Time) error {
	status, lastErr, durMS := resultColumns(r)
	p := q.RecordAttemptParams{
		ID: deliveryID, Attempts: int32(attempt), ResponseStatus: status, LastError: lastErr, DurationMs: durMS,
		LastAttemptAt: &r.At,
	}
	switch {
	case r.OK():
		p.Status = statusSucceeded
	case attempt >= MaxAttempts:
		p.Status = statusFailed
	default:
		p.Status = statusRetrying
		p.NextAttemptAt = next
	}
	if p.Status != statusRetrying {
		end := r.At.Add(r.Duration)
		p.FinishedAt = &end
	}
	return s.inTx(ctx, func(tx *q.Queries) error {
		row, err := tx.RecordAttempt(ctx, p)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrGone
		}
		if err != nil {
			return fmt.Errorf("record attempt: %w", err)
		}
		if p.Status != statusFailed || row.Resource == TestResource {
			return nil
		}
		now := s.opts.Now()
		if _, err := tx.DisableWebhookForFailures(ctx, q.DisableWebhookForFailuresParams{
			WebhookID: row.WebhookID, Since: &row.CreatedAt, Now: &now,
		}); err != nil {
			return fmt.Errorf("disable webhook: %w", err)
		}
		return nil
	})
}

type testData struct {
	ID      uuid.UUID `json:"id"`
	Message string    `json:"message"`
}

// Test sends a test event to a webhook at once, without the queue and without
// retries, and logs it as a delivery. It works for a disabled webhook and
// never affects automatic disabling. The returned delivery is succeeded or
// failed.
func (s *Service) Test(ctx context.Context, actor account.User, webhookID uuid.UUID) (Delivery, error) {
	if err := authorize(actor); err != nil {
		return Delivery{}, err
	}
	if s.sender == nil {
		return Delivery{}, errors.New("test webhook: no sender configured")
	}
	w, err := s.q.GetWebhook(ctx, webhookID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Delivery{}, errNotFound
	}
	if err != nil {
		return Delivery{}, fmt.Errorf("get webhook: %w", err)
	}
	who, err := s.opts.Builder.Actor(ctx, s.q, actor.ID)
	if err != nil {
		return Delivery{}, fmt.Errorf("build actor: %w", err)
	}
	data, err := json.Marshal(testData{ID: w.ID, Message: testMessage})
	if err != nil {
		return Delivery{}, fmt.Errorf("encode test data: %w", err)
	}
	now := s.opts.Now()
	link := strings.TrimSuffix(s.opts.BaseURL, "/") + "/settings/webhooks/" + w.ID.String()
	payload, err := json.Marshal(envelope{
		Action: event.Action(TestResource), Type: TestType, Actor: who, CreatedAt: now.UTC().Format(dateTimeLayout),
		Data: data, URL: &link, OrganizationID: w.WorkspaceID, WebhookID: w.ID,
	})
	if err != nil {
		return Delivery{}, fmt.Errorf("encode test payload: %w", err)
	}
	id := uuid.New()
	res := s.sender.Send(ctx, Attempt{DeliveryID: id, URL: w.Url, Secret: w.Secret, Type: TestType, Envelope: payload})
	status, lastErr, durMS := resultColumns(res)
	end := res.At.Add(res.Duration)
	st := statusFailed
	if res.OK() {
		st = statusSucceeded
	}
	err = s.q.InsertTestDelivery(ctx, q.InsertTestDeliveryParams{
		ID: id, WebhookID: w.ID, Payload: payload, Status: st, ResponseStatus: status, LastError: lastErr,
		DurationMs: durMS, CreatedAt: now, LastAttemptAt: &res.At, FinishedAt: &end,
	})
	if err != nil {
		return Delivery{}, fmt.Errorf("log test delivery: %w", err)
	}
	return Delivery{
		ID: id, WebhookID: w.ID, Resource: TestResource, Action: TestResource, Status: st, Attempts: 1,
		ResponseStatus: intPtr(status), LastError: lastErr, DurationMS: intPtr(durMS), CreatedAt: now,
		LastAttemptAt: &res.At, FinishedAt: &end,
	}, nil
}

// Cleanup deletes finished deliveries beyond the newest 50 of every webhook.
func (s *Service) Cleanup(ctx context.Context) error {
	if _, err := s.q.CleanupDeliveries(ctx); err != nil {
		return fmt.Errorf("cleanup deliveries: %w", err)
	}
	return nil
}

// RunCleanup calls Cleanup every interval until ctx is done.
func (s *Service) RunCleanup(ctx context.Context, every time.Duration, logger *slog.Logger) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.Cleanup(ctx); err != nil && ctx.Err() == nil {
				logger.Error("webhook delivery cleanup failed", slog.String("error", err.Error()))
			}
		}
	}
}
