package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
	"github.com/lizhongxuan/aiops-codex/internal/toolsearch"
)

// toolSearchIndex is the package-level tool search index used by tool_suggest.
var toolSearchIndex = toolsearch.NewToolIndex()

// RebuildToolSearchIndex rebuilds the tool search index from the registry.
func RebuildToolSearchIndex(reg *ToolRegistry) {
	names := reg.Names()
	entries := make([]toolsearch.ToolEntry, 0, len(names))
	reg.mu.RLock()
	for _, name := range names {
		if e, ok := reg.tools[name]; ok {
			entries = append(entries, toolsearch.ToolEntry{
				Name:        e.Name,
				Description: e.Description,
			})
		}
	}
	reg.mu.RUnlock()
	toolSearchIndex.Build(entries)
}

// RegisterToolSuggestTool registers the tool_suggest tool.
func RegisterToolSuggestTool(reg *ToolRegistry) {
	reg.Register(ToolEntry{
		Name:        "tool_suggest",
		Description: "Analyze context and return ranked tool recommendations based on the query.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Natural language description of what you want to accomplish.",
				},
				"top_k": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of suggestions to return (default 5).",
				},
			},
			"required":             []string{"query"},
			"additionalProperties": false,
		},
		Handler:    handleToolSuggest,
		IsReadOnly: true,
	})
}

func handleToolSuggest(ctx context.Context, tc ToolContext, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("tool_suggest requires a non-empty 'query' argument")
	}

	topK := 5
	if k, ok := args["top_k"].(float64); ok && k > 0 {
		topK = int(k)
	}

	results := toolSearchIndex.Search(query, topK)
	if len(results) == 0 {
		return "No matching tools found for the given query.", nil
	}

	type suggestion struct {
		Name  string  `json:"name"`
		Score float64 `json:"score"`
	}
	suggestions := make([]suggestion, len(results))
	for i, r := range results {
		suggestions[i] = suggestion{Name: r.Name, Score: r.Score}
	}

	out, err := json.MarshalIndent(suggestions, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal suggestions: %w", err)
	}
	return string(out), nil
}
