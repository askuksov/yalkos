package milestone_test

import (
	"testing"
	"time"

	"github.com/askuksov/yalkos/internal/domain/milestone"
)

func TestAutoStatus(t *testing.T) {
	cases := []struct {
		p    milestone.Progress
		want milestone.Status
	}{
		{milestone.Progress{}, milestone.Planned},
		{milestone.Progress{Total: 3}, milestone.Planned},
		{milestone.Progress{Total: 3, Active: 1}, milestone.InProgress},
		{milestone.Progress{Completed: 1, Total: 3, Active: 1}, milestone.InProgress},
		{milestone.Progress{Completed: 3, Total: 3, Active: 3}, milestone.Done},
	}
	for _, c := range cases {
		if got := milestone.AutoStatus(c.p); got != c.want {
			t.Errorf("AutoStatus(%+v) = %s, want %s", c.p, got, c.want)
		}
	}
}

func TestManualStatusWins(t *testing.T) {
	canceled := milestone.Canceled
	m := milestone.Milestone{Manual: &canceled}
	if got := m.Status(milestone.Progress{Completed: 1, Total: 1, Active: 1}); got != milestone.Canceled {
		t.Errorf("Status = %s, want canceled", got)
	}
}

func TestOverdue(t *testing.T) {
	today := time.Date(2026, 3, 10, 15, 0, 0, 0, time.UTC)
	yesterday := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	todayDate := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	inProgress := milestone.Progress{Total: 2, Active: 1}
	done := milestone.Progress{Completed: 2, Total: 2, Active: 2}
	if !(milestone.Milestone{TargetDate: &yesterday}).Overdue(inProgress, today) {
		t.Error("yesterday & in progress: want overdue")
	}
	if (milestone.Milestone{TargetDate: &yesterday}).Overdue(done, today) {
		t.Error("done: want not overdue")
	}
	canceled := milestone.Canceled
	if (milestone.Milestone{TargetDate: &yesterday, Manual: &canceled}).Overdue(inProgress, today) {
		t.Error("canceled: want not overdue")
	}
	if (milestone.Milestone{TargetDate: &todayDate}).Overdue(inProgress, today) {
		t.Error("due today: want not overdue")
	}
	if (milestone.Milestone{}).Overdue(inProgress, today) {
		t.Error("no date: want not overdue")
	}
}
