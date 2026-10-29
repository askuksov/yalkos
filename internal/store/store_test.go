package store_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/askuksov/yalkos/db"
	"github.com/askuksov/yalkos/internal/store"
	"github.com/askuksov/yalkos/internal/testdb"
)

func TestMigrateAppliesAll(t *testing.T) {
	ctx := context.Background()
	_, pool := testdb.New(t)

	if err := store.Migrate(ctx, pool, db.Migrations()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	current, latest, err := store.MigrationStatus(ctx, pool, db.Migrations())
	if err != nil {
		t.Fatalf("MigrationStatus: %v", err)
	}
	want := latestVersion(t)
	if current != want || latest != want {
		t.Errorf("status = current %d, latest %d; want %d, %d", current, latest, want, want)
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.workspaces') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Error("table workspaces does not exist")
	}
}

func TestMigrateIdempotent(t *testing.T) {
	ctx := context.Background()
	_, pool := testdb.New(t)

	for i := range 2 {
		if err := store.Migrate(ctx, pool, db.Migrations()); err != nil {
			t.Fatalf("Migrate #%d: %v", i+1, err)
		}
	}
	current, _, err := store.MigrationStatus(ctx, pool, db.Migrations())
	if want := latestVersion(t); err != nil || current != want {
		t.Fatalf("current = %d, err = %v; want %d, nil", current, err, want)
	}
}

func TestMigrateConcurrent(t *testing.T) {
	ctx := context.Background()
	url, pool := testdb.New(t)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Go(func() {
			p, err := store.Connect(ctx, url)
			if err != nil {
				errs[i] = err
				return
			}
			defer p.Close()
			errs[i] = store.Migrate(ctx, p, db.Migrations())
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("Migrate #%d: %v", i+1, err)
		}
	}
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM goose_db_version WHERE version_id = 1 AND is_applied`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("version 1 applied %d times, want 1", n)
	}
}

func TestReady(t *testing.T) {
	ctx := context.Background()
	_, pool := testdb.New(t)

	err := store.Ready(ctx, pool, db.Migrations())
	wantMsg := fmt.Sprintf("current 0, latest %d", latestVersion(t))
	if !errors.Is(err, store.ErrMigrationsPending) || !strings.Contains(err.Error(), wantMsg) {
		t.Fatalf("Ready before migrate = %v, want ErrMigrationsPending (%s)", err, wantMsg)
	}
	if err := store.Migrate(ctx, pool, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	if err := store.Ready(ctx, pool, db.Migrations()); err != nil {
		t.Fatalf("Ready after migrate = %v, want nil", err)
	}
}

func TestConnectUnreachable(t *testing.T) {
	start := time.Now()
	_, err := store.Connect(context.Background(), "postgres://u:p@127.0.0.1:1/x?sslmode=disable")
	if err == nil {
		t.Fatal("Connect to closed port succeeded")
	}
	if d := time.Since(start); d > 6*time.Second {
		t.Errorf("Connect took %v, want ≤ 6s", d)
	}
}

// latestVersion is the number of embedded migrations (files are numbered 00001, 00002, ...).
func latestVersion(t *testing.T) int64 {
	t.Helper()
	files, err := fs.Glob(db.Migrations(), "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	return int64(len(files))
}
