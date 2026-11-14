package project_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/domain/project"
)

// stmtCounter is a pgx tracer that counts SQL statements.
type stmtCounter struct{ n atomic.Int64 }

func (c *stmtCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (c *stmtCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// Deleting a state that issues are moved out of costs the same number of
// statements whatever the number of issues: their history names are read
// once and the events are written in one statement.
func TestDeleteStateStatementsDoNotGrowWithIssues(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	cfg := e.pool.Config()
	counter := &stmtCounter{}
	cfg.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	svc := project.New(pool, e.clock)

	p := e.create(t, "YLK")
	done := stateByName(t, e, p.ID, "Done")
	number := 0
	statements := func(issues int) int64 {
		var least int64 = -1
		for run := range 3 {
			st, err := e.svc.CreateState(ctx, e.users.Admin, p.ID, project.StateInput{
				Name: fmt.Sprintf("Review %d/%d", issues, run), Type: project.Started, Color: "#ff0000",
			})
			if err != nil {
				t.Fatal(err)
			}
			for range issues {
				number++
				insertIssue(t, e, p.ID, st.ID, number, false)
			}
			before := counter.n.Load()
			if err := svc.DeleteState(ctx, e.users.Admin, st.ID, &done.ID); err != nil {
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
		t.Errorf("statements deleting a state holding 2 issues = %d, holding 10 = %d; want equal", two, ten)
	}
}
