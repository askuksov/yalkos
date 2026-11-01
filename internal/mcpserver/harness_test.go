package mcpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/askuksov/yalkos/internal/authn"
	"github.com/askuksov/yalkos/internal/blob"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/attachment"
	"github.com/askuksov/yalkos/internal/domain/comment"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/testfx"
)

var quiet = slog.New(slog.NewJSONHandler(io.Discard, nil))

const testBase = "http://yalkos.test"

type env struct {
	url   string
	base  *url.URL
	pool  *pgxpool.Pool
	users testfx.Users
	deps  Deps
}

// newEnv serves Guard + NewHandler over a fresh test database.
func newEnv(t *testing.T) *env { return newEnvLogger(t, quiet) }

// newEnvLogger is newEnv with the given server logger.
func newEnvLogger(t *testing.T, logger *slog.Logger) *env {
	t.Helper()
	pool, users, _ := testfx.New(t)
	base, _ := url.Parse(testBase)
	accounts := account.New(pool, time.Now, base.String())
	projects := project.New(pool, time.Now)
	issues := issue.New(pool, projects, time.Now)
	files, err := blob.NewLocalFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	attachments := attachment.New(pool, issues, files, &blob.Locks{}, 25<<20, time.Now)
	issues.SetLinker(attachments)
	deps := Deps{
		Accounts: accounts, Projects: projects, Issues: issues,
		Milestones: milestone.New(pool, projects, time.Now), Labels: label.New(pool, time.Now),
		Comments: comment.New(pool, issues, attachments, time.Now), Attachments: attachments,
		BaseURL: base, Logger: logger,
	}
	srv := httptest.NewServer(Guard(base, authn.Middleware(accounts, base, quiet), NewHandler(deps)))
	t.Cleanup(srv.Close)
	return &env{url: srv.URL, base: base, pool: pool, users: users, deps: deps}
}

// key creates an API key of u.
func (e *env) key(t *testing.T, u account.User) string {
	t.Helper()
	_, plain, err := e.deps.Accounts.CreateAPIKey(context.Background(), u, "test")
	if err != nil {
		t.Fatal(err)
	}
	return plain
}

type bearer struct{ key string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.key)
	return http.DefaultTransport.RoundTrip(r)
}

// client connects an SDK client that sends key.
func (e *env) client(t *testing.T, key string) *mcp.ClientSession {
	t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	tr := &mcp.StreamableClientTransport{
		Endpoint: e.url, HTTPClient: &http.Client{Transport: bearer{key}},
		MaxRetries: -1, DisableStandaloneSSE: true,
	}
	s, err := c.Connect(context.Background(), tr, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// raw sends one HTTP request to the endpoint. JSON content type and the
// streamable Accept header are set unless header overrides them; a "Host"
// entry sets the request host.
func (e *env) raw(t *testing.T, method string, header http.Header, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, e.url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range header {
		if k == "Host" {
			req.Host = v[0]
			continue
		}
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// session returns a session token of the Owner.
func (e *env) session(t *testing.T) string {
	t.Helper()
	_, token, err := e.deps.Accounts.Login(context.Background(), "owner@example.com", "owner-password", "203.0.113.7")
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
