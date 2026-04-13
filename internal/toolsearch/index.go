package toolsearch

import (
	"sync"
)

// ToolEntry mirrors the agentloop.ToolEntry fields needed for indexing.
// This avoids a circular import.
type ToolEntry struct {
	Name        string
	Description string
	Tags        []string
}

// ToolIndex maintains a searchable index of tool entries and supports
// rebuilding when the tool set changes.
type ToolIndex struct {
	mu    sync.RWMutex
	index *Index
	tools []ToolEntry
}

// NewToolIndex creates an empty ToolIndex.
func NewToolIndex() *ToolIndex {
	return &ToolIndex{}
}

// Build constructs the BM25 index from the given tool entries.
func (ti *ToolIndex) Build(tools []ToolEntry) {
	docs := make([]ToolDoc, len(tools))
	for i, t := range tools {
		docs[i] = ToolDoc{
			Name:        t.Name,
			Description: t.Description,
			Tags:        t.Tags,
		}
	}

	idx := NewIndex(docs)

	ti.mu.Lock()
	defer ti.mu.Unlock()
	ti.tools = tools
	ti.index = idx
}

// Rebuild replaces the index with a new set of tools.
func (ti *ToolIndex) Rebuild(tools []ToolEntry) {
	ti.Build(tools)
}

// Search queries the index and returns ranked tool names.
func (ti *ToolIndex) Search(query string, topK int) []SearchResult {
	ti.mu.RLock()
	idx := ti.index
	ti.mu.RUnlock()

	if idx == nil {
		return nil
	}
	return idx.Search(query, topK)
}

// Size returns the number of indexed tools.
func (ti *ToolIndex) Size() int {
	ti.mu.RLock()
	defer ti.mu.RUnlock()
	return len(ti.tools)
}
