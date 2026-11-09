package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/page"
)

type milestoneJSON struct {
	ID, Name, Status, TargetDate, Lead, Description, URL string
	StatusAutomatic, Overdue                             bool
	Progress                                             struct{ Completed, Total int }
}

func (e *env) milestoneCall(t *testing.T, tool string, args map[string]any) (milestoneJSON, string) {
	t.Helper()
	text := e.ok(t, tool, args)
	var out milestoneJSON
	decode(t, text, &out)
	return out, text
}

func TestCreateMilestone(t *testing.T) {
	e := newEnv(t)
	e.project(t, "YLK", "App")
	got, _ := e.milestoneCall(t, "create_milestone", map[string]any{
		"project": "ylk", "name": "Bank APIs", "description": "Sync banks", "targetDate": "2026-12-15", "lead": "me",
	})
	if got.ID == "" || got.Name != "Bank APIs" || got.Status != "planned" || !got.StatusAutomatic || got.Progress.Total != 0 ||
		got.TargetDate != "2026-12-15" || got.Lead != "Owner" || got.Description != "Sync banks" ||
		got.URL != "http://yalkos.test/p/YLK/milestones/"+got.ID || got.Overdue {
		t.Errorf("created = %+v", got)
	}
	if text := e.fail(t, "create_milestone", map[string]any{"project": "YLK", "name": "bank apis"}); text != "Invalid name: a milestone with this name exists." {
		t.Errorf("duplicate = %q", text)
	}
	if text := e.fail(t, "create_milestone", map[string]any{"project": "YLK", "name": "Later", "targetDate": "soon"}); text != "targetDate must be a date YYYY-MM-DD." {
		t.Errorf("bad date = %q", text)
	}
}

func TestCreateMilestoneLead(t *testing.T) {
	const noneErr = `Invalid lead: "none" has nothing to clear on a new milestone; leave lead out.`
	listed := func(e *env) []string {
		var out struct{ Milestones []struct{ Name string } }
		decode(t, e.ok(t, "list_milestones", map[string]any{"project": "YLK", "includeDone": true}), &out)
		names := []string{}
		for _, m := range out.Milestones {
			names = append(names, m.Name)
		}
		return names
	}

	for _, lead := range []string{"none", "None", "  NONE "} {
		t.Run("rejects "+strings.TrimSpace(lead), func(t *testing.T) {
			e := newEnv(t)
			e.project(t, "YLK", "App")
			if got := e.fail(t, "create_milestone", map[string]any{"project": "YLK", "name": "Bank APIs", "lead": lead}); got != noneErr {
				t.Errorf("got %q, want %q", got, noneErr)
			}
			if names := listed(e); len(names) != 0 {
				t.Errorf("milestones after rejected create = %v", names)
			}
		})
	}

	t.Run("without lead", func(t *testing.T) {
		e := newEnv(t)
		e.project(t, "YLK", "App")
		got, text := e.milestoneCall(t, "create_milestone", map[string]any{"project": "YLK", "name": "Bank APIs"})
		if got.Name != "Bank APIs" || strings.Contains(text, `"lead"`) {
			t.Errorf("created = %s", text)
		}
	})

	t.Run("with me", func(t *testing.T) {
		e := newEnv(t)
		e.project(t, "YLK", "App")
		got, _ := e.milestoneCall(t, "create_milestone", map[string]any{"project": "YLK", "name": "Bank APIs", "lead": "me"})
		if got.Lead != "Owner" {
			t.Errorf("lead = %q, want Owner", got.Lead)
		}
	})

	t.Run("update none still clears", func(t *testing.T) {
		e := newEnv(t)
		e.project(t, "YLK", "App")
		e.milestoneCall(t, "create_milestone", map[string]any{"project": "YLK", "name": "Bank APIs", "lead": "me"})
		got, text := e.milestoneCall(t, "update_milestone", map[string]any{"project": "YLK", "milestone": "Bank APIs", "lead": "none"})
		if got.Lead != "" || strings.Contains(text, `"lead"`) {
			t.Errorf("after clearing = %s", text)
		}
	})
}

func TestUpdateMilestone(t *testing.T) {
	e := newEnv(t)
	p := e.project(t, "YLK", "App")
	created, _ := e.milestoneCall(t, "create_milestone", map[string]any{"project": "YLK", "name": "Bank APIs", "targetDate": "2026-12-15", "lead": "me"})
	e.issue(t, issue.CreateInput{ProjectID: &p.ID, Title: "work", MilestoneID: uuidPtr(t, created.ID)})

	got, _ := e.milestoneCall(t, "update_milestone", map[string]any{"project": "YLK", "milestone": "bank apis", "status": "canceled"})
	if got.Status != "canceled" || got.StatusAutomatic || got.Progress.Total != 1 {
		t.Errorf("canceled = %+v", got)
	}
	got, _ = e.milestoneCall(t, "update_milestone", map[string]any{"project": "YLK", "milestone": "Bank APIs", "status": "automatic"})
	if got.Status != "planned" || !got.StatusAutomatic {
		t.Errorf("automatic = %+v", got)
	}
	got, text := e.milestoneCall(t, "update_milestone", map[string]any{"project": "YLK", "milestone": created.ID, "targetDate": "none", "lead": "none", "name": "Banks", "description": "New text"})
	if strings.Contains(text, "targetDate") || strings.Contains(text, `"lead"`) || got.Name != "Banks" || got.Description != "New text" {
		t.Errorf("by id = %s", text)
	}
	got, _ = e.milestoneCall(t, "update_milestone", map[string]any{"project": "YLK", "milestone": "Banks", "lead": e.users.Admin.Email, "targetDate": "2027-01-10"})
	if got.Lead != e.users.Admin.Name || got.TargetDate != "2027-01-10" {
		t.Errorf("lead and date = %+v", got)
	}
}

func TestUpdateMilestoneRejects(t *testing.T) {
	e := newEnv(t)
	e.project(t, "YLK", "App")
	e.milestoneCall(t, "create_milestone", map[string]any{"project": "YLK", "name": "Bank APIs"})
	if got, want := e.fail(t, "update_milestone", map[string]any{"project": "YLK", "milestone": "Bank APIs", "status": "closed"}),
		`Unknown status "closed". Use one of: planned, in_progress, done, canceled, automatic.`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := e.fail(t, "update_milestone", map[string]any{"project": "YLK", "milestone": "Nope", "name": "X"}),
		`Milestone "Nope" not found in project YLK. Milestones: Bank APIs.`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := e.fail(t, "update_milestone", map[string]any{"project": "YLK", "milestone": "none"}),
		`Milestone "none" not found in project YLK. Milestones: Bank APIs.`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMilestoneAudit(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.project(t, "YLK", "App")
	member := e.client(t, e.key(t, e.users.Member))
	text, isErr := e.call(t, member, "create_milestone", map[string]any{"project": "YLK", "name": "Bank APIs"})
	if isErr {
		t.Fatal(text)
	}
	var m milestoneJSON
	decode(t, text, &m)
	if text, isErr := e.call(t, member, "update_milestone", map[string]any{"project": "YLK", "milestone": "Bank APIs", "status": "done"}); isErr {
		t.Fatal(text)
	}
	entries, err := audit.New(e.pool).List(ctx, *uuidPtr(t, m.ID), page.Args{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries.Items) != 2 {
		t.Fatalf("history = %+v", entries.Items)
	}
	for _, en := range entries.Items {
		if en.ActorID == nil || *en.ActorID != e.users.Member.ID {
			t.Errorf("entry %s by %v, want Member", en.Action, en.ActorID)
		}
	}
}
