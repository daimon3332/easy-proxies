package builder

import (
	"testing"
	"time"

	"easy_proxies/internal/config"
	"easy_proxies/internal/outbound/dispatch"
	poolout "easy_proxies/internal/outbound/pool"
)

func TestBuildMultiPortUsesDirectOutboundsAndDispatch(t *testing.T) {
	cfg := &config.Config{
		Mode:      "multi-port",
		LogLevel:  "error",
		MultiPort: config.MultiPortConfig{Address: "127.0.0.1", BasePort: 12000},
		Pool: config.PoolConfig{
			Mode:              "balance",
			FailureThreshold:  2,
			BlacklistDuration: time.Minute,
			RotationInterval:  time.Minute,
		},
		Nodes: []config.NodeConfig{
			{Name: "node-a", URI: "http://127.0.0.1:18001", Port: 12001},
			{Name: "node-b", URI: "socks5://127.0.0.1:18002", Port: 12002},
		},
	}

	options, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(options.Inbounds) != len(cfg.Nodes) {
		t.Fatalf("inbounds = %d, want %d", len(options.Inbounds), len(cfg.Nodes))
	}
	if len(options.Outbounds) != len(cfg.Nodes)+1 {
		t.Fatalf("outbounds = %d, want %d node outbounds + dispatcher", len(options.Outbounds), len(cfg.Nodes)+1)
	}
	poolCount := 0
	for _, outbound := range options.Outbounds {
		if outbound.Type != poolout.Type {
			continue
		}
		poolCount++
		if outbound.Tag != poolout.Tag {
			t.Fatalf("unexpected per-node pool tag %q", outbound.Tag)
		}
		poolOptions, ok := outbound.Options.(*poolout.Options)
		if !ok || len(poolOptions.Members) != len(cfg.Nodes) {
			t.Fatalf("monitor pool options = %#v", outbound.Options)
		}
	}
	if poolCount != 0 {
		t.Fatalf("pool outbounds = %d, want 0", poolCount)
	}
	if len(options.Route.Rules) != 0 {
		t.Fatalf("multi-port route rules = %d, want 0", len(options.Route.Rules))
	}
	if options.Route.Final != dispatch.Tag {
		t.Fatalf("route final = %q, want %q", options.Route.Final, dispatch.Tag)
	}
	var dispatchOptions *dispatch.Options
	for _, outbound := range options.Outbounds {
		if outbound.Type == dispatch.Type {
			dispatchOptions, _ = outbound.Options.(*dispatch.Options)
		}
	}
	if dispatchOptions == nil || len(dispatchOptions.Mappings) != len(cfg.Nodes) {
		t.Fatalf("dispatcher mappings = %#v", dispatchOptions)
	}
}

func TestCoreLogLevelBoundsLargeMultiPortStartupLogs(t *testing.T) {
	large := &config.Config{Mode: "multi-port", LogLevel: "info"}
	if got := coreLogLevel(large, verboseMultiPortLogLimit+1); got != "warn" {
		t.Fatalf("coreLogLevel() = %q, want warn", got)
	}
	large.LogLevel = "debug"
	if got := coreLogLevel(large, verboseMultiPortLogLimit+1); got != "debug" {
		t.Fatalf("explicit debug level changed to %q", got)
	}
	pool := &config.Config{Mode: "pool", LogLevel: "info"}
	if got := coreLogLevel(pool, verboseMultiPortLogLimit+1); got != "info" {
		t.Fatalf("pool log level changed to %q", got)
	}
}
