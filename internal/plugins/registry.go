package plugins

import (
	"fmt"
	"sync"
)

// ToolEntry represents a tool that a plugin can register.
type ToolEntry struct {
	Name        string
	Description string
	Parameters  map[string]interface{}
}

// Hook represents a hook that a plugin can register.
type Hook struct {
	Name    string
	Event   string
	Handler func(ctx interface{}) error
}

// PluginRegistry allows plugins to register capabilities with the host system.
type PluginRegistry interface {
	// RegisterTool registers a new tool provided by the plugin.
	RegisterTool(entry ToolEntry) error
	// RegisterHook registers a new hook provided by the plugin.
	RegisterHook(hook Hook) error
	// RegisterConfig registers a configuration extension provided by the plugin.
	RegisterConfig(key string, value interface{}) error
}

// DefaultRegistry is the default implementation of PluginRegistry.
type DefaultRegistry struct {
	tools   map[string]ToolEntry
	hooks   []Hook
	configs map[string]interface{}
	mu      sync.RWMutex
}

// NewDefaultRegistry creates a new DefaultRegistry.
func NewDefaultRegistry() *DefaultRegistry {
	return &DefaultRegistry{
		tools:   make(map[string]ToolEntry),
		configs: make(map[string]interface{}),
	}
}

// RegisterTool registers a tool entry.
func (r *DefaultRegistry) RegisterTool(entry ToolEntry) error {
	if entry.Name == "" {
		return fmt.Errorf("plugin registry: tool name is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[entry.Name] = entry
	return nil
}

// RegisterHook registers a hook.
func (r *DefaultRegistry) RegisterHook(hook Hook) error {
	if hook.Name == "" {
		return fmt.Errorf("plugin registry: hook name is required")
	}
	if hook.Handler == nil {
		return fmt.Errorf("plugin registry: hook handler is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = append(r.hooks, hook)
	return nil
}

// RegisterConfig registers a configuration extension.
func (r *DefaultRegistry) RegisterConfig(key string, value interface{}) error {
	if key == "" {
		return fmt.Errorf("plugin registry: config key is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configs[key] = value
	return nil
}

// Tools returns all registered tools.
func (r *DefaultRegistry) Tools() map[string]ToolEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]ToolEntry, len(r.tools))
	for k, v := range r.tools {
		out[k] = v
	}
	return out
}

// Hooks returns all registered hooks.
func (r *DefaultRegistry) Hooks() []Hook {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Hook, len(r.hooks))
	copy(out, r.hooks)
	return out
}

// Configs returns all registered configuration extensions.
func (r *DefaultRegistry) Configs() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]interface{}, len(r.configs))
	for k, v := range r.configs {
		out[k] = v
	}
	return out
}
