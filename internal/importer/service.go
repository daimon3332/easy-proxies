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
	"sort"
	"strings"
	"sync"
	"time"

	"easy_proxies/internal/config"
	"easy_proxies/internal/subfetch"
)

type NodeManager interface {
	CreateNode(ctx context.Context, node config.NodeConfig) (config.NodeConfig, error)
	TriggerReload(ctx context.Context) error
}

type NodeBatchCreator interface {
	CreateNodes(ctx context.Context, nodes []config.NodeConfig) ([]config.NodeConfig, error)
}

type NodeUpdater interface {
	UpdateNode(ctx context.Context, name string, node config.NodeConfig) (config.NodeConfig, error)
}

type NodeBatchUpdater interface {
	UpdateNodes(ctx context.Context, nodes map[string]config.NodeConfig) (map[string]config.NodeConfig, error)
}

type NodeRemover interface {
	DeleteNode(ctx context.Context, name string) error
}

type NodeBatchRemover interface {
	DeleteNodes(ctx context.Context, names []string) error
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

	importCancelsMu sync.Mutex
	importCancels   map[string]context.CancelFunc
	testJobsMu      sync.RWMutex
	testJobs        map[string]*TestJob
	testCancelsMu   sync.Mutex
	testCancels     map[string]context.CancelFunc
	refreshJobsMu   sync.RWMutex
	refreshJobs     map[string]*SourceRefreshJob
}

type Option func(*Service)

const (
	subscriptionSourceRefreshMaxWait = 3 * time.Minute
	subscriptionSourceRetryInterval  = 3 * time.Second
	refreshJobPollInterval           = 500 * time.Millisecond
	refreshJobMaxWait                = 2 * time.Hour
	poolFailureDemoteThreshold       = 3
)

func NewService(store *Store, tester *NodeTester, nodeMgr NodeManager, opts ...Option) *Service {
	s := &Service{
		store:   store,
		tester:  tester,
		nodeMgr: nodeMgr,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		importCancels: make(map[string]context.CancelFunc),
		testJobs:      make(map[string]*TestJob),
		testCancels:   make(map[string]context.CancelFunc),
		refreshJobs:   make(map[string]*SourceRefreshJob),
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

type sourceRefreshTarget struct {
	Key       string
	TagPrefix string
	URLs      []string
}

func (s *Service) StartRefreshSources(key string) (string, error) {
	targets, err := s.sourceRefreshTargets(key)
	if err != nil {
		return "", err
	}
	jobID := randomHex(12)
	job := &SourceRefreshJob{
		ID:        jobID,
		Status:    SourceRefreshJobRunning,
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
		Groups:    make([]SourceRefreshGroup, 0, len(targets)),
	}
	for _, target := range targets {
		group := SourceRefreshGroup{
			Key:       target.Key,
			TagPrefix: target.TagPrefix,
			URLs:      make([]SourceRefreshURL, 0, len(target.URLs)),
		}
		for _, rawURL := range target.URLs {
			group.URLs = append(group.URLs, SourceRefreshURL{
				URL:       rawURL,
				Status:    "waiting",
				UpdatedAt: time.Now(),
			})
		}
		job.Groups = append(job.Groups, group)
	}
	s.recalculateRefreshJob(job)
	s.refreshJobsMu.Lock()
	s.refreshJobs[jobID] = job
	for id, existing := range s.refreshJobs {
		if existing.Status != SourceRefreshJobRunning && time.Since(existing.UpdatedAt) > 10*time.Minute {
			delete(s.refreshJobs, id)
		}
	}
	s.refreshJobsMu.Unlock()
	go s.runRefreshJob(jobID, targets)
	return jobID, nil
}

func (s *Service) GetRefreshJob(jobID string) (SourceRefreshJob, bool) {
	s.refreshJobsMu.RLock()
	defer s.refreshJobsMu.RUnlock()
	job, ok := s.refreshJobs[jobID]
	if !ok {
		return SourceRefreshJob{}, false
	}
	copyJob := *job
	copyJob.Groups = make([]SourceRefreshGroup, len(job.Groups))
	for i, group := range job.Groups {
		copyJob.Groups[i] = group
		copyJob.Groups[i].URLs = append([]SourceRefreshURL(nil), group.URLs...)
	}
	return copyJob, true
}

func (s *Service) sourceRefreshTargets(key string) ([]sourceRefreshTarget, error) {
	key = strings.TrimSpace(key)
	sources, err := s.ListImportSources()
	if err != nil {
		return nil, err
	}
	targets := make([]sourceRefreshTarget, 0, len(sources))
	for _, source := range sources {
		if key != "" && source.Key != key {
			continue
		}
		if !source.Refreshable {
			continue
		}
		urls := splitSubscriptionURLs(source.Source)
		if len(urls) == 0 {
			continue
		}
		tagPrefix := strings.TrimSpace(source.TagPrefix)
		if tagPrefix == "" {
			tagPrefix = "local"
		}
		targets = append(targets, sourceRefreshTarget{
			Key:       source.Key,
			TagPrefix: tagPrefix,
			URLs:      urls,
		})
	}
	if len(targets) == 0 {
		if key != "" {
			return nil, fmt.Errorf("未找到可刷新的订阅来源")
		}
		return nil, fmt.Errorf("没有可刷新的订阅来源")
	}
	return targets, nil
}

func (s *Service) runRefreshJob(jobID string, targets []sourceRefreshTarget) {
	for groupIdx, target := range targets {
		for urlIdx, rawURL := range target.URLs {
			s.updateRefreshJob(jobID, func(job *SourceRefreshJob) {
				if groupIdx >= len(job.Groups) || urlIdx >= len(job.Groups[groupIdx].URLs) {
					return
				}
				row := &job.Groups[groupIdx].URLs[urlIdx]
				row.Status = "pulling"
				row.Error = ""
				row.Nodes = 0
				row.Done = 0
				row.Total = 0
				row.Passed = 0
				row.Failed = 0
				row.Promoted = 0
				row.UpdatedAt = time.Now()
			})

			parsed, err := s.parseRefreshSubscriptionURL(target.TagPrefix, rawURL, subscriptionSourceRefreshMaxWait)
			if err != nil {
				moved, markErr := s.MarkSubscriptionFailed(rawURL, err.Error())
				msg := err.Error()
				if moved > 0 {
					msg = fmt.Sprintf("%s；该订阅旧节点已转入失败节点池（%d 个）", msg, moved)
				}
				if markErr != nil {
					msg = fmt.Sprintf("%s；写入失败节点失败: %v", msg, markErr)
				}
				s.updateRefreshJob(jobID, func(job *SourceRefreshJob) {
					if groupIdx >= len(job.Groups) || urlIdx >= len(job.Groups[groupIdx].URLs) {
						return
					}
					row := &job.Groups[groupIdx].URLs[urlIdx]
					row.Status = "failed"
					row.Error = msg
					if moved > row.Nodes {
						row.Nodes = moved
					}
					row.Promoted = 0
					row.UpdatedAt = time.Now()
				})
				continue
			}

			nodeIDs := make([]string, 0, len(parsed.Nodes))
			for _, node := range parsed.Nodes {
				nodeIDs = append(nodeIDs, node.ID)
			}
			s.updateRefreshJob(jobID, func(job *SourceRefreshJob) {
				if groupIdx >= len(job.Groups) || urlIdx >= len(job.Groups[groupIdx].URLs) {
					return
				}
				row := &job.Groups[groupIdx].URLs[urlIdx]
				row.Status = "testing"
				row.Nodes = len(parsed.Nodes)
				row.Total = len(parsed.Nodes)
				row.UpdatedAt = time.Now()
			})

			commit, err := s.Commit(parsed.ImportID, CommitRequest{
				NodeIDs:       nodeIDs,
				AutoReload:    true,
				PromotePassed: true,
			})
			if err != nil {
				moved, markErr := s.MarkSubscriptionFailed(rawURL, "commit: "+err.Error())
				msg := err.Error()
				if moved > 0 {
					msg = fmt.Sprintf("%s；该订阅旧节点已转入失败节点池（%d 个）", msg, moved)
				}
				if markErr != nil {
					msg = fmt.Sprintf("%s；写入失败节点失败: %v", msg, markErr)
				}
				s.updateRefreshJob(jobID, func(job *SourceRefreshJob) {
					if groupIdx >= len(job.Groups) || urlIdx >= len(job.Groups[groupIdx].URLs) {
						return
					}
					row := &job.Groups[groupIdx].URLs[urlIdx]
					row.Status = "failed"
					row.Error = msg
					row.Nodes = len(parsed.Nodes)
					row.Total = len(parsed.Nodes)
					row.Promoted = 0
					row.UpdatedAt = time.Now()
				})
				continue
			}

			importJob, waitErr := s.waitImportJob(commit.JobID)
			if waitErr != nil || importJob.Status == ImportStatusCanceled {
				msg := "刷新任务被终止"
				if waitErr != nil {
					msg = waitErr.Error()
				} else if strings.TrimSpace(importJob.Error) != "" {
					msg = importJob.Error
				}
				moved, markErr := s.MarkSubscriptionFailed(rawURL, msg)
				if moved > 0 {
					msg = fmt.Sprintf("%s；该订阅旧节点已转入失败节点池（%d 个）", msg, moved)
				}
				if markErr != nil {
					msg = fmt.Sprintf("%s；写入失败节点失败: %v", msg, markErr)
				}
				s.updateRefreshJob(jobID, func(job *SourceRefreshJob) {
					if groupIdx >= len(job.Groups) || urlIdx >= len(job.Groups[groupIdx].URLs) {
						return
					}
					row := &job.Groups[groupIdx].URLs[urlIdx]
					row.Status = "failed"
					row.Error = msg
					row.Nodes = len(parsed.Nodes)
					row.Total = importJob.Total
					row.Done = importJob.Passed + importJob.Failed
					row.Passed = importJob.Passed
					row.Failed = importJob.Failed
					row.Promoted = 0
					row.UpdatedAt = time.Now()
				})
				continue
			}

			s.updateRefreshJob(jobID, func(job *SourceRefreshJob) {
				if groupIdx >= len(job.Groups) || urlIdx >= len(job.Groups[groupIdx].URLs) {
					return
				}
				row := &job.Groups[groupIdx].URLs[urlIdx]
				row.Status = "completed"
				row.Error = strings.TrimSpace(importJob.Error)
				row.Nodes = len(parsed.Nodes)
				row.Total = importJob.Total
				row.Done = importJob.Passed + importJob.Failed
				row.Passed = importJob.Passed
				row.Failed = importJob.Failed
				row.Promoted = importJob.Promoted
				row.UpdatedAt = time.Now()
			})
		}
	}

	s.updateRefreshJob(jobID, func(job *SourceRefreshJob) {
		s.finalizeRefreshJob(job)
		job.UpdatedAt = time.Now()
	})
}

func (s *Service) parseRefreshSubscriptionURL(tagPrefix, subURL string, maxWait time.Duration) (ParseResponse, error) {
	tagPrefix = strings.TrimSpace(tagPrefix)
	if tagPrefix == "" {
		tagPrefix = s.tagPrefixForImportSource(subURL)
	}
	if tagPrefix == "" {
		tagPrefix = "local"
	}
	deadline := time.Now().Add(maxWait)
	var lastErr error
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		resp, err := s.parseRefreshSubscriptionURLOnce(tagPrefix, subURL, remaining)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if time.Until(deadline) <= subscriptionSourceRetryInterval {
			break
		}
		time.Sleep(subscriptionSourceRetryInterval)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("3 分钟内未拉取到任何节点")
	}
	return ParseResponse{}, lastErr
}

func (s *Service) parseRefreshSubscriptionURLOnce(tagPrefix, subURL string, timeout time.Duration) (ParseResponse, error) {
	subURL = strings.TrimSpace(subURL)
	if subURL == "" {
		return ParseResponse{}, fmt.Errorf("订阅 URL 不能为空")
	}
	if !strings.HasPrefix(subURL, "http://") && !strings.HasPrefix(subURL, "https://") {
		return ParseResponse{}, fmt.Errorf("订阅 URL 必须以 http:// 或 https:// 开头")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	body, err := subfetch.Fetch(ctx, subURL, subfetch.Options{
		Timeout: timeout,
		ProxyFallback: func(proxyCtx context.Context, rawURL string, headers http.Header) ([]byte, error) {
			return s.fetchSubscriptionViaPool(proxyCtx, rawURL, headers, timeout)
		},
	})
	if err != nil {
		return ParseResponse{}, fmt.Errorf("获取订阅 %s: %w", subURL, err)
	}
	content := string(body)
	configNodes, err := config.ParseSubscriptionContent(content)
	if err != nil {
		return ParseResponse{}, fmt.Errorf("解析订阅 %s: %w", subURL, err)
	}
	if len(configNodes) == 0 {
		return ParseResponse{}, fmt.Errorf("订阅 %s 未拉取到任何节点", subURL)
	}

	importID := randomHex(12)
	format := detectFormat(content)
	now := time.Now()
	seen := make(map[string]int, len(configNodes))
	nodes := make([]ManagedNode, 0, len(configNodes))
	nodeIDs := make([]string, 0, len(configNodes))
	for _, cn := range configNodes {
		id := nodeID(cn.URI)
		name := cleanNodeName(cn.Name)
		if name == "" {
			name = cleanNodeName(extractNameFromURI(cn.URI))
		}
		mn := ManagedNode{
			ID:           id,
			URI:          cn.URI,
			OriginalName: name,
			Name:         tagPrefix + "-" + name,
			TagPrefix:    tagPrefix,
			ImportID:     importID,
			ImportMode:   "url",
			ImportSource: subURL,
			ImportFormat: format,
			State:        StateParsed,
			Enabled:      true,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if idx, ok := seen[id]; ok {
			nodes[idx] = mn
			continue
		}
		seen[id] = len(nodes)
		nodes = append(nodes, mn)
		nodeIDs = append(nodeIDs, mn.ID)
	}
	if _, err := s.DeleteBySubscription(subURL); err != nil {
		return ParseResponse{}, fmt.Errorf("替换旧节点 %s: %w", subURL, err)
	}
	if err := s.store.UpsertNodes(nodes); err != nil {
		return ParseResponse{}, fmt.Errorf("保存节点: %w", err)
	}
	if err := s.store.UpsertJob(ImportJob{
		ID:        importID,
		Status:    ImportStatusParsed,
		Mode:      "url",
		Format:    format,
		TagPrefix: tagPrefix,
		Source:    subURL,
		Total:     len(nodes),
		NodeIDs:   nodeIDs,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		return ParseResponse{}, fmt.Errorf("保存导入任务: %w", err)
	}
	return ParseResponse{
		ImportID: importID,
		Format:   format,
		Nodes:    nodes,
	}, nil
}

func (s *Service) waitImportJob(jobID string) (ImportJob, error) {
	deadline := time.Now().Add(refreshJobMaxWait)
	for time.Now().Before(deadline) {
		job, ok := s.store.GetJob(jobID)
		if !ok {
			return ImportJob{}, fmt.Errorf("导入任务 %s 不存在", jobID)
		}
		switch job.Status {
		case ImportStatusCompleted, ImportStatusFailed, ImportStatusCanceled:
			return job, nil
		}
		time.Sleep(refreshJobPollInterval)
	}
	return ImportJob{}, fmt.Errorf("等待导入任务 %s 超时", jobID)
}

func (s *Service) recalculateRefreshJob(job *SourceRefreshJob) {
	if job == nil {
		return
	}
	job.TotalURLs = 0
	job.DoneURLs = 0
	job.Successful = 0
	job.Failed = 0
	for gi := range job.Groups {
		group := &job.Groups[gi]
		group.Total = len(group.URLs)
		group.Done = 0
		group.Successful = 0
		group.Failed = 0
		for _, row := range group.URLs {
			job.TotalURLs++
			switch row.Status {
			case "completed":
				group.Done++
				group.Successful++
				job.DoneURLs++
				job.Successful++
			case "failed":
				group.Done++
				group.Failed++
				job.DoneURLs++
				job.Failed++
			}
		}
	}
	job.PoolCount = len(s.store.ListPoolNodes())
	job.UpdatedAt = time.Now()
}

func (s *Service) updateRefreshJob(jobID string, fn func(*SourceRefreshJob)) {
	s.refreshJobsMu.Lock()
	defer s.refreshJobsMu.Unlock()
	job, ok := s.refreshJobs[jobID]
	if !ok {
		return
	}
	fn(job)
	s.recalculateRefreshJob(job)
}

func (s *Service) finalizeRefreshJob(job *SourceRefreshJob) {
	if job == nil {
		return
	}
	job.Status = SourceRefreshJobFinished
	if job.TotalURLs > 0 && job.Successful == 0 {
		job.Status = SourceRefreshJobFailed
		job.Error = "全部订阅链接都未拉取到节点"
		return
	}
	job.Error = ""
}

func (s *Service) Parse(req ParseRequest) (ParseResponse, error) {
	req.TagPrefix = strings.TrimSpace(req.TagPrefix)
	req.URL = strings.TrimSpace(req.URL)
	if req.TagPrefix == "" {
		req.TagPrefix = "local"
	}
	if req.Mode != "url" && req.Mode != "content" {
		return ParseResponse{}, fmt.Errorf("mode 必须为 url 或 content")
	}

	type parsedNode struct {
		node   config.NodeConfig
		source string
		format string
	}
	var parsedNodes []parsedNode
	replaceTag := false
	if req.Mode == "url" {
		urls := splitSubscriptionURLs(req.URL)
		if len(urls) == 0 {
			return ParseResponse{}, fmt.Errorf("url 不能为空")
		}
		for _, subURL := range urls {
			if !strings.HasPrefix(subURL, "http://") && !strings.HasPrefix(subURL, "https://") {
				return ParseResponse{}, fmt.Errorf("url 必须以 http:// 或 https:// 开头")
			}
		}
		if len(urls) == 1 {
			if existingPrefix := s.tagPrefixForImportSource(urls[0]); existingPrefix != "" {
				req.TagPrefix = existingPrefix
			}
		} else if existingPrefix := s.sharedTagPrefixForImportSources(urls); existingPrefix != "" {
			req.TagPrefix = existingPrefix
		}
		timeout := 30 * time.Second
		if s.httpClient != nil && s.httpClient.Timeout > 0 {
			timeout = s.httpClient.Timeout
		}
		for _, subURL := range urls {
			body, err := subfetch.Fetch(context.Background(), subURL, subfetch.Options{
				Timeout: timeout,
				ProxyFallback: func(ctx context.Context, rawURL string, headers http.Header) ([]byte, error) {
					return s.fetchSubscriptionViaPool(ctx, rawURL, headers, timeout)
				},
			})
			if err != nil {
				return ParseResponse{}, fmt.Errorf("获取订阅 %s: %w", subURL, err)
			}
			content := string(body)
			configNodes, err := config.ParseSubscriptionContent(content)
			if err != nil {
				return ParseResponse{}, fmt.Errorf("解析订阅 %s: %w", subURL, err)
			}
			format := detectFormat(content)
			for _, cn := range configNodes {
				parsedNodes = append(parsedNodes, parsedNode{node: cn, source: subURL, format: format})
			}
		}
		replaceTag = true
	} else {
		content := req.Content
		configNodes, err := config.ParseSubscriptionContent(content)
		if err != nil {
			return ParseResponse{}, fmt.Errorf("解析订阅: %w", err)
		}
		format := detectFormat(content)
		for _, cn := range configNodes {
			parsedNodes = append(parsedNodes, parsedNode{node: cn, source: req.Mode, format: format})
		}
	}
	if len(parsedNodes) == 0 {
		return ParseResponse{}, fmt.Errorf("未找到有效节点")
	}

	importID := randomHex(12)
	format := parsedNodes[0].format
	nodes := make([]ManagedNode, 0, len(parsedNodes))
	nodeIDs := make([]string, 0, len(parsedNodes))
	now := time.Now()
	existingNodes := s.nodesByID()
	seen := make(map[string]int, len(parsedNodes))

	for _, item := range parsedNodes {
		cn := item.node
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
			ImportSource: item.source,
			ImportFormat: item.format,
			State:        StateParsed,
			Enabled:      true,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if existing, ok := existingNodes[id]; ok && !replaceTag {
			mn = mergeImportedNode(existing, mn)
		}
		if idx, ok := seen[id]; ok {
			nodes[idx] = mn
			continue
		}
		seen[id] = len(nodes)
		nodes = append(nodes, mn)
	}
	for _, n := range nodes {
		nodeIDs = append(nodeIDs, n.ID)
	}

	if replaceTag {
		if _, err := s.deleteByTagPrefix(req.TagPrefix); err != nil {
			return ParseResponse{}, fmt.Errorf("替换旧节点: %w", err)
		}
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

func splitSubscriptionURLs(raw string) []string {
	seen := make(map[string]struct{})
	lines := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r'
	})
	urls := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		urls = append(urls, line)
	}
	return urls
}

func (s *Service) fetchSubscriptionViaPool(ctx context.Context, rawURL string, headers http.Header, timeout time.Duration) ([]byte, error) {
	nodes := s.store.ListPoolNodes()
	if len(nodes) == 0 {
		return nil, fmt.Errorf("没有可用的节点池节点可用于拉取订阅")
	}
	var errs []string
	for _, node := range nodes {
		if strings.TrimSpace(node.URI) == "" {
			continue
		}
		client, closeClient, err := NewHTTPClientForURI(ctx, s.tester.buildOutbound, node.ID, node.URI, timeout, s.tester.skipCertVerify)
		if err != nil {
			errs = append(errs, node.Name+": "+err.Error())
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
		return nil, fmt.Errorf("节点池中没有可用于拉取订阅的节点")
	}
	return nil, fmt.Errorf("%s", strings.Join(errs, " | "))
}

func (s *Service) nodesByID() map[string]ManagedNode {
	nodes := s.store.ListNodes()
	byID := make(map[string]ManagedNode, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	return byID
}

func (s *Service) tagPrefixForImportSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return ""
	}
	for _, node := range s.store.ListNodes() {
		if strings.TrimSpace(node.ImportSource) == source && strings.TrimSpace(node.TagPrefix) != "" {
			return strings.TrimSpace(node.TagPrefix)
		}
	}
	return ""
}

func (s *Service) sharedTagPrefixForImportSources(sources []string) string {
	seen := make(map[string]struct{})
	matched := 0
	for _, source := range sources {
		if prefix := s.tagPrefixForImportSource(source); prefix != "" {
			seen[prefix] = struct{}{}
			matched++
		}
	}
	if len(seen) != 1 || matched != len(sources) {
		return ""
	}
	for prefix := range seen {
		return prefix
	}
	return ""
}

func mergeImportedNode(existing, incoming ManagedNode) ManagedNode {
	incoming.State = existing.State
	incoming.Enabled = existing.Enabled
	incoming.InPool = existing.InPool
	incoming.Port = existing.Port
	incoming.Order = existing.Order
	incoming.LatencyMs = existing.LatencyMs
	incoming.CountryCode = existing.CountryCode
	incoming.CountryName = existing.CountryName
	incoming.LastError = existing.LastError
	incoming.LastTestAt = existing.LastTestAt
	incoming.ConsecutiveFailures = existing.ConsecutiveFailures
	incoming.CreatedAt = existing.CreatedAt
	if existing.OriginalName != "" {
		incoming.OriginalName = existing.OriginalName
	}
	if existing.Name != "" {
		incoming.Name = existing.Name
	}
	if existing.TagPrefix != "" {
		incoming.TagPrefix = existing.TagPrefix
	}
	return incoming
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
		jobID := randomHex(12)
		now := time.Now()
		job = ImportJob{
			ID:        jobID,
			Status:    ImportStatusCompleted,
			Total:     0,
			NodeIDs:   selectedIDs,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := s.store.UpsertJob(job); err != nil {
			return CommitResponse{}, err
		}
		return CommitResponse{JobID: jobID}, nil
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

	ctx, cancel := context.WithCancel(context.Background())
	s.registerImportCancel(jobID, cancel)
	go s.runPipeline(ctx, jobID, nodes, req.PromotePassed)

	return CommitResponse{JobID: jobID}, nil
}

func (s *Service) registerImportCancel(jobID string, cancel context.CancelFunc) {
	s.importCancelsMu.Lock()
	s.importCancels[jobID] = cancel
	s.importCancelsMu.Unlock()
}

func (s *Service) unregisterImportCancel(jobID string) {
	s.importCancelsMu.Lock()
	delete(s.importCancels, jobID)
	s.importCancelsMu.Unlock()
}

func (s *Service) CancelImportJob(jobID string) (ImportJob, error) {
	job, ok := s.store.GetJob(jobID)
	if !ok {
		return ImportJob{}, fmt.Errorf("导入任务 %s 不存在", jobID)
	}
	if job.Status != ImportStatusRunning {
		return job, nil
	}
	s.importCancelsMu.Lock()
	cancel := s.importCancels[jobID]
	s.importCancelsMu.Unlock()
	if cancel != nil {
		cancel()
		_ = s.store.UpdateJob(jobID, func(j *ImportJob) {
			j.Error = "正在终止"
			j.UpdatedAt = time.Now()
		})
		updated, _ := s.store.GetJob(jobID)
		return updated, nil
	}
	_ = s.store.UpdateJob(jobID, func(j *ImportJob) {
		j.Status = ImportStatusCanceled
		j.Error = "已终止"
		j.UpdatedAt = time.Now()
	})
	updated, _ := s.store.GetJob(jobID)
	return updated, nil
}

func (s *Service) runPipeline(ctx context.Context, jobID string, nodes []ManagedNode, promotePassed bool) {
	defer s.unregisterImportCancel(jobID)

	total := len(nodes)
	passed := 0
	failed := 0
	promoted := 0
	processed := 0
	flushEvery := importProgressBatchSize(total)
	updates := make([]ManagedNode, 0, len(nodes))
	passedIDs := make([]string, 0, len(nodes))

	for event := range s.tester.ProbeBatch(ctx, nodes) {
		node, ok := s.store.GetNode(event.NodeID)
		if !ok {
			continue
		}
		if event.Result.Error != nil {
			updated, _ := failedNodeUpdate(node, event.Result.Error.Error())
			updates = append(updates, updated)
			failed++
		} else {
			updates = append(updates, probePassedNodeUpdate(node, event.Result))
			passedIDs = append(passedIDs, node.ID)
			passed++
		}
		processed++
		if processed%flushEvery == 0 || processed == total {
			s.updateImportProgress(jobID, passed, failed, promoted)
		}
	}
	if len(updates) > 0 {
		_ = s.store.UpsertNodes(updates)
	}
	canceled := ctx.Err() != nil
	if promotePassed && len(passedIDs) > 0 && !canceled {
		if promotedNodes, err := s.PromoteMany(passedIDs, true); err == nil {
			promoted = len(promotedNodes)
			s.updateImportProgress(jobID, passed, failed, promoted)
		} else {
			s.store.UpdateJob(jobID, func(j *ImportJob) {
				j.Error = err.Error()
				j.UpdatedAt = time.Now()
			})
		}
	}

	status := ImportStatusCompleted
	if canceled {
		status = ImportStatusCanceled
	} else if failed == total {
		status = ImportStatusFailed
	}
	s.store.UpdateJob(jobID, func(j *ImportJob) {
		j.Status = status
		j.Passed = passed
		j.Failed = failed
		j.Promoted = promoted
		if status == ImportStatusCanceled {
			j.Error = "已终止"
		}
		j.UpdatedAt = time.Now()
	})

	// Initial import is a pure generate_204 probe. Depending on the UI option,
	// passed nodes either remain candidates or are promoted into the runtime pool.
}

func (s *Service) updateImportProgress(jobID string, passed, failed, promoted int) {
	_ = s.store.UpdateJob(jobID, func(j *ImportJob) {
		j.Passed = passed
		j.Failed = failed
		j.Promoted = promoted
		j.UpdatedAt = time.Now()
	})
}

func importProgressBatchSize(total int) int {
	if total <= 1 {
		return 1
	}
	size := total / 20
	if size < 1 {
		return 1
	}
	if size > 10 {
		return 10
	}
	return size
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
	needReload := false
	if req.Retest {
		updates := make([]ManagedNode, 0, len(nodes))
		poolNamesToDelete := make([]string, 0)
		for event := range s.tester.ProbeBatch(context.Background(), nodes) {
			node, ok := s.store.GetNode(event.NodeID)
			if !ok {
				continue
			}
			mu.Lock()
			resp.Retested++
			mu.Unlock()
			if event.Result.Error != nil {
				updated, poolName := failedNodeUpdate(node, event.Result.Error.Error())
				updates = append(updates, updated)
				if poolName != "" {
					poolNamesToDelete = append(poolNamesToDelete, poolName)
					needReload = true
				}
				mu.Lock()
				resp.Failed++
				changed = true
				mu.Unlock()
				continue
			}
			updates = append(updates, probePassedNodeUpdate(node, event.Result))
			mu.Lock()
			resp.Passed++
			mu.Unlock()
		}
		if len(poolNamesToDelete) > 0 {
			s.deleteConfigNodes(poolNamesToDelete)
		}
		if len(updates) > 0 {
			if err := s.store.UpsertNodes(updates); err != nil {
				return resp, err
			}
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
		updates := make([]ManagedNode, 0, len(countryNodes))
		configUpdates := make(map[string]config.NodeConfig)
		usedNames := s.usedNodeNames()
		for event := range s.tester.CountryBatch(context.Background(), countryNodes) {
			node, ok := s.store.GetNode(event.NodeID)
			if !ok {
				continue
			}
			if event.Result.Error != nil {
				node.LastError = event.Result.Error.Error()
				node.UpdatedAt = time.Now()
				updates = append(updates, node)
				mu.Lock()
				resp.CountryBad++
				mu.Unlock()
				continue
			}
			updated, oldName, needsConfigUpdate := s.countryNodeUpdateWithNames(node, event.Result, usedNames)
			if needsConfigUpdate {
				configUpdates[oldName] = updated.ToConfigNode()
				needReload = true
			}
			updates = append(updates, updated)
			mu.Lock()
			resp.CountryOK++
			changed = true
			mu.Unlock()
		}
		if len(configUpdates) > 0 {
			normalized, err := s.updateConfigNodes(configUpdates)
			if err != nil {
				return resp, err
			}
			for i := range updates {
				if cn, ok := normalized[updates[i].Name]; ok {
					updates[i].Port = cn.Port
				}
			}
		}
		if len(updates) > 0 {
			if err := s.store.UpsertNodes(updates); err != nil {
				return resp, err
			}
		}
	}

	if req.PromotePassed {
		promoted, err := s.PromoteMany(req.NodeIDs, false)
		if err == nil && len(promoted) > 0 {
			resp.Promoted += len(promoted)
			changed = true
			needReload = true
		}
	}
	if needReload || (changed && (req.AutoReload || req.PromotePassed)) {
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

	ctx, cancel := context.WithCancel(context.Background())
	s.registerTestCancel(jobID, cancel)
	go s.runBatchTestJob(ctx, jobID, req)
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

func (s *Service) registerTestCancel(jobID string, cancel context.CancelFunc) {
	s.testCancelsMu.Lock()
	s.testCancels[jobID] = cancel
	s.testCancelsMu.Unlock()
}

func (s *Service) unregisterTestCancel(jobID string) {
	s.testCancelsMu.Lock()
	delete(s.testCancels, jobID)
	s.testCancelsMu.Unlock()
}

func (s *Service) CancelTestJob(jobID string) (TestJob, error) {
	job, ok := s.GetTestJob(jobID)
	if !ok {
		return TestJob{}, fmt.Errorf("job 不存在或已过期")
	}
	if job.Status != TestJobRunning {
		return job, nil
	}
	s.testCancelsMu.Lock()
	cancel := s.testCancels[jobID]
	s.testCancelsMu.Unlock()
	if cancel != nil {
		cancel()
		s.updateJob(jobID, func(j *TestJob) {
			j.Phase = "canceling"
			j.Error = "正在终止"
		})
		updated, _ := s.GetTestJob(jobID)
		return updated, nil
	}
	s.updateJob(jobID, func(j *TestJob) {
		j.Status = TestJobCanceled
		j.Phase = "canceled"
		j.Error = "已终止"
	})
	updated, _ := s.GetTestJob(jobID)
	return updated, nil
}

func (s *Service) runBatchTestJob(ctx context.Context, jobID string, req BatchTestRequest) {
	defer s.unregisterTestCancel(jobID)
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
	needReload := false
	finish := func(status TestJobStatus, phase, errText string) {
		if needReload || (changed && (req.AutoReload || req.PromotePassed)) {
			_ = s.nodeMgr.TriggerReload(context.Background())
		}
		s.updateJob(jobID, func(j *TestJob) {
			j.Status = status
			j.Phase = phase
			j.Error = errText
		})
	}

	// --- Phase: probe ---
	if req.Retest {
		s.updateJob(jobID, func(j *TestJob) { j.Phase = "probe"; j.Done = 0 })
		updates := make([]ManagedNode, 0, len(nodes))
		poolNamesToDelete := make([]string, 0)
		for event := range s.tester.ProbeBatch(ctx, nodes) {
			node, ok := s.store.GetNode(event.NodeID)
			if !ok {
				s.updateJob(jobID, func(j *TestJob) { j.Done++ })
				continue
			}
			if event.Result.Error != nil {
				updated, poolName := failedNodeUpdate(node, event.Result.Error.Error())
				updates = append(updates, updated)
				if poolName != "" {
					poolNamesToDelete = append(poolNamesToDelete, poolName)
					needReload = true
				}
				changed = true
				s.updateJob(jobID, func(j *TestJob) { j.Done++; j.Failed++ })
				continue
			}
			updates = append(updates, probePassedNodeUpdate(node, event.Result))
			s.updateJob(jobID, func(j *TestJob) { j.Done++; j.Passed++ })
		}
		if len(poolNamesToDelete) > 0 {
			s.deleteConfigNodes(poolNamesToDelete)
		}
		if len(updates) > 0 {
			_ = s.store.UpsertNodes(updates)
		}
	}
	if ctx.Err() != nil {
		finish(TestJobCanceled, "canceled", "已终止")
		return
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
		updates := make([]ManagedNode, 0, len(countryNodes))
		configUpdates := make(map[string]config.NodeConfig)
		usedNames := s.usedNodeNames()
		for event := range s.tester.CountryBatch(ctx, countryNodes) {
			node, ok := s.store.GetNode(event.NodeID)
			if !ok {
				s.updateJob(jobID, func(j *TestJob) { j.Done++ })
				continue
			}
			if event.Result.Error != nil {
				node.LastError = event.Result.Error.Error()
				node.UpdatedAt = time.Now()
				updates = append(updates, node)
				s.updateJob(jobID, func(j *TestJob) { j.Done++; j.CountryBad++ })
				continue
			}
			updated, oldName, needsConfigUpdate := s.countryNodeUpdateWithNames(node, event.Result, usedNames)
			if needsConfigUpdate {
				configUpdates[oldName] = updated.ToConfigNode()
				needReload = true
			}
			updates = append(updates, updated)
			changed = true
			s.updateJob(jobID, func(j *TestJob) { j.Done++; j.CountryOK++ })
		}
		if len(configUpdates) > 0 {
			if normalized, err := s.updateConfigNodes(configUpdates); err == nil {
				for i := range updates {
					if cn, ok := normalized[updates[i].Name]; ok {
						updates[i].Port = cn.Port
					}
				}
			} else {
				s.updateJob(jobID, func(j *TestJob) {
					j.Status = TestJobFailed
					j.Error = err.Error()
				})
				return
			}
		}
		if len(updates) > 0 {
			_ = s.store.UpsertNodes(updates)
		}
	}
	if ctx.Err() != nil {
		finish(TestJobCanceled, "canceled", "已终止")
		return
	}

	// --- Phase: promote ---
	if req.PromotePassed {
		s.updateJob(jobID, func(j *TestJob) { j.Phase = "promote" })
		promoted, err := s.PromoteMany(req.NodeIDs, false)
		if err == nil && len(promoted) > 0 {
			changed = true
			needReload = true
			s.updateJob(jobID, func(j *TestJob) { j.Promoted += len(promoted) })
		}
	}

	finish(TestJobFinished, "done", "")
}

func (s *Service) markProbePassed(node ManagedNode, result TestResult) error {
	return s.store.UpsertNode(probePassedNodeUpdate(node, result))
}

func (s *Service) markPassed(node ManagedNode, result TestResult) error {
	return s.store.UpsertNode(s.passedNodeUpdateWithNames(node, result, s.usedNodeNames()))
}

func (s *Service) markCountry(node ManagedNode, result TestResult) error {
	node, oldName, needsConfigUpdate := s.countryNodeUpdateWithNames(node, result, s.usedNodeNames())
	if needsConfigUpdate {
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
	node, oldName := failedNodeUpdate(node, lastErr)
	if err := s.store.UpsertNode(node); err != nil {
		return err
	}
	if oldName != "" {
		s.deleteConfigNodes([]string{oldName})
		_ = s.nodeMgr.TriggerReload(context.Background())
	}
	return nil
}

func probePassedNodeUpdate(node ManagedNode, result TestResult) ManagedNode {
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
	node.ConsecutiveFailures = 0
	node.LastTestAt = time.Now()
	node.UpdatedAt = node.LastTestAt
	return node
}

func (s *Service) passedNodeUpdateWithNames(node ManagedNode, result TestResult, usedNames map[string]struct{}) ManagedNode {
	node = probePassedNodeUpdate(node, result)
	node.CountryCode = result.CountryCode
	node.CountryName = result.CountryName
	if node.CountryCode != "" {
		if usedNames != nil && node.Name != "" {
			delete(usedNames, node.Name)
		}
		node.Name = nextCountryNameWithNames(node.TagPrefix, node.CountryCode, usedNames)
	}
	return node
}

func (s *Service) countryNodeUpdateWithNames(node ManagedNode, result TestResult, usedNames map[string]struct{}) (ManagedNode, string, bool) {
	oldName := node.Name
	node.CountryCode = result.CountryCode
	node.CountryName = result.CountryName
	if node.CountryCode != "" {
		if usedNames != nil && oldName != "" {
			delete(usedNames, oldName)
		}
		node.Name = nextCountryNameWithNames(node.TagPrefix, node.CountryCode, usedNames)
	}
	node.LastError = ""
	node.LastTestAt = time.Now()
	needsConfigUpdate := (node.InPool || node.State == StateInPool) && oldName != "" && node.Name != oldName
	return node, oldName, needsConfigUpdate
}

func failedNodeUpdate(node ManagedNode, lastErr string) (ManagedNode, string) {
	oldName := ""
	now := time.Now()
	node.ConsecutiveFailures++
	node.LastError = lastErr
	node.LastTestAt = now
	node.UpdatedAt = now
	if node.InPool || node.State == StateInPool {
		if node.ConsecutiveFailures < poolFailureDemoteThreshold {
			node.State = StateInPool
			node.InPool = true
			return node, ""
		}
		oldName = node.Name
	}
	node.State = StateFailed
	node.InPool = false
	node.Port = 0
	node.LatencyMs = 0
	node.CountryCode = ""
	node.CountryName = ""
	node.Name = taggedOriginalName(node.TagPrefix, node.OriginalName)
	return node, oldName
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

func (s *Service) PromoteMany(nodeIDs []string, autoReload bool) ([]ManagedNode, error) {
	if len(nodeIDs) == 0 {
		return nil, fmt.Errorf("请选择要加入节点池的节点")
	}
	nodes := make([]ManagedNode, 0, len(nodeIDs))
	duplicateIDs := make([]string, 0)
	existingNames, existingURIs := s.existingConfigNodeKeys()
	usedNames := make(map[string]struct{}, len(existingNames)+len(nodeIDs))
	for name := range existingNames {
		usedNames[name] = struct{}{}
	}
	seenURIs := make(map[string]struct{}, len(nodeIDs))
	for _, id := range nodeIDs {
		node, ok := s.store.GetNode(id)
		if !ok || node.InPool || node.State == StateInPool || node.State != StatePassed {
			continue
		}
		name := strings.TrimSpace(node.Name)
		uri := strings.TrimSpace(node.URI)
		if uri != "" {
			if _, ok := existingURIs[uri]; ok {
				duplicateIDs = append(duplicateIDs, node.ID)
				continue
			}
			if _, ok := seenURIs[uri]; ok {
				duplicateIDs = append(duplicateIDs, node.ID)
				continue
			}
			seenURIs[uri] = struct{}{}
		}
		if name == "" {
			name = taggedOriginalName(node.TagPrefix, node.OriginalName)
		}
		node.Name = nextUniqueName(name, usedNames)
		nodes = append(nodes, node)
	}
	if len(duplicateIDs) > 0 {
		_ = s.store.DeleteNodes(duplicateIDs)
	}
	if len(nodes) == 0 {
		return nil, nil
	}
	if err := s.store.UpsertNodes(nodes); err != nil {
		return nil, err
	}

	configNodes := make([]config.NodeConfig, 0, len(nodes))
	for _, node := range nodes {
		configNodes = append(configNodes, node.ToConfigNode())
	}

	created := make([]config.NodeConfig, 0, len(configNodes))
	createdIDs := make([]string, 0, len(configNodes))
	if creator, ok := s.nodeMgr.(NodeBatchCreator); ok {
		var err error
		created, err = creator.CreateNodes(context.Background(), configNodes)
		if err != nil {
			created = created[:0]
			createdIDs = createdIDs[:0]
			for i, cn := range configNodes {
				createdNode, createErr := s.nodeMgr.CreateNode(context.Background(), cn)
				if createErr != nil {
					if isNodeConflict(createErr) {
						_ = s.store.DeleteNode(nodes[i].ID)
						continue
					}
					return nil, createErr
				}
				created = append(created, createdNode)
				createdIDs = append(createdIDs, nodes[i].ID)
			}
		}
	} else {
		for i, cn := range configNodes {
			createdNode, err := s.nodeMgr.CreateNode(context.Background(), cn)
			if err != nil {
				if isNodeConflict(err) {
					_ = s.store.DeleteNode(nodes[i].ID)
					continue
				}
				return nil, err
			}
			created = append(created, createdNode)
			createdIDs = append(createdIDs, nodes[i].ID)
		}
	}
	if len(createdIDs) == 0 && len(created) > 0 {
		for i := range created {
			if i >= len(nodes) {
				break
			}
			createdIDs = append(createdIDs, nodes[i].ID)
		}
	}

	ports := make(map[string]uint16, len(created))
	for i, cn := range created {
		if i >= len(createdIDs) {
			break
		}
		ports[createdIDs[i]] = cn.Port
	}
	updated, err := s.store.MarkInPoolMany(ports)
	if err != nil {
		return nil, fmt.Errorf("mark in pool: %w", err)
	}
	if autoReload && len(updated) > 0 {
		_ = s.nodeMgr.TriggerReload(context.Background())
	}
	return updated, nil
}

func (s *Service) existingConfigNodeKeys() (map[string]struct{}, map[string]struct{}) {
	names := make(map[string]struct{})
	uris := make(map[string]struct{})
	lister, ok := s.nodeMgr.(NodeLister)
	if !ok {
		return names, uris
	}
	configNodes, err := lister.ListConfigNodes(context.Background())
	if err != nil {
		return names, uris
	}
	for _, cn := range configNodes {
		if name := strings.TrimSpace(cn.Name); name != "" {
			names[name] = struct{}{}
		}
		if uri := strings.TrimSpace(cn.URI); uri != "" {
			uris[uri] = struct{}{}
		}
	}
	return names, uris
}

func isNodeConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "节点名称或端口已存在") ||
		strings.Contains(msg, "节点已存在") ||
		strings.Contains(msg, "已存在") ||
		strings.Contains(strings.ToLower(msg), "already exists")
}

func nextUniqueName(base string, used map[string]struct{}) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "node"
	}
	if used == nil {
		return base
	}
	if _, exists := used[base]; !exists {
		used[base] = struct{}{}
		return base
	}
	for next := 2; ; next++ {
		name := fmt.Sprintf("%s-%d", base, next)
		if _, exists := used[name]; !exists {
			used[name] = struct{}{}
			return name
		}
	}
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
		s.deleteConfigNodes([]string{node.Name})
		_ = s.nodeMgr.TriggerReload(context.Background())
	}
	return s.store.DeleteNode(nodeID)
}

func (s *Service) DeleteMany(nodeIDs []string) (int, error) {
	if len(nodeIDs) == 0 {
		return 0, fmt.Errorf("请选择要删除的节点")
	}
	want := make(map[string]struct{}, len(nodeIDs))
	for _, id := range nodeIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			want[id] = struct{}{}
		}
	}
	if len(want) == 0 {
		return 0, fmt.Errorf("请选择要删除的节点")
	}

	all := s.store.ListNodes()
	storeIDs := make([]string, 0, len(want))
	poolNames := make([]string, 0)
	for _, n := range all {
		if _, ok := want[n.ID]; !ok {
			continue
		}
		storeIDs = append(storeIDs, n.ID)
		if (n.InPool || n.State == StateInPool) && strings.TrimSpace(n.Name) != "" {
			poolNames = append(poolNames, n.Name)
		}
	}
	if len(storeIDs) == 0 {
		return 0, fmt.Errorf("没有找到可删除的节点")
	}

	if len(poolNames) > 0 {
		s.deleteConfigNodes(poolNames)
		_ = s.nodeMgr.TriggerReload(context.Background())
	}
	if err := s.store.DeleteNodes(storeIDs); err != nil {
		return 0, err
	}
	return len(storeIDs), nil
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
	ids := make([]string, 0)
	poolNames := make([]string, 0)
	for _, n := range all {
		if n.ImportSource != url {
			continue
		}
		ids = append(ids, n.ID)
		if (n.InPool || n.State == StateInPool) && strings.TrimSpace(n.Name) != "" {
			poolNames = append(poolNames, n.Name)
		}
	}
	if len(poolNames) > 0 {
		s.deleteConfigNodes(poolNames)
		_ = s.nodeMgr.TriggerReload(context.Background())
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if err := s.store.DeleteNodes(ids); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (s *Service) MarkSubscriptionFailed(url, lastErr string) (int, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return 0, fmt.Errorf("订阅 URL 不能为空")
	}
	if strings.TrimSpace(lastErr) == "" {
		lastErr = "订阅刷新失败"
	}
	all := s.store.ListNodes()
	updates := make([]ManagedNode, 0)
	poolNames := make([]string, 0)
	for _, node := range all {
		if strings.TrimSpace(node.ImportSource) != url {
			continue
		}
		updated, poolName := failedNodeUpdate(node, lastErr)
		updates = append(updates, updated)
		if poolName != "" {
			poolNames = append(poolNames, poolName)
		}
	}
	if len(poolNames) > 0 {
		s.deleteConfigNodes(poolNames)
		_ = s.nodeMgr.TriggerReload(context.Background())
	}
	if len(updates) == 0 {
		return 0, nil
	}
	if err := s.store.UpsertNodes(updates); err != nil {
		return 0, err
	}
	return len(updates), nil
}

func (s *Service) DeleteImportSource(key string) (int, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return 0, fmt.Errorf("导入来源不能为空")
	}
	if source, ok := strings.CutPrefix(key, "url:"); ok {
		return s.DeleteBySubscription(source)
	}
	if tagPrefix, ok := strings.CutPrefix(key, "tag:"); ok {
		return s.deleteByTagPrefix(tagPrefix)
	}
	all := s.store.ListNodes()
	ids := make([]string, 0)
	poolNames := make([]string, 0)
	for _, n := range all {
		if importSourceKey(n) != key {
			continue
		}
		ids = append(ids, n.ID)
		if (n.InPool || n.State == StateInPool) && strings.TrimSpace(n.Name) != "" {
			poolNames = append(poolNames, n.Name)
		}
	}
	if len(poolNames) > 0 {
		s.deleteConfigNodes(poolNames)
		_ = s.nodeMgr.TriggerReload(context.Background())
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if err := s.store.DeleteNodes(ids); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (s *Service) deleteByTagPrefix(tagPrefix string) (int, error) {
	tagPrefix = strings.TrimSpace(tagPrefix)
	if tagPrefix == "" {
		return 0, fmt.Errorf("标签不能为空")
	}
	all := s.store.ListNodes()
	ids := make([]string, 0)
	poolNames := make([]string, 0)
	for _, n := range all {
		if strings.TrimSpace(n.TagPrefix) != tagPrefix {
			continue
		}
		ids = append(ids, n.ID)
		if (n.InPool || n.State == StateInPool) && strings.TrimSpace(n.Name) != "" {
			poolNames = append(poolNames, n.Name)
		}
	}
	if len(poolNames) > 0 {
		s.deleteConfigNodes(poolNames)
		_ = s.nodeMgr.TriggerReload(context.Background())
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if err := s.store.DeleteNodes(ids); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (s *Service) DeleteAllImportSources() (int, error) {
	all := s.store.ListNodes()
	if len(all) == 0 {
		return 0, nil
	}
	ids := make([]string, 0, len(all))
	poolNames := make([]string, 0)
	for _, node := range all {
		ids = append(ids, node.ID)
		if (node.InPool || node.State == StateInPool) && strings.TrimSpace(node.Name) != "" {
			poolNames = append(poolNames, node.Name)
		}
	}
	if len(poolNames) > 0 {
		s.deleteConfigNodes(poolNames)
		_ = s.nodeMgr.TriggerReload(context.Background())
	}
	if err := s.store.DeleteNodes(ids); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (s *Service) ListImportSources() ([]ImportSourceSummary, error) {
	nodes := s.store.ListNodes()
	groups := make(map[string]*ImportSourceSummary)
	for _, node := range nodes {
		key := importSourceKey(node)
		if key == "" {
			key = "node:" + node.ID
		}
		group := groups[key]
		if group == nil {
			group = &ImportSourceSummary{
				Key:         key,
				ImportID:    node.ImportID,
				Mode:        node.ImportMode,
				Format:      node.ImportFormat,
				TagPrefix:   node.TagPrefix,
				Source:      node.ImportSource,
				Refreshable: node.ImportMode == "url" && strings.TrimSpace(node.ImportSource) != "",
				CreatedAt:   node.CreatedAt,
				UpdatedAt:   node.UpdatedAt,
			}
			groups[key] = group
		}
		if group.TagPrefix == "" {
			group.TagPrefix = node.TagPrefix
		}
		if group.Format == "" {
			group.Format = node.ImportFormat
		}
		if group.Mode == "" {
			group.Mode = node.ImportMode
		}
		if group.Source == "" {
			group.Source = node.ImportSource
		} else if node.ImportMode == "url" && strings.TrimSpace(node.ImportSource) != "" && !sourceListContains(group.Source, node.ImportSource) {
			group.Source += "\n" + strings.TrimSpace(node.ImportSource)
		}
		if group.CreatedAt.IsZero() || (!node.CreatedAt.IsZero() && node.CreatedAt.Before(group.CreatedAt)) {
			group.CreatedAt = node.CreatedAt
		}
		if node.UpdatedAt.After(group.UpdatedAt) {
			group.UpdatedAt = node.UpdatedAt
		}
		group.Total++
		switch {
		case node.InPool || node.State == StateInPool:
			group.Pool++
		case node.State == StatePassed:
			group.Candidate++
		case node.State == StateFailed:
			group.Failed++
		}
	}
	result := make([]ImportSourceSummary, 0, len(groups))
	for _, group := range groups {
		result = append(result, *group)
	}
	sort.Slice(result, func(i, j int) bool {
		if !result[i].UpdatedAt.Equal(result[j].UpdatedAt) {
			return result[i].UpdatedAt.After(result[j].UpdatedAt)
		}
		return result[i].Key < result[j].Key
	})
	return result, nil
}

func importSourceKey(node ManagedNode) string {
	source := strings.TrimSpace(node.ImportSource)
	if node.ImportMode == "url" && source != "" {
		if tag := strings.TrimSpace(node.TagPrefix); tag != "" {
			return "tag:" + tag
		}
		return "url:" + source
	}
	if strings.TrimSpace(node.ImportID) != "" {
		return "import:" + strings.TrimSpace(node.ImportID)
	}
	if source != "" {
		return "source:" + strings.TrimSpace(node.ImportMode) + ":" + strings.TrimSpace(node.ImportFormat) + ":" + strings.TrimSpace(node.TagPrefix) + ":" + source
	}
	return ""
}

func sourceListContains(list, source string) bool {
	source = strings.TrimSpace(source)
	for _, item := range splitSubscriptionURLs(list) {
		if item == source {
			return true
		}
	}
	return false
}

func (s *Service) deleteConfigNodes(names []string) {
	clean := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		clean = append(clean, name)
	}
	if len(clean) == 0 {
		return
	}
	if remover, ok := s.nodeMgr.(NodeBatchRemover); ok {
		_ = remover.DeleteNodes(context.Background(), clean)
		return
	}
	if remover, ok := s.nodeMgr.(NodeRemover); ok {
		for _, name := range clean {
			_ = remover.DeleteNode(context.Background(), name)
		}
	}
}

func (s *Service) updateConfigNodes(nodes map[string]config.NodeConfig) (map[string]config.NodeConfig, error) {
	if len(nodes) == 0 {
		return nil, nil
	}
	if updater, ok := s.nodeMgr.(NodeBatchUpdater); ok {
		return updater.UpdateNodes(context.Background(), nodes)
	}
	updated := make(map[string]config.NodeConfig, len(nodes))
	if updater, ok := s.nodeMgr.(NodeUpdater); ok {
		for oldName, node := range nodes {
			cn, err := updater.UpdateNode(context.Background(), oldName, node)
			if err != nil {
				return updated, err
			}
			updated[cn.Name] = cn
		}
	}
	return updated, nil
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

func (s *Service) Summary() (DashboardSummary, error) {
	nodes := s.syncRuntimeNodes(s.store.ListNodes())
	summary := DashboardSummary{
		Ports:     make([]ManagedNode, 0),
		UpdatedAt: time.Now(),
	}
	for _, node := range nodes {
		summary.Total++
		switch {
		case node.InPool || node.State == StateInPool:
			summary.InPool++
			summary.Ports = append(summary.Ports, node)
		case node.State == StateParsed:
			summary.Parsed++
		case node.State == StateTesting:
			summary.Testing++
		case node.State == StatePassed:
			summary.Passed++
		case node.State == StateFailed:
			summary.Failed++
		case node.State == StateExcluded:
			summary.Excluded++
		}
	}
	sort.Slice(summary.Ports, func(i, j int) bool {
		return summary.Ports[i].Order < summary.Ports[j].Order
	})
	return summary, nil
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
	used := s.usedNodeNames()
	if currentID != "" {
		for _, n := range s.store.ListNodes() {
			if n.ID == currentID {
				delete(used, n.Name)
				break
			}
		}
	}
	return nextCountryNameWithNames(tagPrefix, countryCode, used)
}

func (s *Service) usedNodeNames() map[string]struct{} {
	nodes := s.store.ListNodes()
	used := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		if n.Name != "" {
			used[n.Name] = struct{}{}
		}
	}
	return used
}

func nextCountryNameWithNames(tagPrefix, countryCode string, used map[string]struct{}) string {
	tagPrefix = strings.TrimSpace(tagPrefix)
	if tagPrefix == "" {
		tagPrefix = "local"
	}
	countryCode = strings.ToUpper(strings.TrimSpace(countryCode))
	if countryCode == "" {
		return tagPrefix
	}
	prefix := tagPrefix + "-" + countryDisplayName(countryCode)
	for next := 1; ; next++ {
		name := fmt.Sprintf("%s%d", prefix, next)
		if used == nil {
			return name
		}
		if _, exists := used[name]; !exists {
			used[name] = struct{}{}
			return name
		}
	}
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
	if len(content) > 16384 {
		content = content[:16384]
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
