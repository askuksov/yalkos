package plane

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/askuksov/yalkos/internal/netguard"
)

// fakeClock records the sleeps of a client instead of sleeping.
type fakeClock struct {
	mu     sync.Mutex
	sleeps []time.Duration
	now    time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sleeps = append(c.sleeps, d)
	return ctx.Err()
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) recorded() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.sleeps)
}

func newTestClient(t *testing.T, f *fakePlane, clock *fakeClock) *Client {
	t.Helper()
	c, err := NewClient(f.URL(), fakeWorkspace, f.key, ClientOptions{Sleep: clock.sleep, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func assertSleeps(t *testing.T, clock *fakeClock, want ...time.Duration) {
	t.Helper()
	if got := clock.recorded(); !slices.Equal(got, want) {
		t.Fatalf("sleeps = %v, want %v", got, want)
	}
}

func TestPagination(t *testing.T) {
	f := newFakePlane(t)
	const path = "projects/p1/work-items/"
	items := make([]any, 250)
	for i := range items {
		items[i] = map[string]any{"id": fmt.Sprintf("w%03d", i), "sequence_id": i + 1}
	}
	f.setPaged(path, items...)
	c := newTestClient(t, f, newFakeClock())

	got, err := c.WorkItems(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 250 {
		t.Fatalf("got %d work items, want 250", len(got))
	}
	for i, w := range got {
		if w.ID != fmt.Sprintf("w%03d", i) || w.SequenceID != i+1 {
			t.Fatalf("item %d = %+v", i, w)
		}
	}
	if n := f.Requests(path); n != 3 {
		t.Fatalf("requests = %d, want 3", n)
	}
	qs := f.Queries(path)
	for i, q := range qs {
		if q.Get("per_page") != "100" {
			t.Errorf("request %d: per_page = %q", i, q.Get("per_page"))
		}
	}
	if qs[0].Get("cursor") != "" || qs[1].Get("cursor") != "100:1:0" || qs[2].Get("cursor") != "100:2:0" {
		t.Errorf("cursors = %q %q %q", qs[0].Get("cursor"), qs[1].Get("cursor"), qs[2].Get("cursor"))
	}
	if c.Requests() != 3 {
		t.Errorf("Requests() = %d, want 3", c.Requests())
	}
}

func TestRetryAfter429(t *testing.T) {
	for _, tc := range []struct {
		name, header string
		want         time.Duration
	}{
		{"header", "7", 7 * time.Second},
		{"missing", "", 60 * time.Second},
		{"invalid", "soon", 60 * time.Second},
		{"zero", "0", time.Second},
		{"negative", "-3", time.Second},
		{"huge", "99999999999", 300 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakePlane(t)
			const path = "projects/p1/states/"
			f.setPaged(path, map[string]any{"id": "s1", "name": "Todo", "group": "unstarted"})
			f.rateLimitOnce(path, tc.header)
			clock := newFakeClock()
			c := newTestClient(t, f, clock)

			got, err := c.States(context.Background(), "p1")
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].ID != "s1" {
				t.Fatalf("states = %+v", got)
			}
			assertSleeps(t, clock, tc.want)
			if n := f.Requests(path); n != 2 {
				t.Fatalf("requests = %d, want 2", n)
			}
		})
	}
}

func TestRemainingZeroWaits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reset time.Duration
		want  time.Duration
	}{
		{"short", 20 * time.Second, 20 * time.Second},
		{"capped", 500 * time.Second, 61 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakePlane(t)
			f.setPaged("projects/", map[string]any{"id": "p1"})
			f.setPlain("members/", []any{})
			clock := newFakeClock()
			f.headerOn("projects/", "X-Ratelimit-Remaining", "0")
			f.headerOn("projects/", "X-Ratelimit-Reset", fmt.Sprint(clock.now.Add(tc.reset).Unix()))
			c := newTestClient(t, f, clock)

			if _, err := c.Projects(context.Background()); err != nil {
				t.Fatal(err)
			}
			assertSleeps(t, clock)
			if _, err := c.Members(context.Background()); err != nil {
				t.Fatal(err)
			}
			assertSleeps(t, clock, tc.want)
			if _, err := c.Members(context.Background()); err != nil {
				t.Fatal(err)
			}
			assertSleeps(t, clock, tc.want) // the wait is spent once
		})
	}
}

func TestServerErrorRetries(t *testing.T) {
	const path = "projects/p1/labels/"
	backoff := []time.Duration{time.Second, 5 * time.Second, 30 * time.Second}

	t.Run("recovers", func(t *testing.T) {
		f := newFakePlane(t)
		f.setPaged(path, map[string]any{"id": "l1", "name": "bug"})
		f.failOn(path, http.StatusBadGateway, 3)
		clock := newFakeClock()
		c := newTestClient(t, f, clock)

		got, err := c.Labels(context.Background(), "p1")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Name != "bug" {
			t.Fatalf("labels = %+v", got)
		}
		assertSleeps(t, clock, backoff...)
		if n := f.Requests(path); n != 4 {
			t.Fatalf("requests = %d, want 4", n)
		}
	})

	t.Run("gives up", func(t *testing.T) {
		f := newFakePlane(t)
		f.setPaged(path, map[string]any{"id": "l1"})
		f.failOn(path, http.StatusBadGateway, 4)
		clock := newFakeClock()
		c := newTestClient(t, f, clock)

		_, err := c.Labels(context.Background(), "p1")
		if err == nil {
			t.Fatal("want error")
		}
		if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "502") {
			t.Fatalf("error %q does not name the path and 502", err)
		}
		var se *StatusError
		if !errors.As(err, &se) || se.Code != http.StatusBadGateway {
			t.Fatalf("error %v is not a 502 StatusError", err)
		}
		assertSleeps(t, clock, backoff...)
		if n := f.Requests(path); n != 4 {
			t.Fatalf("requests = %d, want 4", n)
		}
	})

	t.Run("network error", func(t *testing.T) {
		f := newFakePlane(t)
		clock := newFakeClock()
		c, err := NewClient(f.URL(), fakeWorkspace, f.key, ClientOptions{Sleep: clock.sleep, Now: clock.Now})
		if err != nil {
			t.Fatal(err)
		}
		f.srv.Close()
		if _, err := c.Labels(context.Background(), "p1"); err == nil {
			t.Fatal("want error")
		}
		assertSleeps(t, clock, backoff...)
		if c.Requests() != 4 {
			t.Fatalf("requests = %d, want 4", c.Requests())
		}
	})
}

func TestNotFoundNoRetry(t *testing.T) {
	f := newFakePlane(t)
	clock := newFakeClock()
	c := newTestClient(t, f, clock)

	_, err := c.Modules(context.Background(), "missing")
	if err == nil {
		t.Fatal("want error")
	}
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusNotFound {
		t.Fatalf("error = %v, want 404 StatusError", err)
	}
	if n := f.Requests("projects/missing/modules/"); n != 1 {
		t.Fatalf("requests = %d, want 1", n)
	}
	assertSleeps(t, clock)
}

func TestContextCancelStopsRetries(t *testing.T) {
	f := newFakePlane(t)
	f.failOn("projects/", http.StatusBadGateway, -1)
	ctx, cancel := context.WithCancel(context.Background())
	c, err := NewClient(f.URL(), fakeWorkspace, f.key, ClientOptions{
		Sleep: func(ctx context.Context, _ time.Duration) error {
			cancel()
			return ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Projects(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if n := f.Requests("projects/"); n != 1 {
		t.Fatalf("requests = %d, want 1", n)
	}
}

func TestKeyNotSentAcrossHosts(t *testing.T) {
	const path = "projects/p1/work-items/i1/attachments/a1/"
	file := fakeFile{Name: "notes.txt", ContentType: "text/plain", Body: "hello"}

	t.Run("other host", func(t *testing.T) {
		var mu sync.Mutex
		var seen []string
		other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			seen = append(seen, r.Header.Get("X-API-Key"))
			mu.Unlock()
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Disposition", `attachment; filename*=UTF-8''notes.txt`)
			_, _ = io.WriteString(w, "hello")
		}))
		defer other.Close()
		f := newFakePlane(t)
		f.setFile(path, file)
		f.redirectAttachment(other.URL + "/blob?sig=1")
		c := newTestClient(t, f, newFakeClock())

		d, err := c.Download(context.Background(), "p1", "i1", "a1", 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(d.Body)
		_ = d.Body.Close()
		if err != nil || string(body) != "hello" {
			t.Fatalf("body = %q, err = %v", body, err)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(seen) != 1 || seen[0] != "" {
			t.Fatalf("other host saw keys %q, want one empty", seen)
		}
		if keys := f.keys[path]; len(keys) != 1 || keys[0] != f.key {
			t.Fatalf("API host saw keys %q", keys)
		}
	})

	t.Run("same host", func(t *testing.T) {
		f := newFakePlane(t)
		f.setFile(path, file)
		c := newTestClient(t, f, newFakeClock())

		d, err := c.Download(context.Background(), "p1", "i1", "a1", 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(d.Body)
		_ = d.Body.Close()
		if string(body) != "hello" {
			t.Fatalf("body = %q", body)
		}
		if d.Name != "notes.txt" || d.ContentType != "text/plain" {
			t.Fatalf("download = %q %q", d.Name, d.ContentType)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if keys := f.keys["storage/"+path]; len(keys) != 1 || keys[0] != "" {
			t.Fatalf("same-host redirect keys = %q, want no key", keys)
		}
	})

	t.Run("same host over plain http", func(t *testing.T) {
		var mu sync.Mutex
		var seen []string
		rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
			mu.Lock()
			seen = append(seen, r.URL.Scheme+" key="+r.Header.Get("X-API-Key"))
			mu.Unlock()
			h := http.Header{}
			if r.URL.Scheme == "https" {
				h.Set("Location", "http://plane.example.invalid/uploads/f")
				return &http.Response{StatusCode: http.StatusFound, Header: h, Body: http.NoBody, Request: r}, nil
			}
			h.Set("Content-Type", "text/plain")
			return &http.Response{StatusCode: http.StatusOK, Header: h, Body: io.NopCloser(strings.NewReader("hello")), ContentLength: 5, Request: r}, nil
		})
		c, err := NewClient("https://plane.example.invalid", fakeWorkspace, fakeKey, ClientOptions{HTTP: &http.Client{Transport: rt}})
		if err != nil {
			t.Fatal(err)
		}
		d, err := c.Download(context.Background(), "p1", "i1", "a1", 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		_ = d.Body.Close()
		mu.Lock()
		defer mu.Unlock()
		want := []string{"https key=" + fakeKey, "http key="}
		if !slices.Equal(seen, want) {
			t.Fatalf("requests = %q, want %q", seen, want)
		}
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRateLimitHeadersOnRedirect(t *testing.T) {
	const path = "projects/p1/work-items/i1/attachments/a1/"
	f := newFakePlane(t)
	f.setFile(path, fakeFile{Name: "a.txt", Body: "abc"})
	f.setPlain("members/", []any{})
	clock := newFakeClock()
	f.headerOn(path, "X-Ratelimit-Remaining", "0")
	f.headerOn(path, "X-Ratelimit-Reset", fmt.Sprint(clock.now.Add(20*time.Second).Unix()))
	c := newTestClient(t, f, clock)

	d, err := c.Download(context.Background(), "p1", "i1", "a1", 10)
	if err != nil {
		t.Fatal(err)
	}
	_ = d.Body.Close()
	assertSleeps(t, clock)
	if _, err := c.Members(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertSleeps(t, clock, 20*time.Second)
}

func TestPaginationCursorGuards(t *testing.T) {
	serve := func(t *testing.T, next func(cursor string) (string, bool)) *Client {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cursor, more := next(r.URL.Query().Get("cursor"))
			writeJSON(w, map[string]any{
				"results": []any{map[string]any{"id": "x"}}, "next_cursor": cursor, "next_page_results": more,
			})
		}))
		t.Cleanup(srv.Close)
		c, err := NewClient(srv.URL, fakeWorkspace, fakeKey, ClientOptions{Sleep: newFakeClock().sleep})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	t.Run("cycle", func(t *testing.T) {
		c := serve(t, func(cursor string) (string, bool) {
			switch cursor {
			case "":
				return "A", true
			case "A":
				return "B", true
			}
			return "A", true
		})
		_, err := c.Projects(context.Background())
		if err == nil || !strings.Contains(err.Error(), "repeats") {
			t.Fatalf("error = %v, want a repeated cursor error", err)
		}
		if c.Requests() != 3 {
			t.Fatalf("requests = %d, want 3", c.Requests())
		}
	})

	t.Run("empty cursor", func(t *testing.T) {
		c := serve(t, func(string) (string, bool) { return "", true })
		_, err := c.Projects(context.Background())
		if err == nil || !strings.Contains(err.Error(), "no next cursor") {
			t.Fatalf("error = %v, want an empty cursor error", err)
		}
	})
}

func TestRateLimitedCancel(t *testing.T) {
	f := newFakePlane(t)
	f.rateLimitOnce("projects/", "5")
	ctx, cancel := context.WithCancel(context.Background())
	c, err := NewClient(f.URL(), fakeWorkspace, f.key, ClientOptions{
		Sleep: func(ctx context.Context, _ time.Duration) error {
			cancel()
			return ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Projects(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if n := f.Requests("projects/"); n != 1 {
		t.Fatalf("requests = %d, want 1", n)
	}
}

func TestErrorsHideKey(t *testing.T) {
	const secret = "plane_api_secretvalue"
	f := newFakePlane(t)
	f.key = secret
	clock := newFakeClock()
	ctx := context.Background()

	calls := func(c *Client) map[string]error {
		out := map[string]error{}
		_, out["Members"] = c.Members(ctx)
		_, out["Projects"] = c.Projects(ctx)
		_, out["ProjectMembers"] = c.ProjectMembers(ctx, "p")
		_, out["States"] = c.States(ctx, "p")
		_, out["Labels"] = c.Labels(ctx, "p")
		_, out["Modules"] = c.Modules(ctx, "p")
		_, out["ModuleIssues"] = c.ModuleIssues(ctx, "p", "m")
		_, out["WorkItems"] = c.WorkItems(ctx, "p")
		_, out["Comments"] = c.Comments(ctx, "p", "i")
		_, out["Attachments"] = c.Attachments(ctx, "p", "i")
		_, out["Links"] = c.Links(ctx, "p", "i")
		_, out["Relations"] = c.Relations(ctx, "p", "i")
		_, out["Download"] = c.Download(ctx, "p", "i", "a", 10)
		return out
	}
	check := func(name string, c *Client) {
		t.Helper()
		for call, err := range calls(c) {
			if err == nil {
				t.Errorf("%s/%s: want error", name, call)
				continue
			}
			if strings.Contains(err.Error(), "secretvalue") {
				t.Errorf("%s/%s: error leaks the key: %v", name, call, err)
			}
		}
	}

	// 404 for every path.
	c, err := NewClient(f.URL(), fakeWorkspace, secret, ClientOptions{Sleep: clock.sleep, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	check("404", c)

	// 401 for a wrong key that is a variation of the real one.
	c, err = NewClient(f.URL(), fakeWorkspace, secret+"x", ClientOptions{Sleep: clock.sleep, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	check("401", c)

	// 502 forever.
	f.failOn("projects/", http.StatusBadGateway, -1)
	f.failOn("members/", http.StatusBadGateway, -1)
	c, _ = NewClient(f.URL(), fakeWorkspace, secret, ClientOptions{Sleep: clock.sleep, Now: clock.Now})
	check("502", c)

	// Refused connection.
	f.srv.Close()
	c, _ = NewClient(f.URL(), fakeWorkspace, secret, ClientOptions{Sleep: clock.sleep, Now: clock.Now})
	check("closed", c)

	// Constructor errors.
	if _, err := NewClient("not a url", fakeWorkspace, secret, ClientOptions{}); err == nil || strings.Contains(err.Error(), "secretvalue") {
		t.Errorf("bad url error = %v", err)
	}
}

func TestDownloadLimit(t *testing.T) {
	const path = "projects/p1/work-items/i1/attachments/a1/"
	f := newFakePlane(t)
	f.setFile(path, fakeFile{Name: "big.bin", Body: strings.Repeat("x", 2<<20)})
	c := newTestClient(t, f, newFakeClock())

	d, err := c.Download(context.Background(), "p1", "i1", "a1", 1<<20)
	if err == nil {
		_, err = io.ReadAll(d.Body)
		_ = d.Body.Close()
	}
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("error = %v, want ErrTooLarge", err)
	}

	d, err = c.Download(context.Background(), "p1", "i1", "a1", 4<<20)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(d.Body)
	_ = d.Body.Close()
	if err != nil || len(body) != 2<<20 {
		t.Fatalf("read %d bytes, err = %v", len(body), err)
	}
}

func TestDownloadLimitAnnouncedSize(t *testing.T) {
	const path = "projects/p1/work-items/i1/attachments/a1/"
	f := newFakePlane(t)
	f.setFile(path, fakeFile{Name: "big.bin", Body: strings.Repeat("x", 2<<20), Length: true})
	c := newTestClient(t, f, newFakeClock())

	d, err := c.Download(context.Background(), "p1", "i1", "a1", 1<<20)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Download error = %v, want ErrTooLarge", err)
	}
	if d.Body != nil {
		t.Fatal("Download returned a body for a file over the limit")
	}
	if n := c.Requests(); n != 1 {
		t.Fatalf("requests = %d, want 1 (no retry)", n)
	}
}

func TestDownloadLimitWithoutLength(t *testing.T) {
	// A body of unknown length (chunked) is cut while reading.
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Transfer-Encoding", "chunked")
		for range 4 {
			_, _ = io.WriteString(w, strings.Repeat("x", 512<<10))
			w.(http.Flusher).Flush()
		}
	}))
	defer other.Close()
	f := newFakePlane(t)
	f.setFile("projects/p1/work-items/i1/attachments/a1/", fakeFile{Name: "x"})
	f.redirectAttachment(other.URL)
	c := newTestClient(t, f, newFakeClock())

	d, err := c.Download(context.Background(), "p1", "i1", "a1", 1<<20)
	if err == nil {
		_, err = io.ReadAll(d.Body)
		_ = d.Body.Close()
	}
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("error = %v, want ErrTooLarge", err)
	}
}

func TestRedirectLimit(t *testing.T) {
	var srv *httptest.Server
	var hits atomic.Int64
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, srv.URL+r.URL.Path+"x", http.StatusFound)
	}))
	defer srv.Close()
	clock := newFakeClock()
	c, err := NewClient(srv.URL, fakeWorkspace, fakeKey, ClientOptions{Sleep: clock.sleep, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Projects(context.Background())
	if err == nil || !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("error = %v, want too many redirects", err)
	}
	if c.Requests() != 1 || len(clock.recorded()) != 0 { // a redirect loop is not retried
		t.Fatalf("requests = %d, sleeps = %v", c.Requests(), clock.recorded())
	}
	if hits.Load() != 6 { // the first request and five redirects
		t.Fatalf("server hits = %d, want 6", hits.Load())
	}
}

func TestEndpointsDecode(t *testing.T) {
	f := newFakePlane(t)
	f.setPlain("members/", []any{
		map[string]any{"id": "u1", "email": "ann@example.invalid", "display_name": "ann", "first_name": "Ann", "last_name": "A"},
	})
	f.setPaged("projects/", map[string]any{
		"id": "p1", "identifier": "ALP", "name": "Alpha", "workspace": "w1",
		"created_at": "2026-01-02T03:04:05Z", "archived_at": nil, "created_by": "u1",
	})
	f.setPlain("projects/p1/members/", []any{map[string]any{"id": "u1"}, map[string]any{"id": "u2"}})
	f.setPaged("projects/p1/states/", map[string]any{"id": "s1", "name": "Todo", "color": "#fff", "group": "unstarted", "sequence": 15000.5, "default": true})
	f.setPaged("projects/p1/labels/", map[string]any{"id": "l1", "name": "bug", "color": "#ff0000", "parent": "l0", "sort_order": 2.5})
	f.setPaged("projects/p1/modules/",
		map[string]any{"id": "m1", "name": "One", "description_html": "<p>x</p>", "status": "in-progress", "start_date": "2026-02-01", "lead": nil},
		map[string]any{"id": "m2", "name": "Two", "description_html": nil},
		map[string]any{"id": "m3", "name": "Three", "description_html": map[string]any{"a": 1}},
	)
	f.setPaged("projects/p1/modules/m1/module-issues/",
		map[string]any{"id": "mi1", "issue": "w1"},
		map[string]any{"id": "w2"},
	)
	f.setPaged("projects/p1/work-items/i1/comments/", map[string]any{
		"id": "c1", "comment_html": "<p>hi</p>", "actor": "u1", "parent": nil, "edited_at": "2026-03-01T00:00:00Z",
	})
	f.setPaged("projects/p1/work-items/i1/attachments/", map[string]any{
		"id": "a1", "attributes": map[string]any{"name": "shot.png", "type": "image/png", "size": 12}, "created_by": "u1",
	})
	f.setPaged("projects/p1/work-items/i1/links/", map[string]any{"title": "Docs", "url": "https://example.invalid/d"}, map[string]any{"title": nil, "url": "https://example.invalid/e"})
	f.setPlain("projects/p1/work-items/i1/relations/", map[string]any{
		"blocking": []any{map[string]any{"project_id": "p2", "issue_id": "w9"}}, "blocked_by": []any{},
	})
	clock := newFakeClock()
	c, err := NewClient(f.URL()+"/", fakeWorkspace, f.key, ClientOptions{Sleep: clock.sleep, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	members, err := c.Members(ctx)
	if err != nil || len(members) != 1 || members[0].Email != "ann@example.invalid" || members[0].FirstName != "Ann" {
		t.Fatalf("members = %+v, %v", members, err)
	}
	projects, err := c.Projects(ctx)
	if err != nil || len(projects) != 1 || projects[0].Identifier != "ALP" || projects[0].Workspace != "w1" ||
		projects[0].ArchivedAt != nil || projects[0].CreatedAt.Year() != 2026 {
		t.Fatalf("projects = %+v, %v", projects, err)
	}
	ids, err := c.ProjectMembers(ctx, "p1")
	if err != nil || !slices.Equal(ids, []string{"u1", "u2"}) {
		t.Fatalf("project members = %v, %v", ids, err)
	}
	states, err := c.States(ctx, "p1")
	if err != nil || len(states) != 1 || !states[0].Default || states[0].Sequence != 15000.5 {
		t.Fatalf("states = %+v, %v", states, err)
	}
	labels, err := c.Labels(ctx, "p1")
	if err != nil || len(labels) != 1 || labels[0].Parent == nil || *labels[0].Parent != "l0" || labels[0].SortOrder != 2.5 {
		t.Fatalf("labels = %+v, %v", labels, err)
	}
	modules, err := c.Modules(ctx, "p1")
	if err != nil || len(modules) != 3 {
		t.Fatalf("modules = %+v, %v", modules, err)
	}
	if modules[0].DescriptionHTML != "<p>x</p>" || modules[0].Status != "in-progress" || modules[0].StartDate == nil || *modules[0].StartDate != "2026-02-01" || modules[0].Lead != nil {
		t.Errorf("module 0 = %+v", modules[0])
	}
	if modules[1].DescriptionHTML != "" || modules[2].DescriptionHTML != "" || modules[2].Name != "Three" {
		t.Errorf("modules 1, 2 = %+v %+v", modules[1], modules[2])
	}
	issueIDs, err := c.ModuleIssues(ctx, "p1", "m1")
	if err != nil || !slices.Equal(issueIDs, []string{"w1", "w2"}) {
		t.Fatalf("module issues = %v, %v", issueIDs, err)
	}
	comments, err := c.Comments(ctx, "p1", "i1")
	if err != nil || len(comments) != 1 || comments[0].CommentHTML != "<p>hi</p>" || comments[0].Parent != nil || comments[0].EditedAt == nil {
		t.Fatalf("comments = %+v, %v", comments, err)
	}
	atts, err := c.Attachments(ctx, "p1", "i1")
	if err != nil || len(atts) != 1 || atts[0].Name != "shot.png" || atts[0].Size != 12 || atts[0].CreatedBy != "u1" {
		t.Fatalf("attachments = %+v, %v", atts, err)
	}
	links, err := c.Links(ctx, "p1", "i1")
	if err != nil || len(links) != 2 || links[0].Title != "Docs" || links[1].Title != "" || links[1].URL != "https://example.invalid/e" {
		t.Fatalf("links = %+v, %v", links, err)
	}
	rel, err := c.Relations(ctx, "p1", "i1")
	if err != nil || len(rel["blocking"]) != 1 || rel["blocking"][0] != (RelationRef{ProjectID: "p2", IssueID: "w9"}) || len(rel["blocked_by"]) != 0 {
		t.Fatalf("relations = %+v, %v", rel, err)
	}
}

func TestNewClientValidation(t *testing.T) {
	for name, args := range map[string][3]string{
		"empty url":       {"", "ws", "k"},
		"no scheme":       {"plane.example.invalid", "ws", "k"},
		"ftp":             {"ftp://plane.example.invalid", "ws", "k"},
		"empty workspace": {"https://plane.example.invalid", "", "k"},
		"empty key":       {"https://plane.example.invalid", "ws", ""},
	} {
		if _, err := NewClient(args[0], args[1], args[2], ClientOptions{}); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, err := NewClient("https://plane.example.invalid/", "ws", "k", ClientOptions{}); err != nil {
		t.Errorf("valid args: %v", err)
	}
}

func TestFakeLoadFixture(t *testing.T) {
	dir := t.TempDir()
	doc := `{"paged": {"projects/": [{"id": "p1", "identifier": "ALP"}]},
		"plain": {"members/": [{"id": "u1", "email": "ann@example.invalid"}]},
		"files": {"projects/p1/work-items/i1/attachments/a1/": {"name": "a.txt", "body": "abc"}}}`
	if err := os.WriteFile(filepath.Join(dir, "all.json"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	f := loadFixture(t, dir)
	c := newTestClient(t, f, newFakeClock())
	ctx := context.Background()

	if p, err := c.Projects(ctx); err != nil || len(p) != 1 || p[0].Identifier != "ALP" {
		t.Fatalf("projects = %+v, %v", p, err)
	}
	if m, err := c.Members(ctx); err != nil || len(m) != 1 {
		t.Fatalf("members = %+v, %v", m, err)
	}
	d, err := c.Download(ctx, "p1", "i1", "a1", 10)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(d.Body)
	_ = d.Body.Close()
	if string(body) != "abc" || d.Name != "a.txt" {
		t.Fatalf("download = %q %q", d.Name, body)
	}
}

func TestResponseBodyLimit(t *testing.T) {
	old := maxBody
	maxBody = 1 << 10
	t.Cleanup(func() { maxBody = old })
	f := newFakePlane(t)
	items := make([]any, 50)
	for i := range items {
		items[i] = map[string]any{"id": fmt.Sprintf("w%03d", i), "name": strings.Repeat("n", 40)}
	}
	f.setPaged("projects/p1/work-items/", items...)
	f.setPlain("members/", []any{map[string]any{"id": "u1"}})
	clock := newFakeClock()
	c := newTestClient(t, f, clock)

	_, err := c.WorkItems(context.Background(), "p1")
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("error = %v, want ErrTooLarge", err)
	}
	if n := f.Requests("projects/p1/work-items/"); n != 1 {
		t.Fatalf("requests = %d, want 1", n)
	}
	assertSleeps(t, clock)
	// An answer under the limit still works.
	if m, err := c.Members(context.Background()); err != nil || len(m) != 1 {
		t.Fatalf("members = %+v, %v", m, err)
	}
}

func TestDefaultClientRefusesLinkLocal(t *testing.T) {
	clock := newFakeClock()
	c, err := NewClient("http://169.254.169.254", fakeWorkspace, fakeKey, ClientOptions{Sleep: clock.sleep, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Projects(context.Background())
	if !errors.Is(err, netguard.ErrBlocked) {
		t.Fatalf("error = %v, want netguard.ErrBlocked", err)
	}
	if c.Requests() != 1 {
		t.Fatalf("requests = %d, want 1 (no retry)", c.Requests())
	}
	assertSleeps(t, clock)
}

func TestRedirectSchemeRefused(t *testing.T) {
	for _, target := range []string{"file:///etc/passwd", "ftp://example.invalid/x"} {
		t.Run(target, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target, http.StatusFound)
			}))
			defer srv.Close()
			clock := newFakeClock()
			c, err := NewClient(srv.URL, fakeWorkspace, fakeKey, ClientOptions{Sleep: clock.sleep, Now: clock.Now})
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Projects(context.Background())
			if err == nil || !strings.Contains(err.Error(), "scheme") {
				t.Fatalf("error = %v, want a refused scheme", err)
			}
			if c.Requests() != 1 {
				t.Fatalf("requests = %d, want 1 (no retry)", c.Requests())
			}
			assertSleeps(t, clock)
		})
	}
}

func TestDefaultClientGuardsRedirectTargets(t *testing.T) {
	var hits atomic.Int64
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
		switch {
		case strings.HasPrefix(r.URL.Path, "/uploads/"):
			writeJSON(w, []any{})
		case strings.HasSuffix(r.URL.Path, "/members/"):
			http.Redirect(w, r, "http://127.0.0.1:"+port+"/uploads/f", http.StatusFound)
		default:
			// Another name for the same machine: not the --url host.
			http.Redirect(w, r, "http://localhost:"+port+"/uploads/f", http.StatusFound)
		}
	}))
	defer srv.Close()
	clock := newFakeClock()
	c, err := NewClient(srv.URL, fakeWorkspace, fakeKey, ClientOptions{Sleep: clock.sleep, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}

	// The same host (even on a private address) is allowed.
	if _, err := c.Members(context.Background()); err != nil {
		t.Fatalf("same-host redirect: %v", err)
	}
	hits.Store(0)

	// Another host name that resolves to loopback is refused, once, never reached.
	_, err = c.Projects(context.Background())
	if !errors.Is(err, netguard.ErrBlocked) {
		t.Fatalf("error = %v, want netguard.ErrBlocked", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("server hits = %d, want 1 (the redirect target is not dialed)", hits.Load())
	}
	assertSleeps(t, clock)
}

func TestDefaultTransportIgnoresEnvProxy(t *testing.T) {
	if p := guardedTransport("plane.example.invalid").Proxy; p != nil {
		t.Fatal("default transport has a Proxy func, want nil (no proxy from the environment)")
	}
}
