package importer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func newTestStore(t testing.TB, path string) (*Store, error) {
	t.Helper()
	store, err := NewStore(path)
	if err == nil {
		t.Cleanup(func() { _ = store.Close() })
	}
	return store, err
}

func TestStoreProgressUpdateDoesNotPersistFullSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed_nodes.json")
	store, err := newTestStore(t, path)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	job := ImportJob{
		ID:        "progress-job",
		Status:    ImportStatusRunning,
		Total:     2,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.UpsertJob(job); err != nil {
		t.Fatalf("UpsertJob() error = %v", err)
	}
	if err := store.UpdateJobProgress(job.ID, func(current *ImportJob) {
		current.ProbeRound = 1
		current.ProbeRoundDone = 1
		current.ProbeRoundTotal = 2
		current.ProbePending = 1
		current.Detail = "progress update"
		current.UpdatedAt = time.Now()
	}); err != nil {
		t.Fatalf("UpdateJobProgress() error = %v", err)
	}

	current, ok := store.GetJob(job.ID)
	if !ok || current.ProbeRoundDone != 1 || current.ProbePending != 1 {
		t.Fatalf("volatile progress was not visible in memory: %#v, found=%v", current, ok)
	}
	loaded, err := newTestStore(t, path)
	if err != nil {
		t.Fatalf("NewStore(load) error = %v", err)
	}
	persisted, ok := loaded.GetJob(job.ID)
	if !ok {
		t.Fatal("persisted job not found")
	}
	if persisted.ProbeRound != 0 || persisted.ProbeRoundDone != 0 || persisted.ProbePending != 0 || persisted.Detail != "" {
		t.Fatalf("volatile progress was persisted: %#v", persisted)
	}
}

func TestStoreRecoversAndBoundsPersistedJobs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed_nodes.json")
	now := time.Now()
	jobs := make(map[string]ImportJob, maxStoredJobs+3)
	for i := 0; i < maxStoredJobs+3; i++ {
		id := randomHex(12)
		status := ImportStatusCompleted
		createdAt := now.Add(-time.Duration(i) * time.Minute)
		if i == 0 {
			status = ImportStatusRunning
		}
		jobs[id] = ImportJob{ID: id, Status: status, CreatedAt: createdAt, UpdatedAt: createdAt}
	}
	payload, err := json.Marshal(storeFile{Version: storeVersion, Nodes: map[string]ManagedNode{}, Jobs: jobs})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(path, payload, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	store, err := newTestStore(t, path)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	loaded, err := newTestStore(t, path)
	if err != nil {
		t.Fatalf("NewStore(load) error = %v", err)
	}
	if len(loaded.jobs) > maxStoredJobs {
		t.Fatalf("persisted jobs are not bounded: got=%d max=%d", len(loaded.jobs), maxStoredJobs)
	}
	var running int
	for _, job := range loaded.jobs {
		if job.Status == ImportStatusRunning {
			running++
		}
	}
	if running != 0 {
		t.Fatalf("stale running jobs remain after recovery: %d", running)
	}
	if _, ok := store.GetJob("missing"); ok {
		t.Fatal("unexpected missing job")
	}
}

func TestStoreMigratesLegacyJSONOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed_nodes.json")
	legacyNode := ManagedNode{ID: "legacy", URI: "trojan://legacy", State: StatePassed}
	payload, err := json.Marshal(storeFile{
		Version: storeVersion,
		Nodes:   map[string]ManagedNode{legacyNode.ID: legacyNode},
		Jobs:    map[string]ImportJob{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0600); err != nil {
		t.Fatal(err)
	}

	store, err := newTestStore(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.GetNode(legacyNode.ID); !ok {
		t.Fatal("legacy node was not migrated")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("legacy JSON was not preserved: %v", err)
	}

	if err := os.WriteFile(path, []byte(`{"version":1,"nodes":{},"jobs":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := newTestStore(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.GetNode(legacyNode.ID); !ok {
		t.Fatal("legacy JSON was imported more than once")
	}
}

func TestStoreMigratesV1SQLiteUIColumns(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "managed_nodes.json")
	db, err := sql.Open("sqlite", filepath.Join(dir, "managed_nodes.db"))
	if err != nil {
		t.Fatal(err)
	}
	node := ManagedNode{ID: "legacy-db", URI: "trojan://legacy-db", Name: "Legacy DB", TagPrefix: "old", CountryCode: "JP", LatencyMs: 123, State: StatePassed}
	payload, err := json.Marshal(node)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE managed_nodes (id TEXT PRIMARY KEY, state TEXT NOT NULL, in_pool INTEGER NOT NULL, order_index INTEGER NOT NULL, tag_prefix TEXT NOT NULL, import_id TEXT NOT NULL, updated_at INTEGER NOT NULL, payload BLOB NOT NULL)`,
		`CREATE TABLE store_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT INTO store_meta(key, value) VALUES('legacy_json_imported', 'test')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO managed_nodes(id, state, in_pool, order_index, tag_prefix, import_id, updated_at, payload) VALUES(?, ?, 0, 0, ?, '', 0, ?)`, node.ID, node.State, node.TagPrefix, payload); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := newTestStore(t, legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	result, total, countries, tags, err := store.queryUINodes(UINodeListQuery{Scope: "candidate", Sort: "latency", Order: "asc", Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(result) != 1 || result[0].Name != node.Name || result[0].LatencyMs != node.LatencyMs {
		t.Fatalf("v1 UI columns were not backfilled: total=%d nodes=%#v", total, result)
	}
	if !reflect.DeepEqual(countries, []string{"JP"}) || !reflect.DeepEqual(tags, []string{"old"}) {
		t.Fatalf("v1 filter columns were not backfilled: countries=%v tags=%v", countries, tags)
	}
}

func TestStoreRetentionKeepsParsedJobsAheadOfCompletedHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed_nodes.json")
	now := time.Now()
	jobs := make(map[string]ImportJob, maxStoredJobs+1)
	for i := 0; i < maxStoredJobs; i++ {
		id := fmt.Sprintf("completed-%03d", i)
		jobs[id] = ImportJob{ID: id, Status: ImportStatusCompleted, CreatedAt: now, UpdatedAt: now}
	}
	jobs["pending-import"] = ImportJob{
		ID:        "pending-import",
		Status:    ImportStatusParsed,
		CreatedAt: now.Add(-time.Hour),
		UpdatedAt: now.Add(-time.Hour),
	}
	payload, err := json.Marshal(storeFile{Version: storeVersion, Nodes: map[string]ManagedNode{}, Jobs: jobs})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0644); err != nil {
		t.Fatal(err)
	}
	store, err := newTestStore(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.GetJob("pending-import"); !ok {
		t.Fatal("parsed import job was pruned before completed history")
	}
}

func TestStoreConcurrentUpdatesPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed_nodes.json")
	store, err := newTestStore(t, path)
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

	loaded, err := newTestStore(t, path)
	if err != nil {
		t.Fatalf("NewStore(load) error = %v", err)
	}
	if len(loaded.ListNodes()) == 0 {
		t.Fatal("loaded store has no nodes")
	}
}

func TestStoreApplyNodeChangesRollsBackMemoryOnDatabaseFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed_nodes.json")
	store, err := newTestStore(t, path)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	original := ManagedNode{
		ID:           "shared",
		URI:          "trojan://shared",
		State:        StatePassed,
		TagPrefix:    "A",
		ImportMode:   "url",
		ImportSource: "https://example.test/a",
	}
	if err := store.UpsertNode(original); err != nil {
		t.Fatalf("UpsertNode() error = %v", err)
	}
	before, _ := store.GetNode(original.ID)
	updated := before
	updated.TagPrefix = "B"
	updated.ImportSource = "https://example.test/b"
	updated.SourceRefs = []NodeSourceRef{sourceRefFromNode(updated)}
	if err := store.db.close(); err != nil {
		t.Fatalf("close state DB: %v", err)
	}
	if err := store.ApplyNodeChanges([]ManagedNode{updated}, nil); err == nil {
		t.Fatal("ApplyNodeChanges() succeeded with a closed database")
	}
	after, ok := store.GetNode(original.ID)
	if !ok || !reflect.DeepEqual(after, before) {
		t.Fatalf("memory state was not rolled back: before=%#v after=%#v found=%v", before, after, ok)
	}
}
