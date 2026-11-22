package audit_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/comment"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/store/q"
	"github.com/askuksov/yalkos/internal/testfx"
)

// stored is the create row of an entity in audit_events.
type stored struct {
	entityType, action string
	projectID, actorID *uuid.UUID
	changes            any
	at                 time.Time
}

func storedCreate(t *testing.T, pool *pgxpool.Pool, entityID uuid.UUID) stored {
	t.Helper()
	var (
		s   stored
		raw []byte
	)
	err := pool.QueryRow(context.Background(), `
		SELECT entity_type, action, project_id, actor_id, changes, created_at
		FROM audit_events WHERE entity_id = $1 AND action = 'create'`, entityID,
	).Scan(&s.entityType, &s.action, &s.projectID, &s.actorID, &raw, &s.at)
	if err != nil {
		t.Fatalf("read create event of %s: %v", entityID, err)
	}
	if err := json.Unmarshal(raw, &s.changes); err != nil {
		t.Fatal(err)
	}
	return s
}

// sameAs fails unless e carries what the stored create row holds.
func (s stored) sameAs(t *testing.T, kind string, e audit.Event) {
	t.Helper()
	changes := e.Changes
	if changes == nil {
		changes = map[string]audit.Change{}
	}
	b, err := json.Marshal(changes)
	if err != nil {
		t.Fatal(err)
	}
	var got any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if e.EntityType != s.entityType || e.Action != s.action {
		t.Errorf("%s: type/action = %s/%s, stored %s/%s", kind, e.EntityType, e.Action, s.entityType, s.action)
	}
	if !reflect.DeepEqual(e.ProjectID, s.projectID) {
		t.Errorf("%s: project id = %v, stored %v", kind, e.ProjectID, s.projectID)
	}
	if !reflect.DeepEqual(e.ActorID, s.actorID) {
		t.Errorf("%s: actor id = %v, stored %v", kind, e.ActorID, s.actorID)
	}
	if !reflect.DeepEqual(got, s.changes) {
		t.Errorf("%s: changes = %v, stored %v", kind, got, s.changes)
	}
	if !e.At.Equal(s.at) {
		t.Errorf("%s: at = %v, stored %v", kind, e.At, s.at)
	}
}

func TestCreatedEventsMatchNativeCreate(t *testing.T) {
	ctx := context.Background()
	pool, users, clock := testfx.New(t)
	db := q.New(pool)
	actor := &users.Admin.ID

	projects := project.New(pool, clock)
	issues := issue.New(pool, projects, clock)
	labels := label.New(pool, clock)
	milestones := milestone.New(pool, projects, clock)
	comments := comment.New(pool, issues, nil, clock)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	p, err := projects.Create(ctx, users.Admin, project.CreateInput{Key: "ALP", Name: "Alpha"})
	must(err)
	row, err := db.GetProject(ctx, p.ID)
	must(err)
	// Golden: the native create stores the creator as the only member.
	if native := storedCreate(t, pool, p.ID); !reflect.DeepEqual(
		native.changes.(map[string]any)["members"], map[string]any{"from": nil, "to": []any{users.Admin.ID.String()}}) {
		t.Errorf("stored members = %v, want only the creator", native.changes.(map[string]any)["members"])
	}
	storedCreate(t, pool, p.ID).sameAs(t, "project", project.CreatedEvent(row, []uuid.UUID{users.Admin.ID}, actor))

	st, err := projects.CreateState(ctx, users.Admin, p.ID, project.StateInput{Name: "Review", Type: project.Started, Color: "#112233"})
	must(err)
	stRow, err := db.GetState(ctx, st.ID)
	must(err)
	storedCreate(t, pool, st.ID).sameAs(t, "state", project.StateCreatedEvent(stRow, actor))

	var labelIDs []uuid.UUID
	for _, name := range []string{"red", "blue"} {
		l, err := labels.Create(ctx, users.Admin, label.CreateInput{Name: name, Color: "#aabbcc", Description: "d"})
		must(err)
		lRow, err := db.GetLabel(ctx, l.ID)
		must(err)
		storedCreate(t, pool, l.ID).sameAs(t, "label", label.CreatedEvent(lRow, actor))
		labelIDs = append(labelIDs, l.ID)
	}

	lead := users.Member.ID
	target := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)
	m, err := milestones.Create(ctx, users.Admin, p.ID, milestone.CreateInput{Name: "v1", Description: "first", TargetDate: &target, LeadID: &lead})
	must(err)
	mRow, err := db.GetMilestone(ctx, m.ID)
	must(err)
	storedCreate(t, pool, m.ID).sameAs(t, "milestone", milestone.CreatedEvent(mRow, actor))

	due := time.Date(2030, 2, 3, 0, 0, 0, 0, time.UTC)
	i, err := issues.Create(ctx, users.Admin, issue.CreateInput{
		ProjectID: &p.ID, Title: "Task", Description: "text", Priority: 2, StateID: &st.ID, AssigneeID: &lead,
		MilestoneID: &m.ID, LabelIDs: labelIDs, DueDate: &due,
	})
	must(err)
	iRow, err := db.GetIssue(ctx, i.ID)
	must(err)
	storedCreate(t, pool, i.ID).sameAs(t, "issue", issue.CreatedEvent(iRow.Issue, p.Key, i.LabelIDs, actor))

	c, err := comments.Create(ctx, users.Admin, i.ID, "hello", nil)
	must(err)
	cRow, err := db.GetComment(ctx, c.ID)
	must(err)
	storedCreate(t, pool, c.ID).sameAs(t, "comment", comment.CreatedEvent(cRow, p.ID, actor))
}

func TestCreatedEventsProjectListsMembers(t *testing.T) {
	a, b, actor := uuid.New(), uuid.New(), uuid.New()
	row := q.Project{ID: uuid.New(), Key: "BET", Name: "Beta"}
	e := project.CreatedEvent(row, []uuid.UUID{a, b}, &actor)
	if got := e.Changes["members"].To; !reflect.DeepEqual(got, []uuid.UUID{a, b}) {
		t.Errorf("members = %v, want [%v %v]", got, a, b)
	}
	if got := project.CreatedEvent(row, nil, &actor).Changes["members"].To; !reflect.DeepEqual(got, []uuid.UUID{}) {
		t.Errorf("no members = %#v, want an empty slice", got)
	}
}
