package agentloop

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// ─── Constants ───────────────────────────────────────────────────────────────

const (
	// MaxSingleToolResultChars is the max character count for a single tool result
	// before it gets truncated (L1).
	MaxSingleToolResultChars = 50_000

	// MaxToolResultsPerMsgChars is the aggregate character budget for all tool
	// results in a single message.
	MaxToolResultsPerMsgChars = 200_000

	// PersistPreviewChars is the number of characters kept as a preview when a
	// tool result is truncated and persisted to disk.
	PersistPreviewChars = 2_000

	// CompressThresholdRatio is the fraction of usable context at which
	// automatic compression (L4) kicks in.
	CompressThresholdRatio = 0.83

	// CompressBufferTokens is the token headroom reserved for the model's
	// response and tool definitions.
	CompressBufferTokens = 13_000
)

// FileUnchangedStub is the placeholder returned by L2 when a file's content
// has not changed since the last read.
const FileUnchangedStub = "File unchanged since last read..."

// readOnlyTools lists tool names whose results can be safely discarded during
// L3 micro-compaction.
var readOnlyTools = map[string]bool{
	"host_summary":    true,
	"host_file_read":  true,
	"coroot_metrics":  true,
	"read_file":       true,
	"list_files":      true,
	"search_files":    true,
}

// ─── FileStateCache (L2) ─────────────────────────────────────────────────────

// FileStateCache tracks content hashes for files so that repeated reads of
// unchanged files can be replaced with a short stub.
type FileStateCache struct {
	mu     sync.Mutex
	hashes map[string]string // filePath → SHA-256 hex
}

// NewFileStateCache creates an empty FileStateCache.
func NewFileStateCache() *FileStateCache {
	return &FileStateCache{hashes: make(map[string]string)}
}

// Check returns true if the file at path has already been seen with identical
// content (i.e. the content is unchanged).
func (c *FileStateCache) Check(path, content string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	h := hashContent(content)
	prev, exists := c.hashes[path]
	return exists && prev == h
}

// Update records (or overwrites) the content hash for the given path.
func (c *FileStateCache) Update(path, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hashes[path] = hashContent(content)
}

func hashContent(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}

// ─── Compressor ──────────────────────────────────────────────────────────────

// Compressor implements the five-layer context explosion prevention system.
type Compressor struct {
	gateway       *bifrost.Gateway
	contextWindow int
	summaryModel  string
	fileCache     *FileStateCache
	frozenResults map[string]bool // tool_call_id → true (truncated) / false (kept)
}

// NewCompressor creates a Compressor wired to the given Bifrost gateway.
func NewCompressor(gateway *bifrost.Gateway, contextWindow int, summaryModel string) *Compressor {
	return &Compressor{
		gateway:       gateway,
		contextWindow: contextWindow,
		summaryModel:  summaryModel,
		fileCache:     NewFileStateCache(),
		frozenResults: make(map[string]bool),
	}
}

// ─── L1: Source Truncation ───────────────────────────────────────────────────

// truncateLargeToolResults scans tool messages and truncates any whose content
// exceeds MaxSingleToolResultChars. The full content is conceptually "persisted
// to disk" (caller is responsible for actual persistence); the message is
// replaced with a PersistPreviewChars preview plus a truncation notice.
//
// Decision freezing: once a tool_call_id has been processed, the same decision
// (truncate or keep) is reused on subsequent calls so that the prompt-cache
// prefix stays byte-stable.
func (c *Compressor) truncateLargeToolResults(msgs []bifrost.Message) []bifrost.Message {
	out := make([]bifrost.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role != "tool" {
			out = append(out, m)
			continue
		}

		content := messageContentString(m)
		id := m.ToolCallID

		// Check frozen decision.
		if frozen, exists := c.frozenResults[id]; exists {
			if frozen {
				// Was truncated before — keep the truncated version.
				out = append(out, m)
			} else {
				// Was kept before — keep as-is.
				out = append(out, m)
			}
			continue
		}

		// First time seeing this tool_call_id — decide now.
		if len(content) > MaxSingleToolResultChars {
			preview := content[:PersistPreviewChars]
			m.Content = preview + "\n\n[truncated, full content saved to disk]"
			c.frozenResults[id] = true
		} else {
			c.frozenResults[id] = false
		}
		out = append(out, m)
	}
	return out
}

// ─── L3: Micro-compaction ────────────────────────────────────────────────────

// microcompact removes old read-only tool results while preserving write tool
// results. Only results from older turns are removed; the most recent turn's
// results are always kept.
//
// A "turn" boundary is detected by the presence of a user or assistant message
// after tool messages.
func (c *Compressor) microcompact(msgs []bifrost.Message) []bifrost.Message {
	if len(msgs) == 0 {
		return msgs
	}

	// Find the index of the last user message — everything from there onward
	// is the "current turn" and must be preserved.
	lastUserIdx := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			lastUserIdx = i
			break
		}
	}

	out := make([]bifrost.Message, 0, len(msgs))
	for i, m := range msgs {
		if m.Role == "tool" && i < lastUserIdx {
			// Old tool result — check if it's read-only.
			toolName := resolveToolName(msgs, m.ToolCallID)
			if readOnlyTools[toolName] {
				// Replace with a compact stub.
				stub := m
				stub.Content = "[old read result removed]"
				out = append(out, stub)
				continue
			}
		}
		out = append(out, m)
	}
	return out
}

// resolveToolName walks backwards from a tool message to find the assistant
// message that issued the matching tool_call, and returns the function name.
func resolveToolName(msgs []bifrost.Message, toolCallID string) string {
	for _, m := range msgs {
		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				if tc.ID == toolCallID {
					return tc.Function.Name
				}
			}
		}
	}
	return ""
}

// ─── L4: Auto-compression (structured summary) ──────────────────────────────

// summaryPromptTemplate is the 9-dimension structured summary prompt tailored
// for ops scenarios.
const summaryPromptTemplate = `You are a conversation summarizer for an AI operations assistant.
Summarize the following conversation into a structured format. Preserve ALL technical details exactly.

<conversation>
%s
</conversation>

<analysis>
Think step by step about what information must be preserved.
</analysis>

<summary>
Produce a summary with these 9 sections:

1. **Primary Request and Intent**: What the user originally asked for.
2. **Target Environment**: Hosts, services, clusters, IPs, ports mentioned.
3. **Commands Executed**: Every command run and its outcome. Preserve exact error messages.
4. **Errors and Fixes**: Problems encountered and how they were resolved.
5. **Configuration Changes**: Exact file paths and changes made.
6. **Diagnostic Findings**: Metrics, logs, health check results.
7. **All User Messages**: Reproduce every user message verbatim.
8. **Pending Tasks**: What remains to be done.
9. **Current State + Next Step**: Where we are now and what to do next. Quote the last assistant message verbatim.
</summary>`

// generateSummary calls the LLM (via the Bifrost gateway) to produce a
// 9-dimension structured summary of the conversation so far.
func (c *Compressor) generateSummary(ctx context.Context, msgs []bifrost.Message) (string, error) {
	// Build a textual representation of the conversation.
	var sb strings.Builder
	for _, m := range msgs {
		if m.Role == "system" {
			continue // don't include system prompt in summary input
		}
		sb.WriteString(fmt.Sprintf("[%s", m.Role))
		if m.ToolCallID != "" {
			sb.WriteString(fmt.Sprintf(" tool_call_id=%s", m.ToolCallID))
		}
		sb.WriteString("]\n")
		sb.WriteString(messageContentString(m))
		sb.WriteString("\n\n")
	}

	prompt := fmt.Sprintf(summaryPromptTemplate, sb.String())

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
		return "", fmt.Errorf("compressor: summary generation failed: %w", err)
	}

	return messageContentString(resp.Message), nil
}

// ─── L5: Head truncation (last resort) ──────────────────────────────────────

// truncateHeadForRetry groups messages into API turns and drops the oldest
// turns first. It always keeps the system message and at least the most recent
// turn. It performs at most 3 iterations of dropping.
func (c *Compressor) truncateHeadForRetry(msgs []bifrost.Message) []bifrost.Message {
	const maxDropIterations = 3

	// Separate system messages from the rest.
	var systemMsgs []bifrost.Message
	var rest []bifrost.Message
	for _, m := range msgs {
		if m.Role == "system" {
			systemMsgs = append(systemMsgs, m)
		} else {
			rest = append(rest, m)
		}
	}

	// Group rest into turns. A turn starts with a user message.
	turns := groupIntoTurns(rest)

	for i := 0; i < maxDropIterations; i++ {
		if len(turns) <= 1 {
			break // keep at least the most recent turn
		}
		// Drop the oldest turn.
		turns = turns[1:]
	}

	// Reassemble: system messages + remaining turns.
	result := make([]bifrost.Message, 0, len(systemMsgs)+countTurnMessages(turns))
	result = append(result, systemMsgs...)
	for _, turn := range turns {
		result = append(result, turn...)
	}
	return result
}

// groupIntoTurns splits a message slice into turns. Each turn starts at a
// "user" message and includes all subsequent assistant/tool messages until the
// next user message.
func groupIntoTurns(msgs []bifrost.Message) [][]bifrost.Message {
	if len(msgs) == 0 {
		return nil
	}

	var turns [][]bifrost.Message
	var current []bifrost.Message

	for _, m := range msgs {
		if m.Role == "user" && len(current) > 0 {
			turns = append(turns, current)
			current = nil
		}
		current = append(current, m)
	}
	if len(current) > 0 {
		turns = append(turns, current)
	}
	return turns
}

func countTurnMessages(turns [][]bifrost.Message) int {
	n := 0
	for _, t := range turns {
		n += len(t)
	}
	return n
}

// ─── Main entry points ──────────────────────────────────────────────────────

// ShouldCompress returns true when the estimated token count is high enough
// that compression should be attempted.
func (c *Compressor) ShouldCompress(estimatedTokens int) bool {
	usable := c.contextWindow - CompressBufferTokens
	if usable <= 0 {
		return true
	}
	threshold := int(float64(usable) * CompressThresholdRatio)
	return estimatedTokens > threshold
}

// Compact runs the five-layer compression pipeline on the ContextManager's
// message history:
//
//	L1 → L2 → L3 → (check) → L4 → (check) → L5
func (c *Compressor) Compact(ctx context.Context, cm *ContextManager) error {
	msgs := cm.Messages()

	// L1: truncate oversized tool results.
	msgs = c.truncateLargeToolResults(msgs)

	// L2: deduplicate unchanged file reads.
	msgs = c.deduplicateFileReads(msgs)

	// L3: micro-compact old read-only tool results.
	msgs = c.microcompact(msgs)

	cm.ReplaceMessages(msgs)

	// Re-estimate after lightweight layers.
	if !c.ShouldCompress(cm.EstimateTokens()) {
		return nil
	}

	// L4: generate a structured summary and replace history.
	summary, err := c.generateSummary(ctx, cm.Messages())
	if err != nil {
		// L4 failed — fall through to L5.
		_ = err
	} else {
		// Replace all non-system messages with the summary + a continuation
		// instruction so the model picks up where it left off.
		var systemMsgs []bifrost.Message
		for _, m := range cm.Messages() {
			if m.Role == "system" {
				systemMsgs = append(systemMsgs, m)
			}
		}
		compressed := make([]bifrost.Message, 0, len(systemMsgs)+1)
		compressed = append(compressed, systemMsgs...)
		compressed = append(compressed, bifrost.Message{
			Role:    "user",
			Content: "[Previous conversation was compressed]\n\n" + summary + "\n\nPlease continue from where you left off.",
		})
		cm.ReplaceMessages(compressed)
	}

	// Re-check after L4.
	if !c.ShouldCompress(cm.EstimateTokens()) {
		return nil
	}

	// L5: head truncation as last resort.
	msgs = cm.Messages()
	msgs = c.truncateHeadForRetry(msgs)
	cm.ReplaceMessages(msgs)

	return nil
}

// deduplicateFileReads applies L2 deduplication: for tool results that look
// like file reads, if the content hash matches any previously seen read of the
// same content the result is replaced with FileUnchangedStub.
// We key the cache by the content hash itself so that reads of the same file
// (possibly via different tool_call_ids) are correctly deduplicated.
func (c *Compressor) deduplicateFileReads(msgs []bifrost.Message) []bifrost.Message {
	out := make([]bifrost.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "tool" {
			toolName := resolveToolName(msgs, m.ToolCallID)
			if toolName == "read_file" || toolName == "host_file_read" {
				content := messageContentString(m)
				h := hashContent(content)
				if c.fileCache.Check(h, content) {
					stub := m
					stub.Content = FileUnchangedStub
					out = append(out, stub)
					continue
				}
				c.fileCache.Update(h, content)
			}
		}
		out = append(out, m)
	}
	return out
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// messageContentString extracts the string content from a Message.
// If Content is not a string it returns an empty string.
func messageContentString(m bifrost.Message) string {
	if s, ok := m.Content.(string); ok {
		return s
	}
	return ""
}
