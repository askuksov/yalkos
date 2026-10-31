// Package comment implements flat Markdown comments on issues. Comment text
// never enters the audit log: events record only the action and the comment id.
package comment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/attachment"
	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/store/q"
)

const maxBody = 100_000

// Comment is one comment; EditedAt is set once the author changes it.
type Comment struct {
	ID, IssueID, AuthorID uuid.UUID
	Body                  string
	CreatedAt, UpdatedAt  time.Time
	EditedAt              *time.Time
}

// Service holds comment rules.
type Service struct {
	pool   *pgxpool.Pool
	q      *q.Queries
	issues *issue.Service
	linker attachment.Linker
	now    func() time.Time
}

// New builds the service; linker (optional) links uploads named by a comment
// to its issue; now is the clock.
func New(pool *pgxpool.Pool, issues *issue.Service, linker attachment.Linker, now func() time.Time) *Service {
	return &Service{pool: pool, q: q.New(pool), issues: issues, linker: linker, now: now}
}

func (s *Service) link(ctx context.Context, tx *q.Queries, actor account.User, issueID uuid.UUID, explicit []uuid.UUID, body string) error {
	if s.linker == nil {
		return nil
	}
	return s.linker.Link(ctx, tx, actor.ID, issueID, explicit, body)
}

func (s *Service) inTx(ctx context.Context, fn func(*q.Queries) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return fn(s.q.WithTx(tx)) })
}

func toComment(c q.Comment) Comment {
	return Comment{
		ID: c.ID, IssueID: c.IssueID, AuthorID: c.AuthorID, Body: c.Body,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, EditedAt: c.EditedAt,
	}
}

var errNotFound = kit.NotFoundError{Entity: "Comment"}

func validBody(v string) (string, error) {
	v = strings.TrimSpace(v)
	switch n := utf8.RuneCountInString(v); {
	case n == 0:
		return "", kit.Input("body", kit.ReasonRequired, "write a comment")
	case n > maxBody:
		return "", kit.Input("body", kit.ReasonTooLong, "is too long").With("max", maxBody)
	}
	return v, nil
}

// record writes a comment event without its text.
func record(ctx context.Context, tx *q.Queries, actor account.User, id uuid.UUID, projectID uuid.UUID, action string, at time.Time) error {
	return audit.Record(ctx, tx, audit.Event{
		EntityType: "comment", Action: action, EntityID: id, ProjectID: &projectID, ActorID: &actor.ID, At: at,
	})
}

// Create adds a comment to a live issue of a writable project; the actor's
// pending uploads in attachmentIDs or in the text are linked to the issue.
func (s *Service) Create(ctx context.Context, actor account.User, issueID uuid.UUID, body string, attachmentIDs []uuid.UUID) (Comment, error) {
	body, err := validBody(body)
	if err != nil {
		return Comment{}, err
	}
	var out Comment
	err = s.inTx(ctx, func(tx *q.Queries) error {
		i, err := s.issues.LockForWrite(ctx, tx, issueID)
		if err != nil {
			return err
		}
		now := s.now()
		row, err := tx.CreateComment(ctx, q.CreateCommentParams{IssueID: issueID, AuthorID: actor.ID, Body: body, Now: now})
		if err != nil {
			return fmt.Errorf("create comment: %w", err)
		}
		out = toComment(row)
		if err := record(ctx, tx, actor, row.ID, i.ProjectID, "create", now); err != nil {
			return err
		}
		return s.link(ctx, tx, actor, issueID, attachmentIDs, body)
	})
	if err != nil {
		return Comment{}, err
	}
	return out, nil
}

// lock locks the comment's issue (live, writable project) and the comment.
func (s *Service) lock(ctx context.Context, tx *q.Queries, id uuid.UUID) (Comment, issue.Issue, error) {
	c, err := tx.GetComment(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Comment{}, issue.Issue{}, errNotFound
	}
	if err != nil {
		return Comment{}, issue.Issue{}, fmt.Errorf("get comment: %w", err)
	}
	i, err := s.issues.LockForWrite(ctx, tx, c.IssueID)
	if errors.Is(err, kit.ErrNotFound) {
		return Comment{}, issue.Issue{}, errNotFound
	}
	if err != nil {
		return Comment{}, issue.Issue{}, err
	}
	c, err = tx.GetCommentForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Comment{}, issue.Issue{}, errNotFound
	}
	if err != nil {
		return Comment{}, issue.Issue{}, fmt.Errorf("lock comment: %w", err)
	}
	return toComment(c), i, nil
}

// Update changes the text of the author's own comment.
func (s *Service) Update(ctx context.Context, actor account.User, id uuid.UUID, body string) (Comment, error) {
	body, err := validBody(body)
	if err != nil {
		return Comment{}, err
	}
	var out Comment
	err = s.inTx(ctx, func(tx *q.Queries) error {
		cur, i, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if cur.AuthorID != actor.ID {
			return kit.ErrForbidden
		}
		if cur.Body == body {
			out = cur
			return nil
		}
		now := s.now()
		row, err := tx.UpdateComment(ctx, q.UpdateCommentParams{ID: id, Body: body, Now: now})
		if err != nil {
			return fmt.Errorf("update comment: %w", err)
		}
		out = toComment(row)
		if err := record(ctx, tx, actor, id, i.ProjectID, "update", now); err != nil {
			return err
		}
		return s.link(ctx, tx, actor, i.ID, nil, body)
	})
	if err != nil {
		return Comment{}, err
	}
	return out, nil
}

// Delete removes a comment for good (its author, Owner or Admin); the issue's
// history keeps that a comment was deleted.
func (s *Service) Delete(ctx context.Context, actor account.User, id uuid.UUID) error {
	return s.inTx(ctx, func(tx *q.Queries) error {
		cur, i, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if cur.AuthorID != actor.ID && !actor.IsAdmin() {
			return kit.ErrForbidden
		}
		if err := tx.DeleteComment(ctx, id); err != nil {
			return fmt.Errorf("delete comment: %w", err)
		}
		now := s.now()
		if err := record(ctx, tx, actor, id, i.ProjectID, "delete", now); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			EntityType: "issue", Action: "update", EntityID: i.ID, ProjectID: &i.ProjectID, ActorID: &actor.ID,
			Changes: map[string]audit.Change{"commentDeleted": {From: id, To: nil}}, At: now,
		})
	})
}

const cursorField = "createdAt"

// List returns the issue's comments, oldest first.
func (s *Service) List(ctx context.Context, issueID uuid.UUID, a page.Args) (page.Result[Comment], error) {
	w, err := a.Window()
	if err != nil {
		return page.Result[Comment]{}, err
	}
	var at *time.Time
	var id *uuid.UUID
	if w.Cursor != nil {
		field := "after"
		if !w.Forward {
			field = "before"
		}
		c, err := page.Decode(*w.Cursor, field)
		if err != nil {
			return page.Result[Comment]{}, err
		}
		var t time.Time
		if c.F != cursorField || c.D != "asc" || json.Unmarshal(c.V, &t) != nil {
			return page.Result[Comment]{}, kit.Input(field, kit.ReasonInvalidFormat, "invalid cursor")
		}
		at, id = &t, &c.ID
	}
	var rows []q.Comment
	if w.Forward {
		rows, err = s.q.ListCommentsForward(ctx, q.ListCommentsForwardParams{IssueID: issueID, AfterAt: at, AfterID: id, Lim: int32(w.Limit + 1)})
	} else {
		rows, err = s.q.ListCommentsBackward(ctx, q.ListCommentsBackwardParams{IssueID: issueID, BeforeAt: at, BeforeID: id, Lim: int32(w.Limit + 1)})
	}
	if err != nil {
		return page.Result[Comment]{}, fmt.Errorf("list comments: %w", err)
	}
	items := make([]Comment, len(rows))
	for i, r := range rows {
		items[i] = toComment(r)
	}
	return page.Finish(items, w, func(c Comment) string {
		v, _ := json.Marshal(c.CreatedAt)
		return page.Encode(page.Cursor{F: cursorField, D: "asc", V: v, ID: c.ID})
	}), nil
}

// Counts returns the number of comments of each issue (0 for none).
func (s *Service) Counts(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	rows, err := s.q.CommentCounts(ctx, issueIDs)
	if err != nil {
		return nil, fmt.Errorf("comment counts: %w", err)
	}
	out := make(map[uuid.UUID]int, len(issueIDs))
	for _, id := range issueIDs {
		out[id] = 0
	}
	for _, r := range rows {
		out[r.IssueID] = int(r.N)
	}
	return out, nil
}

// ByIDs loads comments in the order of ids; missing ones are omitted.
func (s *Service) ByIDs(ctx context.Context, ids []uuid.UUID) ([]Comment, error) {
	rows, err := s.q.ListCommentsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("comments by ids: %w", err)
	}
	byID := make(map[uuid.UUID]Comment, len(rows))
	for _, r := range rows {
		byID[r.ID] = toComment(r)
	}
	out := make([]Comment, 0, len(ids))
	for _, id := range ids {
		if c, ok := byID[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}
