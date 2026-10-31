package issue_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/domain/project"
)

func TestSearchTerms(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"fix  login!", "fix:* & login:*", true},
		{"вход", "вход:*", true},
		{"YLK-2", "YLK:* & 2:*", true},
		{"!!!", "", false},
		{"", "", false},
		{"a b c d e f g h i j k l", "a:* & b:* & c:* & d:* & e:* & f:* & g:* & h:* & i:* & j:*", true},
	} {
		got, ok := issue.SearchQuery(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("searchQuery(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestSearchFilter(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	first := e.create(t, "Fix login redirect")
	second, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{
		ProjectID: &e.ylk.ID, Title: "Board drag", Description: "логин сломан",
	})
	if err != nil {
		t.Fatal(err)
	}
	search := func(s string) []string {
		t.Helper()
		r, err := e.issues.List(ctx, issue.Filter{Search: &s}, issue.Order{}, page.Args{})
		if err != nil {
			t.Fatalf("search %q: %v", s, err)
		}
		var out []string
		for _, i := range r.Items {
			out = append(out, i.Identifier)
		}
		slices.Sort(out)
		return out
	}
	for _, tc := range []struct {
		q    string
		want []string
	}{
		{"logi", []string{first.Identifier}},
		{"FIX Redir", []string{first.Identifier}},
		{"логин", []string{second.Identifier}},
		{"ЛОГ", []string{second.Identifier}},
		{second.Identifier, []string{second.Identifier}},
		{strings.ToLower(second.Identifier), []string{second.Identifier}},
		{"nothing-here", nil},
		{"!!!", []string{first.Identifier, second.Identifier}},
	} {
		if got := search(tc.q); !slices.Equal(got, tc.want) {
			t.Errorf("search %q = %v, want %v", tc.q, got, tc.want)
		}
	}
	// A former identifier still finds the issue after a key change.
	if _, err := e.projects.Update(ctx, e.users.Admin, e.ylk.ID, project.UpdateInput{Key: ptr("YK")}); err != nil {
		t.Fatal(err)
	}
	if got := search(second.Identifier); len(got) != 1 || got[0] != "YK-2" {
		t.Errorf("search by former identifier = %v", got)
	}
}

func TestCount(t *testing.T) {
	f := newListFixture(t)
	ctx := context.Background()
	for _, flt := range []issue.Filter{
		{},
		{StateType: &issue.StateTypeCond{Eq: ptr(f.types[f.done.ID])}},
		{Project: &issue.IDCond{Eq: &f.web.ID}},
	} {
		n, err := f.issues.Count(ctx, flt)
		if err != nil {
			t.Fatal(err)
		}
		r, err := f.issues.List(ctx, flt, issue.Order{}, page.Args{First: ptr(250)})
		if err != nil {
			t.Fatal(err)
		}
		if n != len(r.Items) || n == 0 {
			t.Errorf("Count(%+v) = %d, List = %d", flt, n, len(r.Items))
		}
	}
}
