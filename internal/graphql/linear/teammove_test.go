package linear_test

import "testing"

// teamId of the issue's own team is no move, so other fields may come with it.
func TestIssueUpdateOwnTeamWithOtherFields(t *testing.T) {
	f := newIssueFix(t)
	done := stateNamed(t, f.env, f.ylk, "Done")
	p := f.update(t, "YLK-2", map[string]any{"teamId": "YLK", "title": "Same team", "stateId": done.ID.String(), "priority": 4})
	if p.Issue.Identifier != "YLK-2" || p.Issue.Title != "Same team" || p.Issue.State.Name != "Done" || p.Issue.PriorityLabel != "Low" {
		t.Fatalf("update = %+v", p.Issue)
	}
	// Control: a real move with other fields is still refused.
	f.gql(t, f.ownerKey, updateIssue, map[string]any{"id": "YLK-2", "input": map[string]any{"teamId": "WEB", "title": "x"}}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid teamId: move the issue in its own update (teamId with stateId only)", "teamId")
}
