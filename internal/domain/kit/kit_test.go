package kit_test

import (
	"context"
	"errors"
	"fmt"
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
