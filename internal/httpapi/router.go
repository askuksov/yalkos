// Package httpapi wires HTTP routes: health checks, both GraphQL endpoints and the web UI.
package httpapi

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
)

// maxGraphQLBody caps GraphQL request bodies (1 MiB) so one request cannot exhaust memory.
const maxGraphQLBody = 1 << 20

// Deps are the handlers and services the router needs.
type Deps struct {
	// Ready reports whether the app can serve traffic (DB reachable, migrations applied).
	Ready func(context.Context) error
	// UI is the built web app (index.html + assets/).
	UI fs.FS
	// NativeGraphQL serves /api/graphql, LinearGraphQL serves /graphql (ADR-006).
	NativeGraphQL http.Handler
	LinearGraphQL http.Handler
	Logger        *slog.Logger
}

// NewRouter returns the root HTTP handler.
func NewRouter(d Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", healthz)
	mux.Handle("GET /readyz", readyz(d.Ready))

	mux.Handle("/api/graphql", postOnly(d.NativeGraphQL))
	mux.Handle("/graphql", postOnly(d.LinearGraphQL))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})

	mux.Handle("GET /assets/", assets(d.UI))
	mux.Handle("/", spaIndex(d.UI))

	return logRequests(d.Logger, mux)
}

// postOnly rejects every method except POST with 405, so GET never falls through to the SPA,
// and caps the body at maxGraphQLBody (413 when the declared length is larger).
func postOnly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		if r.ContentLength > maxGraphQLBody {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxGraphQLBody)
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
