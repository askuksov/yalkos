package plane

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Report says what an import created, found already imported and skipped.
// Issues are named only by KEY-N: no title or text of an issue appears in it.
type Report struct {
	Source            Source             `json:"source"`
	DryRun            bool               `json:"dryRun"`
	Conflicts         []Conflict         `json:"conflicts"`
	Projects          []ProjectReport    `json:"projects"`
	Users             []UserReport       `json:"users"`
	Labels            LabelReport        `json:"labels"`
	RenamedMilestones []RenamedMilestone `json:"renamedMilestones"`
	RenamedStates     []RenamedMilestone `json:"renamedStates"`
	StateFallback     []string           `json:"stateFallback"`
	Renumbered        []Renumbered       `json:"renumbered"`
	ChangedInPlane    []ChangedIssue     `json:"changedInPlane"`
	Dropped           Dropped            `json:"dropped"`
	Attachments       AttachmentReport   `json:"attachments"`
	Cycles            int                `json:"cycles"`
	ArchivedProjects  []string           `json:"archivedProjects"`
	NotMember         []string           `json:"notMember"`
	// ArchivedInYalkos are projects an earlier import created that are
	// archived in Yalkos: nothing new is written into them.
	ArchivedInYalkos []string `json:"archivedInYalkos"`
}

// Source is the Plane instance and workspace read.
type Source struct {
	URL       string `json:"url"`
	Workspace string `json:"workspace"`
}

// Conflict is a Plane project identifier that cannot become a Yalkos key.
type Conflict struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// ProjectReport counts per project.
type ProjectReport struct {
	Key      string `json:"key"`
	Created  Counts `json:"created"`
	Existing Counts `json:"existing"`
	Skipped  Counts `json:"skipped"`
}

// Counts of entities of one project. Skipped states are triage states,
// skipped comments are deleted ones, skipped attachments failed or too large;
// skipped milestones and issues are new ones of a project archived in Yalkos.
type Counts struct {
	Milestones  int `json:"milestones"`
	States      int `json:"states"`
	Issues      int `json:"issues"`
	Comments    int `json:"comments"`
	Attachments int `json:"attachments"`
}

// UserReport is a user the import creates.
type UserReport struct {
	Email    string `json:"email"`
	Created  bool   `json:"created"`
	Disabled bool   `json:"disabled"`
}

// LabelReport lists label merges and mappings.
type LabelReport struct {
	// Merged are labels found in more than one project, with their colours.
	Merged []MergedLabel `json:"merged"`
	// TypeMapped are Plane labels that became Type labels.
	TypeMapped []TypeMapped `json:"typeMapped"`
	// TypeConflicts are issues with more than one Type label: the first by
	// precedence wins, the others stay as ordinary labels.
	TypeConflicts []string `json:"typeConflicts"`
	// Flattened are labels deeper than a group's children, moved into the group.
	Flattened []FlattenedLabel `json:"flattened"`
	Renamed   []RenamedLabel   `json:"renamed"`
}

// RenamedLabel is a label created under another name: its own clashed with a
// Yalkos label of another kind.
type RenamedLabel struct {
	Plane  string `json:"plane"`
	Yalkos string `json:"yalkos"`
}

// MergedLabel is one label made of same-named Plane labels.
type MergedLabel struct {
	Name     string   `json:"name"`
	Projects []string `json:"projects"`
	Colors   []string `json:"colors"`
}

// TypeMapped is a Plane label mapped to a Type label, with its issue count.
type TypeMapped struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Issues int    `json:"issues"`
}

// FlattenedLabel is a label placed directly into its top-level group.
type FlattenedLabel struct {
	Name  string `json:"name"`
	Group string `json:"group"`
}

// RenamedMilestone is a Plane module (or, in renamedStates, a Plane state)
// whose name clashed in its project.
type RenamedMilestone struct {
	Project string `json:"project"`
	Plane   string `json:"plane"`
	Yalkos  string `json:"yalkos"`
}

// Renumbered is a Plane issue whose number was taken in Yalkos.
type Renumbered struct {
	Plane  string `json:"plane"`
	Yalkos string `json:"yalkos"`
}

// ChangedIssue is an imported issue that changed in Plane afterwards.
type ChangedIssue struct {
	Key           string `json:"key"`
	CommentsAdded int    `json:"commentsAdded"`
}

// Dropped lists the issues (and modules) that lost data Yalkos has no place for.
type Dropped struct {
	StartDates       []string       `json:"startDates"`
	ExtraAssignees   []string       `json:"extraAssignees"`
	ExtraModules     []string       `json:"extraModules"`
	ModuleStartDates []string       `json:"moduleStartDates"`
	LabelsOver20     []string       `json:"labelsOver20"`
	UnknownTags      map[string]int `json:"unknownTags"`
	CutTexts         []string       `json:"cutTexts"`
	// Parents are issues whose Plane parent is not imported.
	Parents []string `json:"parents"`
	// GroupLabels are issues that carried a Plane label that became a group.
	GroupLabels []string `json:"groupLabels"`
	// DeletedInYalkos are entities an earlier import created that were deleted
	// in Yalkos since; they are not brought back, and nothing is added to them.
	DeletedInYalkos []string `json:"deletedInYalkos"`
}

// AttachmentReport lists files that were not copied.
type AttachmentReport struct {
	Failed   []FailedAttachment `json:"failed"`
	TooLarge []string           `json:"tooLarge"`
}

// FailedAttachment is a file whose download failed.
type FailedAttachment struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// orEmpty returns s, or an empty slice for nil, so JSON shows [] not null.
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// filled is a copy of r with every nil list replaced by an empty one.
func (r *Report) filled() Report {
	c := *r
	c.Conflicts = orEmpty(c.Conflicts)
	c.Projects = orEmpty(c.Projects)
	c.Users = orEmpty(c.Users)
	c.Labels.Merged = orEmpty(c.Labels.Merged)
	for i, m := range c.Labels.Merged {
		m.Projects, m.Colors = orEmpty(m.Projects), orEmpty(m.Colors)
		c.Labels.Merged[i] = m
	}
	c.Labels.TypeMapped = orEmpty(c.Labels.TypeMapped)
	c.Labels.TypeConflicts = orEmpty(c.Labels.TypeConflicts)
	c.Labels.Flattened = orEmpty(c.Labels.Flattened)
	c.Labels.Renamed = orEmpty(c.Labels.Renamed)
	c.RenamedMilestones = orEmpty(c.RenamedMilestones)
	c.RenamedStates = orEmpty(c.RenamedStates)
	c.StateFallback = orEmpty(c.StateFallback)
	c.Renumbered = orEmpty(c.Renumbered)
	c.ChangedInPlane = orEmpty(c.ChangedInPlane)
	d := &c.Dropped
	d.StartDates, d.ExtraAssignees, d.ExtraModules = orEmpty(d.StartDates), orEmpty(d.ExtraAssignees), orEmpty(d.ExtraModules)
	d.ModuleStartDates, d.LabelsOver20, d.CutTexts = orEmpty(d.ModuleStartDates), orEmpty(d.LabelsOver20), orEmpty(d.CutTexts)
	d.Parents, d.GroupLabels = orEmpty(d.Parents), orEmpty(d.GroupLabels)
	d.DeletedInYalkos = orEmpty(d.DeletedInYalkos)
	if d.UnknownTags == nil {
		d.UnknownTags = map[string]int{}
	}
	c.Attachments.Failed = orEmpty(c.Attachments.Failed)
	c.Attachments.TooLarge = orEmpty(c.Attachments.TooLarge)
	c.ArchivedProjects = orEmpty(c.ArchivedProjects)
	c.NotMember = orEmpty(c.NotMember)
	c.ArchivedInYalkos = orEmpty(c.ArchivedInYalkos)
	return c
}

// WriteJSON writes the report as indented JSON.
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r.filled())
}

// WriteText writes the report for a person: counts first, then every
// non-empty list.
func (r *Report) WriteText(w io.Writer) {
	tw := &textWriter{w: w}
	src := "Plane"
	if r.Source.Workspace != "" {
		src = "Plane workspace " + r.Source.Workspace
		if r.Source.URL != "" {
			src += " at " + r.Source.URL
		}
	}
	if r.DryRun {
		tw.line("Dry run of an import from %s: nothing was written.", src)
	} else {
		tw.line("Import from %s.", src)
	}

	if len(r.Conflicts) > 0 {
		tw.line("")
		tw.line("Conflicts, nothing can be written until they are resolved:")
		for _, c := range r.Conflicts {
			tw.line("  %s: %s", c.Key, c.Reason)
		}
	}

	if len(r.Projects) > 0 {
		tw.line("")
		tw.line("Projects (new / already imported / skipped):")
		for _, p := range r.Projects {
			tw.line("  %s", p.Key)
			tw.counts("issues", p.Created.Issues, p.Existing.Issues, p.Skipped.Issues)
			tw.counts("states", p.Created.States, p.Existing.States, p.Skipped.States)
			tw.counts("milestones", p.Created.Milestones, p.Existing.Milestones, p.Skipped.Milestones)
			tw.counts("comments", p.Created.Comments, p.Existing.Comments, p.Skipped.Comments)
			tw.counts("attachments", p.Created.Attachments, p.Existing.Attachments, p.Skipped.Attachments)
		}
	}

	if len(r.Users) > 0 {
		tw.line("")
		tw.line("New users:")
		for _, u := range r.Users {
			state := "enabled"
			if u.Disabled {
				state = "disabled"
			}
			tw.line("  %s (%s)", u.Email, state)
		}
	}

	if len(r.Labels.Merged) > 0 {
		tw.line("")
		tw.line("Labels merged across projects:")
		for _, m := range r.Labels.Merged {
			tw.line("  %s: %s; colours %s", m.Name, strings.Join(m.Projects, ", "), strings.Join(m.Colors, ", "))
		}
	}
	if len(r.Labels.TypeMapped) > 0 {
		tw.line("")
		tw.line("Plane labels that became Type labels:")
		for _, m := range r.Labels.TypeMapped {
			tw.line("  %s -> %s, issues: %d", m.Name, m.Type, m.Issues)
		}
	}
	tw.list("Issues with more than one Type label (the others kept as labels)", r.Labels.TypeConflicts)
	if len(r.Labels.Flattened) > 0 {
		tw.line("")
		tw.line("Labels moved into their top-level group:")
		for _, f := range r.Labels.Flattened {
			tw.line("  %s -> group %s", f.Name, f.Group)
		}
	}
	if len(r.Labels.Renamed) > 0 {
		tw.line("")
		tw.line("Labels renamed (name taken by a Yalkos label of another kind):")
		for _, l := range r.Labels.Renamed {
			tw.line("  %s -> %s", l.Plane, l.Yalkos)
		}
	}
	tw.renamed("Plane modules renamed (name taken in the project)", r.RenamedMilestones)
	tw.renamed("Plane states renamed (name taken in the project)", r.RenamedStates)
	tw.list("Issues in a skipped or unknown state, put in the default state", r.StateFallback)
	if len(r.Renumbered) > 0 {
		tw.line("")
		tw.line("Issues renumbered (number taken in Yalkos):")
		for _, n := range r.Renumbered {
			tw.line("  %s (Plane) -> %s", n.Plane, n.Yalkos)
		}
	}
	if len(r.ChangedInPlane) > 0 {
		tw.line("")
		tw.line("Issues changed in Plane after import (fields left as they are):")
		for _, c := range r.ChangedInPlane {
			tw.line("  %s, comments added: %d", c.Key, c.CommentsAdded)
		}
	}

	d := r.Dropped
	tw.list("Start dates dropped", d.StartDates)
	tw.list("Extra assignees dropped (first one kept)", d.ExtraAssignees)
	tw.list("Extra Plane modules dropped (earliest kept)", d.ExtraModules)
	tw.list("Plane module start dates dropped", d.ModuleStartDates)
	tw.list("Labels over 20 dropped", d.LabelsOver20)
	tw.list("Texts cut to the Yalkos limit", d.CutTexts)
	tw.list("Parents not imported (issue imported without parent)", d.Parents)
	tw.list("Plane group labels dropped from issues", d.GroupLabels)
	tw.list("Deleted in Yalkos after an earlier import, not brought back", d.DeletedInYalkos)
	if len(d.UnknownTags) > 0 {
		tw.line("")
		tw.line("Unknown HTML tags kept as text:")
		tags := make([]string, 0, len(d.UnknownTags))
		for t := range d.UnknownTags {
			tags = append(tags, t)
		}
		slices.Sort(tags)
		for _, t := range tags {
			tw.line("  %s: %d", t, d.UnknownTags[t])
		}
	}

	if len(r.Attachments.Failed) > 0 {
		tw.line("")
		tw.line("Attachments not copied:")
		for _, f := range r.Attachments.Failed {
			tw.line("  %s %s: %s", f.Key, f.Name, f.Reason)
		}
	}
	tw.list("Attachments too large", r.Attachments.TooLarge)
	if r.Cycles > 0 {
		tw.line("")
		tw.line("Plane cycles skipped: %d", r.Cycles)
	}
	tw.list("Archived Plane projects skipped", r.ArchivedProjects)
	tw.list("Plane projects skipped, the API key's owner is not a member", r.NotMember)
	tw.list("Projects archived in Yalkos, nothing new added", r.ArchivedInYalkos)
}

// textWriter writes lines; a write error is kept and later writes are skipped.
type textWriter struct {
	w   io.Writer
	err error
}

func (t *textWriter) line(format string, args ...any) {
	if t.err != nil {
		return
	}
	_, t.err = fmt.Fprintf(t.w, format+"\n", args...)
}

func (t *textWriter) counts(name string, created, existing, skipped int) {
	if created == 0 && existing == 0 && skipped == 0 {
		return
	}
	t.line("    %-12s %d / %d / %d", name, created, existing, skipped)
}

func (t *textWriter) renamed(title string, items []RenamedMilestone) {
	if len(items) == 0 {
		return
	}
	t.line("")
	t.line("%s:", title)
	for _, m := range items {
		t.line("  %s: %s -> %s", m.Project, m.Plane, m.Yalkos)
	}
}

func (t *textWriter) list(title string, items []string) {
	if len(items) == 0 {
		return
	}
	t.line("")
	t.line("%s: %s", title, strings.Join(items, ", "))
}
