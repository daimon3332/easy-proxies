package monitor

import (
	"net/http"
	"runtime"

	"easy_proxies/internal/importer"
)

type diagnosticsProvider interface {
	Diagnostics() map[string]any
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	result := map[string]any{
		"go": map[string]any{
			"goroutines":       runtime.NumGoroutine(),
			"heap_alloc_bytes": memory.HeapAlloc,
			"heap_objects":     memory.HeapObjects,
			"gc_count":         memory.NumGC,
		},
	}
	if provider, ok := s.nodeMgr.(diagnosticsProvider); ok {
		result["runtime"] = provider.Diagnostics()
	}
	if svc, ok := s.importSvc.(interface {
		ProbeSchedulerStats() importer.ProbeSchedulerStats
	}); ok {
		result["probe_scheduler"] = svc.ProbeSchedulerStats()
	}
	if svc, ok := s.importSvc.(interface {
		JobEventStats() importer.JobEventStats
	}); ok {
		result["job_events"] = svc.JobEventStats()
	}
	if svc, ok := s.importSvc.(interface {
		JobRetentionStats() importer.JobRetentionStats
	}); ok {
		result["retained_jobs"] = svc.JobRetentionStats()
	}
	writeJSON(w, result)
}
