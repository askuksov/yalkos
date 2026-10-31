package issue

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// Progress counts an issue's live direct children: Total excludes canceled
// ones, Completed counts those in a completed state.
type Progress struct {
	Completed, Total int
}

// SubIssueProgress returns progress per parent id; parents without children
// are absent (zero progress).
func (s *Service) SubIssueProgress(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]Progress, error) {
	rows, err := s.q.SubIssueProgress(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("sub-issue progress: %w", err)
	}
	out := make(map[uuid.UUID]Progress, len(rows))
	for _, r := range rows {
		if r.ParentID != nil {
			out[*r.ParentID] = Progress{Completed: int(r.Completed), Total: int(r.Total)}
		}
	}
	return out, nil
}
