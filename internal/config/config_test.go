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
	for _, tc := range []struct {
		val  string
		want int
	}{{"0", 0}, {"120", 120}, {"1000000", 1000000}} {
		t.Run("accept "+tc.val, func(t *testing.T) {
			cfg, err := Load(env(map[string]string{"DATABASE_URL": "postgres://x", "API_RATE_LIMIT": tc.val}))
			if err != nil || cfg.APIRateLimit != tc.want {
				t.Errorf("%q = %d, %v; want %d", tc.val, cfg.APIRateLimit, err, tc.want)
			}
		})
	}
	for _, v := range []string{"-1", "abc", "1.5", "1000001", "99999999999999999999"} {
		t.Run("reject "+v, func(t *testing.T) {
			_, err := Load(env(map[string]string{"DATABASE_URL": "postgres://x", "API_RATE_LIMIT": v}))
			if err == nil || err.Error() != "API_RATE_LIMIT must be a whole number from 0 to 1000000" {
				t.Errorf("%q: err = %v", v, err)
			}
		})
	}
}

func TestLoadWebhookAllowPrivate(t *testing.T) {
	load := func(v string, set bool) (Config, error) {
		m := map[string]string{"DATABASE_URL": "postgres://x"}
		if set {
			m["WEBHOOK_ALLOW_PRIVATE"] = v
		}
		return Load(env(m))
	}
	for _, tc := range []struct {
		name string
		val  string
		set  bool
		want bool
	}{
		{"unset", "", false, true},
		{"true", "true", true, true},
		{"false", "false", true, false},
	} {
		cfg, err := load(tc.val, tc.set)
		if err != nil {
			t.Fatalf("%s: Load: %v", tc.name, err)
		}
		if cfg.WebhookAllowPrivate != tc.want {
			t.Errorf("%s: WebhookAllowPrivate = %v, want %v", tc.name, cfg.WebhookAllowPrivate, tc.want)
		}
	}
	for _, bad := range []string{"yes", "1", "FALSE", ""} {
		_, err := load(bad, bad != "")
		if bad == "" {
			// Positive control: an empty value is the same as unset.
			if err != nil {
				t.Errorf("empty value: %v, want no error", err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "WEBHOOK_ALLOW_PRIVATE must be true or false") {
			t.Errorf("%q: err = %v, want WEBHOOK_ALLOW_PRIVATE must be true or false", bad, err)
		}
	}
}
