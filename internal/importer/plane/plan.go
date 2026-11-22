package plane

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/project"
)

// Plan is what an import of a surveyed workspace creates, worked out without
// reading or writing anything else. Entities that an earlier import created
// carry their Target; the rest are created.
type Plan struct {
	// Conflicts are Plane identifiers that cannot become Yalkos keys; their
	// projects are not planned and nothing may be written while any is left.
	Conflicts []string
	Users     []UserPlan
	// Labels are in creation order: a group comes before its labels.
	Labels   []LabelPlan
	Projects []ProjectPlan
	Report   *Report
}

// UserPlan is a Plane workspace member other than the seed bot.
type UserPlan struct {
	PlaneID string
	Email   string
	Name    string
	// Target is the Yalkos user the member maps to; uuid.Nil when Create.
	Target uuid.UUID
	// Create is set for a member no Yalkos user matches: created disabled.
	Create bool
}

// LabelPlan is one workspace label made of same-named Plane labels.
type LabelPlan struct {
	// Key identifies the label in IssuePlan.Labels (see labelKey).
	Key   string
	Name  string
	Color string
	// Group is the Key of the label's group; empty at top level.
	Group   string
	IsGroup bool
	// PlaneIDs are the Plane labels merged into this one.
	PlaneIDs []string
	// Target is the existing Yalkos label; uuid.Nil when it is created.
	Target uuid.UUID
}

// ProjectPlan is one Plane project to import.
type ProjectPlan struct {
	Data *ProjectData
	Key  string
	// Name is the project name on one line, within the Yalkos limit.
	Name string
	// Target is the project an earlier import created; uuid.Nil for a new one.
	Target uuid.UUID
	// Members are the Plane user ids of the project members that map to users.
	Members []string
	// States, Milestones and Issues are the ones to create; Issues in
	// parent-first order.
	States     []StatePlan
	Milestones []MilestonePlan
	Issues     []IssuePlan
	// Changed are the imported issues updated in Plane since.
	Changed []ChangedItem
	// MaxNumber is the highest Plane number of the project's issues.
	MaxNumber int
}

// ChangedItem is an imported work item Plane updated since.
type ChangedItem struct {
	PlaneID string
	// Key is the issue's KEY-N: the Plane number at first, the current
	// Yalkos key once Run has named it (see nameChanged).
	Key string
}

// StatePlan is a Plane state to create.
type StatePlan struct {
	PlaneID  string
	Name     string
	Color    string
	Type     string
	Position float64
	Default  bool
	// Target is an existing Yalkos state of the same name the Plane state is
	// mapped onto; uuid.Nil when it is created.
	Target uuid.UUID
}

// MilestonePlan is a Plane module to create as a milestone.
type MilestonePlan struct {
	Module Module
	// Name is unique in the project (case-insensitive).
	Name string
	// Status is the manual status: planned, in_progress, done or canceled.
	Status string
}

// IssuePlan is a Plane work item to create. References to other entities are
// Plane ids (resolved through the import map when written), labels are
// LabelPlan keys.
type IssuePlan struct {
	Item     WorkItem
	Key      string
	Number   int
	State    string
	Priority int
	// Parent is the Plane id of the parent; empty when there is none to keep.
	Parent string
	// Milestone is the Plane id of the earliest module of the issue.
	Milestone string
	// Assignee is the Plane user id of the first assignee.
	Assignee string
	// Type is the Type label; nil when the workspace has no default type.
	Type   *uuid.UUID
	Labels []string
}

var (
	seedBotRe = regexp.MustCompile(`^bot_user_[0-9a-f-]+@plane\.so$`)
	colorRe   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

const (
	fallbackStateColor = "#95a2b3"
	fallbackLabelColor = "#6e7781"
)

// Name limits of the entities the import creates, the same as when they are
// created in Yalkos. Names are cut before they are made unique, so a cut
// cannot make two names equal.
const (
	maxUserName      = 100
	maxProjectName   = 100
	maxStateName     = 50
	maxLabelName     = 50
	maxMilestoneName = 100
)

// name puts a Plane name on one line and cuts it to limit runes, with no
// space left at the end; what names it in the report's cut texts when it is
// cut.
func (r *Report) name(s string, limit int, what func(cut string) string) string {
	out, cut := cutName(oneLine(s), limit)
	if cut {
		if entry := what(out); !slices.Contains(r.Dropped.CutTexts, entry) {
			r.Dropped.CutTexts = append(r.Dropped.CutTexts, entry)
		}
	}
	return out
}

// cutName cuts a name to limit runes and trims the space a cut may leave at
// its end.
func cutName(s string, limit int) (string, bool) {
	out, cut := cutRunes(s, limit)
	if cut {
		out = strings.TrimSpace(out)
	}
	return out, cut
}

// isSeedBot reports Plane's seed bot account, skipped by the import.
func isSeedBot(email string) bool { return seedBotRe.MatchString(strings.ToLower(email)) }

// validColor is c in lower case, so colours compare equal whatever their
// case, or fallback when c is not a #rrggbb colour.
func validColor(c, fallback string) string {
	if colorRe.MatchString(c) {
		return strings.ToLower(c)
	}
	return fallback
}

// MakePlan maps a surveyed workspace onto Yalkos. It is pure: everything it
// needs is in ws and ex.
func MakePlan(ws *Workspace, ex Existing) (Plan, error) {
	if ws == nil {
		return Plan{}, errors.New("plane: nothing surveyed")
	}
	r := &Report{}
	p := Plan{Report: r}

	var active []*ProjectData
	keys := map[*ProjectData]string{}
	for i := range ws.Projects {
		pd := &ws.Projects[i]
		if pd.ArchivedAt != nil {
			r.ArchivedProjects = append(r.ArchivedProjects, pd.Identifier)
			continue
		}
		if !pd.member() {
			r.NotMember = append(r.NotMember, pd.Identifier)
			continue
		}
		key, reason := projectKey(pd, &ex)
		if reason != "" {
			p.Conflicts = append(p.Conflicts, pd.Identifier)
			r.Conflicts = append(r.Conflicts, Conflict{Key: pd.Identifier, Reason: reason})
			continue
		}
		keys[pd] = key
		active = append(active, pd)
		r.Cycles += pd.TotalCycles
	}

	var members map[string]bool
	p.Users, members = planUsers(ws.Members, &ex, r)

	lp := newLabelPlanner(&ex, r)
	for _, pd := range active {
		lp.addProject(keys[pd], pd.Labels)
	}
	lp.finish()

	for _, pd := range active {
		pp, err := planProject(pd, keys[pd], &ex, members, lp, r)
		if err != nil {
			return Plan{}, err
		}
		p.Projects = append(p.Projects, pp)
	}
	lp.report()
	p.Labels = lp.labels
	return p, nil
}

// projectKey returns the Yalkos key of a Plane project, or why it cannot have
// one. A project an earlier import created keeps its current Yalkos key.
func projectKey(pd *ProjectData, ex *Existing) (key, conflict string) {
	if target, ok := ex.mapped(kindProject, pd.ID); ok {
		if k, ok := ex.ProjectKeyByID[target]; ok {
			return k, ""
		}
	}
	key, err := project.NormalizeKey("key", pd.Identifier)
	if err != nil {
		return "", "not a valid Yalkos project key (a letter, then up to 6 letters or digits)"
	}
	owner, taken := ex.ProjectKeys[key]
	if !taken {
		return key, ""
	}
	if target, ok := ex.mapped(kindProject, pd.ID); ok && target == owner {
		return key, ""
	}
	return "", "the key belongs to a Yalkos project this import did not create"
}

// planUsers matches the members to users by import map, then by email
// (case-insensitive); the rest are created. It also returns the Plane ids of
// the members that map to users.
func planUsers(members []Member, ex *Existing, r *Report) ([]UserPlan, map[string]bool) {
	var out []UserPlan
	known := map[string]bool{}
	for _, m := range members {
		if isSeedBot(m.Email) {
			continue
		}
		up := UserPlan{PlaneID: m.ID, Email: strings.TrimSpace(m.Email)}
		up.Name = r.name(memberName(m), maxUserName, func(string) string { return up.Email + " (user name)" })
		if up.Name == "" {
			up.Name, _ = cutRunes(up.Email, maxUserName)
		}
		if id, ok := ex.mapped(kindUser, m.ID); ok {
			up.Target = id
		} else if id, ok := ex.Users[strings.ToLower(up.Email)]; ok {
			up.Target = id
		} else {
			up.Create = true
			r.Users = append(r.Users, UserReport{Email: up.Email, Created: true, Disabled: true})
		}
		known[m.ID] = true
		out = append(out, up)
	}
	return out, known
}

// memberName is the display name, else the full name, else the email's local
// part.
func memberName(m Member) string {
	if n := strings.TrimSpace(m.DisplayName); n != "" {
		return n
	}
	if n := strings.TrimSpace(strings.TrimSpace(m.FirstName) + " " + strings.TrimSpace(m.LastName)); n != "" {
		return n
	}
	local, _, _ := strings.Cut(strings.TrimSpace(m.Email), "@")
	return local
}

// freeName returns name, or "name (2)", "name (3)", … whichever is first not
// taken (taken gets lower-case names). The suffix stays within limit runes:
// name is cut to make room for it.
func freeName(name string, limit int, taken func(lower string) bool) string {
	final := name
	for n := 2; taken(strings.ToLower(final)); n++ {
		suffix := fmt.Sprintf(" (%d)", n)
		base, _ := cutName(name, max(limit-utf8.RuneCountInString(suffix), 0))
		final = base + suffix
	}
	return final
}
