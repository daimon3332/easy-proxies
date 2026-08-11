package importer

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func NormalizeVerificationPolicy(test204 *bool, siteTargets []string) (VerificationPolicy, error) {
	policy := VerificationPolicy{Test204: true}
	if test204 != nil {
		policy.Test204 = *test204
	}
	wanted := make(map[string]struct{}, len(siteTargets))
	for _, raw := range siteTargets {
		id := strings.ToLower(strings.TrimSpace(raw))
		if id == "" {
			continue
		}
		if _, ok := connectivityTargetByID(id); !ok {
			return VerificationPolicy{}, fmt.Errorf("不支持的站点检测目标 %q", raw)
		}
		wanted[id] = struct{}{}
	}
	for _, target := range connectivityTargets {
		if _, ok := wanted[target.ID]; ok {
			policy.SiteTargets = append(policy.SiteTargets, target.ID)
		}
	}
	if !policy.Test204 && len(policy.SiteTargets) == 0 {
		return VerificationPolicy{}, fmt.Errorf("至少启用 204 测速或选择一个站点")
	}
	return policy, nil
}

func VerificationPolicyPointers(policy VerificationPolicy) (*bool, []string) {
	test204 := policy.Test204
	return &test204, append([]string(nil), policy.SiteTargets...)
}

func (s *Service) verifyNodes(
	ctx context.Context,
	nodes []ManagedNode,
	policy VerificationPolicy,
	onProbe func(ProbeRoundProgress),
	onSites func([]SiteTestProgress),
) map[string]TestResult {
	results := make(map[string]TestResult, len(nodes))
	eligible := append([]ManagedNode(nil), nodes...)
	if policy.Test204 {
		eligible = eligible[:0]
		byID := make(map[string]ManagedNode, len(nodes))
		for _, node := range nodes {
			byID[node.ID] = node
		}
		for event := range s.tester.ProbeBatchWithProgress(ctx, nodes, onProbe) {
			results[event.NodeID] = event.Result
			if event.Result.Error == nil {
				if node, ok := byID[event.NodeID]; ok {
					eligible = append(eligible, node)
				}
			}
		}
	}
	if ctx.Err() != nil || len(policy.SiteTargets) == 0 || len(eligible) == 0 {
		return results
	}
	siteResults := s.verifySiteTargets(ctx, eligible, policy.SiteTargets, connectivityProbeTimeout, onSites)
	for _, node := range eligible {
		result := results[node.ID]
		latencyTotal := int64(0)
		latencyCount := int64(0)
		for _, targetID := range policy.SiteTargets {
			site, ok := siteResults[connectivityResultKey(node.ID, targetID)]
			if !ok {
				result.Error = fmt.Errorf("站点 %s 检测结果不完整", connectivityTargetName(targetID))
				break
			}
			if !site.Success {
				message := strings.TrimSpace(site.Error)
				if message == "" {
					message = "不可用"
				}
				result.Error = fmt.Errorf("站点 %s 检测失败: %s", connectivityTargetName(targetID), message)
				break
			}
			latencyTotal += site.LatencyMs
			latencyCount++
		}
		if !policy.Test204 && result.Error == nil && latencyCount > 0 {
			result.LatencyMs = latencyTotal / latencyCount
		}
		results[node.ID] = result
	}
	return results
}

func (s *Service) verifySiteTargets(
	ctx context.Context,
	nodes []ManagedNode,
	targets []string,
	timeout time.Duration,
	onProgress func([]SiteTestProgress),
) map[string]ConnectivityResult {
	results := make(map[string]ConnectivityResult, len(nodes)*len(targets))
	selected := make(map[string]map[string]struct{}, len(nodes))
	progress := make(map[string]*SiteTestProgress, len(targets))
	for _, targetID := range targets {
		progress[targetID] = &SiteTestProgress{TargetID: targetID, Name: connectivityTargetName(targetID), Total: len(nodes)}
	}
	for _, node := range nodes {
		selected[node.ID] = make(map[string]struct{}, len(targets))
		for _, targetID := range targets {
			selected[node.ID][targetID] = struct{}{}
		}
	}
	probePass := s.connectivityProbePass
	if probePass == nil {
		probePass = s.tester.probeConnectivityPass
	}
	publish := func() {
		if onProgress != nil {
			onProgress(siteProgressSnapshot(progress, targets))
		}
	}
	publish()
	for event := range probePass(ctx, nodes, selected, 1, timeout) {
		if _, ok := progress[event.result.TargetID]; !ok {
			continue
		}
		key := connectivityResultKey(event.nodeID, event.result.TargetID)
		results[key] = event.result
		item := progress[event.result.TargetID]
		item.Done++
		if event.result.Success {
			item.Passed++
		} else {
			item.Failed++
		}
		publish()
	}
	if ctx.Err() != nil {
		return results
	}
	retryTargets := make(map[string]map[string]struct{})
	for _, node := range nodes {
		for _, targetID := range targets {
			result, ok := results[connectivityResultKey(node.ID, targetID)]
			if !ok || result.Success || !result.Retryable {
				continue
			}
			if retryTargets[node.ID] == nil {
				retryTargets[node.ID] = make(map[string]struct{})
			}
			retryTargets[node.ID][targetID] = struct{}{}
		}
	}
	if len(retryTargets) == 0 {
		return results
	}
	timer := time.NewTimer(connectivityRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return results
	case <-timer.C:
	}
	for event := range probePass(ctx, nodes, retryTargets, 2, timeout) {
		item := progress[event.result.TargetID]
		if item == nil {
			continue
		}
		key := connectivityResultKey(event.nodeID, event.result.TargetID)
		previous := results[key]
		event.result.FirstSuccess = previous.FirstSuccess
		results[key] = event.result
		item.Retried++
		if !previous.Success && event.result.Success {
			item.Failed--
			item.Passed++
			item.Recovered++
		}
		publish()
	}
	return results
}

func siteProgressSnapshot(progress map[string]*SiteTestProgress, targets []string) []SiteTestProgress {
	result := make([]SiteTestProgress, 0, len(targets))
	for _, targetID := range targets {
		if item := progress[targetID]; item != nil {
			result = append(result, *item)
		}
	}
	return result
}

func connectivityTargetName(targetID string) string {
	if target, ok := connectivityTargetByID(targetID); ok {
		return target.Name
	}
	return targetID
}
