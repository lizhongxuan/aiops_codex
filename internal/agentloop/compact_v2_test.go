package agentloop

import (
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// ─── Task 11.15: Compressor V2 Tests ────────────────────────────────────────

func TestCompressorV2_RebuildCompactedHistory(t *testing.T) {
	c := &CompressorV2{
		Compressor: NewCompressor(nil, 128000, ""),
	}

	msgs := []bifrost.Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
		{Role: "user", Content: "do something"},
		{Role: "assistant", Content: "done!"},
	}

	rebuilt := c.rebuildCompactedHistory(msgs, "Summary of conversation")

	// Should have: system + compressed user + last assistant.
	if len(rebuilt) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(rebuilt))
	}
	if rebuilt[0].Role != "system" {
		t.Errorf("expected system role first, got %q", rebuilt[0].Role)
	}
	if rebuilt[1].Role != "user" {
		t.Errorf("expected user role for summary, got %q", rebuilt[1].Role)
	}
	content, _ := rebuilt[1].Content.(string)
	if !findSubstring(content, "Summary of conversation") {
		t.Error("expected summary in compressed message")
	}
	if rebuilt[2].Role != "assistant" {
		t.Errorf("expected assistant role last, got %q", rebuilt[2].Role)
	}
}

func TestCompressorV2_RebuildCompactedHistory_NoAssistant(t *testing.T) {
	c := &CompressorV2{
		Compressor: NewCompressor(nil, 128000, ""),
	}

	msgs := []bifrost.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "hello"},
	}

	rebuilt := c.rebuildCompactedHistory(msgs, "Summary")
	// No assistant message to preserve.
	if len(rebuilt) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(rebuilt))
	}
}

func TestCompactTemplates_RenderSummaryPrompt(t *testing.T) {
	ct := NewCompactTemplates("")

	msgs := []bifrost.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}

	prompt := ct.RenderSummaryPrompt(msgs)
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	if !findSubstring(prompt, "[user]") {
		t.Error("expected [user] marker in prompt")
	}
	if !findSubstring(prompt, "hello") {
		t.Error("expected user content in prompt")
	}
}

func TestCompactTemplates_RenderContinuationPrompt(t *testing.T) {
	ct := NewCompactTemplates("")

	prompt := ct.RenderContinuationPrompt("This is the summary")
	if !findSubstring(prompt, "This is the summary") {
		t.Error("expected summary in continuation prompt")
	}
	if !findSubstring(prompt, "compressed") {
		t.Error("expected 'compressed' marker in continuation prompt")
	}
}

func TestCompactTemplates_DefaultTemplateLoading(t *testing.T) {
	ct := NewCompactTemplates("")

	// Load summary template.
	template := ct.loadTemplate("summary")
	if template == "" {
		t.Fatal("expected non-empty default summary template")
	}
	if !findSubstring(template, "{{conversation}}") {
		t.Error("expected placeholder in template")
	}

	// Load continuation template.
	template = ct.loadTemplate("continuation")
	if template == "" {
		t.Fatal("expected non-empty default continuation template")
	}
	if !findSubstring(template, "{{summary}}") {
		t.Error("expected placeholder in continuation template")
	}
}

func TestCompactTemplates_CachesTemplates(t *testing.T) {
	ct := NewCompactTemplates("")

	// First load.
	t1 := ct.loadTemplate("summary")
	// Second load should come from cache.
	t2 := ct.loadTemplate("summary")

	if t1 != t2 {
		t.Error("expected cached template to match")
	}
}

func TestCompressorV2_ShouldCompress(t *testing.T) {
	c := &CompressorV2{
		Compressor: NewCompressor(nil, 128000, ""),
	}

	// Below threshold.
	if c.ShouldCompress(50000) {
		t.Error("should not compress at 50000 tokens")
	}

	// Above threshold (83% of 128000-13000 = 95450).
	if !c.ShouldCompress(100000) {
		t.Error("should compress at 100000 tokens")
	}
}

func TestNewCompressorV2_DefaultDelegation(t *testing.T) {
	c := NewCompressorV2(CompressorV2Config{
		ContextWindow: 128000,
		SummaryModel:  "gpt-4o-mini",
	})

	if c.delegation != CompactLocal {
		t.Errorf("expected local delegation by default, got %v", c.delegation)
	}
}

func TestNewCompressorV2_RemoteDelegation(t *testing.T) {
	c := NewCompressorV2(CompressorV2Config{
		ContextWindow:  128000,
		SummaryModel:   "gpt-4o-mini",
		Delegation:     CompactRemote,
		RemoteEndpoint: "http://localhost:8080/compact",
	})

	if c.delegation != CompactRemote {
		t.Errorf("expected remote delegation, got %v", c.delegation)
	}
	if c.remoteEndpoint != "http://localhost:8080/compact" {
		t.Errorf("unexpected endpoint: %q", c.remoteEndpoint)
	}
}

// helper
func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
