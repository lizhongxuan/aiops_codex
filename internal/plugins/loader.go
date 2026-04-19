// Package plugins provides a plugin system for extending the aiops-codex platform.
// Plugins can register tools, hooks, and configuration extensions.
package plugins

import (
	"fmt"
	"log"
	"sync"
)

// Plugin is the interface that all plugins must implement.
type Plugin interface {
	// Name returns the unique name of the plugin.
	Name() string
	// Init initializes the plugin with access to the registry for capability registration.
	Init(registry PluginRegistry) error
	// Close releases any resources held by the plugin.
	Close() error
}

// Loader discovers and loads plugins from a directory.
type Loader struct {
	dir     string
	plugins map[string]Plugin
	mu      sync.RWMutex
	errors  map[string]error
}

// NewLoader creates a new plugin Loader for the given directory.
func NewLoader(dir string) *Loader {
	return &Loader{
		dir:     dir,
		plugins: make(map[string]Plugin),
		errors:  make(map[string]error),
	}
}

// LoadAll discovers and loads all plugins from the configured directory.
// Plugin failures are isolated — a failing plugin does not affect others.
func (l *Loader) LoadAll() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// In a full implementation, this would scan the directory for plugin binaries
	// or Go plugin files (.so). For now, plugins are registered programmatically.
	// The key design principle is failure isolation.
	return nil
}

// Register manually registers a plugin (for programmatic plugin registration).
func (l *Loader) Register(plugin Plugin, registry PluginRegistry) error {
	if plugin == nil {
		return fmt.Errorf("plugin loader: nil plugin")
	}

	name := plugin.Name()
	if name == "" {
		return fmt.Errorf("plugin loader: plugin has empty name")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Initialize with failure isolation
	if err := l.safeInit(plugin, registry); err != nil {
		l.errors[name] = err
		log.Printf("[plugins] failed to initialize plugin %q: %v", name, err)
		return err
	}

	l.plugins[name] = plugin
	return nil
}

// safeInit initializes a plugin with panic recovery for failure isolation.
func (l *Loader) safeInit(plugin Plugin, registry PluginRegistry) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plugin %q panicked during init: %v", plugin.Name(), r)
		}
	}()
	return plugin.Init(registry)
}

// Get returns a loaded plugin by name.
func (l *Loader) Get(name string) (Plugin, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	p, ok := l.plugins[name]
	return p, ok
}

// List returns the names of all loaded plugins.
func (l *Loader) List() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	names := make([]string, 0, len(l.plugins))
	for name := range l.plugins {
		names = append(names, name)
	}
	return names
}

// Errors returns a map of plugin names to their initialization errors.
func (l *Loader) Errors() map[string]error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make(map[string]error, len(l.errors))
	for k, v := range l.errors {
		out[k] = v
	}
	return out
}

// CloseAll closes all loaded plugins.
func (l *Loader) CloseAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for name, plugin := range l.plugins {
		if err := plugin.Close(); err != nil {
			log.Printf("[plugins] error closing plugin %q: %v", name, err)
		}
	}
	l.plugins = make(map[string]Plugin)
}
