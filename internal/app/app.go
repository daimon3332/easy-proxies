package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"easy_proxies/internal/boxmgr"
	"easy_proxies/internal/builder"
	"easy_proxies/internal/config"
	"easy_proxies/internal/importer"
	"easy_proxies/internal/monitor"
	"easy_proxies/internal/subscription"
)

// Run builds the runtime components from config and blocks until shutdown.
func Run(ctx context.Context, cfg *config.Config) error {
	// Build monitor config
	proxyUsername := cfg.Listener.Username
	proxyPassword := cfg.Listener.Password
	if cfg.Mode == "multi-port" || cfg.Mode == "hybrid" {
		proxyUsername = cfg.MultiPort.Username
		proxyPassword = cfg.MultiPort.Password
	}

	monitorCfg := monitor.Config{
		Enabled:       cfg.ManagementEnabled(),
		Listen:        cfg.Management.Listen,
		ProbeTarget:   cfg.Management.ProbeTarget,
		Password:      cfg.Management.Password,
		ProxyUsername: proxyUsername,
		ProxyPassword: proxyPassword,
		ExternalIP:    cfg.ExternalIP,
	}

	// Create and start BoxManager
	boxMgr := boxmgr.New(cfg, monitorCfg)
	if err := boxMgr.EnsureMonitor(ctx); err != nil {
		return fmt.Errorf("init monitor server: %w", err)
	}

	// Wire up config to monitor server for settings API
	if server := boxMgr.MonitorServer(); server != nil {
		server.SetConfig(cfg)
	}

	// Always create SubscriptionManager so WebUI can hot-reload subscription config
	subMgr := subscription.New(cfg, boxMgr)
	defer subMgr.Stop()

	// Wire up subscription manager to monitor server for API endpoints
	if server := boxMgr.MonitorServer(); server != nil {
		server.SetSubscriptionRefresher(subMgr)
	}

	// Initialize import service
	storePath := filepath.Join(filepath.Dir(cfg.FilePath()), "managed_nodes.json")
	nodeStore, err := importer.NewStore(storePath)
	if err != nil {
		return fmt.Errorf("create node store: %w", err)
	}

	// Pool is the single source of truth for sing-box listeners.
	// Filter cfg.Nodes to pool DB members before sing-box starts so the
	// runtime listener count == WebUI pool count == config.yaml managed pool.
	// On fresh installs (empty pool) we leave cfg.Nodes untouched so the
	// initial subscription-loaded nodes remain usable until first promote.
	if poolNodes := nodeStore.ListPoolNodes(); len(poolNodes) > 0 {
		poolByURI := make(map[string]importer.ManagedNode, len(poolNodes))
		for _, pn := range poolNodes {
			if pn.URI != "" {
				poolByURI[pn.URI] = pn
			}
		}
		filtered := make([]config.NodeConfig, 0, len(poolByURI))
		seen := make(map[string]struct{}, len(poolByURI))
		for _, n := range cfg.Nodes {
			if pn, ok := poolByURI[n.URI]; ok {
				if pn.Name != "" {
					n.Name = pn.Name
				}
				filtered = append(filtered, n)
				seen[n.URI] = struct{}{}
			}
		}
		// Include pool entries that have no matching subscription URI yet
		// (e.g. user manually imported them). These arrive with Port=0
		// and will be assigned by RebuildPortAssignments below.
		for uri, pn := range poolByURI {
			if _, ok := seen[uri]; ok {
				continue
			}
			filtered = append(filtered, pn.ToConfigNode())
		}
		cfg.Nodes = filtered
		// Reset ports to 0 so the rebuild assigns them contiguously from
		// base_port, independent of any stale port values carried over from
		// the prior session's config load.
		for i := range cfg.Nodes {
			cfg.Nodes[i].Port = 0
		}
		if err := boxMgr.RebuildPortAssignments(); err != nil {
			return fmt.Errorf("rebuild port assignments: %w", err)
		}
	}

	tester := importer.NewNodeTester(builder.BuildSingleNodeOutbound,
		importer.WithProbeTarget(cfg.Management.ProbeTarget),
		importer.WithTesterTimeout(cfg.SubscriptionRefresh.Timeout),
		importer.WithSkipCertVerify(cfg.SkipCertVerify),
	)

	importSvc := importer.NewService(nodeStore, tester, boxMgr)
	if server := boxMgr.MonitorServer(); server != nil {
		server.SetImportService(importSvc)
	}

	if err := boxMgr.Start(ctx); err != nil {
		return fmt.Errorf("start box manager: %w", err)
	}
	defer boxMgr.Close()

	// Start refresh loop only after the initial sing-box instance is ready.
	if cfg.SubscriptionRefresh.Enabled && len(cfg.Subscriptions) > 0 {
		subMgr.Start()
	}

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case <-ctx.Done():
		fmt.Println("Context cancelled, initiating graceful shutdown...")
	case sig := <-sigCh:
		fmt.Printf("Received %s, initiating graceful shutdown...\n", sig)
	}

	// Create shutdown context with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Graceful shutdown sequence
	fmt.Println("Stopping subscription manager...")
	if subMgr != nil {
		subMgr.Stop()
	}

	fmt.Println("Stopping box manager...")
	if err := boxMgr.Close(); err != nil {
		fmt.Printf("Error closing box manager: %v\n", err)
	}

	// Wait for connections to drain
	fmt.Println("Waiting for connections to drain...")
	select {
	case <-time.After(2 * time.Second):
		fmt.Println("Graceful shutdown completed")
	case <-shutdownCtx.Done():
		fmt.Println("Shutdown timeout exceeded, forcing exit")
	}

	return nil
}
