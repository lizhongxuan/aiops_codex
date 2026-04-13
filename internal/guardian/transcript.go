package guardian

import (
	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

const (
	// MaxMessageTranscriptTokens is the token budget for message entries.
	MaxMessageTranscriptTokens = 10000
	// MaxToolTranscriptTokens is the token budget for tool entries.
	MaxToolTranscriptTokens = 10000
	// MaxRecentEntries is the maximum number of recent entries to include.
	MaxRecentEntries = 40
	// MaxMessageEntryTokens is the per-entry token limit for messages.
	MaxMessageEntryTokens = 2000
	// MaxToolEntryTokens is the per-entry token limit for tool results.
	MaxToolEntryTokens = 1000
)

// TranscriptEntry represents a single entry in the guardian transcript.
type TranscriptEntry struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Tokens  int    `json:"tokens"`
}

// GuardianApprovalRequest contains the details of the operation being reviewed.
type GuardianApprovalRequest struct {
	ToolName    string `json:"tool_name"`
	Arguments   string `json:"arguments"`
	Description string `json:"description"`
}

// BuildTranscript constructs a compact transcript from conversation messages
// and the pending approval request. It enforces token budgets:
// - Message entries: 10,000 tokens total, 2,000 per entry
// - Tool entries: 10,000 tokens total, 1,000 per entry
// - Maximum 40 most recent entries; older entries are truncated.
func BuildTranscript(messages []bifrost.Message, request GuardianApprovalRequest) []TranscriptEntry {
	var messageEntries []TranscriptEntry
	var toolEntries []TranscriptEntry

	for _, msg := range messages {
		content := extractContent(msg)
		tokens := estimateTokens(content)

		if msg.Role == "tool" {
			if tokens > MaxToolEntryTokens {
				content = truncateToTokens(content, MaxToolEntryTokens)
				tokens = MaxToolEntryTokens
			}
			toolEntries = append(toolEntries, TranscriptEntry{
				Role:    msg.Role,
				Content: content,
				Tokens:  tokens,
			})
		} else {
			if tokens > MaxMessageEntryTokens {
				content = truncateToTokens(content, MaxMessageEntryTokens)
				tokens = MaxMessageEntryTokens
			}
			messageEntries = append(messageEntries, TranscriptEntry{
				Role:    msg.Role,
				Content: content,
				Tokens:  tokens,
			})
		}
	}

	// Trim to most recent MaxRecentEntries across both categories.
	messageEntries = trimToRecent(messageEntries, MaxRecentEntries)
	toolEntries = trimToRecent(toolEntries, MaxRecentEntries)

	// Enforce total token budgets.
	messageEntries = enforceTokenBudget(messageEntries, MaxMessageTranscriptTokens)
	toolEntries = enforceTokenBudget(toolEntries, MaxToolTranscriptTokens)

	// Combine: messages first, then tools, then the pending request.
	result := make([]TranscriptEntry, 0, len(messageEntries)+len(toolEntries)+1)
	result = append(result, messageEntries...)
	result = append(result, toolEntries...)

	// Append the pending approval request as a final entry.
	requestContent := "PENDING APPROVAL: tool=" + request.ToolName
	if request.Arguments != "" {
		requestContent += " args=" + request.Arguments
	}
	if request.Description != "" {
		requestContent += " desc=" + request.Description
	}
	result = append(result, TranscriptEntry{
		Role:    "system",
		Content: requestContent,
		Tokens:  estimateTokens(requestContent),
	})

	return result
}

// trimToRecent keeps only the most recent n entries.
func trimToRecent(entries []TranscriptEntry, n int) []TranscriptEntry {
	if len(entries) <= n {
		return entries
	}
	return entries[len(entries)-n:]
}

// enforceTokenBudget removes oldest entries until total tokens fit within budget.
func enforceTokenBudget(entries []TranscriptEntry, budget int) []TranscriptEntry {
	total := 0
	for _, e := range entries {
		total += e.Tokens
	}
	// Remove from the front (oldest) until within budget.
	for total > budget && len(entries) > 0 {
		total -= entries[0].Tokens
		entries = entries[1:]
	}
	return entries
}

// extractContent converts a bifrost.Message content to a string.
func extractContent(msg bifrost.Message) string {
	if msg.Content == nil {
		return ""
	}
	switch v := msg.Content.(type) {
	case string:
		return v
	default:
		return ""
	}
}

// estimateTokens provides a rough token count (approx 4 chars per token).
func estimateTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	tokens := len(s) / 4
	if tokens == 0 {
		tokens = 1
	}
	return tokens
}

// truncateToTokens truncates a string to approximately the given token count.
func truncateToTokens(s string, maxTokens int) string {
	maxChars := maxTokens * 4
	if len(s) <= maxChars {
		return s
	}
	return s[:maxChars]
}
