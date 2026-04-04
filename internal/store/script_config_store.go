package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// ScriptConfigStore provides thread-safe, JSON-persisted storage for
// script configuration profiles.
type ScriptConfigStore struct {
	mu      sync.RWMutex
	records map[string]model.ScriptConfigProfile // key: id
	path    string
}

// NewScriptConfigStore creates a new store that persists to the given path.
func NewScriptConfigStore(path string) *ScriptConfigStore {
	return &ScriptConfigStore{
		records: make(map[string]model.ScriptConfigProfile),
		path:    path,
	}
}

// Load restores records from the JSON file on disk. If the file does not
// exist the store starts empty (no error).
func (s *ScriptConfigStore) Load() error {
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
	var records []model.ScriptConfigProfile
	if err := json.Unmarshal(content, &records); err != nil {
		return err
	}
	s.records = make(map[string]model.ScriptConfigProfile, len(records))
	for _, r := range records {
		s.records[r.ID] = r
	}
	return nil
}

// persist writes the current records to disk as indented JSON, using an
// atomic write-then-rename pattern.
func (s *ScriptConfigStore) persist() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	records := make([]model.ScriptConfigProfile, 0, len(s.records))
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

// Add creates a new script config profile and persists to disk.
func (s *ScriptConfigStore) Add(record model.ScriptConfigProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if record.ID == "" {
		return errors.New("script config ID is required")
	}
	if _, exists := s.records[record.ID]; exists {
		return errors.New("script config already exists")
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

// Get returns the script config profile with the given ID and true, or a
// zero value and false if not found.
func (s *ScriptConfigStore) Get(id string) (model.ScriptConfigProfile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, ok := s.records[id]
	return r, ok
}

// List returns all script config profiles.
func (s *ScriptConfigStore) List() []model.ScriptConfigProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.ScriptConfigProfile, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, r)
	}
	return out
}

// ListByScript returns all config profiles for the given script name.
func (s *ScriptConfigStore) ListByScript(scriptName string) []model.ScriptConfigProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []model.ScriptConfigProfile
	for _, r := range s.records {
		if r.ScriptName == scriptName {
			out = append(out, r)
		}
	}
	return out
}

// Update replaces an existing script config profile and persists.
func (s *ScriptConfigStore) Update(record model.ScriptConfigProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.records[record.ID]
	if !ok {
		return errors.New("script config not found")
	}
	if record.CreatedAt == "" {
		record.CreatedAt = existing.CreatedAt
	}
	record.UpdatedAt = model.NowString()
	s.records[record.ID] = record
	return s.persist()
}

// Delete removes a script config profile and persists.
func (s *ScriptConfigStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.records[id]; !ok {
		return errors.New("script config not found")
	}
	delete(s.records, id)
	return s.persist()
}

// Stats returns aggregate counters for the script config store.
func (s *ScriptConfigStore) Stats() ScriptConfigStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var stats ScriptConfigStats
	stats.Total = len(s.records)
	for _, r := range s.records {
		switch r.Status {
		case "active":
			stats.Active++
		case "draft":
			stats.Draft++
		case "disabled":
			stats.Disabled++
		}
	}
	return stats
}

// ScriptConfigStats holds aggregate counters for the script config store.
type ScriptConfigStats struct {
	Total    int `json:"total"`
	Active   int `json:"active"`
	Draft    int `json:"draft"`
	Disabled int `json:"disabled"`
}
