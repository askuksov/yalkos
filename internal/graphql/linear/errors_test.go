package linear_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/graphql/linear"
)

func TestPresenterTranslates(t *testing.T) {
	g := linear.Present(kit.Input("milestoneId", kit.ReasonWrongProject, "milestone belongs to another project"))
	msg := "Invalid projectId: project belongs to another team"
	if g.Message != msg || g.Extensions["field"] != "projectId" || g.Extensions["type"] != "invalid input" ||
		g.Extensions["code"] != "INVALID_INPUT" || g.Extensions["userPresentableMessage"] != msg || g.Extensions["reason"] != "wrong_project" {
		t.Fatalf("milestone input = %q %v", g.Message, g.Extensions)
	}
	for domain, want := range map[string]string{
		"no such milestone":                    "Invalid projectId: no such project",
		"no such project":                      "Invalid projectId: no such team",
		"project is archived":                  "Invalid projectId: team is archived",
		"choose a project":                     "Invalid projectId: choose a team",
		"issue is already in this project":     "Invalid projectId: issue is already in this team",
		"parent belongs to another project":    "Invalid projectId: parent belongs to another team",
		"state belongs to another project":     "Invalid projectId: state belongs to another team",
		"a milestone with this name exists":    "Invalid projectId: a project with this name exists in this team",
		"milestone belongs to another project": "Invalid projectId: project belongs to another team",
	} {
		// The field projectId (a Yalkos project) reads as teamId; milestoneId as projectId.
		g := linear.Present(kit.Input("milestoneId", kit.ReasonInvalidFormat, domain))
		if g.Message != want {
			t.Errorf("%q → %q, want %q", domain, g.Message, want)
		}
	}
	if g := linear.Present(kit.Input("projectId", kit.ReasonArchived, "project is archived")); g.Message != "Invalid teamId: team is archived" || g.Extensions["field"] != "teamId" {
		t.Errorf("projectId: %q %v", g.Message, g.Extensions)
	}
	if g := linear.Present(kit.Input("stateId", kit.ReasonWrongProject, "state belongs to another project")); g.Message != "Invalid stateId: state belongs to another team" || g.Extensions["field"] != "stateId" {
		t.Errorf("stateId: %q %v", g.Message, g.Extensions)
	}
	if g := linear.Present(kit.Input("title", kit.ReasonTooLong, "at most 255 characters")); g.Message != "Invalid title: at most 255 characters" {
		t.Errorf("other message: %q", g.Message)
	}
	for entity, want := range map[string]string{"State": "WorkflowState not found", "Project": "Team not found", "Milestone": "Project not found", "Label": "IssueLabel not found", "Issue": "Issue not found"} {
		g := linear.Present(kit.NotFoundError{Entity: entity})
		if g.Message != want || g.Extensions["code"] != "NOT_FOUND" || g.Extensions["type"] != "invalid input" || g.Extensions["userPresentableMessage"] != want {
			t.Errorf("not found %s = %q %v", entity, g.Message, g.Extensions)
		}
	}
	g = linear.Present(kit.ErrForbidden)
	if g.Extensions["code"] != "FORBIDDEN" || g.Extensions["type"] != "forbidden" {
		t.Errorf("forbidden = %v", g.Extensions)
	}
	g = linear.Present(errors.New("pg: connection reset"))
	if g.Extensions["type"] != "internal error" || g.Message != "Internal error" || g.Extensions["userPresentableMessage"] != nil {
		t.Errorf("internal = %q %v", g.Message, g.Extensions)
	}
}

func TestValidationErrorType(t *testing.T) {
	e := newEnv(t)
	g := e.gql(t, e.ownerKey, `{ viewer { nope } }`, nil).fails(t)
	if g.Extensions["type"] != "graphql error" || g.Extensions["code"] != "GRAPHQL_VALIDATION_FAILED" || !strings.Contains(g.Message, `"nope"`) {
		t.Fatalf("validation error = %+v", g)
	}
	g = e.gql(t, e.ownerKey, `{ viewer { `, nil).fails(t)
	if g.Extensions["type"] != "graphql error" || g.Extensions["code"] != "GRAPHQL_PARSE_FAILED" {
		t.Fatalf("parse error = %+v", g)
	}
}

func TestUnauthenticatedType(t *testing.T) {
	e := newEnv(t)
	r := e.gql(t, "", `{ viewer { id } }`, nil)
	if r.Status != 200 || string(r.Data) != "null" {
		t.Fatalf("status %d data %s", r.Status, r.Data)
	}
	r.fails(t).want(t, "AUTHENTICATION_ERROR", "authentication error", "Authentication required", "")
	r = e.gql(t, "lin_api_wrong", `{ viewer { id } }`, nil)
	if r.Status != 401 && (len(r.Errors) == 0 || r.Errors[0].Extensions["type"] != "authentication error") {
		t.Fatalf("wrong key: %d %s", r.Status, r.Raw)
	}
}
