// Package config loads runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// MaxAPIRateLimit is the highest accepted API_RATE_LIMIT. One million requests
// per hour gives an exact refill interval of 3.6 ms per token, so the
// limiter's bucket capacity equals the limit.
const MaxAPIRateLimit = 1_000_000

// Config is the process configuration.
type Config struct {
	// DatabaseURL is the PostgreSQL connection string (DATABASE_URL, required).
	DatabaseURL string
	// ListenAddr is the HTTP listen address (LISTEN_ADDR, default ":8080").
	ListenAddr string
	// LogLevel is the minimum log level (LOG_LEVEL: debug|info|warn|error, default info).
	LogLevel slog.Level
	// BaseURL is the public URL of the app (BASE_URL, default http://localhost:8080),
	// used for links and to decide whether cookies are Secure.
	BaseURL *url.URL
	// DataDir holds uploaded files (DATA_DIR, default ./data).
	DataDir string
	// MaxUploadMB caps one uploaded file (MAX_UPLOAD_MB, 1–1024, default 25).
	MaxUploadMB int
	// APIRateLimit is the number of API-key requests per hour and user
	// (API_RATE_LIMIT, 0–1000000, default 5000; 0 turns the limit off).
	APIRateLimit int
	// WebhookAllowPrivate lets webhooks target loopback and private networks
	// (WEBHOOK_ALLOW_PRIVATE: true|false, default true). Link-local and
	// metadata addresses are blocked either way.
	WebhookAllowPrivate bool
	// TrustedProxies lists the addresses and CIDR ranges of reverse proxies whose
	// X-Forwarded-For header is believed (TRUSTED_PROXIES, comma-separated, default
	// empty: the header is ignored). A bare address means a single host; host bits
	// of a range are cleared.
	TrustedProxies []netip.Prefix
}

// Load reads the configuration through getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		DatabaseURL: getenv("DATABASE_URL"),
		ListenAddr:  getenv("LISTEN_ADDR"),
		LogLevel:    slog.LevelInfo,
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}
	if lvl := getenv("LOG_LEVEL"); lvl != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(lvl)); err != nil {
			return Config{}, fmt.Errorf("LOG_LEVEL: unknown level %q", lvl)
		}
	}
	base, err := parseBaseURL(getenv("BASE_URL"))
	if err != nil {
		return Config{}, err
	}
	cfg.BaseURL = base
	cfg.DataDir = getenv("DATA_DIR")
	if cfg.DataDir == "" {
		cfg.DataDir = "./data"
	}
	cfg.MaxUploadMB = 25
	if v := getenv("MAX_UPLOAD_MB"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1024 {
			return Config{}, fmt.Errorf("MAX_UPLOAD_MB: %q must be a number of megabytes from 1 to 1024", v)
		}
		cfg.MaxUploadMB = n
	}
	cfg.APIRateLimit = 5000
	if v := getenv("API_RATE_LIMIT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > MaxAPIRateLimit {
			return Config{}, fmt.Errorf("API_RATE_LIMIT must be a whole number from 0 to %d", MaxAPIRateLimit)
		}
		cfg.APIRateLimit = n
	}
	cfg.WebhookAllowPrivate = true
	switch v := getenv("WEBHOOK_ALLOW_PRIVATE"); v {
	case "", "true":
	case "false":
		cfg.WebhookAllowPrivate = false
	default:
		return Config{}, errors.New("WEBHOOK_ALLOW_PRIVATE must be true or false")
	}
	if v := getenv("TRUSTED_PROXIES"); v != "" {
		cfg.TrustedProxies, err = parseTrustedProxies(v)
		if err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

// parseTrustedProxies splits a comma-separated list of IP addresses and CIDR ranges.
func parseTrustedProxies(raw string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		bad := fmt.Errorf("TRUSTED_PROXIES: %q is not an IP address or CIDR range", item)
		p, err := netip.ParsePrefix(item)
		if err != nil {
			a, aerr := netip.ParseAddr(item)
			if aerr != nil || a.Zone() != "" {
				return nil, bad
			}
			// An IPv4-mapped IPv6 address is the same host as its IPv4 form.
			a = a.Unmap()
			p = netip.PrefixFrom(a, a.BitLen())
		}
		// Peers are unmapped before matching, so a mapped range must be IPv4.
		if p.Addr().Is4In6() {
			if p.Bits() < 96 {
				return nil, bad
			}
			p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

func parseBaseURL(raw string) (*url.URL, error) {
	if raw == "" {
		raw = "http://localhost:8080"
	}
	u, err := url.Parse(raw)
	bad := err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != ""
	if bad {
		return nil, fmt.Errorf("BASE_URL: %q must be an absolute http(s) URL without a path", raw)
	}
	// Normalize so it compares equal to the browser's Origin header.
	u.Scheme = strings.ToLower(u.Scheme)
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	u.Host = host
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	}
	u.Path = ""
	return u, nil
}
