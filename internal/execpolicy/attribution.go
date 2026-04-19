package execpolicy

import (
	"fmt"
	"strings"
)

// AddCoAuthoredBy appends a Co-authored-by trailer to a commit message.
// It includes the agent model name and session ID for AI-assisted change attribution.
func AddCoAuthoredBy(commitMsg, modelName, sessionID string) string {
	commitMsg = strings.TrimRight(commitMsg, "\n")
	trailer := fmt.Sprintf("Co-authored-by: %s <session:%s>", modelName, sessionID)

	// Check if there's already a blank line before trailers
	lines := strings.Split(commitMsg, "\n")
	if len(lines) == 0 {
		return trailer
	}

	// If the last line is empty or already a trailer, just append
	lastLine := strings.TrimSpace(lines[len(lines)-1])
	if lastLine == "" || isTrailer(lastLine) {
		return commitMsg + "\n" + trailer
	}

	// Add blank line separator before trailer
	return commitMsg + "\n\n" + trailer
}

// isTrailer checks if a line looks like a git trailer (Key: Value format).
func isTrailer(line string) bool {
	idx := strings.Index(line, ": ")
	if idx <= 0 {
		return false
	}
	key := line[:idx]
	// Trailers have no spaces in the key
	return !strings.Contains(key, " ") || strings.HasPrefix(line, "Co-authored-by:")
}
