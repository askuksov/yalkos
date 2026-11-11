package delivery_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/webhook"
	"github.com/askuksov/yalkos/internal/store/q"
	"github.com/askuksov/yalkos/internal/testfx"
	"github.com/askuksov/yalkos/internal/webhook/delivery"
)

func job(attemptedAt time.Time, failures int) *river.Job[webhook.DeliveryArgs] {
	return &river.Job[webhook.DeliveryArgs]{JobRow: &rivertype.JobRow{
		AttemptedAt: &attemptedAt, Errors: make([]rivertype.AttemptError, failures),
	}}
}

func TestNextRetrySchedule(t *testing.T) {
	at := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	w := &delivery.Worker{}
	for _, c := range []struct {
		failures int
		want     time.Duration
	}{{0, time.Minute}, {1, time.Hour}, {2, 6 * time.Hour}, {7, 6 * time.Hour}} {
		if got := w.NextRetry(job(at, c.failures)); !got.Equal(at.Add(c.want)) {
			t.Fatalf("after %d failures: %v, want %v", c.failures, got, at.Add(c.want))
		}
	}
	// The wait counts from the attempt's start, not from the clock.
	later := at.Add(90 * time.Second)
	if got := w.NextRetry(job(later, 0)); !got.Equal(later.Add(time.Minute)) {
		t.Fatalf("NextRetry = %v, want %v", got, later.Add(time.Minute))
	}
	custom := &delivery.Worker{Delays: []time.Duration{time.Second, 2 * time.Second}}
	if got := custom.NextRetry(job(at, 1)); !got.Equal(at.Add(2 * time.Second)) {
		t.Fatalf("custom delays: %v", got)
	}
	if !reflectEqual(delivery.DefaultDelays, []time.Duration{time.Minute, time.Hour, 6 * time.Hour}) {
		t.Fatalf("DefaultDelays = %v", delivery.DefaultDelays)
	}
	if got := w.Timeout(job(at, 0)); got != 10*time.Second {
		t.Fatalf("Timeout = %v", got)
	}
}

func reflectEqual(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// stubBuilder gives every event a fixed payload.
type stubBuilder struct{}

func (stubBuilder) Build(context.Context, *q.Queries, event.Change) (webhook.Built, error) {
	return webhook.Built{
		Type: "Issue", Data: json.RawMessage(`{"id":"i-1","title":"T"}`), Actor: json.RawMessage(`{"id":"u-1","type":"user"}`),
	}, nil
}

func (stubBuilder) Actor(context.Context, *q.Queries, uuid.UUID) (json.RawMessage, error) {
	return json.RawMessage(`{"id":"u-1","type":"user"}`), nil
}

type workerEnv struct {
	pool *pgxpool.Pool
	svc  *webhook.Service
	hook func(t *testing.T, url string, enabled bool) webhook.Webhook
	emit func(t *testing.T) // one issue event
	// run starts a working client with the given delays and stops it with the test.
	run func(t *testing.T, delays []time.Duration)
}

func newWorkerEnv(t *testing.T) workerEnv {
	t.Helper()
	pool, users, clock := testfx.New(t)
	// A client that only queues, for the service; a second one works the jobs.
	inserter, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	if err != nil {
		t.Fatal(err)
	}
	svc := webhook.New(pool, webhook.Options{
		Now: clock, AllowPrivate: true, BaseURL: "http://yalkos.test", Builder: stubBuilder{}, Queue: inserter,
	})
	return workerEnv{
		pool: pool, svc: svc,
		hook: func(t *testing.T, url string, enabled bool) webhook.Webhook {
			t.Helper()
			w, err := svc.Create(context.Background(), users.Admin, webhook.CreateInput{
				URL: url, Resources: []event.Resource{event.Issue}, Enabled: enabled,
			})
			if err != nil {
				t.Fatal(err)
			}
			return w
		},
		emit: func(t *testing.T) {
			t.Helper()
			err := pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
				return svc.Emit(context.Background(), tx, event.Change{
					Resource: event.Issue, Action: event.Create, EntityID: uuid.New(), ActorID: users.Admin.ID,
					At: time.Date(2026, 1, 1, 0, 0, 5, 0, time.UTC),
				})
			})
			if err != nil {
				t.Fatal(err)
			}
		},
		run: func(t *testing.T, delays []time.Duration) {
			t.Helper()
			w := &delivery.Worker{Service: svc, Sender: delivery.NewSender(delivery.SenderOptions{AllowPrivate: true}), Delays: delays}
			c, err := delivery.NewClient(pool, w, true)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := c.Stop(ctx); err != nil {
					t.Errorf("stop client: %v", err)
				}
			})
		},
	}
}

type deliveryRow struct {
	ID                        uuid.UUID
	WebhookID                 uuid.UUID
	Status                    string
	Attempts                  int
	ResponseStatus            *int
	LastError                 *string
	NextAttemptAt, FinishedAt *time.Time
}

func (e workerEnv) deliveries(t *testing.T) []deliveryRow {
	t.Helper()
	rows, err := e.pool.Query(context.Background(), `
		SELECT id, webhook_id, status, attempts, response_status, last_error, next_attempt_at, finished_at
		FROM webhook_deliveries ORDER BY created_at, id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []deliveryRow
	for rows.Next() {
		var r deliveryRow
		if err := rows.Scan(&r.ID, &r.WebhookID, &r.Status, &r.Attempts, &r.ResponseStatus, &r.LastError, &r.NextAttemptAt, &r.FinishedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// waitFor polls until cond holds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (e workerEnv) jobStates(t *testing.T) map[string]int {
	t.Helper()
	rows, err := e.pool.Query(context.Background(), `SELECT state::text, count(*) FROM river_job GROUP BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			t.Fatal(err)
		}
		out[s] = n
	}
	return out
}

func TestAttemptsRestampAndKeepDeliveryID(t *testing.T) {
	e := newWorkerEnv(t)
	var calls atomic.Int32
	rc := newReceiver(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) <= 2 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	})
	// River drops a retry time that is already past when the attempt ends, so
	// the delay must outlast an attempt.
	const delay = 500 * time.Millisecond
	hook := e.hook(t, rc.URL, true)
	e.emit(t)
	e.run(t, []time.Duration{delay, delay, delay})

	waitFor(t, "the delivery to succeed", func() bool {
		d := e.deliveries(t)
		return len(d) == 1 && d[0].Status == "succeeded"
	})
	d := e.deliveries(t)[0]
	reqs := rc.requests()
	if len(reqs) != 3 || d.Attempts != 3 {
		t.Fatalf("requests = %d, attempts = %d, want 3 and 3", len(reqs), d.Attempts)
	}
	secret := mustSecret(t, e, hook.ID)
	var last int64
	for i, r := range reqs {
		if got := r.Header.Get("Linear-Delivery"); got != d.ID.String() {
			t.Fatalf("request %d: Linear-Delivery = %s, want %s", i, got, d.ID)
		}
		var doc struct {
			WebhookTimestamp int64 `json:"webhookTimestamp"`
			WebhookID        string
		}
		if err := json.Unmarshal(r.Body, &doc); err != nil {
			t.Fatal(err)
		}
		if doc.WebhookTimestamp <= last {
			t.Fatalf("request %d: webhookTimestamp %d not after %d", i, doc.WebhookTimestamp, last)
		}
		last = doc.WebhookTimestamp
		if r.Header.Get("Linear-Timestamp") != strconv.FormatInt(doc.WebhookTimestamp, 10) {
			t.Fatalf("request %d: Linear-Timestamp %s != body %d", i, r.Header.Get("Linear-Timestamp"), doc.WebhookTimestamp)
		}
		if got, want := r.Header.Get("Linear-Signature"), hmacHex(secret, r.Body); got != want {
			t.Fatalf("request %d: signature %s does not match its body (%s)", i, got, want)
		}
		if i > 0 && r.Header.Get("Linear-Signature") == reqs[i-1].Header.Get("Linear-Signature") {
			t.Fatalf("request %d: signature equals the previous attempt's", i)
		}
	}
	if d.FinishedAt == nil || d.NextAttemptAt != nil || d.ResponseStatus == nil || *d.ResponseStatus != 200 || d.LastError != nil {
		t.Fatalf("delivery = %+v", d)
	}
	waitFor(t, "the job to complete", func() bool { return e.jobStates(t)["completed"] == 1 })
}

func mustSecret(t *testing.T, e workerEnv, id uuid.UUID) string {
	t.Helper()
	var s string
	if err := e.pool.QueryRow(context.Background(), `SELECT secret FROM webhooks WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStoredNextAttemptIsRiversRetryTime(t *testing.T) {
	e := newWorkerEnv(t)
	rc := newReceiver(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) })
	e.hook(t, rc.URL, true)
	e.emit(t)
	e.run(t, []time.Duration{time.Hour})

	waitFor(t, "the first failure", func() bool {
		d := e.deliveries(t)
		return len(d) == 1 && d[0].Status == "retrying" && e.jobStates(t)["retryable"] == 1
	})
	d := e.deliveries(t)[0]
	var scheduled, attempted time.Time
	err := e.pool.QueryRow(context.Background(), `SELECT scheduled_at, attempted_at FROM river_job`).Scan(&scheduled, &attempted)
	if err != nil {
		t.Fatal(err)
	}
	if d.NextAttemptAt == nil || !d.NextAttemptAt.Equal(scheduled) {
		t.Fatalf("next_attempt_at = %v, River scheduled_at = %v", d.NextAttemptAt, scheduled)
	}
	if !scheduled.Equal(attempted.Add(time.Hour)) {
		t.Fatalf("scheduled_at = %v, want attempted_at %v + 1h", scheduled, attempted)
	}
	if d.Attempts != 1 || d.ResponseStatus == nil || *d.ResponseStatus != 503 || d.LastError == nil || *d.LastError != "HTTP 503" {
		t.Fatalf("delivery = %+v", d)
	}
	if len(rc.requests()) != 1 {
		t.Fatalf("requests = %d, want 1 (the retry is an hour away)", len(rc.requests()))
	}
}

func TestFinalFailureDisablesWebhook(t *testing.T) {
	e := newWorkerEnv(t)
	rc := newReceiver(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
	hook := e.hook(t, rc.URL, true)
	e.emit(t)
	d := 500 * time.Millisecond
	e.run(t, []time.Duration{d, d, d})

	waitFor(t, "the delivery to fail", func() bool {
		r := e.deliveries(t)
		return len(r) == 1 && r[0].Status == "failed"
	})
	r := e.deliveries(t)[0]
	if r.Attempts != 4 || r.FinishedAt == nil || r.LastError == nil || *r.LastError != "HTTP 500" {
		t.Fatalf("delivery = %+v", r)
	}
	if n := len(rc.requests()); n != 4 {
		t.Fatalf("requests = %d, want 4", n)
	}
	var enabled bool
	var reason *string
	if err := e.pool.QueryRow(context.Background(), `SELECT enabled, disabled_reason FROM webhooks WHERE id = $1`, hook.ID).Scan(&enabled, &reason); err != nil {
		t.Fatal(err)
	}
	if enabled || reason == nil || *reason != webhook.ReasonFailures {
		t.Fatalf("webhook enabled = %v, reason = %v, want disabled for failures", enabled, reason)
	}
	waitFor(t, "the job to be discarded", func() bool { return e.jobStates(t)["discarded"] == 1 })
}

func TestWorkerGoneDisabledAndControl(t *testing.T) {
	e := newWorkerEnv(t)
	hits := map[string]*atomic.Int32{"gone": {}, "off": {}, "on": {}}
	servers := map[string]*receiver{}
	for name, c := range hits {
		servers[name] = newReceiver(t, func(w http.ResponseWriter, _ *http.Request) { c.Add(1); w.WriteHeader(200) })
	}
	gone := e.hook(t, servers["gone"].URL, true)
	off := e.hook(t, servers["off"].URL, true)
	on := e.hook(t, servers["on"].URL, true)
	e.emit(t) // one delivery and one queued job per webhook

	// The first webhook is deleted and the second switched off before any worker runs.
	if _, err := e.pool.Exec(context.Background(), `DELETE FROM webhooks WHERE id = $1`, gone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE webhooks SET enabled = false WHERE id = $1`, off.ID); err != nil {
		t.Fatal(err)
	}
	e.run(t, nil)

	waitFor(t, "all three jobs to finish", func() bool {
		s := e.jobStates(t)
		return s["completed"]+s["cancelled"] == 3
	})
	states := e.jobStates(t)
	if states["completed"] != 2 || states["cancelled"] != 1 || states["discarded"] != 0 || states["retryable"] != 0 {
		t.Fatalf("job states = %v, want 2 completed (deleted, control) and 1 cancelled (disabled)", states)
	}
	if hits["gone"].Load() != 0 || hits["off"].Load() != 0 {
		t.Fatalf("deleted/disabled webhooks were called: %d, %d", hits["gone"].Load(), hits["off"].Load())
	}
	if hits["on"].Load() != 1 {
		t.Fatalf("control webhook calls = %d, want 1", hits["on"].Load())
	}
	byHook := map[uuid.UUID]deliveryRow{}
	for _, r := range e.deliveries(t) {
		byHook[r.WebhookID] = r
	}
	if len(byHook) != 2 {
		t.Fatalf("deliveries = %v, want those of the disabled and the control webhook", byHook)
	}
	if r := byHook[off.ID]; r.Status != "canceled" || r.LastError == nil || *r.LastError != "webhook disabled" || r.Attempts != 0 {
		t.Fatalf("disabled webhook's delivery = %+v", r)
	}
	if r := byHook[on.ID]; r.Status != "succeeded" || r.Attempts != 1 {
		t.Fatalf("control delivery = %+v", r)
	}
}

// rawSender answers every attempt with a fixed result, as a sender that does
// not clean its error text would.
type rawSender struct{ res webhook.Result }

func (s rawSender) Send(context.Context, webhook.Attempt) webhook.Result { return s.res }

// TestWorkReturnsCleanError: the error given to River is stored as JSON, so
// it must carry no NUL byte, only valid UTF-8, and be bounded.
func TestWorkReturnsCleanError(t *testing.T) {
	for _, c := range []struct {
		name, errText, want string
	}{
		{"clean text kept", "HTTP 500: boom", "HTTP 500: boom"},
		{"NUL dropped", "HTTP 500: a\x00b", "HTTP 500: ab"},
		{"invalid UTF-8 replaced", "HTTP 500: \xff\xfe", "HTTP 500: \ufffd"},
		{"long text bounded", "HTTP 500: " + strings.Repeat("x", 5000), "HTTP 500: " + strings.Repeat("x", 990)},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newWorkerEnv(t)
			e.hook(t, "http://127.0.0.1:1/", true)
			e.emit(t)
			id := e.deliveries(t)[0].ID
			w := &delivery.Worker{
				Service: e.svc, Sender: rawSender{webhook.Result{Status: 500, Err: c.errText}},
				Delays: []time.Duration{time.Hour},
			}
			now := time.Now()
			j := &river.Job[webhook.DeliveryArgs]{
				JobRow: &rivertype.JobRow{Attempt: 1, AttemptedAt: &now},
				Args:   webhook.DeliveryArgs{DeliveryID: id},
			}
			err := w.Work(context.Background(), j)
			if err == nil {
				t.Fatal("Work returned nil for a failed attempt")
			}
			if err.Error() != c.want {
				t.Fatalf("Work error = %q, want %q", err.Error(), c.want)
			}
			if strings.ContainsRune(err.Error(), 0) || !utf8.ValidString(err.Error()) {
				t.Fatalf("Work error is not clean: %q", err.Error())
			}
		})
	}
}

// TestPoisonedErrorBodyDoesNotStallQueue: a receiver that answers with a NUL
// byte in the error body must not keep any job of the client from finishing.
func TestPoisonedErrorBodyDoesNotStallQueue(t *testing.T) {
	e := newWorkerEnv(t)
	bad := newReceiver(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("a\x00b \xff"))
	})
	good := newReceiver(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	badHook := e.hook(t, bad.URL, true)
	e.hook(t, good.URL, true)
	e.emit(t) // one delivery and one job per webhook
	e.run(t, []time.Duration{time.Hour})

	// Bounded wait: a stalled queue leaves the jobs running.
	deadline := time.Now().Add(10 * time.Second)
	for {
		s := e.jobStates(t)
		if s["completed"] == 1 && s["retryable"] == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("jobs did not leave running within 10s: %v", s)
		}
		time.Sleep(25 * time.Millisecond)
	}

	var errs []byte
	err := e.pool.QueryRow(context.Background(), `SELECT array_to_json(errors)::text FROM river_job WHERE state = 'retryable'`).Scan(&errs)
	if err != nil {
		t.Fatal(err)
	}
	var attempts []struct {
		Attempt int    `json:"attempt"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(errs, &attempts); err != nil || len(attempts) != 1 {
		t.Fatalf("job errors = %s (%v), want one entry", errs, err)
	}
	if want := "HTTP 500: ab \ufffd"; attempts[0].Error != want {
		t.Fatalf("job error = %q, want %q", attempts[0].Error, want)
	}
	for _, d := range e.deliveries(t) {
		if d.WebhookID != badHook.ID {
			if d.Status != "succeeded" {
				t.Fatalf("good delivery = %+v", d)
			}
			continue
		}
		if d.Status != "retrying" || d.LastError == nil || *d.LastError != "HTTP 500: ab \ufffd" {
			t.Fatalf("poisoned delivery = %+v (last_error %v)", d, d.LastError)
		}
	}
}

// blockingSender signals when a send starts and then waits for the attempt's
// context to end, as a send that hangs until it is cut off would.
type blockingSender struct{ started chan struct{} }

func (s blockingSender) Send(ctx context.Context, _ webhook.Attempt) webhook.Result {
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return webhook.Result{At: time.Now(), Err: "connection failed: " + ctx.Err().Error()}
}

// lateSender signals when a send starts, waits for the attempt's context to
// end and only then reports a 200 answer, as a receiver that answers just as
// the job times out would.
type lateSender struct{ started chan struct{} }

func (s lateSender) Send(ctx context.Context, _ webhook.Attempt) webhook.Result {
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return webhook.Result{At: time.Now(), Status: 200, Duration: time.Millisecond}
}

// failingSender fails the test if an attempt is sent.
type failingSender struct{ t *testing.T }

func (s failingSender) Send(context.Context, webhook.Attempt) webhook.Result {
	s.t.Error("an attempt was sent")
	return webhook.Result{At: time.Now(), Err: "unexpected send"}
}

type finalAttempt struct {
	workerEnv
	id, hookID uuid.UUID
	client     *river.Client[pgx.Tx]
}

// retryingDelivery makes a delivery whose first attempts all failed with
// HTTP 500; its job has run as many attempts.
func retryingDelivery(t *testing.T, failed int) finalAttempt {
	t.Helper()
	ctx := context.Background()
	e := newWorkerEnv(t)
	hook := e.hook(t, "http://127.0.0.1:1/", true)
	e.emit(t)
	id := e.deliveries(t)[0].ID
	if _, err := e.pool.Exec(ctx, `UPDATE webhook_deliveries SET status = 'retrying', attempts = $2, response_status = 500,
		last_error = 'HTTP 500', last_attempt_at = now(), next_attempt_at = now() WHERE id = $1`, id, failed); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE river_job SET attempt = $2 WHERE args ->> 'deliveryId' = $1`, id.String(), failed); err != nil {
		t.Fatal(err)
	}
	return finalAttempt{workerEnv: e, id: id, hookID: hook.ID}
}

// start starts a client that sends with snd and stops it with the test.
func (f *finalAttempt) start(t *testing.T, snd webhook.Sender, delays []time.Duration) {
	t.Helper()
	c, err := delivery.NewClient(f.pool, &delivery.Worker{Service: f.svc, Sender: snd, Delays: delays}, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.StopAndCancel(sctx); err != nil {
			t.Errorf("stop client: %v", err)
		}
	})
	f.client = c
}

// startFinalAttempt makes a delivery whose three attempts already failed and
// starts a client whose sender signals on started when the fourth begins.
func startFinalAttempt(t *testing.T, snd webhook.Sender, started <-chan struct{}) finalAttempt {
	t.Helper()
	// River raises attempt to 4 when it fetches the job.
	f := retryingDelivery(t, 3)
	f.start(t, snd, nil)
	select {
	case <-started:
	case <-time.After(30 * time.Second):
		t.Fatal("the final attempt did not start")
	}
	return f
}

func (f finalAttempt) job(t *testing.T) (state string, attempt int) {
	t.Helper()
	if err := f.pool.QueryRow(context.Background(), `SELECT state::text, attempt FROM river_job WHERE args ->> 'deliveryId' = $1`,
		f.id.String()).Scan(&state, &attempt); err != nil {
		t.Fatal(err)
	}
	return state, attempt
}

func (f finalAttempt) webhookState(t *testing.T) (enabled bool, reason *string) {
	t.Helper()
	if err := f.pool.QueryRow(context.Background(), `SELECT enabled, disabled_reason FROM webhooks WHERE id = $1`,
		f.hookID).Scan(&enabled, &reason); err != nil {
		t.Fatal(err)
	}
	return enabled, reason
}

// TestLateOutcomeIsRecorded: a last attempt whose answer arrives only after the
// job timeout still records its outcome, so the delivery ends as River counts
// the attempt.
func TestLateOutcomeIsRecorded(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		started := make(chan struct{}, 1)
		f := startFinalAttempt(t, lateSender{started: started}, started)
		waitFor(t, "the job to leave running", func() bool {
			s, _ := f.job(t)
			return s != "running"
		})
		if state, attempt := f.job(t); state != "completed" || attempt != 4 {
			t.Fatalf("job state = %s, attempt = %d, want completed after attempt 4", state, attempt)
		}
		d := f.deliveries(t)[0]
		if d.Status != "succeeded" || d.Attempts != 4 || d.ResponseStatus == nil || *d.ResponseStatus != 200 ||
			d.LastError != nil || d.FinishedAt == nil || d.NextAttemptAt != nil {
			t.Fatalf("delivery = %+v, want succeeded with 200 after 4 attempts", d)
		}
		if enabled, reason := f.webhookState(t); !enabled || reason != nil {
			t.Fatalf("webhook enabled = %v, reason = %v, want still on", enabled, reason)
		}
	})
	t.Run("failure", func(t *testing.T) {
		started := make(chan struct{}, 1)
		f := startFinalAttempt(t, blockingSender{started: started}, started)
		waitFor(t, "the job to leave running", func() bool {
			s, _ := f.job(t)
			return s != "running"
		})
		if state, attempt := f.job(t); state != "discarded" || attempt != 4 {
			t.Fatalf("job state = %s, attempt = %d, want discarded after attempt 4", state, attempt)
		}
		d := f.deliveries(t)[0]
		const want = "connection failed: context deadline exceeded"
		if d.Status != "failed" || d.Attempts != 4 || d.LastError == nil || *d.LastError != want || d.FinishedAt == nil {
			t.Fatalf("delivery = %+v, want failed with %q after 4 attempts", d, want)
		}
		if enabled, reason := f.webhookState(t); enabled || reason == nil || *reason != webhook.ReasonFailures {
			t.Fatalf("webhook enabled = %v, reason = %v, want disabled for failures", enabled, reason)
		}
	})
}

// crash marks the delivery's job as running attempt number attempt for a
// client that died two hours ago, past River's rescue threshold.
func (f finalAttempt) crash(t *testing.T, attempt int) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `UPDATE river_job SET state = 'running', attempt = $2,
		attempted_at = now() - interval '2 hours', attempted_by = ARRAY['crashed-client'] WHERE args ->> 'deliveryId' = $1`,
		f.id.String(), attempt); err != nil {
		t.Fatal(err)
	}
}

// TestInterruptedFinalAttemptIsRecovered: a process killed during the last
// attempt records no outcome, and River's rescuer discards the job; the
// delivery would stay retrying forever, so Cleanup fails it without turning
// the webhook off.
func TestInterruptedFinalAttemptIsRecovered(t *testing.T) {
	ctx := context.Background()
	t.Run("last attempt", func(t *testing.T) {
		f := retryingDelivery(t, 3)
		f.crash(t, 4)
		f.start(t, failingSender{t}, nil)
		waitFor(t, "the job to leave running", func() bool {
			s, _ := f.job(t)
			return s != "running"
		})
		if state, attempt := f.job(t); state != "discarded" || attempt != 4 {
			t.Fatalf("job state = %s, attempt = %d, want discarded after attempt 4", state, attempt)
		}
		if d := f.deliveries(t)[0]; d.Status != "retrying" || d.Attempts != 3 {
			t.Fatalf("delivery before cleanup = %+v, want retrying after 3 attempts", d)
		}

		if err := f.svc.Cleanup(ctx); err != nil {
			t.Fatalf("Cleanup: %v", err)
		}
		d := f.deliveries(t)[0]
		if d.Status != "failed" || d.LastError == nil || *d.LastError != webhook.ErrorInterrupted || d.Attempts != 3 ||
			d.FinishedAt == nil || d.NextAttemptAt != nil {
			t.Fatalf("delivery after cleanup = %+v, want failed with %q after 3 attempts", d, webhook.ErrorInterrupted)
		}
		if enabled, reason := f.webhookState(t); !enabled || reason != nil {
			t.Fatalf("webhook enabled = %v, reason = %v, want still on", enabled, reason)
		}
	})
	// Control: a crash before the last attempt is retried later, so Cleanup
	// leaves the delivery to that retry.
	t.Run("control: earlier attempt", func(t *testing.T) {
		f := retryingDelivery(t, 2)
		f.crash(t, 3)
		// The retry is due about 22 hours ahead and is not worked here.
		f.start(t, failingSender{t}, []time.Duration{24 * time.Hour})
		waitFor(t, "the job to leave running", func() bool {
			s, _ := f.job(t)
			return s != "running"
		})
		if state, attempt := f.job(t); state != "retryable" || attempt != 3 {
			t.Fatalf("job state = %s, attempt = %d, want retryable after attempt 3", state, attempt)
		}
		if err := f.svc.Cleanup(ctx); err != nil {
			t.Fatalf("Cleanup: %v", err)
		}
		if d := f.deliveries(t)[0]; d.Status != "retrying" || d.Attempts != 2 {
			t.Fatalf("delivery after cleanup = %+v, want retrying after 2 attempts", d)
		}
	})
}

// TestStoppedFinalAttemptIsRetried is a control: a client stop cancels the last
// attempt, River puts the job back without counting it, and Cleanup leaves the
// delivery to that retry.
func TestStoppedFinalAttemptIsRetried(t *testing.T) {
	ctx := context.Background()
	started := make(chan struct{}, 1)
	f := startFinalAttempt(t, blockingSender{started: started}, started)
	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := f.client.StopAndCancel(sctx); err != nil {
		t.Fatalf("stop client: %v", err)
	}
	if state, attempt := f.job(t); state != "available" || attempt != 3 {
		t.Fatalf("job state = %s, attempt = %d, want available with attempt 3", state, attempt)
	}
	if err := f.svc.Cleanup(ctx); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if d := f.deliveries(t)[0]; d.Status != "retrying" || d.Attempts != 3 || d.LastError == nil || *d.LastError != "HTTP 500" {
		t.Fatalf("delivery after cleanup = %+v, want retrying after 3 attempts", d)
	}
}
