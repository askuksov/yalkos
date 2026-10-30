package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func wantPgCode(t *testing.T, err error, code string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Fatalf("err = %v, want SQLSTATE %s", err, code)
	}
}

func scanID(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&id); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return id
}

func insertProject(t *testing.T, pool *pgxpool.Pool, ws, key string) string {
	t.Helper()
	return scanID(t, pool, `INSERT INTO projects (workspace_id, key, name, created_at, updated_at)
		VALUES ($1, $2, $2, now(), now()) RETURNING id::text`, ws, key)
}

func insertState(pool *pgxpool.Pool, project, name string, isDefault bool) (string, error) {
	var id string
	err := pool.QueryRow(context.Background(), `INSERT INTO states (project_id, name, type, color, position, is_default, created_at, updated_at)
		VALUES ($1, $2, 'unstarted', '#e2e2e2', 0, $3, now(), now()) RETURNING id::text`, project, name, isDefault).Scan(&id)
	return id, err
}

func mustState(t *testing.T, pool *pgxpool.Pool, project, name string) string {
	t.Helper()
	id, err := insertState(pool, project, name, false)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

const insertIssueSQL = `INSERT INTO issues (project_id, number, title, state_id, priority, creator_id, parent_id, sort_order, created_at, updated_at)
	VALUES ($1, $2, 't', $3, $4, $5, $6, 0, now(), now()) RETURNING id::text`

type schemaFixture struct {
	pool   *pgxpool.Pool
	user   string
	p, q   string // project ids
	pState string
	qState string
}

func newSchemaFixture(t *testing.T) schemaFixture {
	t.Helper()
	pool := migrated(t)
	ws := insertWorkspace(t, pool)
	user, err := insertUser(pool, ws, "u@x.io", "owner")
	if err != nil {
		t.Fatal(err)
	}
	f := schemaFixture{pool: pool, user: user, p: insertProject(t, pool, ws, "P"), q: insertProject(t, pool, ws, "Q")}
	f.pState = mustState(t, pool, f.p, "Todo")
	f.qState = mustState(t, pool, f.q, "Todo")
	return f
}

func TestIssueStateFromOtherProject(t *testing.T) {
	f := newSchemaFixture(t)
	var id string
	err := f.pool.QueryRow(context.Background(), insertIssueSQL, f.p, 1, f.qState, 0, f.user, nil).Scan(&id)
	wantPgCode(t, err, "23503")
}

func TestParentFromOtherProject(t *testing.T) {
	f := newSchemaFixture(t)
	ctx := context.Background()
	parent := scanID(t, f.pool, insertIssueSQL, f.q, 1, f.qState, 0, f.user, nil)
	err := pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error {
		var id string
		// Deferred FK: the insert succeeds, COMMIT fails.
		if err := tx.QueryRow(ctx, insertIssueSQL, f.p, 1, f.pState, 0, f.user, parent).Scan(&id); err != nil {
			t.Fatalf("insert before commit: %v", err)
		}
		return nil
	})
	wantPgCode(t, err, "23503")
}

func TestOneDefaultState(t *testing.T) {
	f := newSchemaFixture(t)
	if _, err := insertState(f.pool, f.p, "A", true); err != nil {
		t.Fatal(err)
	}
	_, err := insertState(f.pool, f.p, "B", true)
	wantPgCode(t, err, "23505")
	if _, err := insertState(f.pool, f.q, "B", true); err != nil {
		t.Fatalf("default in another project: %v", err)
	}
}

func TestStateNameCaseInsensitive(t *testing.T) {
	f := newSchemaFixture(t)
	_, err := insertState(f.pool, f.p, "TODO", false)
	wantPgCode(t, err, "23505")
}

func TestPriorityRange(t *testing.T) {
	f := newSchemaFixture(t)
	var id string
	err := f.pool.QueryRow(context.Background(), insertIssueSQL, f.p, 1, f.pState, 5, f.user, nil).Scan(&id)
	wantPgCode(t, err, "23514")
}

func TestDeleteProjectCascades(t *testing.T) {
	f := newSchemaFixture(t)
	ctx := context.Background()
	issue := scanID(t, f.pool, insertIssueSQL, f.p, 1, f.pState, 0, f.user, nil)
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO issue_numbers (project_id, number, issue_id) VALUES ($1, 1, $2)`, []any{f.p, issue}},
		{`INSERT INTO project_keys (key, project_id) VALUES ('P', $1)`, []any{f.p}},
		{`INSERT INTO project_members (project_id, user_id, created_at) VALUES ($1, $2, now())`, []any{f.p, f.user}},
	} {
		if _, err := f.pool.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("%s: %v", s.sql, err)
		}
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, f.p); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"states", "issues", "issue_numbers", "project_keys", "project_members"} {
		var n int
		if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE project_id = $1`, f.p).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s: %d rows left", table, n)
		}
	}
}
