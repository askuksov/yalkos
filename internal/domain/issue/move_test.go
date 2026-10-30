package issue_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/project"
)

func changeFields(e audit.Entry) string {
	var f []string
	for _, c := range e.Changes {
		f = append(f, c.Field)
	}
	return strings.Join(f, ",")
}

func (e env) createIn(t *testing.T, projectID uuid.UUID, title string) issue.Issue {
	t.Helper()
	i, err := e.issues.Create(context.Background(), e.users.Member, issue.CreateInput{ProjectID: &projectID, Title: title})
	if err != nil {
		t.Fatalf("Create %q: %v", title, err)
	}
	return i
}

func TestMoveIssue(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	web := e.project(t, "WEB")
	e.createIn(t, web.ID, "web 1")
	i := e.create(t, "x")
	moved, err := e.issues.Move(ctx, e.users.Member, i.ID, web.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	webTodo := e.state(t, web.ID, "Todo")
	if moved.Identifier != "WEB-2" || moved.ProjectID != web.ID || moved.StateID != webTodo.ID || moved.ID != i.ID {
		t.Fatalf("moved = %+v", moved)
	}
	h := e.history(t, i.ID)
	if len(h) != 2 || h[1].Action != "update" || changeFields(h[1]) != "number,projectId,stateId" {
		t.Fatalf("history = %+v", h)
	}
}

func TestOldIdentifierResolvesAfterMove(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	web := e.project(t, "WEB")
	i := e.create(t, "x")
	if _, err := e.issues.Move(ctx, e.users.Member, i.ID, web.ID, nil); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"YLK-1", "web-1"} {
		got, err := e.issues.Get(ctx, ref)
		if err != nil || got.ID != i.ID || got.Identifier != "WEB-1" {
			t.Fatalf("Get(%q) = %+v, %v", ref, got, err)
		}
	}
	if n := e.create(t, "fresh").Number; n != 2 {
		t.Fatalf("new YLK number = %d, want 2", n)
	}
	// A key change keeps the pre-move identifier working too.
	if _, err := e.projects.Update(ctx, e.users.Admin, e.ylk.ID, project.UpdateInput{Key: ptr("YK")}); err != nil {
		t.Fatal(err)
	}
	if got, err := e.issues.Get(ctx, "YLK-1"); err != nil || got.ID != i.ID {
		t.Fatalf("after key change: %+v, %v", got, err)
	}
}

func TestMoveWithSubtree(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	web := e.project(t, "WEB")
	root := e.create(t, "root")
	c := e.child(t, root.ID, "child")
	g := e.child(t, c.ID, "grandchild")
	if _, err := e.issues.Move(ctx, e.users.Member, root.ID, web.ID, nil); err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, x := range []issue.Issue{root, c, g} {
		got, err := e.issues.Get(ctx, x.ID.String())
		if err != nil || got.ProjectID != web.ID || seen[got.Number] {
			t.Fatalf("%s after move = %+v, %v", x.Title, got, err)
		}
		seen[got.Number] = true
	}
	gotC, _ := e.issues.Get(ctx, c.ID.String())
	gotG, _ := e.issues.Get(ctx, g.ID.String())
	if *gotC.ParentID != root.ID || *gotG.ParentID != c.ID {
		t.Fatalf("parent links: child %v grandchild %v", gotC.ParentID, gotG.ParentID)
	}
}

func TestMoveChildAlone(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	web := e.project(t, "WEB")
	parent := e.create(t, "parent")
	c := e.child(t, parent.ID, "child")
	moved, err := e.issues.Move(ctx, e.users.Member, c.ID, web.ID, nil)
	if err != nil || moved.ParentID != nil {
		t.Fatalf("moved = %+v, %v", moved, err)
	}
	var n int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM issues WHERE parent_id = $1`, parent.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("old parent children = %d, %v", n, err)
	}
	h := e.history(t, c.ID)
	if changeFields(h[len(h)-1]) != "number,parentId,projectId,stateId" {
		t.Fatalf("history = %+v", h[len(h)-1])
	}
}

func TestMoveStateMapping(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	web := e.project(t, "WEB")
	review, err := e.projects.CreateState(ctx, e.users.Admin, e.ylk.ID, project.StateInput{Name: "Review", Type: project.Started, Color: "#ff0000"})
	if err != nil {
		t.Fatal(err)
	}
	i, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ProjectID: &e.ylk.ID, Title: "x", StateID: &review.ID})
	if err != nil {
		t.Fatal(err)
	}
	moved, err := e.issues.Move(ctx, e.users.Member, i.ID, web.ID, nil)
	if err != nil || moved.StateID != e.state(t, web.ID, "In Progress").ID {
		t.Fatalf("mapped state = %+v, %v", moved, err)
	}

	j := e.create(t, "y")
	webDone := e.state(t, web.ID, "Done")
	moved, err = e.issues.Move(ctx, e.users.Member, j.ID, web.ID, &webDone.ID)
	if err != nil || moved.StateID != webDone.ID || moved.CompletedAt == nil {
		t.Fatalf("explicit state = %+v, %v", moved, err)
	}

	k := e.create(t, "z")
	ylkDone := e.state(t, e.ylk.ID, "Done")
	_, err = e.issues.Move(ctx, e.users.Member, k.ID, web.ID, &ylkDone.ID)
	wantInput(t, err, "stateId", kit.ReasonWrongProject)
}

func TestMoveIntoArchivedRejected(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	web := e.project(t, "WEB")
	i := e.create(t, "x")
	_, err := e.issues.Move(ctx, e.users.Member, i.ID, e.ylk.ID, nil)
	wantInput(t, err, "projectId", kit.ReasonNotAllowed)
	_, err = e.issues.Move(ctx, e.users.Member, i.ID, uuid.New(), nil)
	wantInput(t, err, "projectId", kit.ReasonNotFound)
	if _, err := e.projects.SetArchived(ctx, e.users.Admin, web.ID, true); err != nil {
		t.Fatal(err)
	}
	_, err = e.issues.Move(ctx, e.users.Member, i.ID, web.ID, nil)
	wantInput(t, err, "projectId", kit.ReasonArchived)
	// Moving out of an archived project is refused too.
	if _, err := e.projects.SetArchived(ctx, e.users.Admin, web.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.projects.SetArchived(ctx, e.users.Admin, e.ylk.ID, true); err != nil {
		t.Fatal(err)
	}
	_, err = e.issues.Move(ctx, e.users.Member, i.ID, web.ID, nil)
	wantInput(t, err, "projectId", kit.ReasonArchived)
}

func TestMoveConcurrentOpposite(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	web := e.project(t, "WEB")
	var ylkIssues, webIssues []issue.Issue
	for range 5 {
		ylkIssues = append(ylkIssues, e.create(t, "y"))
		webIssues = append(webIssues, e.createIn(t, web.ID, "w"))
	}
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for n := range 5 {
		wg.Go(func() {
			_, err := e.issues.Move(ctx, e.users.Member, ylkIssues[n].ID, web.ID, nil)
			errs <- err
		})
		wg.Go(func() {
			_, err := e.issues.Move(ctx, e.users.Member, webIssues[n].ID, e.ylk.ID, nil)
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var dup int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM (SELECT project_id, number FROM issues GROUP BY 1, 2 HAVING count(*) > 1) d`).Scan(&dup); err != nil || dup != 0 {
		t.Fatalf("duplicate numbers: %d, %v", dup, err)
	}
}
