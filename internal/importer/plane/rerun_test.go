package plane

import (
	"bytes"
	"context"
	"errors"
	"io"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/domain/attachment"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/store/q"
)

// runAll imports through Run, as the command does; keys choose projects.
func (e *issueEnv) runAll(t *testing.T, keys []string, progress io.Writer) (*Report, error) {
	t.Helper()
	return Run(context.Background(), RunOptions{
		Client: e.client, Pool: e.pool, Store: e.store, MaxUpload: e.maxUpload, Actor: e.users.Owner,
		Keys: keys, URL: "http://plane.example.invalid", Workspace: fakeWorkspace, Progress: progress, Now: e.clock,
	})
}

func (e *issueEnv) mustRunAll(t *testing.T, keys ...string) *Report {
	t.Helper()
	r, err := e.runAll(t, keys, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return r
}

// snapshotTables are the tables an import writes entities into.
var snapshotTables = []string{
	"users", "projects", "states", "labels", "milestones", "issues", "issue_labels", "comments", "attachments", "project_members",
}

// rows returns every row a query selects as JSON text, sorted.
func (e *issueEnv) rows(t *testing.T, sql string, args ...any) []string {
	t.Helper()
	rs, err := e.pool.Query(context.Background(), sql, args...)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	out, err := pgx.CollectRows(rs, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	slices.Sort(out)
	return out
}

// snapshot holds every column of every row of the snapshot tables and of
// the extra tables.
func (e *issueEnv) snapshot(t *testing.T, extra ...string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, table := range append(slices.Clone(snapshotTables), extra...) {
		out[table] = e.rows(t, `SELECT to_jsonb(t)::text FROM `+table+` t`)
	}
	return out
}

func assertSameRows(t *testing.T, before, after map[string][]string) {
	t.Helper()
	for name, rows := range before {
		if !slices.Equal(rows, after[name]) {
			t.Errorf("%s changed:\nbefore %v\nafter  %v", name, rows, after[name])
		}
	}
}

// issueRequests counts the requests below a work item: its comments, files,
// links and relations, or the work item itself.
func (e *issueEnv) issueRequests() int {
	e.fake.mu.Lock()
	defer e.fake.mu.Unlock()
	n := 0
	for path, c := range e.fake.requests {
		if _, rest, ok := strings.Cut(path, "/work-items/"); ok && rest != "" {
			n += c
		}
	}
	return n
}

// mappedUpdated is the Plane updated_at recorded for a work item.
func (e *issueEnv) mappedUpdated(t *testing.T, planeID string) string {
	t.Helper()
	var at string
	if err := e.pool.QueryRow(context.Background(), `SELECT to_char(source_updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		FROM import_map WHERE kind = 'issue' AND plane_id = $1`, planeID).Scan(&at); err != nil {
		t.Fatalf("map row %s: %v", planeID, err)
	}
	return at
}

// estimateRe matches the time estimate of a progress line.
var estimateRe = regexp.MustCompile(`, ~[0-9]+m left$`)

// progressLines runs an import and returns its progress lines without their
// time estimates.
func (e *issueEnv) progressLines(t *testing.T) (*Report, []string) {
	t.Helper()
	var buf bytes.Buffer
	r, err := e.runAll(t, nil, &buf)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n") {
		lines = append(lines, estimateRe.ReplaceAllString(l, ""))
	}
	return r, lines
}

func TestRerunChangesNothing(t *testing.T) {
	e := newIssueEnv(t)
	_, lines := e.progressLines(t)
	if want := []string{"ALP 0/5 issues", "ALP 5/5 issues", "BET 0/3 issues", "BET 3/3 issues"}; !slices.Equal(lines, want) {
		t.Errorf("first run progress = %q, want %q", lines, want)
	}

	before := e.snapshot(t, "import_map", "blobs")
	events := e.count(t, `SELECT count(*) FROM audit_events`)
	requests := e.issueRequests()

	r, lines := e.progressLines(t)
	if want := []string{"ALP nothing new", "BET nothing new"}; !slices.Equal(lines, want) {
		t.Errorf("re-run progress = %q, want %q", lines, want)
	}
	assertSameRows(t, before, e.snapshot(t, "import_map", "blobs"))
	if n := e.count(t, `SELECT count(*) FROM audit_events`); n != events {
		t.Errorf("audit events %d -> %d", events, n)
	}
	if n := e.issueRequests() - requests; n != 0 {
		t.Errorf("per-issue requests on an unchanged re-run = %d, want 0", n)
	}
	if len(r.ChangedInPlane) != 0 {
		t.Errorf("changed in Plane = %v, want none", r.ChangedInPlane)
	}
	for _, key := range []string{"ALP", "BET"} {
		if p := projectReport(t, r, key); p.Created != (Counts{}) {
			t.Errorf("%s created = %+v, want nothing", key, p.Created)
		}
	}
}

func TestRerunAddsNewComment(t *testing.T) {
	ctx := context.Background()
	e := newIssueEnv(t)
	e.mustRunAll(t)

	alp2 := e.issue(t, "ALP-2")
	if _, err := e.pool.Exec(ctx, `UPDATE issues SET title = 'Edited in Yalkos' WHERE id = $1`, alp2.ID); err != nil {
		t.Fatal(err)
	}
	const comments = "projects/p-alp/work-items/i-alp-2/comments/"
	e.fake.setPaged(comments,
		map[string]any{
			"id": "c-alp-2-new", "comment_html": `<p>Still happens after the update.</p><image-component src="as-alp-2-pic"></image-component>`,
			"actor": "u-bob", "parent": nil, "created_at": "2025-07-01T10:00:00Z", "updated_at": "2025-07-01T10:00:00Z",
			"edited_at": nil, "deleted_at": nil,
		},
		map[string]any{
			"id": "c-alp-2-gone", "comment_html": "<p>Never mind.</p>", "actor": "u-ann", "parent": nil,
			"created_at": "2025-07-01T09:00:00Z", "updated_at": "2025-07-01T09:30:00Z", "edited_at": nil, "deleted_at": "2025-07-01T09:30:00Z",
		})
	e.fake.setFile("projects/p-alp/work-items/i-alp-2/attachments/as-alp-2-pic/",
		fakeFile{Name: "pic.gif", ContentType: "image/gif", Body: "GIF89a-new-comment-pic"})
	e.editItem(t, "p-alp", "i-alp-2", func(it map[string]any) { it["updated_at"] = "2025-07-01T10:00:00Z" })

	allComments := e.count(t, `SELECT count(*) FROM comments`)
	commentReqs := e.fake.Requests(comments)
	listReqs := e.fake.Requests("projects/p-alp/work-items/i-alp-2/attachments/") +
		e.fake.Requests("projects/p-alp/work-items/i-alp-2/links/") + e.fake.Requests("projects/p-alp/work-items/i-alp-2/relations/")

	r, lines := e.progressLines(t)
	if want := []string{"ALP nothing new", "BET nothing new", "ALP 1/1 changed issues"}; !slices.Equal(lines, want) {
		t.Errorf("progress = %q, want %q", lines, want)
	}
	if n := e.fake.Requests(comments) - commentReqs; n != 1 {
		t.Errorf("ALP-2 comments requests = %d, want 1", n)
	}
	if n := e.fake.Requests("projects/p-alp/work-items/i-alp-2/attachments/") +
		e.fake.Requests("projects/p-alp/work-items/i-alp-2/links/") +
		e.fake.Requests("projects/p-alp/work-items/i-alp-2/relations/") - listReqs; n != 0 {
		t.Errorf("ALP-2 attachment, link and relation requests = %d, want 0", n)
	}
	if n := e.count(t, `SELECT count(*) FROM comments`); n != allComments+1 {
		t.Errorf("comments %d -> %d, want one more", allComments, n)
	}
	got := e.issue(t, "ALP-2")
	if got.Title != "Edited in Yalkos" || !got.UpdatedAt.Equal(alp2.UpdatedAt) {
		t.Errorf("ALP-2 title %q updated %v, want the Yalkos edit kept (updated %v)", got.Title, got.UpdatedAt, alp2.UpdatedAt)
	}
	files := e.attachments(t, alp2.ID)
	if len(files) != 1 || files[0].Name != "pic.gif" || files[0].Uploader != e.userID(t, "bob@example.invalid") {
		t.Fatalf("ALP-2 attachments = %+v", files)
	}
	link := attachment.URL(attachment.Attachment{ID: files[0].ID, Name: files[0].Name})
	cs := e.comments(t, alp2.ID)
	if len(cs) != 1 || cs[0].AuthorID != e.userID(t, "bob@example.invalid") ||
		cs[0].Body != "Still happens after the update.\n\n![image]("+link+")" ||
		!cs[0].CreatedAt.Equal(planeTime(t, "2025-07-01T10:00:00Z")) {
		t.Fatalf("ALP-2 comments = %+v", cs)
	}
	if n := e.count(t, `SELECT count(*) FROM audit_events WHERE entity_type = 'comment' AND action = 'create'
		AND entity_id = $1 AND created_at = $2`, cs[0].ID, cs[0].CreatedAt); n != 1 {
		t.Errorf("new comment create events = %d, want 1", n)
	}
	if n := e.count(t, `SELECT count(*) FROM import_map WHERE kind = 'comment' AND plane_id = 'c-alp-2-new' AND target_id = $1`,
		cs[0].ID); n != 1 {
		t.Errorf("new comment map rows = %d, want 1", n)
	}
	if at := e.mappedUpdated(t, "i-alp-2"); at != "2025-07-01T10:00:00Z" {
		t.Errorf("recorded updated_at = %s, want the Plane one", at)
	}
	if want := []ChangedIssue{{Key: "ALP-2", CommentsAdded: 1}}; !slices.Equal(r.ChangedInPlane, want) {
		t.Errorf("changed in Plane = %v, want %v", r.ChangedInPlane, want)
	}
	if alp := projectReport(t, r, "ALP"); alp.Created.Comments != 1 || alp.Created.Attachments != 1 || alp.Created.Issues != 0 {
		t.Errorf("ALP report = %+v", alp)
	}

	requests := e.issueRequests()
	r = e.mustRunAll(t)
	if n := e.issueRequests() - requests; n != 0 {
		t.Errorf("third run per-issue requests = %d, want 0", n)
	}
	if len(r.ChangedInPlane) != 0 {
		t.Errorf("third run changed in Plane = %v, want none", r.ChangedInPlane)
	}
	if n := len(e.comments(t, alp2.ID)); n != 1 {
		t.Errorf("ALP-2 comments after the third run = %d, want 1", n)
	}
}

// projectRows holds every column of the issues numbered up to upTo in a
// project, with their comments and attachments, and the project's states
// and milestones.
func (e *issueEnv) projectRows(t *testing.T, project uuid.UUID, upTo int) map[string][]string {
	t.Helper()
	return map[string][]string{
		"issues": e.rows(t, `SELECT to_jsonb(i)::text FROM issues i WHERE project_id = $1 AND number <= $2`, project, upTo),
		"comments": e.rows(t, `SELECT to_jsonb(c)::text FROM comments c JOIN issues i ON i.id = c.issue_id
			WHERE i.project_id = $1 AND i.number <= $2`, project, upTo),
		"attachments": e.rows(t, `SELECT to_jsonb(a)::text FROM attachments a JOIN issues i ON i.id = a.issue_id
			WHERE i.project_id = $1 AND i.number <= $2`, project, upTo),
		"states":     e.rows(t, `SELECT to_jsonb(s)::text FROM states s WHERE project_id = $1`, project),
		"milestones": e.rows(t, `SELECT to_jsonb(m)::text FROM milestones m WHERE project_id = $1`, project),
	}
}

func TestRerunNewProjectAndIssue(t *testing.T) {
	ctx := context.Background()
	e := newIssueEnv(t)
	e.mustRunAll(t, "ALP")
	if _, err := q.New(e.pool).GetProjectByKey(ctx, "BET"); err == nil {
		t.Fatal("BET imported by a run for ALP only")
	}
	alp := e.projectID(t, "ALP")
	before := e.projectRows(t, alp, 5)

	e.addItem("p-alp", map[string]any{
		"id": "i-alp-6", "name": "Added in Plane later", "description_html": "", "priority": "none",
		"state": "st-alp-todo", "sequence_id": 6, "sort_order": 6000, "parent": "i-alp-5",
		"assignees": []any{}, "labels": []any{}, "start_date": nil, "target_date": nil,
		"created_at": "2025-05-01T10:00:00Z", "updated_at": "2025-05-01T10:00:00Z", "completed_at": nil, "created_by": "u-ann",
	})
	r := e.mustRunAll(t)

	assertSameRows(t, before, e.projectRows(t, alp, 5))
	alp6 := e.issue(t, "ALP-6")
	if alp6.Title != "Added in Plane later" || alp6.ParentID == nil || *alp6.ParentID != e.issue(t, "ALP-5").ID {
		t.Errorf("ALP-6 = %q parent %v", alp6.Title, alp6.ParentID)
	}
	for _, key := range []string{"BET-1", "BET-2", "BET-3"} {
		e.issue(t, key)
	}
	if bet := projectReport(t, r, "BET"); bet.Created.Issues != 3 || bet.Created.Milestones != 2 {
		t.Errorf("BET report = %+v", bet)
	}
	if a := projectReport(t, r, "ALP"); a.Created.Issues != 1 || a.Existing.Issues != 5 {
		t.Errorf("ALP report = %+v", a)
	}
	if len(r.ChangedInPlane) != 0 {
		t.Errorf("changed in Plane = %v, want none", r.ChangedInPlane)
	}
}

func TestRerunChangedReported(t *testing.T) {
	ctx := context.Background()
	e := newIssueEnv(t)
	e.mustRunAll(t)

	alp3 := e.issue(t, "ALP-3")
	e.editItem(t, "p-alp", "i-alp-3", func(it map[string]any) {
		it["name"] = "Renamed in Plane"
		it["updated_at"] = "2025-07-02T10:00:00Z"
	})
	// ALP-2 changed in Plane too, but was deleted in Yalkos meanwhile.
	e.fake.setPaged("projects/p-alp/work-items/i-alp-2/comments/", map[string]any{
		"id": "c-alp-2-late", "comment_html": "<p>Late comment.</p>", "actor": "u-ann", "parent": nil,
		"created_at": "2025-07-02T09:00:00Z", "updated_at": "2025-07-02T09:00:00Z", "edited_at": nil, "deleted_at": nil,
	})
	e.editItem(t, "p-alp", "i-alp-2", func(it map[string]any) { it["updated_at"] = "2025-07-02T09:00:00Z" })
	if _, err := e.pool.Exec(ctx, `UPDATE issues SET deleted_at = now() WHERE id = $1`, e.issue(t, "ALP-2").ID); err != nil {
		t.Fatal(err)
	}
	const comments = "projects/p-alp/work-items/i-alp-3/comments/"
	commentReqs := e.fake.Requests(comments)
	before := e.snapshot(t)

	r := e.mustRunAll(t)
	if got := e.issue(t, "ALP-3"); got.Title != "Rate limit the public endpoints" || !got.UpdatedAt.Equal(alp3.UpdatedAt) {
		t.Errorf("ALP-3 title %q updated %v, want both unchanged", got.Title, got.UpdatedAt)
	}
	assertSameRows(t, before, e.snapshot(t))
	if n := e.fake.Requests(comments) - commentReqs; n != 1 {
		t.Errorf("ALP-3 comments requests = %d, want 1", n)
	}
	if n := e.fake.Requests("projects/p-alp/work-items/i-alp-2/comments/"); n != 1 {
		t.Errorf("ALP-2 comments requests = %d, want only the first run's", n)
	}
	if !slices.Contains(r.ChangedInPlane, ChangedIssue{Key: "ALP-3", CommentsAdded: 0}) {
		t.Errorf("changed in Plane = %v, want ALP-3 with 0 comments added", r.ChangedInPlane)
	}
	if !slices.Contains(r.Dropped.DeletedInYalkos, "issue ALP-2") {
		t.Errorf("deleted in Yalkos = %v, want issue ALP-2", r.Dropped.DeletedInYalkos)
	}
	if want := []ChangedIssue{{Key: "ALP-3", CommentsAdded: 0}}; !slices.Equal(r.ChangedInPlane, want) {
		t.Errorf("changed in Plane = %v, want only %v (ALP-2 is deleted in Yalkos)", r.ChangedInPlane, want)
	}
	if at := e.mappedUpdated(t, "i-alp-2"); at != "2025-03-02T10:00:00Z" {
		t.Errorf("recorded ALP-2 updated_at = %s, want the old one kept", at)
	}
	if at := e.mappedUpdated(t, "i-alp-3"); at != "2025-07-02T10:00:00Z" {
		t.Errorf("recorded ALP-3 updated_at = %s, want the Plane one", at)
	}

	r = e.mustRunAll(t)
	if len(r.ChangedInPlane) != 0 {
		t.Errorf("third run changed in Plane = %v, want none", r.ChangedInPlane)
	}
	if !slices.Contains(r.Dropped.DeletedInYalkos, "issue ALP-2") {
		t.Errorf("third run deleted in Yalkos = %v, want issue ALP-2 again", r.Dropped.DeletedInYalkos)
	}
	if n := e.fake.Requests("projects/p-alp/work-items/i-alp-2/comments/"); n != 1 {
		t.Errorf("ALP-2 comments requests after the third run = %d, want only the first run's", n)
	}
}

func TestRunStopsBeforeWriting(t *testing.T) {
	ctx := context.Background()
	e := newIssueEnv(t)
	if _, err := e.pool.Exec(ctx, `DELETE FROM import_sources`); err != nil {
		t.Fatal(err)
	}
	before := e.snapshot(t)

	r, err := Run(ctx, RunOptions{
		Client: e.client, Pool: e.pool, Store: e.store, MaxUpload: e.maxUpload, Actor: e.users.Owner,
		URL: "http://plane.example.invalid", Workspace: fakeWorkspace, DryRun: true, Now: e.clock,
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !r.DryRun || r.Source.Workspace != fakeWorkspace || projectReport(t, r, "ALP").Created.Issues != 5 {
		t.Errorf("dry run report = %+v", r)
	}
	assertSameRows(t, before, e.snapshot(t))
	if n := e.count(t, `SELECT count(*) FROM import_sources`); n != 0 {
		t.Errorf("import sources after a dry run = %d, want 0", n)
	}
	if n := e.issueRequests(); n != 0 {
		t.Errorf("per-issue requests of a dry run = %d, want 0", n)
	}

	if _, err := project.New(e.pool, e.clock).Create(ctx, e.users.Owner, project.CreateInput{Key: "ALP", Name: "Native"}); err != nil {
		t.Fatal(err)
	}
	before = e.snapshot(t)
	for _, dry := range []bool{true, false} {
		r, err := Run(ctx, RunOptions{
			Client: e.client, Pool: e.pool, Store: e.store, MaxUpload: e.maxUpload, Actor: e.users.Owner,
			URL: "http://plane.example.invalid", Workspace: fakeWorkspace, DryRun: dry, Now: e.clock,
		})
		var ce *ConflictError
		if !errors.As(err, &ce) || !slices.Equal(ce.Keys, []string{"ALP"}) {
			t.Fatalf("dry %v: error = %v, want a conflict on ALP", dry, err)
		}
		if r == nil || len(r.Conflicts) != 1 || r.Conflicts[0].Key != "ALP" {
			t.Fatalf("dry %v: report = %+v, want the conflict listed", dry, r)
		}
		// A dry run predicts; a stopped real run wrote nothing and says so.
		if bet := projectReport(t, r, "BET"); dry != (bet.Created.Issues > 0) || (!dry && bet.Created != (Counts{})) {
			t.Errorf("dry %v: BET created = %+v", dry, bet.Created)
		}
		created := 0
		for _, u := range r.Users {
			if u.Created {
				created++
			}
		}
		if dry != (created > 0) {
			t.Errorf("dry %v: %d users flagged created", dry, created)
		}
	}
	assertSameRows(t, before, e.snapshot(t))
	if n := e.count(t, `SELECT count(*) FROM import_sources`); n != 0 {
		t.Errorf("import sources after a conflict = %d, want 0", n)
	}
}

func TestRunNothingToImport(t *testing.T) {
	e := newIssueEnv(t)
	e.fake.setPaged("projects/")
	sources := e.count(t, `SELECT count(*) FROM import_sources`)
	before := e.snapshot(t)

	r := e.mustRunAll(t)
	if len(r.Projects) != 0 || r.Source.Workspace != fakeWorkspace {
		t.Errorf("report = %+v", r)
	}
	var text bytes.Buffer
	if err := r.WriteText(&text); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if !strings.Contains(text.String(), "Nothing to import.") {
		t.Errorf("text report = %q, want it to say there is nothing to import", text.String())
	}
	if n := e.count(t, `SELECT count(*) FROM import_sources`); n != sources {
		t.Errorf("import sources %d -> %d, want no new row", sources, n)
	}
	assertSameRows(t, before, e.snapshot(t))
}

func TestRerunStopsOnCommentsError(t *testing.T) {
	e := newIssueEnv(t)
	e.mustRunAll(t)

	const comments = "projects/p-alp/work-items/i-alp-2/comments/"
	e.fake.setPaged(comments, map[string]any{
		"id": "c-alp-2-new", "comment_html": "<p>Seen again.</p>", "actor": "u-ann", "parent": nil,
		"created_at": "2025-07-03T10:00:00Z", "updated_at": "2025-07-03T10:00:00Z", "edited_at": nil, "deleted_at": nil,
	})
	e.editItem(t, "p-alp", "i-alp-2", func(it map[string]any) { it["updated_at"] = "2025-07-03T10:00:00Z" })
	e.fake.failOn(comments, 502, 99)
	before := e.snapshot(t, "import_map", "blobs")

	if _, err := e.runAll(t, nil, nil); err == nil {
		t.Fatal("Run: want an error when Plane keeps failing")
	}
	assertSameRows(t, before, e.snapshot(t, "import_map", "blobs"))
	if at := e.mappedUpdated(t, "i-alp-2"); at != "2025-03-02T10:00:00Z" {
		t.Errorf("recorded updated_at = %s, want the old one kept", at)
	}

	e.fake.failOn(comments, 502, 0)
	r := e.mustRunAll(t)
	cs := e.comments(t, e.issue(t, "ALP-2").ID)
	if len(cs) != 1 || cs[0].Body != "Seen again." {
		t.Errorf("ALP-2 comments = %+v, want the new one", cs)
	}
	if want := []ChangedIssue{{Key: "ALP-2", CommentsAdded: 1}}; !slices.Equal(r.ChangedInPlane, want) {
		t.Errorf("changed in Plane = %v, want %v", r.ChangedInPlane, want)
	}
	if at := e.mappedUpdated(t, "i-alp-2"); at != "2025-07-03T10:00:00Z" {
		t.Errorf("recorded updated_at = %s, want the Plane one", at)
	}
}

func TestProgressLines(t *testing.T) {
	c := newTestClient(t, newFakePlane(t), newFakeClock())
	var buf bytes.Buffer
	pr := newProgress(&buf, c, time.Now, "issues", 120)
	for _, call := range []struct{ done, total int }{{0, 0}, {0, 0}, {0, 120}, {50, 120}, {100, 120}, {120, 120}} {
		key := "ALP"
		if call.total > 0 {
			key = "BET"
		}
		pr.report(key, call.done, call.total)
	}
	changed := newProgress(&buf, c, time.Now, "changed issues", 3)
	changed.report("BET", 3, 3)
	want := "ALP nothing new\nBET 0/120 issues\nBET 50/120 issues\nBET 100/120 issues\nBET 120/120 issues\nBET 3/3 changed issues\n"
	if buf.String() != want {
		t.Errorf("progress =\n%s\nwant\n%s", buf.String(), want)
	}
}

func TestExclusiveGroupKeepsOneLabel(t *testing.T) {
	ctx := context.Background()
	e := newIssueEnv(t)
	e.mustRun(t)
	// The group an earlier import created is made exclusive in Yalkos.
	if _, err := e.pool.Exec(ctx, `UPDATE labels SET exclusive = true WHERE name = 'Area' AND parent_id IS NULL`); err != nil {
		t.Fatal(err)
	}
	item := planeItem("i-alp-6", "Two labels of one group", 6)
	item["labels"] = []any{"lb-alp-v2", "lb-alp-api"}
	e.addItem("p-alp", item)
	e.mustRun(t)

	names := e.rows(t, `SELECT l.name FROM issue_labels il JOIN labels l ON l.id = il.label_id
		WHERE il.issue_id = $1 AND l.parent_id IS NOT NULL AND l.parent_id <> (SELECT type_group_id FROM workspaces)`,
		e.issue(t, "ALP-6").ID)
	if !slices.Equal(names, []string{"api"}) {
		t.Errorf("ALP-6 Area labels = %q, want only api (first in Plane order)", names)
	}
}

func TestRerunNamesChangedIssueByYalkosKey(t *testing.T) {
	ctx := context.Background()
	e := newIssueEnv(t)
	e.mustRunAll(t)
	alp := e.projectID(t, "ALP")
	issues := issue.New(e.pool, project.New(e.pool, e.clock), e.clock)
	if native, err := issues.Create(ctx, e.users.Owner, issue.CreateInput{ProjectID: &alp, Title: "Written in Yalkos"}); err != nil || native.Number != 6 {
		t.Fatalf("native issue = %d, %v; want ALP-6", native.Number, err)
	}
	e.addItem("p-alp", planeItem("i-alp-6", "Added in Plane later", 6))
	if r := e.mustRunAll(t); !slices.Equal(r.Renumbered, []Renumbered{{Plane: "ALP-6", Yalkos: "ALP-7"}}) {
		t.Fatalf("renumbered = %v, want ALP-6 as ALP-7", r.Renumbered)
	}

	e.editItem(t, "p-alp", "i-alp-6", func(it map[string]any) { it["updated_at"] = "2025-07-02T10:00:00Z" })
	e.fake.setPaged("projects/p-alp/work-items/i-alp-6/comments/", map[string]any{
		"id": "c-alp-6-new", "comment_html": "<p>Seen again.</p>", "actor": "u-ann", "parent": nil,
		"created_at": "2025-07-02T10:00:00Z", "updated_at": "2025-07-02T10:00:00Z", "edited_at": nil, "deleted_at": nil,
	})
	dry, err := Run(ctx, RunOptions{
		Client: e.client, Pool: e.pool, Store: e.store, MaxUpload: e.maxUpload, Actor: e.users.Owner,
		URL: "http://plane.example.invalid", Workspace: fakeWorkspace, DryRun: true, Now: e.clock,
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if want := []ChangedIssue{{Key: "ALP-7"}}; !slices.Equal(dry.ChangedInPlane, want) {
		t.Errorf("dry run changed in Plane = %v, want %v", dry.ChangedInPlane, want)
	}
	r := e.mustRunAll(t)
	if want := []ChangedIssue{{Key: "ALP-7", CommentsAdded: 1}}; !slices.Equal(r.ChangedInPlane, want) {
		t.Errorf("changed in Plane = %v, want %v", r.ChangedInPlane, want)
	}
	if got := e.comments(t, e.issue(t, "ALP-7").ID); len(got) != 1 || got[0].Body != "Seen again." {
		t.Errorf("ALP-7 comments = %+v", got)
	}
}

func TestRerunSkipsProjectArchivedInYalkos(t *testing.T) {
	ctx := context.Background()
	e := newIssueEnv(t)
	e.mustRunAll(t)
	alp := e.projectID(t, "ALP")
	if _, err := e.pool.Exec(ctx, `UPDATE projects SET archived_at = now() WHERE id = $1`, alp); err != nil {
		t.Fatal(err)
	}
	before := e.snapshot(t)

	e.addItem("p-alp", planeItem("i-alp-6", "Added in Plane later", 6))
	e.editItem(t, "p-alp", "i-alp-2", func(it map[string]any) { it["updated_at"] = "2025-07-02T10:00:00Z" })
	const comments = "projects/p-alp/work-items/i-alp-2/comments/"
	e.fake.setPaged(comments, map[string]any{
		"id": "c-alp-2-new", "comment_html": "<p>Late.</p>", "actor": "u-ann", "parent": nil,
		"created_at": "2025-07-02T10:00:00Z", "updated_at": "2025-07-02T10:00:00Z", "edited_at": nil, "deleted_at": nil,
	})
	e.fake.setPaged("projects/p-alp/modules/", append(e.fake.paged["projects/p-alp/modules/"], map[string]any{
		"id": "md-alp-later", "name": "Later", "description_html": "", "status": "planned", "start_date": nil,
		"target_date": nil, "lead": nil, "sort_order": 9000, "created_at": "2025-07-01T09:00:00Z",
		"updated_at": "2025-07-01T09:00:00Z", "created_by": "u-ann",
	})...)
	e.fake.setPaged("projects/p-alp/modules/md-alp-later/module-issues/")
	commentReqs := e.fake.Requests(comments)

	for _, dry := range []bool{true, false} {
		r, err := Run(ctx, RunOptions{
			Client: e.client, Pool: e.pool, Store: e.store, MaxUpload: e.maxUpload, Actor: e.users.Owner,
			URL: "http://plane.example.invalid", Workspace: fakeWorkspace, DryRun: dry, Now: e.clock,
		})
		if err != nil {
			t.Fatalf("dry %v: Run: %v", dry, err)
		}
		if !slices.Equal(r.ArchivedInYalkos, []string{"ALP"}) {
			t.Errorf("dry %v: archived in Yalkos = %v, want ALP", dry, r.ArchivedInYalkos)
		}
		if a := projectReport(t, r, "ALP"); a.Created != (Counts{}) || a.Skipped.Issues != 1 || a.Existing.Issues != 5 {
			t.Errorf("dry %v: ALP report = %+v", dry, a)
		}
		if len(r.ChangedInPlane) != 0 {
			t.Errorf("dry %v: changed in Plane = %v, want none", dry, r.ChangedInPlane)
		}
	}
	assertSameRows(t, before, e.snapshot(t))
	if n := e.fake.Requests(comments) - commentReqs; n != 0 {
		t.Errorf("ALP-2 comments requests = %d, want none", n)
	}
}

func TestIssuesStopInProjectArchivedMidRun(t *testing.T) {
	ctx := context.Background()
	e := newIssueEnv(t)
	e.fake.onRequest("projects/p-alp/work-items/i-alp-3/comments/", func() {
		if _, err := e.pool.Exec(ctx, `UPDATE projects SET archived_at = now() WHERE key = 'ALP'`); err != nil {
			t.Error(err)
		}
	})
	r := e.mustRunAll(t)
	alp := e.projectID(t, "ALP")
	written := e.count(t, `SELECT count(*) FROM issues WHERE project_id = $1`, alp)
	if written == 0 || written >= 5 {
		t.Fatalf("ALP issues written = %d, want the ones before the archive only", written)
	}
	if n := e.count(t, `SELECT count(*) FROM import_map WHERE kind = 'issue' AND plane_id = 'i-alp-3'`); n != 0 {
		t.Errorf("ALP-3 written into the archived project")
	}
	a := projectReport(t, r, "ALP")
	if a.Created.Issues != written || a.Skipped.Issues != 5-written {
		t.Errorf("ALP report = %+v, want %d created and %d skipped", a, written, 5-written)
	}
	if !slices.Equal(r.ArchivedInYalkos, []string{"ALP"}) {
		t.Errorf("archived in Yalkos = %v, want ALP", r.ArchivedInYalkos)
	}
	if b := projectReport(t, r, "BET"); b.Created.Issues != 3 {
		t.Errorf("BET issues created = %d, want 3", b.Created.Issues)
	}

	if _, err := e.pool.Exec(ctx, `UPDATE projects SET archived_at = NULL WHERE id = $1`, alp); err != nil {
		t.Fatal(err)
	}
	e.mustRunAll(t)
	if n := e.count(t, `SELECT count(*) FROM issues WHERE project_id = $1`, alp); n != 5 {
		t.Errorf("ALP issues after unarchiving = %d, want 5", n)
	}
}

func TestNewCommentsInArchivedProjectOfMovedIssue(t *testing.T) {
	ctx := context.Background()
	e := newIssueEnv(t)
	e.mustRunAll(t)
	bet := e.projectID(t, "BET")
	issues := issue.New(e.pool, project.New(e.pool, e.clock), e.clock)
	moved, err := issues.Move(ctx, e.users.Owner, e.issue(t, "ALP-1").ID, bet, nil)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE projects SET archived_at = now() WHERE id = $1`, bet); err != nil {
		t.Fatal(err)
	}
	e.editItem(t, "p-alp", "i-alp-1", func(it map[string]any) { it["updated_at"] = "2025-07-02T10:00:00Z" })
	e.fake.setPaged("projects/p-alp/work-items/i-alp-1/comments/", map[string]any{
		"id": "c-alp-1-new", "comment_html": "<p>Late.</p>", "actor": "u-ann", "parent": nil,
		"created_at": "2025-07-02T10:00:00Z", "updated_at": "2025-07-02T10:00:00Z", "edited_at": nil, "deleted_at": nil,
	})
	recorded := e.mappedUpdated(t, "i-alp-1")
	had := len(e.comments(t, moved.ID))

	r := e.mustRunAll(t)
	if n := len(e.comments(t, moved.ID)); n != had {
		t.Errorf("comments of the moved issue %d -> %d, want none added in the archived project", had, n)
	}
	if at := e.mappedUpdated(t, "i-alp-1"); at != recorded {
		t.Errorf("recorded updated_at = %s, want %s kept", at, recorded)
	}
	if !slices.Contains(r.ArchivedInYalkos, "BET") || len(r.ChangedInPlane) != 0 {
		t.Errorf("archived in Yalkos = %v, changed in Plane = %v; want BET listed and no change", r.ArchivedInYalkos, r.ChangedInPlane)
	}

	if _, err := e.pool.Exec(ctx, `UPDATE projects SET archived_at = NULL WHERE id = $1`, bet); err != nil {
		t.Fatal(err)
	}
	e.mustRunAll(t)
	if cs := e.comments(t, moved.ID); len(cs) != had+1 || cs[len(cs)-1].Body != "Late." {
		t.Errorf("comments after unarchiving = %+v, want the new one added", cs)
	}
}
