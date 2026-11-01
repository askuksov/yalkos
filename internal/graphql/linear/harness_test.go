package linear_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/db"
	"github.com/askuksov/yalkos/internal/authn"
	"github.com/askuksov/yalkos/internal/blob"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/attachment"
	"github.com/askuksov/yalkos/internal/domain/comment"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/graphql/linear"
	"github.com/askuksov/yalkos/internal/store"
	"github.com/askuksov/yalkos/internal/testdb"
	"github.com/askuksov/yalkos/internal/testfx"
)

var quiet = slog.New(slog.NewJSONHandler(io.Discard, nil))

const base = "http://yalkos.test"

// env is a /graphql server on a fresh database with a set-up workspace
// "Acme" whose owner is Olga.
type env struct {
	pool  *pgxpool.Pool
	deps  linear.Deps
	srv   *httptest.Server
	owner account.User
	// ownerKey is an API key of the owner.
	ownerKey string
}

var userSeq atomic.Int64

func newEnv(t *testing.T) *env {
	t.Helper()
	_, pool := testdb.New(t)
	return newEnvOn(t, pool)
}

func newEnvOn(t *testing.T, pool *pgxpool.Pool) *env {
	t.Helper()
	ctx := context.Background()
	if err := store.Migrate(ctx, pool, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	baseURL, _ := url.Parse(base)
	accounts := account.New(pool, time.Now, base)
	if err := accounts.RefreshSetupCode(ctx); err != nil {
		t.Fatal(err)
	}
	owner, _, err := accounts.Setup(ctx, account.SetupInput{
		Code: accounts.SetupCode(), WorkspaceName: "Acme", Name: "Olga", Email: "owner@example.com", Password: "owner-password",
	}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	clock := testfx.Clock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	projects := project.New(pool, clock)
	issues := issue.New(pool, projects, clock)
	files, err := blob.NewLocalFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	attachments := attachment.New(pool, issues, files, &blob.Locks{}, 1<<20, clock)
	issues.SetLinker(attachments)
	d := linear.Deps{
		Accounts: accounts, Projects: projects, Issues: issues,
		Milestones: milestone.New(pool, projects, clock), Labels: label.New(pool, clock),
		Comments: comment.New(pool, issues, attachments, clock), Attachments: attachments,
		BaseURL: baseURL, Logger: quiet, Now: clock,
	}
	srv := httptest.NewServer(authn.Middleware(accounts, baseURL, quiet)(linear.NewHandler(d)))
	t.Cleanup(srv.Close)
	e := &env{pool: pool, deps: d, srv: srv, owner: owner}
	e.ownerKey = e.key(t, owner)
	return e
}

// key creates an API key for u.
func (e *env) key(t *testing.T, u account.User) string {
	t.Helper()
	_, key, err := e.deps.Accounts.CreateAPIKey(context.Background(), u, "test")
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// user adds a workspace user through an invite.
func (e *env) user(t *testing.T, name, email string, role account.Role) account.User {
	t.Helper()
	ctx := context.Background()
	inv, err := e.deps.Accounts.CreateInvite(ctx, e.owner, role, 7)
	if err != nil {
		t.Fatal(err)
	}
	if email == "" {
		email = fmt.Sprintf("u%d@example.com", userSeq.Add(1))
	}
	u, _, err := e.deps.Accounts.AcceptInvite(ctx, inv.URL[strings.LastIndex(inv.URL, "/")+1:], name, email, "user-password")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func (e *env) project(t *testing.T, key, name string) project.Project {
	t.Helper()
	p, err := e.deps.Projects.Create(context.Background(), e.owner, project.CreateInput{Key: key, Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (e *env) issue(t *testing.T, p project.Project, title string, mod ...func(*issue.CreateInput)) issue.Issue {
	t.Helper()
	in := issue.CreateInput{ProjectID: &p.ID, Title: title}
	for _, m := range mod {
		m(&in)
	}
	i, err := e.deps.Issues.Create(context.Background(), e.owner, in)
	if err != nil {
		t.Fatalf("create %q: %v", title, err)
	}
	return i
}

type gqlErr struct {
	Message    string         `json:"message"`
	Path       []any          `json:"path"`
	Extensions map[string]any `json:"extensions"`
}

type result struct {
	Status int
	Header http.Header
	Data   json.RawMessage
	Errors []gqlErr
	Raw    string
}

// gql posts a query with the given API key ("" = no credentials).
func (e *env) gql(t *testing.T, key, query string, vars map[string]any) result {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Data   json.RawMessage `json:"data"`
		Errors []gqlErr        `json:"errors"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return result{Status: resp.StatusCode, Header: resp.Header, Data: out.Data, Errors: out.Errors, Raw: string(raw)}
}

// ok fails on any error and decodes data into v.
func (r result) ok(t *testing.T, v any) {
	t.Helper()
	if len(r.Errors) > 0 {
		t.Fatalf("errors: %s", r.Raw)
	}
	if v != nil {
		if err := json.Unmarshal(r.Data, v); err != nil {
			t.Fatalf("data %s: %v", r.Data, err)
		}
	}
}

// fails expects exactly one error and returns it.
func (r result) fails(t *testing.T) gqlErr {
	t.Helper()
	if len(r.Errors) != 1 {
		t.Fatalf("want one error, got %s", r.Raw)
	}
	return r.Errors[0]
}

// wantErr checks code, type, message (and field when not "").
func (g gqlErr) want(t *testing.T, code, typ, msg, field string) {
	t.Helper()
	if g.Extensions["code"] != code || g.Extensions["type"] != typ || g.Message != msg {
		t.Fatalf("error = %q %v, want %q code %s type %q", g.Message, g.Extensions, msg, code, typ)
	}
	if field != "" && g.Extensions["field"] != field {
		t.Fatalf("field = %v, want %s", g.Extensions["field"], field)
	}
	if g.Extensions["userError"] == true && g.Extensions["userPresentableMessage"] != msg {
		t.Fatalf("userPresentableMessage = %v, want %q", g.Extensions["userPresentableMessage"], msg)
	}
}
