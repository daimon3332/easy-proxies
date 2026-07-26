package monitor

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	backupsvc "easy_proxies/internal/backup"
	"easy_proxies/internal/config"
	"easy_proxies/internal/importer"
)

type tagExportFile struct {
	Name        string
	ContentType string
	Data        []byte
}

type tagExportGroup struct {
	Tag         string
	URLs        []string
	URLSeen     map[string]struct{}
	Nodes       map[string][]config.NodeConfig
	NodeURISeen map[string]map[string]struct{}
}

func (s *Server) handleDataExportNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.ensureImportService(w) {
		return
	}
	var req struct {
		All     bool     `json:"all"`
		NodeIDs []string `json:"node_ids"`
		Tags    []string `json:"tags"`
		Format  string   `json:"format"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format != "uri" && format != "base64" && format != "yaml" {
		writeAPIError(w, http.StatusBadRequest, "导出格式只支持 uri、base64 或 yaml")
		return
	}
	allNodes, err := s.importSvc.ListAll()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	selected := make(map[string]struct{}, len(req.NodeIDs))
	for _, id := range req.NodeIDs {
		if id = strings.TrimSpace(id); id != "" {
			selected[id] = struct{}{}
		}
	}
	selectedTags := normalizedSet(req.Tags)
	nodes := make([]config.NodeConfig, 0, len(allNodes))
	seen := make(map[string]struct{}, len(allNodes))
	for _, node := range allNodes {
		if !matchesNodeExportSelection(node, req.All, selected, selectedTags) {
			continue
		}
		uri := strings.TrimSpace(node.URI)
		if uri == "" {
			continue
		}
		if _, ok := seen[uri]; ok {
			continue
		}
		seen[uri] = struct{}{}
		nodes = append(nodes, config.NodeConfig{Name: node.Name, URI: uri})
	}
	if req.All && len(selectedTags) == 0 && s.nodeMgr != nil {
		configured, err := s.nodeMgr.ListConfigNodes(r.Context())
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, node := range configured {
			uri := strings.TrimSpace(node.URI)
			if uri == "" || contains(seen, uri) {
				continue
			}
			seen[uri] = struct{}{}
			node.URI = uri
			nodes = append(nodes, node)
		}
	}
	if len(nodes) == 0 {
		writeAPIError(w, http.StatusBadRequest, "没有可导出的节点")
		return
	}
	var data []byte
	filename := "easy_proxies_nodes.txt"
	contentType := "text/plain; charset=utf-8"
	switch format {
	case "uri":
		data = []byte(joinNodeURIs(nodes))
	case "base64":
		data = []byte(joinBase64NodeURIs(nodes))
		filename = "easy_proxies_nodes_base64.txt"
	case "yaml":
		data, err = config.ExportClashYAML(nodes)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		filename = "easy_proxies_nodes.yaml"
		contentType = "application/yaml; charset=utf-8"
	}
	writeDownload(w, filename, contentType, data)
}

func (s *Server) handleDataExportSubscriptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		All           bool     `json:"all"`
		Subscriptions []string `json:"subscriptions"`
		Tags          []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	selectedTags := normalizedSet(req.Tags)
	if len(selectedTags) > 0 {
		if !s.ensureImportService(w) {
			return
		}
		nodes, err := s.importSvc.ListAll()
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		result := subscriptionURLsByTags(nodes, selectedTags)
		if len(result) == 0 {
			writeAPIError(w, http.StatusBadRequest, "选中的 Tag 没有订阅链接")
			return
		}
		writeDownload(w, "easy_proxies_subscriptions.txt", "text/plain; charset=utf-8", []byte(strings.Join(result, "\n")+"\n"))
		return
	}
	s.cfgMu.RLock()
	var configured []string
	if s.cfgSrc != nil {
		configured = append(configured, s.cfgSrc.Subscriptions...)
	}
	s.cfgMu.RUnlock()
	selected := make(map[string]struct{}, len(req.Subscriptions))
	for _, item := range req.Subscriptions {
		selected[strings.TrimSpace(item)] = struct{}{}
	}
	result := make([]string, 0, len(configured))
	seen := make(map[string]struct{}, len(configured))
	for _, item := range configured {
		item = strings.TrimSpace(item)
		if item == "" || (!req.All && !contains(selected, item)) || contains(seen, item) {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	if len(result) == 0 {
		writeAPIError(w, http.StatusBadRequest, "没有可导出的订阅链接")
		return
	}
	writeDownload(w, "easy_proxies_subscriptions.txt", "text/plain; charset=utf-8", []byte(strings.Join(result, "\n")+"\n"))
}

func (s *Server) handleDataExportTags(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.ensureImportService(w) {
		return
	}
	var req struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	tags := normalizedSet(req.Tags)
	if len(tags) == 0 {
		writeAPIError(w, http.StatusBadRequest, "请至少选择一个 Tag")
		return
	}
	nodes, err := s.importSvc.ListAll()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	files, err := buildTagExportFiles(nodes, tags)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(files) == 1 {
		writeDownload(w, files[0].Name, files[0].ContentType, files[0].Data)
		return
	}
	data, err := buildTagExportZIP(files)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	name := "easy_proxies_tags_" + time.Now().Format("20060102_150405") + ".zip"
	writeDownload(w, name, "application/zip", data)
}

func buildTagExportFiles(nodes []importer.ManagedNode, selected map[string]struct{}) ([]tagExportFile, error) {
	groups := make(map[string]*tagExportGroup, len(selected))
	for _, node := range nodes {
		tag := strings.TrimSpace(node.TagPrefix)
		if !contains(selected, tag) {
			continue
		}
		group := groups[tag]
		if group == nil {
			group = &tagExportGroup{
				Tag:         tag,
				URLSeen:     make(map[string]struct{}),
				Nodes:       make(map[string][]config.NodeConfig),
				NodeURISeen: make(map[string]map[string]struct{}),
			}
			groups[tag] = group
		}
		if node.ImportMode == "url" && strings.TrimSpace(node.ImportSource) != "" {
			for _, rawURL := range strings.Fields(node.ImportSource) {
				rawURL = strings.TrimSpace(rawURL)
				if rawURL == "" || contains(group.URLSeen, rawURL) {
					continue
				}
				group.URLSeen[rawURL] = struct{}{}
				group.URLs = append(group.URLs, rawURL)
			}
			continue
		}
		uri := strings.TrimSpace(node.URI)
		if uri == "" {
			continue
		}
		format := normalizeImportFormat(node.ImportFormat)
		seen := group.NodeURISeen[format]
		if seen == nil {
			seen = make(map[string]struct{})
			group.NodeURISeen[format] = seen
		}
		if contains(seen, uri) {
			continue
		}
		seen[uri] = struct{}{}
		group.Nodes[format] = append(group.Nodes[format], config.NodeConfig{Name: node.Name, URI: uri})
	}

	missing := make([]string, 0)
	for tag := range selected {
		group := groups[tag]
		if group == nil || (len(group.URLs) == 0 && countTagNodes(group.Nodes) == 0) {
			missing = append(missing, tag)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("Tag 不存在或没有可导出数据: %s", strings.Join(missing, ", "))
	}

	tags := make([]string, 0, len(groups))
	for tag := range groups {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	files := make([]tagExportFile, 0, len(tags))
	usedNames := make(map[string]int)
	for _, tag := range tags {
		group := groups[tag]
		base := safeExportName(tag)
		if len(group.URLs) > 0 {
			sort.Strings(group.URLs)
			files = append(files, tagExportFile{
				Name:        uniqueExportName(base+"_subscription_urls", ".txt", usedNames),
				ContentType: "text/plain; charset=utf-8",
				Data:        []byte(strings.Join(group.URLs, "\n") + "\n"),
			})
		}
		formats := make([]string, 0, len(group.Nodes))
		for format := range group.Nodes {
			formats = append(formats, format)
		}
		sort.Strings(formats)
		for _, format := range formats {
			nodes := group.Nodes[format]
			if len(nodes) == 0 {
				continue
			}
			file, err := buildTagFormatFile(base, format, nodes, usedNames)
			if err != nil {
				return nil, fmt.Errorf("Tag %q 导出失败: %w", tag, err)
			}
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("选中的 Tag 没有可导出数据")
	}
	return files, nil
}

func countTagNodes(groups map[string][]config.NodeConfig) int {
	total := 0
	for _, nodes := range groups {
		total += len(nodes)
	}
	return total
}

func buildTagFormatFile(base, format string, nodes []config.NodeConfig, usedNames map[string]int) (tagExportFile, error) {
	switch format {
	case "base64":
		encoded := base64.StdEncoding.EncodeToString([]byte(joinNodeURIs(nodes))) + "\n"
		return tagExportFile{Name: uniqueExportName(base+"_base64", ".txt", usedNames), ContentType: "text/plain; charset=utf-8", Data: []byte(encoded)}, nil
	case "clash_yaml":
		data, err := config.ExportClashYAML(nodes)
		if err != nil {
			return tagExportFile{}, err
		}
		return tagExportFile{Name: uniqueExportName(base+"_clash", ".yaml", usedNames), ContentType: "application/yaml; charset=utf-8", Data: data}, nil
	default:
		return tagExportFile{Name: uniqueExportName(base+"_uris", ".txt", usedNames), ContentType: "text/plain; charset=utf-8", Data: []byte(joinNodeURIs(nodes))}, nil
	}
}

func buildTagExportZIP(files []tagExportFile) ([]byte, error) {
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, file := range files {
		writer, err := zw.Create(file.Name)
		if err != nil {
			_ = zw.Close()
			return nil, err
		}
		if _, err := writer.Write(file.Data); err != nil {
			_ = zw.Close()
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func normalizeImportFormat(format string) string {
	switch strings.TrimSpace(format) {
	case "base64":
		return "base64"
	case "clash_yaml":
		return "clash_yaml"
	default:
		return "uri_list"
	}
}

func safeExportName(tag string) string {
	var out strings.Builder
	for _, r := range strings.TrimSpace(tag) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			out.WriteRune(r)
		default:
			out.WriteByte('_')
		}
		if out.Len() >= 64 {
			break
		}
	}
	name := strings.Trim(out.String(), "._-")
	if name == "" {
		return "tag"
	}
	return name
}

func uniqueExportName(base, extension string, used map[string]int) string {
	for index := 1; ; index++ {
		name := base + extension
		if index > 1 {
			name = fmt.Sprintf("%s_%d%s", base, index, extension)
		}
		if used[name] == 0 {
			used[name] = 1
			return name
		}
	}
}

func (s *Server) handleLocalBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.ensureBackupService(w) {
		return
	}
	data, name, err := s.backupSvc.Create()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeDownload(w, name, "application/zip", data)
}

func (s *Server) handleLocalRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.ensureBackupService(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, backupsvc.MaxArchiveSize+(1<<20))
	if err := r.ParseMultipartForm(backupsvc.MaxArchiveSize); err != nil {
		writeAPIError(w, http.StatusBadRequest, "备份文件过大或上传格式错误")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "请选择备份 ZIP 文件")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, backupsvc.MaxArchiveSize+1))
	if err != nil || len(data) > backupsvc.MaxArchiveSize {
		writeAPIError(w, http.StatusBadRequest, "读取备份文件失败或文件过大")
		return
	}
	result, err := s.backupSvc.Restore(r.Context(), data)
	if err != nil {
		writeAPIError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, map[string]any{"message": "本地备份恢复完成", "result": result})
}

func (s *Server) handleWebDAVSettings(w http.ResponseWriter, r *http.Request) {
	if !s.ensureBackupService(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.backupSvc.WebDAVConfig())
	case http.MethodPut:
		var cfg config.WebDAVConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeAPIError(w, http.StatusBadRequest, "请求格式错误")
			return
		}
		if err := s.backupSvc.SetWebDAVConfig(cfg); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, map[string]any{"message": "WebDAV 设置已保存", "webdav": s.backupSvc.WebDAVConfig()})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleWebDAVTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.ensureBackupService(w) {
		return
	}
	if err := s.backupSvc.TestWebDAV(); err != nil {
		writeAPIError(w, http.StatusBadGateway, "WebDAV 连接失败: "+err.Error())
		return
	}
	writeJSON(w, map[string]string{"message": "WebDAV 连接成功"})
}

func (s *Server) handleWebDAVFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.ensureBackupService(w) {
		return
	}
	files, err := s.backupSvc.ListWebDAV()
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, map[string]any{"files": files})
}

func (s *Server) handleWebDAVCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.ensureBackupService(w) {
		return
	}
	file, err := s.backupSvc.CreateWebDAV()
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, map[string]any{"message": "WebDAV 备份创建完成", "file": file})
}

func (s *Server) handleWebDAVFileAction(w http.ResponseWriter, r *http.Request) {
	if !s.ensureBackupService(w) {
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/backup/webdav/"), "/"), "/")
	if len(parts) != 2 {
		writeAPIError(w, http.StatusNotFound, "WebDAV 备份操作不存在")
		return
	}
	name, err := url.PathUnescape(parts[0])
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "备份文件名无效")
		return
	}
	switch parts[1] {
	case "download":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		data, err := s.backupSvc.DownloadWebDAV(name)
		if err != nil {
			writeAPIError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeDownload(w, name, "application/zip", data)
	case "restore":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		result, err := s.backupSvc.RestoreWebDAV(r.Context(), name)
		if err != nil {
			writeAPIError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, map[string]any{"message": "WebDAV 备份恢复完成", "result": result})
	case "delete":
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := s.backupSvc.DeleteWebDAV(name); err != nil {
			writeAPIError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, map[string]string{"message": "WebDAV 备份已删除"})
	default:
		writeAPIError(w, http.StatusNotFound, "WebDAV 备份操作不存在")
	}
}

func (s *Server) ensureBackupService(w http.ResponseWriter) bool {
	if s.backupSvc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "备份服务未初始化")
		return false
	}
	return true
}

func writeDownload(w http.ResponseWriter, filename, contentType string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	_, _ = w.Write(data)
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	writeJSON(w, map[string]string{"error": message})
}

func joinNodeURIs(nodes []config.NodeConfig) string {
	lines := make([]string, 0, len(nodes))
	for _, node := range nodes {
		lines = append(lines, node.URI)
	}
	return strings.Join(lines, "\n") + "\n"
}

func joinBase64NodeURIs(nodes []config.NodeConfig) string {
	lines := make([]string, 0, len(nodes))
	for _, node := range nodes {
		lines = append(lines, base64.StdEncoding.EncodeToString([]byte(node.URI)))
	}
	return strings.Join(lines, "\n") + "\n"
}

func normalizedSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}

func matchesNodeExportSelection(node importer.ManagedNode, all bool, ids, tags map[string]struct{}) bool {
	if len(ids) > 0 {
		return contains(ids, node.ID)
	}
	if len(tags) > 0 {
		return contains(tags, strings.TrimSpace(node.TagPrefix))
	}
	return all
}

func subscriptionURLsByTags(nodes []importer.ManagedNode, tags map[string]struct{}) []string {
	result := make([]string, 0)
	seen := make(map[string]struct{})
	for _, node := range nodes {
		if node.ImportMode != "url" || !contains(tags, strings.TrimSpace(node.TagPrefix)) {
			continue
		}
		for _, value := range strings.Fields(node.ImportSource) {
			value = strings.TrimSpace(value)
			if value == "" || contains(seen, value) {
				continue
			}
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}

func contains[T comparable](values map[T]struct{}, value T) bool {
	_, ok := values[value]
	return ok
}
