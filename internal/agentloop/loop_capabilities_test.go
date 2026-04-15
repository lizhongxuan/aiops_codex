package agentloop

import (
	"context"
	"fmt"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
	"pgregory.net/rapid"
)

// capTestProvider is a configurable bifrost.Provider for capability tests.
type capTestProvider struct {
	caps bifrost.ProviderCapabilities
}

func (p *capTestProvider) Name() string { return "captest" }
func (p *capTestProvider) ChatCompletion(_ context.Context, _ bifrost.ChatRequest) (*bifrost.ChatResponse, error) {
	return nil, nil
}
func (p *capTestProvider) StreamChatCompletion(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
	ch := make(chan bifrost.StreamEvent)
	close(ch)
	return ch, nil
}
func (p *capTestProvider) SupportsToolCalling() bool              { return true }
func (p *capTestProvider) Capabilities() bifrost.ProviderCapabilities { return p.caps }

// newCapTestLoop creates a Loop with a configurable provider and tool registry
// for capability-aware buildChatRequest tests.
func newCapTestLoop(caps bifrost.ProviderCapabilities, webSearchMode string, tools ...ToolEntry) (*Loop, *Session) {
	provider := &capTestProvider{caps: caps}
	gw := bifrost.NewGateway(bifrost.GatewayConfig{DefaultProvider: "captest"})
	gw.RegisterProvider("captest", provider)

	reg := NewToolRegistry()
	for _, t := range tools {
		reg.Register(t)
	}

	loop := NewLoop(gw, reg, nil)
	loop.SetWebSearchMode(webSearchMode)

	// Build enabled tool names from the registered tools.
	enabledNames := make([]string, 0, len(tools))
	for _, t := range tools {
		enabledNames = append(enabledNames, t.Name)
	}

	session := NewSession("cap-test", SessionSpec{
		Model:        "captest-model",
		DynamicTools: enabledNames,
	})
	session.ContextManager().AppendUser("test input")
	return loop, session
}

// genToolName generates a random non-empty tool name that is not "web_search".
func genToolName() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		name := rapid.StringMatching(`[a-z][a-z0-9_]{2,15}`).Draw(t, "tool_name")
		if name == "web_search" {
			name = "other_tool"
		}
		return name
	})
}

// genNonWebSearchTools generates a slice of ToolEntry with random names (none named "web_search").
func genNonWebSearchTools(minCount, maxCount int) *rapid.Generator[[]ToolEntry] {
	return rapid.Custom(func(t *rapid.T) []ToolEntry {
		count := rapid.IntRange(minCount, maxCount).Draw(t, "tool_count")
		seen := make(map[string]bool)
		tools := make([]ToolEntry, 0, count)
		for i := 0; i < count; i++ {
			name := genToolName().Draw(t, "name")
			for seen[name] {
				name = genToolName().Draw(t, "name")
			}
			seen[name] = true
			tools = append(tools, ToolEntry{Name: name, Description: "test tool"})
		}
		return tools
	})
}

// Feature: bifrost-provider-capabilities, Property 2: Native search tool exclusion
// **Validates: Requirements 2.2, 5.2**
// When webSearchMode is "native" and the provider supports native search,
// buildChatRequest must NOT include a function tool named "web_search" and
// must set WebSearchEnabled=true and UseResponsesAPI=true.
func TestProperty2_NativeSearchToolExclusion(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random extra tools (not web_search).
		extraTools := genNonWebSearchTools(0, 5).Draw(t, "extra_tools")

		// Always include web_search in the registry.
		allTools := append([]ToolEntry{{Name: "web_search", Description: "search the web"}}, extraTools...)

		caps := bifrost.ProviderCapabilities{
			SupportsNativeSearch:       true,
			SupportsStreamingToolCalls: rapid.Bool().Draw(t, "streaming_tool_calls"),
			ToolCallingFormat:          "openai_function",
		}

		loop, session := newCapTestLoop(caps, "native", allTools...)
		req := loop.buildChatRequest(session)

		// web_search must NOT appear in the request tools.
		for _, tool := range req.Tools {
			if tool.Function.Name == "web_search" {
				t.Fatalf("web_search function tool should be excluded when native search is enabled, but found it in request tools")
			}
		}

		// WebSearchEnabled and UseResponsesAPI must be true.
		if !req.WebSearchEnabled {
			t.Fatal("WebSearchEnabled should be true when native search is active")
		}
		if !req.UseResponsesAPI {
			t.Fatal("UseResponsesAPI should be true when native search is active")
		}

		// All extra tools should still be present.
		toolNames := make(map[string]bool)
		for _, tool := range req.Tools {
			toolNames[tool.Function.Name] = true
		}
		for _, et := range extraTools {
			if !toolNames[et.Name] {
				t.Fatalf("extra tool %q should still be present in request tools", et.Name)
			}
		}
	})
}

// Feature: bifrost-provider-capabilities, Property 3: Non-native search includes function tool
// **Validates: Requirements 2.3, 5.3**
// When webSearchMode is NOT "native" (or provider doesn't support native search),
// buildChatRequest must include the web_search function tool and NOT set
// WebSearchEnabled or UseResponsesAPI.
func TestProperty3_NonNativeSearchIncludesFunctionTool(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random extra tools.
		extraTools := genNonWebSearchTools(0, 5).Draw(t, "extra_tools")

		// Always include web_search in the registry.
		allTools := append([]ToolEntry{{Name: "web_search", Description: "search the web"}}, extraTools...)

		// Pick a non-native mode.
		mode := rapid.SampledFrom([]string{"duckduckgo", "brave", ""}).Draw(t, "mode")

		caps := bifrost.ProviderCapabilities{
			SupportsNativeSearch:       rapid.Bool().Draw(t, "native_search"),
			SupportsStreamingToolCalls: true,
			ToolCallingFormat:          "openai_function",
		}

		loop, session := newCapTestLoop(caps, mode, allTools...)
		req := loop.buildChatRequest(session)

		// web_search function tool must be present.
		found := false
		for _, tool := range req.Tools {
			if tool.Function.Name == "web_search" {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("web_search function tool should be present when mode is not 'native'")
		}

		// WebSearchEnabled and UseResponsesAPI must be false.
		if req.WebSearchEnabled {
			t.Fatal("WebSearchEnabled should be false when mode is not 'native'")
		}
		if req.UseResponsesAPI {
			t.Fatal("UseResponsesAPI should be false when mode is not 'native'")
		}
	})
}

// Feature: bifrost-provider-capabilities, Property 12: Tool registry integrity after buildChatRequest
// **Validates: Requirements 5.6**
// buildChatRequest must never mutate the ToolRegistry. After calling buildChatRequest,
// the registry must still contain the same set of tools as before.
func TestProperty12_ToolRegistryIntegrityAfterBuildChatRequest(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random tools, always including web_search.
		extraTools := genNonWebSearchTools(0, 5).Draw(t, "extra_tools")
		allTools := append([]ToolEntry{{Name: "web_search", Description: "search the web"}}, extraTools...)

		// Random capabilities and mode.
		nativeSearch := rapid.Bool().Draw(t, "native_search")
		mode := rapid.SampledFrom([]string{"native", "duckduckgo", "brave", "disabled"}).Draw(t, "mode")

		caps := bifrost.ProviderCapabilities{
			SupportsNativeSearch:       nativeSearch,
			SupportsStreamingToolCalls: rapid.Bool().Draw(t, "streaming_tool_calls"),
			ToolCallingFormat:          "openai_function",
		}

		loop, session := newCapTestLoop(caps, mode, allTools...)

		// Snapshot registry state before.
		regNamesBefore := loop.toolReg.Names()

		// Call buildChatRequest (potentially filters tools).
		_ = loop.buildChatRequest(session)

		// Snapshot registry state after.
		regNamesAfter := loop.toolReg.Names()

		// Registry must be unchanged.
		if len(regNamesBefore) != len(regNamesAfter) {
			t.Fatalf("registry size changed: before=%d after=%d", len(regNamesBefore), len(regNamesAfter))
		}
		for i, name := range regNamesBefore {
			if regNamesAfter[i] != name {
				t.Fatalf("registry tool at index %d changed: before=%q after=%q", i, name, regNamesAfter[i])
			}
		}
	})
}

// --- Task 13.1: Integration test for fallback capability re-evaluation ---

// namedCapTestProvider is a configurable bifrost.Provider with a custom name.
type namedCapTestProvider struct {
	name string
	caps bifrost.ProviderCapabilities
}

func (p *namedCapTestProvider) Name() string { return p.name }
func (p *namedCapTestProvider) ChatCompletion(_ context.Context, _ bifrost.ChatRequest) (*bifrost.ChatResponse, error) {
	return nil, fmt.Errorf("provider %s: simulated failure", p.name)
}
func (p *namedCapTestProvider) StreamChatCompletion(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
	ch := make(chan bifrost.StreamEvent)
	close(ch)
	return ch, nil
}
func (p *namedCapTestProvider) SupportsToolCalling() bool                    { return true }
func (p *namedCapTestProvider) Capabilities() bifrost.ProviderCapabilities { return p.caps }

// TestFallbackCapabilityReEvaluation verifies that when the fallback chain
// switches providers, buildChatRequest re-evaluates capabilities fresh and
// adjusts the tool list accordingly.
//
// Scenario: primary provider supports native search (web_search excluded),
// fallback provider does NOT support native search (web_search included).
// Requirements: 9.1, 9.2, 9.3
func TestFallbackCapabilityReEvaluation(t *testing.T) {
	// Primary provider: supports native search.
	primary := &namedCapTestProvider{
		name: "primary",
		caps: bifrost.ProviderCapabilities{
			SupportsNativeSearch:       true,
			SupportsStreamingToolCalls: true,
			ToolCallingFormat:          "openai_function",
		},
	}

	// Fallback provider: does NOT support native search.
	fallback := &namedCapTestProvider{
		name: "fallback",
		caps: bifrost.ProviderCapabilities{
			SupportsNativeSearch:       false,
			SupportsStreamingToolCalls: true,
			ToolCallingFormat:          "openai_function",
		},
	}

	// Create gateway with primary as default.
	gw := bifrost.NewGateway(bifrost.GatewayConfig{
		DefaultProvider: "primary",
		DefaultModel:    "test-model",
	})
	gw.RegisterProvider("primary", primary)
	gw.RegisterProvider("fallback", fallback)

	// Set up fallback chain.
	fc := bifrost.NewFallbackChain([]bifrost.FallbackEntry{
		{Provider: "fallback", Model: "fallback-model"},
	})
	gw.SetFallbackChain(fc)

	// Register tools including web_search.
	reg := NewToolRegistry()
	reg.Register(ToolEntry{Name: "web_search", Description: "search the web"})
	reg.Register(ToolEntry{Name: "read_file", Description: "read a file"})

	loop := NewLoop(gw, reg, nil)
	loop.SetWebSearchMode("native")

	session := NewSession("fallback-test", SessionSpec{
		Model:        "test-model",
		DynamicTools: []string{"web_search", "read_file"},
	})
	session.ContextManager().AppendUser("test input")

	// --- Phase 1: Primary provider is active (native search supported) ---
	req1 := loop.buildChatRequest(session)

	// web_search should be EXCLUDED (native search active).
	for _, tool := range req1.Tools {
		if tool.Function.Name == "web_search" {
			t.Fatal("Phase 1: web_search should be excluded when primary provider supports native search")
		}
	}
	if !req1.WebSearchEnabled {
		t.Fatal("Phase 1: WebSearchEnabled should be true for primary provider")
	}
	if !req1.UseResponsesAPI {
		t.Fatal("Phase 1: UseResponsesAPI should be true for primary provider")
	}

	// --- Phase 2: Activate fallback (no native search) ---
	activated := fc.TryActivate(gw)
	if !activated {
		t.Fatal("Phase 2: fallback chain should have activated")
	}

	// Update session model to match fallback.
	session2 := NewSession("fallback-test-2", SessionSpec{
		Model:        "fallback-model",
		DynamicTools: []string{"web_search", "read_file"},
	})
	session2.ContextManager().AppendUser("test input")

	req2 := loop.buildChatRequest(session2)

	// web_search should be INCLUDED (fallback provider doesn't support native search).
	foundWebSearch := false
	for _, tool := range req2.Tools {
		if tool.Function.Name == "web_search" {
			foundWebSearch = true
			break
		}
	}
	if !foundWebSearch {
		t.Fatal("Phase 2: web_search should be included when fallback provider doesn't support native search")
	}
	if req2.WebSearchEnabled {
		t.Fatal("Phase 2: WebSearchEnabled should be false for fallback provider")
	}
	if req2.UseResponsesAPI {
		t.Fatal("Phase 2: UseResponsesAPI should be false for fallback provider")
	}

	// --- Phase 3: Restore primary and verify capabilities revert ---
	fc.RestorePrimary(gw)

	session3 := NewSession("fallback-test-3", SessionSpec{
		Model:        "test-model",
		DynamicTools: []string{"web_search", "read_file"},
	})
	session3.ContextManager().AppendUser("test input")

	req3 := loop.buildChatRequest(session3)

	// web_search should be EXCLUDED again (back to primary with native search).
	for _, tool := range req3.Tools {
		if tool.Function.Name == "web_search" {
			t.Fatal("Phase 3: web_search should be excluded after restoring primary provider")
		}
	}
	if !req3.WebSearchEnabled {
		t.Fatal("Phase 3: WebSearchEnabled should be true after restoring primary")
	}
}

// --- Task 13.2: Property test for fallback capability adjustment ---

// Feature: bifrost-provider-capabilities, Property 13: Fallback capability adjustment
// **Validates: Requirements 9.2, 9.3**
// For any two providers where one supports native search and the other does not,
// switching the active provider (via fallback) and calling buildChatRequest should
// produce tool lists that reflect the new provider's capabilities — specifically,
// the presence or absence of web_search in the function tools should change accordingly.
func TestProperty13_FallbackCapabilityAdjustment(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random extra tools (not web_search).
		extraTools := genNonWebSearchTools(1, 5).Draw(t, "extra_tools")

		// Randomly decide which provider supports native search.
		// One must support it, the other must not.
		primaryNative := rapid.Bool().Draw(t, "primary_native")
		fallbackNative := !primaryNative

		primaryCaps := bifrost.ProviderCapabilities{
			SupportsNativeSearch:       primaryNative,
			SupportsStreamingToolCalls: true,
			ToolCallingFormat:          "openai_function",
		}
		fallbackCaps := bifrost.ProviderCapabilities{
			SupportsNativeSearch:       fallbackNative,
			SupportsStreamingToolCalls: true,
			ToolCallingFormat:          "openai_function",
		}

		primaryProv := &namedCapTestProvider{name: "primary", caps: primaryCaps}
		fallbackProv := &namedCapTestProvider{name: "fallback", caps: fallbackCaps}

		gw := bifrost.NewGateway(bifrost.GatewayConfig{
			DefaultProvider: "primary",
			DefaultModel:    "primary-model",
		})
		gw.RegisterProvider("primary", primaryProv)
		gw.RegisterProvider("fallback", fallbackProv)

		fc := bifrost.NewFallbackChain([]bifrost.FallbackEntry{
			{Provider: "fallback", Model: "fallback-model"},
		})
		gw.SetFallbackChain(fc)

		// Register tools: always include web_search + extra tools.
		reg := NewToolRegistry()
		reg.Register(ToolEntry{Name: "web_search", Description: "search the web"})
		enabledNames := []string{"web_search"}
		for _, et := range extraTools {
			reg.Register(et)
			enabledNames = append(enabledNames, et.Name)
		}

		loop := NewLoop(gw, reg, nil)
		loop.SetWebSearchMode("native")

		// Build request with primary provider active.
		session1 := NewSession("prop13-primary", SessionSpec{
			Model:        "primary-model",
			DynamicTools: enabledNames,
		})
		session1.ContextManager().AppendUser("test")

		req1 := loop.buildChatRequest(session1)
		req1HasWebSearch := hasToolNamed(req1.Tools, "web_search")

		// Activate fallback.
		if !fc.TryActivate(gw) {
			t.Fatal("fallback chain should activate")
		}

		// Build request with fallback provider active.
		session2 := NewSession("prop13-fallback", SessionSpec{
			Model:        "fallback-model",
			DynamicTools: enabledNames,
		})
		session2.ContextManager().AppendUser("test")

		req2 := loop.buildChatRequest(session2)
		req2HasWebSearch := hasToolNamed(req2.Tools, "web_search")

		// The provider with native search should NOT have web_search in tools.
		// The provider without native search SHOULD have web_search in tools.
		if primaryNative {
			// Primary supports native → web_search excluded in req1.
			if req1HasWebSearch {
				t.Fatal("primary supports native search: web_search should be excluded from req1 tools")
			}
			if !req1.WebSearchEnabled {
				t.Fatal("primary supports native search: WebSearchEnabled should be true in req1")
			}
			// Fallback does NOT support native → web_search included in req2.
			if !req2HasWebSearch {
				t.Fatal("fallback lacks native search: web_search should be included in req2 tools")
			}
			if req2.WebSearchEnabled {
				t.Fatal("fallback lacks native search: WebSearchEnabled should be false in req2")
			}
		} else {
			// Primary does NOT support native → web_search included in req1.
			if !req1HasWebSearch {
				t.Fatal("primary lacks native search: web_search should be included in req1 tools")
			}
			if req1.WebSearchEnabled {
				t.Fatal("primary lacks native search: WebSearchEnabled should be false in req1")
			}
			// Fallback supports native → web_search excluded in req2.
			if req2HasWebSearch {
				t.Fatal("fallback supports native search: web_search should be excluded from req2 tools")
			}
			if !req2.WebSearchEnabled {
				t.Fatal("fallback supports native search: WebSearchEnabled should be true in req2")
			}
		}

		// The web_search presence should differ between the two requests.
		if req1HasWebSearch == req2HasWebSearch {
			t.Fatal("web_search presence should differ between primary and fallback requests")
		}
	})
}

// hasToolNamed checks if a tool with the given name exists in the tool list.
func hasToolNamed(tools []bifrost.ToolDefinition, name string) bool {
	for _, t := range tools {
		if t.Function.Name == name {
			return true
		}
	}
	return false
}
