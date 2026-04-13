package bifrost

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// --- test doubles ---

type stubProvider struct {
	name     string
	resp     *ChatResponse
	err      error
	streamCh chan StreamEvent
}

func (s *stubProvider) Name() string { return s.name }
func (s *stubProvider) SupportsToolCalling() bool { return true }

func (s *stubProvider) ChatCompletion(_ context.Context, _ ChatRequest) (*ChatResponse, error) {
	return s.resp, s.err
}

func (s *stubProvider) StreamChatCompletion(_ context.Context, _ ChatRequest) (<-chan StreamEvent, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.streamCh, nil
}

type stubTracker struct {
	mu      sync.Mutex
	records []UsageRecord
}

func (t *stubTracker) Record(rec UsageRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.records = append(t.records, rec)
}

// --- helpers ---

func validReq(model string) ChatRequest {
	return ChatRequest{
		Model:    model,
		Messages: []Message{{Role: "user", Content: "hello"}},
	}
}

func newTestGateway(defaultProvider, defaultModel string) *Gateway {
	return NewGateway(GatewayConfig{
		DefaultProvider: defaultProvider,
		DefaultModel:    defaultModel,
	})
}

// --- NewGateway ---

func TestNewGateway(t *testing.T) {
	gw := NewGateway(GatewayConfig{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4o",
		Fallbacks:       []FallbackEntry{{Provider: "anthropic", Model: "claude-sonnet-4-20250514"}},
	})
	if gw.defaultProvider != "openai" {
		t.Errorf("defaultProvider: got %q, want %q", gw.defaultProvider, "openai")
	}
	if gw.defaultModel != "gpt-4o" {
		t.Errorf("defaultModel: got %q, want %q", gw.defaultModel, "gpt-4o")
	}
	if len(gw.fallbacks) != 1 {
		t.Fatalf("fallbacks length: got %d, want 1", len(gw.fallbacks))
	}
}

// --- RegisterProvider / getProvider ---

func TestRegisterAndGetProvider(t *testing.T) {
	gw := newTestGateway("openai", "gpt-4o")
	sp := &stubProvider{name: "openai"}
	gw.RegisterProvider("openai", sp)

	p, err := gw.getProvider("openai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("Name: got %q, want %q", p.Name(), "openai")
	}
}

func TestGetProvider_Unknown(t *testing.T) {
	gw := newTestGateway("openai", "gpt-4o")
	_, err := gw.getProvider("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

// --- ChatCompletion ---

func TestChatCompletion_Success(t *testing.T) {
	tracker := &stubTracker{}
	gw := NewGateway(GatewayConfig{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4o",
		Tracker:         tracker,
	})
	gw.RegisterProvider("openai", &stubProvider{
		name: "openai",
		resp: &ChatResponse{
			Message: Message{Role: "assistant", Content: "hi"},
			Usage:   Usage{PromptTokens: 10, CompletionTokens: 5},
		},
	})

	resp, err := gw.ChatCompletion(context.Background(), validReq("gpt-4o"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Role != "assistant" {
		t.Errorf("Role: got %q, want %q", resp.Message.Role, "assistant")
	}

	// Tracker should have one record.
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if len(tracker.records) != 1 {
		t.Fatalf("tracker records: got %d, want 1", len(tracker.records))
	}
	rec := tracker.records[0]
	if rec.Provider != "openai" || rec.PromptTokens != 10 || rec.OutputTokens != 5 {
		t.Errorf("record: got %+v", rec)
	}
}

func TestChatCompletion_ValidationError(t *testing.T) {
	gw := newTestGateway("openai", "gpt-4o")
	_, err := gw.ChatCompletion(context.Background(), ChatRequest{Model: "", Messages: nil})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestChatCompletion_ProviderError(t *testing.T) {
	gw := newTestGateway("openai", "gpt-4o")
	gw.RegisterProvider("openai", &stubProvider{
		name: "openai",
		err:  errors.New("rate limited"),
	})

	_, err := gw.ChatCompletion(context.Background(), validReq("gpt-4o"))
	if err == nil || err.Error() != "rate limited" {
		t.Fatalf("expected 'rate limited' error, got: %v", err)
	}
}

func TestChatCompletion_NoTracker(t *testing.T) {
	gw := newTestGateway("openai", "gpt-4o")
	gw.RegisterProvider("openai", &stubProvider{
		name: "openai",
		resp: &ChatResponse{
			Message: Message{Role: "assistant", Content: "ok"},
			Usage:   Usage{PromptTokens: 1, CompletionTokens: 1},
		},
	})

	// Should not panic when tracker is nil.
	resp, err := gw.ChatCompletion(context.Background(), validReq("gpt-4o"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

// --- Provider resolution via model prefix ---

func TestChatCompletion_ModelPrefixResolution(t *testing.T) {
	gw := newTestGateway("openai", "gpt-4o")
	gw.RegisterProvider("anthropic", &stubProvider{
		name: "anthropic",
		resp: &ChatResponse{
			Message: Message{Role: "assistant", Content: "bonjour"},
			Usage:   Usage{PromptTokens: 5, CompletionTokens: 3},
		},
	})

	// "anthropic/claude-sonnet-4-20250514" should route to the anthropic provider.
	resp, err := gw.ChatCompletion(context.Background(), validReq("anthropic/claude-sonnet-4-20250514"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Content != "bonjour" {
		t.Errorf("Content: got %v, want %q", resp.Message.Content, "bonjour")
	}
}

func TestChatCompletion_NoProviderConfigured(t *testing.T) {
	gw := newTestGateway("", "")
	_, err := gw.ChatCompletion(context.Background(), validReq("gpt-4o"))
	if err == nil {
		t.Fatal("expected error when no provider is configured")
	}
}

// --- StreamChatCompletion ---

func TestStreamChatCompletion_Success(t *testing.T) {
	ch := make(chan StreamEvent, 2)
	ch <- StreamEvent{Type: "content_delta", Delta: "hi"}
	ch <- StreamEvent{Type: "done"}
	close(ch)

	gw := newTestGateway("openai", "gpt-4o")
	gw.RegisterProvider("openai", &stubProvider{name: "openai", streamCh: ch})

	stream, err := gw.StreamChatCompletion(context.Background(), validReq("gpt-4o"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for ev := range stream {
		events = append(events, ev)
	}
	if len(events) != 2 {
		t.Fatalf("events: got %d, want 2", len(events))
	}
	if events[0].Type != "content_delta" || events[0].Delta != "hi" {
		t.Errorf("event[0]: got %+v", events[0])
	}
}

func TestStreamChatCompletion_ValidationError(t *testing.T) {
	gw := newTestGateway("openai", "gpt-4o")
	_, err := gw.StreamChatCompletion(context.Background(), ChatRequest{Model: "", Messages: nil})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestStreamChatCompletion_ProviderError(t *testing.T) {
	gw := newTestGateway("openai", "gpt-4o")
	gw.RegisterProvider("openai", &stubProvider{
		name: "openai",
		err:  errors.New("stream failed"),
	})

	_, err := gw.StreamChatCompletion(context.Background(), validReq("gpt-4o"))
	if err == nil || err.Error() != "stream failed" {
		t.Fatalf("expected 'stream failed' error, got: %v", err)
	}
}

// --- Concurrent access ---

func TestConcurrentRegisterAndGet(t *testing.T) {
	gw := newTestGateway("openai", "gpt-4o")
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := "provider"
			gw.RegisterProvider(name, &stubProvider{name: name})
		}(i)
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			gw.getProvider("provider")
		}()
	}

	wg.Wait()
}
