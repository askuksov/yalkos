package linear

import (
	"strings"

	"github.com/askuksov/yalkos/internal/domain/attachment"
	"github.com/askuksov/yalkos/internal/domain/issue"
)

var priorityLabels = [5]string{"No priority", "Urgent", "High", "Medium", "Low"}

const maxBranchName = 60

// branchName is the lower-cased identifier and the title slug; longer than
// 60 characters it is cut at the last "-" of the title part (a hard cut when
// the title part has none).
func branchName(identifier, title string) string {
	prefix := strings.ToLower(identifier)
	s := prefix
	if t := slug(title); t != "" {
		s += "-" + t
	}
	if len(s) <= maxBranchName {
		return s
	}
	if i := strings.LastIndex(s[:maxBranchName+1], "-"); i > len(prefix) {
		return s[:i]
	}
	return s[:maxBranchName]
}

func priorityLabel(p int) string {
	if p < 0 || p >= len(priorityLabels) {
		return priorityLabels[0]
	}
	return priorityLabels[p]
}

// issueOut maps an issue; relations are resolved from Src.
func (r *Resolver) issueOut(i issue.Issue) *Issue {
	out := &Issue{
		ID: i.ID.String(), Identifier: i.Identifier, Number: float64(i.Number), Title: i.Title,
		Description: nonEmpty(i.Description), Priority: float64(i.Priority), PriorityLabel: priorityLabel(i.Priority),
		SortOrder: i.SortOrder, BoardOrder: i.SortOrder, PrioritySortOrder: i.SortOrder,
		URL: r.d.base() + "/issue/" + i.Identifier, BranchName: branchName(i.Identifier, i.Title),
		DueDate: i.DueDate, CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt, CompletedAt: i.CompletedAt, CanceledAt: i.CanceledAt,
		PreviousIdentifiers: []string{}, Reactions: []Reaction{}, ReactionData: JSONObject{V: []any{}},
		SharedAccess: &IssueSharedAccess{DisallowedIssueFields: []IssueSharedAccessDisallowedField{}, SharedWithUsers: []User{}},
		Src:          i,
	}
	if i.ParentID != nil {
		so := i.SortOrder
		out.SubIssueSortOrder = &so
	}
	return out
}

// attachmentOut maps an uploaded file.
func (r *Resolver) attachmentOut(a attachment.Attachment) *Attachment {
	return &Attachment{
		ID: a.ID.String(), Title: a.Name, URL: r.d.base() + attachment.URL(a),
		Metadata:  JSONObject{V: map[string]any{"size": a.Size, "contentType": a.ContentType}},
		CreatedAt: a.CreatedAt, UpdatedAt: a.CreatedAt, Src: a,
	}
}
