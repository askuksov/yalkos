package plane

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/store/q"
	"github.com/askuksov/yalkos/internal/testfx"
)

func TestSurveyReadsChosenProjects(t *testing.T) {
	ws, f := surveyFixture(t, "alp")
	if len(ws.Projects) != 1 || ws.Projects[0].Identifier != "ALP" {
		t.Fatalf("projects = %+v, want only ALP", ws.Projects)
	}
	pd := ws.Projects[0]
	if len(pd.States) != 6 || len(pd.Labels) != 6 || len(pd.Modules) != 2 || len(pd.Items) != 5 || len(pd.MemberIDs) != 3 {
		t.Errorf("ALP lists: %d states, %d labels, %d modules, %d items, %d members",
			len(pd.States), len(pd.Labels), len(pd.Modules), len(pd.Items), len(pd.MemberIDs))
	}
	if got := pd.ModuleIssues["md-alp-polish"]; len(got) != 2 || got[0] != "i-alp-2" {
		t.Errorf("module issues = %v", got)
	}
	if len(ws.Members) != 3 {
		t.Errorf("members = %d, want 3", len(ws.Members))
	}
	// Every listed project stays known, to name relations into other projects.
	for _, id := range []string{"p-alp", "p-bet", "p-old"} {
		if _, ok := ws.AllProjects[id]; !ok {
			t.Errorf("AllProjects has no %s", id)
		}
	}
	if n := f.Requests("projects/p-bet/work-items/"); n != 0 {
		t.Errorf("BET work items requested %d times", n)
	}
}

func TestSurveyUnknownProject(t *testing.T) {
	f := loadFixture(t, filepath.Join("testdata", "plane"))
	_, err := Survey(context.Background(), newTestClient(t, f, newFakeClock()), []string{"ALP", "ZZZ"})
	var ue *UsageError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %v, want *UsageError", err)
	}
	if !strings.Contains(ue.Error(), "ZZZ") || strings.Contains(ue.Error(), "ALP") {
		t.Errorf("message = %q, want only ZZZ named", ue.Error())
	}
	if n := f.Requests("projects/p-alp/states/"); n != 0 {
		t.Errorf("states requested %d times before the usage error", n)
	}
}

func TestChooseProjectsMatchesAsSelected(t *testing.T) {
	// The identifier starts with the Kelvin sign (U+212A), which folds to K
	// under Unicode case folding but is not an ASCII K.
	projects := []Project{{ID: "p-kel", Identifier: "KEL"}, {ID: "p-alp", Identifier: "ALP"}}
	_, err := chooseProjects(projects, []string{"KEL"})
	var ue *UsageError
	if !errors.As(err, &ue) || !strings.Contains(ue.Error(), "KEL") {
		t.Errorf("err = %v, want a usage error naming KEL", err)
	}
	out, err := chooseProjects(projects, []string{" alp "})
	if err != nil || len(out) != 1 || out[0].ID != "p-alp" {
		t.Errorf("chosen = %+v, %v; want ALP", out, err)
	}
}

func TestConflictErrorNamesKeys(t *testing.T) {
	err := error(&ConflictError{Keys: []string{"ALP", "TOOLONGKEY"}})
	if !strings.Contains(err.Error(), "ALP") || !strings.Contains(err.Error(), "TOOLONGKEY") {
		t.Errorf("message = %q", err.Error())
	}
}

func TestLoadExisting(t *testing.T) {
	ctx := context.Background()
	pool, users, _ := testfx.New(t)
	qs := q.New(pool)
	at := time.Date(2025, 5, 1, 10, 0, 0, 0, time.UTC)

	var wsID uuid.UUID
	if err := pool.QueryRow(ctx, "SELECT id FROM workspaces LIMIT 1").Scan(&wsID); err != nil {
		t.Fatal(err)
	}
	ann, err := qs.ImportCreateUser(ctx, q.ImportCreateUserParams{
		WorkspaceID: wsID, Email: "ANN@example.invalid", Name: "Ann", PasswordHash: "x",
	})
	if err != nil {
		t.Fatalf("ImportCreateUser: %v", err)
	}
	p, err := qs.ImportCreateProject(ctx, q.ImportCreateProjectParams{
		Key: "ALP", Name: "Alpha", CreatedBy: &users.Owner.ID, CreatedAt: at, UpdatedAt: at,
	})
	if err != nil {
		t.Fatalf("ImportCreateProject: %v", err)
	}
	for _, k := range []string{"ALP", "OLDALP"} {
		if err := qs.InsertProjectKey(ctx, q.InsertProjectKeyParams{Key: k, ProjectID: p.ID}); err != nil {
			t.Fatalf("InsertProjectKey: %v", err)
		}
	}
	todo, err := qs.ImportCreateState(ctx, q.ImportCreateStateParams{
		ProjectID: p.ID, Name: "Todo", Type: "unstarted", Color: "#95a2b3", IsDefault: true, CreatedAt: at, UpdatedAt: at,
	})
	if err != nil {
		t.Fatalf("ImportCreateState: %v", err)
	}
	launch, err := qs.ImportCreateMilestone(ctx, q.ImportCreateMilestoneParams{
		ProjectID: p.ID, Name: "Launch", StatusManual: "planned", CreatedAt: at, UpdatedAt: at,
	})
	if err != nil {
		t.Fatalf("ImportCreateMilestone: %v", err)
	}
	group, err := qs.ImportCreateLabel(ctx, q.ImportCreateLabelParams{IsGroup: true, Name: "Area", Color: "#6e7781", CreatedAt: at, UpdatedAt: at})
	if err != nil {
		t.Fatalf("ImportCreateLabel: %v", err)
	}
	child, err := qs.ImportCreateLabel(ctx, q.ImportCreateLabelParams{ParentID: &group.ID, Name: "API", Color: "#6e7781", CreatedAt: at, UpdatedAt: at})
	if err != nil {
		t.Fatalf("ImportCreateLabel: %v", err)
	}
	src, err := qs.UpsertImportSource(ctx, q.UpsertImportSourceParams{Kind: "plane", SourceID: "ws-acme", Url: "http://plane.example.invalid", Workspace: "acme", Now: at})
	if err != nil {
		t.Fatalf("UpsertImportSource: %v", err)
	}
	issueID := uuid.MustParse("00000000-0000-7000-8000-0000000000d1")
	for _, m := range []q.InsertImportMapParams{
		{SourceID: src.ID, Kind: "project", PlaneID: "p-alp", TargetID: p.ID, Now: at},
		{SourceID: src.ID, Kind: "issue", PlaneID: "i-alp-1", TargetID: issueID, SourceUpdatedAt: &at, Now: at},
	} {
		if err := qs.InsertImportMap(ctx, m); err != nil {
			t.Fatalf("InsertImportMap: %v", err)
		}
	}

	ex, err := LoadExisting(ctx, qs, src.ID)
	if err != nil {
		t.Fatalf("LoadExisting: %v", err)
	}
	if ex.Map["project"]["p-alp"] != p.ID || ex.Map["issue"]["i-alp-1"] != issueID {
		t.Errorf("map = %v", ex.Map)
	}
	if got := ex.Updated["i-alp-1"]; !got.Equal(at) {
		t.Errorf("updated = %v, want %v", got, at)
	}
	if ex.Users["ann@example.invalid"] != ann.ID || ex.Users["owner@example.com"] != users.Owner.ID {
		t.Errorf("users = %v", ex.Users)
	}
	if ex.ProjectKeys["ALP"] != p.ID || ex.ProjectKeys["OLDALP"] != p.ID {
		t.Errorf("project keys = %v", ex.ProjectKeys)
	}
	if ex.Labels["area"] != group.ID || ex.GroupLabels[group.ID]["api"] != child.ID {
		t.Errorf("labels = %v, group labels = %v", ex.Labels, ex.GroupLabels)
	}
	if _, ok := ex.Labels["api"]; ok {
		t.Error("a child label is listed as top level")
	}
	if ex.ProjectKeyByID[p.ID] != "ALP" {
		t.Errorf("key by id = %v", ex.ProjectKeyByID)
	}
	if ex.States[p.ID]["todo"] != todo.ID || ex.Milestones[p.ID]["launch"] != launch.ID {
		t.Errorf("states = %v, milestones = %v", ex.States, ex.Milestones)
	}
	if ex.LabelKinds["area"] != (LabelKind{Group: true}) || ex.LabelKinds["type"] != (LabelKind{Group: true, Exclusive: true}) {
		t.Errorf("label kinds = %v", ex.LabelKinds)
	}
	if ex.Type.DefaultID == nil || ex.TypeNames["task"] != *ex.Type.DefaultID || len(ex.TypeNames) != 4 {
		t.Errorf("type names = %v, default %v", ex.TypeNames, ex.Type.DefaultID)
	}
	if _, ok := ex.TypeNames["bug"]; !ok {
		t.Error("no Bug type")
	}

	// No source yet: nothing imported, the rest still read.
	fresh, err := LoadExisting(ctx, qs, uuid.Nil)
	if err != nil {
		t.Fatalf("LoadExisting(no source): %v", err)
	}
	if len(fresh.Map["issue"]) != 0 || fresh.ProjectKeys["ALP"] != p.ID {
		t.Errorf("fresh = %+v", fresh)
	}
}

func TestSurveyProjectNotMember(t *testing.T) {
	f := loadFixture(t, filepath.Join("testdata", "plane"))
	_, err := Survey(context.Background(), newTestClient(t, f, newFakeClock()), []string{"gam"})
	var ue *UsageError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %v, want *UsageError", err)
	}
	if !strings.Contains(ue.Error(), "GAM") || !strings.Contains(ue.Error(), "join") {
		t.Errorf("message = %q, want GAM and a hint to join", ue.Error())
	}
}
