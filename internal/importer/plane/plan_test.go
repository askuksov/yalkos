package plane

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/label"
)

var (
	typeGroupID       = uuid.MustParse("00000000-0000-7000-8000-0000000000a0")
	typeBugID         = uuid.MustParse("00000000-0000-7000-8000-0000000000a1")
	typeFeatureID     = uuid.MustParse("00000000-0000-7000-8000-0000000000a2")
	typeTaskID        = uuid.MustParse("00000000-0000-7000-8000-0000000000a3")
	typeImprovementID = uuid.MustParse("00000000-0000-7000-8000-0000000000a4")
)

// surveyFixture reads the fixture workspace through Survey.
func surveyFixture(t *testing.T, keys ...string) (*Workspace, *fakePlane) {
	t.Helper()
	f := loadFixture(t, filepath.Join("testdata", "plane"))
	ws, err := Survey(context.Background(), newTestClient(t, f, newFakeClock()), keys)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	return ws, f
}

// emptyExisting is a Yalkos instance with nothing imported and the seeded
// Type labels.
func emptyExisting() Existing {
	def := typeTaskID
	return Existing{
		Type: label.TypeInfo{GroupID: typeGroupID, DefaultID: &def,
			LabelIDs: []uuid.UUID{typeBugID, typeFeatureID, typeTaskID, typeImprovementID}},
		TypeNames: map[string]uuid.UUID{
			"bug": typeBugID, "feature": typeFeatureID, "task": typeTaskID, "improvement": typeImprovementID,
		},
	}
}

func makeFixturePlan(t *testing.T, ex Existing) (Plan, *Workspace) {
	t.Helper()
	ws, _ := surveyFixture(t)
	p, err := MakePlan(ws, ex)
	if err != nil {
		t.Fatalf("MakePlan: %v", err)
	}
	return p, ws
}

func findProject(t *testing.T, p Plan, key string) *ProjectPlan {
	t.Helper()
	for i := range p.Projects {
		if p.Projects[i].Key == key {
			return &p.Projects[i]
		}
	}
	t.Fatalf("project %s not planned", key)
	return nil
}

func findIssue(t *testing.T, pp *ProjectPlan, key string) IssuePlan {
	t.Helper()
	for _, is := range pp.Issues {
		if is.Key == key {
			return is
		}
	}
	t.Fatalf("issue %s not planned", key)
	return IssuePlan{}
}

func findLabel(t *testing.T, p Plan, key string) LabelPlan {
	t.Helper()
	var found []LabelPlan
	for _, l := range p.Labels {
		if l.Key == key {
			found = append(found, l)
		}
	}
	if len(found) != 1 {
		t.Fatalf("labels with key %q: %d, want 1", key, len(found))
	}
	return found[0]
}

func TestPlanStatesAndPriorities(t *testing.T) {
	p, _ := makeFixturePlan(t, emptyExisting())
	alp := findProject(t, p, "ALP")

	type st struct {
		name, typ, color string
		def              bool
	}
	var got []st
	for _, s := range alp.States {
		got = append(got, st{s.Name, s.Type, s.Color, s.Default})
	}
	want := []st{
		{"Backlog", "backlog", "#bec2c8", false},
		{"Todo", "unstarted", "#3a3a3a", true},
		{"In Progress", "started", "#f59e0b", false},
		{"Done", "completed", "#95a2b3", false},
		{"Cancelled", "canceled", "#9ca3af", false},
	}
	if !slices.Equal(got, want) {
		t.Errorf("ALP states = %+v\nwant %+v", got, want)
	}
	bet := findProject(t, p, "BET")
	for _, s := range bet.States {
		if s.Default != (s.Name == "Doing") {
			t.Errorf("BET state %s default = %v", s.Name, s.Default)
		}
	}

	prio := map[string]int{"ALP-1": 1, "ALP-2": 2, "ALP-3": 3, "ALP-4": 4, "ALP-5": 0}
	for key, want := range prio {
		if got := findIssue(t, alp, key).Priority; got != want {
			t.Errorf("%s priority = %d, want %d", key, got, want)
		}
	}

	// ALP-3 sits in the skipped triage state: it falls back to the default.
	if got := findIssue(t, alp, "ALP-3").State; got != "st-alp-todo" {
		t.Errorf("ALP-3 state = %q, want the default st-alp-todo", got)
	}
	if got := findIssue(t, alp, "ALP-1").State; got != "st-alp-todo" {
		t.Errorf("ALP-1 state = %q", got)
	}
	if !slices.Equal(p.Report.StateFallback, []string{"ALP-3"}) {
		t.Errorf("stateFallback = %v, want [ALP-3]", p.Report.StateFallback)
	}
	pr := p.Report.Projects[0]
	if pr.Key != "ALP" || pr.Skipped.States != 1 || pr.Created.States != 5 || pr.Created.Issues != 5 {
		t.Errorf("ALP report = %+v", pr)
	}
}

func TestPlanLabelsMergeAndType(t *testing.T) {
	p, _ := makeFixturePlan(t, emptyExisting())

	fe := findLabel(t, p, labelKey("", "Frontend"))
	if fe.Name != "Frontend" || fe.Color != "#3b82f6" || fe.IsGroup || fe.Group != "" {
		t.Errorf("frontend label = %+v", fe)
	}
	if !slices.Equal(fe.PlaneIDs, []string{"lb-alp-frontend", "lb-bet-frontend"}) {
		t.Errorf("frontend plane ids = %v", fe.PlaneIDs)
	}
	var merged *MergedLabel
	for i := range p.Report.Labels.Merged {
		if p.Report.Labels.Merged[i].Name == "Frontend" {
			merged = &p.Report.Labels.Merged[i]
		}
	}
	if merged == nil {
		t.Fatalf("merged = %+v, want Frontend", p.Report.Labels.Merged)
	}
	if !slices.Equal(merged.Projects, []string{"ALP", "BET"}) || !slices.Equal(merged.Colors, []string{"#3b82f6", "#22c55e"}) {
		t.Errorf("merged Frontend = %+v", *merged)
	}

	area := findLabel(t, p, labelKey("", "Area"))
	if !area.IsGroup || area.Group != "" {
		t.Errorf("Area = %+v, want a top-level group", area)
	}
	api := findLabel(t, p, labelKey("Area", "api"))
	v2 := findLabel(t, p, labelKey("Area", "v2"))
	if api.Group != area.Key || v2.Group != area.Key || api.IsGroup || v2.IsGroup {
		t.Errorf("api = %+v, v2 = %+v, want both in group Area", api, v2)
	}
	if !slices.Equal(p.Report.Labels.Flattened, []FlattenedLabel{{Name: "v2", Group: "Area"}}) {
		t.Errorf("flattened = %+v", p.Report.Labels.Flattened)
	}
	for _, l := range p.Labels {
		if l.Key == labelKey("", "bug") || l.Key == labelKey("", "improvement") {
			t.Errorf("Type label %q planned as an ordinary label", l.Name)
		}
	}
	// feature loses to bug on ALP-2, so it is needed as an ordinary label.
	findLabel(t, p, labelKey("", "feature"))

	alp, bet := findProject(t, p, "ALP"), findProject(t, p, "BET")
	cases := []struct {
		pp     *ProjectPlan
		key    string
		typ    uuid.UUID
		labels []string
	}{
		{alp, "ALP-1", typeBugID, []string{labelKey("", "Frontend")}},
		{alp, "ALP-2", typeBugID, []string{labelKey("", "feature")}},
		{alp, "ALP-3", typeTaskID, []string{labelKey("Area", "api"), labelKey("Area", "v2")}},
		{alp, "ALP-4", typeTaskID, nil},
		{alp, "ALP-5", typeFeatureID, nil},
		{bet, "BET-1", typeTaskID, []string{labelKey("", "frontend")}},
		{bet, "BET-2", typeImprovementID, nil},
	}
	for _, c := range cases {
		is := findIssue(t, c.pp, c.key)
		if is.Type == nil || *is.Type != c.typ {
			t.Errorf("%s type = %v, want %v", c.key, is.Type, c.typ)
		}
		if !slices.Equal(is.Labels, c.labels) {
			t.Errorf("%s labels = %q, want %q", c.key, is.Labels, c.labels)
		}
	}

	wantTypes := []TypeMapped{
		{Name: "bug", Type: "Bug", Issues: 2},
		{Name: "feature", Type: "Feature", Issues: 1},
		{Name: "Improvement", Type: "Improvement", Issues: 1},
	}
	if !slices.Equal(p.Report.Labels.TypeMapped, wantTypes) {
		t.Errorf("typeMapped = %+v\nwant %+v", p.Report.Labels.TypeMapped, wantTypes)
	}
	if !slices.Equal(p.Report.Labels.TypeConflicts, []string{"ALP-2"}) {
		t.Errorf("typeConflicts = %v", p.Report.Labels.TypeConflicts)
	}
}

func TestPlanMilestones(t *testing.T) {
	p, _ := makeFixturePlan(t, emptyExisting())

	type ms struct{ name, status string }
	got := func(pp *ProjectPlan) []ms {
		var out []ms
		for _, m := range pp.Milestones {
			out = append(out, ms{m.Name, m.Status})
		}
		return out
	}
	alp, bet := findProject(t, p, "ALP"), findProject(t, p, "BET")
	if g, w := got(alp), []ms{{"Launch", "in_progress"}, {"Polish", "planned"}}; !slices.Equal(g, w) {
		t.Errorf("ALP milestones = %v, want %v", g, w)
	}
	if g, w := got(bet), []ms{{"Beta", "planned"}, {"beta (2)", "canceled"}}; !slices.Equal(g, w) {
		t.Errorf("BET milestones = %v, want %v", g, w)
	}
	if !slices.Equal(p.Report.Dropped.ModuleStartDates, []string{"BET: beta (2)"}) {
		t.Errorf("moduleStartDates = %v", p.Report.Dropped.ModuleStartDates)
	}
	if !slices.Equal(p.Report.RenamedMilestones, []RenamedMilestone{{Project: "BET", Plane: "beta", Yalkos: "beta (2)"}}) {
		t.Errorf("renamedMilestones = %+v", p.Report.RenamedMilestones)
	}

	// The earliest module by order wins; the others are reported.
	if m := findIssue(t, alp, "ALP-2").Milestone; m != "md-alp-launch" {
		t.Errorf("ALP-2 milestone = %q", m)
	}
	if m := findIssue(t, alp, "ALP-4").Milestone; m != "md-alp-polish" {
		t.Errorf("ALP-4 milestone = %q", m)
	}
	if m := findIssue(t, alp, "ALP-3").Milestone; m != "" {
		t.Errorf("ALP-3 milestone = %q, want none", m)
	}
	if !slices.Equal(p.Report.Dropped.ExtraModules, []string{"ALP-2"}) {
		t.Errorf("extraModules = %v", p.Report.Dropped.ExtraModules)
	}
	if !slices.Equal(p.Report.Dropped.StartDates, []string{"BET-3"}) {
		t.Errorf("startDates = %v", p.Report.Dropped.StartDates)
	}
	if !slices.Equal(p.Report.Dropped.ExtraAssignees, []string{"BET-3"}) {
		t.Errorf("extraAssignees = %v", p.Report.Dropped.ExtraAssignees)
	}
	if a := findIssue(t, bet, "BET-3").Assignee; a != "u-ann" {
		t.Errorf("BET-3 assignee = %q, want the first one", a)
	}
}

func TestPlanUsers(t *testing.T) {
	annID := uuid.MustParse("00000000-0000-7000-8000-0000000000b1")
	ex := emptyExisting()
	ex.Users = map[string]uuid.UUID{strings.ToLower("ANN@example.invalid"): annID}
	p, _ := makeFixturePlan(t, ex)

	if len(p.Users) != 2 {
		t.Fatalf("users = %+v, want ann and bob (the seed bot skipped)", p.Users)
	}
	ann, bob := p.Users[0], p.Users[1]
	if ann.PlaneID != "u-ann" || ann.Create || ann.Target != annID {
		t.Errorf("ann = %+v, want matched to the existing user", ann)
	}
	if bob.PlaneID != "u-bob" || !bob.Create || bob.Target != uuid.Nil || bob.Name != "Bob Baker" || bob.Email != "bob@example.invalid" {
		t.Errorf("bob = %+v, want a new user", bob)
	}
	want := []UserReport{{Email: "bob@example.invalid", Created: true, Disabled: true}}
	if !slices.Equal(p.Report.Users, want) {
		t.Errorf("report users = %+v, want %+v", p.Report.Users, want)
	}
	if m := findProject(t, p, "ALP").Members; !slices.Equal(m, []string{"u-ann", "u-bob"}) {
		t.Errorf("ALP members = %v, want the bot skipped", m)
	}
}

func TestPlanParentFirst(t *testing.T) {
	p, _ := makeFixturePlan(t, emptyExisting())
	for _, pp := range p.Projects {
		pos := map[string]int{}
		for i, is := range pp.Issues {
			pos[is.Item.ID] = i
		}
		for i, is := range pp.Issues {
			if is.Parent == "" {
				continue
			}
			if at, ok := pos[is.Parent]; !ok || at >= i {
				t.Errorf("%s: parent %s at %d, issue at %d", is.Key, is.Parent, at, i)
			}
		}
	}
	alp := findProject(t, p, "ALP")
	if len(alp.Issues) != 5 {
		t.Fatalf("ALP issues = %d, want 5", len(alp.Issues))
	}
	if got := findIssue(t, alp, "ALP-4").Parent; got != "i-alp-5" {
		t.Errorf("ALP-4 parent = %q, want i-alp-5", got)
	}
	if got := findIssue(t, alp, "ALP-4").Number; got != 4 {
		t.Errorf("ALP-4 number = %d", got)
	}
}

func TestPlanConflicts(t *testing.T) {
	ws, _ := surveyFixture(t)
	for i := range ws.Projects {
		if ws.Projects[i].Identifier == "BET" {
			ws.Projects[i].Identifier = "TOOLONGKEY"
		}
	}
	other := uuid.MustParse("00000000-0000-7000-8000-0000000000c1")
	ex := emptyExisting()
	ex.ProjectKeys = map[string]uuid.UUID{"ALP": other}

	p, err := MakePlan(ws, ex)
	if err != nil {
		t.Fatalf("MakePlan: %v", err)
	}
	if !slices.Equal(p.Conflicts, []string{"ALP", "TOOLONGKEY"}) {
		t.Errorf("conflicts = %v, want [ALP TOOLONGKEY]", p.Conflicts)
	}
	var keys []string
	for _, c := range p.Report.Conflicts {
		keys = append(keys, c.Key)
		if c.Reason == "" {
			t.Errorf("conflict %s has no reason", c.Key)
		}
	}
	if !slices.Equal(keys, p.Conflicts) {
		t.Errorf("report conflicts = %+v", p.Report.Conflicts)
	}
	if len(p.Projects) != 0 {
		t.Errorf("projects planned despite conflicts: %d", len(p.Projects))
	}

	// The key belongs to the project an earlier import created: no conflict.
	ws2, _ := surveyFixture(t)
	ex.Map = map[string]map[string]uuid.UUID{"project": {"p-alp": other}}
	p, err = MakePlan(ws2, ex)
	if err != nil {
		t.Fatalf("MakePlan: %v", err)
	}
	if len(p.Conflicts) != 0 {
		t.Errorf("conflicts = %v, want none", p.Conflicts)
	}
	if alp := findProject(t, p, "ALP"); alp.Target != other {
		t.Errorf("ALP target = %v, want %v", alp.Target, other)
	}
}

func TestPlanArchivedProjectSkipped(t *testing.T) {
	ws, f := surveyFixture(t)
	p, err := MakePlan(ws, emptyExisting())
	if err != nil {
		t.Fatalf("MakePlan: %v", err)
	}
	if !slices.Equal(p.Report.ArchivedProjects, []string{"OLD"}) {
		t.Errorf("archivedProjects = %v, want [OLD]", p.Report.ArchivedProjects)
	}
	for _, pp := range p.Projects {
		if pp.Key == "OLD" {
			t.Error("archived project OLD planned")
		}
	}
	for _, path := range []string{"projects/p-old/states/", "projects/p-old/work-items/", "projects/p-old/labels/"} {
		if n := f.Requests(path); n != 0 {
			t.Errorf("%s requested %d times", path, n)
		}
	}
}

func TestReportHasNoIssueText(t *testing.T) {
	p, ws := makeFixturePlan(t, emptyExisting())
	p.Report.Source = Source{URL: "http://plane.example.invalid", Workspace: "acme"}
	p.Report.DryRun = true

	var js, text bytes.Buffer
	if err := p.Report.WriteJSON(&js); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	p.Report.WriteText(&text)
	if text.Len() == 0 {
		t.Fatal("empty text report")
	}

	var secrets []string
	for _, pd := range ws.Projects {
		for _, it := range pd.Items {
			secrets = append(secrets, it.Name)
			if d := strings.TrimSuffix(strings.TrimPrefix(it.DescriptionHTML, "<p>"), "</p>"); d != "" {
				secrets = append(secrets, d)
			}
		}
	}
	if len(secrets) < 10 {
		t.Fatalf("only %d fixture texts", len(secrets))
	}
	for _, s := range secrets {
		if strings.Contains(js.String(), s) {
			t.Errorf("JSON report contains %q", s)
		}
		if strings.Contains(text.String(), s) {
			t.Errorf("text report contains %q", s)
		}
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(js.Bytes(), &doc); err != nil {
		t.Fatalf("report JSON: %v", err)
	}
	for _, k := range []string{"source", "dryRun", "projects", "users", "labels", "renumbered", "changedInPlane",
		"dropped", "attachments", "cycles", "archivedProjects"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("report JSON has no %q", k)
		}
	}
	if strings.Contains(js.String(), "null") {
		t.Errorf("report JSON has null lists: %s", js.String())
	}
}

func projectData(t *testing.T, ws *Workspace, key string) *ProjectData {
	t.Helper()
	for i := range ws.Projects {
		if ws.Projects[i].Identifier == key {
			return &ws.Projects[i]
		}
	}
	t.Fatalf("project %s not surveyed", key)
	return nil
}

func setItemLabels(t *testing.T, pd *ProjectData, n int, labels ...string) {
	t.Helper()
	for i := range pd.Items {
		if pd.Items[i].SequenceID == n {
			pd.Items[i].Labels = labels
			return
		}
	}
	t.Fatalf("no item %d", n)
}

func TestPlanLabelCapCountsType(t *testing.T) {
	ws, _ := surveyFixture(t)
	alp := projectData(t, ws, "ALP")
	ids := []string{"lb-alp-bug"}
	for i := range 24 {
		id := "lb-extra-" + strconv.Itoa(i)
		alp.Labels = append(alp.Labels, Label{ID: id, Name: "extra" + strconv.Itoa(i), Color: "#123456", SortOrder: float64(10000 + i)})
		ids = append(ids, id)
	}
	setItemLabels(t, alp, 1, ids...)

	p, err := MakePlan(ws, emptyExisting())
	if err != nil {
		t.Fatalf("MakePlan: %v", err)
	}
	is := findIssue(t, findProject(t, p, "ALP"), "ALP-1")
	if is.Type == nil || *is.Type != typeBugID {
		t.Errorf("type = %v, want Bug", is.Type)
	}
	if len(is.Labels) != 19 || is.Labels[0] != labelKey("", "extra0") || is.Labels[18] != labelKey("", "extra18") {
		t.Errorf("labels = %d %q, want the first 19 ordinary labels", len(is.Labels), is.Labels)
	}
	if !slices.Equal(p.Report.Dropped.LabelsOver20, []string{"ALP-1"}) {
		t.Errorf("labelsOver20 = %v", p.Report.Dropped.LabelsOver20)
	}
}

func TestPlanLosingTypeLabelColour(t *testing.T) {
	ws, _ := surveyFixture(t)
	setItemLabels(t, projectData(t, ws, "ALP"), 2, "lb-alp-bug")
	bet := projectData(t, ws, "BET")
	bet.Labels = append(bet.Labels, Label{ID: "lb-bet-bug", Name: "Bug", Color: "#ff0000", SortOrder: 4000})
	setItemLabels(t, bet, 1, "lb-bet-frontend", "lb-bet-feature", "lb-bet-bug")

	p, err := MakePlan(ws, emptyExisting())
	if err != nil {
		t.Fatalf("MakePlan: %v", err)
	}
	fe := findLabel(t, p, labelKey("", "feature"))
	if fe.Color != "#8e4ec6" || fe.Name != "feature" {
		t.Errorf("feature = %+v, want ALP's name and colour", fe)
	}
	if is := findIssue(t, findProject(t, p, "BET"), "BET-1"); !slices.Equal(is.Labels, []string{labelKey("", "frontend"), fe.Key}) {
		t.Errorf("BET-1 labels = %q", is.Labels)
	}
	want := MergedLabel{Name: "feature", Projects: []string{"ALP", "BET"}, Colors: []string{"#8e4ec6", "#6e56cf"}}
	found := false
	for _, m := range p.Report.Labels.Merged {
		if m.Name == "feature" {
			found = true
			if !slices.Equal(m.Projects, want.Projects) || !slices.Equal(m.Colors, want.Colors) {
				t.Errorf("merged feature = %+v, want %+v", m, want)
			}
		}
	}
	if !found {
		t.Errorf("merged = %+v, want feature", p.Report.Labels.Merged)
	}
}

// mappedExisting says ALP was imported before as the Yalkos project key.
func mappedExisting(alpID uuid.UUID, key string) Existing {
	ex := emptyExisting()
	ex.Map = map[string]map[string]uuid.UUID{"project": {"p-alp": alpID}}
	ex.ProjectKeys = map[string]uuid.UUID{"ALP": alpID, key: alpID}
	ex.ProjectKeyByID = map[uuid.UUID]string{alpID: key}
	return ex
}

func TestPlanMilestoneNameClashes(t *testing.T) {
	alpID := uuid.MustParse("00000000-0000-7000-8000-0000000000e1")
	launchID := uuid.MustParse("00000000-0000-7000-8000-0000000000e2")
	ex := mappedExisting(alpID, "ALP")
	ex.Milestones = map[uuid.UUID]map[string]uuid.UUID{alpID: {"launch": launchID, "launch (2)": launchID}}
	p, _ := makeFixturePlan(t, ex)

	var names []string
	for _, m := range findProject(t, p, "ALP").Milestones {
		names = append(names, m.Name)
	}
	if !slices.Equal(names, []string{"Launch (3)", "Polish"}) {
		t.Errorf("ALP milestones = %q", names)
	}
	want := []RenamedMilestone{{Project: "ALP", Plane: "Launch", Yalkos: "Launch (3)"}, {Project: "BET", Plane: "beta", Yalkos: "beta (2)"}}
	if !slices.Equal(p.Report.RenamedMilestones, want) {
		t.Errorf("renamedMilestones = %+v, want %+v", p.Report.RenamedMilestones, want)
	}
}

func TestPlanStateReuseAndDuplicates(t *testing.T) {
	alpID := uuid.MustParse("00000000-0000-7000-8000-0000000000f1")
	todoID := uuid.MustParse("00000000-0000-7000-8000-0000000000f2")
	ws, _ := surveyFixture(t)
	alp := projectData(t, ws, "ALP")
	alp.States = append(alp.States, State{ID: "st-alp-done2", Name: "done", Color: "#16a34a", Group: "completed", Sequence: 46000})

	ex := mappedExisting(alpID, "ALP")
	ex.States = map[uuid.UUID]map[string]uuid.UUID{alpID: {"todo": todoID}}
	p, err := MakePlan(ws, ex)
	if err != nil {
		t.Fatalf("MakePlan: %v", err)
	}
	pp := findProject(t, p, "ALP")
	type st struct {
		name   string
		target uuid.UUID
		def    bool
	}
	var got []st
	for _, s := range pp.States {
		got = append(got, st{s.Name, s.Target, s.Default})
	}
	want := []st{{"Backlog", uuid.Nil, false}, {"Todo", todoID, false}, {"In Progress", uuid.Nil, false},
		{"Done", uuid.Nil, false}, {"done (2)", uuid.Nil, false}, {"Cancelled", uuid.Nil, false}}
	if !slices.Equal(got, want) {
		t.Errorf("states = %+v\nwant %+v", got, want)
	}
	pr := p.Report.Projects[0]
	if pr.Created.States != 5 || pr.Existing.States != 1 {
		t.Errorf("ALP state counts: created %d, existing %d", pr.Created.States, pr.Existing.States)
	}
	if !slices.Equal(p.Report.RenamedStates, []RenamedMilestone{{Project: "ALP", Plane: "done", Yalkos: "done (2)"}}) {
		t.Errorf("renamedStates = %+v", p.Report.RenamedStates)
	}
}

func TestPlanLabelKindClashes(t *testing.T) {
	ws, _ := surveyFixture(t)
	alp := projectData(t, ws, "ALP")
	parent := "lb-alp-type"
	alp.Labels = append(alp.Labels,
		Label{ID: "lb-alp-type", Name: "Type", Color: "#111111", SortOrder: 7000},
		Label{ID: "lb-alp-kind", Name: "kind", Color: "#222222", Parent: &parent, SortOrder: 8000})

	frontendGroup := uuid.MustParse("00000000-0000-7000-8000-000000000101")
	areaPlain := uuid.MustParse("00000000-0000-7000-8000-000000000102")
	featurePlain := uuid.MustParse("00000000-0000-7000-8000-000000000103")
	ex := emptyExisting()
	ex.Labels = map[string]uuid.UUID{"frontend": frontendGroup, "area": areaPlain, "feature": featurePlain, "type": typeGroupID}
	ex.LabelKinds = map[string]LabelKind{
		"frontend": {Group: true}, "area": {}, "feature": {}, "type": {Group: true, Exclusive: true},
	}
	p, err := MakePlan(ws, ex)
	if err != nil {
		t.Fatalf("MakePlan: %v", err)
	}
	cases := []struct {
		key, name string
		target    uuid.UUID
	}{
		{labelKey("", "Frontend"), "Frontend (2)", uuid.Nil}, // plain never joins a Yalkos group
		{labelKey("", "Area"), "Area (2)", uuid.Nil},         // a group never joins a plain label
		{labelKey("", "Type"), "Type (2)", uuid.Nil},         // nor an exclusive group
		{labelKey("", "feature"), "feature", featurePlain},   // same kind: reused
	}
	for _, c := range cases {
		l := findLabel(t, p, c.key)
		if l.Name != c.name || l.Target != c.target {
			t.Errorf("%q = %+v, want name %q target %v", c.key, l, c.name, c.target)
		}
	}
	want := []RenamedLabel{{Plane: "Frontend", Yalkos: "Frontend (2)"}, {Plane: "Area", Yalkos: "Area (2)"}, {Plane: "Type", Yalkos: "Type (2)"}}
	if !slices.Equal(p.Report.Labels.Renamed, want) {
		t.Errorf("renamed = %+v, want %+v", p.Report.Labels.Renamed, want)
	}
}

func TestPlanCyclesAndNotMember(t *testing.T) {
	ws, f := surveyFixture(t)
	p, err := MakePlan(ws, emptyExisting())
	if err != nil {
		t.Fatalf("MakePlan: %v", err)
	}
	if p.Report.Cycles != 2 {
		t.Errorf("cycles = %d, want 2 (BET only)", p.Report.Cycles)
	}
	if !slices.Equal(p.Report.NotMember, []string{"GAM"}) {
		t.Errorf("notMember = %v, want [GAM]", p.Report.NotMember)
	}
	for _, pp := range p.Projects {
		if pp.Key == "GAM" {
			t.Error("GAM planned")
		}
	}
	if n := f.Requests("projects/p-gam/work-items/"); n != 0 {
		t.Errorf("GAM work items requested %d times", n)
	}
}

func TestPlanMappedProjectUsesYalkosKey(t *testing.T) {
	alpID := uuid.MustParse("00000000-0000-7000-8000-000000000201")
	ex := mappedExisting(alpID, "ALPHA")
	ex.Map["issue"] = map[string]uuid.UUID{"i-alp-1": uuid.MustParse("00000000-0000-7000-8000-000000000202")}
	ex.Updated = map[string]time.Time{"i-alp-1": time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)}
	p, _ := makeFixturePlan(t, ex)

	pp := findProject(t, p, "ALPHA")
	if !slices.Equal(pp.Changed, []ChangedItem{{PlaneID: "i-alp-1", Key: "ALPHA-1"}}) {
		t.Errorf("changed = %v", pp.Changed)
	}
	findIssue(t, pp, "ALPHA-2")
	if p.Report.Projects[0].Key != "ALPHA" || p.Report.ChangedInPlane[0].Key != "ALPHA-1" {
		t.Errorf("report = %+v / %+v", p.Report.Projects[0], p.Report.ChangedInPlane)
	}
}

func TestPlanCutsNamesBeforeMakingThemUnique(t *testing.T) {
	ws, _ := surveyFixture(t)
	ws.Members = append(ws.Members, Member{ID: "u-long", Email: "long@example.invalid", DisplayName: strings.Repeat("n", 101)})
	alp := projectData(t, ws, "ALP")
	a50 := strings.Repeat("a", 50)
	alp.States = append(alp.States,
		State{ID: "st-alp-long1", Name: a50 + "X", Color: "#123456", Group: "started", Sequence: 36000},
		State{ID: "st-alp-long2", Name: a50 + "Y", Color: "#123456", Group: "started", Sequence: 37000})
	m100 := strings.Repeat("m", 100)
	alp.Modules = append(alp.Modules,
		Module{ID: "md-alp-long1", Name: m100, Status: "planned", SortOrder: 3000},
		Module{ID: "md-alp-long2", Name: m100, Status: "planned", SortOrder: 4000},
		Module{ID: "md-alp-long3", Name: m100 + "Z", Status: "planned", SortOrder: 5000})
	l50 := strings.Repeat("l", 50)
	alp.Labels = append(alp.Labels,
		Label{ID: "lb-alp-long1", Name: l50 + "1", Color: "#123456", SortOrder: 7000},
		Label{ID: "lb-alp-long2", Name: l50 + "2", Color: "#123456", SortOrder: 8000})

	p, err := MakePlan(ws, emptyExisting())
	if err != nil {
		t.Fatalf("MakePlan: %v", err)
	}
	pp := findProject(t, p, "ALP")
	var states []string
	for _, s := range pp.States {
		if strings.HasPrefix(s.PlaneID, "st-alp-long") {
			states = append(states, s.Name)
		}
	}
	if want := []string{a50, strings.Repeat("a", 46) + " (2)"}; !slices.Equal(states, want) {
		t.Errorf("long states = %q, want %q", states, want)
	}
	var milestones []string
	for _, m := range pp.Milestones {
		if strings.HasPrefix(m.Module.ID, "md-alp-long") {
			milestones = append(milestones, m.Name)
		}
	}
	m96 := strings.Repeat("m", 96)
	if want := []string{m100, m96 + " (2)", m96 + " (3)"}; !slices.Equal(milestones, want) {
		t.Errorf("long milestones = %q, want %q", milestones, want)
	}
	l := findLabel(t, p, labelKey("", l50))
	if l.Name != l50 || len(l.PlaneIDs) != 2 {
		t.Errorf("long label = %+v, want one label of 50 runes", l)
	}
	var user UserPlan
	for _, u := range p.Users {
		if u.PlaneID == "u-long" {
			user = u
		}
	}
	if user.Name != strings.Repeat("n", 100) {
		t.Errorf("long user name = %d runes", len(user.Name))
	}
	for _, want := range []string{
		"ALP: " + a50 + " (state name)", "ALP: " + m100 + " (milestone name)", l50 + " (label name)",
		"long@example.invalid (user name)",
	} {
		if !slices.Contains(p.Report.Dropped.CutTexts, want) {
			t.Errorf("cutTexts = %q, want %q in it", p.Report.Dropped.CutTexts, want)
		}
	}
}

func TestPlanColoursLowerCase(t *testing.T) {
	ws, _ := surveyFixture(t)
	alp, bet := projectData(t, ws, "ALP"), projectData(t, ws, "BET")
	alp.Labels = append(alp.Labels, Label{ID: "lb-alp-shared", Name: "Shared", Color: "#ABCDEF", SortOrder: 9000})
	bet.Labels = append(bet.Labels, Label{ID: "lb-bet-shared", Name: "shared", Color: "#abcdef", SortOrder: 9000})
	alp.States = append(alp.States, State{ID: "st-alp-upper", Name: "Review", Color: "#AABBCC", Group: "started", Sequence: 36000})

	p, err := MakePlan(ws, emptyExisting())
	if err != nil {
		t.Fatalf("MakePlan: %v", err)
	}
	if l := findLabel(t, p, labelKey("", "shared")); l.Color != "#abcdef" {
		t.Errorf("shared colour = %q, want #abcdef", l.Color)
	}
	for _, m := range p.Report.Labels.Merged {
		if m.Name == "Shared" && !slices.Equal(m.Colors, []string{"#abcdef"}) {
			t.Errorf("merged Shared colours = %q, want one colour", m.Colors)
		}
	}
	for _, s := range findProject(t, p, "ALP").States {
		if s.PlaneID == "st-alp-upper" && s.Color != "#aabbcc" {
			t.Errorf("Review colour = %q, want #aabbcc", s.Color)
		}
	}
}

func TestPlanCutNamesHaveNoTrailingSpace(t *testing.T) {
	ws, _ := surveyFixture(t)
	alp := projectData(t, ws, "ALP")
	a45 := strings.Repeat("a", 45)
	// 49 runes, not cut; the (2) variant cuts the base right after the space.
	alp.States = append(alp.States,
		State{ID: "st-alp-sp1", Name: a45 + " bcd", Color: "#123456", Group: "started", Sequence: 36000},
		State{ID: "st-alp-sp2", Name: a45 + " bcd", Color: "#123456", Group: "started", Sequence: 37000})
	m99 := strings.Repeat("m", 99)
	alp.Modules = append(alp.Modules, Module{ID: "md-alp-sp", Name: m99 + " tail", Status: "planned", SortOrder: 3000})
	l49 := strings.Repeat("l", 49)
	alp.Labels = append(alp.Labels, Label{ID: "lb-alp-sp", Name: l49 + " tail", Color: "#123456", SortOrder: 7000})

	p, err := MakePlan(ws, emptyExisting())
	if err != nil {
		t.Fatalf("MakePlan: %v", err)
	}
	pp := findProject(t, p, "ALP")
	var states []string
	for _, s := range pp.States {
		if strings.HasPrefix(s.PlaneID, "st-alp-sp") {
			states = append(states, s.Name)
		}
	}
	if want := []string{a45 + " bcd", a45 + " (2)"}; !slices.Equal(states, want) {
		t.Errorf("states = %q, want %q", states, want)
	}
	for _, m := range pp.Milestones {
		if m.Module.ID == "md-alp-sp" && m.Name != m99 {
			t.Errorf("milestone = %q, want %q", m.Name, m99)
		}
	}
	if l := findLabel(t, p, labelKey("", l49+" tail")); l.Name != l49 {
		t.Errorf("label = %q, want %q", l.Name, l49)
	}
	if !slices.Contains(p.Report.Dropped.CutTexts, l49+" (label name)") {
		t.Errorf("cutTexts = %q, want the trimmed label name", p.Report.Dropped.CutTexts)
	}
}
