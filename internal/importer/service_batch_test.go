package importer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"easy_proxies/internal/config"

	"github.com/sagernet/sing-box/option"
)

type batchNodeManagerStub struct {
	createdBatches [][]config.NodeConfig
	deletedBatches [][]string
	reloadCount    int
	nextPort       uint16
	configNodes    []config.NodeConfig
	restoreCount   int
	createErr      error
	deleteErr      error
	reloadErr      error
	listErr        error
	restoreErr     error
}

func (m *batchNodeManagerStub) CreateNode(ctx context.Context, node config.NodeConfig) (config.NodeConfig, error) {
	nodes, err := m.CreateNodes(ctx, []config.NodeConfig{node})
	if err != nil {
		return config.NodeConfig{}, err
	}
	return nodes[0], nil
}

func (m *batchNodeManagerStub) CreateNodes(ctx context.Context, nodes []config.NodeConfig) ([]config.NodeConfig, error) {
	_ = ctx
	if m.createErr != nil {
		return nil, m.createErr
	}
	m.createdBatches = append(m.createdBatches, append([]config.NodeConfig(nil), nodes...))
	out := make([]config.NodeConfig, len(nodes))
	for i, node := range nodes {
		if m.nextPort == 0 {
			m.nextPort = 24000
		}
		node.Port = m.nextPort
		m.nextPort++
		out[i] = node
		m.configNodes = append(m.configNodes, node)
	}
	return out, nil
}

func (m *batchNodeManagerStub) ListConfigNodes(ctx context.Context) ([]config.NodeConfig, error) {
	_ = ctx
	if m.listErr != nil {
		return nil, m.listErr
	}
	out := make([]config.NodeConfig, len(m.configNodes))
	copy(out, m.configNodes)
	return out, nil
}

func (m *batchNodeManagerStub) RestoreConfigNodes(ctx context.Context, nodes []config.NodeConfig) error {
	_ = ctx
	m.restoreCount++
	if m.restoreErr != nil {
		return m.restoreErr
	}
	m.configNodes = append([]config.NodeConfig(nil), nodes...)
	return nil
}

func TestCancelRefreshUsesSingleParentSnapshotRestore(t *testing.T) {
	mgr := &batchNodeManagerStub{}
	store, err := NewStore(filepath.Join(t.TempDir(), "managed_nodes.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	tester := NewNodeTester(nil, WithTesterConcurrency(2))
	tester.retryDelay = time.Millisecond
	tester.probeOverride = func(ctx context.Context, _ ManagedNode, _ string, _ time.Duration) TestResult {
		<-ctx.Done()
		return TestResult{Error: ctx.Err()}
	}
	nodes := []ManagedNode{
		{ID: "a", Name: "a", URI: "trojan://a", TagPrefix: "A", ImportMode: "uri", State: StateInPool, InPool: true, Enabled: true, Port: 24000},
		{ID: "b", Name: "b", URI: "trojan://b", TagPrefix: "B", ImportMode: "uri", State: StateInPool, InPool: true, Enabled: true, Port: 24001},
	}
	for _, node := range nodes {
		mgr.configNodes = append(mgr.configNodes, node.ToConfigNode())
	}
	if err := store.UpsertNodes(nodes); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}
	svc := NewService(store, tester, mgr)
	jobID, err := svc.StartRefreshSources("")
	if err != nil {
		t.Fatalf("StartRefreshSources() error = %v", err)
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		job, _ := svc.GetRefreshJob(jobID)
		if job.Phase == "testing" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := svc.CancelRefreshJob(jobID); err != nil {
		t.Fatalf("CancelRefreshJob() error = %v", err)
	}
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		job, _ := svc.GetRefreshJob(jobID)
		if job.Status == SourceRefreshJobCanceled {
			if mgr.restoreCount != 1 || len(store.ListPoolNodes()) != 2 {
				t.Fatalf("restoreCount=%d pool=%d job=%#v", mgr.restoreCount, len(store.ListPoolNodes()), job)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, _ := svc.GetRefreshJob(jobID)
	t.Fatalf("refresh job did not cancel: %#v", job)
}

func (m *batchNodeManagerStub) DeleteNode(ctx context.Context, name string) error {
	return m.DeleteNodes(ctx, []string{name})
}

func (m *batchNodeManagerStub) DeleteNodes(ctx context.Context, names []string) error {
	_ = ctx
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.deletedBatches = append(m.deletedBatches, append([]string(nil), names...))
	return nil
}

func (m *batchNodeManagerStub) TriggerReload(ctx context.Context) error {
	_ = ctx
	m.reloadCount++
	return m.reloadErr
}

func newBatchServiceForTest(t *testing.T, mgr *batchNodeManagerStub) (*Service, *Store) {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "managed_nodes.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	return NewService(store, nil, mgr), store
}

func waitTestJobTerminal(t *testing.T, svc *Service, jobID string) TestJob {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := svc.GetTestJob(jobID)
		if ok && job.Status != TestJobRunning {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, _ := svc.GetTestJob(jobID)
	t.Fatalf("test job did not finish: %#v", job)
	return TestJob{}
}

func waitRefreshJobTerminal(t *testing.T, svc *Service, jobID string) SourceRefreshJob {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := svc.GetRefreshJob(jobID)
		if ok && job.Status != SourceRefreshJobRunning {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, _ := svc.GetRefreshJob(jobID)
	t.Fatalf("refresh job did not finish: %#v", job)
	return SourceRefreshJob{}
}

func TestBatchRetestRemovesPoolNodeOnlyAfterThreeFailedRounds(t *testing.T) {
	mgr := &batchNodeManagerStub{configNodes: []config.NodeConfig{{Name: "tag-node", URI: "trojan://node", Port: 24000}}}
	store, err := NewStore(filepath.Join(t.TempDir(), "managed_nodes.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	tester := NewNodeTester(nil, WithTesterConcurrency(2))
	tester.retryDelay = time.Millisecond
	attempts := 0
	var mu sync.Mutex
	tester.probeOverride = func(context.Context, ManagedNode, string, time.Duration) TestResult {
		mu.Lock()
		attempts++
		mu.Unlock()
		return TestResult{Error: errors.New("fixture failure")}
	}
	svc := NewService(store, tester, mgr)
	if err := store.UpsertNode(ManagedNode{ID: "node", Name: "tag-node", OriginalName: "node", URI: "trojan://node", State: StateInPool, InPool: true, Enabled: true, Port: 24000}); err != nil {
		t.Fatalf("UpsertNode() error = %v", err)
	}
	jobID, err := svc.StartBatchTest(BatchTestRequest{NodeIDs: []string{"node"}, Retest: true, PromotePassed: true, AutoReload: true})
	if err != nil {
		t.Fatalf("StartBatchTest() error = %v", err)
	}
	job := waitTestJobTerminal(t, svc, jobID)
	node, _ := store.GetNode("node")
	if attempts != 3 || job.Failed != 1 || !job.Applied || node.State != StateFailed || node.InPool || node.Port != 0 {
		t.Fatalf("attempts=%d job=%#v node=%#v", attempts, job, node)
	}
}

func TestBatchRetestKeepsPoolNodeWhenSecondRoundPasses(t *testing.T) {
	mgr := &batchNodeManagerStub{configNodes: []config.NodeConfig{{Name: "tag-node", URI: "trojan://node", Port: 24000}}}
	store, err := NewStore(filepath.Join(t.TempDir(), "managed_nodes.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	tester := NewNodeTester(nil, WithTesterConcurrency(2))
	tester.retryDelay = time.Millisecond
	attempts := 0
	tester.probeOverride = func(context.Context, ManagedNode, string, time.Duration) TestResult {
		attempts++
		if attempts == 1 {
			return TestResult{Error: errors.New("first round failure")}
		}
		return TestResult{LatencyMs: 12}
	}
	svc := NewService(store, tester, mgr)
	if err := store.UpsertNode(ManagedNode{ID: "node", Name: "tag-node", URI: "trojan://node", State: StateInPool, InPool: true, Enabled: true, Port: 24000}); err != nil {
		t.Fatalf("UpsertNode() error = %v", err)
	}
	jobID, err := svc.StartBatchTest(BatchTestRequest{NodeIDs: []string{"node"}, Retest: true, PromotePassed: true, AutoReload: true})
	if err != nil {
		t.Fatalf("StartBatchTest() error = %v", err)
	}
	job := waitTestJobTerminal(t, svc, jobID)
	node, _ := store.GetNode("node")
	if attempts != 2 || job.Passed != 1 || !job.Applied || node.State != StateInPool || !node.InPool || len(mgr.deletedBatches) != 0 {
		t.Fatalf("attempts=%d job=%#v node=%#v deleted=%#v", attempts, job, node, mgr.deletedBatches)
	}
}

func TestBatchRetestProtectsPoolWhenAllControlNodesFail(t *testing.T) {
	mgr := &batchNodeManagerStub{}
	store, err := NewStore(filepath.Join(t.TempDir(), "managed_nodes.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	tester := NewNodeTester(nil, WithTesterConcurrency(8))
	tester.retryDelay = time.Millisecond
	tester.probeOverride = func(context.Context, ManagedNode, string, time.Duration) TestResult {
		return TestResult{Error: errors.New("systemic fixture failure")}
	}
	nodes := make([]ManagedNode, 20)
	ids := make([]string, 20)
	for i := range nodes {
		ids[i] = fmt.Sprintf("node-%02d", i)
		nodes[i] = ManagedNode{ID: ids[i], Name: ids[i], URI: "trojan://" + ids[i], State: StateInPool, InPool: true, Enabled: true, Port: uint16(24000 + i)}
		mgr.configNodes = append(mgr.configNodes, nodes[i].ToConfigNode())
	}
	if err := store.UpsertNodes(nodes); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}
	svc := NewService(store, tester, mgr)
	jobID, err := svc.StartBatchTest(BatchTestRequest{NodeIDs: ids, Retest: true, PromotePassed: true, AutoReload: true})
	if err != nil {
		t.Fatalf("StartBatchTest() error = %v", err)
	}
	job := waitTestJobTerminal(t, svc, jobID)
	if !job.Protected || job.Applied || job.Phase != "protected" || len(store.ListPoolNodes()) != len(nodes) || len(mgr.deletedBatches) != 0 {
		t.Fatalf("job=%#v pool=%d deleted=%#v", job, len(store.ListPoolNodes()), mgr.deletedBatches)
	}
}

func TestPromoteManyUsesSingleConfigBatchAndReload(t *testing.T) {
	mgr := &batchNodeManagerStub{nextPort: 25000}
	svc, store := newBatchServiceForTest(t, mgr)

	nodes := []ManagedNode{
		{ID: "n1", Name: "tag-US1", URI: "vmess://one", State: StatePassed, Enabled: true},
		{ID: "n2", Name: "tag-JP1", URI: "vmess://two", State: StatePassed, Enabled: true},
	}
	if err := store.UpsertNodes(nodes); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	promoted, err := svc.PromoteMany([]string{"n1", "n2"}, true)
	if err != nil {
		t.Fatalf("PromoteMany() error = %v", err)
	}
	if len(promoted) != 2 {
		t.Fatalf("PromoteMany() promoted %d nodes, want 2", len(promoted))
	}
	if len(mgr.createdBatches) != 1 || len(mgr.createdBatches[0]) != 2 {
		t.Fatalf("CreateNodes batches = %#v, want one batch of two nodes", mgr.createdBatches)
	}
	if mgr.reloadCount != 1 {
		t.Fatalf("reloadCount = %d, want 1", mgr.reloadCount)
	}
	for _, id := range []string{"n1", "n2"} {
		node, ok := store.GetNode(id)
		if !ok || !node.InPool || node.State != StateInPool || node.Port == 0 {
			t.Fatalf("node %s not marked in pool correctly: %#v found=%v", id, node, ok)
		}
	}
}

func TestPromoteManyDeletesExistingCandidatesAndContinues(t *testing.T) {
	mgr := &batchNodeManagerStub{
		nextPort: 25000,
		configNodes: []config.NodeConfig{
			{Name: "tag-US1", URI: "vmess://one", Port: 24000},
		},
	}
	svc, store := newBatchServiceForTest(t, mgr)

	nodes := []ManagedNode{
		{ID: "existing", Name: "tag-US1", URI: "vmess://one", State: StatePassed, Enabled: true},
		{ID: "new", Name: "tag-JP1", URI: "vmess://two", State: StatePassed, Enabled: true},
	}
	if err := store.UpsertNodes(nodes); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	promoted, err := svc.PromoteMany([]string{"existing", "new"}, true)
	if err != nil {
		t.Fatalf("PromoteMany() error = %v", err)
	}
	if len(promoted) != 1 || promoted[0].ID != "new" {
		t.Fatalf("PromoteMany() promoted %#v, want only new", promoted)
	}
	if _, ok := store.GetNode("existing"); ok {
		t.Fatal("existing candidate should be deleted from store")
	}
	newNode, ok := store.GetNode("new")
	if !ok || !newNode.InPool || newNode.State != StateInPool {
		t.Fatalf("new node not promoted correctly: %#v found=%v", newNode, ok)
	}
}

func TestPromoteManyRenamesDuplicateCandidateNames(t *testing.T) {
	mgr := &batchNodeManagerStub{nextPort: 25000}
	svc, store := newBatchServiceForTest(t, mgr)

	nodes := []ManagedNode{
		{ID: "n1", Name: "free1-node", URI: "vmess://one", State: StatePassed, Enabled: true},
		{ID: "n2", Name: "free1-node", URI: "vmess://two", State: StatePassed, Enabled: true},
	}
	if err := store.UpsertNodes(nodes); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	promoted, err := svc.PromoteMany([]string{"n1", "n2"}, true)
	if err != nil {
		t.Fatalf("PromoteMany() error = %v", err)
	}
	if len(promoted) != 2 {
		t.Fatalf("PromoteMany() promoted %d nodes, want 2", len(promoted))
	}
	n1, _ := store.GetNode("n1")
	n2, _ := store.GetNode("n2")
	if n1.Name != "free1-node" || n2.Name != "free1-node-2" {
		t.Fatalf("names = %q, %q; want free1-node, free1-node-2", n1.Name, n2.Name)
	}
	if !n1.InPool || !n2.InPool {
		t.Fatalf("nodes should both be in pool: %#v %#v", n1, n2)
	}
}

func TestDeleteManyUsesSingleConfigBatchAndReload(t *testing.T) {
	mgr := &batchNodeManagerStub{}
	svc, store := newBatchServiceForTest(t, mgr)

	nodes := []ManagedNode{
		{ID: "pool-1", Name: "tag-US1", URI: "vmess://one", State: StateInPool, InPool: true, Port: 24000},
		{ID: "pool-2", Name: "tag-JP1", URI: "vmess://two", State: StateInPool, InPool: true, Port: 24001},
		{ID: "failed-1", Name: "tag-bad", URI: "vmess://bad", State: StateFailed},
	}
	if err := store.UpsertNodes(nodes); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	deleted, err := svc.DeleteMany([]string{"pool-1", "pool-2", "failed-1"})
	if err != nil {
		t.Fatalf("DeleteMany() error = %v", err)
	}
	if deleted != 3 {
		t.Fatalf("DeleteMany() deleted %d nodes, want 3", deleted)
	}
	if len(mgr.deletedBatches) != 1 {
		t.Fatalf("DeleteNodes batch count = %d, want 1", len(mgr.deletedBatches))
	}
	wantDeleted := []string{"tag-US1", "tag-JP1"}
	if !reflect.DeepEqual(mgr.deletedBatches[0], wantDeleted) {
		t.Fatalf("DeleteNodes names = %#v, want %#v", mgr.deletedBatches[0], wantDeleted)
	}
	if mgr.reloadCount != 1 {
		t.Fatalf("reloadCount = %d, want 1", mgr.reloadCount)
	}
	if remaining := store.ListNodes(); len(remaining) != 0 {
		t.Fatalf("remaining nodes = %#v, want none", remaining)
	}
}

func TestDeleteBySubscriptionBatchesStoreAndConfigRemoval(t *testing.T) {
	mgr := &batchNodeManagerStub{}
	svc, store := newBatchServiceForTest(t, mgr)
	const subURL = "https://example.test/sub"

	nodes := []ManagedNode{
		{ID: "pool-1", Name: "tag-US1", URI: "vmess://one", State: StateInPool, InPool: true, ImportSource: subURL},
		{ID: "pool-2", Name: "tag-JP1", URI: "vmess://two", State: StateInPool, InPool: true, ImportSource: subURL},
		{ID: "other", Name: "tag-other", URI: "vmess://other", State: StatePassed, ImportSource: "content"},
	}
	if err := store.UpsertNodes(nodes); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	deleted, err := svc.DeleteBySubscription(subURL)
	if err != nil {
		t.Fatalf("DeleteBySubscription() error = %v", err)
	}
	if deleted != 2 {
		t.Fatalf("DeleteBySubscription() deleted %d nodes, want 2", deleted)
	}
	if len(mgr.deletedBatches) != 1 || len(mgr.deletedBatches[0]) != 2 {
		t.Fatalf("DeleteNodes batches = %#v, want one batch of two names", mgr.deletedBatches)
	}
	if mgr.reloadCount != 1 {
		t.Fatalf("reloadCount = %d, want 1", mgr.reloadCount)
	}
	if _, ok := store.GetNode("other"); !ok {
		t.Fatal("node from another source should remain")
	}
}

func TestMarkSubscriptionFailedDemotesPoolNodeAfterConsecutiveFailures(t *testing.T) {
	mgr := &batchNodeManagerStub{}
	svc, store := newBatchServiceForTest(t, mgr)
	const subURL = "https://example.test/sub"

	nodes := []ManagedNode{
		{ID: "pool-1", Name: "tag-US1", OriginalName: "US1", URI: "vmess://one", State: StateInPool, InPool: true, Port: 24000, ImportSource: subURL, TagPrefix: "tag"},
		{ID: "cand-1", Name: "tag-JP1", OriginalName: "JP1", URI: "vmess://two", State: StatePassed, ImportSource: subURL, TagPrefix: "tag"},
		{ID: "fail-1", Name: "tag-HK1", OriginalName: "HK1", URI: "vmess://four", State: StateFailed, LastError: "old error", ImportSource: subURL, TagPrefix: "tag"},
		{ID: "other", Name: "other-SG1", OriginalName: "SG1", URI: "vmess://three", State: StatePassed, ImportSource: "content", TagPrefix: "other"},
	}
	if err := store.UpsertNodes(nodes); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	moved, err := svc.MarkSubscriptionFailed(subURL, "refresh timeout")
	if err != nil {
		t.Fatalf("MarkSubscriptionFailed() error = %v", err)
	}
	if moved != 3 {
		t.Fatalf("moved = %d, want 3", moved)
	}
	if mgr.reloadCount != 0 {
		t.Fatalf("reloadCount = %d, want 0 before failure threshold", mgr.reloadCount)
	}
	if len(mgr.deletedBatches) != 0 {
		t.Fatalf("deletedBatches = %#v, want none before failure threshold", mgr.deletedBatches)
	}
	if len(mgr.createdBatches) != 0 {
		t.Fatalf("createdBatches = %#v, want none", mgr.createdBatches)
	}

	poolNode, _ := store.GetNode("pool-1")
	if poolNode.State != StateInPool || !poolNode.InPool || poolNode.Port != 24000 || poolNode.Name != "tag-US1" || poolNode.LastError != "refresh timeout" || poolNode.ConsecutiveFailures != 1 {
		t.Fatalf("poolNode = %#v, want retained pool node with one failure", poolNode)
	}
	candNode, _ := store.GetNode("cand-1")
	if candNode.State != StateFailed || candNode.InPool || candNode.Port != 0 || candNode.Name != "tag-JP1" || candNode.LastError != "refresh timeout" {
		t.Fatalf("candNode = %#v, want failed candidate node", candNode)
	}
	failNode, _ := store.GetNode("fail-1")
	if failNode.State != StateFailed || failNode.InPool || failNode.Port != 0 || failNode.Name != "tag-HK1" || failNode.LastError != "refresh timeout" {
		t.Fatalf("failNode = %#v, want failed node retained in failed pool", failNode)
	}
	otherNode, _ := store.GetNode("other")
	if otherNode.State != StatePassed {
		t.Fatalf("otherNode = %#v, want unchanged node from another source", otherNode)
	}

	for i := 0; i < 2; i++ {
		if _, err := svc.MarkSubscriptionFailed(subURL, "refresh timeout"); err != nil {
			t.Fatalf("MarkSubscriptionFailed() retry %d error = %v", i, err)
		}
	}
	if mgr.reloadCount != 1 {
		t.Fatalf("reloadCount = %d, want 1 after failure threshold", mgr.reloadCount)
	}
	if len(mgr.deletedBatches) != 1 || !reflect.DeepEqual(mgr.deletedBatches[0], []string{"tag-US1"}) {
		t.Fatalf("deletedBatches = %#v, want [[\"tag-US1\"]]", mgr.deletedBatches)
	}
	poolNode, _ = store.GetNode("pool-1")
	if poolNode.State != StateFailed || poolNode.InPool || poolNode.Port != 0 || poolNode.ConsecutiveFailures != 3 {
		t.Fatalf("poolNode = %#v, want failed after threshold", poolNode)
	}
}

func TestFinalizeRefreshJobFailsWhenNoSuccessfulURLs(t *testing.T) {
	svc, _ := newBatchServiceForTest(t, &batchNodeManagerStub{})
	job := &SourceRefreshJob{TotalURLs: 1, Successful: 0, PoolCount: 5}
	svc.finalizeRefreshJob(job)
	if job.Status != SourceRefreshJobFailed {
		t.Fatalf("job.Status = %q, want %q", job.Status, SourceRefreshJobFailed)
	}
	svc.recalculateRefreshJob(job)
	if job.Phase != "failed" {
		t.Fatalf("job.Phase = %q, want failed", job.Phase)
	}
	if job.Error != "全部导入来源重新检测失败" {
		t.Fatalf("job.Error = %q, want default failure message", job.Error)
	}
}

func TestSourceRefreshTargetsIncludeEveryTaggedSource(t *testing.T) {
	svc, store := newBatchServiceForTest(t, &batchNodeManagerStub{})
	nodes := []ManagedNode{
		{ID: "url", State: StatePassed, ImportMode: "url", ImportSource: "https://example.test/sub", TagPrefix: "shared"},
		{ID: "failed", State: StateFailed, ImportID: "imp-1", ImportMode: "content", ImportSource: "content", ImportFormat: "clash_yaml", TagPrefix: "shared"},
		{ID: "pool", State: StateInPool, InPool: true, ImportID: "imp-2", ImportMode: "content", ImportSource: "content", ImportFormat: "base64", TagPrefix: "shared"},
		{ID: "legacy", State: StateParsed, TagPrefix: "legacy"},
		{ID: "untagged", State: StateFailed, ImportID: "imp-3", ImportMode: "content", ImportSource: "content"},
	}
	if err := store.UpsertNodes(nodes); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	targets, err := svc.sourceRefreshTargets("")
	if err != nil {
		t.Fatalf("sourceRefreshTargets() error = %v", err)
	}
	if len(targets) != 2 || targets[0].TagPrefix != "legacy" || targets[1].TagPrefix != "shared" {
		t.Fatalf("targets = %#v, want legacy and shared tags", targets)
	}
	shared := targets[1]
	if !reflect.DeepEqual(shared.URLs, []string{"https://example.test/sub"}) {
		t.Fatalf("shared URLs = %#v", shared.URLs)
	}
	if !reflect.DeepEqual(shared.LocalNodeIDs, []string{"failed", "pool"}) {
		t.Fatalf("shared local nodes = %#v", shared.LocalNodeIDs)
	}
	if !reflect.DeepEqual(shared.LocalFormats, []string{"base64", "clash_yaml"}) {
		t.Fatalf("shared local formats = %#v", shared.LocalFormats)
	}

	selected, err := svc.sourceRefreshTargets("import:imp-1")
	if err != nil {
		t.Fatalf("sourceRefreshTargets(import) error = %v", err)
	}
	if len(selected) != 1 || selected[0].TagPrefix != "shared" || len(selected[0].LocalNodeIDs) != 2 || len(selected[0].URLs) != 1 {
		t.Fatalf("selected targets = %#v, want every source under the shared tag", selected)
	}
}

func TestRefreshLocalSourceRetestsEveryTaggedState(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "managed_nodes.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	tester := NewNodeTester(func(tag, uri string, skipCertVerify bool) (option.Outbound, error) {
		return option.Outbound{}, errors.New("probe fixture failure")
	})
	svc := NewService(store, tester, &batchNodeManagerStub{})
	nodes := []ManagedNode{
		{ID: "parsed", URI: "trojan://parsed", State: StateParsed, ImportID: "imp", ImportMode: "content", ImportSource: "content", ImportFormat: "uri_list", TagPrefix: "local"},
		{ID: "passed", URI: "trojan://passed", State: StatePassed, ImportID: "imp", ImportMode: "content", ImportSource: "content", ImportFormat: "uri_list", TagPrefix: "local"},
		{ID: "failed", URI: "trojan://failed", State: StateFailed, ImportID: "imp", ImportMode: "content", ImportSource: "content", ImportFormat: "uri_list", TagPrefix: "local"},
		{ID: "pool", URI: "trojan://pool", Name: "local-pool", State: StateInPool, InPool: true, Port: 24000, ImportID: "imp", ImportMode: "content", ImportSource: "content", ImportFormat: "uri_list", TagPrefix: "local"},
		{ID: "excluded", URI: "trojan://excluded", State: StateExcluded, ImportID: "imp", ImportMode: "content", ImportSource: "content", ImportFormat: "uri_list", TagPrefix: "local"},
		{ID: "untagged", URI: "trojan://untagged", State: StateFailed, ImportID: "other", ImportMode: "content", ImportSource: "content"},
	}
	if err := store.UpsertNodes(nodes); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	jobID, err := svc.StartRefreshSources("import:imp")
	if err != nil {
		t.Fatalf("StartRefreshSources() error = %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var job SourceRefreshJob
	for time.Now().Before(deadline) {
		job, _ = svc.GetRefreshJob(jobID)
		if job.Status != SourceRefreshJobRunning {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if job.Status != SourceRefreshJobFinished {
		t.Fatalf("refresh job = %#v, want finished", job)
	}
	if len(job.Groups) != 1 || len(job.Groups[0].URLs) != 1 {
		t.Fatalf("refresh groups = %#v", job.Groups)
	}
	row := job.Groups[0].URLs[0]
	if row.Kind != "content" || row.Total != 5 || row.Done != 5 || row.Passed != 0 || row.Failed != 5 {
		t.Fatalf("local refresh row = %#v, want complete 5-node progress", row)
	}
	for _, id := range []string{"parsed", "passed", "failed", "pool", "excluded"} {
		node, _ := store.GetNode(id)
		if node.LastTestAt.IsZero() {
			t.Fatalf("node %s was not retested: %#v", id, node)
		}
	}
	untagged, _ := store.GetNode("untagged")
	if !untagged.LastTestAt.IsZero() {
		t.Fatalf("untagged node was unexpectedly tested: %#v", untagged)
	}
}

func TestApplyImportJobProgress(t *testing.T) {
	row := SourceRefreshURL{Status: "testing"}
	applyImportJobProgress(&row, ImportJob{
		Status:   ImportStatusRunning,
		Total:    10,
		Passed:   6,
		Failed:   4,
		Promoted: 2,
	})
	if row.Status != "promoting" || row.Total != 10 || row.Done != 10 || row.Passed != 6 || row.Failed != 4 || row.Promoted != 2 {
		t.Fatalf("row = %#v, want live promoting progress", row)
	}
}

func TestRecalculateRefreshJobAggregatesNodeProgress(t *testing.T) {
	svc, _ := newBatchServiceForTest(t, &batchNodeManagerStub{})
	job := &SourceRefreshJob{
		Status: SourceRefreshJobRunning,
		Groups: []SourceRefreshGroup{{URLs: []SourceRefreshURL{
			{Status: "completed", Total: 10, Done: 10, Passed: 8, Failed: 2, Promoted: 8},
			{Status: "testing", Total: 5, Done: 3, Passed: 2, Failed: 1},
		}}},
	}
	svc.recalculateRefreshJob(job)
	if job.Phase != "testing" || job.TotalNodes != 15 || job.DoneNodes != 13 || job.Passed != 10 || job.FailedNodes != 3 || job.Promoted != 8 {
		t.Fatalf("job = %#v, want aggregated live progress", job)
	}
}

func TestStartRefreshSourcesReusesRunningJob(t *testing.T) {
	svc, _ := newBatchServiceForTest(t, &batchNodeManagerStub{})
	svc.refreshJobs["active"] = &SourceRefreshJob{ID: "active", Status: SourceRefreshJobRunning}
	jobID, err := svc.StartRefreshSources("")
	if err != nil {
		t.Fatalf("StartRefreshSources() error = %v", err)
	}
	if jobID != "active" {
		t.Fatalf("jobID = %q, want active", jobID)
	}
}

func TestRefreshSourcesDoesNotLetSlowFirstURLBlockHealthySources(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer slow.Close()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("trojan://pass@example.com:443#fast\n"))
	}))
	defer fast.Close()

	store, err := NewStore(filepath.Join(t.TempDir(), "managed_nodes.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	tester := NewNodeTester(func(tag, uri string, skipCertVerify bool) (option.Outbound, error) {
		return option.Outbound{}, errors.New("probe fixture failure")
	})
	svc := NewService(store, tester, &batchNodeManagerStub{})
	svc.refreshConcurrency = 2
	svc.refreshSourceTimeout = 300 * time.Millisecond
	svc.refreshProxyCandidates = 1
	if err := store.UpsertNodes([]ManagedNode{
		{ID: "slow", URI: "trojan://slow", State: StateFailed, ImportMode: "url", ImportSource: slow.URL, TagPrefix: "A-slow"},
		{ID: "fast", URI: "trojan://fast", State: StateFailed, ImportMode: "url", ImportSource: fast.URL, TagPrefix: "B-fast"},
	}); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	jobID, err := svc.StartRefreshSources("")
	if err != nil {
		t.Fatalf("StartRefreshSources() error = %v", err)
	}
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		job, _ := svc.GetRefreshJob(jobID)
		if job.Groups[1].URLs[0].Status != "waiting" {
			_, _ = svc.CancelRefreshJob(jobID)
			for cancelDeadline := time.Now().Add(time.Second); time.Now().Before(cancelDeadline); {
				canceled, _ := svc.GetRefreshJob(jobID)
				if canceled.Status == SourceRefreshJobCanceled {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, _ := svc.GetRefreshJob(jobID)
	t.Fatalf("healthy source stayed blocked behind slow source: %#v", job.Groups)
}

func TestCancelRefreshSourcesStopsPullingJob(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer slow.Close()
	svc, store := newBatchServiceForTest(t, &batchNodeManagerStub{})
	svc.refreshSourceTimeout = 10 * time.Second
	if err := store.UpsertNode(ManagedNode{ID: "slow", URI: "trojan://slow", State: StateFailed, ImportMode: "url", ImportSource: slow.URL, TagPrefix: "slow"}); err != nil {
		t.Fatalf("UpsertNode() error = %v", err)
	}
	jobID, err := svc.StartRefreshSources("")
	if err != nil {
		t.Fatalf("StartRefreshSources() error = %v", err)
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		job, _ := svc.GetRefreshJob(jobID)
		if job.Phase == "pulling" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := svc.CancelRefreshJob(jobID); err != nil {
		t.Fatalf("CancelRefreshJob() error = %v", err)
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		job, _ := svc.GetRefreshJob(jobID)
		if job.Status == SourceRefreshJobCanceled {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, _ := svc.GetRefreshJob(jobID)
	t.Fatalf("refresh job did not cancel promptly: %#v", job)
}

func TestCanceledImportRestoresUntestedNodes(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "managed_nodes.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	tester := NewNodeTester(func(tag, uri string, skipCertVerify bool) (option.Outbound, error) {
		return option.Outbound{}, errors.New("unexpected probe")
	})
	svc := NewService(store, tester, &batchNodeManagerStub{})
	nodes := []ManagedNode{
		{ID: "parsed-1", URI: "trojan://one", State: StateTesting},
		{ID: "parsed-2", URI: "trojan://two", State: StateTesting},
	}
	if err := store.UpsertNodes(nodes); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}
	jobID := "canceled-import"
	if err := store.UpsertJob(ImportJob{ID: jobID, Status: ImportStatusRunning, Total: len(nodes)}); err != nil {
		t.Fatalf("UpsertJob() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc.runPipeline(ctx, jobID, nodes, map[string]ManagedNodeState{
		"parsed-1": StateParsed,
		"parsed-2": StateParsed,
	}, false)

	for _, id := range []string{"parsed-1", "parsed-2"} {
		node, _ := store.GetNode(id)
		if node.State != StateParsed {
			t.Fatalf("node %s state = %q, want %q", id, node.State, StateParsed)
		}
	}
	job, _ := store.GetJob(jobID)
	if job.Status != ImportStatusCanceled {
		t.Fatalf("job status = %q, want %q", job.Status, ImportStatusCanceled)
	}
}

func TestNewServiceRecoversPersistedTestingNodes(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "managed_nodes.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	now := time.Now()
	if err := store.UpsertNodes([]ManagedNode{
		{ID: "new", URI: "trojan://new", State: StateTesting, Enabled: true},
		{ID: "pool", URI: "trojan://pool", State: StateTesting, InPool: true},
		{ID: "excluded", URI: "trojan://excluded", State: StateTesting, LastTestAt: now, Enabled: false},
		{ID: "failed", URI: "trojan://failed", State: StateTesting, LastTestAt: now, LastError: "timeout", Enabled: true},
		{ID: "passed", URI: "trojan://passed", State: StateTesting, LastTestAt: now, Enabled: true},
	}); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}
	NewService(store, nil, &batchNodeManagerStub{})

	want := map[string]ManagedNodeState{
		"new": StateParsed, "pool": StateInPool, "excluded": StateExcluded, "failed": StateFailed, "passed": StatePassed,
	}
	for id, state := range want {
		node, _ := store.GetNode(id)
		if node.State != state {
			t.Fatalf("node %s state = %q, want %q", id, node.State, state)
		}
	}
}

func TestCompactRefreshSourceErrorRedactsURLAndPreservesUTF8(t *testing.T) {
	const sourceURL = "https://example.test/sub?token=secret-token"
	err := errors.New("Get \"" + sourceURL + "\": " + strings.Repeat("连接超时", 100))
	message := compactRefreshSourceError(err, sourceURL)
	if strings.Contains(message, sourceURL) || strings.Contains(message, "secret-token") {
		t.Fatalf("message leaked source URL: %q", message)
	}
	if !strings.Contains(message, "订阅地址") || !utf8.ValidString(message) {
		t.Fatalf("message was not safely compacted: %q", message)
	}
}

func TestFetchSubscriptionViaPoolBoundsAndPrioritizesCandidates(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "managed_nodes.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	attempted := make([]string, 0)
	tester := NewNodeTester(func(tag, uri string, skipCertVerify bool) (option.Outbound, error) {
		attempted = append(attempted, uri)
		return option.Outbound{}, errors.New("fixture build failure")
	})
	svc := NewService(store, tester, &batchNodeManagerStub{})
	svc.refreshProxyCandidates = 2
	now := time.Now()
	nodes := []ManagedNode{
		{ID: "same", URI: "trojan://same", TagPrefix: "source", State: StateInPool, InPool: true, LatencyMs: 10, LastTestAt: now},
		{ID: "older", URI: "trojan://older", TagPrefix: "other", State: StateInPool, InPool: true, LatencyMs: 30, LastTestAt: now.Add(-time.Minute)},
		{ID: "newer", URI: "trojan://newer", TagPrefix: "other", State: StateInPool, InPool: true, LatencyMs: 80, LastTestAt: now},
	}
	if err := store.UpsertNodes(nodes); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}
	_, err = svc.fetchSubscriptionViaPool(context.Background(), "https://example.test/sub", http.Header{}, "source", 0, nil)
	if err == nil {
		t.Fatal("fetchSubscriptionViaPool() unexpectedly succeeded")
	}
	want := []string{"trojan://newer", "trojan://older"}
	if !reflect.DeepEqual(attempted, want) {
		t.Fatalf("attempted = %#v, want bounded prioritized candidates %#v", attempted, want)
	}
}

func TestParseRefreshSubscriptionURLOnceReplacesSameSourceOnly(t *testing.T) {
	mgr := &batchNodeManagerStub{}
	svc, store := newBatchServiceForTest(t, mgr)
	const newURI = "trojan://pass@example.com:443#Alpha"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(newURI + "\n"))
	}))
	defer ts.Close()

	old := ManagedNode{
		ID:           "old",
		Name:         "sub-OLD1",
		OriginalName: "OLD1",
		URI:          "trojan://old@example.com:443#OLD1",
		State:        StateInPool,
		InPool:       true,
		Port:         24000,
		ImportMode:   "url",
		ImportSource: ts.URL,
		ImportFormat: "uri_list",
		TagPrefix:    "sub",
	}
	other := ManagedNode{
		ID:           "other",
		Name:         "sub-KEEP1",
		OriginalName: "KEEP1",
		URI:          "trojan://keep@example.com:443#KEEP1",
		State:        StatePassed,
		ImportMode:   "url",
		ImportSource: "https://example.test/other",
		ImportFormat: "uri_list",
		TagPrefix:    "sub",
	}
	if err := store.UpsertNodes([]ManagedNode{old, other}); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	parsed, err := svc.parseRefreshSubscriptionURLOnce("sub", ts.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("parseRefreshSubscriptionURLOnce() error = %v", err)
	}
	if parsed.ImportID == "" || len(parsed.Nodes) != 1 {
		t.Fatalf("parsed = %#v, want one parsed node with import id", parsed)
	}
	if len(mgr.deletedBatches) != 1 || !reflect.DeepEqual(mgr.deletedBatches[0], []string{"sub-OLD1"}) {
		t.Fatalf("deletedBatches = %#v, want [[\"sub-OLD1\"]]", mgr.deletedBatches)
	}
	if mgr.reloadCount != 1 {
		t.Fatalf("reloadCount = %d, want 1", mgr.reloadCount)
	}
	if _, ok := store.GetNode("old"); ok {
		t.Fatal("old source node should be replaced")
	}
	if _, ok := store.GetNode("other"); !ok {
		t.Fatal("other source node should remain")
	}
	newNode, ok := store.GetNode(nodeID(newURI))
	if !ok || newNode.State != StateParsed || newNode.ImportSource != ts.URL || newNode.TagPrefix != "sub" {
		t.Fatalf("newNode = %#v found=%v, want parsed replacement node", newNode, ok)
	}
}

func TestRefreshStageKeepsOldPoolUntilResultsAreApplied(t *testing.T) {
	mgr := &batchNodeManagerStub{nextPort: 25000}
	svc, store := newBatchServiceForTest(t, mgr)
	const newURI = "trojan://pass@example.com:443#New"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(newURI + "\n"))
	}))
	defer ts.Close()
	old := ManagedNode{ID: "old", Name: "sub-Old", URI: "trojan://old", State: StateInPool, InPool: true, Enabled: true, Port: 24000, ImportMode: "url", ImportSource: ts.URL, TagPrefix: "sub"}
	mgr.configNodes = append(mgr.configNodes, old.ToConfigNode())
	if err := store.UpsertNode(old); err != nil {
		t.Fatalf("UpsertNode() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	parsed, err := svc.parseRefreshSubscriptionURLContext(ctx, "sub", ts.URL, 0, nil)
	if err != nil {
		t.Fatalf("parseRefreshSubscriptionURLContext() error = %v", err)
	}
	if oldNode, ok := store.GetNode("old"); !ok || !oldNode.InPool {
		t.Fatalf("old pool node changed during staging: %#v found=%v", oldNode, ok)
	}
	if len(parsed.Nodes) != 1 || parsed.Nodes[0].ImportMode != "refresh_stage" {
		t.Fatalf("parsed = %#v", parsed)
	}
	staged, _ := store.GetNode(parsed.Nodes[0].ID)
	staged.State = StatePassed
	if err := store.UpsertNode(staged); err != nil {
		t.Fatalf("UpsertNode(staged) error = %v", err)
	}
	promoted, err := svc.applyStagedRefreshNodes(ts.URL, []string{staged.ID})
	if err != nil {
		t.Fatalf("applyStagedRefreshNodes() error = %v", err)
	}
	if promoted != 1 {
		t.Fatalf("promoted = %d, want 1", promoted)
	}
	if _, ok := store.GetNode("old"); ok {
		t.Fatal("old node still exists after apply")
	}
	newNode, ok := store.GetNode(nodeID(newURI))
	if !ok || newNode.State != StateInPool || !newNode.InPool || newNode.Port == 0 {
		t.Fatalf("new node = %#v found=%v", newNode, ok)
	}
}

func TestImportProgressBatchSize(t *testing.T) {
	cases := []struct {
		total int
		want  int
	}{
		{0, 1},
		{1, 1},
		{19, 1},
		{40, 2},
		{200, 10},
		{1000, 10},
	}
	for _, tc := range cases {
		if got := importProgressBatchSize(tc.total); got != tc.want {
			t.Fatalf("importProgressBatchSize(%d) = %d, want %d", tc.total, got, tc.want)
		}
	}
}

func TestParseDuplicateURLReplacesPrefixSnapshot(t *testing.T) {
	mgr := &batchNodeManagerStub{}
	svc, store := newBatchServiceForTest(t, mgr)
	const uri = "trojan://pass@example.com:443#Alpha"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(uri + "\n"))
	}))
	defer ts.Close()

	existing := ManagedNode{
		ID:           nodeID(uri),
		URI:          uri,
		OriginalName: "Alpha",
		Name:         "old-Alpha",
		TagPrefix:    "old",
		ImportMode:   "url",
		ImportSource: ts.URL,
		ImportFormat: "uri_list",
		State:        StateInPool,
		Enabled:      true,
		InPool:       true,
		Port:         24000,
		LatencyMs:    88,
		CountryCode:  "JP",
	}
	if err := store.UpsertNode(existing); err != nil {
		t.Fatalf("UpsertNode() error = %v", err)
	}

	parsed, err := svc.Parse(ParseRequest{Mode: "url", URL: ts.URL, TagPrefix: "new"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(parsed.Nodes) != 1 {
		t.Fatalf("Parse() nodes = %d, want 1", len(parsed.Nodes))
	}
	node := parsed.Nodes[0]
	if node.TagPrefix != "old" || node.Name != "old-Alpha" || node.State != StateParsed || node.InPool || node.Port != 0 || node.LatencyMs != 0 || node.CountryCode != "" {
		t.Fatalf("duplicate node was not replaced as latest snapshot: %#v", node)
	}

	stored, ok := store.GetNode(node.ID)
	if !ok || stored.State != StateParsed || stored.InPool {
		t.Fatalf("stored node = %#v found=%v, want latest parsed snapshot", stored, ok)
	}
}

func TestParseContentReplacesFailedPrefixSnapshot(t *testing.T) {
	mgr := &batchNodeManagerStub{}
	svc, store := newBatchServiceForTest(t, mgr)
	const uri = "trojan://pass@example.com:443#Alpha"
	if err := store.UpsertNodes([]ManagedNode{
		{ID: nodeID(uri), URI: uri, Name: "Glados-Alpha", TagPrefix: "Glados", State: StateFailed, ImportMode: "url", ImportSource: "https://example.test/glados"},
		{ID: "stale", URI: "trojan://stale@example.com:443#Stale", Name: "Glados-Stale", TagPrefix: "Glados", State: StatePassed},
	}); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	parsed, err := svc.Parse(ParseRequest{Mode: "content", Content: uri + "\n", TagPrefix: "Glados"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(parsed.Nodes) != 1 || parsed.Nodes[0].State != StateParsed {
		t.Fatalf("parsed nodes = %#v, want one fresh parsed node", parsed.Nodes)
	}
	if _, ok := store.GetNode("stale"); ok {
		t.Fatal("stale node from the replaced tag should be deleted")
	}
	job, ok := store.GetJob(parsed.ImportID)
	if !ok || job.Total != 1 || job.Mode != "content" || job.TagPrefix != "Glados" {
		t.Fatalf("import job = %#v found=%v, want content snapshot metadata", job, ok)
	}
}

func TestCommitRejectsNonParsedNodes(t *testing.T) {
	svc, store := newBatchServiceForTest(t, &batchNodeManagerStub{})
	if err := store.UpsertNode(ManagedNode{ID: "failed", State: StateFailed}); err != nil {
		t.Fatalf("UpsertNode() error = %v", err)
	}
	if err := store.UpsertJob(ImportJob{ID: "import", Status: ImportStatusParsed, NodeIDs: []string{"failed"}}); err != nil {
		t.Fatalf("UpsertJob() error = %v", err)
	}
	if _, err := svc.Commit("import", CommitRequest{}); err == nil {
		t.Fatal("Commit() should reject an empty parsed selection")
	}
	if _, ok := store.GetJob("import"); !ok {
		t.Fatal("the parsed import job should remain available for diagnosis")
	}
}

func TestParseRejectsEmptyTagPrefix(t *testing.T) {
	svc, _ := newBatchServiceForTest(t, &batchNodeManagerStub{})
	if _, err := svc.Parse(ParseRequest{Mode: "content", Content: "ss://example"}); err == nil {
		t.Fatal("Parse() should reject an empty tag prefix")
	}
}

func TestListAndDeleteImportSources(t *testing.T) {
	mgr := &batchNodeManagerStub{}
	svc, store := newBatchServiceForTest(t, mgr)
	nodes := []ManagedNode{
		{ID: "url-1", Name: "sub-US1", URI: "trojan://one", State: StateInPool, InPool: true, ImportMode: "url", ImportSource: "https://example.test/sub", ImportFormat: "uri_list", TagPrefix: "sub"},
		{ID: "url-2", Name: "sub-JP1", URI: "trojan://two", State: StateFailed, ImportMode: "url", ImportSource: "https://example.test/sub", ImportFormat: "uri_list", TagPrefix: "sub"},
		{ID: "content-1", Name: "local-SG1", URI: "trojan://three", State: StatePassed, ImportID: "imp-1", ImportMode: "content", ImportSource: "content", ImportFormat: "base64", TagPrefix: "local"},
	}
	if err := store.UpsertNodes(nodes); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	sources, err := svc.ListImportSources()
	if err != nil {
		t.Fatalf("ListImportSources() error = %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("ListImportSources() = %#v, want 2 groups", sources)
	}
	byKey := map[string]ImportSourceSummary{}
	for _, source := range sources {
		byKey[source.Key] = source
	}
	urlGroup := byKey["tag:sub"]
	if !urlGroup.Refreshable || urlGroup.Total != 2 || urlGroup.Pool != 1 || urlGroup.Failed != 1 || urlGroup.TagPrefix != "sub" {
		t.Fatalf("url group = %#v", urlGroup)
	}
	contentGroup := byKey["import:imp-1"]
	if !contentGroup.Refreshable || contentGroup.Format != "base64" || contentGroup.Candidate != 1 || contentGroup.TagPrefix != "local" {
		t.Fatalf("content group = %#v", contentGroup)
	}

	deleted, err := svc.DeleteImportSource("import:imp-1")
	if err != nil {
		t.Fatalf("DeleteImportSource() error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("DeleteImportSource() deleted %d, want 1", deleted)
	}
	if _, ok := store.GetNode("content-1"); ok {
		t.Fatal("content source node should be deleted")
	}
	if _, ok := store.GetNode("url-1"); !ok {
		t.Fatal("url source node should remain")
	}
}

func TestDeleteAllImportSourcesBatchesStoreAndConfigRemoval(t *testing.T) {
	mgr := &batchNodeManagerStub{}
	svc, store := newBatchServiceForTest(t, mgr)
	nodes := []ManagedNode{
		{ID: "pool-url", Name: "sub-US1", URI: "trojan://one", State: StateInPool, InPool: true, ImportMode: "url", ImportSource: "https://example.test/sub", TagPrefix: "sub"},
		{ID: "pool-content", Name: "local-SG1", URI: "trojan://two", State: StateInPool, InPool: true, ImportID: "imp-1", ImportMode: "content", ImportSource: "content", TagPrefix: "local"},
		{ID: "failed", Name: "bad", URI: "trojan://three", State: StateFailed, ImportID: "imp-2", ImportMode: "content", ImportSource: "content"},
	}
	if err := store.UpsertNodes(nodes); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}

	deleted, err := svc.DeleteAllImportSources()
	if err != nil {
		t.Fatalf("DeleteAllImportSources() error = %v", err)
	}
	if deleted != 3 {
		t.Fatalf("DeleteAllImportSources() deleted %d, want 3", deleted)
	}
	if remaining := store.ListNodes(); len(remaining) != 0 {
		t.Fatalf("remaining nodes = %#v, want none", remaining)
	}
	if len(mgr.deletedBatches) != 1 {
		t.Fatalf("DeleteNodes batch count = %d, want 1", len(mgr.deletedBatches))
	}
	wantDeleted := []string{"sub-US1", "local-SG1"}
	sort.Strings(wantDeleted)
	sort.Strings(mgr.deletedBatches[0])
	if !reflect.DeepEqual(mgr.deletedBatches[0], wantDeleted) {
		t.Fatalf("DeleteNodes names = %#v, want %#v", mgr.deletedBatches[0], wantDeleted)
	}
	if mgr.reloadCount != 1 {
		t.Fatalf("reloadCount = %d, want 1", mgr.reloadCount)
	}
}

func TestStartRefreshRequiresConfigSnapshot(t *testing.T) {
	mgr := &batchNodeManagerStub{listErr: errors.New("snapshot unavailable")}
	svc, store := newBatchServiceForTest(t, mgr)
	if err := store.UpsertNode(ManagedNode{ID: "node", URI: "trojan://node", TagPrefix: "local", ImportMode: "content", State: StateFailed}); err != nil {
		t.Fatalf("UpsertNode() error = %v", err)
	}
	if _, err := svc.StartRefreshSources("tag:local"); err == nil || !strings.Contains(err.Error(), "创建刷新回滚点") {
		t.Fatalf("StartRefreshSources() error = %v", err)
	}
	if jobID := svc.activeRefreshJobID(); jobID != "" {
		t.Fatalf("unexpected active refresh job %q", jobID)
	}
}

func TestFinalizeRefreshJobReportsPartialCompletion(t *testing.T) {
	svc, _ := newBatchServiceForTest(t, &batchNodeManagerStub{})
	job := &SourceRefreshJob{TotalURLs: 2, Successful: 1, Failed: 1, PoolCount: 5}
	svc.finalizeRefreshJob(job)
	svc.recalculateRefreshJob(job)
	if job.Status != SourceRefreshJobFinished || job.Phase != "partial" || job.Error == "" {
		t.Fatalf("job = %#v, want partial completion", job)
	}
}

func TestRefreshMixedSourcesReportsPartialAndCleansTransientState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fixture unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	node := ManagedNode{ID: "local", Name: "local", URI: "trojan://local", TagPrefix: "mixed", ImportMode: "content", ImportFormat: "uri_list", State: StateFailed, Enabled: true}
	mgr := &batchNodeManagerStub{}
	store, err := NewStore(filepath.Join(t.TempDir(), "managed_nodes.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if err := store.UpsertNode(node); err != nil {
		t.Fatalf("UpsertNode() error = %v", err)
	}
	tester := NewNodeTester(nil, WithTesterConcurrency(2))
	tester.retryDelay = time.Millisecond
	tester.probeOverride = func(context.Context, ManagedNode, string, time.Duration) TestResult {
		return TestResult{LatencyMs: 1}
	}
	svc := NewService(store, tester, mgr)
	svc.refreshSourceTimeout = 20 * time.Millisecond
	targets := []sourceRefreshTarget{{
		Key:          "tag:mixed",
		TagPrefix:    "mixed",
		URLs:         []string{server.URL},
		LocalNodeIDs: []string{node.ID},
		LocalFormats: []string{"uri_list"},
	}}
	jobID := "mixed-refresh"
	now := time.Now()
	svc.refreshJobs[jobID] = &SourceRefreshJob{
		ID:        jobID,
		Status:    SourceRefreshJobRunning,
		StartedAt: now,
		UpdatedAt: now,
		Groups: []SourceRefreshGroup{{
			Key:       "tag:mixed",
			TagPrefix: "mixed",
			URLs: []SourceRefreshURL{
				{URL: server.URL, Kind: "url", Status: "waiting", UpdatedAt: now},
				{Kind: "content", Status: "waiting", Nodes: 1, Total: 1, UpdatedAt: now},
			},
		}},
	}
	snapshot, err := svc.captureSourceRefreshSnapshot()
	if err != nil {
		t.Fatalf("captureSourceRefreshSnapshot() error = %v", err)
	}
	svc.runRefreshJob(context.Background(), jobID, targets, snapshot)

	job, _ := svc.GetRefreshJob(jobID)
	if job.Status != SourceRefreshJobFinished || job.Phase != "partial" || !job.Applied || job.Successful != 1 || job.Failed != 1 {
		t.Fatalf("job = %#v, want applied partial completion", job)
	}
	if job.Passed != 1 || job.PoolCount != 1 {
		t.Fatalf("passed=%d pool=%d, want 1", job.Passed, job.PoolCount)
	}
	for _, current := range store.ListNodes() {
		if current.ImportMode == "refresh_stage" || current.State == StateTesting {
			t.Fatalf("transient node remained: %#v", current)
		}
	}
}

func TestRefreshApplyFailureRestoresParentSnapshot(t *testing.T) {
	const newURI = "trojan://pass@example.com:443#New"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(newURI + "\n"))
	}))
	defer server.Close()

	for _, tc := range []struct {
		name      string
		createErr error
		deleteErr error
		reloadErr error
	}{
		{name: "create", createErr: errors.New("create failed")},
		{name: "delete", deleteErr: errors.New("delete failed")},
		{name: "reload", reloadErr: errors.New("reload failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr := &batchNodeManagerStub{nextPort: 25000, createErr: tc.createErr, deleteErr: tc.deleteErr, reloadErr: tc.reloadErr}
			store, err := NewStore(filepath.Join(t.TempDir(), "managed_nodes.json"))
			if err != nil {
				t.Fatalf("NewStore() error = %v", err)
			}
			tester := NewNodeTester(nil, WithTesterConcurrency(2))
			tester.retryDelay = time.Millisecond
			tester.probeOverride = func(context.Context, ManagedNode, string, time.Duration) TestResult {
				return TestResult{LatencyMs: 1}
			}
			old := ManagedNode{ID: "old", Name: "sub-Old", URI: "trojan://old", State: StateInPool, InPool: true, Enabled: true, Port: 24000, ImportMode: "url", ImportSource: server.URL, TagPrefix: "sub"}
			mgr.configNodes = []config.NodeConfig{old.ToConfigNode()}
			if err := store.UpsertNode(old); err != nil {
				t.Fatalf("UpsertNode() error = %v", err)
			}
			svc := NewService(store, tester, mgr)
			jobID, err := svc.StartRefreshSources("tag:sub")
			if err != nil {
				t.Fatalf("StartRefreshSources() error = %v", err)
			}
			job := waitRefreshJobTerminal(t, svc, jobID)
			if !job.Protected || job.Applied || job.Phase != "protected" || mgr.restoreCount != 1 {
				t.Fatalf("job=%#v restoreCount=%d", job, mgr.restoreCount)
			}
			restored, ok := store.GetNode("old")
			if !ok || !restored.InPool || restored.Port != 24000 || len(store.ListNodes()) != 1 {
				t.Fatalf("restored=%#v found=%v nodes=%#v", restored, ok, store.ListNodes())
			}
		})
	}
}

func TestRefreshAllFailedStagedNodesKeepOldSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("trojan://failed@example.com:443#Failed\n"))
	}))
	defer server.Close()
	mgr := &batchNodeManagerStub{}
	store, err := NewStore(filepath.Join(t.TempDir(), "managed_nodes.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	tester := NewNodeTester(nil, WithTesterConcurrency(2))
	tester.retryDelay = time.Millisecond
	tester.probeOverride = func(context.Context, ManagedNode, string, time.Duration) TestResult {
		return TestResult{Error: errors.New("probe failed")}
	}
	old := ManagedNode{ID: "old", Name: "sub-Old", URI: "trojan://old", State: StateInPool, InPool: true, Enabled: true, Port: 24000, ImportMode: "url", ImportSource: server.URL, TagPrefix: "sub"}
	mgr.configNodes = []config.NodeConfig{old.ToConfigNode()}
	if err := store.UpsertNode(old); err != nil {
		t.Fatalf("UpsertNode() error = %v", err)
	}
	svc := NewService(store, tester, mgr)
	jobID, err := svc.StartRefreshSources("tag:sub")
	if err != nil {
		t.Fatalf("StartRefreshSources() error = %v", err)
	}
	job := waitRefreshJobTerminal(t, svc, jobID)
	restored, ok := store.GetNode("old")
	if !job.Protected || job.Applied || !ok || !restored.InPool || len(store.ListNodes()) != 1 {
		t.Fatalf("job=%#v restored=%#v found=%v nodes=%#v", job, restored, ok, store.ListNodes())
	}
}

func TestRefreshProtectionRestoreFailureIsReportedAsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("trojan://pass@example.com:443#New\n"))
	}))
	defer server.Close()
	mgr := &batchNodeManagerStub{createErr: errors.New("create failed"), restoreErr: errors.New("restore failed")}
	store, err := NewStore(filepath.Join(t.TempDir(), "managed_nodes.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	tester := NewNodeTester(nil, WithTesterConcurrency(2))
	tester.retryDelay = time.Millisecond
	tester.probeOverride = func(context.Context, ManagedNode, string, time.Duration) TestResult {
		return TestResult{LatencyMs: 1}
	}
	old := ManagedNode{ID: "old", Name: "sub-Old", URI: "trojan://old", State: StateInPool, InPool: true, Enabled: true, Port: 24000, ImportMode: "url", ImportSource: server.URL, TagPrefix: "sub"}
	mgr.configNodes = []config.NodeConfig{old.ToConfigNode()}
	if err := store.UpsertNode(old); err != nil {
		t.Fatalf("UpsertNode() error = %v", err)
	}
	svc := NewService(store, tester, mgr)
	jobID, err := svc.StartRefreshSources("tag:sub")
	if err != nil {
		t.Fatalf("StartRefreshSources() error = %v", err)
	}
	job := waitRefreshJobTerminal(t, svc, jobID)
	if job.Status != SourceRefreshJobFailed || job.Phase != "failed" || job.Applied || !strings.Contains(job.Error, "恢复检测前节点池失败") {
		t.Fatalf("job = %#v, want rollback failure", job)
	}
	if mgr.restoreCount != 1 {
		t.Fatalf("restoreCount = %d, want 1", mgr.restoreCount)
	}
}

func TestWaitTestJobTimeoutCancelsChild(t *testing.T) {
	mgr := &batchNodeManagerStub{}
	store, err := NewStore(filepath.Join(t.TempDir(), "managed_nodes.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	tester := NewNodeTester(nil, WithTesterConcurrency(1))
	tester.probeOverride = func(ctx context.Context, _ ManagedNode, _ string, _ time.Duration) TestResult {
		<-ctx.Done()
		return TestResult{Error: ctx.Err()}
	}
	node := ManagedNode{ID: "node", Name: "node", URI: "trojan://node", State: StateInPool, InPool: true, Enabled: true, Port: 24000, TagPrefix: "local", ImportMode: "content"}
	mgr.configNodes = []config.NodeConfig{node.ToConfigNode()}
	if err := store.UpsertNode(node); err != nil {
		t.Fatalf("UpsertNode() error = %v", err)
	}
	svc := NewService(store, tester, mgr)
	svc.refreshJobMaxWait = 20 * time.Millisecond
	jobID, err := svc.StartBatchTest(BatchTestRequest{NodeIDs: []string{node.ID}, Retest: true, PromotePassed: true, AutoReload: true})
	if err != nil {
		t.Fatalf("StartBatchTest() error = %v", err)
	}
	job, waitErr := svc.waitTestJob(context.Background(), jobID, nil)
	if waitErr == nil || !strings.Contains(waitErr.Error(), "超时") || job.Status != TestJobCanceled {
		t.Fatalf("job=%#v waitErr=%v", job, waitErr)
	}
	restored, _ := store.GetNode(node.ID)
	if !restored.InPool || restored.State != StateInPool {
		t.Fatalf("restored node = %#v", restored)
	}
}

func TestStoreMutationRollsBackWhenSaveFails(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "managed_nodes.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	old := ManagedNode{ID: "old", URI: "trojan://old", State: StateFailed}
	if err := store.UpsertNode(old); err != nil {
		t.Fatalf("UpsertNode() error = %v", err)
	}
	job := ImportJob{ID: "job", Status: ImportStatusParsed}
	if err := store.UpsertJob(job); err != nil {
		t.Fatalf("UpsertJob() error = %v", err)
	}
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	store.path = filepath.Join(blocker, "managed_nodes.json")
	if err := store.UpsertNode(ManagedNode{ID: "new", URI: "trojan://new"}); err == nil {
		t.Fatal("UpsertNode() unexpectedly succeeded")
	}
	if _, ok := store.GetNode("new"); ok {
		t.Fatal("failed upsert remained in memory")
	}
	if err := store.DeleteNode("old"); err == nil {
		t.Fatal("DeleteNode() unexpectedly succeeded")
	}
	if _, ok := store.GetNode("old"); !ok {
		t.Fatal("failed delete removed node from memory")
	}
	if _, err := store.UpdateNodeState("old", StatePassed, ""); err == nil {
		t.Fatal("UpdateNodeState() unexpectedly succeeded")
	}
	if current, _ := store.GetNode("old"); current.State != StateFailed {
		t.Fatalf("failed state update remained in memory: %#v", current)
	}
	if _, err := store.MarkInPool("old", 24000); err == nil {
		t.Fatal("MarkInPool() unexpectedly succeeded")
	}
	if current, _ := store.GetNode("old"); current.InPool || current.Port != 0 {
		t.Fatalf("failed pool update remained in memory: %#v", current)
	}
	if err := store.UpdateJob("job", func(current *ImportJob) { current.Status = ImportStatusRunning }); err == nil {
		t.Fatal("UpdateJob() unexpectedly succeeded")
	}
	if current, _ := store.GetJob("job"); current.Status != ImportStatusParsed {
		t.Fatalf("failed job update remained in memory: %#v", current)
	}
	if err := store.UpsertJob(ImportJob{ID: "new-job"}); err == nil {
		t.Fatal("UpsertJob() unexpectedly succeeded")
	}
	if _, ok := store.GetJob("new-job"); ok {
		t.Fatal("failed job upsert remained in memory")
	}
}
