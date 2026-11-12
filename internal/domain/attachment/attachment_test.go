package attachment_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/blob"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/attachment"
	"github.com/askuksov/yalkos/internal/domain/comment"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/testfx"
)

func ptr[T any](v T) *T { return &v }

var errDeleteFailed = errors.New("test: delete failed")

// flakyStore is a LocalFS whose Delete fails while fail says so.
type flakyStore struct {
	*blob.LocalFS
	mu   sync.Mutex
	fail func(sum []byte) bool
}

func (s *flakyStore) failDeletes(f func(sum []byte) bool) {
	s.mu.Lock()
	s.fail = f
	s.mu.Unlock()
}

func (s *flakyStore) Delete(ctx context.Context, sum []byte) error {
	s.mu.Lock()
	fail := s.fail != nil && s.fail(sum)
	s.mu.Unlock()
	if fail {
		return errDeleteFailed
	}
	return s.LocalFS.Delete(ctx, sum)
}

// clock is a settable time source.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(time.Millisecond)
	return c.t
}
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type env struct {
	pool        *pgxpool.Pool
	users       testfx.Users
	clock       *clock
	dir         string
	store       *flakyStore
	projects    *project.Service
	issues      *issue.Service
	comments    *comment.Service
	attachments *attachment.Service
	ylk         project.Project
}

const png = "\x89PNG\r\n\x1a\n0000IHDR"

func newEnv(t *testing.T) env {
	t.Helper()
	ctx := context.Background()
	pool, users, _ := testfx.New(t)
	c := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	dir := t.TempDir()
	local, err := blob.NewLocalFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	store := &flakyStore{LocalFS: local}
	projects := project.New(pool, c.now)
	issues := issue.New(pool, projects, c.now)
	atts := attachment.New(pool, issues, store, &blob.Locks{}, 1<<20, c.now)
	issues.SetLinker(atts)
	e := env{pool: pool, users: users, clock: c, dir: dir, store: store, projects: projects, issues: issues,
		comments: comment.New(pool, issues, atts, c.now), attachments: atts}
	// The workspace row gets the real time, the test clock starts earlier:
	// date the workspace before any file time a test sets, so cleanup sweeps.
	e.setWorkspaceCreated(t, c.t.AddDate(-1, 0, 0))
	if e.ylk, err = projects.Create(ctx, users.Admin, project.CreateInput{Key: "YLK", Name: "Yalkos"}); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e env) upload(t *testing.T, actor account.User, name, data string, issueID *uuid.UUID) attachment.Attachment {
	t.Helper()
	a, err := e.attachments.Upload(context.Background(), actor, name, strings.NewReader(data), issueID)
	if err != nil {
		t.Fatalf("Upload %s: %v", name, err)
	}
	return a
}

func (e env) issue(t *testing.T, in issue.CreateInput) issue.Issue {
	t.Helper()
	in.ProjectID = &e.ylk.ID
	if in.Title == "" {
		in.Title = "t"
	}
	i, err := e.issues.Create(context.Background(), e.users.Member, in)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func (e env) issueOf(t *testing.T, id uuid.UUID) *uuid.UUID {
	t.Helper()
	var issueID *uuid.UUID
	if err := e.pool.QueryRow(context.Background(), `SELECT issue_id FROM attachments WHERE id = $1`, id).Scan(&issueID); err != nil {
		t.Fatal(err)
	}
	return issueID
}

func (e env) blobFiles(t *testing.T) int {
	t.Helper()
	n := 0
	_ = filepath.WalkDir(filepath.Join(e.dir, "blobs"), func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func (e env) issueChanges(t *testing.T, issueID uuid.UUID, field string) []string {
	t.Helper()
	rows, err := e.pool.Query(context.Background(), `SELECT changes->$2 #>> '{}' FROM audit_events
		WHERE entity_type = 'issue' AND entity_id = $1 AND changes ? $2 ORDER BY created_at, id`, issueID, field)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func TestUploadToIssue(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	i := e.issue(t, issue.CreateInput{})
	a := e.upload(t, e.users.Member, "shot.png", png, &i.ID)
	if a.ContentType != "image/png" || a.Size != int64(len(png)) || a.IssueID == nil || *a.IssueID != i.ID || a.Name != "shot.png" {
		t.Fatalf("attachment = %+v", a)
	}
	if attachment.URL(a) != "/api/attachments/"+a.ID.String()+"/shot.png" || !attachment.Inline(a.ContentType) {
		t.Errorf("url %q inline %v", attachment.URL(a), attachment.Inline(a.ContentType))
	}
	got, r, err := e.attachments.Open(ctx, e.users.Member, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	_ = r.Close()
	if string(b) != png || got.ID != a.ID {
		t.Errorf("open = %+v %q", got, b)
	}
	list, err := e.attachments.List(ctx, i.ID)
	if err != nil || len(list) != 1 || list[0].ID != a.ID {
		t.Errorf("list = %+v, %v", list, err)
	}
	if ch := e.issueChanges(t, i.ID, "attachmentAdded"); len(ch) != 1 || !strings.Contains(ch[0], `"name": "shot.png"`) {
		t.Errorf("issue audit = %v", ch)
	}
	if _, _, err := e.attachments.Open(ctx, e.users.Member, uuid.New()); !errors.Is(err, kit.ErrNotFound) {
		t.Errorf("open missing = %v", err)
	}
	_, err = e.attachments.Upload(ctx, e.users.Member, "x", strings.NewReader("x"), ptr(uuid.New()))
	var ie *kit.InputError
	if !errors.As(err, &ie) || ie.Field != "issueId" || ie.Reason != kit.ReasonNotFound {
		t.Errorf("upload to missing issue = %v", err)
	}
}

func TestUploadTooLargeDomain(t *testing.T) {
	e := newEnv(t)
	_, err := e.attachments.Upload(context.Background(), e.users.Member, "big.bin", strings.NewReader(strings.Repeat("x", 1<<20+1)), nil)
	var ie *kit.InputError
	if !errors.As(err, &ie) || ie.Reason != kit.ReasonTooLarge || ie.Params["maxMb"] != 1 {
		t.Fatalf("err = %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(e.dir, "tmp")); len(entries) != 0 {
		t.Errorf("tmp has %d files", len(entries))
	}
}

func TestUploadNameSanitizedDomain(t *testing.T) {
	cases := map[string]string{
		"../../a\x00b.png":                      "ab.png",
		`C:\Users\me\shot.png`:                  "shot.png",
		"  ":                                    "file",
		"..":                                    "file",
		strings.Repeat("я", 300) + ".tar.gz":    strings.Repeat("я", 252) + ".gz",
		"report\u202e.pdf":                      "report.pdf",
		strings.Repeat("x", 254) + ".verylong1": strings.Repeat("x", 245) + ".verylong1",
		strings.Repeat("x", 250) + "." + strings.Repeat("e", 40): strings.Repeat("x", 250) + "." + strings.Repeat("e", 4),
	}
	for in, want := range cases {
		if got := attachment.CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLinkPendingOnlyOwnUploads(t *testing.T) {
	e := newEnv(t)
	other := testfx.AddUser(t, e.pool, e.users.Owner, "member")
	a := e.upload(t, other, "a.png", png, nil)
	i := e.issue(t, issue.CreateInput{AttachmentIDs: []uuid.UUID{a.ID}, Description: "![a](" + attachment.URL(a) + ")"})
	if got := e.issueOf(t, a.ID); got != nil {
		t.Fatalf("someone else's pending upload linked to %v", got)
	}
	if ch := e.issueChanges(t, i.ID, "attachmentAdded"); len(ch) != 0 {
		t.Errorf("audit = %v", ch)
	}
}

func TestLinkFromMarkdown(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	inMD := e.upload(t, e.users.Member, "a.png", png, nil)
	explicit := e.upload(t, e.users.Member, "b.txt", "hello", nil)
	i := e.issue(t, issue.CreateInput{AttachmentIDs: []uuid.UUID{explicit.ID}, Description: "see ![a](" + attachment.URL(inMD) + ")"})
	for _, a := range []attachment.Attachment{inMD, explicit} {
		if got := e.issueOf(t, a.ID); got == nil || *got != i.ID {
			t.Errorf("%s linked to %v, want %s", a.Name, got, i.ID)
		}
	}
	if ch := e.issueChanges(t, i.ID, "attachmentAdded"); len(ch) != 2 {
		t.Errorf("attachmentAdded audit = %v", ch)
	}
	// A description update links uploads pasted later.
	later := e.upload(t, e.users.Member, "c.png", png, nil)
	if _, err := e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{Description: ptr("now " + attachment.URL(later))}); err != nil {
		t.Fatal(err)
	}
	if got := e.issueOf(t, later.ID); got == nil || *got != i.ID {
		t.Errorf("later linked to %v", got)
	}
	// A new comment links its uploads to the comment's issue.
	forComment := e.upload(t, e.users.Member, "d.png", png, nil)
	if _, err := e.comments.Create(ctx, e.users.Member, i.ID, "look", []uuid.UUID{forComment.ID}); err != nil {
		t.Fatal(err)
	}
	if got := e.issueOf(t, forComment.ID); got == nil || *got != i.ID {
		t.Errorf("comment upload linked to %v", got)
	}
	// An attachment of another issue stays where it is.
	j := e.issue(t, issue.CreateInput{Description: attachment.URL(inMD)})
	if got := e.issueOf(t, inMD.ID); got == nil || *got != i.ID {
		t.Errorf("linked attachment moved to %v (issue %s)", got, j.ID)
	}
	if ids := attachment.IDsInMarkdown("x /api/attachments/" + inMD.ID.String() + "/a.png and /api/attachments/nope/x"); !slices.Equal(ids, []uuid.UUID{inMD.ID}) {
		t.Errorf("IDsInMarkdown = %v", ids)
	}
}

func TestCleanup(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	old := e.upload(t, e.users.Member, "old.bin", "old", nil)
	e.clock.add(2 * time.Hour)
	i := e.issue(t, issue.CreateInput{})
	shared := e.upload(t, e.users.Member, "shared.bin", "shared", &i.ID)
	e.clock.add(23 * time.Hour)
	recent := e.upload(t, e.users.Member, "recent.bin", "recent", nil)
	e.clock.add(time.Hour) // old is 26 h old, recent 1 h
	if n := e.blobFiles(t); n != 3 {
		t.Fatalf("blob files = %d", n)
	}
	r, err := e.attachments.Cleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r.Pending != 1 || r.Blobs != 1 {
		t.Errorf("result = %+v, want 1 pending upload and its blob", r)
	}
	if _, _, err := e.attachments.Open(ctx, e.users.Member, old.ID); !errors.Is(err, kit.ErrNotFound) {
		t.Errorf("old pending = %v", err)
	}
	for _, a := range []attachment.Attachment{shared, recent} {
		if _, r, err := e.attachments.Open(ctx, e.users.Member, a.ID); err != nil {
			t.Errorf("%s: %v", a.Name, err)
		} else {
			_ = r.Close()
		}
	}
	if n := e.blobFiles(t); n != 2 {
		t.Errorf("blob files after cleanup = %d, want 2", n)
	}
	var blobs int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM blobs`).Scan(&blobs); err != nil || blobs != 2 {
		t.Errorf("blob rows = %d, %v", blobs, err)
	}
}

func TestDeleteAttachmentPermissions(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	i := e.issue(t, issue.CreateInput{})
	a := e.upload(t, e.users.Member, "a.png", png, &i.ID)
	dup := e.upload(t, e.users.Member, "same.png", png, &i.ID)
	other := testfx.AddUser(t, e.pool, e.users.Owner, "member")
	if err := e.attachments.Delete(ctx, other, a.ID); !errors.Is(err, kit.ErrForbidden) {
		t.Fatalf("other member delete = %v", err)
	}
	if err := e.attachments.Delete(ctx, e.users.Member, a.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.blobFiles(t); n != 1 {
		t.Errorf("shared blob removed while referenced: %d files", n)
	}
	if err := e.attachments.Delete(ctx, e.users.Admin, dup.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.blobFiles(t); n != 0 {
		t.Errorf("unreferenced blob kept: %d files", n)
	}
	if ch := e.issueChanges(t, i.ID, "attachmentDeleted"); len(ch) != 2 {
		t.Errorf("attachmentDeleted audit = %v", ch)
	}
	if err := e.attachments.Delete(ctx, e.users.Admin, dup.ID); !errors.Is(err, kit.ErrNotFound) {
		t.Errorf("second delete = %v", err)
	}
	if _, err := e.projects.SetArchived(ctx, e.users.Admin, e.ylk.ID, true); err != nil {
		t.Fatal(err)
	}
	_, err := e.attachments.Upload(ctx, e.users.Member, "x", strings.NewReader("x"), &i.ID)
	var ie *kit.InputError
	if !errors.As(err, &ie) || ie.Reason != kit.ReasonArchived {
		t.Errorf("upload to archived = %v", err)
	}
}

// A file not linked yet is the uploader's alone: anyone else, Owner and Admin
// included, gets not found, as for a missing id.
func TestDeletePendingUploadOnlyByUploader(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	pending := e.upload(t, e.users.Member, "p.bin", "pending", nil)
	other := testfx.AddUser(t, e.pool, e.users.Owner, "member")
	for name, actor := range map[string]account.User{"member": other, "admin": e.users.Admin, "owner": e.users.Owner} {
		if err := e.attachments.Delete(ctx, actor, pending.ID); !errors.Is(err, kit.ErrNotFound) {
			t.Errorf("%s deletes another's pending upload = %v, want not found", name, err)
		}
	}
	var n int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM attachments WHERE id = $1`, pending.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("pending upload rows = %d, %v; want it kept", n, err)
	}
	if err := e.attachments.Delete(ctx, e.users.Member, pending.ID); err != nil {
		t.Errorf("uploader deletes own pending upload = %v", err)
	}
	i := e.issue(t, issue.CreateInput{})
	linked := e.upload(t, e.users.Member, "l.bin", "linked", &i.ID)
	if err := e.attachments.Delete(ctx, e.users.Admin, linked.ID); err != nil {
		t.Errorf("admin deletes another's linked attachment = %v", err)
	}
}

func TestDeleteProjectRemovesAttachmentAudit(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	i := e.issue(t, issue.CreateInput{})
	a := e.upload(t, e.users.Member, "a.png", png, &i.ID)
	b := e.upload(t, e.users.Member, "b.png", "bbb", &i.ID)
	if err := e.attachments.Delete(ctx, e.users.Member, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.projects.Delete(ctx, e.users.Owner, e.ylk.ID, "YLK"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE entity_id = ANY($1)`, []uuid.UUID{a.ID, b.ID}).Scan(&n); err != nil || n != 0 {
		t.Errorf("attachment audit rows after project delete = %d, %v", n, err)
	}
}

// auditRows returns "action:project" for each audit event of entity id, oldest first.
func (e env) auditRows(t *testing.T, id uuid.UUID) []string {
	t.Helper()
	rows, err := e.pool.Query(context.Background(), `SELECT action || ':' || coalesce(project_id::text, 'null')
		FROM audit_events WHERE entity_id = $1 ORDER BY created_at, id`, id)
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

func TestPendingUploadAuditedWhenLinked(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := e.upload(t, e.users.Member, "a.png", png, nil)
	if got := e.auditRows(t, a.ID); len(got) != 0 {
		t.Fatalf("pending upload audit = %v, want none", got)
	}
	e.issue(t, issue.CreateInput{AttachmentIDs: []uuid.UUID{a.ID}})
	if got := e.auditRows(t, a.ID); !slices.Equal(got, []string{"create:" + e.ylk.ID.String()}) {
		t.Fatalf("audit after link = %v", got)
	}
	var name string
	var size int64
	var actor uuid.UUID
	if err := e.pool.QueryRow(ctx, `SELECT changes->'name'->>'to', (changes->'size'->>'to')::bigint, actor_id
		FROM audit_events WHERE entity_type = 'attachment' AND entity_id = $1`, a.ID).Scan(&name, &size, &actor); err != nil {
		t.Fatal(err)
	}
	if name != "a.png" || size != int64(len(png)) || actor != e.users.Member.ID {
		t.Errorf("create event name %q size %d actor %s", name, size, actor)
	}
}

func TestDeleteProjectRemovesAuditOfLinkedUpload(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := e.upload(t, e.users.Member, "a.png", png, nil)
	e.issue(t, issue.CreateInput{AttachmentIDs: []uuid.UUID{a.ID}})
	if err := e.attachments.Delete(ctx, e.users.Member, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.projects.Delete(ctx, e.users.Owner, e.ylk.ID, "YLK"); err != nil {
		t.Fatal(err)
	}
	if got := e.auditRows(t, a.ID); len(got) != 0 {
		t.Errorf("audit after project delete = %v", got)
	}
}

func TestPendingUploadLeavesNoAudit(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	expired := e.upload(t, e.users.Member, "old.png", png, nil)
	e.clock.add(25 * time.Hour)
	if _, err := e.attachments.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.auditRows(t, expired.ID); len(got) != 0 {
		t.Errorf("expired upload audit = %v", got)
	}
	deleted := e.upload(t, e.users.Member, "gone.png", png, nil)
	if err := e.attachments.Delete(ctx, e.users.Member, deleted.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.auditRows(t, deleted.ID); len(got) != 0 {
		t.Errorf("deleted upload audit = %v", got)
	}
}

func TestCleanupRemovesLegacyPendingAudit(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	gone := uuid.New()
	kept := e.upload(t, e.users.Member, "kept.png", png, nil)
	for _, id := range []uuid.UUID{gone, kept.ID} {
		if _, err := e.pool.Exec(ctx, `INSERT INTO audit_events (entity_type, entity_id, project_id, actor_id, action, changes, created_at)
			VALUES ('attachment', $1, NULL, $2, 'create', '{}', now())`, id, e.users.Member.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.attachments.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.auditRows(t, gone); len(got) != 0 {
		t.Errorf("audit of a missing upload = %v", got)
	}
	if got := e.auditRows(t, kept.ID); !slices.Equal(got, []string{"create:null"}) {
		t.Errorf("audit of an existing upload = %v", got)
	}
}

// blobPath is the stored file of content data.
func (e env) blobPath(data string) string {
	sum := sha256.Sum256([]byte(data))
	h := hex.EncodeToString(sum[:])
	return filepath.Join(e.dir, "blobs", h[:2], h)
}

// touch creates path (and its directory) unless it exists and sets its
// modification time to at.
func touch(t *testing.T, path string, at time.Time) {
	t.Helper()
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// orphanPath is a blob file name under blobs/ab that no row references.
func (e env) orphanPath(fill string) string {
	h := "ab" + strings.Repeat(fill, 62)
	return filepath.Join(e.dir, "blobs", "ab", h)
}

// keepBlobRow uploads a linked file, so the blobs table is not empty.
func (e env) keepBlobRow(t *testing.T) string {
	t.Helper()
	i := e.issue(t, issue.CreateInput{})
	e.upload(t, e.users.Member, "keep.bin", "keep", &i.ID)
	return e.blobPath("keep")
}

// captureDefaultLog sends slog.Default to a buffer until the test ends.
func captureDefaultLog(t *testing.T) *syncBuffer {
	t.Helper()
	logs := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return logs
}

// syncBuffer collects log output written from another goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestCleanupRemovesFileLeftByFailedBlobDrop(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.keepBlobRow(t)
	e.upload(t, e.users.Member, "a.bin", "lost", nil)
	touch(t, e.blobPath("lost"), e.clock.now())
	e.clock.add(25 * time.Hour)
	e.store.failDeletes(func([]byte) bool { return true })
	if _, err := e.attachments.Cleanup(ctx); !errors.Is(err, errDeleteFailed) {
		t.Fatalf("cleanup with a failing delete = %v", err)
	}
	if !exists(e.blobPath("lost")) {
		t.Fatal("file removed although its delete failed")
	}
	e.store.failDeletes(nil)
	e.clock.add(25 * time.Hour)
	r, err := e.attachments.Cleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if exists(e.blobPath("lost")) || r.Files != 1 {
		t.Errorf("file left by a failed drop: exists %v, result %+v", exists(e.blobPath("lost")), r)
	}
	if !exists(e.blobPath("keep")) {
		t.Error("referenced file removed")
	}
}

func TestDeleteSucceedsWhenFileDeleteFails(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.keepBlobRow(t)
	i := e.issue(t, issue.CreateInput{})
	a := e.upload(t, e.users.Member, "a.bin", "linked", &i.ID)
	touch(t, e.blobPath("linked"), e.clock.now())
	e.store.failDeletes(func([]byte) bool { return true })
	if err := e.attachments.Delete(ctx, e.users.Member, a.ID); err != nil {
		t.Fatalf("delete after commit = %v, want nil", err)
	}
	if _, _, err := e.attachments.Open(ctx, e.users.Member, a.ID); !errors.Is(err, kit.ErrNotFound) {
		t.Errorf("deleted attachment opens: %v", err)
	}
	e.store.failDeletes(nil)
	e.clock.add(25 * time.Hour)
	if _, err := e.attachments.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if exists(e.blobPath("linked")) {
		t.Error("file of the deleted attachment kept")
	}
}

func TestCleanupSweepsOrphanFiles(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	now := e.clock.now()
	e.keepBlobRow(t)
	crashed := filepath.Join(e.dir, "tmp", "upload-crashed")
	running := filepath.Join(e.dir, "tmp", "upload-running")
	orphan, fresh := e.orphanPath("c"), e.orphanPath("d")
	touch(t, crashed, now.Add(-72*time.Hour))
	touch(t, running, now.Add(-time.Hour))
	touch(t, orphan, now.Add(-72*time.Hour))
	touch(t, fresh, now.Add(-time.Hour))
	touch(t, e.blobPath("keep"), now.Add(-72*time.Hour))
	r, err := e.attachments.Cleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r != (attachment.CleanupResult{Temp: 1, Files: 1}) {
		t.Errorf("result = %+v", r)
	}
	for path, want := range map[string]bool{crashed: false, orphan: false, running: true, fresh: true, e.blobPath("keep"): true} {
		if exists(path) != want {
			t.Errorf("%s exists = %v, want %v", path, !want, want)
		}
	}
}

func TestCleanupContinuesPastErrors(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.upload(t, e.users.Member, "a.bin", "first", nil)
	e.upload(t, e.users.Member, "b.bin", "second", nil)
	e.clock.add(25 * time.Hour)
	var failed []byte
	e.store.failDeletes(func(sum []byte) bool {
		if failed == nil {
			failed = sum
		}
		return bytes.Equal(sum, failed)
	})
	r, err := e.attachments.Cleanup(ctx)
	if !errors.Is(err, errDeleteFailed) {
		t.Fatalf("cleanup = %v, want the delete error", err)
	}
	left := 0
	for _, data := range []string{"first", "second"} {
		if exists(e.blobPath(data)) {
			left++
		}
	}
	if left != 1 || r.Pending != 2 || r.Blobs != 2 {
		t.Errorf("files left = %d (want the failed one only), result %+v", left, r)
	}
}

func TestCleanupKeepsFilesWhenNoBlobRows(t *testing.T) {
	e := newEnv(t)
	logs := captureDefaultLog(t)
	orphan := e.orphanPath("e")
	touch(t, orphan, e.clock.now().Add(-72*time.Hour))
	r, err := e.attachments.Cleanup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !exists(orphan) || r.Files != 0 {
		t.Errorf("file removed with an empty blobs table: result %+v", r)
	}
	if n := strings.Count(logs.String(), `"level":"WARN"`); n != 1 {
		t.Errorf("warnings = %d, want 1; log %q", n, logs.String())
	}
}

// setWorkspaceCreated sets the creation time of the workspace.
func (e env) setWorkspaceCreated(t *testing.T, at time.Time) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(), `UPDATE workspaces SET created_at = $1`, at); err != nil {
		t.Fatal(err)
	}
}

// A database recreated next to an existing data directory has a new
// workspace: stored files older than it are kept until the dump is restored.
func TestCleanupKeepsFilesOlderThanWorkspace(t *testing.T) {
	for _, tc := range []struct {
		name    string
		created time.Duration // workspace creation, relative to now
		kept    bool
	}{
		{"workspace created after the file", 0, true},
		{"workspace created before the file", -96 * time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			now := e.clock.now()
			e.keepBlobRow(t)
			e.setWorkspaceCreated(t, now.Add(tc.created))
			orphan := e.orphanPath("f")
			touch(t, orphan, now.Add(-72*time.Hour))
			r, err := e.attachments.Cleanup(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if exists(orphan) != tc.kept || (r.Files == 0) != tc.kept {
				t.Errorf("file kept = %v, want %v; result %+v", exists(orphan), tc.kept, r)
			}
			if !exists(e.blobPath("keep")) {
				t.Error("referenced file removed")
			}
		})
	}
}

func TestRunCleanupRunsAtStart(t *testing.T) {
	e := newEnv(t)
	a := e.upload(t, e.users.Member, "old.bin", "old", nil)
	e.clock.add(25 * time.Hour)
	logs := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.attachments.RunCleanup(ctx, time.Hour, slog.New(slog.NewJSONHandler(logs, nil)))
		close(done)
	}()
	stopped := false
	defer func() {
		if !stopped {
			cancel()
			<-done
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var n int
		if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM attachments WHERE id = $1`, a.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 0 && strings.Contains(logs.String(), `"pending":1`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending upload not removed at start: rows %d, log %q", n, logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
		stopped = true
	case <-time.After(5 * time.Second):
		t.Fatal("RunCleanup did not stop after its context ended")
	}
}

func TestUsedInText(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	i := e.issue(t, issue.CreateInput{})
	j := e.issue(t, issue.CreateInput{})
	inDescription := e.upload(t, e.users.Member, "a.png", png, &i.ID)
	inComment := e.upload(t, e.users.Member, "b.png", png, &i.ID)
	elsewhere := e.upload(t, e.users.Member, "c.png", png, &i.ID)
	pending := e.upload(t, e.users.Member, "d.png", png, nil)
	if _, err := e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{Description: ptr("see " + attachment.URL(inDescription))}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.comments.Create(ctx, e.users.Member, i.ID, "look "+attachment.URL(inComment), nil); err != nil {
		t.Fatal(err)
	}
	// A link in another issue's text does not count for this issue.
	if _, err := e.comments.Create(ctx, e.users.Member, j.ID, "also "+attachment.URL(elsewhere), nil); err != nil {
		t.Fatal(err)
	}
	got, err := e.attachments.UsedInText(ctx, []uuid.UUID{i.ID, j.ID})
	if err != nil {
		t.Fatal(err)
	}
	want := map[uuid.UUID]bool{inDescription.ID: true, inComment.ID: true, elsewhere.ID: false}
	if !maps.Equal(got, want) {
		t.Errorf("UsedInText = %v, want %v (pending %s)", got, want, pending.ID)
	}
}
