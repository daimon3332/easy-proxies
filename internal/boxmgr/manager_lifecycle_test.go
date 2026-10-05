package boxmgr

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easy_proxies/internal/config"
	"easy_proxies/internal/monitor"
)

type shutdownNodeManager struct {
	*Manager
	started chan struct{}
}

func (m shutdownNodeManager) ListConfigNodes(ctx context.Context) ([]config.NodeConfig, error) {
	close(m.started)
	<-ctx.Done()
	return m.Manager.ListConfigNodes(ctx)
}

func TestCloseDrainsHandlerWithoutHoldingManagerLock(t *testing.T) {
	address := net.JoinHostPort("127.0.0.1", fmt.Sprint(freePort(t)))
	mgr := New(&config.Config{}, monitor.Config{Enabled: true, Listen: address})
	if err := mgr.EnsureMonitor(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	started := make(chan struct{})
	mgr.MonitorServer().SetNodeManager(shutdownNodeManager{mgr, started})
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		resp, err := http.Get("http://" + address + "/api/nodes/config")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP handler did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := mgr.CloseContext(ctx); err != nil {
		t.Fatalf("CloseContext blocked on the HTTP handler: %v", err)
	}
	<-requestDone
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("management port remains occupied: %v", err)
	}
	_ = listener.Close()
	if err := mgr.Start(context.Background()); err == nil {
		t.Fatal("Start accepted after Close")
	}
	if err := mgr.TriggerReload(context.Background()); err == nil {
		t.Fatal("reload accepted after Close")
	}
}

func TestCloseWaitsForRuntimeOperation(t *testing.T) {
	mgr := New(&config.Config{}, monitor.Config{})
	mgr.reloadMu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := mgr.CloseContext(ctx)
	mgr.reloadMu.Unlock()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CloseContext returned before runtime operation completed: %v", err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err := mgr.CloseContext(ctx2); err != nil {
		t.Fatal(err)
	}
}

func TestExtractPortFromWrappedBindError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	duplicate, err := net.Listen("tcp", listener.Addr().String())
	if err == nil {
		_ = duplicate.Close()
		t.Fatal("duplicate listener unexpectedly succeeded")
	}
	want := uint16(listener.Addr().(*net.TCPAddr).Port)
	if got := extractPortFromBindError(fmt.Errorf("start inbound: %w", err)); got != want {
		t.Fatalf("conflicting port = %d, want %d: %v", got, want, err)
	}
	for _, tc := range []struct {
		message string
		port    uint16
	}{
		{"listen tcp4 0.0.0.0:24000: bind: address already in use", 24000},
		{"listen tcp6 [::1]:24001: bind: address already in use", 24001},
		{"listen tcp 127.0.0.1:24002: bind: Only one usage of each socket address (protocol/network address/port) is normally permitted.", 24002},
		{"listen tcp 127.0.0.1:24003: bind: permission denied", 0},
		{"listen tcp 127.0.0.1:65536: bind: address already in use", 0},
	} {
		if got := extractPortFromBindError(errors.New(tc.message)); got != tc.port {
			t.Errorf("port = %d, want %d for %q", got, tc.port, tc.message)
		}
	}
}

func TestRuntimeShutdownReleasesPortsAndContexts(t *testing.T) {
	if os.Getenv("EASY_PROXIES_RUNTIME_TEST") != "1" {
		t.Skip("set EASY_PROXIES_RUNTIME_TEST=1 to run sing-box integration test")
	}
	for _, saveFailure := range []bool{false, true} {
		t.Run(fmt.Sprintf("saveFailure=%v", saveFailure), func(t *testing.T) {
			clashPort := freePort(t)
			port := freePort(t)
			if clashPort > port {
				clashPort, port = port, clashPort
			}
			t.Setenv("EASY_PROXIES_CLASH_API_LISTEN", net.JoinHostPort("127.0.0.1", fmt.Sprint(clashPort)))
			cfg := &config.Config{
				Mode: "multi-port", LogLevel: "error",
				MultiPort: config.MultiPortConfig{Address: "127.0.0.1", BasePort: port},
				Pool:      config.PoolConfig{Mode: "balance"},
				Nodes:     []config.NodeConfig{{Name: "node", URI: "socks5://127.0.0.1:1", Port: port}},
			}
			if saveFailure {
				// Force reassignment, then fail persistence after the box starts.
				occupied, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)))
				if err != nil {
					t.Fatal(err)
				}
				defer occupied.Close()
				path := filepath.Join(t.TempDir(), "not-a-directory")
				if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
				cfg.SetFilePath(filepath.Join(path, "config.yaml"))
			}
			mgr := New(cfg, monitor.Config{})
			defer mgr.Close()
			err := mgr.Start(context.Background())
			if (err != nil) != saveFailure {
				t.Fatalf("Start error=%v saveFailure=%v", err, saveFailure)
			}
			if saveFailure && !strings.Contains(err.Error(), "save reassigned ports") {
				t.Fatalf("expected persistence failure after port reassignment, got %v", err)
			}
			if err := mgr.Close(); err != nil {
				t.Fatal(err)
			}
			mgr.runtimeContexts.Range(func(key, value any) bool {
				t.Error("runtime context retained after shutdown")
				return true
			})
			listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(cfg.Nodes[0].Port)))
			if err != nil {
				t.Fatalf("proxy port remains occupied: %v", err)
			}
			_ = listener.Close()
		})
	}
}

func TestEnsureMonitorFailsWhenManagementPortIsBusy(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	mgr := New(&config.Config{}, monitor.Config{Enabled: true, Listen: occupied.Addr().String()})
	defer mgr.Close()
	if err := mgr.EnsureMonitor(context.Background()); err == nil {
		t.Fatal("EnsureMonitor succeeded on an occupied management port")
	}
	if mgr.MonitorServer() != nil {
		t.Fatal("failed monitor server remains registered")
	}
}
