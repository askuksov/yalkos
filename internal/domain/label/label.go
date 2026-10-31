// Package label implements workspace labels, label groups and the issue type
// (labels of the seeded Type group).
package label

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

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

const (
	maxName        = 50
	maxDescription = 200
)

// Label is a label or, with IsGroup, a group of labels (one level deep).
type Label struct {
	ID                       uuid.UUID
	ParentID                 *uuid.UUID
	IsGroup, Exclusive       bool
	Name, Description, Color string
	Position                 float64
}

// TypeInfo describes the issue type: the Type group, its labels and the
// default type for new issues (nil when none is chosen).
type TypeInfo struct {
	GroupID   uuid.UUID
	DefaultID *uuid.UUID
	LabelIDs  []uuid.UUID
}

// Service holds label rules.
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

// FromRow converts a stored label.
func FromRow(l q.Label) Label {
	return Label{
		ID: l.ID, ParentID: l.ParentID, IsGroup: l.IsGroup, Exclusive: l.Exclusive,
		Name: l.Name, Description: l.Description, Color: l.Color, Position: l.Position,
	}
}

func snapshot(l Label) map[string]any {
	return map[string]any{
		"name": l.Name, "color": l.Color, "description": l.Description,
		"parentId": l.ParentID, "exclusive": l.Exclusive, "position": l.Position,
	}
}

func record(ctx context.Context, tx *q.Queries, actor account.User, l Label, action string, changes map[string]audit.Change, at time.Time) error {
	return audit.Record(ctx, tx, audit.Event{
		EntityType: "label", Action: action, EntityID: l.ID, ActorID: &actor.ID, Changes: changes, At: at,
	})
}

var errNotFound = kit.NotFoundError{Entity: "Label"}

func nameTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "labels_name"
}

func takenErr() error {
	return kit.Input("name", kit.ReasonTaken, "a label with this name exists here")
}

func validDescription(v string) (string, error) {
	v = strings.TrimSpace(v)
	if utf8.RuneCountInString(v) > maxDescription {
		return "", kit.Input("description", kit.ReasonTooLong, "is too long").With("max", maxDescription)
	}
	return v, nil
}

func settings(ctx context.Context, db *q.Queries) (q.GetLabelSettingsRow, error) {
	st, err := db.GetLabelSettings(ctx)
	if err != nil {
		return q.GetLabelSettingsRow{}, fmt.Errorf("label settings: %w", err)
	}
	if st.TypeGroupID == nil {
		return q.GetLabelSettingsRow{}, errors.New("label: workspace has no type group")
	}
	return st, nil
}

// group reads a group for parentId, locking it so concurrent issue label
// writes (which share-lock labels and their groups) wait for this change.
func group(ctx context.Context, tx *q.Queries, id uuid.UUID) (q.Label, error) {
	g, err := tx.GetLabelForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return q.Label{}, kit.Input("parentId", kit.ReasonNotFound, "no such group")
	}
	if err != nil {
		return q.Label{}, fmt.Errorf("get group: %w", err)
	}
	if !g.IsGroup {
		return q.Label{}, kit.Input("parentId", kit.ReasonNotAllowed, "labels can only be placed in a group")
	}
	return g, nil
}

// CreateInput is the new label form. Exclusive applies to groups (default true).
type CreateInput struct {
	Name, Color, Description string
	ParentID                 *uuid.UUID
	IsGroup                  bool
	Exclusive                *bool
}

// Create adds a label at the bottom of its siblings. Members create free
// labels and labels in groups other than Type; groups and type labels need
// Owner or Admin.
func (s *Service) Create(ctx context.Context, actor account.User, in CreateInput) (Label, error) {
	name, err := project.Text("name", in.Name, maxName)
	if err != nil {
		return Label{}, err
	}
	color, err := project.Color("color", in.Color)
	if err != nil {
		return Label{}, err
	}
	desc, err := validDescription(in.Description)
	if err != nil {
		return Label{}, err
	}
	exclusive := false
	if in.IsGroup {
		if !actor.IsAdmin() {
			return Label{}, kit.ErrForbidden
		}
		if in.ParentID != nil {
			return Label{}, kit.Input("parentId", kit.ReasonNotAllowed, "groups cannot be nested")
		}
		exclusive = in.Exclusive == nil || *in.Exclusive
	} else if in.Exclusive != nil && *in.Exclusive {
		return Label{}, kit.Input("exclusive", kit.ReasonNotAllowed, "only groups can be exclusive")
	}
	var out Label
	err = s.inTx(ctx, func(tx *q.Queries) error {
		if in.ParentID != nil {
			if _, err := group(ctx, tx, *in.ParentID); err != nil {
				return err
			}
			st, err := settings(ctx, tx)
			if err != nil {
				return err
			}
			if *in.ParentID == *st.TypeGroupID && !actor.IsAdmin() {
				return kit.ErrForbidden
			}
		}
		pos, err := tx.NextLabelPosition(ctx, in.ParentID)
		if err != nil {
			return fmt.Errorf("label position: %w", err)
		}
		now := s.now()
		row, err := tx.CreateLabel(ctx, q.CreateLabelParams{
			ParentID: in.ParentID, IsGroup: in.IsGroup, Exclusive: exclusive, Name: name, Description: desc,
			Color: color, Position: pos, CreatedBy: &actor.ID, Now: now,
		})
		if nameTaken(err) {
			return takenErr()
		}
		if err != nil {
			return fmt.Errorf("create label: %w", err)
		}
		out = FromRow(row)
		return record(ctx, tx, actor, out, "create", audit.Diff(nil, snapshot(out)), now)
	})
	if err != nil {
		return Label{}, err
	}
	return out, nil
}

// List returns every group and label by position.
func (s *Service) List(ctx context.Context) ([]Label, error) {
	rows, err := s.q.ListLabels(ctx)
	if err != nil {
		return nil, fmt.Errorf("list labels: %w", err)
	}
	out := make([]Label, len(rows))
	for i, r := range rows {
		out[i] = FromRow(r)
	}
	return out, nil
}

// ByIDs loads labels in the order of ids; missing ones are omitted.
func (s *Service) ByIDs(ctx context.Context, ids []uuid.UUID) ([]Label, error) {
	rows, err := s.q.ListLabelsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("labels by ids: %w", err)
	}
	byID := make(map[uuid.UUID]Label, len(rows))
	for _, r := range rows {
		byID[r.ID] = FromRow(r)
	}
	out := make([]Label, 0, len(ids))
	for _, id := range ids {
		if l, ok := byID[id]; ok {
			out = append(out, l)
		}
	}
	return out, nil
}

// TypeInfo returns the type group, its labels and the default type.
func (s *Service) TypeInfo(ctx context.Context) (TypeInfo, error) {
	return ReadTypeInfo(ctx, s.q)
}

// ReadTypeInfo reads TypeInfo with db (a pool or a transaction).
func ReadTypeInfo(ctx context.Context, db *q.Queries) (TypeInfo, error) {
	st, err := settings(ctx, db)
	if err != nil {
		return TypeInfo{}, err
	}
	children, err := db.ListLabelChildren(ctx, st.TypeGroupID)
	if err != nil {
		return TypeInfo{}, fmt.Errorf("type labels: %w", err)
	}
	ti := TypeInfo{GroupID: *st.TypeGroupID, DefaultID: st.DefaultTypeLabelID, LabelIDs: make([]uuid.UUID, len(children))}
	for i, c := range children {
		ti.LabelIDs[i] = c.ID
	}
	return ti, nil
}

// SetDefaultType picks the type new issues get (nil: none). Owner/Admin only.
func (s *Service) SetDefaultType(ctx context.Context, actor account.User, labelID *uuid.UUID) error {
	if !actor.IsAdmin() {
		return kit.ErrForbidden
	}
	return s.inTx(ctx, func(tx *q.Queries) error {
		if labelID != nil {
			st, err := settings(ctx, tx)
			if err != nil {
				return err
			}
			l, err := tx.GetLabel(ctx, *labelID)
			if errors.Is(err, pgx.ErrNoRows) {
				return kit.Input("labelId", kit.ReasonNotFound, "no such label")
			}
			if err != nil {
				return fmt.Errorf("get label: %w", err)
			}
			if l.ParentID == nil || *l.ParentID != *st.TypeGroupID {
				return kit.Input("labelId", kit.ReasonNotAllowed, "the default must be a type")
			}
		}
		if err := tx.SetDefaultTypeLabel(ctx, labelID); err != nil {
			return fmt.Errorf("set default type: %w", err)
		}
		return nil
	})
}
