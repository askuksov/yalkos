package issue

import (
	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/store/q"
)

// CreatedEvent is the history entry of a new issue; key is its project's key
// and labelIDs the labels it was created with.
func CreatedEvent(i q.Issue, key string, labelIDs []uuid.UUID, actor *uuid.UUID) audit.Event {
	out := toIssue(i, key)
	out.LabelIDs = labelIDs
	return audit.Event{
		EntityType: "issue", Action: "create", EntityID: i.ID, ProjectID: &i.ProjectID, ActorID: actor,
		Changes: audit.Diff(nil, snapshot(out)), At: i.CreatedAt,
	}
}

// snapshot is the audited view of an issue, keyed by API field names.
func snapshot(i Issue) map[string]any {
	var due any
	if i.DueDate != nil {
		due = i.DueDate.Format("2006-01-02")
	}
	return map[string]any{
		"title":       i.Title,
		"description": i.Description,
		"stateId":     i.StateID,
		"priority":    i.Priority,
		"assigneeId":  i.AssigneeID,
		"dueDate":     due,
		"parentId":    i.ParentID,
		"sortOrder":   i.SortOrder,
		"projectId":   i.ProjectID,
		"number":      i.Number,
		"completedAt": i.CompletedAt,
		"canceledAt":  i.CanceledAt,
		"milestoneId": i.MilestoneID,
		"labelIds":    i.LabelIDs,
	}
}
