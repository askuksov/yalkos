package project_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/testfx"
)

// Deleting a state that issues are moved out of costs the same number of
// statements whatever the number of issues: their history names are read
// once and the events are written in one statement.
func TestDeleteStateStatementsDoNotGrowWithIssues(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	counter := &testfx.StmtCounter{}
	pool := testfx.TracedPool(t, e.pool, counter)
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
			before := counter.N()
			if err := svc.DeleteState(ctx, e.users.Admin, st.ID, &done.ID); err != nil {
				t.Fatal(err)
			}
			if used := counter.N() - before; least < 0 || used < least {
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
