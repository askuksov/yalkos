package plane

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/webhook"
	"github.com/askuksov/yalkos/internal/store/q"
	"github.com/askuksov/yalkos/internal/testfx"
)

// baseEnv is a fresh Yalkos instance and the surveyed fixture workspace.
type baseEnv struct {
	pool   *pgxpool.Pool
	users  testfx.Users
	clock  func() time.Time
	source uuid.UUID
	ws     *Workspace
}

func newBaseEnv(t *testing.T) baseEnv {
	t.Helper()
	pool, users, clock := testfx.New(t)
	src, err := q.New(pool).UpsertImportSource(context.Background(), q.UpsertImportSourceParams{
		Kind: "plane", SourceID: "ws-acme", Url: "http://plane.example.invalid", Workspace: "acme", Now: clock(),
	})
	if err != nil {
		t.Fatalf("UpsertImportSource: %v", err)
	}
	ws, _ := surveyFixture(t)
	return baseEnv{pool: pool, users: users, clock: clock, source: src.ID, ws: ws}
}

// prepare reads what the instance has now and maps the fixture workspace onto it.
func (e baseEnv) prepare(t *testing.T) (Plan, Existing) {
	t.Helper()
	ex, err := LoadExisting(context.Background(), q.New(e.pool), e.source)
	if err != nil {
		t.Fatalf("LoadExisting: %v", err)
	}
	p, err := MakePlan(e.ws, ex)
	if err != nil {
		t.Fatalf("MakePlan: %v", err)
	}
	return p, ex
}

// base plans and writes the base entities once.
func (e baseEnv) base(t *testing.T) (Plan, Existing) {
	t.Helper()
	p, ex := e.prepare(t)
	if err := NewWriter(e.pool, e.source, e.users.Owner, e.clock).Base(context.Background(), p, &ex); err != nil {
		t.Fatalf("Base: %v", err)
	}
	return p, ex
}

func (e baseEnv) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func (e baseEnv) projectID(t *testing.T, key string) uuid.UUID {
	t.Helper()
	p, err := q.New(e.pool).GetProjectByKey(context.Background(), key)
	if err != nil {
		t.Fatalf("project %s: %v", key, err)
	}
	return p.ID
}

func (e baseEnv) userID(t *testing.T, email string) uuid.UUID {
	t.Helper()
	u, err := q.New(e.pool).GetUserByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("user %s: %v", email, err)
	}
	return u.ID
}

func planeTime(t *testing.T, s string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

// createdLabels are the labels of p that Base creates.
func createdLabels(p Plan) []LabelPlan {
	var out []LabelPlan
	for _, l := range p.Labels {
		if l.Target == uuid.Nil {
			out = append(out, l)
		}
	}
	return out
}

func TestBaseCreatesWithPlaneTimes(t *testing.T) {
	ctx := context.Background()
	e := newBaseEnv(t)
	e.base(t)
	qs := q.New(e.pool)

	alp, err := qs.GetProject(ctx, e.projectID(t, "ALP"))
	if err != nil {
		t.Fatal(err)
	}
	if want := planeTime(t, "2025-01-10T09:00:00Z"); !alp.CreatedAt.Equal(want) {
		t.Errorf("ALP created_at = %v, want %v", alp.CreatedAt, want)
	}
	if want := planeTime(t, "2025-06-01T09:00:00Z"); !alp.UpdatedAt.Equal(want) {
		t.Errorf("ALP updated_at = %v, want %v", alp.UpdatedAt, want)
	}
	if alp.Name != "Alpha" || alp.Description != "The alpha product." {
		t.Errorf("ALP = %q, %q", alp.Name, alp.Description)
	}
	ann := e.userID(t, "ann@example.invalid")
	if alp.CreatedBy == nil || *alp.CreatedBy != ann {
		t.Errorf("ALP created_by = %v, want ann %v", alp.CreatedBy, ann)
	}
	bet, err := qs.GetProject(ctx, e.projectID(t, "BET"))
	if err != nil {
		t.Fatal(err)
	}
	// Plane leaves description_html null and keeps the text in description:
	// stored as it is, newlines and Markdown characters included.
	if want := "Beta keeps *plain* text.\nSecond line."; bet.Description != want {
		t.Errorf("BET description = %q, want %q", bet.Description, want)
	}
	if bet.CreatedBy == nil || *bet.CreatedBy != e.users.Owner.ID {
		t.Errorf("BET (seed bot) created_by = %v, want the actor", bet.CreatedBy)
	}

	// Members: the mapped Plane members and the actor; the seed bot is not one.
	members, err := qs.ListProjectMemberIDs(ctx, alp.ID)
	if err != nil {
		t.Fatal(err)
	}
	bob := e.userID(t, "bob@example.invalid")
	for _, id := range []uuid.UUID{ann, bob, e.users.Owner.ID} {
		if !slices.Contains(members, id) {
			t.Errorf("ALP members %v lack %v", members, id)
		}
	}
	if len(members) != 3 {
		t.Errorf("ALP members = %v, want 3", members)
	}

	// New users are disabled members with a well-formed password hash.
	u, err := qs.GetUserByID(ctx, bob)
	if err != nil {
		t.Fatal(err)
	}
	if !u.Disabled || u.Role != "member" || u.Name != "Bob Baker" || len(u.PasswordHash) < len("$argon2id$") || u.PasswordHash[:10] != "$argon2id$" {
		t.Errorf("bob = disabled %v, role %q, name %q, hash %q", u.Disabled, u.Role, u.Name, u.PasswordHash)
	}

	// Exactly Plane's states in Plane order, triage skipped; Plane's default.
	type st struct {
		name, typ, color string
		def              bool
	}
	wantStates := map[string][]st{
		"ALP": {
			{"Backlog", "backlog", "#bec2c8", false}, {"Todo", "unstarted", "#3a3a3a", true},
			{"In Progress", "started", "#f59e0b", false}, {"Done", "completed", "#95a2b3", false},
			{"Cancelled", "canceled", "#9ca3af", false},
		},
		"BET": {
			{"Backlog", "backlog", "#bec2c8", false}, {"Doing", "started", "#f59e0b", true},
			{"Done", "completed", "#16a34a", false}, {"Dropped", "canceled", "#9ca3af", false},
		},
	}
	for key, want := range wantStates {
		states, err := qs.ListStates(ctx, e.projectID(t, key))
		if err != nil {
			t.Fatal(err)
		}
		var got []st
		for _, s := range states {
			got = append(got, st{s.Name, s.Type, s.Color, s.IsDefault})
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s states = %v, want %v", key, got, want)
		}
	}
	states, err := qs.ListStates(ctx, alp.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		want := planeTime(t, "2025-01-10T09:01:00Z")
		if s.Name == "In Progress" {
			want = planeTime(t, "2025-04-01T08:00:00Z")
		}
		if !s.UpdatedAt.Equal(want) {
			t.Errorf("state %s updated_at = %v, want %v", s.Name, s.UpdatedAt, want)
		}
	}
	if want := planeTime(t, "2025-01-10T09:01:00Z"); !states[0].CreatedAt.Equal(want) {
		t.Errorf("state created_at = %v, want %v", states[0].CreatedAt, want)
	}

	// Milestones keep Plane's order, dates, lead and status.
	ms, err := qs.ListMilestonesByProjects(ctx, []uuid.UUID{alp.ID})
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(ms, func(a, b q.Milestone) int { return int(a.SortOrder - b.SortOrder) })
	if len(ms) != 2 || ms[0].Name != "Launch" || ms[0].SortOrder != 1000 || ms[1].Name != "Polish" || ms[1].SortOrder != 2000 {
		t.Fatalf("ALP milestones = %+v", ms)
	}
	launch := ms[0]
	if launch.Description != "First public version." || launch.StatusManual == nil || *launch.StatusManual != "in_progress" {
		t.Errorf("Launch = %q, status %v", launch.Description, launch.StatusManual)
	}
	if launch.TargetDate == nil || launch.TargetDate.Format(time.DateOnly) != "2026-03-01" {
		t.Errorf("Launch target date = %v", launch.TargetDate)
	}
	if launch.LeadID == nil || *launch.LeadID != ann {
		t.Errorf("Launch lead = %v, want ann", launch.LeadID)
	}
	if want := planeTime(t, "2025-01-11T09:00:00Z"); !launch.CreatedAt.Equal(want) {
		t.Errorf("Launch created_at = %v, want %v", launch.CreatedAt, want)
	}
	if ms[1].StatusManual == nil || *ms[1].StatusManual != "planned" || ms[1].LeadID != nil {
		t.Errorf("Polish = status %v, lead %v", ms[1].StatusManual, ms[1].LeadID)
	}
	if want := "Rough edges first.\nThen the docs."; ms[1].Description != want {
		t.Errorf("Polish description = %q, want %q", ms[1].Description, want)
	}

	// Labels merged across projects are created once, with the earliest time.
	if n := e.count(t, "SELECT count(*) FROM labels WHERE lower(name) = 'frontend'"); n != 1 {
		t.Errorf("Frontend labels = %d, want 1", n)
	}
	var fe q.Label
	labels, err := qs.ListLabels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range labels {
		if l.Name == "Frontend" {
			fe = l
		}
	}
	if fe.Color != "#3b82f6" || !fe.CreatedAt.Equal(planeTime(t, "2025-01-10T10:00:00Z")) || fe.CreatedBy == nil || *fe.CreatedBy != bob {
		t.Errorf("Frontend = %+v", fe)
	}
	// Plane's updated_at is kept: the latest of the merged labels; a label
	// Plane gives none was last updated when it was created.
	if want := planeTime(t, "2025-05-03T08:00:00Z"); !fe.UpdatedAt.Equal(want) {
		t.Errorf("Frontend updated_at = %v, want %v", fe.UpdatedAt, want)
	}
	// The group and its (flattened) labels.
	var area q.Label
	for _, l := range labels {
		if l.Name == "Area" {
			area = l
		}
	}
	if !area.UpdatedAt.Equal(area.CreatedAt) {
		t.Errorf("Area updated_at = %v, want its created_at %v", area.UpdatedAt, area.CreatedAt)
	}
	if !area.IsGroup {
		t.Fatalf("Area = %+v, want a group", area)
	}
	var children []string
	for _, l := range labels {
		if l.ParentID != nil && *l.ParentID == area.ID {
			children = append(children, l.Name)
		}
	}
	slices.Sort(children)
	if !slices.Equal(children, []string{"api", "v2"}) {
		t.Errorf("Area labels = %v", children)
	}
}

func TestBaseMapsEveryEntity(t *testing.T) {
	e := newBaseEnv(t)
	p, ex := e.base(t)

	planeIDs := 0
	for _, l := range createdLabels(p) {
		planeIDs += len(l.PlaneIDs)
	}
	states, milestones := 0, 0
	for _, pp := range p.Projects {
		states += len(pp.States)
		milestones += len(pp.Milestones)
	}
	want := map[string]int{"user": 2, "project": 2, "state": states, "label": planeIDs, "milestone": milestones}
	if states != 9 || milestones != 4 {
		t.Fatalf("fixture: %d states, %d milestones", states, milestones)
	}
	tables := map[string]string{
		"user": "users", "project": "projects", "state": "states", "label": "labels", "milestone": "milestones",
	}
	for kind, n := range want {
		got := e.count(t, "SELECT count(*) FROM import_map WHERE source_id = $1 AND kind = $2", e.source, kind)
		if got != n {
			t.Errorf("import_map %s rows = %d, want %d", kind, got, n)
		}
		dangling := e.count(t, "SELECT count(*) FROM import_map m WHERE m.source_id = $1 AND m.kind = $2 AND NOT EXISTS (SELECT 1 FROM "+
			tables[kind]+" t WHERE t.id = m.target_id)", e.source, kind)
		if dangling != 0 {
			t.Errorf("import_map %s: %d rows point at nothing", kind, dangling)
		}
		if len(ex.Map[kind]) != n {
			t.Errorf("ex.Map[%s] has %d entries, want %d", kind, len(ex.Map[kind]), n)
		}
	}
	if got := e.count(t, "SELECT count(*) FROM import_map WHERE kind NOT IN ('user', 'project', 'state', 'label', 'milestone')"); got != 0 {
		t.Errorf("%d rows of other kinds", got)
	}
	// Both Plane labels merged into Frontend point at the same label.
	if a, b := ex.Map["label"]["lb-alp-frontend"], ex.Map["label"]["lb-bet-frontend"]; a == uuid.Nil || a != b {
		t.Errorf("frontend targets = %v, %v", a, b)
	}
	if got := ex.Map["project"]["p-alp"]; got != e.projectID(t, "ALP") {
		t.Errorf("ex.Map project p-alp = %v", got)
	}
}

func TestBaseWritesCreateEvents(t *testing.T) {
	ctx := context.Background()
	e := newBaseEnv(t)
	p, _ := e.base(t)

	want := map[string]int{"project": 2, "state": 9, "label": len(createdLabels(p)), "milestone": 4}
	for typ, n := range want {
		got := e.count(t, "SELECT count(*) FROM audit_events WHERE entity_type = $1 AND action = 'create'", typ)
		if got != n {
			t.Errorf("%s create events = %d, want %d", typ, got, n)
		}
	}
	if got := e.count(t, "SELECT count(*) FROM audit_events WHERE action <> 'create'"); got != 0 {
		t.Errorf("%d events other than create", got)
	}

	ann := e.userID(t, "ann@example.invalid")
	bob := e.userID(t, "bob@example.invalid")
	type ev struct {
		at    time.Time
		actor uuid.UUID
	}
	event := func(sql string, args ...any) ev {
		t.Helper()
		var out ev
		var actor *uuid.UUID
		if err := e.pool.QueryRow(ctx, sql, args...).Scan(&out.at, &actor); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		if actor != nil {
			out.actor = *actor
		}
		return out
	}
	const byEntity = "SELECT a.created_at, a.actor_id FROM audit_events a WHERE a.action = 'create' AND a.entity_id = $1"
	cases := []struct {
		name string
		got  ev
		want ev
	}{
		{"project ALP", event(byEntity, e.projectID(t, "ALP")), ev{planeTime(t, "2025-01-10T09:00:00Z"), ann}},
		{"project BET (seed bot)", event(byEntity, e.projectID(t, "BET")), ev{planeTime(t, "2025-02-10T09:00:00Z"), e.users.Owner.ID}},
		{"state ALP Todo", event("SELECT a.created_at, a.actor_id FROM audit_events a JOIN states s ON s.id = a.entity_id WHERE s.project_id = $1 AND s.name = 'Todo'",
			e.projectID(t, "ALP")), ev{planeTime(t, "2025-01-10T09:01:00Z"), ann}},
		{"state BET Doing (seed bot)", event("SELECT a.created_at, a.actor_id FROM audit_events a JOIN states s ON s.id = a.entity_id WHERE s.project_id = $1 AND s.name = 'Doing'",
			e.projectID(t, "BET")), ev{planeTime(t, "2025-02-10T09:01:00Z"), e.users.Owner.ID}},
		{"label Frontend", event("SELECT a.created_at, a.actor_id FROM audit_events a JOIN labels l ON l.id = a.entity_id WHERE l.name = 'Frontend'"),
			ev{planeTime(t, "2025-01-10T10:00:00Z"), bob}},
		{"label Area (seed bot)", event("SELECT a.created_at, a.actor_id FROM audit_events a JOIN labels l ON l.id = a.entity_id WHERE l.name = 'Area'"),
			ev{planeTime(t, "2025-01-10T11:00:00Z"), e.users.Owner.ID}},
		{"milestone Launch", event("SELECT a.created_at, a.actor_id FROM audit_events a JOIN milestones m ON m.id = a.entity_id WHERE m.name = 'Launch'"),
			ev{planeTime(t, "2025-01-11T09:00:00Z"), ann}},
		{"milestone Polish (seed bot)", event("SELECT a.created_at, a.actor_id FROM audit_events a JOIN milestones m ON m.id = a.entity_id WHERE m.name = 'Polish'"),
			ev{planeTime(t, "2025-01-12T09:00:00Z"), e.users.Owner.ID}},
	}
	for _, c := range cases {
		if !c.got.at.Equal(c.want.at) || c.got.actor != c.want.actor {
			t.Errorf("%s: event at %v by %v, want at %v by %v", c.name, c.got.at, c.got.actor, c.want.at, c.want.actor)
		}
	}
}

func TestBaseEmitsNoWebhooks(t *testing.T) {
	ctx := context.Background()
	e := newBaseEnv(t)
	svc := webhook.New(e.pool, webhook.Options{Now: e.clock, AllowPrivate: true, BaseURL: "http://yalkos.test"})
	if _, err := svc.Create(ctx, e.users.Owner, webhook.CreateInput{
		URL: "https://hooks.example.invalid/", Name: "all", Resources: event.Resources(), Enabled: true,
	}); err != nil {
		t.Fatalf("create webhook: %v", err)
	}
	e.base(t)
	if n := e.count(t, "SELECT count(*) FROM webhooks"); n != 1 {
		t.Fatalf("webhooks = %d, want 1", n)
	}
	if n := e.count(t, "SELECT count(*) FROM webhook_deliveries"); n != 0 {
		t.Errorf("webhook deliveries = %d, want 0", n)
	}
}

func TestBaseSkipsMapped(t *testing.T) {
	ctx := context.Background()
	e := newBaseEnv(t)
	p, ex := e.base(t)

	tables := []string{"users", "projects", "project_keys", "project_members", "states", "labels", "milestones", "import_map", "audit_events"}
	counts := func() map[string]int {
		out := map[string]int{}
		for _, tbl := range tables {
			out[tbl] = e.count(t, "SELECT count(*) FROM "+tbl)
		}
		return out
	}
	first := counts()

	// The same Plan again: everything is in ex.Map now.
	if err := NewWriter(e.pool, e.source, e.users.Owner, e.clock).Base(ctx, p, &ex); err != nil {
		t.Fatalf("Base with the same Plan: %v", err)
	}
	if got := counts(); !maps.Equal(got, first) {
		t.Errorf("same Plan again: counts %v, want %v", got, first)
	}

	// A fresh run reads the instance and plans again.
	e.base(t)
	if got := counts(); !maps.Equal(got, first) {
		t.Errorf("second run: counts %v, want %v", got, first)
	}
}

func TestBaseSkipsDeletedInYalkos(t *testing.T) {
	ctx := context.Background()
	e := newBaseEnv(t)
	e.base(t)
	alp := e.projectID(t, "ALP")
	if _, err := e.pool.Exec(ctx, "DELETE FROM projects WHERE id = $1", alp); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, "DELETE FROM labels WHERE name = 'Frontend'"); err != nil {
		t.Fatal(err)
	}
	projects := e.count(t, "SELECT count(*) FROM projects")
	labels := e.count(t, "SELECT count(*) FROM labels")

	p, ex := e.prepare(t)
	if err := NewWriter(e.pool, e.source, e.users.Owner, e.clock).Base(ctx, p, &ex); err != nil {
		t.Fatalf("Base: %v", err)
	}
	if got := e.count(t, "SELECT count(*) FROM projects"); got != projects {
		t.Errorf("projects = %d, want %d (a deleted project is not brought back)", got, projects)
	}
	if got := e.count(t, "SELECT count(*) FROM labels"); got != labels {
		t.Errorf("labels = %d, want %d (a deleted label is not brought back)", got, labels)
	}
	want := []string{"label Frontend", "project ALP"}
	got := slices.Clone(p.Report.Dropped.DeletedInYalkos)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("deletedInYalkos = %v, want %v", got, want)
	}
	if _, ok := ex.Map["project"]["p-alp"]; ok {
		t.Error("the deleted project is still a write target")
	}
}

func TestBaseMapsEmailMatchedUsers(t *testing.T) {
	ctx := context.Background()
	e := newBaseEnv(t)
	qs := q.New(e.pool)
	ws, err := qs.GetWorkspace(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ann, err := qs.ImportCreateUser(ctx, q.ImportCreateUserParams{
		WorkspaceID: ws.ID, Email: "ANN@example.invalid", Name: "Ann", PasswordHash: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	users := e.count(t, "SELECT count(*) FROM users")
	_, ex := e.base(t)
	if got := ex.Map["user"]["u-ann"]; got != ann.ID {
		t.Errorf("ex.Map user u-ann = %v, want the matched user %v", got, ann.ID)
	}
	if got := e.count(t, "SELECT count(*) FROM import_map WHERE kind = 'user' AND plane_id = 'u-ann' AND target_id = $1", ann.ID); got != 1 {
		t.Errorf("map rows of the matched user = %d, want 1", got)
	}
	if got := e.count(t, "SELECT count(*) FROM users"); got != users+1 {
		t.Errorf("users = %d, want %d (only bob is created)", got, users+1)
	}

	// The matched user's email changes in Yalkos: a re-run still knows them.
	if _, err := e.pool.Exec(ctx, "UPDATE users SET email = 'ann.new@example.invalid' WHERE id = $1", ann.ID); err != nil {
		t.Fatal(err)
	}
	e.base(t)
	if got := e.count(t, "SELECT count(*) FROM users"); got != users+1 {
		t.Errorf("users after a re-run = %d, want %d", got, users+1)
	}
}

func TestBaseMapsOntoExistingLabel(t *testing.T) {
	ctx := context.Background()
	e := newBaseEnv(t)
	at := planeTime(t, "2024-01-01T00:00:00Z")
	fe, err := q.New(e.pool).ImportCreateLabel(ctx, q.ImportCreateLabelParams{
		Name: "frontend", Color: "#000000", Position: 5000, CreatedAt: at, UpdatedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, ex := e.base(t)
	for _, id := range []string{"lb-alp-frontend", "lb-bet-frontend"} {
		if got := ex.Map["label"][id]; got != fe.ID {
			t.Errorf("ex.Map label %s = %v, want the existing label %v", id, got, fe.ID)
		}
		if got := e.count(t, "SELECT count(*) FROM import_map WHERE kind = 'label' AND plane_id = $1 AND target_id = $2", id, fe.ID); got != 1 {
			t.Errorf("map rows of %s = %d, want 1", id, got)
		}
	}
	if got := e.count(t, "SELECT count(*) FROM labels WHERE lower(name) = 'frontend'"); got != 1 {
		t.Errorf("frontend labels = %d, want 1", got)
	}
	if got := e.count(t, "SELECT count(*) FROM audit_events WHERE entity_id = $1", fe.ID); got != 0 {
		t.Errorf("events of the existing label = %d, want 0", got)
	}
}

func TestBaseMapsStateOntoExisting(t *testing.T) {
	ctx := context.Background()
	e := newBaseEnv(t)
	e.base(t)
	alp := e.projectID(t, "ALP")
	at := planeTime(t, "2025-07-01T00:00:00Z")
	review, err := q.New(e.pool).ImportCreateState(ctx, q.ImportCreateStateParams{
		ProjectID: alp, Name: "review", Type: "started", Color: "#123456", Position: 10, CreatedAt: at, UpdatedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	pd := projectData(t, e.ws, "ALP")
	pd.States = append(pd.States, State{ID: "st-alp-review", Name: "Review", Color: "#654321", Group: "started", Sequence: 36000})
	states := e.count(t, "SELECT count(*) FROM states WHERE project_id = $1", alp)

	p, ex := e.prepare(t)
	sp := findProject(t, p, "ALP").States
	if len(sp) != 1 || sp[0].Target != review.ID {
		t.Fatalf("ALP states to write = %+v, want Review onto the existing state", sp)
	}
	if err := NewWriter(e.pool, e.source, e.users.Owner, e.clock).Base(ctx, p, &ex); err != nil {
		t.Fatalf("Base: %v", err)
	}
	if got := ex.Map["state"]["st-alp-review"]; got != review.ID {
		t.Errorf("ex.Map state st-alp-review = %v, want %v", got, review.ID)
	}
	if got := e.count(t, "SELECT count(*) FROM import_map WHERE kind = 'state' AND plane_id = 'st-alp-review' AND target_id = $1", review.ID); got != 1 {
		t.Errorf("map rows of the reused state = %d, want 1", got)
	}
	if got := e.count(t, "SELECT count(*) FROM states WHERE project_id = $1", alp); got != states {
		t.Errorf("ALP states = %d, want %d (no new state)", got, states)
	}
	if got := e.count(t, "SELECT count(*) FROM audit_events WHERE entity_id = $1", review.ID); got != 0 {
		t.Errorf("events of the reused state = %d, want 0", got)
	}
}

func TestBaseKeyTakenMeanwhile(t *testing.T) {
	for _, c := range []struct{ name, current string }{
		{"current key", "ALP"},
		{"former key", "OTHER"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			e := newBaseEnv(t)
			p, ex := e.prepare(t)

			// Another project takes the key between the survey and the write.
			at := planeTime(t, "2025-08-01T00:00:00Z")
			qs := q.New(e.pool)
			other, err := qs.ImportCreateProject(ctx, q.ImportCreateProjectParams{
				Key: c.current, Name: "Other", CreatedBy: &e.users.Owner.ID, CreatedAt: at, UpdatedAt: at,
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, k := range slices.Compact([]string{c.current, "ALP"}) {
				if err := qs.InsertProjectKey(ctx, q.InsertProjectKeyParams{Key: k, ProjectID: other.ID}); err != nil {
					t.Fatal(err)
				}
			}

			err = NewWriter(e.pool, e.source, e.users.Owner, e.clock).Base(ctx, p, &ex)
			if err == nil || !strings.Contains(err.Error(), "project key ALP was taken") {
				t.Fatalf("Base = %v, want the key ALP taken", err)
			}
			if got := e.count(t, "SELECT count(*) FROM projects"); got != 1 {
				t.Errorf("projects = %d, want only the other one", got)
			}
			if got := e.count(t, "SELECT count(*) FROM states"); got != 0 {
				t.Errorf("states = %d, want 0 (no partial project)", got)
			}
			if got := e.count(t, "SELECT count(*) FROM import_map WHERE kind IN ('project', 'state')"); got != 0 {
				t.Errorf("project and state map rows = %d, want 0", got)
			}
			if _, ok := ex.Map["project"]["p-alp"]; ok {
				t.Error("ex.Map has p-alp")
			}
		})
	}
}

func TestBaseReportsLabelsOfDeletedGroup(t *testing.T) {
	ctx := context.Background()
	e := newBaseEnv(t)
	_, ex := e.base(t)
	// A new Plane label in the group, then the group and its labels go in Yalkos.
	pd := projectData(t, e.ws, "ALP")
	area := "lb-alp-area"
	pd.Labels = append(pd.Labels, Label{ID: "lb-alp-web", Name: "web", Color: "#123456", Parent: &area, SortOrder: 9000})
	if _, err := e.pool.Exec(ctx, "DELETE FROM labels WHERE parent_id = $1", ex.Map["label"]["lb-alp-area"]); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, "DELETE FROM labels WHERE id = $1", ex.Map["label"]["lb-alp-area"]); err != nil {
		t.Fatal(err)
	}
	labels := e.count(t, "SELECT count(*) FROM labels")

	p, ex2 := e.prepare(t)
	if err := NewWriter(e.pool, e.source, e.users.Owner, e.clock).Base(ctx, p, &ex2); err != nil {
		t.Fatalf("Base: %v", err)
	}
	if got := e.count(t, "SELECT count(*) FROM labels"); got != labels {
		t.Errorf("labels = %d, want %d", got, labels)
	}
	if !slices.Contains(p.Report.Dropped.DeletedInYalkos, "label Area/web (group deleted)") {
		t.Errorf("deletedInYalkos = %q, want the new label of the deleted group", p.Report.Dropped.DeletedInYalkos)
	}
}
