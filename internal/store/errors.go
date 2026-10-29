package store

import "errors"

// Readiness failure classes. Callers may show these names publicly; the wrapped
// details (hosts, users, driver messages) must only go to server logs.
var (
	ErrDatabaseUnavailable = errors.New("database unavailable")
	ErrMigrationsPending   = errors.New("migrations pending")
)
