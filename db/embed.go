// Package db embeds the SQL migrations applied at startup.
package db

import (
	"embed"
	"io/fs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations returns the goose migration files; the root contains NNNNN_name.sql files.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		// Only possible if the embed directive above is changed incorrectly.
		panic(err)
	}
	return sub
}
