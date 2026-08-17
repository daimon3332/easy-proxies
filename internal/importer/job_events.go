package importer

import (
	"context"
	"sync"
	"time"
)

type JobEvent struct {
	Kind         string            `json:"kind"`
	ID           string            `json:"id"`
	Test         *TestJob          `json:"test,omitempty"`
	Refresh      *SourceRefreshJob `json:"refresh,omitempty"`
	Connectivity *ConnectivityJob  `json:"connectivity,omitempty"`
}

type JobEventStats struct {
	Subscribers int    `json:"subscribers"`
	Coalesced   uint64 `json:"coalesced"`
}

type JobRetentionStats struct {
	TestRetained         int `json:"test_retained"`
	TestRunning          int `json:"test_running"`
	RefreshRetained      int `json:"refresh_retained"`
	RefreshRunning       int `json:"refresh_running"`
	ConnectivityRetained int `json:"connectivity_retained"`
	ConnectivityRunning  int `json:"connectivity_running"`
	TagBindingRetained   int `json:"tag_binding_retained"`
	TagBindingRunning    int `json:"tag_binding_running"`
}

func (s *Service) JobEventStats() JobEventStats {
	s.jobEventsMu.Lock()
	subscribers := len(s.jobEventSubs)
	s.jobEventsMu.Unlock()
	return JobEventStats{Subscribers: subscribers, Coalesced: s.jobEventCoalesced.Load()}
}

func (s *Service) JobRetentionStats() JobRetentionStats {
	var stats JobRetentionStats
	s.testJobsMu.RLock()
	stats.TestRetained = len(s.testJobs)
	for _, job := range s.testJobs {
		if job != nil && job.Status == TestJobRunning {
			stats.TestRunning++
		}
	}
	s.testJobsMu.RUnlock()

	s.refreshJobsMu.RLock()
	stats.RefreshRetained = len(s.refreshJobs)
	for _, job := range s.refreshJobs {
		if job != nil && job.Status == SourceRefreshJobRunning {
			stats.RefreshRunning++
		}
	}
	s.refreshJobsMu.RUnlock()

	s.connectivityJobsMu.RLock()
	stats.ConnectivityRetained = len(s.connectivityJobs)
	for _, state := range s.connectivityJobs {
		if state != nil && state.job.Status == ConnectivityJobRunning {
			stats.ConnectivityRunning++
		}
	}
	s.connectivityJobsMu.RUnlock()

	s.tagBindingJobsMu.RLock()
	stats.TagBindingRetained = len(s.tagBindingJobs)
	for _, job := range s.tagBindingJobs {
		if job != nil && job.Status == "running" {
			stats.TagBindingRunning++
		}
	}
	s.tagBindingJobsMu.RUnlock()
	return stats
}

func (s *Service) cleanupExpiredJobs(now time.Time) {
	s.testJobsMu.Lock()
	for id, job := range s.testJobs {
		if job != nil && job.Status != TestJobRunning && now.Sub(job.UpdatedAt) > testJobTTL {
			delete(s.testJobs, id)
		}
	}
	s.testJobsMu.Unlock()

	s.refreshJobsMu.Lock()
	for id, job := range s.refreshJobs {
		if job != nil && job.Status != SourceRefreshJobRunning && now.Sub(job.UpdatedAt) > refreshJobTTL {
			delete(s.refreshJobs, id)
		}
	}
	s.refreshJobsMu.Unlock()

	s.connectivityJobsMu.Lock()
	for id, state := range s.connectivityJobs {
		if state != nil && state.job.Status != ConnectivityJobRunning && now.Sub(state.job.UpdatedAt) > connectivityJobTTL {
			delete(s.connectivityJobs, id)
		}
	}
	s.connectivityJobsMu.Unlock()

	s.tagBindingJobsMu.Lock()
	for id, job := range s.tagBindingJobs {
		if job != nil && job.Status != "running" && now.Sub(job.UpdatedAt) > refreshJobTTL {
			delete(s.tagBindingJobs, id)
		}
	}
	s.tagBindingJobsMu.Unlock()
}

func (s *Service) runJobCleanup(ctx context.Context) {
	ticker := time.NewTicker(jobCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			s.cleanupExpiredJobs(now)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) SubscribeJobEvents() (<-chan JobEvent, func()) {
	ch := make(chan JobEvent, 1)
	s.jobEventsMu.Lock()
	if s.jobEventsClosed {
		close(ch)
		s.jobEventsMu.Unlock()
		return ch, func() {}
	}
	s.jobEventNext++
	id := s.jobEventNext
	s.jobEventSubs[id] = ch
	s.jobEventsMu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			s.jobEventsMu.Lock()
			delete(s.jobEventSubs, id)
			s.jobEventsMu.Unlock()
		})
	}
}

func (s *Service) JobEventSnapshot() []JobEvent {
	events := make([]JobEvent, 0)
	s.testJobsMu.RLock()
	for _, job := range s.testJobs {
		copyJob := *job
		events = append(events, JobEvent{Kind: "test", ID: job.ID, Test: &copyJob})
	}
	s.testJobsMu.RUnlock()
	s.refreshJobsMu.RLock()
	for _, job := range s.refreshJobs {
		copyJob := cloneRefreshJob(job)
		events = append(events, JobEvent{Kind: "refresh", ID: job.ID, Refresh: &copyJob})
	}
	s.refreshJobsMu.RUnlock()
	s.connectivityJobsMu.RLock()
	for _, state := range s.connectivityJobs {
		job := connectivityJobSnapshot(state)
		events = append(events, JobEvent{Kind: "connectivity", ID: job.ID, Connectivity: &job})
	}
	s.connectivityJobsMu.RUnlock()
	return events
}

func (s *Service) publishJobEvent(event JobEvent) {
	s.jobEventsMu.Lock()
	defer s.jobEventsMu.Unlock()
	if s.jobEventsClosed {
		return
	}
	for _, ch := range s.jobEventSubs {
		select {
		case ch <- event:
		default:
			select {
			case <-ch:
				s.jobEventCoalesced.Add(1)
			default:
			}
			select {
			case ch <- event:
			default:
			}
		}
	}
}

func (s *Service) closeJobEvents() {
	s.jobEventsMu.Lock()
	if !s.jobEventsClosed {
		s.jobEventsClosed = true
		for id, ch := range s.jobEventSubs {
			close(ch)
			delete(s.jobEventSubs, id)
		}
	}
	s.jobEventsMu.Unlock()
}

func cloneRefreshJob(job *SourceRefreshJob) SourceRefreshJob {
	copyJob := *job
	copyJob.Groups = make([]SourceRefreshGroup, len(job.Groups))
	for i, group := range job.Groups {
		copyJob.Groups[i] = group
		copyJob.Groups[i].URLs = append([]SourceRefreshURL(nil), group.URLs...)
		for j := range copyJob.Groups[i].URLs {
			copyJob.Groups[i].URLs[j].ChainProbe = cloneChainProbe(group.URLs[j].ChainProbe)
			copyJob.Groups[i].URLs[j].SiteProgress = append([]SiteTestProgress(nil), group.URLs[j].SiteProgress...)
		}
	}
	copyJob.SiteTargets = append([]string(nil), job.SiteTargets...)
	return copyJob
}

func cloneChainProbe(probe *ChainProbeResult) *ChainProbeResult {
	if probe == nil {
		return nil
	}
	copyProbe := *probe
	return &copyProbe
}
