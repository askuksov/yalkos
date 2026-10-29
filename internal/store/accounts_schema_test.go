package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/db"
	"github.com/askuksov/yalkos/internal/store"
	"github.com/askuksov/yalkos/internal/testdb"
)

func migrated(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_, pool := testdb.New(t)
	if err := store.Migrate(context.Background(), pool, db.Migrations()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return pool
}

func wantUniqueViolation(t *testing.T, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("err = %v, want unique violation 23505", err)
	}
}

func insertWorkspace(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO workspaces (name) VALUES ('w') RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertUser(pool *pgxpool.Pool, ws, email, role string) (string, error) {
	var id string
	err := pool.QueryRow(context.Background(),
		`INSERT INTO users (workspace_id, email, name, password_hash, role)
		 VALUES ($1, $2, 'n', 'h', $3) RETURNING id::text`, ws, email, role).Scan(&id)
	return id, err
}

func TestWorkspaceSingleton(t *testing.T) {
	pool := migrated(t)
	insertWorkspace(t, pool)
	_, err := pool.Exec(context.Background(), `INSERT INTO workspaces (name) VALUES ('second')`)
	wantUniqueViolation(t, err)
}

func TestUserEmailCaseInsensitive(t *testing.T) {
	pool := migrated(t)
	ws := insertWorkspace(t, pool)
	if _, err := insertUser(pool, ws, "a@x.io", "member"); err != nil {
		t.Fatal(err)
	}
	_, err := insertUser(pool, ws, "A@X.io", "member")
	wantUniqueViolation(t, err)
}

func TestSingleOwner(t *testing.T) {
	pool := migrated(t)
	ws := insertWorkspace(t, pool)
	if _, err := insertUser(pool, ws, "o1@x.io", "owner"); err != nil {
		t.Fatal(err)
	}
	_, err := insertUser(pool, ws, "o2@x.io", "owner")
	wantUniqueViolation(t, err)
}

func TestIDsAreUUIDv7(t *testing.T) {
	pool := migrated(t)
	ws := insertWorkspace(t, pool)
	id, err := insertUser(pool, ws, "v7@x.io", "member")
	if err != nil {
		t.Fatal(err)
	}
	// Version nibble is the first hex digit of the third group: xxxxxxxx-xxxx-7xxx-...
	if len(id) != 36 || id[14] != '7' {
		t.Errorf("user id %q is not UUIDv7", id)
	}
	if ws[14] != '7' {
		t.Errorf("workspace id %q is not UUIDv7", ws)
	}
}
