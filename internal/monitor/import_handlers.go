package monitor

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"easy_proxies/internal/importer"
)

type ImportService interface {
	Parse(req importer.ParseRequest) (importer.ParseResponse, error)
	Commit(importID string, req importer.CommitRequest) (importer.CommitResponse, error)
	CancelImportJob(jobID string) (importer.ImportJob, error)
	Retest(nodeID string) (importer.ManagedNode, error)
	TestCountry(nodeID string) (importer.ManagedNode, error)
	BatchTest(req importer.BatchTestRequest) (importer.BatchTestResponse, error)
	StartBatchTest(req importer.BatchTestRequest) (string, error)
	GetTestJob(jobID string) (importer.TestJob, bool)
	CancelTestJob(jobID string) (importer.TestJob, error)
	Promote(nodeID string, autoReload bool) (importer.ManagedNode, error)
	PromoteMany(nodeIDs []string, autoReload bool) ([]importer.ManagedNode, error)
	Exclude(nodeID string) (importer.ManagedNode, error)
	Delete(nodeID string) error
	DeleteMany(nodeIDs []string) (int, error)
	DeleteBySubscription(url string) (int, error)
	DeleteImportSource(key string) (int, error)
	DeleteAllImportSources() (int, error)
	ListImportSources() ([]importer.ImportSourceSummary, error)
	StartRefreshSources(key string) (string, error)
	GetRefreshJob(jobID string) (importer.SourceRefreshJob, bool)
	ListAll() ([]importer.ManagedNode, error)
	ListPool() ([]importer.ManagedNode, error)
	ListFailed() ([]importer.ManagedNode, error)
	Summary() (importer.DashboardSummary, error)
	UISummary() (importer.UISummary, error)
	ListUINodes(query importer.UINodeListQuery) (importer.UINodeListResponse, error)
	UIPortPreview(limit int) ([]importer.UIPortPreviewItem, error)
	ReorderPoolBy(mode string) error
	Reorder(ids []string) error
	Job(jobID string) (importer.ImportJob, bool)
}

func (s *Server) ensureImportService(w http.ResponseWriter) bool {
	if s.importSvc == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"error": "import service not available"})
		return false
	}
	return true
}

func (s *Server) handleImportSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 GET 请求"})
		return
	}
	if !s.ensureImportService(w) {
		return
	}
	summary, err := s.importSvc.Summary()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, summary)
}

func (s *Server) handleUISummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 GET 请求"})
		return
	}
	if !s.ensureImportService(w) {
		return
	}
	summary, err := s.importSvc.UISummary()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, summary)
}

func (s *Server) handleUINodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 GET 请求"})
		return
	}
	if !s.ensureImportService(w) {
		return
	}
	query := r.URL.Query()
	page, err := parseUIQueryInt(query.Get("page"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "page 必须是整数"})
		return
	}
	pageSize, err := parseUIQueryInt(query.Get("page_size"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "page_size 必须是整数"})
		return
	}
	result, err := s.importSvc.ListUINodes(importer.UINodeListQuery{
		Scope:    query.Get("scope"),
		Country:  query.Get("country"),
		Tag:      query.Get("tag"),
		Query:    query.Get("q"),
		Latency:  query.Get("latency"),
		Sort:     query.Get("sort"),
		Order:    query.Get("order"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleUIPorts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 GET 请求"})
		return
	}
	if !s.ensureImportService(w) {
		return
	}
	limit, err := parseUIQueryInt(r.URL.Query().Get("limit"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "limit 必须是整数"})
		return
	}
	items, err := s.importSvc.UIPortPreview(limit)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, items)
}

func (s *Server) handleUIPoolOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 POST 请求"})
		return
	}
	if !s.ensureImportService(w) {
		return
	}
	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "请求格式错误"})
		return
	}
	if err := s.importSvc.ReorderPoolBy(req.Mode); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func parseUIQueryInt(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	return strconv.Atoi(raw)
}

func (s *Server) handleImportParse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 POST 请求"})
		return
	}
	if !s.ensureImportService(w) {
		return
	}
	var req importer.ParseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "请求格式错误"})
		return
	}
	if strings.TrimSpace(req.TagPrefix) == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "Tag 前缀不能为空"})
		return
	}
	resp, err := s.importSvc.Parse(req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, resp)
}

func (s *Server) handleImportSources(w http.ResponseWriter, r *http.Request) {
	if !s.ensureImportService(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		sources, err := s.importSvc.ListImportSources()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, sources)
	case http.MethodPost, http.MethodDelete:
		var req struct {
			Key string `json:"key"`
			All bool   `json:"all"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]string{"error": "请求格式错误"})
			return
		}
		if req.All {
			deleted, err := s.importSvc.DeleteAllImportSources()
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, map[string]string{"error": err.Error()})
				return
			}
			if err := s.clearSubscriptionURLs(); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, map[string]any{"deleted_nodes": deleted, "deleted_sources": "all"})
			return
		}
		deleted, err := s.importSvc.DeleteImportSource(req.Key)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"deleted_nodes": deleted})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 GET/POST/DELETE 请求"})
	}
}

func (s *Server) clearSubscriptionURLs() error {
	enabled := true
	interval := 24 * time.Hour
	s.cfgMu.Lock()
	if s.cfgSrc != nil {
		enabled = s.cfgSrc.SubscriptionRefresh.Enabled
		if s.cfgSrc.SubscriptionRefresh.Interval > 0 {
			interval = s.cfgSrc.SubscriptionRefresh.Interval
		}
		s.cfgSrc.Subscriptions = nil
		if err := s.cfgSrc.SaveSettings(); err != nil {
			s.cfgMu.Unlock()
			return err
		}
	}
	s.cfgMu.Unlock()
	if s.subRefresher != nil {
		return s.subRefresher.UpdateConfigAndRefresh(nil, enabled, interval)
	}
	return nil
}

func (s *Server) handleImportAction(w http.ResponseWriter, r *http.Request) {
	if !s.ensureImportService(w) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/import/")

	if path == "refresh" && r.Method == http.MethodPost {
		var req struct {
			Key string `json:"key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]string{"error": "请求格式错误"})
			return
		}
		jobID, err := s.importSvc.StartRefreshSources(req.Key)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]string{"job_id": jobID})
		return
	}

	if strings.HasPrefix(path, "refresh/jobs/") && r.Method == http.MethodGet {
		jobID := strings.TrimRight(path[len("refresh/jobs/"):], "/")
		job, found := s.importSvc.GetRefreshJob(jobID)
		if !found {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]string{"error": "refresh job not found"})
			return
		}
		writeJSON(w, job)
		return
	}

	if strings.HasPrefix(path, "jobs/") && r.Method == http.MethodGet {
		jobID := strings.TrimRight(path[len("jobs/"):], "/")
		job, found := s.importSvc.Job(jobID)
		if !found {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]string{"error": "job not found"})
			return
		}
		writeJSON(w, job)
		return
	}

	if strings.HasPrefix(path, "jobs/") && strings.HasSuffix(path, "/cancel") && r.Method == http.MethodPost {
		jobID := strings.TrimRight(strings.TrimSuffix(path[len("jobs/"):], "/cancel"), "/")
		job, err := s.importSvc.CancelImportJob(jobID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, job)
		return
	}

	if strings.HasSuffix(path, "/commit") && r.Method == http.MethodPost {
		importID := strings.TrimRight(path[:len(path)-len("/commit")], "/")
		var req importer.CommitRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]string{"error": "请求格式错误"})
			return
		}
		resp, err := s.importSvc.Commit(importID, req)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, resp)
		return
	}

	w.WriteHeader(http.StatusNotFound)
	writeJSON(w, map[string]string{"error": "未知的导入操作"})
}

func (s *Server) handleManagedNodesAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 GET 请求"})
		return
	}
	if !s.ensureImportService(w) {
		return
	}
	nodes, err := s.importSvc.ListAll()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, nodes)
}

func (s *Server) handleManagedNodesPool(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 GET 请求"})
		return
	}
	if !s.ensureImportService(w) {
		return
	}
	nodes, err := s.importSvc.ListPool()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, nodes)
}

func (s *Server) handleManagedNodesFailed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 GET 请求"})
		return
	}
	if !s.ensureImportService(w) {
		return
	}
	nodes, err := s.importSvc.ListFailed()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, nodes)
}

func (s *Server) handleManagedNodesOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 PUT 请求"})
		return
	}
	if !s.ensureImportService(w) {
		return
	}
	var req struct {
		Order []string `json:"order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "请求格式错误"})
		return
	}
	if err := s.importSvc.Reorder(req.Order); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleManagedNodeAction(w http.ResponseWriter, r *http.Request) {
	if !s.ensureImportService(w) {
		return
	}
	if strings.TrimRight(r.URL.Path, "/") == "/api/managed-nodes/batch-test" {
		s.handleManagedNodesBatchTest(w, r)
		return
	}
	if strings.TrimRight(r.URL.Path, "/") == "/api/managed-nodes/batch-test/start" {
		s.handleManagedNodesBatchTestStart(w, r)
		return
	}
	if strings.TrimRight(r.URL.Path, "/") == "/api/managed-nodes/batch-test/status" {
		s.handleManagedNodesBatchTestStatus(w, r)
		return
	}
	if strings.TrimRight(r.URL.Path, "/") == "/api/managed-nodes/batch-test/cancel" {
		s.handleManagedNodesBatchTestCancel(w, r)
		return
	}
	if strings.TrimRight(r.URL.Path, "/") == "/api/managed-nodes/batch-promote" {
		s.handleManagedNodesBatchPromote(w, r)
		return
	}
	if strings.TrimRight(r.URL.Path, "/") == "/api/managed-nodes/batch-delete" {
		s.handleManagedNodesBatchDelete(w, r)
		return
	}
	nodeID, action := extractManagedNodeAction(r.URL.Path)
	if nodeID == "" || action == "" {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "未知的节点操作"})
		return
	}

	switch action {
	case "retest":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			writeJSON(w, map[string]string{"error": "仅支持 POST 请求"})
			return
		}
		node, err := s.importSvc.Retest(nodeID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, node)

	case "country":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			writeJSON(w, map[string]string{"error": "仅支持 POST 请求"})
			return
		}
		node, err := s.importSvc.TestCountry(nodeID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, node)

	case "promote":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			writeJSON(w, map[string]string{"error": "仅支持 POST 请求"})
			return
		}
		node, err := s.importSvc.Promote(nodeID, false)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, node)

	case "exclude":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			writeJSON(w, map[string]string{"error": "仅支持 POST 请求"})
			return
		}
		node, err := s.importSvc.Exclude(nodeID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, node)

	case "delete":
		if r.Method != http.MethodPost && r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			writeJSON(w, map[string]string{"error": "仅支持 POST/DELETE 请求"})
			return
		}
		if err := s.importSvc.Delete(nodeID); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]string{"status": "deleted"})

	default:
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "未知操作"})
	}
}

func (s *Server) handleManagedNodesBatchTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 POST 请求"})
		return
	}
	var req importer.BatchTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "请求格式错误"})
		return
	}
	resp, err := s.importSvc.BatchTest(req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, resp)
}

func (s *Server) handleManagedNodesBatchTestStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 POST 请求"})
		return
	}
	var req importer.BatchTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "请求格式错误"})
		return
	}
	jobID, err := s.importSvc.StartBatchTest(req)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"job_id": jobID})
}

func (s *Server) handleManagedNodesBatchPromote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 POST 请求"})
		return
	}
	var req struct {
		NodeIDs    []string `json:"node_ids"`
		AutoReload bool     `json:"auto_reload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "请求格式错误"})
		return
	}
	nodes, err := s.importSvc.PromoteMany(req.NodeIDs, req.AutoReload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"promoted": len(nodes), "nodes": nodes})
}

func (s *Server) handleManagedNodesBatchDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 POST/DELETE 请求"})
		return
	}
	var req struct {
		NodeIDs []string `json:"node_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "请求格式错误"})
		return
	}
	deleted, err := s.importSvc.DeleteMany(req.NodeIDs)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"deleted": deleted})
}

func (s *Server) handleManagedNodesBatchTestStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 GET 请求"})
		return
	}
	jobID := strings.TrimSpace(r.URL.Query().Get("id"))
	if jobID == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "缺少 id"})
		return
	}
	job, ok := s.importSvc.GetTestJob(jobID)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "job 不存在或已过期"})
		return
	}
	writeJSON(w, job)
}

func (s *Server) handleManagedNodesBatchTestCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 POST 请求"})
		return
	}
	jobID := strings.TrimSpace(r.URL.Query().Get("id"))
	if jobID == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "缺少 id"})
		return
	}
	job, err := s.importSvc.CancelTestJob(jobID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, job)
}

func extractManagedNodeAction(path string) (nodeID, action string) {
	p := strings.TrimPrefix(path, "/api/managed-nodes/")
	p = strings.TrimRight(p, "/")
	idx := strings.LastIndex(p, "/")
	if idx < 0 {
		return "", ""
	}
	return p[:idx], p[idx+1:]
}
