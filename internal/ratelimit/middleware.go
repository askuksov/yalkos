package ratelimit

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/google/uuid"
)

// Middleware limits the requests of the user returned by who. A nil limiter
// or who returning false passes the request through untouched. Limited
// requests get 429 with Retry-After and a body in the endpoint's error shape.
func Middleware(l *Limiter, who func(*http.Request) (uuid.UUID, bool)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if l == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := who(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			res := l.Take(user)
			h := w.Header()
			h.Set("X-RateLimit-Requests-Limit", strconv.Itoa(res.Limit))
			h.Set("X-RateLimit-Requests-Remaining", strconv.Itoa(res.Remaining))
			h.Set("X-RateLimit-Requests-Reset", strconv.FormatInt(res.Reset.UnixMilli(), 10))
			if res.Allowed {
				next.ServeHTTP(w, r)
				return
			}
			secs := int64(math.Ceil(res.RetryAfter.Seconds()))
			msg := fmt.Sprintf("Rate limit exceeded: %d requests per hour. Try again in %d seconds.", res.Limit, secs)
			h.Set("Retry-After", strconv.FormatInt(secs, 10))
			h.Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(body(r.URL.Path, msg))
		})
	}
}

func body(path, msg string) any {
	if path == "/graphql" || path == "/api/graphql" {
		return map[string]any{"errors": []any{map[string]any{
			"message": msg,
			"extensions": map[string]any{
				"code": "RATELIMITED", "type": "ratelimited", "userError": true, "userPresentableMessage": msg,
			},
		}}}
	}
	return map[string]string{"error": msg}
}
