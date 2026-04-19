package execpolicy

import (
	"strings"
	"testing"
)

func TestAddCoAuthoredBy_SimpleMessage(t *testing.T) {
	msg := "Fix bug in parser"
	result := AddCoAuthoredBy(msg, "gpt-4o", "sess-abc123")

	if !strings.Contains(result, "Co-authored-by: gpt-4o <session:sess-abc123>") {
		t.Errorf("expected Co-authored-by trailer, got:\n%s", result)
	}
	// Should have blank line separator
	if !strings.Contains(result, "\n\n") {
		t.Errorf("expected blank line separator before trailer")
	}
}

func TestAddCoAuthoredBy_MessageWithExistingTrailer(t *testing.T) {
	msg := "Fix bug in parser\n\nSigned-off-by: dev@example.com"
	result := AddCoAuthoredBy(msg, "gpt-4o", "sess-abc123")

	if !strings.Contains(result, "Co-authored-by: gpt-4o <session:sess-abc123>") {
		t.Errorf("expected Co-authored-by trailer, got:\n%s", result)
	}
	// Should not add extra blank line when trailer already exists
	if strings.Count(result, "\n\n") > 1 {
		t.Errorf("should not add extra blank line when trailer exists")
	}
}

func TestAddCoAuthoredBy_EmptyMessage(t *testing.T) {
	result := AddCoAuthoredBy("", "gpt-4o", "sess-abc123")
	if !strings.Contains(result, "Co-authored-by:") {
		t.Errorf("expected trailer even for empty message, got: %s", result)
	}
}

func TestAddCoAuthoredBy_MultilineMessage(t *testing.T) {
	msg := "feat: add new feature\n\nThis adds a new feature that does things."
	result := AddCoAuthoredBy(msg, "claude-3", "sess-xyz")

	if !strings.HasSuffix(result, "Co-authored-by: claude-3 <session:sess-xyz>") {
		t.Errorf("trailer should be at end, got:\n%s", result)
	}
}
