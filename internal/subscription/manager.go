package subscription

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"easy_proxies/internal/boxmgr"
	"easy_proxies/internal/builder"
	"easy_proxies/internal/config"
	"easy_proxies/internal/importer"
	"easy_proxies/internal/monitor"
	"easy_proxies/internal/subfetch"
)

// Logger defines logging interface.
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

// Option configures the Manager.
type Option func(*Manager)

// WithLogger sets a custom logger.
func WithLogger(l Logger) Option {
	return func(m *Manager) { m.logger = l }
}

// Manager handles periodic subscription refresh.
type Manager struct {
	mu sync.RWMutex

	baseCfg *config.Config
	boxMgr  *boxmgr.Manager
	logger  Logger

	status        monitor.SubscriptionStatus
	ctx           context.Context
	cancel        context.CancelFunc
	refreshMu     sync.Mutex // prevents concurrent refreshes
	manualRefresh chan struct{}

	// Track nodes.txt content hash to detect modifications
	lastSubHash      string    // Hash of nodes.txt content after last subscription refresh
	lastNodesModTime time.Time // Last known modification time of nodes.txt
}

// New creates a SubscriptionManager.
func New(cfg *config.Config, boxMgr *boxmgr.Manager, opts ...Option) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	m := &Manager{
		baseCfg:       cfg,
		boxMgr:        boxMgr,
		ctx:           ctx,
		cancel:        cancel,
		manualRefresh: make(chan struct{}, 1),
	}
	for _, opt := range opts {
		opt(m)
	}
	if m.logger == nil {
		m.logger = defaultLogger{}
	}
	return m
}

// Start begins the periodic refresh loop.
func (m *Manager) Start() {
	if !m.baseCfg.SubscriptionRefresh.Enabled {
		m.logger.Infof("subscription refresh disabled")
		return
	}
	if len(m.baseCfg.Subscriptions) == 0 {
		m.logger.Infof("no subscriptions configured, refresh disabled")
		return
	}

	interval := m.baseCfg.SubscriptionRefresh.Interval
	m.logger.Infof("starting subscription refresh, interval: %s", interval)

	go m.refreshLoop(interval)
}

// Stop stops the periodic refresh.
func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}

}

// UpdateConfig hot-reloads subscription URLs and refresh settings without restart.
func (m *Manager) UpdateConfig(urls []string, enabled bool, interval time.Duration) {
	m.mu.Lock()
	m.baseCfg.Subscriptions = urls
	m.baseCfg.SubscriptionRefresh.Enabled = enabled
	if interval > 0 {
		m.baseCfg.SubscriptionRefresh.Interval = interval
	}
	m.mu.Unlock()

	// Persist to config.yaml
	if err := m.baseCfg.SaveSettings(); err != nil {
		m.logger.Errorf("failed to save subscription config: %v", err)
	}

	// Restart the refresh loop with new settings
	if m.cancel != nil {
		m.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.ctx = ctx
	m.cancel = cancel
	m.manualRefresh = make(chan struct{}, 1)
	m.mu.Unlock()

	if len(urls) == 0 {
		m.logger.Infof("no subscription URLs configured, skipping refresh")
		return
	}

	// Always start the refresh loop to handle the immediate refresh signal
	m.logger.Infof("subscription config updated: %d URLs, enabled=%v, interval=%s", len(urls), enabled, m.baseCfg.SubscriptionRefresh.Interval)
	go m.refreshLoop(m.baseCfg.SubscriptionRefresh.Interval)

	// Always trigger an immediate fetch when URLs are provided,
	// regardless of the "enabled" flag (which only controls periodic auto-refresh)
	select {
	case m.manualRefresh <- struct{}{}:
		m.logger.Infof("triggered immediate refresh after config update")
	default:
		// A refresh is already pending
	}
}

// UpdateConfigAndRefresh updates subscription config and synchronously waits for
// the first refresh to complete before returning. This ensures the caller (WebUI API)
// can confirm the update took effect.
func (m *Manager) UpdateConfigAndRefresh(urls []string, enabled bool, interval time.Duration) error {
	m.UpdateConfig(urls, enabled, interval)

	if len(urls) == 0 {
		return nil
	}

	// Wait for the refresh triggered by UpdateConfig to complete
	timeout := m.baseCfg.SubscriptionRefresh.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := timeout + m.baseCfg.SubscriptionRefresh.HealthCheckTimeout

	ctx, cancel := context.WithTimeout(m.ctx, deadline)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	startCount := m.Status().RefreshCount
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("刷新超时")
		case <-ticker.C:
			status := m.Status()
			if status.RefreshCount > startCount {
				if status.LastError != "" {
					return fmt.Errorf("刷新失败: %s", status.LastError)
				}
				return nil
			}
		}
	}
}

// RefreshNow triggers an immediate refresh.
func (m *Manager) RefreshNow() error {
	select {
	case m.manualRefresh <- struct{}{}:
	default:
		// Already a refresh pending
	}

	// Wait for refresh to complete or timeout
	timeout := m.baseCfg.SubscriptionRefresh.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(m.ctx, timeout+m.baseCfg.SubscriptionRefresh.HealthCheckTimeout)
	defer cancel()

	// Poll status until refresh completes
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	startCount := m.Status().RefreshCount
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("refresh timeout")
		case <-ticker.C:
			status := m.Status()
			if status.RefreshCount > startCount {
				if status.LastError != "" {
					return fmt.Errorf("refresh failed: %s", status.LastError)
				}
				return nil
			}
		}
	}
}

// Status returns the current refresh status.
func (m *Manager) Status() monitor.SubscriptionStatus {
	m.mu.RLock()
	status := m.status
	m.mu.RUnlock()

	// Check if nodes have been modified since last refresh
	status.NodesModified = m.CheckNodesModified()
	return status
}

// refreshLoop runs the periodic refresh.
func (m *Manager) refreshLoop(interval time.Duration) {
	m.mu.RLock()
	autoEnabled := m.baseCfg.SubscriptionRefresh.Enabled
	m.mu.RUnlock()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	if autoEnabled {
		// Update next refresh time only when auto-refresh is enabled
		m.mu.Lock()
		m.status.NextRefresh = time.Now().Add(interval)
		m.mu.Unlock()
	}

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			// Only do periodic refresh when auto-refresh is enabled
			if !autoEnabled {
				continue
			}
			m.doRefresh()
			m.mu.Lock()
			m.status.NextRefresh = time.Now().Add(interval)
			m.mu.Unlock()
		case <-m.manualRefresh:
			// Always honor manual/immediate refresh regardless of enabled flag
			m.doRefresh()
			if autoEnabled {
				ticker.Reset(interval)
				m.mu.Lock()
				m.status.NextRefresh = time.Now().Add(interval)
				m.mu.Unlock()
			}
		}
	}
}

// doRefresh performs a single refresh operation.
func (m *Manager) doRefresh() {
	// Prevent concurrent refreshes
	if !m.refreshMu.TryLock() {
		m.logger.Warnf("refresh already in progress, skipping")
		return
	}
	defer m.refreshMu.Unlock()

	m.mu.Lock()
	m.status.IsRefreshing = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.status.IsRefreshing = false
		m.status.RefreshCount++
		m.mu.Unlock()
	}()

	m.logger.Infof("starting subscription refresh")

	// Fetch nodes from all subscriptions
	nodes, err := m.fetchAllSubscriptions()
	if err != nil {
		m.logger.Errorf("fetch subscriptions failed: %v", err)
		m.mu.Lock()
		m.status.LastError = err.Error()
		m.status.LastRefresh = time.Now()
		m.mu.Unlock()
		return
	}

	if len(nodes) == 0 {
		m.logger.Warnf("no nodes fetched from subscriptions")
		m.mu.Lock()
		m.status.LastError = "no nodes fetched"
		m.status.LastRefresh = time.Now()
		m.mu.Unlock()
		return
	}

	m.logger.Infof("fetched %d nodes from subscriptions", len(nodes))

	// Write subscription nodes to nodes.txt
	nodesFilePath := m.getNodesFilePath()
	if err := m.writeNodesToFile(nodesFilePath, nodes); err != nil {
		m.logger.Errorf("failed to write nodes.txt: %v", err)
		m.mu.Lock()
		m.status.LastError = fmt.Sprintf("write nodes.txt: %v", err)
		m.status.LastRefresh = time.Now()
		m.mu.Unlock()
		return
	}
	m.logger.Infof("written %d nodes to %s", len(nodes), nodesFilePath)

	// Update hash and mod time after writing
	newHash := m.computeNodesHash(nodes)
	m.mu.Lock()
	m.lastSubHash = newHash
	if info, err := os.Stat(nodesFilePath); err == nil {
		m.lastNodesModTime = info.ModTime()
	} else {
		m.lastNodesModTime = time.Now()
	}
	m.status.NodesModified = false
	m.mu.Unlock()

	// Subscription refresh only updates the candidate library (nodes.txt).
	// sing-box listeners are exclusively managed by the importer pool (Promote/Demote).
	// This guarantees: WebUI port count == netstat listener count == config.yaml pool count.
	m.mu.Lock()
	m.status.LastRefresh = time.Now()
	m.status.NodeCount = len(nodes)
	m.status.LastError = ""
	m.mu.Unlock()

	m.logger.Infof("subscription refresh completed, %d candidate nodes written to nodes.txt", len(nodes))
}

// getNodesFilePath returns the path to nodes.txt.
func (m *Manager) getNodesFilePath() string {
	if m.baseCfg.NodesFile != "" {
		return m.baseCfg.NodesFile
	}
	return filepath.Join(filepath.Dir(m.baseCfg.FilePath()), "nodes.txt")
}

// writeNodesToFile writes nodes to a file (one URI per line).
func (m *Manager) writeNodesToFile(path string, nodes []config.NodeConfig) error {
	var lines []string
	for _, node := range nodes {
		lines = append(lines, node.URI)
	}
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// computeNodesHash computes a hash of node URIs for change detection.
func (m *Manager) computeNodesHash(nodes []config.NodeConfig) string {
	var uris []string
	for _, node := range nodes {
		uris = append(uris, node.URI)
	}
	content := strings.Join(uris, "\n")
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

// CheckNodesModified checks if nodes.txt has been modified since last refresh.
// Uses file modification time as a fast path to avoid unnecessary file reads.
func (m *Manager) CheckNodesModified() bool {
	m.mu.RLock()
	lastHash := m.lastSubHash
	lastMod := m.lastNodesModTime
	m.mu.RUnlock()

	if lastHash == "" {
		return false // No previous refresh, can't determine modification
	}

	nodesFilePath := m.getNodesFilePath()

	// Fast path: check modification time first
	info, err := os.Stat(nodesFilePath)
	if err != nil {
		return false // File doesn't exist or can't stat
	}
	modTime := info.ModTime()
	if !modTime.After(lastMod) {
		return false // File hasn't been modified
	}

	// Slow path: file was modified, compute hash
	data, err := os.ReadFile(nodesFilePath)
	if err != nil {
		return false // File doesn't exist or can't read
	}

	// Parse nodes from file content
	var nodes []config.NodeConfig
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if config.IsProxyURI(line) {
			nodes = append(nodes, config.NodeConfig{URI: line})
		}
	}

	currentHash := m.computeNodesHash(nodes)
	changed := currentHash != lastHash

	// Update cached mod time
	m.mu.Lock()
	m.lastNodesModTime = modTime
	m.mu.Unlock()

	return changed
}

// MarkNodesModified updates the modification status.
func (m *Manager) MarkNodesModified() {
	m.mu.Lock()
	m.status.NodesModified = true
	m.mu.Unlock()
}

// fetchAllSubscriptions fetches nodes from all configured subscription URLs.
func (m *Manager) fetchAllSubscriptions() ([]config.NodeConfig, error) {
	var allNodes []config.NodeConfig
	var lastErr error

	timeout := m.baseCfg.SubscriptionRefresh.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	for _, subURL := range m.baseCfg.Subscriptions {
		nodes, err := m.fetchSubscription(subURL, timeout)
		if err != nil {
			m.logger.Warnf("failed to fetch %s: %v", subURL, err)
			lastErr = err
			continue
		}
		m.logger.Infof("fetched %d nodes from subscription", len(nodes))
		allNodes = append(allNodes, nodes...)
	}

	if len(allNodes) == 0 && lastErr != nil {
		return nil, lastErr
	}

	return allNodes, nil
}

// fetchSubscription fetches and parses a single subscription URL.
func (m *Manager) fetchSubscription(subURL string, timeout time.Duration) ([]config.NodeConfig, error) {
	ctx, cancel := context.WithTimeout(m.ctx, timeout)
	defer cancel()

	body, err := subfetch.Fetch(ctx, subURL, subfetch.Options{
		Timeout:       timeout,
		SkipTLSVerify: m.baseCfg.SkipCertVerify,
		ProxyFallback: func(ctx context.Context, rawURL string, headers http.Header) ([]byte, error) {
			return m.fetchSubscriptionViaPool(ctx, rawURL, headers, timeout)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	return config.ParseSubscriptionContent(string(body))
}

func (m *Manager) fetchSubscriptionViaPool(ctx context.Context, rawURL string, headers http.Header, timeout time.Duration) ([]byte, error) {
	if m.boxMgr == nil {
		return nil, fmt.Errorf("box manager unavailable")
	}
	nodes, err := m.boxMgr.ListConfigNodes(ctx)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("当前没有池内节点可用于拉取订阅")
	}
	var errs []string
	for _, node := range nodes {
		if strings.TrimSpace(node.URI) == "" {
			continue
		}
		client, closeClient, clientErr := importer.NewHTTPClientForURI(ctx, builder.BuildSingleNodeOutbound, node.Name, node.URI, timeout, m.baseCfg.SkipCertVerify)
		if clientErr != nil {
			errs = append(errs, node.Name+": "+clientErr.Error())
			continue
		}
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if reqErr != nil {
			closeClient()
			return nil, reqErr
		}
		req.Header = headers.Clone()
		resp, doErr := client.Do(req)
		if doErr != nil {
			closeClient()
			errs = append(errs, node.Name+": "+doErr.Error())
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			closeClient()
			errs = append(errs, node.Name+": HTTP "+resp.Status)
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()
		closeClient()
		if readErr != nil {
			errs = append(errs, node.Name+": "+readErr.Error())
			continue
		}
		return body, nil
	}
	if len(errs) == 0 {
		return nil, fmt.Errorf("没有可用于拉取订阅的池内节点")
	}
	return nil, fmt.Errorf("%s", strings.Join(errs, " | "))
}

type defaultLogger struct{}

func (defaultLogger) Infof(format string, args ...any) {
	log.Printf("[subscription] "+format, args...)
}

func (defaultLogger) Warnf(format string, args ...any) {
	log.Printf("[subscription] WARN: "+format, args...)
}

func (defaultLogger) Errorf(format string, args ...any) {
	log.Printf("[subscription] ERROR: "+format, args...)
}
