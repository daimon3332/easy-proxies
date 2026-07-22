package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPoolDefaults(t *testing.T) {
	cfg := &Config{}
	if err := cfg.normalize(); err != nil {
		t.Fatalf("normalize() error = %v", err)
	}
	if cfg.Mode != "multi-port" {
		t.Fatalf("Mode = %q, want multi-port", cfg.Mode)
	}
	if cfg.Pool.Mode != "rotate" {
		t.Fatalf("Pool.Mode = %q, want rotate", cfg.Pool.Mode)
	}
	if cfg.Pool.FailureThreshold != 2 {
		t.Fatalf("Pool.FailureThreshold = %d, want 2", cfg.Pool.FailureThreshold)
	}
	if cfg.Pool.BlacklistDuration != 10*time.Minute {
		t.Fatalf("Pool.BlacklistDuration = %s, want 10m", cfg.Pool.BlacklistDuration)
	}
	if cfg.Pool.RotationInterval != 2*time.Minute {
		t.Fatalf("Pool.RotationInterval = %s, want 2m", cfg.Pool.RotationInterval)
	}
}

func TestModeNormalization(t *testing.T) {
	cfg := &Config{Mode: "multi_port"}
	if err := cfg.NormalizeWithPortMap(nil); err != nil {
		t.Fatalf("NormalizeWithPortMap() error = %v", err)
	}
	if cfg.Mode != "multi-port" {
		t.Fatalf("Mode = %q, want multi-port", cfg.Mode)
	}
}

func TestExampleConfig(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatalf("Load(config.example.yaml) error = %v", err)
	}
	if cfg.Mode != "multi-port" {
		t.Fatalf("Mode = %q, want multi-port", cfg.Mode)
	}
	if len(cfg.Subscriptions) != 0 || len(cfg.Nodes) != 0 {
		t.Fatalf("example config must not contain subscriptions or nodes")
	}
}

func TestLoadDoesNotFetchSubscriptionsDuringStartup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte("subscriptions:\n  - http://127.0.0.1:1/sub\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Nodes) != 0 {
		t.Fatalf("Load() nodes = %d, want 0 without cached nodes.txt", len(cfg.Nodes))
	}
	if cfg.NodesFile != filepath.Join(dir, "nodes.txt") {
		t.Fatalf("NodesFile = %q, want default nodes.txt", cfg.NodesFile)
	}
	if cfg.Management.ProbeTarget != "https://www.gstatic.com/generate_204" {
		t.Fatalf("ProbeTarget = %q", cfg.Management.ProbeTarget)
	}
	if cfg.SubscriptionRefresh.HealthCheckTimeout != 5*time.Second {
		t.Fatalf("HealthCheckTimeout = %s, want 5s", cfg.SubscriptionRefresh.HealthCheckTimeout)
	}
}
