package importer

import (
	"strconv"
	"testing"
	"time"
)

func TestCleanupExpiredJobsPreservesRunningAndRecent(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	s := &Service{
		testJobs: map[string]*TestJob{
			"expired": {Status: TestJobFinished, UpdatedAt: now.Add(-testJobTTL - time.Second)},
			"recent":  {Status: TestJobFinished, UpdatedAt: now.Add(-testJobTTL + time.Second)},
			"running": {Status: TestJobRunning, UpdatedAt: now.Add(-24 * time.Hour)},
		},
		refreshJobs: map[string]*SourceRefreshJob{
			"expired": {Status: SourceRefreshJobFinished, UpdatedAt: now.Add(-refreshJobTTL - time.Second)},
			"recent":  {Status: SourceRefreshJobFinished, UpdatedAt: now.Add(-refreshJobTTL + time.Second)},
			"running": {Status: SourceRefreshJobRunning, UpdatedAt: now.Add(-24 * time.Hour)},
		},
		connectivityJobs: map[string]*connectivityJobState{
			"expired": {job: ConnectivityJob{Status: ConnectivityJobFinished, UpdatedAt: now.Add(-connectivityJobTTL - time.Second)}},
			"recent":  {job: ConnectivityJob{Status: ConnectivityJobFinished, UpdatedAt: now.Add(-connectivityJobTTL + time.Second)}},
			"running": {job: ConnectivityJob{Status: ConnectivityJobRunning, UpdatedAt: now.Add(-24 * time.Hour)}},
		},
	}

	s.cleanupExpiredJobs(now)

	assertRetainedJobs(t, "test", s.testJobs)
	assertRetainedJobs(t, "refresh", s.refreshJobs)
	if _, ok := s.connectivityJobs["expired"]; ok {
		t.Fatal("expired connectivity job was retained")
	}
	if _, ok := s.connectivityJobs["recent"]; !ok {
		t.Fatal("recent connectivity job was removed")
	}
	if _, ok := s.connectivityJobs["running"]; !ok {
		t.Fatal("running connectivity job was removed")
	}
	stats := s.JobRetentionStats()
	if stats.TestRetained != 2 || stats.TestRunning != 1 || stats.RefreshRetained != 2 || stats.RefreshRunning != 1 || stats.ConnectivityRetained != 2 || stats.ConnectivityRunning != 1 {
		t.Fatalf("unexpected retention stats: %+v", stats)
	}
}

func TestCleanupExpiredJobsHandlesTenThousandResults(t *testing.T) {
	now := time.Now()
	largeResults := make(map[string]ConnectivityResult, 10_000)
	for i := 0; i < 10_000; i++ {
		largeResults[strconv.Itoa(i)] = ConnectivityResult{NodeID: strconv.Itoa(i)}
	}
	s := &Service{connectivityJobs: map[string]*connectivityJobState{
		"expired": {
			job:     ConnectivityJob{Status: ConnectivityJobFinished, UpdatedAt: now.Add(-connectivityJobTTL - time.Second)},
			results: largeResults,
		},
		"running": {
			job:     ConnectivityJob{Status: ConnectivityJobRunning, UpdatedAt: now.Add(-24 * time.Hour)},
			results: largeResults,
		},
	}}

	s.cleanupExpiredJobs(now)

	if _, ok := s.connectivityJobs["expired"]; ok {
		t.Fatal("expired job with 10,000 results was retained")
	}
	if running := s.connectivityJobs["running"]; running == nil || len(running.results) != 10_000 {
		t.Fatal("running job with 10,000 results was not preserved")
	}
}

func assertRetainedJobs[T any](t *testing.T, kind string, jobs map[string]*T) {
	t.Helper()
	if _, ok := jobs["expired"]; ok {
		t.Fatalf("expired %s job was retained", kind)
	}
	if _, ok := jobs["recent"]; !ok {
		t.Fatalf("recent %s job was removed", kind)
	}
	if _, ok := jobs["running"]; !ok {
		t.Fatalf("running %s job was removed", kind)
	}
}
