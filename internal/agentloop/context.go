package agentloop

import (
	"encoding/json"
	"sync"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// ContextManager manages the conversation message history for an agent loop session.
// It provides thread-safe append/read operations, sanitization (ensuring tool_call/tool_result
// pairing), and a lazy-precision token estimation strategy.
type ContextManager struct {
	messages      []bifrost.Message
	contextWindow int // max context window size in tokens
	mu            sync.Mutex
}

// NewContextManager creates a ContextManager with the given context window size (in tokens).
func NewContextManager(contextWindow int) *ContextManager {
	return &ContextManager{
		contextWindow: contextWindow,
	}
}

// AppendSystem appends a system message.
func (cm *ContextManager) AppendSystem(content string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.messages = append(cm.messages, bifrost.Message{
		Role:    "system",
		Content: content,
	})
}

// AppendUser appends a user message.
func (cm *ContextManager) AppendUser(content string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.messages = append(cm.messages, bifrost.Message{
		Role:    "user",
		Content: content,
	})
}

// AppendAssistant appends an assistant message with optional tool calls.
func (cm *ContextManager) AppendAssistant(content string, toolCalls []bifrost.ToolCall) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.messages = append(cm.messages, bifrost.Message{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
	})
}

// AppendToolResult appends a tool result message with the given call ID.
func (cm *ContextManager) AppendToolResult(callID, result string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.messages = append(cm.messages, bifrost.Message{
		Role:       "tool",
		Content:    result,
		ToolCallID: callID,
	})
}

// Append appends a raw bifrost.Message to the history. Used by persistence
// restore and other low-level callers.
func (cm *ContextManager) Append(msg bifrost.Message) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.messages = append(cm.messages, msg)
}

// Messages returns a copy of the current message history.
func (cm *ContextManager) Messages() []bifrost.Message {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	out := make([]bifrost.Message, len(cm.messages))
	copy(out, cm.messages)
	return out
}

// ReplaceMessages replaces the entire message history.
func (cm *ContextManager) ReplaceMessages(msgs []bifrost.Message) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.messages = make([]bifrost.Message, len(msgs))
	copy(cm.messages, msgs)
}

// Len returns the number of messages.
func (cm *ContextManager) Len() int {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return len(cm.messages)
}

// Sanitize normalizes the message history by ensuring every tool_call has a
// corresponding tool result and removing orphan tool results.
// Ported from Codex context_manager/normalize.rs.
func (cm *ContextManager) Sanitize() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.ensureCallOutputsPresent()
	cm.removeOrphanOutputs()
}

// ensureCallOutputsPresent scans for assistant messages with tool_calls and
// ensures each tool_call has a corresponding tool message with matching ToolCallID.
// If missing, a stub message is inserted immediately after the assistant message.
func (cm *ContextManager) ensureCallOutputsPresent() {
	var result []bifrost.Message

	for i, msg := range cm.messages {
		result = append(result, msg)

		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}

		// Collect existing tool result IDs that follow this assistant message.
		existingIDs := make(map[string]bool)
		for j := i + 1; j < len(cm.messages); j++ {
			if cm.messages[j].Role == "tool" {
				existingIDs[cm.messages[j].ToolCallID] = true
			} else {
				// Stop at the next non-tool message.
				break
			}
		}

		// Insert stubs for any missing tool results.
		for _, tc := range msg.ToolCalls {
			if !existingIDs[tc.ID] {
				result = append(result, bifrost.Message{
					Role:       "tool",
					Content:    "[result not available]",
					ToolCallID: tc.ID,
				})
			}
		}
	}

	cm.messages = result
}

// removeOrphanOutputs removes tool messages whose ToolCallID doesn't match
// any tool_call in preceding assistant messages.
func (cm *ContextManager) removeOrphanOutputs() {
	// Build a set of all valid tool call IDs from assistant messages.
	validIDs := make(map[string]bool)
	for _, msg := range cm.messages {
		if msg.Role == "assistant" {
			for _, tc := range msg.ToolCalls {
				validIDs[tc.ID] = true
			}
		}
	}

	// Filter: keep non-tool messages and tool messages with valid IDs.
	var filtered []bifrost.Message
	for _, msg := range cm.messages {
		if msg.Role == "tool" && !validIDs[msg.ToolCallID] {
			continue // orphan tool result
		}
		filtered = append(filtered, msg)
	}
	cm.messages = filtered
}

// EstimateTokens returns an estimated token count for the current message history.
// It uses a lazy precision strategy:
//   - When far from the threshold (< 70% of contextWindow): rough estimate (4 chars ≈ 1 token)
//   - When close to the threshold (≥ 70%): more precise JSON-serialized byte count / 4
func (cm *ContextManager) EstimateTokens() int {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Quick rough estimate first.
	rough := cm.roughEstimate()

	threshold := int(float64(cm.contextWindow) * 0.70)
	if rough < threshold {
		return rough
	}

	// Close to threshold — use precise calculation.
	return cm.preciseEstimate()
}

// roughEstimate counts total characters across all messages / 4.
func (cm *ContextManager) roughEstimate() int {
	total := 0
	for _, msg := range cm.messages {
		total += cm.messageCharLen(msg)
	}
	return total / 4
}

// preciseEstimate serializes messages to JSON and counts bytes / 4.
func (cm *ContextManager) preciseEstimate() int {
	data, err := json.Marshal(cm.messages)
	if err != nil {
		// Fallback to rough estimate on marshal error.
		return cm.roughEstimate()
	}
	return len(data) / 4
}

// messageCharLen returns the character length of a message's content plus tool call data.
func (cm *ContextManager) messageCharLen(msg bifrost.Message) int {
	n := len(msg.Role)
	switch v := msg.Content.(type) {
	case string:
		n += len(v)
	default:
		// For non-string content, marshal it.
		if data, err := json.Marshal(v); err == nil {
			n += len(data)
		}
	}
	for _, tc := range msg.ToolCalls {
		n += len(tc.ID) + len(tc.Function.Name) + len(tc.Function.Arguments)
	}
	if msg.ToolCallID != "" {
		n += len(msg.ToolCallID)
	}
	return n
}
