// Package authn authenticates HTTP requests: API key header or session cookie.
package authn

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/askuksov/yalkos/internal/auth"
	"github.com/askuksov/yalkos/internal/domain/account"
)

// CookieName is the session cookie.
const CookieName = "yalkos_session"

const sessionMaxAge = 30 * 24 * time.Hour

// Authenticator resolves credentials to users (implemented by account.Service).
type Authenticator interface {
	UserByAPIKey(ctx context.Context, key string) (account.User, error)
	UserBySession(ctx context.Context, token string) (account.User, error)
}

// Actor is the authenticated caller of a request.
type Actor struct {
	User account.User
	// Via is "api_key" or "session".
	Via string
	// SessionToken is set when Via == "session" (needed for logout and password change).
	SessionToken string
}

type ctxKey int

const (
	actorKey ctxKey = iota
	cookieKey
	ipKey
)

// ActorFrom returns the authenticated caller, if any.
func ActorFrom(ctx context.Context) (*Actor, bool) {
	a, ok := ctx.Value(actorKey).(*Actor)
	return a, ok && a != nil
}

// ClientIP returns the remote IP of the request (used for rate limits).
func ClientIP(ctx context.Context) string {
	ip, _ := ctx.Value(ipKey).(string)
	return ip
}

type cookieWriter struct {
	w      http.ResponseWriter
	secure bool
}

// SetSessionCookie sets the session cookie on the current response.
func SetSessionCookie(ctx context.Context, token string) {
	if cw, ok := ctx.Value(cookieKey).(*cookieWriter); ok {
		http.SetCookie(cw.w, &http.Cookie{
			Name: CookieName, Value: token, Path: "/", MaxAge: int(sessionMaxAge.Seconds()),
			HttpOnly: true, Secure: cw.secure, SameSite: http.SameSiteLaxMode,
		})
	}
}

// ClearSessionCookie removes the session cookie in the browser.
func ClearSessionCookie(ctx context.Context) {
	if cw, ok := ctx.Value(cookieKey).(*cookieWriter); ok {
		http.SetCookie(cw.w, &http.Cookie{
			Name: CookieName, Value: "", Path: "/", MaxAge: -1,
			HttpOnly: true, Secure: cw.secure, SameSite: http.SameSiteLaxMode,
		})
	}
}

// Middleware puts the caller (if any) into the request context.
// An Authorization header always wins: an invalid key means "anonymous", the cookie
// is not consulted. Cookie-authenticated unsafe requests from a foreign Origin
// get 403 (CSRF defence on top of SameSite=Lax; also surfaces a wrong BASE_URL).
func Middleware(a Authenticator, baseURL *url.URL, logger *slog.Logger) func(http.Handler) http.Handler {
	origin := baseURL.Scheme + "://" + baseURL.Host
	secure := baseURL.Scheme == "https"

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), cookieKey, &cookieWriter{w: w, secure: secure})
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			ctx = context.WithValue(ctx, ipKey, host)

			var actor *Actor
			if h := r.Header.Get("Authorization"); h != "" {
				if key, ok := auth.ParseAuthorization(h); ok {
					u, err := a.UserByAPIKey(ctx, key)
					switch {
					case err == nil:
						actor = &Actor{User: u, Via: "api_key"}
					case !errors.Is(err, account.ErrUnauthenticated):
						logger.Error("api key lookup failed", slog.String("error", err.Error()))
					}
				}
			} else if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
				unsafe := r.Method != http.MethodGet && r.Method != http.MethodHead
				if o := r.Header.Get("Origin"); unsafe && o != "" && !strings.EqualFold(o, origin) {
					logger.Warn("cross-origin request with session cookie rejected", slog.String("origin", o))
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"error":"request origin does not match BASE_URL"}`))
					return
				}
				u, err := a.UserBySession(ctx, c.Value)
				switch {
				case err == nil:
					actor = &Actor{User: u, Via: "session", SessionToken: c.Value}
				case !errors.Is(err, account.ErrUnauthenticated):
					logger.Error("session lookup failed", slog.String("error", err.Error()))
				}
			}
			if actor != nil {
				ctx = context.WithValue(ctx, actorKey, actor)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
