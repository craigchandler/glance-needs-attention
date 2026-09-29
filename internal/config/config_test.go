package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDurationsAndDefaults(t *testing.T) {
	for _, tc := range []struct{ name, body, errorField string }{
		{"defaults", `{}`, ""},
		{"positive", `{"refreshInterval":"500ms","httpTimeout":"2s"}`, ""},
		{"zero refresh", `{"refreshInterval":"0s"}`, "refreshInterval"},
		{"negative refresh", `{"refreshInterval":"-1s"}`, "refreshInterval"},
		{"invalid refresh", `{"refreshInterval":"never"}`, "refreshInterval"},
		{"zero timeout", `{"httpTimeout":"0"}`, "httpTimeout"},
		{"negative timeout", `{"httpTimeout":"-1m"}`, "httpTimeout"},
		{"invalid timeout", `{"httpTimeout":"forever"}`, "httpTimeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if tc.errorField != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errorField) {
					t.Fatalf("expected %s error, got %v", tc.errorField, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.ListenAddr != "127.0.0.1:8795" || cfg.RefreshDuration() <= 0 || cfg.HTTPTimeoutDuration() <= 0 {
				t.Fatalf("unsafe defaults: %+v", cfg)
			}
		})
	}
}

func TestExampleConfigurations(t *testing.T) {
	for _, path := range []string{"../../config.example.json", "../../config.generic-example.json"} {
		if _, err := Load(path); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}
