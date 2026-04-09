package server

import (
	"strings"
	"testing"
)

func TestEnforceToolResultBudgetUnderLimit(t *testing.T) {
	// Content under 200K should pass through unchanged
	content := "short output"
	result := enforceToolResultBudget(content, "ev-123")
	if result != content {
		t.Errorf("expected unchanged content, got %q", result)
	}
}

func TestEnforceToolResultBudgetOverLimit(t *testing.T) {
	// Content over 200K should be truncated with head/tail
	content := make([]byte, maxToolResultChars+1000)
	for i := range content {
		content[i] = 'x'
	}
	result := enforceToolResultBudget(string(content), "ev-456")
	if len(result) >= len(content) {
		t.Errorf("expected truncated result, got length %d", len(result))
	}
	if !strings.Contains(result, "evidence ev-456") {
		t.Error("expected evidence ID in truncated result")
	}
}

func TestSummarizeCommandOutput(t *testing.T) {
	output := "line1\nline2\nline3\nERROR: something failed\nline5\nline6\nline7\nline8\nline9\nline10"
	result := summarizeCommandOutput(output, 1, 500, "ev-789")
	if !strings.Contains(result, "Exit code: 1") {
		t.Error("expected exit code in summary")
	}
	if !strings.Contains(result, "ERROR: something failed") {
		t.Error("expected error line in summary")
	}
	if !strings.Contains(result, "ev-789") {
		t.Error("expected evidence ID in summary")
	}
}

func TestMicrocompactMessagesKeepsRecentTurns(t *testing.T) {
	messages := make([]map[string]any, 10)
	for i := range messages {
		messages[i] = map[string]any{
			"role":    "tool_result",
			"content": strings.Repeat("x", 600),
		}
	}
	result := microcompactMessages(messages, 4)
	// Last 4 should be unchanged
	for i := 6; i < 10; i++ {
		content := result[i]["content"].(string)
		if strings.Contains(content, "[compacted") {
			t.Errorf("message %d should not be compacted", i)
		}
	}
	// Earlier ones should be compacted
	for i := 0; i < 6; i++ {
		content := result[i]["content"].(string)
		if !strings.Contains(content, "[compacted") {
			t.Errorf("message %d should be compacted", i)
		}
	}
}

func TestAutoCompactIfNeededUnderBudget(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "hello"},
	}
	failures := 0
	result, compacted, err := autoCompactIfNeeded(messages, &failures)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if compacted {
		t.Error("should not compact under budget")
	}
	if len(result) != 1 {
		t.Errorf("expected 1 message, got %d", len(result))
	}
}

func TestShouldCircuitBreak(t *testing.T) {
	if shouldCircuitBreak(0) {
		t.Error("should not break at 0")
	}
	if shouldCircuitBreak(2) {
		t.Error("should not break at 2")
	}
	if !shouldCircuitBreak(3) {
		t.Error("should break at 3")
	}
}

func TestRecoveryMessageForMaxTokens(t *testing.T) {
	msg := recoveryMessageForMaxTokens()
	if msg == "" {
		t.Error("recovery message should not be empty")
	}
	if !strings.Contains(msg, "Resume directly") {
		t.Error("recovery message should contain resume instruction")
	}
}
