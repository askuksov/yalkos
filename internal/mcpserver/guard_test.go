package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/askuksov/yalkos/internal/authn"
)

const (
	wantInstructions = `Yalkos is an issue tracker. Projects have keys such as YLK; issues have identifiers KEY-N such as YLK-12 — use them in tool calls, commits and branch names. Refer to states, labels, types, milestones and people by name; "me" is the owner of the API key and "none" clears a value. Every issue has at most one type (Bug, Feature, Task, Improvement unless renamed). Times are UTC, written YYYY-MM-DD HH:mm.`
	wantUnauthorized = `{"error":"API key required: send Authorization: Bearer <key> (create one under Settings → API keys)"}`
	wantForbidden    = `{"error":"origin not allowed"}`
	initOld          = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"raw","version":"0"}}}`
	toolsList        = `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
)

func auth(key string) http.Header {
	return http.Header{"Authorization": {"Bearer " + key}}
}

func checkUnauthorized(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); got != `Bearer realm="yalkos"` {
		t.Errorf("WWW-Authenticate = %q", got)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if body := strings.TrimSpace(readBody(t, resp)); body != wantUnauthorized {
		t.Errorf("body = %s", body)
	}
}

func checkForbidden(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if body := strings.TrimSpace(readBody(t, resp)); body != wantForbidden {
		t.Errorf("body = %s", body)
	}
}

func TestInitializeWithKey(t *testing.T) {
	e := newEnv(t)
	s := e.client(t, e.key(t, e.users.Owner))
	res := s.InitializeResult()
	if res == nil {
		t.Fatal("no initialize result")
	}
	if res.Instructions != wantInstructions {
		t.Errorf("instructions = %q", res.Instructions)
	}
	if res.ServerInfo == nil || res.ServerInfo.Name != "yalkos" || res.ServerInfo.Title != "Yalkos" {
		t.Errorf("server info = %+v", res.ServerInfo)
	}
}

func TestOldProtocolInitialize(t *testing.T) {
	e := newEnv(t)
	key := e.key(t, e.users.Owner)
	resp := e.raw(t, "POST", auth(key), initOld)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize status = %d: %s", resp.StatusCode, readBody(t, resp))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.Errorf("Mcp-Session-Id = %q, want none", sid)
	}
	var r struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(readBody(t, resp)), &r); err != nil {
		t.Fatal(err)
	}
	if r.Result.ProtocolVersion != "2025-11-25" {
		t.Errorf("protocolVersion = %q", r.Result.ProtocolVersion)
	}
	h := auth(key)
	h.Set("MCP-Protocol-Version", "2025-11-25")
	resp = e.raw(t, "POST", h, toolsList)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tools/list status = %d: %s", resp.StatusCode, readBody(t, resp))
	}
	if body := readBody(t, resp); !strings.Contains(body, `"result"`) {
		t.Errorf("tools/list body = %s", body)
	}
}

func TestNoKeyIsUnauthorized(t *testing.T) {
	e := newEnv(t)
	checkUnauthorized(t, e.raw(t, "POST", nil, initOld))
}

func TestBadKeyIsUnauthorized(t *testing.T) {
	e := newEnv(t)
	checkUnauthorized(t, e.raw(t, "POST", auth("yal_api_wrong"), initOld))

	ctx := context.Background()
	k, plain, err := e.deps.Accounts.CreateAPIKey(ctx, e.users.Owner, "old")
	if err != nil {
		t.Fatal(err)
	}
	if resp := e.raw(t, "POST", auth(plain), initOld); resp.StatusCode != http.StatusOK {
		t.Fatalf("positive control: status = %d", resp.StatusCode)
	}
	if err := e.deps.Accounts.RevokeAPIKey(ctx, e.users.Owner, k.ID); err != nil {
		t.Fatal(err)
	}
	checkUnauthorized(t, e.raw(t, "POST", auth(plain), initOld))
}

func TestCookieOnlyIsUnauthorized(t *testing.T) {
	e := newEnv(t)
	h := http.Header{
		"Cookie": {authn.CookieName + "=" + e.session(t)},
		"Origin": {testBase},
	}
	checkUnauthorized(t, e.raw(t, "POST", h, initOld))
}

func TestForeignOriginForbidden(t *testing.T) {
	e := newEnv(t)
	h := auth(e.key(t, e.users.Owner))
	h.Set("Origin", "https://evil.example")
	checkForbidden(t, e.raw(t, "POST", h, initOld))

	h = http.Header{
		"Cookie": {authn.CookieName + "=" + e.session(t)},
		"Origin": {"https://evil.example"},
	}
	checkForbidden(t, e.raw(t, "POST", h, initOld))
}

func TestSameOriginAllowed(t *testing.T) {
	e := newEnv(t)
	h := auth(e.key(t, e.users.Owner))
	h.Set("Origin", testBase)
	if resp := e.raw(t, "POST", h, initOld); resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, readBody(t, resp))
	}
}

func TestGetAndDeleteNotAllowed(t *testing.T) {
	e := newEnv(t)
	key := e.key(t, e.users.Owner)
	for _, m := range []string{"GET", "DELETE"} {
		resp := e.raw(t, m, auth(key), "")
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s status = %d, want 405", m, resp.StatusCode)
		}
		if a := resp.Header.Get("Allow"); a != "POST" {
			t.Errorf("%s Allow = %q", m, a)
		}
	}
}

func TestBodyTooLarge(t *testing.T) {
	e := newEnv(t)
	prefix := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"x":"`
	suffix := `"}}`
	body := prefix + strings.Repeat("a", 1<<20+1-len(prefix)-len(suffix)) + suffix
	if len(body) != 1<<20+1 {
		t.Fatalf("body size %d", len(body))
	}
	if resp := e.raw(t, "POST", auth(e.key(t, e.users.Owner)), body); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}

func TestProxiedHostAllowed(t *testing.T) {
	e := newEnv(t)
	if !strings.HasPrefix(e.url, "http://127.0.0.1:") {
		t.Fatalf("test server not on loopback: %s", e.url)
	}
	h := auth(e.key(t, e.users.Owner))
	h.Set("Host", "app.example.com")
	if resp := e.raw(t, "POST", h, initOld); resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, readBody(t, resp))
	}
}
