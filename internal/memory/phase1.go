package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// Phase1Config holds configuration for Phase 1 memory extraction.
type Phase1Config struct {
	Model       string // LLM model to use for summarization
	Concurrency int    // Max concurrent rollout processing (default 8)
	TokenLimit  int    // Max tokens per summarization call
}

// DefaultPhase1Config returns sensible defaults for Phase 1 extraction.
func DefaultPhase1Config() Phase1Config {
	return Phase1Config{
		Model:       "gpt-4o-mini",
		Concurrency: 8,
		TokenLimit:  512,
	}
}

// ExtractPhase1 processes eligible rollouts concurrently, extracting raw memories.
// It uses a semaphore to limit concurrency to config.Concurrency goroutines.
func ExtractPhase1(
	ctx context.Context,
	gateway *bifrost.Gateway,
	config Phase1Config,
	rollouts [][]MemoryTrace,
) ([]RawMemory, error) {
	if config.Concurrency <= 0 {
		config.Concurrency = 8
	}

	sem := make(chan struct{}, config.Concurrency)
	var mu sync.Mutex
	var memories []RawMemory
	var firstErr error

	var wg sync.WaitGroup
	for i, rollout := range rollouts {
		if len(rollout) == 0 {
			continue
		}

		wg.Add(1)
		go func(idx int, traces []MemoryTrace) {
			defer wg.Done()

			// Acquire semaphore slot
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				if firstErr == nil {
					firstErr = ctx.Err()
				}
				mu.Unlock()
				return
			}

			raw, err := extractSingleRollout(ctx, gateway, config, idx, traces)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			memories = append(memories, raw)
		}(i, rollout)
	}

	wg.Wait()

	if firstErr != nil {
		return memories, firstErr
	}
	return memories, nil
}

// extractSingleRollout processes a single rollout's traces into a RawMemory.
func extractSingleRollout(
	ctx context.Context,
	gateway *bifrost.Gateway,
	config Phase1Config,
	idx int,
	traces []MemoryTrace,
) (RawMemory, error) {
	// Build a combined content from all traces in the rollout
	var combined string
	for _, t := range traces {
		combined += fmt.Sprintf("[%s] %s\n", t.Role, t.Content)
	}

	// Truncate if too long (rough token estimate: 4 chars per token)
	maxChars := config.TokenLimit * 4
	if len(combined) > maxChars {
		combined = combined[:maxChars]
	}

	prompt := fmt.Sprintf(
		"Extract key memories from this session rollout. Focus on decisions made, tools used, and outcomes:\n\n%s",
		combined,
	)

	model := config.Model
	if model == "" {
		model = "gpt-4o-mini"
	}

	req := bifrost.ChatRequest{
		Model: model,
		Messages: []bifrost.Message{
			{Role: "system", Content: "You are a memory extraction agent. Extract concise, actionable memories from session rollouts."},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   config.TokenLimit,
		Temperature: 0.3,
	}

	resp, err := gateway.ChatCompletion(ctx, req)
	if err != nil {
		return RawMemory{}, fmt.Errorf("memory/phase1: extract rollout %d: %w", idx, err)
	}

	content, _ := resp.Message.Content.(string)

	// Determine source rollout ID from first trace
	sourceRollout := fmt.Sprintf("rollout-%d", idx)
	if len(traces) > 0 && traces[0].SessionID != "" {
		sourceRollout = traces[0].SessionID
	}

	return RawMemory{
		ID:            fmt.Sprintf("raw-%d-%d", idx, time.Now().UnixNano()),
		SourceRollout: sourceRollout,
		Summary:       content,
		ExtractedAt:   time.Now(),
	}, nil
}
