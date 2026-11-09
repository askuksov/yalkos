package mcpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// logRecords parses a JSON log into records, skipping non-JSON lines.
func logRecords(t *testing.T, log string) []map[string]any {
	t.Helper()
	var recs []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(log), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err == nil {
			recs = append(recs, m)
		}
	}
	return recs
}

func TestHandlerQuietsSDKInfoLogs(t *testing.T) {
	var buf syncBuffer
	e := newEnvLogger(t, slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	e.project(t, "YLK", "App")
	e.ok(t, "list_states", map[string]any{"project": "YLK"})
	recs := logRecords(t, buf.String())

	t.Run("no SDK info records", func(t *testing.T) {
		sdk := []string{"server connecting", "server session connected", "session initialized", "server session ended", "server session disconnected", "server run start"}
		for _, r := range recs {
			for _, m := range sdk {
				if r["msg"] == m {
					t.Errorf("SDK info record %q was logged", m)
				}
			}
			if r["msg"] != "mcp tool" && r["level"] == "INFO" {
				t.Errorf("unexpected info record %v", r)
			}
		}
	})

	t.Run("tool record kept at info", func(t *testing.T) {
		n := 0
		for _, r := range recs {
			if r["msg"] == "mcp tool" && r["level"] == "INFO" && r["tool"] == "list_states" {
				n++
			}
		}
		if n != 1 {
			t.Errorf("mcp tool info records = %d, want 1 (log %s)", n, buf.String())
		}
	})
}

func TestMinLevelHandler(t *testing.T) {
	ctx := context.Background()
	newLogger := func(buf *syncBuffer) *slog.Logger {
		return sdkLogger(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}

	t.Run("drops debug and info", func(t *testing.T) {
		var buf syncBuffer
		l := newLogger(&buf)
		l.Debug("d")
		l.Info("i")
		if buf.String() != "" {
			t.Errorf("log = %q, want empty", buf.String())
		}
		for _, lv := range []slog.Level{slog.LevelDebug, slog.LevelInfo} {
			if l.Handler().Enabled(ctx, lv) {
				t.Errorf("level %v enabled", lv)
			}
		}
	})

	t.Run("passes warn and error", func(t *testing.T) {
		var buf syncBuffer
		l := newLogger(&buf)
		l.Warn("w", "k", "v")
		l.Error("e")
		recs := logRecords(t, buf.String())
		if len(recs) != 2 || recs[0]["msg"] != "w" || recs[0]["level"] != "WARN" || recs[0]["k"] != "v" || recs[1]["msg"] != "e" || recs[1]["level"] != "ERROR" {
			t.Errorf("records = %v", recs)
		}
	})

	t.Run("keeps attrs and groups", func(t *testing.T) {
		var buf syncBuffer
		l := newLogger(&buf).With("a", 1).WithGroup("g")
		l.Info("dropped")
		l.Warn("kept", "b", 2)
		recs := logRecords(t, buf.String())
		if len(recs) != 1 {
			t.Fatalf("records = %v", recs)
		}
		g, _ := recs[0]["g"].(map[string]any)
		if recs[0]["a"] != float64(1) || g["b"] != float64(2) {
			t.Errorf("record = %v", recs[0])
		}
	})

	t.Run("nil logger stays nil", func(t *testing.T) {
		if sdkLogger(nil) != nil {
			t.Error("want nil")
		}
	})
}
