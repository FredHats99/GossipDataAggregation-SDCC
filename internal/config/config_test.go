package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromPathOrEnv_FileAndEnvOverride(t *testing.T) {
	t.Setenv(envNodeID, "")
	t.Setenv(envHTTPAddr, "")
	t.Setenv(envLogLevel, "")
	t.Setenv(envShutdownTimeout, "")

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "node.json")
	content := []byte(`{"node_id":"node-from-file","http_addr":":9010","log_level":"debug","shutdown_timeout_seconds":7}`)
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	t.Setenv(envHTTPAddr, ":9090")
	t.Setenv(envShutdownTimeout, "12")

	cfg, err := LoadFromPathOrEnv(configPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	if cfg.NodeID != "node-from-file" {
		t.Fatalf("unexpected node id: %s", cfg.NodeID)
	}
	if cfg.HTTPAddr != ":9090" {
		t.Fatalf("unexpected http addr override: %s", cfg.HTTPAddr)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("unexpected log level: %s", cfg.LogLevel)
	}
	if cfg.ShutdownTimeout != 12 {
		t.Fatalf("unexpected shutdown timeout override: %d", cfg.ShutdownTimeout)
	}
}

func TestLoadFromPathOrEnv_DefaultsOnly(t *testing.T) {
	t.Setenv(envNodeID, "")
	t.Setenv(envHTTPAddr, "")
	t.Setenv(envLogLevel, "")
	t.Setenv(envShutdownTimeout, "")

	cfg, err := LoadFromPathOrEnv("")
	if err != nil {
		t.Fatalf("load defaults failed: %v", err)
	}

	if cfg.NodeID == "" || cfg.HTTPAddr == "" || cfg.LogLevel == "" || cfg.ShutdownTimeout <= 0 {
		t.Fatalf("defaults are not valid: %+v", cfg)
	}
}
