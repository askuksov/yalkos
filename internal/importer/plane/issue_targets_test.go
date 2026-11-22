package plane

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/store/q"
)

func TestIssuesTargetsDeletedInYalkos(t *testing.T) {
	ctx := context.Background()
	e := newIssueEnv(t)
	e.afterBase = func() {
		for _, sql := range []string{
			`DELETE FROM states WHERE name = 'In Progress'`,
			`DELETE FROM milestones WHERE name = 'Launch'`,
			`DELETE FROM labels WHERE name = 'Frontend'`,
		} {
			if _, err := e.pool.Exec(ctx, sql); err != nil {
				t.Fatalf("%s: %v", sql, err)
			}
		}
	}
	r := e.mustRun(t)

	alp2 := e.issue(t, "ALP-2")
	var state string
	if err := e.pool.QueryRow(ctx, `SELECT name FROM states WHERE id = $1`, alp2.StateID).Scan(&state); err != nil || state != "Todo" {
		t.Errorf("ALP-2 state = %q, %v; want the default Todo", state, err)
	}
	if alp2.MilestoneID != nil || e.issue(t, "ALP-1").MilestoneID != nil {
		t.Errorf("milestones of ALP-1/ALP-2 kept after deletion")
	}
	if n := e.count(t, `SELECT count(*) FROM issue_labels WHERE issue_id = $1`, e.issue(t, "ALP-1").ID); n != 1 {
		t.Errorf("ALP-1 labels = %d, want only the Type label", n)
	}
	for _, want := range []string{"state ALP: In Progress", "milestone ALP: Launch", "label Frontend"} {
		if !slices.Contains(r.Dropped.DeletedInYalkos, want) {
			t.Errorf("deleted in Yalkos = %v, want %q", r.Dropped.DeletedInYalkos, want)
		}
	}
	if !slices.Contains(r.StateFallback, "ALP-2") {
		t.Errorf("state fallback = %v, want ALP-2", r.StateFallback)
	}
}

// planeItem is a minimal Plane work item of ALP for tests that add some.
func planeItem(id, name string, number int) map[string]any {
	return map[string]any{
		"id": id, "name": name, "description_html": "", "priority": "none",
		"state": "st-alp-todo", "sequence_id": number, "sort_order": number * 1000, "parent": nil,
		"assignees": []any{}, "labels": []any{}, "start_date": nil, "target_date": nil,
		"created_at": "2025-05-01T10:00:00Z", "updated_at": "2025-05-01T10:00:00Z", "completed_at": nil, "created_by": "u-ann",
	}
}

func TestTakenNumberDoesNotShiftLaterNumbers(t *testing.T) {
	ctx := context.Background()
	e := newIssueEnv(t)
	e.mustRun(t)
	issues := issue.New(e.pool, project.New(e.pool, e.clock), e.clock)
	alp := e.projectID(t, "ALP")
	if native, err := issues.Create(ctx, e.users.Owner, issue.CreateInput{ProjectID: &alp, Title: "Written in Yalkos"}); err != nil || native.Number != 6 {
		t.Fatalf("native issue = %d, %v; want ALP-6", native.Number, err)
	}

	e.addItem("p-alp", planeItem("i-alp-6", "Sixth in Plane", 6))
	e.addItem("p-alp", planeItem("i-alp-7", "Seventh in Plane", 7))
	e.fake.setPlain("projects/p-alp/work-items/i-alp-7/relations/", map[string]any{
		"relates_to": []any{map[string]any{"project_id": "p-alp", "issue_id": "i-alp-6"}},
	})
	r := e.mustRun(t)
	if want := []Renumbered{{Plane: "ALP-6", Yalkos: "ALP-8"}}; !slices.Equal(r.Renumbered, want) {
		t.Errorf("renumbered = %v, want %v", r.Renumbered, want)
	}
	if got := e.issue(t, "ALP-7"); got.Title != "Seventh in Plane" || got.Description != "### Relations\n\n- relates to ALP-8" {
		t.Errorf("ALP-7 = %q, %q; want the Plane item, related to its renumbered neighbour", got.Title, got.Description)
	}
	if got := e.issue(t, "ALP-8").Title; got != "Sixth in Plane" {
		t.Errorf("ALP-8 = %q", got)
	}
}

func TestNativeIssueMidRunKeepsNumbers(t *testing.T) {
	ctx := context.Background()
	e := newIssueEnv(t)
	issues := issue.New(e.pool, project.New(e.pool, e.clock), e.clock)
	var native issue.Issue
	e.fake.onRequest("projects/p-alp/work-items/i-alp-2/comments/", func() {
		alp := e.projectID(t, "ALP")
		var err error
		if native, err = issues.Create(ctx, e.users.Owner, issue.CreateInput{ProjectID: &alp, Title: "Written meanwhile"}); err != nil {
			t.Errorf("native issue: %v", err)
		}
	})
	r := e.mustRun(t)
	if native.Number != 6 {
		t.Errorf("native issue = ALP-%d, want ALP-6 (above every Plane number)", native.Number)
	}
	if len(r.Renumbered) != 0 {
		t.Errorf("renumbered = %v, want none", r.Renumbered)
	}
	if got := e.issue(t, "ALP-2").Title; got != "Remember the last opened board" {
		t.Errorf("ALP-2 = %q", got)
	}
}

func TestIssuesTargetsDeletedMidRun(t *testing.T) {
	ctx := context.Background()
	e := newIssueEnv(t)
	exec := func(sql string) func() {
		return func() {
			if _, err := e.pool.Exec(ctx, sql); err != nil {
				t.Errorf("%s: %v", sql, err)
			}
		}
	}
	// Both run after the project's states and milestones were read.
	e.fake.onRequest("projects/p-alp/work-items/i-alp-1/comments/", exec(`DELETE FROM milestones WHERE name = 'Launch'`))
	e.fake.onRequest("projects/p-alp/work-items/i-alp-2/comments/", exec(`DELETE FROM states WHERE name = 'In Progress'`))
	r := e.mustRun(t)

	if e.issue(t, "ALP-1").MilestoneID != nil {
		t.Error("ALP-1 keeps a deleted milestone")
	}
	var state string
	if err := e.pool.QueryRow(ctx, `SELECT name FROM states WHERE id = $1`, e.issue(t, "ALP-2").StateID).Scan(&state); err != nil || state != "Todo" {
		t.Errorf("ALP-2 state = %q, %v; want the default Todo", state, err)
	}
	for _, want := range []string{"state ALP: In Progress", "milestone ALP: Launch"} {
		if !slices.Contains(r.Dropped.DeletedInYalkos, want) {
			t.Errorf("deleted in Yalkos = %v, want %q", r.Dropped.DeletedInYalkos, want)
		}
	}
	if !slices.Contains(r.StateFallback, "ALP-2") {
		t.Errorf("state fallback = %v, want ALP-2", r.StateFallback)
	}
}

func TestDownloadServerErrorStopsRun(t *testing.T) {
	e := newIssueEnv(t)
	file := "projects/p-alp/work-items/i-alp-1/attachments/at-alp-1-log/"
	e.fake.failOn(file, 502, 99)
	if _, err := e.run(t); err == nil {
		t.Fatal("Issues: want an error")
	}
	if n := e.count(t, `SELECT count(*) FROM issues`); n != 0 {
		t.Errorf("issues = %d, want none: ALP-1 waits for its file", n)
	}

	e.fake.failOn(file, 502, 0)
	r := e.mustRun(t)
	if files := e.attachments(t, e.issue(t, "ALP-1").ID); len(files) != 2 {
		t.Errorf("ALP-1 attachments = %+v, want 2", files)
	}
	if len(r.Attachments.Failed) != 0 {
		t.Errorf("failed = %v", r.Attachments.Failed)
	}
}

func TestFailedTransactionLeavesNoRows(t *testing.T) {
	ctx := context.Background()
	e := newIssueEnv(t)
	if _, err := e.pool.Exec(ctx, `
		CREATE FUNCTION fail_comment() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.body = 'Start with the sign-in route.' THEN RAISE EXCEPTION 'comment refused'; END IF;
			RETURN NEW;
		END $$;
		CREATE TRIGGER fail_comment BEFORE INSERT ON comments FOR EACH ROW EXECUTE FUNCTION fail_comment();`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.run(t); err == nil {
		t.Fatal("Issues: want an error")
	}
	_, err := q.New(e.pool).GetIssueByIdentifier(ctx, q.GetIssueByIdentifierParams{Key: "ALP", Number: 3})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("ALP-3 = %v, want no row", err)
	}
	if n := e.count(t, `SELECT count(*) FROM issue_numbers WHERE number = 3`); n != 0 {
		t.Errorf("issue numbers 3 = %d, want 0", n)
	}
	if n := e.count(t, `SELECT count(*) FROM import_map WHERE plane_id IN ('i-alp-3', 'c-alp-3-first', 'at-alp-3-limits')`); n != 0 {
		t.Errorf("map rows of ALP-3 = %d, want 0", n)
	}
	if n := e.count(t, `SELECT count(*) FROM attachments WHERE name = 'limits.csv'`); n != 0 {
		t.Errorf("ALP-3 attachments = %d, want 0", n)
	}
	if _, ok := e.lastEx.Map[kindIssue]["i-alp-3"]; ok {
		t.Error("ex.Map holds ALP-3 after its transaction failed")
	}

	if _, err := e.pool.Exec(ctx, `DROP TRIGGER fail_comment ON comments`); err != nil {
		t.Fatal(err)
	}
	e.mustRun(t)
	alp3 := e.issue(t, "ALP-3")
	if cs, files := e.comments(t, alp3.ID), e.attachments(t, alp3.ID); len(cs) != 1 || len(files) != 1 {
		t.Errorf("ALP-3 comments %d, attachments %d; want 1 and 1", len(cs), len(files))
	}
}

func TestDescriptionKeepsSections(t *testing.T) {
	body := strings.Repeat("a", maxText+10)
	got, cut := describe(body, []string{"- https://example.invalid/x"}, []string{"- blocks ALP-1"})
	if !cut {
		t.Error("cut = false")
	}
	if !strings.HasSuffix(got, cutNote+"\n\n### Links\n\n- https://example.invalid/x\n\n### Relations\n\n- blocks ALP-1") {
		t.Errorf("description ends with %q", got[len(got)-120:])
	}
	if n := utf8.RuneCountInString(got); n > maxText+utf8.RuneCountInString(cutNote) {
		t.Errorf("description has %d runes", n)
	}
	if got, cut := describe("short", nil, []string{"- blocks ALP-1"}); cut || got != "short\n\n### Relations\n\n- blocks ALP-1" {
		t.Errorf("short description = %q, %v", got, cut)
	}
}
