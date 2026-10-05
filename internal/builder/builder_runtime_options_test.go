package builder

import (
	"os"
	"path/filepath"
	"testing"

	"easy_proxies/internal/config"
)

func multiPortGeoIPConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		Mode:      "multi-port",
		LogLevel:  "error",
		MultiPort: config.MultiPortConfig{Address: "127.0.0.1", BasePort: 12000},
		GeoIP:     config.GeoIPConfig{Enabled: true, DatabasePath: filepath.Join(t.TempDir(), "missing.mmdb")},
		Nodes:     []config.NodeConfig{{Name: "node-a", URI: "http://127.0.0.1:18001", Port: 12001}},
	}
}

func TestBuildMultiPortSkipsGeoIPLookup(t *testing.T) {
	cfg := multiPortGeoIPConfig(t)
	if GeoIPRoutingEnabled(cfg) {
		t.Fatal("multi-port mode must not enable GeoIP routing")
	}
	if _, err := Build(cfg); err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if _, err := os.Stat(cfg.GeoIP.DatabasePath); !os.IsNotExist(err) {
		t.Fatalf("GeoIP database was touched in multi-port mode: %v", err)
	}
	for _, mode := range []string{"pool", "hybrid"} {
		cfg.Mode = mode
		if !GeoIPRoutingEnabled(cfg) {
			t.Fatalf("%s mode should enable GeoIP routing", mode)
		}
	}
}

func TestBuildClashAPIIsOptIn(t *testing.T) {
	cfg := multiPortGeoIPConfig(t)
	cfg.GeoIP.Enabled = false
	t.Setenv("EASY_PROXIES_CLASH_API_LISTEN", "")
	options, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if options.Experimental != nil {
		t.Fatalf("Clash API enabled without opt-in: %#v", options.Experimental)
	}
	t.Setenv("EASY_PROXIES_CLASH_API_LISTEN", "127.0.0.1:19092")
	options, err = Build(cfg)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if options.Experimental == nil || options.Experimental.ClashAPI == nil || options.Experimental.ClashAPI.ExternalController != "127.0.0.1:19092" {
		t.Fatalf("Clash API opt-in not applied: %#v", options.Experimental)
	}
}
