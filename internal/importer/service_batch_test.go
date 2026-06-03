package importer

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

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
