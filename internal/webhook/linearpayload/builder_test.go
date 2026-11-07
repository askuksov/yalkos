package linearpayload_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/domain/comment"
	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/domain/webhook"
	"github.com/askuksov/yalkos/internal/store/q"
	"github.com/askuksov/yalkos/internal/testfx"
	"github.com/askuksov/yalkos/internal/webhook/linearpayload"
)

const base = "http://yalkos.test"

func ptr[T any](v T) *T { return &v }

// captured is a change together with the payload built for it.
type captured struct {
	Change event.Change
	Built  webhook.Built
}

// capture is an emitter that builds the payload of every change in the
// emitting transaction, the way the webhook service does, and keeps it.
type capture struct {
	builder *linearpayload.Builder
	got     []captured
}

func (c *capture) Emit(ctx context.Context, tx pgx.Tx, ch event.Change) error {
	built, err := c.builder.Build(ctx, q.New(tx), ch)
	if err != nil {
		return fmt.Errorf("build %s %s: %w", ch.Resource, ch.Action, err)
	}
	c.got = append(c.got, captured{Change: ch, Built: built})
	return nil
}

type env struct {
	pool       *pgxpool.Pool
	users      testfx.Users
	now        time.Time
	projects   *project.Service
	issues     *issue.Service
	labels     *label.Service
	milestones *milestone.Service
	comments   *comment.Service
	cap        *capture
	ylk        project.Project
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool, users, _ := testfx.New(t)
	e := &env{pool: pool, users: users, cap: &capture{builder: linearpayload.New(base)}}
	e.at("2026-11-01T10:00:00.000000Z")
	clock := func() time.Time { return e.now }
	e.projects = project.New(pool, clock)
	e.issues = issue.New(pool, e.projects, clock)
	e.labels = label.New(pool, clock)
	e.milestones = milestone.New(pool, e.projects, clock)
	e.comments = comment.New(pool, e.issues, nil, clock)
	e.ylk = e.project(t, "YLK", "Yalkos")
	return e
}

// noDefaultType stops new issues from getting the workspace's default type
// label, so that the labels of an issue are the ones a test gives it.
func (e *env) noDefaultType(t *testing.T) {
	t.Helper()
	if err := e.labels.SetDefaultType(context.Background(), e.users.Admin, nil); err != nil {
		t.Fatal(err)
	}
}

// at sets the clock of every service.
func (e *env) at(ts string) {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		panic(err)
	}
	e.now = t
}

// watch starts building payloads for every change made from now on; what the
// test set up before emitted nothing.
func (e *env) watch() {
	e.projects.SetEmitter(e.cap)
	e.issues.SetEmitter(e.cap)
	e.labels.SetEmitter(e.cap)
	e.milestones.SetEmitter(e.cap)
	e.comments.SetEmitter(e.cap)
}

func (e *env) project(t *testing.T, key, name string) project.Project {
	t.Helper()
	p, err := e.projects.Create(context.Background(), e.users.Admin, project.CreateInput{Key: key, Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (e *env) state(t *testing.T, projectID uuid.UUID, name string) project.State {
	t.Helper()
	states, err := e.projects.States(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no state %q", name)
	return project.State{}
}

func (e *env) issue(t *testing.T, in issue.CreateInput) issue.Issue {
	t.Helper()
	if in.ProjectID == nil && in.ParentID == nil {
		in.ProjectID = &e.ylk.ID
	}
	i, err := e.issues.Create(context.Background(), e.users.Admin, in)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func (e *env) label(t *testing.T, in label.CreateInput) label.Label {
	t.Helper()
	l, err := e.labels.Create(context.Background(), e.users.Admin, in)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func (e *env) milestone(t *testing.T, name string, in milestone.CreateInput) milestone.Milestone {
	t.Helper()
	in.Name = name
	m, err := e.milestones.Create(context.Background(), e.users.Member, e.ylk.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// only returns the single change captured.
func (e *env) only(t *testing.T) captured {
	t.Helper()
	if len(e.cap.got) != 1 {
		t.Fatalf("captured %d changes, want 1: %v", len(e.cap.got), e.lines())
	}
	return e.cap.got[0]
}

func (e *env) lines() []string {
	var out []string
	for _, c := range e.cap.got {
		out = append(out, fmt.Sprintf("%s %s %s", c.Change.Resource, c.Change.Action, c.Change.EntityID))
	}
	return out
}

func (e *env) reset() { e.cap.got = nil }

// obj decodes a JSON object.
func obj(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("not a JSON object: %s: %v", raw, err)
	}
	return m
}

func sub(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("%q = %v, want an object", key, m[key])
	}
	return v
}

// canon re-encodes JSON with sorted keys; null and [] stay different.
func canon(t *testing.T, raw string) string {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("bad JSON %q: %v", raw, err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// wantJSON compares a payload part with the JSON text want.
func wantJSON(t *testing.T, what string, got json.RawMessage, want string) {
	t.Helper()
	if g, w := canon(t, string(got)), canon(t, want); g != w {
		t.Errorf("%s =\n%s\nwant\n%s", what, g, w)
	}
}

// wantRaw checks the raw bytes of a payload part for a fragment.
func wantRaw(t *testing.T, raw json.RawMessage, fragments ...string) {
	t.Helper()
	for _, f := range fragments {
		if !bytes.Contains(raw, []byte(f)) {
			t.Errorf("raw payload lacks %s:\n%s", f, raw)
		}
	}
}

func wantNoRaw(t *testing.T, raw json.RawMessage, fragments ...string) {
	t.Helper()
	for _, f := range fragments {
		if bytes.Contains(raw, []byte(f)) {
			t.Errorf("raw payload has %s:\n%s", f, raw)
		}
	}
}

func js(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func sortedIDs(ids ...uuid.UUID) []uuid.UUID {
	out := slices.Clone(ids)
	slices.SortFunc(out, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	return out
}

func TestBuildIssue(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.noDefaultType(t)
	area := e.label(t, label.CreateInput{Name: "Area", Color: "#111111", IsGroup: true})
	feature := e.label(t, label.CreateInput{Name: "Feature", Color: "#3b82f6", ParentID: &area.ID})
	bug := e.label(t, label.CreateInput{Name: "bug", Color: "#ef4444"})
	beta := e.milestone(t, "Beta", milestone.CreateInput{})
	inProgress := e.state(t, e.ylk.ID, "In Progress")
	e.watch()

	e.at("2026-11-04T18:23:05.123999Z")
	due := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	i, err := e.issues.Create(ctx, e.users.Owner, issue.CreateInput{
		ProjectID: &e.ylk.ID, Title: "Export to CSV", Priority: 2, StateID: &inProgress.ID,
		AssigneeID: &e.users.Member.ID, MilestoneID: &beta.ID, DueDate: &due,
		LabelIDs: []uuid.UUID{bug.ID, feature.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	c := e.only(t)
	b := c.Built
	if b.Type != "Issue" || b.URL == nil || *b.URL != base+"/issue/YLK-1" || b.UpdatedFrom != nil {
		t.Fatalf("built = type %q url %v updatedFrom %s", b.Type, b.URL, b.UpdatedFrom)
	}
	d := obj(t, b.Data)
	for k, want := range map[string]any{
		"id": i.ID.String(), "identifier": "YLK-1", "number": float64(1), "title": "Export to CSV",
		"priority": float64(2), "priorityLabel": "High", "url": base + "/issue/YLK-1",
		"stateId": inProgress.ID.String(), "teamId": e.ylk.ID.String(),
		"assigneeId": e.users.Member.ID.String(), "creatorId": e.users.Owner.ID.String(),
		"projectId": beta.ID.String(), "parentId": nil, "description": nil,
		"dueDate": "2026-11-20", "createdAt": "2026-11-04T18:23:05.123Z", "updatedAt": "2026-11-04T18:23:05.123Z",
		"completedAt": nil, "canceledAt": nil, "trashed": false, "subIssueSortOrder": nil,
		"sortOrder": i.SortOrder, "prioritySortOrder": i.SortOrder,
	} {
		if !reflect.DeepEqual(d[k], want) {
			t.Errorf("data.%s = %#v, want %#v", k, d[k], want)
		}
	}
	wantJSON(t, "data.state", mustRaw(t, d["state"]), js(map[string]any{
		"id": inProgress.ID, "name": "In Progress", "type": "started", "color": "#f2c94c",
	}))
	wantJSON(t, "data.team", mustRaw(t, d["team"]), js(map[string]any{"id": e.ylk.ID, "key": "YLK", "name": "Yalkos"}))
	owner, member := e.users.Owner, e.users.Member
	wantJSON(t, "data.creator", mustRaw(t, d["creator"]), js(map[string]any{
		"id": owner.ID, "name": owner.Name, "email": owner.Email, "url": base, "avatarUrl": nil,
	}))
	wantJSON(t, "data.assignee", mustRaw(t, d["assignee"]), js(map[string]any{
		"id": member.ID, "name": member.Name, "email": member.Email, "url": base, "avatarUrl": nil,
	}))
	wantJSON(t, "data.project", mustRaw(t, d["project"]), js(map[string]any{
		"id": beta.ID, "name": "Beta", "url": base + "/p/YLK/milestones/" + beta.ID.String(),
	}))
	ids := sortedIDs(bug.ID, feature.ID)
	if !reflect.DeepEqual(d["labelIds"], toAny(ids)) {
		t.Errorf("data.labelIds = %v, want %v", d["labelIds"], ids)
	}
	byID := map[uuid.UUID]map[string]any{
		bug.ID:     {"id": bug.ID, "name": "bug", "color": "#ef4444", "parentId": nil},
		feature.ID: {"id": feature.ID, "name": "Feature", "color": "#3b82f6", "parentId": area.ID},
	}
	var wantLabels []any
	for _, id := range ids {
		wantLabels = append(wantLabels, byID[id])
	}
	wantJSON(t, "data.labels", mustRaw(t, d["labels"]), js(wantLabels))
	wantRaw(t, b.Data, `"description":null`, `"avatarUrl":null`, `"completedAt":null`, `"trashed":false`,
		`"previousIdentifiers":[]`, `"subscriberIds":[]`, `"releases":[]`, `"reactionData":[]`)

	// A description stays text, a sub-issue carries its sort order, and the
	// bare string "" is the only description that becomes null.
	e.reset()
	e.at("2026-11-04T19:00:00.000Z")
	sub1 := e.issue(t, issue.CreateInput{ParentID: &i.ID, Title: "child", Description: "details"})
	c = e.only(t)
	d = obj(t, c.Built.Data)
	if d["description"] != "details" || d["parentId"] != i.ID.String() || d["subIssueSortOrder"] != sub1.SortOrder || d["identifier"] != "YLK-2" {
		t.Errorf("sub-issue data = description %v parentId %v subIssueSortOrder %v identifier %v",
			d["description"], d["parentId"], d["subIssueSortOrder"], d["identifier"])
	}
	wantRaw(t, c.Built.Data, `"projectId":`+js(beta.ID.String())) // inherited from the parent
}

func mustRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func toAny(ids []uuid.UUID) []any {
	out := make([]any, len(ids))
	for n, id := range ids {
		out[n] = id.String()
	}
	return out
}

func TestBuildIssueUpdatedFrom(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.noDefaultType(t)
	beta := e.milestone(t, "Beta", milestone.CreateInput{})
	rc := e.milestone(t, "RC", milestone.CreateInput{})
	bug := e.label(t, label.CreateInput{Name: "bug", Color: "#ef4444"})
	chore := e.label(t, label.CreateInput{Name: "chore", Color: "#999999"})
	done := e.state(t, e.ylk.ID, "Done")
	todo := e.state(t, e.ylk.ID, "Todo")
	parent := e.issue(t, issue.CreateInput{Title: "parent"})
	e.at("2026-11-02T08:00:00.250Z")
	i := e.issue(t, issue.CreateInput{Title: "one"})
	e.watch()

	update := func(at string, p issue.Patch) captured {
		t.Helper()
		e.reset()
		e.at(at)
		if _, err := e.issues.Update(ctx, e.users.Member, i.ID, p); err != nil {
			t.Fatal(err)
		}
		c := e.only(t)
		if c.Change.Action != event.Update || c.Built.Type != "Issue" {
			t.Fatalf("change = %+v", c.Change)
		}
		return c
	}

	// State to a completed one: the state, and the completion time it sets.
	c := update("2026-11-03T09:12:44.501999Z", issue.Patch{StateID: &done.ID})
	wantJSON(t, "updatedFrom", c.Built.UpdatedFrom, js(map[string]any{
		"updatedAt": "2026-11-02T08:00:00.250Z", "stateId": todo.ID, "completedAt": nil,
	}))
	wantRaw(t, c.Built.UpdatedFrom, `"completedAt":null`)
	d := obj(t, c.Built.Data)
	if d["completedAt"] != "2026-11-03T09:12:44.501Z" || sub(t, d, "state")["type"] != "completed" {
		t.Errorf("data = completedAt %v state %v", d["completedAt"], d["state"])
	}

	// Back to an open state: the previous completion time comes back.
	c = update("2026-11-03T10:00:00.000Z", issue.Patch{StateID: &todo.ID})
	wantJSON(t, "updatedFrom", c.Built.UpdatedFrom, js(map[string]any{
		"updatedAt": "2026-11-03T09:12:44.501Z", "stateId": done.ID, "completedAt": "2026-11-03T09:12:44.501Z",
	}))

	// Milestone: the previous one is the project id (null when there was none).
	c = update("2026-11-03T11:00:00.000Z", issue.Patch{MilestoneID: kit.Opt[uuid.UUID]{Set: true, Value: &beta.ID}})
	wantJSON(t, "updatedFrom", c.Built.UpdatedFrom, js(map[string]any{"updatedAt": "2026-11-03T10:00:00.000Z", "projectId": nil}))
	wantRaw(t, c.Built.UpdatedFrom, `"projectId":null`)
	c = update("2026-11-03T12:00:00.000Z", issue.Patch{MilestoneID: kit.Opt[uuid.UUID]{Set: true, Value: &rc.ID}})
	wantJSON(t, "updatedFrom", c.Built.UpdatedFrom, js(map[string]any{"updatedAt": "2026-11-03T11:00:00.000Z", "projectId": beta.ID}))

	// Labels: the previous array, [] and not null when there was none.
	c = update("2026-11-03T13:00:00.000Z", issue.Patch{AddLabelIDs: []uuid.UUID{bug.ID}})
	wantJSON(t, "updatedFrom", c.Built.UpdatedFrom, js(map[string]any{"updatedAt": "2026-11-03T12:00:00.000Z", "labelIds": []string{}}))
	wantRaw(t, c.Built.UpdatedFrom, `"labelIds":[]`)
	wantRaw(t, c.Built.Data, `"labelIds":[`+js(bug.ID.String())+`]`)
	c = update("2026-11-03T14:00:00.000Z", issue.Patch{AddLabelIDs: []uuid.UUID{chore.ID}})
	wantJSON(t, "updatedFrom", c.Built.UpdatedFrom, js(map[string]any{
		"updatedAt": "2026-11-03T13:00:00.000Z", "labelIds": []uuid.UUID{bug.ID},
	}))

	// Scalars, each with its previous value; description "" is null.
	due := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	c = update("2026-11-03T15:00:00.000Z", issue.Patch{
		Title: ptr("renamed"), Description: ptr("now text"), Priority: ptr(3),
		AssigneeID: kit.Opt[uuid.UUID]{Set: true, Value: &e.users.Owner.ID},
		DueDate:    kit.Opt[time.Time]{Set: true, Value: &due},
		ParentID:   kit.Opt[uuid.UUID]{Set: true, Value: &parent.ID},
	})
	wantJSON(t, "updatedFrom", c.Built.UpdatedFrom, js(map[string]any{
		"updatedAt": "2026-11-03T14:00:00.000Z", "title": "one", "description": nil, "priority": 0,
		"assigneeId": nil, "dueDate": nil, "parentId": nil,
	}))
	wantRaw(t, c.Built.UpdatedFrom, `"description":null`, `"dueDate":null`, `"assigneeId":null`)
	c = update("2026-11-03T16:00:00.000Z", issue.Patch{
		Description: ptr("again"), Priority: ptr(1),
		AssigneeID: kit.Opt[uuid.UUID]{Set: true, Value: &e.users.Admin.ID},
		DueDate:    kit.Opt[time.Time]{Set: true, Value: ptr(due.AddDate(0, 0, 1))},
		SortOrder:  ptr(42.5),
	})
	wantJSON(t, "updatedFrom", c.Built.UpdatedFrom, js(map[string]any{
		"updatedAt": "2026-11-03T15:00:00.000Z", "description": "now text", "priority": 3,
		"assigneeId": e.users.Owner.ID, "dueDate": "2026-12-01", "sortOrder": c.Change.Diff["sortOrder"].From,
	}))
	if d = obj(t, c.Built.Data); d["sortOrder"] != 42.5 || d["prioritySortOrder"] != 42.5 || d["priorityLabel"] != "Urgent" {
		t.Errorf("data = sortOrder %v prioritySortOrder %v priorityLabel %v", d["sortOrder"], d["prioritySortOrder"], d["priorityLabel"])
	}
	wantNoRaw(t, c.Built.UpdatedFrom, `"title"`, `"labelIds"`, `"stateId"`)
}

func TestBuildIssueMoveUpdatedFrom(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	old := e.project(t, "OLD", "Old project")
	next := e.project(t, "NEW", "New project")
	var issues []issue.Issue
	for n := range 3 {
		issues = append(issues, e.issue(t, issue.CreateInput{ProjectID: &old.ID, Title: fmt.Sprintf("o%d", n+1)}))
	}
	child := e.issue(t, issue.CreateInput{ParentID: &issues[2].ID, Title: "child"})
	todoOld := e.state(t, old.ID, "Todo")
	e.at("2026-11-02T10:00:00.000Z")
	e.watch()

	if _, err := e.issues.Move(ctx, e.users.Admin, issues[2].ID, next.ID, nil); err != nil {
		t.Fatal(err)
	}
	if len(e.cap.got) != 2 || e.cap.got[0].Change.EntityID != issues[2].ID || e.cap.got[1].Change.EntityID != child.ID {
		t.Fatalf("captured %v, want the root then its child", e.lines())
	}
	root := e.cap.got[0]
	uf := obj(t, root.Built.UpdatedFrom)
	for k, want := range map[string]any{
		"teamId": old.ID.String(), "number": float64(3), "identifier": "OLD-3",
		"stateId": todoOld.ID.String(), "updatedAt": "2026-11-01T10:00:00.000Z",
	} {
		if !reflect.DeepEqual(uf[k], want) {
			t.Errorf("updatedFrom.%s = %#v, want %#v", k, uf[k], want)
		}
	}
	d := obj(t, root.Built.Data)
	if d["teamId"] != next.ID.String() || d["identifier"] != "NEW-1" || d["number"] != float64(1) || d["url"] != base+"/issue/NEW-1" {
		t.Errorf("data = teamId %v identifier %v number %v url %v", d["teamId"], d["identifier"], d["number"], d["url"])
	}
	if sub(t, d, "team")["key"] != "NEW" || *root.Built.URL != base+"/issue/NEW-1" {
		t.Errorf("team %v url %v", d["team"], *root.Built.URL)
	}

	// The descendant follows with its own previous identifier.
	cuf := obj(t, e.cap.got[1].Built.UpdatedFrom)
	if cuf["identifier"] != "OLD-4" || cuf["number"] != float64(4) || cuf["teamId"] != old.ID.String() {
		t.Errorf("child updatedFrom = %v", cuf)
	}
	if cd := obj(t, e.cap.got[1].Built.Data); cd["identifier"] != "NEW-2" || cd["parentId"] != issues[2].ID.String() {
		t.Errorf("child data = identifier %v parentId %v", cd["identifier"], cd["parentId"])
	}
}

func TestBuildIssueMoveAfterKeyChange(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	old := e.project(t, "OLD", "Old project")
	next := e.project(t, "NEW", "New project")
	e.issue(t, issue.CreateInput{ProjectID: &old.ID, Title: "one"})
	i := e.issue(t, issue.CreateInput{ProjectID: &old.ID, Title: "two"})
	// The old identifier is built with the key the old project has when the issue moves.
	if _, err := e.projects.Update(ctx, e.users.Admin, old.ID, project.UpdateInput{Key: ptr("OLDER")}); err != nil {
		t.Fatal(err)
	}
	e.watch()

	if _, err := e.issues.Move(ctx, e.users.Admin, i.ID, next.ID, nil); err != nil {
		t.Fatal(err)
	}
	uf := obj(t, e.only(t).Built.UpdatedFrom)
	if uf["identifier"] != "OLDER-2" || uf["number"] != float64(2) || uf["teamId"] != old.ID.String() {
		t.Errorf("updatedFrom = %v, want identifier OLDER-2", uf)
	}
}

// A move that keeps the number still changes the identifier.
func TestBuildIssueMoveSameNumber(t *testing.T) {
	e := newEnv(t)
	old := e.project(t, "OLD", "Old project")
	next := e.project(t, "NEW", "New project")
	i := e.issue(t, issue.CreateInput{ProjectID: &old.ID, Title: "one"})
	e.watch()

	moved, err := e.issues.Move(context.Background(), e.users.Admin, i.ID, next.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Number != 1 {
		t.Fatalf("moved number = %d, want 1 (the case under test)", moved.Number)
	}
	uf := obj(t, e.only(t).Built.UpdatedFrom)
	if uf["identifier"] != "OLD-1" || uf["teamId"] != old.ID.String() {
		t.Errorf("updatedFrom = %v, want identifier OLD-1 and teamId", uf)
	}
	if _, ok := uf["number"]; ok {
		t.Errorf("updatedFrom has number %v although the number did not change", uf["number"])
	}
}

// An issue gets the workspace's default type label; it counts among the labels.
func TestBuildIssueTypeLabel(t *testing.T) {
	e := newEnv(t)
	e.watch()

	e.issue(t, issue.CreateInput{Title: "typed"})
	d := obj(t, e.only(t).Built.Data)
	ids, _ := d["labelIds"].([]any)
	labels, _ := d["labels"].([]any)
	if len(ids) != 1 || len(labels) != 1 {
		t.Fatalf("labelIds %v labels %v, want the default type label", d["labelIds"], d["labels"])
	}
	l := labels[0].(map[string]any)
	if l["id"] != ids[0] || l["name"] == "" || l["parentId"] == nil {
		t.Errorf("type label = %v, want a label inside the type group", l)
	}
}

func TestBuildIssueRemove(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	parent := e.issue(t, issue.CreateInput{Title: "parent"})
	kid := e.issue(t, issue.CreateInput{ParentID: &parent.ID, Title: "kid"})
	e.at("2026-11-03T08:00:00.000Z")
	e.watch()

	if err := e.issues.Delete(ctx, e.users.Admin, parent.ID); err != nil {
		t.Fatal(err)
	}
	if len(e.cap.got) != 2 {
		t.Fatalf("captured %v", e.lines())
	}
	// The child became top-level first.
	up := e.cap.got[0]
	if up.Change.EntityID != kid.ID || up.Change.Action != event.Update {
		t.Fatalf("first change = %+v", up.Change)
	}
	wantJSON(t, "updatedFrom", up.Built.UpdatedFrom, js(map[string]any{"updatedAt": "2026-11-01T10:00:00.000Z", "parentId": parent.ID}))
	wantRaw(t, up.Built.Data, `"parentId":null`, `"trashed":false`)

	rm := e.cap.got[1]
	if rm.Change.Action != event.Remove || rm.Change.EntityID != parent.ID {
		t.Fatalf("second change = %+v", rm.Change)
	}
	if rm.Built.Type != "Issue" || rm.Built.UpdatedFrom != nil || *rm.Built.URL != base+"/issue/YLK-1" {
		t.Errorf("remove built = type %q updatedFrom %s url %v", rm.Built.Type, rm.Built.UpdatedFrom, rm.Built.URL)
	}
	d := obj(t, rm.Built.Data)
	if d["id"] != parent.ID.String() || d["identifier"] != "YLK-1" || d["title"] != "parent" || d["trashed"] != true {
		t.Errorf("remove data = id %v identifier %v trashed %v", d["id"], d["identifier"], d["trashed"])
	}
	wantRaw(t, rm.Built.Data, `"trashed":true`)
}

func TestBuildProjectDeleteRemoves(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.project(t, "DEL", "Doomed")
	i := e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "last"})
	m, err := e.milestones.Create(ctx, e.users.Member, p.ID, milestone.CreateInput{Name: "Gone"})
	if err != nil {
		t.Fatal(err)
	}
	e.watch()

	if err := e.projects.Delete(ctx, e.users.Owner, p.ID, "DEL"); err != nil {
		t.Fatal(err)
	}
	if len(e.cap.got) != 2 {
		t.Fatalf("captured %v", e.lines())
	}
	is, ms := e.cap.got[0], e.cap.got[1]
	if is.Change.EntityID != i.ID || !is.Change.Purged || is.Built.Type != "Issue" {
		t.Fatalf("issue change = %+v", is.Change)
	}
	// Gone for good, not restorable: not trashed.
	wantRaw(t, is.Built.Data, `"trashed":false`)
	wantNoRaw(t, is.Built.Data, `"trashed":true`)
	if ms.Change.EntityID != m.ID || ms.Built.Type != "Project" {
		t.Fatalf("milestone change = %+v", ms.Change)
	}
	wantRaw(t, ms.Built.Data, `"trashed":true`)
}

func TestBuildIssueAfterLabelDelete(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.noDefaultType(t)
	a := e.label(t, label.CreateInput{Name: "a", Color: "#aaaaaa"})
	b := e.label(t, label.CreateInput{Name: "b", Color: "#bbbbbb"})
	i := e.issue(t, issue.CreateInput{Title: "labelled", LabelIDs: []uuid.UUID{a.ID, b.ID}})
	e.at("2026-11-05T09:00:00.500Z")
	e.watch()

	if err := e.labels.Delete(ctx, e.users.Admin, a.ID); err != nil {
		t.Fatal(err)
	}
	if len(e.cap.got) != 2 {
		t.Fatalf("captured %v", e.lines())
	}
	rm, up := e.cap.got[0], e.cap.got[1]
	if rm.Change.Resource != event.Label || rm.Change.Action != event.Remove || rm.Built.Type != "IssueLabel" {
		t.Fatalf("first change = %+v", rm.Change)
	}
	if d := obj(t, rm.Built.Data); d["id"] != a.ID.String() || d["name"] != "a" {
		t.Errorf("label remove data = %v", d)
	}
	if up.Change.EntityID != i.ID || up.Change.Action != event.Update {
		t.Fatalf("second change = %+v", up.Change)
	}
	d := obj(t, up.Built.Data)
	if !reflect.DeepEqual(d["labelIds"], []any{b.ID.String()}) || d["updatedAt"] != "2026-11-05T09:00:00.500Z" {
		t.Errorf("issue data = labelIds %v updatedAt %v", d["labelIds"], d["updatedAt"])
	}
	wantJSON(t, "updatedFrom", up.Built.UpdatedFrom, js(map[string]any{
		"updatedAt": "2026-11-01T10:00:00.000Z", "labelIds": sortedIDs(a.ID, b.ID),
	}))
	wantNoRaw(t, up.Built.Data, a.ID.String())
	if d["updatedAt"].(string) <= obj(t, up.Built.UpdatedFrom)["updatedAt"].(string) {
		t.Errorf("updatedAt %v does not move past updatedFrom.updatedAt", d["updatedAt"])
	}
}

func TestBuildComment(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	i := e.issue(t, issue.CreateInput{Title: "Export to CSV"})
	e.watch()

	e.at("2026-11-04T18:30:00.000Z")
	cm, err := e.comments.Create(ctx, e.users.Member, i.ID, "Looks good", nil)
	if err != nil {
		t.Fatal(err)
	}
	c := e.only(t)
	url := base + "/issue/YLK-1#comment-" + cm.ID.String()
	if c.Built.Type != "Comment" || c.Built.URL == nil || *c.Built.URL != url || c.Built.UpdatedFrom != nil {
		t.Fatalf("built = type %q url %v updatedFrom %s", c.Built.Type, c.Built.URL, c.Built.UpdatedFrom)
	}
	d := obj(t, c.Built.Data)
	for k, want := range map[string]any{
		"id": cm.ID.String(), "body": "Looks good", "issueId": i.ID.String(), "userId": e.users.Member.ID.String(),
		"createdAt": "2026-11-04T18:30:00.000Z", "updatedAt": "2026-11-04T18:30:00.000Z", "editedAt": nil,
		"parentId": nil, "projectId": nil, "resolvedAt": nil,
	} {
		if !reflect.DeepEqual(d[k], want) {
			t.Errorf("data.%s = %#v, want %#v", k, d[k], want)
		}
	}
	wantJSON(t, "data.issue", mustRaw(t, d["issue"]), js(map[string]any{
		"id": i.ID, "identifier": "YLK-1", "title": "Export to CSV", "url": base + "/issue/YLK-1",
		"teamId": e.ylk.ID, "team": map[string]any{"id": e.ylk.ID, "key": "YLK", "name": "Yalkos"},
	}))
	m := e.users.Member
	wantJSON(t, "data.user", mustRaw(t, d["user"]), js(map[string]any{
		"id": m.ID, "name": m.Name, "email": m.Email, "url": base, "avatarUrl": nil,
	}))
	wantRaw(t, c.Built.Data, `"editedAt":null`, `"reactionData":[]`)

	// Edits report the previous body and edit time.
	e.reset()
	e.at("2026-11-04T18:40:00.000Z")
	if _, err := e.comments.Update(ctx, e.users.Member, cm.ID, "Looks great"); err != nil {
		t.Fatal(err)
	}
	c = e.only(t)
	wantJSON(t, "updatedFrom", c.Built.UpdatedFrom, js(map[string]any{
		"body": "Looks good", "editedAt": nil, "updatedAt": "2026-11-04T18:30:00.000Z",
	}))
	wantRaw(t, c.Built.UpdatedFrom, `"editedAt":null`)
	if d = obj(t, c.Built.Data); d["body"] != "Looks great" || d["editedAt"] != "2026-11-04T18:40:00.000Z" {
		t.Errorf("data = body %v editedAt %v", d["body"], d["editedAt"])
	}
	e.reset()
	e.at("2026-11-04T18:50:00.000Z")
	if _, err := e.comments.Update(ctx, e.users.Member, cm.ID, "Looks perfect"); err != nil {
		t.Fatal(err)
	}
	wantJSON(t, "updatedFrom", e.only(t).Built.UpdatedFrom, js(map[string]any{
		"body": "Looks great", "editedAt": "2026-11-04T18:40:00.000Z", "updatedAt": "2026-11-04T18:40:00.000Z",
	}))

	// A delete is built while the comment is still there.
	e.reset()
	if err := e.comments.Delete(ctx, e.users.Member, cm.ID); err != nil {
		t.Fatal(err)
	}
	c = e.only(t)
	if c.Change.Action != event.Remove || c.Built.UpdatedFrom != nil || *c.Built.URL != url {
		t.Errorf("remove = %+v updatedFrom %s url %v", c.Change, c.Built.UpdatedFrom, c.Built.URL)
	}
	if d = obj(t, c.Built.Data); d["body"] != "Looks perfect" {
		t.Errorf("remove data body = %v", d["body"])
	}
}

func TestBuildLabel(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	group := e.label(t, label.CreateInput{Name: "Area", Color: "#111111", IsGroup: true})
	e.watch()

	e.at("2026-11-04T12:00:00.000Z")
	l, err := e.labels.Create(ctx, e.users.Admin, label.CreateInput{Name: "Feature", Color: "#3b82f6", ParentID: &group.ID})
	if err != nil {
		t.Fatal(err)
	}
	c := e.only(t)
	if c.Built.Type != "IssueLabel" || c.Built.URL != nil || c.Built.UpdatedFrom != nil {
		t.Fatalf("built = type %q url %v updatedFrom %s", c.Built.Type, c.Built.URL, c.Built.UpdatedFrom)
	}
	wantJSON(t, "data", c.Built.Data, js(map[string]any{
		"id": l.ID, "name": "Feature", "color": "#3b82f6", "description": nil, "isGroup": false, "parentId": group.ID,
		"creatorId": e.users.Admin.ID, "teamId": nil, "inheritedFromId": nil, "archivedAt": nil,
		"createdAt": "2026-11-04T12:00:00.000Z", "updatedAt": "2026-11-04T12:00:00.000Z",
	}))
	wantRaw(t, c.Built.Data, `"description":null`, `"teamId":null`, `"archivedAt":null`, `"isGroup":false`)

	// A group says so.
	e.reset()
	g2 := e.label(t, label.CreateInput{Name: "Other", Color: "#222222", IsGroup: true})
	if d := obj(t, e.only(t).Built.Data); d["id"] != g2.ID.String() || d["isGroup"] != true || d["parentId"] != nil {
		t.Errorf("group data = %v", d)
	}

	// Only fields a Linear-compatible label has are reported; an exclusive-only change sends the update time alone.
	e.reset()
	e.at("2026-11-04T13:00:00.000Z")
	if _, err := e.labels.Update(ctx, e.users.Admin, group.ID, label.Patch{Exclusive: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	c = e.only(t)
	if keys := diffKeys(c.Change); !slices.Equal(keys, []string{"exclusive"}) {
		t.Fatalf("diff keys = %v, want [exclusive]", keys)
	}
	wantJSON(t, "updatedFrom", c.Built.UpdatedFrom, js(map[string]any{"updatedAt": "2026-11-01T10:00:00.000Z"}))

	// Positive control: those fields come through, "" description as null.
	e.reset()
	e.at("2026-11-04T14:00:00.000Z")
	if _, err := e.labels.Update(ctx, e.users.Admin, l.ID, label.Patch{
		Name: ptr("Feat"), Color: ptr("#000000"), Description: ptr("about"), Position: ptr(7.0),
		ParentID: kit.Opt[uuid.UUID]{Set: true},
	}); err != nil {
		t.Fatal(err)
	}
	c = e.only(t)
	wantJSON(t, "updatedFrom", c.Built.UpdatedFrom, js(map[string]any{
		"updatedAt": "2026-11-04T12:00:00.000Z", "name": "Feature", "color": "#3b82f6", "description": nil, "parentId": group.ID,
	}))
	wantRaw(t, c.Built.UpdatedFrom, `"description":null`)
	if d := obj(t, c.Built.Data); d["name"] != "Feat" || d["description"] != "about" || d["parentId"] != nil {
		t.Errorf("data = %v", d)
	}
}

func diffKeys(c event.Change) []string {
	var keys []string
	for k := range c.Diff {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func TestBuildProject(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	target := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	e.watch()

	e.at("2026-11-04T10:00:00.000Z")
	m, err := e.milestones.Create(ctx, e.users.Member, e.ylk.ID, milestone.CreateInput{
		Name: "Beta", TargetDate: &target, LeadID: &e.users.Admin.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	c := e.only(t)
	url := base + "/p/YLK/milestones/" + m.ID.String()
	if c.Built.Type != "Project" || c.Built.URL == nil || *c.Built.URL != url || c.Built.UpdatedFrom != nil {
		t.Fatalf("built = type %q url %v updatedFrom %s", c.Built.Type, c.Built.URL, c.Built.UpdatedFrom)
	}
	d := obj(t, c.Built.Data)
	for k, want := range map[string]any{
		"id": m.ID.String(), "name": "Beta", "description": "", "content": nil, "slugId": m.ID.String(), "url": url,
		"sortOrder": m.SortOrder, "prioritySortOrder": m.SortOrder, "targetDate": "2026-12-01",
		"statusId": "00000000-0000-7000-8000-000000000001", "leadId": e.users.Admin.ID.String(),
		"creatorId": e.users.Member.ID.String(), "teamIds": []any{e.ylk.ID.String()}, "color": "#6B7280",
		"priority": float64(0), "trashed": false, "createdAt": "2026-11-04T10:00:00.000Z", "updatedAt": "2026-11-04T10:00:00.000Z",
		"startDate": nil, "completedAt": nil, "icon": nil, "health": nil, "identifier": nil,
	} {
		if !reflect.DeepEqual(d[k], want) {
			t.Errorf("data.%s = %#v, want %#v", k, d[k], want)
		}
	}
	wantJSON(t, "data.status", mustRaw(t, d["status"]), `{"id":"00000000-0000-7000-8000-000000000001","name":"Planned","type":"planned","color":"#6B7280"}`)
	ad := e.users.Admin
	wantJSON(t, "data.lead", mustRaw(t, d["lead"]), js(map[string]any{
		"id": ad.ID, "name": ad.Name, "email": ad.Email, "url": base, "avatarUrl": nil,
	}))
	wantRaw(t, c.Built.Data, `"content":null`, `"description":""`, `"labelIds":[]`, `"memberIds":[]`, `"initiatives":null`, `"milestones":null`)

	// Two issues in the milestone, one done: the status computes to In Progress.
	done, todo := e.state(t, e.ylk.ID, "Done"), e.state(t, e.ylk.ID, "Todo")
	for _, st := range []uuid.UUID{done.ID, todo.ID} {
		e.issue(t, issue.CreateInput{Title: "in beta", StateID: &st, MilestoneID: &m.ID})
	}
	e.reset()
	e.at("2026-11-04T11:00:00.000Z")
	if _, err := e.milestones.Update(ctx, e.users.Member, m.ID, milestone.Patch{Name: ptr("Beta 2"), Description: ptr("long text")}); err != nil {
		t.Fatal(err)
	}
	c = e.only(t)
	d = obj(t, c.Built.Data)
	if d["statusId"] != "00000000-0000-7000-8000-000000000002" || d["content"] != "long text" || d["description"] != "long text" {
		t.Errorf("data = statusId %v content %v description %v", d["statusId"], d["content"], d["description"])
	}
	wantJSON(t, "data.status", mustRaw(t, d["status"]), `{"id":"00000000-0000-7000-8000-000000000002","name":"In Progress","type":"started","color":"#F59E0B"}`)
	wantJSON(t, "updatedFrom", c.Built.UpdatedFrom, js(map[string]any{
		"updatedAt": "2026-11-04T10:00:00.000Z", "name": "Beta", "description": "", "content": nil,
	}))
	wantRaw(t, c.Built.UpdatedFrom, `"content":null`)

	// Pinning Done: the status before was the computed In Progress.
	e.reset()
	e.at("2026-11-04T12:00:00.000Z")
	pin := milestone.Done
	if _, err := e.milestones.Update(ctx, e.users.Member, m.ID, milestone.Patch{Status: kit.Opt[milestone.Status]{Set: true, Value: &pin}}); err != nil {
		t.Fatal(err)
	}
	c = e.only(t)
	wantJSON(t, "updatedFrom", c.Built.UpdatedFrom, `{"updatedAt":"2026-11-04T11:00:00.000Z","statusId":"00000000-0000-7000-8000-000000000002"}`)
	if obj(t, c.Built.Data)["statusId"] != "00000000-0000-7000-8000-000000000003" {
		t.Errorf("data.statusId = %v, want the pinned Done", obj(t, c.Built.Data)["statusId"])
	}

	// Back to automatic: the status before was the pin.
	e.reset()
	e.at("2026-11-04T13:00:00.000Z")
	if _, err := e.milestones.Update(ctx, e.users.Member, m.ID, milestone.Patch{Status: kit.Opt[milestone.Status]{Set: true}}); err != nil {
		t.Fatal(err)
	}
	c = e.only(t)
	wantJSON(t, "updatedFrom", c.Built.UpdatedFrom, `{"updatedAt":"2026-11-04T12:00:00.000Z","statusId":"00000000-0000-7000-8000-000000000003"}`)

	// Target date and lead: previous values, null when unset.
	e.reset()
	e.at("2026-11-04T14:00:00.000Z")
	if _, err := e.milestones.Update(ctx, e.users.Member, m.ID, milestone.Patch{
		TargetDate: kit.Opt[time.Time]{Set: true}, LeadID: kit.Opt[uuid.UUID]{Set: true},
	}); err != nil {
		t.Fatal(err)
	}
	c = e.only(t)
	wantJSON(t, "updatedFrom", c.Built.UpdatedFrom, js(map[string]any{
		"updatedAt": "2026-11-04T13:00:00.000Z", "targetDate": "2026-12-01", "leadId": e.users.Admin.ID,
	}))
	wantRaw(t, c.Built.Data, `"targetDate":null`, `"leadId":null`, `"lead":null`)

	// Reorder: the previous sort order.
	other := e.milestone(t, "Other", milestone.CreateInput{})
	e.reset()
	e.at("2026-11-04T15:00:00.000Z")
	if err := e.milestones.Reorder(ctx, e.users.Member, e.ylk.ID, []uuid.UUID{other.ID, m.ID}); err != nil {
		t.Fatal(err)
	}
	if len(e.cap.got) != 2 {
		t.Fatalf("captured %v", e.lines())
	}
	for _, c := range e.cap.got {
		prev := c.Change.Diff["sortOrder"].From
		wantJSON(t, "updatedFrom", c.Built.UpdatedFrom, js(map[string]any{"updatedAt": "2026-11-04T14:00:00.000Z", "sortOrder": prev}))
		if want := map[uuid.UUID]float64{other.ID: 2000, m.ID: 1000}[c.Change.EntityID]; prev != want {
			t.Errorf("previous sort order of %s = %v, want %v", c.Change.EntityID, prev, want)
		}
	}

	// Deleting: the issues lose the milestone (project id in updatedFrom), then the milestone is gone.
	e.reset()
	e.at("2026-11-04T16:00:00.000Z")
	if err := e.milestones.Delete(ctx, e.users.Member, m.ID); err != nil {
		t.Fatal(err)
	}
	if len(e.cap.got) != 3 {
		t.Fatalf("captured %v", e.lines())
	}
	for _, c := range e.cap.got[:2] {
		wantRaw(t, c.Built.Data, `"projectId":null`, `"project":null`)
		if obj(t, c.Built.UpdatedFrom)["projectId"] != m.ID.String() {
			t.Errorf("updatedFrom = %s, want projectId %s", c.Built.UpdatedFrom, m.ID)
		}
	}
	rm := e.cap.got[2]
	if rm.Change.Action != event.Remove || rm.Built.Type != "Project" || rm.Built.UpdatedFrom != nil {
		t.Fatalf("remove = %+v type %q", rm.Change, rm.Built.Type)
	}
	wantRaw(t, rm.Built.Data, `"trashed":true`)
	if d = obj(t, rm.Built.Data); d["id"] != m.ID.String() || d["name"] != "Beta 2" {
		t.Errorf("remove data = id %v name %v", d["id"], d["name"])
	}
}

func TestActor(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	m := e.users.Member
	b := linearpayload.New(base)

	raw, err := b.Actor(ctx, q.New(e.pool), m.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, "actor", raw, js(map[string]any{
		"id": m.ID, "name": m.Name, "email": m.Email, "url": base, "avatarUrl": nil, "type": "user",
	}))
	wantRaw(t, raw, `"avatarUrl":null`, `"type":"user"`)

	// Positive control: another user gives other values, and a trailing slash
	// on the base URL does not double up.
	o := e.users.Owner
	raw, err = linearpayload.New(base+"/").Actor(ctx, q.New(e.pool), o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := obj(t, raw); got["id"] != o.ID.String() || got["email"] != o.Email || got["url"] != base {
		t.Errorf("actor = %v", got)
	}
	if _, err := b.Actor(ctx, q.New(e.pool), uuid.New()); err == nil {
		t.Error("an unknown user built an actor")
	}
	if strings.Contains(string(raw), "password") {
		t.Errorf("actor leaks: %s", raw)
	}
}
