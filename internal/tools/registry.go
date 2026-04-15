package tools

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// ToolHandler is the function signature for tool execution handlers.
type ToolHandler func(ctx context.Context, tc ToolContext, call bifrost.ToolCall, args map[string]interface{}) (string, error)

// ToolEntry describes a single tool that can be registered in the ToolRegistry.
type ToolEntry struct {
	Name             string
	Description      string
	Parameters       map[string]interface{}
	Handler          ToolHandler
	RequiresApproval bool
	IsReadOnly       bool
}

// ToolRegistry is a thread-safe registry of tool entries.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]*ToolEntry
}

// NewToolRegistry creates an empty ToolRegistry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]*ToolEntry),
	}
}

// Register adds a tool entry to the registry. If a tool with the same name
// already exists it is silently overwritten.
func (r *ToolRegistry) Register(entry ToolEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := entry // copy
	r.tools[entry.Name] = &e
}

// Get returns the tool entry for the given name and a boolean indicating
// whether it was found.
func (r *ToolRegistry) Get(name string) (*ToolEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.tools[name]
	return e, ok
}

// Names returns a sorted list of all registered tool names.
func (r *ToolRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Definitions returns bifrost.ToolDefinition entries for registered tools,
// sorted by name to guarantee a stable ordering for cache keys.
// When enabledSets is non-empty it is treated as an allowlist of tool names.
func (r *ToolRegistry) Definitions(enabledSets []string) []bifrost.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	enabled := make(map[string]struct{}, len(enabledSets))
	for _, name := range enabledSets {
		if name == "" {
			continue
		}
		enabled[name] = struct{}{}
	}

	entries := make([]*ToolEntry, 0, len(r.tools))
	for _, e := range r.tools {
		if len(enabled) > 0 {
			if _, ok := enabled[e.Name]; !ok {
				continue
			}
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	defs := make([]bifrost.ToolDefinition, 0, len(entries))
	for _, e := range entries {
		defs = append(defs, bifrost.ToolDefinition{
			Type: "function",
			Function: bifrost.FunctionSpec{
				Name:        e.Name,
				Description: e.Description,
				Parameters:  e.Parameters,
			},
		})
	}
	return defs
}

// Dispatch looks up the tool by name and invokes its handler.
// Returns an error if the tool is not found or has no handler.
func (r *ToolRegistry) Dispatch(ctx context.Context, tc ToolContext, call bifrost.ToolCall, name string, args map[string]interface{}) (string, error) {
	r.mu.RLock()
	e, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("tool %q not found", name)
	}
	if e.Handler == nil {
		return "", fmt.Errorf("tool %q has no handler", name)
	}
	return e.Handler(ctx, tc, call, args)
}
