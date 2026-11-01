package linear

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/kit"
)

// In-memory comparators for lists filtered after one domain read. Several
// operators in one comparator are AND-ed.

func matchString(c *StringComparator, v string) bool {
	if c == nil {
		return true
	}
	lv := strings.ToLower(v)
	checks := []struct {
		arg *string
		ok  func(string) bool
	}{
		{c.Eq, func(s string) bool { return v == s }},
		{c.Neq, func(s string) bool { return v != s }},
		{c.EqIgnoreCase, func(s string) bool { return lv == strings.ToLower(s) }},
		{c.NeqIgnoreCase, func(s string) bool { return lv != strings.ToLower(s) }},
		{c.Contains, func(s string) bool { return strings.Contains(v, s) }},
		{c.NotContains, func(s string) bool { return !strings.Contains(v, s) }},
		{c.ContainsIgnoreCase, func(s string) bool { return strings.Contains(lv, strings.ToLower(s)) }},
		{c.NotContainsIgnoreCase, func(s string) bool { return !strings.Contains(lv, strings.ToLower(s)) }},
		{c.StartsWith, func(s string) bool { return strings.HasPrefix(v, s) }},
		{c.StartsWithIgnoreCase, func(s string) bool { return strings.HasPrefix(lv, strings.ToLower(s)) }},
		{c.NotStartsWith, func(s string) bool { return !strings.HasPrefix(v, s) }},
		{c.EndsWith, func(s string) bool { return strings.HasSuffix(v, s) }},
		{c.NotEndsWith, func(s string) bool { return !strings.HasSuffix(v, s) }},
	}
	for _, ch := range checks {
		if ch.arg != nil && !ch.ok(*ch.arg) {
			return false
		}
	}
	if c.In != nil && !slices.Contains(c.In, v) {
		return false
	}
	if c.Nin != nil && slices.Contains(c.Nin, v) {
		return false
	}
	return true
}

func matchID(field string, c *IDComparator, id uuid.UUID) (bool, error) {
	if c == nil {
		return true, nil
	}
	ok := true
	for _, op := range []struct {
		arg  *string
		want bool
	}{{c.Eq, true}, {c.Neq, false}} {
		if op.arg == nil {
			continue
		}
		v, err := parseID(field, *op.arg)
		if err != nil {
			return false, err
		}
		ok = ok && (v == id) == op.want
	}
	for _, op := range []struct {
		arg  []string
		want bool
	}{{c.In, true}, {c.Nin, false}} {
		if op.arg == nil {
			continue
		}
		ids, err := parseIDs(field, op.arg)
		if err != nil {
			return false, err
		}
		ok = ok && slices.Contains(ids, id) == op.want
	}
	return ok, nil
}

func matchBool(c *BooleanComparator, v bool) bool {
	if c == nil {
		return true
	}
	return (c.Eq == nil || *c.Eq == v) && (c.Neq == nil || *c.Neq != v)
}

var durationRe = regexp.MustCompile(`^([+-])?P(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)

func badDate(field string) error {
	return kit.Input(field, kit.ReasonInvalidFormat, "use an ISO 8601 date, date-time or duration")
}

// duration parses an ISO 8601 duration of weeks, days, hours, minutes and
// seconds; timeless allows only weeks and days.
func duration(s string, timeless bool) (weeks, days int, clock time.Duration, ok bool) {
	m := durationRe.FindStringSubmatch(s)
	if m == nil || m[2]+m[3]+m[4]+m[5]+m[6] == "" || strings.HasSuffix(s, "T") {
		return 0, 0, 0, false
	}
	if timeless && m[4]+m[5]+m[6] != "" {
		return 0, 0, 0, false
	}
	n := func(x string) int { v, _ := strconv.Atoi(x); return v }
	sign := 1
	if m[1] == "-" {
		sign = -1
	}
	clock = time.Duration(n(m[4]))*time.Hour + time.Duration(n(m[5]))*time.Minute + time.Duration(n(m[6]))*time.Second
	return sign * n(m[2]), sign * n(m[3]), time.Duration(sign) * clock, true
}

// parseDateOrDuration reads an RFC 3339 date-time, a YYYY-MM-DD date
// (midnight UTC) or a signed duration added to now.
func parseDateOrDuration(field, s string, now time.Time) (time.Time, error) {
	if w, d, c, ok := duration(s, false); ok {
		return now.AddDate(0, 0, 7*w+d).Add(c), nil
	}
	if t, err := time.Parse(dateLayout, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, badDate(field)
}
