package webhook_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/webhook"
	"github.com/askuksov/yalkos/internal/store/q"
)

// fakeBuilder returns fixed JSON and counts calls.
type fakeBuilder struct {
	calls atomic.Int32
	err   error
	built webhook.Built
}

func newFakeBuilder() *fakeBuilder {
	return &fakeBuilder{built: webhook.Built{
		Type:  "Issue",
		Data:  json.RawMessage(`{"id":"i-1","title":"T"}`),
		Actor: json.RawMessage(`{"id":"u-1","type":"user"}`),
		URL:   ptr("http://yalkos.test/issue/YLK-1"),
	}}
}

func (b *fakeBuilder) Build(context.Context, *q.Queries, event.Change) (webhook.Built, error) {
	b.calls.Add(1)
	return b.built, b.err
}

func (b *fakeBuilder) Actor(context.Context, *q.Queries, uuid.UUID) (json.RawMessage, error) {
	return b.built.Actor, nil
}

type emitEnv struct {
	env
	svc     *webhook.Service
	builder *fakeBuilder
}

// newEmitEnv builds a service with the fake builder and an insert-only River client.
func newEmitEnv(t *testing.T) emitEnv {
	t.Helper()
	e := newEnv(t)
	client, err := river.NewClient(riverpgxv5.New(e.pool), &river.Config{})
	if err != nil {
		t.Fatalf("river client: %v", err)
	}
	b := newFakeBuilder()
	o := e.opts
	o.Builder, o.Queue = b, client
	return emitEnv{env: e, svc: webhook.New(e.pool, o), builder: b}
}

var at = time.Date(2026, 1, 1, 0, 0, 5, 0, time.UTC)

func (e emitEnv) emit(t *testing.T, c event.Change) error {
	t.Helper()
	if c.At.IsZero() {
		c.At = at
	}
	if c.ActorID == uuid.Nil {
		c.ActorID = e.users.Admin.ID
	}
	if c.EntityID == uuid.Nil {
		c.EntityID = uuid.New()
	}
	return pgx.BeginFunc(context.Background(), e.pool, func(tx pgx.Tx) error {
		return e.svc.Emit(context.Background(), tx, c)
	})
}

func (e emitEnv) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e emitEnv) deliveryWebhooks(t *testing.T) []uuid.UUID {
	t.Helper()
	rows, err := e.pool.Query(context.Background(), `SELECT webhook_id FROM webhook_deliveries`)
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

func sameIDs(t *testing.T, got, want []uuid.UUID) {
	t.Helper()
	key := func(s []uuid.UUID) []string {
		out := make([]string, len(s))
		for i, v := range s {
			out[i] = v.String()
		}
		sort.Strings(out)
		return out
	}
	if !reflect.DeepEqual(key(got), key(want)) {
		t.Fatalf("webhooks with deliveries = %v, want %v", key(got), key(want))
	}
}

func TestEmitNoMatchInsertsNothing(t *testing.T) {
	e := newEmitEnv(t)
	if err := e.emit(t, event.Change{Resource: event.Issue, Action: event.Create}); err != nil {
		t.Fatal(err)
	}
	if n := e.builder.calls.Load(); n != 0 {
		t.Fatalf("builder calls = %d, want 0", n)
	}
	if n := e.count(t, `SELECT count(*) FROM webhook_deliveries`); n != 0 {
		t.Fatalf("deliveries = %d, want 0", n)
	}
	if n := e.count(t, `SELECT count(*) FROM river_job`); n != 0 {
		t.Fatalf("jobs = %d, want 0", n)
	}
	// Positive control: a matching webhook does produce a delivery.
	e.create(t, webhook.CreateInput{Enabled: true})
	if err := e.emit(t, event.Change{Resource: event.Issue, Action: event.Create}); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT count(*) FROM webhook_deliveries`); n != 1 {
		t.Fatalf("deliveries after control = %d, want 1", n)
	}
}

func TestEmitMatching(t *testing.T) {
	e := newEmitEnv(t)
	a, b := e.project(t, "AAA"), e.project(t, "BBB")
	issue := []event.Resource{event.Issue}
	all := e.create(t, webhook.CreateInput{Enabled: true, Resources: issue})
	scopedA := e.create(t, webhook.CreateInput{Enabled: true, Resources: issue, ProjectID: &a.ID})
	e.create(t, webhook.CreateInput{Enabled: true, Resources: issue, ProjectID: &b.ID})
	e.create(t, webhook.CreateInput{Enabled: true, Resources: []event.Resource{event.Comment}})
	e.create(t, webhook.CreateInput{Enabled: false, Resources: issue})

	err := e.emit(t, event.Change{Resource: event.Issue, Action: event.Create, ProjectIDs: []uuid.UUID{a.ID}})
	if err != nil {
		t.Fatal(err)
	}
	sameIDs(t, e.deliveryWebhooks(t), []uuid.UUID{all.ID, scopedA.ID})
	if n := e.builder.calls.Load(); n != 1 {
		t.Fatalf("builder calls = %d, want 1", n)
	}
	if n := e.count(t, `SELECT count(*) FROM river_job`); n != 2 {
		t.Fatalf("jobs = %d, want 2", n)
	}
}

func TestEmitMoveMatchesBothProjects(t *testing.T) {
	e := newEmitEnv(t)
	a, b, c := e.project(t, "AAA"), e.project(t, "BBB"), e.project(t, "CCC")
	issue := []event.Resource{event.Issue}
	wa := e.create(t, webhook.CreateInput{Enabled: true, Resources: issue, ProjectID: &a.ID})
	wb := e.create(t, webhook.CreateInput{Enabled: true, Resources: issue, ProjectID: &b.ID})
	e.create(t, webhook.CreateInput{Enabled: true, Resources: issue, ProjectID: &c.ID})

	err := e.emit(t, event.Change{Resource: event.Issue, Action: event.Update, ProjectIDs: []uuid.UUID{a.ID, b.ID}})
	if err != nil {
		t.Fatal(err)
	}
	sameIDs(t, e.deliveryWebhooks(t), []uuid.UUID{wa.ID, wb.ID})
}

func TestEmitLabelMatchesEveryScope(t *testing.T) {
	e := newEmitEnv(t)
	a := e.project(t, "AAA")
	label := []event.Resource{event.Label}
	all := e.create(t, webhook.CreateInput{Enabled: true, Resources: label})
	scoped := e.create(t, webhook.CreateInput{Enabled: true, Resources: label, ProjectID: &a.ID})
	e.create(t, webhook.CreateInput{Enabled: true, Resources: []event.Resource{event.Issue}, ProjectID: &a.ID})

	if err := e.emit(t, event.Change{Resource: event.Label, Action: event.Create}); err != nil {
		t.Fatal(err)
	}
	sameIDs(t, e.deliveryWebhooks(t), []uuid.UUID{all.ID, scoped.ID})

	// Positive control for the scope rule: an issue change with no project matches
	// only the unscoped issue webhook, not the scoped one.
	if err := e.emit(t, event.Change{Resource: event.Issue, Action: event.Create}); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT count(*) FROM webhook_deliveries`); n != 2 {
		t.Fatalf("deliveries = %d, want 2 (no issue webhook is unscoped)", n)
	}
}

func TestEmitEnvelope(t *testing.T) {
	e := newEmitEnv(t)
	w := e.create(t, webhook.CreateInput{Enabled: true})
	entity := uuid.New()
	when := time.Date(2026, 1, 1, 0, 0, 5, 123_999_000, time.UTC)

	e.builder.built.UpdatedFrom = json.RawMessage(`{"title":"Old"}`)
	if err := e.emit(t, event.Change{Resource: event.Issue, Action: event.Update, EntityID: entity, At: when}); err != nil {
		t.Fatal(err)
	}
	e.builder.built.UpdatedFrom = nil
	e.builder.built.URL = nil
	if err := e.emit(t, event.Change{Resource: event.Issue, Action: event.Create, EntityID: entity, At: when}); err != nil {
		t.Fatal(err)
	}

	read := func(action string) (raw []byte, createdAt time.Time, resource string, ent *uuid.UUID, status string) {
		err := e.pool.QueryRow(context.Background(),
			`SELECT payload::text, created_at, resource, entity_id, status FROM webhook_deliveries WHERE action = $1`, action,
		).Scan(&raw, &createdAt, &resource, &ent, &status)
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	keys := func(raw []byte) []string {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	field := func(raw []byte, k string) string {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		return string(m[k])
	}

	upd, createdAt, resource, ent, status := read("update")
	wantKeys := []string{"action", "actor", "createdAt", "data", "organizationId", "type", "updatedFrom", "url", "webhookId"}
	if got := keys(upd); !reflect.DeepEqual(got, wantKeys) {
		t.Fatalf("update keys = %v, want %v", got, wantKeys)
	}
	if got := field(upd, "createdAt"); got != `"2026-01-01T00:00:05.123Z"` {
		t.Fatalf("createdAt = %s", got)
	}
	if got := field(upd, "url"); got != `"http://yalkos.test/issue/YLK-1"` {
		t.Fatalf("url = %s", got)
	}
	if got := field(upd, "updatedFrom"); got != `{"title": "Old"}` {
		t.Fatalf("updatedFrom = %s", got)
	}
	if got := field(upd, "action"); got != `"update"` {
		t.Fatalf("action = %s", got)
	}
	if got := field(upd, "type"); got != `"Issue"` {
		t.Fatalf("type = %s", got)
	}
	if got := field(upd, "webhookId"); got != `"`+w.ID.String()+`"` {
		t.Fatalf("webhookId = %s, want %s", got, w.ID)
	}
	var orgID uuid.UUID
	if err := e.pool.QueryRow(context.Background(), `SELECT workspace_id FROM webhooks WHERE id = $1`, w.ID).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if got := field(upd, "organizationId"); got != `"`+orgID.String()+`"` {
		t.Fatalf("organizationId = %s, want %s", got, orgID)
	}
	if got := field(upd, "actor"); got != `{"id": "u-1", "type": "user"}` {
		t.Fatalf("actor = %s", got)
	}
	if got := field(upd, "data"); got != `{"id": "i-1", "title": "T"}` {
		t.Fatalf("data = %s", got)
	}
	if !createdAt.Equal(when) || resource != "issue" || ent == nil || *ent != entity || status != "pending" {
		t.Fatalf("row = %v %s %v %s", createdAt, resource, ent, status)
	}

	crt, _, _, _, _ := read("create")
	wantCreate := []string{"action", "actor", "createdAt", "data", "organizationId", "type", "url", "webhookId"}
	if got := keys(crt); !reflect.DeepEqual(got, wantCreate) {
		t.Fatalf("create keys = %v, want %v (updatedFrom absent)", got, wantCreate)
	}
	if got := field(crt, "url"); got != "null" {
		t.Fatalf("url = %s, want null", got)
	}
}

func TestEmitJobPerDelivery(t *testing.T) {
	e := newEmitEnv(t)
	e.create(t, webhook.CreateInput{Enabled: true})
	e.create(t, webhook.CreateInput{Enabled: true})
	if err := e.emit(t, event.Change{Resource: event.Issue, Action: event.Create}); err != nil {
		t.Fatal(err)
	}
	rows, err := e.pool.Query(context.Background(),
		`SELECT kind, queue, max_attempts, args->>'deliveryId' FROM river_job`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	jobs := map[string]bool{}
	for rows.Next() {
		var kind, queue, id string
		var maxAttempts int
		if err := rows.Scan(&kind, &queue, &maxAttempts, &id); err != nil {
			t.Fatal(err)
		}
		if kind != "webhook_delivery" || queue != "webhooks" || maxAttempts != 4 {
			t.Fatalf("job = %s/%s/%d, want webhook_delivery/webhooks/4", kind, queue, maxAttempts)
		}
		jobs[id] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	drows, err := e.pool.Query(context.Background(), `SELECT id::text FROM webhook_deliveries`)
	if err != nil {
		t.Fatal(err)
	}
	defer drows.Close()
	n := 0
	for drows.Next() {
		var id string
		if err := drows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		if !jobs[id] {
			t.Fatalf("no job for delivery %s (jobs %v)", id, jobs)
		}
		n++
	}
	if n != 2 || len(jobs) != 2 {
		t.Fatalf("deliveries = %d, jobs = %d, want 2 and 2", n, len(jobs))
	}
}

func TestEmitRollsBackWithChange(t *testing.T) {
	e := newEmitEnv(t)
	e.create(t, webhook.CreateInput{Enabled: true})
	boom := errors.New("change failed")
	err := pgx.BeginFunc(context.Background(), e.pool, func(tx pgx.Tx) error {
		c := event.Change{Resource: event.Issue, Action: event.Create, EntityID: uuid.New(), At: at}
		if err := e.svc.Emit(context.Background(), tx, c); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM webhook_deliveries`).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			t.Errorf("deliveries inside tx = %d, want 1", n)
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the change error", err)
	}
	if n := e.count(t, `SELECT count(*) FROM webhook_deliveries`); n != 0 {
		t.Fatalf("deliveries = %d, want 0 after rollback", n)
	}
	if n := e.count(t, `SELECT count(*) FROM river_job`); n != 0 {
		t.Fatalf("jobs = %d, want 0 after rollback", n)
	}
}

func TestEmitBuilderErrorFails(t *testing.T) {
	e := newEmitEnv(t)
	e.create(t, webhook.CreateInput{Enabled: true})
	boom := errors.New("build failed")
	e.builder.err = boom
	if err := e.emit(t, event.Change{Resource: event.Issue, Action: event.Create}); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want builder error", err)
	}
	if n := e.count(t, `SELECT count(*) FROM webhook_deliveries`); n != 0 {
		t.Fatalf("deliveries = %d, want 0", n)
	}
	// Positive control: the same emit succeeds without the error.
	e.builder.err = nil
	if err := e.emit(t, event.Change{Resource: event.Issue, Action: event.Create}); err != nil {
		t.Fatal(err)
	}
}

func TestEmitNeedsBuilderAndQueue(t *testing.T) {
	e := newEmitEnv(t)
	e.create(t, webhook.CreateInput{Enabled: true})
	client, err := river.NewClient(riverpgxv5.New(e.pool), &river.Config{})
	if err != nil {
		t.Fatal(err)
	}
	emitWith := func(o webhook.Options, c event.Change) error {
		c.At, c.ActorID, c.EntityID = at, e.users.Admin.ID, uuid.New()
		return pgx.BeginFunc(context.Background(), e.pool, func(tx pgx.Tx) error {
			return webhook.New(e.pool, o).Emit(context.Background(), tx, c)
		})
	}
	issueCreate := event.Change{Resource: event.Issue, Action: event.Create}
	for name, o := range map[string]webhook.Options{
		"no builder, no queue": e.opts,
		"no builder":           {Now: e.opts.Now, Queue: client},
		"no queue":             {Now: e.opts.Now, Builder: e.builder},
	} {
		err := emitWith(o, issueCreate)
		if err == nil || !strings.Contains(err.Error(), "builder and queue are required") {
			t.Errorf("%s: err = %v, want builder and queue are required", name, err)
		}
	}
	if n := e.count(t, `SELECT count(*) FROM webhook_deliveries`); n != 0 {
		t.Errorf("deliveries = %d, want 0", n)
	}
	// Positive controls: no match needs neither, and a full setup succeeds.
	if err := emitWith(e.opts, event.Change{Resource: event.Comment, Action: event.Create}); err != nil {
		t.Errorf("no match without builder/queue: %v", err)
	}
	full := e.opts
	full.Builder, full.Queue = e.builder, client
	if err := emitWith(full, issueCreate); err != nil {
		t.Errorf("full setup: %v", err)
	}
	if n := e.count(t, `SELECT count(*) FROM webhook_deliveries`); n != 1 {
		t.Errorf("deliveries after control = %d, want 1", n)
	}
}

func TestDeliveriesNewestFirstAndLimit(t *testing.T) {
	e := newEmitEnv(t)
	w := e.create(t, webhook.CreateInput{Enabled: true})
	other := e.create(t, webhook.CreateInput{Enabled: true, Resources: []event.Resource{event.Comment}})
	ents := make([]uuid.UUID, 3)
	for i := range ents {
		ents[i] = uuid.New()
		c := event.Change{Resource: event.Issue, Action: event.Create, EntityID: ents[i], At: at.Add(time.Duration(i) * time.Second)}
		if err := e.emit(t, c); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.emit(t, event.Change{Resource: event.Comment, Action: event.Create, At: at}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	got, err := e.svc.Deliveries(ctx, e.users.Admin, w.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (only this webhook's deliveries)", len(got))
	}
	for i, d := range got {
		want := ents[len(ents)-1-i]
		if d.EntityID == nil || *d.EntityID != want || d.WebhookID != w.ID {
			t.Fatalf("delivery %d = %+v, want entity %s", i, d, want)
		}
		if d.Resource != "issue" || d.Action != "create" || d.Status != "pending" || d.Attempts != 0 {
			t.Fatalf("delivery %d = %+v", i, d)
		}
	}
	if got, err := e.svc.Deliveries(ctx, e.users.Admin, w.ID, 2); err != nil || len(got) != 2 || *got[0].EntityID != ents[2] {
		t.Fatalf("limit 2 = %d rows, err %v", len(got), err)
	}
	if got, err := e.svc.Deliveries(ctx, e.users.Admin, other.ID, 1); err != nil || len(got) != 1 {
		t.Fatalf("other webhook = %d rows, err %v", len(got), err)
	}
	for _, n := range []int{0, 51, -1} {
		_, err := e.svc.Deliveries(ctx, e.users.Admin, w.ID, n)
		wantInput(t, err, "first", kit.ReasonOutOfRange, "must be 1–50")
		if p := inputParams(err); p["min"] != 1 || p["max"] != 50 {
			t.Fatalf("limit %d params = %v", n, p)
		}
	}
	if _, err := e.svc.Deliveries(ctx, e.users.Member, w.ID, 10); !errors.Is(err, kit.ErrForbidden) {
		t.Fatalf("member err = %v, want ErrForbidden", err)
	}
	if _, err := e.svc.Deliveries(ctx, e.users.Owner, w.ID, 10); err != nil {
		t.Fatalf("Owner role: %v", err)
	}
}
