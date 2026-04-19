package memory

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// MemoryTrace represents a single interaction trace from a past session/rollout.
type MemoryTrace struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Timestamp time.Time `json:"timestamp"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	ToolCalls []string  `json:"tool_calls,omitempty"`
}

// RawMemory is an extracted memory from a single rollout, produced by Phase 1.
type RawMemory struct {
	ID            string    `json:"id"`
	SourceRollout string    `json:"source_rollout"`
	Summary       string    `json:"summary"`
	KeyInsights   []string  `json:"key_insights,omitempty"`
	ExtractedAt   time.Time `json:"extracted_at"`
}

// ConsolidatedMemory is the merged summary produced by Phase 2 consolidation.
type ConsolidatedMemory struct {
	Summary      string     `json:"summary"`
	KeyFacts     []string   `json:"key_facts,omitempty"`
	Citations    []Citation `json:"citations,omitempty"`
	ConsolidatedAt time.Time `json:"consolidated_at"`
}

// LoadTraces loads memory traces from JSON or JSONL files in the given directory.
func LoadTraces(dir string) ([]MemoryTrace, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("memory: read trace dir: %w", err)
	}

	var traces []MemoryTrace
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		path := filepath.Join(dir, name)

		switch {
		case strings.HasSuffix(name, ".json"):
			loaded, err := loadJSONTraces(path)
			if err != nil {
				return nil, err
			}
			traces = append(traces, loaded...)
		case strings.HasSuffix(name, ".jsonl"):
			loaded, err := loadJSONLTraces(path)
			if err != nil {
				return nil, err
			}
			traces = append(traces, loaded...)
		}
	}
	return traces, nil
}

func loadJSONTraces(path string) ([]MemoryTrace, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("memory: read %s: %w", path, err)
	}
	var traces []MemoryTrace
	if err := json.Unmarshal(data, &traces); err != nil {
		// Try single trace
		var single MemoryTrace
		if err2 := json.Unmarshal(data, &single); err2 != nil {
			return nil, fmt.Errorf("memory: unmarshal %s: %w", path, err)
		}
		traces = append(traces, single)
	}
	return traces, nil
}

func loadJSONLTraces(path string) ([]MemoryTrace, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("memory: open %s: %w", path, err)
	}
	defer f.Close()

	var traces []MemoryTrace
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var t MemoryTrace
		if err := json.Unmarshal([]byte(line), &t); err != nil {
			return nil, fmt.Errorf("memory: unmarshal line in %s: %w", path, err)
		}
		traces = append(traces, t)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("memory: scan %s: %w", path, err)
	}
	return traces, nil
}


// NormalizeTraces ensures all traces have a consistent internal format.
// It trims whitespace, normalizes roles, and filters out empty content.
func NormalizeTraces(traces []MemoryTrace) []MemoryTrace {
	normalized := make([]MemoryTrace, 0, len(traces))
	for _, t := range traces {
		t.Content = strings.TrimSpace(t.Content)
		t.Role = strings.ToLower(strings.TrimSpace(t.Role))
		if t.Content == "" {
			continue
		}
		normalized = append(normalized, t)
	}
	return normalized
}

// SummarizeTrace uses an LLM via the bifrost gateway to summarize a single trace.
func SummarizeTrace(ctx context.Context, gateway *bifrost.Gateway, trace MemoryTrace) (string, error) {
	prompt := fmt.Sprintf(
		"Summarize the following interaction trace concisely, focusing on key decisions and outcomes:\n\nRole: %s\nContent: %s",
		trace.Role, trace.Content,
	)

	req := bifrost.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []bifrost.Message{
			{Role: "system", Content: "You are a memory summarization agent. Produce concise summaries."},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   256,
		Temperature: 0.3,
	}

	resp, err := gateway.ChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("memory: summarize trace: %w", err)
	}

	content, _ := resp.Message.Content.(string)
	return content, nil
}
