package linear

import (
	"context"
	"errors"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/graphql/gqlerr"
)

// userErr is an input error raised by this layer: its message is final.
type userErr struct{ field, msg string }

func (e *userErr) Error() string { return e.msg }

// badArg reports an invalid argument with a complete message.
func badArg(field, msg string) error { return &userErr{field: field, msg: msg} }

// errorTypes are the error types clients read to pick an error class.
var errorTypes = map[string]string{
	"AUTHENTICATION_ERROR":      "authentication error",
	"FORBIDDEN":                 "forbidden",
	"INVALID_INPUT":             "invalid input",
	"NOT_FOUND":                 "invalid input",
	"GRAPHQL_VALIDATION_FAILED": "graphql error",
	"GRAPHQL_PARSE_FAILED":      "graphql error",
	"COMPLEXITY_LIMIT_EXCEEDED": "graphql error",
	"RATELIMITED":               "ratelimited",
	"RATE_LIMITED":              "ratelimited",
	gqlerr.CodeInternal:         "internal error",
}

// fieldNames maps domain field names to the names used here.
var fieldNames = map[string]string{"projectId": "teamId", "milestoneId": "projectId"}

// entityNames maps not-found messages of domain entities.
var entityNames = map[string]string{
	"Project not found":   "Team not found",
	"Milestone not found": "Project not found",
	"State not found":     "WorkflowState not found",
	"Label not found":     "IssueLabel not found",
}

// domainMessages maps domain messages that name domain entities.
var domainMessages = map[string]string{
	"milestone belongs to another project": "project belongs to another team",
	"no such milestone":                    "no such project",
	"no such project":                      "no such team",
	"project is archived":                  "team is archived",
	"choose a project":                     "choose a team",
	"issue is already in this project":     "issue is already in this team",
	"parent belongs to another project":    "parent belongs to another team",
	"state belongs to another project":     "state belongs to another team",
	"a milestone with this name exists":    "a project with this name exists in this team",
}

// presenter builds errors as clients of this API expect them: an error type
// they map to an error class, a user-presentable message, and field, entity
// and message names in this API's terms.
func presenter(logf func(context.Context, error)) graphql.ErrorPresenterFunc {
	base := gqlerr.Presenter(logf)
	return func(ctx context.Context, err error) *gqlerror.Error {
		var ue *userErr
		var out *gqlerror.Error
		if errors.As(err, &ue) {
			out = &gqlerror.Error{Path: graphql.GetPath(ctx), Message: ue.msg, Extensions: map[string]any{
				"code": "INVALID_INPUT", "userError": true, "field": ue.field, "reason": string(kit.ReasonInvalidFormat),
			}}
		} else {
			out = base(ctx, err)
		}
		if out.Extensions == nil {
			out.Extensions = map[string]any{}
		}
		code, _ := out.Extensions["code"].(string)
		switch {
		case code == "INVALID_INPUT" && ue == nil:
			field, _ := out.Extensions["field"].(string)
			if f, ok := fieldNames[field]; ok {
				field = f
				out.Extensions["field"] = f
			}
			msg := out.Message
			if m, ok := domainMessages[msg]; ok {
				msg = m
			}
			out.Message = "Invalid " + field + ": " + msg
		case code == "NOT_FOUND":
			if m, ok := entityNames[out.Message]; ok {
				out.Message = m
			}
		}
		if t, ok := errorTypes[code]; ok {
			out.Extensions["type"] = t
		}
		if out.Extensions["userError"] == true {
			out.Extensions["userPresentableMessage"] = out.Message
		}
		return out
	}
}
