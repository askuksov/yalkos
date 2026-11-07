package linearpayload_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/domain/milestone"
)

// The field names of the payload types, typed by hand from the published
// field lists (sorted).
var (
	issueFields = []string{
		"addedToCycleAt", "addedToProjectAt", "addedToTeamAt", "archivedAt", "assignee", "assigneeId",
		"autoArchivedAt", "autoClosedAt", "botActor", "canceledAt", "completedAt", "createdAt", "creator",
		"creatorId", "cycle", "cycleId", "delegate", "delegateId", "description", "descriptionData", "dueDate",
		"estimate", "externalUserCreator", "externalUserCreatorId", "id", "identifier", "integrationSourceType",
		"labelIds", "labels", "lastAppliedTemplateId", "milestone", "number", "parentId", "previousIdentifiers",
		"priority", "priorityLabel", "prioritySortOrder", "project", "projectId", "projectMilestone",
		"projectMilestoneId", "reactionData", "recurringIssueTemplateId", "releases", "slaBreachesAt",
		"slaHighRiskAt", "slaMediumRiskAt", "slaStartedAt", "slaType", "snoozedUntilAt", "sortOrder",
		"sourceCommentId", "startedAt", "startedTriageAt", "state", "stateId", "subIssueSortOrder",
		"subscriberIds", "syncedWith", "team", "teamId", "title", "trashed", "triagedAt", "updatedAt", "url",
	}
	commentFields = []string{
		"archivedAt", "body", "botActor", "createdAt", "documentContent", "documentContentId", "editedAt",
		"externalUser", "externalUserId", "id", "initiativeId", "initiativeUpdate", "initiativeUpdateId", "issue",
		"issueId", "parent", "parentId", "postId", "projectId", "projectUpdate", "projectUpdateId", "quotedText",
		"reactionData", "resolvedAt", "resolvingCommentId", "resolvingUserId", "syncedWith", "updatedAt", "user",
		"userId",
	}
	labelFields = []string{
		"archivedAt", "color", "createdAt", "creatorId", "description", "id", "inheritedFromId", "isGroup", "name",
		"parentId", "teamId", "updatedAt",
	}
	projectFields = []string{
		"archivedAt", "autoArchivedAt", "canceledAt", "color", "completedAt", "completedIssueCountHistory",
		"completedScopeHistory", "content", "convertedFromIssueId", "createdAt", "creatorId", "description",
		"documentContentId", "health", "healthUpdatedAt", "icon", "id", "identifier", "inProgressScopeHistory",
		"initiatives", "issueCountHistory", "labelIds", "lastAppliedTemplateId", "lastUpdateId", "lead", "leadId",
		"memberIds", "milestones", "name", "previousIdentifiers", "priority", "prioritySortOrder",
		"projectUpdateRemindersPausedUntilAt", "scopeHistory", "slugId", "sortOrder", "startDate",
		"startDateResolution", "startedAt", "status", "statusId", "syncedWith", "targetDate",
		"targetDateResolution", "teamIds", "trashed", "updatedAt", "url",
	}
)

// wantFields checks that data has exactly the keys in want, no more, no fewer.
func wantFields(t *testing.T, data json.RawMessage, want []string) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	var got []string
	for k := range m {
		got = append(got, k)
	}
	slices.Sort(got)
	var missing, extra []string
	for _, k := range want {
		if !slices.Contains(got, k) {
			missing = append(missing, k)
		}
	}
	for _, k := range got {
		if !slices.Contains(want, k) {
			extra = append(extra, k)
		}
	}
	if len(missing) > 0 || len(extra) > 0 {
		t.Errorf("fields: missing %v, extra %v", missing, extra)
	}
}

func TestFieldListsAreComplete(t *testing.T) {
	for name, c := range map[string]struct {
		list []string
		n    int
	}{"issue": {issueFields, 66}, "comment": {commentFields, 30}, "label": {labelFields, 12}, "project": {projectFields, 48}} {
		if len(c.list) != c.n {
			t.Errorf("%s list has %d names, want %d", name, len(c.list), c.n)
		}
		if s := slices.Sorted(slices.Values(c.list)); !slices.Equal(s, c.list) || len(slices.Compact(slices.Clone(s))) != len(s) {
			t.Errorf("%s list is unsorted or has duplicates", name)
		}
	}
}

func TestIssueDataFieldSet(t *testing.T) {
	e := newEnv(t)
	e.noDefaultType(t)
	bug := e.label(t, label.CreateInput{Name: "bug", Color: "#ef4444"})
	e.watch()

	i := e.issue(t, issue.CreateInput{Title: "bare"})
	b := e.only(t).Built
	wantFields(t, b.Data, issueFields)
	// Raw bytes: empty arrays are [] and unset values null, never swapped.
	wantRaw(t, b.Data, `"labelIds":[]`, `"labels":[]`, `"reactionData":[]`, `"previousIdentifiers":[]`,
		`"subscriberIds":[]`, `"releases":[]`, `"estimate":null`, `"assignee":null`, `"assigneeId":null`,
		`"project":null`, `"projectId":null`, `"parentId":null`, `"description":null`, `"dueDate":null`,
		`"projectMilestone":null`, `"milestone":null`, `"cycle":null`, `"delegate":null`, `"startedAt":null`,
		`"archivedAt":null`, `"botActor":null`, `"descriptionData":null`, `"syncedWith":null`, `"snoozedUntilAt":null`)
	wantNoRaw(t, b.Data, `"labelIds":null`, `"labels":null`, `"reactionData":null`, `"estimate":0`, `"estimate":[]`)

	// Positive control: with a label the array is filled, not null or [].
	e.reset()
	if _, err := e.issues.Update(context.Background(), e.users.Member, i.ID, issue.Patch{AddLabelIDs: []uuid.UUID{bug.ID}}); err != nil {
		t.Fatal(err)
	}
	b = e.only(t).Built
	wantFields(t, b.Data, issueFields)
	wantRaw(t, b.Data, `"labelIds":[`+js(bug.ID.String())+`]`, `"labels":[{`)
	wantNoRaw(t, b.Data, `"labelIds":[]`, `"labels":[]`)
}

func TestCommentDataFieldSet(t *testing.T) {
	e := newEnv(t)
	i := e.issue(t, issue.CreateInput{Title: "t"})
	e.watch()

	if _, err := e.comments.Create(context.Background(), e.users.Member, i.ID, "hi", nil); err != nil {
		t.Fatal(err)
	}
	b := e.only(t).Built
	wantFields(t, b.Data, commentFields)
	wantRaw(t, b.Data, `"reactionData":[]`, `"editedAt":null`, `"parent":null`, `"parentId":null`, `"projectId":null`,
		`"resolvedAt":null`, `"quotedText":null`, `"externalUser":null`, `"documentContent":null`, `"syncedWith":null`)
	wantNoRaw(t, b.Data, `"reactionData":null`)
}

func TestLabelDataFieldSet(t *testing.T) {
	e := newEnv(t)
	e.watch()

	e.label(t, label.CreateInput{Name: "bug", Color: "#ef4444"})
	b := e.only(t).Built
	wantFields(t, b.Data, labelFields)
	wantRaw(t, b.Data, `"description":null`, `"parentId":null`, `"teamId":null`, `"inheritedFromId":null`,
		`"archivedAt":null`, `"isGroup":false`)

	// Positive control: set values are not null.
	e.reset()
	e.label(t, label.CreateInput{Name: "Area", Color: "#111111", Description: "about", IsGroup: true})
	b = e.only(t).Built
	wantRaw(t, b.Data, `"description":"about"`, `"isGroup":true`)
	wantNoRaw(t, b.Data, `"description":null`)
}

func TestProjectDataFieldSet(t *testing.T) {
	e := newEnv(t)
	e.watch()

	e.milestone(t, "Beta", milestone.CreateInput{})
	b := e.only(t).Built
	wantFields(t, b.Data, projectFields)
	// Required arrays are []; the nullable lists and every unsupported field are null.
	wantRaw(t, b.Data, `"labelIds":[]`, `"memberIds":[]`, `"completedIssueCountHistory":[]`, `"completedScopeHistory":[]`,
		`"inProgressScopeHistory":[]`, `"issueCountHistory":[]`, `"scopeHistory":[]`,
		`"initiatives":null`, `"milestones":null`, `"previousIdentifiers":null`, `"content":null`, `"description":""`,
		`"lead":null`, `"leadId":null`, `"targetDate":null`, `"startDate":null`, `"icon":null`, `"health":null`,
		`"identifier":null`, `"syncedWith":null`, `"archivedAt":null`)
	wantNoRaw(t, b.Data, `"labelIds":null`, `"scopeHistory":null`, `"initiatives":[]`, `"milestones":[]`)

	// Positive control: a description gives content text, not null.
	e.reset()
	e.milestone(t, "Other", milestone.CreateInput{Description: "text"})
	b = e.only(t).Built
	wantRaw(t, b.Data, `"content":"text"`, `"description":"text"`)
	wantNoRaw(t, b.Data, `"content":null`)
}
