package delivery

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/askuksov/yalkos/internal/domain/webhook"
)

// DefaultDelays are the waits after the first, second and third failed attempt.
var DefaultDelays = []time.Duration{time.Minute, time.Hour, 6 * time.Hour}

const (
	workers = 10
	// jobTimeout bounds one River attempt; the send itself stops sooner.
	jobTimeout = 10 * time.Second
)

// Worker works delivery jobs: one send per job run.
type Worker struct {
	river.WorkerDefaults[webhook.DeliveryArgs]
	Service *webhook.Service
	Sender  webhook.Sender
	// Delays are the waits after failed attempts, indexed by the number of
	// failures so far; DefaultDelays when empty.
	Delays []time.Duration
}

// Timeout implements river.Worker.
func (*Worker) Timeout(*river.Job[webhook.DeliveryArgs]) time.Duration { return jobTimeout }

// retryAt is when a job that just failed is tried again: the wait after this
// many failures, counted from the start of the attempt. len(job.Errors) does
// not include the current failure yet.
func (w *Worker) retryAt(job *river.Job[webhook.DeliveryArgs]) time.Time {
	delays := w.Delays
	if len(delays) == 0 {
		delays = DefaultDelays
	}
	from := time.Now()
	if job.AttemptedAt != nil {
		from = *job.AttemptedAt
	}
	return from.Add(delays[min(len(job.Errors), len(delays)-1)])
}

// NextRetry implements river.Worker.
func (w *Worker) NextRetry(job *river.Job[webhook.DeliveryArgs]) time.Time { return w.retryAt(job) }

// Work makes one attempt. A deleted delivery ends the job quietly and a
// disabled webhook cancels it. The outcome is recorded before the job
// reports; a failed attempt returns an error so that River retries.
func (w *Worker) Work(ctx context.Context, job *river.Job[webhook.DeliveryArgs]) error {
	id := job.Args.DeliveryID
	a, err := w.Service.LoadAttempt(ctx, id)
	switch {
	case errors.Is(err, webhook.ErrGone):
		return nil
	case errors.Is(err, webhook.ErrDisabled):
		return river.JobCancel(err)
	case err != nil:
		return fmt.Errorf("load attempt: %w", err)
	}
	res := w.Sender.Send(ctx, a)
	// A stopped client cancels the context: the attempt is not counted, River
	// puts the job back.
	if cerr := ctx.Err(); cerr != nil && !errors.Is(cerr, context.DeadlineExceeded) {
		return cerr
	}
	next := w.retryAt(job)
	if err := w.Service.Record(ctx, id, res, job.Attempt, &next); errors.Is(err, webhook.ErrGone) {
		return nil
	} else if err != nil {
		return fmt.Errorf("record attempt: %w", err)
	}
	if res.OK() {
		return nil
	}
	// River stores the error as JSON, which cannot hold a NUL byte: one bad
	// entry would stop its whole batch of completions. Whatever the sender
	// gave, only clean, bounded text goes back.
	return errors.New(webhook.CleanText(res.Err, webhook.MaxErrorChars))
}

// NewClient builds the River client that works delivery jobs. With testOnly
// the client skips start-up delays that tests do not want.
func NewClient(pool *pgxpool.Pool, w *Worker, testOnly bool) (*river.Client[pgx.Tx], error) {
	ws := river.NewWorkers()
	if err := river.AddWorkerSafely(ws, w); err != nil {
		return nil, fmt.Errorf("add delivery worker: %w", err)
	}
	c, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues:   map[string]river.QueueConfig{webhook.QueueDeliveries: {MaxWorkers: workers}},
		Workers:  ws,
		TestOnly: testOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("river client: %w", err)
	}
	return c, nil
}
