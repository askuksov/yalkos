package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

func TestNewServerTimeouts(t *testing.T) {
	srv := newServer(http.NotFoundHandler())
	if srv.ReadHeaderTimeout != 10*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 10s", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout != 30*time.Second {
		t.Errorf("ReadTimeout = %v, want 30s", srv.ReadTimeout)
	}
	if srv.IdleTimeout != 120*time.Second {
		t.Errorf("IdleTimeout = %v, want 120s", srv.IdleTimeout)
	}
}

// fakeQueue records how the queue was stopped.
type fakeQueue struct {
	softErr, hardErr error
	soft, hard       int
}

func (q *fakeQueue) Stop(ctx context.Context) error {
	q.soft++
	if q.softErr != nil {
		<-ctx.Done()
		return q.softErr
	}
	return nil
}

func (q *fakeQueue) StopAndCancel(context.Context) error { q.hard++; return q.hardErr }

func TestStopQueue(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	// Positive control: a soft stop that finishes in time is the only stop.
	q := &fakeQueue{}
	stopQueue(q, time.Second, time.Second, logger)
	if q.soft != 1 || q.hard != 0 {
		t.Errorf("soft stop in time: soft=%d hard=%d, want 1 and 0", q.soft, q.hard)
	}

	// A soft stop that outlasts its window is followed by a hard stop.
	q = &fakeQueue{softErr: context.DeadlineExceeded}
	began := time.Now()
	stopQueue(q, 50*time.Millisecond, time.Second, logger)
	if q.soft != 1 || q.hard != 1 {
		t.Errorf("soft stop timed out: soft=%d hard=%d, want 1 and 1", q.soft, q.hard)
	}
	if d := time.Since(began); d > 5*time.Second {
		t.Errorf("stopQueue took %v", d)
	}
}
