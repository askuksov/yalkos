package native

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/domain/project"
)

// Project is the GraphQL Project; states and members resolve lazily.
type Project struct {
	ID          string
	Key, Name   string
	Description string
	Icon, Color *string
	ArchivedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time

	uid uuid.UUID
}

// State is the GraphQL State; project resolves through the loader.
type State struct {
	ID        string
	Name      string
	Type      StateType
	Color     string
	Position  int
	IsDefault bool

	projectID uuid.UUID
}

func projectOut(p project.Project) *Project {
	return &Project{
		ID: p.ID.String(), Key: p.Key, Name: p.Name, Description: p.Description, Icon: p.Icon, Color: p.Color,
		ArchivedAt: p.ArchivedAt, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, uid: p.ID,
	}
}

func stateOut(s project.State) *State {
	return &State{
		ID: s.ID.String(), Name: s.Name, Type: stateTypeOut(s.Type), Color: s.Color,
		Position: s.Position, IsDefault: s.IsDefault, projectID: s.ProjectID,
	}
}

func statesOut(in []project.State) []State {
	out := make([]State, 0, len(in))
	for _, s := range in {
		out = append(out, *stateOut(s))
	}
	return out
}

func stateTypeOut(t project.StateType) StateType { return StateType(strings.ToUpper(string(t))) }
func stateTypeIn(t StateType) project.StateType  { return project.StateType(strings.ToLower(string(t))) }

func pageArgs(first *int, after *string, last *int, before *string) page.Args {
	return page.Args{First: first, After: after, Last: last, Before: before}
}

func pageInfoOut(i page.Info) *PageInfo {
	return &PageInfo{HasNextPage: i.HasNext, HasPreviousPage: i.HasPrev, StartCursor: i.Start, EndCursor: i.End}
}

// notNull reads a nullable input field that may be omitted but not set to null.
func notNull[T any](field string, o graphql.Omittable[*T]) (*T, error) {
	v, set := o.ValueOK()
	if set && v == nil {
		return nil, kit.Input(field, kit.ReasonRequired, "cannot be null")
	}
	return v, nil
}

// optOf turns an omittable input field into a patch field (explicit null clears).
func optOf[T any](o graphql.Omittable[*T]) kit.Opt[T] {
	v, set := o.ValueOK()
	return kit.Opt[T]{Set: set, Value: v}
}

func parseIDs(field string, ids []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(ids))
	for _, s := range ids {
		id, err := parseID(field, s)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func parseOptID(field string, s *string) (*uuid.UUID, error) {
	if s == nil {
		return nil, nil
	}
	id, err := parseID(field, *s)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// Issue is the GraphQL Issue; related entities resolve through loaders.
type Issue struct {
	ID          string
	Identifier  string
	Number      int
	Title       string
	Description string
	Priority    int
	DueDate     *Date
	SortOrder   float64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt *time.Time
	CanceledAt  *time.Time

	uid, projectID, stateID, creatorID uuid.UUID
	assigneeID, parentID               *uuid.UUID
}

// HistoryEntry is one audit event; actor resolves through the user loader.
type HistoryEntry struct {
	ID        string
	Action    string
	Changes   []FieldChange
	CreatedAt time.Time

	actorID *uuid.UUID
}

func issueOut(i issue.Issue) *Issue {
	out := &Issue{
		ID: i.ID.String(), Identifier: i.Identifier, Number: i.Number, Title: i.Title, Description: i.Description,
		Priority: i.Priority, SortOrder: i.SortOrder, CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt,
		CompletedAt: i.CompletedAt, CanceledAt: i.CanceledAt,
		uid: i.ID, projectID: i.ProjectID, stateID: i.StateID, creatorID: i.CreatorID,
		assigneeID: i.AssigneeID, parentID: i.ParentID,
	}
	if i.DueDate != nil {
		d := Date(*i.DueDate)
		out.DueDate = &d
	}
	return out
}

func issueConnOut(res page.Result[issue.Issue]) *IssueConnection {
	conn := &IssueConnection{
		Nodes:    make([]Issue, 0, len(res.Items)),
		Edges:    make([]IssueEdge, 0, len(res.Items)),
		PageInfo: pageInfoOut(res.Info),
	}
	for n, i := range res.Items {
		node := issueOut(i)
		conn.Nodes = append(conn.Nodes, *node)
		conn.Edges = append(conn.Edges, IssueEdge{Node: node, Cursor: res.Cursors[n]})
	}
	return conn
}

// jsonText returns a stored JSON value as text; absent or JSON null is nil.
func jsonText(raw json.RawMessage) *string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	s := string(raw)
	return &s
}

func historyOut(res page.Result[audit.Entry]) *HistoryConnection {
	conn := &HistoryConnection{
		Nodes:    make([]HistoryEntry, 0, len(res.Items)),
		Edges:    make([]HistoryEdge, 0, len(res.Items)),
		PageInfo: pageInfoOut(res.Info),
	}
	for n, e := range res.Items {
		changes := make([]FieldChange, 0, len(e.Changes))
		for _, c := range e.Changes {
			changes = append(changes, FieldChange{Field: c.Field, From: jsonText(c.From), To: jsonText(c.To)})
		}
		node := &HistoryEntry{ID: e.ID.String(), Action: e.Action, Changes: changes, CreatedAt: e.At, actorID: e.ActorID}
		conn.Nodes = append(conn.Nodes, *node)
		conn.Edges = append(conn.Edges, HistoryEdge{Node: node, Cursor: res.Cursors[n]})
	}
	return conn
}
