package linearpayload

import (
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/milestone"
)

const (
	// dateTimeLayout is the payload date-time format: UTC, milliseconds, truncated.
	dateTimeLayout = "2006-01-02T15:04:05.000Z"
	// dateLayout is the format of date-only fields.
	dateLayout = "2006-01-02"
)

var priorityLabels = [5]string{"No priority", "Urgent", "High", "Medium", "Low"}

// PriorityLabel is the name of a priority 0-4; anything else reads as no priority.
func PriorityLabel(p int) string {
	if p < 0 || p >= len(priorityLabels) {
		return priorityLabels[0]
	}
	return priorityLabels[p]
}

// Status is one of the four fixed project statuses.
type Status struct{ ID, Name, Color, Type string }

// Statuses are the project statuses in order: one per milestone status.
var Statuses = [4]Status{
	{"00000000-0000-7000-8000-000000000001", "Planned", "#6B7280", "planned"},
	{"00000000-0000-7000-8000-000000000002", "In Progress", "#F59E0B", "started"},
	{"00000000-0000-7000-8000-000000000003", "Done", "#22C55E", "completed"},
	{"00000000-0000-7000-8000-000000000004", "Canceled", "#EF4444", "canceled"},
}

// statusOrder is the milestone status of each of Statuses.
var statusOrder = [4]milestone.Status{milestone.Planned, milestone.InProgress, milestone.Done, milestone.Canceled}

// StatusOf is the project status of a milestone status; an unknown one reads as Planned.
func StatusOf(s milestone.Status) Status {
	for i, o := range statusOrder {
		if o == s {
			return Statuses[i]
		}
	}
	return Statuses[0]
}

// StatusByID reads a project status id.
func StatusByID(id string) (milestone.Status, bool) {
	for i, s := range Statuses {
		if s.ID == id {
			return statusOrder[i], true
		}
	}
	return "", false
}

// BaseURL is the public URL without a trailing slash; "" for none.
func BaseURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	return strings.TrimSuffix(u.String(), "/")
}

// IssueURL is the link to an issue; base has no trailing slash.
func IssueURL(base, identifier string) string { return base + "/issue/" + identifier }

// MilestoneURL is the link to a milestone of the project with the given key.
func MilestoneURL(base, key string, id uuid.UUID) string {
	return base + "/p/" + key + "/milestones/" + id.String()
}

// ts formats a date-time.
func ts(t time.Time) string { return t.UTC().Format(dateTimeLayout) }

// tsp formats an optional date-time; nil stays null.
func tsp(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := ts(*t)
	return &s
}

// datep formats an optional date; nil stays null.
func datep(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(dateLayout)
	return &s
}

// nonEmpty is null for the empty string.
func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// idp formats an optional id; nil stays null.
func idp(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

// idStrings formats ids as a non-nil slice, so that none encodes as [].
func idStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// teamRef is {id, key, name} of a project.
type teamRef struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

// userRef is a user inside a payload.
type userRef struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Email     string  `json:"email"`
	URL       string  `json:"url"`
	AvatarURL *string `json:"avatarUrl"`
}

// stateRef is {id, name, type, color} of a workflow state or a project status.
type stateRef struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Color string `json:"color"`
}

// labelRef is a label inside an issue payload.
type labelRef struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Color    string  `json:"color"`
	ParentID *string `json:"parentId"`
}

// milestoneRef is a milestone inside an issue payload.
type milestoneRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}
