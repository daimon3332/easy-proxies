package importer

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sagernet/sing-box/option"
)

func TestNormalizeProbeURL(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   string
	}{
		{name: "empty default", target: "", want: "https://www.gstatic.com/generate_204"},
		{name: "host port", target: "www.apple.com:80", want: "http://www.apple.com:80/generate_204"},
		{name: "host only", target: "cp.cloudflare.com", want: "http://cp.cloudflare.com/generate_204"},
		{name: "http full path", target: "http://cp.cloudflare.com/generate_204", want: "http://cp.cloudflare.com/generate_204"},
		{name: "https full path", target: "https://cp.cloudflare.com/generate_204", want: "https://cp.cloudflare.com/generate_204"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeProbeURL(tt.target)
			if err != nil {
				t.Fatalf("normalizeProbeURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeProbeURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProbeBatchDefersFailuresAcrossThreeRounds(t *testing.T) {
	tester := NewNodeTester(nil, WithTesterConcurrency(8))
	tester.retryDelay = 10 * time.Millisecond
	var mu sync.Mutex
	attempts := make(map[string]int)
	times := make(map[string][]time.Time)
	tester.probeOverride = func(_ context.Context, node ManagedNode, _ string, _ time.Duration) TestResult {
		mu.Lock()
		attempts[node.ID]++
		attempt := attempts[node.ID]
		times[node.ID] = append(times[node.ID], time.Now())
		mu.Unlock()
		if node.ID == "first" || node.ID == "second" && attempt == 2 {
			return TestResult{LatencyMs: int64(attempt)}
		}
		return TestResult{Error: errors.New("fixture failure")}
	}
	nodes := []ManagedNode{{ID: "first"}, {ID: "second"}, {ID: "failed"}}
	progress := make([]ProbeRoundProgress, 0, 3)
	results := make(map[string]TestResult)
	for event := range tester.ProbeBatchWithProgress(context.Background(), nodes, func(round ProbeRoundProgress) {
		progress = append(progress, round)
	}) {
		results[event.NodeID] = event.Result
	}

	if !reflect.DeepEqual(attempts, map[string]int{"first": 1, "second": 2, "failed": 3}) {
		t.Fatalf("attempts = %#v", attempts)
	}
	if results["first"].Error != nil || results["second"].Error != nil || results["failed"].Error == nil {
		t.Fatalf("results = %#v", results)
	}
	starts := make([]ProbeRoundProgress, 0, probeRounds)
	completed := make(map[int]ProbeRoundProgress, probeRounds)
	for _, item := range progress {
		if item.Completed == 0 {
			starts = append(starts, item)
		}
		if item.Pending == 0 {
			completed[item.Round] = item
		}
	}
	if len(starts) != 3 || starts[0].Pending != 3 || starts[1].Pending != 2 || starts[2].Pending != 1 {
		t.Fatalf("progress starts = %#v", starts)
	}
	if starts[0].Target != DefaultProbeTarget || starts[1].Target != AlternateProbeTarget || starts[2].Target != AlternateProbeTarget {
		t.Fatalf("targets = %#v", starts)
	}
	if starts[0].Concurrency != 8 || starts[1].Concurrency != 4 || starts[2].Concurrency != 2 {
		t.Fatalf("concurrency = %#v", starts)
	}
	for round, total := range map[int]int{1: 3, 2: 2, 3: 1} {
		item, ok := completed[round]
		if !ok || item.Completed != total || item.Total != total {
			t.Fatalf("round %d completion = %#v", round, item)
		}
	}
	failedTimes := times["failed"]
	if len(failedTimes) != 3 || failedTimes[1].Sub(failedTimes[0]) < tester.retryDelay || failedTimes[2].Sub(failedTimes[1]) < tester.retryDelay {
		t.Fatalf("retry times = %#v", failedTimes)
	}
}

type countingProbeSession struct {
	mu       sync.Mutex
	attempts int
	closed   int
}

func (s *countingProbeSession) Probe(_ context.Context, _ string) TestResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts++
	return TestResult{Error: errors.New("fixture failure")}
}

func (s *countingProbeSession) Close() {
	s.mu.Lock()
	s.closed++
	s.mu.Unlock()
}

func TestProbeBatchReusesOneSessionAcrossRetryRounds(t *testing.T) {
	tester := NewNodeTester(nil, WithTesterConcurrency(4))
	tester.retryDelay = time.Millisecond
	session := &countingProbeSession{}
	created := 0
	tester.sessionFactory = func(_ context.Context, _ ManagedNode, _ time.Duration) (probeSession, error) {
		created++
		return session, nil
	}

	var result TestResult
	for event := range tester.ProbeBatch(context.Background(), []ManagedNode{{ID: "node-1"}}) {
		result = event.Result
	}
	if result.Error == nil {
		t.Fatal("expected final probe failure")
	}
	if created != 1 {
		t.Fatalf("session creations = %d, want 1", created)
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.attempts != probeRounds {
		t.Fatalf("session attempts = %d, want %d", session.attempts, probeRounds)
	}
	if session.closed != 1 {
		t.Fatalf("session closes = %d, want 1", session.closed)
	}
}

func TestProbeBatchDoesNotShareSessionAcrossDifferentURIs(t *testing.T) {
	tester := NewNodeTester(nil, WithTesterConcurrency(1))
	created := 0
	tester.sessionFactory = func(_ context.Context, _ ManagedNode, _ time.Duration) (probeSession, error) {
		created++
		return &successfulProbeSession{}, nil
	}
	nodes := []ManagedNode{
		{ID: "same-id", URI: "ss://first"},
		{ID: "same-id", URI: "ss://second"},
	}
	for range tester.ProbeBatch(context.Background(), nodes) {
	}
	if created != len(nodes) {
		t.Fatalf("session creations = %d, want %d", created, len(nodes))
	}
}

func TestProbeBatchRejectsPooledNodeWhenRuntimeListenerFails(t *testing.T) {
	var requests int
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer proxy.Close()
	host, port := testServerAddress(t, proxy.Listener.Addr())

	tester := NewNodeTester(nil,
		WithProbeTarget("https://example.test/generate_204"),
		WithRuntimeProxy(host, "", ""),
	)
	tester.retryDelay = time.Millisecond
	tester.sessionFactory = func(context.Context, ManagedNode, time.Duration) (probeSession, error) {
		return &successfulProbeSession{}, nil
	}

	var result TestResult
	for event := range tester.ProbeBatch(context.Background(), []ManagedNode{{
		ID: "pooled", URI: "ss://pooled", Port: port, State: StateInPool, InPool: true,
	}}) {
		result = event.Result
	}
	if result.Error == nil || !strings.Contains(result.Error.Error(), "runtime listener") {
		t.Fatalf("result error = %v, want runtime listener failure", result.Error)
	}
	if requests != probeRounds {
		t.Fatalf("runtime listener requests = %d, want %d", requests, probeRounds)
	}
}

func TestProbeBatchCandidateDoesNotRequireRuntimeListener(t *testing.T) {
	tester := NewNodeTester(nil, WithRuntimeProxy("127.0.0.1", "", ""))
	tester.sessionFactory = func(context.Context, ManagedNode, time.Duration) (probeSession, error) {
		return &successfulProbeSession{}, nil
	}

	var result TestResult
	for event := range tester.ProbeBatch(context.Background(), []ManagedNode{{
		ID: "candidate", URI: "ss://candidate", State: StateParsed,
	}}) {
		result = event.Result
	}
	if result.Error != nil {
		t.Fatalf("candidate probe error = %v", result.Error)
	}
}

func TestProbeBatchAcceptsPooledNodeThroughAuthenticatedRuntimeListener(t *testing.T) {
	const username = "proxy-user"
	const password = "proxy-password"
	var authorization string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Proxy-Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()
	host, port := testServerAddress(t, proxy.Listener.Addr())

	tester := NewNodeTester(nil,
		WithProbeTarget("http://example.test/generate_204"),
		WithRuntimeProxy(host, username, password),
	)
	tester.sessionFactory = func(context.Context, ManagedNode, time.Duration) (probeSession, error) {
		return &successfulProbeSession{}, nil
	}

	var result TestResult
	for event := range tester.ProbeBatch(context.Background(), []ManagedNode{{
		ID: "pooled", URI: "ss://pooled", Port: port, State: StateInPool, InPool: true,
	}}) {
		result = event.Result
	}
	if result.Error != nil {
		t.Fatalf("pooled probe error = %v", result.Error)
	}
	wantAuthorization := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
	if authorization != wantAuthorization {
		t.Fatalf("proxy authorization = %q, want %q", authorization, wantAuthorization)
	}
}

func testServerAddress(t *testing.T, address net.Addr) (string, uint16) {
	t.Helper()
	host, rawPort, err := net.SplitHostPort(address.String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.ParseUint(rawPort, 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	return host, uint16(port)
}

type successfulProbeSession struct{}

func (*successfulProbeSession) Probe(context.Context, string) TestResult { return TestResult{} }
func (*successfulProbeSession) Close()                                   {}

func TestRunBatchUsesBoundedWorkers(t *testing.T) {
	const concurrency = 4
	tester := NewNodeTester(nil, WithTesterConcurrency(concurrency))
	nodes := make([]ManagedNode, 100)
	for i := range nodes {
		nodes[i].ID = randomHex(12)
	}
	var mu sync.Mutex
	active := 0
	peak := 0
	fn := func(_ context.Context, _ ManagedNode) TestResult {
		mu.Lock()
		active++
		if active > peak {
			peak = active
		}
		mu.Unlock()
		time.Sleep(time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		return TestResult{}
	}
	count := 0
	for range tester.runBatchWithConcurrency(context.Background(), nodes, concurrency, time.Second, fn) {
		count++
	}
	if count != len(nodes) {
		t.Fatalf("results = %d, want %d", count, len(nodes))
	}
	if peak > concurrency {
		t.Fatalf("peak workers = %d, max %d", peak, concurrency)
	}
}

func TestProbeDefaultsKeepFullBatchFast(t *testing.T) {
	tester := NewNodeTester(nil)
	if tester.concurrency != 32 {
		t.Fatalf("concurrency = %d", tester.concurrency)
	}
	for round := 1; round <= probeRounds; round++ {
		if timeout := probeRoundTimeout(DefaultProbeTimeout, round); timeout != DefaultProbeTimeout {
			t.Fatalf("round %d timeout = %s", round, timeout)
		}
	}
	if concurrency := probeRoundConcurrency(tester.concurrency, 3, tester.concurrency*4); concurrency != tester.concurrency {
		t.Fatalf("large retry queue concurrency = %d", concurrency)
	}
}

func TestNodeTesterRecoverPanic(t *testing.T) {
	tester := NewNodeTester(func(tag, uri string, skipCertVerify bool) (option.Outbound, error) {
		panic("bad outbound")
	})

	result := tester.Probe(context.Background(), ManagedNode{ID: "node-1", URI: "vless://example"})
	if result.Error == nil {
		t.Fatal("expected panic to be converted to an error")
	}
	if !strings.Contains(result.Error.Error(), "node test panic: bad outbound") {
		t.Fatalf("unexpected error: %v", result.Error)
	}
}
