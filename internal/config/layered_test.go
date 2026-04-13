package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMergeOverrides_ScalarOverride(t *testing.T) {
	lc := &LayeredConfig{
		System: ConfigOverrides{
			LLMProvider: "openai",
			LLMModel:    "gpt-4o-mini",
		},
		User: ConfigOverrides{
			LLMModel: "gpt-4o",
		},
		Repo: ConfigOverrides{
			LLMProvider: "anthropic",
		},
	}

	merged := lc.Merge()

	if merged.LLMProvider != "anthropic" {
		t.Errorf("expected repo override 'anthropic', got %s", merged.LLMProvider)
	}
	if merged.LLMModel != "gpt-4o" {
		t.Errorf("expected user override 'gpt-4o', got %s", merged.LLMModel)
	}
}

func TestMergeOverrides_ArrayCombination(t *testing.T) {
	lc := &LayeredConfig{
		System: ConfigOverrides{
			MCPPaths:   []string{"/system/mcp.json"},
			SkillRoots: []string{"/system/skills"},
		},
		User: ConfigOverrides{
			MCPPaths:   []string{"/user/mcp.json"},
			SkillRoots: []string{"/user/skills"},
		},
		Repo: ConfigOverrides{
			MCPPaths: []string{"/repo/mcp.json"},
		},
	}

	merged := lc.Merge()

	// Arrays should be combined, not replaced
	if len(merged.MCPPaths) != 3 {
		t.Errorf("expected 3 MCP paths, got %d: %v", len(merged.MCPPaths), merged.MCPPaths)
	}
	if len(merged.SkillRoots) != 2 {
		t.Errorf("expected 2 skill roots, got %d: %v", len(merged.SkillRoots), merged.SkillRoots)
	}
}

func TestMergeOverrides_ArrayDeduplication(t *testing.T) {
	lc := &LayeredConfig{
		System: ConfigOverrides{
			MCPPaths: []string{"/shared/mcp.json"},
		},
		User: ConfigOverrides{
			MCPPaths: []string{"/shared/mcp.json", "/user/mcp.json"},
		},
	}

	merged := lc.Merge()

	if len(merged.MCPPaths) != 2 {
		t.Errorf("expected 2 deduplicated paths, got %d: %v", len(merged.MCPPaths), merged.MCPPaths)
	}
}

func TestMergeOverrides_EmptyLayers(t *testing.T) {
	lc := &LayeredConfig{
		System: ConfigOverrides{
			LLMProvider: "openai",
			LLMModel:    "gpt-4o-mini",
		},
	}

	merged := lc.Merge()

	if merged.LLMProvider != "openai" {
		t.Errorf("expected system default 'openai', got %s", merged.LLMProvider)
	}
}

func TestLoadLayered_LoadsFromFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a config file
	configDir := filepath.Join(tmpDir, ".aiops_codex")
	os.MkdirAll(configDir, 0755)

	overrides := ConfigOverrides{
		LLMProvider: "test-provider",
		MCPPaths:    []string{"/test/path"},
	}
	data, _ := json.Marshal(overrides)
	os.WriteFile(filepath.Join(configDir, "config.json"), data, 0644)

	// loadOverridesFile should work
	loaded, err := loadOverridesFile(filepath.Join(configDir, "config.json"))
	if err != nil {
		t.Fatalf("loadOverridesFile failed: %v", err)
	}
	if loaded.LLMProvider != "test-provider" {
		t.Errorf("expected 'test-provider', got %s", loaded.LLMProvider)
	}
}

func TestLoadLayered_MissingFile(t *testing.T) {
	_, err := loadOverridesFile("/nonexistent/config.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestConfigOverrides_IsZero(t *testing.T) {
	empty := ConfigOverrides{}
	if !empty.IsZero() {
		t.Error("expected empty overrides to be zero")
	}

	nonEmpty := ConfigOverrides{LLMProvider: "test"}
	if nonEmpty.IsZero() {
		t.Error("expected non-empty overrides to not be zero")
	}
}

func TestMergeOverrides_BoolPointer(t *testing.T) {
	trueVal := true
	falseVal := false

	lc := &LayeredConfig{
		System: ConfigOverrides{
			DebugMode: &falseVal,
		},
		User: ConfigOverrides{
			DebugMode: &trueVal,
		},
	}

	merged := lc.Merge()
	if merged.DebugMode == nil || *merged.DebugMode != true {
		t.Error("expected debug mode to be true from user override")
	}
}
