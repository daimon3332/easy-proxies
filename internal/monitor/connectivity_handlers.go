package monitor

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"easy_proxies/internal/importer"
)

func (s *Server) handleConnectivityScopes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 GET 请求"})
		return
	}
	if !s.ensureImportService(w) {
		return
	}
	writeJSON(w, s.importSvc.ConnectivityScopes())
}

func (s *Server) handleConnectivityStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 POST 请求"})
		return
	}
	if !s.ensureImportService(w) {
		return
	}
	var req importer.ConnectivityStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "请求格式错误"})
		return
	}
	jobID, err := s.importSvc.StartConnectivityJob(req)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"job_id": jobID})
}

func (s *Server) handleConnectivityStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 GET 请求"})
		return
	}
	if !s.ensureImportService(w) {
		return
	}
	jobID := strings.TrimSpace(r.URL.Query().Get("id"))
	job, ok := s.importSvc.GetConnectivityJob(jobID)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "检测任务不存在或已过期"})
		return
	}
	writeJSON(w, job)
}

func (s *Server) handleConnectivityCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 POST 请求"})
		return
	}
	if !s.ensureImportService(w) {
		return
	}
	job, err := s.importSvc.CancelConnectivityJob(strings.TrimSpace(r.URL.Query().Get("id")))
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, job)
}

func (s *Server) handleConnectivityResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 GET 请求"})
		return
	}
	if !s.ensureImportService(w) {
		return
	}
	query := importer.ConnectivityResultQuery{
		JobID:    strings.TrimSpace(r.URL.Query().Get("id")),
		Tag:      strings.TrimSpace(r.URL.Query().Get("tag")),
		TargetID: strings.TrimSpace(r.URL.Query().Get("target")),
		Status:   strings.TrimSpace(r.URL.Query().Get("status")),
		Page:     queryInt(r, "page", 1),
		PageSize: queryInt(r, "page_size", 100),
	}
	page, err := s.importSvc.ListConnectivityResults(query)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, page)
}

func (s *Server) handleConnectivityHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 GET 请求"})
		return
	}
	if !s.ensureImportService(w) {
		return
	}
	comparison, err := s.importSvc.ConnectivityHistory(strings.TrimSpace(r.URL.Query().Get("id")))
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, comparison)
}

func queryInt(r *http.Request, name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get(name)))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func (s *Server) handleConnectivityPortPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 POST 请求"})
		return
	}
	if !s.ensureImportService(w) {
		return
	}
	var req importer.ConnectivityPortRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "请求格式错误"})
		return
	}
	preview, err := s.importSvc.PreviewConnectivityPool(req)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, preview)
}

func (s *Server) handleConnectivityPortApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 POST 请求"})
		return
	}
	if !s.ensureImportService(w) {
		return
	}
	var req importer.ConnectivityPortRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "请求格式错误"})
		return
	}
	result, err := s.importSvc.ApplyConnectivityPool(r.Context(), req)
	if err != nil {
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, result)
}
