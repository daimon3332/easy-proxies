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

type StoreSnapshot struct {
	Version int                    `json:"version"`
	Nodes   map[string]ManagedNode `json:"nodes"`
}

type storeFile struct {
	Version int                    `json:"version"`
	Nodes   map[string]ManagedNode `json:"nodes"`
	Jobs    map[string]ImportJob   `json:"jobs"`
}

type Store struct {
	mu         sync.RWMutex
	mutationMu sync.Mutex
	saveMu     sync.Mutex
	path       string
	nodes      map[string]ManagedNode
	jobs       map[string]ImportJob
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
		if removeErr := os.Remove(s.path); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("replace store: %w", err)
		}
		if retryErr := os.Rename(tmp, s.path); retryErr != nil {
			return fmt.Errorf("replace store: %w", retryErr)
		}
	}
	return nil
}

func (s *Store) UpsertNode(node ManagedNode) error {
	return s.UpsertNodes([]ManagedNode{node})
}

func (s *Store) UpsertNodes(nodes []ManagedNode) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	s.mu.Lock()
	now := time.Now()
	previous := make(map[string]ManagedNode, len(nodes))
	existed := make(map[string]bool, len(nodes))
	for i := range nodes {
		previous[nodes[i].ID], existed[nodes[i].ID] = s.nodes[nodes[i].ID]
		if nodes[i].CreatedAt.IsZero() {
			nodes[i].CreatedAt = now
		}
		nodes[i].UpdatedAt = now
		s.nodes[nodes[i].ID] = nodes[i]
	}
	sf := s.snapshotLocked()
	s.mu.Unlock()
	if err := s.saveSnapshot(sf); err != nil {
		s.mu.Lock()
		for id, node := range previous {
			if existed[id] {
				s.nodes[id] = node
			} else {
				delete(s.nodes, id)
			}
		}
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *Store) RestoreNodeStatesIfCurrent(states map[string]ManagedNodeState, current ManagedNodeState) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	s.mu.Lock()
	now := time.Now()
	changed := false
	previous := make(map[string]ManagedNode, len(states))
	for id, state := range states {
		node, ok := s.nodes[id]
		if !ok || node.State != current {
			continue
		}
		previous[id] = node
		node.State = state
		node.UpdatedAt = now
		s.nodes[id] = node
		changed = true
	}
	if !changed {
		s.mu.Unlock()
		return nil
	}
	snapshot := s.snapshotLocked()
	s.mu.Unlock()
	if err := s.saveSnapshot(snapshot); err != nil {
		s.mu.Lock()
		for id, node := range previous {
			s.nodes[id] = node
		}
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *Store) RecoverStaleTestingNodes() (int, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	s.mu.Lock()
	now := time.Now()
	recovered := 0
	previous := make(map[string]ManagedNode)
	for id, node := range s.nodes {
		if node.State != StateTesting {
			continue
		}
		previous[id] = node
		switch {
		case node.InPool:
			node.State = StateInPool
		case !node.Enabled:
			node.State = StateExcluded
		case node.LastTestAt.IsZero():
			node.State = StateParsed
		case node.LastError != "":
			node.State = StateFailed
		default:
			node.State = StatePassed
		}
		node.UpdatedAt = now
		s.nodes[id] = node
		recovered++
	}
	if recovered == 0 {
		s.mu.Unlock()
		return 0, nil
	}
	snapshot := s.snapshotLocked()
	s.mu.Unlock()
	if err := s.saveSnapshot(snapshot); err != nil {
		s.mu.Lock()
		for id, node := range previous {
			s.nodes[id] = node
		}
		s.mu.Unlock()
		return 0, err
	}
	return recovered, nil
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

func (s *Store) BackupSnapshot() StoreSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	nodes := make(map[string]ManagedNode, len(s.nodes))
	for id, node := range s.nodes {
		nodes[id] = node
	}
	return StoreSnapshot{Version: storeVersion, Nodes: nodes}
}

func (s *Store) RestoreNodesSnapshot(snapshot StoreSnapshot) error {
	if snapshot.Version != storeVersion {
		return fmt.Errorf("unsupported managed nodes version %d", snapshot.Version)
	}
	nodes := make(map[string]ManagedNode, len(snapshot.Nodes))
	for id, node := range snapshot.Nodes {
		nodes[id] = node
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	s.mu.Lock()
	previous := s.nodes
	s.nodes = nodes
	fileSnapshot := s.snapshotLocked()
	s.mu.Unlock()
	if err := s.saveSnapshot(fileSnapshot); err != nil {
		s.mu.Lock()
		s.nodes = previous
		s.mu.Unlock()
		return err
	}
	return nil
}

func DecodeStoreSnapshot(data []byte) (StoreSnapshot, error) {
	var snapshot StoreSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return StoreSnapshot{}, fmt.Errorf("decode managed nodes: %w", err)
	}
	if snapshot.Version != storeVersion {
		return StoreSnapshot{}, fmt.Errorf("unsupported managed nodes version %d", snapshot.Version)
	}
	if snapshot.Nodes == nil {
		snapshot.Nodes = make(map[string]ManagedNode)
	}
	for id, node := range snapshot.Nodes {
		if id == "" || node.ID == "" || id != node.ID || node.URI == "" {
			return StoreSnapshot{}, fmt.Errorf("invalid managed node %q", id)
		}
	}
	return snapshot, nil
}

func (s *Store) ReplaceSnapshot(snapshot StoreSnapshot) error {
	if snapshot.Version != storeVersion {
		return fmt.Errorf("unsupported managed nodes version %d", snapshot.Version)
	}
	nodes := make(map[string]ManagedNode, len(snapshot.Nodes))
	for id, node := range snapshot.Nodes {
		nodes[id] = node
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	s.mu.Lock()
	previousNodes := s.nodes
	previousJobs := s.jobs
	s.nodes = nodes
	s.jobs = make(map[string]ImportJob)
	sf := s.snapshotLocked()
	s.mu.Unlock()
	if err := s.saveSnapshot(sf); err != nil {
		s.mu.Lock()
		s.nodes = previousNodes
		s.jobs = previousJobs
		s.mu.Unlock()
		return err
	}
	return nil
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
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	s.mu.Lock()
	n, ok := s.nodes[id]
	if !ok {
		s.mu.Unlock()
		return ManagedNode{}, fmt.Errorf("node %s not found", id)
	}
	previous := n
	n.State = state
	n.LastError = lastErr
	n.LastTestAt = time.Now()
	n.UpdatedAt = time.Now()
	s.nodes[id] = n
	sf := s.snapshotLocked()
	s.mu.Unlock()
	if err := s.saveSnapshot(sf); err != nil {
		s.mu.Lock()
		s.nodes[id] = previous
		s.mu.Unlock()
		return ManagedNode{}, err
	}
	return n, nil
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
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	s.mu.Lock()
	now := time.Now()
	maxOrder := -1
	for _, existing := range s.nodes {
		if existing.InPool && existing.State == StateInPool && existing.Order > maxOrder {
			maxOrder = existing.Order
		}
	}

	updated := make([]ManagedNode, 0, len(ports))
	previous := make(map[string]ManagedNode, len(ports))
	for id, port := range ports {
		n, ok := s.nodes[id]
		if !ok {
			continue
		}
		previous[id] = n
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
	if err := s.saveSnapshot(sf); err != nil {
		s.mu.Lock()
		for id, node := range previous {
			s.nodes[id] = node
		}
		s.mu.Unlock()
		return nil, err
	}
	return updated, nil
}

func (s *Store) SetOrder(ids []string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	s.mu.Lock()
	previous := make(map[string]ManagedNode, len(ids))
	for order, id := range ids {
		if n, ok := s.nodes[id]; ok {
			previous[id] = n
			n.Order = order
			n.UpdatedAt = time.Now()
			s.nodes[id] = n
		}
	}
	sf := s.snapshotLocked()
	s.mu.Unlock()
	if err := s.saveSnapshot(sf); err != nil {
		s.mu.Lock()
		for id, node := range previous {
			s.nodes[id] = node
		}
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *Store) UpsertJob(job ImportJob) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	s.mu.Lock()
	previous, existed := s.jobs[job.ID]
	s.jobs[job.ID] = job
	sf := s.snapshotLocked()
	s.mu.Unlock()
	if err := s.saveSnapshot(sf); err != nil {
		s.mu.Lock()
		if existed {
			s.jobs[job.ID] = previous
		} else {
			delete(s.jobs, job.ID)
		}
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *Store) GetJob(id string) (ImportJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	return j, ok
}

func (s *Store) UpdateJob(id string, fn func(*ImportJob)) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	s.mu.Lock()
	j, ok := s.jobs[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("job %s not found", id)
	}
	previous := j
	fn(&j)
	s.jobs[id] = j
	sf := s.snapshotLocked()
	s.mu.Unlock()
	if err := s.saveSnapshot(sf); err != nil {
		s.mu.Lock()
		s.jobs[id] = previous
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *Store) DeleteNode(id string) error {
	return s.DeleteNodes([]string{id})
}

func (s *Store) DeleteNodes(ids []string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	s.mu.Lock()
	previous := make(map[string]ManagedNode, len(ids))
	existed := make(map[string]bool, len(ids))
	for _, id := range ids {
		previous[id], existed[id] = s.nodes[id]
		delete(s.nodes, id)
	}
	sf := s.snapshotLocked()
	s.mu.Unlock()
	if err := s.saveSnapshot(sf); err != nil {
		s.mu.Lock()
		for id, node := range previous {
			if existed[id] {
				s.nodes[id] = node
			}
		}
		s.mu.Unlock()
		return err
	}
	return nil
}
