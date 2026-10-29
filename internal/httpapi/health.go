package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/askuksov/yalkos/internal/store"
)

func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz answers 503 with a generic reason; the full error (hosts, users, driver
// messages) is logged server-side only.
func readyz(ready func(context.Context) error, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := ready(r.Context())
		if err == nil {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
			return
		}
		logger.Warn("not ready", slog.String("error", err.Error()))
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not ready", "error": publicReason(err)})
	})
}

func publicReason(err error) string {
	switch {
	case errors.Is(err, store.ErrDatabaseUnavailable):
		return store.ErrDatabaseUnavailable.Error()
	case errors.Is(err, store.ErrMigrationsPending):
		return store.ErrMigrationsPending.Error()
	default:
		return "not ready"
	}
}
