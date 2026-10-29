package linear_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/askuksov/yalkos/db"
	"github.com/askuksov/yalkos/internal/authn"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/graphql/linear"
	"github.com/askuksov/yalkos/internal/store"
	"github.com/askuksov/yalkos/internal/testdb"
)

var quiet = slog.New(slog.NewJSONHandler(io.Discard, nil))

type viewer struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	Active      bool   `json:"active"`
	Admin       bool   `json:"admin"`
}

type env struct {
	url   string
	svc   *account.Service
	owner account.User
}

func newEnv(t *testing.T) env {
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
	owner, _, err := svc.Setup(ctx, account.SetupInput{
		Code: svc.SetupCode(), WorkspaceName: "Acme", Name: "Olga", Email: "owner@example.com", Password: "owner-password",
	}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(authn.Middleware(svc, base, quiet)(linear.NewHandler(svc, quiet)))
	t.Cleanup(srv.Close)
	return env{url: srv.URL, svc: svc, owner: owner}
}

func queryViewer(t *testing.T, e env, authz string) (viewer, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"query": "{ viewer { id name displayName email active admin } }"})
	req, _ := http.NewRequest("POST", e.url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Data struct {
			Viewer *viewer `json:"viewer"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &out)
	if out.Data.Viewer == nil {
		return viewer{}, string(raw)
	}
	return *out.Data.Viewer, string(raw)
}

func TestLinearViewerWithAPIKey(t *testing.T) {
	e := newEnv(t)
	_, key, err := e.svc.CreateAPIKey(context.Background(), e.owner, "sdk")
	if err != nil {
		t.Fatal(err)
	}
	v, raw := queryViewer(t, e, key) // bare key, like @linear/sdk sends it
	want := viewer{ID: e.owner.ID.String(), Name: "Olga", DisplayName: "Olga", Email: "owner@example.com", Active: true, Admin: true}
	if v != want {
		t.Errorf("viewer = %+v, want %+v (%s)", v, want, raw)
	}
}

func TestLinearViewerMemberNotAdmin(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	inv, _ := e.svc.CreateInvite(ctx, e.owner, account.RoleMember, 7)
	token := inv.URL[strings.LastIndex(inv.URL, "/")+1:]
	member, _, err := e.svc.AcceptInvite(ctx, token, "Mia", "mia@example.com", "mia-password")
	if err != nil {
		t.Fatal(err)
	}
	_, key, _ := e.svc.CreateAPIKey(ctx, member, "mia")
	if v, raw := queryViewer(t, e, "Bearer "+key); v.Admin || !v.Active || v.Email != "mia@example.com" {
		t.Errorf("member viewer = %+v (%s)", v, raw)
	}
}

func TestLinearViewerNoAuth(t *testing.T) {
	e := newEnv(t)
	if _, raw := queryViewer(t, e, ""); !strings.Contains(raw, "AUTHENTICATION_ERROR") {
		t.Errorf("got %s", raw)
	}
}
