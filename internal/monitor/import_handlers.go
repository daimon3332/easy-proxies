package monitor

import (
	"encoding/json"
	"net/http"
	"strings"

	"easy_proxies/internal/importer"
)

type ImportService interface {
	Parse(req importer.ParseRequest) (importer.ParseResponse, error)
	Commit(importID string, req importer.CommitRequest) (importer.CommitResponse, error)
	Retest(nodeID string) (importer.ManagedNode, error)
	TestCountry(nodeID string) (importer.ManagedNode, error)
	BatchTest(req importer.BatchTestRequest) (importer.BatchTestResponse, error)
	StartBatchTest(req importer.BatchTestRequest) (string, error)
	GetTestJob(jobID string) (importer.TestJob, bool)
	Promote(nodeID string, autoReload bool) (importer.ManagedNode, error)
	PromoteMany(nodeIDs []string, autoReload bool) ([]importer.ManagedNode, error)
	Exclude(nodeID string) (importer.ManagedNode, error)
	Delete(nodeID string) error
	DeleteMany(nodeIDs []string) (int, error)
	DeleteBySubscription(url string) (int, error)
	ListAll() ([]importer.ManagedNode, error)
	ListPool() ([]importer.ManagedNode, error)
	ListFailed() ([]importer.ManagedNode, error)
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
	resp, err := s.importSvc.Parse(req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, resp)
}

func (s *Server) handleImportAction(w http.ResponseWriter, r *http.Request) {
	if !s.ensureImportService(w) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/import/")

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

func extractManagedNodeAction(path string) (nodeID, action string) {
	p := strings.TrimPrefix(path, "/api/managed-nodes/")
	p = strings.TrimRight(p, "/")
	idx := strings.LastIndex(p, "/")
	if idx < 0 {
		return "", ""
	}
	return p[:idx], p[idx+1:]
}
