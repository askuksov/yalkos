package attachment

import (
	"context"
	"log/slog"
	"time"
)

// RunCleanup calls Cleanup at once and then every interval until ctx is
// done. Each run logs its error, if any, and the counts when something was
// removed (also alongside an error: the steps commit separately).
func (s *Service) RunCleanup(ctx context.Context, interval time.Duration, logger *slog.Logger) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		s.cleanupOnce(ctx, logger)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) cleanupOnce(ctx context.Context, logger *slog.Logger) {
	r, err := s.Cleanup(ctx)
	if err != nil && ctx.Err() == nil {
		logger.Error("attachment cleanup failed", slog.String("error", err.Error()))
	}
	if r.Pending > 0 || r.Blobs > 0 || r.Temp > 0 || r.Files > 0 {
		logger.Info("attachment cleanup", slog.Int("pending", r.Pending), slog.Int("blobs", r.Blobs),
			slog.Int("temp", r.Temp), slog.Int("files", r.Files))
	}
}
