package audit

import "github.com/google/uuid"

// LookupIDs returns the ids the names lookup of RecordMany asks for: labels,
// milestones, states and projects.
func LookupIDs(events []Event) ([4][]uuid.UUID, error) {
	_, all, err := eventRefs(events)
	return [4][]uuid.UUID{all.LabelIds, all.MilestoneIds, all.StateIds, all.ProjectIds}, err
}
