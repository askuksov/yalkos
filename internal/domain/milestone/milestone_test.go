package milestone_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/testfx"
)

func ptr[T any](v T) *T { return &v }

type env struct {
	pool       *pgxpool.Pool
	users      testfx.Users
	projects   *project.Service
	issues     *issue.Service
	milestones *milestone.Service
	ylk        project.Project
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool, users, clock := testfx.New(t)
	projects := project.New(pool, clock)
	e := env{pool: pool, users: users, projects: projects, issues: issue.New(pool, projects, clock),
		milestones: milestone.New(pool, projects, clock)}
	p, err := projects.Create(context.Background(), users.Admin, project.CreateInput{Key: "YLK", Name: "Yalkos"})
	if err != nil {
		t.Fatal(err)
	}
	e.ylk = p
	return e
}

func (e env) create(t *testing.T, name string) milestone.Milestone {
	t.Helper()
	m, err := e.milestones.Create(context.Background(), e.users.Member, e.ylk.ID, milestone.CreateInput{Name: name})
	if err != nil {
		t.Fatalf("Create %q: %v", name, err)
	}
	return m
}

func (e env) stateID(t *testing.T, name string) uuid.UUID {
	t.Helper()
	states, err := e.projects.States(context.Background(), e.ylk.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		if s.Name == name {
			return s.ID
		}
	}
	t.Fatalf("no state %q", name)
	return uuid.Nil
}

// issueIn creates an issue in state and puts it in milestone m (by SQL: issues learn milestones later).
func (e env) issueIn(t *testing.T, m milestone.Milestone, state string) issue.Issue {
	t.Helper()
	st := e.stateID(t, state)
	i, err := e.issues.Create(context.Background(), e.users.Member, issue.CreateInput{ProjectID: &e.ylk.ID, Title: "t", StateID: &st})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE issues SET milestone_id = $1 WHERE id = $2`, m.ID, i.ID); err != nil {
		t.Fatal(err)
	}
	return i
}

func wantInput(t *testing.T, err error, field string, reason kit.Reason) {
	t.Helper()
	var ie *kit.InputError
	if !errors.As(err, &ie) || ie.Field != field || ie.Reason != reason {
		t.Fatalf("err = %v, want InputError %s %s", err, field, reason)
	}
}

func TestCreateMilestone(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := e.create(t, "Bank APIs")
	b := e.create(t, "Launch")
	if a.Name != "Bank APIs" || a.ProjectID != e.ylk.ID || a.CreatedBy == nil || *a.CreatedBy != e.users.Member.ID || a.Manual != nil {
		t.Errorf("milestone = %+v", a)
	}
	if b.SortOrder <= a.SortOrder {
		t.Errorf("sort orders %v, %v: new milestone must go to the bottom", a.SortOrder, b.SortOrder)
	}
	_, err := e.milestones.Create(ctx, e.users.Member, e.ylk.ID, milestone.CreateInput{Name: "bank apis"})
	wantInput(t, err, "name", kit.ReasonTaken)
	_, err = e.milestones.Create(ctx, e.users.Member, e.ylk.ID, milestone.CreateInput{Name: "  "})
	wantInput(t, err, "name", kit.ReasonRequired)
	_, err = e.milestones.Create(ctx, e.users.Member, e.ylk.ID, milestone.CreateInput{Name: strings.Repeat("x", 101)})
	wantInput(t, err, "name", kit.ReasonTooLong)
	missing := uuid.New()
	_, err = e.milestones.Create(ctx, e.users.Member, e.ylk.ID, milestone.CreateInput{Name: "L", LeadID: &missing})
	wantInput(t, err, "leadId", kit.ReasonNotFound)
	disabled := testfx.DisabledUser(t, e.pool, e.users.Owner)
	_, err = e.milestones.Create(ctx, e.users.Member, e.ylk.ID, milestone.CreateInput{Name: "L", LeadID: &disabled.ID})
	wantInput(t, err, "leadId", kit.ReasonNotAllowed)
	date := time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC)
	c, err := e.milestones.Create(ctx, e.users.Member, e.ylk.ID, milestone.CreateInput{Name: "Dated", TargetDate: &date, LeadID: &e.users.Admin.ID})
	if err != nil || c.TargetDate == nil || c.TargetDate.Format("2006-01-02 15:04") != "2026-05-01 00:00" || *c.LeadID != e.users.Admin.ID {
		t.Fatalf("dated = %+v, %v", c, err)
	}
	got, err := e.milestones.Get(ctx, c.ID)
	if err != nil || got.Name != "Dated" {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if _, err := e.milestones.Get(ctx, uuid.New()); !errors.Is(err, kit.ErrNotFound) {
		t.Errorf("Get missing = %v", err)
	}
}

func TestStatusManualAndBack(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	m := e.create(t, "M")
	e.issueIn(t, m, "Done")
	m, err := e.milestones.Update(ctx, e.users.Member, m.ID, milestone.Patch{Status: kit.Opt[milestone.Status]{Set: true, Value: ptr(milestone.Canceled)}})
	if err != nil {
		t.Fatal(err)
	}
	prog, err := e.milestones.Progress(ctx, []uuid.UUID{m.ID})
	if err != nil {
		t.Fatal(err)
	}
	if m.Status(prog[m.ID]) != milestone.Canceled {
		t.Errorf("status = %s, want canceled", m.Status(prog[m.ID]))
	}
	m, err = e.milestones.Update(ctx, e.users.Member, m.ID, milestone.Patch{Status: kit.Opt[milestone.Status]{Set: true}})
	if err != nil {
		t.Fatal(err)
	}
	if m.Manual != nil || m.Status(prog[m.ID]) != milestone.Done {
		t.Errorf("after automatic: manual %v status %s, want nil done", m.Manual, m.Status(prog[m.ID]))
	}
	_, err = e.milestones.Update(ctx, e.users.Member, m.ID, milestone.Patch{Status: kit.Opt[milestone.Status]{Set: true, Value: ptr(milestone.Status("bogus"))}})
	wantInput(t, err, "status", kit.ReasonInvalidFormat)
	hist, err := audit.New(e.pool).List(ctx, m.ID, page.Args{First: ptr(50)})
	if err != nil {
		t.Fatal(err)
	}
	var statuses []string
	for _, h := range hist.Items {
		for _, c := range h.Changes {
			if c.Field == "status" {
				statuses = append(statuses, string(c.From)+">"+string(c.To))
			}
		}
	}
	if strings.Join(statuses, " ") != `null>"automatic" "automatic">"canceled" "canceled">"automatic"` {
		t.Errorf("status audit = %v", statuses)
	}
}

func TestUpdateMilestoneFields(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	date := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	m := e.create(t, "M")
	m, err := e.milestones.Update(ctx, e.users.Member, m.ID, milestone.Patch{
		Name: ptr(" New "), Description: ptr("text"),
		TargetDate: kit.Opt[time.Time]{Set: true, Value: &date}, LeadID: kit.Opt[uuid.UUID]{Set: true, Value: &e.users.Owner.ID},
	})
	if err != nil || m.Name != "New" || m.Description != "text" || m.TargetDate == nil || m.LeadID == nil {
		t.Fatalf("update = %+v, %v", m, err)
	}
	m, err = e.milestones.Update(ctx, e.users.Member, m.ID, milestone.Patch{TargetDate: kit.Opt[time.Time]{Set: true}, LeadID: kit.Opt[uuid.UUID]{Set: true}})
	if err != nil || m.TargetDate != nil || m.LeadID != nil {
		t.Fatalf("clear = %+v, %v", m, err)
	}
	e.create(t, "Other")
	_, err = e.milestones.Update(ctx, e.users.Member, m.ID, milestone.Patch{Name: ptr("OTHER")})
	wantInput(t, err, "name", kit.ReasonTaken)
}

func TestProgressCounts(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	m := e.create(t, "M")
	empty := e.create(t, "Empty")
	e.issueIn(t, m, "Done")
	e.issueIn(t, m, "Todo")
	e.issueIn(t, m, "Canceled")
	gone := e.issueIn(t, m, "Done")
	if err := e.issues.Delete(ctx, e.users.Member, gone.ID); err != nil {
		t.Fatal(err)
	}
	prog, err := e.milestones.Progress(ctx, []uuid.UUID{m.ID, empty.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := prog[m.ID]; got != (milestone.Progress{Completed: 1, Total: 2, Active: 1}) {
		t.Errorf("progress = %+v, want {1 2 1}", got)
	}
	if got := prog[empty.ID]; got != (milestone.Progress{}) {
		t.Errorf("empty progress = %+v", got)
	}
	if s := m.Status(prog[m.ID]); s != milestone.InProgress {
		t.Errorf("status = %s, want in_progress", s)
	}
}

func TestListMilestones(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := e.create(t, "A")
	b := e.create(t, "B")
	c := e.create(t, "C")
	e.issueIn(t, b, "Done")
	if _, err := e.milestones.Update(ctx, e.users.Member, c.ID, milestone.Patch{Status: kit.Opt[milestone.Status]{Set: true, Value: ptr(milestone.Canceled)}}); err != nil {
		t.Fatal(err)
	}
	all, err := e.milestones.List(ctx, e.ylk.ID, true)
	if err != nil || len(all) != 3 || all[0].ID != a.ID || all[2].ID != c.ID {
		t.Fatalf("List(all) = %+v, %v", all, err)
	}
	open, err := e.milestones.List(ctx, e.ylk.ID, false)
	if err != nil || len(open) != 1 || open[0].ID != a.ID {
		t.Fatalf("List(open) = %+v, %v", open, err)
	}
	if err := e.milestones.Reorder(ctx, e.users.Member, e.ylk.ID, []uuid.UUID{c.ID, a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	all, _ = e.milestones.List(ctx, e.ylk.ID, true)
	if all[0].ID != c.ID || all[1].ID != a.ID || all[2].ID != b.ID || all[0].SortOrder != 1000 || all[2].SortOrder != 3000 {
		t.Errorf("after reorder = %+v", all)
	}
	err = e.milestones.Reorder(ctx, e.users.Member, e.ylk.ID, []uuid.UUID{c.ID, a.ID})
	wantInput(t, err, "milestoneIds", kit.ReasonNotAllowed)
	err = e.milestones.Reorder(ctx, e.users.Member, e.ylk.ID, []uuid.UUID{c.ID, a.ID, a.ID})
	wantInput(t, err, "milestoneIds", kit.ReasonNotAllowed)
	byIDs, err := e.milestones.ByIDs(ctx, []uuid.UUID{b.ID, uuid.New(), a.ID})
	if err != nil || len(byIDs) != 2 || byIDs[0].ID != b.ID || byIDs[1].ID != a.ID {
		t.Errorf("ByIDs = %+v, %v", byIDs, err)
	}
}

func TestDeleteMilestone(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	m := e.create(t, "M")
	i := e.issueIn(t, m, "Todo")
	other := testfx.AddUser(t, e.pool, e.users.Owner, "member")
	if err := e.milestones.Delete(ctx, other, m.ID); !errors.Is(err, kit.ErrForbidden) {
		t.Fatalf("other member delete = %v, want Forbidden", err)
	}
	if err := e.milestones.Delete(ctx, e.users.Member, m.ID); err != nil {
		t.Fatalf("creator delete: %v", err)
	}
	got, err := e.issues.Get(ctx, i.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	var milestoneID *uuid.UUID
	if err := e.pool.QueryRow(ctx, `SELECT milestone_id FROM issues WHERE id = $1`, got.ID).Scan(&milestoneID); err != nil || milestoneID != nil {
		t.Fatalf("issue milestone = %v, %v", milestoneID, err)
	}
	hist, err := audit.New(e.pool).List(ctx, i.ID, page.Args{Last: ptr(1)})
	if err != nil || len(hist.Items) != 1 {
		t.Fatal(err)
	}
	last := hist.Items[0]
	if last.Action != "update" || len(last.Changes) != 1 || last.Changes[0].Field != "milestoneId" ||
		string(last.Changes[0].From) != `"`+m.ID.String()+`"` || string(last.Changes[0].To) != "null" {
		t.Errorf("issue audit = %+v", last)
	}
	if _, err := e.milestones.Get(ctx, m.ID); !errors.Is(err, kit.ErrNotFound) {
		t.Errorf("Get after delete = %v", err)
	}
	// Owner and Admin may delete anyone's milestone.
	for _, u := range []struct{ name string }{{"owner"}, {"admin"}} {
		n := e.create(t, u.name)
		actor := e.users.Owner
		if u.name == "admin" {
			actor = e.users.Admin
		}
		if err := e.milestones.Delete(ctx, actor, n.ID); err != nil {
			t.Errorf("%s delete: %v", u.name, err)
		}
	}
}

func TestMilestoneArchived(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	m := e.create(t, "M")
	if _, err := e.projects.SetArchived(ctx, e.users.Admin, e.ylk.ID, true); err != nil {
		t.Fatal(err)
	}
	_, err := e.milestones.Create(ctx, e.users.Member, e.ylk.ID, milestone.CreateInput{Name: "N"})
	wantInput(t, err, "projectId", kit.ReasonArchived)
	_, err = e.milestones.Update(ctx, e.users.Member, m.ID, milestone.Patch{Name: ptr("X")})
	wantInput(t, err, "projectId", kit.ReasonArchived)
	err = e.milestones.Delete(ctx, e.users.Member, m.ID)
	wantInput(t, err, "projectId", kit.ReasonArchived)
	err = e.milestones.Reorder(ctx, e.users.Member, e.ylk.ID, []uuid.UUID{m.ID})
	wantInput(t, err, "projectId", kit.ReasonArchived)
}

func TestDeleteProjectRemovesMilestoneAudit(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	m := e.create(t, "M")
	if err := e.projects.Delete(ctx, e.users.Owner, e.ylk.ID, "YLK"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE entity_id = $1`, m.ID).Scan(&n); err != nil || n != 0 {
		t.Errorf("milestone audit rows after project delete = %d, %v", n, err)
	}
}
