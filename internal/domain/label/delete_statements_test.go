package label_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/testfx"
)

// stmtCounter is a pgx tracer that counts SQL statements.
type stmtCounter struct{ n atomic.Int64 }

func (c *stmtCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (c *stmtCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// tracedLabels returns a label service on a second pool over the same
// database that counts its statements.
func tracedLabels(t *testing.T, e env) (*label.Service, *stmtCounter) {
	t.Helper()
	cfg := e.pool.Config()
	counter := &stmtCounter{}
	cfg.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	clock := testfx.Clock(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	return label.New(pool, clock), counter
}

// Deleting a label that issues hold costs the same number of statements
// whatever the number of issues: their history names are read once and the
// events are written in one statement.
func TestDeleteLabelStatementsDoNotGrowWithIssues(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	svc, counter := tracedLabels(t, e)
	statements := func(issues int) int64 {
		var least int64 = -1
		for run := range 3 {
			l := e.create(t, e.users.Member, label.CreateInput{Name: fmt.Sprintf("Perf %d/%d", issues, run)})
			for range issues {
				e.issueWith(t, l)
			}
			before := counter.n.Load()
			if err := svc.Delete(ctx, e.users.Admin, l.ID); err != nil {
				t.Fatal(err)
			}
			if used := counter.n.Load() - before; least < 0 || used < least {
				least = used
			}
		}
		return least
	}
	two, ten := statements(2), statements(10)
	if two != ten {
		t.Errorf("statements deleting a label held by 2 issues = %d, by 10 = %d; want equal", two, ten)
	}
}
