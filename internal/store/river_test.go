package store_test

import (
	"context"
	"testing"

	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/askuksov/yalkos/db"
	"github.com/askuksov/yalkos/internal/store"
	"github.com/askuksov/yalkos/internal/testdb"
)

func TestMigrateCreatesRiverTables(t *testing.T) {
	ctx := context.Background()
	_, pool := testdb.New(t)

	if err := store.Migrate(ctx, pool, db.Migrations()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	for _, table := range []string{"river_job", "river_leader", "river_queue", "river_migration"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass('public.' || $1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("table %s does not exist", table)
		}
	}
	var version int
	if err := pool.QueryRow(ctx, `SELECT max(version) FROM river_migration`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != store.RiverSchemaVersion {
		t.Errorf("river_migration max version = %d, want %d", version, store.RiverSchemaVersion)
	}
}

// The goose step pins the River schema version; a River upgrade that ships a
// newer schema must add a new step instead of silently changing this one.
func TestRiverSchemaVersionPinned(t *testing.T) {
	migrator, err := rivermigrate.New(riverpgxv5.New(nil), nil)
	if err != nil {
		t.Fatalf("rivermigrate.New: %v", err)
	}
	if got := len(migrator.AllVersions()); got != store.RiverSchemaVersion {
		t.Errorf("River ships %d schema versions, pinned step targets %d", got, store.RiverSchemaVersion)
	}
}

func TestMigrationStatusCountsRiverStep(t *testing.T) {
	ctx := context.Background()
	_, pool := testdb.New(t)

	if err := store.Migrate(ctx, pool, db.Migrations()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	current, latest, err := store.MigrationStatus(ctx, pool, db.Migrations())
	if err != nil {
		t.Fatalf("MigrationStatus: %v", err)
	}
	if current != 6 || latest != 6 {
		t.Errorf("status = current %d, latest %d; want 6, 6", current, latest)
	}
}
