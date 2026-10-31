// Package attachment implements files on issues: uploads (pending until their
// issue or comment is saved), downloads, deletion and cleanup.
package attachment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/blob"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/store/q"
)

const (
	maxName      = 255
	maxExtension = 16
	// Pending uploads (no issue yet) older than this are removed.
	pendingTTL = 24 * time.Hour
	// Blobs no attachment references are removed once older than this.
	orphanTTL = time.Hour
)

// Attachment is an uploaded file; IssueID is nil while it is pending.
type Attachment struct {
	ID          uuid.UUID
	IssueID     *uuid.UUID
	UploaderID  uuid.UUID
	Name        string
	ContentType string
	Size        int64
	CreatedAt   time.Time
}

// Linker links pending uploads to an issue (implemented by *Service).
type Linker = issue.Linker

// Service holds attachment rules.
type Service struct {
	pool     *pgxpool.Pool
	q        *q.Queries
	issues   *issue.Service
	store    blob.Store
	locks    *blob.Locks
	maxBytes int64
	now      func() time.Time
}

// New builds the service; maxBytes caps one file; now is the clock.
func New(pool *pgxpool.Pool, issues *issue.Service, store blob.Store, locks *blob.Locks, maxBytes int64, now func() time.Time) *Service {
	return &Service{pool: pool, q: q.New(pool), issues: issues, store: store, locks: locks, maxBytes: maxBytes, now: now}
}

// MaxBytes is the per-file limit.
func (s *Service) MaxBytes() int64 { return s.maxBytes }

func (s *Service) inTx(ctx context.Context, fn func(*q.Queries) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return fn(s.q.WithTx(tx)) })
}

var errNotFound = kit.NotFoundError{Entity: "Attachment"}

var inlineTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "application/pdf": true,
}

// Inline reports whether a file of this (detected) type may be shown in the
// browser: raster images and PDF. Everything else downloads.
func Inline(contentType string) bool { return inlineTypes[contentType] }

// URL is the relative download link of a.
func URL(a Attachment) string {
	return "/api/attachments/" + a.ID.String() + "/" + url.PathEscape(a.Name)
}

// CleanName keeps the base name of a client file name without control or
// format characters, at most 255 runes (a short extension survives trimming).
func CleanName(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || name == "/" {
		return "file"
	}
	if utf8.RuneCountInString(name) <= maxName {
		return name
	}
	ext := path.Ext(name)
	if n := utf8.RuneCountInString(ext); n > 1 && n <= maxExtension {
		base := []rune(strings.TrimSuffix(name, ext))
		return string(base[:maxName-n]) + ext
	}
	return string([]rune(name)[:maxName])
}

func fromRow(a q.Attachment, size int64, contentType string) Attachment {
	return Attachment{
		ID: a.ID, IssueID: a.IssueID, UploaderID: a.UploaderID, Name: a.Name,
		ContentType: contentType, Size: size, CreatedAt: a.CreatedAt,
	}
}

func (s *Service) tooLarge() error {
	mb := s.maxBytes >> 20
	return kit.Input("file", kit.ReasonTooLarge, fmt.Sprintf("file is larger than %d MB", mb)).With("maxMb", int(mb))
}

// lockIssue locks a live issue of a writable project named by issueId.
func (s *Service) lockIssue(ctx context.Context, tx *q.Queries, id uuid.UUID) (issue.Issue, error) {
	i, err := s.issues.LockForWrite(ctx, tx, id)
	if errors.Is(err, kit.ErrNotFound) {
		return issue.Issue{}, kit.Input("issueId", kit.ReasonNotFound, "no such issue")
	}
	return i, err
}

// Upload stores a file; with issueID it belongs to that issue at once,
// otherwise it stays pending until an issue or comment links it.
func (s *Service) Upload(ctx context.Context, actor account.User, name string, r io.Reader, issueID *uuid.UUID) (Attachment, error) {
	name = CleanName(name)
	st, err := s.store.Put(ctx, r, s.maxBytes)
	if errors.Is(err, blob.ErrTooLarge) {
		return Attachment{}, s.tooLarge()
	}
	if err != nil {
		return Attachment{}, err
	}
	// The type comes from the content, never from the client.
	contentType := http.DetectContentType(st.Head)
	unlock := s.locks.Lock(st.Sum)
	defer unlock()
	if err := st.Commit(); err != nil {
		st.Discard()
		return Attachment{}, err
	}
	var out Attachment
	err = s.inTx(ctx, func(tx *q.Queries) error {
		var projectID *uuid.UUID
		if issueID != nil {
			i, err := s.lockIssue(ctx, tx, *issueID)
			if err != nil {
				return err
			}
			projectID = &i.ProjectID
		}
		now := s.now()
		if err := tx.InsertBlob(ctx, q.InsertBlobParams{Sha256: st.Sum, Size: st.Size, ContentType: contentType, Now: now}); err != nil {
			return fmt.Errorf("insert blob: %w", err)
		}
		row, err := tx.CreateAttachment(ctx, q.CreateAttachmentParams{
			IssueID: issueID, UploaderID: actor.ID, BlobSha256: st.Sum, Name: name, Now: now,
		})
		if err != nil {
			return fmt.Errorf("create attachment: %w", err)
		}
		// The stored blob keeps the type detected at its first upload.
		b, err := tx.GetAttachment(ctx, row.ID)
		if err != nil {
			return fmt.Errorf("get attachment: %w", err)
		}
		out = fromRow(row, b.Size, b.ContentType)
		if err := audit.Record(ctx, tx, audit.Event{
			EntityType: "attachment", Action: "create", EntityID: row.ID, ProjectID: projectID, ActorID: &actor.ID,
			Changes: map[string]audit.Change{"name": {To: name}, "size": {To: out.Size}}, At: now,
		}); err != nil {
			return err
		}
		if issueID == nil {
			return nil
		}
		return audit.Record(ctx, tx, audit.Event{
			EntityType: "issue", Action: "update", EntityID: *issueID, ProjectID: projectID, ActorID: &actor.ID,
			Changes: map[string]audit.Change{"attachmentAdded": {To: added(out.ID, name, out.Size)}}, At: now,
		})
	})
	if err != nil {
		s.dropIfUnused(ctx, st.Sum)
		return Attachment{}, err
	}
	return out, nil
}

func added(id uuid.UUID, name string, size int64) map[string]any {
	return map[string]any{"id": id, "name": name, "size": size}
}

// dropIfUnused removes a committed file whose blob row was never written
// (caller holds the hash lock).
func (s *Service) dropIfUnused(ctx context.Context, sum []byte) {
	exists, err := s.q.BlobExists(ctx, sum)
	if err != nil || exists {
		return
	}
	if err := s.store.Delete(ctx, sum); err != nil {
		slog.Default().Warn("attachment: drop unused file", slog.String("error", err.Error()))
	}
}

// Link implements issue.Linker: the actor's pending uploads named in explicit
// or in markdown join issueID; other ids are ignored.
func (s *Service) Link(ctx context.Context, tx *q.Queries, actorID, issueID uuid.UUID, explicit []uuid.UUID, markdown string) error {
	ids := append(append([]uuid.UUID(nil), explicit...), IDsInMarkdown(markdown)...)
	if len(ids) == 0 {
		return nil
	}
	linked, err := tx.LinkPendingAttachments(ctx, q.LinkPendingAttachmentsParams{IssueID: &issueID, Ids: ids, UploaderID: actorID})
	if err != nil {
		return fmt.Errorf("link attachments: %w", err)
	}
	if len(linked) == 0 {
		return nil
	}
	row, err := tx.GetIssue(ctx, issueID)
	if err != nil {
		return fmt.Errorf("get issue: %w", err)
	}
	now := s.now()
	for _, l := range linked {
		if err := audit.Record(ctx, tx, audit.Event{
			EntityType: "issue", Action: "update", EntityID: issueID, ProjectID: &row.Issue.ProjectID, ActorID: &actorID,
			Changes: map[string]audit.Change{"attachmentAdded": {To: added(l.ID, l.Name, l.Size)}}, At: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

// Open returns an attachment with its content.
func (s *Service) Open(ctx context.Context, id uuid.UUID) (Attachment, io.ReadSeekCloser, error) {
	row, err := s.q.GetAttachment(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Attachment{}, nil, errNotFound
	}
	if err != nil {
		return Attachment{}, nil, fmt.Errorf("get attachment: %w", err)
	}
	r, err := s.store.Open(ctx, row.Attachment.BlobSha256)
	if err != nil {
		return Attachment{}, nil, err
	}
	return fromRow(row.Attachment, row.Size, row.ContentType), r, nil
}

// List returns the issue's attachments, oldest first.
func (s *Service) List(ctx context.Context, issueID uuid.UUID) ([]Attachment, error) {
	m, err := s.ByIssues(ctx, []uuid.UUID{issueID})
	if err != nil {
		return nil, err
	}
	return m[issueID], nil
}

// ByIssues returns the attachments of each issue, oldest first.
func (s *Service) ByIssues(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID][]Attachment, error) {
	rows, err := s.q.ListIssueAttachments(ctx, issueIDs)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	out := make(map[uuid.UUID][]Attachment, len(issueIDs))
	for _, id := range issueIDs {
		out[id] = []Attachment{}
	}
	for _, r := range rows {
		id := *r.Attachment.IssueID
		out[id] = append(out[id], fromRow(r.Attachment, r.Size, r.ContentType))
	}
	return out, nil
}

// Delete removes an attachment (uploader, Owner or Admin); its file goes when
// nothing else references the same content.
func (s *Service) Delete(ctx context.Context, actor account.User, id uuid.UUID) error {
	var sum []byte
	err := s.inTx(ctx, func(tx *q.Queries) error {
		row, err := tx.GetAttachment(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound
		}
		if err != nil {
			return fmt.Errorf("get attachment: %w", err)
		}
		var projectID *uuid.UUID
		if row.Attachment.IssueID != nil {
			i, err := s.issues.LockForWrite(ctx, tx, *row.Attachment.IssueID)
			if errors.Is(err, kit.ErrNotFound) {
				return errNotFound
			}
			if err != nil {
				return err
			}
			projectID = &i.ProjectID
		}
		a, err := tx.GetAttachmentForUpdate(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound
		}
		if err != nil {
			return fmt.Errorf("lock attachment: %w", err)
		}
		if a.UploaderID != actor.ID && !actor.IsAdmin() {
			return kit.ErrForbidden
		}
		if !sameIssue(a.IssueID, row.Attachment.IssueID) {
			return errors.New("attachment: linked concurrently, try again")
		}
		if err := tx.DeleteAttachment(ctx, id); err != nil {
			return fmt.Errorf("delete attachment: %w", err)
		}
		sum = a.BlobSha256
		now := s.now()
		if err := audit.Record(ctx, tx, audit.Event{
			EntityType: "attachment", Action: "delete", EntityID: id, ProjectID: projectID, ActorID: &actor.ID,
			Changes: map[string]audit.Change{"name": {From: a.Name}}, At: now,
		}); err != nil {
			return err
		}
		if a.IssueID == nil {
			return nil
		}
		return audit.Record(ctx, tx, audit.Event{
			EntityType: "issue", Action: "update", EntityID: *a.IssueID, ProjectID: projectID, ActorID: &actor.ID,
			Changes: map[string]audit.Change{"attachmentDeleted": {From: map[string]any{"id": id, "name": a.Name}}}, At: now,
		})
	})
	if err != nil {
		return err
	}
	_, err = s.dropBlob(ctx, sum)
	return err
}

func sameIssue(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// dropBlob deletes the blob row and file when no attachment references them.
func (s *Service) dropBlob(ctx context.Context, sum []byte) (bool, error) {
	unlock := s.locks.Lock(sum)
	defer unlock()
	n, err := s.q.DeleteBlobIfUnreferenced(ctx, sum)
	if err != nil {
		return false, fmt.Errorf("delete blob: %w", err)
	}
	if n == 0 {
		return false, nil
	}
	return true, s.store.Delete(ctx, sum)
}

// Cleanup removes pending uploads older than 24 h and unreferenced files
// older than 1 h; it returns the number of removed uploads.
func (s *Service) Cleanup(ctx context.Context) (int, error) {
	now := s.now()
	removed, err := s.q.DeleteOldPendingAttachments(ctx, now.Add(-pendingTTL))
	if err != nil {
		return 0, fmt.Errorf("delete pending attachments: %w", err)
	}
	sums, err := s.q.UnreferencedBlobs(ctx, now.Add(-orphanTTL))
	if err != nil {
		return len(removed), fmt.Errorf("unreferenced blobs: %w", err)
	}
	for _, sum := range sums {
		if _, err := s.dropBlob(ctx, sum); err != nil {
			return len(removed), err
		}
	}
	return len(removed), nil
}
