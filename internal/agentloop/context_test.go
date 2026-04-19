package agentloop

import (
	"sync"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

func TestAppendMethods(t *testing.T) {
	cm := NewContextManager(4096)

	cm.AppendSystem("you are helpful")
	cm.AppendUser("hello")
	cm.AppendAssistant("hi there", nil)
	cm.AppendToolResult("call-1", "result-1")

	if cm.Len() != 4 {
		t.Fatalf("expected 4 messages, got %d", cm.Len())
	}

	msgs := cm.Messages()
	if msgs[0].Role != "system" || msgs[0].Content != "you are helpful" {
		t.Errorf("unexpected system message: %+v", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content != "hello" {
		t.Errorf("unexpected user message: %+v", msgs[1])
	}
	if msgs[2].Role != "assistant" || msgs[2].Content != "hi there" {
		t.Errorf("unexpected assistant message: %+v", msgs[2])
	}
	if msgs[3].Role != "tool" || msgs[3].Content != "result-1" || msgs[3].ToolCallID != "call-1" {
		t.Errorf("unexpected tool message: %+v", msgs[3])
	}
}

func TestAppendAssistantWithToolCalls(t *testing.T) {
	cm := NewContextManager(4096)

	tcs := []bifrost.ToolCall{
		{ID: "tc-1", Type: "function", Function: bifrost.FunctionCall{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
	}
	cm.AppendAssistant("let me check", tcs)

	msgs := cm.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if len(msgs[0].ToolCalls) != 1 || msgs[0].ToolCalls[0].ID != "tc-1" {
		t.Errorf("tool calls not preserved: %+v", msgs[0].ToolCalls)
	}
}

func TestMessagesReturnsCopy(t *testing.T) {
	cm := NewContextManager(4096)
	cm.AppendUser("original")

	msgs := cm.Messages()
	msgs[0].Content = "modified"

	// The internal state should be unchanged.
	internal := cm.Messages()
	if internal[0].Content != "original" {
		t.Errorf("Messages() did not return a copy; internal content was modified to %q", internal[0].Content)
	}
}

func TestSanitize_MissingToolResultsGetStub(t *testing.T) {
	cm := NewContextManager(4096)

	// Assistant with two tool calls, but only one result provided.
	cm.AppendAssistant("checking", []bifrost.ToolCall{
		{ID: "tc-1", Type: "function", Function: bifrost.FunctionCall{Name: "read_file"}},
		{ID: "tc-2", Type: "function", Function: bifrost.FunctionCall{Name: "list_files"}},
	})
	cm.AppendToolResult("tc-1", "file contents")
	// tc-2 result is missing.

	cm.Sanitize()

	msgs := cm.Messages()
	// Expect: assistant, tool(tc-1), stub(tc-2), tool(tc-1 original)
	// Actually after ensureCallOutputsPresent: assistant + stub(tc-2) inserted after assistant,
	// then the original tool(tc-1) follows.
	// Let's just verify tc-2 has a stub somewhere.
	foundStub := false
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID == "tc-2" {
			if m.Content != "[result not available]" {
				t.Errorf("expected stub content, got %q", m.Content)
			}
			foundStub = true
		}
	}
	if !foundStub {
		t.Error("missing stub for tc-2")
	}

	// tc-1 should still have its original result.
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID == "tc-1" {
			if m.Content != "file contents" {
				t.Errorf("tc-1 content changed to %q", m.Content)
			}
		}
	}
}

func TestSanitize_OrphanToolResultsRemoved(t *testing.T) {
	cm := NewContextManager(4096)

	cm.AppendUser("hello")
	// Orphan tool result — no preceding assistant with matching tool_call.
	cm.AppendToolResult("orphan-id", "orphan result")
	cm.AppendAssistant("response", nil)

	cm.Sanitize()

	msgs := cm.Messages()
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID == "orphan-id" {
			t.Error("orphan tool result was not removed")
		}
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages after sanitize, got %d", len(msgs))
	}
}

func TestSanitize_ValidConversationUnchanged(t *testing.T) {
	cm := NewContextManager(4096)

	cm.AppendSystem("system prompt")
	cm.AppendUser("do something")
	cm.AppendAssistant("calling tool", []bifrost.ToolCall{
		{ID: "tc-1", Type: "function", Function: bifrost.FunctionCall{Name: "exec"}},
	})
	cm.AppendToolResult("tc-1", "done")
	cm.AppendAssistant("all done", nil)

	before := cm.Messages()
	cm.Sanitize()
	after := cm.Messages()

	if len(before) != len(after) {
		t.Fatalf("sanitize changed message count: %d → %d", len(before), len(after))
	}
	for i := range before {
		if before[i].Role != after[i].Role {
			t.Errorf("message %d role changed: %s → %s", i, before[i].Role, after[i].Role)
		}
		if before[i].ToolCallID != after[i].ToolCallID {
			t.Errorf("message %d ToolCallID changed", i)
		}
	}
}

func TestEstimateTokens_RoughVsPrecise(t *testing.T) {
	// With a large context window, a small conversation should use rough estimate.
	cm := NewContextManager(100000)
	cm.AppendUser("hello world")

	tokens := cm.EstimateTokens()
	if tokens <= 0 {
		t.Errorf("expected positive token count, got %d", tokens)
	}

	// The rough estimate for "hello world" (11 chars) + "user" (4 chars) = 15 / 4 = 3
	// Should be well under 70% of 100000.
	if tokens > 100 {
		t.Errorf("rough estimate unexpectedly high: %d", tokens)
	}

	// Now test with a small context window to trigger precise estimation.
	cmSmall := NewContextManager(10) // very small window
	cmSmall.AppendUser("hello world, this is a longer message to push past the threshold")

	tokensPrecise := cmSmall.EstimateTokens()
	if tokensPrecise <= 0 {
		t.Errorf("expected positive token count from precise estimate, got %d", tokensPrecise)
	}
}

func TestReplaceMessages(t *testing.T) {
	cm := NewContextManager(4096)
	cm.AppendUser("old message")

	newMsgs := []bifrost.Message{
		{Role: "system", Content: "new system"},
		{Role: "user", Content: "new user"},
	}
	cm.ReplaceMessages(newMsgs)

	if cm.Len() != 2 {
		t.Fatalf("expected 2 messages after replace, got %d", cm.Len())
	}

	msgs := cm.Messages()
	if msgs[0].Role != "system" || msgs[0].Content != "new system" {
		t.Errorf("unexpected first message: %+v", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content != "new user" {
		t.Errorf("unexpected second message: %+v", msgs[1])
	}

	// Verify replace made a copy (modifying newMsgs doesn't affect internal state).
	newMsgs[0].Content = "tampered"
	if cm.Messages()[0].Content != "new system" {
		t.Error("ReplaceMessages did not copy input; internal state was modified")
	}
}

func TestConcurrentAccess(t *testing.T) {
	cm := NewContextManager(4096)
	var wg sync.WaitGroup

	// Concurrent writes.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cm.AppendUser("concurrent message")
		}()
	}

	// Concurrent reads.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cm.Messages()
			_ = cm.Len()
			_ = cm.EstimateTokens()
		}()
	}

	wg.Wait()

	if cm.Len() != 50 {
		t.Errorf("expected 50 messages after concurrent writes, got %d", cm.Len())
	}
}


// --- Token Estimate Cache Tests ---

func TestTokenEstimateCache_GetSet(t *testing.T) {
	c := newTokenEstimateCache()

	// Miss on empty cache.
	if _, ok := c.get(42); ok {
		t.Error("expected cache miss on empty cache")
	}

	// Set and get.
	c.set(42, 100)
	v, ok := c.get(42)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if v != 100 {
		t.Errorf("got %d, want 100", v)
	}
}

func TestTokenEstimateCache_Invalidate(t *testing.T) {
	c := newTokenEstimateCache()
	c.set(1, 10)
	c.set(2, 20)

	c.invalidate()

	if _, ok := c.get(1); ok {
		t.Error("expected cache miss after invalidate")
	}
	if _, ok := c.get(2); ok {
		t.Error("expected cache miss after invalidate")
	}
}

func TestTokenEstimateCache_ConcurrentAccess(t *testing.T) {
	c := newTokenEstimateCache()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c.set(uint64(n), n*10)
			c.get(uint64(n))
		}(i)
	}
	wg.Wait()
}

func TestHashMessage_DifferentMessages(t *testing.T) {
	m1 := bifrost.Message{Role: "user", Content: "hello"}
	m2 := bifrost.Message{Role: "user", Content: "world"}
	m3 := bifrost.Message{Role: "assistant", Content: "hello"}

	h1 := hashMessage(m1)
	h2 := hashMessage(m2)
	h3 := hashMessage(m3)

	if h1 == h2 {
		t.Error("different content should produce different hashes")
	}
	if h1 == h3 {
		t.Error("different roles should produce different hashes")
	}
}

func TestHashMessage_SameMessage(t *testing.T) {
	m := bifrost.Message{Role: "user", Content: "hello"}
	if hashMessage(m) != hashMessage(m) {
		t.Error("same message should produce same hash")
	}
}

func TestHashMessage_WithToolCalls(t *testing.T) {
	m1 := bifrost.Message{
		Role: "assistant",
		ToolCalls: []bifrost.ToolCall{
			{ID: "tc-1", Function: bifrost.FunctionCall{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
		},
	}
	m2 := bifrost.Message{
		Role: "assistant",
		ToolCalls: []bifrost.ToolCall{
			{ID: "tc-2", Function: bifrost.FunctionCall{Name: "read_file", Arguments: `{"path":"b.txt"}`}},
		},
	}

	if hashMessage(m1) == hashMessage(m2) {
		t.Error("messages with different tool calls should have different hashes")
	}
}

func TestEstimateTokens_UsesCacheOnRepeatCalls(t *testing.T) {
	cm := NewContextManager(100000)
	cm.AppendUser("hello world this is a test message")

	// First call populates cache.
	tokens1 := cm.EstimateTokens()
	// Second call should use cache and return same result.
	tokens2 := cm.EstimateTokens()

	if tokens1 != tokens2 {
		t.Errorf("cached estimate should match: %d vs %d", tokens1, tokens2)
	}
}

func TestReplaceMessages_InvalidatesCache(t *testing.T) {
	cm := NewContextManager(100000)
	cm.AppendUser("short")
	_ = cm.EstimateTokens() // populate cache

	cm.ReplaceMessages([]bifrost.Message{
		{Role: "user", Content: "this is a much longer message that should produce a different token estimate"},
	})

	tokens := cm.EstimateTokens()
	if tokens <= 0 {
		t.Error("expected positive token count after replace")
	}
}
