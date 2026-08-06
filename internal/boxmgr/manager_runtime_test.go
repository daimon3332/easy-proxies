package boxmgr

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"easy_proxies/internal/config"
	"easy_proxies/internal/monitor"
)

func TestIncrementalMultiPortReconcileKeepsBoxRunning(t *testing.T) {
	if os.Getenv("EASY_PROXIES_RUNTIME_TEST") != "1" {
		t.Skip("set EASY_PROXIES_RUNTIME_TEST=1 to run sing-box integration test")
	}
	firstPort := freePort(t)
	secondPort := freePort(t)
	for secondPort == firstPort {
		secondPort = freePort(t)
	}
	clashAPIPort := freePort(t)
	t.Setenv("EASY_PROXIES_CLASH_API_LISTEN", net.JoinHostPort("127.0.0.1", fmt.Sprint(clashAPIPort)))
	cfg := &config.Config{
		Mode:       "multi-port",
		LogLevel:   "error",
		MultiPort:  config.MultiPortConfig{Address: "127.0.0.1", BasePort: firstPort},
		Pool:       config.PoolConfig{Mode: "balance"},
		Management: config.ManagementConfig{},
		Nodes: []config.NodeConfig{{
			Name: "runtime-a", URI: "socks5://127.0.0.1:1", Port: firstPort, Source: config.NodeSourceInline,
		}},
	}
	cfg.SetFilePath(filepath.Join(t.TempDir(), "config.yaml"))
	if err := cfg.SaveFull(); err != nil {
		t.Fatal(err)
	}

	manager := New(cfg, monitor.Config{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	manager.mu.RLock()
	initialBox := manager.currentBox
	manager.mu.RUnlock()
	assertListening(t, firstPort, true)

	if _, err := manager.CreateNode(ctx, config.NodeConfig{
		Name: "runtime-b", URI: "socks5://127.0.0.1:2", Port: secondPort, Source: config.NodeSourceInline,
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.TriggerReload(ctx); err != nil {
		t.Fatal(err)
	}
	manager.mu.RLock()
	afterAdd := manager.currentBox
	manager.mu.RUnlock()
	if afterAdd != initialBox {
		t.Fatal("adding one node replaced the sing-box instance")
	}
	assertListening(t, firstPort, true)
	assertListening(t, secondPort, true)

	if err := manager.DeleteNode(ctx, "runtime-b"); err != nil {
		t.Fatal(err)
	}
	if err := manager.TriggerReload(ctx); err != nil {
		t.Fatal(err)
	}
	manager.mu.RLock()
	afterDelete := manager.currentBox
	manager.mu.RUnlock()
	if afterDelete != initialBox {
		t.Fatal("deleting one node replaced the sing-box instance")
	}
	assertListening(t, firstPort, true)
	assertListening(t, secondPort, false)
}

func freePort(t *testing.T) uint16 {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return uint16(listener.Addr().(*net.TCPAddr).Port)
}

func assertListening(t *testing.T, port uint16, want bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)), 100*time.Millisecond)
		if err == nil {
			conn.Close()
		}
		if (err == nil) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("port %d listening = %v, want %v", port, err == nil, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
