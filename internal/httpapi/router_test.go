package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/askuksov/yalkos/internal/store"
)

const indexHTML = "<html>app</html>"

func testUI() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte(indexHTML)},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
}

func text(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
}

func newTestRouter(t *testing.T, mod func(*Deps)) http.Handler {
	t.Helper()
	d := Deps{
		Ready:         func(context.Context) error { return nil },
		UI:            testUI(),
		NativeGraphQL: text("native"),
		LinearGraphQL: text("linear"),
		Logger:        slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	if mod != nil {
		mod(&d)
	}
	return NewRouter(d)
}

func do(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func jsonBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var m map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return m
}

func TestHealthz(t *testing.T) {
	rec := do(newTestRouter(t, nil), "GET", "/healthz")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if b := jsonBody(t, rec); b["status"] != "ok" {
		t.Errorf("body = %v", b)
	}
}

func TestReadyzReady(t *testing.T) {
	rec := do(newTestRouter(t, nil), "GET", "/readyz")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if b := jsonBody(t, rec); b["status"] != "ready" {
		t.Errorf("body = %v", b)
	}
}

func TestReadyzNotReady(t *testing.T) {
	cases := []struct {
		err    error
		public string
	}{
		{fmt.Errorf("%w: failed to connect to `user=yalkos database=yalkos`: dial tcp 10.0.0.5:5432", store.ErrDatabaseUnavailable), "database unavailable"},
		{fmt.Errorf("%w: current 0, latest 1", store.ErrMigrationsPending), "migrations pending"},
		{errors.New("something else at 10.0.0.5"), "not ready"},
	}
	for _, c := range cases {
		var logs bytes.Buffer
		h := newTestRouter(t, func(d *Deps) {
			d.Ready = func(context.Context) error { return c.err }
			d.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
		})
		rec := do(h, "GET", "/readyz")
		if rec.Code != 503 {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
		b := jsonBody(t, rec)
		if b["status"] != "not ready" || b["error"] != c.public {
			t.Errorf("body = %v, want error %q", b, c.public)
		}
		if strings.Contains(rec.Body.String(), "10.0.0.5") || strings.Contains(rec.Body.String(), "user=") {
			t.Errorf("response leaks details: %s", rec.Body.String())
		}
		if !strings.Contains(logs.String(), c.err.Error()) {
			t.Errorf("full error not logged; logs: %s", logs.String())
		}
	}
}

func TestGraphQLRouting(t *testing.T) {
	h := newTestRouter(t, nil)
	if got := do(h, "POST", "/api/graphql").Body.String(); got != "native" {
		t.Errorf("POST /api/graphql → %q, want native", got)
	}
	if got := do(h, "POST", "/graphql").Body.String(); got != "linear" {
		t.Errorf("POST /graphql → %q, want linear", got)
	}
}

func TestGraphQLMethodNotAllowed(t *testing.T) {
	h := newTestRouter(t, nil)
	for _, p := range []string{"/graphql", "/api/graphql"} {
		if rec := do(h, "GET", p); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET %s → %d, want 405", p, rec.Code)
		}
	}
}

func TestSPAIndexAndFallback(t *testing.T) {
	h := newTestRouter(t, nil)
	for _, p := range []string{"/", "/projects/YLK/issues"} {
		rec := do(h, "GET", p)
		if rec.Code != 200 || rec.Body.String() != indexHTML {
			t.Errorf("GET %s → %d %q", p, rec.Code, rec.Body.String())
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("GET %s Cache-Control = %q, want no-cache", p, cc)
		}
	}
}

func TestSPAAsset(t *testing.T) {
	rec := do(newTestRouter(t, nil), "GET", "/assets/app.js")
	if rec.Code != 200 || rec.Body.String() != "console.log(1)" {
		t.Fatalf("→ %d %q", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q", cc)
	}
}

func TestSPADoesNotCoverAPIOrAssets(t *testing.T) {
	h := newTestRouter(t, nil)

	rec := do(h, "GET", "/api/nope")
	if rec.Code != 404 {
		t.Errorf("GET /api/nope → %d, want 404", rec.Code)
	}
	if b := jsonBody(t, rec); b["error"] != "not found" {
		t.Errorf("GET /api/nope body = %v", b)
	}

	rec = do(h, "GET", "/assets/missing.js")
	if rec.Code != 404 || rec.Body.String() == indexHTML {
		t.Errorf("GET /assets/missing.js → %d %q, want 404 without index", rec.Code, rec.Body.String())
	}
}

func TestUINotBuilt(t *testing.T) {
	h := newTestRouter(t, func(d *Deps) { d.UI = fstest.MapFS{".gitkeep": {}} })
	rec := do(h, "GET", "/")
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "web UI is not built") {
		t.Errorf("GET / → %d %q", rec.Code, rec.Body.String())
	}
}

func TestRequestLogged(t *testing.T) {
	var buf bytes.Buffer
	h := newTestRouter(t, func(d *Deps) { d.Logger = slog.New(slog.NewJSONHandler(&buf, nil)) })
	do(h, "GET", "/")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d log lines, want 1: %q", len(lines), buf.String())
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["path"] != "/" || entry["status"] != float64(200) || entry["method"] != "GET" {
		t.Errorf("log entry = %v", entry)
	}
	if _, ok := entry["duration_ms"]; !ok {
		t.Errorf("log entry has no duration_ms: %v", entry)
	}
}

func TestGraphQLBodyLimit(t *testing.T) {
	h := newTestRouter(t, nil)
	for _, p := range []string{"/graphql", "/api/graphql"} {
		big := strings.NewReader(strings.Repeat("x", maxGraphQLBody+1))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", p, big))
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("POST %s with %d bytes → %d, want 413", p, maxGraphQLBody+1, rec.Code)
		}
	}
}

func TestGraphQLBodyLimitUnknownLength(t *testing.T) {
	// Chunked body (no Content-Length): the handler must not be able to read past the limit.
	var read int64
	h := newTestRouter(t, func(d *Deps) {
		d.LinearGraphQL = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			read, _ = io.Copy(io.Discard, r.Body)
		})
	})
	req := httptest.NewRequest("POST", "/graphql", io.MultiReader(strings.NewReader(strings.Repeat("x", maxGraphQLBody+10))))
	req.ContentLength = -1
	h.ServeHTTP(httptest.NewRecorder(), req)
	if read > maxGraphQLBody {
		t.Errorf("handler read %d bytes, limit is %d", read, maxGraphQLBody)
	}
}

func TestAuthMiddlewareOnlyOnGraphQL(t *testing.T) {
	h := newTestRouter(t, func(d *Deps) {
		d.Auth = func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Auth-Seen", "1")
				next.ServeHTTP(w, r)
			})
		}
	})
	for _, p := range []string{"/graphql", "/api/graphql"} {
		if rec := do(h, "POST", p); rec.Header().Get("X-Auth-Seen") != "1" {
			t.Errorf("POST %s not wrapped by auth middleware", p)
		}
	}
	for _, p := range []string{"/healthz", "/", "/assets/app.js"} {
		if rec := do(h, "GET", p); rec.Header().Get("X-Auth-Seen") != "" {
			t.Errorf("GET %s wrapped by auth middleware", p)
		}
	}
}
