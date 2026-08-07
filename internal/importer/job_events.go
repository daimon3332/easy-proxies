package importer

import "sync"

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

func (s *Service) JobEventStats() JobEventStats {
	s.jobEventsMu.Lock()
	subscribers := len(s.jobEventSubs)
	s.jobEventsMu.Unlock()
	return JobEventStats{Subscribers: subscribers, Coalesced: s.jobEventCoalesced.Load()}
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
		}
	}
	return copyJob
}

func cloneChainProbe(probe *ChainProbeResult) *ChainProbeResult {
	if probe == nil {
		return nil
	}
	copyProbe := *probe
	return &copyProbe
}
