package bifrost

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"pgregory.net/rapid"
)

// genNonEmptyString generates a non-empty string suitable for reasoning content.
func genNonEmptyString() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-zA-Z0-9 .,!?]{1,200}`)
}

// Feature: bifrost-provider-capabilities, Property 4: Reasoning content stream parsing
// **Validates: Requirements 3.2, 3.3**
func TestProperty4_ReasoningContentStreamParsing(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		reasoningText := genNonEmptyString().Draw(t, "reasoning_content")

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)

			// Build SSE chunk with reasoning_content in the delta.
			chunk := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"delta": map[string]interface{}{
							"reasoning_content": reasoningText,
						},
					},
				},
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			fmt.Fprint(w, "data: [DONE]\n\n")
			flusher.Flush()
		}))
		defer srv.Close()

		p := NewOpenAIProvider("sk-test", srv.URL)
		req := ChatRequest{
			Model:    "gpt-4o",
			Messages: []Message{{Role: "user", Content: "think"}},
		}

		ch, err := p.StreamChatCompletion(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var events []StreamEvent
		for ev := range ch {
			events = append(events, ev)
		}

		// Must have at least 2 events: reasoning_delta + done.
		if len(events) < 2 {
			t.Fatalf("expected at least 2 events, got %d", len(events))
		}

		// First event must be reasoning_delta with the correct content.
		if events[0].Type != "reasoning_delta" {
			t.Fatalf("expected first event type reasoning_delta, got %q", events[0].Type)
		}
		if events[0].ReasoningContent != reasoningText {
			t.Fatalf("reasoning content mismatch: got %q, want %q", events[0].ReasoningContent, reasoningText)
		}

		// Last event must be done.
		if events[len(events)-1].Type != "done" {
			t.Fatalf("expected last event type done, got %q", events[len(events)-1].Type)
		}
	})
}

// TestProperty4_EmptyReasoningContentSkipped verifies that empty reasoning_content
// strings do not produce reasoning_delta events (Requirement 3.3).
func TestProperty4_EmptyReasoningContentSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		// Send a chunk with empty reasoning_content — should be skipped.
		chunk := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"delta": map[string]interface{}{
						"reasoning_content": "",
						"content":           "hello",
					},
				},
			},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
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

	// Should have content_delta + done, no reasoning_delta.
	for _, ev := range events {
		if ev.Type == "reasoning_delta" {
			t.Fatalf("unexpected reasoning_delta event for empty reasoning_content")
		}
	}
}

// Feature: bifrost-provider-capabilities, Property 6: Non-streaming reasoning content preservation
// **Validates: Requirements 3.6**
func TestProperty6_NonStreamingReasoningContentPreservation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		reasoningText := rapid.String().Draw(t, "reasoning_content")

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":              "assistant",
							"content":           "answer",
							"reasoning_content": reasoningText,
						},
					},
				},
				"usage": map[string]interface{}{
					"prompt_tokens":     10,
					"completion_tokens": 5,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer srv.Close()

		p := NewOpenAIProvider("sk-test", srv.URL)
		req := ChatRequest{
			Model:    "gpt-4o",
			Messages: []Message{{Role: "user", Content: "think"}},
		}

		resp, err := p.ChatCompletion(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.ReasoningContent != reasoningText {
			t.Fatalf("ReasoningContent mismatch: got %q, want %q", resp.ReasoningContent, reasoningText)
		}
	})
}
