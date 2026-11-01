package mcpserver

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/askuksov/yalkos/internal/domain/kit"
)

// userError is a message for the agent, returned as is.
type userError struct{ msg string }

func (e *userError) Error() string { return e.msg }

// errf builds a userError.
func errf(format string, a ...any) error { return &userError{msg: fmt.Sprintf(format, a...)} }

// inputParams maps domain input fields to tool fields.
var inputParams = map[string]string{
	"assigneeId":  "assignee",
	"stateId":     "state",
	"milestoneId": "milestone",
	"parentId":    "parent",
	"labelIds":    "labels",
	"projectId":   "project",
	"leadId":      "lead",
	"after":       "cursor",
	"before":      "cursor",
	"first":       "limit",
	"last":        "limit",
}

// userText turns a tool error into one sentence for the agent. Unexpected
// errors are logged and hidden behind a generic text.
func userText(err error, logger *slog.Logger) string {
	var ue *userError
	var ie *kit.InputError
	var nf kit.NotFoundError
	switch {
	case errors.As(err, &ue):
		return ue.msg
	case errors.As(err, &ie):
		param := ie.Field
		if p, ok := inputParams[param]; ok {
			param = p
		}
		return "Invalid " + param + ": " + strings.TrimSuffix(ie.Msg, ".") + "."
	case errors.As(err, &nf):
		return nf.Entity + " not found."
	case errors.Is(err, kit.ErrForbidden):
		return "You do not have permission to do this."
	}
	logger.Error("mcp tool failed", slog.String("error", err.Error()))
	return "Internal error; try again later."
}

// nameList joins names for an error text: at most 30, then "…"; "none" when empty.
func nameList(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	if len(names) > 30 {
		return strings.Join(names[:30], ", ") + ", …"
	}
	return strings.Join(names, ", ")
}
