// Package testdb gives each test its own throwaway PostgreSQL database.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// New creates database yalkos_test_<8 hex> using TEST_DATABASE_URL (an admin URL,
// usually to the "postgres" database) and drops it when the test ends.
// Without TEST_DATABASE_URL the test is skipped locally and fails on CI.
func New(t *testing.T) (dbURL string, pool *pgxpool.Pool) {
	t.Helper()
	admin := os.Getenv("TEST_DATABASE_URL")
	if admin == "" {
		if os.Getenv("CI") == "true" {
			t.Fatal("TEST_DATABASE_URL is not set on CI")
		}
		t.Skip("TEST_DATABASE_URL not set; run `make db-up` and `make test`")
	}
	ctx := context.Background()

	b := make([]byte, 4)
	_, _ = rand.Read(b)
	name := "yalkos_test_" + hex.EncodeToString(b)

	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("testdb: connect admin: %v", err)
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		_ = conn.Close(ctx)
		t.Fatalf("testdb: create %s: %v", name, err)
	}
	_ = conn.Close(ctx)

	u, err := url.Parse(admin)
	if err != nil {
		t.Fatalf("testdb: parse TEST_DATABASE_URL: %v", err)
	}
	u.Path = "/" + name
	dbURL = u.String()

	pool, err = pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("testdb: open pool: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		c, err := pgx.Connect(ctx, admin)
		if err != nil {
			t.Errorf("testdb: connect admin for cleanup: %v", err)
			return
		}
		defer func() { _ = c.Close(ctx) }()
		if _, err := c.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("testdb: drop %s: %v", name, err)
		}
	})
	return dbURL, pool
}
