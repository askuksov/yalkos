// Package config loads runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
)

// Config is the process configuration.
type Config struct {
	// DatabaseURL is the PostgreSQL connection string (DATABASE_URL, required).
	DatabaseURL string
	// ListenAddr is the HTTP listen address (LISTEN_ADDR, default ":8080").
	ListenAddr string
	// LogLevel is the minimum log level (LOG_LEVEL: debug|info|warn|error, default info).
	LogLevel slog.Level
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
	return cfg, nil
}
