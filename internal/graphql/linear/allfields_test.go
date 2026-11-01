package linear_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/vektah/gqlparser/v2/ast"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/graphql/linear"
)

// selection lists every field of an object type without required
// arguments: scalars and enums as is, objects as { id } (or { __typename }),
// connections as { nodes { id } }, unions as { __typename }.
func selection(schema *ast.Schema, typeName string) string {
	def := schema.Types[typeName]
	var b strings.Builder
	for _, f := range def.Fields {
		if strings.HasPrefix(f.Name, "__") {
			continue
		}
		required := false
		for _, a := range f.Arguments {
			if a.Type.NonNull && a.DefaultValue == nil {
				required = true
			}
		}
		if required {
			continue
		}
		t := schema.Types[f.Type.Name()]
		switch t.Kind {
		case ast.Scalar, ast.Enum:
			b.WriteString(f.Name + " ")
		case ast.Union, ast.Interface:
			b.WriteString(f.Name + " { __typename } ")
		case ast.Object:
			switch {
			case strings.HasSuffix(t.Name, "Connection"):
				b.WriteString(f.Name + " { nodes { id } pageInfo { hasNextPage } } ")
			case t.Fields.ForName("id") != nil:
				b.WriteString(f.Name + " { id } ")
			default:
				b.WriteString(f.Name + " { __typename } ")
			}
		}
	}
	return b.String()
}

func TestEveryFieldResolves(t *testing.T) {
	f := newIssueFix(t)
	ctx := context.Background()
	schema := linear.NewExecutableSchema(linear.Config{Resolvers: &linear.Resolver{}}).Schema()
	m, err := f.deps.Milestones.Create(ctx, f.owner, f.ylk.ID, milestone.CreateInput{Name: "Bank APIs", Description: "d", LeadID: &f.mia.ID})
	if err != nil {
		t.Fatal(err)
	}
	be := f.label(t, "Backend")
	y3 := f.i["YLK-3"].ID
	if _, err := f.deps.Issues.Update(ctx, f.owner, y3, issue.Patch{MilestoneID: kit.Opt[uuid.UUID]{Set: true, Value: &m.ID}, AddLabelIDs: []uuid.UUID{be.ID}}); err != nil {
		t.Fatal(err)
	}
	c := f.comment(t, "YLK-3", "hello")
	if _, err := f.deps.Attachments.Upload(ctx, f.owner, "a.txt", strings.NewReader("x"), &y3); err != nil {
		t.Fatal(err)
	}
	ws := func(name string) string { return selection(schema, name) }
	conn := func(name string) string { return "nodes { " + ws(name) + "} pageInfo { " + ws("PageInfo") + "}" }
	state := stateNamed(t, f.env, f.ylk, "Todo")
	vars := map[string]any{
		"team": "YLK", "state": state.ID.String(), "issue": "YLK-3", "label": be.ID.String(),
		"project": m.ID.String(), "comment": c.ID.String(),
	}
	// One query per root field keeps each under the cost limit.
	queries := map[string]string{
		"viewer":         "viewer { " + ws("User") + "}",
		"organization":   "organization { " + ws("Organization") + "}",
		"team":           "team(id: $team) { " + ws("Team") + "}",
		"workflowState":  "workflowState(id: $state) { " + ws("WorkflowState") + "}",
		"issue":          "issue(id: $issue) { " + ws("Issue") + "}",
		"issueLabel":     "issueLabel(id: $label) { " + ws("IssueLabel") + "}",
		"project":        "project(id: $project) { " + ws("Project") + "}",
		"comment":        "comment(id: $comment) { " + ws("Comment") + "}",
		"searchIssues":   `searchIssues(term: "fix") { totalCount archivePayload { ` + ws("ArchiveResponse") + "} " + conn("IssueSearchResult") + "}",
		"users":          "users(first: 5) { " + conn("User") + "}",
		"teams":          "teams(first: 5) { " + conn("Team") + "}",
		"issues":         "issues(first: 5) { " + conn("Issue") + "}",
		"issueLabels":    "issueLabels(first: 5) { " + conn("IssueLabel") + "}",
		"projects":       "projects(first: 5) { " + conn("Project") + "}",
		"workflowStates": "workflowStates(first: 5) { " + conn("WorkflowState") + "}",
		"attachments":    "issue(id: $issue) { attachments { " + conn("Attachment") + "} relations { " + conn("IssueRelation") + "} }",
	}
	used := map[string]string{"$team": "$team: String!", "$state": "$state: String!", "$issue": "$issue: String!",
		"$label": "$label: String!", "$project": "$project: String!", "$comment": "$comment: String"}
	lists := map[string]bool{"searchIssues": true, "users": true, "teams": true, "issues": true, "issueLabels": true, "projects": true, "workflowStates": true}
	total := 0
	for name, body := range queries {
		var decl []string
		for v, d := range used {
			if strings.Contains(body, v+")") {
				decl = append(decl, d)
			}
		}
		q := "{ " + body + " }"
		if len(decl) > 0 {
			q = "query(" + strings.Join(decl, ", ") + ") " + q
		}
		total += strings.Count(q, " ")
		r := f.gql(t, f.ownerKey, q, vars)
		for _, e := range r.Errors {
			t.Errorf("%s %v: %s", name, e.Path, e.Message)
		}
		var data map[string]json.RawMessage
		if err := json.Unmarshal(r.Data, &data); err != nil || len(data) != 1 {
			t.Fatalf("%s: data %s (%v)", name, r.Data, err)
		}
		for _, v := range data {
			if string(v) == "null" {
				t.Errorf("%s is null", name)
			}
			var list struct{ Nodes []json.RawMessage }
			if lists[name] {
				if err := json.Unmarshal(v, &list); err != nil || len(list.Nodes) == 0 {
					t.Errorf("%s: no nodes seeded", name)
				}
			}
		}
	}
	if total < 1000 {
		t.Fatalf("queries too small (%d tokens): the selections built nothing", total)
	}
}
