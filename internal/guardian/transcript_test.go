package guardian

import (
	"strings"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

func TestBuildTranscript_BasicMessages(t *testing.T) {
	messages := []bifrost.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}
	request := GuardianApprovalRequest{
		ToolName:    "exec",
		Arguments:   "rm -rf /tmp/test",
		Description: "delete temp files",
	}

	result := BuildTranscript(messages, request)

	// Should have 2 message entries + 1 request entry.
	if len(result) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result))
	}
	if result[0].Role != "user" {
		t.Errorf("first entry role = %q, want user", result[0].Role)
	}
	if result[1].Role != "assistant" {
		t.Errorf("second entry role = %q, want assistant", result[1].Role)
	}
	// Last entry is the pending approval request.
	if result[2].Role != "system" {
		t.Errorf("last entry role = %q, want system", result[2].Role)
	}
	if !strings.Contains(result[2].Content, "PENDING APPROVAL") {
		t.Errorf("last entry should contain PENDING APPROVAL, got %q", result[2].Content)
	}
}

func TestBuildTranscript_SeparatesToolEntries(t *testing.T) {
	messages := []bifrost.Message{
		{Role: "user", Content: "do something"},
		{Role: "tool", Content: "tool result here"},
		{Role: "assistant", Content: "done"},
	}
	request := GuardianApprovalRequest{ToolName: "read_file"}

	result := BuildTranscript(messages, request)

	// 2 message entries (user, assistant) + 1 tool entry + 1 request = 4
	if len(result) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(result))
	}
}

func TestBuildTranscript_TruncatesLongMessages(t *testing.T) {
	// Create a message that exceeds MaxMessageEntryTokens (2000 tokens ~ 8000 chars).
	longContent := strings.Repeat("a", 10000)
	messages := []bifrost.Message{
		{Role: "user", Content: longContent},
	}
	request := GuardianApprovalRequest{ToolName: "test"}

	result := BuildTranscript(messages, request)

	// The message entry should be truncated to MaxMessageEntryTokens.
	if result[0].Tokens > MaxMessageEntryTokens {
		t.Errorf("message tokens = %d, should be <= %d", result[0].Tokens, MaxMessageEntryTokens)
	}
}

func TestBuildTranscript_TruncatesLongToolEntries(t *testing.T) {
	// Create a tool entry that exceeds MaxToolEntryTokens (1000 tokens ~ 4000 chars).
	longContent := strings.Repeat("b", 5000)
	messages := []bifrost.Message{
		{Role: "tool", Content: longContent},
	}
	request := GuardianApprovalRequest{ToolName: "test"}

	result := BuildTranscript(messages, request)

	// First entry is the tool entry (before the request).
	if result[0].Tokens > MaxToolEntryTokens {
		t.Errorf("tool tokens = %d, should be <= %d", result[0].Tokens, MaxToolEntryTokens)
	}
}

func TestBuildTranscript_LimitsRecentEntries(t *testing.T) {
	// Create more than MaxRecentEntries messages.
	messages := make([]bifrost.Message, 50)
	for i := range messages {
		messages[i] = bifrost.Message{Role: "user", Content: "msg"}
	}
	request := GuardianApprovalRequest{ToolName: "test"}

	result := BuildTranscript(messages, request)

	// Should have at most MaxRecentEntries message entries + 1 request.
	messageCount := len(result) - 1 // subtract request entry
	if messageCount > MaxRecentEntries {
		t.Errorf("message count = %d, should be <= %d", messageCount, MaxRecentEntries)
	}
}

func TestBuildTranscript_EnforcesTokenBudget(t *testing.T) {
	// Create entries that collectively exceed the token budget.
	// Each entry ~500 tokens (2000 chars), 25 entries = 12500 tokens > 10000 budget.
	messages := make([]bifrost.Message, 25)
	content := strings.Repeat("x", 2000)
	for i := range messages {
		messages[i] = bifrost.Message{Role: "user", Content: content}
	}
	request := GuardianApprovalRequest{ToolName: "test"}

	result := BuildTranscript(messages, request)

	// Calculate total tokens (excluding the request entry).
	totalTokens := 0
	for i := 0; i < len(result)-1; i++ {
		totalTokens += result[i].Tokens
	}
	if totalTokens > MaxMessageTranscriptTokens {
		t.Errorf("total tokens = %d, should be <= %d", totalTokens, MaxMessageTranscriptTokens)
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"hi", 1},       // 2 chars / 4 = 0, min 1
		{"hello world", 2}, // 11 chars / 4 = 2
	}
	for _, tt := range tests {
		got := estimateTokens(tt.input)
		if got != tt.expected {
			t.Errorf("estimateTokens(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestTruncateToTokens(t *testing.T) {
	input := strings.Repeat("a", 100)
	result := truncateToTokens(input, 10) // 10 tokens = 40 chars
	if len(result) != 40 {
		t.Errorf("truncateToTokens length = %d, want 40", len(result))
	}
}
