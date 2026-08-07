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
	profiles []proxychain.Profile
	nodes    []importer.ManagedNode
	value    proxychain.Profile
}

func (s *chainProfileImportStub) ChainProfiles() []proxychain.Profile      { return s.profiles }
func (s *chainProfileImportStub) ListAll() ([]importer.ManagedNode, error) { return s.nodes, nil }
func (s *chainProfileImportStub) TestChainProfileValue(_ context.Context, profile proxychain.Profile) importer.ChainProbeResult {
	s.value = profile
	return importer.ChainProbeResult{ProfileID: profile.ID, ProfileName: profile.Name, LatencyMs: 12}
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
