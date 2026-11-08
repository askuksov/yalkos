package linear

import (
	"math"
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

// timeConds are the parsed operators of a date comparator.
type timeConds struct {
	eq, neq, lt, lte, gt, gte *time.Time
	in, nin                   []time.Time
	null                      *bool
}

type dateOps struct {
	eq, neq, lt, lte, gt, gte *string
	in, nin                   []string
	null                      *bool
}

func dateOpsOf(c *DateComparator) dateOps {
	if c == nil {
		return dateOps{}
	}
	return dateOps{c.Eq, c.Neq, c.Lt, c.Lte, c.Gt, c.Gte, c.In, c.Nin, nil}
}

func nullableDateOpsOf(c *NullableDateComparator) dateOps {
	if c == nil {
		return dateOps{}
	}
	return dateOps{c.Eq, c.Neq, c.Lt, c.Lte, c.Gt, c.Gte, c.In, c.Nin, c.Null}
}

func timelessOpsOf(c *NullableTimelessDateComparator) dateOps {
	if c == nil {
		return dateOps{}
	}
	return dateOps{c.Eq, c.Neq, c.Lt, c.Lte, c.Gt, c.Gte, c.In, c.Nin, c.Null}
}

// parse reads every operator with parse.
func (o dateOps) parse(field string, parse func(field, s string) (time.Time, error)) (timeConds, error) {
	out := timeConds{null: o.null}
	for _, p := range []struct {
		dst **time.Time
		src *string
	}{{&out.eq, o.eq}, {&out.neq, o.neq}, {&out.lt, o.lt}, {&out.lte, o.lte}, {&out.gt, o.gt}, {&out.gte, o.gte}} {
		if p.src == nil {
			continue
		}
		t, err := parse(field, *p.src)
		if err != nil {
			return out, err
		}
		*p.dst = &t
	}
	for _, p := range []struct {
		dst *[]time.Time
		src []string
	}{{&out.in, o.in}, {&out.nin, o.nin}} {
		if p.src == nil {
			continue
		}
		*p.dst = make([]time.Time, len(p.src))
		for i, s := range p.src {
			t, err := parse(field, s)
			if err != nil {
				return out, err
			}
			(*p.dst)[i] = t
		}
	}
	return out, nil
}

func (c timeConds) match(t time.Time) bool {
	has := func(list []time.Time) bool {
		return slices.ContainsFunc(list, func(x time.Time) bool { return x.Equal(t) })
	}
	return (c.eq == nil || t.Equal(*c.eq)) && (c.neq == nil || !t.Equal(*c.neq)) &&
		(c.lt == nil || t.Before(*c.lt)) && (c.lte == nil || !t.After(*c.lte)) &&
		(c.gt == nil || t.After(*c.gt)) && (c.gte == nil || !t.Before(*c.gte)) &&
		(c.in == nil || has(c.in)) && (c.nin == nil || !has(c.nin))
}

func matchTime(field string, c *DateComparator, t, now time.Time) (bool, error) {
	if c == nil {
		return true, nil
	}
	conds, err := dateOpsOf(c).parse(field, func(f, s string) (time.Time, error) { return parseDateOrDuration(f, s, now) })
	if err != nil {
		return false, err
	}
	return conds.match(t), nil
}

func matchNullableTime(field string, c *NullableDateComparator, t *time.Time, now time.Time) (bool, error) {
	if c == nil {
		return true, nil
	}
	conds, err := nullableDateOpsOf(c).parse(field, func(f, s string) (time.Time, error) { return parseDateOrDuration(f, s, now) })
	if err != nil {
		return false, err
	}
	if conds.null != nil && *conds.null != (t == nil) {
		return false, nil
	}
	if t == nil {
		// Negative operators keep rows without a value; others need one.
		return conds.eq == nil && conds.lt == nil && conds.lte == nil && conds.gt == nil && conds.gte == nil && conds.in == nil, nil
	}
	return conds.match(*t), nil
}

var durationRe = regexp.MustCompile(`^([+-])?P(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)

func badDate(field string) error {
	return kit.Input(field, kit.ReasonInvalidFormat, "use an ISO 8601 date, date-time or duration")
}

// Limits of an ISO 8601 duration. The day part (weeks*7 + days) may span at
// most maxDurationDays in either direction: about 5 476 years, which keeps
// the resulting time inside the range of the database timestamp and date
// types, so a comparator never fails there with a range error. The clock part
// (hours, minutes, seconds) is held in a time.Duration, so its total in
// seconds must fit there; that is the stricter bound for a clock.
const (
	maxDurationDays = 2_000_000
	maxClockSeconds = math.MaxInt64 / int64(time.Second)
)

// duration parses an ISO 8601 duration of weeks, days, hours, minutes and
// seconds; timeless allows only weeks and days. A component that does not
// fit, or a total beyond the limits above, is not a duration (ok is false).
func duration(s string, timeless bool) (weeks, days int, clock time.Duration, ok bool) {
	m := durationRe.FindStringSubmatch(s)
	if m == nil || m[2]+m[3]+m[4]+m[5]+m[6] == "" || strings.HasSuffix(s, "T") {
		return 0, 0, 0, false
	}
	if timeless && m[4]+m[5]+m[6] != "" {
		return 0, 0, 0, false
	}
	// n parses one component (absent is zero); a value above limit is
	// refused, so the products and sums below cannot overflow.
	n := func(x string, limit int64) (int64, bool) {
		if x == "" {
			return 0, true
		}
		v, err := strconv.ParseInt(x, 10, 64)
		return v, err == nil && v <= limit
	}
	w, okW := n(m[2], maxDurationDays/7)
	d, okD := n(m[3], maxDurationDays)
	h, okH := n(m[4], maxClockSeconds/3600)
	mi, okM := n(m[5], maxClockSeconds/60)
	sec, okS := n(m[6], maxClockSeconds)
	if !okW || !okD || !okH || !okM || !okS {
		return 0, 0, 0, false
	}
	if 7*w+d > maxDurationDays || h*3600+mi*60+sec > maxClockSeconds {
		return 0, 0, 0, false
	}
	sign := int64(1)
	if m[1] == "-" {
		sign = -1
	}
	clock = time.Duration(h*3600+mi*60+sec) * time.Second
	return int(sign * w), int(sign * d), time.Duration(sign) * clock, true
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

// parseTimelessOrDuration reads a date or a duration in weeks and days
// added to today (a UTC date).
func parseTimelessOrDuration(field, s string, today time.Time) (time.Time, error) {
	if w, d, _, ok := duration(s, true); ok {
		y, m, dd := today.UTC().Date()
		t := time.Date(y, m, dd, 0, 0, 0, 0, time.UTC).AddDate(0, 0, 7*w+d)
		// The SQL date parameter is rendered as text, and a year before 1 is
		// not valid there, so such a result is refused as an input error.
		if t.Year() < 1 {
			return time.Time{}, badDate(field)
		}
		return t, nil
	}
	if t, err := time.Parse(dateLayout, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		y, m, d := t.UTC().Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC), nil
	}
	return time.Time{}, badDate(field)
}
