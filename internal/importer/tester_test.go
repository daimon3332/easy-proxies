package importer

import (
	"context"
	"errors"
	"reflect"
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
