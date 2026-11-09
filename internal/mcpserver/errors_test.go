package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/askuksov/yalkos/internal/authn"
	"github.com/askuksov/yalkos/internal/domain/kit"
)

func TestUserText(t *testing.T) {
	var buf syncBuffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	cases := []struct {
		err  error
		want string
	}{
		{kit.Input("assigneeId", kit.ReasonNotAllowed, "user is disabled"), "Invalid assignee: user is disabled."},
		{kit.Input("after", kit.ReasonInvalidFormat, "invalid cursor"), "Invalid cursor: invalid cursor."},
		{kit.Input("first", kit.ReasonOutOfRange, "must be 1–250"), "Invalid limit: must be 1–250."},
		{kit.Input("title", kit.ReasonTooLong, "too long"), "Invalid title: too long."},
		{fmt.Errorf("wrap: %w", kit.Input("labelIds", kit.ReasonTooMany, "at most 20 labels")), "Invalid labels: at most 20 labels."},
		{kit.NotFoundError{Entity: "Milestone"}, "Milestone not found."},
		{fmt.Errorf("x: %w", kit.ErrForbidden), "You do not have permission to do this."},
		{errf("Issue %s not found.", "YLK-9"), "Issue YLK-9 not found."},
	}
	for _, c := range cases {
		if got := userText(c.err, "test_tool", logger); got != c.want {
			t.Errorf("userText(%v) = %q, want %q", c.err, got, c.want)
		}
	}
	if buf.String() != "" {
		t.Errorf("known errors logged: %s", buf.String())
	}
	if got := userText(errors.New("boom"), "test_tool", logger); got != "Internal error; try again later." {
		t.Errorf("internal = %q", got)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "boom") || !strings.Contains(lines[0], `"tool":"test_tool"`) || !strings.Contains(lines[0], `"level":"ERROR"`) {
		t.Errorf("log = %q", buf.String())
	}
}

// TestInternalErrorLog checks the log record of an unexpected tool error that
// goes through a real MCP call.
func TestInternalErrorLog(t *testing.T) {
	const secret = "distinctive-argument-value-7f3a"
	var buf syncBuffer
	e := newEnvLogger(t, slog.New(slog.NewJSONHandler(&buf, nil)))

	srv := mcp.NewServer(&mcp.Implementation{Name: "yalkos-test", Version: "0"}, nil)
	addTool(srv, &e.deps, &mcp.Tool{Name: "explode", Description: "Always fails."},
		func(context.Context, *call, struct {
			Note string `json:"note"`
		}) (any, error) {
			return nil, errors.New("database exploded")
		})
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, DisableLocalhostProtection: true,
	})
	ts := httptest.NewServer(Guard(e.base, authn.Middleware(e.deps.Accounts, e.base, quiet), h))
	t.Cleanup(ts.Close)
	e2 := *e
	e2.url = ts.URL

	text := e2.fail(t, "explode", map[string]any{"note": secret})

	var rec map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err == nil && m["msg"] == "mcp tool failed" {
			rec = m
		}
	}

	t.Run("record exists", func(t *testing.T) {
		if rec == nil {
			t.Fatalf("no mcp tool failed record in %s", buf.String())
		}
	})
	t.Run("tool attribute", func(t *testing.T) {
		if rec["tool"] != "explode" {
			t.Errorf("tool = %v, want explode (record %v)", rec["tool"], rec)
		}
	})
	t.Run("error text", func(t *testing.T) {
		if rec["error"] != "database exploded" || rec["level"] != "ERROR" {
			t.Errorf("record = %v", rec)
		}
	})
	t.Run("no argument values", func(t *testing.T) {
		if strings.Contains(buf.String(), secret) {
			t.Errorf("argument value logged: %s", buf.String())
		}
	})
	t.Run("no api key", func(t *testing.T) {
		// e2.fail filled ownerKey; an empty key would make the check vacuous.
		if e2.ownerKey == "" {
			t.Fatal("owner key is empty")
		}
		if strings.Contains(buf.String(), e2.ownerKey) {
			t.Error("api key reached the internal error log")
		}
	})
	t.Run("user text unchanged", func(t *testing.T) {
		if text != "Internal error; try again later." {
			t.Errorf("text = %q", text)
		}
	})
}

func TestCutAndLinks(t *testing.T) {
	if s, cut := cut("abcdé", 4); s != "abcd…" || !cut {
		t.Errorf("cut = %q, %v", s, cut)
	}
	if s, cut := cut("abcdé", 5); s != "abcdé" || cut {
		t.Errorf("cut exact = %q, %v", s, cut)
	}
	if got := absLinks("![a](/api/attachments/x/a.png) [b](/api/attachments/y/b.txt)", "http://h"); got != "![a](http://h/api/attachments/x/a.png) [b](http://h/api/attachments/y/b.txt)" {
		t.Errorf("absLinks = %q", got)
	}
	at := time.Date(2026, 11, 1, 13, 5, 7, 0, time.FixedZone("EET", 2*3600))
	if got := stamp(at); got != "2026-11-01 11:05" {
		t.Errorf("stamp = %q", got)
	}
	if got := day(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)); got != "2026-12-01" {
		t.Errorf("day = %q", got)
	}
}

func TestEncodeKeepsHTML(t *testing.T) {
	got, err := encode(map[string]string{"title": "a < b & c > d"})
	if err != nil || got != `{"title":"a < b & c > d"}` {
		t.Errorf("encode = %q, %v", got, err)
	}
}
