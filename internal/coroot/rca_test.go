package coroot

import (
	"context"
	"testing"
)

func TestStubRCAEngine_Analyze(t *testing.T) {
	engine := NewStubRCAEngine()
	result, err := engine.Analyze(context.Background(), "inc-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IncidentID != "inc-123" {
		t.Errorf("IncidentID = %q, want %q", result.IncidentID, "inc-123")
	}
	if len(result.RootCauses) == 0 {
		t.Error("expected at least one root cause")
	}
	if len(result.Suggestions) == 0 {
		t.Error("expected at least one suggestion")
	}
	if len(result.Timeline) == 0 {
		t.Error("expected at least one timeline event")
	}
	if result.AnalyzedAt.IsZero() {
		t.Error("AnalyzedAt should not be zero")
	}
}

func TestStubRCAEngine_Analyze_EmptyID(t *testing.T) {
	engine := NewStubRCAEngine()
	_, err := engine.Analyze(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty incidentID")
	}
}

func TestCorootRCAEngine_Analyze_EmptyID(t *testing.T) {
	engine := NewCorootRCAEngine(nil)
	_, err := engine.Analyze(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty incidentID")
	}
}

func TestRCAEngineInterface(t *testing.T) {
	// Compile-time check that both implementations satisfy the interface.
	var _ RCAEngine = (*CorootRCAEngine)(nil)
	var _ RCAEngine = (*StubRCAEngine)(nil)
}
