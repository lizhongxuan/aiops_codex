package bifrost

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- NewOpenAIProvider ---

func TestNewOpenAIProvider_DefaultBaseURL(t *testing.T) {
	p := NewOpenAIProvider("sk-test", "")
	if p.baseURL != defaultOpenAIBaseURL {
		t.Errorf("baseURL: got %q, want %q", p.baseURL, defaultOpenAIBaseURL)
	}
	if p.apiKey != "sk-test" {
		t.Errorf("apiKey: got %q, want %q", p.apiKey, "sk-test")
	}
}

func TestNewOpenAIProvider_CustomBaseURL(t *testing.T) {
	p := NewOpenAIProvider("sk-test", "https://vllm.example.com/v1/")
	if p.baseURL != "https://vllm.example.com/v1" {
		t.Errorf("baseURL: got %q, want %q (trailing slash should be stripped)", p.baseURL, "https://vllm.example.com/v1")
	}
}

func TestOpenAIProvider_Name(t *testing.T) {
	p := NewOpenAIProvider("", "")
	if p.Name() != "openai" {
		t.Errorf("Name: got %q, want %q", p.Name(), "openai")
	}
}

func TestOpenAIProvider_SupportsToolCalling(t *testing.T) {
	p := NewOpenAIProvider("", "")
	if !p.SupportsToolCalling() {
		t.Error("SupportsToolCalling: got false, want true")
	}
}

// --- ChatCompletion ---

func TestOpenAIProvider_ChatCompletion_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request.
		if r.Method != http.MethodPost {
			t.Errorf("method: got %q, want POST", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path: got %q, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("Authorization: got %q, want %q", got, "Bearer sk-test")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type: got %q, want %q", got, "application/json")
		}

		// Decode request body to verify it's well-formed.
		var reqBody openAIRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if reqBody.Model != "gpt-4o" {
			t.Errorf("request model: got %q, want %q", reqBody.Model, "gpt-4o")
		}
		if reqBody.Stream {
			t.Error("request stream: got true, want false")
		}

		// Return a valid response.
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Hello!",
					},
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"cached_tokens":     2,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := NewOpenAIProvider("sk-test", srv.URL)
	req := ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}

	resp, err := p.ChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Role != "assistant" {
		t.Errorf("role: got %q, want %q", resp.Message.Role, "assistant")
	}
	if resp.Message.Content != "Hello!" {
		t.Errorf("content: got %v, want %q", resp.Message.Content, "Hello!")
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("prompt_tokens: got %d, want 10", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 5 {
		t.Errorf("completion_tokens: got %d, want 5", resp.Usage.CompletionTokens)
	}
	if resp.Usage.CachedTokens != 2 {
		t.Errorf("cached_tokens: got %d, want 2", resp.Usage.CachedTokens)
	}
}

func TestOpenAIProvider_ChatCompletion_WithToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]interface{}{
							{
								"id":   "call_abc",
								"type": "function",
								"function": map[string]interface{}{
									"name":      "get_weather",
									"arguments": `{"city":"Tokyo"}`,
								},
							},
						},
					},
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     20,
				"completion_tokens": 15,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := NewOpenAIProvider("sk-test", srv.URL)
	req := ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "weather?"}},
		Tools: []ToolDefinition{
			{Type: "function", Function: FunctionSpec{Name: "get_weather"}},
		},
	}

	resp, err := p.ChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls length: got %d, want 1", len(resp.Message.ToolCalls))
	}
	tc := resp.Message.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("tool_call id: got %q, want %q", tc.ID, "call_abc")
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("tool_call function name: got %q, want %q", tc.Function.Name, "get_weather")
	}
}

func TestOpenAIProvider_ChatCompletion_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Rate limit exceeded",
				"type":    "rate_limit_error",
			},
		})
	}))
	defer srv.Close()

	p := NewOpenAIProvider("sk-test", srv.URL)
	req := ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}

	_, err := p.ChatCompletion(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
	if got := err.Error(); !contains(got, "429") || !contains(got, "Rate limit exceeded") {
		t.Errorf("error message: got %q, want to contain 429 and rate limit message", got)
	}
}

func TestOpenAIProvider_ChatCompletion_NonJSONError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal server error")
	}))
	defer srv.Close()

	p := NewOpenAIProvider("sk-test", srv.URL)
	req := ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}

	_, err := p.ChatCompletion(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if got := err.Error(); !contains(got, "500") {
		t.Errorf("error message: got %q, want to contain 500", got)
	}
}

func TestOpenAIProvider_ChatCompletion_NoAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("expected no Authorization header when apiKey is empty")
		}
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"role": "assistant", "content": "ok"}},
			},
			"usage": map[string]interface{}{"prompt_tokens": 1, "completion_tokens": 1},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := NewOpenAIProvider("", srv.URL)
	req := ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}

	_, err := p.ChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- StreamChatCompletion ---

func TestOpenAIProvider_StreamChatCompletion_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody openAIRequest
		json.NewDecoder(r.Body).Decode(&reqBody)
		if !reqBody.Stream {
			t.Error("request stream: got false, want true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := NewOpenAIProvider("sk-test", srv.URL)
	req := ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}

	ch, err := p.StreamChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) != 3 {
		t.Fatalf("events count: got %d, want 3", len(events))
	}
	if events[0].Type != "content_delta" || events[0].Delta != "Hello" {
		t.Errorf("event[0]: got %+v", events[0])
	}
	if events[1].Type != "content_delta" || events[1].Delta != " world" {
		t.Errorf("event[1]: got %+v", events[1])
	}
	if events[2].Type != "done" {
		t.Errorf("event[2]: got %+v, want done", events[2])
	}
}

func TestOpenAIProvider_StreamChatCompletion_ToolCallDelta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_123","function":{"name":"fn","arguments":"{"}}]}}]}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"}}]}}]}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := NewOpenAIProvider("sk-test", srv.URL)
	req := ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "call tool"}},
	}

	ch, err := p.StreamChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) != 3 {
		t.Fatalf("events count: got %d, want 3", len(events))
	}
	if events[0].Type != "tool_call_delta" {
		t.Errorf("event[0].Type: got %q, want tool_call_delta", events[0].Type)
	}
	if events[0].ToolCallID != "call_123" {
		t.Errorf("event[0].ToolCallID: got %q, want call_123", events[0].ToolCallID)
	}
	if events[0].FuncName != "fn" {
		t.Errorf("event[0].FuncName: got %q, want fn", events[0].FuncName)
	}
	if events[0].FuncArgs != "{" {
		t.Errorf("event[0].FuncArgs: got %q, want {", events[0].FuncArgs)
	}
	if events[1].FuncArgs != "}" {
		t.Errorf("event[1].FuncArgs: got %q, want }", events[1].FuncArgs)
	}
	if events[2].Type != "done" {
		t.Errorf("event[2].Type: got %q, want done", events[2].Type)
	}
}

func TestOpenAIProvider_StreamChatCompletion_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Invalid API key",
				"type":    "authentication_error",
			},
		})
	}))
	defer srv.Close()

	p := NewOpenAIProvider("bad-key", srv.URL)
	req := ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}

	_, err := p.StreamChatCompletion(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if got := err.Error(); !contains(got, "401") || !contains(got, "Invalid API key") {
		t.Errorf("error: got %q", got)
	}
}

func TestOpenAIProvider_StreamChatCompletion_EmptyLines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		// Include empty lines and comments that should be ignored.
		fmt.Fprint(w, "\n")
		fmt.Fprint(w, ": keep-alive\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := NewOpenAIProvider("sk-test", srv.URL)
	req := ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}

	ch, err := p.StreamChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) != 2 {
		t.Fatalf("events count: got %d, want 2", len(events))
	}
	if events[0].Delta != "ok" {
		t.Errorf("event[0].Delta: got %q, want ok", events[0].Delta)
	}
}

// contains is a test helper for substring matching.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
