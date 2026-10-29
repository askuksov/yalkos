package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// logPath hides secrets carried in URL paths (invite tokens).
func logPath(p string) string {
	if strings.HasPrefix(p, "/invite/") {
		return "/invite/[redacted]"
	}
	return p
}

// logRequests writes one JSON line per request; health checks go to debug to keep logs quiet.
func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Invite links carry a secret token in the path; keep it out of referrers.
		w.Header().Set("Referrer-Policy", "no-referrer")
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		level := slog.LevelInfo
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			level = slog.LevelDebug
		}
		logger.LogAttrs(context.Background(), level, "request",
			slog.String("method", r.Method),
			slog.String("path", logPath(r.URL.Path)),
			slog.Int("status", rec.status),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
		)
	})
}
