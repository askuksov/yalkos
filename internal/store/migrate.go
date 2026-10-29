package store

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// withProvider runs fn with a goose provider over pool. Closing the provider's
// *sql.DB does not close the pgx pool.
func withProvider(pool *pgxpool.Pool, migrations fs.FS, locked bool, fn func(*goose.Provider) error) error {
	var opts []goose.ProviderOption
	if locked {
		// Postgres advisory lock: concurrent instances apply migrations one at a time.
		locker, err := lock.NewPostgresSessionLocker()
		if err != nil {
			return fmt.Errorf("create migration lock: %w", err)
		}
		opts = append(opts, goose.WithSessionLocker(locker))
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, stdlib.OpenDBFromPool(pool), migrations, opts...)
	if err != nil {
		return fmt.Errorf("init migrations: %w", err)
	}
	defer func() { _ = provider.Close() }()
	return fn(provider)
}

// Migrate applies all pending migrations under a Postgres advisory lock.
func Migrate(ctx context.Context, pool *pgxpool.Pool, migrations fs.FS) error {
	return withProvider(pool, migrations, true, func(p *goose.Provider) error {
		if _, err := p.Up(ctx); err != nil {
			return fmt.Errorf("apply migrations: %w", err)
		}
		return nil
	})
}

// MigrationStatus returns the applied (current) and newest embedded (latest) versions.
func MigrationStatus(ctx context.Context, pool *pgxpool.Pool, migrations fs.FS) (current, latest int64, err error) {
	err = withProvider(pool, migrations, false, func(p *goose.Provider) error {
		current, latest, err = p.GetVersions(ctx)
		if err != nil {
			return fmt.Errorf("read migration status: %w", err)
		}
		return nil
	})
	return current, latest, err
}

// Ready reports whether the database answers and all migrations are applied.
func Ready(ctx context.Context, pool *pgxpool.Pool, migrations fs.FS) error {
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("database unreachable: %w", err)
	}
	current, latest, err := MigrationStatus(ctx, pool, migrations)
	if err != nil {
		return err
	}
	if current != latest {
		return fmt.Errorf("migrations pending: current %d, latest %d", current, latest)
	}
	return nil
}
