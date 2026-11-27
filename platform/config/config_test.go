package config

import "testing"

func TestLoadRejectsPartialFeishuCredentials(t *testing.T) {
	t.Setenv("FEISHU_APP_ID", "cli_test")
	t.Setenv("FEISHU_APP_SECRET", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want partial Feishu credential error")
	}
}

func TestLoadUsesBinaryFriendlyDefaults(t *testing.T) {
	t.Setenv("TEMPORAL_ADDRESS", "")
	t.Setenv("HTTP_ADDR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.TemporalAddress != "127.0.0.1:7233" {
		t.Errorf("TemporalAddress = %q, want %q", cfg.TemporalAddress, "127.0.0.1:7233")
	}
	if cfg.HTTPAddr != "127.0.0.1:8080" {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, "127.0.0.1:8080")
	}
}
