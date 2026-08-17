package monitor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"easy_proxies/internal/importer"
	"easy_proxies/internal/proxychain"
)

type chainProfileImportStub struct {
	ImportService
	profiles      []proxychain.Profile
	nodes         []importer.ManagedNode
	value         proxychain.Profile
	bindingReq    importer.TagBindingRequest
	bindingJob    importer.TagBindingJob
	bindingCancel string
}

func (s *chainProfileImportStub) ChainProfiles() []proxychain.Profile      { return s.profiles }
func (s *chainProfileImportStub) ListAll() ([]importer.ManagedNode, error) { return s.nodes, nil }
func (s *chainProfileImportStub) TestChainProfileValue(_ context.Context, profile proxychain.Profile) importer.ChainProbeResult {
	s.value = profile
	return importer.ChainProbeResult{ProfileID: profile.ID, ProfileName: profile.Name, LatencyMs: 12}
}
func (s *chainProfileImportStub) StartTagBinding(req importer.TagBindingRequest) (string, error) {
	s.bindingReq = req
	return "binding-1", nil
}
func (s *chainProfileImportStub) GetTagBindingJob(id string) (importer.TagBindingJob, bool) {
	if id != s.bindingJob.ID {
		return importer.TagBindingJob{}, false
	}
	return s.bindingJob, true
}
func (s *chainProfileImportStub) CancelTagBindingJob(id string) (importer.TagBindingJob, error) {
	s.bindingCancel = id
	return s.bindingJob, nil
}

func TestChainProfileGetIncludesUsage(t *testing.T) {
	stub := &chainProfileImportStub{
		profiles: []proxychain.Profile{{ID: "front", Name: "Front", Enabled: true, Hops: []proxychain.Hop{{URI: "socks5://127.0.0.1:1080"}}}},
		nodes: []importer.ManagedNode{
			{ID: "a", ChainProfileID: "front", State: importer.StateInPool, InPool: true, Port: 24000},
			{ID: "b", ChainProfileID: "front", State: importer.StateFailed},
		},
	}
	server := &Server{importSvc: stub}
	recorder := httptest.NewRecorder()
	server.handleChainProfiles(recorder, httptest.NewRequest(http.MethodGet, "/api/chain-profiles", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"nodes":2`) || !strings.Contains(recorder.Body.String(), `"ports":1`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestChainProfileDeleteInUseRequiresCascade(t *testing.T) {
	stub := &chainProfileImportStub{
		profiles: []proxychain.Profile{{ID: "front", Name: "Front", Enabled: true, Hops: []proxychain.Hop{{URI: "socks5://127.0.0.1:1080"}}}},
		nodes:    []importer.ManagedNode{{ID: "a", ChainProfileID: "front", State: importer.StateInPool, InPool: true, Port: 24000}},
	}
	server := &Server{importSvc: stub}
	recorder := httptest.NewRecorder()
	server.handleChainProfiles(recorder, httptest.NewRequest(http.MethodPut, "/api/chain-profiles", strings.NewReader(`{"profiles":[]}`)))
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), `"requires_cascade":true`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestChainProfileTestAcceptsUnsavedProfile(t *testing.T) {
	stub := &chainProfileImportStub{}
	server := &Server{importSvc: stub}
	recorder := httptest.NewRecorder()
	body := `{"profile":{"name":"Draft","enabled":true,"hops":[{"uri":"socks5://127.0.0.1:1080"}]}}`
	server.handleChainProfileTest(recorder, httptest.NewRequest(http.MethodPost, "/api/chain-profiles/test", strings.NewReader(body)))
	if recorder.Code != http.StatusOK || stub.value.Name != "Draft" || !strings.Contains(recorder.Body.String(), `"latency_ms":12`) {
		t.Fatalf("status=%d profile=%#v body=%s", recorder.Code, stub.value, recorder.Body.String())
	}
}

func TestTagBindingAPIStartsReadsAndCancelsJob(t *testing.T) {
	stub := &chainProfileImportStub{bindingJob: importer.TagBindingJob{ID: "binding-1", Status: "running", TotalTags: 2}}
	server := &Server{importSvc: stub}

	start := httptest.NewRecorder()
	server.handleImportAction(start, httptest.NewRequest(http.MethodPost, "/api/import/bindings", strings.NewReader(`{"tags":["A","B"],"chain_profile_id":"front","test_204":true}`)))
	if start.Code != http.StatusOK || !strings.Contains(start.Body.String(), `"job_id":"binding-1"`) {
		t.Fatalf("start status=%d body=%s", start.Code, start.Body.String())
	}
	if len(stub.bindingReq.Tags) != 2 || stub.bindingReq.ChainProfileID != "front" || stub.bindingReq.Test204 == nil || !*stub.bindingReq.Test204 {
		t.Fatalf("binding request = %#v", stub.bindingReq)
	}

	status := httptest.NewRecorder()
	server.handleImportAction(status, httptest.NewRequest(http.MethodGet, "/api/import/bindings/jobs/binding-1", nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"total_tags":2`) {
		t.Fatalf("status code=%d body=%s", status.Code, status.Body.String())
	}

	cancel := httptest.NewRecorder()
	server.handleImportAction(cancel, httptest.NewRequest(http.MethodDelete, "/api/import/bindings/jobs/binding-1", nil))
	if cancel.Code != http.StatusOK || stub.bindingCancel != "binding-1" {
		t.Fatalf("cancel code=%d id=%q body=%s", cancel.Code, stub.bindingCancel, cancel.Body.String())
	}
}
