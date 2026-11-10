package webhook

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RecordTx is Record run inside the caller's transaction, so a test can hold a
// recording open while another one runs.
func (s *Service) RecordTx(ctx context.Context, tx pgx.Tx, deliveryID uuid.UUID, r Result, attempt int, next *time.Time) error {
	return s.record(ctx, s.q.WithTx(tx), recordParams(deliveryID, r, attempt, next))
}
