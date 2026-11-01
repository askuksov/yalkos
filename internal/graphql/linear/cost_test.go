package linear_test

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/askuksov/yalkos/internal/graphql/linear"
)

func TestCostFormula(t *testing.T) {
	cases := []struct {
		q    string
		vars map[string]any
		want float64
	}{
		{`{ viewer { id name } }`, nil, 1.2},
		{`{ users { nodes { id } } }`, nil, 105},
		{`query($first: Int) { users(first: $first) { nodes { id } } }`, map[string]any{"first": 10}, 21},
		{`{ users(last: 3) { nodes { id } pageInfo { hasNextPage } } }`, nil, (1 + 1.1 + 1.1) * 3},
		{`{ users(first: 900) { nodes { id } } }`, nil, 2.1 * 250},
		{`{ users(first: 0) { nodes { id } } }`, nil, 2.1},
		{`{ a: viewer { ...F } b: viewer { id } } fragment F on User { id name }`, nil, 2.3},
		{`{ viewer { ... on User { id } } }`, nil, 1.1},
	}
	for _, c := range cases {
		got, err := linear.CostOf(c.q, c.vars)
		if err != nil {
			t.Fatalf("%s: %v", c.q, err)
		}
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s = %v, want %v", c.q, got, c.want)
		}
	}
}

func TestCostLimitAndHeader(t *testing.T) {
	e := newEnv(t)
	r := e.gql(t, e.ownerKey, `{ viewer { id name } }`, nil)
	r.ok(t, nil)
	if r.Header.Get("X-Complexity") != "2" {
		t.Fatalf("X-Complexity = %q", r.Header.Get("X-Complexity"))
	}
	var ids strings.Builder
	for i := range 400 {
		fmt.Fprintf(&ids, "i%d: id ", i)
	}
	r = e.gql(t, e.ownerKey, `{ users(first: 250) { nodes { `+ids.String()+` } } }`, nil)
	if r.Status != 200 || string(r.Data) != "null" {
		t.Fatalf("over limit: status %d data %s", r.Status, r.Data)
	}
	r.fails(t).want(t, "COMPLEXITY_LIMIT_EXCEEDED", "graphql error", "Query too complex: 10500 points, the limit is 10000", "")
	var roots strings.Builder
	for i := range 51 {
		fmt.Fprintf(&roots, "v%d: viewer { id } ", i)
	}
	e.gql(t, e.ownerKey, `{ `+roots.String()+` }`, nil).fails(t).
		want(t, "COMPLEXITY_LIMIT_EXCEEDED", "graphql error", "Operation has 51 root fields, the limit is 50", "")
	// Positive control: 50 root fields pass.
	roots.Reset()
	for i := range 50 {
		fmt.Fprintf(&roots, "v%d: viewer { id } ", i)
	}
	e.gql(t, e.ownerKey, `{ `+roots.String()+` }`, nil).ok(t, nil)
}

func TestDurationParsing(t *testing.T) {
	now := time.Date(2026, 10, 15, 8, 30, 0, 0, time.UTC)
	cases := map[string]time.Time{
		"-P2W":                      now.AddDate(0, 0, -14),
		"P1DT12H":                   now.Add(36 * time.Hour),
		"-PT12H":                    now.Add(-12 * time.Hour),
		"+P1D":                      now.Add(24 * time.Hour),
		"PT90M":                     now.Add(90 * time.Minute),
		"PT30S":                     now.Add(30 * time.Second),
		"2026-10-01":                time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		"2026-10-01T10:00:00Z":      time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC),
		"2026-10-01T12:00:00+02:00": time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC),
	}
	for in, want := range cases {
		got, err := linear.ParseDateOrDuration("createdAt", in, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("%q = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"P", "2W", "PT", "P1Y", "P1M", "yesterday", ""} {
		_, err := linear.ParseDateOrDuration("createdAt", in, now)
		if err == nil {
			t.Errorf("%q: no error", in)
			continue
		}
		if g := linear.Present(err); g.Message != "Invalid createdAt: use an ISO 8601 date, date-time or duration" || g.Extensions["field"] != "createdAt" {
			t.Errorf("%q: %q %v", in, g.Message, g.Extensions)
		}
	}
}

// fanOut is a query whose fragments each spread the next one twice: walked
// naively it visits 2^levels fields.
func fanOut(levels int) string {
	var b strings.Builder
	b.WriteString("{ viewer { ...F0 } }\n")
	for i := range levels {
		fmt.Fprintf(&b, "fragment F%d on User { ...F%d ...F%d }\n", i, i+1, i+1)
	}
	fmt.Fprintf(&b, "fragment F%d on User { id }\n", levels)
	return b.String()
}

func TestCostFragmentFanOut(t *testing.T) {
	done := make(chan float64, 1)
	go func() {
		c, err := linear.CostOf(fanOut(40), nil)
		if err != nil {
			t.Error(err)
		}
		done <- c
	}()
	select {
	case c := <-done:
		if c <= 10_000 {
			t.Fatalf("cost = %v, want over the limit", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cost of a fragment fan-out took over 2s")
	}
	e := newEnv(t)
	start := time.Now()
	g := e.gql(t, e.ownerKey, fanOut(40), nil).fails(t)
	if g.Extensions["code"] != "COMPLEXITY_LIMIT_EXCEEDED" || time.Since(start) > 2*time.Second {
		t.Fatalf("fan-out over HTTP: %+v after %v", g, time.Since(start))
	}
	// Root-level fan-out is counted without walking every spread either.
	start = time.Now()
	g = e.gql(t, e.ownerKey, strings.Replace(fanOut(40), "{ viewer { ...F0 } }", "{ ...Q0 }", 1)+rootFanOut(40), nil).fails(t)
	if time.Since(start) > 2*time.Second {
		t.Fatalf("root fan-out took %v: %+v", time.Since(start), g)
	}
}

func rootFanOut(levels int) string {
	var b strings.Builder
	for i := range levels {
		fmt.Fprintf(&b, "fragment Q%d on Query { ...Q%d ...Q%d }\n", i, i+1, i+1)
	}
	fmt.Fprintf(&b, "fragment Q%d on Query { viewer { ...F0 } }\n", levels)
	return b.String()
}
