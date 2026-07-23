package importer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"easy_proxies/internal/config"
)

type batchNodeManagerStub struct {
	createdBatches [][]config.NodeConfig
	deletedBatches [][]string
	reloadCount    int
	nextPort       uint16
	configNodes    []config.NodeConfig
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
	out := make([]config.NodeConfig, len(m.configNodes))
	copy(out, m.configNodes)
	return out, nil
}

func (m *batchNodeManagerStub) DeleteNode(ctx context.Context, name string) error {
	return m.DeleteNodes(ctx, []string{name})
}

func (m *batchNodeManagerStub) DeleteNodes(ctx context.Context, names []string) error {
	_ = ctx
	m.deletedBatches = append(m.deletedBatches, append([]string(nil), names...))
	return nil
}

func (m *batchNodeManagerStub) TriggerReload(ctx context.Context) error {
	_ = ctx
	m.reloadCount++
	return nil
}

func newBatchServiceForTest(t *testing.T, mgr *batchNodeManagerStub) (*Service, *Store) {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "managed_nodes.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	return NewService(store, nil, mgr), store
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
	if job.Error != "全部订阅链接都未拉取到节点" {
		t.Fatalf("job.Error = %q, want default failure message", job.Error)
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
	if contentGroup.Format != "base64" || contentGroup.Candidate != 1 || contentGroup.TagPrefix != "local" {
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
