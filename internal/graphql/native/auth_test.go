package native_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/db"
	"github.com/askuksov/yalkos/internal/authn"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/graphql/native"
	"github.com/askuksov/yalkos/internal/store"
	"github.com/askuksov/yalkos/internal/testdb"
)

var quiet = slog.New(slog.NewJSONHandler(io.Discard, nil))

type server struct {
	url  string
	svc  *account.Service
	pool *pgxpool.Pool
}

func newServer(t *testing.T) server {
	t.Helper()
	ctx := context.Background()
	_, pool := testdb.New(t)
	if err := store.Migrate(ctx, pool, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	base, _ := url.Parse("http://yalkos.test")
	svc := account.New(pool, time.Now, base.String())
	if err := svc.RefreshSetupCode(ctx); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(authn.Middleware(svc, base, quiet)(native.NewHandler(svc, quiet)))
	t.Cleanup(srv.Close)
	return server{url: srv.URL, svc: svc, pool: pool}
}

type gqlErr struct {
	Message    string         `json:"message"`
	Extensions map[string]any `json:"extensions"`
}

type result struct {
	Data   map[string]json.RawMessage `json:"data"`
	Errors []gqlErr                   `json:"errors"`
	raw    string
}

// client is a browser-like client (cookie jar) or an API client (fixed Authorization).
type client struct {
	t    *testing.T
	s    server
	http *http.Client
	auth string
}

func (s server) browser(t *testing.T) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, s: s, http: &http.Client{Jar: jar}}
}

func (s server) apiClient(t *testing.T, key string) *client {
	return &client{t: t, s: s, http: &http.Client{}, auth: key}
}

func (c *client) do(query string, vars map[string]any) result {
	c.t.Helper()
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, _ := http.NewRequest("POST", c.s.url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if c.auth != "" {
		req.Header.Set("Authorization", c.auth)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var r result
	if err := json.Unmarshal(raw, &r); err != nil {
		c.t.Fatalf("decode %s: %v", raw, err)
	}
	r.raw = string(raw)
	return r
}

func (r result) code() string {
	if len(r.Errors) == 0 {
		return ""
	}
	s, _ := r.Errors[0].Extensions["code"].(string)
	return s
}

func (r result) field() string {
	if len(r.Errors) == 0 {
		return ""
	}
	s, _ := r.Errors[0].Extensions["field"].(string)
	return s
}

func (r result) str(t *testing.T, key, path string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Data[key], &m); err != nil {
		t.Fatalf("data.%s: %v (%s)", key, err, r.raw)
	}
	var cur any = m
	for _, p := range strings.Split(path, ".") {
		cur = cur.(map[string]any)[p]
	}
	s, _ := cur.(string)
	return s
}

const setupMutation = `mutation($in: SetupInput!) { setup(input: $in) { id role } }`

func setupVars(code string) map[string]any {
	return map[string]any{"in": map[string]any{
		"code": code, "workspaceName": "Acme", "name": "Olga", "email": "owner@example.com", "password": "owner-password",
	}}
}

func ownerBrowser(t *testing.T, s server) *client {
	t.Helper()
	b := s.browser(t)
	if r := b.do(setupMutation, setupVars(s.svc.SetupCode())); len(r.Errors) > 0 {
		t.Fatalf("setup: %s", r.raw)
	}
	return b
}

func TestSetupLoginViewerFlow(t *testing.T) {
	s := newServer(t)
	b := s.browser(t)

	if r := b.do(`{ setupStatus { needed } }`, nil); !strings.Contains(r.raw, `"needed":true`) {
		t.Fatalf("setupStatus: %s", r.raw)
	}
	if r := b.do(setupMutation, setupVars(s.svc.SetupCode())); r.str(t, "setup", "role") != "OWNER" {
		t.Fatalf("setup: %s", r.raw)
	}
	if r := b.do(`{ viewer { role email } }`, nil); r.str(t, "viewer", "role") != "OWNER" {
		t.Fatalf("viewer after setup: %s", r.raw)
	}
	if r := b.do(`mutation { logout }`, nil); len(r.Errors) > 0 {
		t.Fatalf("logout: %s", r.raw)
	}
	if r := b.do(`{ viewer { id } }`, nil); r.code() != "AUTHENTICATION_ERROR" {
		t.Fatalf("viewer after logout: %s", r.raw)
	}
	r := b.do(`mutation($e: String!, $p: String!) { login(email: $e, password: $p) { name } }`,
		map[string]any{"e": "owner@example.com", "p": "owner-password"})
	if r.str(t, "login", "name") != "Olga" {
		t.Fatalf("login: %s", r.raw)
	}
	if r := b.do(`{ viewer { name } }`, nil); r.str(t, "viewer", "name") != "Olga" {
		t.Fatalf("viewer after login: %s", r.raw)
	}
}

func TestSetupWrongCode(t *testing.T) {
	s := newServer(t)
	r := s.browser(t).do(setupMutation, setupVars("WRONGCOD"))
	if r.code() != "INVALID_INPUT" || r.field() != "code" {
		t.Fatalf("got %s", r.raw)
	}
}

func TestInviteAcceptFlow(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	r := owner.do(`mutation { createInvite(role: MEMBER) { url invite { role } } }`, nil)
	inviteURL := r.str(t, "createInvite", "url")
	token := inviteURL[strings.LastIndex(inviteURL, "/")+1:]
	if !strings.HasPrefix(inviteURL, "http://yalkos.test/invite/") || token == "" {
		t.Fatalf("createInvite: %s", r.raw)
	}

	m := s.browser(t)
	r = m.do(`mutation($in: AcceptInviteInput!) { acceptInvite(input: $in) { role } }`, map[string]any{"in": map[string]any{
		"token": token, "name": "Mia", "email": "mia@example.com", "password": "mia-password",
	}})
	if r.str(t, "acceptInvite", "role") != "MEMBER" {
		t.Fatalf("acceptInvite: %s", r.raw)
	}
	if r := m.do(`{ viewer { role } }`, nil); r.str(t, "viewer", "role") != "MEMBER" {
		t.Fatalf("viewer: %s", r.raw)
	}
	if r := m.do(`mutation { createInvite(role: MEMBER) { url } }`, nil); r.code() != "FORBIDDEN" {
		t.Errorf("member createInvite: %s", r.raw)
	}
	if r := owner.do(`{ members { email role } }`, nil); !strings.Contains(r.raw, "mia@example.com") {
		t.Errorf("members: %s", r.raw)
	}
}

func TestApiKeyFlow(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	r := owner.do(`mutation { createApiKey(name: "CI") { key apiKey { id displayPrefix } } }`, nil)
	key := r.str(t, "createApiKey", "key")
	id := r.str(t, "createApiKey", "apiKey.id")
	if !strings.HasPrefix(key, "yal_api_") {
		t.Fatalf("createApiKey: %s", r.raw)
	}
	api := s.apiClient(t, key)
	if r := api.do(`{ viewer { role } }`, nil); r.str(t, "viewer", "role") != "OWNER" {
		t.Fatalf("viewer with key: %s", r.raw)
	}
	if r := owner.do(`mutation($id: ID!) { revokeApiKey(id: $id) }`, map[string]any{"id": id}); len(r.Errors) > 0 {
		t.Fatalf("revoke: %s", r.raw)
	}
	if r := api.do(`{ viewer { role } }`, nil); r.code() != "AUTHENTICATION_ERROR" {
		t.Fatalf("viewer with revoked key: %s", r.raw)
	}
}

func TestValidationError(t *testing.T) {
	s := newServer(t)
	ownerBrowser(t, s)
	r := s.browser(t).do(`mutation { login(email: "", password: "x") { id } }`, nil)
	if r.code() != "INVALID_INPUT" || r.field() != "email" {
		t.Fatalf("got %s", r.raw)
	}
	r = s.browser(t).do(`mutation { login(email: "owner@example.com", password: "nope-nope") { id } }`, nil)
	if r.code() != "AUTHENTICATION_ERROR" || r.Errors[0].Message != "Invalid email or password" {
		t.Fatalf("wrong password: %s", r.raw)
	}
}

func TestInternalErrorHidden(t *testing.T) {
	s := newServer(t)
	b := ownerBrowser(t, s)
	s.pool.Close()
	r := b.do(`mutation { login(email: "owner@example.com", password: "owner-password") { id } }`, nil)
	if r.code() != "INTERNAL_ERROR" || r.Errors[0].Message != "Internal error" {
		t.Fatalf("got %s", r.raw)
	}
	if strings.Contains(strings.ToLower(r.raw), "pool") || strings.Contains(strings.ToLower(r.raw), "sql") {
		t.Errorf("internal details leaked: %s", r.raw)
	}
}
