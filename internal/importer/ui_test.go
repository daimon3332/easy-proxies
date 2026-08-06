package importer

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

func newUIService(t *testing.T, nodes []ManagedNode) *Service {
	t.Helper()
	store, err := newTestStore(t, filepath.Join(t.TempDir(), "managed_nodes.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if err := store.UpsertNodes(nodes); err != nil {
		t.Fatalf("UpsertNodes() error = %v", err)
	}
	return NewService(store, nil, nil)
}

func TestUISummaryAndListUINodes(t *testing.T) {
	service := newUIService(t, []ManagedNode{
		{ID: "candidate-jp", URI: "vless://secret@candidate.example", Name: "candidate", TagPrefix: "alpha", CountryCode: "JP", LatencyMs: 120, State: StatePassed},
		{ID: "pool-us", URI: "trojan://secret@pool.example", Name: "pool", TagPrefix: "beta", CountryCode: "US", LatencyMs: 320, Port: 24001, Order: 1, InPool: true, State: StateInPool},
		{ID: "failed-jp", URI: "ss://secret@failed.example", Name: "failed", TagPrefix: "alpha", CountryCode: "JP", State: StateFailed, LastError: "timeout"},
		{ID: "parsed", URI: "vmess://secret@parsed.example", Name: "parsed", State: StateParsed},
	})

	summary, err := service.UISummary()
	if err != nil {
		t.Fatalf("UISummary() error = %v", err)
	}
	if summary.Total != 4 || summary.Passed != 1 || summary.InPool != 1 || summary.Failed != 1 || summary.Parsed != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}

	result, err := service.ListUINodes(UINodeListQuery{Scope: "candidate", Page: 1, PageSize: 100, Sort: "latency", Order: "asc"})
	if err != nil {
		t.Fatalf("ListUINodes() error = %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 || result.Items[0].ID != "candidate-jp" {
		t.Fatalf("unexpected candidate page: %#v", result)
	}
	if !reflect.DeepEqual(result.Countries, []string{"JP"}) || !reflect.DeepEqual(result.Tags, []string{"alpha"}) {
		t.Fatalf("unexpected filters: countries=%v tags=%v", result.Countries, result.Tags)
	}

	encoded, err := json.Marshal(result.Items[0])
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(encoded) == "" || containsAny(string(encoded), "vless://", "import_source", "uri") {
		t.Fatalf("compact UI item leaked private node fields: %s", encoded)
	}
}

func TestUINodeListTruncatesLongDisplayFields(t *testing.T) {
	service := newUIService(t, []ManagedNode{{
		ID:        "failed",
		Name:      string(make([]byte, 200)),
		LastError: string(make([]byte, 2_000)),
		State:     StateFailed,
	}})
	result, err := service.ListUINodes(UINodeListQuery{Scope: "failed", Page: 1, PageSize: 100, Sort: "name"})
	if err != nil {
		t.Fatalf("ListUINodes() error = %v", err)
	}
	if len(result.Items) != 1 || len(result.Items[0].Name) > 163 || len(result.Items[0].LastError) > 515 {
		t.Fatalf("long display fields were not truncated: %#v", result.Items)
	}
}

func TestListUINodesPaginationAndFilters(t *testing.T) {
	service := newUIService(t, []ManagedNode{
		{ID: "a", Name: "alpha", TagPrefix: "one", CountryCode: "JP", LatencyMs: 30, State: StatePassed},
		{ID: "b", Name: "bravo", TagPrefix: "two", CountryCode: "US", LatencyMs: 20, State: StatePassed},
		{ID: "c", Name: "charlie", TagPrefix: "one", CountryCode: "JP", LatencyMs: 10, State: StatePassed},
	})

	result, err := service.ListUINodes(UINodeListQuery{Scope: "candidate", Country: "JP", Page: 2, PageSize: 1, Sort: "latency", Order: "asc"})
	if err != nil {
		t.Fatalf("ListUINodes() error = %v", err)
	}
	if result.Total != 2 || len(result.Items) != 1 || result.Items[0].ID != "a" {
		t.Fatalf("unexpected filtered page: %#v", result)
	}
	if _, err := service.ListUINodes(UINodeListQuery{Scope: "candidate", Sort: "unknown"}); err == nil {
		t.Fatal("ListUINodes() accepted an unsupported sort field")
	}
}

func TestListUINodesKeepsMissingSortValuesLast(t *testing.T) {
	service := newUIService(t, []ManagedNode{
		{ID: "none", Name: "none", State: StatePassed},
		{ID: "fast", Name: "fast", LatencyMs: 20, State: StatePassed},
		{ID: "slow", Name: "slow", LatencyMs: 100, State: StatePassed},
	})

	for _, order := range []string{"asc", "desc"} {
		result, err := service.ListUINodes(UINodeListQuery{Scope: "candidate", Page: 1, PageSize: 100, Sort: "latency", Order: order})
		if err != nil {
			t.Fatalf("ListUINodes() error = %v", err)
		}
		if len(result.Items) != 3 || result.Items[2].ID != "none" {
			t.Fatalf("missing latency was not last for %s: %#v", order, result.Items)
		}
	}
}

func TestListUINodesSQLSearchAndLatencyFilters(t *testing.T) {
	service := newUIService(t, []ManagedNode{
		{ID: "fast-jp", URI: "trojan://fast", Name: "Tokyo Fast", TagPrefix: "alpha", CountryCode: "JP", CountryName: "Japan", LatencyMs: 200, State: StatePassed},
		{ID: "slow-jp", URI: "trojan://slow", Name: "Tokyo Slow", TagPrefix: "alpha", CountryCode: "JP", CountryName: "Japan", LatencyMs: 1800, State: StatePassed},
		{ID: "fast-us", URI: "trojan://us", Name: "Seattle Fast", TagPrefix: "beta", CountryCode: "US", LatencyMs: 300, State: StatePassed},
	})
	result, err := service.ListUINodes(UINodeListQuery{
		Scope: "candidate", Country: "JP", Tag: "alpha", Query: "tokyo", Latency: "0-500", Sort: "name", Order: "asc", Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Items) != 1 || result.Items[0].ID != "fast-jp" {
		t.Fatalf("unexpected SQL-filtered result: %#v", result)
	}
	if !reflect.DeepEqual(result.Countries, []string{"JP", "US"}) || !reflect.DeepEqual(result.Tags, []string{"alpha", "beta"}) {
		t.Fatalf("scope filter options changed: countries=%v tags=%v", result.Countries, result.Tags)
	}
}

func BenchmarkListUINodesPage5000(b *testing.B) {
	store, err := NewStore(filepath.Join(b.TempDir(), "managed_nodes.json"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	nodes := make([]ManagedNode, 5000)
	for i := range nodes {
		nodes[i] = ManagedNode{
			ID: fmt.Sprintf("node-%05d", i), URI: fmt.Sprintf("trojan://node-%d", i), Name: fmt.Sprintf("Node %05d", i),
			TagPrefix: fmt.Sprintf("tag-%d", i%10), CountryCode: []string{"JP", "US", "SG"}[i%3], LatencyMs: int64(i%2000 + 1), State: StatePassed,
		}
	}
	if err := store.UpsertNodes(nodes); err != nil {
		b.Fatal(err)
	}
	service := NewService(store, nil, nil)
	query := UINodeListQuery{Scope: "candidate", Page: 1, PageSize: 100, Sort: "latency", Order: "asc"}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := service.ListUINodes(query); err != nil {
			b.Fatal(err)
		}
	}
}

func TestUIPortPreviewAndBatchScopes(t *testing.T) {
	service := newUIService(t, []ManagedNode{
		{ID: "candidate", Name: "candidate", State: StatePassed},
		{ID: "pool", Name: "pool", Port: 24001, Order: 1, InPool: true, State: StateInPool},
		{ID: "failed", Name: "failed", State: StateFailed},
	})

	ports, err := service.UIPortPreview(80)
	if err != nil {
		t.Fatalf("UIPortPreview() error = %v", err)
	}
	if len(ports) != 1 || ports[0].Port != 24001 || ports[0].Name != "pool" {
		t.Fatalf("unexpected port preview: %#v", ports)
	}

	ids, err := service.resolveBatchTestNodeIDs(BatchTestRequest{Scopes: []string{"candidate", "failed"}})
	if err != nil {
		t.Fatalf("resolveBatchTestNodeIDs() error = %v", err)
	}
	if !reflect.DeepEqual(ids, []string{"candidate", "failed"}) {
		t.Fatalf("unexpected scoped ids: %v", ids)
	}
}

func containsAny(value string, values ...string) bool {
	for _, candidate := range values {
		if len(candidate) > 0 && contains(value, candidate) {
			return true
		}
	}
	return false
}

func contains(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
