package issue_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/domain/comment"
	"github.com/askuksov/yalkos/internal/domain/issue"
)

// Writers lock an issue's project, then the issue. These tests move an issue
// to another project while a write waits for the old project, and let a third
// session hold the new project and then ask for the issue: the write must
// release the issue before it waits for the new project, or the two deadlock.

// session is a transaction that takes locks like a concurrent writer.
type session struct {
	t   *testing.T
	tx  pgx.Tx
	pid int
}

func begin(t *testing.T, e env) session {
	t.Helper()
	ctx := context.Background()
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	s := session{t: t, tx: tx}
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '5s'`); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&s.pid); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s session) exec(sql string, args ...any) error {
	_, err := s.tx.Exec(context.Background(), sql, args...)
	return err
}

func (s session) must(sql string, args ...any) {
	s.t.Helper()
	if err := s.exec(sql, args...); err != nil {
		s.t.Fatal(err)
	}
}

func (s session) commit() {
	s.t.Helper()
	if err := s.tx.Commit(context.Background()); err != nil {
		s.t.Fatal(err)
	}
}

// waitBlockedBy waits until some session waits for a lock that s holds.
func waitBlockedBy(t *testing.T, e env, s session) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		var n int
		if err := e.pool.QueryRow(context.Background(),
			`SELECT count(*) FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid))`, s.pid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			return
		}
	}
	t.Fatalf("no session waits for session %d", s.pid)
}

// movedMeanwhile runs write against an issue of YLK that another session moves
// to WEB while write waits, with a third session holding WEB and then asking
// for the issue.
func movedMeanwhile(t *testing.T, e env, write func(issue.Issue) error) issue.Issue {
	t.Helper()
	web := e.project(t, "WEB")
	i := e.createIn(t, e.ylk.ID, "Moving target")
	todo := e.state(t, web.ID, "Todo")

	move := begin(t, e) // a move in progress
	move.must(`SELECT 1 FROM projects WHERE id = $1 FOR UPDATE`, e.ylk.ID)
	move.must(`SELECT 1 FROM issues WHERE id = $1 FOR UPDATE`, i.ID)
	move.must(`UPDATE issues SET project_id = $1, state_id = $2, number = 999 WHERE id = $3`, web.ID, todo.ID, i.ID)
	// NO KEY UPDATE, unlike UPDATE, does not wait for the key share the move's
	// foreign key holds on WEB, and still blocks a writer locking WEB.
	other := begin(t, e)
	other.must(`SELECT 1 FROM projects WHERE id = $1 FOR NO KEY UPDATE`, web.ID)

	done := make(chan error, 1)
	go func() { done <- write(i) }()
	waitBlockedBy(t, e, move)
	move.commit()
	waitBlockedBy(t, e, other)
	if err := other.exec(`SELECT 1 FROM issues WHERE id = $1 FOR UPDATE`, i.ID); err != nil {
		t.Fatalf("third session locking the issue: %v", err)
	}
	other.commit()
	if err := <-done; err != nil {
		t.Fatalf("write: %v", err)
	}
	return i
}

func TestUpdateStartsOverWhenIssueMovedMeanwhile(t *testing.T) {
	e := newEnv(t)
	i := movedMeanwhile(t, e, func(i issue.Issue) error {
		_, err := e.issues.Update(context.Background(), e.users.Member, i.ID, issue.Patch{Title: ptr("Renamed")})
		return err
	})
	got, err := e.issues.Get(context.Background(), i.ID.String())
	if err != nil || got.Title != "Renamed" {
		t.Errorf("issue = %+v, %v; want title Renamed", got, err)
	}
}

func TestMoveStartsOverWhenIssueMovedMeanwhile(t *testing.T) {
	e := newEnv(t)
	ops := e.project(t, "OPS")
	i := movedMeanwhile(t, e, func(i issue.Issue) error {
		_, err := e.issues.Move(context.Background(), e.users.Member, i.ID, ops.ID, nil)
		return err
	})
	got, err := e.issues.Get(context.Background(), i.ID.String())
	if err != nil || got.ProjectID != ops.ID {
		t.Errorf("issue = %+v, %v; want project OPS", got, err)
	}
}

// LockForWrite runs inside another service's transaction, which starts over too.
func TestCommentStartsOverWhenIssueMovedMeanwhile(t *testing.T) {
	e := newEnv(t)
	comments := comment.New(e.pool, e.issues, nil, e.clock)
	movedMeanwhile(t, e, func(i issue.Issue) error {
		_, err := comments.Create(context.Background(), e.users.Member, i.ID, "Looks good", nil)
		return err
	})
}
