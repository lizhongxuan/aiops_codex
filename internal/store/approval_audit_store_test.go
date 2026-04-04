package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func TestApprovalAuditStoreAddAndGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.json")
	s := NewApprovalAuditStore(path)

	rec := model.ApprovalAuditRecord{
		ID:       "aud-001",
		Event:    "approval.requested",
		HostID:   "web-01",
		Decision: "accept",
	}
	if err := s.Add(rec); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, ok := s.Get("aud-001")
	if !ok {
		t.Fatal("expected record to exist")
	}
	if got.HostID != "web-01" {
		t.Fatalf("expected HostID web-01, got %q", got.HostID)
	}
	if got.CreatedAt == "" {
		t.Fatal("expected CreatedAt to be set automatically")
	}

	_, ok = s.Get("nonexistent")
	if ok {
		t.Fatal("expected Get to return false for missing ID")
	}
}

func TestApprovalAuditStoreListFiltersAndPagination(t *testing.T) {
	s := NewApprovalAuditStore("")

	records := []model.ApprovalAuditRecord{
		{ID: "a1", HostID: "h1", Decision: "accept", Operator: "alice", CreatedAt: "2025-01-01T00:00:00Z"},
		{ID: "a2", HostID: "h2", Decision: "reject", Operator: "bob", CreatedAt: "2025-01-02T00:00:00Z"},
		{ID: "a3", HostID: "h1", Decision: "accept", Operator: "alice", CreatedAt: "2025-01-03T00:00:00Z"},
		{ID: "a4", HostID: "h1", Decision: "auto_accept", Operator: "alice", CreatedAt: "2025-01-04T00:00:00Z"},
	}
	for _, r := range records {
		if err := s.Add(r); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	// Filter by HostID.
	got := s.List(ApprovalAuditFilter{HostID: "h1"})
	if len(got) != 3 {
		t.Fatalf("expected 3 records for h1, got %d", len(got))
	}

	// Filter by Decision.
	got = s.List(ApprovalAuditFilter{Decision: "reject"})
	if len(got) != 1 || got[0].ID != "a2" {
		t.Fatalf("expected a2 for reject filter, got %v", got)
	}

	// Filter by time range.
	got = s.List(ApprovalAuditFilter{
		TimeFrom: "2025-01-02T00:00:00Z",
		TimeTo:   "2025-01-03T00:00:00Z",
	})
	if len(got) != 2 {
		t.Fatalf("expected 2 records in time range, got %d", len(got))
	}

	// Newest first ordering.
	all := s.List(ApprovalAuditFilter{})
	if len(all) != 4 {
		t.Fatalf("expected 4 records, got %d", len(all))
	}
	if all[0].ID != "a4" || all[3].ID != "a1" {
		t.Fatalf("expected newest first, got first=%s last=%s", all[0].ID, all[3].ID)
	}

	// Pagination: offset=1, limit=2.
	got = s.List(ApprovalAuditFilter{Limit: 2, Offset: 1})
	if len(got) != 2 {
		t.Fatalf("expected 2 records with pagination, got %d", len(got))
	}
	if got[0].ID != "a3" || got[1].ID != "a2" {
		t.Fatalf("unexpected pagination result: %s, %s", got[0].ID, got[1].ID)
	}
}

func TestApprovalAuditStoreStats(t *testing.T) {
	s := NewApprovalAuditStore("")

	today := time.Now().UTC().Format(time.RFC3339)
	records := []model.ApprovalAuditRecord{
		{ID: "s1", Decision: "accept", Status: "resolved", GrantMode: "session", CreatedAt: today},
		{ID: "s2", Decision: "auto_accept", Status: "resolved", CreatedAt: today},
		{ID: "s3", Decision: "", Status: "pending", CreatedAt: today},
		{ID: "s4", Decision: "accept", Status: "resolved", GrantMode: "none", CreatedAt: "2024-01-01T00:00:00Z"},
	}
	for _, r := range records {
		if err := s.Add(r); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	stats := s.Stats()
	if stats.TodayApprovalCount != 3 {
		t.Fatalf("expected 3 today approvals, got %d", stats.TodayApprovalCount)
	}
	if stats.PendingCount != 1 {
		t.Fatalf("expected 1 pending, got %d", stats.PendingCount)
	}
	if stats.AutoAcceptedCount != 1 {
		t.Fatalf("expected 1 auto-accepted, got %d", stats.AutoAcceptedCount)
	}
	if stats.GrantedCmdCount != 1 {
		t.Fatalf("expected 1 granted cmd (accept + non-none grant), got %d", stats.GrantedCmdCount)
	}
}

func TestApprovalAuditStorePersistAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "audit.json")

	s1 := NewApprovalAuditStore(path)
	if err := s1.Add(model.ApprovalAuditRecord{ID: "p1", HostID: "h1", Decision: "accept"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s1.Add(model.ApprovalAuditRecord{ID: "p2", HostID: "h2", Decision: "reject"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Verify file was created.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected JSON file to exist: %v", err)
	}

	// Load into a fresh store.
	s2 := NewApprovalAuditStore(path)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	got, ok := s2.Get("p1")
	if !ok {
		t.Fatal("expected p1 to be loaded")
	}
	if got.HostID != "h1" {
		t.Fatalf("expected HostID h1, got %q", got.HostID)
	}

	all := s2.List(ApprovalAuditFilter{})
	if len(all) != 2 {
		t.Fatalf("expected 2 records after load, got %d", len(all))
	}
}

func TestApprovalAuditStoreLoadMissingFile(t *testing.T) {
	s := NewApprovalAuditStore(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err := s.Load(); err != nil {
		t.Fatalf("Load on missing file should not error, got %v", err)
	}
	if got := s.List(ApprovalAuditFilter{}); len(got) != 0 {
		t.Fatalf("expected empty store, got %d records", len(got))
	}
}

func TestApprovalAuditStoreListFilterByOperatorAndToolName(t *testing.T) {
	s := NewApprovalAuditStore("")
	_ = s.Add(model.ApprovalAuditRecord{ID: "f1", Operator: "alice", ToolName: "bash", CreatedAt: "2025-01-01T00:00:00Z"})
	_ = s.Add(model.ApprovalAuditRecord{ID: "f2", Operator: "bob", ToolName: "file_write", CreatedAt: "2025-01-02T00:00:00Z"})
	_ = s.Add(model.ApprovalAuditRecord{ID: "f3", Operator: "alice", ToolName: "file_write", CreatedAt: "2025-01-03T00:00:00Z"})

	got := s.List(ApprovalAuditFilter{Operator: "alice"})
	if len(got) != 2 {
		t.Fatalf("expected 2 records for alice, got %d", len(got))
	}

	got = s.List(ApprovalAuditFilter{ToolName: "file_write"})
	if len(got) != 2 {
		t.Fatalf("expected 2 records for file_write, got %d", len(got))
	}

	got = s.List(ApprovalAuditFilter{Operator: "alice", ToolName: "file_write"})
	if len(got) != 1 || got[0].ID != "f3" {
		t.Fatalf("expected f3 for combined filter, got %v", got)
	}
}
