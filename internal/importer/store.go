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
	mu     sync.RWMutex
	saveMu sync.Mutex
	path   string
	nodes  map[string]ManagedNode
	jobs   map[string]ImportJob
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

func (s *Store) snapshotLocked() storeFile {
	nodes := make(map[string]ManagedNode, len(s.nodes))
	for k, v := range s.nodes {
		nodes[k] = v
	}
	jobs := make(map[string]ImportJob, len(s.jobs))
	for k, v := range s.jobs {
		jobs[k] = v
	}
	return storeFile{
		Version: storeVersion,
		Nodes:   nodes,
		Jobs:    jobs,
	}
}

func (s *Store) saveSnapshot(sf storeFile) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()

	data, err := json.MarshalIndent(sf, "", "\t")
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
	now := time.Now()
	for i := range nodes {
		if nodes[i].CreatedAt.IsZero() {
			nodes[i].CreatedAt = now
		}
		nodes[i].UpdatedAt = now
		s.nodes[nodes[i].ID] = nodes[i]
	}
	sf := s.snapshotLocked()
	s.mu.Unlock()
	return s.saveSnapshot(sf)
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
	n, ok := s.nodes[id]
	if !ok {
		s.mu.Unlock()
		return ManagedNode{}, fmt.Errorf("node %s not found", id)
	}
	n.State = state
	n.LastError = lastErr
	n.LastTestAt = time.Now()
	n.UpdatedAt = time.Now()
	s.nodes[id] = n
	sf := s.snapshotLocked()
	s.mu.Unlock()
	return n, s.saveSnapshot(sf)
}

func (s *Store) MarkInPool(id string, port uint16) (ManagedNode, error) {
	nodes, err := s.MarkInPoolMany(map[string]uint16{id: port})
	if err != nil {
		return ManagedNode{}, err
	}
	if len(nodes) == 0 {
		return ManagedNode{}, fmt.Errorf("node %s not found", id)
	}
	return nodes[0], nil
}

func (s *Store) MarkInPoolMany(ports map[string]uint16) ([]ManagedNode, error) {
	s.mu.Lock()
	now := time.Now()
	maxOrder := -1
	for _, existing := range s.nodes {
		if existing.InPool && existing.State == StateInPool && existing.Order > maxOrder {
			maxOrder = existing.Order
		}
	}

	updated := make([]ManagedNode, 0, len(ports))
	for id, port := range ports {
		n, ok := s.nodes[id]
		if !ok {
			continue
		}
		wasInPool := n.InPool && n.State == StateInPool
		n.State = StateInPool
		n.InPool = true
		n.Enabled = true
		n.Port = port
		if !wasInPool {
			maxOrder++
			n.Order = maxOrder
		}
		n.UpdatedAt = now
		s.nodes[id] = n
		updated = append(updated, n)
	}
	if len(updated) == 0 {
		s.mu.Unlock()
		return nil, nil
	}
	sf := s.snapshotLocked()
	s.mu.Unlock()
	return updated, s.saveSnapshot(sf)
}

func (s *Store) SetOrder(ids []string) error {
	s.mu.Lock()
	for order, id := range ids {
		if n, ok := s.nodes[id]; ok {
			n.Order = order
			n.UpdatedAt = time.Now()
			s.nodes[id] = n
		}
	}
	sf := s.snapshotLocked()
	s.mu.Unlock()
	return s.saveSnapshot(sf)
}

func (s *Store) UpsertJob(job ImportJob) error {
	s.mu.Lock()
	s.jobs[job.ID] = job
	sf := s.snapshotLocked()
	s.mu.Unlock()
	return s.saveSnapshot(sf)
}

func (s *Store) GetJob(id string) (ImportJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	return j, ok
}

func (s *Store) UpdateJob(id string, fn func(*ImportJob)) error {
	s.mu.Lock()
	j, ok := s.jobs[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("job %s not found", id)
	}
	fn(&j)
	s.jobs[id] = j
	sf := s.snapshotLocked()
	s.mu.Unlock()
	return s.saveSnapshot(sf)
}

func (s *Store) DeleteNode(id string) error {
	return s.DeleteNodes([]string{id})
}

func (s *Store) DeleteNodes(ids []string) error {
	s.mu.Lock()
	for _, id := range ids {
		delete(s.nodes, id)
	}
	sf := s.snapshotLocked()
	s.mu.Unlock()
	return s.saveSnapshot(sf)
}
