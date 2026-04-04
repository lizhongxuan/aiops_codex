package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// LabEnvironmentStore provides thread-safe, JSON-persisted storage for
// lab environments.
type LabEnvironmentStore struct {
	mu      sync.RWMutex
	records map[string]model.LabEnvironment // key: id
	path    string

	// hostStore is used to register/remove mock hosts when environments
	// are started or stopped.
	hostStore *Store
}

// NewLabEnvironmentStore creates a new store that persists to the given path.
func NewLabEnvironmentStore(path string, hostStore *Store) *LabEnvironmentStore {
	return &LabEnvironmentStore{
		records:   make(map[string]model.LabEnvironment),
		path:      path,
		hostStore: hostStore,
	}
}

// Load restores records from the JSON file on disk. If the file does not
// exist the store starts empty (no error).
func (s *LabEnvironmentStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.path == "" {
		return nil
	}
	content, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var records []model.LabEnvironment
	if err := json.Unmarshal(content, &records); err != nil {
		return err
	}
	s.records = make(map[string]model.LabEnvironment, len(records))
	for _, r := range records {
		s.records[r.ID] = r
	}
	return nil
}

// persist writes the current records to disk as indented JSON, using an
// atomic write-then-rename pattern.
func (s *LabEnvironmentStore) persist() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	records := make([]model.LabEnvironment, 0, len(s.records))
	for _, r := range s.records {
		records = append(records, r)
	}
	content, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, content, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

// Add creates a new lab environment and persists to disk.
func (s *LabEnvironmentStore) Add(record model.LabEnvironment) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if record.ID == "" {
		return errors.New("lab environment ID is required")
	}
	if _, exists := s.records[record.ID]; exists {
		return errors.New("lab environment already exists")
	}
	if record.CreatedAt == "" {
		record.CreatedAt = model.NowString()
	}
	if record.Status == "" {
		record.Status = "draft"
	}
	record.UpdatedAt = record.CreatedAt
	s.records[record.ID] = record
	return s.persist()
}

// Get returns the lab environment with the given ID and true, or a zero
// value and false if not found.
func (s *LabEnvironmentStore) Get(id string) (model.LabEnvironment, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, ok := s.records[id]
	return r, ok
}

// List returns all lab environments.
func (s *LabEnvironmentStore) List() []model.LabEnvironment {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.LabEnvironment, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, r)
	}
	return out
}

// Update replaces an existing lab environment and persists.
func (s *LabEnvironmentStore) Update(record model.LabEnvironment) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.records[record.ID]
	if !ok {
		return errors.New("lab environment not found")
	}
	if record.CreatedAt == "" {
		record.CreatedAt = existing.CreatedAt
	}
	record.UpdatedAt = model.NowString()
	s.records[record.ID] = record
	return s.persist()
}

// Delete removes a lab environment and persists. It also removes any
// registered mock hosts.
func (s *LabEnvironmentStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	env, ok := s.records[id]
	if !ok {
		return errors.New("lab environment not found")
	}
	// Remove mock hosts registered for this environment.
	if s.hostStore != nil {
		for _, hostID := range env.MockHostIDs {
			s.hostStore.RemoveHost(hostID)
		}
	}
	delete(s.records, id)
	return s.persist()
}

// Start transitions the environment to "running" and registers mock hosts
// for each topology node.
func (s *LabEnvironmentStore) Start(id string) (model.LabEnvironment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	env, ok := s.records[id]
	if !ok {
		return model.LabEnvironment{}, errors.New("lab environment not found")
	}
	if env.Status == "running" {
		return env, nil
	}

	// Register mock hosts for each topology node.
	mockHostIDs := make([]string, 0, len(env.Topology.Nodes))
	for _, node := range env.Topology.Nodes {
		hostID := "lab-" + env.ID + "-" + node.ID
		labels := map[string]string{
			"lab.env":  env.ID,
			"lab.node": node.ID,
			"lab.role": node.Role,
		}
		for k, v := range node.Labels {
			labels[k] = v
		}
		if s.hostStore != nil {
			s.hostStore.UpsertHost(model.Host{
				ID:         hostID,
				Name:       node.Name,
				Kind:       "lab",
				Status:     "online",
				Executable: true,
				OS:         node.OS,
				Labels:     labels,
			})
		}
		mockHostIDs = append(mockHostIDs, hostID)
	}

	env.MockHostIDs = mockHostIDs
	env.Status = "running"
	env.UpdatedAt = model.NowString()
	s.records[id] = env
	if err := s.persist(); err != nil {
		return env, err
	}
	return env, nil
}

// Stop transitions the environment to "stopped" and removes mock hosts.
func (s *LabEnvironmentStore) Stop(id string) (model.LabEnvironment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	env, ok := s.records[id]
	if !ok {
		return model.LabEnvironment{}, errors.New("lab environment not found")
	}
	if env.Status == "stopped" || env.Status == "draft" {
		return env, nil
	}

	// Remove mock hosts.
	if s.hostStore != nil {
		for _, hostID := range env.MockHostIDs {
			s.hostStore.RemoveHost(hostID)
		}
	}

	env.MockHostIDs = nil
	env.Status = "stopped"
	env.UpdatedAt = model.NowString()
	s.records[id] = env
	if err := s.persist(); err != nil {
		return env, err
	}
	return env, nil
}

// InjectFault simulates a fault injection on a running environment by
// marking specified mock hosts as having errors.
func (s *LabEnvironmentStore) InjectFault(id string, nodeIDs []string, faultType string) (model.LabEnvironment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	env, ok := s.records[id]
	if !ok {
		return model.LabEnvironment{}, errors.New("lab environment not found")
	}
	if env.Status != "running" {
		return model.LabEnvironment{}, errors.New("lab environment is not running")
	}

	// Mark targeted mock hosts with the fault.
	targetSet := make(map[string]bool, len(nodeIDs))
	for _, nid := range nodeIDs {
		targetSet[nid] = true
	}

	if s.hostStore != nil {
		for _, node := range env.Topology.Nodes {
			if !targetSet[node.ID] {
				continue
			}
			hostID := "lab-" + env.ID + "-" + node.ID
			s.hostStore.UpsertHost(model.Host{
				ID:        hostID,
				Name:      node.Name,
				Kind:      "lab",
				Status:    "error",
				OS:        node.OS,
				LastError: "fault-injection: " + faultType,
				Labels: map[string]string{
					"lab.env":   env.ID,
					"lab.node":  node.ID,
					"lab.role":  node.Role,
					"lab.fault": faultType,
				},
			})
		}
	}

	env.UpdatedAt = model.NowString()
	s.records[id] = env
	if err := s.persist(); err != nil {
		return env, err
	}
	return env, nil
}

// Reset restores all mock hosts in a running environment to healthy state.
func (s *LabEnvironmentStore) Reset(id string) (model.LabEnvironment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	env, ok := s.records[id]
	if !ok {
		return model.LabEnvironment{}, errors.New("lab environment not found")
	}
	if env.Status != "running" {
		return model.LabEnvironment{}, errors.New("lab environment is not running")
	}

	// Restore all mock hosts to healthy state.
	if s.hostStore != nil {
		for _, node := range env.Topology.Nodes {
			hostID := "lab-" + env.ID + "-" + node.ID
			labels := map[string]string{
				"lab.env":  env.ID,
				"lab.node": node.ID,
				"lab.role": node.Role,
			}
			for k, v := range node.Labels {
				labels[k] = v
			}
			s.hostStore.UpsertHost(model.Host{
				ID:         hostID,
				Name:       node.Name,
				Kind:       "lab",
				Status:     "online",
				Executable: true,
				OS:         node.OS,
				Labels:     labels,
			})
		}
	}

	env.UpdatedAt = model.NowString()
	s.records[id] = env
	if err := s.persist(); err != nil {
		return env, err
	}
	return env, nil
}

// LabEnvironmentStats holds aggregate counters.
type LabEnvironmentStats struct {
	Total   int `json:"total"`
	Running int `json:"running"`
	Stopped int `json:"stopped"`
	Draft   int `json:"draft"`
}

// Stats returns aggregate counters for the lab environment store.
func (s *LabEnvironmentStore) Stats() LabEnvironmentStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var stats LabEnvironmentStats
	stats.Total = len(s.records)
	for _, r := range s.records {
		switch r.Status {
		case "running":
			stats.Running++
		case "stopped":
			stats.Stopped++
		case "draft":
			stats.Draft++
		}
	}
	return stats
}
