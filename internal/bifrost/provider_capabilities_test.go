package bifrost

import (
	"encoding/json"
	"testing"

	"pgregory.net/rapid"
)

// genProviderCapabilities generates arbitrary ProviderCapabilities values.
func genProviderCapabilities() *rapid.Generator[ProviderCapabilities] {
	return rapid.Custom(func(t *rapid.T) ProviderCapabilities {
		format := rapid.SampledFrom([]string{"openai_function", "anthropic_tool_use"}).Draw(t, "tool_calling_format")
		return ProviderCapabilities{
			SupportsNativeSearch:       rapid.Bool().Draw(t, "supports_native_search"),
			SupportsReasoningContent:   rapid.Bool().Draw(t, "supports_reasoning_content"),
			SupportsStreamingToolCalls: rapid.Bool().Draw(t, "supports_streaming_tool_calls"),
			SupportsToolUseFormat:      rapid.Bool().Draw(t, "supports_tool_use_format"),
			ToolCallingFormat:          format,
		}
	})
}

// Feature: bifrost-provider-capabilities, Property 14: ProviderCapabilities JSON round-trip
// Validates: Requirements 10.1, 10.2
func TestProperty14_ProviderCapabilities_JSONRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		original := genProviderCapabilities().Draw(t, "caps")

		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}

		var decoded ProviderCapabilities
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}

		if decoded != original {
			t.Fatalf("round-trip mismatch: got %+v, want %+v", decoded, original)
		}
	})
}

// Feature: bifrost-provider-capabilities, Property 15: ProviderCapabilities JSON field names
// Validates: Requirements 10.3
func TestProperty15_ProviderCapabilities_JSONFieldNames(t *testing.T) {
	expectedKeys := map[string]bool{
		"supports_native_search":        true,
		"supports_reasoning_content":    true,
		"supports_streaming_tool_calls": true,
		"supports_tool_use_format":      true,
		"tool_calling_format":           true,
	}

	rapid.Check(t, func(t *rapid.T) {
		caps := genProviderCapabilities().Draw(t, "caps")

		data, err := json.Marshal(caps)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}

		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("unmarshal to map failed: %v", err)
		}

		if len(raw) != len(expectedKeys) {
			t.Fatalf("expected %d keys, got %d: %v", len(expectedKeys), len(raw), raw)
		}

		for key := range raw {
			if !expectedKeys[key] {
				t.Fatalf("unexpected JSON key: %q", key)
			}
		}

		for key := range expectedKeys {
			if _, ok := raw[key]; !ok {
				t.Fatalf("missing expected JSON key: %q", key)
			}
		}
	})
}

// Feature: bifrost-provider-capabilities, Property 1: ProviderCapabilities validity
// Validates: Requirements 1.1
func TestProperty1_ProviderCapabilities_Validity(t *testing.T) {
	validFormats := map[string]bool{
		"openai_function":    true,
		"anthropic_tool_use": true,
	}

	rapid.Check(t, func(t *rapid.T) {
		caps := genProviderCapabilities().Draw(t, "caps")

		// ToolCallingFormat must be one of the known formats.
		if !validFormats[caps.ToolCallingFormat] {
			t.Fatalf("invalid ToolCallingFormat: %q", caps.ToolCallingFormat)
		}

		// All boolean fields must be valid Go bools (always true by type system,
		// but verify they survive JSON round-trip correctly).
		data, err := json.Marshal(caps)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}

		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}

		boolFields := []string{
			"supports_native_search",
			"supports_reasoning_content",
			"supports_streaming_tool_calls",
			"supports_tool_use_format",
		}
		for _, field := range boolFields {
			v, ok := raw[field]
			if !ok {
				t.Fatalf("missing field %q", field)
			}
			if _, isBool := v.(bool); !isBool {
				t.Fatalf("field %q is not a bool: %T", field, v)
			}
		}

		// ToolCallingFormat must be a non-empty string.
		tcf, ok := raw["tool_calling_format"]
		if !ok {
			t.Fatal("missing field tool_calling_format")
		}
		if s, isStr := tcf.(string); !isStr || s == "" {
			t.Fatalf("tool_calling_format must be a non-empty string, got %v", tcf)
		}
	})
}

// --- Unit tests for per-provider Capabilities() return values ---
// Validates: Requirements 1.3, 1.4, 1.6

func TestOpenAIProvider_Capabilities(t *testing.T) {
	p := NewOpenAIProvider("test-key", "")
	caps := p.Capabilities()

	if caps.SupportsNativeSearch != true {
		t.Errorf("SupportsNativeSearch: got %v, want true", caps.SupportsNativeSearch)
	}
	if caps.SupportsReasoningContent != true {
		t.Errorf("SupportsReasoningContent: got %v, want true", caps.SupportsReasoningContent)
	}
	if caps.SupportsStreamingToolCalls != true {
		t.Errorf("SupportsStreamingToolCalls: got %v, want true", caps.SupportsStreamingToolCalls)
	}
	if caps.SupportsToolUseFormat != false {
		t.Errorf("SupportsToolUseFormat: got %v, want false", caps.SupportsToolUseFormat)
	}
	if caps.ToolCallingFormat != "openai_function" {
		t.Errorf("ToolCallingFormat: got %q, want %q", caps.ToolCallingFormat, "openai_function")
	}
}

func TestAnthropicProvider_Capabilities(t *testing.T) {
	p := NewAnthropicProvider("test-key", "")
	caps := p.Capabilities()

	if caps.SupportsNativeSearch != true {
		t.Errorf("SupportsNativeSearch: got %v, want true", caps.SupportsNativeSearch)
	}
	if caps.SupportsReasoningContent != false {
		t.Errorf("SupportsReasoningContent: got %v, want false", caps.SupportsReasoningContent)
	}
	if caps.SupportsStreamingToolCalls != true {
		t.Errorf("SupportsStreamingToolCalls: got %v, want true", caps.SupportsStreamingToolCalls)
	}
	if caps.SupportsToolUseFormat != true {
		t.Errorf("SupportsToolUseFormat: got %v, want true", caps.SupportsToolUseFormat)
	}
	if caps.ToolCallingFormat != "anthropic_tool_use" {
		t.Errorf("ToolCallingFormat: got %q, want %q", caps.ToolCallingFormat, "anthropic_tool_use")
	}
}

func TestOllamaProvider_Capabilities(t *testing.T) {
	p := NewOllamaProvider("")
	caps := p.Capabilities()

	if caps.SupportsNativeSearch != false {
		t.Errorf("SupportsNativeSearch: got %v, want false", caps.SupportsNativeSearch)
	}
	if caps.SupportsReasoningContent != false {
		t.Errorf("SupportsReasoningContent: got %v, want false", caps.SupportsReasoningContent)
	}
	if caps.SupportsStreamingToolCalls != true {
		t.Errorf("SupportsStreamingToolCalls: got %v, want true", caps.SupportsStreamingToolCalls)
	}
	if caps.SupportsToolUseFormat != false {
		t.Errorf("SupportsToolUseFormat: got %v, want false", caps.SupportsToolUseFormat)
	}
	if caps.ToolCallingFormat != "openai_function" {
		t.Errorf("ToolCallingFormat: got %q, want %q", caps.ToolCallingFormat, "openai_function")
	}
}
