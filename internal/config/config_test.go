package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromPathOrEnv_FileAndEnvOverride(t *testing.T) {
	t.Setenv(envNodeID, "")
	t.Setenv(envHTTPAddr, "")
	t.Setenv(envBindAddr, "")
	t.Setenv(envSeedNodes, "")
	t.Setenv(envGossipInterval, "")
	t.Setenv(envFanout, "")
	t.Setenv(envLogLevel, "")
	t.Setenv(envShutdownTimeout, "")

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "node.json")
	content := []byte(`{"node_id":"node-from-file","http_addr":":9010","bind_addr":"0.0.0.0:7000","seed_nodes":"node1:7000,node2:7000","gossip_interval_ms":1500,"fanout":3,"log_level":"debug","shutdown_timeout_seconds":7}`)
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	t.Setenv(envHTTPAddr, ":9090")
	t.Setenv(envBindAddr, "127.0.0.1:17000")
	t.Setenv(envSeedNodes, "node3:7000,node4:7000")
	t.Setenv(envGossipInterval, "900")
	t.Setenv(envFanout, "4")
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
	if cfg.BindAddr != "127.0.0.1:17000" {
		t.Fatalf("unexpected bind addr override: %s", cfg.BindAddr)
	}
	if len(cfg.SeedNodeList()) != 2 || cfg.SeedNodeList()[0] != "node3:7000" || cfg.SeedNodeList()[1] != "node4:7000" {
		t.Fatalf("unexpected seed nodes override: %v", cfg.SeedNodeList())
	}
	if cfg.GossipInterval != 900 {
		t.Fatalf("unexpected gossip interval override: %d", cfg.GossipInterval)
	}
	if cfg.Fanout != 4 {
		t.Fatalf("unexpected fanout override: %d", cfg.Fanout)
	}
	if cfg.ShutdownTimeout != 12 {
		t.Fatalf("unexpected shutdown timeout override: %d", cfg.ShutdownTimeout)
	}
}

func TestLoadFromPathOrEnv_DefaultsOnly(t *testing.T) {
	t.Setenv(envNodeID, "")
	t.Setenv(envHTTPAddr, "")
	t.Setenv(envBindAddr, "")
	t.Setenv(envSeedNodes, "")
	t.Setenv(envGossipInterval, "")
	t.Setenv(envFanout, "")
	t.Setenv(envLogLevel, "")
	t.Setenv(envShutdownTimeout, "")

	cfg, err := LoadFromPathOrEnv("")
	if err != nil {
		t.Fatalf("load defaults failed: %v", err)
	}

	if cfg.NodeID == "" || cfg.HTTPAddr == "" || cfg.BindAddr == "" || cfg.LogLevel == "" || cfg.ShutdownTimeout <= 0 || cfg.GossipInterval <= 0 || cfg.Fanout <= 0 {
		t.Fatalf("defaults are not valid: %+v", cfg)
	}
}

func TestLoadFromPathOrEnv_InvalidSeedNodes(t *testing.T) {
	t.Setenv(envNodeID, "node-x")
	t.Setenv(envHTTPAddr, ":8080")
	t.Setenv(envBindAddr, "0.0.0.0:7000")
	t.Setenv(envLogLevel, "info")
	t.Setenv(envShutdownTimeout, "10")
	t.Setenv(envGossipInterval, "1000")
	t.Setenv(envFanout, "2")

	t.Setenv(envSeedNodes, "node1:7000,node1:7000")
	if _, err := LoadFromPathOrEnv(""); err == nil {
		t.Fatalf("expected duplicate seed nodes validation error")
	}

	t.Setenv(envSeedNodes, "node1")
	if _, err := LoadFromPathOrEnv(""); err == nil {
		t.Fatalf("expected invalid seed nodes format error")
	}
}
