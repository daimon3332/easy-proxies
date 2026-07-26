package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"easy_proxies/internal/config"
	"easy_proxies/internal/importer"

	"github.com/studio-b12/gowebdav"
)

const (
	SchemaVersion    = 1
	MaxArchiveSize   = 20 << 20
	legacyFilePrefix = "easy_proxies_backup_"
)

type Runtime interface {
	ApplyRestoredConfig(context.Context, *config.Config) error
	CurrentConfig() *config.Config
	ListConfigNodes(context.Context) ([]config.NodeConfig, error)
}

type SubscriptionUpdater interface {
	ApplyRestoredConfig(*config.Config)
}

type ActivityChecker interface {
	HasActiveJobs() bool
}

type Manifest struct {
	SchemaVersion int               `json:"schema_version"`
	CreatedAt     time.Time         `json:"created_at"`
	NodeCount     int               `json:"node_count"`
	PoolNodeCount int               `json:"pool_node_count"`
	Subscriptions int               `json:"subscriptions"`
	Checksums     map[string]string `json:"checksums"`
}

type RestoreResult struct {
	NodeCount       int  `json:"node_count"`
	PoolNodeCount   int  `json:"pool_node_count"`
	Subscriptions   int  `json:"subscriptions"`
	RestartRequired bool `json:"restart_required"`
}

type RemoteFile struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

type Service struct {
	mu            sync.Mutex
	cfg           *config.Config
	store         *importer.Store
	runtime       Runtime
	subscriptions SubscriptionUpdater
	activity      ActivityChecker
}

func NewService(cfg *config.Config, store *importer.Store, runtime Runtime, subscriptions SubscriptionUpdater, activity ActivityChecker) *Service {
	return &Service{cfg: cfg, store: store, runtime: runtime, subscriptions: subscriptions, activity: activity}
}

func (s *Service) WebDAVConfig() config.WebDAVConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg == nil {
		return config.WebDAVConfig{Folder: config.DefaultWebDAVFolder}
	}
	value := s.cfg.WebDAV
	if value.Folder == "" {
		value.Folder = config.DefaultWebDAVFolder
	}
	return value
}

func (s *Service) SetWebDAVConfig(value config.WebDAVConfig) error {
	value.Address = strings.TrimSpace(value.Address)
	value.Username = strings.TrimSpace(value.Username)
	folder, err := normalizeWebDAVFolder(value.Folder)
	if err != nil {
		return err
	}
	value.Folder = folder
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.currentConfigLocked()
	if cfg == nil {
		return errors.New("配置未初始化")
	}
	previous := cfg.WebDAV
	cfg.WebDAV = value
	if err := cfg.SaveSettings(); err != nil {
		cfg.WebDAV = previous
		return err
	}
	s.cfg = cfg
	return nil
}

func (s *Service) Create() ([]byte, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createLocked()
}

func (s *Service) createLocked() ([]byte, string, error) {
	cfg := s.currentConfigLocked()
	if cfg == nil || s.store == nil {
		return nil, "", errors.New("备份服务未初始化")
	}
	configData, err := cfg.BackupYAML()
	if err != nil {
		return nil, "", err
	}
	storeSnapshot := s.store.BackupSnapshot()
	storeData, err := json.MarshalIndent(storeSnapshot, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("encode managed nodes: %w", err)
	}
	poolCount := 0
	for _, node := range storeSnapshot.Nodes {
		if node.InPool || node.State == importer.StateInPool {
			poolCount++
		}
	}
	manifest := Manifest{
		SchemaVersion: SchemaVersion,
		CreatedAt:     time.Now(),
		NodeCount:     len(storeSnapshot.Nodes),
		PoolNodeCount: poolCount,
		Subscriptions: len(cfg.Subscriptions),
		Checksums: map[string]string{
			"config.yaml":        checksum(configData),
			"managed_nodes.json": checksum(storeData),
		},
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, "", err
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, file := range []struct {
		name string
		data []byte
	}{{"manifest.json", manifestData}, {"config.yaml", configData}, {"managed_nodes.json", storeData}} {
		writer, err := zw.Create(file.name)
		if err != nil {
			return nil, "", err
		}
		if _, err := writer.Write(file.data); err != nil {
			return nil, "", err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, "", err
	}
	name := backupFileName(manifest.CreatedAt)
	return out.Bytes(), name, nil
}

func (s *Service) Restore(ctx context.Context, archive []byte) (RestoreResult, error) {
	if len(archive) == 0 || len(archive) > MaxArchiveSize {
		return RestoreResult{}, fmt.Errorf("备份文件大小必须在 1 字节到 %d MiB 之间", MaxArchiveSize>>20)
	}
	manifest, configData, storeData, err := parseArchive(archive)
	if err != nil {
		return RestoreResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activity != nil && s.activity.HasActiveJobs() {
		return RestoreResult{}, errors.New("当前有导入、刷新或测速任务正在运行，请等待任务结束后恢复")
	}
	currentCfg := s.currentConfigLocked()
	if currentCfg == nil || s.store == nil {
		return RestoreResult{}, errors.New("备份服务未初始化")
	}
	restoredCfg, err := config.DecodeBackupYAML(configData, currentCfg.FilePath())
	if err != nil {
		return RestoreResult{}, err
	}
	restoredStore, err := importer.DecodeStoreSnapshot(storeData)
	if err != nil {
		return RestoreResult{}, err
	}
	if err := validateManifestCounts(manifest, restoredCfg, restoredStore); err != nil {
		return RestoreResult{}, err
	}
	poolNodes := make([]importer.ManagedNode, 0, len(restoredStore.Nodes))
	for _, node := range restoredStore.Nodes {
		if node.InPool || node.State == importer.StateInPool {
			poolNodes = append(poolNodes, node)
		}
	}
	sort.Slice(poolNodes, func(i, j int) bool {
		if poolNodes[i].Order == poolNodes[j].Order {
			return poolNodes[i].ID < poolNodes[j].ID
		}
		return poolNodes[i].Order < poolNodes[j].Order
	})
	configuredURIs := make(map[string]struct{}, len(restoredCfg.Nodes)+len(poolNodes))
	for _, node := range restoredCfg.Nodes {
		configuredURIs[strings.TrimSpace(node.URI)] = struct{}{}
	}
	for _, node := range poolNodes {
		value := node.ToConfigNode()
		uri := strings.TrimSpace(value.URI)
		if uri == "" {
			continue
		}
		if _, exists := configuredURIs[uri]; exists {
			continue
		}
		value.Source = config.NodeSourceInline
		restoredCfg.Nodes = append(restoredCfg.Nodes, value)
		configuredURIs[uri] = struct{}{}
	}
	for i := range restoredCfg.Nodes {
		restoredCfg.Nodes[i].Source = config.NodeSourceInline
	}
	oldCfg := currentCfg
	oldStore := s.store.BackupSnapshot()
	oldFile, err := os.ReadFile(oldCfg.FilePath())
	if err != nil {
		return RestoreResult{}, fmt.Errorf("读取当前配置失败: %w", err)
	}
	if err := restoredCfg.SaveFull(); err != nil {
		return RestoreResult{}, fmt.Errorf("写入恢复配置失败: %w", err)
	}
	if err := s.store.ReplaceSnapshot(restoredStore); err != nil {
		_ = os.WriteFile(oldCfg.FilePath(), oldFile, 0o644)
		return RestoreResult{}, fmt.Errorf("恢复节点数据失败: %w", err)
	}
	if s.runtime != nil {
		if err := s.runtime.ApplyRestoredConfig(ctx, restoredCfg); err != nil {
			_ = os.WriteFile(oldCfg.FilePath(), oldFile, 0o644)
			_ = s.store.ReplaceSnapshot(oldStore)
			return RestoreResult{}, fmt.Errorf("重载恢复配置失败，已回滚: %w", err)
		}
		runtimeNodes, err := s.runtime.ListConfigNodes(ctx)
		if err != nil {
			_ = s.runtime.ApplyRestoredConfig(ctx, oldCfg)
			_ = os.WriteFile(oldCfg.FilePath(), oldFile, 0o644)
			_ = s.store.ReplaceSnapshot(oldStore)
			return RestoreResult{}, fmt.Errorf("读取恢复后的运行节点失败，已回滚: %w", err)
		}
		ports := make(map[string]uint16, len(runtimeNodes))
		for _, node := range runtimeNodes {
			ports[node.URI] = node.Port
		}
		for id, node := range restoredStore.Nodes {
			if port, ok := ports[node.URI]; ok && (node.InPool || node.State == importer.StateInPool) {
				node.Port = port
				restoredStore.Nodes[id] = node
			}
		}
		if err := s.store.ReplaceSnapshot(restoredStore); err != nil {
			_ = s.runtime.ApplyRestoredConfig(ctx, oldCfg)
			_ = os.WriteFile(oldCfg.FilePath(), oldFile, 0o644)
			_ = s.store.ReplaceSnapshot(oldStore)
			return RestoreResult{}, fmt.Errorf("同步恢复后的端口失败，已回滚: %w", err)
		}
	}
	if s.subscriptions != nil {
		s.subscriptions.ApplyRestoredConfig(restoredCfg)
	}
	s.cfg = restoredCfg
	return RestoreResult{
		NodeCount:       manifest.NodeCount,
		PoolNodeCount:   manifest.PoolNodeCount,
		Subscriptions:   manifest.Subscriptions,
		RestartRequired: restartRequired(oldCfg, restoredCfg),
	}, nil
}

func (s *Service) TestWebDAV() error {
	client, folder, err := s.webDAVLocation()
	if err != nil {
		return err
	}
	if err := client.Connect(); err != nil {
		return err
	}
	return ensureWebDAVFolder(client, folder)
}

func (s *Service) CreateWebDAV() (RemoteFile, error) {
	data, name, err := s.Create()
	if err != nil {
		return RemoteFile{}, err
	}
	client, folder, err := s.webDAVLocation()
	if err != nil {
		return RemoteFile{}, err
	}
	if err := ensureWebDAVFolder(client, folder); err != nil {
		return RemoteFile{}, err
	}
	temporary := remotePath(folder, "."+name+".tmp")
	final := remotePath(folder, name)
	if err := client.Write(temporary, data, 0o600); err != nil {
		return RemoteFile{}, fmt.Errorf("上传 WebDAV 备份失败: %w", err)
	}
	if err := client.Rename(temporary, final, true); err != nil {
		_ = client.Remove(temporary)
		return RemoteFile{}, fmt.Errorf("完成 WebDAV 备份失败: %w", err)
	}
	return RemoteFile{Name: name, Size: int64(len(data)), ModTime: time.Now()}, nil
}

func (s *Service) ListWebDAV() ([]RemoteFile, error) {
	client, folder, err := s.webDAVLocation()
	if err != nil {
		return nil, err
	}
	if err := ensureWebDAVFolder(client, folder); err != nil {
		return nil, err
	}
	entries, err := client.ReadDir(folder)
	if err != nil {
		return nil, fmt.Errorf("读取 WebDAV 备份列表失败: %w", err)
	}
	files := make([]RemoteFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !validBackupName(entry.Name()) {
			continue
		}
		files = append(files, RemoteFile{Name: entry.Name(), Size: entry.Size(), ModTime: entry.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ModTime.After(files[j].ModTime) })
	return files, nil
}

func (s *Service) DownloadWebDAV(name string) ([]byte, error) {
	if !validBackupName(name) {
		return nil, errors.New("备份文件名无效")
	}
	client, folder, err := s.webDAVLocation()
	if err != nil {
		return nil, err
	}
	reader, err := client.ReadStream(remotePath(folder, name))
	if err != nil {
		return nil, fmt.Errorf("下载 WebDAV 备份失败: %w", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, MaxArchiveSize+1))
	if err != nil {
		return nil, fmt.Errorf("下载 WebDAV 备份失败: %w", err)
	}
	if len(data) == 0 || len(data) > MaxArchiveSize {
		return nil, errors.New("WebDAV 备份文件大小无效")
	}
	return data, nil
}

func (s *Service) currentConfigLocked() *config.Config {
	if s.runtime == nil {
		return s.cfg
	}
	current := s.runtime.CurrentConfig()
	if current == nil {
		return s.cfg
	}
	if s.cfg != nil {
		current.WebDAV = s.cfg.WebDAV
	}
	s.cfg = current
	return current
}

func (s *Service) RestoreWebDAV(ctx context.Context, name string) (RestoreResult, error) {
	data, err := s.DownloadWebDAV(name)
	if err != nil {
		return RestoreResult{}, err
	}
	return s.Restore(ctx, data)
}

func (s *Service) DeleteWebDAV(name string) error {
	if !validBackupName(name) {
		return errors.New("备份文件名无效")
	}
	client, folder, err := s.webDAVLocation()
	if err != nil {
		return err
	}
	if err := client.Remove(remotePath(folder, name)); err != nil {
		return fmt.Errorf("删除 WebDAV 备份失败: %w", err)
	}
	return nil
}

func (s *Service) webDAVLocation() (*gowebdav.Client, string, error) {
	cfg := s.WebDAVConfig()
	if cfg.Address == "" {
		return nil, "", errors.New("请先填写 WebDAV 地址")
	}
	folder, err := normalizeWebDAVFolder(cfg.Folder)
	if err != nil {
		return nil, "", err
	}
	client := gowebdav.NewClient(strings.TrimRight(cfg.Address, "/"), cfg.Username, cfg.Password)
	client.SetTimeout(30 * time.Second)
	return client, folder, nil
}

func ensureWebDAVFolder(client *gowebdav.Client, folder string) error {
	if err := client.MkdirAll(folder, 0o755); err != nil {
		return fmt.Errorf("创建或访问 WebDAV 备份目录失败: %w", err)
	}
	return nil
}

func normalizeWebDAVFolder(folder string) (string, error) {
	folder = strings.TrimSpace(strings.ReplaceAll(folder, "\\", "/"))
	if folder == "" {
		return config.DefaultWebDAVFolder, nil
	}
	if strings.ContainsRune(folder, '\x00') {
		return "", errors.New("WebDAV 备份目录无效")
	}
	for _, segment := range strings.Split(folder, "/") {
		if segment == ".." {
			return "", errors.New("WebDAV 备份目录不能包含 ..")
		}
	}
	if !strings.HasPrefix(folder, "/") {
		folder = "/" + folder
	}
	return path.Clean(folder), nil
}

func parseArchive(data []byte) (Manifest, []byte, []byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Manifest{}, nil, nil, fmt.Errorf("打开备份文件失败: %w", err)
	}
	files := make(map[string][]byte, 3)
	var totalSize uint64
	for _, file := range zr.File {
		if file.Name != path.Base(file.Name) || strings.Contains(file.Name, "\\") {
			return Manifest{}, nil, nil, errors.New("备份文件包含无效路径")
		}
		switch file.Name {
		case "manifest.json", "config.yaml", "managed_nodes.json":
		default:
			return Manifest{}, nil, nil, fmt.Errorf("备份文件包含未知内容 %q", file.Name)
		}
		totalSize += file.UncompressedSize64
		if _, exists := files[file.Name]; exists || file.UncompressedSize64 > MaxArchiveSize || totalSize > MaxArchiveSize {
			return Manifest{}, nil, nil, fmt.Errorf("备份条目 %q 无效", file.Name)
		}
		reader, err := file.Open()
		if err != nil {
			return Manifest{}, nil, nil, err
		}
		content, readErr := io.ReadAll(io.LimitReader(reader, MaxArchiveSize+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || len(content) > MaxArchiveSize {
			return Manifest{}, nil, nil, fmt.Errorf("读取备份条目 %q 失败", file.Name)
		}
		files[file.Name] = content
	}
	for _, name := range []string{"manifest.json", "config.yaml", "managed_nodes.json"} {
		if files[name] == nil {
			return Manifest{}, nil, nil, fmt.Errorf("备份文件缺少 %s", name)
		}
	}
	var manifest Manifest
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		return Manifest{}, nil, nil, fmt.Errorf("解析备份清单失败: %w", err)
	}
	if manifest.SchemaVersion != SchemaVersion {
		return Manifest{}, nil, nil, fmt.Errorf("不支持的备份版本 %d", manifest.SchemaVersion)
	}
	for _, name := range []string{"config.yaml", "managed_nodes.json"} {
		if manifest.Checksums[name] == "" || manifest.Checksums[name] != checksum(files[name]) {
			return Manifest{}, nil, nil, fmt.Errorf("备份文件 %s 校验失败", name)
		}
	}
	return manifest, files["config.yaml"], files["managed_nodes.json"], nil
}

func checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validateManifestCounts(manifest Manifest, cfg *config.Config, snapshot importer.StoreSnapshot) error {
	poolCount := 0
	for _, node := range snapshot.Nodes {
		if node.InPool || node.State == importer.StateInPool {
			poolCount++
		}
	}
	if manifest.NodeCount != len(snapshot.Nodes) || manifest.PoolNodeCount != poolCount || manifest.Subscriptions != len(cfg.Subscriptions) {
		return errors.New("备份清单与实际内容不一致")
	}
	return nil
}

func validBackupName(name string) bool {
	if name != path.Base(name) || strings.Contains(name, "\\") || !strings.HasSuffix(name, ".zip") {
		return false
	}
	stamp := strings.TrimSuffix(name, ".zip")
	stamp = strings.TrimPrefix(stamp, legacyFilePrefix)
	if len(stamp) != len("20060102_150405_000") || stamp[8] != '_' || stamp[15] != '_' {
		return false
	}
	if _, err := time.Parse("20060102_150405", stamp[:15]); err != nil {
		return false
	}
	for _, value := range stamp[16:] {
		if value < '0' || value > '9' {
			return false
		}
	}
	return true
}

func backupFileName(createdAt time.Time) string {
	milliseconds := createdAt.Nanosecond() / int(time.Millisecond)
	return fmt.Sprintf("%s_%03d.zip", createdAt.Format("20060102_150405"), milliseconds)
}

func remotePath(folder, name string) string {
	return path.Join(folder, name)
}

func restartRequired(oldCfg, newCfg *config.Config) bool {
	return oldCfg.Management.Listen != newCfg.Management.Listen ||
		oldCfg.Management.Password != newCfg.Management.Password ||
		oldCfg.Log != newCfg.Log
}
