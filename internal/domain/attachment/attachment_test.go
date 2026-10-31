package attachment_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
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
	store, err := blob.NewLocalFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	projects := project.New(pool, c.now)
	issues := issue.New(pool, projects, c.now)
	atts := attachment.New(pool, issues, store, &blob.Locks{}, 1<<20, c.now)
	issues.SetLinker(atts)
	e := env{pool: pool, users: users, clock: c, dir: dir, projects: projects, issues: issues,
		comments: comment.New(pool, issues, atts, c.now), attachments: atts}
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
	got, r, err := e.attachments.Open(ctx, a.ID)
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
	if _, _, err := e.attachments.Open(ctx, uuid.New()); !errors.Is(err, kit.ErrNotFound) {
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
	removed, err := e.attachments.Cleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1 (the old pending upload)", removed)
	}
	if _, _, err := e.attachments.Open(ctx, old.ID); !errors.Is(err, kit.ErrNotFound) {
		t.Errorf("old pending = %v", err)
	}
	for _, a := range []attachment.Attachment{shared, recent} {
		if _, r, err := e.attachments.Open(ctx, a.ID); err != nil {
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
