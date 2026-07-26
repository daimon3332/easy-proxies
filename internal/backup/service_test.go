package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"easy_proxies/internal/config"
	"easy_proxies/internal/importer"
)

type runtimeStub struct {
	cfg *config.Config
	err error
}

func (r *runtimeStub) ApplyRestoredConfig(_ context.Context, cfg *config.Config) error {
	if r.err != nil {
		return r.err
	}
	r.cfg = cfg
	return nil
}

func (r *runtimeStub) ListConfigNodes(context.Context) ([]config.NodeConfig, error) {
	if r.cfg == nil {
		return nil, nil
	}
	return append([]config.NodeConfig(nil), r.cfg.Nodes...), nil
}

func (r *runtimeStub) CurrentConfig() *config.Config {
	if r.cfg == nil {
		return nil
	}
	cloned := *r.cfg
	cloned.Nodes = append([]config.NodeConfig(nil), r.cfg.Nodes...)
	cloned.Subscriptions = append([]string(nil), r.cfg.Subscriptions...)
	cloned.SetFilePath(r.cfg.FilePath())
	return &cloned
}

type subscriptionStub struct {
	cfg *config.Config
}

func (s *subscriptionStub) ApplyRestoredConfig(cfg *config.Config) { s.cfg = cfg }

func testService(t *testing.T) (*Service, *config.Config, *importer.Store, *runtimeStub, *subscriptionStub) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	content := []byte("mode: multi-port\nmanagement:\n  enabled: true\n  listen: 127.0.0.1:9091\nsubscriptions:\n  - https://example.com/sub\n")
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := importer.NewStore(filepath.Join(dir, "managed_nodes.json"))
	if err != nil {
		t.Fatal(err)
	}
	node := importer.ManagedNode{ID: "node-1", Name: "node", URI: "ss://YWVzLTI1Ni1nY206cGFzcw==@example.com:8388#node", State: importer.StateInPool, InPool: true, Enabled: true, Port: 24000}
	if err := store.UpsertNode(node); err != nil {
		t.Fatal(err)
	}
	cfg.Nodes = []config.NodeConfig{node.ToConfigNode()}
	runtime := &runtimeStub{cfg: cfg}
	subscription := &subscriptionStub{}
	return NewService(cfg, store, runtime, subscription, nil), cfg, store, runtime, subscription
}

func TestBackupRestoreRoundTrip(t *testing.T) {
	service, cfg, store, runtime, subscription := testService(t)
	cfg.Nodes = append(cfg.Nodes, config.NodeConfig{Name: "config-only", URI: "trojan://pass@example.net:443#config-only"})
	archive, _, err := service.Create()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Subscriptions = nil
	if err := store.ReplaceSnapshot(importer.StoreSnapshot{Version: 1, Nodes: map[string]importer.ManagedNode{}}); err != nil {
		t.Fatal(err)
	}
	result, err := service.Restore(context.Background(), archive)
	if err != nil {
		t.Fatal(err)
	}
	if result.NodeCount != 1 || result.PoolNodeCount != 1 || result.Subscriptions != 1 {
		t.Fatalf("unexpected restore result: %+v", result)
	}
	if len(store.ListNodes()) != 1 || runtime.cfg == nil || len(runtime.cfg.Nodes) != 2 {
		t.Fatal("node state was not restored")
	}
	if subscription.cfg == nil || len(subscription.cfg.Subscriptions) != 1 {
		t.Fatal("subscription config was not applied")
	}
}

func TestValidateManifestCounts(t *testing.T) {
	cfg := &config.Config{Subscriptions: []string{"https://example.com/sub"}}
	snapshot := importer.StoreSnapshot{Version: 1, Nodes: map[string]importer.ManagedNode{
		"node-1": {ID: "node-1", State: importer.StateInPool, InPool: true},
	}}
	manifest := Manifest{NodeCount: 1, PoolNodeCount: 1, Subscriptions: 1}
	if err := validateManifestCounts(manifest, cfg, snapshot); err != nil {
		t.Fatal(err)
	}
	manifest.NodeCount = 2
	if err := validateManifestCounts(manifest, cfg, snapshot); err == nil {
		t.Fatal("validateManifestCounts() expected mismatch error")
	}
}

func TestCreateUsesCurrentRuntimeConfig(t *testing.T) {
	service, cfg, _, runtime, _ := testService(t)
	if err := service.SetWebDAVConfig(config.WebDAVConfig{Address: "https://dav.example.com", Username: "user", Password: "pass", Folder: "/easy_proxies"}); err != nil {
		t.Fatal(err)
	}
	current := *cfg
	current.Subscriptions = []string{"https://example.com/one", "https://example.com/two"}
	current.SetFilePath(cfg.FilePath())
	runtime.cfg = &current
	archive, _, err := service.Create()
	if err != nil {
		t.Fatal(err)
	}
	manifest, configData, _, err := parseArchive(archive)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Subscriptions != 2 {
		t.Fatalf("manifest subscriptions = %d, want 2", manifest.Subscriptions)
	}
	restored, err := config.DecodeBackupYAML(configData, cfg.FilePath())
	if err != nil {
		t.Fatal(err)
	}
	if restored.WebDAV.Password != "pass" || len(restored.Subscriptions) != 2 {
		t.Fatalf("backup config is stale: %+v", restored)
	}
}

func TestRestoreRejectsZipTraversal(t *testing.T) {
	service, _, _, _, _ := testService(t)
	var data bytes.Buffer
	zw := zip.NewWriter(&data)
	w, err := zw.Create("../config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("mode: multi-port"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Restore(context.Background(), data.Bytes()); err == nil {
		t.Fatal("Restore() expected traversal error")
	}
}

func TestWebDAVBackupLifecycle(t *testing.T) {
	service, _, _, _, _ := testService(t)
	var mu sync.Mutex
	files := make(map[string][]byte)
	directories := map[string]bool{"/": true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath := path.Clean(r.URL.Path)
		username, password, ok := r.BasicAuth()
		if !ok || username != "user" || password != "pass" {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodOptions:
			w.Header().Set("DAV", "1")
			w.WriteHeader(http.StatusOK)
		case http.MethodPut:
			data, _ := io.ReadAll(r.Body)
			mu.Lock()
			files[r.URL.Path] = data
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		case "MKCOL":
			mu.Lock()
			directories[requestPath] = true
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		case "MOVE":
			destination, _ := url.Parse(r.Header.Get("Destination"))
			mu.Lock()
			files[destination.Path] = files[r.URL.Path]
			delete(files, r.URL.Path)
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		case "PROPFIND":
			mu.Lock()
			defer mu.Unlock()
			if !directories[requestPath] {
				if _, ok := files[requestPath]; !ok {
					w.WriteHeader(http.StatusNotFound)
					return
				}
			}
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(207)
			_, _ = fmt.Fprintf(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>%s/</d:href><d:propstat><d:prop><d:displayname>%s</d:displayname><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`, strings.TrimSuffix(requestPath, "/"), path.Base(requestPath))
			for name, data := range files {
				if path.Dir(name) == requestPath {
					_, _ = fmt.Fprintf(w, `<d:response><d:href>%s</d:href><d:propstat><d:prop><d:displayname>%s</d:displayname><d:resourcetype/><d:getcontentlength>%d</d:getcontentlength><d:getlastmodified>%s</d:getlastmodified></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`, name, path.Base(name), len(data), time.Now().UTC().Format(http.TimeFormat))
				}
			}
			_, _ = io.WriteString(w, `</d:multistatus>`)
		case http.MethodGet:
			mu.Lock()
			data, ok := files[r.URL.Path]
			mu.Unlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(data)
		case http.MethodDelete:
			mu.Lock()
			delete(files, r.URL.Path)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	if err := service.SetWebDAVConfig(config.WebDAVConfig{Address: server.URL, Username: "user", Password: "pass", Folder: "/easy_proxies"}); err != nil {
		t.Fatal(err)
	}
	if err := service.TestWebDAV(); err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateWebDAV()
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	for name := range files {
		if path.Dir(name) != "/easy_proxies" {
			mu.Unlock()
			t.Fatalf("WebDAV file escaped configured folder: %s", name)
		}
	}
	mu.Unlock()
	listed, err := service.ListWebDAV()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Name != created.Name {
		t.Fatalf("unexpected WebDAV list: %+v", listed)
	}
	data, err := service.DownloadWebDAV(created.Name)
	if err != nil || len(data) == 0 {
		t.Fatalf("DownloadWebDAV() data=%d err=%v", len(data), err)
	}
	if err := service.DeleteWebDAV(created.Name); err != nil {
		t.Fatal(err)
	}
	listed, err = service.ListWebDAV()
	if err != nil || len(listed) != 0 {
		t.Fatalf("WebDAV file was not deleted: %+v, %v", listed, err)
	}
}

func TestNormalizeWebDAVFolder(t *testing.T) {
	for input, want := range map[string]string{"": "/easy_proxies", "backups": "/backups", `\team\backups\`: "/team/backups"} {
		got, err := normalizeWebDAVFolder(input)
		if err != nil || got != want {
			t.Fatalf("normalizeWebDAVFolder(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := normalizeWebDAVFolder("/easy_proxies/../other"); err == nil {
		t.Fatal("normalizeWebDAVFolder() should reject parent traversal")
	}
}

func TestSetWebDAVConfigNormalizesFolder(t *testing.T) {
	service, _, _, _, _ := testService(t)
	if err := service.SetWebDAVConfig(config.WebDAVConfig{Address: "https://dav.example.com", Folder: "backups"}); err != nil {
		t.Fatal(err)
	}
	if got := service.WebDAVConfig().Folder; got != "/backups" {
		t.Fatalf("WebDAV folder = %q, want /backups", got)
	}
}

func TestBackupFileNameUsesTimestampAndAcceptsLegacyNames(t *testing.T) {
	createdAt := time.Date(2026, 7, 26, 9, 33, 43, 123456789, time.Local)
	name := backupFileName(createdAt)
	if name != "20260726_093343_123.zip" {
		t.Fatalf("backupFileName() = %q", name)
	}
	for _, candidate := range []string{name, "easy_proxies_backup_20260726_093343_000.zip"} {
		if !validBackupName(candidate) {
			t.Fatalf("validBackupName(%q) = false", candidate)
		}
	}
	for _, candidate := range []string{"backup.zip", "20260726_093343.zip", "20260726_093343_abc.zip"} {
		if validBackupName(candidate) {
			t.Fatalf("validBackupName(%q) = true", candidate)
		}
	}
}
