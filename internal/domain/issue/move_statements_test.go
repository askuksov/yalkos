package issue_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/testfx"
)

// Moving a subtree reads the history names once and writes the history
// entries of all its issues in one statement, however many issues it has.
func TestMoveSubtreeWritesHistoryOnce(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	web := e.project(t, "WEB")
	var mu sync.Mutex
	seen := map[string]int{}
	counter := &testfx.StmtCounter{Ended: func(sql string) {
		for _, name := range []string{"AuditNames", "InsertAuditEvents"} {
			if strings.Contains(sql, "-- name: "+name+" ") {
				mu.Lock()
				seen[name]++
				mu.Unlock()
			}
		}
	}}
	svc := issue.New(testfx.TracedPool(t, e.pool, counter), e.projects, e.clock)

	for _, children := range []int{1, 6} {
		root := e.create(t, "root")
		ids := []uuid.UUID{root.ID}
		for i := range children {
			c, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ParentID: &root.ID, Title: fmt.Sprintf("child %d", i)})
			if err != nil {
				t.Fatal(err)
			}
			ids = append(ids, c.ID)
		}
		mu.Lock()
		clear(seen)
		mu.Unlock()
		if _, err := svc.Move(ctx, e.users.Member, root.ID, web.ID, nil); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		names, inserts := seen["AuditNames"], seen["InsertAuditEvents"]
		mu.Unlock()
		if names != 1 || inserts != 1 {
			t.Fatalf("%d children: names lookups %d, history inserts %d; want 1 and 1", children, names, inserts)
		}
		for _, id := range ids {
			h := e.history(t, id)
			if last := h[len(h)-1]; !strings.Contains(changeFields(last), "projectId") || len(last.Names) == 0 {
				t.Fatalf("issue %s: last history entry %+v, want the move with names", id, last)
			}
		}
	}
}
