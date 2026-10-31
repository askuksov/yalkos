// Package config loads runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
)

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
	return cfg, nil
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
