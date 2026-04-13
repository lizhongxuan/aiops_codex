package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
)

// LayeredConfig loads configuration from three layers:
// System (defaults), User (~/.aiops_codex/config.json), and Repo (.aiops_codex/config.json).
type LayeredConfig struct {
	System ConfigOverrides `json:"system"`
	User   ConfigOverrides `json:"user"`
	Repo   ConfigOverrides `json:"repo"`
}

// ConfigOverrides represents partial configuration that can override values.
// Only non-zero fields are considered as overrides.
type ConfigOverrides struct {
	LLMProvider  string   `json:"llm_provider,omitempty"`
	LLMModel     string   `json:"llm_model,omitempty"`
	LLMBaseURL   string   `json:"llm_base_url,omitempty"`
	MCPPaths     []string `json:"mcp_paths,omitempty"`
	SkillRoots   []string `json:"skill_roots,omitempty"`
	ExecPolicy   string   `json:"exec_policy_path,omitempty"`
	PluginDir    string   `json:"plugin_dir,omitempty"`
	OtelExporter string   `json:"otel_exporter,omitempty"`
	DebugMode    *bool    `json:"debug_mode,omitempty"`
}

// LoadLayered loads all three configuration layers.
func LoadLayered() (*LayeredConfig, error) {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	lc := &LayeredConfig{
		System: defaultOverrides(),
	}

	// User-level config
	if home != "" {
		userPath := filepath.Join(home, ".aiops_codex", "config.json")
		if overrides, err := loadOverridesFile(userPath); err == nil {
			lc.User = overrides
		}
	}

	// Repo-level config
	if cwd != "" {
		repoPath := filepath.Join(cwd, ".aiops_codex", "config.json")
		if overrides, err := loadOverridesFile(repoPath); err == nil {
			lc.Repo = overrides
		}
	}

	return lc, nil
}

// Merge produces the effective configuration by merging layers.
// Repo overrides User, User overrides System.
// Array-type configurations are combined rather than replaced.
func (lc *LayeredConfig) Merge() ConfigOverrides {
	result := lc.System

	// Apply user overrides
	result = mergeOverrides(result, lc.User)

	// Apply repo overrides
	result = mergeOverrides(result, lc.Repo)

	return result
}

// mergeOverrides merges src into dst. Non-zero fields in src override dst.
// Array fields are combined (appended) rather than replaced.
func mergeOverrides(dst, src ConfigOverrides) ConfigOverrides {
	if src.LLMProvider != "" {
		dst.LLMProvider = src.LLMProvider
	}
	if src.LLMModel != "" {
		dst.LLMModel = src.LLMModel
	}
	if src.LLMBaseURL != "" {
		dst.LLMBaseURL = src.LLMBaseURL
	}
	if src.ExecPolicy != "" {
		dst.ExecPolicy = src.ExecPolicy
	}
	if src.PluginDir != "" {
		dst.PluginDir = src.PluginDir
	}
	if src.OtelExporter != "" {
		dst.OtelExporter = src.OtelExporter
	}
	if src.DebugMode != nil {
		dst.DebugMode = src.DebugMode
	}

	// Array combination: append unique values
	dst.MCPPaths = combineStringSlices(dst.MCPPaths, src.MCPPaths)
	dst.SkillRoots = combineStringSlices(dst.SkillRoots, src.SkillRoots)

	return dst
}

// combineStringSlices combines two string slices, deduplicating entries.
func combineStringSlices(base, additions []string) []string {
	if len(additions) == 0 {
		return base
	}
	seen := make(map[string]bool, len(base))
	for _, v := range base {
		seen[v] = true
	}
	result := append([]string(nil), base...)
	for _, v := range additions {
		if !seen[v] {
			result = append(result, v)
			seen[v] = true
		}
	}
	return result
}

// defaultOverrides returns system-level default configuration.
func defaultOverrides() ConfigOverrides {
	return ConfigOverrides{
		LLMProvider:  "openai",
		LLMModel:     "gpt-4o-mini",
		OtelExporter: "noop",
	}
}

// loadOverridesFile loads a ConfigOverrides from a JSON file.
func loadOverridesFile(path string) (ConfigOverrides, error) {
	var overrides ConfigOverrides
	data, err := os.ReadFile(path)
	if err != nil {
		return overrides, err
	}
	if err := json.Unmarshal(data, &overrides); err != nil {
		return overrides, err
	}
	return overrides, nil
}

// IsZero returns true if the overrides have no non-zero fields.
func (co ConfigOverrides) IsZero() bool {
	return reflect.DeepEqual(co, ConfigOverrides{})
}
