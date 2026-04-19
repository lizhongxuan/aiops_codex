package plugins

import (
	"fmt"
	"testing"
)

// mockPlugin is a test plugin implementation.
type mockPlugin struct {
	name      string
	initErr   error
	closeErr  error
	initCalls int
}

func (m *mockPlugin) Name() string { return m.name }
func (m *mockPlugin) Init(registry PluginRegistry) error {
	m.initCalls++
	if m.initErr != nil {
		return m.initErr
	}
	// Register a tool
	return registry.RegisterTool(ToolEntry{
		Name:        m.name + "_tool",
		Description: "Tool from " + m.name,
	})
}
func (m *mockPlugin) Close() error { return m.closeErr }

// panicPlugin panics during Init.
type panicPlugin struct{}

func (p *panicPlugin) Name() string                      { return "panic-plugin" }
func (p *panicPlugin) Init(registry PluginRegistry) error { panic("init panic!") }
func (p *panicPlugin) Close() error                      { return nil }

func TestLoader_Register(t *testing.T) {
	loader := NewLoader("/tmp/plugins")
	registry := NewDefaultRegistry()

	plugin := &mockPlugin{name: "test-plugin"}
	err := loader.Register(plugin, registry)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Check plugin is loaded
	p, ok := loader.Get("test-plugin")
	if !ok {
		t.Fatal("expected plugin to be loaded")
	}
	if p.Name() != "test-plugin" {
		t.Errorf("expected name 'test-plugin', got %s", p.Name())
	}

	// Check tool was registered
	tools := registry.Tools()
	if _, ok := tools["test-plugin_tool"]; !ok {
		t.Error("expected tool to be registered")
	}
}

func TestLoader_Register_InitError(t *testing.T) {
	loader := NewLoader("/tmp/plugins")
	registry := NewDefaultRegistry()

	plugin := &mockPlugin{name: "bad-plugin", initErr: fmt.Errorf("init failed")}
	err := loader.Register(plugin, registry)
	if err == nil {
		t.Fatal("expected error from failing plugin")
	}

	// Plugin should not be in loaded list
	_, ok := loader.Get("bad-plugin")
	if ok {
		t.Error("failing plugin should not be loaded")
	}

	// Error should be recorded
	errors := loader.Errors()
	if _, ok := errors["bad-plugin"]; !ok {
		t.Error("expected error to be recorded")
	}
}

func TestLoader_Register_PanicIsolation(t *testing.T) {
	loader := NewLoader("/tmp/plugins")
	registry := NewDefaultRegistry()

	plugin := &panicPlugin{}
	err := loader.Register(plugin, registry)
	if err == nil {
		t.Fatal("expected error from panicking plugin")
	}

	// Should not crash the loader
	_, ok := loader.Get("panic-plugin")
	if ok {
		t.Error("panicking plugin should not be loaded")
	}
}

func TestLoader_List(t *testing.T) {
	loader := NewLoader("/tmp/plugins")
	registry := NewDefaultRegistry()

	loader.Register(&mockPlugin{name: "plugin-a"}, registry)
	loader.Register(&mockPlugin{name: "plugin-b"}, registry)

	names := loader.List()
	if len(names) != 2 {
		t.Errorf("expected 2 plugins, got %d", len(names))
	}
}

func TestLoader_CloseAll(t *testing.T) {
	loader := NewLoader("/tmp/plugins")
	registry := NewDefaultRegistry()

	loader.Register(&mockPlugin{name: "plugin-a"}, registry)
	loader.Register(&mockPlugin{name: "plugin-b"}, registry)

	loader.CloseAll()

	names := loader.List()
	if len(names) != 0 {
		t.Errorf("expected 0 plugins after CloseAll, got %d", len(names))
	}
}

func TestLoader_Register_NilPlugin(t *testing.T) {
	loader := NewLoader("/tmp/plugins")
	registry := NewDefaultRegistry()

	err := loader.Register(nil, registry)
	if err == nil {
		t.Error("expected error for nil plugin")
	}
}

func TestDefaultRegistry_RegisterTool(t *testing.T) {
	r := NewDefaultRegistry()
	err := r.RegisterTool(ToolEntry{Name: "my_tool", Description: "A tool"})
	if err != nil {
		t.Fatalf("RegisterTool failed: %v", err)
	}

	tools := r.Tools()
	if _, ok := tools["my_tool"]; !ok {
		t.Error("expected tool to be registered")
	}
}

func TestDefaultRegistry_RegisterTool_EmptyName(t *testing.T) {
	r := NewDefaultRegistry()
	err := r.RegisterTool(ToolEntry{Name: ""})
	if err == nil {
		t.Error("expected error for empty tool name")
	}
}

func TestDefaultRegistry_RegisterHook(t *testing.T) {
	r := NewDefaultRegistry()
	err := r.RegisterHook(Hook{
		Name:    "my_hook",
		Event:   "pre_tool_use",
		Handler: func(ctx interface{}) error { return nil },
	})
	if err != nil {
		t.Fatalf("RegisterHook failed: %v", err)
	}

	hooks := r.Hooks()
	if len(hooks) != 1 {
		t.Errorf("expected 1 hook, got %d", len(hooks))
	}
}

func TestDefaultRegistry_RegisterHook_NoHandler(t *testing.T) {
	r := NewDefaultRegistry()
	err := r.RegisterHook(Hook{Name: "bad_hook", Event: "test"})
	if err == nil {
		t.Error("expected error for hook without handler")
	}
}

func TestDefaultRegistry_RegisterConfig(t *testing.T) {
	r := NewDefaultRegistry()
	err := r.RegisterConfig("my.setting", "value123")
	if err != nil {
		t.Fatalf("RegisterConfig failed: %v", err)
	}

	configs := r.Configs()
	if configs["my.setting"] != "value123" {
		t.Errorf("expected config value 'value123', got %v", configs["my.setting"])
	}
}

func TestDefaultRegistry_RegisterConfig_EmptyKey(t *testing.T) {
	r := NewDefaultRegistry()
	err := r.RegisterConfig("", "value")
	if err == nil {
		t.Error("expected error for empty config key")
	}
}
