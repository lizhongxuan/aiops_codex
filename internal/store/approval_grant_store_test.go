package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func TestApprovalGrantStoreAddAndGet(t *testing.T) {
	dir := t.TempDir()
	s := NewApprovalGrantStore(filepath.Join(dir, "grants.json"))

	record := model.ApprovalGrantRecord{
		ID:          "grant-1",
		HostID:      "host-1",
		GrantType:   "command",
		Fingerprint: "command|host-1|/tmp|ls",
		Command:     "ls",
		Status:      "active",
	}
	if err := s.Add(record); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, ok := s.Get("grant-1")
	if !ok {
		t.Fatal("expected record to be found")
	}
	if got.CreatedAt == "" {
		t.Fatal("expected CreatedAt to be auto-filled")
	}
	if got.HostID != "host-1" || got.Command != "ls" {
		t.Fatalf("unexpected record: %#v", got)
	}
}

func TestApprovalGrantStoreHighRiskRequiresExpiry(t *testing.T) {
	dir := t.TempDir()
	s := NewApprovalGrantStore(filepath.Join(dir, "grants.json"))

	record := model.ApprovalGrantRecord{
		ID:        "grant-hr",
		HostID:    "host-1",
		GrantType: "command",
		Command:   "rm -rf /important",
		Status:    "active",
	}
	if err := s.Add(record); err == nil {
		t.Fatal("expected error for high-risk command without ExpiresAt")
	}

	record.ExpiresAt = time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	if err := s.Add(record); err != nil {
		t.Fatalf("Add with ExpiresAt: %v", err)
	}
}

func TestApprovalGrantStoreListByHost(t *testing.T) {
	dir := t.TempDir()
	s := NewApprovalGrantStore(filepath.Join(dir, "grants.json"))

	_ = s.Add(model.ApprovalGrantRecord{ID: "g1", HostID: "host-1", GrantType: "command", Command: "ls"})
	_ = s.Add(model.ApprovalGrantRecord{ID: "g2", HostID: "host-1", GrantType: "command", Command: "pwd"})
	_ = s.Add(model.ApprovalGrantRecord{ID: "g3", HostID: "host-2", GrantType: "command", Command: "date"})

	host1 := s.ListByHost("host-1")
	if len(host1) != 2 {
		t.Fatalf("expected 2 records for host-1, got %d", len(host1))
	}
	host2 := s.ListByHost("host-2")
	if len(host2) != 1 {
		t.Fatalf("expected 1 record for host-2, got %d", len(host2))
	}
	empty := s.ListByHost("host-unknown")
	if len(empty) != 0 {
		t.Fatalf("expected 0 records for unknown host, got %d", len(empty))
	}
}

func TestApprovalGrantStoreMatchFingerprint(t *testing.T) {
	dir := t.TempDir()
	s := NewApprovalGrantStore(filepath.Join(dir, "grants.json"))

	_ = s.Add(model.ApprovalGrantRecord{
		ID:          "g1",
		HostID:      "host-1",
		GrantType:   "command",
		Fingerprint: "command|host-1|/tmp|ls",
		Command:     "ls",
		Status:      "active",
	})

	got, ok := s.MatchFingerprint("host-1", "command|host-1|/tmp|ls")
	if !ok {
		t.Fatal("expected active record to match")
	}
	if got.ID != "g1" {
		t.Fatalf("unexpected match: %#v", got)
	}

	// Non-matching fingerprint
	_, ok = s.MatchFingerprint("host-1", "command|host-1|/tmp|pwd")
	if ok {
		t.Fatal("expected no match for different fingerprint")
	}

	// Non-matching host
	_, ok = s.MatchFingerprint("host-2", "command|host-1|/tmp|ls")
	if ok {
		t.Fatal("expected no match for different host")
	}
}

func TestApprovalGrantStoreMatchFingerprintSkipsExpired(t *testing.T) {
	dir := t.TempDir()
	s := NewApprovalGrantStore(filepath.Join(dir, "grants.json"))

	_ = s.Add(model.ApprovalGrantRecord{
		ID:          "g-expired",
		HostID:      "host-1",
		GrantType:   "command",
		Fingerprint: "command|host-1|/tmp|ls",
		Command:     "ls",
		Status:      "active",
		ExpiresAt:   time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
	})

	_, ok := s.MatchFingerprint("host-1", "command|host-1|/tmp|ls")
	if ok {
		t.Fatal("expected expired record not to match")
	}
}

func TestApprovalGrantStoreMatchFingerprintSkipsNonActive(t *testing.T) {
	dir := t.TempDir()
	s := NewApprovalGrantStore(filepath.Join(dir, "grants.json"))

	_ = s.Add(model.ApprovalGrantRecord{
		ID:          "g-revoked",
		HostID:      "host-1",
		GrantType:   "command",
		Fingerprint: "command|host-1|/tmp|ls",
		Command:     "ls",
		Status:      "active",
	})
	_ = s.Revoke("g-revoked")

	_, ok := s.MatchFingerprint("host-1", "command|host-1|/tmp|ls")
	if ok {
		t.Fatal("expected revoked record not to match")
	}
}

func TestApprovalGrantStoreRevokeDisableEnable(t *testing.T) {
	dir := t.TempDir()
	s := NewApprovalGrantStore(filepath.Join(dir, "grants.json"))

	_ = s.Add(model.ApprovalGrantRecord{ID: "g1", HostID: "host-1", GrantType: "command", Command: "ls"})

	if err := s.Revoke("g1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	r, _ := s.Get("g1")
	if r.Status != "revoked" {
		t.Fatalf("expected revoked, got %q", r.Status)
	}

	if err := s.Enable("g1"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	r, _ = s.Get("g1")
	if r.Status != "active" {
		t.Fatalf("expected active, got %q", r.Status)
	}

	if err := s.Disable("g1"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	r, _ = s.Get("g1")
	if r.Status != "disabled" {
		t.Fatalf("expected disabled, got %q", r.Status)
	}

	// Not found cases
	if err := s.Revoke("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent record")
	}
	if err := s.Disable("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent record")
	}
	if err := s.Enable("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent record")
	}
}

func TestApprovalGrantStoreExpireStale(t *testing.T) {
	dir := t.TempDir()
	s := NewApprovalGrantStore(filepath.Join(dir, "grants.json"))

	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	future := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

	_ = s.Add(model.ApprovalGrantRecord{ID: "g-stale", HostID: "host-1", GrantType: "command", Command: "ls", ExpiresAt: past})
	_ = s.Add(model.ApprovalGrantRecord{ID: "g-fresh", HostID: "host-1", GrantType: "command", Command: "pwd", ExpiresAt: future})
	_ = s.Add(model.ApprovalGrantRecord{ID: "g-noexp", HostID: "host-1", GrantType: "command", Command: "date"})

	count := s.ExpireStale()
	if count != 1 {
		t.Fatalf("expected 1 expired, got %d", count)
	}

	stale, _ := s.Get("g-stale")
	if stale.Status != "expired" {
		t.Fatalf("expected stale to be expired, got %q", stale.Status)
	}
	fresh, _ := s.Get("g-fresh")
	if fresh.Status != "active" {
		t.Fatalf("expected fresh to remain active, got %q", fresh.Status)
	}
	noexp, _ := s.Get("g-noexp")
	if noexp.Status != "active" {
		t.Fatalf("expected no-expiry to remain active, got %q", noexp.Status)
	}
}

func TestApprovalGrantStorePersistAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "grants.json")
	s := NewApprovalGrantStore(path)

	_ = s.Add(model.ApprovalGrantRecord{ID: "g1", HostID: "host-1", GrantType: "command", Fingerprint: "fp1", Command: "ls"})
	_ = s.Add(model.ApprovalGrantRecord{ID: "g2", HostID: "host-2", GrantType: "file_change", Fingerprint: "fp2"})

	// Verify file was written
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected JSON file to exist: %v", err)
	}

	// Load into a new store
	s2 := NewApprovalGrantStore(path)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	r1, ok := s2.Get("g1")
	if !ok {
		t.Fatal("expected g1 to be loaded")
	}
	if r1.Command != "ls" || r1.HostID != "host-1" {
		t.Fatalf("unexpected loaded record: %#v", r1)
	}

	r2, ok := s2.Get("g2")
	if !ok {
		t.Fatal("expected g2 to be loaded")
	}
	if r2.GrantType != "file_change" || r2.HostID != "host-2" {
		t.Fatalf("unexpected loaded record: %#v", r2)
	}

	// Verify byHost index was rebuilt
	host1 := s2.ListByHost("host-1")
	if len(host1) != 1 || host1[0].ID != "g1" {
		t.Fatalf("expected byHost index to be rebuilt for host-1, got %#v", host1)
	}
	host2 := s2.ListByHost("host-2")
	if len(host2) != 1 || host2[0].ID != "g2" {
		t.Fatalf("expected byHost index to be rebuilt for host-2, got %#v", host2)
	}
}

func TestApprovalGrantStoreLoadMissingFile(t *testing.T) {
	s := NewApprovalGrantStore(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err := s.Load(); err != nil {
		t.Fatalf("Load on missing file should not error: %v", err)
	}
}

func TestApprovalGrantStoreEmptyPath(t *testing.T) {
	s := NewApprovalGrantStore("")
	if err := s.Load(); err != nil {
		t.Fatalf("Load with empty path: %v", err)
	}
	if err := s.Add(model.ApprovalGrantRecord{ID: "g1", HostID: "h1", GrantType: "command", Command: "ls"}); err != nil {
		t.Fatalf("Add with empty path: %v", err)
	}
}
