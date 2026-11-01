package mcpserver

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/label"
)

// stamp formats a timestamp as YYYY-MM-DD HH:mm UTC.
func stamp(t time.Time) string { return t.UTC().Format("2006-01-02 15:04") }

// day formats a date as YYYY-MM-DD.
func day(t time.Time) string { return t.Format("2006-01-02") }

func dayPtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return day(*t)
}

// cut keeps the first limit runes of s; a cut text ends with "…".
func cut(s string, limit int) (string, bool) {
	if utf8.RuneCountInString(s) <= limit {
		return s, false
	}
	n := 0
	for i := range s {
		if n == limit {
			return s[:i] + "…", true
		}
		n++
	}
	return s, false
}

func cutText(s string, limit int) string {
	out, _ := cut(s, limit)
	return out
}

// absLinks makes relative attachment links in Markdown absolute.
func absLinks(md, base string) string {
	return strings.ReplaceAll(md, "](/api/attachments/", "]("+base+"/api/attachments/")
}

// base is BASE_URL without a trailing slash.
func (c *call) base() string { return strings.TrimSuffix(c.d.BaseURL.String(), "/") }

// link makes an absolute link from a path starting with "/".
func (c *call) link(path string) string { return c.base() + path }

// labelName is Group/Name for a label in a group, else its name.
func labelName(l label.Label, groups map[uuid.UUID]label.Label) string {
	if l.ParentID != nil {
		if g, ok := groups[*l.ParentID]; ok {
			return g.Name + "/" + l.Name
		}
	}
	return l.Name
}

// encode writes v as compact JSON without HTML escaping.
func encode(v any) (string, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}
