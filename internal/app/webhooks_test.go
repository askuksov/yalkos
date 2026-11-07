package app_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/db"
	"github.com/askuksov/yalkos/internal/config"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/domain/webhook"
	"github.com/askuksov/yalkos/internal/store"
	"github.com/askuksov/yalkos/internal/testdb"
)

const hookSecret = "app-test-secret"

// received is one request that reached the receiver.
type received struct {
	header http.Header
	body   []byte
}

// hookApp is a migrated database with an owner, an API key, a project and a
// webhook that posts issue events to a receiver.
type hookApp struct {
	dbURL     string
	pool      *pgxpool.Pool
	key       string
	projectID string
	hookID    string
}

func newHookApp(t *testing.T, receiverURL string) hookApp {
	t.Helper()
	url, pool := testdb.New(t)
	ctx := context.Background()
	if err := store.Migrate(ctx, pool, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	accounts := account.New(pool, time.Now, "http://localhost:8080")
	_ = accounts.RefreshSetupCode(ctx)
	owner, _, err := accounts.Setup(ctx, account.SetupInput{Code: accounts.SetupCode(), WorkspaceName: "W", Name: "O",
		Email: "o@example.com", Password: "owner-password"}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := accounts.CreateAPIKey(ctx, owner, "k")
	if err != nil {
		t.Fatal(err)
	}
	p, err := project.New(pool, time.Now).Create(ctx, owner, project.CreateInput{Key: "APP", Name: "App"})
	if err != nil {
		t.Fatal(err)
	}
	secret := hookSecret
	w, err := webhook.New(pool, webhook.Options{AllowPrivate: true}).Create(ctx, owner, webhook.CreateInput{
		URL: receiverURL, Name: "receiver", Resources: []event.Resource{event.Issue}, Secret: &secret, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return hookApp{dbURL: url, pool: pool, key: key, projectID: p.ID.String(), hookID: w.ID.String()}
}

func (h hookApp) config() config.Config {
	return config.Config{DatabaseURL: h.dbURL, ListenAddr: "127.0.0.1:0", WebhookAllowPrivate: true}
}

// createIssue creates an issue through the native API.
func (h hookApp) createIssue(t *testing.T, addr string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"query":     `mutation($i: CreateIssueInput!) { createIssue(input: $i) { id } }`,
		"variables": map[string]any{"i": map[string]any{"projectId": h.projectID, "title": "Hello webhook"}},
	})
	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/api/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	var res struct {
		Errors []json.RawMessage `json:"errors"`
	}
	if resp.StatusCode != 200 || json.Unmarshal(out, &res) != nil || len(res.Errors) != 0 {
		t.Fatalf("createIssue: %d %s", resp.StatusCode, out)
	}
}

// deliveryStatus returns the status of the webhook's only delivery.
func (h hookApp) deliveryStatus(t *testing.T) string {
	t.Helper()
	var st string
	err := h.pool.QueryRow(context.Background(),
		`SELECT status FROM webhook_deliveries WHERE webhook_id = $1`, h.hookID).Scan(&st)
	if err != nil {
		t.Fatalf("delivery status: %v", err)
	}
	return st
}

// waitDeliveryStatus polls until the delivery has the status.
func (h hookApp) waitDeliveryStatus(t *testing.T, want string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		var st string
		err := h.pool.QueryRow(context.Background(),
			`SELECT status FROM webhook_deliveries WHERE webhook_id = $1`, h.hookID).Scan(&st)
		if err == nil && st == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("delivery status = %q (%v), want %q within %v", st, err, want, within)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestRunDeliversSignedWebhook(t *testing.T) {
	got := make(chan received, 4)
	recv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- received{header: r.Header.Clone(), body: b}
	}))
	defer recv.Close()
	h := newHookApp(t, recv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := h.config()
	cfg.DataDir = t.TempDir()
	s := start(ctx, t, cfg)

	h.createIssue(t, s.addr)

	var r received
	select {
	case r = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("receiver got no request within 5s")
	}
	mac := hmac.New(sha256.New, []byte(hookSecret))
	mac.Write(r.body)
	if sig := r.header.Get("Linear-Signature"); sig != hex.EncodeToString(mac.Sum(nil)) {
		t.Errorf("Linear-Signature = %q, want HMAC-SHA256 of the body", sig)
	}
	if ev := r.header.Get("Linear-Event"); ev != "Issue" {
		t.Errorf("Linear-Event = %q, want Issue", ev)
	}
	var p struct {
		Type             string `json:"type"`
		Action           string `json:"action"`
		WebhookTimestamp int64  `json:"webhookTimestamp"`
		Data             struct {
			Title string `json:"title"`
		} `json:"data"`
	}
	if err := json.Unmarshal(r.body, &p); err != nil {
		t.Fatalf("body is not JSON: %v: %s", err, r.body)
	}
	if p.Type != "Issue" || p.Action != "create" || p.Data.Title != "Hello webhook" {
		t.Errorf("payload = %+v, want Issue/create/Hello webhook", p)
	}
	if d := time.Since(time.UnixMilli(p.WebhookTimestamp)); d < -time.Minute || d > time.Minute {
		t.Errorf("webhookTimestamp %d is %v from now, want within 60s", p.WebhookTimestamp, d)
	}
	if ts := r.header.Get("Linear-Timestamp"); ts != strconv.FormatInt(p.WebhookTimestamp, 10) {
		t.Errorf("Linear-Timestamp = %q, want %d", ts, p.WebhookTimestamp)
	}
	h.waitDeliveryStatus(t, "succeeded", 5*time.Second)
	// Exactly one request: nothing else follows.
	select {
	case extra := <-got:
		t.Errorf("second request: %s", extra.body)
	case <-time.After(500 * time.Millisecond):
	}
}

// TestRunStopsRiverOnShutdown: a delivery in flight at shutdown is not lost.
func TestRunStopsRiverOnShutdown(t *testing.T) {
	// run creates an issue, cancels the app while the receiver holds the
	// request and returns how long Run took to stop. The handler gets a
	// channel that is closed when the test ends.
	run := func(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, release <-chan struct{})) (hookApp, time.Duration) {
		t.Helper()
		arrived := make(chan struct{}, 4)
		release := make(chan struct{})
		recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			arrived <- struct{}{}
			handler(w, r, release)
		}))
		t.Cleanup(recv.Close)
		// Registered after the server's Close, so it runs first: blocked
		// handlers must return for Close to finish.
		t.Cleanup(func() { close(release) })
		h := newHookApp(t, recv.URL)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cfg := h.config()
		cfg.DataDir = t.TempDir()
		s := start(ctx, t, cfg)
		h.createIssue(t, s.addr)
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatal("receiver got no request within 5s")
		}
		began := time.Now()
		cancel()
		select {
		case err := <-s.errCh:
			if err != nil {
				t.Errorf("Run = %v, want nil", err)
			}
		case <-time.After(12 * time.Second):
			t.Fatal("Run did not stop within 12s")
		}
		return h, time.Since(began)
	}

	t.Run("slow receiver finishes within the soft stop", func(t *testing.T) {
		h, took := run(t, func(w http.ResponseWriter, _ *http.Request, _ <-chan struct{}) {
			time.Sleep(3 * time.Second)
			w.WriteHeader(http.StatusOK)
		})
		if st := h.deliveryStatus(t); st != "succeeded" {
			t.Errorf("delivery status = %q, want succeeded (took %v)", st, took)
		}
	})

	t.Run("receiver that outlasts the attempt leaves a retry", func(t *testing.T) {
		h, took := run(t, func(_ http.ResponseWriter, r *http.Request, release <-chan struct{}) {
			select {
			case <-release:
			case <-r.Context().Done():
			}
		})
		if st := h.deliveryStatus(t); st != "retrying" {
			t.Errorf("delivery status = %q, want retrying (took %v)", st, took)
		}
		var state string
		if err := h.pool.QueryRow(context.Background(), `SELECT state FROM river_job`).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state != "retryable" {
			t.Errorf("job state = %q, want retryable", state)
		}
	})
}
