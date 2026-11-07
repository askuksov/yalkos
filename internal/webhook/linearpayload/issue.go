package linearpayload

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/webhook"
	"github.com/askuksov/yalkos/internal/store/q"
)

// issueUnset holds the fields of IssueWebhookPayload an issue has no value for:
// they are present and null.
type issueUnset struct {
	AddedToCycleAt           any `json:"addedToCycleAt"`
	AddedToProjectAt         any `json:"addedToProjectAt"`
	AddedToTeamAt            any `json:"addedToTeamAt"`
	ArchivedAt               any `json:"archivedAt"`
	AutoArchivedAt           any `json:"autoArchivedAt"`
	AutoClosedAt             any `json:"autoClosedAt"`
	BotActor                 any `json:"botActor"`
	Cycle                    any `json:"cycle"`
	CycleID                  any `json:"cycleId"`
	Delegate                 any `json:"delegate"`
	DelegateID               any `json:"delegateId"`
	DescriptionData          any `json:"descriptionData"`
	Estimate                 any `json:"estimate"`
	ExternalUserCreator      any `json:"externalUserCreator"`
	ExternalUserCreatorID    any `json:"externalUserCreatorId"`
	IntegrationSourceType    any `json:"integrationSourceType"`
	LastAppliedTemplateID    any `json:"lastAppliedTemplateId"`
	Milestone                any `json:"milestone"`
	ProjectMilestone         any `json:"projectMilestone"`
	ProjectMilestoneID       any `json:"projectMilestoneId"`
	RecurringIssueTemplateID any `json:"recurringIssueTemplateId"`
	SLABreachesAt            any `json:"slaBreachesAt"`
	SLAHighRiskAt            any `json:"slaHighRiskAt"`
	SLAMediumRiskAt          any `json:"slaMediumRiskAt"`
	SLAStartedAt             any `json:"slaStartedAt"`
	SLAType                  any `json:"slaType"`
	SnoozedUntilAt           any `json:"snoozedUntilAt"`
	SourceCommentID          any `json:"sourceCommentId"`
	StartedAt                any `json:"startedAt"`
	StartedTriageAt          any `json:"startedTriageAt"`
	SyncedWith               any `json:"syncedWith"`
	TriagedAt                any `json:"triagedAt"`
}

// issueData is IssueWebhookPayload.
type issueData struct {
	ID                  string        `json:"id"`
	Identifier          string        `json:"identifier"`
	Number              int           `json:"number"`
	Title               string        `json:"title"`
	Description         *string       `json:"description"`
	Priority            int           `json:"priority"`
	PriorityLabel       string        `json:"priorityLabel"`
	SortOrder           float64       `json:"sortOrder"`
	PrioritySortOrder   float64       `json:"prioritySortOrder"`
	SubIssueSortOrder   *float64      `json:"subIssueSortOrder"`
	URL                 string        `json:"url"`
	StateID             string        `json:"stateId"`
	State               stateRef      `json:"state"`
	TeamID              string        `json:"teamId"`
	Team                teamRef       `json:"team"`
	AssigneeID          *string       `json:"assigneeId"`
	Assignee            *userRef      `json:"assignee"`
	CreatorID           string        `json:"creatorId"`
	Creator             *userRef      `json:"creator"`
	ParentID            *string       `json:"parentId"`
	ProjectID           *string       `json:"projectId"`
	Project             *milestoneRef `json:"project"`
	LabelIDs            []string      `json:"labelIds"`
	Labels              []labelRef    `json:"labels"`
	DueDate             *string       `json:"dueDate"`
	CreatedAt           string        `json:"createdAt"`
	UpdatedAt           string        `json:"updatedAt"`
	CompletedAt         *string       `json:"completedAt"`
	CanceledAt          *string       `json:"canceledAt"`
	Trashed             bool          `json:"trashed"`
	PreviousIdentifiers []string      `json:"previousIdentifiers"`
	SubscriberIDs       []string      `json:"subscriberIds"`
	Releases            []any         `json:"releases"`
	ReactionData        []any         `json:"reactionData"`
	issueUnset
}

// getIssue reads an issue by id, a soft-deleted one included.
func getIssue(ctx context.Context, db *q.Queries, id uuid.UUID) (q.GetIssueForPayloadRow, error) {
	row, err := db.GetIssueForPayload(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, fmt.Errorf("issue %s not found", id)
	}
	if err != nil {
		return row, fmt.Errorf("get issue: %w", err)
	}
	return row, nil
}

// issue builds an Issue event. It reads the issue itself and what it points
// at (state, people, labels, milestone), never its sub-issues: a move reports
// the root before its descendants have moved.
func (b *Builder) issue(ctx context.Context, db *q.Queries, c event.Change) (webhook.Built, error) {
	row, err := getIssue(ctx, db, c.EntityID)
	if err != nil {
		return webhook.Built{}, err
	}
	i := row.Issue
	identifier := fmt.Sprintf("%s-%d", row.ProjectKey, i.Number)
	url := IssueURL(b.base, identifier)

	states, err := db.ListStatesByIDs(ctx, []uuid.UUID{i.StateID})
	if err != nil {
		return webhook.Built{}, fmt.Errorf("get issue state: %w", err)
	}
	if len(states) != 1 {
		return webhook.Built{}, fmt.Errorf("state %s not found", i.StateID)
	}
	people, err := b.users(ctx, db, &i.CreatorID, i.AssigneeID)
	if err != nil {
		return webhook.Built{}, err
	}
	labelIDs, labels, err := issueLabels(ctx, db, i.ID)
	if err != nil {
		return webhook.Built{}, err
	}
	ms, err := b.issueMilestone(ctx, db, row)
	if err != nil {
		return webhook.Built{}, err
	}

	st := states[0]
	d := issueData{
		ID: i.ID.String(), Identifier: identifier, Number: int(i.Number), Title: i.Title, Description: nonEmpty(i.Description),
		Priority: int(i.Priority), PriorityLabel: PriorityLabel(int(i.Priority)),
		SortOrder: i.SortOrder, PrioritySortOrder: i.SortOrder, URL: url,
		StateID: st.ID.String(), State: stateRef{ID: st.ID.String(), Name: st.Name, Type: st.Type, Color: st.Color},
		TeamID: i.ProjectID.String(), Team: teamRef{ID: i.ProjectID.String(), Key: row.ProjectKey, Name: row.ProjectName},
		AssigneeID: idp(i.AssigneeID), Assignee: userOf(people, i.AssigneeID),
		CreatorID: i.CreatorID.String(), Creator: userOf(people, &i.CreatorID),
		ParentID: idp(i.ParentID), ProjectID: idp(i.MilestoneID), Project: ms,
		LabelIDs: idStrings(labelIDs), Labels: labels,
		DueDate: datep(i.DueDate), CreatedAt: ts(i.CreatedAt), UpdatedAt: ts(i.UpdatedAt),
		CompletedAt: tsp(i.CompletedAt), CanceledAt: tsp(i.CanceledAt),
		// Only a soft delete can be undone; a project's delete takes its issues for good.
		Trashed:             c.Action == event.Remove && !c.Purged,
		PreviousIdentifiers: []string{}, SubscriberIDs: []string{}, Releases: []any{}, ReactionData: []any{},
	}
	if i.ParentID != nil {
		d.SubIssueSortOrder = &i.SortOrder
	}
	var uf map[string]any
	if c.Action == event.Update {
		if uf, err = issueUpdatedFrom(ctx, db, c, i.ProjectID, int(i.Number)); err != nil {
			return webhook.Built{}, err
		}
	}
	return built("Issue", d, &url, uf)
}

// issueLabels reads an issue's label ids (sorted) and the labels in that order.
func issueLabels(ctx context.Context, db *q.Queries, issueID uuid.UUID) ([]uuid.UUID, []labelRef, error) {
	rows, err := db.IssueLabelIDs(ctx, []uuid.UUID{issueID})
	if err != nil {
		return nil, nil, fmt.Errorf("issue labels: %w", err)
	}
	ids := make([]uuid.UUID, len(rows))
	for n, r := range rows {
		ids[n] = r.LabelID
	}
	labels := []labelRef{}
	if len(ids) == 0 {
		return ids, labels, nil
	}
	found, err := db.ListLabelsByIDs(ctx, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("list labels: %w", err)
	}
	byID := make(map[uuid.UUID]q.Label, len(found))
	for _, l := range found {
		byID[l.ID] = l
	}
	for _, id := range ids {
		l, ok := byID[id]
		if !ok {
			return nil, nil, fmt.Errorf("label %s not found", id)
		}
		labels = append(labels, labelRef{ID: l.ID.String(), Name: l.Name, Color: l.Color, ParentID: idp(l.ParentID)})
	}
	return ids, labels, nil
}

// issueMilestone is the issue's milestone as the payload's project; nil for none.
func (b *Builder) issueMilestone(ctx context.Context, db *q.Queries, row q.GetIssueForPayloadRow) (*milestoneRef, error) {
	if row.Issue.MilestoneID == nil {
		return nil, nil
	}
	ms, err := db.ListMilestonesByIDs(ctx, []uuid.UUID{*row.Issue.MilestoneID})
	if err != nil {
		return nil, fmt.Errorf("get issue milestone: %w", err)
	}
	if len(ms) != 1 {
		return nil, fmt.Errorf("milestone %s not found", *row.Issue.MilestoneID)
	}
	m := ms[0]
	key := row.ProjectKey
	if m.ProjectID != row.Issue.ProjectID {
		p, err := getProject(ctx, db, m.ProjectID)
		if err != nil {
			return nil, err
		}
		key = p.Key
	}
	return &milestoneRef{ID: m.ID.String(), Name: m.Name, URL: MilestoneURL(b.base, key, m.ID)}, nil
}
