package bifrost

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// --- WithRetry tests ---

func TestWithRetry_SuccessOnFirstTry(t *testing.T) {
	cfg := DefaultRetryConfig()
	expected := &ChatResponse{
		Message: Message{Role: "assistant", Content: "hello"},
		Usage:   Usage{PromptTokens: 10, CompletionTokens: 5},
	}

	resp, err := WithRetry(context.Background(), cfg, func() (*ChatResponse, error) {
		return expected, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != expected {
		t.Errorf("got %+v, want %+v", resp, expected)
	}
}

func TestWithRetry_SuccessAfterRetries(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		BackoffFactor:  2.0,
	}

	attempt := 0
	expected := &ChatResponse{
		Message: Message{Role: "assistant", Content: "ok"},
	}

	resp, err := WithRetry(context.Background(), cfg, func() (*ChatResponse, error) {
		attempt++
		if attempt < 3 {
			return nil, &APIError{StatusCode: 500, Message: "server error"}
		}
		return expected, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != expected {
		t.Errorf("got %+v, want %+v", resp, expected)
	}
	if attempt != 3 {
		t.Errorf("attempts: got %d, want 3", attempt)
	}
}

func TestWithRetry_MaxRetriesExceeded(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries:     2,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		BackoffFactor:  2.0,
	}

	attempt := 0
	_, err := WithRetry(context.Background(), cfg, func() (*ChatResponse, error) {
		attempt++
		return nil, &APIError{StatusCode: 503, Message: "service unavailable"}
	})
	if err == nil {
		t.Fatal("expected error after max retries")
	}
	if attempt != 3 { // initial + 2 retries
		t.Errorf("attempts: got %d, want 3", attempt)
	}
	if got := err.Error(); got == "" {
		t.Error("expected non-empty error message")
	}
}

func TestWithRetry_ContextCancellation(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries:     5,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     10 * time.Second,
		BackoffFactor:  2.0,
	}

	ctx, cancel := context.WithCancel(context.Background())
	attempt := 0

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := WithRetry(ctx, cfg, func() (*ChatResponse, error) {
		attempt++
		return nil, &APIError{StatusCode: 500, Message: "server error"}
	})
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestWithRetry_NonRetryableError(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		BackoffFactor:  2.0,
	}

	attempt := 0
	_, err := WithRetry(context.Background(), cfg, func() (*ChatResponse, error) {
		attempt++
		return nil, &APIError{StatusCode: 400, Message: "bad request"}
	})
	if err == nil {
		t.Fatal("expected error for non-retryable error")
	}
	if attempt != 1 {
		t.Errorf("attempts: got %d, want 1 (should not retry 400)", attempt)
	}
}

func TestWithRetry_429IsRetryable(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries:     2,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		BackoffFactor:  2.0,
	}

	attempt := 0
	expected := &ChatResponse{
		Message: Message{Role: "assistant", Content: "ok"},
	}

	resp, err := WithRetry(context.Background(), cfg, func() (*ChatResponse, error) {
		attempt++
		if attempt == 1 {
			return nil, &APIError{StatusCode: 429, Message: "rate limited"}
		}
		return expected, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != expected {
		t.Errorf("got %+v, want %+v", resp, expected)
	}
	if attempt != 2 {
		t.Errorf("attempts: got %d, want 2", attempt)
	}
}

func TestWithRetry_EmptyResponseTriggersRetry(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries:     2,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		BackoffFactor:  2.0,
	}

	attempt := 0
	expected := &ChatResponse{
		Message: Message{Role: "assistant", Content: "ok"},
	}

	resp, err := WithRetry(context.Background(), cfg, func() (*ChatResponse, error) {
		attempt++
		if attempt == 1 {
			return &ChatResponse{}, nil // empty response
		}
		return expected, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != expected {
		t.Errorf("got %+v, want %+v", resp, expected)
	}
}

// --- IsTransportError tests ---

func TestIsTransportError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"connection reset", errors.New("read tcp: connection reset by peer"), true},
		{"EOF", errors.New("unexpected EOF"), true},
		{"TLS handshake timeout", errors.New("TLS handshake timeout"), true},
		{"broken pipe", errors.New("write: broken pipe"), true},
		{"connection refused", errors.New("dial tcp: connection refused"), true},
		{"i/o timeout", errors.New("i/o timeout"), true},
		{"closed connection", errors.New("use of closed network connection"), true},
		{"regular API error", errors.New("bifrost/openai: API error 400: bad request"), false},
		{"auth error", errors.New("bifrost/openai: API error 401: unauthorized"), false},
		{"rate limit text", errors.New("rate limit exceeded"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsTransportError(tt.err)
			if got != tt.want {
				t.Errorf("IsTransportError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// --- isRetryableError tests ---

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"429 APIError", &APIError{StatusCode: 429, Message: "rate limited"}, true},
		{"500 APIError", &APIError{StatusCode: 500, Message: "internal error"}, true},
		{"502 APIError", &APIError{StatusCode: 502, Message: "bad gateway"}, true},
		{"503 APIError", &APIError{StatusCode: 503, Message: "unavailable"}, true},
		{"400 APIError", &APIError{StatusCode: 400, Message: "bad request"}, false},
		{"401 APIError", &APIError{StatusCode: 401, Message: "unauthorized"}, false},
		{"403 APIError", &APIError{StatusCode: 403, Message: "forbidden"}, false},
		{"transport error", errors.New("connection reset by peer"), true},
		{"generic error with 429", fmt.Errorf("bifrost/openai: API error 429: rate limit"), true},
		{"generic error with 500", fmt.Errorf("bifrost/openai: API error 500: server error"), true},
		{"generic unrelated error", errors.New("something went wrong"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetryableError(tt.err)
			if got != tt.want {
				t.Errorf("isRetryableError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// --- DefaultRetryConfig test ---

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries: got %d, want 3", cfg.MaxRetries)
	}
	if cfg.InitialBackoff != 5*time.Second {
		t.Errorf("InitialBackoff: got %v, want 5s", cfg.InitialBackoff)
	}
	if cfg.MaxBackoff != 120*time.Second {
		t.Errorf("MaxBackoff: got %v, want 120s", cfg.MaxBackoff)
	}
	if cfg.BackoffFactor != 2.0 {
		t.Errorf("BackoffFactor: got %f, want 2.0", cfg.BackoffFactor)
	}
}

// --- interruptibleSleep test ---

func TestInterruptibleSleep_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := interruptibleSleep(ctx, 5*time.Second)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	if elapsed > 1*time.Second {
		t.Errorf("sleep took too long: %v (should have been interrupted quickly)", elapsed)
	}
}
