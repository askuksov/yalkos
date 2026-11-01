package linear

import "github.com/askuksov/yalkos/internal/domain/account"

// The four project statuses, one per milestone status, with fixed ids.
var statusRows = [4]struct {
	id, name, color string
	typ             ProjectStatusType
}{
	{"00000000-0000-7000-8000-000000000001", "Planned", "#6B7280", ProjectStatusTypePlanned},
	{"00000000-0000-7000-8000-000000000002", "In Progress", "#F59E0B", ProjectStatusTypeStarted},
	{"00000000-0000-7000-8000-000000000003", "Done", "#22C55E", ProjectStatusTypeCompleted},
	{"00000000-0000-7000-8000-000000000004", "Canceled", "#EF4444", ProjectStatusTypeCanceled},
}

// projectStatuses lists the statuses in order; they date from the workspace.
func projectStatuses(ws account.Workspace) []ProjectStatus {
	out := make([]ProjectStatus, len(statusRows))
	for i, r := range statusRows {
		out[i] = ProjectStatus{
			ID: r.id, Name: r.name, Color: r.color, Type: r.typ, Position: float64(i),
			CreatedAt: ws.CreatedAt, UpdatedAt: ws.CreatedAt,
		}
	}
	return out
}
