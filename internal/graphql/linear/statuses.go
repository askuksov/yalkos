package linear

import (
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/webhook/linearpayload"
)

// projectStatuses lists the four project statuses in order; they date from the workspace.
func projectStatuses(ws account.Workspace) []ProjectStatus {
	out := make([]ProjectStatus, len(linearpayload.Statuses))
	for i, r := range linearpayload.Statuses {
		out[i] = ProjectStatus{
			ID: r.ID, Name: r.Name, Color: r.Color, Type: ProjectStatusType(r.Type), Position: float64(i),
			CreatedAt: ws.CreatedAt, UpdatedAt: ws.CreatedAt,
		}
	}
	return out
}
