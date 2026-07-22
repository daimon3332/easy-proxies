package importer

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStoreConcurrentUpdatesPersistValidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed_nodes.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	const count = 24
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			node := ManagedNode{
				ID:        randomHex(12),
				URI:       "vmess://node",
				Name:      "node",
				State:     StateParsed,
				Enabled:   true,
				CreatedAt: time.Now(),
			}
			if err := store.UpsertNode(node); err != nil {
				t.Errorf("UpsertNode() error = %v", err)
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			job := ImportJob{
				ID:        randomHex(12),
				Status:    ImportStatusRunning,
				Total:     i + 1,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}
			if err := store.UpsertJob(job); err != nil {
				t.Errorf("UpsertJob() error = %v", err)
			}
		}(i)
	}
	wg.Wait()

	loaded, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore(load) error = %v", err)
	}
	if len(loaded.ListNodes()) == 0 {
		t.Fatal("loaded store has no nodes")
	}
}
