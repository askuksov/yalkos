package authn

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/account"
)

type fakeAuth struct {
	keys     map[string]account.User
	sessions map[string]account.User
}

func (f fakeAuth) UserByAPIKey(_ context.Context, key string) (account.User, error) {
	if u, ok := f.keys[key]; ok {
		return u, nil
	}
	return account.User{}, account.ErrUnauthenticated
}

func (f fakeAuth) UserBySession(_ context.Context, tok string) (account.User, error) {
	if u, ok := f.sessions[tok]; ok {
		return u, nil
	}
	return account.User{}, account.ErrUnauthenticated
}

var (
	keyUser     = account.User{ID: uuid.New(), Name: "Key"}
	sessionUser = account.User{ID: uuid.New(), Name: "Session"}
	quiet       = slog.New(slog.NewJSONHandler(io.Discard, nil))
)

// probe runs req through the middleware and returns the actor seen by the handler.
func probe(t *testing.T, base string, req *http.Request) (*Actor, *httptest.ResponseRecorder) {
	t.Helper()
	u, _ := url.Parse(base)
	mw := Middleware(fakeAuth{
		keys:     map[string]account.User{"yal_api_good": keyUser},
		sessions: map[string]account.User{"sess-good": sessionUser},
	}, u, quiet)
	var seen *Actor
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen, _ = ActorFrom(r.Context())
	})).ServeHTTP(rec, req)
	return seen, rec
}

func TestActorFromAPIKeyBearerAndBare(t *testing.T) {
	for _, h := range []string{"Bearer yal_api_good", "yal_api_good"} {
		req := httptest.NewRequest("POST", "/graphql", nil)
		req.Header.Set("Authorization", h)
		a, _ := probe(t, "http://localhost:8080", req)
		if a == nil || a.User.ID != keyUser.ID || a.Via != "api_key" {
			t.Errorf("%q → actor %+v", h, a)
		}
	}
}

func TestActorFromSessionCookie(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/graphql", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "sess-good"})
	a, _ := probe(t, "http://localhost:8080", req)
	if a == nil || a.User.ID != sessionUser.ID || a.Via != "session" || a.SessionToken != "sess-good" {
		t.Errorf("actor = %+v", a)
	}
}

func TestInvalidKeyIgnoresCookie(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/graphql", nil)
	req.Header.Set("Authorization", "Bearer yal_api_bad")
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "sess-good"})
	if a, _ := probe(t, "http://localhost:8080", req); a != nil {
		t.Errorf("actor = %+v, want none", a)
	}
}

func TestCookieAuthRejectsForeignOrigin(t *testing.T) {
	cases := map[string]bool{"http://localhost:8080": true, "": true}
	for origin, want := range cases {
		req := httptest.NewRequest("POST", "/api/graphql", nil)
		req.AddCookie(&http.Cookie{Name: CookieName, Value: "sess-good"})
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		a, _ := probe(t, "http://localhost:8080", req)
		if (a != nil) != want {
			t.Errorf("Origin %q: actor = %+v, want present=%v", origin, a, want)
		}
	}
}

// A cookie request from another origin gets an explicit 403 instead of silently
// becoming anonymous (which looked like a login loop when BASE_URL was wrong).
func TestCookieForeignOriginIs403(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/graphql", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "sess-good"})
	req.Header.Set("Origin", "http://192.168.1.10:8080")
	u, _ := url.Parse("http://localhost:8080")
	called := false
	rec := httptest.NewRecorder()
	Middleware(fakeAuth{}, u, quiet)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(rec, req)
	if called || rec.Code != http.StatusForbidden {
		t.Fatalf("handler called=%v, status=%d; want not called, 403", called, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "does not match BASE_URL") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestClientIP(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/graphql", nil)
	req.RemoteAddr = "203.0.113.9:51234"
	u, _ := url.Parse("http://localhost:8080")
	var got string
	Middleware(fakeAuth{}, u, quiet)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = ClientIP(r.Context())
	})).ServeHTTP(httptest.NewRecorder(), req)
	if got != "203.0.113.9" {
		t.Errorf("ClientIP = %q", got)
	}
}

func TestClientIPBehindProxy(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("192.168.0.0/16")}
	withV6 := append([]netip.Prefix{netip.MustParsePrefix("fd00::/8")}, trusted...)
	for _, tc := range []struct {
		name    string
		remote  string
		xff     []string
		trusted []netip.Prefix
		want    string
	}{
		{"untrusted peer ignores header", "203.0.113.9:5000", []string{"6.6.6.6"}, trusted, "203.0.113.9"},
		{"trusted peer, no header", "172.18.0.1:5000", nil, trusted, "172.18.0.1"},
		{"trusted peer, one entry", "172.18.0.1:5000", []string{"198.51.100.7"}, trusted, "198.51.100.7"},
		{"forged then real", "172.18.0.1:5000", []string{"6.6.6.6, 198.51.100.7"}, trusted, "198.51.100.7"},
		{"forged inside trusted", "172.18.0.1:5000", []string{"192.168.1.200, 198.51.100.7"}, trusted, "198.51.100.7"},
		{"real inside trusted", "172.18.0.1:5000", []string{"6.6.6.6, 192.168.1.5"}, trusted, "192.168.1.5"},
		{"garbage rightmost", "172.18.0.1:5000", []string{"198.51.100.7, nope"}, trusted, "172.18.0.1"},
		{"empty header value", "172.18.0.1:5000", []string{""}, trusted, "172.18.0.1"},
		{"several lines", "172.18.0.1:5000", []string{"6.6.6.6", "198.51.100.7, 203.0.113.4"}, trusted, "203.0.113.4"},
		{"ipv6 peer and entry", "[fd00::1]:5000", []string{"2001:db8::5"}, withV6, "2001:db8::5"},
		{"no trusted list", "172.18.0.1:5000", []string{"198.51.100.7"}, nil, "172.18.0.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := clientIP(tc.remote, tc.xff, tc.trusted); got != tc.want {
				t.Errorf("clientIP(%q, %q) = %q, want %q", tc.remote, tc.xff, got, tc.want)
			}
		})
	}
}

func TestClientIPMiddlewareUsesTrusted(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/graphql", nil)
	req.RemoteAddr = "172.18.0.1:1"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	u, _ := url.Parse("http://localhost:8080")
	var got string
	Middleware(fakeAuth{}, u, quiet, netip.MustParsePrefix("172.16.0.0/12"))(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = ClientIP(r.Context())
	})).ServeHTTP(httptest.NewRecorder(), req)
	if got != "198.51.100.7" {
		t.Errorf("ClientIP = %q", got)
	}
}

func cookieFrom(t *testing.T, base string, set func(context.Context)) *http.Cookie {
	t.Helper()
	u, _ := url.Parse(base)
	rec := httptest.NewRecorder()
	Middleware(fakeAuth{}, u, quiet)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		set(r.Context())
	})).ServeHTTP(rec, httptest.NewRequest("POST", "/api/graphql", nil))
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	return cookies[0]
}

func TestSetSessionCookieAttributes(t *testing.T) {
	c := cookieFrom(t, "http://localhost:8080", func(ctx context.Context) { SetSessionCookie(ctx, "tok") })
	if c.Name != "yalkos_session" || c.Value != "tok" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode ||
		c.Path != "/" || c.MaxAge != int((30*24*time.Hour).Seconds()) || c.Secure {
		t.Errorf("http cookie = %+v", c)
	}
	if c := cookieFrom(t, "https://app.yalkos.com", func(ctx context.Context) { SetSessionCookie(ctx, "tok") }); !c.Secure {
		t.Error("https BASE_URL: cookie not Secure")
	}
}

func TestClearSessionCookie(t *testing.T) {
	c := cookieFrom(t, "http://localhost:8080", ClearSessionCookie)
	if c.Name != CookieName || c.MaxAge != -1 {
		t.Errorf("cookie = %+v, want MaxAge -1", c)
	}
}
