package linear

import (
	"strings"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/project"
)

// slug lower-cases s, turns runs of other characters than a-z and 0-9 into
// "-" and trims "-".
func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
			continue
		}
		dash = true
	}
	return b.String()
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// teamOut maps a project to a team.
func teamOut(p project.Project) *Team {
	return &Team{
		ID: p.ID.String(), Key: p.Key, Name: p.Name, DisplayName: p.Name, Description: nonEmpty(p.Description),
		Icon: p.Icon, Color: p.Color, ArchivedAt: p.ArchivedAt, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		Timezone: "Etc/UTC", IssueEstimationType: "notUsed", Visibility: TeamVisibilityPublic,
		Src: p,
	}
}

// stateOut maps a project state.
func stateOut(s project.State) *WorkflowState {
	return &WorkflowState{
		ID: s.ID.String(), Name: s.Name, Color: s.Color, Type: string(s.Type), Position: float64(s.Position),
		CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, ProjectID: s.ProjectID,
	}
}

// organizationOut maps the workspace; users are all workspace users.
func organizationOut(ws account.Workspace, users []account.User, issueCount int) *Organization {
	active := 0
	for _, u := range users {
		if !u.Disabled {
			active++
		}
	}
	urlKey := slug(ws.Name)
	if urlKey == "" {
		urlKey = "workspace"
	}
	return &Organization{
		ID: ws.ID.String(), Name: ws.Name, URLKey: urlKey, CreatedAt: ws.CreatedAt, UpdatedAt: ws.CreatedAt,
		UserCount: active, CreatedIssueCount: issueCount, ProjectStatuses: projectStatuses(ws),
		InitiativeUpdateRemindersDay: DayMonday, ProjectUpdateRemindersDay: DayMonday,
		ReleaseChannel: ReleaseChannelPublic, ProjectUpdatesReminderFrequency: ProjectUpdateReminderFrequencyNever,
		SLADayCount:         SLADayCountTypeAll,
		AllowedAuthServices: []string{}, PreviousURLKeys: []string{},
	}
}
