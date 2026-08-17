package importer

import (
	"context"
	"errors"
	"testing"
	"time"

	"easy_proxies/internal/config"
	"easy_proxies/internal/proxychain"
)

func boolPointer(value bool) *bool { return &value }

func TestListImportSourcesReportsMixedChainBinding(t *testing.T) {
	svc, store := newBatchServiceForTest(t, &batchNodeManagerStub{})
	if err := store.UpsertNodes([]ManagedNode{
		{ID: "direct", URI: "trojan://direct", TagPrefix: "mixed", ImportMode: "content", ImportSource: "content", State: StateFailed},
		{ID: "chained", URI: "trojan://chained", ChainProfileID: "front", TagPrefix: "mixed", ImportMode: "content", ImportSource: "content", State: StateFailed},
	}); err != nil {
		t.Fatal(err)
	}

	sources, err := svc.ListImportSources()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].ChainBinding != ChainBindingMixed || sources[0].ChainProfileID != "" {
		t.Fatalf("sources = %#v, want one mixed binding", sources)
	}
}

func TestStageTagBindingMergesSharedTargetRoute(t *testing.T) {
	mgr := &batchNodeManagerStub{configNodes: []config.NodeConfig{
		{Name: "A-shared", URI: "trojan://shared", ChainProfileID: "front", Port: 24000},
		{Name: "B-shared", URI: "trojan://shared", Port: 24001},
	}}
	svc, store := newBatchServiceForTest(t, mgr)
	svc.tester = NewNodeTester(nil)
	if err := svc.SetChainProfiles([]proxychain.Profile{{
		ID: "front", Name: "Front", Enabled: true, Hops: []proxychain.Hop{{URI: "socks5://127.0.0.1:1080"}},
	}}); err != nil {
		t.Fatal(err)
	}
	frontID := svc.routeNodeID("trojan://shared", "front")
	if err := store.UpsertNodes([]ManagedNode{
		{ID: frontID, URI: "trojan://shared", ChainProfileID: "front", Name: "A-shared", OriginalName: "shared", TagPrefix: "A", ImportMode: "content", ImportSource: "content", State: StateInPool, InPool: true, Enabled: true, Port: 24000},
		{ID: nodeID("trojan://shared"), URI: "trojan://shared", Name: "B-shared", OriginalName: "shared", TagPrefix: "B", ImportMode: "content", ImportSource: "content", State: StateInPool, InPool: true, Enabled: true, Port: 24001},
	}); err != nil {
		t.Fatal(err)
	}

	stage, err := svc.stageTagBinding("B", "front")
	if err != nil {
		t.Fatal(err)
	}
	if stage.Noop || len(stage.NodeIDs) != 1 {
		t.Fatalf("stage = %#v", stage)
	}
	staged, ok := store.GetNode(stage.NodeIDs[0])
	if !ok {
		t.Fatal("staged node not found")
	}
	staged.State = StatePassed
	if err := store.UpsertNode(staged); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.applyStagedTagNodes(context.Background(), "B", stage.Revision, stage.NodeIDs, true, nil); err != nil {
		t.Fatal(err)
	}

	shared, ok := store.GetNode(frontID)
	if !ok || !shared.InPool || shared.Port != 24000 {
		t.Fatalf("shared route = %#v found=%v", shared, ok)
	}
	refs := nodeSourceRefs(shared)
	if len(refs) != 2 || refs[0].TagPrefix == refs[1].TagPrefix {
		t.Fatalf("shared refs = %#v, want A and B", refs)
	}
	if _, ok := store.GetNode(nodeID("trojan://shared")); ok {
		t.Fatal("old direct B route remained")
	}
}

func TestTagBindingFailureKeepsOriginalBindingAndPort(t *testing.T) {
	mgr := &batchNodeManagerStub{}
	svc, store := newBatchServiceForTest(t, mgr)
	tester := NewNodeTester(nil, WithTesterConcurrency(2))
	tester.retryDelay = time.Millisecond
	tester.probeOverride = func(context.Context, ManagedNode, string, time.Duration) TestResult {
		return TestResult{Error: errors.New("probe failed")}
	}
	svc.tester = tester
	if err := svc.SetChainProfiles([]proxychain.Profile{{
		ID: "front", Name: "Front", Enabled: true, Hops: []proxychain.Hop{{URI: "socks5://127.0.0.1:1080"}},
	}}); err != nil {
		t.Fatal(err)
	}
	oldID := svc.routeNodeID("trojan://keep", "front")
	old := ManagedNode{
		ID: oldID, URI: "trojan://keep", ChainProfileID: "front", Name: "keep", OriginalName: "keep",
		TagPrefix: "A", ImportMode: "content", ImportSource: "content", State: StateInPool,
		InPool: true, Enabled: true, Port: 24000,
	}
	mgr.configNodes = []config.NodeConfig{old.ToConfigNode()}
	if err := store.UpsertNode(old); err != nil {
		t.Fatal(err)
	}

	jobID, err := svc.StartTagBinding(TagBindingRequest{Tags: []string{"A"}, Test204: boolPointer(true)})
	if err != nil {
		t.Fatal(err)
	}
	var job TagBindingJob
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		job, _ = svc.GetTagBindingJob(jobID)
		if job.Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job.Status != "failed" || job.Failed != 1 {
		t.Fatalf("binding job = %#v", job)
	}
	kept, ok := store.GetNode(oldID)
	if !ok || !kept.InPool || kept.Port != 24000 || kept.ChainProfileID != "front" {
		t.Fatalf("original node = %#v found=%v", kept, ok)
	}
	if len(mgr.configNodes) != 1 || mgr.configNodes[0].Port != 24000 || mgr.configNodes[0].ChainProfileID != "front" {
		t.Fatalf("runtime nodes = %#v", mgr.configNodes)
	}
}
