package linearpayload_test

import (
	"net/url"
	"testing"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/webhook/linearpayload"
)

func TestPriorityLabel(t *testing.T) {
	for p, want := range map[int]string{0: "No priority", 1: "Urgent", 2: "High", 3: "Medium", 4: "Low", 5: "No priority", -1: "No priority"} {
		if got := linearpayload.PriorityLabel(p); got != want {
			t.Errorf("PriorityLabel(%d) = %q, want %q", p, got, want)
		}
	}
}

func TestProjectStatuses(t *testing.T) {
	want := map[milestone.Status]linearpayload.Status{
		milestone.Planned:    {ID: "00000000-0000-7000-8000-000000000001", Name: "Planned", Color: "#6B7280", Type: "planned"},
		milestone.InProgress: {ID: "00000000-0000-7000-8000-000000000002", Name: "In Progress", Color: "#F59E0B", Type: "started"},
		milestone.Done:       {ID: "00000000-0000-7000-8000-000000000003", Name: "Done", Color: "#22C55E", Type: "completed"},
		milestone.Canceled:   {ID: "00000000-0000-7000-8000-000000000004", Name: "Canceled", Color: "#EF4444", Type: "canceled"},
	}
	for s, w := range want {
		if got := linearpayload.StatusOf(s); got != w {
			t.Errorf("StatusOf(%s) = %+v, want %+v", s, got, w)
		}
		back, ok := linearpayload.StatusByID(w.ID)
		if !ok || back != s {
			t.Errorf("StatusByID(%s) = %q, %v, want %q", w.ID, back, ok, s)
		}
	}
	for i, w := range linearpayload.Statuses {
		if got := linearpayload.StatusOf([]milestone.Status{milestone.Planned, milestone.InProgress, milestone.Done, milestone.Canceled}[i]); got != w {
			t.Errorf("Statuses[%d] = %+v, StatusOf = %+v", i, w, got)
		}
	}
	if s, ok := linearpayload.StatusByID("00000000-0000-7000-8000-000000000009"); ok || s != "" {
		t.Errorf("StatusByID of an unknown id = %q, %v", s, ok)
	}
}

func TestURLs(t *testing.T) {
	id := uuid.MustParse("0193a1b2-0000-7000-8000-000000000001")
	if got := linearpayload.IssueURL("https://y.example", "YLK-12"); got != "https://y.example/issue/YLK-12" {
		t.Errorf("IssueURL = %q", got)
	}
	if got := linearpayload.MilestoneURL("https://y.example", "YLK", id); got != "https://y.example/p/YLK/milestones/"+id.String() {
		t.Errorf("MilestoneURL = %q", got)
	}
	for in, want := range map[string]string{"https://y.example": "https://y.example", "https://y.example/": "https://y.example"} {
		u, err := url.Parse(in)
		if err != nil {
			t.Fatal(err)
		}
		if got := linearpayload.BaseURL(u); got != want {
			t.Errorf("BaseURL(%q) = %q, want %q", in, got, want)
		}
	}
	if got := linearpayload.BaseURL(nil); got != "" {
		t.Errorf("BaseURL(nil) = %q, want empty", got)
	}
}
