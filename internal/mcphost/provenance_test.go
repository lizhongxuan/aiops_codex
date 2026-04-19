package mcphost

import (
	"testing"
)

func TestProvenanceRegistry_Register(t *testing.T) {
	r := NewProvenanceRegistry()

	r.Register("list_dir", ToolProvenance{Source: SourceBuiltin, SourceName: "core"})
	r.Register("mcp_read", ToolProvenance{Source: SourceMCP, SourceName: "fs-server"})
	r.Register("custom_tool", ToolProvenance{Source: SourceDynamic, SourceName: "user"})
	r.Register("deploy", ToolProvenance{Source: SourceSkill, SourceName: "deploy-skill"})

	t.Run("get_existing", func(t *testing.T) {
		p, ok := r.Get("list_dir")
		if !ok {
			t.Fatal("expected to find list_dir")
		}
		if p.Source != SourceBuiltin {
			t.Errorf("expected builtin, got %s", p.Source)
		}
		if p.SourceName != "core" {
			t.Errorf("expected core, got %s", p.SourceName)
		}
	})

	t.Run("get_missing", func(t *testing.T) {
		_, ok := r.Get("nonexistent")
		if ok {
			t.Error("expected not found")
		}
	})

	t.Run("all", func(t *testing.T) {
		all := r.All()
		if len(all) != 4 {
			t.Errorf("expected 4 entries, got %d", len(all))
		}
	})

	t.Run("tools_from_source", func(t *testing.T) {
		builtins := r.ToolsFromSource(SourceBuiltin)
		if len(builtins) != 1 || builtins[0] != "list_dir" {
			t.Errorf("unexpected builtins: %v", builtins)
		}

		mcpTools := r.ToolsFromSource(SourceMCP)
		if len(mcpTools) != 1 || mcpTools[0] != "mcp_read" {
			t.Errorf("unexpected mcp tools: %v", mcpTools)
		}
	})

	t.Run("remove", func(t *testing.T) {
		r.Remove("custom_tool")
		_, ok := r.Get("custom_tool")
		if ok {
			t.Error("expected custom_tool to be removed")
		}
		if len(r.All()) != 3 {
			t.Errorf("expected 3 entries after remove, got %d", len(r.All()))
		}
	})
}

func TestProvenanceRegistry_Empty(t *testing.T) {
	r := NewProvenanceRegistry()
	all := r.All()
	if len(all) != 0 {
		t.Errorf("expected 0 entries, got %d", len(all))
	}
	tools := r.ToolsFromSource(SourceBuiltin)
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}
