package mcpserver

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	// ownerKey is an API key of users.Owner, created lazily by owner().
	ownerKey string
	ownerSes *mcp.ClientSession
}

// newEnv serves Guard + NewHandler over a fresh test database.
func newEnv(t *testing.T) *env { return newEnvLogger(t, quiet) }

// newEnvLogger is newEnv with the given server logger.
func newEnvLogger(t *testing.T, logger *slog.Logger) *env {
	t.Helper()
	pool, users, _ := testfx.New(t)
	return serveEnv(t, pool, users, logger)
}

// counter is a pgx tracer that counts SQL statements.
type counter struct{ n atomic.Int64 }

func (c *counter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (c *counter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// newTracedEnv is newEnv whose services use a pool that counts SQL statements.
func newTracedEnv(t *testing.T) (*env, *counter) {
	t.Helper()
	setup, users, _ := testfx.New(t)
	cfg, err := pgxpool.ParseConfig(setup.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	queries := &counter{}
	cfg.ConnConfig.Tracer = queries
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return serveEnv(t, pool, users, quiet), queries
}

func serveEnv(t *testing.T, pool *pgxpool.Pool, users testfx.Users, logger *slog.Logger) *env {
	t.Helper()
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

// call runs a tool and returns its text content and isError.
func (e *env) call(t *testing.T, s *mcp.ClientSession, tool string, args map[string]any) (string, bool) {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", tool, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

// owner returns a client session of users.Owner (one per env).
func (e *env) owner(t *testing.T) *mcp.ClientSession {
	t.Helper()
	if e.ownerSes == nil {
		e.ownerKey = e.key(t, e.users.Owner)
		e.ownerSes = e.client(t, e.ownerKey)
	}
	return e.ownerSes
}

// ok calls a tool as Owner and fails the test on a tool error.
func (e *env) ok(t *testing.T, tool string, args map[string]any) string {
	t.Helper()
	text, isErr := e.call(t, e.owner(t), tool, args)
	if isErr {
		t.Fatalf("%s %v: tool error %q", tool, args, text)
	}
	return text
}

// fail calls a tool as Owner and returns the error text; fails on success.
func (e *env) fail(t *testing.T, tool string, args map[string]any) string {
	t.Helper()
	text, isErr := e.call(t, e.owner(t), tool, args)
	if !isErr {
		t.Fatalf("%s %v: want tool error, got %s", tool, args, text)
	}
	return text
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

// user invites a user with name, email and role and accepts the invite.
func (e *env) user(t *testing.T, name, email string, role account.Role) account.User {
	t.Helper()
	ctx := context.Background()
	inv, err := e.deps.Accounts.CreateInvite(ctx, e.users.Owner, role, 7)
	if err != nil {
		t.Fatal(err)
	}
	u, _, err := e.deps.Accounts.AcceptInvite(ctx, strings.TrimPrefix(inv.URL, testBase+"/invite/"), name, email, "user-password")
	if err != nil {
		t.Fatal(err)
	}
	return u
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

// Fixtures: domain objects created directly through the services as Owner.

func (e *env) project(t *testing.T, key, name string) project.Project {
	t.Helper()
	p, err := e.deps.Projects.Create(context.Background(), e.users.Owner, project.CreateInput{Key: key, Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (e *env) stateID(t *testing.T, p project.Project, name string) uuid.UUID {
	t.Helper()
	states, err := e.deps.Projects.States(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		if s.Name == name {
			return s.ID
		}
	}
	t.Fatalf("no state %q", name)
	return uuid.Nil
}

func (e *env) label(t *testing.T, name string, parent *uuid.UUID) label.Label {
	t.Helper()
	l, err := e.deps.Labels.Create(context.Background(), e.users.Owner, label.CreateInput{Name: name, Color: "#336699", ParentID: parent})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func (e *env) group(t *testing.T, name string, exclusive bool) label.Label {
	t.Helper()
	l, err := e.deps.Labels.Create(context.Background(), e.users.Owner, label.CreateInput{Name: name, Color: "#336699", IsGroup: true, Exclusive: &exclusive})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func (e *env) milestone(t *testing.T, p project.Project, in milestone.CreateInput) milestone.Milestone {
	t.Helper()
	m, err := e.deps.Milestones.Create(context.Background(), e.users.Owner, p.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (e *env) issue(t *testing.T, in issue.CreateInput) issue.Issue {
	t.Helper()
	i, err := e.deps.Issues.Create(context.Background(), e.users.Owner, in)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

// newCall is a tool call context of actor, for resolver tests.
func (e *env) newCall(actor account.User) *call { return &call{d: &e.deps, actor: actor} }

// errText fails unless err is set and returns its user text.
func errText(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("want error, got nil")
	}
	return userText(err, quiet)
}

// syncBuffer is a log sink safe for concurrent writes and reads.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
