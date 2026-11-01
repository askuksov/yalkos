package mcpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/testfx"
)

// decode parses a tool answer into v.
func decode(t *testing.T, text string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(text), v); err != nil {
		t.Fatalf("decode %s: %v", text, err)
	}
}

var lookupTools = []string{"list_labels", "list_milestones", "list_projects", "list_states", "list_users"}

func TestListProjects(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.deps.Projects.Create(ctx, e.users.Owner, project.CreateInput{Key: "YLK", Name: "App", Description: strings.Repeat("d", 600)}); err != nil {
		t.Fatal(err)
	}
	e.project(t, "WEB", "Web")
	old := e.project(t, "OLD", "Old")
	if _, err := e.deps.Projects.SetArchived(ctx, e.users.Owner, old.ID, true); err != nil {
		t.Fatal(err)
	}

	type proj struct {
		Key, Name, Description, URL string
		Archived                    bool
	}
	var out struct{ Projects []proj }
	decode(t, e.ok(t, "list_projects", nil), &out)
	if len(out.Projects) != 2 || out.Projects[0].Key != "YLK" || out.Projects[1].Key != "WEB" {
		t.Fatalf("projects = %+v", out.Projects)
	}
	if got := out.Projects[0]; got.URL != "http://yalkos.test/p/YLK" || got.Archived || got.Description != strings.Repeat("d", 500)+"…" {
		t.Errorf("YLK = %+v", got)
	}

	out.Projects = nil
	text := e.ok(t, "list_projects", map[string]any{"includeArchived": true})
	decode(t, text, &out)
	keys := []string{}
	for _, p := range out.Projects {
		keys = append(keys, p.Key)
	}
	if !slices.Equal(keys, []string{"YLK", "OLD", "WEB"}) || !out.Projects[1].Archived {
		t.Errorf("with archived = %+v", out.Projects)
	}
	if strings.Count(text, `"archived"`) != 1 {
		t.Errorf("archived key only on archived projects: %s", text)
	}
}

func TestListStates(t *testing.T) {
	e := newEnv(t)
	e.project(t, "YLK", "App")
	e.project(t, "WEB", "Web")
	want := `{"project":"YLK","states":[{"name":"Backlog","type":"backlog"},{"name":"Todo","type":"unstarted","default":true},{"name":"In Progress","type":"started"},{"name":"Done","type":"completed"},{"name":"Canceled","type":"canceled"}]}`
	if got := e.ok(t, "list_states", map[string]any{"project": "ylk"}); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got, want := e.fail(t, "list_states", map[string]any{"project": "NOPE"}), `Project NOPE not found. Projects: YLK, WEB.`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestListMilestones(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.project(t, "YLK", "App")
	target := time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC)
	a := e.milestone(t, p, milestone.CreateInput{Name: "Bank APIs", Description: "Sync banks", TargetDate: &target, LeadID: &e.users.Admin.ID})
	b := e.milestone(t, p, milestone.CreateInput{Name: "Old"})
	done := milestone.Done
	if _, err := e.deps.Milestones.Update(ctx, e.users.Owner, b.ID, milestone.Patch{Status: kit.Opt[milestone.Status]{Set: true, Value: &done}}); err != nil {
		t.Fatal(err)
	}
	doneState := e.stateID(t, p, "Done")
	e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "one", MilestoneID: &a.ID, StateID: &doneState})
	e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "two", MilestoneID: &a.ID})

	want := `{"project":"YLK","milestones":[{"id":"` + a.ID.String() + `","name":"Bank APIs","status":"in_progress","statusAutomatic":true,"progress":{"completed":1,"total":2},"targetDate":"2026-12-15","overdue":false,"lead":"` + e.users.Admin.Name + `","description":"Sync banks","url":"http://yalkos.test/p/YLK/milestones/` + a.ID.String() + `"}]}`
	if got := e.ok(t, "list_milestones", map[string]any{"project": "YLK"}); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}

	var out struct {
		Milestones []struct {
			Name, Status    string
			StatusAutomatic bool
		}
	}
	decode(t, e.ok(t, "list_milestones", map[string]any{"project": "YLK", "includeDone": true}), &out)
	if len(out.Milestones) != 2 || out.Milestones[1].Name != "Old" || out.Milestones[1].Status != "done" || out.Milestones[1].StatusAutomatic {
		t.Errorf("includeDone = %+v", out.Milestones)
	}
}

func TestListLabels(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.deps.Labels.Create(ctx, e.users.Owner, label.CreateInput{Name: "Perf", Color: "#336699", Description: "Slow paths"}); err != nil {
		t.Fatal(err)
	}
	area := e.group(t, "Area", false)
	e.label(t, "API", &area.ID)
	size := e.group(t, "Size", true)
	e.label(t, "S", &size.ID)
	want := `{"types":["Bug","Feature","Task","Improvement"],"defaultType":"Task","labels":[{"name":"Perf","description":"Slow paths"}],"groups":[{"name":"Area","exclusive":false,"labels":[{"name":"API"}]},{"name":"Size","exclusive":true,"labels":[{"name":"S"}]}]}`
	if got := e.ok(t, "list_labels", nil); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestListUsers(t *testing.T) {
	e := newEnv(t)
	d := testfx.DisabledUser(t, e.pool, e.users.Owner)
	var out struct{ Users []map[string]any }
	decode(t, e.ok(t, "list_users", nil), &out)
	if len(out.Users) != 3 {
		t.Fatalf("users = %v", out.Users)
	}
	names := []string{}
	for _, u := range out.Users {
		names = append(names, u["name"].(string))
		if u["email"] == d.Email {
			t.Errorf("disabled user listed: %v", u)
		}
		me, has := u["me"]
		if u["email"] == "owner@example.com" {
			if me != true || u["role"] != "owner" {
				t.Errorf("caller = %v", u)
			}
		} else if has {
			t.Errorf("me key on %v", u)
		}
	}
	if !slices.IsSorted(names) {
		t.Errorf("not by name: %v", names)
	}
}

func listTools(t *testing.T, e *env) *mcp.ListToolsResult {
	t.Helper()
	res, err := e.owner(t).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestToolsListAnnotations(t *testing.T) {
	e := newEnv(t)
	byName := map[string]*mcp.Tool{}
	for _, tool := range listTools(t, e).Tools {
		byName[tool.Name] = tool
	}
	for _, name := range lookupTools {
		tool := byName[name]
		if tool == nil {
			t.Errorf("%s missing", name)
			continue
		}
		a := tool.Annotations
		if a == nil || !a.ReadOnlyHint || a.OpenWorldHint == nil || *a.OpenWorldHint {
			t.Errorf("%s annotations = %+v", name, a)
		}
		if tool.OutputSchema != nil {
			t.Errorf("%s has an output schema", name)
		}
		if tool.Title == "" || tool.Description == "" {
			t.Errorf("%s without title or description", name)
		}
	}
}

func TestToolCallLogLine(t *testing.T) {
	var buf syncBuffer
	e := newEnvLogger(t, slog.New(slog.NewJSONHandler(&buf, nil)))
	e.project(t, "YLK", "App")
	e.ok(t, "list_states", map[string]any{"project": "YLK"})

	var lines []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err == nil && m["msg"] == "mcp tool" {
			lines = append(lines, m)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("mcp tool lines = %v (log %s)", lines, buf.String())
	}
	l := lines[0]
	if l["tool"] != "list_states" || l["user_id"] != e.users.Owner.ID.String() || l["error"] != false || l["level"] != "INFO" {
		t.Errorf("line = %v", l)
	}
	if _, ok := l["duration_ms"].(float64); !ok {
		t.Errorf("duration_ms = %v", l["duration_ms"])
	}
	if strings.Contains(buf.String(), "YLK") {
		t.Errorf("arguments or results logged: %s", buf.String())
	}
}

func TestNoTeamWord(t *testing.T) {
	e := newEnv(t)
	b, err := json.Marshal(listTools(t, e))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(b)), "team") {
		t.Errorf("tools/list mentions team: %s", b)
	}
}
