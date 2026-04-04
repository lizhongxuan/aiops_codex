package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// CapabilityBindingStore provides thread-safe, JSON-persisted storage for
// capability binding records.
type CapabilityBindingStore struct {
	mu       sync.RWMutex
	records  map[string]model.CapabilityBinding // key: id
	bySource map[string][]string                // sourceType:sourceId -> []bindingId
	path     string
}

// NewCapabilityBindingStore creates a new store that persists to the given path.
func NewCapabilityBindingStore(path string) *CapabilityBindingStore {
	return &CapabilityBindingStore{
		records:  make(map[string]model.CapabilityBinding),
		bySource: make(map[string][]string),
		path:     path,
	}
}

// sourceKey builds the index key for a binding's source.
func sourceKey(sourceType, sourceID string) string {
	return sourceType + ":" + sourceID
}

// Load restores records from the JSON file on disk and rebuilds the bySource
// index. If the file does not exist the store starts empty (no error).
func (s *CapabilityBindingStore) Load() error {
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
	var records []model.CapabilityBinding
	if err := json.Unmarshal(content, &records); err != nil {
		return err
	}

	s.records = make(map[string]model.CapabilityBinding, len(records))
	s.bySource = make(map[string][]string)
	for _, r := range records {
		s.records[r.ID] = r
		key := sourceKey(r.SourceType, r.SourceID)
		s.bySource[key] = append(s.bySource[key], r.ID)
	}
	return nil
}

// persist writes the current records to disk as indented JSON.
func (s *CapabilityBindingStore) persist() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	records := make([]model.CapabilityBinding, 0, len(s.records))
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

// Add creates a new binding record and persists to disk.
func (s *CapabilityBindingStore) Add(record model.CapabilityBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if record.ID == "" {
		return errors.New("binding ID is required")
	}
	if record.CreatedAt == "" {
		record.CreatedAt = model.NowString()
	}
	if record.Status == "" {
		record.Status = "active"
	}
	record.UpdatedAt = record.CreatedAt
	s.records[record.ID] = record
	key := sourceKey(record.SourceType, record.SourceID)
	s.bySource[key] = append(s.bySource[key], record.ID)
	return s.persist()
}

// Get returns the binding with the given ID and true, or a zero value and
// false if not found.
func (s *CapabilityBindingStore) Get(id string) (model.CapabilityBinding, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, ok := s.records[id]
	return r, ok
}

// List returns all binding records.
func (s *CapabilityBindingStore) List() []model.CapabilityBinding {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.CapabilityBinding, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, r)
	}
	return out
}

// ListBySource returns all bindings for the given source type and ID.
func (s *CapabilityBindingStore) ListBySource(sourceType, sourceID string) []model.CapabilityBinding {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := sourceKey(sourceType, sourceID)
	ids := s.bySource[key]
	out := make([]model.CapabilityBinding, 0, len(ids))
	for _, id := range ids {
		if r, ok := s.records[id]; ok {
			out = append(out, r)
		}
	}
	return out
}

// Update replaces an existing binding record and persists.
func (s *CapabilityBindingStore) Update(record model.CapabilityBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.records[record.ID]
	if !ok {
		return errors.New("binding not found")
	}
	if record.CreatedAt == "" {
		record.CreatedAt = existing.CreatedAt
	}
	record.UpdatedAt = model.NowString()

	// If source changed, update the bySource index.
	oldKey := sourceKey(existing.SourceType, existing.SourceID)
	newKey := sourceKey(record.SourceType, record.SourceID)
	if oldKey != newKey {
		s.removeFromIndex(oldKey, record.ID)
		s.bySource[newKey] = append(s.bySource[newKey], record.ID)
	}
	s.records[record.ID] = record
	return s.persist()
}

// Delete removes a binding record and persists.
func (s *CapabilityBindingStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.records[id]
	if !ok {
		return errors.New("binding not found")
	}
	key := sourceKey(existing.SourceType, existing.SourceID)
	s.removeFromIndex(key, id)
	delete(s.records, id)
	return s.persist()
}

// removeFromIndex removes a binding ID from the bySource index for the given
// key. Must be called with the write lock held.
func (s *CapabilityBindingStore) removeFromIndex(key, bindingID string) {
	ids := s.bySource[key]
	for i, id := range ids {
		if id == bindingID {
			s.bySource[key] = append(ids[:i], ids[i+1:]...)
			break
		}
	}
}
