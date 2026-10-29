// Package config loads runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
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
	return cfg, nil
}

func parseBaseURL(raw string) (*url.URL, error) {
	if raw == "" {
		raw = "http://localhost:8080"
	}
	u, err := url.Parse(raw)
	bad := err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != ""
	if bad {
		return nil, fmt.Errorf("BASE_URL: %q must be an absolute http(s) URL without a path", raw)
	}
	u.Path = ""
	return u, nil
}
