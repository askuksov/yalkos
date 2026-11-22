package plane

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/blob"
	"github.com/askuksov/yalkos/internal/domain/attachment"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/store/q"
	"github.com/askuksov/yalkos/internal/testfx"
)

// issueEnv is a fresh Yalkos instance, the fixture workspace served by a fake
// Plane and a blob store, ready for whole import runs.
type issueEnv struct {
	baseEnv
	fake      *fakePlane
	client    *Client
	store     *blob.LocalFS
	maxUpload int64
	progress  []string
	// afterBase, when set, runs between Base and Issues.
	afterBase func()
	// lastEx is what the last run left in Existing.
	lastEx *Existing
}

func newIssueEnv(t *testing.T) *issueEnv {
	t.Helper()
	pool, users, clock := testfx.New(t)
	src, err := q.New(pool).UpsertImportSource(context.Background(), q.UpsertImportSourceParams{
		Kind: "plane", SourceID: "ws-acme", Url: "http://plane.example.invalid", Workspace: "acme", Now: clock(),
	})
	if err != nil {
		t.Fatalf("UpsertImportSource: %v", err)
	}
	f := loadFixture(t, filepath.Join("testdata", "plane"))
	store, err := blob.NewLocalFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &issueEnv{
		baseEnv: baseEnv{pool: pool, users: users, clock: clock, source: src.ID},
		fake:    f, client: newTestClient(t, f, newFakeClock()), store: store, maxUpload: 1 << 20,
	}
}

// run surveys the fake Plane and imports everything, as a run of the
// command does.
func (e *issueEnv) run(t *testing.T) (*Report, error) {
	t.Helper()
	ctx := context.Background()
	ws, err := Survey(ctx, e.client, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	ex, err := LoadExisting(ctx, q.New(e.pool), e.source)
	if err != nil {
		t.Fatalf("LoadExisting: %v", err)
	}
	p, err := MakePlan(ws, ex)
	if err != nil {
		t.Fatalf("MakePlan: %v", err)
	}
	w := NewWriter(e.pool, e.source, e.users.Owner, e.clock)
	if err := w.Base(ctx, p, &ex); err != nil {
		t.Fatalf("Base: %v", err)
	}
	if e.afterBase != nil {
		e.afterBase()
	}
	err = w.Issues(ctx, e.client, e.store, e.maxUpload, p, &ex, func(key string, done, total int) {
		e.progress = append(e.progress, key+" "+strconv.Itoa(done)+"/"+strconv.Itoa(total))
	})
	e.lastEx = &ex
	return p.Report, err
}

func (e *issueEnv) mustRun(t *testing.T) *Report {
	t.Helper()
	r, err := e.run(t)
	if err != nil {
		t.Fatalf("Issues: %v", err)
	}
	return r
}

func (e *issueEnv) issue(t *testing.T, ident string) q.Issue {
	t.Helper()
	key, num, _ := strings.Cut(ident, "-")
	n, err := strconv.Atoi(num)
	if err != nil {
		t.Fatal(err)
	}
	row, err := q.New(e.pool).GetIssueByIdentifier(context.Background(), q.GetIssueByIdentifierParams{Key: key, Number: int32(n)})
	if err != nil {
		t.Fatalf("issue %s: %v", ident, err)
	}
	return row.Issue
}

// editItem changes a work item of the fixture before the survey reads it.
func (e *issueEnv) editItem(t *testing.T, project, id string, fn func(item map[string]any)) {
	t.Helper()
	e.fake.mu.Lock()
	defer e.fake.mu.Unlock()
	for _, it := range e.fake.paged["projects/"+project+"/work-items/"] {
		if m := it.(map[string]any); m["id"] == id {
			fn(m)
			return
		}
	}
	t.Fatalf("no work item %s", id)
}

// addItem adds a work item without comments, files, links or relations.
func (e *issueEnv) addItem(project string, item map[string]any) {
	path := "projects/" + project + "/work-items/"
	e.fake.mu.Lock()
	e.fake.paged[path] = append(e.fake.paged[path], item)
	e.fake.mu.Unlock()
	id := item["id"].(string)
	for _, rest := range []string{"comments/", "attachments/", "links/"} {
		e.fake.setPaged(path + id + "/" + rest)
	}
	e.fake.setPlain(path+id+"/relations/", map[string]any{})
}

type attachmentRow struct {
	ID         uuid.UUID
	Name       string
	Uploader   uuid.UUID
	Sum        []byte
	CreatedAt  time.Time
	ContentTyp string
}

func (e *issueEnv) attachments(t *testing.T, issueID uuid.UUID) []attachmentRow {
	t.Helper()
	rows, err := e.pool.Query(context.Background(), `
		SELECT a.id, a.name, a.uploader_id, a.blob_sha256, a.created_at, b.content_type
		FROM attachments a JOIN blobs b ON b.sha256 = a.blob_sha256
		WHERE a.issue_id = $1 ORDER BY a.name`, issueID)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (attachmentRow, error) {
		var a attachmentRow
		err := r.Scan(&a.ID, &a.Name, &a.Uploader, &a.Sum, &a.CreatedAt, &a.ContentTyp)
		return a, err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (e *issueEnv) comments(t *testing.T, issueID uuid.UUID) []q.Comment {
	t.Helper()
	rows, err := e.pool.Query(context.Background(), `
		SELECT id, issue_id, author_id, body, created_at, updated_at, edited_at
		FROM comments WHERE issue_id = $1 ORDER BY created_at, id`, issueID)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[q.Comment])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func projectReport(t *testing.T, r *Report, key string) ProjectReport {
	t.Helper()
	for _, p := range r.Projects {
		if p.Key == key {
			return p
		}
	}
	t.Fatalf("no report for project %s", key)
	return ProjectReport{}
}

func TestIssuesKeepNumbersAndParents(t *testing.T) {
	ctx := context.Background()
	e := newIssueEnv(t)
	e.mustRun(t)

	titles := map[string]string{
		"ALP-1": "Sign-in button does nothing",
		"ALP-2": "Remember the last opened board",
		"ALP-3": "Rate limit the public endpoints",
		"ALP-4": "Draft the onboarding checklist",
		"ALP-5": "Onboarding for new teams",
		"BET-1": "Export boards as CSV",
		"BET-2": "Retire the old importer",
		"BET-3": "Audit log retention",
	}
	for ident, title := range titles {
		if got := e.issue(t, ident); got.Title != title {
			t.Errorf("%s title = %q, want %q", ident, got.Title, title)
		}
	}

	alp4, alp5 := e.issue(t, "ALP-4"), e.issue(t, "ALP-5")
	if alp4.ParentID == nil || *alp4.ParentID != alp5.ID {
		t.Errorf("ALP-4 parent = %v, want ALP-5 %v", alp4.ParentID, alp5.ID)
	}

	var milestone string
	if err := e.pool.QueryRow(ctx, `SELECT m.name FROM issues i JOIN milestones m ON m.id = i.milestone_id WHERE i.id = $1`,
		e.issue(t, "ALP-1").ID).Scan(&milestone); err != nil || milestone != "Launch" {
		t.Errorf("ALP-1 milestone = %q, %v; want Launch", milestone, err)
	}

	alp1 := e.issue(t, "ALP-1")
	if want := planeTime(t, "2025-03-01T10:00:00Z"); !alp1.CreatedAt.Equal(want) {
		t.Errorf("ALP-1 created_at = %v, want %v", alp1.CreatedAt, want)
	}
	if alp1.CreatorID != e.users.Owner.ID {
		t.Errorf("ALP-1 creator = %v, want the actor (seed bot)", alp1.CreatorID)
	}
	if alp1.Priority != 1 || alp1.DueDate == nil || alp1.DueDate.Format(time.DateOnly) != "2026-02-01" {
		t.Errorf("ALP-1 priority %d due %v", alp1.Priority, alp1.DueDate)
	}
	if alp1.AssigneeID == nil || *alp1.AssigneeID != e.userID(t, "ann@example.invalid") {
		t.Errorf("ALP-1 assignee = %v, want ann", alp1.AssigneeID)
	}
	if alp5.CreatorID != e.userID(t, "bob@example.invalid") {
		t.Errorf("ALP-5 creator = %v, want bob", alp5.CreatorID)
	}
	if want := planeTime(t, "2025-03-09T10:00:00Z"); alp5.CompletedAt == nil || !alp5.CompletedAt.Equal(want) || alp5.CanceledAt != nil {
		t.Errorf("ALP-5 completed %v canceled %v, want completed %v", alp5.CompletedAt, alp5.CanceledAt, want)
	}
	bet2 := e.issue(t, "BET-2")
	if want := planeTime(t, "2025-04-05T10:00:00Z"); bet2.CanceledAt == nil || !bet2.CanceledAt.Equal(want) || bet2.CompletedAt != nil {
		t.Errorf("BET-2 completed %v canceled %v, want canceled %v", bet2.CompletedAt, bet2.CanceledAt, want)
	}
	if alp1.CompletedAt != nil || alp1.CanceledAt != nil {
		t.Errorf("ALP-1 completed %v canceled %v, want none", alp1.CompletedAt, alp1.CanceledAt)
	}

	// Frontend plus the Type label Bug.
	if n := e.count(t, `SELECT count(*) FROM issue_labels il JOIN labels l ON l.id = il.label_id
		WHERE il.issue_id = $1 AND l.name IN ('Frontend', 'Bug')`, alp1.ID); n != 2 {
		t.Errorf("ALP-1 labels Frontend+Bug = %d, want 2", n)
	}

	for key, want := range map[string]int32{"ALP": 5, "BET": 3} {
		p, err := q.New(e.pool).GetProject(ctx, e.projectID(t, key))
		if err != nil {
			t.Fatal(err)
		}
		if p.SequenceCounter != want {
			t.Errorf("%s counter = %d, want %d", key, p.SequenceCounter, want)
		}
	}
	if n := e.count(t, `SELECT count(*) FROM audit_events WHERE entity_type = 'issue' AND action = 'create'`); n != 8 {
		t.Errorf("issue create events = %d, want 8", n)
	}
	if n := e.count(t, `SELECT count(*) FROM audit_events WHERE entity_type = 'issue' AND action = 'create' AND entity_id = $1
		AND created_at = '2025-03-01T10:00:00Z' AND actor_id = $2`, alp1.ID, e.users.Owner.ID); n != 1 {
		t.Errorf("ALP-1 create event with Plane time and actor = %d, want 1", n)
	}
	if !slices.Contains(e.progress, "ALP 5/5") || !slices.Contains(e.progress, "BET 3/3") {
		t.Errorf("progress = %v", e.progress)
	}
}

func TestIssuesTextAndSections(t *testing.T) {
	e := newIssueEnv(t)
	long := strings.Repeat("é", 150) + "\n" + strings.Repeat("x", 149)
	e.editItem(t, "p-alp", "i-alp-2", func(it map[string]any) { it["name"] = long })
	e.editItem(t, "p-alp", "i-alp-3", func(it map[string]any) { it["name"] = strings.Repeat("x", 254) + " yy" })
	e.editItem(t, "p-alp", "i-alp-4", func(it map[string]any) { it["name"] = "  \n  " })
	r := e.mustRun(t)

	want := "Clicking the sign-in button in Safari has no effect." +
		"\n\n### Links\n\n- [Browser notes](https://example.invalid/notes)\n- https://example.invalid/plain" +
		"\n\n### Relations\n\n- blocked by BET-2\n- relates to GAM-7\n- relates to ALP-3"
	if got := e.issue(t, "ALP-1").Description; got != want {
		t.Errorf("ALP-1 description =\n%s\nwant\n%s", got, want)
	}
	if got := e.issue(t, "BET-2").Description; got != "### Relations\n\n- blocks ALP-1" {
		t.Errorf("BET-2 description = %q", got)
	}
	if !slices.Contains(r.Dropped.UnresolvedRelations, "ALP-1") {
		t.Errorf("unresolved relations = %v, want ALP-1", r.Dropped.UnresolvedRelations)
	}

	title := e.issue(t, "ALP-2").Title
	if utf8.RuneCountInString(title) != 255 || strings.Contains(title, "\n") ||
		title != strings.Repeat("é", 150)+" "+strings.Repeat("x", 104) {
		t.Errorf("ALP-2 title = %q (%d runes)", title, utf8.RuneCountInString(title))
	}
	for _, want := range []string{"ALP-2 (title)", "ALP-3 (title)", "ALP-4 (title)"} {
		if !slices.Contains(r.Dropped.CutTexts, want) {
			t.Errorf("cut texts = %v, want %s", r.Dropped.CutTexts, want)
		}
	}
	if got := e.issue(t, "ALP-3").Title; got != strings.Repeat("x", 254) {
		t.Errorf("ALP-3 title = %q, want 254 x without the trailing space", got)
	}
	if got := e.issue(t, "ALP-4").Title; got != "ALP-4" {
		t.Errorf("ALP-4 title = %q, want its key", got)
	}

	alp5 := e.issue(t, "ALP-5")
	files := e.attachments(t, alp5.ID)
	if len(files) != 1 || files[0].Name != "flow.gif" {
		t.Fatalf("ALP-5 attachments = %+v", files)
	}
	link := attachment.URL(attachment.Attachment{ID: files[0].ID, Name: files[0].Name})
	if wantDesc := "Make the first hour pleasant.\n\n![image](" + link + ")"; alp5.Description != wantDesc {
		t.Errorf("ALP-5 description = %q, want %q", alp5.Description, wantDesc)
	}
}

func TestIssuesCommentsAndAttachments(t *testing.T) {
	ctx := context.Background()
	e := newIssueEnv(t)
	r := e.mustRun(t)
	ann, bob := e.userID(t, "ann@example.invalid"), e.userID(t, "bob@example.invalid")

	alp1 := e.issue(t, "ALP-1")
	files := e.attachments(t, alp1.ID)
	if len(files) != 2 || files[0].Name != "console log.txt" || files[1].Name != "shot.gif" {
		t.Fatalf("ALP-1 attachments = %+v", files)
	}
	log, shot := files[0], files[1]
	if log.Uploader != ann || !log.CreatedAt.Equal(planeTime(t, "2025-03-01T10:30:00Z")) || log.ContentTyp != "text/plain; charset=utf-8" {
		t.Errorf("log attachment = %+v", log)
	}
	if shot.ContentTyp != "image/gif" || shot.Uploader != ann {
		t.Errorf("image attachment = %+v", shot)
	}
	for body, a := range map[string]attachmentRow{"TypeError: x is not defined": log, "GIF89a-first-comment-shot": shot} {
		rc, err := e.store.Open(ctx, a.Sum)
		if err != nil {
			t.Fatalf("open %s: %v", a.Name, err)
		}
		got, _ := io.ReadAll(rc)
		_ = rc.Close()
		if string(got) != body {
			t.Errorf("%s content = %q, want %q", a.Name, got, body)
		}
	}

	cs := e.comments(t, alp1.ID)
	if len(cs) != 2 {
		t.Fatalf("ALP-1 comments = %d, want 2 (deleted one skipped)", len(cs))
	}
	first, reply := cs[0], cs[1]
	link := attachment.URL(attachment.Attachment{ID: shot.ID, Name: shot.Name})
	if first.AuthorID != ann || first.Body != "Here is what I see:\n\n![image]("+link+")" ||
		!first.CreatedAt.Equal(planeTime(t, "2025-03-01T12:00:00Z")) || first.EditedAt != nil {
		t.Errorf("first comment = %+v", first)
	}
	if reply.AuthorID != bob || reply.Body != "Same in Firefox." ||
		!reply.CreatedAt.Equal(planeTime(t, "2025-03-01T13:00:00Z")) ||
		reply.EditedAt == nil || !reply.EditedAt.Equal(planeTime(t, "2025-03-01T14:00:00Z")) ||
		!reply.UpdatedAt.Equal(planeTime(t, "2025-03-01T14:00:00Z")) {
		t.Errorf("reply = %+v", reply)
	}

	bot := e.comments(t, e.issue(t, "ALP-5").ID)
	if len(bot) != 1 || bot[0].AuthorID != e.users.Owner.ID || bot[0].Body != "_(empty comment)_" {
		t.Errorf("ALP-5 comments = %+v", bot)
	}

	alp := projectReport(t, r, "ALP")
	if alp.Created.Comments != 4 || alp.Skipped.Comments != 1 || alp.Created.Attachments != 4 || alp.Skipped.Attachments != 0 {
		t.Errorf("ALP report = %+v", alp)
	}
	if n := e.count(t, `SELECT count(*) FROM import_map WHERE kind = 'comment'`); n != 4 {
		t.Errorf("comment map rows = %d, want 4", n)
	}
	if n := e.count(t, `SELECT count(*) FROM import_map WHERE kind = 'attachment'`); n != 4 {
		t.Errorf("attachment map rows = %d, want 4", n)
	}
	if n := e.count(t, `SELECT count(*) FROM audit_events WHERE entity_type = 'comment' AND action = 'create'
		AND entity_id = $1 AND created_at = $2 AND actor_id = $3`, first.ID, first.CreatedAt, ann); n != 1 {
		t.Errorf("first comment create event = %d, want 1", n)
	}
	if n := e.count(t, `SELECT count(*) FROM audit_events WHERE entity_type = 'comment'`); n != 4 {
		t.Errorf("comment events = %d, want 4", n)
	}
	if len(r.Attachments.Failed) != 0 || len(r.Attachments.TooLarge) != 0 {
		t.Errorf("attachment report = %+v", r.Attachments)
	}
}

func TestIssuesDroppedFieldsReported(t *testing.T) {
	e := newIssueEnv(t)
	r := e.mustRun(t)

	bet3 := e.issue(t, "BET-3")
	if bet3.AssigneeID == nil || *bet3.AssigneeID != e.userID(t, "ann@example.invalid") {
		t.Errorf("BET-3 assignee = %v, want ann (the first)", bet3.AssigneeID)
	}
	if bet3.DueDate == nil || bet3.DueDate.Format(time.DateOnly) != "2026-03-10" {
		t.Errorf("BET-3 due = %v", bet3.DueDate)
	}
	if !slices.Contains(r.Dropped.ExtraAssignees, "BET-3") {
		t.Errorf("extra assignees = %v", r.Dropped.ExtraAssignees)
	}
	if !slices.Contains(r.Dropped.StartDates, "BET-3") {
		t.Errorf("start dates = %v", r.Dropped.StartDates)
	}
}

func TestTakenNumberGetsNextFree(t *testing.T) {
	ctx := context.Background()
	e := newIssueEnv(t)
	e.mustRun(t)

	projects := project.New(e.pool, e.clock)
	issues := issue.New(e.pool, projects, e.clock)
	alp := e.projectID(t, "ALP")
	native, err := issues.Create(ctx, e.users.Owner, issue.CreateInput{ProjectID: &alp, Title: "Written in Yalkos"})
	if err != nil || native.Number != 6 {
		t.Fatalf("native issue = %d, %v; want ALP-6", native.Number, err)
	}

	e.addItem("p-alp", map[string]any{
		"id": "i-alp-6", "name": "Added in Plane later", "description_html": "", "priority": "none",
		"state": "st-alp-todo", "sequence_id": 6, "sort_order": 6000, "parent": nil,
		"assignees": []any{}, "labels": []any{}, "start_date": nil, "target_date": nil,
		"created_at": "2025-05-01T10:00:00Z", "updated_at": "2025-05-01T10:00:00Z", "completed_at": nil, "created_by": "u-ann",
	})
	r := e.mustRun(t)
	if want := []Renumbered{{Plane: "ALP-6", Yalkos: "ALP-7"}}; !slices.Equal(r.Renumbered, want) {
		t.Errorf("renumbered = %v, want %v", r.Renumbered, want)
	}
	if got := e.issue(t, "ALP-7").Title; got != "Added in Plane later" {
		t.Errorf("ALP-7 = %q", got)
	}
	if got := e.issue(t, "ALP-6").Title; got != "Written in Yalkos" {
		t.Errorf("ALP-6 = %q", got)
	}

	next, err := issues.Create(ctx, e.users.Owner, issue.CreateInput{ProjectID: &alp, Title: "Next in Yalkos"})
	if err != nil || next.Number != 8 {
		t.Fatalf("next native issue = %d, %v; want ALP-8", next.Number, err)
	}

	before := e.count(t, `SELECT count(*) FROM issues`)
	r = e.mustRun(t)
	if after := e.count(t, `SELECT count(*) FROM issues`); after != before {
		t.Errorf("third run: issues %d -> %d", before, after)
	}
	if len(r.Renumbered) != 0 || projectReport(t, r, "ALP").Created.Issues != 0 {
		t.Errorf("third run report: renumbered %v, ALP %+v", r.Renumbered, projectReport(t, r, "ALP"))
	}
}

// importTotals counts what an import left in the database.
func importTotals(t *testing.T, e *issueEnv) map[string]int {
	t.Helper()
	out := map[string]int{}
	for name, sql := range map[string]string{
		"issues":         `SELECT count(*) FROM issues`,
		"issue numbers":  `SELECT count(*) FROM issue_numbers`,
		"issue labels":   `SELECT count(*) FROM issue_labels`,
		"comments":       `SELECT count(*) FROM comments`,
		"attachments":    `SELECT count(*) FROM attachments`,
		"blobs":          `SELECT count(*) FROM blobs`,
		"map rows":       `SELECT count(*) FROM import_map`,
		"issue events":   `SELECT count(*) FROM audit_events WHERE entity_type = 'issue'`,
		"comment events": `SELECT count(*) FROM audit_events WHERE entity_type = 'comment'`,
		"counters":       `SELECT coalesce(sum(sequence_counter), 0)::int FROM projects`,
	} {
		out[name] = e.count(t, sql)
	}
	return out
}

func TestStopMidIssueThenResume(t *testing.T) {
	ctx := context.Background()
	e := newIssueEnv(t)
	// Parent-first order: ALP-1, ALP-2, ALP-3, ALP-5, ALP-4.
	failing := "projects/p-alp/work-items/i-alp-3/comments/"
	e.fake.failOn(failing, 502, 99)
	if _, err := e.run(t); err == nil {
		t.Fatal("Issues: want an error")
	}

	e.issue(t, "ALP-1")
	e.issue(t, "ALP-2")
	_, err := q.New(e.pool).GetIssueByIdentifier(ctx, q.GetIssueByIdentifierParams{Key: "ALP", Number: 3})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("ALP-3 = %v, want no row", err)
	}
	if n := e.count(t, `SELECT count(*) FROM issues`); n != 2 {
		t.Errorf("issues = %d, want 2", n)
	}
	if n := e.count(t, `SELECT count(*) FROM comments`); n != 2 {
		t.Errorf("comments = %d, want the 2 of ALP-1", n)
	}
	if n := e.count(t, `SELECT count(*) FROM attachments`); n != 2 {
		t.Errorf("attachments = %d, want the 2 of ALP-1", n)
	}
	if n := e.count(t, `SELECT count(*) FROM import_map WHERE kind IN ('issue', 'comment', 'attachment')`); n != 6 {
		t.Errorf("issue, comment and attachment map rows = %d, want 6", n)
	}

	e.fake.failOn(failing, 502, 0)
	e.mustRun(t)

	clean := newIssueEnv(t)
	clean.mustRun(t)
	if got, want := importTotals(t, e), importTotals(t, clean); !mapsEqual(got, want) {
		t.Errorf("resumed totals = %v, clean run = %v", got, want)
	}
}

func mapsEqual(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestAttachmentTooLarge(t *testing.T) {
	e := newIssueEnv(t)
	e.maxUpload = 20
	// A download that fails is reported too, and its issue still imported.
	e.fake.failOn("projects/p-alp/work-items/i-alp-3/attachments/at-alp-3-limits/", 404, -1)
	r := e.mustRun(t)

	alp1 := e.issue(t, "ALP-1")
	if files := e.attachments(t, alp1.ID); len(files) != 0 {
		t.Errorf("ALP-1 attachments = %+v, want none", files)
	}
	if files := e.attachments(t, e.issue(t, "ALP-3").ID); len(files) != 0 {
		t.Errorf("ALP-3 attachments = %+v, want none", files)
	}
	cs := e.comments(t, alp1.ID)
	if len(cs) != 2 || cs[0].Body != "Here is what I see:\n\n[image not imported]" {
		t.Errorf("ALP-1 comments = %+v", cs)
	}
	if desc := e.issue(t, "ALP-5").Description; desc != "Make the first hour pleasant.\n\n[image not imported]" {
		t.Errorf("ALP-5 description = %q", desc)
	}
	want := []string{"ALP-1: console log.txt", "ALP-1: as-alp-1-shot", "ALP-5: as-alp-5-flow"}
	if !slices.Equal(r.Attachments.TooLarge, want) {
		t.Errorf("too large = %v, want %v", r.Attachments.TooLarge, want)
	}
	if want := []FailedAttachment{{Key: "ALP-3", Name: "limits.csv", Reason: "HTTP status 404"}}; !slices.Equal(r.Attachments.Failed, want) {
		t.Errorf("failed = %v, want %v", r.Attachments.Failed, want)
	}
	if alp := projectReport(t, r, "ALP"); alp.Created.Attachments != 0 || alp.Skipped.Attachments != 4 {
		t.Errorf("ALP report = %+v", alp)
	}
}

func TestAttachmentNotUploaded(t *testing.T) {
	e := newIssueEnv(t)
	// Plane lists a file whose upload never finished; it has nothing to copy.
	e.fake.setPaged("projects/p-alp/work-items/i-alp-2/attachments/", map[string]any{
		"id": "at-alp-2-half", "size": json.Number("12.0"), "is_uploaded": false,
		"attributes": map[string]any{"name": "half.bin", "type": "application/octet-stream", "size": 12},
		"created_at": "2025-03-02T10:30:00Z", "created_by": "u-ann",
	})
	r := e.mustRun(t)

	alp2 := e.issue(t, "ALP-2")
	if files := e.attachments(t, alp2.ID); len(files) != 0 {
		t.Errorf("ALP-2 attachments = %+v, want none", files)
	}
	if n := e.fake.Requests("projects/p-alp/work-items/i-alp-2/attachments/at-alp-2-half/"); n != 0 {
		t.Errorf("download requests = %d, want 0", n)
	}
	want := []FailedAttachment{{Key: "ALP-2", Name: "half.bin", Reason: "not uploaded"}}
	if !slices.Equal(r.Attachments.Failed, want) {
		t.Errorf("failed = %v, want %v", r.Attachments.Failed, want)
	}
	if alp := projectReport(t, r, "ALP"); alp.Created.Attachments != 4 || alp.Skipped.Attachments != 1 {
		t.Errorf("ALP report = %+v", alp)
	}
}

func TestRelationsAsBareIDs(t *testing.T) {
	e := newIssueEnv(t)
	// Some Plane versions list a relation as the related work item's id alone.
	e.fake.setPlain("projects/p-alp/work-items/i-alp-4/relations/", map[string]any{
		"blocking":   []any{map[string]any{"project_id": "p-alp", "issue_id": "i-alp-3"}},
		"relates_to": []any{"i-alp-2", "i-gone-9"},
	})
	r := e.mustRun(t)

	want := "Steps a new team goes through on day one.\n\n### Relations\n\n- blocks ALP-3\n- relates to ALP-2"
	if got := e.issue(t, "ALP-4").Description; got != want {
		t.Errorf("ALP-4 description = %q, want %q", got, want)
	}
	if !slices.Contains(r.Dropped.UnresolvedRelations, "ALP-4") {
		t.Errorf("unresolved relations = %v, want ALP-4", r.Dropped.UnresolvedRelations)
	}
}
