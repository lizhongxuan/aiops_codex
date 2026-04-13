package agentloop

import (
	"context"
	"embed"
	"fmt"
	"strings"
	"sync"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// ─── Task 11.13: Enhanced Compressor V2 ─────────────────────────────────────

// CompactDelegation specifies whether compaction is done locally or remotely.
type CompactDelegation string

const (
	CompactLocal  CompactDelegation = "local"
	CompactRemote CompactDelegation = "remote"
)

// CompressorV2Config configures the enhanced compressor.
type CompressorV2Config struct {
	// Gateway is the bifrost gateway for LLM calls.
	Gateway *bifrost.Gateway
	// ContextWindow is the max context window in tokens.
	ContextWindow int
	// SummaryModel is the model used for summarization.
	SummaryModel string
	// Delegation controls whether compaction is local or remote.
	Delegation CompactDelegation
	// RemoteEndpoint is the URL for remote compaction service (when Delegation is "remote").
	RemoteEndpoint string
	// TemplateDir is the directory for custom compact prompt templates.
	TemplateDir string
}

// CompressorV2 extends the base Compressor with remote delegation,
// local fallback, and template-based compact prompts.
type CompressorV2 struct {
	*Compressor
	delegation     CompactDelegation
	remoteEndpoint string
	templateDir    string
	templates      *CompactTemplates
	mu             sync.Mutex
}

// NewCompressorV2 creates an enhanced compressor with V2 features.
func NewCompressorV2(cfg CompressorV2Config) *CompressorV2 {
	base := NewCompressor(cfg.Gateway, cfg.ContextWindow, cfg.SummaryModel)
	delegation := cfg.Delegation
	if delegation == "" {
		delegation = CompactLocal
	}

	c := &CompressorV2{
		Compressor:     base,
		delegation:     delegation,
		remoteEndpoint: cfg.RemoteEndpoint,
		templateDir:    cfg.TemplateDir,
		templates:      NewCompactTemplates(cfg.TemplateDir),
	}
	return c
}

// Compact runs the V2 compression pipeline with remote delegation support.
func (c *CompressorV2) Compact(ctx context.Context, cm *ContextManager) error {
	msgs := cm.Messages()

	// L1-L3: Same as base compressor.
	msgs = c.truncateLargeToolResults(msgs)
	msgs = c.deduplicateFileReads(msgs)
	msgs = c.microcompact(msgs)
	cm.ReplaceMessages(msgs)

	if !c.ShouldCompress(cm.EstimateTokens()) {
		return nil
	}

	// L4: Try remote compaction first if configured, fall back to local.
	var summary string
	var err error

	if c.delegation == CompactRemote && c.remoteEndpoint != "" {
		summary, err = c.remoteCompact(ctx, cm.Messages())
		if err != nil {
			// Fall back to local on remote failure.
			summary, err = c.localCompact(ctx, cm.Messages())
		}
	} else {
		summary, err = c.localCompact(ctx, cm.Messages())
	}

	if err != nil {
		// L4 failed — fall through to L5.
	} else {
		cm.ReplaceMessages(c.rebuildCompactedHistory(cm.Messages(), summary))
	}

	if !c.ShouldCompress(cm.EstimateTokens()) {
		return nil
	}

	// L5: Head truncation.
	msgs = cm.Messages()
	msgs = c.truncateHeadForRetry(msgs)
	cm.ReplaceMessages(msgs)

	return nil
}

// localCompact generates a summary using the local LLM via template-based prompts.
func (c *CompressorV2) localCompact(ctx context.Context, msgs []bifrost.Message) (string, error) {
	prompt := c.templates.RenderSummaryPrompt(msgs)

	model := c.summaryModel
	if model == "" {
		model = "gpt-4o-mini"
	}

	req := bifrost.ChatRequest{
		Model: model,
		Messages: []bifrost.Message{
			{Role: "user", Content: prompt},
		},
		MaxTokens:   4096,
		Temperature: 0.2,
	}

	resp, err := c.gateway.ChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("compressor v2: local compact failed: %w", err)
	}

	return messageContentString(resp.Message), nil
}

// remoteCompact delegates compaction to a remote service.
// This is a placeholder that falls back to local if the remote is unavailable.
func (c *CompressorV2) remoteCompact(ctx context.Context, msgs []bifrost.Message) (string, error) {
	// In a full implementation, this would make an HTTP call to the remote endpoint.
	// For now, we return an error to trigger local fallback.
	if c.remoteEndpoint == "" {
		return "", fmt.Errorf("remote endpoint not configured")
	}

	// TODO: Implement actual HTTP call to remote compaction service.
	// For now, fall back to local.
	return "", fmt.Errorf("remote compaction not yet implemented, falling back to local")
}

// rebuildCompactedHistory creates a new message history from a summary,
// preserving system messages and adding a continuation marker.
func (c *CompressorV2) rebuildCompactedHistory(msgs []bifrost.Message, summary string) []bifrost.Message {
	var systemMsgs []bifrost.Message
	for _, m := range msgs {
		if m.Role == "system" {
			systemMsgs = append(systemMsgs, m)
		}
	}

	result := make([]bifrost.Message, 0, len(systemMsgs)+2)
	result = append(result, systemMsgs...)
	result = append(result, bifrost.Message{
		Role:    "user",
		Content: "[Previous conversation was compressed]\n\n" + summary + "\n\nPlease continue from where you left off.",
	})

	// Preserve the last assistant message for continuity.
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			result = append(result, msgs[i])
			break
		}
	}

	return result
}

// ─── Task 11.14: Markdown Template-Based Compact Prompts ────────────────────

//go:embed compact_templates
var defaultTemplatesFS embed.FS

// CompactTemplates manages markdown template files for compact prompts.
type CompactTemplates struct {
	mu          sync.RWMutex
	templateDir string
	cache       map[string]string
}

// NewCompactTemplates creates a template manager. If templateDir is empty,
// embedded defaults are used.
func NewCompactTemplates(templateDir string) *CompactTemplates {
	return &CompactTemplates{
		templateDir: templateDir,
		cache:       make(map[string]string),
	}
}

// RenderSummaryPrompt renders the summary prompt template with conversation data.
func (ct *CompactTemplates) RenderSummaryPrompt(msgs []bifrost.Message) string {
	template := ct.loadTemplate("summary")

	// Build conversation text.
	var sb strings.Builder
	for _, m := range msgs {
		if m.Role == "system" {
			continue
		}
		sb.WriteString(fmt.Sprintf("[%s", m.Role))
		if m.ToolCallID != "" {
			sb.WriteString(fmt.Sprintf(" tool_call_id=%s", m.ToolCallID))
		}
		sb.WriteString("]\n")
		sb.WriteString(messageContentString(m))
		sb.WriteString("\n\n")
	}

	// Replace template placeholder with conversation.
	return strings.Replace(template, "{{conversation}}", sb.String(), 1)
}

// RenderContinuationPrompt renders the continuation prompt after compaction.
func (ct *CompactTemplates) RenderContinuationPrompt(summary string) string {
	template := ct.loadTemplate("continuation")
	return strings.Replace(template, "{{summary}}", summary, 1)
}

// loadTemplate loads a template by name, trying custom dir first, then defaults.
func (ct *CompactTemplates) loadTemplate(name string) string {
	ct.mu.RLock()
	if cached, ok := ct.cache[name]; ok {
		ct.mu.RUnlock()
		return cached
	}
	ct.mu.RUnlock()

	// Try loading from custom template dir.
	// (In production, this would read from ct.templateDir filesystem)

	// Fall back to embedded defaults.
	content := ct.defaultTemplate(name)

	ct.mu.Lock()
	ct.cache[name] = content
	ct.mu.Unlock()

	return content
}

// defaultTemplate returns the built-in default template for the given name.
func (ct *CompactTemplates) defaultTemplate(name string) string {
	switch name {
	case "summary":
		return defaultSummaryTemplate
	case "continuation":
		return defaultContinuationTemplate
	default:
		return defaultSummaryTemplate
	}
}

// defaultSummaryTemplate is the built-in summary prompt template.
const defaultSummaryTemplate = `You are a conversation summarizer for an AI operations assistant.
Summarize the following conversation into a structured format. Preserve ALL technical details exactly.

<conversation>
{{conversation}}
</conversation>

<analysis>
Think step by step about what information must be preserved.
</analysis>

<summary>
Produce a summary with these sections:

1. **Primary Request and Intent**: What the user originally asked for.
2. **Target Environment**: Hosts, services, clusters, IPs, ports mentioned.
3. **Commands Executed**: Every command run and its outcome. Preserve exact error messages.
4. **Errors and Fixes**: Problems encountered and how they were resolved.
5. **Configuration Changes**: Exact file paths and changes made.
6. **Diagnostic Findings**: Metrics, logs, health check results.
7. **All User Messages**: Reproduce every user message verbatim.
8. **Pending Tasks**: What remains to be done.
9. **Current State + Next Step**: Where we are now and what to do next.
</summary>`

// defaultContinuationTemplate is the built-in continuation prompt template.
const defaultContinuationTemplate = `[Previous conversation was compressed]

{{summary}}

Please continue from where you left off.`
