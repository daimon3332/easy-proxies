package importer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const storeVersion = 1

type storeFile struct {
	Version int                    `json:"version"`
	Nodes   map[string]ManagedNode `json:"nodes"`
	Jobs    map[string]ImportJob   `json:"jobs"`
}

type Store struct {
	mu    sync.RWMutex
	path  string
	nodes map[string]ManagedNode
	jobs  map[string]ImportJob
}

func NewStore(path string) (*Store, error) {
	s := &Store{
		path:  path,
		nodes: make(map[string]ManagedNode),
		jobs:  make(map[string]ImportJob),
	}
	if err := s.Load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load store: %w", err)
	}
	return s, nil
}

func (s *Store) Load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var sf storeFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return fmt.Errorf("decode store: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sf.Nodes != nil {
		s.nodes = sf.Nodes
	}
	if sf.Jobs != nil {
		s.jobs = sf.Jobs
	}
	return nil
}

func (s *Store) saveLocked() error {
	nodes := make(map[string]ManagedNode, len(s.nodes))
	for k, v := range s.nodes {
		nodes[k] = v
	}
	jobs := make(map[string]ImportJob, len(s.jobs))
	for k, v := range s.jobs {
		jobs[k] = v
	}
	sf := storeFile{
		Version: storeVersion,
		Nodes:   nodes,
		Jobs:    jobs,
	}
	// Release lock before I/O to avoid blocking readers during disk write
	s.mu.Unlock()

	data, err := json.MarshalIndent(sf, "", "\t")
	// Re-acquire lock before returning
	s.mu.Lock()
	if err != nil {
		return err
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(s.path)
		os.Rename(tmp, s.path)
	}
	return nil
}

func (s *Store) UpsertNode(node ManagedNode) error {
	return s.UpsertNodes([]ManagedNode{node})
}

func (s *Store) UpsertNodes(nodes []ManagedNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for i := range nodes {
		if nodes[i].CreatedAt.IsZero() {
			nodes[i].CreatedAt = now
		}
		nodes[i].UpdatedAt = now
		s.nodes[nodes[i].ID] = nodes[i]
	}
	return s.saveLocked()
}

func (s *Store) GetNode(id string) (ManagedNode, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.nodes[id]
	return n, ok
}

func (s *Store) ListNodes() []ManagedNode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]ManagedNode, 0, len(s.nodes))
	for _, n := range s.nodes {
		result = append(result, n)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Order == result[j].Order {
			return result[i].ID < result[j].ID
		}
		return result[i].Order < result[j].Order
	})
	return result
}

func (s *Store) ListPoolNodes() []ManagedNode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []ManagedNode
	for _, n := range s.nodes {
		if n.InPool && n.State == StateInPool {
			result = append(result, n)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Order < result[j].Order })
	return result
}

func (s *Store) ListFailedNodes() []ManagedNode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []ManagedNode
	for _, n := range s.nodes {
		if n.State == StateFailed {
			result = append(result, n)
		}
	}
	return result
}

func (s *Store) UpdateNodeState(id string, state ManagedNodeState, lastErr string) (ManagedNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.nodes[id]
	if !ok {
		return ManagedNode{}, fmt.Errorf("node %s not found", id)
	}
	n.State = state
	n.LastError = lastErr
	n.LastTestAt = time.Now()
	n.UpdatedAt = time.Now()
	s.nodes[id] = n
	return n, s.saveLocked()
}

func (s *Store) MarkInPool(id string, port uint16) (ManagedNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.nodes[id]
	if !ok {
		return ManagedNode{}, fmt.Errorf("node %s not found", id)
	}
	wasInPool := n.InPool && n.State == StateInPool
	n.State = StateInPool
	n.InPool = true
	n.Enabled = true
	n.Port = port
	if !wasInPool {
		maxOrder := -1
		for _, existing := range s.nodes {
			if existing.InPool && existing.State == StateInPool && existing.Order > maxOrder {
				maxOrder = existing.Order
			}
		}
		n.Order = maxOrder + 1
	}
	n.UpdatedAt = time.Now()
	s.nodes[id] = n
	return n, s.saveLocked()
}

func (s *Store) SetOrder(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for order, id := range ids {
		if n, ok := s.nodes[id]; ok {
			n.Order = order
			n.UpdatedAt = time.Now()
			s.nodes[id] = n
		}
	}
	return s.saveLocked()
}

func (s *Store) UpsertJob(job ImportJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = job
	return s.saveLocked()
}

func (s *Store) GetJob(id string) (ImportJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	return j, ok
}

func (s *Store) UpdateJob(id string, fn func(*ImportJob)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return fmt.Errorf("job %s not found", id)
	}
	fn(&j)
	s.jobs[id] = j
	return s.saveLocked()
}

func (s *Store) DeleteNode(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.nodes, id)
	return s.saveLocked()
}
