package webhook_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/webhook"
)

// fakeSender answers every send with res and keeps what it was asked to send.
type fakeSender struct {
	res  webhook.Result
	sent []webhook.Attempt
}

func (f *fakeSender) Send(_ context.Context, a webhook.Attempt) webhook.Result {
	f.sent = append(f.sent, a)
	r := f.res
	if r.At.IsZero() {
		r.At = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	}
	return r
}

var (
	failResult = webhook.Result{Status: 500, Err: "HTTP 500: boom", Duration: 20 * time.Millisecond}
	okResult   = webhook.Result{Status: 200, Duration: 20 * time.Millisecond}
)

func atSec(n int) time.Time { return time.Date(2026, 2, 1, 0, 0, n, 0, time.UTC) }

// emitAt creates one issue delivery per matching webhook, created at t.
func (e emitEnv) emitAt(t *testing.T, at time.Time) {
	t.Helper()
	if err := e.emit(t, event.Change{Resource: event.Issue, Action: event.Update, At: at}); err != nil {
		t.Fatal(err)
	}
}

func (e emitEnv) deliveryIDs(t *testing.T, hookID uuid.UUID) []uuid.UUID {
	t.Helper()
	rows, err := e.pool.Query(context.Background(), `SELECT id FROM webhook_deliveries WHERE webhook_id = $1 ORDER BY created_at, id`, hookID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

type deliveryState struct {
	Status                                 string
	Attempts                               int
	ResponseStatus, DurationMS             *int
	LastError                              *string
	LastAttemptAt, NextAttemptAt, Finished *time.Time
}

func (e emitEnv) delivery(t *testing.T, id uuid.UUID) deliveryState {
	t.Helper()
	var d deliveryState
	err := e.pool.QueryRow(context.Background(), `
		SELECT status, attempts, response_status, duration_ms, last_error, last_attempt_at, next_attempt_at, finished_at
		FROM webhook_deliveries WHERE id = $1`, id).
		Scan(&d.Status, &d.Attempts, &d.ResponseStatus, &d.DurationMS, &d.LastError, &d.LastAttemptAt, &d.NextAttemptAt, &d.Finished)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

type hookState struct {
	Enabled    bool
	Reason     *string
	DisabledAt *time.Time
}

func (e emitEnv) hook(t *testing.T, id uuid.UUID) hookState {
	t.Helper()
	var h hookState
	if err := e.pool.QueryRow(context.Background(), `SELECT enabled, disabled_reason, disabled_at FROM webhooks WHERE id = $1`, id).
		Scan(&h.Enabled, &h.Reason, &h.DisabledAt); err != nil {
		t.Fatal(err)
	}
	return h
}

func (e emitEnv) wantEnabled(t *testing.T, id uuid.UUID, enabled bool, what string) {
	t.Helper()
	h := e.hook(t, id)
	if h.Enabled != enabled {
		t.Fatalf("%s: enabled = %v, want %v", what, h.Enabled, enabled)
	}
	if enabled && (h.Reason != nil || h.DisabledAt != nil) {
		t.Fatalf("%s: enabled webhook has reason %v, disabled_at %v", what, h.Reason, h.DisabledAt)
	}
	if !enabled && (h.Reason == nil || *h.Reason != webhook.ReasonFailures || h.DisabledAt == nil) {
		t.Fatalf("%s: reason = %v, disabled_at = %v, want failures and a time", what, h.Reason, h.DisabledAt)
	}
}

func (e emitEnv) record(t *testing.T, id uuid.UUID, r webhook.Result, attempt int) {
	t.Helper()
	next := r.At.Add(time.Minute)
	if err := e.svc.Record(context.Background(), id, r, attempt, &next); err != nil {
		t.Fatalf("Record attempt %d: %v", attempt, err)
	}
}

func TestRecordFinalFailureDisables(t *testing.T) {
	e := newEmitEnv(t)
	w := e.create(t, webhook.CreateInput{Enabled: true})
	e.emitAt(t, atSec(0))
	id := e.deliveryIDs(t, w.ID)[0]

	// Attempts 1–3 fail: retrying, due at the given time, webhook untouched.
	for attempt := 1; attempt <= 3; attempt++ {
		r := failResult
		r.At = atSec(10 * attempt)
		next := r.At.Add(time.Minute)
		if err := e.svc.Record(context.Background(), id, r, attempt, &next); err != nil {
			t.Fatal(err)
		}
		d := e.delivery(t, id)
		if d.Status != "retrying" || d.Attempts != attempt || d.Finished != nil || d.NextAttemptAt == nil || !d.NextAttemptAt.Equal(next) {
			t.Fatalf("attempt %d: delivery = %+v", attempt, d)
		}
		if d.ResponseStatus == nil || *d.ResponseStatus != 500 || d.LastError == nil || *d.LastError != "HTTP 500: boom" ||
			d.DurationMS == nil || *d.DurationMS != 20 || d.LastAttemptAt == nil || !d.LastAttemptAt.Equal(r.At) {
			t.Fatalf("attempt %d: delivery = %+v", attempt, d)
		}
		e.wantEnabled(t, w.ID, true, fmt.Sprintf("after failed attempt %d", attempt))
	}

	// The fourth failure ends the delivery and disables the webhook.
	r := failResult
	r.At = atSec(40)
	e.record(t, id, r, 4)
	d := e.delivery(t, id)
	if d.Status != "failed" || d.Attempts != 4 || d.Finished == nil || d.NextAttemptAt != nil {
		t.Fatalf("delivery = %+v", d)
	}
	e.wantEnabled(t, w.ID, false, "after the final failure")
	// disabled_at is the service clock (2026-01-01 in tests), not the attempt time.
	if got := e.hook(t, w.ID).DisabledAt; got == nil || !got.After(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("disabled_at = %v", got)
	}

	// Positive control: a later success ends a delivery as succeeded and does
	// not touch the webhook.
	e2 := newEmitEnv(t)
	w2 := e2.create(t, webhook.CreateInput{Enabled: true})
	e2.emitAt(t, atSec(0))
	id2 := e2.deliveryIDs(t, w2.ID)[0]
	ok := okResult
	ok.At = atSec(5)
	e2.record(t, id2, ok, 4)
	d = e2.delivery(t, id2)
	if d.Status != "succeeded" || d.Finished == nil || !d.Finished.Equal(atSec(5).Add(20*time.Millisecond)) || d.LastError != nil {
		t.Fatalf("success: delivery = %+v", d)
	}
	e2.wantEnabled(t, w2.ID, true, "after success")
}

func TestRecordStoresOnlyUnfinishedAndCleanText(t *testing.T) {
	e := newEmitEnv(t)
	w := e.create(t, webhook.CreateInput{Enabled: true})
	e.emitAt(t, atSec(0))
	id := e.deliveryIDs(t, w.ID)[0]

	// A body with a NUL byte, invalid UTF-8 and an excess of characters is stored.
	r := failResult
	r.At = atSec(1)
	r.Err = "HTTP 500: a\x00b\xffc" + strings.Repeat("€", 2000)
	e.record(t, id, r, 1)
	got := *e.delivery(t, id).LastError
	if strings.ContainsRune(got, 0) || !strings.HasPrefix(got, "HTTP 500: ab�c€") || len([]rune(got)) != 1000 {
		t.Fatalf("last_error = %.40q (%d characters)", got, len([]rune(got)))
	}

	ok := okResult
	ok.At = atSec(2)
	e.record(t, id, ok, 2)
	// Recording on a finished delivery is refused and changes nothing.
	err := e.svc.Record(context.Background(), id, failResult, 3, nil)
	if !errors.Is(err, webhook.ErrGone) {
		t.Fatalf("Record on a finished delivery: %v, want ErrGone", err)
	}
	if d := e.delivery(t, id); d.Status != "succeeded" || d.Attempts != 2 {
		t.Fatalf("delivery = %+v", d)
	}
	if err := e.svc.Record(context.Background(), uuid.New(), failResult, 1, nil); !errors.Is(err, webhook.ErrGone) {
		t.Fatalf("Record on an unknown delivery: %v, want ErrGone", err)
	}
}

func TestNoDisableAfterLaterSuccess(t *testing.T) {
	e := newEmitEnv(t)
	// Same sequence on every webhook: D1 created at t0, D2 created at t1.
	later := e.create(t, webhook.CreateInput{Enabled: true, Name: "later success"})
	before := e.create(t, webhook.CreateInput{Enabled: true, Name: "success before D1 was created"})
	edge := e.create(t, webhook.CreateInput{Enabled: true, Name: "success exactly at D1 creation"})
	e.emitAt(t, atSec(10)) // D1 of every webhook
	e.emitAt(t, atSec(20)) // D2 of every webhook

	d1 := func(w webhook.Webhook) uuid.UUID { return e.deliveryIDs(t, w.ID)[0] }
	d2 := func(w webhook.Webhook) uuid.UUID { return e.deliveryIDs(t, w.ID)[1] }

	// D2 succeeds at t2 = 30, after D1 was created: D1's final failure at t3 does not disable.
	ok := okResult
	ok.At = atSec(30)
	e.record(t, d2(later), ok, 1)
	fail := failResult
	fail.At = atSec(40)
	e.record(t, d1(later), fail, 4)
	if d := e.delivery(t, d1(later)); d.Status != "failed" {
		t.Fatalf("D1 = %+v", d)
	}
	e.wantEnabled(t, later.ID, true, "success after D1 was created")

	// Positive control: a success that finished before D1 was created does not count.
	ok.At = atSec(5)
	e.record(t, d2(before), ok, 1) // D2's own created_at is 20, but it finished at 5 < 10
	e.record(t, d1(before), fail, 4)
	e.wantEnabled(t, before.ID, false, "success finished before D1 was created")

	// Boundary: finished_at equal to D1's created_at counts (>=).
	ok.At, ok.Duration = atSec(10), 0
	e.record(t, d2(edge), ok, 1)
	e.record(t, d1(edge), fail, 4)
	e.wantEnabled(t, edge.ID, true, "success finishing exactly when D1 was created")

	// Only an event delivery counts: a failed D2 does not prevent disabling.
	e3 := newEmitEnv(t)
	w3 := e3.create(t, webhook.CreateInput{Enabled: true})
	e3.emitAt(t, atSec(10))
	e3.emitAt(t, atSec(20))
	ids := e3.deliveryIDs(t, w3.ID)
	e3.record(t, ids[1], failResult, 1) // retrying, not succeeded
	e3.record(t, ids[0], fail, 4)
	e3.wantEnabled(t, w3.ID, false, "no success at all")

	// Another webhook's success does not count.
	e4 := newEmitEnv(t)
	a, b := e4.create(t, webhook.CreateInput{Enabled: true}), e4.create(t, webhook.CreateInput{Enabled: true})
	e4.emitAt(t, atSec(10))
	ok.At = atSec(30)
	e4.record(t, e4.deliveryIDs(t, b.ID)[0], ok, 1)
	e4.record(t, e4.deliveryIDs(t, a.ID)[0], fail, 4)
	e4.wantEnabled(t, a.ID, false, "a success of another webhook")
	e4.wantEnabled(t, b.ID, true, "the succeeding webhook")
}

func TestHandDisabledWebhookKeepsNoReason(t *testing.T) {
	e := newEmitEnv(t)
	w := e.create(t, webhook.CreateInput{Enabled: true})
	e.emitAt(t, atSec(0))
	id := e.deliveryIDs(t, w.ID)[0]
	off := false
	if _, err := e.svc.Update(context.Background(), e.users.Admin, w.ID, webhook.Patch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	fail := failResult
	fail.At = atSec(1)
	e.record(t, id, fail, 4)
	if h := e.hook(t, w.ID); h.Enabled || h.Reason != nil || h.DisabledAt != nil {
		t.Fatalf("hand-disabled webhook = %+v, want no reason", h)
	}
}

func TestTestDeliveryNeverDisables(t *testing.T) {
	ctx := context.Background()
	e := newEmitEnv(t)
	snd := &fakeSender{res: okResult}
	e.svc.SetSender(snd)

	// A successful test does not prevent disabling.
	w := e.create(t, webhook.CreateInput{Enabled: true})
	e.emitAt(t, atSec(0))
	d1 := e.deliveryIDs(t, w.ID)[0]
	if d, err := e.svc.Test(ctx, e.users.Admin, w.ID); err != nil || d.Status != "succeeded" {
		t.Fatalf("Test = %+v, %v", d, err)
	}
	fail := failResult
	fail.At = atSec(1)
	e.record(t, d1, fail, 4)
	e.wantEnabled(t, w.ID, false, "after a successful test and a final failure")

	// A failed test does not disable; a failed final record on a test row does not either.
	w2 := e.create(t, webhook.CreateInput{Enabled: true})
	snd.res = failResult
	for range 5 {
		if d, err := e.svc.Test(ctx, e.users.Admin, w2.ID); err != nil || d.Status != "failed" {
			t.Fatalf("Test = %+v, %v", d, err)
		}
	}
	e.wantEnabled(t, w2.ID, true, "after failed tests")
	var testRow uuid.UUID
	if err := e.pool.QueryRow(ctx, `UPDATE webhook_deliveries SET status = 'pending', finished_at = NULL WHERE id =
		(SELECT id FROM webhook_deliveries WHERE webhook_id = $1 LIMIT 1) RETURNING id`, w2.ID).Scan(&testRow); err != nil {
		t.Fatal(err)
	}
	e.record(t, testRow, fail, 4)
	e.wantEnabled(t, w2.ID, true, "after a final failure of a test row")
}

func TestLoadAttemptDisabledCancels(t *testing.T) {
	ctx := context.Background()
	e := newEmitEnv(t)
	w := e.create(t, webhook.CreateInput{Enabled: true, URL: "https://hooks.example.com/a", Secret: ptr("s3cret")})
	e.emitAt(t, atSec(0))
	e.emitAt(t, atSec(1))
	ids := e.deliveryIDs(t, w.ID)

	// Positive control: an enabled webhook gives the attempt.
	a, err := e.svc.LoadAttempt(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if a.DeliveryID != ids[0] || a.URL != "https://hooks.example.com/a" || a.Secret != "s3cret" || a.Type != "Issue" {
		t.Fatalf("attempt = %+v", a)
	}
	var env struct {
		Action, Type string
		WebhookID    uuid.UUID
		Timestamp    *json.RawMessage `json:"webhookTimestamp"`
	}
	if err := json.Unmarshal(a.Envelope, &env); err != nil || env.Action != "update" || env.Type != "Issue" || env.WebhookID != w.ID || env.Timestamp != nil {
		t.Fatalf("envelope = %s (%v)", a.Envelope, err)
	}
	// The attempt's printed forms hide the secret.
	for _, s := range []string{fmt.Sprint(a), fmt.Sprintf("%+v", a), fmt.Sprintf("%#v", a)} {
		if strings.Contains(s, "s3cret") || !strings.Contains(s, ids[0].String()) {
			t.Fatalf("printed attempt = %s", s)
		}
	}
	if d := e.delivery(t, ids[0]); d.Status != "pending" {
		t.Fatalf("loading an attempt changed the delivery: %+v", d)
	}

	// The secret is read at attempt time: after a rotation the new one is used.
	rotated, err := e.svc.RotateSecret(ctx, e.users.Admin, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a, _ = e.svc.LoadAttempt(ctx, ids[0]); a.Secret != rotated.Secret || a.Secret == "s3cret" {
		t.Fatalf("secret after rotation = %q", a.Secret)
	}

	// Disabled: ErrDisabled, the delivery is canceled; the other unfinished one too, at its attempt.
	off := false
	if _, err := e.svc.Update(ctx, e.users.Admin, w.ID, webhook.Patch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.LoadAttempt(ctx, ids[0]); !errors.Is(err, webhook.ErrDisabled) {
		t.Fatalf("LoadAttempt = %v, want ErrDisabled", err)
	}
	d := e.delivery(t, ids[0])
	if d.Status != "canceled" || d.LastError == nil || *d.LastError != "webhook disabled" || d.Finished == nil {
		t.Fatalf("delivery = %+v", d)
	}
	if d := e.delivery(t, ids[1]); d.Status != "pending" {
		t.Fatalf("the other delivery changed before its attempt: %+v", d)
	}
	// A canceled delivery has nothing left to send, even after enabling again.
	on := true
	if _, err := e.svc.Update(ctx, e.users.Admin, w.ID, webhook.Patch{Enabled: &on}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.LoadAttempt(ctx, ids[0]); !errors.Is(err, webhook.ErrGone) {
		t.Fatalf("LoadAttempt on a canceled delivery = %v, want ErrGone", err)
	}
	if _, err := e.svc.LoadAttempt(ctx, ids[1]); err != nil {
		t.Fatalf("LoadAttempt on a pending delivery of the re-enabled webhook: %v", err)
	}

	// A finished delivery is not sent again.
	ok := okResult
	ok.At = atSec(5)
	e.record(t, ids[1], ok, 1)
	if _, err := e.svc.LoadAttempt(ctx, ids[1]); !errors.Is(err, webhook.ErrGone) {
		t.Fatalf("LoadAttempt on a succeeded delivery = %v, want ErrGone", err)
	}

	// Deleted webhook (its deliveries go with it) and unknown ids: ErrGone.
	if err := e.svc.Delete(ctx, e.users.Admin, w.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.LoadAttempt(ctx, ids[0]); !errors.Is(err, webhook.ErrGone) {
		t.Fatalf("LoadAttempt after delete = %v, want ErrGone", err)
	}
	if _, err := e.svc.LoadAttempt(ctx, uuid.New()); !errors.Is(err, webhook.ErrGone) {
		t.Fatalf("LoadAttempt of an unknown id = %v, want ErrGone", err)
	}
}

func TestTestSend(t *testing.T) {
	ctx := context.Background()
	e := newEmitEnv(t)
	snd := &fakeSender{res: okResult}
	e.svc.SetSender(snd)
	w := e.create(t, webhook.CreateInput{Enabled: false, URL: "https://hooks.example.com/t", Secret: ptr("s3cret")})

	// Works for a disabled webhook.
	d, err := e.svc.Test(ctx, e.users.Admin, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snd.sent) != 1 {
		t.Fatalf("sends = %d, want 1", len(snd.sent))
	}
	a := snd.sent[0]
	if a.URL != w.URL || a.Secret != "s3cret" || a.Type != "WebhookTest" || a.DeliveryID != d.ID {
		t.Fatalf("attempt = %+v", a)
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal(a.Envelope, &env); err != nil {
		t.Fatal(err)
	}
	str := func(k string) string {
		var s string
		if err := json.Unmarshal(env[k], &s); err != nil {
			t.Fatalf("%s = %s", k, env[k])
		}
		return s
	}
	var orgID uuid.UUID
	if err := e.pool.QueryRow(ctx, `SELECT workspace_id FROM webhooks WHERE id = $1`, w.ID).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if str("action") != "test" || str("type") != "WebhookTest" || str("webhookId") != w.ID.String() ||
		str("organizationId") != orgID.String() || str("url") != "http://yalkos.test/settings/webhooks/"+w.ID.String() {
		t.Fatalf("envelope = %s", a.Envelope)
	}
	if _, err := time.Parse("2006-01-02T15:04:05.000Z", str("createdAt")); err != nil {
		t.Fatalf("createdAt = %s: %v", env["createdAt"], err)
	}
	if string(env["data"]) != fmt.Sprintf(`{"id":%q,"message":"Test delivery from Yalkos"}`, w.ID) {
		t.Fatalf("data = %s", env["data"])
	}
	if string(env["actor"]) != `{"id":"u-1","type":"user"}` {
		t.Fatalf("actor = %s", env["actor"])
	}
	if _, has := env["webhookTimestamp"]; has {
		t.Fatal("the stored envelope carries webhookTimestamp; the sender adds it")
	}
	if _, has := env["updatedFrom"]; has {
		t.Fatal("test envelope has updatedFrom")
	}

	// Logged as a delivery with resource and action test.
	if d.Resource != "test" || d.Action != "test" || d.Status != "succeeded" || d.Attempts != 1 || d.FinishedAt == nil ||
		d.ResponseStatus == nil || *d.ResponseStatus != 200 || d.LastError != nil || d.EntityID != nil {
		t.Fatalf("delivery = %+v", d)
	}
	list, err := e.svc.Deliveries(ctx, e.users.Admin, w.ID, 50)
	if err != nil || len(list) != 1 || list[0].ID != d.ID || list[0].Resource != "test" || list[0].Action != "test" || list[0].Status != "succeeded" {
		t.Fatalf("Deliveries = %+v, %v", list, err)
	}
	var stored json.RawMessage
	if err := e.pool.QueryRow(ctx, `SELECT payload FROM webhook_deliveries WHERE id = $1`, d.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(t, stored, a.Envelope) {
		t.Fatalf("stored payload %s != sent envelope %s", stored, a.Envelope)
	}
	if h := e.hook(t, w.ID); h.Enabled || h.Reason != nil {
		t.Fatalf("webhook after a test = %+v, want still off by hand", h)
	}

	// A failing receiver: status failed with the error text.
	snd.res = failResult
	d, err = e.svc.Test(ctx, e.users.Admin, w.ID)
	if err != nil || d.Status != "failed" || d.LastError == nil || *d.LastError != "HTTP 500: boom" || d.ResponseStatus == nil || *d.ResponseStatus != 500 {
		t.Fatalf("failed Test = %+v, %v", d, err)
	}

	// Permissions and lookup; nothing is sent for them.
	n := len(snd.sent)
	if _, err := e.svc.Test(ctx, e.users.Member, w.ID); !errors.Is(err, kit.ErrForbidden) {
		t.Fatalf("member Test: %v, want ErrForbidden", err)
	}
	if _, err := e.svc.Test(ctx, e.users.Admin, uuid.New()); !errors.Is(err, kit.ErrNotFound) {
		t.Fatalf("unknown webhook Test: %v, want not found", err)
	}
	if len(snd.sent) != n {
		t.Fatalf("sends grew to %d", len(snd.sent))
	}
	// Owner may test too.
	snd.res = okResult
	if _, err := e.svc.Test(ctx, e.users.Owner, w.ID); err != nil {
		t.Fatalf("owner Test: %v", err)
	}
}

func jsonEqual(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprint(x) == fmt.Sprint(y)
}

func TestTestWithoutSender(t *testing.T) {
	e := newEmitEnv(t)
	w := e.create(t, webhook.CreateInput{Enabled: true})
	if _, err := e.svc.Test(context.Background(), e.users.Admin, w.ID); err == nil {
		t.Fatal("Test without a sender succeeded")
	}
	if n := e.count(t, `SELECT count(*) FROM webhook_deliveries`); n != 0 {
		t.Fatalf("deliveries = %d, want 0", n)
	}
}

func TestCleanupKeeps50Finished(t *testing.T) {
	ctx := context.Background()
	e := newEmitEnv(t)
	a := e.create(t, webhook.CreateInput{Enabled: true})
	b := e.create(t, webhook.CreateInput{Enabled: true})
	statuses := []string{"succeeded", "failed", "canceled"}
	insert := func(w webhook.Webhook, i int, status string) uuid.UUID {
		var id uuid.UUID
		err := e.pool.QueryRow(ctx, `
			INSERT INTO webhook_deliveries (webhook_id, resource, action, payload, status, created_at)
			VALUES ($1, 'issue', 'update', '{}', $2, $3) RETURNING id`, w.ID, status, atSec(0).Add(time.Duration(i)*time.Minute)).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	var finished []uuid.UUID
	for i := range 55 {
		finished = append(finished, insert(a, i, statuses[i%3]))
	}
	// Unfinished ones are the oldest of all and still stay.
	pending := []uuid.UUID{insert(a, -2, "pending"), insert(a, -1, "retrying")}
	// Another webhook with few finished deliveries is untouched.
	var others []uuid.UUID
	for i := range 3 {
		others = append(others, insert(b, i, "succeeded"))
	}

	if err := e.svc.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := e.pool.Query(ctx, `SELECT id FROM webhook_deliveries`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	left := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		left[id] = true
	}
	if len(left) != 50+2+3 {
		t.Fatalf("left = %d deliveries, want 55", len(left))
	}
	for i, id := range finished {
		if want := i >= 5; left[id] != want { // the 5 oldest finished are gone
			t.Fatalf("finished delivery %d: kept = %v, want %v", i, left[id], want)
		}
	}
	for _, id := range append(pending, others...) {
		if !left[id] {
			t.Fatalf("delivery %s was deleted", id)
		}
	}
	// Idempotent.
	if err := e.svc.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT count(*) FROM webhook_deliveries`); n != 55 {
		t.Fatalf("deliveries after a second cleanup = %d, want 55", n)
	}
}

func TestRunCleanupStopsWithContext(t *testing.T) {
	e := newEmitEnv(t)
	w := e.create(t, webhook.CreateInput{Enabled: true})
	for i := range 52 {
		if _, err := e.pool.Exec(context.Background(), `
			INSERT INTO webhook_deliveries (webhook_id, resource, action, payload, status, created_at)
			VALUES ($1, 'issue', 'update', '{}', 'succeeded', $2)`, w.ID, atSec(0).Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.svc.RunCleanup(ctx, 10*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for e.count(t, `SELECT count(*) FROM webhook_deliveries`) != 50 {
		if time.Now().After(deadline) {
			t.Fatal("cleanup did not run")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunCleanup did not stop after its context ended")
	}
}

// beginTx starts a transaction the test holds open; it is rolled back at the end
// unless the test commits it.
func beginTx(t *testing.T, e emitEnv) pgx.Tx {
	t.Helper()
	tx, err := e.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, err := tx.Exec(context.Background(), `SET LOCAL lock_timeout = '5s'`); err != nil {
		t.Fatal(err)
	}
	return tx
}

// finishedOrWaiting waits until the operation behind done finished or another
// session waits for a lock. finished tells which; err is the result if it finished.
func finishedOrWaiting(t *testing.T, e emitEnv, done <-chan error) (finished bool, err error) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		select {
		case err := <-done:
			return true, err
		default:
		}
		if waiters(t, e) >= 1 {
			return false, nil
		}
	}
	t.Fatal("neither finished nor waits for a lock")
	return false, nil
}

// A final failure must not miss a success of the same webhook that another
// transaction is still recording: the webhook stays on.
func TestFinalFailureSeesConcurrentSuccess(t *testing.T) {
	e := newEmitEnv(t)
	w := e.create(t, webhook.CreateInput{Enabled: true})
	e.emitAt(t, atSec(0))
	e.emitAt(t, atSec(1))
	ids := e.deliveryIDs(t, w.ID)
	failed, succeeded := ids[0], ids[1]

	tx := beginTx(t, e)
	ok := okResult
	ok.At = atSec(10)
	if err := e.svc.RecordTx(context.Background(), tx, succeeded, ok, 1, nil); err != nil {
		t.Fatalf("record success: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		fail := failResult
		fail.At = atSec(20)
		done <- e.svc.Record(context.Background(), failed, fail, webhook.MaxAttempts, nil)
	}()
	finished, err := finishedOrWaiting(t, e, done)
	t.Logf("final failure finished before the success committed: %v", finished)
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !finished {
		err = result(t, done, "final failure")
	}
	if err != nil {
		t.Fatalf("record final failure: %v", err)
	}

	e.wantEnabled(t, w.ID, true, "after a concurrent success")
	if d := e.delivery(t, failed); d.Status != "failed" {
		t.Errorf("failed delivery status = %q, want failed", d.Status)
	}
	if d := e.delivery(t, succeeded); d.Status != "succeeded" {
		t.Errorf("succeeded delivery status = %q, want succeeded", d.Status)
	}
}

// A success recorded while a final failure's disable decision is still open
// waits for it, and the decision stands.
func TestSuccessWaitsForDisableDecision(t *testing.T) {
	e := newEmitEnv(t)
	w := e.create(t, webhook.CreateInput{Enabled: true})
	e.emitAt(t, atSec(0))
	e.emitAt(t, atSec(1))
	ids := e.deliveryIDs(t, w.ID)
	failed, succeeded := ids[0], ids[1]

	tx := beginTx(t, e)
	fail := failResult
	fail.At = atSec(10)
	if err := e.svc.RecordTx(context.Background(), tx, failed, fail, webhook.MaxAttempts, nil); err != nil {
		t.Fatalf("record final failure: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		ok := okResult
		ok.At = atSec(20)
		done <- e.svc.Record(context.Background(), succeeded, ok, 1, nil)
	}()
	waitForWaiters(t, e, 1)
	select {
	case err := <-done:
		t.Fatalf("success finished while the disable decision was open (err %v)", err)
	default:
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := result(t, done, "success"); err != nil {
		t.Fatalf("record success: %v", err)
	}

	e.wantEnabled(t, w.ID, false, "the disable decision came first")
	if d := e.delivery(t, succeeded); d.Status != "succeeded" {
		t.Errorf("succeeded delivery status = %q, want succeeded", d.Status)
	}
}
