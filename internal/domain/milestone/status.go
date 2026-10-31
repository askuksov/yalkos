package milestone

import "time"

// Status is a milestone's lifecycle stage.
type Status string

// Milestone statuses; Canceled is only ever set by hand.
const (
	Planned    Status = "planned"
	InProgress Status = "in_progress"
	Done       Status = "done"
	Canceled   Status = "canceled"
)

func (s Status) valid() bool {
	switch s {
	case Planned, InProgress, Done, Canceled:
		return true
	}
	return false
}

// Progress counts a milestone's live issues: Total excludes canceled ones,
// Completed are in a completed state, Active are started or completed.
type Progress struct {
	Completed, Total, Active int
}

// AutoStatus is the status computed from progress.
func AutoStatus(p Progress) Status {
	switch {
	case p.Total == 0 || p.Active == 0:
		return Planned
	case p.Completed == p.Total:
		return Done
	default:
		return InProgress
	}
}

// Status is the manual status when set, else the computed one.
func (m Milestone) Status(p Progress) Status {
	if m.Manual != nil {
		return *m.Manual
	}
	return AutoStatus(p)
}

// Overdue reports a target date before today's date that is not done or canceled.
func (m Milestone) Overdue(p Progress, today time.Time) bool {
	if m.TargetDate == nil {
		return false
	}
	if s := m.Status(p); s == Done || s == Canceled {
		return false
	}
	return m.TargetDate.Format(time.DateOnly) < today.Format(time.DateOnly)
}
