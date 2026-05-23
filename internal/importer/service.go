package importer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"easy_proxies/internal/config"
)

type NodeManager interface {
	CreateNode(ctx context.Context, node config.NodeConfig) (config.NodeConfig, error)
	TriggerReload(ctx context.Context) error
}

type NodeUpdater interface {
	UpdateNode(ctx context.Context, name string, node config.NodeConfig) (config.NodeConfig, error)
}

type NodeRemover interface {
	DeleteNode(ctx context.Context, name string) error
}

type NodeReorderer interface {
	ReorderNodes(ctx context.Context, names []string) error
}

type NodeLister interface {
	ListConfigNodes(ctx context.Context) ([]config.NodeConfig, error)
}

type Service struct {
	store      *Store
	tester     *NodeTester
	nodeMgr    NodeManager
	httpClient *http.Client

	testJobsMu sync.RWMutex
	testJobs   map[string]*TestJob
}

type Option func(*Service)

func NewService(store *Store, tester *NodeTester, nodeMgr NodeManager, opts ...Option) *Service {
	s := &Service{
		store:   store,
		tester:  tester,
		nodeMgr: nodeMgr,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		testJobs: make(map[string]*TestJob),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithHTTPClient(c *http.Client) Option {
	return func(s *Service) {
		if c != nil {
			s.httpClient = c
		}
	}
}

func (s *Service) Parse(req ParseRequest) (ParseResponse, error) {
	if req.TagPrefix == "" {
		req.TagPrefix = "local"
	}
	if req.Mode != "url" && req.Mode != "content" {
		return ParseResponse{}, fmt.Errorf("mode 必须为 url 或 content")
	}

	var content string
	if req.Mode == "url" {
		if req.URL == "" {
			return ParseResponse{}, fmt.Errorf("url 不能为空")
		}
		if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
			return ParseResponse{}, fmt.Errorf("url 必须以 http:// 或 https:// 开头")
		}
		httpReq, err := http.NewRequest(http.MethodGet, req.URL, nil)
		if err != nil {
			return ParseResponse{}, fmt.Errorf("创建订阅请求: %w", err)
		}
		httpReq.Header.Set("User-Agent", "clash-verge/v2.2.3")
		httpReq.Header.Set("Accept", "*/*")
		resp, err := s.httpClient.Do(httpReq)
		if err != nil {
			return ParseResponse{}, fmt.Errorf("获取订阅: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return ParseResponse{}, fmt.Errorf("获取订阅: HTTP %d", resp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		if err != nil {
			return ParseResponse{}, fmt.Errorf("读取订阅: %w", err)
		}
		content = string(body)
	} else {
		content = req.Content
	}

	configNodes, err := config.ParseSubscriptionContent(content)
	if err != nil {
		return ParseResponse{}, fmt.Errorf("解析订阅: %w", err)
	}
	if len(configNodes) == 0 {
		return ParseResponse{}, fmt.Errorf("未找到有效节点")
	}

	importID := randomHex(12)
	format := detectFormat(content)
	importSource := strings.TrimSpace(req.URL)
	if importSource == "" {
		importSource = req.Mode
	}
	nodes := make([]ManagedNode, 0, len(configNodes))
	nodeIDs := make([]string, 0, len(configNodes))
	now := time.Now()

	for _, cn := range configNodes {
		id := nodeID(cn.URI)
		name := cn.Name
		if name == "" {
			name = extractNameFromURI(cn.URI)
		}
		name = cleanNodeName(name)
		mn := ManagedNode{
			ID:           id,
			URI:          cn.URI,
			OriginalName: name,
			Name:         req.TagPrefix + "-" + name,
			TagPrefix:    req.TagPrefix,
			ImportID:     importID,
			ImportMode:   req.Mode,
			ImportSource: importSource,
			ImportFormat: format,
			State:        StateParsed,
			Enabled:      true,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		nodes = append(nodes, mn)
		nodeIDs = append(nodeIDs, id)
	}

	if err := s.store.UpsertNodes(nodes); err != nil {
		return ParseResponse{}, fmt.Errorf("保存节点: %w", err)
	}

	job := ImportJob{
		ID:        importID,
		Status:    ImportStatusParsed,
		Total:     len(nodes),
		NodeIDs:   nodeIDs,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.UpsertJob(job); err != nil {
		return ParseResponse{}, fmt.Errorf("保存导入任务: %w", err)
	}

	return ParseResponse{
		ImportID: importID,
		Format:   format,
		Nodes:    nodes,
	}, nil
}

func (s *Service) Commit(importID string, req CommitRequest) (CommitResponse, error) {
	job, ok := s.store.GetJob(importID)
	if !ok {
		return CommitResponse{}, fmt.Errorf("导入任务 %s 不存在", importID)
	}
	if job.Status == ImportStatusRunning {
		return CommitResponse{}, fmt.Errorf("导入任务正在进行中")
	}

	selectedIDs := req.NodeIDs
	if len(selectedIDs) == 0 {
		selectedIDs = job.NodeIDs
	}

	nodes := make([]ManagedNode, 0, len(selectedIDs))
	for _, id := range selectedIDs {
		n, ok := s.store.GetNode(id)
		if !ok {
			continue
		}
		if n.State != StateParsed {
			continue
		}
		n.State = StateTesting
		nodes = append(nodes, n)
	}
	if len(nodes) == 0 {
		return CommitResponse{}, fmt.Errorf("没有可导入的节点")
	}

	if err := s.store.UpsertNodes(nodes); err != nil {
		return CommitResponse{}, err
	}

	jobID := randomHex(12)
	job = ImportJob{
		ID:        jobID,
		Status:    ImportStatusRunning,
		Total:     len(nodes),
		NodeIDs:   selectedIDs,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.store.UpsertJob(job); err != nil {
		return CommitResponse{}, err
	}

	go s.runPipeline(jobID, nodes)

	return CommitResponse{JobID: jobID}, nil
}

func (s *Service) runPipeline(jobID string, nodes []ManagedNode) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	total := len(nodes)
	passed := 0
	failed := 0

	for event := range s.tester.TestBatch(ctx, nodes) {
		node, ok := s.store.GetNode(event.NodeID)
		if !ok {
			continue
		}
		if event.Result.Error != nil {
			_ = s.markFailed(node, event.Result.Error.Error())
			failed++
		} else {
			if err := s.markPassed(node, event.Result); err != nil {
				s.store.UpdateNodeState(event.NodeID, StateFailed, err.Error())
				failed++
				continue
			}
			passed++
		}
	}

	status := ImportStatusCompleted
	if failed == total {
		status = ImportStatusFailed
	}
	s.store.UpdateJob(jobID, func(j *ImportJob) {
		j.Status = status
		j.Passed = passed
		j.Failed = failed
		j.UpdatedAt = time.Now()
	})

	// Successful imports become candidates first. They are only written into the
	// runtime proxy pool when the user explicitly promotes them.
}

func (s *Service) Retest(nodeID string) (ManagedNode, error) {
	node, ok := s.store.GetNode(nodeID)
	if !ok {
		return ManagedNode{}, fmt.Errorf("节点 %s 不存在", nodeID)
	}
	node.State = StateTesting
	if err := s.store.UpsertNode(node); err != nil {
		return ManagedNode{}, err
	}

	result := s.tester.Probe(context.Background(), node)
	if result.Error != nil {
		_ = s.markFailed(node, result.Error.Error())
	} else {
		if err := s.markProbePassed(node, result); err != nil {
			return ManagedNode{}, err
		}
	}
	n, _ := s.store.GetNode(nodeID)
	return n, nil
}

func (s *Service) TestCountry(nodeID string) (ManagedNode, error) {
	node, ok := s.store.GetNode(nodeID)
	if !ok {
		return ManagedNode{}, fmt.Errorf("节点 %s 不存在", nodeID)
	}
	if node.State == StateFailed {
		return node, fmt.Errorf("失败节点需要先测速成功再测试国家")
	}

	result := s.tester.LookupCountry(context.Background(), node)
	if result.Error != nil {
		node.LastError = result.Error.Error()
		node.UpdatedAt = time.Now()
		_ = s.store.UpsertNode(node)
		return node, result.Error
	}
	if err := s.markCountry(node, result); err != nil {
		return ManagedNode{}, err
	}
	n, _ := s.store.GetNode(nodeID)
	return n, nil
}

func (s *Service) BatchTest(req BatchTestRequest) (BatchTestResponse, error) {
	if len(req.NodeIDs) == 0 {
		return BatchTestResponse{}, fmt.Errorf("请选择节点")
	}
	resp := BatchTestResponse{Total: len(req.NodeIDs)}
	nodes := make([]ManagedNode, 0, len(req.NodeIDs))
	for _, id := range req.NodeIDs {
		if node, ok := s.store.GetNode(id); ok {
			nodes = append(nodes, node)
		}
	}
	if len(nodes) == 0 {
		return resp, nil
	}

	var mu sync.Mutex
	changed := false
	if req.Retest {
		for event := range s.tester.ProbeBatch(context.Background(), nodes) {
			node, ok := s.store.GetNode(event.NodeID)
			if !ok {
				continue
			}
			mu.Lock()
			resp.Retested++
			mu.Unlock()
			if event.Result.Error != nil {
				_ = s.markFailed(node, event.Result.Error.Error())
				mu.Lock()
				resp.Failed++
				changed = true
				mu.Unlock()
				continue
			}
			if err := s.markProbePassed(node, event.Result); err != nil {
				_ = s.markFailed(node, err.Error())
				mu.Lock()
				resp.Failed++
				changed = true
				mu.Unlock()
				continue
			}
			mu.Lock()
			resp.Passed++
			mu.Unlock()
		}
	}

	if req.Country {
		countryNodes := make([]ManagedNode, 0, len(nodes))
		for _, id := range req.NodeIDs {
			node, ok := s.store.GetNode(id)
			if !ok || node.State == StateFailed {
				continue
			}
			countryNodes = append(countryNodes, node)
		}
		for event := range s.tester.CountryBatch(context.Background(), countryNodes) {
			node, ok := s.store.GetNode(event.NodeID)
			if !ok {
				continue
			}
			if event.Result.Error != nil {
				node.LastError = event.Result.Error.Error()
				node.UpdatedAt = time.Now()
				_ = s.store.UpsertNode(node)
				mu.Lock()
				resp.CountryBad++
				mu.Unlock()
				continue
			}
			if err := s.markCountry(node, event.Result); err != nil {
				mu.Lock()
				resp.CountryBad++
				mu.Unlock()
				continue
			}
			mu.Lock()
			resp.CountryOK++
			changed = true
			mu.Unlock()
		}
	}

	if req.PromotePassed {
		for _, id := range req.NodeIDs {
			node, ok := s.store.GetNode(id)
			if !ok || node.State != StatePassed || node.InPool {
				continue
			}
			if promoted, err := s.Promote(id, false); err == nil && (promoted.InPool || promoted.State == StateInPool) {
				resp.Promoted++
				changed = true
			}
		}
	}
	if changed && (req.AutoReload || req.PromotePassed) {
		_ = s.nodeMgr.TriggerReload(context.Background())
	}
	for _, id := range req.NodeIDs {
		if node, ok := s.store.GetNode(id); ok {
			resp.Nodes = append(resp.Nodes, node)
		}
	}
	return resp, nil
}

// StartBatchTest launches a non-blocking batch test and returns a job ID the
// WebUI can poll via GetTestJob. The job runs in a detached goroutine and
// publishes progress under s.testJobs. Behavior mirrors BatchTest with one
// addition: when Retest+PromotePassed are both set, nodes that pass probe
// but lack a country are auto-country-tested before the promote pass so
// failed-pool retries land in pool with country metadata populated.
func (s *Service) StartBatchTest(req BatchTestRequest) (string, error) {
	if len(req.NodeIDs) == 0 {
		return "", fmt.Errorf("请选择节点")
	}
	if !req.Retest && !req.Country {
		return "", fmt.Errorf("至少选择一种操作（测速或测试国家）")
	}
	jobID := randomHex(12)
	now := time.Now()
	job := &TestJob{
		ID:        jobID,
		Status:    TestJobRunning,
		Total:     len(req.NodeIDs),
		Phase:     "queued",
		StartedAt: now,
		UpdatedAt: now,
	}
	s.testJobsMu.Lock()
	s.testJobs[jobID] = job
	// Best-effort GC: keep the map small by dropping finished jobs older than 10 min.
	for id, j := range s.testJobs {
		if j.Status != TestJobRunning && now.Sub(j.UpdatedAt) > 10*time.Minute {
			delete(s.testJobs, id)
		}
	}
	s.testJobsMu.Unlock()

	go s.runBatchTestJob(jobID, req)
	return jobID, nil
}

// GetTestJob returns a snapshot copy of the job by id.
func (s *Service) GetTestJob(jobID string) (TestJob, bool) {
	s.testJobsMu.RLock()
	defer s.testJobsMu.RUnlock()
	j, ok := s.testJobs[jobID]
	if !ok {
		return TestJob{}, false
	}
	return *j, true
}

func (s *Service) updateJob(jobID string, fn func(*TestJob)) {
	s.testJobsMu.Lock()
	defer s.testJobsMu.Unlock()
	j, ok := s.testJobs[jobID]
	if !ok {
		return
	}
	fn(j)
	j.UpdatedAt = time.Now()
}

func (s *Service) runBatchTestJob(jobID string, req BatchTestRequest) {
	defer func() {
		if r := recover(); r != nil {
			s.updateJob(jobID, func(j *TestJob) {
				j.Status = TestJobFailed
				j.Error = fmt.Sprintf("panic: %v", r)
			})
		}
	}()

	nodes := make([]ManagedNode, 0, len(req.NodeIDs))
	for _, id := range req.NodeIDs {
		if n, ok := s.store.GetNode(id); ok {
			nodes = append(nodes, n)
		}
	}
	if len(nodes) == 0 {
		s.updateJob(jobID, func(j *TestJob) {
			j.Status = TestJobFinished
			j.Phase = "empty"
		})
		return
	}

	changed := false
	ctx := context.Background()

	// --- Phase: probe ---
	if req.Retest {
		s.updateJob(jobID, func(j *TestJob) { j.Phase = "probe"; j.Done = 0 })
		for event := range s.tester.ProbeBatch(ctx, nodes) {
			node, ok := s.store.GetNode(event.NodeID)
			if !ok {
				s.updateJob(jobID, func(j *TestJob) { j.Done++ })
				continue
			}
			if event.Result.Error != nil {
				_ = s.markFailed(node, event.Result.Error.Error())
				changed = true
				s.updateJob(jobID, func(j *TestJob) { j.Done++; j.Failed++ })
				continue
			}
			if err := s.markProbePassed(node, event.Result); err != nil {
				_ = s.markFailed(node, err.Error())
				changed = true
				s.updateJob(jobID, func(j *TestJob) { j.Done++; j.Failed++ })
				continue
			}
			s.updateJob(jobID, func(j *TestJob) { j.Done++; j.Passed++ })
		}
	}

	// --- Phase: country (explicit request OR auto-fill for promote-bound nodes) ---
	countryNodeIDs := make(map[string]struct{})
	if req.Country {
		for _, id := range req.NodeIDs {
			n, ok := s.store.GetNode(id)
			if !ok || n.State == StateFailed {
				continue
			}
			countryNodeIDs[id] = struct{}{}
		}
	}
	if req.Retest && req.PromotePassed {
		for _, id := range req.NodeIDs {
			n, ok := s.store.GetNode(id)
			if !ok || n.State != StatePassed || n.InPool {
				continue
			}
			if n.CountryCode == "" {
				countryNodeIDs[id] = struct{}{}
			}
		}
	}
	if len(countryNodeIDs) > 0 {
		countryNodes := make([]ManagedNode, 0, len(countryNodeIDs))
		for id := range countryNodeIDs {
			if n, ok := s.store.GetNode(id); ok {
				countryNodes = append(countryNodes, n)
			}
		}
		s.updateJob(jobID, func(j *TestJob) { j.Phase = "country"; j.Done = 0; j.Total = len(countryNodes) })
		for event := range s.tester.CountryBatch(ctx, countryNodes) {
			node, ok := s.store.GetNode(event.NodeID)
			if !ok {
				s.updateJob(jobID, func(j *TestJob) { j.Done++ })
				continue
			}
			if event.Result.Error != nil {
				node.LastError = event.Result.Error.Error()
				node.UpdatedAt = time.Now()
				_ = s.store.UpsertNode(node)
				s.updateJob(jobID, func(j *TestJob) { j.Done++; j.CountryBad++ })
				continue
			}
			if err := s.markCountry(node, event.Result); err != nil {
				s.updateJob(jobID, func(j *TestJob) { j.Done++; j.CountryBad++ })
				continue
			}
			changed = true
			s.updateJob(jobID, func(j *TestJob) { j.Done++; j.CountryOK++ })
		}
	}

	// --- Phase: promote ---
	if req.PromotePassed {
		s.updateJob(jobID, func(j *TestJob) { j.Phase = "promote" })
		for _, id := range req.NodeIDs {
			n, ok := s.store.GetNode(id)
			if !ok || n.State != StatePassed || n.InPool {
				continue
			}
			if promoted, err := s.Promote(id, false); err == nil && (promoted.InPool || promoted.State == StateInPool) {
				changed = true
				s.updateJob(jobID, func(j *TestJob) { j.Promoted++ })
			}
		}
	}

	if changed && (req.AutoReload || req.PromotePassed) {
		_ = s.nodeMgr.TriggerReload(ctx)
	}

	s.updateJob(jobID, func(j *TestJob) { j.Status = TestJobFinished; j.Phase = "done" })
}

func (s *Service) markProbePassed(node ManagedNode, result TestResult) error {
	node.LatencyMs = result.LatencyMs
	if node.InPool || node.State == StateInPool {
		node.State = StateInPool
	} else {
		node.State = StatePassed
		node.InPool = false
		node.Port = 0
	}
	node.Enabled = true
	node.LastError = ""
	node.LastTestAt = time.Now()
	return s.store.UpsertNode(node)
}

func (s *Service) markPassed(node ManagedNode, result TestResult) error {
	node.LatencyMs = result.LatencyMs
	node.CountryCode = result.CountryCode
	node.CountryName = result.CountryName
	if node.CountryCode != "" {
		node.Name = s.nextCountryName(node.ID, node.TagPrefix, node.CountryCode)
	}
	if node.InPool || node.State == StateInPool {
		node.State = StateInPool
	} else {
		node.State = StatePassed
		node.InPool = false
		node.Port = 0
	}
	node.Enabled = true
	node.LastError = ""
	node.LastTestAt = time.Now()
	return s.store.UpsertNode(node)
}

func (s *Service) markCountry(node ManagedNode, result TestResult) error {
	oldName := node.Name
	node.CountryCode = result.CountryCode
	node.CountryName = result.CountryName
	if node.CountryCode != "" {
		node.Name = s.nextCountryName(node.ID, node.TagPrefix, node.CountryCode)
	}
	node.LastError = ""
	node.LastTestAt = time.Now()
	if (node.InPool || node.State == StateInPool) && oldName != "" && node.Name != oldName {
		updater, ok := s.nodeMgr.(NodeUpdater)
		if !ok {
			return s.store.UpsertNode(node)
		}
		cn, err := updater.UpdateNode(context.Background(), oldName, node.ToConfigNode())
		if err != nil {
			return err
		}
		node.Port = cn.Port
		_ = s.nodeMgr.TriggerReload(context.Background())
	}
	return s.store.UpsertNode(node)
}

func (s *Service) markFailed(node ManagedNode, lastErr string) error {
	wasInPool := node.InPool || node.State == StateInPool
	oldName := node.Name
	node.State = StateFailed
	node.InPool = false
	node.Port = 0
	node.LatencyMs = 0
	node.CountryCode = ""
	node.CountryName = ""
	node.Name = taggedOriginalName(node.TagPrefix, node.OriginalName)
	node.LastError = lastErr
	node.LastTestAt = time.Now()
	if err := s.store.UpsertNode(node); err != nil {
		return err
	}
	if wasInPool {
		if remover, ok := s.nodeMgr.(NodeRemover); ok && oldName != "" {
			_ = remover.DeleteNode(context.Background(), oldName)
		}
		_ = s.nodeMgr.TriggerReload(context.Background())
	}
	return nil
}

func (s *Service) Promote(nodeID string, autoReload bool) (ManagedNode, error) {
	node, ok := s.store.GetNode(nodeID)
	if !ok {
		return ManagedNode{}, fmt.Errorf("节点 %s 不存在", nodeID)
	}
	if node.InPool || node.State == StateInPool {
		return node, nil
	}
	if node.State != StatePassed {
		return node, fmt.Errorf("节点尚未测速成功，不能加入节点池")
	}
	cn, err := s.nodeMgr.CreateNode(context.Background(), node.ToConfigNode())
	if err != nil {
		if strings.Contains(err.Error(), "节点名称或端口已存在") {
			_ = s.store.DeleteNode(nodeID)
			return ManagedNode{}, nil
		}
		return node, err
	}
	if _, err := s.store.MarkInPool(nodeID, cn.Port); err != nil {
		return node, fmt.Errorf("mark in pool: %w", err)
	}
	if autoReload {
		_ = s.nodeMgr.TriggerReload(context.Background())
	}
	n, _ := s.store.GetNode(nodeID)
	return n, nil
}

func (s *Service) Exclude(nodeID string) (ManagedNode, error) {
	node, ok := s.store.GetNode(nodeID)
	if !ok {
		return ManagedNode{}, fmt.Errorf("节点 %s 不存在", nodeID)
	}
	node.State = StateExcluded
	node.InPool = false
	node.Enabled = false
	if err := s.store.UpsertNode(node); err != nil {
		return ManagedNode{}, err
	}
	return node, nil
}

func (s *Service) Delete(nodeID string) error {
	node, ok := s.store.GetNode(nodeID)
	if !ok {
		return fmt.Errorf("节点 %s 不存在", nodeID)
	}
	if node.InPool || node.State == StateInPool {
		if remover, ok := s.nodeMgr.(NodeRemover); ok && node.Name != "" {
			_ = remover.DeleteNode(context.Background(), node.Name)
		}
		_ = s.nodeMgr.TriggerReload(context.Background())
	}
	return s.store.DeleteNode(nodeID)
}

// DeleteBySubscription deletes every ManagedNode whose ImportSource matches the
// given subscription URL. Pool members are first removed from the sing-box
// config (via NodeManager) and a single Reload is triggered at the end to
// minimize churn. Returns the number of nodes removed from the store.
func (s *Service) DeleteBySubscription(url string) (int, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return 0, fmt.Errorf("订阅 URL 不能为空")
	}
	all := s.store.ListNodes()
	remover, _ := s.nodeMgr.(NodeRemover)
	touchedPool := false
	deleted := 0
	for _, n := range all {
		if n.ImportSource != url {
			continue
		}
		if (n.InPool || n.State == StateInPool) && remover != nil && n.Name != "" {
			_ = remover.DeleteNode(context.Background(), n.Name)
			touchedPool = true
		}
		if err := s.store.DeleteNode(n.ID); err != nil {
			return deleted, fmt.Errorf("删除节点 %s: %w", n.ID, err)
		}
		deleted++
	}
	if touchedPool {
		_ = s.nodeMgr.TriggerReload(context.Background())
	}
	return deleted, nil
}

func (s *Service) ListAll() ([]ManagedNode, error) {
	return s.syncRuntimeNodes(s.store.ListNodes()), nil
}

func (s *Service) ListPool() ([]ManagedNode, error) {
	return s.syncRuntimeNodes(s.store.ListPoolNodes()), nil
}

func (s *Service) ListFailed() ([]ManagedNode, error) {
	return s.store.ListFailedNodes(), nil
}

func (s *Service) Reorder(ids []string) error {
	if err := s.store.SetOrder(ids); err != nil {
		return err
	}
	reorderer, ok := s.nodeMgr.(NodeReorderer)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		node, ok := s.store.GetNode(id)
		if !ok || !node.InPool || node.State != StateInPool || node.Name == "" {
			continue
		}
		names = append(names, node.Name)
	}
	if len(names) == 0 {
		return nil
	}
	if err := reorderer.ReorderNodes(context.Background(), names); err != nil {
		return err
	}
	return s.nodeMgr.TriggerReload(context.Background())
}

func (s *Service) Job(jobID string) (ImportJob, bool) {
	return s.store.GetJob(jobID)
}

func (s *Service) syncRuntimeNodes(nodes []ManagedNode) []ManagedNode {
	lister, ok := s.nodeMgr.(NodeLister)
	if !ok {
		return nodes
	}
	configNodes, err := lister.ListConfigNodes(context.Background())
	if err != nil {
		return nodes
	}
	byURI := make(map[string]config.NodeConfig, len(configNodes))
	byName := make(map[string]config.NodeConfig, len(configNodes))
	for _, cn := range configNodes {
		if cn.URI != "" {
			byURI[cn.URI] = cn
		}
		if cn.Name != "" {
			byName[cn.Name] = cn
		}
	}
	for i := range nodes {
		if !nodes[i].InPool && nodes[i].State != StateInPool {
			continue
		}
		if cn, ok := byURI[nodes[i].URI]; ok {
			nodes[i].Port = cn.Port
			continue
		}
		if cn, ok := byName[nodes[i].Name]; ok {
			nodes[i].Port = cn.Port
		}
	}
	return nodes
}

func (s *Service) nextCountryName(currentID, tagPrefix, countryCode string) string {
	tagPrefix = strings.TrimSpace(tagPrefix)
	if tagPrefix == "" {
		tagPrefix = "local"
	}
	countryCode = strings.ToUpper(strings.TrimSpace(countryCode))
	if countryCode == "" {
		return tagPrefix
	}
	country := countryDisplayName(countryCode)
	prefix := tagPrefix + "-" + country
	next := 1
	for _, n := range s.store.ListNodes() {
		if n.ID == currentID {
			continue
		}
		if strings.HasPrefix(n.Name, prefix) {
			next++
		}
	}
	return fmt.Sprintf("%s%d", prefix, next)
}

func countryDisplayName(code string) string {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "JP":
		return "日本"
	case "SG":
		return "新加坡"
	case "HK":
		return "香港"
	case "TW":
		return "台湾"
	case "US":
		return "美国"
	case "KR":
		return "韩国"
	case "CH":
		return "瑞士"
	case "NL":
		return "荷兰"
	case "RU":
		return "俄罗斯"
	case "GB", "UK":
		return "英国"
	case "DE":
		return "德国"
	case "FR":
		return "法国"
	case "CA":
		return "加拿大"
	case "AU":
		return "澳大利亚"
	case "IN":
		return "印度"
	default:
		if code == "" {
			return "未知"
		}
		return strings.ToUpper(code)
	}
}

func taggedOriginalName(tagPrefix, original string) string {
	tagPrefix = strings.TrimSpace(tagPrefix)
	if tagPrefix == "" {
		tagPrefix = "local"
	}
	original = strings.TrimSpace(original)
	if original == "" {
		return tagPrefix
	}
	if strings.HasPrefix(original, tagPrefix+"-") {
		return original
	}
	return tagPrefix + "-" + original
}

func nodeID(uri string) string {
	h := sha256.Sum256([]byte(uri))
	return hex.EncodeToString(h[:])[:16]
}

func randomHex(n int) string {
	b := make([]byte, n/2+1)
	rand.Read(b)
	return hex.EncodeToString(b)[:n]
}

func detectFormat(content string) string {
	content = strings.TrimSpace(content)
	if len(content) > 128 {
		content = content[:128]
	}
	if strings.Contains(content, "proxies:") {
		return "clash_yaml"
	}
	if looksLikeBase64(content) {
		return "base64"
	}
	return "uri_list"
}

func looksLikeBase64(content string) bool {
	content = strings.TrimSpace(content)
	content = strings.ReplaceAll(content, "\r", "")
	content = strings.ReplaceAll(content, "\n", "")
	content = strings.ReplaceAll(content, " ", "")
	if len(content) < 20 || strings.Contains(content, "://") {
		return false
	}
	if _, err := base64.StdEncoding.DecodeString(content); err == nil {
		return true
	}
	if _, err := base64.RawStdEncoding.DecodeString(content); err == nil {
		return true
	}
	if _, err := base64.URLEncoding.DecodeString(content); err == nil {
		return true
	}
	if _, err := base64.RawURLEncoding.DecodeString(content); err == nil {
		return true
	}
	return false
}

func extractNameFromURI(uri string) string {
	if idx := strings.Index(uri, "#"); idx != -1 {
		return cleanNodeName(uri[idx+1:])
	}
	prefixes := []string{"vless://", "vmess://", "trojan://", "ss://", "hysteria2://", "tuic://", "socks5://", "http://"}
	for _, p := range prefixes {
		if strings.HasPrefix(uri, p) {
			rest := uri[len(p):]
			if idx := strings.Index(rest, "@"); idx != -1 {
				hostPart := rest[idx+1:]
				if idx2 := strings.Index(hostPart, "?"); idx2 != -1 {
					hostPart = hostPart[:idx2]
				}
				if idx2 := strings.Index(hostPart, "#"); idx2 != -1 {
					hostPart = hostPart[:idx2]
				}
				return cleanNodeName(hostPart)
			}
			break
		}
	}
	return "node"
}

func cleanNodeName(name string) string {
	name = strings.TrimSpace(name)
	for i := 0; i < 2; i++ {
		decoded, err := url.QueryUnescape(name)
		if err != nil || decoded == "" || decoded == name {
			break
		}
		name = strings.TrimSpace(decoded)
	}
	return name
}
