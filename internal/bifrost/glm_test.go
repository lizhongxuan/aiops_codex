package bifrost

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- NewGLMProvider ---

func TestNewGLMProvider_DefaultBaseURL(t *testing.T) {
	p := NewGLMProvider("sk-test", "")
	if p.openai.baseURL != defaultGLMBaseURL {
		t.Errorf("baseURL: got %q, want %q", p.openai.baseURL, defaultGLMBaseURL)
	}
	if p.openai.apiKey != "sk-test" {
		t.Errorf("apiKey: got %q, want %q", p.openai.apiKey, "sk-test")
	}
}

func TestNewGLMProvider_CustomBaseURL(t *testing.T) {
	p := NewGLMProvider("sk-test", "https://custom.glm.com/v4/")
	if p.openai.baseURL != "https://custom.glm.com/v4" {
		t.Errorf("baseURL: got %q, want %q (trailing slash should be stripped)", p.openai.baseURL, "https://custom.glm.com/v4")
	}
}

func TestGLMProvider_Name(t *testing.T) {
	p := NewGLMProvider("", "")
	if p.Name() != "glm" {
		t.Errorf("Name: got %q, want %q", p.Name(), "glm")
	}
}

func TestGLMProvider_SupportsToolCalling(t *testing.T) {
	p := NewGLMProvider("", "")
	if !p.SupportsToolCalling() {
		t.Error("SupportsToolCalling: got false, want true")
	}
}

func TestGLMProvider_Capabilities(t *testing.T) {
	p := NewGLMProvider("", "")
	caps := p.Capabilities()

	if !caps.SupportsNativeSearch {
		t.Error("SupportsNativeSearch: got false, want true")
	}
	if caps.SupportsReasoningContent {
		t.Error("SupportsReasoningContent: got true, want false")
	}
	if !caps.SupportsStreamingToolCalls {
		t.Error("SupportsStreamingToolCalls: got false, want true")
	}
	if caps.SupportsToolUseFormat {
		t.Error("SupportsToolUseFormat: got true, want false")
	}
	if caps.ToolCallingFormat != "openai_function" {
		t.Errorf("ToolCallingFormat: got %q, want %q", caps.ToolCallingFormat, "openai_function")
	}
}


// --- ChatCompletion delegates to OpenAI-compatible endpoint ---

func TestGLMProvider_ChatCompletion_Delegates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: got %q, want POST", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path: got %q, want /chat/completions", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer sk-glm" {
			t.Errorf("Authorization: got %q, want %q", auth, "Bearer sk-glm")
		}

		var reqBody openAIRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if reqBody.Model != "glm-4" {
			t.Errorf("request model: got %q, want %q", reqBody.Model, "glm-4")
		}

		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"role": "assistant", "content": "Hi from GLM!"}},
			},
			"usage": map[string]interface{}{"prompt_tokens": 5, "completion_tokens": 3},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := NewGLMProvider("sk-glm", srv.URL)
	req := ChatRequest{
		Model:    "glm-4",
		Messages: []Message{{Role: "user", Content: "hello"}},
	}

	resp, err := p.ChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Role != "assistant" {
		t.Errorf("role: got %q, want %q", resp.Message.Role, "assistant")
	}
	if resp.Message.Content != "Hi from GLM!" {
		t.Errorf("content: got %v, want %q", resp.Message.Content, "Hi from GLM!")
	}
	if resp.Usage.PromptTokens != 5 {
		t.Errorf("prompt_tokens: got %d, want 5", resp.Usage.PromptTokens)
	}
}

// --- StreamChatCompletion delegates to OpenAI-compatible endpoint ---

func TestGLMProvider_StreamChatCompletion_Delegates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path: got %q, want /chat/completions", r.URL.Path)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"GLM\"}}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\" streaming\"}}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := NewGLMProvider("sk-glm", srv.URL)
	req := ChatRequest{
		Model:    "glm-4",
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
	if events[0].Type != "content_delta" || events[0].Delta != "GLM" {
		t.Errorf("event[0]: got %+v", events[0])
	}
	if events[1].Type != "content_delta" || events[1].Delta != " streaming" {
		t.Errorf("event[1]: got %+v", events[1])
	}
	if events[2].Type != "done" {
		t.Errorf("event[2]: got %+v, want done", events[2])
	}
}

// --- Provider interface compliance ---

func TestGLMProvider_ImplementsProvider(t *testing.T) {
	var _ Provider = (*GLMProvider)(nil)
}
