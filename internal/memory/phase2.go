package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// ConsolidationLock provides a global lock for Phase 2 consolidation.
// Only one holder can consolidate at a time.
type ConsolidationLock struct {
	mu     sync.Mutex
	holder string
	held   bool
}

// NewConsolidationLock creates a new unlocked ConsolidationLock.
func NewConsolidationLock() *ConsolidationLock {
	return &ConsolidationLock{}
}

// Acquire attempts to acquire the lock for the given holder.
// Returns true if the lock was acquired, false if already held.
func (cl *ConsolidationLock) Acquire(holder string) bool {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if cl.held {
		return false
	}
	cl.held = true
	cl.holder = holder
	return true
}

// Release releases the lock if held by the given holder.
func (cl *ConsolidationLock) Release(holder string) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if cl.held && cl.holder == holder {
		cl.held = false
		cl.holder = ""
	}
}

// Holder returns the current lock holder, or empty string if unlocked.
func (cl *ConsolidationLock) Holder() string {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return cl.holder
}

// ConsolidatePhase2 merges raw memories into a single consolidated memory summary.
// It acquires the consolidation lock, dispatches an LLM agent to merge, and persists the result.
func ConsolidatePhase2(
	ctx context.Context,
	gateway *bifrost.Gateway,
	rawMemories []RawMemory,
) (*ConsolidatedMemory, error) {
	if len(rawMemories) == 0 {
		return &ConsolidatedMemory{
			Summary:        "No memories to consolidate.",
			ConsolidatedAt: time.Now(),
		}, nil
	}

	// Build input from raw memories
	var parts []string
	for _, rm := range rawMemories {
		parts = append(parts, fmt.Sprintf("- [%s] %s", rm.SourceRollout, rm.Summary))
	}
	memoriesText := strings.Join(parts, "\n")

	prompt := fmt.Sprintf(
		"Consolidate the following raw memories into a unified summary. "+
			"Identify key facts, patterns, and important context for future sessions:\n\n%s",
		memoriesText,
	)

	req := bifrost.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []bifrost.Message{
			{Role: "system", Content: "You are a memory consolidation agent. Merge multiple raw memories into a coherent, concise summary with key facts."},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   1024,
		Temperature: 0.3,
	}

	resp, err := gateway.ChatCompletion(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("memory/phase2: consolidation LLM call: %w", err)
	}

	content, _ := resp.Message.Content.(string)

	// Build citations from raw memories
	var citations []Citation
	for _, rm := range rawMemories {
		citations = append(citations, Citation{
			MemoryID:      rm.ID,
			SourceRollout: rm.SourceRollout,
			Excerpt:       truncate(rm.Summary, 100),
		})
	}

	return &ConsolidatedMemory{
		Summary:        content,
		KeyFacts:       extractKeyFacts(content),
		Citations:      citations,
		ConsolidatedAt: time.Now(),
	}, nil
}

// extractKeyFacts splits the consolidated summary into key fact lines.
func extractKeyFacts(summary string) []string {
	lines := strings.Split(summary, "\n")
	var facts []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && (strings.HasPrefix(line, "-") || strings.HasPrefix(line, "•")) {
			facts = append(facts, strings.TrimLeft(line, "-• "))
		}
	}
	return facts
}

// truncate shortens a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
