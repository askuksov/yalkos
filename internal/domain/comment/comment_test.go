package comment_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/domain/comment"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/domain/timeline"
	"github.com/askuksov/yalkos/internal/store/q"
	"github.com/askuksov/yalkos/internal/testfx"
)

func ptr[T any](v T) *T { return &v }

type env struct {
	pool     *pgxpool.Pool
	users    testfx.Users
	projects *project.Service
	issues   *issue.Service
	comments *comment.Service
	ylk      project.Project
	issue    issue.Issue
}

func newEnv(t *testing.T) env {
	t.Helper()
	ctx := context.Background()
	pool, users, clock := testfx.New(t)
	projects := project.New(pool, clock)
	issues := issue.New(pool, projects, clock)
	e := env{pool: pool, users: users, projects: projects, issues: issues, comments: comment.New(pool, issues, nil, clock)}
	p, err := projects.Create(ctx, users.Admin, project.CreateInput{Key: "YLK", Name: "Yalkos"})
	if err != nil {
		t.Fatal(err)
	}
	e.ylk = p
	if e.issue, err = issues.Create(ctx, users.Member, issue.CreateInput{ProjectID: &p.ID, Title: "t"}); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e env) post(t *testing.T, body string) comment.Comment {
	t.Helper()
	c, err := e.comments.Create(context.Background(), e.users.Member, e.issue.ID, body, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return c
}

func wantInput(t *testing.T, err error, field string, reason kit.Reason) {
	t.Helper()
	var ie *kit.InputError
	if !errors.As(err, &ie) || ie.Field != field || ie.Reason != reason {
		t.Fatalf("err = %v, want InputError %s %s", err, field, reason)
	}
}

func TestCommentCRUD(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := e.post(t, "  first  ")
	b := e.post(t, "second")
	if a.Body != "first" || a.AuthorID != e.users.Member.ID || a.IssueID != e.issue.ID || a.EditedAt != nil {
		t.Errorf("created = %+v", a)
	}
	u, err := e.comments.Update(ctx, e.users.Member, a.ID, "first, edited")
	if err != nil || u.Body != "first, edited" || u.EditedAt == nil {
		t.Fatalf("update = %+v, %v", u, err)
	}
	list, err := e.comments.List(ctx, e.issue.ID, page.Args{First: ptr(10)})
	if err != nil || len(list.Items) != 2 || list.Items[0].ID != a.ID || list.Items[1].ID != b.ID || list.Items[0].EditedAt == nil {
		t.Fatalf("list = %+v, %v", list.Items, err)
	}
	next, err := e.comments.List(ctx, e.issue.ID, page.Args{First: ptr(1), After: &list.Cursors[0]})
	if err != nil || len(next.Items) != 1 || next.Items[0].ID != b.ID {
		t.Fatalf("page 2 = %+v, %v", next.Items, err)
	}
	counts, err := e.comments.Counts(ctx, []uuid.UUID{e.issue.ID, uuid.New()})
	if err != nil || counts[e.issue.ID] != 2 || len(counts) != 2 {
		t.Errorf("counts = %v, %v", counts, err)
	}
	byIDs, err := e.comments.ByIDs(ctx, []uuid.UUID{b.ID, a.ID})
	if err != nil || len(byIDs) != 2 || byIDs[0].ID != b.ID {
		t.Errorf("ByIDs = %+v, %v", byIDs, err)
	}
	if _, err := e.comments.Create(ctx, e.users.Member, uuid.New(), "x", nil); !errors.Is(err, kit.ErrNotFound) {
		t.Errorf("comment on missing issue = %v", err)
	}
}

func TestCommentPermissions(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.post(t, "mine")
	other := testfx.AddUser(t, e.pool, e.users.Owner, "member")
	if _, err := e.comments.Update(ctx, other, c.ID, "x"); !errors.Is(err, kit.ErrForbidden) {
		t.Errorf("other update = %v", err)
	}
	if err := e.comments.Delete(ctx, other, c.ID); !errors.Is(err, kit.ErrForbidden) {
		t.Errorf("other delete = %v", err)
	}
	if _, err := e.comments.Update(ctx, e.users.Admin, c.ID, "x"); !errors.Is(err, kit.ErrForbidden) {
		t.Errorf("admin update = %v", err)
	}
	if err := e.comments.Delete(ctx, e.users.Admin, c.ID); err != nil {
		t.Errorf("admin delete = %v", err)
	}
	if err := e.comments.Delete(ctx, e.users.Admin, c.ID); !errors.Is(err, kit.ErrNotFound) {
		t.Errorf("second delete = %v", err)
	}
}

func TestCommentValidation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, err := e.comments.Create(ctx, e.users.Member, e.issue.ID, "  \n ", nil)
	wantInput(t, err, "body", kit.ReasonRequired)
	_, err = e.comments.Create(ctx, e.users.Member, e.issue.ID, strings.Repeat("я", 100_001), nil)
	wantInput(t, err, "body", kit.ReasonTooLong)
	if _, err := e.comments.Create(ctx, e.users.Member, e.issue.ID, strings.Repeat("я", 100_000), nil); err != nil {
		t.Errorf("100 000 runes: %v", err)
	}
	c := e.post(t, "x")
	_, err = e.comments.Update(ctx, e.users.Member, c.ID, " ")
	wantInput(t, err, "body", kit.ReasonRequired)
}

func TestCommentArchived(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.post(t, "x")
	if _, err := e.projects.SetArchived(ctx, e.users.Admin, e.ylk.ID, true); err != nil {
		t.Fatal(err)
	}
	_, err := e.comments.Create(ctx, e.users.Member, e.issue.ID, "y", nil)
	wantInput(t, err, "projectId", kit.ReasonArchived)
	_, err = e.comments.Update(ctx, e.users.Member, c.ID, "y")
	wantInput(t, err, "projectId", kit.ReasonArchived)
	wantInput(t, e.comments.Delete(ctx, e.users.Member, c.ID), "projectId", kit.ReasonArchived)
}

func TestDeletedCommentLeavesNoText(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.post(t, "secret-123")
	if _, err := e.comments.Update(ctx, e.users.Member, c.ID, "secret-123 v2"); err != nil {
		t.Fatal(err)
	}
	if err := e.comments.Delete(ctx, e.users.Member, c.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := e.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM comments WHERE body LIKE '%secret-123%') +
		(SELECT count(*) FROM audit_events WHERE changes::text LIKE '%secret-123%')`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows with the text = %d, %v", n, err)
	}
	var actions []string
	rows, err := e.pool.Query(ctx, `SELECT action FROM audit_events WHERE entity_type = 'comment' AND entity_id = $1 ORDER BY created_at`, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var a string
		_ = rows.Scan(&a)
		actions = append(actions, a)
	}
	if strings.Join(actions, ",") != "create,update,delete" {
		t.Errorf("comment audit = %v", actions)
	}
	tl, err := timeline.List(ctx, q.New(e.pool), e.issue.ID, page.Args{Last: ptr(1)})
	if err != nil || len(tl.Items) != 1 || tl.Items[0].Kind != timeline.Event {
		t.Fatalf("timeline = %+v, %v", tl.Items, err)
	}
	var changes string
	if err := e.pool.QueryRow(ctx, `SELECT changes::text FROM audit_events WHERE id = $1`, tl.Items[0].ID).Scan(&changes); err != nil {
		t.Fatal(err)
	}
	if changes != `{"commentDeleted": {"to": null, "from": "`+c.ID.String()+`"}}` {
		t.Errorf("issue event = %s", changes)
	}
}

func TestDeleteProjectRemovesCommentAudit(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	kept := e.post(t, "kept")
	gone := e.post(t, "gone")
	if err := e.comments.Delete(ctx, e.users.Member, gone.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.projects.Delete(ctx, e.users.Owner, e.ylk.ID, "YLK"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE entity_id = ANY($1)`, []uuid.UUID{kept.ID, gone.ID}).Scan(&n); err != nil || n != 0 {
		t.Errorf("comment audit rows after project delete = %d, %v", n, err)
	}
}
