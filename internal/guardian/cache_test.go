package guardian

import (
	"testing"
)

func TestApprovalCache_StoreAndCheck(t *testing.T) {
	cache := NewApprovalCache()

	decision := ApprovalDecision{
		Outcome:   OutcomeAllow,
		Rationale: "previously approved",
	}

	cache.Store("exec:ls -la", decision)

	got, ok := cache.Check("exec:ls -la")
	if !ok {
		t.Fatal("Check() returned false for stored pattern")
	}
	if got.Outcome != OutcomeAllow {
		t.Errorf("Outcome = %v, want allow", got.Outcome)
	}
	if got.Rationale != "previously approved" {
		t.Errorf("Rationale = %q, want %q", got.Rationale, "previously approved")
	}
	if got.CachedAt.IsZero() {
		t.Error("CachedAt should be set")
	}
}

func TestApprovalCache_CheckMiss(t *testing.T) {
	cache := NewApprovalCache()

	_, ok := cache.Check("nonexistent")
	if ok {
		t.Error("Check() should return false for non-existent pattern")
	}
}

func TestApprovalCache_Clear(t *testing.T) {
	cache := NewApprovalCache()

	cache.Store("pattern1", ApprovalDecision{Outcome: OutcomeAllow, Rationale: "r1"})
	cache.Store("pattern2", ApprovalDecision{Outcome: OutcomeDeny, Rationale: "r2"})

	if cache.Size() != 2 {
		t.Fatalf("Size = %d, want 2", cache.Size())
	}

	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("Size after Clear = %d, want 0", cache.Size())
	}
	if _, ok := cache.Check("pattern1"); ok {
		t.Error("pattern1 should not exist after Clear")
	}
}

func TestApprovalCache_Size(t *testing.T) {
	cache := NewApprovalCache()

	if cache.Size() != 0 {
		t.Errorf("initial Size = %d, want 0", cache.Size())
	}

	cache.Store("a", ApprovalDecision{Outcome: OutcomeAllow, Rationale: "r"})
	cache.Store("b", ApprovalDecision{Outcome: OutcomeDeny, Rationale: "r"})

	if cache.Size() != 2 {
		t.Errorf("Size = %d, want 2", cache.Size())
	}
}

func TestApprovalCache_Overwrite(t *testing.T) {
	cache := NewApprovalCache()

	cache.Store("key", ApprovalDecision{Outcome: OutcomeAllow, Rationale: "first"})
	cache.Store("key", ApprovalDecision{Outcome: OutcomeDeny, Rationale: "second"})

	got, ok := cache.Check("key")
	if !ok {
		t.Fatal("Check() returned false")
	}
	if got.Outcome != OutcomeDeny {
		t.Errorf("Outcome = %v, want deny (overwritten)", got.Outcome)
	}
	if got.Rationale != "second" {
		t.Errorf("Rationale = %q, want %q", got.Rationale, "second")
	}
}

func TestCheckBeforeReview_CacheHit(t *testing.T) {
	cache := NewApprovalCache()
	cache.Store("read_file:/etc/config", ApprovalDecision{
		Outcome:   OutcomeAllow,
		Rationale: "safe read",
	})

	request := GuardianApprovalRequest{
		ToolName:  "read_file",
		Arguments: "/etc/config",
	}

	result := CheckBeforeReview(cache, request)
	if result == nil {
		t.Fatal("CheckBeforeReview returned nil for cached pattern")
	}
	if result.Outcome != OutcomeAllow {
		t.Errorf("Outcome = %v, want allow", result.Outcome)
	}
}

func TestCheckBeforeReview_CacheMiss(t *testing.T) {
	cache := NewApprovalCache()

	request := GuardianApprovalRequest{
		ToolName:  "exec",
		Arguments: "rm -rf /",
	}

	result := CheckBeforeReview(cache, request)
	if result != nil {
		t.Error("CheckBeforeReview should return nil for cache miss")
	}
}

func TestCheckBeforeReview_NilCache(t *testing.T) {
	request := GuardianApprovalRequest{ToolName: "test"}

	result := CheckBeforeReview(nil, request)
	if result != nil {
		t.Error("CheckBeforeReview should return nil for nil cache")
	}
}

func TestCacheDecision(t *testing.T) {
	cache := NewApprovalCache()

	request := GuardianApprovalRequest{
		ToolName:  "write_file",
		Arguments: "/tmp/output.txt",
	}
	assessment := &GuardianAssessment{
		RiskLevel:         RiskLow,
		UserAuthorization: AuthExplicit,
		Outcome:           OutcomeAllow,
		Rationale:         "user approved write",
	}

	CacheDecision(cache, request, assessment)

	got, ok := cache.Check("write_file:/tmp/output.txt")
	if !ok {
		t.Fatal("decision should be cached")
	}
	if got.Outcome != OutcomeAllow {
		t.Errorf("cached Outcome = %v, want allow", got.Outcome)
	}
}

func TestCacheDecision_NilCache(t *testing.T) {
	// Should not panic.
	CacheDecision(nil, GuardianApprovalRequest{ToolName: "test"}, &GuardianAssessment{})
}

func TestCacheDecision_NilAssessment(t *testing.T) {
	cache := NewApprovalCache()
	// Should not panic.
	CacheDecision(cache, GuardianApprovalRequest{ToolName: "test"}, nil)
	if cache.Size() != 0 {
		t.Error("nil assessment should not be cached")
	}
}
