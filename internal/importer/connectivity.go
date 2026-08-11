package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/cookiejar"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"easy_proxies/internal/config"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common"
	M "github.com/sagernet/sing/common/metadata"
)

const (
	connectivityDefaultTimeoutSeconds = 10
	connectivityMinTimeoutSeconds     = 1
	connectivityMaxTimeoutSeconds     = 60
	connectivityProbeTimeout          = connectivityDefaultTimeoutSeconds * time.Second
	connectivityRetryDelay            = time.Second
	connectivityJobTTL                = 30 * time.Minute
	connectivityBodyLimit             = 64 << 10
)

type connectivityComponentSpec struct {
	id           string
	name         string
	url          string
	allowedHosts []string
	marker       string
}

type connectivityTargetSpec struct {
	target     ConnectivityTarget
	components []connectivityComponentSpec
}

var connectivityTargetSpecs = []connectivityTargetSpec{
	{
		target: ConnectivityTarget{ID: "google", Name: "Google", Host: "www.google.com", Port: 443, URL: "https://www.google.com/"},
		components: []connectivityComponentSpec{{
			id: "page", name: "Google 页面", url: "https://www.google.com/", allowedHosts: []string{"google.com"}, marker: "google",
		}},
	},
	{
		target: ConnectivityTarget{ID: "github", Name: "GitHub", Host: "github.com", Port: 443, URL: "https://github.com/"},
		components: []connectivityComponentSpec{{
			id: "page", name: "GitHub 页面", url: "https://github.com/", allowedHosts: []string{"github.com"}, marker: "github",
		}},
	},
	{
		target: ConnectivityTarget{ID: "outlook", Name: "Outlook", Host: "outlook.live.com", Port: 443, URL: "https://outlook.live.com/mail/0/"},
		components: []connectivityComponentSpec{{
			id: "page", name: "Outlook 页面", url: "https://outlook.live.com/mail/0/", allowedHosts: []string{"outlook.live.com"}, marker: "outlook",
		}},
	},
	{
		target: ConnectivityTarget{ID: "proxyspace", Name: "ProxySpace", Host: "dashboard.proxyscrape.com", Port: 443, URL: "https://dashboard.proxyscrape.com/"},
		components: []connectivityComponentSpec{{
			id: "page", name: "ProxyScrape 页面", url: "https://dashboard.proxyscrape.com/", allowedHosts: []string{"dashboard.proxyscrape.com"}, marker: "proxyscrape",
		}},
	},
}

var connectivityTargets = func() []ConnectivityTarget {
	targets := make([]ConnectivityTarget, 0, len(connectivityTargetSpecs))
	for _, spec := range connectivityTargetSpecs {
		targets = append(targets, spec.target)
	}
	return targets
}()

var (
	errConnectivityRedirectLimit = errors.New("connectivity redirect limit")
	errConnectivityRedirectHTTP  = errors.New("connectivity insecure redirect")
)

type connectivityRoute struct {
	node        ManagedNode
	tags        []string
	fingerprint string
}

type connectivityJobState struct {
	job                ConnectivityJob
	routes             map[string]connectivityRoute
	results            map[string]ConnectivityResult
	chainProfilesToken string
}

type connectivityProbeEvent struct {
	nodeID string
	result ConnectivityResult
}

type connectivityOutbound struct {
	outbound adapter.Outbound
	cleanup  func()
}

func ConnectivityTargets() []ConnectivityTarget {
	return append([]ConnectivityTarget(nil), connectivityTargets...)
}

func connectivityTargetByID(id string) (ConnectivityTarget, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, target := range connectivityTargets {
		if target.ID == id {
			return target, true
		}
	}
	return ConnectivityTarget{}, false
}

func connectivityTargetSpecFor(target ConnectivityTarget) connectivityTargetSpec {
	for _, spec := range connectivityTargetSpecs {
		if spec.target.ID == target.ID {
			return spec
		}
	}
	targetURL := strings.TrimSpace(target.URL)
	if targetURL == "" && target.Host != "" {
		targetURL = fmt.Sprintf("https://%s", net.JoinHostPort(target.Host, fmt.Sprint(target.Port)))
	}
	return connectivityTargetSpec{
		target: target,
		components: []connectivityComponentSpec{{
			id: "page", name: target.Name, url: targetURL, allowedHosts: []string{target.Host},
		}},
	}
}

func connectivityRouteFingerprint(node ManagedNode) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(node.URI) + "\x00" + strings.TrimSpace(node.ChainProfileID)))
	return hex.EncodeToString(sum[:16])
}

func managedNodeTags(node ManagedNode) []string {
	seen := make(map[string]struct{})
	for _, ref := range nodeSourceRefs(node) {
		tag := strings.TrimSpace(ref.TagPrefix)
		if tag != "" {
			seen[tag] = struct{}{}
		}
	}
	if tag := strings.TrimSpace(node.TagPrefix); tag != "" {
		seen[tag] = struct{}{}
	}
	tags := make([]string, 0, len(seen))
	for tag := range seen {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

func tagsIntersect(tags []string, selected map[string]struct{}) bool {
	for _, tag := range tags {
		if _, ok := selected[tag]; ok {
			return true
		}
	}
	return false
}

func normalizeConnectivityTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	sort.Strings(result)
	return result
}

func (s *Service) ConnectivityScopes() ConnectivityScopeResponse {
	counts := make(map[string]int)
	for _, node := range s.store.ListNodes() {
		if node.URI == "" || node.State == StateExcluded || node.ImportMode == "refresh_stage" || node.ImportMode == "import_stage" {
			continue
		}
		for _, tag := range managedNodeTags(node) {
			counts[tag]++
		}
	}
	tags := make([]ConnectivityTagScope, 0, len(counts))
	for tag, count := range counts {
		tags = append(tags, ConnectivityTagScope{Tag: tag, Nodes: count})
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].Tag < tags[j].Tag })
	return ConnectivityScopeResponse{Targets: ConnectivityTargets(), Tags: tags}
}

func (s *Service) connectivityRoutes(tags []string) ([]connectivityRoute, error) {
	tags = normalizeConnectivityTags(tags)
	if len(tags) == 0 {
		return nil, fmt.Errorf("请选择至少一个 Tag")
	}
	selected := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		selected[tag] = struct{}{}
	}
	byFingerprint := make(map[string]connectivityRoute)
	for _, node := range s.store.ListNodes() {
		if !connectivityNodeEligible(node) {
			continue
		}
		nodeTags := managedNodeTags(node)
		if !tagsIntersect(nodeTags, selected) {
			continue
		}
		fingerprint := connectivityRouteFingerprint(node)
		if existing, ok := byFingerprint[fingerprint]; ok {
			existing.tags = normalizeConnectivityTags(append(existing.tags, nodeTags...))
			byFingerprint[fingerprint] = existing
			continue
		}
		byFingerprint[fingerprint] = connectivityRoute{node: node, tags: nodeTags, fingerprint: fingerprint}
	}
	if len(byFingerprint) == 0 {
		return nil, fmt.Errorf("所选 Tag 没有可检测节点")
	}
	routes := make([]connectivityRoute, 0, len(byFingerprint))
	for _, route := range byFingerprint {
		routes = append(routes, route)
	}
	sort.Slice(routes, func(i, j int) bool {
		left, right := strings.Join(routes[i].tags, "\x00"), strings.Join(routes[j].tags, "\x00")
		if left == right {
			return routes[i].node.ID < routes[j].node.ID
		}
		return left < right
	})
	return interleaveConnectivityRoutes(routes), nil
}

func connectivityNodeEligible(node ManagedNode) bool {
	return node.URI != "" && node.State != StateExcluded && node.ImportMode != "refresh_stage" && node.ImportMode != "import_stage"
}

func (s *Service) connectivityChainProfilesToken() string {
	if s.tester == nil {
		return ""
	}
	profiles := s.tester.ChainProfiles()
	rows := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		rows = append(rows, fmt.Sprintf("%+v", profile))
	}
	sort.Strings(rows)
	sum := sha256.Sum256([]byte(strings.Join(rows, "\n")))
	return hex.EncodeToString(sum[:16])
}

func interleaveConnectivityRoutes(routes []connectivityRoute) []connectivityRoute {
	groups := make(map[string][]connectivityRoute)
	keys := make([]string, 0)
	for _, route := range routes {
		key := strings.TrimSpace(route.node.ChainProfileID)
		if key == "" {
			key = "direct"
		}
		if len(route.tags) > 0 {
			key += "\x00" + route.tags[0]
		}
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], route)
	}
	sort.Strings(keys)
	result := make([]connectivityRoute, 0, len(routes))
	for len(result) < len(routes) {
		for _, key := range keys {
			if len(groups[key]) == 0 {
				continue
			}
			result = append(result, groups[key][0])
			groups[key] = groups[key][1:]
		}
	}
	return result
}

func (s *Service) StartConnectivityJob(req ConnectivityStartRequest) (string, error) {
	if s.tester == nil {
		return "", fmt.Errorf("节点检测器不可用")
	}
	tags := normalizeConnectivityTags(req.Tags)
	targets, err := normalizeConnectivityTargets(req.Targets)
	if err != nil {
		return "", err
	}
	probeTimeout, err := normalizeConnectivityTimeout(req.TimeoutSeconds)
	if err != nil {
		return "", err
	}
	routes, err := s.connectivityRoutes(tags)
	if err != nil {
		return "", err
	}
	jobID := randomHex(12)
	now := time.Now()
	state := &connectivityJobState{
		job: ConnectivityJob{
			ID: jobID, Status: ConnectivityJobRunning, Phase: "first_pass", Tags: tags, Targets: targets,
			TotalRoutes: len(routes), TotalChecks: len(routes) * len(targets),
			Concurrency: s.tester.concurrency, TimeoutSeconds: int(probeTimeout / time.Second), StartedAt: now, UpdatedAt: now,
		},
		routes:             make(map[string]connectivityRoute, len(routes)),
		results:            make(map[string]ConnectivityResult, len(routes)*len(targets)),
		chainProfilesToken: s.connectivityChainProfilesToken(),
	}
	for _, route := range routes {
		state.routes[route.fingerprint] = route
	}
	s.connectivityJobsMu.Lock()
	for id, old := range s.connectivityJobs {
		if old != nil && old.job.Status != ConnectivityJobRunning && now.Sub(old.job.UpdatedAt) > connectivityJobTTL {
			delete(s.connectivityJobs, id)
		}
	}
	s.connectivityJobs[jobID] = state
	s.connectivityJobsMu.Unlock()
	s.publishConnectivityJob(jobID)
	started := s.launchBackground(func(cancel context.CancelFunc) {
		s.connectivityCancelsMu.Lock()
		s.connectivityCancels[jobID] = cancel
		s.connectivityCancelsMu.Unlock()
	}, func(ctx context.Context) {
		s.runConnectivityJob(ctx, jobID, routes)
	})
	if !started {
		s.finishConnectivityJob(jobID, ConnectivityJobCanceled, "服务正在关闭")
		return "", fmt.Errorf("服务正在关闭")
	}
	return jobID, nil
}

func normalizeConnectivityTimeout(seconds int) (time.Duration, error) {
	if seconds == 0 {
		return connectivityProbeTimeout, nil
	}
	if seconds < connectivityMinTimeoutSeconds || seconds > connectivityMaxTimeoutSeconds {
		return 0, fmt.Errorf("单次超时必须为 %d-%d 秒的整数", connectivityMinTimeoutSeconds, connectivityMaxTimeoutSeconds)
	}
	return time.Duration(seconds) * time.Second, nil
}

func connectivityResultKey(nodeID, targetID string) string {
	return nodeID + "\x00" + targetID
}

func (s *Service) runConnectivityJob(ctx context.Context, jobID string, routes []connectivityRoute) {
	defer func() {
		s.connectivityCancelsMu.Lock()
		delete(s.connectivityCancels, jobID)
		s.connectivityCancelsMu.Unlock()
	}()
	nodes := make([]ManagedNode, 0, len(routes))
	byNode := make(map[string]connectivityRoute, len(routes))
	for _, route := range routes {
		nodes = append(nodes, route.node)
		byNode[route.node.ID] = route
	}
	s.connectivityJobsMu.RLock()
	job := s.connectivityJobs[jobID].job
	jobTargets := append([]string(nil), job.Targets...)
	s.connectivityJobsMu.RUnlock()
	probeTimeout, err := normalizeConnectivityTimeout(job.TimeoutSeconds)
	if err != nil {
		s.finishConnectivityJob(jobID, ConnectivityJobFailed, "检测超时配置无效")
		return
	}
	selectedTargets := make(map[string]map[string]struct{}, len(nodes))
	for _, node := range nodes {
		selectedTargets[node.ID] = make(map[string]struct{}, len(jobTargets))
		for _, target := range jobTargets {
			selectedTargets[node.ID][target] = struct{}{}
		}
	}
	progressStep := max(1, len(nodes)*len(jobTargets)/100)
	probePass := s.connectivityProbePass
	if probePass == nil {
		probePass = s.tester.probeConnectivityPass
	}
	for event := range probePass(ctx, nodes, selectedTargets, 1, probeTimeout) {
		if !containsConnectivityTarget(jobTargets, event.result.TargetID) {
			continue
		}
		route := byNode[event.nodeID]
		event.result.Tags = append([]string(nil), route.tags...)
		event.result.RouteFingerprint = route.fingerprint
		event.result.NodeName = route.node.Name
		s.connectivityJobsMu.Lock()
		state := s.connectivityJobs[jobID]
		if state != nil {
			state.results[connectivityResultKey(event.nodeID, event.result.TargetID)] = event.result
			state.job.DoneChecks++
			state.job.UpdatedAt = time.Now()
		}
		done := 0
		if state != nil {
			done = state.job.DoneChecks
		}
		s.connectivityJobsMu.Unlock()
		if done%progressStep == 0 || done == len(nodes)*len(jobTargets) {
			s.publishConnectivityJob(jobID)
		}
	}
	if ctx.Err() != nil {
		s.finishConnectivityJob(jobID, ConnectivityJobCanceled, "检测已终止")
		return
	}
	retryTargets := make(map[string]map[string]struct{})
	s.connectivityJobsMu.Lock()
	state := s.connectivityJobs[jobID]
	if state != nil {
		for _, result := range state.results {
			if result.Success || !result.Retryable {
				continue
			}
			if retryTargets[result.NodeID] == nil {
				retryTargets[result.NodeID] = make(map[string]struct{})
			}
			retryTargets[result.NodeID][result.TargetID] = struct{}{}
			state.job.RetryChecks++
		}
		if state.job.RetryChecks > 0 {
			state.job.Phase = "retry_wait"
		}
		state.job.UpdatedAt = time.Now()
	}
	s.connectivityJobsMu.Unlock()
	s.publishConnectivityJob(jobID)
	if len(retryTargets) > 0 {
		select {
		case <-ctx.Done():
			s.finishConnectivityJob(jobID, ConnectivityJobCanceled, "检测已终止")
			return
		case <-time.After(connectivityRetryDelay):
		}
		s.connectivityJobsMu.Lock()
		if state := s.connectivityJobs[jobID]; state != nil {
			state.job.Phase = "retry"
			state.job.UpdatedAt = time.Now()
		}
		s.connectivityJobsMu.Unlock()
		s.publishConnectivityJob(jobID)
		for event := range probePass(ctx, nodes, retryTargets, 2, probeTimeout) {
			route := byNode[event.nodeID]
			key := connectivityResultKey(event.nodeID, event.result.TargetID)
			s.connectivityJobsMu.Lock()
			state := s.connectivityJobs[jobID]
			if state != nil {
				previous := state.results[key]
				event.result.FirstSuccess = previous.FirstSuccess
				event.result.Tags = append([]string(nil), route.tags...)
				event.result.RouteFingerprint = route.fingerprint
				event.result.NodeName = route.node.Name
				state.results[key] = event.result
				state.job.RetryDone++
				if !previous.Success && event.result.Success {
					state.job.Recovered++
				}
				state.job.UpdatedAt = time.Now()
			}
			done, total := 0, 0
			if state != nil {
				done = state.job.RetryDone
				total = state.job.RetryChecks
			}
			s.connectivityJobsMu.Unlock()
			if done%progressStep == 0 || done == total {
				s.publishConnectivityJob(jobID)
			}
		}
	}
	if ctx.Err() != nil {
		s.finishConnectivityJob(jobID, ConnectivityJobCanceled, "检测已终止")
		return
	}
	s.finishConnectivityJob(jobID, ConnectivityJobFinished, "")
}

func (s *Service) finishConnectivityJob(jobID string, status ConnectivityJobStatus, message string) {
	historyError := ""
	if status == ConnectivityJobFinished {
		s.connectivityJobsMu.Lock()
		if state := s.connectivityJobs[jobID]; state != nil {
			state.job.Phase = "saving_results"
			state.job.UpdatedAt = time.Now()
		}
		s.connectivityJobsMu.Unlock()
		s.publishConnectivityJob(jobID)
		s.connectivityJobsMu.RLock()
		state := s.connectivityJobs[jobID]
		if state != nil {
			job := connectivityJobSnapshot(state)
			job.UpdatedAt = time.Now()
			results := make(map[string]ConnectivityResult, len(state.results))
			for key, result := range state.results {
				results[key] = result
			}
			complete := true
			for _, route := range state.routes {
				for _, target := range job.Targets {
					if _, ok := results[connectivityResultKey(route.node.ID, target)]; !ok {
						complete = false
						break
					}
				}
				if !complete {
					break
				}
			}
			s.connectivityJobsMu.RUnlock()
			if !complete {
				status = ConnectivityJobFailed
				message = "检测结果不完整，请重新检测"
			} else if err := s.store.db.saveConnectivityRun(job, results); err != nil {
				historyError = "历史结果保存失败，本次结果仍可使用"
			}
		} else {
			s.connectivityJobsMu.RUnlock()
		}
	}
	s.connectivityJobsMu.Lock()
	if state := s.connectivityJobs[jobID]; state != nil {
		state.job.Status = status
		state.job.Phase = string(status)
		state.job.Error = message
		state.job.HistoryError = historyError
		state.job.UpdatedAt = time.Now()
	}
	s.connectivityJobsMu.Unlock()
	s.publishConnectivityJob(jobID)
}

func (s *Service) GetConnectivityJob(jobID string) (ConnectivityJob, bool) {
	s.connectivityJobsMu.RLock()
	state, ok := s.connectivityJobs[strings.TrimSpace(jobID)]
	if !ok {
		s.connectivityJobsMu.RUnlock()
		return ConnectivityJob{}, false
	}
	job := connectivityJobSnapshot(state)
	s.connectivityJobsMu.RUnlock()
	return job, true
}

func (s *Service) publishConnectivityJob(jobID string) {
	job, ok := s.GetConnectivityJob(jobID)
	if ok {
		s.publishJobEvent(JobEvent{Kind: "connectivity", ID: jobID, Connectivity: &job})
	}
}

func (s *Service) CancelConnectivityJob(jobID string) (ConnectivityJob, error) {
	job, ok := s.GetConnectivityJob(jobID)
	if !ok {
		return ConnectivityJob{}, fmt.Errorf("检测任务不存在或已过期")
	}
	if job.Status != ConnectivityJobRunning {
		return job, nil
	}
	s.connectivityCancelsMu.Lock()
	cancel := s.connectivityCancels[jobID]
	s.connectivityCancelsMu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.connectivityJobsMu.Lock()
	if state := s.connectivityJobs[jobID]; state != nil {
		state.job.Phase = "canceling"
		state.job.UpdatedAt = time.Now()
	}
	s.connectivityJobsMu.Unlock()
	s.publishConnectivityJob(jobID)
	job, _ = s.GetConnectivityJob(jobID)
	return job, nil
}

func connectivityJobSnapshot(state *connectivityJobState) ConnectivityJob {
	job := state.job
	job.Tags = append([]string(nil), state.job.Tags...)
	job.Targets = append([]string(nil), state.job.Targets...)
	job.Summaries = connectivitySummaries(job.Tags, job.Targets, state.results)
	return job
}

func connectivityResultVerdict(result ConnectivityResult) ConnectivityVerdict {
	if result.Verdict != "" {
		return result.Verdict
	}
	if result.Success {
		return ConnectivityVerdictUsable
	}
	return ConnectivityVerdictFailed
}

func connectivitySummaries(tags, targets []string, results map[string]ConnectivityResult) []ConnectivityTagSummary {
	type accumulator struct {
		summary   ConnectivityTargetSummary
		latencies []int64
		routes    map[string]struct{}
	}
	byTag := make(map[string]map[string]*accumulator, len(tags))
	for _, tag := range tags {
		byTag[tag] = make(map[string]*accumulator, len(targets))
		for _, targetID := range targets {
			byTag[tag][targetID] = &accumulator{summary: ConnectivityTargetSummary{TargetID: targetID}, routes: make(map[string]struct{})}
		}
	}
	for _, result := range results {
		for _, tag := range result.Tags {
			acc := byTag[tag][result.TargetID]
			if acc == nil {
				continue
			}
			acc.routes[result.RouteFingerprint] = struct{}{}
			acc.summary.Total++
			if result.FirstSuccess {
				acc.summary.FirstPassed++
			}
			switch connectivityResultVerdict(result) {
			case ConnectivityVerdictUsable:
				acc.summary.Passed++
				acc.latencies = append(acc.latencies, result.LatencyMs)
			case ConnectivityVerdictPartial:
				acc.summary.Partial++
				acc.latencies = append(acc.latencies, result.LatencyMs)
			default:
				acc.summary.Failed++
			}
			if result.Attempts > 1 {
				acc.summary.Retried++
				if !result.FirstSuccess && result.Success {
					acc.summary.Recovered++
				}
			}
		}
	}
	summaries := make([]ConnectivityTagSummary, 0, len(tags))
	for _, tag := range tags {
		tagSummary := ConnectivityTagSummary{Tag: tag}
		routes := make(map[string]struct{})
		for _, targetID := range targets {
			acc := byTag[tag][targetID]
			for route := range acc.routes {
				routes[route] = struct{}{}
			}
			sort.Slice(acc.latencies, func(i, j int) bool { return acc.latencies[i] < acc.latencies[j] })
			if len(acc.latencies) > 0 {
				acc.summary.MedianLatencyMs = acc.latencies[len(acc.latencies)/2]
			}
			tagSummary.Targets = append(tagSummary.Targets, acc.summary)
		}
		tagSummary.Routes = len(routes)
		summaries = append(summaries, tagSummary)
	}
	return summaries
}

func (s *Service) ListConnectivityResults(query ConnectivityResultQuery) (ConnectivityResultPage, error) {
	s.connectivityJobsMu.RLock()
	state, ok := s.connectivityJobs[strings.TrimSpace(query.JobID)]
	if !ok {
		s.connectivityJobsMu.RUnlock()
		return ConnectivityResultPage{}, fmt.Errorf("检测任务不存在或已过期")
	}
	items := make([]ConnectivityResult, 0, len(state.results))
	for _, result := range state.results {
		if query.Tag != "" && !containsString(result.Tags, query.Tag) {
			continue
		}
		if query.TargetID != "" && result.TargetID != query.TargetID {
			continue
		}
		verdict := connectivityResultVerdict(result)
		if query.Status == "success" && verdict != ConnectivityVerdictUsable ||
			query.Status == "partial" && verdict != ConnectivityVerdictPartial ||
			query.Status == "failed" && verdict != ConnectivityVerdictFailed {
			continue
		}
		copyResult := result
		copyResult.Tags = append([]string(nil), result.Tags...)
		copyResult.Components = append([]ConnectivityComponentResult(nil), result.Components...)
		items = append(items, copyResult)
	}
	s.connectivityJobsMu.RUnlock()
	sort.Slice(items, func(i, j int) bool {
		if items[i].NodeName == items[j].NodeName {
			return items[i].TargetID < items[j].TargetID
		}
		return items[i].NodeName < items[j].NodeName
	})
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = 100
	}
	if query.PageSize > 500 {
		query.PageSize = 500
	}
	start := (query.Page - 1) * query.PageSize
	if start > len(items) {
		start = len(items)
	}
	end := min(len(items), start+query.PageSize)
	return ConnectivityResultPage{Items: items[start:end], Total: len(items), Page: query.Page, PageSize: query.PageSize}, nil
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func (t *NodeTester) probeConnectivityPass(ctx context.Context, nodes []ManagedNode, selected map[string]map[string]struct{}, attempt int, timeout time.Duration) <-chan connectivityProbeEvent {
	events := make(chan connectivityProbeEvent)
	go func() {
		defer close(events)
		workers := min(max(1, t.concurrency), len(nodes))
		if workers == 0 {
			return
		}
		jobs := make(chan ManagedNode)
		var wg sync.WaitGroup
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for node := range jobs {
					if t.batchSem != nil {
						select {
						case <-ctx.Done():
							return
						case t.batchSem <- struct{}{}:
						}
					}
					results := t.probeConnectivityNode(ctx, node, selected[node.ID], attempt, timeout)
					if t.batchSem != nil {
						<-t.batchSem
					}
					for _, result := range results {
						select {
						case <-ctx.Done():
							return
						case events <- connectivityProbeEvent{nodeID: node.ID, result: result}:
						}
					}
				}
			}()
		}
		for _, node := range nodes {
			if selected != nil && len(selected[node.ID]) == 0 {
				continue
			}
			select {
			case <-ctx.Done():
				close(jobs)
				wg.Wait()
				return
			case jobs <- node:
			}
		}
		close(jobs)
		wg.Wait()
	}()
	return events
}

func (t *NodeTester) probeConnectivityNode(ctx context.Context, node ManagedNode, selected map[string]struct{}, attempt int, timeout time.Duration) []ConnectivityResult {
	targets := connectivityTargets
	if selected != nil {
		targets = make([]ConnectivityTarget, 0, len(selected))
		for _, target := range connectivityTargets {
			if _, ok := selected[target.ID]; ok {
				targets = append(targets, target)
			}
		}
	}
	results := make([]ConnectivityResult, 0, len(targets))
	session, err := t.newConnectivityOutbound(node)
	if err != nil {
		message := redactConnectivityError(err, node)
		for _, target := range targets {
			results = append(results, ConnectivityResult{NodeID: node.ID, TargetID: target.ID, Verdict: ConnectivityVerdictFailed, Attempts: attempt, FailureStage: "build", Error: message, TestedAt: time.Now()})
		}
		return results
	}
	defer session.cleanup()
	client, transport := newConnectivityHTTPClient(session.outbound, nil, timeout)
	defer transport.CloseIdleConnections()
	for _, target := range targets {
		results = append(results, probeConnectivityTargetWithClient(ctx, client, node, target, attempt, timeout))
	}
	return results
}

func (t *NodeTester) newConnectivityOutbound(node ManagedNode) (connectivityOutbound, error) {
	if t.buildOutbound == nil {
		return connectivityOutbound{}, fmt.Errorf("outbound builder is not configured")
	}
	tag := "site-" + safeTagPart(node.ID)
	if node.ChainProfileID != "" {
		if t.buildChainOutbounds == nil {
			return connectivityOutbound{}, fmt.Errorf("chain outbound builder is not configured")
		}
		profile, ok := t.ChainProfile(node.ChainProfileID)
		if !ok || !profile.Enabled {
			return connectivityOutbound{}, fmt.Errorf("前置代理不存在或未启用")
		}
		hops := make([]string, 0, len(profile.Hops))
		for _, hop := range profile.Hops {
			hops = append(hops, hop.URI)
		}
		tag = fmt.Sprintf("%s-%d", tag, t.chainSessionID.Add(1))
		configs, err := t.buildChainOutbounds(tag, node.URI, hops, false)
		if err != nil {
			return connectivityOutbound{}, err
		}
		runtime, err := t.sharedRuntime()
		if err != nil {
			return connectivityOutbound{}, err
		}
		outbound, cleanup, err := runtime.BuildChain(configs)
		if err != nil {
			return connectivityOutbound{}, err
		}
		return connectivityOutbound{outbound: outbound, cleanup: cleanup}, nil
	}
	configOutbound, err := t.buildOutbound(tag, node.URI, false)
	if err != nil {
		return connectivityOutbound{}, err
	}
	runtime, err := t.sharedRuntime()
	if err != nil {
		return connectivityOutbound{}, err
	}
	outbound, err := runtime.Build(configOutbound)
	if err != nil {
		return connectivityOutbound{}, err
	}
	return connectivityOutbound{outbound: outbound, cleanup: func() { _ = common.Close(outbound) }}, nil
}

func newConnectivityHTTPClient(outbound adapter.Outbound, roots *x509.CertPool, timeout time.Duration) (*http.Client, *http.Transport) {
	if timeout <= 0 {
		timeout = connectivityProbeTimeout
	}
	jar, _ := cookiejar.New(nil)
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return outbound.DialContext(ctx, network, M.ParseSocksaddr(address))
		},
		TLSClientConfig:       &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       15 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		Jar:       jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return errConnectivityRedirectHTTP
			}
			if len(via) >= 5 {
				return errConnectivityRedirectLimit
			}
			return nil
		},
	}
	return client, transport
}

func probeConnectivityTarget(ctx context.Context, outbound adapter.Outbound, node ManagedNode, target ConnectivityTarget, attempt int, roots *x509.CertPool, timeout time.Duration) ConnectivityResult {
	client, transport := newConnectivityHTTPClient(outbound, roots, timeout)
	defer transport.CloseIdleConnections()
	return probeConnectivityTargetWithClient(ctx, client, node, target, attempt, timeout)
}

func probeConnectivityTargetWithClient(ctx context.Context, client *http.Client, node ManagedNode, target ConnectivityTarget, attempt int, timeout time.Duration) ConnectivityResult {
	return probeConnectivityTargetSpecWithClient(ctx, client, node, connectivityTargetSpecFor(target), attempt, timeout)
}

func probeConnectivityTargetSpecWithClient(ctx context.Context, client *http.Client, node ManagedNode, spec connectivityTargetSpec, attempt int, timeout time.Duration) ConnectivityResult {
	result := ConnectivityResult{
		NodeID: node.ID, TargetID: spec.target.ID, Verdict: ConnectivityVerdictFailed,
		Attempts: attempt, TestedAt: time.Now(),
	}
	start := time.Now()
	for _, componentSpec := range spec.components {
		component := probeConnectivityComponent(ctx, client, node, componentSpec, attempt, timeout)
		result.Components = append(result.Components, component)
	}
	result.LatencyMs = time.Since(start).Milliseconds()
	if len(result.Components) == 0 {
		result.FailureStage = "content"
		result.Error = "站点检测配置无效"
		return result
	}
	primary := result.Components[0]
	result.TLSVersion = primary.TLSVersion
	result.HTTPStatus = primary.HTTPStatus
	result.FinalHost = primary.FinalHost
	result.ContentType = primary.ContentType
	result.InspectedBytes = primary.InspectedBytes
	if !primary.Success {
		result.FailureStage = primary.FailureStage
		result.Error = primary.Error
		result.Retryable = primary.Retryable
		return result
	}
	allSuccess := true
	for _, component := range result.Components[1:] {
		if component.Success {
			continue
		}
		allSuccess = false
		result.Retryable = result.Retryable || component.Retryable
	}
	if allSuccess {
		result.Verdict = ConnectivityVerdictUsable
		result.Success = true
		result.FirstSuccess = attempt == 1
		return result
	}
	result.Verdict = ConnectivityVerdictPartial
	result.FailureStage = "auth"
	result.Error = "页面可打开，但登录链路不可用"
	return result
}

func probeConnectivityComponent(ctx context.Context, client *http.Client, node ManagedNode, spec connectivityComponentSpec, attempt int, timeout time.Duration) ConnectivityComponentResult {
	result := ConnectivityComponentResult{ID: spec.id, Name: spec.name, Verdict: ConnectivityVerdictFailed, Attempts: attempt}
	if timeout <= 0 {
		timeout = connectivityProbeTimeout
	}
	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, spec.url, nil)
	if err != nil {
		result.FailureStage = "build"
		result.Error = "站点请求配置无效"
		return result
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/136.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	req.Header.Set("Cache-Control", "no-cache")
	start := time.Now()
	response, err := client.Do(req)
	if err != nil {
		result.LatencyMs = time.Since(start).Milliseconds()
		result.FailureStage = connectivityHTTPFailureStage(attemptCtx, err)
		result.Error = redactConnectivityError(err, node)
		result.Retryable = connectivityRetryable(attemptCtx, err, result.FailureStage)
		return result
	}
	defer response.Body.Close()
	result.HTTPStatus = response.StatusCode
	result.FinalHost = strings.ToLower(response.Request.URL.Hostname())
	result.ContentType = response.Header.Get("Content-Type")
	if response.TLS != nil {
		result.TLSVersion = tls.VersionName(response.TLS.Version)
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, connectivityBodyLimit+1))
	result.LatencyMs = time.Since(start).Milliseconds()
	if len(body) > connectivityBodyLimit {
		body = body[:connectivityBodyLimit]
	}
	result.InspectedBytes = int64(len(body))
	if readErr != nil {
		result.FailureStage = connectivityFailureStage(attemptCtx, "content")
		result.Error = redactConnectivityError(readErr, node)
		result.Retryable = connectivityRetryable(attemptCtx, readErr, result.FailureStage)
		return result
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		result.FailureStage = "http"
		result.Error = fmt.Sprintf("HTTP 状态 %d", response.StatusCode)
		result.Retryable = connectivityHTTPStatusRetryable(response.StatusCode)
		return result
	}
	if !connectivityHostAllowed(result.FinalHost, spec.allowedHosts) {
		result.FailureStage = "redirect"
		result.Error = "页面跳转到了非预期站点"
		return result
	}
	mediaType, _, _ := mime.ParseMediaType(result.ContentType)
	if mediaType != "text/html" && mediaType != "application/xhtml+xml" {
		result.FailureStage = "content"
		result.Error = "返回内容不是 HTML 页面"
		return result
	}
	if len(body) < 512 {
		result.FailureStage = "content"
		result.Error = "页面正文过短"
		return result
	}
	lowerBody := bytes.ToLower(body)
	if spec.marker != "" && !bytes.Contains(lowerBody, []byte(strings.ToLower(spec.marker))) {
		result.FailureStage = "content"
		result.Error = "页面内容与目标站点不匹配"
		return result
	}
	result.Verdict = ConnectivityVerdictUsable
	result.Success = true
	return result
}

func connectivityHostAllowed(host string, allowed []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, suffix := range allowed {
		suffix = strings.ToLower(strings.TrimSpace(suffix))
		if suffix != "" && (host == suffix || strings.HasSuffix(host, "."+suffix)) {
			return true
		}
	}
	return false
}

func connectivityHTTPFailureStage(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, errConnectivityRedirectHTTP) || errors.Is(err, errConnectivityRedirectLimit) {
		return "redirect"
	}
	var verification *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	if errors.As(err, &verification) || errors.As(err, &unknownAuthority) || errors.As(err, &hostname) || errors.As(err, &invalid) {
		return "tls"
	}
	return "connect"
}

func connectivityFailureStage(ctx context.Context, fallback string) string {
	if errors.Is(ctx.Err(), context.Canceled) {
		return "canceled"
	}
	return fallback
}

func connectivityRetryable(ctx context.Context, err error, stage string) bool {
	if stage == "canceled" || errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return false
	}
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var verification *tls.CertificateVerificationError
	if errors.As(err, &unknownAuthority) || errors.As(err, &hostname) || errors.As(err, &invalid) || errors.As(err, &verification) {
		return false
	}
	return stage == "connect" || stage == "tls" || stage == "content"
}

func connectivityHTTPStatusRetryable(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusExpectationFailed ||
		status == http.StatusTooEarly || status == http.StatusTooManyRequests || status >= 500
}

func redactConnectivityError(err error, node ManagedNode) string {
	_ = node
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "检测已终止"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "检测超时"
	}
	if errors.Is(err, errConnectivityRedirectHTTP) {
		return "页面跳转到了不安全地址"
	}
	if errors.Is(err, errConnectivityRedirectLimit) {
		return "页面重定向次数过多"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "检测超时"
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "域名解析失败"
	}
	var verification *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	if errors.As(err, &verification) || errors.As(err, &unknownAuthority) || errors.As(err, &hostname) || errors.As(err, &invalid) {
		return "TLS 证书校验失败"
	}
	if errors.Is(err, syscall.ECONNRESET) {
		return "连接被远端重置"
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "目标拒绝连接"
	}
	if errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
		return "目标网络不可达"
	}
	return "代理路由检测失败"
}

type connectivityPoolSelection struct {
	preview    ConnectivityPortPreview
	desiredIDs map[string]struct{}
	failedIDs  map[string]string
	nodes      map[string]ManagedNode
}

const maxConnectivityPreviewItems = 20

func (s *Service) PreviewConnectivityPool(req ConnectivityPortRequest) (ConnectivityPortPreview, error) {
	selection, err := s.connectivityPoolSelection(req)
	if err != nil {
		return ConnectivityPortPreview{}, err
	}
	return selection.preview, nil
}

func (s *Service) connectivityPoolSelection(req ConnectivityPortRequest) (connectivityPoolSelection, error) {
	tags := normalizeConnectivityTags(req.Tags)
	if len(tags) == 0 {
		return connectivityPoolSelection{}, fmt.Errorf("请选择至少一个 Tag")
	}
	if len(req.Targets) == 0 {
		return connectivityPoolSelection{}, fmt.Errorf("请选择至少一个站点")
	}
	targets, err := normalizeConnectivityTargets(req.Targets)
	if err != nil {
		return connectivityPoolSelection{}, err
	}
	s.connectivityJobsMu.RLock()
	state, ok := s.connectivityJobs[strings.TrimSpace(req.JobID)]
	if !ok || state.job.Status != ConnectivityJobFinished {
		s.connectivityJobsMu.RUnlock()
		return connectivityPoolSelection{}, fmt.Errorf("检测任务不存在、未完成或已过期")
	}
	results := make(map[string]ConnectivityResult, len(state.results))
	for key, result := range state.results {
		results[key] = result
	}
	jobTags := append([]string(nil), state.job.Tags...)
	jobTargets := append([]string(nil), state.job.Targets...)
	jobChainProfilesToken := state.chainProfilesToken
	s.connectivityJobsMu.RUnlock()
	if jobChainProfilesToken != s.connectivityChainProfilesToken() {
		return connectivityPoolSelection{}, fmt.Errorf("检测后前置代理配置已变化，请重新检测")
	}
	jobTagSet := make(map[string]struct{}, len(jobTags))
	for _, tag := range jobTags {
		jobTagSet[tag] = struct{}{}
	}
	for _, tag := range tags {
		if _, ok := jobTagSet[tag]; !ok {
			return connectivityPoolSelection{}, fmt.Errorf("Tag %q 不属于该检测任务", tag)
		}
	}
	for _, target := range targets {
		if !containsConnectivityTarget(jobTargets, target) {
			return connectivityPoolSelection{}, fmt.Errorf("站点 %q 未在该任务中检测", target)
		}
	}
	selectedTags := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		selectedTags[tag] = struct{}{}
	}
	current := s.store.ListNodes()
	nodes := make(map[string]ManagedNode, len(current))
	byFingerprint := make(map[string]ManagedNode, len(current))
	for _, node := range current {
		nodes[node.ID] = node
		if connectivityNodeEligible(node) {
			byFingerprint[connectivityRouteFingerprint(node)] = node
		}
	}
	qualifying := make(map[string]struct{})
	nonQualifyingReasons := make(map[string]string)
	selectedCurrent := make(map[string]struct{})
	staleFingerprints := make(map[string]struct{})
	for fingerprint, node := range byFingerprint {
		nodeTags := managedNodeTags(node)
		if !tagsIntersect(nodeTags, selectedTags) {
			continue
		}
		selectedCurrent[node.ID] = struct{}{}
		complete := true
		passed := true
		failureReason := ""
		for _, targetID := range targets {
			result, found := results[connectivityResultKey(node.ID, targetID)]
			if !found || result.RouteFingerprint != fingerprint {
				complete = false
				passed = false
				break
			}
			if !result.Success {
				passed = false
				if failureReason == "" {
					failureReason = connectivityPoolFailureReason(targetID, result)
				}
			}
		}
		if !complete {
			staleFingerprints[fingerprint] = struct{}{}
			continue
		}
		if passed {
			qualifying[node.ID] = struct{}{}
		} else {
			nonQualifyingReasons[node.ID] = failureReason
		}
	}
	for _, result := range results {
		if !tagsIntersect(result.Tags, selectedTags) {
			continue
		}
		if _, found := byFingerprint[result.RouteFingerprint]; !found {
			staleFingerprints[result.RouteFingerprint] = struct{}{}
		}
	}
	desired := make(map[string]struct{})
	failed := make(map[string]string)
	preview := ConnectivityPortPreview{JobID: req.JobID, Tags: tags, Targets: targets, Qualifying: len(qualifying), Stale: len(staleFingerprints)}
	for _, node := range current {
		inPool := node.InPool || node.State == StateInPool
		if inPool {
			preview.CurrentPool++
		}
		_, selected := selectedCurrent[node.ID]
		_, qualified := qualifying[node.ID]
		if !selected {
			if inPool {
				desired[node.ID] = struct{}{}
				preview.Unaffected++
			}
			continue
		}
		if qualified {
			desired[node.ID] = struct{}{}
			if inPool {
				preview.Retained++
			} else {
				preview.Added++
				if len(preview.AddedItems) < maxConnectivityPreviewItems {
					preview.AddedItems = append(preview.AddedItems, connectivityPortChangeItem(node))
				}
			}
			continue
		}
		preview.NonQualifying++
		hasUnselectedTag := false
		for _, tag := range managedNodeTags(node) {
			if _, ok := selectedTags[tag]; !ok {
				hasUnselectedTag = true
				break
			}
		}
		if hasUnselectedTag {
			if inPool {
				desired[node.ID] = struct{}{}
				preview.SharedRetained++
			}
			continue
		}
		failed[node.ID] = nonQualifyingReasons[node.ID]
		preview.WillFail++
		if inPool {
			preview.Removed++
			if len(preview.RemovedItems) < maxConnectivityPreviewItems {
				preview.RemovedItems = append(preview.RemovedItems, connectivityPortChangeItem(node))
			}
		}
	}
	preview.ProjectedPool = len(desired)
	preview.EmptyBlocked = preview.Qualifying == 0
	preview.PreviewToken = connectivityPreviewToken(req.JobID, tags, targets, current)
	sort.Slice(preview.AddedItems, func(i, j int) bool { return preview.AddedItems[i].NodeName < preview.AddedItems[j].NodeName })
	sort.Slice(preview.RemovedItems, func(i, j int) bool { return preview.RemovedItems[i].NodeName < preview.RemovedItems[j].NodeName })
	return connectivityPoolSelection{preview: preview, desiredIDs: desired, failedIDs: failed, nodes: nodes}, nil
}

func connectivityPoolFailureReason(targetID string, result ConnectivityResult) string {
	targetName := targetID
	for _, target := range connectivityTargets {
		if target.ID == targetID {
			targetName = target.Name
			break
		}
	}
	detail := strings.TrimSpace(result.Error)
	if detail == "" {
		switch result.Verdict {
		case ConnectivityVerdictPartial:
			detail = "页面未完整可用"
		case ConnectivityVerdictFailed:
			detail = "页面不可用"
		default:
			detail = "检测未通过"
		}
	}
	return fmt.Sprintf("未通过 %s：%s", targetName, detail)
}

func connectivityPortChangeItem(node ManagedNode) ConnectivityPortChangeItem {
	return ConnectivityPortChangeItem{NodeName: node.Name, Tags: managedNodeTags(node), Port: node.Port, State: node.State}
}

func connectivityPreviewToken(jobID string, tags, targets []string, nodes []ManagedNode) string {
	rows := make([]string, 0, len(nodes))
	for _, node := range nodes {
		rows = append(rows, fmt.Sprintf("%s|%s|%t|%t|%d|%s|%s|%s", node.ID, connectivityRouteFingerprint(node), node.Enabled,
			node.InPool || node.State == StateInPool, node.Port, node.State, node.ImportMode, strings.Join(managedNodeTags(node), ",")))
	}
	sort.Strings(rows)
	sum := sha256.Sum256([]byte(jobID + "\x00" + strings.Join(tags, "\x00") + "\x01" + strings.Join(targets, "\x00") + "\x02" + strings.Join(rows, "\n")))
	return hex.EncodeToString(sum[:16])
}

func (s *Service) ApplyConnectivityPool(ctx context.Context, req ConnectivityPortRequest) (ConnectivityPortApplyResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	selection, err := s.connectivityPoolSelection(req)
	if err != nil {
		return ConnectivityPortApplyResponse{}, err
	}
	if strings.TrimSpace(req.PreviewToken) == "" || req.PreviewToken != selection.preview.PreviewToken {
		return ConnectivityPortApplyResponse{}, fmt.Errorf("端口池已变化或未完成预览，请重新预览后应用")
	}
	if selection.preview.Stale > 0 {
		return ConnectivityPortApplyResponse{}, fmt.Errorf("检测后节点来源已变化，请重新检测")
	}
	if selection.preview.EmptyBlocked && !req.AllowEmpty {
		return ConnectivityPortApplyResponse{}, fmt.Errorf("没有节点满足条件，需明确确认后才能清空所选 Tag 端口")
	}
	lister, ok := s.nodeMgr.(NodeLister)
	if !ok {
		return ConnectivityPortApplyResponse{}, fmt.Errorf("节点管理器不支持配置快照")
	}
	restorer, ok := s.nodeMgr.(NodeSnapshotRestorer)
	if !ok {
		return ConnectivityPortApplyResponse{}, fmt.Errorf("节点管理器不支持配置恢复")
	}
	applyCtx, cancel := context.WithTimeout(ctx, runtimeApplyTimeout)
	defer cancel()
	configBefore, err := lister.ListConfigNodes(applyCtx)
	if err != nil {
		return ConnectivityPortApplyResponse{}, fmt.Errorf("读取运行配置: %w", err)
	}
	nodesBefore := s.store.ListNodes()
	rollback := func(cause error) error {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), runtimeApplyTimeout)
		defer rollbackCancel()
		var rollbackErrors []string
		if err := s.store.ApplyNodeChanges(nodesBefore, nil); err != nil {
			rollbackErrors = append(rollbackErrors, "恢复节点数据: "+err.Error())
		}
		if err := restorer.RestoreConfigNodes(rollbackCtx, configBefore); err != nil {
			rollbackErrors = append(rollbackErrors, "恢复运行配置: "+err.Error())
		}
		if len(rollbackErrors) > 0 {
			return fmt.Errorf("%w; 回滚失败: %s", cause, strings.Join(rollbackErrors, "; "))
		}
		return cause
	}
	configByRoute := make(map[string]config.NodeConfig, len(configBefore))
	for _, node := range configBefore {
		fingerprint := connectivityRouteFingerprint(ManagedNode{URI: node.URI, ChainProfileID: node.ChainProfileID})
		configByRoute[fingerprint] = node
	}
	removeNames := make([]string, 0)
	addNodes := make([]config.NodeConfig, 0)
	for _, node := range nodesBefore {
		_, desired := selection.desiredIDs[node.ID]
		inPool := node.InPool || node.State == StateInPool
		fingerprint := connectivityRouteFingerprint(node)
		if inPool && !desired {
			if configured, ok := configByRoute[fingerprint]; ok {
				removeNames = append(removeNames, configured.Name)
			}
		}
		if desired && !inPool {
			candidate := node.ToConfigNode()
			candidate.Port = 0
			addNodes = append(addNodes, candidate)
		}
	}
	if len(removeNames) > 0 {
		if err := s.deleteConfigNodesStrictContext(applyCtx, removeNames); err != nil {
			return ConnectivityPortApplyResponse{}, rollback(err)
		}
	}
	if len(addNodes) > 0 {
		creator, ok := s.nodeMgr.(NodeBatchCreator)
		if !ok {
			return ConnectivityPortApplyResponse{}, rollback(fmt.Errorf("节点管理器不支持批量生成端口"))
		}
		if _, err := creator.CreateNodes(applyCtx, addNodes); err != nil {
			return ConnectivityPortApplyResponse{}, rollback(err)
		}
	}
	if len(removeNames) > 0 || len(addNodes) > 0 {
		if err := s.nodeMgr.TriggerReload(applyCtx); err != nil {
			return ConnectivityPortApplyResponse{}, rollback(err)
		}
	}
	configured, err := lister.ListConfigNodes(applyCtx)
	if err != nil {
		return ConnectivityPortApplyResponse{}, rollback(err)
	}
	configuredByRoute := make(map[string]config.NodeConfig, len(configured))
	for _, node := range configured {
		fingerprint := connectivityRouteFingerprint(ManagedNode{URI: node.URI, ChainProfileID: node.ChainProfileID})
		configuredByRoute[fingerprint] = node
	}
	updates := make([]ManagedNode, 0, len(nodesBefore))
	order := 0
	for _, node := range nodesBefore {
		_, desired := selection.desiredIDs[node.ID]
		if desired {
			configuredNode, found := configuredByRoute[connectivityRouteFingerprint(node)]
			if !found || configuredNode.Port == 0 {
				return ConnectivityPortApplyResponse{}, rollback(fmt.Errorf("生成后的运行配置缺少节点端口"))
			}
			node.Name = configuredNode.Name
			node.Port = configuredNode.Port
			node.Order = order
			node.State = StateInPool
			node.InPool = true
			node.Enabled = true
			node.ConsecutiveFailures = 0
			node.LastError = ""
			order++
		} else if failureReason, failed := selection.failedIDs[node.ID]; failed {
			node.State = StateFailed
			node.InPool = false
			node.Port = 0
			node.LastError = failureReason
		}
		updates = append(updates, node)
	}
	if err := s.store.ApplyNodeChanges(updates, nil); err != nil {
		return ConnectivityPortApplyResponse{}, rollback(err)
	}
	if err := s.verifyAppliedRuntime(applyCtx); err != nil {
		return ConnectivityPortApplyResponse{}, rollback(err)
	}
	return ConnectivityPortApplyResponse{ConnectivityPortPreview: selection.preview, PoolCount: len(selection.desiredIDs)}, nil
}
