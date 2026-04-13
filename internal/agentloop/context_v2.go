package agentloop

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// ─── Task 11.8: Enhanced Context Manager V2 ─────────────────────────────────

// TrackedMessage wraps a bifrost.Message with per-item token count tracking.
type TrackedMessage struct {
	bifrost.Message
	// TokenCount is the estimated token count for this message.
	TokenCount int `json:"token_count"`
	// Pinned messages are never truncated.
	Pinned bool `json:"pinned,omitempty"`
	// Source identifies where this message came from (e.g., "user", "agent:Atlas", "reference").
	Source string `json:"source,omitempty"`
}

// TruncationPolicy defines how messages should be truncated when context is full.
type TruncationPolicy struct {
	// Strategy is the truncation strategy: "oldest_first", "middle_out", "tool_results_first".
	Strategy string `json:"strategy"`
	// PreserveSystemMessages keeps system messages intact.
	PreserveSystemMessages bool `json:"preserve_system_messages"`
	// PreserveLastN keeps the last N messages regardless of strategy.
	PreserveLastN int `json:"preserve_last_n"`
	// MaxTokenBudget is the target token count after truncation.
	MaxTokenBudget int `json:"max_token_budget"`
}

// DefaultTruncationPolicy returns a sensible default policy.
func DefaultTruncationPolicy() TruncationPolicy {
	return TruncationPolicy{
		Strategy:               "oldest_first",
		PreserveSystemMessages: true,
		PreserveLastN:          4,
		MaxTokenBudget:         100000,
	}
}

// ContextManagerV2 extends ContextManager with per-message token tracking,
// truncation policies, and inter-agent content handling.
type ContextManagerV2 struct {
	mu            sync.Mutex
	tracked       []TrackedMessage
	contextWindow int
	policy        TruncationPolicy
	references    map[string]*ReferenceItem
}

// NewContextManagerV2 creates an enhanced context manager.
func NewContextManagerV2(contextWindow int) *ContextManagerV2 {
	return &ContextManagerV2{
		contextWindow: contextWindow,
		policy:        DefaultTruncationPolicy(),
		references:    make(map[string]*ReferenceItem),
	}
}

// SetTruncationPolicy updates the truncation policy.
func (cm *ContextManagerV2) SetTruncationPolicy(policy TruncationPolicy) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.policy = policy
}

// AppendTracked adds a message with automatic token estimation.
func (cm *ContextManagerV2) AppendTracked(msg bifrost.Message, source string, pinned bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	tokens := estimateMessageTokens(msg)
	cm.tracked = append(cm.tracked, TrackedMessage{
		Message:    msg,
		TokenCount: tokens,
		Pinned:     pinned,
		Source:     source,
	})
}

// AppendUserV2 appends a user message with source tracking.
func (cm *ContextManagerV2) AppendUserV2(content, source string) {
	cm.AppendTracked(bifrost.Message{Role: "user", Content: content}, source, false)
}

// AppendAssistantV2 appends an assistant message with source tracking.
func (cm *ContextManagerV2) AppendAssistantV2(content string, toolCalls []bifrost.ToolCall, source string) {
	cm.AppendTracked(bifrost.Message{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
	}, source, false)
}

// AppendToolResultV2 appends a tool result with source tracking.
func (cm *ContextManagerV2) AppendToolResultV2(callID, result, source string) {
	cm.AppendTracked(bifrost.Message{
		Role:       "tool",
		Content:    result,
		ToolCallID: callID,
	}, source, false)
}

// TrackedMessages returns a copy of all tracked messages.
func (cm *ContextManagerV2) TrackedMessages() []TrackedMessage {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	out := make([]TrackedMessage, len(cm.tracked))
	copy(out, cm.tracked)
	return out
}

// Messages returns the underlying bifrost messages.
func (cm *ContextManagerV2) Messages() []bifrost.Message {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	msgs := make([]bifrost.Message, len(cm.tracked))
	for i, t := range cm.tracked {
		msgs[i] = t.Message
	}
	return msgs
}

// TotalTokens returns the sum of all tracked message token counts.
func (cm *ContextManagerV2) TotalTokens() int {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	total := 0
	for _, t := range cm.tracked {
		total += t.TokenCount
	}
	return total
}

// ApplyTruncation applies the configured truncation policy to reduce context size.
func (cm *ContextManagerV2) ApplyTruncation() int {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	budget := cm.policy.MaxTokenBudget
	if budget <= 0 {
		budget = cm.contextWindow - CompressBufferTokens
	}

	total := 0
	for _, t := range cm.tracked {
		total += t.TokenCount
	}
	if total <= budget {
		return 0 // No truncation needed.
	}

	removed := 0
	switch cm.policy.Strategy {
	case "tool_results_first":
		removed = cm.truncateToolResultsFirst(budget)
	case "middle_out":
		removed = cm.truncateMiddleOut(budget)
	default: // "oldest_first"
		removed = cm.truncateOldestFirst(budget)
	}
	return removed
}

func (cm *ContextManagerV2) truncateOldestFirst(budget int) int {
	preserveN := cm.policy.PreserveLastN
	if preserveN > len(cm.tracked) {
		preserveN = len(cm.tracked)
	}

	// Calculate current total.
	total := 0
	for _, t := range cm.tracked {
		total += t.TokenCount
	}

	removed := 0
	// Remove from the front (oldest), skipping pinned and system messages.
	cutoff := len(cm.tracked) - preserveN
	var kept []TrackedMessage
	for i, t := range cm.tracked {
		if i < cutoff && !t.Pinned && !(cm.policy.PreserveSystemMessages && t.Role == "system") && total > budget {
			total -= t.TokenCount
			removed++
		} else {
			kept = append(kept, t)
		}
	}
	cm.tracked = kept
	return removed
}

func (cm *ContextManagerV2) truncateToolResultsFirst(budget int) int {
	total := 0
	for _, t := range cm.tracked {
		total += t.TokenCount
	}

	removed := 0
	var kept []TrackedMessage
	preserveN := cm.policy.PreserveLastN
	cutoff := len(cm.tracked) - preserveN

	for i, t := range cm.tracked {
		if i < cutoff && t.Role == "tool" && !t.Pinned && total > budget {
			total -= t.TokenCount
			removed++
		} else {
			kept = append(kept, t)
		}
	}
	cm.tracked = kept

	// If still over budget, fall back to oldest_first.
	if total > budget {
		removed += cm.truncateOldestFirst(budget)
	}
	return removed
}

func (cm *ContextManagerV2) truncateMiddleOut(budget int) int {
	total := 0
	for _, t := range cm.tracked {
		total += t.TokenCount
	}

	if total <= budget {
		return 0
	}

	preserveN := cm.policy.PreserveLastN
	// Keep first few and last few, remove from middle.
	keepFront := 2 // system + first user
	keepBack := preserveN
	if keepFront+keepBack >= len(cm.tracked) {
		return 0
	}

	middle := cm.tracked[keepFront : len(cm.tracked)-keepBack]
	removed := 0
	var keptMiddle []TrackedMessage
	for _, t := range middle {
		if !t.Pinned && !(cm.policy.PreserveSystemMessages && t.Role == "system") && total > budget {
			total -= t.TokenCount
			removed++
		} else {
			keptMiddle = append(keptMiddle, t)
		}
	}

	result := make([]TrackedMessage, 0, keepFront+len(keptMiddle)+keepBack)
	result = append(result, cm.tracked[:keepFront]...)
	result = append(result, keptMiddle...)
	result = append(result, cm.tracked[len(cm.tracked)-keepBack:]...)
	cm.tracked = result
	return removed
}

// estimateMessageTokens estimates token count for a single message.
func estimateMessageTokens(msg bifrost.Message) int {
	n := len(msg.Role) + 4 // role overhead
	switch v := msg.Content.(type) {
	case string:
		n += len(v)
	default:
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
	return n / 4 // ~4 chars per token
}

// ─── Task 11.9: Inter-agent content handling in history normalization ────────

// NormalizeInterAgentContent processes messages from child agents and normalizes
// them for inclusion in the parent's context. It prefixes content with the
// agent source identifier and handles role mapping.
func NormalizeInterAgentContent(msgs []TrackedMessage) []TrackedMessage {
	out := make([]TrackedMessage, 0, len(msgs))
	for _, m := range msgs {
		normalized := m
		if m.Source != "" && m.Source != "user" {
			// Prefix content with source attribution for inter-agent messages.
			if s, ok := m.Content.(string); ok && s != "" {
				normalized.Message.Content = fmt.Sprintf("[From %s]: %s", m.Source, s)
			}
		}
		// Ensure role consistency: child agent assistant messages become
		// user messages in parent context (they're "input" to the parent).
		if strings.HasPrefix(m.Source, "agent:") && m.Role == "assistant" {
			normalized.Message.Role = "user"
		}
		out = append(out, normalized)
	}
	return out
}

// MergeChildHistory takes a child agent's tracked messages and merges them
// into the parent context as a summarized block.
func MergeChildHistory(parent *ContextManagerV2, childMsgs []TrackedMessage, childNickname string) {
	if len(childMsgs) == 0 {
		return
	}

	// Build a summary of the child's work.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[Agent %s completed task]\n", childNickname))

	for _, m := range childMsgs {
		if m.Role == "assistant" {
			if s, ok := m.Content.(string); ok && s != "" {
				// Include the last assistant message as the result.
				sb.WriteString(s)
				break
			}
		}
	}

	source := fmt.Sprintf("agent:%s", childNickname)
	parent.AppendUserV2(sb.String(), source)
}

// ─── Task 11.10: Reference Context Items ────────────────────────────────────

// ReferenceItem represents a referenceable context item (file, diff, etc.).
type ReferenceItem struct {
	// ID is the unique reference identifier.
	ID string `json:"id"`
	// Type is the reference type: "file", "diff", "snippet", "url".
	Type string `json:"type"`
	// Path is the file path (for file/diff types).
	Path string `json:"path,omitempty"`
	// Content is the resolved content.
	Content string `json:"content"`
	// Version tracks content changes for diff injection.
	Version int `json:"version"`
	// PreviousContent holds the prior version for diff computation.
	PreviousContent string `json:"-"`
}

// AddReference registers a reference item in the context manager.
func (cm *ContextManagerV2) AddReference(item ReferenceItem) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if existing, ok := cm.references[item.ID]; ok {
		existing.PreviousContent = existing.Content
		existing.Content = item.Content
		existing.Version++
	} else {
		item.Version = 1
		cm.references[item.ID] = &item
	}
}

// ResolveReference retrieves a reference item and optionally generates a diff
// if the content has changed since last resolution.
func (cm *ContextManagerV2) ResolveReference(id string) (*ResolvedReference, bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	item, ok := cm.references[id]
	if !ok {
		return nil, false
	}

	resolved := &ResolvedReference{
		ID:      item.ID,
		Type:    item.Type,
		Path:    item.Path,
		Content: item.Content,
		Version: item.Version,
	}

	// Generate diff if content has changed.
	if item.PreviousContent != "" && item.PreviousContent != item.Content {
		resolved.Diff = generateSimpleDiff(item.PreviousContent, item.Content)
		resolved.HasDiff = true
	}

	return resolved, true
}

// ResolvedReference is the result of resolving a reference item.
type ResolvedReference struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Path    string `json:"path,omitempty"`
	Content string `json:"content"`
	Version int    `json:"version"`
	Diff    string `json:"diff,omitempty"`
	HasDiff bool   `json:"has_diff"`
}

// ListReferences returns all registered reference IDs.
func (cm *ContextManagerV2) ListReferences() []string {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	ids := make([]string, 0, len(cm.references))
	for id := range cm.references {
		ids = append(ids, id)
	}
	return ids
}

// RemoveReference removes a reference item.
func (cm *ContextManagerV2) RemoveReference(id string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.references, id)
}

// generateSimpleDiff produces a basic line-level diff between old and new content.
func generateSimpleDiff(old, new string) string {
	oldLines := strings.Split(old, "\n")
	newLines := strings.Split(new, "\n")

	var diff strings.Builder
	// Simple diff: show removed and added lines.
	oldSet := make(map[string]bool)
	for _, l := range oldLines {
		oldSet[l] = true
	}
	newSet := make(map[string]bool)
	for _, l := range newLines {
		newSet[l] = true
	}

	for _, l := range oldLines {
		if !newSet[l] {
			diff.WriteString("- " + l + "\n")
		}
	}
	for _, l := range newLines {
		if !oldSet[l] {
			diff.WriteString("+ " + l + "\n")
		}
	}
	return diff.String()
}
