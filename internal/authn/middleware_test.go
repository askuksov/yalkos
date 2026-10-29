package authn

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	cases := map[string]bool{"https://evil.example": false, "http://localhost:8080": true, "": true}
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
