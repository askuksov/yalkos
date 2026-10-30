// Package issue implements issues and sub-issues (spec §2.4).
package issue

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/store/q"
)

// Issue is a live (not deleted) issue.
type Issue struct {
	ID, ProjectID, StateID, CreatorID uuid.UUID
	Number                            int
	// Identifier is KEY-N with the project's current key.
	Identifier, Title, Description string
	Priority                       int
	AssigneeID, ParentID           *uuid.UUID
	DueDate                        *time.Time
	SortOrder                      float64
	CreatedAt, UpdatedAt           time.Time
	CompletedAt, CanceledAt        *time.Time
}

// Service holds issue rules.
type Service struct {
	pool     *pgxpool.Pool
	q        *q.Queries
	projects *project.Service
	now      func() time.Time
}

// New builds the service; now is the clock.
func New(pool *pgxpool.Pool, projects *project.Service, now func() time.Time) *Service {
	return &Service{pool: pool, q: q.New(pool), projects: projects, now: now}
}

func (s *Service) inTx(ctx context.Context, fn func(*q.Queries) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return fn(s.q.WithTx(tx)) })
}

func toIssue(i q.Issue, key string) Issue {
	return Issue{
		ID: i.ID, ProjectID: i.ProjectID, StateID: i.StateID, CreatorID: i.CreatorID,
		Number: int(i.Number), Identifier: fmt.Sprintf("%s-%d", key, i.Number),
		Title: i.Title, Description: i.Description, Priority: int(i.Priority),
		AssigneeID: i.AssigneeID, ParentID: i.ParentID, DueDate: i.DueDate, SortOrder: i.SortOrder,
		CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt, CompletedAt: i.CompletedAt, CanceledAt: i.CanceledAt,
	}
}

var errNotFound = kit.NotFoundError{Entity: "Issue"}

func record(ctx context.Context, tx *q.Queries, actor account.User, i Issue, action string, changes map[string]audit.Change, at time.Time) error {
	return audit.Record(ctx, tx, audit.Event{
		EntityType: "issue", Action: action, EntityID: i.ID, ProjectID: &i.ProjectID, ActorID: &actor.ID,
		Changes: changes, At: at,
	})
}

// lockProject locks a writable project referenced by an input field.
func (s *Service) lockProject(ctx context.Context, tx *q.Queries, id uuid.UUID) (q.Project, error) {
	p, err := s.projects.LockWritable(ctx, tx, id)
	if errors.Is(err, kit.ErrNotFound) {
		return q.Project{}, kit.Input("projectId", kit.ReasonNotFound, "no such project")
	}
	return p, err
}

// lockIssue locks the issue's project (writable) and then the live issue row.
// The project lock comes first so writers of one project never deadlock.
func (s *Service) lockIssue(ctx context.Context, tx *q.Queries, id uuid.UUID) (Issue, error) {
	for range 3 {
		row, err := tx.GetIssue(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return Issue{}, errNotFound
		}
		if err != nil {
			return Issue{}, fmt.Errorf("get issue: %w", err)
		}
		if _, err := s.projects.LockWritable(ctx, tx, row.Issue.ProjectID); err != nil {
			return Issue{}, err
		}
		locked, err := tx.GetIssueForUpdate(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return Issue{}, errNotFound
		}
		if err != nil {
			return Issue{}, fmt.Errorf("lock issue: %w", err)
		}
		if locked.Issue.ProjectID == row.Issue.ProjectID {
			return toIssue(locked.Issue, locked.ProjectKey), nil
		}
		// Moved to another project meanwhile: lock that one too.
	}
	return Issue{}, errors.New("issue: moved concurrently, try again")
}

// liveIssue reads a live issue referenced by an input field.
func liveIssue(ctx context.Context, tx *q.Queries, id uuid.UUID, field string) (q.Issue, error) {
	row, err := tx.GetIssue(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return q.Issue{}, kit.Input(field, kit.ReasonNotFound, "no such issue")
	}
	if err != nil {
		return q.Issue{}, fmt.Errorf("get issue: %w", err)
	}
	return row.Issue, nil
}

// stateFor returns the given state of the project, or its default state.
func stateFor(ctx context.Context, tx *q.Queries, projectID uuid.UUID, stateID *uuid.UUID) (q.State, error) {
	if stateID == nil {
		st, err := tx.GetDefaultState(ctx, projectID)
		if err != nil {
			return q.State{}, fmt.Errorf("default state: %w", err)
		}
		return st, nil
	}
	st, err := tx.GetState(ctx, *stateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return q.State{}, kit.Input("stateId", kit.ReasonNotFound, "no such state")
	}
	if err != nil {
		return q.State{}, fmt.Errorf("get state: %w", err)
	}
	if st.ProjectID != projectID {
		return q.State{}, kit.Input("stateId", kit.ReasonWrongProject, "state belongs to another project")
	}
	return st, nil
}

func checkAssignee(ctx context.Context, tx *q.Queries, id *uuid.UUID) error {
	if id == nil {
		return nil
	}
	u, err := tx.GetUserByID(ctx, *id)
	if errors.Is(err, pgx.ErrNoRows) {
		return kit.Input("assigneeId", kit.ReasonNotFound, "no such user")
	}
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if u.Disabled {
		return kit.Input("assigneeId", kit.ReasonNotAllowed, "user is disabled")
	}
	return nil
}

// CreateInput is the new issue form. ProjectID may be omitted for a sub-issue.
type CreateInput struct {
	ProjectID, ParentID, StateID, AssigneeID *uuid.UUID
	Title, Description                       string
	Priority                                 int
	DueDate                                  *time.Time
	SortOrder                                *float64
}

// Create adds an issue with the project's next number.
func (s *Service) Create(ctx context.Context, actor account.User, in CreateInput) (Issue, error) {
	title, err := validTitle(in.Title)
	if err != nil {
		return Issue{}, err
	}
	if err := validDescription(in.Description); err != nil {
		return Issue{}, err
	}
	if err := validPriority(in.Priority); err != nil {
		return Issue{}, err
	}
	if in.ProjectID == nil && in.ParentID == nil {
		return Issue{}, kit.Input("projectId", kit.ReasonRequired, "choose a project")
	}
	var out Issue
	err = s.inTx(ctx, func(tx *q.Queries) error {
		var projectID uuid.UUID
		if in.ProjectID != nil {
			projectID = *in.ProjectID
		}
		if in.ParentID != nil {
			parent, err := liveIssue(ctx, tx, *in.ParentID, "parentId")
			if err != nil {
				return err
			}
			if in.ProjectID != nil && parent.ProjectID != projectID {
				return kit.Input("parentId", kit.ReasonWrongProject, "parent belongs to another project")
			}
			projectID = parent.ProjectID
		}
		p, err := s.lockProject(ctx, tx, projectID)
		if err != nil {
			return err
		}
		if in.ParentID != nil {
			// Re-check under the project lock: the parent may have moved or gone.
			parent, err := liveIssue(ctx, tx, *in.ParentID, "parentId")
			if err != nil {
				return err
			}
			if parent.ProjectID != projectID {
				return kit.Input("parentId", kit.ReasonWrongProject, "parent belongs to another project")
			}
		}
		st, err := stateFor(ctx, tx, projectID, in.StateID)
		if err != nil {
			return err
		}
		if err := checkAssignee(ctx, tx, in.AssigneeID); err != nil {
			return err
		}
		number, err := tx.NextIssueNumber(ctx, projectID)
		if err != nil {
			return fmt.Errorf("next issue number: %w", err)
		}
		var sortOrder float64
		if in.SortOrder != nil {
			sortOrder = *in.SortOrder
		} else if sortOrder, err = tx.MinSortOrder(ctx, projectID); err != nil {
			return fmt.Errorf("min sort order: %w", err)
		}
		now := s.now()
		completed, canceled := project.CompletionTimes("", project.StateType(st.Type), nil, nil, now)
		row, err := tx.CreateIssue(ctx, q.CreateIssueParams{
			ProjectID: projectID, Number: number, Title: title, Description: in.Description, StateID: st.ID,
			Priority: int16(in.Priority), AssigneeID: in.AssigneeID, CreatorID: actor.ID, ParentID: in.ParentID,
			DueDate: dateOnly(in.DueDate), SortOrder: sortOrder, Now: now, CompletedAt: completed, CanceledAt: canceled,
		})
		if err != nil {
			return fmt.Errorf("create issue: %w", err)
		}
		if err := tx.InsertIssueNumber(ctx, q.InsertIssueNumberParams{ProjectID: projectID, Number: number, IssueID: row.ID}); err != nil {
			return fmt.Errorf("issue number: %w", err)
		}
		out = toIssue(row, p.Key)
		return record(ctx, tx, actor, out, "create", audit.Diff(nil, snapshot(out)), now)
	})
	if err != nil {
		return Issue{}, err
	}
	return out, nil
}

var identifierRe = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]{0,6})-([1-9][0-9]{0,9})$`)

// Get finds a live issue by UUID or identifier (KEY-N, also former keys and numbers).
func (s *Service) Get(ctx context.Context, ref string) (Issue, error) {
	ref = strings.TrimSpace(ref)
	if id, err := uuid.Parse(ref); err == nil {
		row, err := s.q.GetIssue(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return Issue{}, errNotFound
		}
		if err != nil {
			return Issue{}, fmt.Errorf("get issue: %w", err)
		}
		return toIssue(row.Issue, row.ProjectKey), nil
	}
	m := identifierRe.FindStringSubmatch(ref)
	if m == nil {
		return Issue{}, errNotFound
	}
	n, err := strconv.ParseInt(m[2], 10, 32)
	if err != nil {
		return Issue{}, errNotFound
	}
	row, err := s.q.GetIssueByIdentifier(ctx, q.GetIssueByIdentifierParams{Key: strings.ToUpper(m[1]), Number: int32(n)})
	if errors.Is(err, pgx.ErrNoRows) {
		return Issue{}, errNotFound
	}
	if err != nil {
		return Issue{}, fmt.Errorf("get issue: %w", err)
	}
	return toIssue(row.Issue, row.ProjectKey), nil
}

// ByIDs loads live issues in the order of ids; missing ones are omitted.
func (s *Service) ByIDs(ctx context.Context, ids []uuid.UUID) ([]Issue, error) {
	rows, err := s.q.ListIssuesByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("issues by ids: %w", err)
	}
	byID := make(map[uuid.UUID]Issue, len(rows))
	for _, r := range rows {
		byID[r.Issue.ID] = toIssue(r.Issue, r.ProjectKey)
	}
	out := make([]Issue, 0, len(ids))
	for _, id := range ids {
		if i, ok := byID[id]; ok {
			out = append(out, i)
		}
	}
	return out, nil
}

// Patch edits an issue: nil pointers keep values; Opt fields can be cleared.
type Patch struct {
	Title, Description *string
	StateID            *uuid.UUID
	Priority           *int
	AssigneeID         kit.Opt[uuid.UUID]
	DueDate            kit.Opt[time.Time]
	ParentID           kit.Opt[uuid.UUID]
	SortOrder          *float64
}

// Update applies a patch; an unchanged issue records nothing and keeps updatedAt.
func (s *Service) Update(ctx context.Context, actor account.User, id uuid.UUID, p Patch) (Issue, error) {
	var title *string
	if p.Title != nil {
		t, err := validTitle(*p.Title)
		if err != nil {
			return Issue{}, err
		}
		title = &t
	}
	if p.Description != nil {
		if err := validDescription(*p.Description); err != nil {
			return Issue{}, err
		}
	}
	if p.Priority != nil {
		if err := validPriority(*p.Priority); err != nil {
			return Issue{}, err
		}
	}
	var out Issue
	err := s.inTx(ctx, func(tx *q.Queries) error {
		cur, err := s.lockIssue(ctx, tx, id)
		if err != nil {
			return err
		}
		now := s.now()
		next := cur
		if title != nil {
			next.Title = *title
		}
		if p.Description != nil {
			next.Description = *p.Description
		}
		if p.Priority != nil {
			next.Priority = *p.Priority
		}
		if p.SortOrder != nil {
			next.SortOrder = *p.SortOrder
		}
		if p.StateID != nil && *p.StateID != cur.StateID {
			st, err := stateFor(ctx, tx, cur.ProjectID, p.StateID)
			if err != nil {
				return err
			}
			old, err := tx.GetState(ctx, cur.StateID)
			if err != nil {
				return fmt.Errorf("get state: %w", err)
			}
			next.StateID = st.ID
			next.CompletedAt, next.CanceledAt = project.CompletionTimes(
				project.StateType(old.Type), project.StateType(st.Type), cur.CompletedAt, cur.CanceledAt, now)
		}
		if p.AssigneeID.Set {
			if err := checkAssignee(ctx, tx, p.AssigneeID.Value); err != nil {
				return err
			}
			next.AssigneeID = p.AssigneeID.Value
		}
		if p.DueDate.Set {
			next.DueDate = dateOnly(p.DueDate.Value)
		}
		if p.ParentID.Set {
			if err := checkParent(ctx, tx, cur, p.ParentID.Value); err != nil {
				return err
			}
			next.ParentID = p.ParentID.Value
		}
		changes := audit.Diff(snapshot(cur), snapshot(next))
		if len(changes) == 0 {
			out = cur
			return nil
		}
		row, err := tx.UpdateIssue(ctx, q.UpdateIssueParams{
			ID: id, Title: next.Title, Description: next.Description, StateID: next.StateID,
			Priority: int16(next.Priority), AssigneeID: next.AssigneeID, ParentID: next.ParentID,
			DueDate: next.DueDate, SortOrder: next.SortOrder, CompletedAt: next.CompletedAt,
			CanceledAt: next.CanceledAt, Now: now,
		})
		if err != nil {
			return fmt.Errorf("update issue: %w", err)
		}
		out = next
		out.UpdatedAt = row.UpdatedAt
		return record(ctx, tx, actor, out, "update", changes, now)
	})
	if err != nil {
		return Issue{}, err
	}
	return out, nil
}

// checkParent validates a new parent for cur (called under cur's project lock,
// so concurrent parent changes in the project are serialized).
func checkParent(ctx context.Context, tx *q.Queries, cur Issue, parentID *uuid.UUID) error {
	if parentID == nil {
		return nil
	}
	cycle := kit.Input("parentId", kit.ReasonCycle, "would create a cycle")
	if *parentID == cur.ID {
		return cycle
	}
	parent, err := liveIssue(ctx, tx, *parentID, "parentId")
	if err != nil {
		return err
	}
	if parent.ProjectID != cur.ProjectID {
		return kit.Input("parentId", kit.ReasonWrongProject, "parent belongs to another project")
	}
	ancestors, err := tx.IssueAncestorIDs(ctx, parent.ID)
	if err != nil {
		return fmt.Errorf("issue ancestors: %w", err)
	}
	if slices.Contains(ancestors, cur.ID) {
		return cycle
	}
	return nil
}

// Delete soft-deletes an issue; its children become top-level issues.
func (s *Service) Delete(ctx context.Context, actor account.User, id uuid.UUID) error {
	return s.inTx(ctx, func(tx *q.Queries) error {
		cur, err := s.lockIssue(ctx, tx, id)
		if err != nil {
			return err
		}
		now := s.now()
		if err := tx.SoftDeleteIssue(ctx, q.SoftDeleteIssueParams{ID: id, Now: &now}); err != nil {
			return fmt.Errorf("delete issue: %w", err)
		}
		children, err := tx.DetachChildren(ctx, q.DetachChildrenParams{ParentID: &id, Now: now})
		if err != nil {
			return fmt.Errorf("detach children: %w", err)
		}
		for _, c := range children {
			if err := record(ctx, tx, actor, Issue{ID: c, ProjectID: cur.ProjectID}, "update",
				map[string]audit.Change{"parentId": {From: id, To: nil}}, now); err != nil {
				return err
			}
		}
		return record(ctx, tx, actor, cur, "delete", nil, now)
	})
}
