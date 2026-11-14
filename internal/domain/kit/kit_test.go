package kit_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/store/q"
	"github.com/askuksov/yalkos/internal/testdb"
)

func TestInTxStartsOverEightTimes(t *testing.T) {
	_, pool := testdb.New(t)
	ctx := context.Background()
	base := q.New(pool)

	calls := 0
	err := kit.InTx(ctx, pool, base, func(*q.Queries) error {
		calls++
		if calls < 8 {
			return fmt.Errorf("moved: %w", kit.ErrStartOver)
		}
		return nil
	})
	if err != nil || calls != 8 {
		t.Fatalf("succeeds on the last run: err %v, calls %d, want nil, 8", err, calls)
	}

	calls = 0
	err = kit.InTx(ctx, pool, base, func(*q.Queries) error {
		calls++
		return fmt.Errorf("moved: %w", kit.ErrStartOver)
	})
	if !errors.Is(err, kit.ErrStartOver) || calls != 8 {
		t.Fatalf("always starts over: err %v, calls %d, want ErrStartOver, 8", err, calls)
	}
}

// captureDefaultLog swaps the default slog logger for one that writes to the
// returned buffer and restores it when the test ends. Tests using it must not
// run in parallel.
func captureDefaultLog(t *testing.T) *strings.Builder {
	t.Helper()
	var buf strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func countWarnings(logs string) int {
	return strings.Count(logs, "level=WARN")
}

func TestInTxWarnsWhenOutOfRetries(t *testing.T) {
	_, pool := testdb.New(t)
	ctx := context.Background()
	base := q.New(pool)
	logs := captureDefaultLog(t)

	err := kit.InTx(ctx, pool, base, func(*q.Queries) error {
		return fmt.Errorf("moved: %w", kit.ErrStartOver)
	})
	if !errors.Is(err, kit.ErrStartOver) {
		t.Fatalf("err %v, want ErrStartOver", err)
	}
	if n := countWarnings(logs.String()); n != 1 {
		t.Fatalf("%d warnings, want 1: %q", n, logs.String())
	}
	if !strings.Contains(logs.String(), "attempts=8") {
		t.Fatalf("warning lacks the attempt count: %q", logs.String())
	}
}

// Control: a run that succeeds on the last attempt logs nothing.
func TestInTxDoesNotWarnWhenLastRetrySucceeds(t *testing.T) {
	_, pool := testdb.New(t)
	ctx := context.Background()
	base := q.New(pool)
	logs := captureDefaultLog(t)

	calls := 0
	err := kit.InTx(ctx, pool, base, func(*q.Queries) error {
		calls++
		if calls < 8 {
			return fmt.Errorf("moved: %w", kit.ErrStartOver)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("err %v, want nil", err)
	}
	if n := countWarnings(logs.String()); n != 0 {
		t.Fatalf("%d warnings, want 0: %q", n, logs.String())
	}
}
