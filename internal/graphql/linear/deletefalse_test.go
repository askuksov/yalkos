package linear_test

import "testing"

// permanentlyDelete: false is the default and deletes as usual.
func TestIssueDeletePermanentlyFalse(t *testing.T) {
	f := newIssueFix(t)
	q := `mutation($id: String!, $p: Boolean) { issueDelete(id: $id, permanentlyDelete: $p) { success } }`
	f.gql(t, f.ownerKey, q, map[string]any{"id": "YLK-2", "p": false}).ok(t, nil)
	f.gql(t, f.ownerKey, `{ issue(id: "YLK-2") { id } }`, nil).fails(t).want(t, "NOT_FOUND", "invalid input", "Issue not found", "")
	f.gql(t, f.ownerKey, q, map[string]any{"id": "YLK-1", "p": true}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "permanentlyDelete is not supported", "permanentlyDelete")
}
