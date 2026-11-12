package label_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/testfx"
)

// Label structure changes and issue label writes run against each other
// through the real services. The only expected refusals are not_allowed
// conflicts of a structure change, named per call; a deadlock (40P01), a lock
// timeout or any other error fails.
//
// The test wires its own services so that it does not depend on other test
// files of this package.
func TestConcurrentLabelChangesAndIssueLabelWrites(t *testing.T) {
	const (
		issueCount   = 4
		writers      = 4
		writerRounds = 60
		moveRounds   = 80
		toggleRounds = 40
		cycleRounds  = 15
	)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, users, clock := testfx.New(t)
	projects := project.New(pool, clock)
	labels := label.New(pool, clock)
	issues := issue.New(pool, projects, clock)
	p, err := projects.Create(ctx, users.Admin, project.CreateInput{Key: "YLK", Name: "Yalkos"})
	if err != nil {
		t.Fatal(err)
	}

	newLabel := func(in label.CreateInput) label.Label {
		t.Helper()
		in.Color = "#AABBCC"
		l, err := labels.Create(ctx, users.Admin, in)
		if err != nil {
			t.Fatalf("create %q: %v", in.Name, err)
		}
		return l
	}
	yes := true
	g1 := newLabel(label.CreateInput{Name: "G1", IsGroup: true, Exclusive: &yes})
	g2 := newLabel(label.CreateInput{Name: "G2", IsGroup: true, Exclusive: &yes})
	groups := []label.Label{g1, g2}
	// Two labels start in each group and two without one.
	var moving []uuid.UUID
	for i, parent := range []*uuid.UUID{&g1.ID, &g1.ID, &g2.ID, &g2.ID, nil, nil} {
		moving = append(moving, newLabel(label.CreateInput{Name: fmt.Sprintf("L%d", i), ParentID: parent}).ID)
	}
	var issueIDs []uuid.UUID
	for range issueCount {
		// Each issue also gets the default type, a label of the exclusive Type group.
		i, err := issues.Create(ctx, users.Member, issue.CreateInput{ProjectID: &p.ID, Title: "t"})
		if err != nil {
			t.Fatal(err)
		}
		issueIDs = append(issueIDs, i.ID)
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		failures []string
		refused  = map[string]int{}
		done     = map[string]int{}
	)
	// check records the outcome of one call; allowed names the input fields
	// whose not_allowed refusal the call may meet.
	check := func(what string, err error, allowed ...string) {
		mu.Lock()
		defer mu.Unlock()
		if err == nil {
			done[what]++
			return
		}
		if f, ok := refusedConflict(err, allowed...); ok {
			refused[what+" "+f]++
			return
		}
		failures = append(failures, fmt.Sprintf("%s: %v", what, err))
	}
	run := func(seed uint64, rounds int, fn func(r *rand.Rand, round int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewPCG(seed, seed))
			for round := range rounds {
				if ctx.Err() != nil {
					return
				}
				fn(r, round)
			}
		}()
	}

	// Issue writers add one label and remove another on random issues: both
	// labels, the issue's current ones and all their groups get locked.
	for w := range writers {
		run(uint64(w+1), writerRounds, func(r *rand.Rand, _ int) {
			add := moving[r.IntN(len(moving))]
			remove := moving[r.IntN(len(moving))]
			_, err := issues.Update(ctx, users.Member, issueIDs[r.IntN(len(issueIDs))], issue.Patch{
				AddLabelIDs: []uuid.UUID{add}, RemoveLabelIDs: []uuid.UUID{remove},
			})
			check("issue label write", err)
		})
	}
	// Regroups among G1, G2 and no group; a move into an exclusive group that
	// would give an issue two of its labels is refused.
	run(101, moveRounds, func(r *rand.Rand, _ int) {
		targets := []*uuid.UUID{&g1.ID, &g2.ID, nil}
		_, err := labels.Update(ctx, users.Admin, moving[r.IntN(len(moving))], label.Patch{
			ParentID: kit.Opt[uuid.UUID]{Set: true, Value: targets[r.IntN(len(targets))]},
		})
		check("regroup", err, "parentId")
	})
	// Exclusivity toggles; making a group exclusive while an issue has two of
	// its labels is refused.
	run(201, toggleRounds, func(r *rand.Rand, _ int) {
		excl := r.IntN(2) == 0
		_, err := labels.Update(ctx, users.Admin, groups[r.IntN(len(groups))].ID, label.Patch{Exclusive: &excl})
		check("exclusivity change", err, "exclusive")
	})
	// A label is created in a group, applied to an issue and deleted.
	run(301, cycleRounds, func(r *rand.Rand, round int) {
		g := groups[r.IntN(len(groups))]
		l, err := labels.Create(ctx, users.Admin, label.CreateInput{
			Name: fmt.Sprintf("Temp%d", round), Color: "#AABBCC", ParentID: &g.ID,
		})
		check("create", err)
		if err != nil {
			return
		}
		_, err = issues.Update(ctx, users.Member, issueIDs[r.IntN(len(issueIDs))], issue.Patch{
			AddLabelIDs: []uuid.UUID{l.ID},
		})
		check("apply new label", err)
		check("delete", labels.Delete(ctx, users.Admin, l.ID))
	})
	wg.Wait()

	t.Logf("succeeded: %v; refused: %v", done, refused)
	for _, f := range failures {
		t.Error(f)
	}
	if ctx.Err() != nil {
		t.Fatalf("the run did not finish in time: %v", ctx.Err())
	}
	for _, what := range []string{"issue label write", "regroup", "exclusivity change", "delete"} {
		if done[what] == 0 {
			t.Errorf("no %s succeeded", what)
		}
	}

	// No issue has two labels of one exclusive group.
	var twice int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM (
		SELECT il.issue_id FROM issue_labels il
		JOIN labels l ON l.id = il.label_id JOIN labels g ON g.id = l.parent_id
		WHERE g.exclusive GROUP BY il.issue_id, g.id HAVING count(*) > 1) x`).Scan(&twice); err != nil {
		t.Fatal(err)
	}
	if twice > 0 {
		t.Errorf("%d issue/group pairs have two labels of an exclusive group", twice)
	}
	// exclusive_group_id is the label's current group when that group is
	// exclusive, otherwise null.
	var stale int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue_labels il
		JOIN labels l ON l.id = il.label_id LEFT JOIN labels g ON g.id = l.parent_id
		WHERE il.exclusive_group_id IS DISTINCT FROM CASE WHEN g.exclusive THEN g.id END`).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale > 0 {
		t.Errorf("%d issue labels have a stale exclusive_group_id", stale)
	}
}

// refusedConflict reports whether err is a not_allowed refusal of one of the
// given input fields, and which.
func refusedConflict(err error, fields ...string) (string, bool) {
	var ie *kit.InputError
	if !errors.As(err, &ie) || ie.Reason != kit.ReasonNotAllowed {
		return "", false
	}
	for _, f := range fields {
		if ie.Field == f {
			return f, true
		}
	}
	return "", false
}
