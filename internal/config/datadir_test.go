package config

import (
	"strings"
	"testing"
)

func TestDataDirConfig(t *testing.T) {
	cfg, err := Load(env(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "./data" || cfg.MaxUploadMB != 25 {
		t.Errorf("defaults = %q %d", cfg.DataDir, cfg.MaxUploadMB)
	}
	cfg, err = Load(env(map[string]string{"DATABASE_URL": "postgres://x", "DATA_DIR": "/data", "MAX_UPLOAD_MB": "100"}))
	if err != nil || cfg.DataDir != "/data" || cfg.MaxUploadMB != 100 {
		t.Errorf("overrides = %q %d, %v", cfg.DataDir, cfg.MaxUploadMB, err)
	}
	for _, bad := range []string{"0", "2000", "x", "-1"} {
		_, err := Load(env(map[string]string{"DATABASE_URL": "postgres://x", "MAX_UPLOAD_MB": bad}))
		if err == nil || !strings.Contains(err.Error(), "MAX_UPLOAD_MB") {
			t.Errorf("MAX_UPLOAD_MB=%s: err = %v", bad, err)
		}
	}
}
