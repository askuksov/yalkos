package attachment

import (
	"context"
	"log/slog"
	"time"
)

// RunCleanup calls Cleanup every interval until ctx is done.
func (s *Service) RunCleanup(ctx context.Context, interval time.Duration, logger *slog.Logger) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n, err := s.Cleanup(ctx)
			if err != nil {
				logger.Error("attachment cleanup failed", slog.String("error", err.Error()))
			} else if n > 0 {
				logger.Info("attachment cleanup", slog.Int("removed", n))
			}
		}
	}
}
