package store_test

import (
	"context"
	"errors"
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
	if current != 1 || latest != 1 {
		t.Errorf("status = current %d, latest %d; want 1, 1", current, latest)
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
	if err != nil || current != 1 {
		t.Fatalf("current = %d, err = %v; want 1, nil", current, err)
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
	if !errors.Is(err, store.ErrMigrationsPending) || !strings.Contains(err.Error(), "current 0, latest 1") {
		t.Fatalf("Ready before migrate = %v, want ErrMigrationsPending (current 0, latest 1)", err)
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
