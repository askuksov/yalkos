package linear_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/askuksov/yalkos/internal/graphql/linear"
)

const durationInputErr = "Invalid createdAt: use an ISO 8601 date, date-time or duration"

// TestDurationBounds covers the limits of ISO 8601 durations: a component
// that does not fit an integer, or a total beyond the allowed span, is an
// input error instead of a wrapped value.
func TestDurationBounds(t *testing.T) {
	now := time.Date(2026, 10, 15, 8, 30, 0, 0, time.UTC)
	day := func(n int) time.Time { return now.AddDate(0, 0, n) }
	accepted := map[string]time.Time{
		"P2000000D":              day(2_000_000),
		"-P2000000D":             day(-2_000_000),
		"+P2000000D":             day(2_000_000),
		"P285714W2D":             day(2_000_000),
		"-P285714W2D":            day(-2_000_000),
		"P285714W":               day(1_999_998),
		"PT9223372036S":          now.Add(9223372036 * time.Second),
		"-PT9223372036S":         now.Add(-9223372036 * time.Second),
		"PT2562047H":             now.Add(2562047 * time.Hour),
		"PT153722867M":           now.Add(153722867 * time.Minute),
		"-PT153722867M":          now.Add(-153722867 * time.Minute),
		"PT2562047H47M16S":       now.Add(9223372036 * time.Second),
		"-PT2562047H47M16S":      now.Add(-9223372036 * time.Second),
		"P2000000DT2562047H47M":  day(2_000_000).Add(2562047*time.Hour + 47*time.Minute),
		"P0D":                    now,
		"PT0S":                   now,
		"P00000000000000000001D": day(1),
	}
	for in, want := range accepted {
		got, err := linear.ParseDateOrDuration("createdAt", in, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("%q = %v, %v; want %v", in, got, err, want)
		}
	}
	rejected := []string{
		"P99999999999999999999D",
		"-P99999999999999999999D",
		"+P99999999999999999999D",
		"P99999999999999999999W",
		"-P99999999999999999999W",
		"PT99999999999999999999H",
		"-PT99999999999999999999H",
		"PT99999999999999999999M",
		"PT99999999999999999999S",
		"P9223372036854775807D",
		"P9223372036854775808D",
		"P2000001D",
		"-P2000001D",
		"+P2000001D",
		"P285715W",
		"P1000000W2000000D",
		"-P1000000W2000000D",
		"P285714W3D",
		"-P285714W3D",
		"P2000000W",
		"PT9223372037S",
		"-PT9223372037S",
		"PT2562048H",
		"-PT2562048H",
		"PT153722868M",
		"-PT153722868M",
		"PT2562047H48M",
		"PT2562047H47M17S",
		"PT153722867281M",
		"P1DT99999999999999999999S",
	}
	for _, in := range rejected {
		got, err := linear.ParseDateOrDuration("createdAt", in, now)
		if err == nil {
			t.Errorf("%q = %v, want an input error", in, got)
			continue
		}
		if g := linear.Present(err); g.Message != durationInputErr || g.Extensions["field"] != "createdAt" {
			t.Errorf("%q: %q %v", in, g.Message, g.Extensions)
		}
	}
}

// daysSinceYearOne is the number of days from 0001-01-01 to the UTC date
// of t.
func daysSinceYearOne(t time.Time) int {
	y, m, d := t.UTC().Date()
	return int((time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() - time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC).Unix()) / 86400)
}

// TestTimelessDurationBounds covers the same limits for the date-only
// variant, which also refuses a clock part and a result before year 1.
func TestTimelessDurationBounds(t *testing.T) {
	today := time.Date(2026, 10, 15, 14, 45, 0, 0, time.UTC)
	midnight := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	toYearOne := daysSinceYearOne(today)
	accepted := map[string]time.Time{
		"P2000000D":  midnight.AddDate(0, 0, 2_000_000),
		"P285714W2D": midnight.AddDate(0, 0, 2_000_000),
		"P1W":        midnight.AddDate(0, 0, 7),
		"-P2D":       midnight.AddDate(0, 0, -2),
		// The last day before the result leaves year 1.
		fmt.Sprintf("-P%dD", toYearOne): time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	for in, want := range accepted {
		got, err := linear.ParseTimelessOrDuration("dueDate", in, today)
		if err != nil || !got.Equal(want) {
			t.Errorf("%q = %v, %v; want %v", in, got, err, want)
		}
	}
	// Far in the future the day bound, not the year, is what limits a
	// negative duration.
	far := time.Date(7500, 1, 1, 9, 0, 0, 0, time.UTC)
	farMidnight := time.Date(7500, 1, 1, 0, 0, 0, 0, time.UTC)
	for in, want := range map[string]time.Time{
		"-P2000000D":  farMidnight.AddDate(0, 0, -2_000_000),
		"-P285714W2D": farMidnight.AddDate(0, 0, -2_000_000),
	} {
		got, err := linear.ParseTimelessOrDuration("dueDate", in, far)
		if err != nil || !got.Equal(want) {
			t.Errorf("%q from year 7500 = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"-P2000001D", "-P285714W3D"} {
		if got, err := linear.ParseTimelessOrDuration("dueDate", in, far); err == nil {
			t.Errorf("%q from year 7500 = %v, want an input error", in, got)
		}
	}
	for _, in := range []string{
		"P99999999999999999999D",
		"P99999999999999999999W",
		"-P99999999999999999999W",
		"P2000001D",
		"-P2000001D",
		// From 2026 these land before year 1 (year 0 and earlier).
		"-P2000000D",
		"-P285714W2D",
		fmt.Sprintf("-P%dD", toYearOne+1),
		"P1000000W2000000D",
		"P285714W3D",
		"PT1H",
		"P1DT1H",
	} {
		got, err := linear.ParseTimelessOrDuration("dueDate", in, today)
		if err == nil {
			t.Errorf("%q = %v, want an input error", in, got)
			continue
		}
		if g := linear.Present(err); g.Message != "Invalid dueDate: use an ISO 8601 date, date-time or duration" || g.Extensions["field"] != "dueDate" {
			t.Errorf("%q: %q %v", in, g.Message, g.Extensions)
		}
	}
}

// TestOversizedDurationComparator checks that a comparator holding an
// oversized duration is an input error and never matches every row.
func TestOversizedDurationComparator(t *testing.T) {
	f := newIssueFix(t)
	q := `query($filter: IssueFilter) { issues(filter: $filter) { nodes { id } } }`
	const createdErr = "Invalid createdAt: use an ISO 8601 date, date-time or duration"
	const dueErr = "Invalid dueDate: use an ISO 8601 date, date-time or duration"
	for _, in := range []string{"P99999999999999999999D", "-P99999999999999999999W", "PT99999999999999999999H", "P2000001D"} {
		for _, op := range []string{"gt", "gte", "lt", "lte", "eq", "neq"} {
			f.gql(t, f.ownerKey, q, filter("createdAt", cmp(op, in))).fails(t).want(t, "INVALID_INPUT", "invalid input", createdErr, "createdAt")
		}
	}
	f.gql(t, f.ownerKey, q, filter("createdAt", cmp("in", []any{"-P1D", "P99999999999999999999D"}))).fails(t).want(t, "INVALID_INPUT", "invalid input", createdErr, "createdAt")
	f.gql(t, f.ownerKey, q, filter("dueDate", cmp("lt", "P99999999999999999999D"))).fails(t).want(t, "INVALID_INPUT", "invalid input", dueErr, "dueDate")
	f.gql(t, f.ownerKey, q, filter("dueDate", cmp("gt", "-P2000001D"))).fails(t).want(t, "INVALID_INPUT", "invalid input", dueErr, "dueDate")
}

// TestDurationRangeAgainstDatabase checks that the edge values of the
// accepted span are valid database timestamps and dates, and that the first
// value beyond it is an input error rather than a database range error.
func TestDurationRangeAgainstDatabase(t *testing.T) {
	f := newIssueFix(t)
	q := `query($filter: IssueFilter) { issues(filter: $filter) { nodes { id } } }`
	run := func(field, op, in string) result { return f.gql(t, f.ownerKey, q, filter(field, cmp(op, in))) }
	succeeds := func(field, in string) {
		for _, op := range []string{"gt", "lt"} {
			if r := run(field, op, in); len(r.Errors) > 0 {
				t.Errorf("%s %s %q: %v", field, op, in, r.Errors)
			}
		}
	}
	fails := func(field, in string) {
		for _, op := range []string{"gt", "lt"} {
			run(field, op, in).fails(t).want(t, "INVALID_INPUT", "invalid input", "Invalid "+field+": use an ISO 8601 date, date-time or duration", field)
		}
	}
	// The harness clock starts here; a margin of days covers its ticks.
	toYearOne := daysSinceYearOne(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	for _, in := range []string{"-P2000000D", "P2000000D", "-P285714W2D", "P285714W2D"} {
		succeeds("createdAt", in)
	}
	for _, in := range []string{"-P2000001D", "P2000001D", "-P3650000D", "P3650000D"} {
		fails("createdAt", in)
	}
	// A timeless date goes to the database as text: it may reach year 1 but
	// not pass it.
	for _, in := range []string{"P2000000D", "P285714W2D", fmt.Sprintf("-P%dD", toYearOne-10), "-P1D"} {
		succeeds("dueDate", in)
	}
	for _, in := range []string{"P2000001D", "-P2000000D", "-P2000001D", "-P3650000D", fmt.Sprintf("-P%dD", toYearOne+10)} {
		fails("dueDate", in)
	}
}
