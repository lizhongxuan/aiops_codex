package server

import (
	"fmt"
	"log"
	"strings"
)

const (
	// maxToolResultChars is the maximum number of characters allowed in a single tool result.
	maxToolResultChars = 200000

	// maxContextChars is the total character budget for all messages in the context window.
	maxContextChars = 800000

	// maxConsecutiveCompactFailures is the threshold before circuit-breaking the compaction loop.
	maxConsecutiveCompactFailures = 3
)

// enforceToolResultBudget truncates tool results that exceed the character budget.
// Returns the truncated content with head/tail preserved and an evidence ID reference
// so the full output can still be retrieved from the evidence store.
func enforceToolResultBudget(content string, evidenceID string) string {
	if len(content) <= maxToolResultChars {
		return content
	}
	head := content[:1000]
	tail := content[len(content)-1000:]
	return fmt.Sprintf("%s\n\n[... truncated %d characters, full output in evidence %s ...]\n\n%s",
		head, len(content)-2000, evidenceID, tail)
}

// summarizeCommandOutput creates a compact summary of command output for context.
// It preserves the first and last 5 lines, any error/fatal/panic lines (up to 10),
// the exit code, duration, and a reference to the full evidence artifact.
func summarizeCommandOutput(output string, exitCode int, durationMS int64, evidenceID string) string {
	lines := strings.Split(output, "\n")

	// Extract first 5 lines.
	firstLines := lines
	if len(firstLines) > 5 {
		firstLines = firstLines[:5]
	}

	// Extract last 5 lines.
	var lastLines []string
	if len(lines) > 5 {
		lastLines = lines[len(lines)-5:]
	} else {
		lastLines = lines
	}

	// Collect error lines (lines containing error/fatal/panic keywords).
	var errorLines []string
	errorKeywords := []string{"error", "Error", "ERROR", "fatal", "FATAL", "panic", "PANIC"}
	for _, line := range lines {
		for _, kw := range errorKeywords {
			if strings.Contains(line, kw) {
				errorLines = append(errorLines, line)
				break
			}
		}
		if len(errorLines) >= 10 {
			break
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Exit code: %d | Duration: %dms | Evidence: %s\n", exitCode, durationMS, evidenceID)

	b.WriteString("--- First lines ---\n")
	for _, l := range firstLines {
		b.WriteString(l)
		b.WriteByte('\n')
	}

	if len(errorLines) > 0 {
		b.WriteString("--- Error lines ---\n")
		for _, l := range errorLines {
			b.WriteString(l)
			b.WriteByte('\n')
		}
	}

	b.WriteString("--- Last lines ---\n")
	for _, l := range lastLines {
		b.WriteString(l)
		b.WriteByte('\n')
	}

	return b.String()
}

// microcompactMessages replaces old tool result content with compact placeholders.
// Only messages older than the last keepRecentTurns turns are compacted, and only
// when their content exceeds 500 characters. This keeps recent context intact while
// freeing budget from stale, verbose tool outputs.
func microcompactMessages(messages []map[string]any, keepRecentTurns int) []map[string]any {
	if len(messages) == 0 {
		return messages
	}

	// Determine the boundary index: messages before this are candidates for compaction.
	boundary := len(messages) - keepRecentTurns
	if boundary < 0 {
		boundary = 0
	}

	result := make([]map[string]any, len(messages))
	for i, msg := range messages {
		result[i] = msg

		if i >= boundary {
			continue // keep recent turns intact
		}

		role, _ := msg["role"].(string)
		if role != "tool_result" {
			continue
		}

		content, _ := msg["content"].(string)
		if len(content) <= 500 {
			continue
		}

		// Extract evidence ID if present in the content.
		evidenceID := extractEvidenceID(content)

		compacted := make(map[string]any, len(msg))
		for k, v := range msg {
			compacted[k] = v
		}
		compacted["content"] = fmt.Sprintf("[compacted: see evidence %s]", evidenceID)
		result[i] = compacted
	}

	return result
}

// extractEvidenceID attempts to find an evidence ID reference in content.
// It looks for the pattern "evidence <id>" and returns the ID, or "unknown" if not found.
func extractEvidenceID(content string) string {
	const marker = "evidence "
	idx := strings.Index(content, marker)
	if idx == -1 {
		return "unknown"
	}
	start := idx + len(marker)
	end := start
	for end < len(content) && content[end] != ' ' && content[end] != '\n' && content[end] != ']' && content[end] != ')' {
		end++
	}
	if end > start {
		return content[start:end]
	}
	return "unknown"
}

// totalContextChars sums the character length of all message content in the slice.
func totalContextChars(messages []map[string]any) int {
	total := 0
	for _, msg := range messages {
		if content, ok := msg["content"].(string); ok {
			total += len(content)
		}
	}
	return total
}

// autoCompactIfNeeded checks if total context exceeds the character budget and
// triggers compaction. It first tries microcompaction on older turns. If that is
// insufficient it summarises the oldest messages and replaces them with a single
// condensed system message. Consecutive failures are tracked so the caller can
// circuit-break if compaction keeps failing.
// Returns the (possibly compacted) messages, whether compaction was performed, and any error.
func autoCompactIfNeeded(messages []map[string]any, consecutiveFailures *int) ([]map[string]any, bool, error) {
	currentSize := totalContextChars(messages)
	if currentSize <= maxContextChars {
		*consecutiveFailures = 0
		return messages, false, nil
	}

	log.Printf("[context_compact] context size %d exceeds budget %d, attempting compaction", currentSize, maxContextChars)

	// Phase 1: micro-compact old tool results (keep last 6 turns intact).
	compacted := microcompactMessages(messages, 6)
	if totalContextChars(compacted) <= maxContextChars {
		*consecutiveFailures = 0
		log.Printf("[context_compact] microcompaction sufficient, new size %d", totalContextChars(compacted))
		return compacted, true, nil
	}

	// Phase 2: summarise oldest messages into a single condensed message.
	compacted, err := summarizeOldestMessages(compacted)
	if err != nil {
		*consecutiveFailures++
		return compacted, false, fmt.Errorf("compaction failed: %w", err)
	}

	newSize := totalContextChars(compacted)
	if newSize > maxContextChars {
		*consecutiveFailures++
		log.Printf("[context_compact] compaction reduced to %d but still over budget", newSize)
		return compacted, true, fmt.Errorf("compaction reduced context to %d chars but budget is %d", newSize, maxContextChars)
	}

	*consecutiveFailures = 0
	log.Printf("[context_compact] compaction successful, new size %d", newSize)
	return compacted, true, nil
}

// summarizeOldestMessages takes the oldest half of messages and replaces them
// with a single summary message, preserving the system prompt (first message)
// and all recent messages.
func summarizeOldestMessages(messages []map[string]any) ([]map[string]any, error) {
	if len(messages) < 4 {
		return messages, fmt.Errorf("too few messages to summarize (%d)", len(messages))
	}

	// Keep the system prompt (index 0) and the recent half of the conversation.
	midpoint := len(messages) / 2
	if midpoint < 1 {
		midpoint = 1
	}

	oldMessages := messages[1:midpoint]
	recentMessages := messages[midpoint:]

	// Build a condensed summary of the old messages.
	var summary strings.Builder
	summary.WriteString("[Context compacted] The following is a summary of earlier conversation turns:\n")

	turnCount := 0
	for _, msg := range oldMessages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)

		// Truncate each old message to a brief excerpt.
		excerpt := content
		if len(excerpt) > 200 {
			excerpt = excerpt[:200] + "..."
		}
		fmt.Fprintf(&summary, "- %s: %s\n", role, excerpt)
		turnCount++
	}
	fmt.Fprintf(&summary, "[%d turns compacted]\n", turnCount)

	summaryMsg := map[string]any{
		"role":    "system",
		"content": summary.String(),
	}

	// Reconstruct: system prompt + summary + recent messages.
	result := make([]map[string]any, 0, 2+len(recentMessages))
	result = append(result, messages[0]) // original system prompt
	result = append(result, summaryMsg)
	result = append(result, recentMessages...)

	return result, nil
}

// recoveryMessageForMaxTokens returns the message to inject when the model's
// output is truncated due to hitting the max output token limit. The message
// instructs the model to resume seamlessly without preamble.
func recoveryMessageForMaxTokens() string {
	return "Output token limit hit. Resume directly — no apology, no recap of what you were doing. Pick up mid-thought if that is where the cut happened. Break remaining work into smaller pieces."
}

// shouldCircuitBreak checks if consecutive compaction failures exceed the
// threshold, indicating the loop should stop retrying compaction.
func shouldCircuitBreak(consecutiveFailures int) bool {
	return consecutiveFailures >= maxConsecutiveCompactFailures
}
