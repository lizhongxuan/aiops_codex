package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- Test LoadTraces from JSON ---

func TestLoadTraces_JSON(t *testing.T) {
	dir := t.TempDir()

	// Write a JSON file with traces
	content := `[
		{"id":"t1","session_id":"s1","role":"user","content":"hello"},
		{"id":"t2","session_id":"s1","role":"assistant","content":"hi there"}
	]`
	if err := os.WriteFile(filepath.Join(dir, "traces.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	traces, err := LoadTraces(dir)
	if err != nil {
		t.Fatalf("LoadTraces: %v", err)
	}
	if len(traces) != 2 {
		t.Fatalf("expected 2 traces, got %d", len(traces))
	}
	if traces[0].ID != "t1" || traces[0].Content != "hello" {
		t.Errorf("unexpected trace[0]: %+v", traces[0])
	}
	if traces[1].Role != "assistant" {
		t.Errorf("unexpected trace[1] role: %s", traces[1].Role)
	}
}

func TestLoadTraces_SingleJSON(t *testing.T) {
	dir := t.TempDir()

	content := `{"id":"t1","session_id":"s1","role":"user","content":"single"}`
	if err := os.WriteFile(filepath.Join(dir, "single.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	traces, err := LoadTraces(dir)
	if err != nil {
		t.Fatalf("LoadTraces: %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(traces))
	}
	if traces[0].Content != "single" {
		t.Errorf("unexpected content: %s", traces[0].Content)
	}
}

// --- Test LoadTraces from JSONL ---

func TestLoadTraces_JSONL(t *testing.T) {
	dir := t.TempDir()

	content := `{"id":"t1","role":"user","content":"line1"}
{"id":"t2","role":"assistant","content":"line2"}
`
	if err := os.WriteFile(filepath.Join(dir, "traces.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	traces, err := LoadTraces(dir)
	if err != nil {
		t.Fatalf("LoadTraces: %v", err)
	}
	if len(traces) != 2 {
		t.Fatalf("expected 2 traces, got %d", len(traces))
	}
	if traces[0].Content != "line1" {
		t.Errorf("unexpected content: %s", traces[0].Content)
	}
}

func TestLoadTraces_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	traces, err := LoadTraces(dir)
	if err != nil {
		t.Fatalf("LoadTraces: %v", err)
	}
	if len(traces) != 0 {
		t.Fatalf("expected 0 traces, got %d", len(traces))
	}
}

// --- Test NormalizeTraces ---

func TestNormalizeTraces(t *testing.T) {
	traces := []MemoryTrace{
		{ID: "1", Role: "  User  ", Content: "  hello  "},
		{ID: "2", Role: "ASSISTANT", Content: ""},
		{ID: "3", Role: "system", Content: "  setup  "},
	}

	normalized := NormalizeTraces(traces)
	if len(normalized) != 2 {
		t.Fatalf("expected 2 normalized traces, got %d", len(normalized))
	}
	if normalized[0].Role != "user" || normalized[0].Content != "hello" {
		t.Errorf("unexpected normalized[0]: %+v", normalized[0])
	}
	if normalized[1].Role != "system" || normalized[1].Content != "setup" {
		t.Errorf("unexpected normalized[1]: %+v", normalized[1])
	}
}


// --- Test Storage: save, load, clear ---

func TestStorage_SaveAndLoadRawMemories(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mem")
	s := NewStorage(dir)

	memories := []RawMemory{
		{ID: "r1", SourceRollout: "s1", Summary: "did stuff", ExtractedAt: time.Now()},
		{ID: "r2", SourceRollout: "s2", Summary: "more stuff", ExtractedAt: time.Now()},
	}

	if err := s.SaveRawMemories(memories); err != nil {
		t.Fatalf("SaveRawMemories: %v", err)
	}

	loaded, err := s.LoadRawMemories()
	if err != nil {
		t.Fatalf("LoadRawMemories: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 memories, got %d", len(loaded))
	}
	if loaded[0].ID != "r1" || loaded[1].Summary != "more stuff" {
		t.Errorf("unexpected loaded memories: %+v", loaded)
	}
}

func TestStorage_SaveAndLoadConsolidated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mem")
	s := NewStorage(dir)

	mem := &ConsolidatedMemory{
		Summary:        "consolidated summary",
		KeyFacts:       []string{"fact1", "fact2"},
		ConsolidatedAt: time.Now(),
	}

	if err := s.SaveConsolidated(mem); err != nil {
		t.Fatalf("SaveConsolidated: %v", err)
	}

	loaded, err := s.LoadConsolidated()
	if err != nil {
		t.Fatalf("LoadConsolidated: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil consolidated memory")
	}
	if loaded.Summary != "consolidated summary" {
		t.Errorf("unexpected summary: %s", loaded.Summary)
	}
	if len(loaded.KeyFacts) != 2 {
		t.Errorf("expected 2 key facts, got %d", len(loaded.KeyFacts))
	}
}

func TestStorage_LoadConsolidated_NotExist(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nonexistent")
	s := NewStorage(dir)

	loaded, err := s.LoadConsolidated()
	if err != nil {
		t.Fatalf("LoadConsolidated: %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil for non-existent, got %+v", loaded)
	}
}

func TestStorage_Clear(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mem")
	s := NewStorage(dir)

	// Save something first
	if err := s.SaveRawMemories([]RawMemory{{ID: "r1", Summary: "test"}}); err != nil {
		t.Fatal(err)
	}

	// Clear
	if err := s.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	// Verify directory is gone
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("expected dir to be removed, got err: %v", err)
	}
}

// --- Test ConsolidationLock ---

func TestConsolidationLock_AcquireRelease(t *testing.T) {
	lock := NewConsolidationLock()

	// First acquire should succeed
	if !lock.Acquire("holder1") {
		t.Fatal("expected first Acquire to succeed")
	}

	// Second acquire by different holder should fail
	if lock.Acquire("holder2") {
		t.Fatal("expected second Acquire to fail")
	}

	// Holder should be holder1
	if lock.Holder() != "holder1" {
		t.Errorf("expected holder1, got %s", lock.Holder())
	}

	// Release by wrong holder should not release
	lock.Release("holder2")
	if lock.Holder() != "holder1" {
		t.Errorf("expected holder1 still, got %s", lock.Holder())
	}

	// Release by correct holder
	lock.Release("holder1")
	if lock.Holder() != "" {
		t.Errorf("expected empty holder, got %s", lock.Holder())
	}

	// Should be acquirable again
	if !lock.Acquire("holder3") {
		t.Fatal("expected Acquire after release to succeed")
	}
}

// --- Test CitationTracker ---

func TestCitationTracker(t *testing.T) {
	ct := NewCitationTracker()

	ct.Add(Citation{MemoryID: "m1", SourceRollout: "r1", Excerpt: "excerpt1"})
	ct.AddFromRawMemory(RawMemory{ID: "m2", SourceRollout: "r2", Summary: "a summary"})

	all := ct.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 citations, got %d", len(all))
	}

	forR1 := ct.ForRollout("r1")
	if len(forR1) != 1 || forR1[0].MemoryID != "m1" {
		t.Errorf("unexpected ForRollout result: %+v", forR1)
	}

	forR2 := ct.ForRollout("r2")
	if len(forR2) != 1 || forR2[0].MemoryID != "m2" {
		t.Errorf("unexpected ForRollout result: %+v", forR2)
	}

	ct.Clear()
	if len(ct.All()) != 0 {
		t.Errorf("expected 0 citations after clear, got %d", len(ct.All()))
	}
}


// --- Test Phase 1 concurrent extraction (semaphore behavior) ---

func TestExtractPhase1_EmptyRollouts(t *testing.T) {
	// With nil gateway and empty rollouts, should return empty without error
	memories, err := ExtractPhase1(t.Context(), nil, DefaultPhase1Config(), nil)
	if err != nil {
		t.Fatalf("ExtractPhase1: %v", err)
	}
	if len(memories) != 0 {
		t.Errorf("expected 0 memories, got %d", len(memories))
	}
}

func TestExtractPhase1_SkipsEmptyRollouts(t *testing.T) {
	rollouts := [][]MemoryTrace{
		{}, // empty, should be skipped
		{}, // empty, should be skipped
	}
	memories, err := ExtractPhase1(t.Context(), nil, DefaultPhase1Config(), rollouts)
	if err != nil {
		t.Fatalf("ExtractPhase1: %v", err)
	}
	if len(memories) != 0 {
		t.Errorf("expected 0 memories for empty rollouts, got %d", len(memories))
	}
}

// --- Test MemorySystem InjectMemoryContext ---

func TestMemorySystem_InjectMemoryContext_Empty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mem")
	ms := NewMemorySystem(nil, dir)

	ctx, err := ms.InjectMemoryContext()
	if err != nil {
		t.Fatalf("InjectMemoryContext: %v", err)
	}
	if ctx != "" {
		t.Errorf("expected empty context, got %q", ctx)
	}
}

func TestMemorySystem_InjectMemoryContext_WithData(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mem")
	ms := NewMemorySystem(nil, dir)

	// Manually save consolidated memory
	consolidated := &ConsolidatedMemory{
		Summary:        "test summary for injection",
		ConsolidatedAt: time.Now(),
	}
	if err := ms.Storage().SaveConsolidated(consolidated); err != nil {
		t.Fatal(err)
	}

	ctx, err := ms.InjectMemoryContext()
	if err != nil {
		t.Fatalf("InjectMemoryContext: %v", err)
	}
	if ctx != "test summary for injection" {
		t.Errorf("unexpected context: %q", ctx)
	}
}
