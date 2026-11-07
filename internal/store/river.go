package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// RiverSchemaVersion is the River schema version created by the goose step
// returned from riverStep.
const RiverSchemaVersion = 8

// riverStep returns the goose Go migration (version 6) that creates and drops
// the River job-queue tables, so every caller of withProvider (app, cli,
// tests) gets them. River itself runs on pool; goose's *sql.DB is unused.
//
// The step is pinned to RiverSchemaVersion. When a River upgrade ships a newer
// schema version, add a new goose step for it; never edit this one.
func riverStep(pool *pgxpool.Pool) *goose.Migration {
	run := func(direction rivermigrate.Direction, target int) func(context.Context, *sql.DB) error {
		return func(ctx context.Context, _ *sql.DB) error {
			migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
			if err != nil {
				return fmt.Errorf("create river migrator: %w", err)
			}
			if _, err := migrator.Migrate(ctx, direction, &rivermigrate.MigrateOpts{TargetVersion: target}); err != nil {
				return fmt.Errorf("migrate river schema %s: %w", direction, err)
			}
			return nil
		}
	}
	return goose.NewGoMigration(6,
		&goose.GoFunc{RunDB: run(rivermigrate.DirectionUp, RiverSchemaVersion)},
		&goose.GoFunc{RunDB: run(rivermigrate.DirectionDown, -1)},
	)
}
