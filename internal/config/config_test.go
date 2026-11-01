package config

import (
	"log/slog"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatabaseURL != "postgres://x" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want :8080", cfg.ListenAddr)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", cfg.LogLevel)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"DATABASE_URL": "postgres://x",
		"LISTEN_ADDR":  "127.0.0.1:9000",
		"LOG_LEVEL":    "debug",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ListenAddr != "127.0.0.1:9000" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want debug", cfg.LogLevel)
	}
}

func TestLoadMissingDatabaseURL(t *testing.T) {
	_, err := Load(env(nil))
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL is required") {
		t.Fatalf("err = %v, want DATABASE_URL is required", err)
	}
}

func TestLoadInvalidLogLevel(t *testing.T) {
	_, err := Load(env(map[string]string{"DATABASE_URL": "postgres://x", "LOG_LEVEL": "loud"}))
	if err == nil || !strings.Contains(err.Error(), `LOG_LEVEL: unknown level "loud"`) {
		t.Fatalf("err = %v, want LOG_LEVEL unknown level", err)
	}
}

func TestBaseURLDefaultAndValidation(t *testing.T) {
	cfg, err := Load(env(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err != nil || cfg.BaseURL.String() != "http://localhost:8080" {
		t.Fatalf("default BaseURL = %v, err %v", cfg.BaseURL, err)
	}
	cfg, err = Load(env(map[string]string{"DATABASE_URL": "postgres://x", "BASE_URL": "https://app.yalkos.com/"}))
	if err != nil || cfg.BaseURL.String() != "https://app.yalkos.com" {
		t.Errorf("https BaseURL = %v, err %v", cfg.BaseURL, err)
	}
	for _, bad := range []string{"ftp://x", "localhost", "https://x/path", "http://"} {
		_, err := Load(env(map[string]string{"DATABASE_URL": "postgres://x", "BASE_URL": bad}))
		if err == nil || !strings.Contains(err.Error(), "BASE_URL") {
			t.Errorf("%q: err = %v, want BASE_URL error", bad, err)
		}
	}
}

func TestBaseURLNormalized(t *testing.T) {
	cases := map[string]string{
		"HTTP://LOCALHOST:8080":       "http://localhost:8080",
		"https://App.Yalkos.com:443/": "https://app.yalkos.com",
		"http://localhost:80":         "http://localhost",
	}
	for in, want := range cases {
		cfg, err := Load(env(map[string]string{"DATABASE_URL": "postgres://x", "BASE_URL": in}))
		if err != nil || cfg.BaseURL.String() != want {
			t.Errorf("%q → %v (err %v), want %q", in, cfg.BaseURL, err, want)
		}
	}
	if _, err := Load(env(map[string]string{"DATABASE_URL": "postgres://x", "BASE_URL": "https://u:p@x.io"})); err == nil {
		t.Error("userinfo in BASE_URL accepted")
	}
}

func TestAPIRateLimitConfig(t *testing.T) {
	cfg, err := Load(env(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err != nil || cfg.APIRateLimit != 5000 {
		t.Fatalf("default = %d, %v; want 5000", cfg.APIRateLimit, err)
	}
	for v, want := range map[string]int{"0": 0, "120": 120} {
		cfg, err := Load(env(map[string]string{"DATABASE_URL": "postgres://x", "API_RATE_LIMIT": v}))
		if err != nil || cfg.APIRateLimit != want {
			t.Fatalf("%q = %d, %v; want %d", v, cfg.APIRateLimit, err, want)
		}
	}
	for _, v := range []string{"-1", "abc", "1.5"} {
		_, err := Load(env(map[string]string{"DATABASE_URL": "postgres://x", "API_RATE_LIMIT": v}))
		if err == nil || err.Error() != "API_RATE_LIMIT must be a non-negative integer" {
			t.Fatalf("%q: err = %v", v, err)
		}
	}
}
