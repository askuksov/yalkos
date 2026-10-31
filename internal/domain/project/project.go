// Package project implements projects (issue containers with a key, states
// and members).
package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/store/q"
)

// Project is an issue container.
type Project struct {
	ID          uuid.UUID
	Key, Name   string
	Description string
	Icon, Color *string
	ArchivedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Service holds project, member and state rules.
type Service struct {
	pool *pgxpool.Pool
	q    *q.Queries
	now  func() time.Time
}

// New builds the service; now is the clock.
func New(pool *pgxpool.Pool, now func() time.Time) *Service {
	return &Service{pool: pool, q: q.New(pool), now: now}
}

func (s *Service) inTx(ctx context.Context, fn func(*q.Queries) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return fn(s.q.WithTx(tx)) })
}

func toProject(p q.Project) Project {
	return Project{
		ID: p.ID, Key: p.Key, Name: p.Name, Description: p.Description, Icon: p.Icon, Color: p.Color,
		ArchivedAt: p.ArchivedAt, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

// snapshot is the audited view of a project (API field names).
func snapshot(p q.Project) map[string]any {
	return map[string]any{
		"key": p.Key, "name": p.Name, "description": p.Description,
		"icon": p.Icon, "color": p.Color, "archivedAt": p.ArchivedAt,
	}
}

func archivedErr() error {
	return kit.Input("projectId", kit.ReasonArchived, "project is archived")
}

func keyTakenErr() error { return kit.Input("key", kit.ReasonTaken, "key is already used") }

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// LockWritable locks a project row for the rest of tx; a missing project is
// NotFound, an archived one is read-only.
func (s *Service) LockWritable(ctx context.Context, tx *q.Queries, id uuid.UUID) (q.Project, error) {
	p, err := lockProject(ctx, tx, id)
	if err != nil {
		return q.Project{}, err
	}
	if p.ArchivedAt != nil {
		return q.Project{}, archivedErr()
	}
	return p, nil
}

func lockProject(ctx context.Context, tx *q.Queries, id uuid.UUID) (q.Project, error) {
	p, err := tx.GetProjectForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return q.Project{}, kit.NotFoundError{Entity: "Project"}
	}
	if err != nil {
		return q.Project{}, fmt.Errorf("lock project: %w", err)
	}
	return p, nil
}

// claimKey records key as one of project id's keys; a key held by another
// project (now or before) is taken.
func claimKey(ctx context.Context, tx *q.Queries, key string, id uuid.UUID) error {
	if err := tx.InsertProjectKey(ctx, q.InsertProjectKeyParams{Key: key, ProjectID: id}); err != nil {
		return fmt.Errorf("insert project key: %w", err)
	}
	owner, err := tx.GetProjectKeyOwner(ctx, key)
	if err != nil {
		return fmt.Errorf("project key owner: %w", err)
	}
	if owner != id {
		return keyTakenErr()
	}
	return nil
}

// CreateInput is the new project form.
type CreateInput struct {
	Key, Name, Description string
	Icon, Color            *string
}

// Create makes a project with default states; the creator becomes a member.
func (s *Service) Create(ctx context.Context, actor account.User, in CreateInput) (Project, error) {
	if !actor.IsAdmin() {
		return Project{}, kit.ErrForbidden
	}
	key, err := NormalizeKey("key", in.Key)
	if err != nil {
		return Project{}, err
	}
	name, err := Text("name", in.Name, maxName)
	if err != nil {
		return Project{}, err
	}
	if err := Description("description", in.Description); err != nil {
		return Project{}, err
	}
	icon, err := optIcon(in.Icon)
	if err != nil {
		return Project{}, err
	}
	color, err := optColor(in.Color)
	if err != nil {
		return Project{}, err
	}
	now := s.now()
	var out q.Project
	err = s.inTx(ctx, func(tx *q.Queries) error {
		p, err := tx.CreateProject(ctx, q.CreateProjectParams{
			Key: key, Name: name, Description: in.Description, Icon: icon, Color: color,
			CreatedBy: &actor.ID, Now: now,
		})
		if isUniqueViolation(err) {
			return keyTakenErr()
		}
		if err != nil {
			return fmt.Errorf("create project: %w", err)
		}
		if err := claimKey(ctx, tx, key, p.ID); err != nil {
			return err
		}
		if err := insertDefaultStates(ctx, tx, p.ID, now); err != nil {
			return err
		}
		if err := tx.AddProjectMember(ctx, q.AddProjectMemberParams{ProjectID: p.ID, UserID: actor.ID, Now: now}); err != nil {
			return fmt.Errorf("add creator: %w", err)
		}
		snap := snapshot(p)
		snap["members"] = []uuid.UUID{actor.ID}
		out = p
		return audit.Record(ctx, tx, audit.Event{
			EntityType: "project", Action: "create", EntityID: p.ID, ProjectID: &p.ID, ActorID: &actor.ID,
			Changes: audit.Diff(nil, snap), At: now,
		})
	})
	if err != nil {
		return Project{}, err
	}
	return toProject(out), nil
}

// UpdateInput patches a project: nil pointers keep values, Opt clears icon/color.
type UpdateInput struct {
	Key, Name, Description *string
	Icon, Color            kit.Opt[string]
}

// Update edits a project; a key change keeps the former key resolving.
func (s *Service) Update(ctx context.Context, actor account.User, id uuid.UUID, in UpdateInput) (Project, error) {
	if !actor.IsAdmin() {
		return Project{}, kit.ErrForbidden
	}
	var key, name *string
	if in.Key != nil {
		k, err := NormalizeKey("key", *in.Key)
		if err != nil {
			return Project{}, err
		}
		key = &k
	}
	if in.Name != nil {
		n, err := Text("name", *in.Name, maxName)
		if err != nil {
			return Project{}, err
		}
		name = &n
	}
	if in.Description != nil {
		if err := Description("description", *in.Description); err != nil {
			return Project{}, err
		}
	}
	icon, err := optIcon(in.Icon.Value)
	if err != nil {
		return Project{}, err
	}
	color, err := optColor(in.Color.Value)
	if err != nil {
		return Project{}, err
	}
	var out q.Project
	err = s.inTx(ctx, func(tx *q.Queries) error {
		p, err := s.LockWritable(ctx, tx, id)
		if err != nil {
			return err
		}
		next := q.UpdateProjectParams{ID: p.ID, Key: p.Key, Name: p.Name, Description: p.Description, Icon: p.Icon, Color: p.Color}
		if key != nil {
			next.Key = *key
		}
		if name != nil {
			next.Name = *name
		}
		if in.Description != nil {
			next.Description = *in.Description
		}
		if in.Icon.Set {
			next.Icon = icon
		}
		if in.Color.Set {
			next.Color = color
		}
		after := p
		after.Key, after.Name, after.Description, after.Icon, after.Color = next.Key, next.Name, next.Description, next.Icon, next.Color
		changes := audit.Diff(snapshot(p), snapshot(after))
		if len(changes) == 0 {
			out = p
			return nil
		}
		now := s.now()
		next.Now = now
		// projects.key first, then project_keys: the same lock order as Create,
		// so a key change racing a create of that key cannot deadlock.
		out, err = tx.UpdateProject(ctx, next)
		if isUniqueViolation(err) {
			return keyTakenErr()
		}
		if err != nil {
			return fmt.Errorf("update project: %w", err)
		}
		if next.Key != p.Key {
			if err := claimKey(ctx, tx, next.Key, p.ID); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Event{
			EntityType: "project", Action: "update", EntityID: p.ID, ProjectID: &p.ID, ActorID: &actor.ID,
			Changes: changes, At: now,
		})
	})
	if err != nil {
		return Project{}, err
	}
	return toProject(out), nil
}

// SetArchived archives (read-only, hidden from lists) or unarchives a project.
func (s *Service) SetArchived(ctx context.Context, actor account.User, id uuid.UUID, archived bool) (Project, error) {
	if !actor.IsAdmin() {
		return Project{}, kit.ErrForbidden
	}
	var out q.Project
	err := s.inTx(ctx, func(tx *q.Queries) error {
		p, err := lockProject(ctx, tx, id)
		if err != nil {
			return err
		}
		if (p.ArchivedAt != nil) == archived {
			out = p
			return nil
		}
		now := s.now()
		var at *time.Time
		if archived {
			at = &now
		}
		out, err = tx.SetProjectArchived(ctx, q.SetProjectArchivedParams{ID: id, ArchivedAt: at, Now: now})
		if err != nil {
			return fmt.Errorf("archive project: %w", err)
		}
		return audit.Record(ctx, tx, audit.Event{
			EntityType: "project", Action: "update", EntityID: id, ProjectID: &id, ActorID: &actor.ID,
			Changes: audit.Diff(snapshot(p), snapshot(out)), At: now,
		})
	})
	if err != nil {
		return Project{}, err
	}
	return toProject(out), nil
}

// Delete removes a project with its states, issues and their history.
// Owner only; confirmKey must be the current key.
func (s *Service) Delete(ctx context.Context, actor account.User, id uuid.UUID, confirmKey string) error {
	if actor.Role != account.RoleOwner {
		return kit.ErrForbidden
	}
	return s.inTx(ctx, func(tx *q.Queries) error {
		p, err := lockProject(ctx, tx, id)
		if err != nil {
			return err
		}
		if strings.ToUpper(strings.TrimSpace(confirmKey)) != p.Key {
			return kit.Input("confirmKey", kit.ReasonMismatch, "type the project key to confirm")
		}
		if err := audit.DeleteProject(ctx, tx, id); err != nil {
			return err
		}
		if err := tx.DeleteProject(ctx, id); err != nil {
			return fmt.Errorf("delete project: %w", err)
		}
		return audit.Record(ctx, tx, audit.Event{
			EntityType: "project", Action: "delete", EntityID: id, ActorID: &actor.ID,
			Changes: map[string]audit.Change{"key": {From: p.Key}, "name": {From: p.Name}}, At: s.now(),
		})
	})
}

var refKeyRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,6}$`)

// Get finds a project by UUID, current key or former key (case-insensitive).
func (s *Service) Get(ctx context.Context, ref string) (Project, error) {
	ref = strings.TrimSpace(ref)
	var p q.Project
	var err error
	if id, perr := uuid.Parse(ref); perr == nil {
		p, err = s.q.GetProject(ctx, id)
	} else if refKeyRe.MatchString(ref) {
		p, err = s.q.GetProjectByKey(ctx, strings.ToUpper(ref))
	} else {
		return Project{}, kit.NotFoundError{Entity: "Project"}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, kit.NotFoundError{Entity: "Project"}
	}
	if err != nil {
		return Project{}, fmt.Errorf("get project: %w", err)
	}
	return toProject(p), nil
}

// ByIDs loads projects in the order of ids; missing ones are omitted.
func (s *Service) ByIDs(ctx context.Context, ids []uuid.UUID) ([]Project, error) {
	rows, err := s.q.ListProjectsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("projects by ids: %w", err)
	}
	byID := make(map[uuid.UUID]q.Project, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	out := make([]Project, 0, len(ids))
	for _, id := range ids {
		if p, ok := byID[id]; ok {
			out = append(out, toProject(p))
		}
	}
	return out, nil
}

const listField = "name"

type listed struct {
	p    q.Project
	sort string
}

// List returns projects ordered by lower(name), id; archived only when asked.
func (s *Service) List(ctx context.Context, includeArchived bool, a page.Args) (page.Result[Project], error) {
	w, err := a.Window()
	if err != nil {
		return page.Result[Project]{}, err
	}
	var afterName *string
	var afterID *uuid.UUID
	if w.Cursor != nil {
		field := "after"
		if !w.Forward {
			field = "before"
		}
		c, err := page.Decode(*w.Cursor, field)
		if err != nil {
			return page.Result[Project]{}, err
		}
		var v string
		if c.F != listField || c.D != "asc" || json.Unmarshal(c.V, &v) != nil {
			return page.Result[Project]{}, kit.Input(field, kit.ReasonInvalidFormat, "invalid cursor")
		}
		afterName, afterID = &v, &c.ID
	}
	var rows []listed
	lim := int32(w.Limit + 1)
	if w.Forward {
		r, err := s.q.ListProjectsForward(ctx, q.ListProjectsForwardParams{IncludeArchived: includeArchived, AfterName: afterName, AfterID: afterID, Lim: lim})
		if err != nil {
			return page.Result[Project]{}, fmt.Errorf("list projects: %w", err)
		}
		for _, x := range r {
			rows = append(rows, listed{x.Project, x.SortName})
		}
	} else {
		r, err := s.q.ListProjectsBackward(ctx, q.ListProjectsBackwardParams{IncludeArchived: includeArchived, BeforeName: afterName, BeforeID: afterID, Lim: lim})
		if err != nil {
			return page.Result[Project]{}, fmt.Errorf("list projects: %w", err)
		}
		for _, x := range r {
			rows = append(rows, listed{x.Project, x.SortName})
		}
	}
	res := page.Finish(rows, w, func(l listed) string {
		v, _ := json.Marshal(l.sort)
		return page.Encode(page.Cursor{F: listField, D: "asc", V: v, ID: l.p.ID})
	})
	out := page.Result[Project]{Cursors: res.Cursors, Info: res.Info, Items: make([]Project, len(res.Items))}
	for i, l := range res.Items {
		out.Items[i] = toProject(l.p)
	}
	return out, nil
}

// defaultStates are created with every project.
var defaultStates = []struct {
	name, typ, color string
	isDefault        bool
}{
	{"Backlog", "backlog", "#bec2c8", false},
	{"Todo", "unstarted", "#e2e2e2", true},
	{"In Progress", "started", "#f2c94c", false},
	{"Done", "completed", "#5e6ad2", false},
	{"Canceled", "canceled", "#95a2b3", false},
}

func insertDefaultStates(ctx context.Context, tx *q.Queries, projectID uuid.UUID, now time.Time) error {
	for i, d := range defaultStates {
		if _, err := tx.CreateState(ctx, q.CreateStateParams{
			ProjectID: projectID, Name: d.name, Type: d.typ, Color: d.color,
			Position: int32(i), IsDefault: d.isDefault, Now: now,
		}); err != nil {
			return fmt.Errorf("create default state: %w", err)
		}
	}
	return nil
}
