package mcphost

import (
	"testing"
)

func TestNamespaceTools(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "read_file", Description: "Read a file", ServerName: "fs-server"},
		{Name: "write_file", Description: "Write a file", ServerName: "fs-server"},
	}

	namespaced := NamespaceTools("fs-server", tools)

	if len(namespaced) != 2 {
		t.Fatalf("expected 2 namespaced tools, got %d", len(namespaced))
	}

	t.Run("callable_name_format", func(t *testing.T) {
		if namespaced[0].CallableName != "fs-server__read_file" {
			t.Errorf("expected fs-server__read_file, got %s", namespaced[0].CallableName)
		}
		if namespaced[1].CallableName != "fs-server__write_file" {
			t.Errorf("expected fs-server__write_file, got %s", namespaced[1].CallableName)
		}
	})

	t.Run("original_name_preserved", func(t *testing.T) {
		if namespaced[0].OriginalName != "read_file" {
			t.Errorf("expected read_file, got %s", namespaced[0].OriginalName)
		}
	})

	t.Run("server_name_preserved", func(t *testing.T) {
		if namespaced[0].ServerName != "fs-server" {
			t.Errorf("expected fs-server, got %s", namespaced[0].ServerName)
		}
	})

	t.Run("description_includes_namespace", func(t *testing.T) {
		if namespaced[0].Description != "[fs-server] Read a file" {
			t.Errorf("unexpected description: %s", namespaced[0].Description)
		}
	})
}

func TestNamespaceTools_Empty(t *testing.T) {
	namespaced := NamespaceTools("server", nil)
	if len(namespaced) != 0 {
		t.Errorf("expected 0 tools, got %d", len(namespaced))
	}
}

func TestResolveNamespacedTool(t *testing.T) {
	tests := []struct {
		name       string
		callable   string
		wantServer string
		wantTool   string
		wantErr    bool
	}{
		{"valid", "fs-server__read_file", "fs-server", "read_file", false},
		{"valid_with_underscores", "my_server__my_tool_name", "my_server", "my_tool_name", false},
		{"no_separator", "invalid-name", "", "", true},
		{"empty_server", "__tool_name", "", "", true},
		{"empty_tool", "server__", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, tool, err := ResolveNamespacedTool(tt.callable)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if server != tt.wantServer {
				t.Errorf("server = %q, want %q", server, tt.wantServer)
			}
			if tool != tt.wantTool {
				t.Errorf("tool = %q, want %q", tool, tt.wantTool)
			}
		})
	}
}
