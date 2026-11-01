package ratelimit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/google/uuid"
)

func serve(l *Limiter, who func(*http.Request) (uuid.UUID, bool), path string) *httptest.ResponseRecorder {
	h := Middleware(l, who)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
	return rec
}

func fixedUser(u uuid.UUID) func(*http.Request) (uuid.UUID, bool) {
	return func(*http.Request) (uuid.UUID, bool) { return u, true }
}

func TestMiddlewareHeaders(t *testing.T) {
	c := newClock()
	l := New(3, c.now)
	rec := serve(l, fixedUser(uuid.New()), "/graphql")
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Fatalf("status %d body %q", rec.Code, rec.Body)
	}
	h := rec.Header()
	reset := strconv.FormatInt(c.now().Add(20*60e9).UnixMilli(), 10)
	if h.Get("X-RateLimit-Requests-Limit") != "3" || h.Get("X-RateLimit-Requests-Remaining") != "2" || h.Get("X-RateLimit-Requests-Reset") != reset {
		t.Fatalf("headers = %v, want reset %s", h, reset)
	}
}

func exhaust(l *Limiter, u uuid.UUID) {
	for range 3 {
		l.Take(u)
	}
}

const wantMsg = "Rate limit exceeded: 3 requests per hour. Try again in 1200 seconds."

func TestRateLimitGraphQLBody(t *testing.T) {
	for _, path := range []string{"/graphql", "/api/graphql"} {
		l := New(3, newClock().now)
		u := uuid.New()
		exhaust(l, u)
		rec := serve(l, fixedUser(u), path)
		if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "1200" || rec.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("%s: status %d headers %v", path, rec.Code, rec.Header())
		}
		if rec.Header().Get("X-RateLimit-Requests-Remaining") != "0" {
			t.Fatalf("%s: remaining %q", path, rec.Header().Get("X-RateLimit-Requests-Remaining"))
		}
		var body struct {
			Errors []struct {
				Message    string         `json:"message"`
				Extensions map[string]any `json:"extensions"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Errors) != 1 {
			t.Fatalf("%s: body %s (%v)", path, rec.Body, err)
		}
		e := body.Errors[0]
		if e.Message != wantMsg || e.Extensions["code"] != "RATELIMITED" || e.Extensions["type"] != "ratelimited" ||
			e.Extensions["userError"] != true || e.Extensions["userPresentableMessage"] != wantMsg {
			t.Fatalf("%s: error = %+v", path, e)
		}
	}
}

func TestRateLimitMCPBody(t *testing.T) {
	for _, path := range []string{"/mcp", "/api/attachments"} {
		l := New(3, newClock().now)
		u := uuid.New()
		exhaust(l, u)
		rec := serve(l, fixedUser(u), path)
		if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "1200" {
			t.Fatalf("%s: status %d headers %v", path, rec.Code, rec.Header())
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] != wantMsg || len(body) != 1 {
			t.Fatalf("%s: body %s (%v)", path, rec.Body, err)
		}
	}
}

func TestMiddlewareSkipsSessions(t *testing.T) {
	l := New(1, newClock().now)
	anon := func(*http.Request) (uuid.UUID, bool) { return uuid.Nil, false }
	for range 3 {
		rec := serve(l, anon, "/graphql")
		if rec.Code != 200 || rec.Header().Get("X-RateLimit-Requests-Limit") != "" {
			t.Fatalf("session request: %d %v", rec.Code, rec.Header())
		}
	}
	u := uuid.New()
	for range 3 {
		if rec := serve(nil, fixedUser(u), "/graphql"); rec.Code != 200 || rec.Header().Get("X-RateLimit-Requests-Limit") != "" {
			t.Fatalf("nil limiter: %d %v", rec.Code, rec.Header())
		}
	}
	// Positive control: the same limiter does limit an API-key user.
	serve(l, fixedUser(u), "/graphql")
	if rec := serve(l, fixedUser(u), "/graphql"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("key user not limited: %d", rec.Code)
	}
}
