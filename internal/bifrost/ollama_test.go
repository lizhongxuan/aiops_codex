package bifrost

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- NewOllamaProvider ---

func TestNewOllamaProvider_DefaultBaseURL(t *testing.T) {
	p := NewOllamaProvider("")
	if p.openai.baseURL != defaultOllamaBaseURL {
		t.Errorf("baseURL: got %q, want %q", p.openai.baseURL, defaultOllamaBaseURL)
	}
	if p.openai.apiKey != "" {
		t.Errorf("apiKey: got %q, want empty (no key for local Ollama)", p.openai.apiKey)
	}
}

func TestNewOllamaProvider_CustomBaseURL(t *testing.T) {
	p := NewOllamaProvider("http://remote-ollama:11434/v1/")
	if p.openai.baseURL != "http://remote-ollama:11434/v1" {
		t.Errorf("baseURL: got %q, want %q (trailing slash should be stripped)", p.openai.baseURL, "http://remote-ollama:11434/v1")
	}
}

func TestOllamaProvider_Name(t *testing.T) {
	p := NewOllamaProvider("")
	if p.Name() != "ollama" {
		t.Errorf("Name: got %q, want %q", p.Name(), "ollama")
	}
}

func TestOllamaProvider_SupportsToolCalling(t *testing.T) {
	p := NewOllamaProvider("")
	if !p.SupportsToolCalling() {
		t.Error("SupportsToolCalling: got false, want true")
	}
}

// --- ChatCompletion delegates to OpenAI-compatible endpoint ---

func TestOllamaProvider_ChatCompletion_Delegates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: got %q, want POST", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path: got %q, want /chat/completions", r.URL.Path)
		}
		// Ollama needs no Authorization header.
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("Authorization: got %q, want empty", auth)
		}

		var reqBody openAIRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if reqBody.Model != "llama3" {
			t.Errorf("request model: got %q, want %q", reqBody.Model, "llama3")
		}

		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"role": "assistant", "content": "Hi from Ollama!"}},
			},
			"usage": map[string]interface{}{"prompt_tokens": 5, "completion_tokens": 3},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := NewOllamaProvider(srv.URL)
	req := ChatRequest{
		Model:    "llama3",
		Messages: []Message{{Role: "user", Content: "hello"}},
	}

	resp, err := p.ChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Role != "assistant" {
		t.Errorf("role: got %q, want %q", resp.Message.Role, "assistant")
	}
	if resp.Message.Content != "Hi from Ollama!" {
		t.Errorf("content: got %v, want %q", resp.Message.Content, "Hi from Ollama!")
	}
	if resp.Usage.PromptTokens != 5 {
		t.Errorf("prompt_tokens: got %d, want 5", resp.Usage.PromptTokens)
	}
}

// --- StreamChatCompletion delegates to OpenAI-compatible endpoint ---

func TestOllamaProvider_StreamChatCompletion_Delegates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path: got %q, want /chat/completions", r.URL.Path)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Ollama\"}}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\" streaming\"}}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := NewOllamaProvider(srv.URL)
	req := ChatRequest{
		Model:    "llama3",
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
	if events[0].Type != "content_delta" || events[0].Delta != "Ollama" {
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

func TestOllamaProvider_ImplementsProvider(t *testing.T) {
	var _ Provider = (*OllamaProvider)(nil)
}
