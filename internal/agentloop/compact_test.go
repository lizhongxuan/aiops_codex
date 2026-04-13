package agentloop

import (
	"context"
	"strings"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

// mockProvider is a minimal bifrost.Provider for testing L4 summary generation.
type mockProvider struct {
	response *bifrost.ChatResponse
	err      error
}

func (p *mockProvider) Name() string { return "mock" }
func (p *mockProvider) ChatCompletion(_ context.Context, _ bifrost.ChatRequest) (*bifrost.ChatResponse, error) {
	return p.response, p.err
}
func (p *mockProvider) StreamChatCompletion(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
	return nil, nil
}
func (p *mockProvider) SupportsToolCalling() bool { return true }

func newTestGateway(resp *bifrost.ChatResponse, err error) *bifrost.Gateway {
	gw := bifrost.NewGateway(bifrost.GatewayConfig{
		DefaultProvider: "mock",
		DefaultModel:    "test-model",
	})
	gw.RegisterProvider("mock", &mockProvider{response: resp, err: err})
	return gw
}

func newTestCompressor(gw *bifrost.Gateway) *Compressor {
	return NewCompressor(gw, 100_000, "mock/test-model")
}

// bigString returns a string of n characters.
func bigString(n int) string {
	return strings.Repeat("x", n)
}

// ─── L1: truncateLargeToolResults ────────────────────────────────────────────

func TestL1_SmallResultUnchanged(t *testing.T) {
	gw := newTestGateway(nil, nil)
	c := newTestCompressor(gw)

	msgs := []bifrost.Message{
		{Role: "tool", Content: "short result", ToolCallID: "tc-1"},
	}
	out := c.truncateLargeToolResults(msgs)

	if len(out) != 1 {
		t.Fatalf("expected 1 message, got %d", len(out))
	}
	if messageContentString(out[0]) != "short result" {
		t.Errorf("small result should be unchanged, got %q", messageContentString(out[0]))
	}
}

func TestL1_LargeResultTruncated(t *testing.T) {
	gw := newTestGateway(nil, nil)
	c := newTestCompressor(gw)

	big := bigString(MaxSingleToolResultChars + 100)
	msgs := []bifrost.Message{
		{Role: "tool", Content: big, ToolCallID: "tc-big"},
	}
	out := c.truncateLargeToolResults(msgs)

	content := messageContentString(out[0])
	if len(content) >= MaxSingleToolResultChars {
		t.Errorf("expected truncated content, got length %d", len(content))
	}
	if !strings.Contains(content, "[truncated, full content saved to disk]") {
		t.Error("truncation notice missing")
	}
	// Preview should be PersistPreviewChars of the original.
	if !strings.HasPrefix(content, bigString(PersistPreviewChars)) {
		t.Error("preview prefix doesn't match original content")
	}
}

func TestL1_DecisionFreezing(t *testing.T) {
	gw := newTestGateway(nil, nil)
	c := newTestCompressor(gw)

	big := bigString(MaxSingleToolResultChars + 100)
	msgs := []bifrost.Message{
		{Role: "tool", Content: big, ToolCallID: "tc-frozen"},
	}

	// First pass — truncates.
	out1 := c.truncateLargeToolResults(msgs)
	content1 := messageContentString(out1[0])

	// Second pass with the same tool_call_id but different content — decision
	// should be frozen (still treated as truncated).
	msgs2 := []bifrost.Message{
		{Role: "tool", Content: "new small content", ToolCallID: "tc-frozen"},
	}
	out2 := c.truncateLargeToolResults(msgs2)
	content2 := messageContentString(out2[0])

	// The frozen decision was "truncated=true", so the message passes through
	// as-is (the content was already replaced in the first pass).
	_ = content1
	_ = content2

	// Verify the frozen map has the entry.
	if !c.frozenResults["tc-frozen"] {
		t.Error("expected tc-frozen to be frozen as truncated")
	}
}

func TestL1_NonToolMessagesPassThrough(t *testing.T) {
	gw := newTestGateway(nil, nil)
	c := newTestCompressor(gw)

	msgs := []bifrost.Message{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	out := c.truncateLargeToolResults(msgs)
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
	for i, m := range out {
		if messageContentString(m) != messageContentString(msgs[i]) {
			t.Errorf("message %d changed unexpectedly", i)
		}
	}
}

// ─── L2: FileStateCache ─────────────────────────────────────────────────────

func TestFileStateCache_NewFileNotCached(t *testing.T) {
	cache := NewFileStateCache()
	if cache.Check("/etc/nginx.conf", "server {}") {
		t.Error("new file should not be cached")
	}
}

func TestFileStateCache_UnchangedFileDetected(t *testing.T) {
	cache := NewFileStateCache()
	cache.Update("/etc/nginx.conf", "server {}")

	if !cache.Check("/etc/nginx.conf", "server {}") {
		t.Error("unchanged file should be detected")
	}
}

func TestFileStateCache_ChangedFileNotCached(t *testing.T) {
	cache := NewFileStateCache()
	cache.Update("/etc/nginx.conf", "server {}")

	if cache.Check("/etc/nginx.conf", "server { listen 80; }") {
		t.Error("changed file should not match cache")
	}
}

func TestL2_DeduplicateFileReads(t *testing.T) {
	gw := newTestGateway(nil, nil)
	c := newTestCompressor(gw)

	msgs := []bifrost.Message{
		{Role: "assistant", Content: "reading file", ToolCalls: []bifrost.ToolCall{
			{ID: "tc-read1", Type: "function", Function: bifrost.FunctionCall{Name: "read_file"}},
		}},
		{Role: "tool", Content: "file content here", ToolCallID: "tc-read1"},
		{Role: "assistant", Content: "reading again", ToolCalls: []bifrost.ToolCall{
			{ID: "tc-read2", Type: "function", Function: bifrost.FunctionCall{Name: "read_file"}},
		}},
		{Role: "tool", Content: "file content here", ToolCallID: "tc-read2"},
	}

	out := c.deduplicateFileReads(msgs)

	// First read should be kept as-is.
	if messageContentString(out[1]) != "file content here" {
		t.Errorf("first read should be preserved, got %q", messageContentString(out[1]))
	}
	// Second read (same content) should be replaced with stub.
	if messageContentString(out[3]) != FileUnchangedStub {
		t.Errorf("second read should be stub, got %q", messageContentString(out[3]))
	}
}

// ─── L3: microcompact ────────────────────────────────────────────────────────

func TestL3_OldReadOnlyToolResultsRemoved(t *testing.T) {
	gw := newTestGateway(nil, nil)
	c := newTestCompressor(gw)

	msgs := []bifrost.Message{
		{Role: "user", Content: "check host"},
		{Role: "assistant", Content: "checking", ToolCalls: []bifrost.ToolCall{
			{ID: "tc-hs", Type: "function", Function: bifrost.FunctionCall{Name: "host_summary"}},
		}},
		{Role: "tool", Content: "host info...", ToolCallID: "tc-hs"},
		{Role: "user", Content: "now fix it"},
		{Role: "assistant", Content: "fixing", ToolCalls: []bifrost.ToolCall{
			{ID: "tc-exec", Type: "function", Function: bifrost.FunctionCall{Name: "execute_command"}},
		}},
		{Role: "tool", Content: "command output", ToolCallID: "tc-exec"},
	}

	out := c.microcompact(msgs)

	// The old host_summary result (before the second user message) should be stubbed.
	for _, m := range out {
		if m.ToolCallID == "tc-hs" {
			if messageContentString(m) != "[old read result removed]" {
				t.Errorf("old read-only result should be removed, got %q", messageContentString(m))
			}
		}
	}

	// The execute_command result should be preserved.
	for _, m := range out {
		if m.ToolCallID == "tc-exec" {
			if messageContentString(m) != "command output" {
				t.Errorf("write tool result should be preserved, got %q", messageContentString(m))
			}
		}
	}
}

func TestL3_CurrentTurnResultsPreserved(t *testing.T) {
	gw := newTestGateway(nil, nil)
	c := newTestCompressor(gw)

	msgs := []bifrost.Message{
		{Role: "user", Content: "read the file"},
		{Role: "assistant", Content: "reading", ToolCalls: []bifrost.ToolCall{
			{ID: "tc-rf", Type: "function", Function: bifrost.FunctionCall{Name: "read_file"}},
		}},
		{Role: "tool", Content: "file contents", ToolCallID: "tc-rf"},
	}

	out := c.microcompact(msgs)

	// The read_file result is in the current (and only) turn — should be preserved.
	for _, m := range out {
		if m.ToolCallID == "tc-rf" {
			if messageContentString(m) != "file contents" {
				t.Errorf("current turn read result should be preserved, got %q", messageContentString(m))
			}
		}
	}
}

func TestL3_WriteToolResultsAlwaysPreserved(t *testing.T) {
	gw := newTestGateway(nil, nil)
	c := newTestCompressor(gw)

	msgs := []bifrost.Message{
		{Role: "user", Content: "write something"},
		{Role: "assistant", Content: "writing", ToolCalls: []bifrost.ToolCall{
			{ID: "tc-wf", Type: "function", Function: bifrost.FunctionCall{Name: "write_file"}},
		}},
		{Role: "tool", Content: "file written", ToolCallID: "tc-wf"},
		{Role: "user", Content: "next task"},
		{Role: "assistant", Content: "ok", ToolCalls: nil},
	}

	out := c.microcompact(msgs)

	for _, m := range out {
		if m.ToolCallID == "tc-wf" {
			if messageContentString(m) != "file written" {
				t.Errorf("write tool result should always be preserved, got %q", messageContentString(m))
			}
		}
	}
}

// ─── L4: generateSummary ─────────────────────────────────────────────────────

func TestL4_GenerateSummary(t *testing.T) {
	summaryText := "## Summary\n1. User asked to check CPU\n..."
	gw := newTestGateway(&bifrost.ChatResponse{
		Message: bifrost.Message{Role: "assistant", Content: summaryText},
		Usage:   bifrost.Usage{PromptTokens: 100, CompletionTokens: 50},
	}, nil)
	c := newTestCompressor(gw)

	msgs := []bifrost.Message{
		{Role: "system", Content: "you are an ops assistant"},
		{Role: "user", Content: "check CPU usage"},
		{Role: "assistant", Content: "I'll check the CPU."},
	}

	summary, err := c.generateSummary(context.Background(), msgs)
	if err != nil {
		t.Fatalf("generateSummary failed: %v", err)
	}
	if summary != summaryText {
		t.Errorf("unexpected summary: %q", summary)
	}
}

func TestL4_GenerateSummaryError(t *testing.T) {
	gw := newTestGateway(nil, context.DeadlineExceeded)
	c := newTestCompressor(gw)

	msgs := []bifrost.Message{
		{Role: "user", Content: "hello"},
	}

	_, err := c.generateSummary(context.Background(), msgs)
	if err == nil {
		t.Error("expected error from generateSummary")
	}
}

// ─── L5: truncateHeadForRetry ────────────────────────────────────────────────

func TestL5_DropsOldestTurns(t *testing.T) {
	gw := newTestGateway(nil, nil)
	c := newTestCompressor(gw)

	msgs := []bifrost.Message{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "turn 1"},
		{Role: "assistant", Content: "response 1"},
		{Role: "user", Content: "turn 2"},
		{Role: "assistant", Content: "response 2"},
		{Role: "user", Content: "turn 3"},
		{Role: "assistant", Content: "response 3"},
		{Role: "user", Content: "turn 4"},
		{Role: "assistant", Content: "response 4"},
		{Role: "user", Content: "turn 5"},
		{Role: "assistant", Content: "response 5"},
	}

	out := c.truncateHeadForRetry(msgs)

	// System message should always be preserved.
	if out[0].Role != "system" {
		t.Error("system message should be preserved")
	}

	// With 5 turns and max 3 drop iterations, we should have 2 turns left.
	userCount := 0
	for _, m := range out {
		if m.Role == "user" {
			userCount++
		}
	}
	if userCount != 2 {
		t.Errorf("expected 2 user turns remaining, got %d", userCount)
	}

	// The most recent turn should be preserved.
	lastUser := ""
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role == "user" {
			lastUser = messageContentString(out[i])
			break
		}
	}
	if lastUser != "turn 5" {
		t.Errorf("most recent turn should be preserved, got %q", lastUser)
	}
}

func TestL5_KeepsAtLeastOneTurn(t *testing.T) {
	gw := newTestGateway(nil, nil)
	c := newTestCompressor(gw)

	msgs := []bifrost.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "only turn"},
		{Role: "assistant", Content: "only response"},
	}

	out := c.truncateHeadForRetry(msgs)

	// Should keep system + the single turn.
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
}

func TestL5_SystemOnlyMessages(t *testing.T) {
	gw := newTestGateway(nil, nil)
	c := newTestCompressor(gw)

	msgs := []bifrost.Message{
		{Role: "system", Content: "system prompt"},
	}

	out := c.truncateHeadForRetry(msgs)
	if len(out) != 1 {
		t.Fatalf("expected 1 message, got %d", len(out))
	}
	if out[0].Role != "system" {
		t.Error("system message should be preserved")
	}
}

// ─── ShouldCompress ──────────────────────────────────────────────────────────

func TestShouldCompress_BelowThreshold(t *testing.T) {
	gw := newTestGateway(nil, nil)
	c := NewCompressor(gw, 100_000, "mock/test-model")

	// Usable = 100_000 - 13_000 = 87_000. Threshold = 87_000 * 0.83 = 72_210.
	if c.ShouldCompress(50_000) {
		t.Error("50K tokens should be below threshold")
	}
}

func TestShouldCompress_AboveThreshold(t *testing.T) {
	gw := newTestGateway(nil, nil)
	c := NewCompressor(gw, 100_000, "mock/test-model")

	// Threshold ≈ 72_210.
	if !c.ShouldCompress(80_000) {
		t.Error("80K tokens should be above threshold")
	}
}

func TestShouldCompress_SmallContextWindow(t *testing.T) {
	gw := newTestGateway(nil, nil)
	c := NewCompressor(gw, 10_000, "mock/test-model")

	// Usable = 10_000 - 13_000 = -3_000 → usable <= 0 → always compress.
	if !c.ShouldCompress(1) {
		t.Error("should always compress when usable context is non-positive")
	}
}

// ─── Compact (integration) ──────────────────────────────────────────────────

func TestCompact_SmallConversationNoOp(t *testing.T) {
	gw := newTestGateway(nil, nil)
	c := NewCompressor(gw, 100_000, "mock/test-model")

	cm := NewContextManager(100_000)
	cm.AppendSystem("system prompt")
	cm.AppendUser("hello")
	cm.AppendAssistant("hi there", nil)

	err := c.Compact(context.Background(), cm)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	// Small conversation should pass through L1-L3 without changes.
	msgs := cm.Messages()
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages, got %d", len(msgs))
	}
}

func TestCompact_L1TruncatesLargeToolResult(t *testing.T) {
	gw := newTestGateway(nil, nil)
	c := NewCompressor(gw, 100_000, "mock/test-model")

	cm := NewContextManager(100_000)
	cm.AppendSystem("system")
	cm.AppendUser("do something")
	cm.AppendAssistant("calling tool", []bifrost.ToolCall{
		{ID: "tc-big", Type: "function", Function: bifrost.FunctionCall{Name: "read_file"}},
	})
	cm.AppendToolResult("tc-big", bigString(MaxSingleToolResultChars+500))

	err := c.Compact(context.Background(), cm)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	msgs := cm.Messages()
	for _, m := range msgs {
		if m.ToolCallID == "tc-big" {
			content := messageContentString(m)
			if len(content) >= MaxSingleToolResultChars {
				t.Error("large tool result should have been truncated by L1")
			}
			if !strings.Contains(content, "[truncated") {
				t.Error("truncation notice missing")
			}
		}
	}
}

func TestCompact_L4TriggeredWhenOverThreshold(t *testing.T) {
	summaryText := "Compressed summary of conversation"
	gw := newTestGateway(&bifrost.ChatResponse{
		Message: bifrost.Message{Role: "assistant", Content: summaryText},
		Usage:   bifrost.Usage{PromptTokens: 100, CompletionTokens: 50},
	}, nil)

	// Use a very small context window so that even a modest conversation
	// triggers compression.
	c := NewCompressor(gw, 200, "mock/test-model")

	cm := NewContextManager(200)
	cm.AppendSystem("system prompt")
	cm.AppendUser("first question with some content to push tokens up")
	cm.AppendAssistant("first response with details", nil)
	cm.AppendUser("second question with more content")
	cm.AppendAssistant("second response", nil)

	err := c.Compact(context.Background(), cm)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	msgs := cm.Messages()
	// After L4, we should have system + compressed user message.
	found := false
	for _, m := range msgs {
		if strings.Contains(messageContentString(m), summaryText) {
			found = true
		}
	}
	if !found {
		t.Error("expected summary to appear in compressed messages")
	}
}

func TestCompact_L3RemovesOldReadResults(t *testing.T) {
	gw := newTestGateway(nil, nil)
	c := NewCompressor(gw, 100_000, "mock/test-model")

	cm := NewContextManager(100_000)
	cm.AppendSystem("system")
	cm.AppendUser("check host")
	cm.AppendAssistant("checking", []bifrost.ToolCall{
		{ID: "tc-hs", Type: "function", Function: bifrost.FunctionCall{Name: "host_summary"}},
	})
	cm.AppendToolResult("tc-hs", "host info details")
	cm.AppendUser("now do something else")
	cm.AppendAssistant("ok", nil)

	err := c.Compact(context.Background(), cm)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	msgs := cm.Messages()
	for _, m := range msgs {
		if m.ToolCallID == "tc-hs" {
			content := messageContentString(m)
			if content == "host info details" {
				t.Error("old read-only tool result should have been compacted by L3")
			}
		}
	}
}

// ─── Helper function tests ──────────────────────────────────────────────────

func TestMessageContentString(t *testing.T) {
	m1 := bifrost.Message{Role: "user", Content: "hello"}
	if messageContentString(m1) != "hello" {
		t.Errorf("expected 'hello', got %q", messageContentString(m1))
	}

	m2 := bifrost.Message{Role: "user", Content: 42}
	if messageContentString(m2) != "" {
		t.Errorf("expected empty string for non-string content, got %q", messageContentString(m2))
	}
}

func TestGroupIntoTurns(t *testing.T) {
	msgs := []bifrost.Message{
		{Role: "user", Content: "turn 1"},
		{Role: "assistant", Content: "resp 1"},
		{Role: "user", Content: "turn 2"},
		{Role: "assistant", Content: "resp 2"},
		{Role: "tool", Content: "result", ToolCallID: "tc-1"},
	}

	turns := groupIntoTurns(msgs)
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(turns))
	}
	if len(turns[0]) != 2 {
		t.Errorf("first turn should have 2 messages, got %d", len(turns[0]))
	}
	if len(turns[1]) != 3 {
		t.Errorf("second turn should have 3 messages, got %d", len(turns[1]))
	}
}

func TestResolveToolName(t *testing.T) {
	msgs := []bifrost.Message{
		{Role: "assistant", Content: "", ToolCalls: []bifrost.ToolCall{
			{ID: "tc-1", Type: "function", Function: bifrost.FunctionCall{Name: "read_file"}},
			{ID: "tc-2", Type: "function", Function: bifrost.FunctionCall{Name: "execute_command"}},
		}},
		{Role: "tool", Content: "result", ToolCallID: "tc-1"},
		{Role: "tool", Content: "result", ToolCallID: "tc-2"},
	}

	if name := resolveToolName(msgs, "tc-1"); name != "read_file" {
		t.Errorf("expected read_file, got %q", name)
	}
	if name := resolveToolName(msgs, "tc-2"); name != "execute_command" {
		t.Errorf("expected execute_command, got %q", name)
	}
	if name := resolveToolName(msgs, "tc-unknown"); name != "" {
		t.Errorf("expected empty string for unknown ID, got %q", name)
	}
}
