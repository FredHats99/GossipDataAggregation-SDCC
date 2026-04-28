package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	envNodeID          = "NODE_ID"
	envHTTPAddr        = "HTTP_ADDR"
	envBindAddr        = "BIND_ADDR"
	envSeedNodes       = "SEED_NODES"
	envGossipInterval  = "GOSSIP_INTERVAL_MS"
	envFanout          = "FANOUT"
	envLogLevel        = "LOG_LEVEL"
	envShutdownTimeout = "SHUTDOWN_TIMEOUT_SECONDS"
)

type Config struct {
	NodeID          string `json:"node_id"`
	HTTPAddr        string `json:"http_addr"`
	BindAddr        string `json:"bind_addr"`
	SeedNodes       string `json:"seed_nodes"`
	GossipInterval  int    `json:"gossip_interval_ms"`
	Fanout          int    `json:"fanout"`
	LogLevel        string `json:"log_level"`
	ShutdownTimeout int    `json:"shutdown_timeout_seconds"`
}

func LoadFromPathOrEnv(path string) (Config, error) {
	cfg := defaultConfig()

	if strings.TrimSpace(path) != "" {
		fileCfg, err := loadFromFile(path)
		if err != nil {
			return Config{}, err
		}
		cfg = merge(cfg, fileCfg)
	}

	cfg = applyEnvOverrides(cfg)

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) ShutdownDuration() time.Duration {
	return time.Duration(c.ShutdownTimeout) * time.Second
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.NodeID) == "" {
		return errors.New("node_id must not be empty")
	}
	if strings.TrimSpace(c.HTTPAddr) == "" {
		return errors.New("http_addr must not be empty")
	}
	if strings.TrimSpace(c.BindAddr) == "" {
		return errors.New("bind_addr must not be empty")
	}
	if strings.TrimSpace(c.LogLevel) == "" {
		return errors.New("log_level must not be empty")
	}
	if c.GossipInterval <= 0 {
		return errors.New("gossip_interval_ms must be greater than zero")
	}
	if c.Fanout <= 0 {
		return errors.New("fanout must be greater than zero")
	}
	if c.ShutdownTimeout <= 0 {
		return errors.New("shutdown_timeout_seconds must be greater than zero")
	}
	if _, _, err := net.SplitHostPort(strings.TrimSpace(c.BindAddr)); err != nil {
		return fmt.Errorf("bind_addr must be host:port: %w", err)
	}
	seedSet := make(map[string]struct{})
	for _, seed := range splitSeeds(c.SeedNodes) {
		if _, _, err := net.SplitHostPort(seed); err != nil {
			return fmt.Errorf("seed_nodes contains invalid host:port %q: %w", seed, err)
		}
		if _, exists := seedSet[seed]; exists {
			return fmt.Errorf("seed_nodes contains duplicate entry %q", seed)
		}
		seedSet[seed] = struct{}{}
	}
	return nil
}

func defaultConfig() Config {
	return Config{
		NodeID:          "node-local",
		HTTPAddr:        ":8080",
		BindAddr:        ":7000",
		SeedNodes:       "",
		GossipInterval:  1000,
		Fanout:          2,
		LogLevel:        "info",
		ShutdownTimeout: 10,
	}
}

func loadFromFile(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config file %q: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config file %q: %w", path, err)
	}
	return cfg, nil
}

func merge(base, override Config) Config {
	if strings.TrimSpace(override.NodeID) != "" {
		base.NodeID = override.NodeID
	}
	if strings.TrimSpace(override.HTTPAddr) != "" {
		base.HTTPAddr = override.HTTPAddr
	}
	if strings.TrimSpace(override.BindAddr) != "" {
		base.BindAddr = override.BindAddr
	}
	if strings.TrimSpace(override.SeedNodes) != "" {
		base.SeedNodes = override.SeedNodes
	}
	if override.GossipInterval > 0 {
		base.GossipInterval = override.GossipInterval
	}
	if override.Fanout > 0 {
		base.Fanout = override.Fanout
	}
	if strings.TrimSpace(override.LogLevel) != "" {
		base.LogLevel = override.LogLevel
	}
	if override.ShutdownTimeout > 0 {
		base.ShutdownTimeout = override.ShutdownTimeout
	}
	return base
}

func applyEnvOverrides(cfg Config) Config {
	if v := strings.TrimSpace(os.Getenv(envNodeID)); v != "" {
		cfg.NodeID = v
	}
	if v := strings.TrimSpace(os.Getenv(envHTTPAddr)); v != "" {
		cfg.HTTPAddr = v
	}
	if v := strings.TrimSpace(os.Getenv(envBindAddr)); v != "" {
		cfg.BindAddr = v
	}
	if v := strings.TrimSpace(os.Getenv(envSeedNodes)); v != "" {
		cfg.SeedNodes = v
	}
	if v := strings.TrimSpace(os.Getenv(envGossipInterval)); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			cfg.GossipInterval = parsed
		}
	}
	if v := strings.TrimSpace(os.Getenv(envFanout)); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			cfg.Fanout = parsed
		}
	}
	if v := strings.TrimSpace(os.Getenv(envLogLevel)); v != "" {
		cfg.LogLevel = v
	}
	if v := strings.TrimSpace(os.Getenv(envShutdownTimeout)); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			cfg.ShutdownTimeout = parsed
		}
	}
	return cfg
}

func (c Config) GossipIntervalDuration() time.Duration {
	return time.Duration(c.GossipInterval) * time.Millisecond
}

func (c Config) SeedNodeList() []string {
	raw := splitSeeds(c.SeedNodes)
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, entry := range raw {
		seed := strings.TrimSpace(entry)
		if _, ok := seen[seed]; ok {
			continue
		}
		seen[seed] = struct{}{}
		out = append(out, seed)
	}
	sort.Strings(out)
	return out
}

func splitSeeds(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	entries := strings.Split(raw, ",")
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		seed := strings.TrimSpace(entry)
		if seed == "" {
			continue
		}
		out = append(out, seed)
	}
	return out
}
