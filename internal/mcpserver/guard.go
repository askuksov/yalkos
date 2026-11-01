package mcpserver

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/askuksov/yalkos/internal/authn"
)

const (
	forbiddenBody    = `{"error":"origin not allowed"}`
	unauthorizedBody = `{"error":"API key required: send Authorization: Bearer <key> (create one under Settings → API keys)"}`
)

// Guard protects the MCP endpoint: a request with a foreign Origin gets 403
// (before auth, so a cookie from another site never reaches the auth
// middleware), and only callers authenticated by an API key reach next —
// a session cookie alone gets 401.
func Guard(baseURL *url.URL, auth func(http.Handler) http.Handler, next http.Handler) http.Handler {
	origin := baseURL.Scheme + "://" + baseURL.Host
	inner := auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a, ok := authn.ActorFrom(r.Context()); !ok || a.Via != "api_key" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="yalkos"`)
			writeRaw(w, http.StatusUnauthorized, unauthorizedBody)
			return
		}
		next.ServeHTTP(w, r)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" && !strings.EqualFold(o, origin) {
			writeRaw(w, http.StatusForbidden, forbiddenBody)
			return
		}
		inner.ServeHTTP(w, r)
	})
}

func writeRaw(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body + "\n"))
}
