package linearpayload

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/webhook"
	"github.com/askuksov/yalkos/internal/store/q"
)

// projectColor is the colour of every project (as in the Linear-compatible API).
const projectColor = "#6B7280"

// projectUnset holds the fields of ProjectWebhookPayload a milestone has no
// value for: they are present and null.
type projectUnset struct {
	ArchivedAt                          any `json:"archivedAt"`
	AutoArchivedAt                      any `json:"autoArchivedAt"`
	CanceledAt                          any `json:"canceledAt"`
	CompletedAt                         any `json:"completedAt"`
	ConvertedFromIssueID                any `json:"convertedFromIssueId"`
	DocumentContentID                   any `json:"documentContentId"`
	Health                              any `json:"health"`
	HealthUpdatedAt                     any `json:"healthUpdatedAt"`
	Icon                                any `json:"icon"`
	Identifier                          any `json:"identifier"`
	Initiatives                         any `json:"initiatives"`
	LastAppliedTemplateID               any `json:"lastAppliedTemplateId"`
	LastUpdateID                        any `json:"lastUpdateId"`
	Milestones                          any `json:"milestones"`
	PreviousIdentifiers                 any `json:"previousIdentifiers"`
	ProjectUpdateRemindersPausedUntilAt any `json:"projectUpdateRemindersPausedUntilAt"`
	StartDate                           any `json:"startDate"`
	StartDateResolution                 any `json:"startDateResolution"`
	StartedAt                           any `json:"startedAt"`
	SyncedWith                          any `json:"syncedWith"`
	TargetDateResolution                any `json:"targetDateResolution"`
}

// projectData is ProjectWebhookPayload.
type projectData struct {
	ID                         string    `json:"id"`
	Name                       string    `json:"name"`
	Description                string    `json:"description"`
	Content                    *string   `json:"content"`
	SlugID                     string    `json:"slugId"`
	URL                        string    `json:"url"`
	SortOrder                  float64   `json:"sortOrder"`
	PrioritySortOrder          float64   `json:"prioritySortOrder"`
	StatusID                   string    `json:"statusId"`
	Status                     stateRef  `json:"status"`
	LeadID                     *string   `json:"leadId"`
	Lead                       *userRef  `json:"lead"`
	CreatorID                  *string   `json:"creatorId"`
	TeamIDs                    []string  `json:"teamIds"`
	Color                      string    `json:"color"`
	Priority                   int       `json:"priority"`
	TargetDate                 *string   `json:"targetDate"`
	CreatedAt                  string    `json:"createdAt"`
	UpdatedAt                  string    `json:"updatedAt"`
	Trashed                    bool      `json:"trashed"`
	LabelIDs                   []string  `json:"labelIds"`
	MemberIDs                  []string  `json:"memberIds"`
	CompletedIssueCountHistory []float64 `json:"completedIssueCountHistory"`
	CompletedScopeHistory      []float64 `json:"completedScopeHistory"`
	InProgressScopeHistory     []float64 `json:"inProgressScopeHistory"`
	IssueCountHistory          []float64 `json:"issueCountHistory"`
	ScopeHistory               []float64 `json:"scopeHistory"`
	projectUnset
}

// progressOf counts a milestone's live issues; none counts as zero.
func progressOf(ctx context.Context, db *q.Queries, id uuid.UUID) (milestone.Progress, error) {
	rows, err := db.MilestoneProgress(ctx, []uuid.UUID{id})
	if err != nil {
		return milestone.Progress{}, fmt.Errorf("milestone progress: %w", err)
	}
	var p milestone.Progress
	for _, r := range rows {
		if r.MilestoneID == id {
			p = milestone.Progress{Completed: int(r.Completed), Total: int(r.Total), Active: int(r.Active)}
		}
	}
	return p, nil
}

// milestone builds a Project event: the milestone is the payload's project. A
// removed milestone is built while the row is still there.
func (b *Builder) milestone(ctx context.Context, db *q.Queries, c event.Change) (webhook.Built, error) {
	m, err := db.GetMilestone(ctx, c.EntityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return webhook.Built{}, fmt.Errorf("milestone %s not found", c.EntityID)
	}
	if err != nil {
		return webhook.Built{}, fmt.Errorf("get milestone: %w", err)
	}
	p, err := getProject(ctx, db, m.ProjectID)
	if err != nil {
		return webhook.Built{}, err
	}
	people, err := b.users(ctx, db, m.LeadID)
	if err != nil {
		return webhook.Built{}, err
	}
	prog, err := progressOf(ctx, db, m.ID)
	if err != nil {
		return webhook.Built{}, err
	}
	// The effective status: the pinned one, else the one the issues compute.
	status := milestone.AutoStatus(prog)
	if m.StatusManual != nil {
		status = milestone.Status(*m.StatusManual)
	}
	st := StatusOf(status)
	url := MilestoneURL(b.base, p.Key, m.ID)
	d := projectData{
		ID: m.ID.String(), Name: m.Name, Description: m.Description, Content: nonEmpty(m.Description),
		SlugID: m.ID.String(), URL: url, SortOrder: m.SortOrder, PrioritySortOrder: m.SortOrder,
		StatusID: st.ID, Status: stateRef{ID: st.ID, Name: st.Name, Type: st.Type, Color: st.Color},
		LeadID: idp(m.LeadID), Lead: userOf(people, m.LeadID), CreatorID: idp(m.CreatedBy),
		TeamIDs: []string{m.ProjectID.String()}, Color: projectColor, Priority: 0,
		TargetDate: datep(m.TargetDate), CreatedAt: ts(m.CreatedAt), UpdatedAt: ts(m.UpdatedAt),
		Trashed:  c.Action == event.Remove,
		LabelIDs: []string{}, MemberIDs: []string{},
		CompletedIssueCountHistory: []float64{}, CompletedScopeHistory: []float64{},
		InProgressScopeHistory: []float64{}, IssueCountHistory: []float64{}, ScopeHistory: []float64{},
	}
	var uf map[string]any
	if c.Action == event.Update {
		uf = milestoneUpdatedFrom(c, prog)
	}
	return built("Project", d, &url, uf)
}
