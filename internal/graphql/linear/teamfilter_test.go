package linear_test

import (
	"context"
	"testing"
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
