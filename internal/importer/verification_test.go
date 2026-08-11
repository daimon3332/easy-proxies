package importer

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNormalizeVerificationPolicy(t *testing.T) {
	t.Run("legacy defaults to 204", func(t *testing.T) {
		policy, err := NormalizeVerificationPolicy(nil, nil)
		if err != nil || !policy.Test204 || len(policy.SiteTargets) != 0 {
			t.Fatalf("policy = %+v, err = %v", policy, err)
		}
	})
	t.Run("site only", func(t *testing.T) {
		disabled := false
		policy, err := NormalizeVerificationPolicy(&disabled, []string{"outlook", "github", "outlook"})
		if err != nil || policy.Test204 || len(policy.SiteTargets) != 2 || policy.SiteTargets[0] != "github" || policy.SiteTargets[1] != "outlook" {
			t.Fatalf("policy = %+v, err = %v", policy, err)
		}
	})
	t.Run("empty policy rejected", func(t *testing.T) {
		disabled := false
		if _, err := NormalizeVerificationPolicy(&disabled, nil); err == nil {
			t.Fatal("expected validation error")
		}
	})
	t.Run("unknown target rejected", func(t *testing.T) {
		if _, err := NormalizeVerificationPolicy(nil, []string{"unknown"}); err == nil {
			t.Fatal("expected validation error")
		}
	})
}

func TestRefreshSourcesWithSiteOnlyPolicy(t *testing.T) {
	store, err := newTestStore(t, filepath.Join(t.TempDir(), "managed_nodes.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	node := ManagedNode{
		ID: "site-only", URI: "trojan://site-only", Name: "site-only", TagPrefix: "site-only",
		ImportMode: "content", ImportSource: "content", ImportFormat: "uri_list", State: StateFailed,
	}
	if err := store.UpsertNode(node); err != nil {
		t.Fatalf("UpsertNode() error = %v", err)
	}
	tester := NewNodeTester(nil, WithTesterConcurrency(2))
	var probeCalls atomic.Int32
	tester.probeOverride = func(context.Context, ManagedNode, string, time.Duration) TestResult {
		probeCalls.Add(1)
		return TestResult{Error: errors.New("204 should not run")}
	}
	mgr := &batchNodeManagerStub{nextPort: 25000}
	svc := NewService(store, tester, mgr)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	svc.connectivityProbePass = func(_ context.Context, nodes []ManagedNode, selected map[string]map[string]struct{}, attempt int, _ time.Duration) <-chan connectivityProbeEvent {
		events := make(chan connectivityProbeEvent, len(nodes))
		for _, current := range nodes {
			for targetID := range selected[current.ID] {
				events <- connectivityProbeEvent{nodeID: current.ID, result: ConnectivityResult{
					NodeID: current.ID, TargetID: targetID, Attempts: attempt, Success: true, LatencyMs: 18,
				}}
			}
		}
		close(events)
		return events
	}
	disabled := false
	jobID, err := svc.StartRefreshSourcesWithPolicy("tag:site-only", &disabled, []string{"outlook"})
	if err != nil {
		t.Fatalf("StartRefreshSourcesWithPolicy() error = %v", err)
	}
	job := waitRefreshJobTerminal(t, svc, jobID)
	pool := store.ListPoolNodes()
	if job.Status != SourceRefreshJobFinished || job.Test204 || len(job.SiteTargets) != 1 || job.SiteTargets[0] != "outlook" {
		t.Fatalf("job = %+v", job)
	}
	if probeCalls.Load() != 0 || len(pool) != 1 || pool[0].Port == 0 || pool[0].State != StateInPool {
		t.Fatalf("probeCalls=%d pool=%+v", probeCalls.Load(), pool)
	}
}

func TestVerifyNodesSiteOnlyRequiresEveryTarget(t *testing.T) {
	tester := NewNodeTester(nil, WithTesterConcurrency(2))
	svc := &Service{tester: tester}
	svc.connectivityProbePass = func(_ context.Context, nodes []ManagedNode, selected map[string]map[string]struct{}, attempt int, _ time.Duration) <-chan connectivityProbeEvent {
		events := make(chan connectivityProbeEvent, len(nodes)*2)
		for _, node := range nodes {
			for targetID := range selected[node.ID] {
				success := node.ID == "good" || targetID == "github"
				events <- connectivityProbeEvent{nodeID: node.ID, result: ConnectivityResult{
					NodeID: node.ID, TargetID: targetID, Attempts: attempt,
					Success: success, LatencyMs: 20,
					Error: map[bool]string{true: "", false: "fixture failure"}[success],
				}}
			}
		}
		close(events)
		return events
	}
	results := svc.verifyNodes(context.Background(), []ManagedNode{{ID: "good"}, {ID: "partial"}}, VerificationPolicy{
		SiteTargets: []string{"github", "outlook"},
	}, nil, nil)
	if results["good"].Error != nil || results["good"].LatencyMs != 20 {
		t.Fatalf("good result = %+v", results["good"])
	}
	if results["partial"].Error == nil {
		t.Fatalf("partial result = %+v, want failure", results["partial"])
	}
}

func TestVerifyNodesCombinedSkipsSitesAfter204Failure(t *testing.T) {
	tester := NewNodeTester(nil, WithTesterConcurrency(2))
	tester.retryDelay = time.Millisecond
	tester.probeOverride = func(_ context.Context, node ManagedNode, _ string, _ time.Duration) TestResult {
		if node.ID == "failed-204" {
			return TestResult{Error: errors.New("fixture 204 failure")}
		}
		return TestResult{LatencyMs: 12}
	}
	svc := &Service{tester: tester}
	var mu sync.Mutex
	checked := make(map[string]bool)
	svc.connectivityProbePass = func(_ context.Context, nodes []ManagedNode, selected map[string]map[string]struct{}, attempt int, _ time.Duration) <-chan connectivityProbeEvent {
		events := make(chan connectivityProbeEvent, len(nodes))
		for _, node := range nodes {
			mu.Lock()
			checked[node.ID] = true
			mu.Unlock()
			for targetID := range selected[node.ID] {
				events <- connectivityProbeEvent{nodeID: node.ID, result: ConnectivityResult{NodeID: node.ID, TargetID: targetID, Attempts: attempt, Success: true, LatencyMs: 30}}
			}
		}
		close(events)
		return events
	}
	results := svc.verifyNodes(context.Background(), []ManagedNode{{ID: "failed-204"}, {ID: "passed"}}, VerificationPolicy{
		Test204: true, SiteTargets: []string{"outlook"},
	}, nil, nil)
	if results["failed-204"].Error == nil || results["passed"].Error != nil {
		t.Fatalf("results = %+v", results)
	}
	mu.Lock()
	defer mu.Unlock()
	if checked["failed-204"] || !checked["passed"] {
		t.Fatalf("site checks = %+v", checked)
	}
}
