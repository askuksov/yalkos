package issue

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
	}
}
