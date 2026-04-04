package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// UICardStore provides thread-safe, JSON-persisted storage for UI card
// definitions. On startup it auto-registers built-in card definitions that
// are not yet present.
type UICardStore struct {
	mu      sync.RWMutex
	records map[string]model.UICardDefinition // key: id
	path    string
}

// NewUICardStore creates a new store that persists to the given path.
func NewUICardStore(path string) *UICardStore {
	return &UICardStore{
		records: make(map[string]model.UICardDefinition),
		path:    path,
	}
}

// Load restores records from the JSON file on disk. If the file does not
// exist the store starts empty (no error). After loading it ensures all
// built-in definitions are present.
func (s *UICardStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.path != "" {
		content, err := os.ReadFile(s.path)
		if err != nil {
			if !os.IsNotExist(err) {
				return err
			}
		} else {
			var records []model.UICardDefinition
			if err := json.Unmarshal(content, &records); err != nil {
				return err
			}
			s.records = make(map[string]model.UICardDefinition, len(records))
			for _, r := range records {
				s.records[r.ID] = r
			}
		}
	}

	// Auto-register built-in definitions that are missing.
	for _, def := range model.DefaultUICardDefinitions() {
		if _, exists := s.records[def.ID]; !exists {
			s.records[def.ID] = def
		}
	}
	return s.persist()
}

// persist writes the current records to disk as indented JSON.
func (s *UICardStore) persist() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	records := make([]model.UICardDefinition, 0, len(s.records))
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

// Add creates a new UI card definition and persists to disk.
func (s *UICardStore) Add(record model.UICardDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if record.ID == "" {
		return errors.New("card definition ID is required")
	}
	if _, exists := s.records[record.ID]; exists {
		return errors.New("card definition already exists")
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

// Get returns the card definition with the given ID and true, or a zero
// value and false if not found.
func (s *UICardStore) Get(id string) (model.UICardDefinition, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, ok := s.records[id]
	return r, ok
}

// List returns all card definitions.
func (s *UICardStore) List() []model.UICardDefinition {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.UICardDefinition, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, r)
	}
	return out
}

// ListByKind returns all card definitions matching the given kind.
func (s *UICardStore) ListByKind(kind string) []model.UICardDefinition {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []model.UICardDefinition
	for _, r := range s.records {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}

// Update replaces an existing card definition and persists.
func (s *UICardStore) Update(record model.UICardDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.records[record.ID]
	if !ok {
		return errors.New("card definition not found")
	}
	if record.CreatedAt == "" {
		record.CreatedAt = existing.CreatedAt
	}
	if record.BuiltIn != existing.BuiltIn {
		record.BuiltIn = existing.BuiltIn
	}
	record.UpdatedAt = model.NowString()
	s.records[record.ID] = record
	return s.persist()
}

// Delete removes a card definition and persists. Built-in definitions
// cannot be deleted.
func (s *UICardStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.records[id]
	if !ok {
		return errors.New("card definition not found")
	}
	if existing.BuiltIn {
		return errors.New("cannot delete built-in card definition")
	}
	delete(s.records, id)
	return s.persist()
}

// Stats returns aggregate counters for the card store.
func (s *UICardStore) Stats() UICardStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var stats UICardStats
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
		if r.BuiltIn {
			stats.BuiltIn++
		} else {
			stats.Custom++
		}
	}
	return stats
}

// UICardStats holds aggregate counters for the UI card store.
type UICardStats struct {
	Total    int `json:"total"`
	Active   int `json:"active"`
	Draft    int `json:"draft"`
	Disabled int `json:"disabled"`
	BuiltIn  int `json:"builtIn"`
	Custom   int `json:"custom"`
}
