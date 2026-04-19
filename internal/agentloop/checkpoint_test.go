package agentloop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

func TestCheckpointStore_SaveAndLoadLatest(t *testing.T) {
	dir := t.TempDir()
	store := NewCheckpointStore(dir)

	// Save two checkpoints for the same session.
	store.Save(IterationCheckpoint{
		SessionID: "sess-1",
		Iteration: 0,
		Phase:     "llm_call",
		Messages:  []bifrost.Message{{Role: "user", Content: "hello"}},
	})
	store.Save(IterationCheckpoint{
		SessionID: "sess-1",
		Iteration: 1,
		Phase:     "tool_exec",
		Messages:  []bifrost.Message{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "hi"}},
		ToolResults: []toolResultEntry{{CallID: "c1", Result: "ok"}},
	})

	latest := store.LoadLatest("sess-1")
	if latest == nil {
		t.Fatal("expected non-nil checkpoint")
	}
	if latest.Iteration != 1 {
		t.Fatalf("expected iteration 1, got %d", latest.Iteration)
	}
	if latest.Phase != "tool_exec" {
		t.Fatalf("expected phase tool_exec, got %s", latest.Phase)
	}
	if len(latest.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(latest.Messages))
	}
	if len(latest.ToolResults) != 1 || latest.ToolResults[0].CallID != "c1" {
		t.Fatalf("unexpected tool results: %+v", latest.ToolResults)
	}
}

func TestCheckpointStore_LoadLatest_NoCheckpoints(t *testing.T) {
	dir := t.TempDir()
	store := NewCheckpointStore(dir)

	latest := store.LoadLatest("nonexistent")
	if latest != nil {
		t.Fatal("expected nil for nonexistent session")
	}
}

func TestCheckpointStore_Clear(t *testing.T) {
	dir := t.TempDir()
	store := NewCheckpointStore(dir)

	store.Save(IterationCheckpoint{
		SessionID: "sess-clear",
		Iteration: 0,
		Phase:     "llm_call",
		Messages:  []bifrost.Message{{Role: "user", Content: "test"}},
	})

	// Verify it was saved.
	if store.LoadLatest("sess-clear") == nil {
		t.Fatal("expected checkpoint to exist before clear")
	}

	store.Clear("sess-clear")

	// Verify it was cleared.
	if store.LoadLatest("sess-clear") != nil {
		t.Fatal("expected nil after clear")
	}

	// Directory should be gone.
	sessDir := filepath.Join(dir, "sess-clear")
	if _, err := os.Stat(sessDir); !os.IsNotExist(err) {
		t.Fatal("expected session directory to be removed")
	}
}

func TestCheckpointStore_DisabledWhenEmptyBaseDir(t *testing.T) {
	store := NewCheckpointStore("")

	// Save should be a no-op.
	store.Save(IterationCheckpoint{
		SessionID: "sess-disabled",
		Iteration: 0,
		Phase:     "llm_call",
	})

	// Load should return nil.
	if store.LoadLatest("sess-disabled") != nil {
		t.Fatal("expected nil when store is disabled")
	}

	// Clear should not panic.
	store.Clear("sess-disabled")
}

func TestCheckpointStore_MultipleSessions(t *testing.T) {
	dir := t.TempDir()
	store := NewCheckpointStore(dir)

	store.Save(IterationCheckpoint{SessionID: "sess-a", Iteration: 0, Phase: "llm_call"})
	store.Save(IterationCheckpoint{SessionID: "sess-b", Iteration: 0, Phase: "llm_call"})
	store.Save(IterationCheckpoint{SessionID: "sess-b", Iteration: 1, Phase: "tool_exec"})

	a := store.LoadLatest("sess-a")
	b := store.LoadLatest("sess-b")

	if a == nil || a.Iteration != 0 {
		t.Fatalf("sess-a: expected iteration 0, got %+v", a)
	}
	if b == nil || b.Iteration != 1 {
		t.Fatalf("sess-b: expected iteration 1, got %+v", b)
	}

	// Clear one session, other should remain.
	store.Clear("sess-a")
	if store.LoadLatest("sess-a") != nil {
		t.Fatal("sess-a should be cleared")
	}
	if store.LoadLatest("sess-b") == nil {
		t.Fatal("sess-b should still exist")
	}
}

func TestCheckpointStore_SaveWithToolCalls(t *testing.T) {
	dir := t.TempDir()
	store := NewCheckpointStore(dir)

	store.Save(IterationCheckpoint{
		SessionID: "sess-tc",
		Iteration: 0,
		Phase:     "llm_call",
		ToolCalls: []bifrost.ToolCall{
			{ID: "tc-1", Type: "function", Function: bifrost.FunctionCall{Name: "read_file", Arguments: `{"path":"test.go"}`}},
		},
	})

	latest := store.LoadLatest("sess-tc")
	if latest == nil {
		t.Fatal("expected non-nil checkpoint")
	}
	if len(latest.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(latest.ToolCalls))
	}
	if latest.ToolCalls[0].Function.Name != "read_file" {
		t.Fatalf("expected read_file, got %s", latest.ToolCalls[0].Function.Name)
	}
}
