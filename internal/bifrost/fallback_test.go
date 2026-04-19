package bifrost

import (
	"context"
	"errors"
	"testing"
)

// --- FallbackChain tests ---

func TestFallbackChain_TryActivate(t *testing.T) {
	gw := NewGateway(GatewayConfig{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4o",
	})
	gw.RegisterProvider("openai", &stubProvider{name: "openai"})
	gw.RegisterProvider("anthropic", &stubProvider{name: "anthropic"})

	fc := NewFallbackChain([]FallbackEntry{
		{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
	})

	// Activate should switch to anthropic.
	if !fc.TryActivate(gw) {
		t.Fatal("expected TryActivate to return true")
	}
	if !fc.IsActivated() {
		t.Error("expected IsActivated to be true")
	}

	gw.mu.RLock()
	if gw.defaultProvider != "anthropic" {
		t.Errorf("defaultProvider: got %q, want %q", gw.defaultProvider, "anthropic")
	}
	if gw.defaultModel != "claude-sonnet-4-20250514" {
		t.Errorf("defaultModel: got %q, want %q", gw.defaultModel, "claude-sonnet-4-20250514")
	}
	gw.mu.RUnlock()
}

func TestFallbackChain_RestorePrimary(t *testing.T) {
	gw := NewGateway(GatewayConfig{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4o",
	})
	gw.RegisterProvider("openai", &stubProvider{name: "openai"})
	gw.RegisterProvider("anthropic", &stubProvider{name: "anthropic"})

	fc := NewFallbackChain([]FallbackEntry{
		{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
	})

	// Activate fallback.
	fc.TryActivate(gw)

	// Restore primary.
	if !fc.RestorePrimary(gw) {
		t.Fatal("expected RestorePrimary to return true")
	}
	if fc.IsActivated() {
		t.Error("expected IsActivated to be false after restore")
	}

	gw.mu.RLock()
	if gw.defaultProvider != "openai" {
		t.Errorf("defaultProvider: got %q, want %q", gw.defaultProvider, "openai")
	}
	if gw.defaultModel != "gpt-4o" {
		t.Errorf("defaultModel: got %q, want %q", gw.defaultModel, "gpt-4o")
	}
	gw.mu.RUnlock()
}

func TestFallbackChain_ChainExhaustion(t *testing.T) {
	gw := NewGateway(GatewayConfig{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4o",
	})

	fc := NewFallbackChain([]FallbackEntry{
		{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
		{Provider: "ollama", Model: "llama3"},
	})

	// First activation: anthropic.
	if !fc.TryActivate(gw) {
		t.Fatal("expected first TryActivate to succeed")
	}
	gw.mu.RLock()
	if gw.defaultProvider != "anthropic" {
		t.Errorf("after first activate: got %q, want %q", gw.defaultProvider, "anthropic")
	}
	gw.mu.RUnlock()

	// Second activation: ollama.
	if !fc.TryActivate(gw) {
		t.Fatal("expected second TryActivate to succeed")
	}
	gw.mu.RLock()
	if gw.defaultProvider != "ollama" {
		t.Errorf("after second activate: got %q, want %q", gw.defaultProvider, "ollama")
	}
	gw.mu.RUnlock()

	// Third activation: chain exhausted.
	if fc.TryActivate(gw) {
		t.Error("expected TryActivate to return false when chain is exhausted")
	}
}

func TestFallbackChain_EmptyChain(t *testing.T) {
	gw := NewGateway(GatewayConfig{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4o",
	})

	fc := NewFallbackChain(nil)
	if fc.TryActivate(gw) {
		t.Error("expected TryActivate to return false for empty chain")
	}
	if fc.IsActivated() {
		t.Error("expected IsActivated to be false for empty chain")
	}
}

func TestFallbackChain_RestoreWithoutActivation(t *testing.T) {
	gw := NewGateway(GatewayConfig{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4o",
	})

	fc := NewFallbackChain([]FallbackEntry{
		{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
	})

	// RestorePrimary without prior activation should return false.
	if fc.RestorePrimary(gw) {
		t.Error("expected RestorePrimary to return false without prior activation")
	}
}

func TestFallbackChain_ReactivateAfterRestore(t *testing.T) {
	gw := NewGateway(GatewayConfig{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4o",
	})
	gw.RegisterProvider("openai", &stubProvider{name: "openai"})
	gw.RegisterProvider("anthropic", &stubProvider{name: "anthropic"})

	fc := NewFallbackChain([]FallbackEntry{
		{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
	})

	// Activate, restore, then re-activate (simulates new turn).
	fc.TryActivate(gw)
	fc.RestorePrimary(gw)

	// Should be able to activate again after restore.
	if !fc.TryActivate(gw) {
		t.Fatal("expected TryActivate to succeed after RestorePrimary")
	}
	gw.mu.RLock()
	if gw.defaultProvider != "anthropic" {
		t.Errorf("defaultProvider: got %q, want %q", gw.defaultProvider, "anthropic")
	}
	gw.mu.RUnlock()
}

// --- Integration: Gateway with retry + fallback ---

func TestGateway_FallbackOnPersistentFailure(t *testing.T) {
	gw := NewGateway(GatewayConfig{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4o",
	})

	// Primary provider always fails.
	gw.RegisterProvider("openai", &stubProvider{
		name: "openai",
		err:  errors.New("bifrost/openai: API error 500: internal server error"),
	})

	// Fallback provider succeeds.
	gw.RegisterProvider("anthropic", &stubProvider{
		name: "anthropic",
		resp: &ChatResponse{
			Message: Message{Role: "assistant", Content: "fallback response"},
			Usage:   Usage{PromptTokens: 5, CompletionTokens: 3},
		},
	})

	fc := NewFallbackChain([]FallbackEntry{
		{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
	})
	gw.SetFallbackChain(fc)

	resp, err := gw.ChatCompletion(context.Background(), validReq("gpt-4o"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Content != "fallback response" {
		t.Errorf("Content: got %v, want %q", resp.Message.Content, "fallback response")
	}
}

func TestGateway_NoFallbackOnSuccess(t *testing.T) {
	gw := NewGateway(GatewayConfig{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4o",
	})

	gw.RegisterProvider("openai", &stubProvider{
		name: "openai",
		resp: &ChatResponse{
			Message: Message{Role: "assistant", Content: "primary response"},
			Usage:   Usage{PromptTokens: 10, CompletionTokens: 5},
		},
	})

	fc := NewFallbackChain([]FallbackEntry{
		{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
	})
	gw.SetFallbackChain(fc)

	resp, err := gw.ChatCompletion(context.Background(), validReq("gpt-4o"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Content != "primary response" {
		t.Errorf("Content: got %v, want %q", resp.Message.Content, "primary response")
	}
	if fc.IsActivated() {
		t.Error("fallback should not be activated on success")
	}
}

func TestGateway_FallbackChainExhausted(t *testing.T) {
	gw := NewGateway(GatewayConfig{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4o",
	})

	// All providers fail.
	gw.RegisterProvider("openai", &stubProvider{
		name: "openai",
		err:  errors.New("bifrost/openai: API error 500: internal server error"),
	})
	gw.RegisterProvider("anthropic", &stubProvider{
		name: "anthropic",
		err:  errors.New("bifrost/anthropic: API error 500: internal server error"),
	})

	fc := NewFallbackChain([]FallbackEntry{
		{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
	})
	gw.SetFallbackChain(fc)

	_, err := gw.ChatCompletion(context.Background(), validReq("gpt-4o"))
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
}

func TestGateway_CredentialRotationOn429(t *testing.T) {
	pool := NewCredentialPool([]*Credential{
		{ID: "1", Provider: "openai", APIKey: "sk-1", Status: "active"},
		{ID: "2", Provider: "openai", APIKey: "sk-2", Status: "active"},
	})

	gw := NewGateway(GatewayConfig{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4o",
		Pool:            pool,
	})

	callCount := 0
	gw.RegisterProvider("openai", &countingProvider{
		name: "openai",
		fn: func() (*ChatResponse, error) {
			callCount++
			if callCount == 1 {
				return nil, &APIError{StatusCode: 429, Message: "rate limited"}
			}
			return &ChatResponse{
				Message: Message{Role: "assistant", Content: "ok"},
				Usage:   Usage{PromptTokens: 5, CompletionTokens: 3},
			}, nil
		},
	})

	resp, err := gw.ChatCompletion(context.Background(), validReq("gpt-4o"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Errorf("Content: got %v, want %q", resp.Message.Content, "ok")
	}
	if callCount < 2 {
		t.Errorf("expected at least 2 calls, got %d", callCount)
	}
}

// countingProvider is a test helper that uses a function for ChatCompletion.
type countingProvider struct {
	name string
	fn   func() (*ChatResponse, error)
}

func (p *countingProvider) Name() string { return p.name }
func (p *countingProvider) SupportsToolCalling() bool { return true }
func (p *countingProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{ToolCallingFormat: "openai_function"}
}
func (p *countingProvider) ChatCompletion(_ context.Context, _ ChatRequest) (*ChatResponse, error) {
	return p.fn()
}
func (p *countingProvider) StreamChatCompletion(_ context.Context, _ ChatRequest) (<-chan StreamEvent, error) {
	return nil, errors.New("not implemented")
}
