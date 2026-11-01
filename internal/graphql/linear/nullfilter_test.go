package linear_test

import "testing"

// null inside and/or groups of a user filter keeps its meaning.
func TestIssueFilterNullInUserGroups(t *testing.T) {
	f := newIssueFix(t)
	set(t, "assignee or [null, isMe]", f.list(t, filter("assignee", map[string]any{"or": []any{
		map[string]any{"null": true}, map[string]any{"isMe": cmp("eq", true)},
	}})), "YLK-1", "YLK-3", "YLK-4", "WEB-1")
	set(t, "assignee and [null]", f.list(t, filter("assignee", map[string]any{"and": []any{map[string]any{"null": true}}})), "YLK-3", "YLK-4", "WEB-1")
	set(t, "assignee or [and [not null, Mia]]", f.list(t, filter("assignee", map[string]any{"or": []any{
		map[string]any{"and": []any{map[string]any{"null": false}, map[string]any{"name": cmp("eq", "Mia")}}},
	}})), "YLK-2")
	set(t, "assignee isMe and or [null]", f.list(t, filter("assignee", map[string]any{
		"isMe": cmp("eq", true), "or": []any{map[string]any{"null": true}},
	})))
	set(t, "creator null", f.list(t, filter("creator", cmp("null", true))))
	set(t, "creator or [null, Mia]", f.list(t, filter("creator", map[string]any{"or": []any{
		map[string]any{"null": true}, map[string]any{"email": cmp("eq", "mia@example.com")},
	}})), "YLK-4")
	// Positive control: the same "unassigned or mine" as a top-level or.
	set(t, "top-level or", f.list(t, filter("or", []any{
		map[string]any{"assignee": cmp("null", true)}, map[string]any{"assignee": map[string]any{"isMe": cmp("eq", true)}},
	})), "YLK-1", "YLK-3", "YLK-4", "WEB-1")
}
