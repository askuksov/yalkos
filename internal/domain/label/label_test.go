package label_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/testfx"
)

func ptr[T any](v T) *T { return &v }

type env struct {
	pool   *pgxpool.Pool
	users  testfx.Users
	labels *label.Service
	issues *issue.Service
	ylk    project.Project
	types  label.TypeInfo
}

func newEnv(t *testing.T) env {
	t.Helper()
	ctx := context.Background()
	pool, users, clock := testfx.New(t)
	projects := project.New(pool, clock)
	e := env{pool: pool, users: users, labels: label.New(pool, clock), issues: issue.New(pool, projects, clock)}
	p, err := projects.Create(ctx, users.Admin, project.CreateInput{Key: "YLK", Name: "Yalkos"})
	if err != nil {
		t.Fatal(err)
	}
	e.ylk = p
	if e.types, err = e.labels.TypeInfo(ctx); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e env) create(t *testing.T, actor account.User, in label.CreateInput) label.Label {
	t.Helper()
	if in.Color == "" {
		in.Color = "#AABBCC"
	}
	l, err := e.labels.Create(context.Background(), actor, in)
	if err != nil {
		t.Fatalf("Create %q: %v", in.Name, err)
	}
	return l
}

func (e env) byName(t *testing.T, name string) label.Label {
	t.Helper()
	all, err := e.labels.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range all {
		if l.Name == name {
			return l
		}
	}
	t.Fatalf("no label %q", name)
	return label.Label{}
}

// issueWith creates an issue and attaches labels directly (issues learn labels later).
func (e env) issueWith(t *testing.T, labels ...label.Label) issue.Issue {
	t.Helper()
	ctx := context.Background()
	i, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ProjectID: &e.ylk.ID, Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range labels {
		if _, err := e.pool.Exec(ctx, `INSERT INTO issue_labels (issue_id, label_id, exclusive_group_id)
			SELECT $1, l.id, CASE WHEN g.exclusive THEN g.id END FROM labels l LEFT JOIN labels g ON g.id = l.parent_id
			WHERE l.id = $2`, i.ID, l.ID); err != nil {
			t.Fatal(err)
		}
	}
	return i
}

func (e env) issueLabels(t *testing.T, id uuid.UUID) []string {
	t.Helper()
	rows, err := e.pool.Query(context.Background(), `SELECT l.name || ':' || coalesce(il.exclusive_group_id::text, '-')
		FROM issue_labels il JOIN labels l ON l.id = il.label_id WHERE il.issue_id = $1 ORDER BY l.name`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// wantInput asserts an InputError and returns its params.
func wantInput(t *testing.T, err error, field string, reason kit.Reason) map[string]any {
	t.Helper()
	var ie *kit.InputError
	if !errors.As(err, &ie) || ie.Field != field || ie.Reason != reason {
		t.Fatalf("err = %v, want InputError %s %s", err, field, reason)
	}
	return ie.Params
}

func wantForbidden(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, kit.ErrForbidden) {
		t.Fatalf("err = %v, want Forbidden", err)
	}
}

func TestTypeInfo(t *testing.T) {
	e := newEnv(t)
	task := e.byName(t, "Task")
	if e.types.DefaultID == nil || *e.types.DefaultID != task.ID || len(e.types.LabelIDs) != 4 || task.ParentID == nil || *task.ParentID != e.types.GroupID {
		t.Errorf("TypeInfo = %+v, task = %+v", e.types, task)
	}
	g := e.byName(t, "Type")
	if !g.IsGroup || !g.Exclusive || g.ID != e.types.GroupID {
		t.Errorf("type group = %+v", g)
	}
}

func TestCreateLabelPermissions(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	free := e.create(t, e.users.Member, label.CreateInput{Name: "Perf", Description: "fast"})
	if free.Color != "#aabbcc" || free.ParentID != nil || free.IsGroup || free.Description != "fast" {
		t.Errorf("free = %+v", free)
	}
	group := e.create(t, e.users.Admin, label.CreateInput{Name: "Area", IsGroup: true})
	if !group.IsGroup || !group.Exclusive {
		t.Errorf("group = %+v, want exclusive by default", group)
	}
	open := e.create(t, e.users.Admin, label.CreateInput{Name: "Tags", IsGroup: true, Exclusive: ptr(false)})
	if open.Exclusive {
		t.Errorf("open group exclusive = true")
	}
	inGroup := e.create(t, e.users.Member, label.CreateInput{Name: "Backend", ParentID: &group.ID})
	if inGroup.ParentID == nil || *inGroup.ParentID != group.ID {
		t.Errorf("inGroup = %+v", inGroup)
	}
	_, err := e.labels.Create(ctx, e.users.Member, label.CreateInput{Name: "G2", Color: "#000000", IsGroup: true})
	wantForbidden(t, err)
	_, err = e.labels.Create(ctx, e.users.Member, label.CreateInput{Name: "Chore", Color: "#000000", ParentID: &e.types.GroupID})
	wantForbidden(t, err)
	chore := e.create(t, e.users.Admin, label.CreateInput{Name: "Chore", ParentID: &e.types.GroupID})
	if chore.ParentID == nil || *chore.ParentID != e.types.GroupID {
		t.Errorf("chore = %+v", chore)
	}
	_, err = e.labels.Create(ctx, e.users.Member, label.CreateInput{Name: "X", Color: "red"})
	wantInput(t, err, "color", kit.ReasonInvalidFormat)
	_, err = e.labels.Create(ctx, e.users.Member, label.CreateInput{Name: strings.Repeat("x", 51), Color: "#000000"})
	wantInput(t, err, "name", kit.ReasonTooLong)
	_, err = e.labels.Create(ctx, e.users.Member, label.CreateInput{Name: "X", Color: "#000000", Description: strings.Repeat("d", 201)})
	wantInput(t, err, "description", kit.ReasonTooLong)
	_, err = e.labels.Create(ctx, e.users.Member, label.CreateInput{Name: "X", Color: "#000000", Exclusive: ptr(true)})
	wantInput(t, err, "exclusive", kit.ReasonNotAllowed)
	missing := uuid.New()
	_, err = e.labels.Create(ctx, e.users.Member, label.CreateInput{Name: "X", Color: "#000000", ParentID: &missing})
	wantInput(t, err, "parentId", kit.ReasonNotFound)
	hist, err := audit.New(e.pool).List(ctx, free.ID, page.Args{First: ptr(10)})
	if err != nil || len(hist.Items) != 1 || hist.Items[0].Action != "create" {
		t.Errorf("audit = %+v, %v", hist.Items, err)
	}
}

func TestLabelNameUniqueAmongSiblings(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.create(t, e.users.Member, label.CreateInput{Name: "UI"})
	_, err := e.labels.Create(ctx, e.users.Member, label.CreateInput{Name: "ui", Color: "#000000"})
	wantInput(t, err, "name", kit.ReasonTaken)
	g := e.create(t, e.users.Admin, label.CreateInput{Name: "Area", IsGroup: true})
	e.create(t, e.users.Member, label.CreateInput{Name: "UI", ParentID: &g.ID})
}

func TestNoNestedGroups(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	g := e.create(t, e.users.Admin, label.CreateInput{Name: "Area", IsGroup: true})
	_, err := e.labels.Create(ctx, e.users.Admin, label.CreateInput{Name: "Sub", Color: "#000000", IsGroup: true, ParentID: &g.ID})
	wantInput(t, err, "parentId", kit.ReasonNotAllowed)
	free := e.create(t, e.users.Member, label.CreateInput{Name: "Free"})
	_, err = e.labels.Create(ctx, e.users.Member, label.CreateInput{Name: "Child", Color: "#000000", ParentID: &free.ID})
	wantInput(t, err, "parentId", kit.ReasonNotAllowed)
	other := e.create(t, e.users.Admin, label.CreateInput{Name: "Other", IsGroup: true})
	_, err = e.labels.Update(ctx, e.users.Admin, g.ID, label.Patch{ParentID: kit.Opt[uuid.UUID]{Set: true, Value: &other.ID}})
	wantInput(t, err, "parentId", kit.ReasonNotAllowed)
}

func TestUpdateLabel(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	l := e.create(t, e.users.Member, label.CreateInput{Name: "Perf"})
	_, err := e.labels.Update(ctx, e.users.Member, l.ID, label.Patch{Name: ptr("Speed")})
	wantForbidden(t, err)
	u, err := e.labels.Update(ctx, e.users.Admin, l.ID, label.Patch{Name: ptr(" Speed "), Color: ptr("#00FF00"), Description: ptr("d"), Position: ptr(5.5)})
	if err != nil || u.Name != "Speed" || u.Color != "#00ff00" || u.Description != "d" || u.Position != 5.5 {
		t.Fatalf("update = %+v, %v", u, err)
	}
	_, err = e.labels.Update(ctx, e.users.Admin, l.ID, label.Patch{Exclusive: ptr(true)})
	wantInput(t, err, "exclusive", kit.ReasonNotAllowed)
	if _, err := e.labels.Update(ctx, e.users.Admin, uuid.New(), label.Patch{Name: ptr("x")}); !errors.Is(err, kit.ErrNotFound) {
		t.Errorf("missing = %v", err)
	}
}

func TestMoveIntoExclusiveGroupConflict(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	g := e.create(t, e.users.Admin, label.CreateInput{Name: "G", IsGroup: true})
	a := e.create(t, e.users.Member, label.CreateInput{Name: "A", ParentID: &g.ID})
	b := e.create(t, e.users.Member, label.CreateInput{Name: "B"})
	i := e.issueWith(t, a, b)
	_, err := e.labels.Update(ctx, e.users.Admin, b.ID, label.Patch{ParentID: kit.Opt[uuid.UUID]{Set: true, Value: &g.ID}})
	params := wantInput(t, err, "parentId", kit.ReasonNotAllowed)
	if params["conflicts"] != 1 {
		t.Errorf("params = %v, want conflicts 1", params)
	}
	if _, err := e.pool.Exec(ctx, `DELETE FROM issue_labels WHERE label_id = $1`, a.ID); err != nil {
		t.Fatal(err)
	}
	moved, err := e.labels.Update(ctx, e.users.Admin, b.ID, label.Patch{ParentID: kit.Opt[uuid.UUID]{Set: true, Value: &g.ID}})
	if err != nil || moved.ParentID == nil || *moved.ParentID != g.ID {
		t.Fatalf("move = %+v, %v", moved, err)
	}
	if got := e.issueLabels(t, i.ID); !slices.Equal(got, []string{"B:" + g.ID.String()}) {
		t.Errorf("issue labels = %v", got)
	}
	// Out of the group again: exclusive_group_id is cleared.
	if _, err := e.labels.Update(ctx, e.users.Admin, b.ID, label.Patch{ParentID: kit.Opt[uuid.UUID]{Set: true}}); err != nil {
		t.Fatal(err)
	}
	if got := e.issueLabels(t, i.ID); !slices.Equal(got, []string{"B:-"}) {
		t.Errorf("issue labels after leaving = %v", got)
	}
}

func TestMakeGroupExclusiveConflict(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	g := e.create(t, e.users.Admin, label.CreateInput{Name: "G", IsGroup: true, Exclusive: ptr(false)})
	a := e.create(t, e.users.Member, label.CreateInput{Name: "A", ParentID: &g.ID})
	b := e.create(t, e.users.Member, label.CreateInput{Name: "B", ParentID: &g.ID})
	i := e.issueWith(t, a, b)
	e.issueWith(t, a, b)
	e.issueWith(t, a)
	_, err := e.labels.Update(ctx, e.users.Admin, g.ID, label.Patch{Exclusive: ptr(true)})
	params := wantInput(t, err, "exclusive", kit.ReasonNotAllowed)
	if params["conflicts"] != 2 {
		t.Errorf("params = %v, want conflicts 2", params)
	}
	if _, err := e.pool.Exec(ctx, `DELETE FROM issue_labels WHERE label_id = $1`, b.ID); err != nil {
		t.Fatal(err)
	}
	u, err := e.labels.Update(ctx, e.users.Admin, g.ID, label.Patch{Exclusive: ptr(true)})
	if err != nil || !u.Exclusive {
		t.Fatalf("exclusive = %+v, %v", u, err)
	}
	if got := e.issueLabels(t, i.ID); !slices.Equal(got, []string{"A:" + g.ID.String()}) {
		t.Errorf("issue labels = %v", got)
	}
	if _, err := e.labels.Update(ctx, e.users.Admin, g.ID, label.Patch{Exclusive: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if got := e.issueLabels(t, i.ID); !slices.Equal(got, []string{"A:-"}) {
		t.Errorf("issue labels after non-exclusive = %v", got)
	}
}

func TestDeleteGroupOnlyEmpty(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	g := e.create(t, e.users.Admin, label.CreateInput{Name: "G", IsGroup: true})
	a := e.create(t, e.users.Member, label.CreateInput{Name: "A", ParentID: &g.ID})
	wantForbidden(t, e.labels.Delete(ctx, e.users.Member, a.ID))
	wantInput(t, e.labels.Delete(ctx, e.users.Admin, g.ID), "id", kit.ReasonNotAllowed)
	if err := e.labels.Delete(ctx, e.users.Admin, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.labels.Delete(ctx, e.users.Admin, g.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.labels.Delete(ctx, e.users.Admin, g.ID); !errors.Is(err, kit.ErrNotFound) {
		t.Errorf("second delete = %v", err)
	}
}

func TestTypeGroupProtected(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	wantInput(t, e.labels.Delete(ctx, e.users.Owner, e.types.GroupID), "id", kit.ReasonNotAllowed)
	_, err := e.labels.Update(ctx, e.users.Owner, e.types.GroupID, label.Patch{Exclusive: ptr(false)})
	wantInput(t, err, "exclusive", kit.ReasonNotAllowed)
	r, err := e.labels.Update(ctx, e.users.Owner, e.types.GroupID, label.Patch{Name: ptr("Kind")})
	if err != nil || r.Name != "Kind" {
		t.Fatalf("rename = %+v, %v", r, err)
	}
	ti, err := e.labels.TypeInfo(ctx)
	if err != nil || ti.GroupID != e.types.GroupID {
		t.Errorf("TypeInfo after rename = %+v, %v", ti, err)
	}
	// Type labels: only Owner/Admin may edit; they can be recoloured and deleted.
	bug := e.byName(t, "Bug")
	_, err = e.labels.Update(ctx, e.users.Member, bug.ID, label.Patch{Color: ptr("#000000")})
	wantForbidden(t, err)
	free := e.create(t, e.users.Member, label.CreateInput{Name: "Free"})
	_, err = e.labels.Update(ctx, e.users.Admin, free.ID, label.Patch{ParentID: kit.Opt[uuid.UUID]{Set: true, Value: &e.types.GroupID}})
	if err != nil {
		t.Errorf("admin moves a label into Type: %v", err)
	}
}

func TestDeleteLabelRemovesFromIssues(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	perf := e.create(t, e.users.Member, label.CreateInput{Name: "Perf"})
	sec := e.create(t, e.users.Member, label.CreateInput{Name: "Sec"})
	i := e.issueWith(t, perf, sec)
	if err := e.labels.Delete(ctx, e.users.Admin, perf.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.issueLabels(t, i.ID); !slices.Equal(got, []string{"Sec:-"}) {
		t.Errorf("issue labels = %v", got)
	}
	hist, err := audit.New(e.pool).List(ctx, i.ID, page.Args{Last: ptr(1)})
	if err != nil || len(hist.Items) != 1 {
		t.Fatal(err)
	}
	ch := hist.Items[0].Changes
	ids := []string{perf.ID.String(), sec.ID.String()}
	slices.Sort(ids)
	var from, to []string
	if len(ch) == 1 {
		_ = json.Unmarshal(ch[0].From, &from)
		_ = json.Unmarshal(ch[0].To, &to)
	}
	if hist.Items[0].Action != "update" || len(ch) != 1 || ch[0].Field != "labelIds" ||
		!slices.Equal(from, ids) || !slices.Equal(to, []string{sec.ID.String()}) {
		t.Errorf("issue audit = %+v %s -> %s", hist.Items[0], ch[0].From, ch[0].To)
	}
}

func TestSetDefaultType(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	bug := e.byName(t, "Bug")
	wantForbidden(t, e.labels.SetDefaultType(ctx, e.users.Member, &bug.ID))
	if err := e.labels.SetDefaultType(ctx, e.users.Admin, &bug.ID); err != nil {
		t.Fatal(err)
	}
	ti, _ := e.labels.TypeInfo(ctx)
	if ti.DefaultID == nil || *ti.DefaultID != bug.ID {
		t.Errorf("default = %v, want Bug", ti.DefaultID)
	}
	free := e.create(t, e.users.Member, label.CreateInput{Name: "Free"})
	wantInput(t, e.labels.SetDefaultType(ctx, e.users.Admin, &free.ID), "labelId", kit.ReasonNotAllowed)
	if err := e.labels.Delete(ctx, e.users.Admin, bug.ID); err != nil {
		t.Fatal(err)
	}
	ti, _ = e.labels.TypeInfo(ctx)
	if ti.DefaultID != nil || len(ti.LabelIDs) != 3 {
		t.Errorf("after deleting default: %+v", ti)
	}
	if err := e.labels.SetDefaultType(ctx, e.users.Admin, nil); err != nil {
		t.Errorf("clear default: %v", err)
	}
}

func TestByIDs(t *testing.T) {
	e := newEnv(t)
	a := e.create(t, e.users.Member, label.CreateInput{Name: "A"})
	b := e.create(t, e.users.Member, label.CreateInput{Name: "B"})
	got, err := e.labels.ByIDs(context.Background(), []uuid.UUID{b.ID, uuid.New(), a.ID})
	if err != nil || len(got) != 2 || got[0].ID != b.ID || got[1].ID != a.ID {
		t.Errorf("ByIDs = %+v, %v", got, err)
	}
}
