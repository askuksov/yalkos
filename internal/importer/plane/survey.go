package plane

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/store/q"
)

// UsageError is a wrong command line, such as a --project key Plane does not
// have. The command exits 2 on it.
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

// ConflictError lists the chosen Plane projects whose identifiers cannot
// become Yalkos keys: taken by a project an earlier import did not create, or
// not a valid key. Nothing is written when it is returned.
type ConflictError struct{ Keys []string }

func (e *ConflictError) Error() string {
	return "Plane project identifiers that cannot become Yalkos project keys (taken by another project or not valid): " +
		strings.Join(e.Keys, ", ")
}

// Workspace is what Survey read from Plane.
type Workspace struct {
	Members []Member
	// Projects are the chosen projects in Plane order. An archived project, or
	// one the key's owner is not a member of, has only its Project filled: its
	// lists are not read.
	Projects []ProjectData
	// AllProjects holds every listed project by Plane id, chosen or not, so
	// relations into other projects can be named.
	AllProjects map[string]Project
}

// ProjectData is a Plane project with its lists.
type ProjectData struct {
	Project
	MemberIDs []string
	States    []State
	Labels    []Label
	Modules   []Module
	// ModuleIssues maps a module id to the ids of its work items.
	ModuleIssues map[string][]string
	Items        []WorkItem
}

// Survey reads the workspace members, the projects and, for each chosen
// project, its members, states, labels, modules with their work item ids and
// its work items. keys are Plane identifiers (case-insensitive); none means
// every project. A key Plane does not have is a *UsageError, returned before
// any project list is read.
func Survey(ctx context.Context, c *Client, keys []string) (*Workspace, error) {
	members, err := c.Members(ctx)
	if err != nil {
		return nil, fmt.Errorf("plane members: %w", err)
	}
	projects, err := c.Projects(ctx)
	if err != nil {
		return nil, fmt.Errorf("plane projects: %w", err)
	}
	ws := &Workspace{Members: members, AllProjects: make(map[string]Project, len(projects))}
	for _, p := range projects {
		ws.AllProjects[p.ID] = p
	}
	chosen, err := chooseProjects(projects, keys)
	if err != nil {
		return nil, err
	}
	for _, p := range chosen {
		pd := ProjectData{Project: p}
		if p.ArchivedAt == nil && p.member() {
			if err := readProject(ctx, c, &pd); err != nil {
				return nil, fmt.Errorf("plane project %s: %w", p.Identifier, err)
			}
		}
		ws.Projects = append(ws.Projects, pd)
	}
	return ws, nil
}

func chooseProjects(projects []Project, keys []string) ([]Project, error) {
	if len(keys) == 0 {
		return projects, nil
	}
	want := map[string]bool{}
	var unknown []string
	for _, k := range keys {
		k = strings.ToUpper(strings.TrimSpace(k))
		if want[k] {
			continue
		}
		want[k] = true
		// The same comparison selects the projects below: a key that folds
		// to an identifier only under Unicode case folding is unknown.
		found := false
		for _, p := range projects {
			if strings.ToUpper(p.Identifier) == k {
				found = true
				break
			}
		}
		if !found {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		return nil, &UsageError{Msg: "no Plane project with identifier " + strings.Join(unknown, ", ")}
	}
	var out []Project
	var outside []string
	for _, p := range projects {
		if !want[strings.ToUpper(p.Identifier)] {
			continue
		}
		if p.ArchivedAt == nil && !p.member() {
			outside = append(outside, p.Identifier)
		}
		out = append(out, p)
	}
	if len(outside) > 0 {
		return nil, &UsageError{Msg: "the owner of the Plane API key is not a member of Plane project " +
			strings.Join(outside, ", ") + "; join the project in Plane first"}
	}
	return out, nil
}

// member reports whether the key's owner is a member of the project; Plane
// lists public projects to non-members too, but their lists stay closed.
func (p Project) member() bool { return p.IsMember == nil || *p.IsMember }

func readProject(ctx context.Context, c *Client, pd *ProjectData) error {
	var err error
	if pd.MemberIDs, err = c.ProjectMembers(ctx, pd.ID); err != nil {
		return fmt.Errorf("members: %w", err)
	}
	if pd.States, err = c.States(ctx, pd.ID); err != nil {
		return fmt.Errorf("states: %w", err)
	}
	if pd.Labels, err = c.Labels(ctx, pd.ID); err != nil {
		return fmt.Errorf("labels: %w", err)
	}
	if pd.Modules, err = c.Modules(ctx, pd.ID); err != nil {
		return fmt.Errorf("modules: %w", err)
	}
	pd.ModuleIssues = make(map[string][]string, len(pd.Modules))
	for _, m := range pd.Modules {
		ids, err := c.ModuleIssues(ctx, pd.ID, m.ID)
		if err != nil {
			return fmt.Errorf("module issues: %w", err)
		}
		pd.ModuleIssues[m.ID] = ids
	}
	if pd.Items, err = c.WorkItems(ctx, pd.ID); err != nil {
		return fmt.Errorf("work items: %w", err)
	}
	return nil
}

// Map kinds of import_map.
const (
	kindUser       = "user"
	kindProject    = "project"
	kindState      = "state"
	kindLabel      = "label"
	kindMilestone  = "milestone"
	kindIssue      = "issue"
	kindComment    = "comment"
	kindAttachment = "attachment"
)

var mapKinds = []string{kindUser, kindProject, kindState, kindLabel, kindMilestone, kindIssue, kindComment, kindAttachment}

// Existing is what the Yalkos instance already has.
type Existing struct {
	// Map holds what earlier imports of this source created: kind → Plane id → target.
	Map map[string]map[string]uuid.UUID
	// Updated is the recorded Plane updated_at of each imported issue, by Plane id.
	Updated map[string]time.Time
	// Users maps lower(email) to the user.
	Users map[string]uuid.UUID
	// ProjectKeys maps every key a project has had to the project.
	ProjectKeys map[string]uuid.UUID
	// Labels maps lower(name) of a top-level label (or group) to the label.
	Labels map[string]uuid.UUID
	// GroupLabels maps a group's label id → lower(label name) to a label in
	// that group.
	GroupLabels map[uuid.UUID]map[string]uuid.UUID
	// LabelKinds tells, by lower(name) of a top-level label, whether it is a
	// group and whether that group is exclusive.
	LabelKinds map[string]LabelKind
	// ProjectKeyByID is the current key of each project an earlier import created.
	ProjectKeyByID map[uuid.UUID]string
	// Archived holds the projects an earlier import created that are archived
	// in Yalkos.
	Archived map[uuid.UUID]bool
	// States and Milestones map, for each project an earlier import created,
	// lower(name) to the state or milestone.
	States     map[uuid.UUID]map[string]uuid.UUID
	Milestones map[uuid.UUID]map[string]uuid.UUID
	// Type is the Type group with its default.
	Type label.TypeInfo
	// TypeNames maps lower(name) of each Type label to the label.
	TypeNames map[string]uuid.UUID
}

// LabelKind describes an existing top-level label.
type LabelKind struct{ Group, Exclusive bool }

// mapped returns the target an earlier import recorded for a Plane id.
func (ex *Existing) mapped(kind, planeID string) (uuid.UUID, bool) {
	id, ok := ex.Map[kind][planeID]
	return id, ok
}

// ready gives ex the empty maps the writers fill.
func (ex *Existing) ready() {
	if ex.Map == nil {
		ex.Map = map[string]map[string]uuid.UUID{}
	}
	for _, kind := range mapKinds {
		if ex.Map[kind] == nil {
			ex.Map[kind] = map[string]uuid.UUID{}
		}
	}
	if ex.Updated == nil {
		ex.Updated = map[string]time.Time{}
	}
	if ex.Archived == nil {
		ex.Archived = map[uuid.UUID]bool{}
	}
}

// LoadExisting reads what the instance has: the import map of source
// (uuid.Nil: nothing imported yet), users, project keys, labels and the Type
// labels.
func LoadExisting(ctx context.Context, db *q.Queries, source uuid.UUID) (Existing, error) {
	ex := Existing{
		Map:         make(map[string]map[string]uuid.UUID, len(mapKinds)),
		Updated:     map[string]time.Time{},
		Users:       map[string]uuid.UUID{},
		ProjectKeys: map[string]uuid.UUID{},
		Labels:      map[string]uuid.UUID{},
		GroupLabels: map[uuid.UUID]map[string]uuid.UUID{},
		LabelKinds:  map[string]LabelKind{},
		TypeNames:   map[string]uuid.UUID{},

		ProjectKeyByID: map[uuid.UUID]string{},
		Archived:       map[uuid.UUID]bool{},
		States:         map[uuid.UUID]map[string]uuid.UUID{},
		Milestones:     map[uuid.UUID]map[string]uuid.UUID{},
	}
	for _, kind := range mapKinds {
		ex.Map[kind] = map[string]uuid.UUID{}
		if source == uuid.Nil {
			continue
		}
		rows, err := db.ListImportMap(ctx, q.ListImportMapParams{SourceID: source, Kind: kind})
		if err != nil {
			return Existing{}, fmt.Errorf("import map: %w", err)
		}
		for _, r := range rows {
			ex.Map[kind][r.PlaneID] = r.TargetID
			if kind == kindIssue && r.SourceUpdatedAt != nil {
				ex.Updated[r.PlaneID] = *r.SourceUpdatedAt
			}
		}
	}

	if err := loadProjects(ctx, db, &ex); err != nil {
		return Existing{}, err
	}

	users, err := db.ListUsers(ctx)
	if err != nil {
		return Existing{}, fmt.Errorf("users: %w", err)
	}
	for _, u := range users {
		ex.Users[strings.ToLower(u.Email)] = u.ID
	}

	keys, err := db.ListProjectKeys(ctx)
	if err != nil {
		return Existing{}, fmt.Errorf("project keys: %w", err)
	}
	for _, k := range keys {
		ex.ProjectKeys[k.Key] = k.ProjectID
	}

	labels, err := db.ListLabels(ctx)
	if err != nil {
		return Existing{}, fmt.Errorf("labels: %w", err)
	}
	names := make(map[uuid.UUID]string, len(labels))
	for _, l := range labels {
		names[l.ID] = strings.ToLower(l.Name)
	}
	for _, l := range labels {
		if l.ParentID == nil {
			ex.Labels[names[l.ID]] = l.ID
			ex.LabelKinds[names[l.ID]] = LabelKind{Group: l.IsGroup, Exclusive: l.Exclusive}
			continue
		}
		group := *l.ParentID
		if ex.GroupLabels[group] == nil {
			ex.GroupLabels[group] = map[string]uuid.UUID{}
		}
		ex.GroupLabels[group][names[l.ID]] = l.ID
	}

	if ex.Type, err = label.ReadTypeInfo(ctx, db); err != nil {
		return Existing{}, err
	}
	for _, id := range ex.Type.LabelIDs {
		ex.TypeNames[names[id]] = id
	}
	return ex, nil
}

// loadProjects reads the key, archived state, states and milestones of every
// project an earlier import created.
func loadProjects(ctx context.Context, db *q.Queries, ex *Existing) error {
	ids := make([]uuid.UUID, 0, len(ex.Map[kindProject]))
	for _, id := range ex.Map[kindProject] {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	projects, err := db.ListProjectsByIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("imported projects: %w", err)
	}
	for _, p := range projects {
		ex.ProjectKeyByID[p.ID] = p.Key
		if p.ArchivedAt != nil {
			ex.Archived[p.ID] = true
		}
	}
	states, err := db.ListStatesByProjectIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("states of imported projects: %w", err)
	}
	for _, s := range states {
		addName(ex.States, s.ProjectID, s.Name, s.ID)
	}
	milestones, err := db.ListMilestonesByProjects(ctx, ids)
	if err != nil {
		return fmt.Errorf("milestones of imported projects: %w", err)
	}
	for _, m := range milestones {
		addName(ex.Milestones, m.ProjectID, m.Name, m.ID)
	}
	return nil
}

func addName(m map[uuid.UUID]map[string]uuid.UUID, project uuid.UUID, name string, id uuid.UUID) {
	if m[project] == nil {
		m[project] = map[string]uuid.UUID{}
	}
	m[project][strings.ToLower(name)] = id
}
