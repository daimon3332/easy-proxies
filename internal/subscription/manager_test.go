package subscription

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"easy_proxies/internal/config"
	"easy_proxies/internal/importer"
)

type fakeSourceRefresher struct {
	job      importer.SourceRefreshJob
	startErr error
	starts   atomic.Int32
}

func (f *fakeSourceRefresher) StartRefreshSources(string) (string, error) {
	f.starts.Add(1)
	return f.job.ID, f.startErr
}

func (f *fakeSourceRefresher) GetRefreshJob(string) (importer.SourceRefreshJob, bool) {
	return f.job, f.job.ID != ""
}

func TestFetchSubscriptionsBoundedAndOrdered(t *testing.T) {
	urls := []string{"first", "second", "third", "fourth", "fifth"}
	var active atomic.Int32
	var peak atomic.Int32
	results := fetchSubscriptions(context.Background(), urls, 2, func(_ context.Context, rawURL string) ([]config.NodeConfig, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			observed := peak.Load()
			if current <= observed || peak.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		return []config.NodeConfig{{Name: rawURL}}, nil
	})

	if got := peak.Load(); got != 2 {
		t.Fatalf("peak concurrency = %d, want 2", got)
	}
	for i, result := range results {
		if result.err != nil {
			t.Fatalf("result %d error = %v", i, result.err)
		}
		if len(result.nodes) != 1 || result.nodes[0].Name != urls[i] {
			t.Fatalf("result %d = %#v, want source %q", i, result.nodes, urls[i])
		}
	}
}

func TestFetchSubscriptionsPreservesErrors(t *testing.T) {
	urls := []string{"first", "second", "third"}
	results := fetchSubscriptions(context.Background(), urls, 3, func(_ context.Context, rawURL string) ([]config.NodeConfig, error) {
		if rawURL == "second" {
			return nil, fmt.Errorf("fetch failed")
		}
		return []config.NodeConfig{{Name: rawURL}}, nil
	})

	if results[0].err != nil || results[2].err != nil {
		t.Fatalf("successful results contain errors: %#v", results)
	}
	if results[1].err == nil {
		t.Fatal("failed result has no error")
	}
}

func TestManagerDelegatesRefreshToManagedSources(t *testing.T) {
	manager := New(&config.Config{}, nil)
	defer manager.Stop()
	refresher := &fakeSourceRefresher{job: importer.SourceRefreshJob{
		ID:     "refresh-job",
		Status: importer.SourceRefreshJobFinished,
		Passed: 17,
	}}
	manager.SetSourceRefresher(refresher)

	manager.doRefresh()

	status := manager.Status()
	if refresher.starts.Load() != 1 {
		t.Fatalf("managed refresh starts = %d, want 1", refresher.starts.Load())
	}
	if status.RefreshCount != 1 || status.NodeCount != 17 || status.LastError != "" {
		t.Fatalf("status = %#v", status)
	}
}

func TestManagerReportsManagedRefreshFailure(t *testing.T) {
	manager := New(&config.Config{}, nil)
	defer manager.Stop()
	refresher := &fakeSourceRefresher{job: importer.SourceRefreshJob{
		ID:     "refresh-job",
		Status: importer.SourceRefreshJobFailed,
		Error:  "test failure",
	}}
	manager.SetSourceRefresher(refresher)

	manager.doRefresh()

	status := manager.Status()
	if status.LastError == "" {
		t.Fatal("managed refresh failure was not reported")
	}
}
