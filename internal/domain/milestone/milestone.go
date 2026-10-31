// Package milestone implements milestones: named goals of one project whose
// status and progress follow their issues.
package milestone

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/store/q"
)

const maxName = 100

// Milestone is a project's goal; Manual is nil while the status is automatic.
type Milestone struct {
	ID, ProjectID        uuid.UUID
	Name, Description    string
	TargetDate           *time.Time
	LeadID, CreatedBy    *uuid.UUID
	SortOrder            float64
	Manual               *Status
	CreatedAt, UpdatedAt time.Time
}

// Service holds milestone rules.
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

// Today is the server's current date for Overdue.
func (s *Service) Today() time.Time { return s.now().Local() }

func (s *Service) inTx(ctx context.Context, fn func(*q.Queries) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return fn(s.q.WithTx(tx)) })
}

func toMilestone(m q.Milestone) Milestone {
	out := Milestone{
		ID: m.ID, ProjectID: m.ProjectID, Name: m.Name, Description: m.Description, TargetDate: m.TargetDate,
		LeadID: m.LeadID, CreatedBy: m.CreatedBy, SortOrder: m.SortOrder, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
	if m.StatusManual != nil {
		st := Status(*m.StatusManual)
		out.Manual = &st
	}
	return out
}

// snapshot is the audited view of a milestone (API field names).
func snapshot(m Milestone) map[string]any {
	var date any
	if m.TargetDate != nil {
		date = m.TargetDate.Format(time.DateOnly)
	}
	status := "automatic"
	if m.Manual != nil {
		status = string(*m.Manual)
	}
	return map[string]any{
		"name": m.Name, "description": m.Description, "targetDate": date, "leadId": m.LeadID, "status": status,
	}
}

func record(ctx context.Context, tx *q.Queries, actor account.User, m Milestone, action string, changes map[string]audit.Change, at time.Time) error {
	return audit.Record(ctx, tx, audit.Event{
		EntityType: "milestone", Action: action, EntityID: m.ID, ProjectID: &m.ProjectID, ActorID: &actor.ID,
		Changes: changes, At: at,
	})
}

var errNotFound = kit.NotFoundError{Entity: "Milestone"}

func nameTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "milestones_name"
}

func takenErr() error { return kit.Input("name", kit.ReasonTaken, "a milestone with this name exists") }

func checkLead(ctx context.Context, tx *q.Queries, id *uuid.UUID) error {
	if id == nil {
		return nil
	}
	u, err := tx.GetUserByID(ctx, *id)
	if errors.Is(err, pgx.ErrNoRows) {
		return kit.Input("leadId", kit.ReasonNotFound, "no such user")
	}
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if u.Disabled {
		return kit.Input("leadId", kit.ReasonNotAllowed, "user is disabled")
	}
	return nil
}

// dateOnly keeps the calendar day of t at UTC midnight.
func dateOnly(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return &d
}

// lockProject locks a writable project; a missing one is reported on projectId.
func (s *Service) lockProject(ctx context.Context, tx *q.Queries, id uuid.UUID) error {
	_, err := s.projects.LockWritable(ctx, tx, id)
	if errors.Is(err, kit.ErrNotFound) {
		return kit.Input("projectId", kit.ReasonNotFound, "no such project")
	}
	return err
}

// lock locks the milestone's project (writable), then the milestone row.
func (s *Service) lock(ctx context.Context, tx *q.Queries, id uuid.UUID) (Milestone, error) {
	m, err := tx.GetMilestone(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Milestone{}, errNotFound
	}
	if err != nil {
		return Milestone{}, fmt.Errorf("get milestone: %w", err)
	}
	if _, err := s.projects.LockWritable(ctx, tx, m.ProjectID); err != nil {
		return Milestone{}, err
	}
	// Milestones never change project, so the row read under the project lock is current.
	m, err = tx.GetMilestoneForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Milestone{}, errNotFound
	}
	if err != nil {
		return Milestone{}, fmt.Errorf("lock milestone: %w", err)
	}
	return toMilestone(m), nil
}

// CreateInput is the new milestone form.
type CreateInput struct {
	Name, Description string
	TargetDate        *time.Time
	LeadID            *uuid.UUID
}

// Create adds a milestone at the bottom of the project's list.
func (s *Service) Create(ctx context.Context, actor account.User, projectID uuid.UUID, in CreateInput) (Milestone, error) {
	name, err := project.Text("name", in.Name, maxName)
	if err != nil {
		return Milestone{}, err
	}
	if err := project.Description("description", in.Description); err != nil {
		return Milestone{}, err
	}
	var out Milestone
	err = s.inTx(ctx, func(tx *q.Queries) error {
		if err := s.lockProject(ctx, tx, projectID); err != nil {
			return err
		}
		if err := checkLead(ctx, tx, in.LeadID); err != nil {
			return err
		}
		now := s.now()
		row, err := tx.CreateMilestone(ctx, q.CreateMilestoneParams{
			ProjectID: projectID, Name: name, Description: in.Description, TargetDate: dateOnly(in.TargetDate),
			LeadID: in.LeadID, CreatedBy: &actor.ID, Now: now,
		})
		if nameTaken(err) {
			return takenErr()
		}
		if err != nil {
			return fmt.Errorf("create milestone: %w", err)
		}
		out = toMilestone(row)
		return record(ctx, tx, actor, out, "create", audit.Diff(nil, snapshot(out)), now)
	})
	if err != nil {
		return Milestone{}, err
	}
	return out, nil
}

// Patch edits a milestone; Status set with a nil value returns to automatic.
type Patch struct {
	Name, Description *string
	TargetDate        kit.Opt[time.Time]
	LeadID            kit.Opt[uuid.UUID]
	Status            kit.Opt[Status]
}

// Update applies a patch; an unchanged milestone records nothing.
func (s *Service) Update(ctx context.Context, actor account.User, id uuid.UUID, p Patch) (Milestone, error) {
	var name *string
	if p.Name != nil {
		n, err := project.Text("name", *p.Name, maxName)
		if err != nil {
			return Milestone{}, err
		}
		name = &n
	}
	if p.Description != nil {
		if err := project.Description("description", *p.Description); err != nil {
			return Milestone{}, err
		}
	}
	if p.Status.Set && p.Status.Value != nil && !p.Status.Value.valid() {
		return Milestone{}, kit.Input("status", kit.ReasonInvalidFormat, "unknown status")
	}
	var out Milestone
	err := s.inTx(ctx, func(tx *q.Queries) error {
		cur, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		next := cur
		if name != nil {
			next.Name = *name
		}
		if p.Description != nil {
			next.Description = *p.Description
		}
		if p.TargetDate.Set {
			next.TargetDate = dateOnly(p.TargetDate.Value)
		}
		if p.LeadID.Set {
			if err := checkLead(ctx, tx, p.LeadID.Value); err != nil {
				return err
			}
			next.LeadID = p.LeadID.Value
		}
		if p.Status.Set {
			next.Manual = p.Status.Value
		}
		changes := audit.Diff(snapshot(cur), snapshot(next))
		if len(changes) == 0 {
			out = cur
			return nil
		}
		var manual *string
		if next.Manual != nil {
			v := string(*next.Manual)
			manual = &v
		}
		now := s.now()
		row, err := tx.UpdateMilestone(ctx, q.UpdateMilestoneParams{
			ID: id, Name: next.Name, Description: next.Description, TargetDate: next.TargetDate,
			LeadID: next.LeadID, StatusManual: manual, Now: now,
		})
		if nameTaken(err) {
			return takenErr()
		}
		if err != nil {
			return fmt.Errorf("update milestone: %w", err)
		}
		out = toMilestone(row)
		return record(ctx, tx, actor, out, "update", changes, now)
	})
	if err != nil {
		return Milestone{}, err
	}
	return out, nil
}

// Reorder sets the manual order; ids must be exactly the project's milestones.
func (s *Service) Reorder(ctx context.Context, actor account.User, projectID uuid.UUID, ids []uuid.UUID) error {
	_ = actor // any active user may reorder
	return s.inTx(ctx, func(tx *q.Queries) error {
		if err := s.lockProject(ctx, tx, projectID); err != nil {
			return err
		}
		rows, err := tx.ListProjectMilestones(ctx, projectID)
		if err != nil {
			return fmt.Errorf("list milestones: %w", err)
		}
		have := make([]uuid.UUID, 0, len(rows))
		for _, r := range rows {
			have = append(have, r.ID)
		}
		want := slices.Clone(ids)
		cmp := func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) }
		slices.SortFunc(have, cmp)
		slices.SortFunc(want, cmp)
		if !slices.Equal(have, want) {
			return kit.Input("milestoneIds", kit.ReasonNotAllowed, "list every milestone of the project exactly once")
		}
		for i, id := range ids {
			if err := tx.SetMilestoneSortOrder(ctx, q.SetMilestoneSortOrderParams{ID: id, SortOrder: float64((i + 1) * 1000)}); err != nil {
				return fmt.Errorf("set milestone order: %w", err)
			}
		}
		return nil
	})
}

// Delete removes a milestone (creator, Owner or Admin); its issues lose it.
func (s *Service) Delete(ctx context.Context, actor account.User, id uuid.UUID) error {
	return s.inTx(ctx, func(tx *q.Queries) error {
		cur, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if !actor.IsAdmin() && (cur.CreatedBy == nil || *cur.CreatedBy != actor.ID) {
			return kit.ErrForbidden
		}
		now := s.now()
		issues, err := tx.ClearIssuesMilestone(ctx, q.ClearIssuesMilestoneParams{MilestoneID: &id, Now: now})
		if err != nil {
			return fmt.Errorf("clear issues milestone: %w", err)
		}
		for _, issueID := range issues {
			if err := audit.Record(ctx, tx, audit.Event{
				EntityType: "issue", Action: "update", EntityID: issueID, ProjectID: &cur.ProjectID, ActorID: &actor.ID,
				Changes: map[string]audit.Change{"milestoneId": {From: id, To: nil}}, At: now,
			}); err != nil {
				return err
			}
		}
		if err := tx.DeleteMilestone(ctx, id); err != nil {
			return fmt.Errorf("delete milestone: %w", err)
		}
		return record(ctx, tx, actor, cur, "delete", nil, now)
	})
}

// Get finds a milestone by id.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Milestone, error) {
	m, err := s.q.GetMilestone(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Milestone{}, errNotFound
	}
	if err != nil {
		return Milestone{}, fmt.Errorf("get milestone: %w", err)
	}
	return toMilestone(m), nil
}

// ByIDs loads milestones in the order of ids; missing ones are omitted.
func (s *Service) ByIDs(ctx context.Context, ids []uuid.UUID) ([]Milestone, error) {
	rows, err := s.q.ListMilestonesByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("milestones by ids: %w", err)
	}
	byID := make(map[uuid.UUID]Milestone, len(rows))
	for _, r := range rows {
		byID[r.ID] = toMilestone(r)
	}
	out := make([]Milestone, 0, len(ids))
	for _, id := range ids {
		if m, ok := byID[id]; ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// List returns the project's milestones in manual order; without includeDone
// those whose status is done or canceled are left out.
func (s *Service) List(ctx context.Context, projectID uuid.UUID, includeDone bool) ([]Milestone, error) {
	rows, err := s.q.ListProjectMilestones(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list milestones: %w", err)
	}
	out := make([]Milestone, 0, len(rows))
	for _, r := range rows {
		out = append(out, toMilestone(r))
	}
	if includeDone {
		return out, nil
	}
	ids := make([]uuid.UUID, len(out))
	for i, m := range out {
		ids[i] = m.ID
	}
	prog, err := s.Progress(ctx, ids)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(out, func(m Milestone) bool {
		st := m.Status(prog[m.ID])
		return st == Done || st == Canceled
	}), nil
}

// Progress counts the live issues of each milestone in one query; milestones
// without issues get a zero Progress.
func (s *Service) Progress(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]Progress, error) {
	rows, err := s.q.MilestoneProgress(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("milestone progress: %w", err)
	}
	out := make(map[uuid.UUID]Progress, len(ids))
	for _, id := range ids {
		out[id] = Progress{}
	}
	for _, r := range rows {
		out[r.MilestoneID] = Progress{Completed: int(r.Completed), Total: int(r.Total), Active: int(r.Active)}
	}
	return out, nil
}
