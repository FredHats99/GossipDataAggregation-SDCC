package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	envNodeID          = "NODE_ID"
	envHTTPAddr        = "HTTP_ADDR"
	envLogLevel        = "LOG_LEVEL"
	envShutdownTimeout = "SHUTDOWN_TIMEOUT_SECONDS"
)

type Config struct {
	NodeID          string `json:"node_id"`
	HTTPAddr        string `json:"http_addr"`
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
	if strings.TrimSpace(c.LogLevel) == "" {
		return errors.New("log_level must not be empty")
	}
	if c.ShutdownTimeout <= 0 {
		return errors.New("shutdown_timeout_seconds must be greater than zero")
	}
	return nil
}

func defaultConfig() Config {
	return Config{
		NodeID:          "node-local",
		HTTPAddr:        ":8080",
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
