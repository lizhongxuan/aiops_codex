package agentloop

import (
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// ─── Task 11.15: Unit Tests for Context V2 and Compressor V2 ────────────────

// --- 11.8: Context Manager V2 Tests ---

func TestContextManagerV2_AppendAndRetrieve(t *testing.T) {
	cm := NewContextManagerV2(128000)

	cm.AppendUserV2("hello", "user")
	cm.AppendAssistantV2("hi there", nil, "assistant")
	cm.AppendToolResultV2("call-1", "result data", "tool")

	msgs := cm.Messages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("expected user role, got %q", msgs[0].Role)
	}
	if msgs[1].Role != "assistant" {
		t.Errorf("expected assistant role, got %q", msgs[1].Role)
	}
	if msgs[2].Role != "tool" {
		t.Errorf("expected tool role, got %q", msgs[2].Role)
	}
}

func TestContextManagerV2_TrackedMessages(t *testing.T) {
	cm := NewContextManagerV2(128000)
	cm.AppendUserV2("test message", "user")

	tracked := cm.TrackedMessages()
	if len(tracked) != 1 {
		t.Fatalf("expected 1 tracked message, got %d", len(tracked))
	}
	if tracked[0].Source != "user" {
		t.Errorf("expected source 'user', got %q", tracked[0].Source)
	}
	if tracked[0].TokenCount <= 0 {
		t.Error("expected positive token count")
	}
}

func TestContextManagerV2_TotalTokens(t *testing.T) {
	cm := NewContextManagerV2(128000)
	cm.AppendUserV2("short", "user")
	cm.AppendAssistantV2("also short", nil, "assistant")

	tokens := cm.TotalTokens()
	if tokens <= 0 {
		t.Error("expected positive total tokens")
	}
}

func TestContextManagerV2_PinnedMessages(t *testing.T) {
	cm := NewContextManagerV2(128000)

	// Add a pinned system message.
	cm.AppendTracked(bifrost.Message{Role: "system", Content: "important"}, "system", true)
	cm.AppendUserV2("msg1", "user")
	cm.AppendUserV2("msg2", "user")

	tracked := cm.TrackedMessages()
	if !tracked[0].Pinned {
		t.Error("expected first message to be pinned")
	}
	if tracked[1].Pinned {
		t.Error("expected second message to not be pinned")
	}
}

func TestContextManagerV2_ApplyTruncation_NoTruncationNeeded(t *testing.T) {
	cm := NewContextManagerV2(128000)
	cm.SetTruncationPolicy(TruncationPolicy{
		Strategy:               "oldest_first",
		PreserveSystemMessages: true,
		PreserveLastN:          2,
		MaxTokenBudget:         100000,
	})

	cm.AppendUserV2("hello", "user")
	cm.AppendAssistantV2("world", nil, "assistant")

	removed := cm.ApplyTruncation()
	if removed != 0 {
		t.Errorf("expected 0 removed, got %d", removed)
	}
}

func TestContextManagerV2_ApplyTruncation_OldestFirst(t *testing.T) {
	cm := NewContextManagerV2(128000)
	cm.SetTruncationPolicy(TruncationPolicy{
		Strategy:               "oldest_first",
		PreserveSystemMessages: true,
		PreserveLastN:          2,
		MaxTokenBudget:         10, // Very low budget to force truncation.
	})

	// Add many messages to exceed budget.
	for i := 0; i < 10; i++ {
		cm.AppendUserV2("this is a longer message that takes up tokens", "user")
	}

	removed := cm.ApplyTruncation()
	if removed == 0 {
		t.Error("expected some messages to be removed")
	}

	remaining := cm.TrackedMessages()
	if len(remaining) > 10 {
		t.Error("expected fewer messages after truncation")
	}
}

func TestContextManagerV2_ApplyTruncation_PreservesPinned(t *testing.T) {
	cm := NewContextManagerV2(128000)
	cm.SetTruncationPolicy(TruncationPolicy{
		Strategy:               "oldest_first",
		PreserveSystemMessages: true,
		PreserveLastN:          1,
		MaxTokenBudget:         5, // Very low.
	})

	cm.AppendTracked(bifrost.Message{Role: "system", Content: "system prompt"}, "system", true)
	cm.AppendUserV2("old message with lots of content to take up space", "user")
	cm.AppendUserV2("another old message", "user")
	cm.AppendUserV2("latest message", "user")

	cm.ApplyTruncation()

	// System message should still be there (pinned).
	tracked := cm.TrackedMessages()
	foundSystem := false
	for _, m := range tracked {
		if m.Role == "system" {
			foundSystem = true
		}
	}
	if !foundSystem {
		t.Error("expected pinned system message to be preserved")
	}
}

func TestContextManagerV2_ApplyTruncation_ToolResultsFirst(t *testing.T) {
	cm := NewContextManagerV2(128000)
	cm.SetTruncationPolicy(TruncationPolicy{
		Strategy:               "tool_results_first",
		PreserveSystemMessages: true,
		PreserveLastN:          1,
		MaxTokenBudget:         5,
	})

	cm.AppendUserV2("user msg", "user")
	cm.AppendToolResultV2("call-1", "large tool result with lots of data", "tool")
	cm.AppendToolResultV2("call-2", "another large tool result", "tool")
	cm.AppendUserV2("latest", "user")

	removed := cm.ApplyTruncation()
	if removed == 0 {
		t.Error("expected tool results to be removed first")
	}
}

// --- 11.9: Inter-agent Content Handling Tests ---

func TestNormalizeInterAgentContent(t *testing.T) {
	msgs := []TrackedMessage{
		{Message: bifrost.Message{Role: "user", Content: "hello"}, Source: "user"},
		{Message: bifrost.Message{Role: "assistant", Content: "result"}, Source: "agent:Atlas"},
	}

	normalized := NormalizeInterAgentContent(msgs)

	// User message should be unchanged.
	if s, ok := normalized[0].Content.(string); !ok || s != "hello" {
		t.Errorf("expected unchanged user message, got %v", normalized[0].Content)
	}

	// Agent message should be prefixed and role-mapped.
	if normalized[1].Role != "user" {
		t.Errorf("expected role 'user' for agent message in parent, got %q", normalized[1].Role)
	}
	if s, ok := normalized[1].Content.(string); !ok {
		t.Error("expected string content")
	} else if s != "[From agent:Atlas]: result" {
		t.Errorf("unexpected content: %q", s)
	}
}

func TestMergeChildHistory(t *testing.T) {
	parent := NewContextManagerV2(128000)
	childMsgs := []TrackedMessage{
		{Message: bifrost.Message{Role: "user", Content: "task"}, Source: "user"},
		{Message: bifrost.Message{Role: "assistant", Content: "done!"}, Source: "agent:Bolt"},
	}

	MergeChildHistory(parent, childMsgs, "Bolt")

	msgs := parent.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 merged message, got %d", len(msgs))
	}
	content, ok := msgs[0].Content.(string)
	if !ok {
		t.Fatal("expected string content")
	}
	if !ctxV2ContainsStr(content, "Agent Bolt completed task") {
		t.Errorf("expected completion marker, got %q", content)
	}
}

// --- 11.10: Reference Context Items Tests ---

func TestContextManagerV2_AddAndResolveReference(t *testing.T) {
	cm := NewContextManagerV2(128000)

	cm.AddReference(ReferenceItem{
		ID:      "file-1",
		Type:    "file",
		Path:    "/src/main.go",
		Content: "package main\nfunc main() {}",
	})

	ref, ok := cm.ResolveReference("file-1")
	if !ok {
		t.Fatal("expected reference to be found")
	}
	if ref.Path != "/src/main.go" {
		t.Errorf("expected path '/src/main.go', got %q", ref.Path)
	}
	if ref.Version != 1 {
		t.Errorf("expected version 1, got %d", ref.Version)
	}
	if ref.HasDiff {
		t.Error("expected no diff on first version")
	}
}

func TestContextManagerV2_ReferenceWithDiff(t *testing.T) {
	cm := NewContextManagerV2(128000)

	cm.AddReference(ReferenceItem{
		ID:      "file-1",
		Type:    "file",
		Path:    "/src/main.go",
		Content: "line1\nline2\nline3",
	})

	// Update the reference.
	cm.AddReference(ReferenceItem{
		ID:      "file-1",
		Type:    "file",
		Path:    "/src/main.go",
		Content: "line1\nline2_modified\nline3\nline4",
	})

	ref, ok := cm.ResolveReference("file-1")
	if !ok {
		t.Fatal("expected reference to be found")
	}
	if ref.Version != 2 {
		t.Errorf("expected version 2, got %d", ref.Version)
	}
	if !ref.HasDiff {
		t.Error("expected diff to be present")
	}
	if ref.Diff == "" {
		t.Error("expected non-empty diff")
	}
}

func TestContextManagerV2_ListAndRemoveReferences(t *testing.T) {
	cm := NewContextManagerV2(128000)

	cm.AddReference(ReferenceItem{ID: "ref-1", Type: "file", Content: "a"})
	cm.AddReference(ReferenceItem{ID: "ref-2", Type: "snippet", Content: "b"})

	refs := cm.ListReferences()
	if len(refs) != 2 {
		t.Errorf("expected 2 references, got %d", len(refs))
	}

	cm.RemoveReference("ref-1")
	refs = cm.ListReferences()
	if len(refs) != 1 {
		t.Errorf("expected 1 reference after removal, got %d", len(refs))
	}
}

func TestContextManagerV2_ResolveNonexistent(t *testing.T) {
	cm := NewContextManagerV2(128000)
	_, ok := cm.ResolveReference("nonexistent")
	if ok {
		t.Error("expected false for nonexistent reference")
	}
}

// helper
func ctxV2ContainsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
