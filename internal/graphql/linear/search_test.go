package linear_test

import (
	"testing"
)

type searchPayload struct {
	Nodes []struct {
		Identifier string
		Metadata   map[string]any
		Team       struct{ Key string }
		LabelIDs   []string `json:"labelIds"`
	}
	TotalCount     float64
	PageInfo       struct{ HasNextPage bool }
	ArchivePayload struct {
		Archive              string
		TotalCount           float64
		DatabaseVersion      float64
		IncludesDependencies []string
	}
}

func TestSearchIssuesTerm(t *testing.T) {
	f := newIssueFix(t)
	q := `query($term: String!, $teamId: String, $includeComments: Boolean, $first: Int, $filter: IssueFilter) {
		searchIssues(term: $term, teamId: $teamId, includeComments: $includeComments, first: $first, filter: $filter) {
			nodes { identifier metadata team { key } labelIds } totalCount pageInfo { hasNextPage }
			archivePayload { archive totalCount databaseVersion includesDependencies } } }`
	run := func(vars map[string]any) searchPayload {
		var out struct{ SearchIssues searchPayload }
		f.gql(t, f.ownerKey, q, vars).ok(t, &out)
		return out.SearchIssues
	}
	ids := func(p searchPayload) []string {
		var out []string
		for _, n := range p.Nodes {
			out = append(out, n.Identifier)
		}
		return out
	}
	p := run(map[string]any{"term": "bank", "includeComments": true})
	set(t, "bank", ids(p), "YLK-1", "YLK-2")
	if p.TotalCount != 2 || p.ArchivePayload.Archive != "" || p.ArchivePayload.TotalCount != 0 || p.ArchivePayload.IncludesDependencies == nil {
		t.Fatalf("payload = %+v", p)
	}
	if p.Nodes[0].Metadata == nil || len(p.Nodes[0].Metadata) != 0 || p.Nodes[0].Team.Key != "YLK" || len(p.Nodes[0].LabelIDs) != 1 {
		t.Fatalf("node = %+v", p.Nodes[0])
	}
	set(t, "identifier", ids(run(map[string]any{"term": "YLK-2"})), "YLK-2")
	set(t, "sync", ids(run(map[string]any{"term": "sync"})), "YLK-1", "WEB-1")
	set(t, "sync in WEB", ids(run(map[string]any{"term": "sync", "teamId": "WEB"})), "WEB-1")
	set(t, "sync in YLK by id", ids(run(map[string]any{"term": "sync", "teamId": f.ylk.ID.String()})), "YLK-1")
	set(t, "with filter", ids(run(map[string]any{"term": "bank", "filter": filter("priority", cmp("eq", 1))["filter"]})), "YLK-2")
	paged := run(map[string]any{"term": "bank", "first": 1})
	if len(paged.Nodes) != 1 || !paged.PageInfo.HasNextPage || paged.TotalCount != 2 {
		t.Fatalf("paged = %+v", paged)
	}
	f.gql(t, f.ownerKey, q, map[string]any{"term": "bank", "teamId": "NOPE"}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid teamId: no such team", "teamId")
}

func TestIssueSearchQuery(t *testing.T) {
	f := newIssueFix(t)
	var out struct{ IssueSearch issueConn }
	f.gql(t, f.ownerKey, `{ issueSearch(query: "bank", filter: { state: { type: { eq: "unstarted" } } }) { nodes { identifier } } }`, nil).ok(t, &out)
	set(t, "issueSearch", idents(out.IssueSearch.Nodes), "YLK-1")
	f.gql(t, f.ownerKey, `{ issueSearch { nodes { identifier } } }`, nil).ok(t, &out)
	set(t, "no query", idents(out.IssueSearch.Nodes), "YLK-1", "YLK-2", "YLK-3", "YLK-4", "WEB-1")
}

func TestBranchSearch(t *testing.T) {
	f := newIssueFix(t)
	q := `query($b: String!) { issueVcsBranchSearch(branchName: $b) { identifier } }`
	for branch, want := range map[string]string{
		"feature/ylk-3-retry": "YLK-3",
		"olga/YLK-3":          "YLK-3",
		"ylk-2":               "YLK-2",
		"fix-7-then-web-1":    "WEB-1",
		"main":                "",
		"ylk-999-x":           "",
	} {
		r := f.gql(t, f.ownerKey, q, map[string]any{"b": branch})
		var out struct{ IssueVcsBranchSearch *issueNode }
		r.ok(t, &out)
		switch {
		case want == "" && out.IssueVcsBranchSearch != nil:
			t.Errorf("%s → %+v, want null", branch, out.IssueVcsBranchSearch)
		case want != "" && (out.IssueVcsBranchSearch == nil || out.IssueVcsBranchSearch.Identifier != want):
			t.Errorf("%s → %+v, want %s", branch, out.IssueVcsBranchSearch, want)
		}
	}
}
