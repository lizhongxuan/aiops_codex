package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// ApprovalAuditFilter controls which records List returns.
type ApprovalAuditFilter struct {
	TimeFrom    string
	TimeTo      string
	SessionKind string
	HostID      string
	Operator    string
	Decision    string
	ToolName    string
	Limit       int
	Offset      int
}

// ApprovalAuditStats holds aggregate counters for the approval audit store.
type ApprovalAuditStats struct {
	TodayApprovalCount int `json:"todayApprovalCount"`
	PendingCount       int `json:"pendingCount"`
	AutoAcceptedCount  int `json:"autoAcceptedCount"`
	GrantedCmdCount    int `json:"grantedCmdCount"`
}

// ApprovalAuditStore provides thread-safe, JSON-persisted storage for
// approval audit records.
type ApprovalAuditStore struct {
	mu      sync.RWMutex
	records []model.ApprovalAuditRecord
	path    string // JSON file persistence path
}

// NewApprovalAuditStore creates a new store that persists to the given path.
func NewApprovalAuditStore(path string) *ApprovalAuditStore {
	return &ApprovalAuditStore{
		records: make([]model.ApprovalAuditRecord, 0),
		path:    path,
	}
}

// Load restores records from the JSON file on disk. If the file does not
// exist the store starts empty (no error).
func (s *ApprovalAuditStore) Load() error {
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
	var records []model.ApprovalAuditRecord
	if err := json.Unmarshal(content, &records); err != nil {
		return err
	}
	s.records = records
	return nil
}

// persist writes the current records to disk as indented JSON, using an
// atomic write-then-rename pattern consistent with the rest of the store
// package.
func (s *ApprovalAuditStore) persist() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(s.records, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, content, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

// Add appends a record and persists to disk.
func (s *ApprovalAuditStore) Add(record model.ApprovalAuditRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if record.CreatedAt == "" {
		record.CreatedAt = model.NowString()
	}
	s.records = append(s.records, record)
	return s.persist()
}

// Get returns the record with the given ID and true, or a zero value and
// false if not found.
func (s *ApprovalAuditStore) Get(id string) (model.ApprovalAuditRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, r := range s.records {
		if r.ID == id {
			return r, true
		}
	}
	return model.ApprovalAuditRecord{}, false
}

// List returns records matching the filter. Records are returned in reverse
// chronological order (newest first). Limit/Offset provide pagination.
func (s *ApprovalAuditStore) List(filter ApprovalAuditFilter) []model.ApprovalAuditRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Collect matching records in reverse order (newest first).
	var matched []model.ApprovalAuditRecord
	for i := len(s.records) - 1; i >= 0; i-- {
		r := s.records[i]
		if !matchesFilter(r, filter) {
			continue
		}
		matched = append(matched, r)
	}

	// Apply offset.
	if filter.Offset > 0 {
		if filter.Offset >= len(matched) {
			return nil
		}
		matched = matched[filter.Offset:]
	}

	// Apply limit.
	if filter.Limit > 0 && len(matched) > filter.Limit {
		matched = matched[:filter.Limit]
	}

	return matched
}

// Stats returns aggregate counters across all stored records.
func (s *ApprovalAuditStore) Stats() ApprovalAuditStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	todayPrefix := time.Now().UTC().Format("2006-01-02")
	var stats ApprovalAuditStats
	for _, r := range s.records {
		if strings.HasPrefix(r.CreatedAt, todayPrefix) {
			stats.TodayApprovalCount++
		}
		if r.Status == "pending" {
			stats.PendingCount++
		}
		if r.Decision == "auto_accept" {
			stats.AutoAcceptedCount++
		}
		if r.Decision == "accept" && r.GrantMode != "" && r.GrantMode != "none" {
			stats.GrantedCmdCount++
		}
	}
	return stats
}

// matchesFilter checks whether a record satisfies all non-empty filter fields.
func matchesFilter(r model.ApprovalAuditRecord, f ApprovalAuditFilter) bool {
	if f.TimeFrom != "" && r.CreatedAt < f.TimeFrom {
		return false
	}
	if f.TimeTo != "" && r.CreatedAt > f.TimeTo {
		return false
	}
	if f.SessionKind != "" && r.SessionKind != f.SessionKind {
		return false
	}
	if f.HostID != "" && r.HostID != f.HostID {
		return false
	}
	if f.Operator != "" && r.Operator != f.Operator {
		return false
	}
	if f.Decision != "" && r.Decision != f.Decision {
		return false
	}
	if f.ToolName != "" && r.ToolName != f.ToolName {
		return false
	}
	return true
}
