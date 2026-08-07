package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"easy_proxies/internal/importer"
)

type connectivityImportStub struct {
	ImportService
	startRequest importer.ConnectivityStartRequest
	query        importer.ConnectivityResultQuery
	request      importer.ConnectivityPortRequest
	page         importer.ConnectivityResultPage
	history      importer.ConnectivityHistoryComparison
}

func (s *connectivityImportStub) ConnectivityScopes() importer.ConnectivityScopeResponse {
	return importer.ConnectivityScopeResponse{Tags: []importer.ConnectivityTagScope{{Tag: "A", Nodes: 2}}}
}

func (s *connectivityImportStub) StartConnectivityJob(req importer.ConnectivityStartRequest) (string, error) {
	s.startRequest = req
	return "job-1", nil
}

func (s *connectivityImportStub) GetConnectivityJob(jobID string) (importer.ConnectivityJob, bool) {
	return importer.ConnectivityJob{ID: jobID, Status: importer.ConnectivityJobRunning}, jobID == "job-1"
}

func (s *connectivityImportStub) CancelConnectivityJob(jobID string) (importer.ConnectivityJob, error) {
	return importer.ConnectivityJob{ID: jobID, Status: importer.ConnectivityJobCanceled}, nil
}

func (s *connectivityImportStub) ListConnectivityResults(query importer.ConnectivityResultQuery) (importer.ConnectivityResultPage, error) {
	s.query = query
	if s.page.Items != nil {
		return s.page, nil
	}
	return importer.ConnectivityResultPage{Page: query.Page, PageSize: query.PageSize}, nil
}

func (s *connectivityImportStub) ConnectivityHistory(jobID string) (importer.ConnectivityHistoryComparison, error) {
	result := s.history
	result.JobID = jobID
	return result, nil
}

func (s *connectivityImportStub) PreviewConnectivityPool(req importer.ConnectivityPortRequest) (importer.ConnectivityPortPreview, error) {
	s.request = req
	return importer.ConnectivityPortPreview{JobID: req.JobID, Tags: req.Tags, Targets: req.Targets, Qualifying: 3}, nil
}

func (s *connectivityImportStub) ApplyConnectivityPool(_ context.Context, req importer.ConnectivityPortRequest) (importer.ConnectivityPortApplyResponse, error) {
	s.request = req
	return importer.ConnectivityPortApplyResponse{PoolCount: 3}, nil
}

func TestConnectivityHandlersLifecycle(t *testing.T) {
	stub := &connectivityImportStub{}
	server := &Server{importSvc: stub}

	start := httptest.NewRecorder()
	server.handleConnectivityStart(start, httptest.NewRequest(http.MethodPost, "/api/connectivity/jobs/start", strings.NewReader(`{"tags":["A"],"timeout_seconds":23}`)))
	if start.Code != http.StatusOK || !strings.Contains(start.Body.String(), `"job_id":"job-1"`) || stub.startRequest.TimeoutSeconds != 23 {
		t.Fatalf("start status=%d body=%s", start.Code, start.Body.String())
	}

	status := httptest.NewRecorder()
	server.handleConnectivityStatus(status, httptest.NewRequest(http.MethodGet, "/api/connectivity/jobs/status?id=job-1", nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"status":"running"`) {
		t.Fatalf("status=%d body=%s", status.Code, status.Body.String())
	}

	results := httptest.NewRecorder()
	server.handleConnectivityResults(results, httptest.NewRequest(http.MethodGet, "/api/connectivity/results?id=job-1&tag=A&target=github&status=failed&page=2&page_size=25", nil))
	if results.Code != http.StatusOK || stub.query.JobID != "job-1" || stub.query.Tag != "A" || stub.query.TargetID != "github" || stub.query.Status != "failed" || stub.query.Page != 2 || stub.query.PageSize != 25 {
		t.Fatalf("results status=%d query=%#v", results.Code, stub.query)
	}

	history := httptest.NewRecorder()
	server.handleConnectivityHistory(history, httptest.NewRequest(http.MethodGet, "/api/connectivity/history?id=job-1", nil))
	if history.Code != http.StatusOK || !strings.Contains(history.Body.String(), `"job_id":"job-1"`) {
		t.Fatalf("history status=%d body=%s", history.Code, history.Body.String())
	}

	preview := httptest.NewRecorder()
	body := `{"job_id":"job-1","tags":["A"],"targets":["google","github"]}`
	server.handleConnectivityPortPreview(preview, httptest.NewRequest(http.MethodPost, "/api/connectivity/ports/preview", strings.NewReader(body)))
	if preview.Code != http.StatusOK || stub.request.JobID != "job-1" || len(stub.request.Targets) != 2 {
		t.Fatalf("preview status=%d request=%#v", preview.Code, stub.request)
	}

	apply := httptest.NewRecorder()
	server.handleConnectivityPortApply(apply, httptest.NewRequest(http.MethodPost, "/api/connectivity/ports/apply", strings.NewReader(body)))
	var applied importer.ConnectivityPortApplyResponse
	if err := json.Unmarshal(apply.Body.Bytes(), &applied); err != nil || apply.Code != http.StatusOK || applied.PoolCount != 3 {
		t.Fatalf("apply status=%d body=%s err=%v", apply.Code, apply.Body.String(), err)
	}

	cancel := httptest.NewRecorder()
	server.handleConnectivityCancel(cancel, httptest.NewRequest(http.MethodPost, "/api/connectivity/jobs/cancel?id=job-1", nil))
	if cancel.Code != http.StatusOK || !strings.Contains(cancel.Body.String(), `"status":"canceled"`) {
		t.Fatalf("cancel status=%d body=%s", cancel.Code, cancel.Body.String())
	}
}

func TestConnectivityWebUIIncludesTimeoutControl(t *testing.T) {
	data, err := embeddedFS.ReadFile("assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	for _, required := range []string{
		`CONNECTIVITY_DEFAULT_TIMEOUT_SECONDS=10`,
		`id="connectivityTimeout"`,
		`min="1" max="60" step="1"`,
		`timeout_seconds`,
		`单次超时`,
	} {
		if !strings.Contains(page, required) {
			t.Fatalf("WebUI missing %q", required)
		}
	}
}

func TestConnectivityStatusReturnsNotFound(t *testing.T) {
	server := &Server{importSvc: &connectivityImportStub{}}
	recorder := httptest.NewRecorder()
	server.handleConnectivityStatus(recorder, httptest.NewRequest(http.MethodGet, "/api/connectivity/jobs/status?id=missing", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestConnectivityResultsExposePartialComponents(t *testing.T) {
	stub := &connectivityImportStub{page: importer.ConnectivityResultPage{Items: []importer.ConnectivityResult{{
		TargetID: "fixture", Verdict: importer.ConnectivityVerdictPartial, Success: false, HTTPStatus: http.StatusOK,
		Components: []importer.ConnectivityComponentResult{
			{ID: "page", Name: "目标页面", Verdict: importer.ConnectivityVerdictUsable, Success: true, HTTPStatus: http.StatusOK},
			{ID: "resource", Name: "关键资源", Verdict: importer.ConnectivityVerdictFailed, FailureStage: "connect"},
		},
	}}, Total: 1, Page: 1, PageSize: 100}}
	server := &Server{importSvc: stub}
	recorder := httptest.NewRecorder()
	server.handleConnectivityResults(recorder, httptest.NewRequest(http.MethodGet, "/api/connectivity/results?id=job-1&status=partial", nil))
	if recorder.Code != http.StatusOK || stub.query.Status != "partial" || !strings.Contains(recorder.Body.String(), `"verdict":"partial"`) || !strings.Contains(recorder.Body.String(), `"id":"resource"`) {
		t.Fatalf("status=%d query=%#v body=%s", recorder.Code, stub.query, recorder.Body.String())
	}
}
