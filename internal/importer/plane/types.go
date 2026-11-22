package plane

import (
	"encoding/json"
	"math"
	"time"
)

// Member is a workspace member as listed by Plane.
type Member struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
}

// Project is a Plane project. Workspace is the Plane workspace uuid.
type Project struct {
	ID              string     `json:"id"`
	Identifier      string     `json:"identifier"`
	Name            string     `json:"name"`
	DescriptionHTML string     `json:"description_html"`
	Description     string     `json:"description"`
	Workspace       string     `json:"workspace"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	ArchivedAt      *time.Time `json:"archived_at"`
	CreatedBy       string     `json:"created_by"`
}

// UnmarshalJSON accepts description_html and description as any JSON value,
// as Module does, so one odd project cannot fail the whole project list.
func (p *Project) UnmarshalJSON(data []byte) error {
	type plain Project
	// The raw fields sit above the embedded ones, so they take the values.
	aux := struct {
		*plain
		HTML json.RawMessage `json:"description_html"`
		Text json.RawMessage `json:"description"`
	}{plain: (*plain)(p)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	p.DescriptionHTML, p.Description = jsonString(aux.HTML), jsonString(aux.Text)
	return nil
}

// State is a workflow state of a project.
type State struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Color    string  `json:"color"`
	Group    string  `json:"group"`
	Sequence float64 `json:"sequence"`
	Default  bool    `json:"default"`
}

// Label is a project label; Parent points at its label group.
type Label struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Color     string  `json:"color"`
	Parent    *string `json:"parent"`
	SortOrder float64 `json:"sort_order"`
}

// Module is a Plane module.
type Module struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	DescriptionHTML string    `json:"description_html"`
	Description     string    `json:"description"`
	Status          string    `json:"status"`
	StartDate       *string   `json:"start_date"`
	TargetDate      *string   `json:"target_date"`
	Lead            *string   `json:"lead"`
	SortOrder       float64   `json:"sort_order"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	CreatedBy       string    `json:"created_by"`
}

// UnmarshalJSON accepts description_html and description as a JSON string,
// null or any other JSON value: Plane stores the first in a JSON field, and
// only a string is usable.
func (m *Module) UnmarshalJSON(data []byte) error {
	type plain Module
	// The raw fields sit above the embedded ones, so they take the values.
	aux := struct {
		*plain
		HTML json.RawMessage `json:"description_html"`
		Text json.RawMessage `json:"description"`
	}{plain: (*plain)(m)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	m.DescriptionHTML, m.Description = jsonString(aux.HTML), jsonString(aux.Text)
	return nil
}

// jsonString is the value of a JSON string; "" for anything else.
func jsonString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// WorkItem is a Plane work item (an issue).
type WorkItem struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	DescriptionHTML string     `json:"description_html"`
	Priority        string     `json:"priority"`
	State           string     `json:"state"`
	SequenceID      int        `json:"sequence_id"`
	SortOrder       float64    `json:"sort_order"`
	Parent          *string    `json:"parent"`
	Assignees       []string   `json:"assignees"`
	Labels          []string   `json:"labels"`
	StartDate       *string    `json:"start_date"`
	TargetDate      *string    `json:"target_date"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	CompletedAt     *time.Time `json:"completed_at"`
	CreatedBy       string     `json:"created_by"`
}

// Comment is a work item comment.
type Comment struct {
	ID          string     `json:"id"`
	CommentHTML string     `json:"comment_html"`
	Actor       string     `json:"actor"`
	Parent      *string    `json:"parent"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	EditedAt    *time.Time `json:"edited_at"`
	DeletedAt   *time.Time `json:"deleted_at"`
}

// Attachment is a file attached to a work item. IsUploaded is false for a
// file whose upload to Plane's storage never finished; it has nothing to copy.
type Attachment struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	IsUploaded bool      `json:"is_uploaded"`
	CreatedAt  time.Time `json:"created_at"`
	CreatedBy  string    `json:"created_by"`
}

// UnmarshalJSON reads the file name and size from Plane's attributes object,
// falling back to the top-level size (Plane 1.3 sends it as a float such as
// 173254.0). A missing is_uploaded counts as uploaded.
func (a *Attachment) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID         string    `json:"id"`
		Size       float64   `json:"size"`
		IsUploaded *bool     `json:"is_uploaded"`
		CreatedAt  time.Time `json:"created_at"`
		CreatedBy  string    `json:"created_by"`
		Attributes struct {
			Name string  `json:"name"`
			Size float64 `json:"size"`
		} `json:"attributes"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	size := wholeSize(raw.Attributes.Size)
	if size == 0 {
		size = wholeSize(raw.Size)
	}
	*a = Attachment{
		ID:         raw.ID,
		Name:       raw.Attributes.Name,
		Size:       size,
		IsUploaded: raw.IsUploaded == nil || *raw.IsUploaded,
		CreatedAt:  raw.CreatedAt,
		CreatedBy:  raw.CreatedBy,
	}
	return nil
}

// wholeSize turns a JSON number into a byte count, rounding a fraction up so
// the size limit never lets a file through by truncation. A negative or NaN
// value is 0.
func wholeSize(n float64) int64 {
	switch {
	case math.IsNaN(n) || n <= 0:
		return 0
	case n >= math.MaxInt64:
		return math.MaxInt64
	}
	return int64(math.Ceil(n))
}

// Link is a web link of a work item.
type Link struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// RelationRef points at a related work item, possibly in another project.
type RelationRef struct {
	ProjectID string `json:"project_id"`
	IssueID   string `json:"issue_id"`
}

// UnmarshalJSON also accepts the related work item's id alone, as some Plane
// versions list it; its project is then unknown, and the item is named only
// when it was imported or surveyed.
func (r *RelationRef) UnmarshalJSON(data []byte) error {
	var id string
	if json.Unmarshal(data, &id) == nil {
		*r = RelationRef{IssueID: id}
		return nil
	}
	type plain RelationRef
	return json.Unmarshal(data, (*plain)(r))
}

// Relations maps a relation kind (blocking, blocked_by, duplicate, relates_to,
// start_after, start_before, finish_after, finish_before) to its targets.
type Relations map[string][]RelationRef
