package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// ApprovalGrantStore provides thread-safe, JSON-persisted storage for
// approval grant records.
type ApprovalGrantStore struct {
	mu      sync.RWMutex
	records map[string]model.ApprovalGrantRecord // key: id
	byHost  map[string][]string                  // hostId -> []recordId
	path    string                               // JSON file persistence path
}

// NewApprovalGrantStore creates a new store that persists to the given path.
func NewApprovalGrantStore(path string) *ApprovalGrantStore {
	return &ApprovalGrantStore{
		records: make(map[string]model.ApprovalGrantRecord),
		byHost:  make(map[string][]string),
		path:    path,
	}
}

// Load restores records from the JSON file on disk and rebuilds the byHost
// index. If the file does not exist the store starts empty (no error).
func (s *ApprovalGrantStore) Load() error {
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
	var records []model.ApprovalGrantRecord
	if err := json.Unmarshal(content, &records); err != nil {
		return err
	}

	s.records = make(map[string]model.ApprovalGrantRecord, len(records))
	s.byHost = make(map[string][]string)
	for _, r := range records {
		s.records[r.ID] = r
		s.byHost[r.HostID] = append(s.byHost[r.HostID], r.ID)
	}
	return nil
}

// persist writes the current records to disk as indented JSON, using an
// atomic write-then-rename pattern consistent with the rest of the store
// package.
func (s *ApprovalGrantStore) persist() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	records := make([]model.ApprovalGrantRecord, 0, len(s.records))
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

// Add appends a record and persists to disk. High-risk commands must have
// ExpiresAt set.
func (s *ApprovalGrantStore) Add(record model.ApprovalGrantRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if record.GrantType == "command" && model.IsHighRiskCommand(record.Command) && record.ExpiresAt == "" {
		return errors.New("high-risk command grants must have ExpiresAt set")
	}
	if record.CreatedAt == "" {
		record.CreatedAt = model.NowString()
	}
	if record.Status == "" {
		record.Status = "active"
	}
	s.records[record.ID] = record
	s.byHost[record.HostID] = append(s.byHost[record.HostID], record.ID)
	return s.persist()
}

// Get returns the record with the given ID and true, or a zero value and
// false if not found.
func (s *ApprovalGrantStore) Get(id string) (model.ApprovalGrantRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, ok := s.records[id]
	return r, ok
}

// ListByHost returns all records for the given host ID.
func (s *ApprovalGrantStore) ListByHost(hostID string) []model.ApprovalGrantRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := s.byHost[hostID]
	out := make([]model.ApprovalGrantRecord, 0, len(ids))
	for _, id := range ids {
		if r, ok := s.records[id]; ok {
			out = append(out, r)
		}
	}
	return out
}

// MatchFingerprint returns the first active, non-expired record for the
// given host and fingerprint. Returns a zero value and false if none match.
func (s *ApprovalGrantStore) MatchFingerprint(hostID, fingerprint string) (model.ApprovalGrantRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now().Format(time.RFC3339)
	for _, id := range s.byHost[hostID] {
		r, ok := s.records[id]
		if !ok {
			continue
		}
		if r.Fingerprint != fingerprint {
			continue
		}
		if r.Status != "active" {
			continue
		}
		if r.ExpiresAt != "" && r.ExpiresAt <= now {
			continue
		}
		return r, true
	}
	return model.ApprovalGrantRecord{}, false
}

// Revoke sets the status of the record to "revoked" and persists.
func (s *ApprovalGrantStore) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.records[id]
	if !ok {
		return errors.New("record not found")
	}
	r.Status = "revoked"
	s.records[id] = r
	return s.persist()
}

// Disable sets the status of the record to "disabled" and persists.
func (s *ApprovalGrantStore) Disable(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.records[id]
	if !ok {
		return errors.New("record not found")
	}
	r.Status = "disabled"
	s.records[id] = r
	return s.persist()
}

// Enable sets the status of the record back to "active" and persists.
func (s *ApprovalGrantStore) Enable(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.records[id]
	if !ok {
		return errors.New("record not found")
	}
	r.Status = "active"
	s.records[id] = r
	return s.persist()
}

// ExpireStale finds all records where ExpiresAt is set and past, marks them
// "expired", and returns the count of records that were expired.
func (s *ApprovalGrantStore) ExpireStale() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format(time.RFC3339)
	count := 0
	for id, r := range s.records {
		if r.ExpiresAt == "" || r.Status != "active" {
			continue
		}
		if r.ExpiresAt <= now {
			r.Status = "expired"
			s.records[id] = r
			count++
		}
	}
	if count > 0 {
		_ = s.persist()
	}
	return count
}
