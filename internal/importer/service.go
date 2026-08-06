package importer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"easy_proxies/internal/config"
	"easy_proxies/internal/subfetch"
)

var ErrNoRefreshSources = errors.New("no refreshable import sources")

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

type NodePortResolver interface {
	ResolveNodePorts(ctx context.Context, nodeURIs []string) (map[string]uint16, error)
}

type NodeSnapshotRestorer interface {
	RestoreConfigNodes(ctx context.Context, nodes []config.NodeConfig) error
}

type Service struct {
	store      *Store
	tester     *NodeTester
	nodeMgr    NodeManager
	httpClient *http.Client

	importCancelsMu   sync.Mutex
	importCancels     map[string]context.CancelFunc
	testJobsMu        sync.RWMutex
	testJobs          map[string]*TestJob
	testCancelsMu     sync.Mutex
	testCancels       map[string]context.CancelFunc
	refreshJobsMu     sync.RWMutex
	refreshJobs       map[string]*SourceRefreshJob
	refreshCancelsMu  sync.Mutex
	refreshCancels    map[string]context.CancelFunc
	jobEventsMu       sync.Mutex
	jobEventSubs      map[uint64]chan JobEvent
	jobEventNext      uint64
	jobEventsClosed   bool
	jobEventCoalesced atomic.Uint64
	lifecycleMu       sync.Mutex
	jobsWG            sync.WaitGroup
	closing           bool

	refreshConcurrency     int
	refreshRetryDelay      time.Duration
	refreshSourceTimeout   time.Duration
	refreshProxyTimeout    time.Duration
	refreshProxyCandidates int
	refreshJobMaxWait      time.Duration
	applyMu                sync.Mutex
}

type Option func(*Service)

func (s *Service) ProbeSchedulerStats() ProbeSchedulerStats {
	if s == nil || s.tester == nil {
		return ProbeSchedulerStats{}
	}
	return s.tester.ProbeSchedulerStats()
}

const (
	defaultRefreshConcurrency     = 3
	defaultRefreshRetryDelay      = time.Second
	defaultRefreshSourceTimeout   = 60 * time.Second
	defaultRefreshProxyTimeout    = 8 * time.Second
	defaultRefreshProxyCandidates = 5
	refreshJobPollInterval        = 500 * time.Millisecond
	refreshJobMaxWait             = 2 * time.Hour
	poolFailureDemoteThreshold    = 3
)

func NewService(store *Store, tester *NodeTester, nodeMgr NodeManager, opts ...Option) *Service {
	cleanupRefreshStageNodes(store)
	_, _ = store.RecoverStaleTestingNodes()
	s := &Service{
		store:   store,
		tester:  tester,
		nodeMgr: nodeMgr,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		importCancels:          make(map[string]context.CancelFunc),
		testJobs:               make(map[string]*TestJob),
		testCancels:            make(map[string]context.CancelFunc),
		refreshJobs:            make(map[string]*SourceRefreshJob),
		refreshCancels:         make(map[string]context.CancelFunc),
		jobEventSubs:           make(map[uint64]chan JobEvent),
		refreshConcurrency:     defaultRefreshConcurrency,
		refreshRetryDelay:      defaultRefreshRetryDelay,
		refreshSourceTimeout:   defaultRefreshSourceTimeout,
		refreshProxyTimeout:    defaultRefreshProxyTimeout,
		refreshProxyCandidates: defaultRefreshProxyCandidates,
		refreshJobMaxWait:      refreshJobMaxWait,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) launchBackground(register func(context.CancelFunc), run func(context.Context)) bool {
	ctx, cancel := context.WithCancel(context.Background())
	s.lifecycleMu.Lock()
	if s.closing {
		s.lifecycleMu.Unlock()
		cancel()
		return false
	}
	register(cancel)
	s.jobsWG.Add(1)
	s.lifecycleMu.Unlock()
	go func() {
		defer s.jobsWG.Done()
		run(ctx)
	}()
	return true
}

func (s *Service) Close(ctx context.Context) error {
	s.lifecycleMu.Lock()
	if !s.closing {
		s.closing = true
		s.importCancelsMu.Lock()
		for _, cancel := range s.importCancels {
			cancel()
		}
		s.importCancelsMu.Unlock()
		s.testCancelsMu.Lock()
		for _, cancel := range s.testCancels {
			cancel()
		}
		s.testCancelsMu.Unlock()
		s.refreshCancelsMu.Lock()
		for _, cancel := range s.refreshCancels {
			cancel()
		}
		s.refreshCancelsMu.Unlock()
	}
	s.lifecycleMu.Unlock()
	s.closeJobEvents()
	done := make(chan struct{})
	go func() {
		s.jobsWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) HasActiveJobs() bool {
	s.importCancelsMu.Lock()
	imports := len(s.importCancels)
	s.importCancelsMu.Unlock()
	s.testCancelsMu.Lock()
	tests := len(s.testCancels)
	s.testCancelsMu.Unlock()
	s.refreshJobsMu.RLock()
	refreshing := false
	for _, job := range s.refreshJobs {
		if job != nil && job.Status == "running" {
			refreshing = true
			break
		}
	}
	s.refreshJobsMu.RUnlock()
	return imports > 0 || tests > 0 || refreshing
}

func WithHTTPClient(c *http.Client) Option {
	return func(s *Service) {
		if c != nil {
			s.httpClient = c
		}
	}
}

type sourceRefreshTarget struct {
	Key          string
	TagPrefix    string
	URLs         []string
	LocalNodeIDs []string
	LocalFormats []string
}

type sourceRefreshSnapshot struct {
	store       StoreSnapshot
	configNodes []config.NodeConfig
	hasConfig   bool
}

func (s *Service) StartRefreshSources(key string) (string, error) {
	if jobID := s.activeRefreshJobID(); jobID != "" {
		return jobID, nil
	}
	targets, err := s.sourceRefreshTargets(key)
	if err != nil {
		return "", err
	}
	snapshot, err := s.captureSourceRefreshSnapshot()
	if err != nil {
		return "", fmt.Errorf("创建刷新回滚点: %w", err)
	}
	jobID := randomHex(12)
	job := &SourceRefreshJob{
		ID:               jobID,
		Status:           SourceRefreshJobRunning,
		StartedAt:        time.Now(),
		UpdatedAt:        time.Now(),
		InitialPoolCount: len(s.store.ListPoolNodes()),
		Groups:           make([]SourceRefreshGroup, 0, len(targets)),
	}
	for _, target := range targets {
		group := SourceRefreshGroup{
			Key:       target.Key,
			TagPrefix: target.TagPrefix,
			URLs:      make([]SourceRefreshURL, 0, len(target.URLs)+1),
		}
		for _, rawURL := range target.URLs {
			group.URLs = append(group.URLs, SourceRefreshURL{
				URL:       rawURL,
				Kind:      "url",
				Label:     "订阅链接",
				Status:    "waiting",
				UpdatedAt: time.Now(),
			})
		}
		if len(target.LocalNodeIDs) > 0 {
			group.URLs = append(group.URLs, SourceRefreshURL{
				Kind:      "content",
				Label:     localRefreshLabel(target.LocalFormats),
				Status:    "waiting",
				Nodes:     len(target.LocalNodeIDs),
				Total:     len(target.LocalNodeIDs),
				UpdatedAt: time.Now(),
			})
		}
		job.Groups = append(job.Groups, group)
	}
	s.recalculateRefreshJob(job)
	s.refreshJobsMu.Lock()
	for id, existing := range s.refreshJobs {
		if existing.Status == SourceRefreshJobRunning {
			s.refreshJobsMu.Unlock()
			return id, nil
		}
	}
	s.refreshJobs[jobID] = job
	for id, existing := range s.refreshJobs {
		if existing.Status != SourceRefreshJobRunning && time.Since(existing.UpdatedAt) > 10*time.Minute {
			delete(s.refreshJobs, id)
		}
	}
	s.refreshJobsMu.Unlock()
	copyJob := cloneRefreshJob(job)
	s.publishJobEvent(JobEvent{Kind: "refresh", ID: jobID, Refresh: &copyJob})
	started := s.launchBackground(func(cancel context.CancelFunc) {
		s.refreshCancelsMu.Lock()
		s.refreshCancels[jobID] = cancel
		s.refreshCancelsMu.Unlock()
	}, func(ctx context.Context) {
		s.runRefreshJob(ctx, jobID, targets, snapshot)
	})
	if !started {
		s.updateRefreshJob(jobID, func(active *SourceRefreshJob) {
			active.Status = SourceRefreshJobCanceled
			active.Phase = "canceled"
			active.Error = "服务正在关闭"
		})
		return "", fmt.Errorf("服务正在关闭")
	}
	return jobID, nil
}

func (s *Service) CancelRefreshJob(jobID string) (SourceRefreshJob, error) {
	job, ok := s.GetRefreshJob(jobID)
	if !ok {
		return SourceRefreshJob{}, fmt.Errorf("刷新任务不存在或已过期")
	}
	if job.Status != SourceRefreshJobRunning {
		return job, nil
	}
	s.refreshCancelsMu.Lock()
	cancel := s.refreshCancels[jobID]
	s.refreshCancelsMu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.updateRefreshJob(jobID, func(active *SourceRefreshJob) {
		active.Phase = "canceling"
		active.Error = "正在取消重新检测"
	})
	updated, _ := s.GetRefreshJob(jobID)
	return updated, nil
}

func (s *Service) unregisterRefreshCancel(jobID string) {
	s.refreshCancelsMu.Lock()
	delete(s.refreshCancels, jobID)
	s.refreshCancelsMu.Unlock()
}

func (s *Service) activeRefreshJobID() string {
	s.refreshJobsMu.RLock()
	defer s.refreshJobsMu.RUnlock()
	for id, job := range s.refreshJobs {
		if job.Status == SourceRefreshJobRunning {
			return id
		}
	}
	return ""
}

func (s *Service) GetRefreshJob(jobID string) (SourceRefreshJob, bool) {
	s.refreshJobsMu.RLock()
	defer s.refreshJobsMu.RUnlock()
	job, ok := s.refreshJobs[jobID]
	if !ok {
		return SourceRefreshJob{}, false
	}
	return cloneRefreshJob(job), true
}

func (s *Service) sourceRefreshTargets(key string) ([]sourceRefreshTarget, error) {
	key = strings.TrimSpace(key)
	nodes := s.store.ListNodes()
	selectedTags := make(map[string]struct{})
	if key != "" {
		for _, node := range nodes {
			for _, ref := range nodeSourceRefs(node) {
				view := nodeWithSource(node, ref)
				tagPrefix := strings.TrimSpace(ref.TagPrefix)
				if tagPrefix != "" && importSourceKey(view) == key {
					selectedTags[tagPrefix] = struct{}{}
				}
			}
		}
		if tagPrefix, ok := strings.CutPrefix(key, "tag:"); ok && strings.TrimSpace(tagPrefix) != "" {
			selectedTags[strings.TrimSpace(tagPrefix)] = struct{}{}
		}
	}

	targetByTag := make(map[string]*sourceRefreshTarget)
	urlSeen := make(map[string]map[string]struct{})
	formatSeen := make(map[string]map[string]struct{})
	for _, node := range nodes {
		for _, ref := range nodeSourceRefs(node) {
			tagPrefix := strings.TrimSpace(ref.TagPrefix)
			if tagPrefix == "" {
				continue
			}
			if key != "" {
				if _, ok := selectedTags[tagPrefix]; !ok {
					continue
				}
			}
			target := targetByTag[tagPrefix]
			if target == nil {
				target = &sourceRefreshTarget{Key: "tag:" + tagPrefix, TagPrefix: tagPrefix}
				targetByTag[tagPrefix] = target
				urlSeen[tagPrefix] = make(map[string]struct{})
				formatSeen[tagPrefix] = make(map[string]struct{})
			}
			source := strings.TrimSpace(ref.Source)
			if isURLSourceRef(ref) && source != "" {
				for _, rawURL := range splitSubscriptionURLs(source) {
					if _, exists := urlSeen[tagPrefix][rawURL]; exists {
						continue
					}
					urlSeen[tagPrefix][rawURL] = struct{}{}
					target.URLs = append(target.URLs, rawURL)
				}
				continue
			}
			if !stringSliceContains(target.LocalNodeIDs, node.ID) {
				target.LocalNodeIDs = append(target.LocalNodeIDs, node.ID)
			}
			format := strings.TrimSpace(ref.Format)
			if format != "" {
				if _, exists := formatSeen[tagPrefix][format]; !exists {
					formatSeen[tagPrefix][format] = struct{}{}
					target.LocalFormats = append(target.LocalFormats, format)
				}
			}
		}
	}

	targets := make([]sourceRefreshTarget, 0, len(targetByTag))
	for _, target := range targetByTag {
		sort.Strings(target.URLs)
		sort.Strings(target.LocalNodeIDs)
		sort.Strings(target.LocalFormats)
		targets = append(targets, *target)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].TagPrefix < targets[j].TagPrefix })
	if len(targets) == 0 {
		if key != "" {
			return nil, fmt.Errorf("%w: 未找到可重新检测的导入来源", ErrNoRefreshSources)
		}
		return nil, fmt.Errorf("%w: 没有带 Tag 的导入来源", ErrNoRefreshSources)
	}
	return targets, nil
}

func localRefreshLabel(formats []string) string {
	if len(formats) != 1 {
		return "本地内容"
	}
	switch formats[0] {
	case "base64":
		return "Base64"
	case "clash_yaml":
		return "Clash YAML"
	case "uri_list":
		return "URI 列表"
	default:
		return "本地内容"
	}
}

type sourceRefreshWork struct {
	groupIdx int
	rowIdx   int
	target   sourceRefreshTarget
	rawURL   string
}

func (s *Service) runRefreshJob(ctx context.Context, jobID string, targets []sourceRefreshTarget, snapshot sourceRefreshSnapshot) {
	defer s.unregisterRefreshCancel(jobID)
	defer cleanupRefreshStageNodes(s.store)
	defer func() {
		if recovered := recover(); recovered != nil {
			restoreErr := s.restoreSourceRefreshSnapshot(snapshot)
			s.updateRefreshJob(jobID, func(job *SourceRefreshJob) {
				job.Status = SourceRefreshJobFailed
				job.Phase = "failed"
				job.Error = fmt.Sprintf("重新检测异常: %v", recovered)
				if restoreErr != nil {
					job.Error += "; 恢复节点池失败: " + restoreErr.Error()
				}
			})
		}
	}()

	works := make([]sourceRefreshWork, 0)
	for groupIdx, target := range targets {
		for rowIdx, rawURL := range target.URLs {
			works = append(works, sourceRefreshWork{groupIdx: groupIdx, rowIdx: rowIdx, target: target, rawURL: rawURL})
		}
		if len(target.LocalNodeIDs) > 0 {
			works = append(works, sourceRefreshWork{groupIdx: groupIdx, rowIdx: len(target.URLs), target: target})
		}
	}

	s.runRefreshWorkQueue(ctx, jobID, works)
	job, _ := s.GetRefreshJob(jobID)
	if ctx.Err() != nil || job.Protected {
		if err := s.restoreSourceRefreshSnapshot(snapshot); err != nil {
			s.updateRefreshJob(jobID, func(active *SourceRefreshJob) {
				active.Status = SourceRefreshJobFailed
				active.Phase = "failed"
				active.Error = "恢复检测前节点池失败: " + err.Error()
			})
			return
		}
	}
	s.updateRefreshJob(jobID, func(active *SourceRefreshJob) {
		if ctx.Err() != nil {
			cancelRefreshJob(active)
			active.Applied = false
			return
		}
		if active.Protected {
			active.Status = SourceRefreshJobFinished
			active.Phase = "protected"
			active.Protected = true
			active.Applied = false
			active.Error = active.ProtectionReason
			return
		}
		s.finalizeRefreshJob(active)
		active.Applied = true
	})
}

func (s *Service) captureSourceRefreshSnapshot() (sourceRefreshSnapshot, error) {
	snapshot := sourceRefreshSnapshot{store: s.store.BackupSnapshot()}
	if s.nodeMgr == nil {
		return sourceRefreshSnapshot{}, fmt.Errorf("节点管理器未初始化")
	}
	lister, ok := s.nodeMgr.(NodeLister)
	if !ok {
		return sourceRefreshSnapshot{}, fmt.Errorf("节点管理器不支持配置快照")
	}
	if _, ok := s.nodeMgr.(NodeSnapshotRestorer); !ok {
		return sourceRefreshSnapshot{}, fmt.Errorf("节点管理器不支持配置恢复")
	}
	nodes, err := lister.ListConfigNodes(context.Background())
	if err != nil {
		return sourceRefreshSnapshot{}, fmt.Errorf("读取运行配置: %w", err)
	}
	snapshot.configNodes = nodes
	snapshot.hasConfig = true
	return snapshot, nil
}

func (s *Service) restoreSourceRefreshSnapshot(snapshot sourceRefreshSnapshot) error {
	if err := s.store.RestoreNodesSnapshot(snapshot.store); err != nil {
		return err
	}
	if !snapshot.hasConfig {
		return nil
	}
	restorer, ok := s.nodeMgr.(NodeSnapshotRestorer)
	if !ok {
		return fmt.Errorf("节点管理器不支持配置快照恢复")
	}
	return restorer.RestoreConfigNodes(context.Background(), snapshot.configNodes)
}

type sourceRefreshAttempt struct {
	work         sourceRefreshWork
	finalAttempt bool
	readyAt      time.Time
}

type sourceRefreshResult struct {
	attempt sourceRefreshAttempt
	err     error
}

func (s *Service) runRefreshWorkQueue(ctx context.Context, jobID string, works []sourceRefreshWork) {
	limit := s.refreshConcurrency
	if limit <= 0 {
		limit = defaultRefreshConcurrency
	}
	retryDelay := s.refreshRetryDelay
	if retryDelay <= 0 {
		retryDelay = defaultRefreshRetryDelay
	}
	runSourceRefreshQueue(ctx, works, limit, retryDelay, func(ctx context.Context, work sourceRefreshWork, finalAttempt bool) error {
		return s.runRefreshWork(ctx, jobID, work, finalAttempt)
	})
}

func runSourceRefreshQueue(ctx context.Context, works []sourceRefreshWork, limit int, retryDelay time.Duration, run func(context.Context, sourceRefreshWork, bool) error) {
	if len(works) == 0 || run == nil {
		return
	}
	if limit <= 0 {
		limit = defaultRefreshConcurrency
	}
	if limit > len(works) {
		limit = len(works)
	}
	if retryDelay <= 0 {
		retryDelay = defaultRefreshRetryDelay
	}
	results := make(chan sourceRefreshResult, limit)
	var workers sync.WaitGroup
	active := 0
	nextFirst := 0
	retries := make([]sourceRefreshAttempt, 0)
	launch := func(attempt sourceRefreshAttempt) {
		active++
		workers.Add(1)
		go func() {
			defer workers.Done()
			result := sourceRefreshResult{
				attempt: attempt,
				err:     run(ctx, attempt.work, attempt.finalAttempt),
			}
			results <- result
		}()
	}

	for nextFirst < len(works) || len(retries) > 0 || active > 0 {
		if ctx.Err() != nil {
			workers.Wait()
			return
		}
		for active < limit && nextFirst < len(works) {
			launch(sourceRefreshAttempt{work: works[nextFirst]})
			nextFirst++
		}
		if nextFirst == len(works) {
			for active < limit {
				now := time.Now()
				ready := -1
				for i := range retries {
					if !retries[i].readyAt.After(now) {
						ready = i
						break
					}
				}
				if ready < 0 {
					break
				}
				attempt := retries[ready]
				retries = append(retries[:ready], retries[ready+1:]...)
				launch(attempt)
			}
		}

		if active == 0 && nextFirst == len(works) && len(retries) == 0 {
			break
		}
		var timer *time.Timer
		var timerC <-chan time.Time
		if active < limit && nextFirst == len(works) && len(retries) > 0 {
			nextReady := retries[0].readyAt
			for _, retry := range retries[1:] {
				if retry.readyAt.Before(nextReady) {
					nextReady = retry.readyAt
				}
			}
			wait := time.Until(nextReady)
			if wait <= 0 {
				continue
			}
			timer = time.NewTimer(wait)
			timerC = timer.C
		}

		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			workers.Wait()
			return
		case result := <-results:
			if timer != nil {
				timer.Stop()
			}
			active--
			if result.err != nil && result.attempt.work.rawURL != "" && !result.attempt.finalAttempt && ctx.Err() == nil {
				retries = append(retries, sourceRefreshAttempt{
					work:         result.attempt.work,
					finalAttempt: true,
					readyAt:      time.Now().Add(retryDelay),
				})
			}
		case <-timerC:
		}
	}
	workers.Wait()
}

func (s *Service) runRefreshWork(ctx context.Context, jobID string, work sourceRefreshWork, finalAttempt bool) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("刷新来源异常: %v", recovered)
			s.protectRefreshRow(jobID, work, 0, err.Error())
		}
	}()
	if work.rawURL == "" {
		return s.refreshExistingNodes(ctx, jobID, work, s.existingLocalRefreshNodeIDs(work.target), false, "")
	}
	return s.refreshURLSource(ctx, jobID, work, finalAttempt)
}

func (s *Service) refreshURLSource(ctx context.Context, jobID string, work sourceRefreshWork, finalAttempt bool) error {
	attempt := 1
	proxyStart := 0
	status := "pulling"
	if finalAttempt {
		attempt = 2
		proxyStart = s.refreshProxyCandidates
		status = "retrying"
	}
	s.updateRefreshRow(jobID, work, func(row *SourceRefreshURL) {
		row.Status = status
		row.Stage = "direct"
		row.Detail = map[bool]string{true: "正在进行最终重试", false: "正在直连并准备网络兜底"}[finalAttempt]
		row.Error = ""
		row.Warning = ""
		row.Attempt = attempt
		row.Attempts = 2
		row.Cached = false
		row.Nodes = 0
		row.Done = 0
		row.Total = 0
		row.Passed = 0
		row.Failed = 0
		row.Promoted = 0
	})

	timeout := s.refreshSourceTimeout
	if timeout <= 0 {
		timeout = defaultRefreshSourceTimeout
	}
	fetchCtx, cancel := context.WithTimeout(ctx, timeout)
	parsed, err := s.parseRefreshSubscriptionURLContext(fetchCtx, work.target.TagPrefix, work.rawURL, proxyStart, func(current, total int) {
		s.updateRefreshRow(jobID, work, func(row *SourceRefreshURL) {
			row.Stage = "proxy"
			row.Detail = fmt.Sprintf("正在尝试节点池代理 %d/%d", current, total)
		})
	})
	cancel()
	if err != nil {
		msg := compactRefreshSourceError(err, work.rawURL)
		if ctx.Err() != nil {
			s.updateRefreshRow(jobID, work, func(row *SourceRefreshURL) {
				row.Status = "canceled"
				row.Error = "任务已取消"
			})
			return ctx.Err()
		}
		if !finalAttempt {
			s.updateRefreshRow(jobID, work, func(row *SourceRefreshURL) {
				row.Status = "retry_waiting"
				row.Stage = "waiting"
				row.Detail = "已进入重试队列，将在空闲并发槽位中自动重试"
				row.Warning = msg
			})
			return err
		}
		ids := s.existingURLRefreshNodeIDs(work.target.TagPrefix, work.rawURL)
		warning := "订阅拉取失败，已改为检测保存节点：" + msg
		if len(ids) == 0 {
			s.updateRefreshRow(jobID, work, func(row *SourceRefreshURL) {
				row.Status = "failed"
				row.Stage = "failed"
				row.Detail = "订阅拉取失败且没有保存节点"
				row.Error = msg
			})
			return err
		}
		return s.refreshExistingNodes(ctx, jobID, work, ids, true, warning)
	}
	return s.refreshParsedNodes(ctx, jobID, work, parsed)
}

func (s *Service) refreshParsedNodes(ctx context.Context, jobID string, work sourceRefreshWork, parsed ParseResponse) error {
	nodeIDs := make([]string, 0, len(parsed.Nodes))
	for _, node := range parsed.Nodes {
		nodeIDs = append(nodeIDs, node.ID)
	}
	s.updateRefreshRow(jobID, work, func(row *SourceRefreshURL) {
		row.Status = "testing"
		row.Stage = "testing"
		row.Detail = "订阅已更新，正在测试节点"
		row.Nodes = len(parsed.Nodes)
		row.Total = len(parsed.Nodes)
	})
	commit, err := s.Commit(parsed.ImportID, CommitRequest{NodeIDs: nodeIDs, AutoReload: false, PromotePassed: false})
	if err != nil {
		s.protectRefreshRow(jobID, work, len(parsed.Nodes), "启动节点测试失败: "+compactRefreshError(err))
		return err
	}
	importJob, waitErr := s.waitImportJob(ctx, commit.JobID, func(importJob ImportJob) {
		s.updateRefreshRow(jobID, work, func(row *SourceRefreshURL) { applyImportJobProgress(row, importJob) })
	})
	allNodesFailed := importJob.Total > 0 && importJob.Failed == importJob.Total && importJob.Passed == 0 && strings.TrimSpace(importJob.Error) == ""
	if waitErr != nil || importJob.Status == ImportStatusCanceled || importJob.Status == ImportStatusFailed && !allNodesFailed {
		_ = s.cleanupStagedNodes(nodeIDs)
		msg := compactRefreshError(waitErr)
		if msg == "" {
			msg = strings.TrimSpace(importJob.Error)
		}
		if msg == "" {
			msg = "节点测试任务失败"
		}
		s.protectRefreshRow(jobID, work, len(parsed.Nodes), msg)
		return fmt.Errorf("%s", msg)
	}
	if importJob.Passed == 0 {
		if err := s.cleanupStagedNodes(nodeIDs); err != nil {
			s.protectRefreshRow(jobID, work, len(parsed.Nodes), "清理无效订阅节点失败: "+compactRefreshError(err))
			return err
		}
		existingIDs := s.existingURLRefreshNodeIDs(work.target.TagPrefix, work.rawURL)
		warning := fmt.Sprintf("订阅新节点 %d 个三轮检测全部失败", len(parsed.Nodes))
		if len(existingIDs) > 0 {
			return s.refreshExistingNodes(ctx, jobID, work, existingIDs, true, warning+"，已改为检测保存节点")
		}
		s.updateRefreshRow(jobID, work, func(row *SourceRefreshURL) {
			applyImportJobProgress(row, importJob)
			row.Status = "completed"
			row.Stage = "completed"
			row.Detail = "订阅节点检测完成，未发现可用节点"
			row.Warning = warning
			row.Error = ""
		})
		return nil
	}
	promoted, err := s.applyStagedRefreshNodes(work.rawURL, nodeIDs)
	if err != nil {
		_ = s.cleanupStagedNodes(nodeIDs)
		s.protectRefreshRow(jobID, work, len(parsed.Nodes), "应用订阅检测结果失败: "+compactRefreshError(err))
		return err
	}
	s.updateRefreshRow(jobID, work, func(row *SourceRefreshURL) {
		applyImportJobProgress(row, importJob)
		row.Promoted = promoted
		row.Status = "completed"
		row.Stage = "completed"
		row.Detail = "订阅更新和节点检测完成"
		row.Error = ""
	})
	return nil
}

func (s *Service) cleanupStagedNodes(nodeIDs []string) error {
	ids := make([]string, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		if node, ok := s.store.GetNode(id); ok && node.ImportMode == "refresh_stage" {
			ids = append(ids, id)
		}
	}
	if len(ids) > 0 {
		return s.store.DeleteNodes(ids)
	}
	return nil
}

func (s *Service) applyStagedRefreshNodes(sourceURL string, nodeIDs []string) (int, error) {
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	staged := make([]ManagedNode, 0, len(nodeIDs))
	passedIDs := make([]string, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		node, ok := s.store.GetNode(id)
		if !ok || node.ImportMode != "refresh_stage" || node.State != StatePassed && node.State != StateFailed {
			continue
		}
		node.ID = nodeID(node.URI)
		node.ImportMode = "url"
		node.InPool = false
		node.Port = 0
		staged = append(staged, node)
		if node.State == StatePassed {
			passedIDs = append(passedIDs, node.ID)
		}
	}
	if len(staged) != len(nodeIDs) {
		return 0, fmt.Errorf("临时节点状态不完整：期望 %d，实际 %d", len(nodeIDs), len(staged))
	}
	if _, err := s.deleteBySubscription(sourceURL, false); err != nil {
		return 0, err
	}
	promoted := 0
	if len(staged) > 0 {
		if err := s.store.UpsertNodes(staged); err != nil {
			return 0, err
		}
		created, err := s.PromoteMany(passedIDs, false)
		if err != nil {
			return 0, err
		}
		promoted = len(created)
	}
	if err := s.cleanupStagedNodes(nodeIDs); err != nil {
		return 0, fmt.Errorf("清理临时节点: %w", err)
	}
	return promoted, s.nodeMgr.TriggerReload(context.Background())
}

func (s *Service) refreshExistingNodes(ctx context.Context, jobID string, work sourceRefreshWork, nodeIDs []string, cached bool, warning string) error {
	s.updateRefreshRow(jobID, work, func(row *SourceRefreshURL) {
		row.Status = "testing"
		row.Stage = map[bool]string{true: "cache", false: "testing"}[cached]
		row.Detail = map[bool]string{true: "正在检测保存节点", false: "正在重新检测本地节点"}[cached]
		row.Warning = warning
		row.Cached = cached
		row.Error = ""
		row.Nodes = len(nodeIDs)
		row.Total = len(nodeIDs)
		row.Done = 0
		row.Passed = 0
		row.Failed = 0
		row.Promoted = 0
	})
	if len(nodeIDs) == 0 {
		s.failRefreshRow(jobID, work, 0, "该 Tag 下没有可重新检测的节点")
		return fmt.Errorf("该 Tag 下没有可重新检测的节点")
	}
	testJobID, err := s.StartBatchTest(BatchTestRequest{NodeIDs: nodeIDs, Retest: true, PromotePassed: true, AutoReload: true, ParentRefresh: true})
	if err != nil {
		s.failRefreshRow(jobID, work, len(nodeIDs), compactRefreshError(err))
		return err
	}
	testJob, waitErr := s.waitTestJob(ctx, testJobID, func(testJob TestJob) {
		s.updateRefreshRow(jobID, work, func(row *SourceRefreshURL) { applyTestJobProgress(row, testJob) })
	})
	failed := waitErr != nil || testJob.Status == TestJobCanceled || testJob.Status == TestJobFailed
	s.updateRefreshRow(jobID, work, func(row *SourceRefreshURL) {
		applyTestJobProgress(row, testJob)
		row.Done = row.Total
		row.Status = "completed"
		row.Stage = "completed"
		row.Detail = map[bool]string{true: "保存节点检测完成", false: "本地节点检测完成"}[cached]
		if failed {
			row.Status = "failed"
			row.Stage = "failed"
			row.Error = compactRefreshError(waitErr)
			if row.Error == "" {
				row.Error = strings.TrimSpace(testJob.Error)
			}
			if row.Error == "" {
				row.Error = "节点检测任务失败"
			}
			row.Warning = "节点检测子任务异常，父任务将恢复检测前节点池"
			row.Protected = true
		}
	})
	if failed {
		return fmt.Errorf("节点检测任务失败")
	}
	return nil
}

func (s *Service) updateRefreshRow(jobID string, work sourceRefreshWork, fn func(*SourceRefreshURL)) {
	s.updateRefreshJob(jobID, func(job *SourceRefreshJob) {
		if work.groupIdx >= len(job.Groups) || work.rowIdx >= len(job.Groups[work.groupIdx].URLs) {
			return
		}
		row := &job.Groups[work.groupIdx].URLs[work.rowIdx]
		fn(row)
		row.UpdatedAt = time.Now()
	})
}

func (s *Service) failRefreshRow(jobID string, work sourceRefreshWork, nodes int, message string) {
	s.updateRefreshRow(jobID, work, func(row *SourceRefreshURL) {
		row.Status = "failed"
		row.Stage = "failed"
		row.Detail = "重新检测失败"
		row.Error = message
		row.Nodes = nodes
		if row.Total == 0 {
			row.Total = nodes
		}
	})
}

func (s *Service) protectRefreshRow(jobID string, work sourceRefreshWork, nodes int, message string) {
	s.failRefreshRow(jobID, work, nodes, message)
	s.updateRefreshRow(jobID, work, func(row *SourceRefreshURL) {
		row.Warning = message
		row.Protected = true
	})
}

func cancelRefreshJob(job *SourceRefreshJob) {
	job.Status = SourceRefreshJobCanceled
	job.Phase = "canceled"
	job.Error = "重新检测已取消"
	for groupIdx := range job.Groups {
		for rowIdx := range job.Groups[groupIdx].URLs {
			row := &job.Groups[groupIdx].URLs[rowIdx]
			if row.Status == "completed" || row.Status == "failed" {
				continue
			}
			row.Status = "canceled"
			row.Stage = "canceled"
			row.Detail = "任务已取消"
			row.UpdatedAt = time.Now()
		}
	}
}

func compactRefreshError(err error) string {
	if err == nil {
		return ""
	}
	return compactRefreshMessage(err.Error())
}

func compactRefreshSourceError(err error, sourceURL string) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if sourceURL = strings.TrimSpace(sourceURL); sourceURL != "" {
		message = strings.ReplaceAll(message, sourceURL, "订阅地址")
	}
	return compactRefreshMessage(message)
}

func compactRefreshMessage(message string) string {
	runes := []rune(strings.TrimSpace(message))
	if len(runes) > 320 {
		return string(runes[:320]) + "..."
	}
	return string(runes)
}

func (s *Service) existingLocalRefreshNodeIDs(target sourceRefreshTarget) []string {
	ids := make([]string, 0, len(target.LocalNodeIDs))
	for _, id := range target.LocalNodeIDs {
		node, ok := s.store.GetNode(id)
		if !ok || !nodeHasSource(node, func(ref NodeSourceRef) bool {
			return strings.TrimSpace(ref.TagPrefix) == target.TagPrefix && !isURLSourceRef(ref)
		}) {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func (s *Service) existingURLRefreshNodeIDs(tagPrefix, subURL string) []string {
	ids := make([]string, 0)
	for _, node := range s.store.ListNodes() {
		if !nodeHasSource(node, func(ref NodeSourceRef) bool {
			return ref.TagPrefix == tagPrefix && isURLSourceRef(ref) && strings.TrimSpace(ref.Source) == subURL
		}) {
			continue
		}
		ids = append(ids, node.ID)
	}
	sort.Strings(ids)
	return ids
}

func (s *Service) parseRefreshSubscriptionURLContext(ctx context.Context, tagPrefix, subURL string, proxyStart int, onProxyAttempt func(int, int)) (ParseResponse, error) {
	tagPrefix = strings.TrimSpace(tagPrefix)
	if tagPrefix == "" {
		tagPrefix = s.tagPrefixForImportSource(subURL)
	}
	if tagPrefix == "" {
		tagPrefix = "local"
	}
	return s.parseRefreshSubscriptionURLWithContextMode(ctx, tagPrefix, subURL, proxyStart, onProxyAttempt, true)
}

func (s *Service) parseRefreshSubscriptionURLOnce(tagPrefix, subURL string, timeout time.Duration) (ParseResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.parseRefreshSubscriptionURLWithContextMode(ctx, tagPrefix, subURL, 0, nil, false)
}

func (s *Service) parseRefreshSubscriptionURLWithContext(ctx context.Context, tagPrefix, subURL string, proxyStart int, onProxyAttempt func(int, int)) (ParseResponse, error) {
	return s.parseRefreshSubscriptionURLWithContextMode(ctx, tagPrefix, subURL, proxyStart, onProxyAttempt, false)
}

func (s *Service) parseRefreshSubscriptionURLWithContextMode(ctx context.Context, tagPrefix, subURL string, proxyStart int, onProxyAttempt func(int, int), stage bool) (ParseResponse, error) {
	subURL = strings.TrimSpace(subURL)
	if subURL == "" {
		return ParseResponse{}, fmt.Errorf("订阅 URL 不能为空")
	}
	if !strings.HasPrefix(subURL, "http://") && !strings.HasPrefix(subURL, "https://") {
		return ParseResponse{}, fmt.Errorf("订阅 URL 必须以 http:// 或 https:// 开头")
	}
	timeout := s.refreshSourceTimeout
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	}
	if timeout <= 0 {
		return ParseResponse{}, context.DeadlineExceeded
	}
	body, err := subfetch.Fetch(ctx, subURL, subfetch.Options{
		Timeout: timeout,
		ProxyFallback: func(proxyCtx context.Context, rawURL string, headers http.Header) ([]byte, error) {
			return s.fetchSubscriptionViaPool(proxyCtx, rawURL, headers, tagPrefix, proxyStart, onProxyAttempt)
		},
	})
	if err != nil {
		return ParseResponse{}, fmt.Errorf("获取订阅失败: %w", err)
	}
	content := string(body)
	configNodes, err := config.ParseSubscriptionContent(content)
	if err != nil {
		return ParseResponse{}, fmt.Errorf("解析订阅失败: %w", err)
	}
	if len(configNodes) == 0 {
		return ParseResponse{}, fmt.Errorf("订阅未拉取到任何节点")
	}

	importID := randomHex(12)
	format := detectFormat(content)
	now := time.Now()
	seen := make(map[string]int, len(configNodes))
	nodes := make([]ManagedNode, 0, len(configNodes))
	nodeIDs := make([]string, 0, len(configNodes))
	for _, cn := range configNodes {
		canonicalID := nodeID(cn.URI)
		id := canonicalID
		importMode := "url"
		if stage {
			id = canonicalID + "-refresh-" + importID
			importMode = "refresh_stage"
		}
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
			ImportMode:   importMode,
			ImportSource: subURL,
			ImportFormat: format,
			State:        StateParsed,
			Enabled:      true,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if idx, ok := seen[canonicalID]; ok {
			nodes[idx] = mn
			continue
		}
		seen[canonicalID] = len(nodes)
		nodes = append(nodes, mn)
		nodeIDs = append(nodeIDs, mn.ID)
	}
	if !stage {
		if _, err := s.DeleteBySubscription(subURL); err != nil {
			return ParseResponse{}, fmt.Errorf("替换旧节点 %s: %w", subURL, err)
		}
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

func cleanupRefreshStageNodes(store *Store) error {
	ids := make([]string, 0)
	for _, node := range store.ListNodes() {
		if node.ImportMode == "refresh_stage" {
			ids = append(ids, node.ID)
		}
	}
	if len(ids) > 0 {
		return store.DeleteNodes(ids)
	}
	return nil
}

func (s *Service) waitImportJob(ctx context.Context, jobID string, onProgress func(ImportJob)) (ImportJob, error) {
	maxWait := s.refreshJobMaxWait
	if maxWait <= 0 {
		maxWait = refreshJobMaxWait
	}
	deadline := time.Now().Add(maxWait)
	var last ImportJob
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			_, _ = s.CancelImportJob(jobID)
			return s.waitImportJobStopped(jobID, last), ctx.Err()
		}
		job, ok := s.store.GetJob(jobID)
		if !ok {
			return ImportJob{}, fmt.Errorf("导入任务 %s 不存在", jobID)
		}
		last = job
		if onProgress != nil {
			onProgress(job)
		}
		switch job.Status {
		case ImportStatusCompleted, ImportStatusFailed, ImportStatusCanceled:
			return job, nil
		}
		select {
		case <-ctx.Done():
			_, _ = s.CancelImportJob(jobID)
			return s.waitImportJobStopped(jobID, last), ctx.Err()
		case <-time.After(refreshJobPollInterval):
		}
	}
	_, _ = s.CancelImportJob(jobID)
	return s.waitImportJobStopped(jobID, last), fmt.Errorf("等待导入任务 %s 超时", jobID)
}

func (s *Service) waitTestJob(ctx context.Context, jobID string, onProgress func(TestJob)) (TestJob, error) {
	maxWait := s.refreshJobMaxWait
	if maxWait <= 0 {
		maxWait = refreshJobMaxWait
	}
	deadline := time.Now().Add(maxWait)
	var last TestJob
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			_, _ = s.CancelTestJob(jobID)
			return s.waitTestJobStopped(jobID, last), ctx.Err()
		}
		job, ok := s.GetTestJob(jobID)
		if !ok {
			return TestJob{}, fmt.Errorf("测试任务 %s 不存在", jobID)
		}
		last = job
		if onProgress != nil {
			onProgress(job)
		}
		switch job.Status {
		case TestJobFinished, TestJobFailed, TestJobCanceled:
			return job, nil
		}
		select {
		case <-ctx.Done():
			_, _ = s.CancelTestJob(jobID)
			return s.waitTestJobStopped(jobID, last), ctx.Err()
		case <-time.After(refreshJobPollInterval):
		}
	}
	_, _ = s.CancelTestJob(jobID)
	return s.waitTestJobStopped(jobID, last), fmt.Errorf("等待测试任务 %s 超时", jobID)
}

func (s *Service) waitImportJobStopped(jobID string, last ImportJob) ImportJob {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := s.store.GetJob(jobID)
		if !ok {
			return last
		}
		last = job
		if job.Status != ImportStatusRunning {
			return job
		}
		time.Sleep(50 * time.Millisecond)
	}
	return last
}

func (s *Service) waitTestJobStopped(jobID string, last TestJob) TestJob {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := s.GetTestJob(jobID)
		if !ok {
			return last
		}
		last = job
		if job.Status != TestJobRunning {
			return job
		}
		time.Sleep(50 * time.Millisecond)
	}
	return last
}

func applyImportJobProgress(row *SourceRefreshURL, job ImportJob) {
	if row == nil {
		return
	}
	row.Total = job.Total
	row.Done = min(job.Total, job.Passed+job.Failed)
	row.Passed = job.Passed
	row.Failed = job.Failed
	row.Promoted = job.Promoted
	row.ProbeRound = job.ProbeRound
	row.ProbeRounds = job.ProbeRounds
	row.ProbeRoundDone = job.ProbeRoundDone
	row.ProbeRoundTotal = job.ProbeRoundTotal
	row.ProbePending = job.ProbePending
	row.ProbeTarget = job.ProbeTarget
	row.ProbeConcurrency = job.ProbeConcurrency
	if job.ProbeRound > 0 {
		row.Detail = fmt.Sprintf("第 %d/%d 轮，本轮 %d/%d，剩余 %d 个节点", job.ProbeRound, job.ProbeRounds, job.ProbeRoundDone, job.ProbeRoundTotal, job.ProbePending)
	}
	row.Status = "testing"
	if job.Status == ImportStatusRunning && job.Total > 0 && row.Done >= job.Total {
		row.Status = "promoting"
	}
	row.UpdatedAt = time.Now()
}

func applyTestJobProgress(row *SourceRefreshURL, job TestJob) {
	if row == nil {
		return
	}
	if row.Nodes == 0 {
		row.Nodes = job.Total
		row.Total = job.Total
	}
	row.Passed = job.Passed
	row.Failed = job.Failed
	row.Promoted = job.Promoted
	row.ProbeRound = job.ProbeRound
	row.ProbeRounds = job.ProbeRounds
	row.ProbeRoundDone = job.ProbeRoundDone
	row.ProbeRoundTotal = job.ProbeRoundTotal
	row.ProbePending = job.ProbePending
	row.ProbeTarget = job.ProbeTarget
	row.ProbeConcurrency = job.ProbeConcurrency
	row.Protected = job.Protected
	if job.Protected && job.ProtectionReason != "" {
		row.Warning = job.ProtectionReason
	}
	if job.ProbeRound > 0 {
		row.Detail = fmt.Sprintf("第 %d/%d 轮，本轮 %d/%d，剩余 %d 个节点", job.ProbeRound, job.ProbeRounds, job.ProbeRoundDone, job.ProbeRoundTotal, job.ProbePending)
	}
	row.Status = "testing"
	switch job.Phase {
	case "country", "promote", "done":
		row.Done = row.Total
	case "probe":
		row.Done = min(row.Total, job.Done)
	}
	if job.Phase == "promote" {
		row.Status = "promoting"
	}
	row.UpdatedAt = time.Now()
}

func (s *Service) recalculateRefreshJob(job *SourceRefreshJob) {
	if job == nil {
		return
	}
	protected := job.Protected
	protectionReason := job.ProtectionReason
	job.TotalURLs = 0
	job.DoneURLs = 0
	job.Successful = 0
	job.Failed = 0
	job.TotalNodes = 0
	job.DoneNodes = 0
	job.Passed = 0
	job.ProbePassed = 0
	job.FailedNodes = 0
	job.Promoted = 0
	job.Protected = protected
	job.ProtectionReason = protectionReason
	canceling := job.Phase == "canceling"
	switch job.Status {
	case SourceRefreshJobFinished:
		if job.Protected {
			job.Phase = "protected"
		} else if job.Phase != "partial" {
			job.Phase = "finished"
		}
	case SourceRefreshJobFailed:
		job.Phase = "failed"
	case SourceRefreshJobCanceled:
		job.Phase = "canceled"
	default:
		if canceling {
			job.Phase = "canceling"
		} else {
			job.Phase = "waiting"
		}
	}
	for gi := range job.Groups {
		group := &job.Groups[gi]
		group.Total = len(group.URLs)
		group.Done = 0
		group.Successful = 0
		group.Failed = 0
		for _, row := range group.URLs {
			job.TotalURLs++
			job.TotalNodes += row.Total
			job.DoneNodes += min(row.Total, row.Done)
			job.ProbePassed += row.Passed
			job.FailedNodes += row.Failed
			job.Promoted += row.Promoted
			if row.Protected {
				job.Protected = true
				if job.ProtectionReason == "" {
					job.ProtectionReason = row.Warning
				}
			}
			if job.Status == SourceRefreshJobRunning && job.Phase != "canceling" {
				switch row.Status {
				case "promoting":
					job.Phase = "promoting"
				case "testing":
					if job.Phase != "promoting" {
						job.Phase = "testing"
					}
				case "retrying", "retry_waiting":
					if job.Phase != "promoting" && job.Phase != "testing" {
						job.Phase = "retrying"
					}
				case "pulling":
					if job.Phase == "waiting" || job.Phase == "pulling" {
						job.Phase = "pulling"
					}
				}
			}
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
	if job.Status == SourceRefreshJobFinished {
		job.Passed = job.PoolCount
	} else {
		job.Passed = job.ProbePassed
	}
	job.UpdatedAt = time.Now()
}

func (s *Service) updateRefreshJob(jobID string, fn func(*SourceRefreshJob)) {
	s.refreshJobsMu.Lock()
	job, ok := s.refreshJobs[jobID]
	if !ok {
		s.refreshJobsMu.Unlock()
		return
	}
	fn(job)
	s.recalculateRefreshJob(job)
	copyJob := cloneRefreshJob(job)
	s.refreshJobsMu.Unlock()
	s.publishJobEvent(JobEvent{Kind: "refresh", ID: jobID, Refresh: &copyJob})
}

func (s *Service) finalizeRefreshJob(job *SourceRefreshJob) {
	if job == nil {
		return
	}
	job.Status = SourceRefreshJobFinished
	job.Phase = "finished"
	if job.TotalURLs > 0 && job.Successful == 0 {
		job.Status = SourceRefreshJobFailed
		job.Phase = "failed"
		job.Error = "全部导入来源重新检测失败"
		return
	}
	if job.Failed > 0 {
		job.Phase = "partial"
		job.Error = fmt.Sprintf("%d 个导入来源重新检测失败，其余结果已应用", job.Failed)
		return
	}
	job.Error = ""
}

func (s *Service) Parse(req ParseRequest) (ParseResponse, error) {
	req.TagPrefix = strings.TrimSpace(req.TagPrefix)
	req.URL = strings.TrimSpace(req.URL)
	if req.Mode != "url" && req.Mode != "content" {
		return ParseResponse{}, fmt.Errorf("mode 必须为 url 或 content")
	}
	if req.TagPrefix == "" {
		return ParseResponse{}, fmt.Errorf("Tag 前缀不能为空")
	}

	type parsedNode struct {
		node   config.NodeConfig
		source string
		format string
	}
	var parsedNodes []parsedNode
	replaceTag := req.Mode == "content"
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
			fetchCtx, cancel := context.WithTimeout(context.Background(), timeout)
			body, err := subfetch.Fetch(fetchCtx, subURL, subfetch.Options{
				Timeout: timeout,
				ProxyFallback: func(ctx context.Context, rawURL string, headers http.Header) ([]byte, error) {
					return s.fetchSubscriptionViaPool(ctx, rawURL, headers, req.TagPrefix, 0, nil)
				},
			})
			cancel()
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
		Mode:      req.Mode,
		Format:    format,
		TagPrefix: req.TagPrefix,
		Source:    importSourceForParse(req),
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

func importSourceForParse(req ParseRequest) string {
	if req.Mode == "url" {
		return req.URL
	}
	return req.Mode
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

func (s *Service) fetchSubscriptionViaPool(ctx context.Context, rawURL string, headers http.Header, sourceTag string, start int, onAttempt func(int, int)) ([]byte, error) {
	nodes := s.refreshProxyNodes(sourceTag)
	if len(nodes) == 0 {
		return nil, fmt.Errorf("没有可用的节点池节点可用于拉取订阅")
	}
	limit := s.refreshProxyCandidates
	if limit <= 0 {
		limit = defaultRefreshProxyCandidates
	}
	if start < 0 || start >= len(nodes) {
		start = 0
	}
	end := min(len(nodes), start+limit)
	nodes = nodes[start:end]
	timeout := s.refreshProxyTimeout
	if timeout <= 0 {
		timeout = defaultRefreshProxyTimeout
	}
	attempts := 0
	for idx, node := range nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if onAttempt != nil {
			onAttempt(idx+1, len(nodes))
		}
		attempts++
		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		client, closeClient, err := NewHTTPClientForURI(attemptCtx, s.tester.buildOutbound, node.ID, node.URI, timeout, s.tester.skipCertVerify)
		if err != nil {
			cancel()
			continue
		}
		req, reqErr := http.NewRequestWithContext(attemptCtx, http.MethodGet, rawURL, nil)
		if reqErr != nil {
			closeClient()
			cancel()
			return nil, reqErr
		}
		req.Header = headers.Clone()
		resp, doErr := client.Do(req)
		if doErr != nil {
			closeClient()
			cancel()
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			closeClient()
			cancel()
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()
		closeClient()
		cancel()
		if readErr != nil {
			continue
		}
		return body, nil
	}
	if attempts == 0 {
		return nil, fmt.Errorf("节点池中没有可用于拉取订阅的节点")
	}
	return nil, fmt.Errorf("尝试 %d 个节点池代理均失败", attempts)
}

func (s *Service) refreshProxyNodes(sourceTag string) []ManagedNode {
	nodes := s.store.ListPoolNodes()
	filtered := nodes[:0]
	for _, node := range nodes {
		if strings.TrimSpace(node.URI) == "" {
			continue
		}
		filtered = append(filtered, node)
	}
	nodes = filtered
	sort.SliceStable(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		aSame, bSame := a.TagPrefix == sourceTag, b.TagPrefix == sourceTag
		if aSame != bSame {
			return !aSame
		}
		if a.ConsecutiveFailures != b.ConsecutiveFailures {
			return a.ConsecutiveFailures < b.ConsecutiveFailures
		}
		if a.LastTestAt.IsZero() != b.LastTestAt.IsZero() {
			return !a.LastTestAt.IsZero()
		}
		if !a.LastTestAt.Equal(b.LastTestAt) {
			return a.LastTestAt.After(b.LastTestAt)
		}
		if a.LatencyMs > 0 && b.LatencyMs > 0 && a.LatencyMs != b.LatencyMs {
			return a.LatencyMs < b.LatencyMs
		}
		return a.ID < b.ID
	})
	return nodes
}

func (s *Service) tagPrefixForImportSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return ""
	}
	for _, node := range s.store.ListNodes() {
		for _, ref := range nodeSourceRefs(node) {
			if strings.TrimSpace(ref.Source) == source && strings.TrimSpace(ref.TagPrefix) != "" {
				return strings.TrimSpace(ref.TagPrefix)
			}
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
	originalStates := make(map[string]ManagedNodeState, len(selectedIDs))
	for _, id := range selectedIDs {
		n, ok := s.store.GetNode(id)
		if !ok {
			continue
		}
		if n.State != StateParsed {
			continue
		}
		originalStates[n.ID] = n.State
		n.State = StateTesting
		nodes = append(nodes, n)
	}
	if len(nodes) == 0 {
		return CommitResponse{}, fmt.Errorf("没有可测试的导入节点")
	}

	if err := s.store.UpsertNodes(nodes); err != nil {
		return CommitResponse{}, err
	}

	jobID := randomHex(12)
	job = ImportJob{
		ID:        jobID,
		Status:    ImportStatusRunning,
		Mode:      job.Mode,
		Format:    job.Format,
		TagPrefix: job.TagPrefix,
		Source:    job.Source,
		Total:     len(nodes),
		NodeIDs:   selectedIDs,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.store.UpsertJob(job); err != nil {
		if restoreErr := s.store.RestoreNodeStatesIfCurrent(originalStates, StateTesting); restoreErr != nil {
			return CommitResponse{}, fmt.Errorf("保存导入任务: %w; 恢复节点状态: %v", err, restoreErr)
		}
		return CommitResponse{}, err
	}

	started := s.launchBackground(func(cancel context.CancelFunc) {
		s.registerImportCancel(jobID, cancel)
	}, func(ctx context.Context) {
		s.runPipeline(ctx, jobID, nodes, originalStates, req.PromotePassed)
	})
	if !started {
		_ = s.store.RestoreNodeStatesIfCurrent(originalStates, StateTesting)
		_ = s.store.UpdateJob(jobID, func(job *ImportJob) {
			job.Status = ImportStatusCanceled
			job.Error = "服务正在关闭"
			job.UpdatedAt = time.Now()
		})
		return CommitResponse{}, fmt.Errorf("服务正在关闭")
	}

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
		_ = s.store.UpdateJobProgress(jobID, func(j *ImportJob) {
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

func (s *Service) runPipeline(ctx context.Context, jobID string, nodes []ManagedNode, originalStates map[string]ManagedNodeState, promotePassed bool) {
	defer s.unregisterImportCancel(jobID)

	total := len(nodes)
	passed := 0
	failed := 0
	promoted := 0
	processed := 0
	flushEvery := importProgressBatchSize(total)
	updates := make([]ManagedNode, 0, len(nodes))
	passedIDs := make([]string, 0, len(nodes))
	processedIDs := make(map[string]struct{}, len(nodes))

	if ctx.Err() == nil {
		for event := range s.tester.ProbeBatchWithProgress(ctx, nodes, func(progress ProbeRoundProgress) {
			_ = s.store.UpdateJobProgress(jobID, func(job *ImportJob) {
				job.ProbeRound = progress.Round
				job.ProbeRounds = progress.Rounds
				job.ProbeRoundDone = progress.Completed
				job.ProbeRoundTotal = progress.Total
				job.ProbePending = progress.Pending
				job.ProbeTarget = progress.Target
				job.ProbeConcurrency = progress.Concurrency
				job.Detail = fmt.Sprintf("第 %d/%d 轮，本轮 %d/%d，剩余 %d 个节点", progress.Round, progress.Rounds, progress.Completed, progress.Total, progress.Pending)
				job.UpdatedAt = time.Now()
			})
		}) {
			processedIDs[event.NodeID] = struct{}{}
			node, ok := s.store.GetNode(event.NodeID)
			if !ok {
				continue
			}
			if event.Result.Error != nil {
				updated, _ := finalFailedNodeUpdate(node, event.Result.Error.Error())
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
	}
	if len(updates) > 0 {
		if err := s.store.UpsertNodes(updates); err != nil {
			_ = s.store.UpdateJob(jobID, func(j *ImportJob) {
				j.Status = ImportStatusFailed
				j.Error = "保存测速结果: " + err.Error()
				j.ProbePending = 0
				j.UpdatedAt = time.Now()
			})
			return
		}
	}
	canceled := ctx.Err() != nil
	if canceled {
		pendingStates := make(map[string]ManagedNodeState, len(originalStates)-len(processedIDs))
		for id, state := range originalStates {
			if _, processed := processedIDs[id]; !processed {
				pendingStates[id] = state
			}
		}
		_ = s.store.RestoreNodeStatesIfCurrent(pendingStates, StateTesting)
	}
	if promotePassed && len(passedIDs) > 0 && !canceled {
		if promotedNodes, err := s.PromoteMany(passedIDs, true); err == nil {
			promoted = len(promotedNodes)
			s.updateImportProgress(jobID, passed, failed, promoted)
		} else {
			_ = s.store.UpdateJob(jobID, func(j *ImportJob) {
				j.Status = ImportStatusFailed
				j.Error = "节点入池失败: " + err.Error()
				j.ProbePending = 0
				j.UpdatedAt = time.Now()
			})
			return
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
		j.ProbePending = 0
		if status == ImportStatusCanceled {
			j.Error = "已终止"
		}
		j.UpdatedAt = time.Now()
	})

	// Initial import is a pure generate_204 probe. Depending on the UI option,
	// passed nodes either remain candidates or are promoted into the runtime pool.
}

func (s *Service) updateImportProgress(jobID string, passed, failed, promoted int) {
	_ = s.store.UpdateJobProgress(jobID, func(j *ImportJob) {
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
	nodeIDs, err := s.resolveBatchTestNodeIDs(req)
	if err != nil {
		return BatchTestResponse{}, err
	}
	req.NodeIDs = nodeIDs
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
				updated, poolName := finalFailedNodeUpdate(node, event.Result.Error.Error())
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
	nodeIDs, err := s.resolveBatchTestNodeIDs(req)
	if err != nil {
		return "", err
	}
	req.NodeIDs = nodeIDs
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
	copyJob := *job
	s.publishJobEvent(JobEvent{Kind: "test", ID: jobID, Test: &copyJob})

	started := s.launchBackground(func(cancel context.CancelFunc) {
		s.registerTestCancel(jobID, cancel)
	}, func(ctx context.Context) {
		s.runBatchTestJob(ctx, jobID, req)
	})
	if !started {
		s.updateJob(jobID, func(job *TestJob) {
			job.Status = TestJobCanceled
			job.Phase = "canceled"
			job.Error = "服务正在关闭"
		})
		return "", fmt.Errorf("服务正在关闭")
	}
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
	j, ok := s.testJobs[jobID]
	if !ok {
		s.testJobsMu.Unlock()
		return
	}
	fn(j)
	j.UpdatedAt = time.Now()
	copyJob := *j
	s.testJobsMu.Unlock()
	s.publishJobEvent(JobEvent{Kind: "test", ID: jobID, Test: &copyJob})
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
	var snapshot sourceRefreshSnapshot
	hasSnapshot := false
	defer func() {
		if r := recover(); r != nil {
			message := fmt.Sprintf("测试任务异常: %v", r)
			if hasSnapshot {
				if err := s.restoreSourceRefreshSnapshot(snapshot); err != nil {
					message += "; 恢复节点池失败: " + err.Error()
				}
			}
			s.updateJob(jobID, func(j *TestJob) {
				j.Status = TestJobFailed
				j.Phase = "failed"
				j.Error = message
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
	if !req.ParentRefresh {
		var err error
		snapshot, err = s.captureSourceRefreshSnapshot()
		if err != nil {
			s.updateJob(jobID, func(j *TestJob) {
				j.Status = TestJobFailed
				j.Phase = "failed"
				j.Error = "创建测试回滚点失败: " + err.Error()
			})
			return
		}
		hasSnapshot = true
	}

	changed := false
	needReload := false
	finish := func(status TestJobStatus, phase, errText string) {
		rollback := status == TestJobCanceled || phase == "protected"
		if rollback && hasSnapshot {
			if err := s.restoreSourceRefreshSnapshot(snapshot); err != nil {
				status = TestJobFailed
				phase = "failed"
				errText += "; 恢复检测前节点池失败: " + err.Error()
			}
		}
		if !rollback && (needReload || changed && (req.AutoReload || req.PromotePassed)) {
			if err := s.nodeMgr.TriggerReload(context.Background()); err != nil {
				var restoreErr error
				if hasSnapshot {
					restoreErr = s.restoreSourceRefreshSnapshot(snapshot)
				}
				status = TestJobFinished
				phase = "protected"
				errText = "配置重载失败，已保留检测前节点池: " + err.Error()
				if restoreErr != nil {
					status = TestJobFailed
					phase = "failed"
					errText += "; 恢复失败: " + restoreErr.Error()
				}
			}
		}
		s.updateJob(jobID, func(j *TestJob) {
			j.Status = status
			j.Phase = phase
			j.Error = errText
			j.ProbePending = 0
			j.Applied = status == TestJobFinished && phase == "done"
			j.Protected = phase == "protected"
			if j.Protected {
				j.ProtectionReason = errText
			}
		})
	}

	// --- Phase: probe ---
	if req.Retest {
		s.updateJob(jobID, func(j *TestJob) { j.Phase = "probe"; j.Done = 0 })
		updates := make([]ManagedNode, 0, len(nodes))
		poolNamesToDelete := make([]string, 0)
		for event := range s.tester.ProbeBatchWithProgress(ctx, nodes, func(progress ProbeRoundProgress) {
			s.updateJob(jobID, func(job *TestJob) {
				job.ProbeRound = progress.Round
				job.ProbeRounds = progress.Rounds
				job.ProbeRoundDone = progress.Completed
				job.ProbeRoundTotal = progress.Total
				job.ProbePending = progress.Pending
				job.ProbeTarget = progress.Target
				job.ProbeConcurrency = progress.Concurrency
			})
		}) {
			node, ok := s.store.GetNode(event.NodeID)
			if !ok {
				s.updateJob(jobID, func(j *TestJob) { j.Done++ })
				continue
			}
			if event.Result.Error != nil {
				updated, poolName := finalFailedNodeUpdate(node, event.Result.Error.Error())
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
		s.applyMu.Lock()
		defer s.applyMu.Unlock()
		if len(poolNamesToDelete) > 0 {
			if err := s.deleteConfigNodesStrict(poolNamesToDelete); err != nil {
				finish(TestJobFinished, "protected", "移除失效节点配置失败，已保留检测前节点池: "+err.Error())
				return
			}
		}
		if len(updates) > 0 {
			if err := s.store.UpsertNodes(updates); err != nil {
				finish(TestJobFinished, "protected", "保存测速结果失败，已保留检测前节点池: "+err.Error())
				return
			}
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
				finish(TestJobFinished, "protected", "更新节点配置失败，已保留检测前节点池: "+err.Error())
				return
			}
		}
		if len(updates) > 0 {
			if err := s.store.UpsertNodes(updates); err != nil {
				finish(TestJobFinished, "protected", "保存国家检测结果失败，已保留检测前节点池: "+err.Error())
				return
			}
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
		if err != nil {
			finish(TestJobFinished, "protected", "节点入池失败，已保留检测前节点池: "+err.Error())
			return
		}
		if len(promoted) > 0 {
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
	node, oldName := finalFailedNodeUpdate(node, lastErr)
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
	return failedNodeUpdateWithThreshold(node, lastErr, poolFailureDemoteThreshold)
}

func finalFailedNodeUpdate(node ManagedNode, lastErr string) (ManagedNode, string) {
	return failedNodeUpdateWithThreshold(node, lastErr, 1)
}

func failedNodeUpdateWithThreshold(node ManagedNode, lastErr string, threshold int) (ManagedNode, string) {
	oldName := ""
	now := time.Now()
	node.ConsecutiveFailures++
	node.LastError = lastErr
	node.LastTestAt = now
	node.UpdatedAt = now
	if node.InPool || node.State == StateInPool {
		if node.ConsecutiveFailures < threshold {
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
	return s.deleteBySubscription(url, true)
}

func (s *Service) deleteBySubscription(url string, reload bool) (int, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return 0, fmt.Errorf("订阅 URL 不能为空")
	}
	return s.detachSources(func(ref NodeSourceRef) bool {
		return isURLSourceRef(ref) && strings.TrimSpace(ref.Source) == url
	}, reload)
}

func (s *Service) detachSources(match func(NodeSourceRef) bool, reload bool) (int, error) {
	updates := make([]ManagedNode, 0)
	deleted := make([]string, 0)
	poolNames := make([]string, 0)
	touched := 0
	for _, node := range s.store.ListNodes() {
		refs := nodeSourceRefs(node)
		kept := make([]NodeSourceRef, 0, len(refs))
		removed := false
		for _, ref := range refs {
			if match(ref) {
				removed = true
				continue
			}
			kept = append(kept, ref)
		}
		if !removed {
			continue
		}
		touched++
		if len(kept) == 0 {
			deleted = append(deleted, node.ID)
			if (node.InPool || node.State == StateInPool) && strings.TrimSpace(node.Name) != "" {
				poolNames = append(poolNames, node.Name)
			}
			continue
		}
		node.SourceRefs = kept
		if match(sourceRefFromNode(node)) {
			applyPrimarySource(&node, kept[0])
		}
		updates = append(updates, node)
	}
	if len(poolNames) > 0 {
		if err := s.deleteConfigNodesStrict(poolNames); err != nil {
			return 0, err
		}
		if reload {
			if err := s.nodeMgr.TriggerReload(context.Background()); err != nil {
				return 0, err
			}
		}
	}
	if touched == 0 {
		return 0, nil
	}
	if err := s.store.ApplyNodeChanges(updates, deleted); err != nil {
		return 0, err
	}
	return touched, nil
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
		matches := 0
		refs := nodeSourceRefs(node)
		for _, ref := range refs {
			if isURLSourceRef(ref) && strings.TrimSpace(ref.Source) == url {
				matches++
			}
		}
		if matches == 0 || matches < len(refs) {
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
	return s.detachSources(func(ref NodeSourceRef) bool {
		return importSourceKey(nodeWithSource(ManagedNode{}, ref)) == key
	}, true)
}

func (s *Service) deleteByTagPrefix(tagPrefix string) (int, error) {
	tagPrefix = strings.TrimSpace(tagPrefix)
	if tagPrefix == "" {
		return 0, fmt.Errorf("标签不能为空")
	}
	return s.detachSources(func(ref NodeSourceRef) bool {
		return strings.TrimSpace(ref.TagPrefix) == tagPrefix
	}, true)
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
		for _, ref := range nodeSourceRefs(node) {
			view := nodeWithSource(node, ref)
			key := importSourceKey(view)
			if key == "" {
				key = "node:" + node.ID
			}
			group := groups[key]
			if group == nil {
				group = &ImportSourceSummary{
					Key:         key,
					ImportID:    view.ImportID,
					Mode:        view.ImportMode,
					Format:      view.ImportFormat,
					TagPrefix:   view.TagPrefix,
					Source:      view.ImportSource,
					Refreshable: strings.TrimSpace(view.TagPrefix) != "",
					CreatedAt:   node.CreatedAt,
					UpdatedAt:   node.UpdatedAt,
				}
				groups[key] = group
			}
			if group.TagPrefix == "" {
				group.TagPrefix = view.TagPrefix
			}
			if strings.TrimSpace(view.TagPrefix) != "" {
				group.Refreshable = true
			}
			if group.Format == "" {
				group.Format = view.ImportFormat
			}
			if group.Mode == "" {
				group.Mode = view.ImportMode
			}
			if group.Source == "" {
				group.Source = view.ImportSource
			} else if isURLSourceRef(ref) && strings.TrimSpace(view.ImportSource) != "" && !sourceListContains(group.Source, view.ImportSource) {
				group.Source += "\n" + strings.TrimSpace(view.ImportSource)
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
	if isURLSourceRef(sourceRefFromNode(node)) && source != "" {
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
	_ = s.deleteConfigNodesStrict(names)
}

func (s *Service) deleteConfigNodesStrict(names []string) error {
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
		return nil
	}
	if remover, ok := s.nodeMgr.(NodeBatchRemover); ok {
		return remover.DeleteNodes(context.Background(), clean)
	}
	if remover, ok := s.nodeMgr.(NodeRemover); ok {
		for _, name := range clean {
			if err := remover.DeleteNode(context.Background(), name); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("节点管理器不支持删除配置节点")
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
