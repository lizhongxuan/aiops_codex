package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ToolHandlerRegistry stores handlers and their static descriptors by tool name.
type ToolHandlerRegistry struct {
	mu       sync.RWMutex
	handlers map[string]*toolRegistryEntry
	aliases  map[string]string
}

type toolRegistryEntry struct {
	descriptor ToolDescriptor
	handler    ToolHandler
	unified    UnifiedTool
	aliases    []string
}

// NewToolHandlerRegistry creates an empty registry.
func NewToolHandlerRegistry() *ToolHandlerRegistry {
	return &ToolHandlerRegistry{
		handlers: make(map[string]*toolRegistryEntry),
		aliases:  make(map[string]string),
	}
}

// Register adds a legacy handler to the registry.
func (r *ToolHandlerRegistry) Register(handler ToolHandler) error {
	return r.registerTool(handler, nil)
}

// RegisterUnifiedTool adds a unified tool to the registry.
func (r *ToolHandlerRegistry) RegisterUnifiedTool(tool UnifiedTool) error {
	return r.registerTool(nil, tool)
}

// MustRegister registers a handler and panics on failure.
func (r *ToolHandlerRegistry) MustRegister(handler ToolHandler) {
	if err := r.Register(handler); err != nil {
		panic(err)
	}
}

// MustRegisterUnifiedTool registers a unified tool and panics on failure.
func (r *ToolHandlerRegistry) MustRegisterUnifiedTool(tool UnifiedTool) {
	if err := r.RegisterUnifiedTool(tool); err != nil {
		panic(err)
	}
}

// Get returns the registered legacy handler for the given tool name.
func (r *ToolHandlerRegistry) Get(name string) (ToolHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.lookupEntryLocked(name)
	if !ok || entry == nil {
		return nil, false
	}
	if entry.handler != nil {
		return entry.handler, true
	}
	if entry.unified != nil {
		return newToolHandlerAdapter(entry.unified, entry.descriptor), true
	}
	return nil, false
}

// GetUnified returns the registered unified tool for the given tool name.
func (r *ToolHandlerRegistry) GetUnified(name string) (UnifiedTool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.lookupEntryLocked(name)
	if !ok || entry == nil {
		return nil, false
	}
	if entry.unified != nil {
		return entry.unified, true
	}
	if entry.handler != nil {
		return newUnifiedToolAdapter(entry.handler, entry.descriptor), true
	}
	return nil, false
}

// Lookup returns the descriptor and legacy handler for the given tool name.
func (r *ToolHandlerRegistry) Lookup(name string) (ToolDescriptor, ToolHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.lookupEntryLocked(name)
	if !ok || entry == nil {
		return ToolDescriptor{}, nil, false
	}
	if entry.handler != nil {
		return entry.descriptor.Clone(), entry.handler, true
	}
	if entry.unified != nil {
		return entry.descriptor.Clone(), newToolHandlerAdapter(entry.unified, entry.descriptor), true
	}
	return ToolDescriptor{}, nil, false
}

// LookupUnified returns the descriptor and unified tool for the given tool name.
func (r *ToolHandlerRegistry) LookupUnified(name string) (ToolDescriptor, UnifiedTool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.lookupEntryLocked(name)
	if !ok || entry == nil {
		return ToolDescriptor{}, nil, false
	}
	if entry.unified != nil {
		return entry.descriptor.Clone(), entry.unified, true
	}
	if entry.handler != nil {
		return entry.descriptor.Clone(), newUnifiedToolAdapter(entry.handler, entry.descriptor), true
	}
	return ToolDescriptor{}, nil, false
}

// Descriptor returns the descriptor for the given tool name.
func (r *ToolHandlerRegistry) Descriptor(name string) (ToolDescriptor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.lookupEntryLocked(name)
	if !ok || entry == nil {
		return ToolDescriptor{}, false
	}
	return entry.descriptor.Clone(), true
}

// ListDescriptors returns a stable snapshot of all registered descriptors.
func (r *ToolHandlerRegistry) ListDescriptors() []ToolDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.handlers) == 0 {
		return nil
	}

	names := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		names = append(names, name)
	}
	sort.Strings(names)

	descriptors := make([]ToolDescriptor, 0, len(names))
	for _, name := range names {
		descriptors = append(descriptors, r.handlers[name].descriptor.Clone())
	}
	return descriptors
}

// Len reports how many handlers are registered.
func (r *ToolHandlerRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.handlers)
}

// DispatchCategory resolves the effective dispatch category for a tool by
// combining explicit call-site classification with registered descriptor hints.
func (r *ToolHandlerRegistry) DispatchCategory(name string, fallback toolDispatchCategory) toolDispatchCategory {
	switch fallback {
	case toolCategoryBlocking, toolCategoryApproval:
		return fallback
	}

	if r != nil {
		if desc, ok := r.Descriptor(name); ok {
			if desc.RequiresApproval {
				return toolCategoryApproval
			}
			if desc.IsReadOnly {
				return toolCategoryReadonly
			}
		}
	}

	if fallback != "" {
		return fallback
	}
	return categorizeToolForDispatch(name)
}

func normalizeToolName(name string) string {
	return strings.TrimSpace(name)
}

func (r *ToolHandlerRegistry) registerTool(handler ToolHandler, unified UnifiedTool) error {
	var desc ToolDescriptor
	var aliases []string

	switch {
	case handler != nil:
		desc = handler.Descriptor()
	case unified != nil:
		desc = toolDescriptorFromUnifiedTool(unified)
		aliases = normalizeToolAliases(unified.Aliases(), desc.Name)
	default:
		return fmt.Errorf("tool handler is nil")
	}

	name := normalizeToolName(desc.Name)
	if name == "" {
		return fmt.Errorf("tool handler descriptor name is required")
	}

	entry := &toolRegistryEntry{
		descriptor: desc.Clone(),
		aliases:    append([]string(nil), aliases...),
	}
	if handler != nil {
		entry.handler = handler
	}
	if unified != nil {
		entry.unified = unified
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.handlers[name]; exists {
		return fmt.Errorf("tool handler %q is already registered", name)
	}
	if _, exists := r.aliases[name]; exists {
		return fmt.Errorf("tool handler %q is already registered", name)
	}
	for _, alias := range aliases {
		if _, exists := r.handlers[alias]; exists {
			return fmt.Errorf("tool handler alias %q conflicts with an existing tool", alias)
		}
		if _, exists := r.aliases[alias]; exists {
			return fmt.Errorf("tool handler alias %q is already registered", alias)
		}
	}
	r.handlers[name] = entry
	for _, alias := range aliases {
		r.aliases[alias] = name
	}
	return nil
}

func (r *ToolHandlerRegistry) lookupEntryLocked(name string) (*toolRegistryEntry, bool) {
	if r == nil {
		return nil, false
	}
	normalized := normalizeToolName(name)
	if normalized == "" {
		return nil, false
	}
	if entry, ok := r.handlers[normalized]; ok && entry != nil {
		return entry, true
	}
	canonical, ok := r.aliases[normalized]
	if !ok {
		return nil, false
	}
	entry, ok := r.handlers[canonical]
	if !ok || entry == nil {
		return nil, false
	}
	return entry, true
}

func toolDescriptorFromUnifiedTool(tool UnifiedTool) ToolDescriptor {
	if tool == nil {
		return ToolDescriptor{}
	}

	desc := ToolDescriptor{
		Name:         normalizeToolName(tool.Name()),
		DisplayLabel: normalizeToolName(tool.Name()),
	}
	if description := strings.TrimSpace(tool.Description(ToolDescriptionContext{})); description != "" {
		desc.DisplayLabel = description
	}

	if tool.IsReadOnly(ToolCallRequest{}) {
		desc.IsReadOnly = true
	}
	if tool.IsDestructive(ToolCallRequest{}) {
		desc.IsReadOnly = false
	}

	if result, err := tool.CheckPermissions(context.Background(), ToolCallRequest{}); err == nil && result.RequiresApproval {
		desc.RequiresApproval = true
	}
	if progressTool, ok := tool.(StreamingUnifiedTool); ok && progressTool.SupportsStreamingProgress() {
		desc.SupportsStreamingProgress = true
	}

	if display := tool.Display(); display != nil {
		desc.Kind = "unified"
	}

	return desc.Clone()
}

func normalizeToolAliases(raw []string, canonical string) []string {
	if len(raw) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	canonical = normalizeToolName(canonical)
	for _, alias := range raw {
		normalized := normalizeToolName(alias)
		if normalized == "" || normalized == canonical {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}
