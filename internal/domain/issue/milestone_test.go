package issue_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/domain/project"
)

func (e env) milestone(t *testing.T, p project.Project, name string) milestone.Milestone {
	t.Helper()
	m, err := milestone.New(e.pool, e.projects, e.clock).Create(context.Background(), e.users.Member, p.ID, milestone.CreateInput{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func setMilestone(e env, id uuid.UUID, m *uuid.UUID) (issue.Issue, error) {
	return e.issues.Update(context.Background(), e.users.Member, id, issue.Patch{MilestoneID: kit.Opt[uuid.UUID]{Set: true, Value: m}})
}

func TestIssueMilestone(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	m := e.milestone(t, e.ylk, "M")
	i := e.create(t, "a")
	got, err := setMilestone(e, i.ID, &m.ID)
	if err != nil || got.MilestoneID == nil || *got.MilestoneID != m.ID {
		t.Fatalf("set = %+v, %v", got.MilestoneID, err)
	}
	read, err := e.issues.Get(ctx, i.ID.String())
	if err != nil || read.MilestoneID == nil || *read.MilestoneID != m.ID {
		t.Fatalf("Get milestone = %v, %v", read.MilestoneID, err)
	}
	h := e.history(t, i.ID)
	last := h[len(h)-1]
	if len(last.Changes) != 1 || last.Changes[0].Field != "milestoneId" || string(last.Changes[0].To) != `"`+m.ID.String()+`"` {
		t.Errorf("audit = %+v", last.Changes)
	}
	got, err = setMilestone(e, i.ID, nil)
	if err != nil || got.MilestoneID != nil {
		t.Fatalf("clear = %+v, %v", got.MilestoneID, err)
	}
	web := e.project(t, "WEB")
	other := e.milestone(t, web, "W")
	_, err = setMilestone(e, i.ID, &other.ID)
	wantInput(t, err, "milestoneId", kit.ReasonWrongProject)
	missing := uuid.New()
	_, err = setMilestone(e, i.ID, &missing)
	wantInput(t, err, "milestoneId", kit.ReasonNotFound)
	_, err = e.issues.Create(ctx, e.users.Member, issue.CreateInput{ProjectID: &e.ylk.ID, Title: "x", MilestoneID: &other.ID})
	wantInput(t, err, "milestoneId", kit.ReasonWrongProject)
	created, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ProjectID: &e.ylk.ID, Title: "y", MilestoneID: &m.ID})
	if err != nil || created.MilestoneID == nil || *created.MilestoneID != m.ID {
		t.Fatalf("create with milestone = %+v, %v", created.MilestoneID, err)
	}
}

func TestSubIssueInheritsMilestone(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	m := e.milestone(t, e.ylk, "M")
	n := e.milestone(t, e.ylk, "N")
	parent := e.create(t, "p")
	if _, err := setMilestone(e, parent.ID, &m.ID); err != nil {
		t.Fatal(err)
	}
	child := e.child(t, parent.ID, "c")
	if child.MilestoneID == nil || *child.MilestoneID != m.ID {
		t.Errorf("child milestone = %v, want %s", child.MilestoneID, m.ID)
	}
	explicit, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ParentID: &parent.ID, Title: "e", MilestoneID: &n.ID})
	if err != nil || explicit.MilestoneID == nil || *explicit.MilestoneID != n.ID {
		t.Errorf("explicit child milestone = %v, %v", explicit.MilestoneID, err)
	}
}

func TestMoveClearsMilestone(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	web := e.project(t, "WEB")
	m := e.milestone(t, e.ylk, "M")
	root := e.create(t, "root")
	if _, err := setMilestone(e, root.ID, &m.ID); err != nil {
		t.Fatal(err)
	}
	child := e.child(t, root.ID, "child")
	moved, err := e.issues.Move(ctx, e.users.Member, root.ID, web.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if moved.MilestoneID != nil {
		t.Errorf("moved milestone = %v", moved.MilestoneID)
	}
	c, err := e.issues.Get(ctx, child.ID.String())
	if err != nil || c.MilestoneID != nil {
		t.Errorf("child after move = %v, %v", c.MilestoneID, err)
	}
	for _, id := range []uuid.UUID{root.ID, child.ID} {
		h := e.history(t, id)
		found := false
		for _, ch := range h[len(h)-1].Changes {
			if ch.Field == "milestoneId" && string(ch.To) == "null" {
				found = true
			}
		}
		if !found {
			t.Errorf("issue %s: move audit lacks milestoneId: %+v", id, h[len(h)-1].Changes)
		}
	}
}

func TestFilterMilestone(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	m := e.milestone(t, e.ylk, "M")
	a := e.create(t, "a")
	e.create(t, "b")
	if _, err := setMilestone(e, a.ID, &m.ID); err != nil {
		t.Fatal(err)
	}
	r, err := e.issues.List(ctx, issue.Filter{Milestone: &issue.IDCond{Eq: &m.ID}}, issue.Order{}, page.Args{First: ptr(50)})
	if err != nil || len(r.Items) != 1 || r.Items[0].ID != a.ID || r.Items[0].MilestoneID == nil {
		t.Fatalf("eq = %+v, %v", r.Items, err)
	}
	r, err = e.issues.List(ctx, issue.Filter{Milestone: &issue.IDCond{Null: ptr(true)}}, issue.Order{}, page.Args{First: ptr(50)})
	if err != nil || len(r.Items) != 1 || r.Items[0].Title != "b" {
		t.Fatalf("null = %+v, %v", r.Items, err)
	}
	n, err := e.issues.Count(ctx, issue.Filter{Milestone: &issue.IDCond{In: []uuid.UUID{m.ID}}})
	if err != nil || n != 1 {
		t.Fatalf("count in = %d, %v", n, err)
	}
}
