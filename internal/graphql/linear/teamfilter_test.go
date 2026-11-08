package linear_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// A team filter lists issues of archived teams only when it names them by
// id or key, or with includeArchived.
func TestIssueFilterTeamArchived(t *testing.T) {
	f := newIssueFix(t)
	ctx := context.Background()
	old := f.project(t, "OLD", "Old stuff")
	f.issue(t, old, "Old work")
	if _, err := f.deps.Projects.SetArchived(ctx, f.owner, old.ID, true); err != nil {
		t.Fatal(err)
	}
	team := func(c map[string]any) map[string]any { return filter("team", c) }
	set(t, "key neq", f.list(t, team(map[string]any{"key": cmp("neq", "WEB")})), "YLK-1", "YLK-2", "YLK-3", "YLK-4")
	set(t, "name contains", f.list(t, team(map[string]any{"name": cmp("containsIgnoreCase", "old")})))
	set(t, "key eq archived", f.list(t, team(map[string]any{"key": cmp("eq", "OLD")})), "OLD-1")
	set(t, "key in archived", f.list(t, team(map[string]any{"key": cmp("in", []string{"OLD", "WEB"})})), "OLD-1", "WEB-1")
	set(t, "id eq archived", f.list(t, team(map[string]any{"id": cmp("eq", old.ID.String())})), "OLD-1")
	vars := team(map[string]any{"key": cmp("neq", "WEB")})
	vars["includeArchived"] = true
	set(t, "key neq + includeArchived", f.list(t, vars), "YLK-1", "YLK-2", "YLK-3", "YLK-4", "OLD-1")
}

// A team filter naming an archived team selects its issues the same way at
// the top level and inside or/and groups; other branches and archived teams
// not named stay hidden unless includeArchived is set.
func TestIssueFilterTeamArchivedInGroups(t *testing.T) {
	f := newIssueFix(t)
	ctx := context.Background()
	old := f.project(t, "OLD", "Old stuff")
	f.issue(t, old, "Old work")
	gone := f.project(t, "GONE", "Gone stuff")
	f.issue(t, gone, "Gone sync")
	for _, id := range []uuid.UUID{old.ID, gone.ID} {
		if _, err := f.deps.Projects.SetArchived(ctx, f.owner, id, true); err != nil {
			t.Fatal(err)
		}
	}
	type m = map[string]any
	run := func(t *testing.T, filterVal m, includeArchived bool) []string {
		t.Helper()
		vars := m{"filter": filterVal}
		if includeArchived {
			vars["includeArchived"] = true
		}
		return f.list(t, vars)
	}
	sync := m{"title": cmp("contains", "sync")}
	names := []struct {
		name  string
		team  m
		named []string // issues of the teams the filter names
	}{
		{"id eq", m{"id": cmp("eq", old.ID.String())}, []string{"OLD-1"}},
		{"id in", m{"id": cmp("in", []string{old.ID.String(), f.web.ID.String()})}, []string{"OLD-1", "WEB-1"}},
		{"key eq", m{"key": cmp("eq", "OLD")}, []string{"OLD-1"}},
		{"key in", m{"key": cmp("in", []string{"OLD", "WEB"})}, []string{"OLD-1", "WEB-1"}},
	}
	union := func(a []string, b ...string) []string {
		out := slices.Clone(a)
		for _, s := range b {
			if !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
		return out
	}
	for _, n := range names {
		for _, archived := range []bool{false, true} {
			suffix := ""
			if archived {
				suffix = " + includeArchived"
			}
			// Branch "title contains sync": YLK-1, WEB-1, and GONE-1 only
			// with includeArchived.
			syncHits := []string{"YLK-1", "WEB-1"}
			if archived {
				syncHits = append(syncHits, "GONE-1")
			}
			t.Run(n.name+" top level"+suffix, func(t *testing.T) {
				set(t, "top", run(t, m{"team": n.team}, archived), n.named...)
			})
			t.Run(n.name+" or single"+suffix, func(t *testing.T) {
				set(t, "or", run(t, m{"or": []any{m{"team": n.team}}}, archived), n.named...)
			})
			t.Run(n.name+" and single"+suffix, func(t *testing.T) {
				set(t, "and", run(t, m{"and": []any{m{"team": n.team}}}, archived), n.named...)
			})
			t.Run(n.name+" or with title branch"+suffix, func(t *testing.T) {
				set(t, "or mixed", run(t, m{"or": []any{m{"team": n.team}, sync}}, archived), union(n.named, syncHits...)...)
			})
			t.Run(n.name+" and with title"+suffix, func(t *testing.T) {
				set(t, "and mixed", run(t, m{"and": []any{m{"team": n.team}, m{"title": cmp("contains", "work")}}}, archived), "OLD-1")
			})
			t.Run(n.name+" nested or in and"+suffix, func(t *testing.T) {
				got := run(t, m{"and": []any{m{"or": []any{m{"team": n.team}, sync}}}}, archived)
				set(t, "nested", got, union(n.named, syncHits...)...)
			})
		}
	}
	t.Run("team not named stays hidden in or", func(t *testing.T) {
		set(t, "or", run(t, m{"or": []any{m{"team": m{"key": cmp("neq", "WEB")}}, sync}}, false), "YLK-1", "YLK-2", "YLK-3", "YLK-4", "WEB-1")
	})
	t.Run("team not named in or + includeArchived", func(t *testing.T) {
		set(t, "or", run(t, m{"or": []any{m{"team": m{"key": cmp("neq", "WEB")}}, sync}}, true),
			"YLK-1", "YLK-2", "YLK-3", "YLK-4", "WEB-1", "OLD-1", "GONE-1")
	})
	t.Run("other archived team not named stays hidden", func(t *testing.T) {
		set(t, "or", run(t, m{"or": []any{m{"team": m{"key": cmp("eq", "OLD")}}, m{"title": cmp("contains", "Gone")}}}, false), "OLD-1")
	})
}
