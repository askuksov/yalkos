package mcpserver

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

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
		if got := userText(c.err, logger); got != c.want {
			t.Errorf("userText(%v) = %q, want %q", c.err, got, c.want)
		}
	}
	if buf.String() != "" {
		t.Errorf("known errors logged: %s", buf.String())
	}
	if got := userText(errors.New("boom"), logger); got != "Internal error; try again later." {
		t.Errorf("internal = %q", got)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "boom") || !strings.Contains(lines[0], `"level":"ERROR"`) {
		t.Errorf("log = %q", buf.String())
	}
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
