package bifrost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------- Constructor & metadata ----------

func TestNewAnthropicProvider_Defaults(t *testing.T) {
	p := NewAnthropicProvider("sk-test", "")
	if p.baseURL != defaultAnthropicBaseURL {
		t.Errorf("expected default base URL %q, got %q", defaultAnthropicBaseURL, p.baseURL)
	}
	if p.apiKey != "sk-test" {
		t.Errorf("expected apiKey %q, got %q", "sk-test", p.apiKey)
	}
}

func TestNewAnthropicProvider_CustomBaseURL(t *testing.T) {
	p := NewAnthropicProvider("key", "https://custom.api.com/")
	if p.baseURL != "https://custom.api.com" {
		t.Errorf("expected trailing slash stripped, got %q", p.baseURL)
	}
}

func TestAnthropicProvider_Name(t *testing.T) {
	p := NewAnthropicProvider("", "")
	if p.Name() != "anthropic" {
		t.Errorf("expected name %q, got %q", "anthropic", p.Name())
	}
}

func TestAnthropicProvider_SupportsToolCalling(t *testing.T) {
	p := NewAnthropicProvider("", "")
	if !p.SupportsToolCalling() {
		t.Error("expected SupportsToolCalling to return true")
	}
}

// ---------- Request conversion tests ----------

func TestConvertRequest_SystemExtraction(t *testing.T) {
	p := NewAnthropicProvider("", "")
	req := ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "system", Content: "Be concise."},
			{Role: "user", Content: "Hello"},
		},
		MaxTokens: 100,
	}
	aReq := p.convertRequest(req)

	if aReq.System != "You are helpful.\n\nBe concise." {
		t.Errorf("system not concatenated correctly: %q", aReq.System)
	}
	// System messages should not appear in the messages array.
	for _, m := range aReq.Messages {
		if m.Role == "system" {
			t.Error("system message should not appear in messages array")
		}
	}
}

func TestConvertRequest_NoSystemMessage(t *testing.T) {
	p := NewAnthropicProvider("", "")
	req := ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
		MaxTokens: 100,
	}
	aReq := p.convertRequest(req)
	if aReq.System != "" {
		t.Errorf("expected empty system, got %q", aReq.System)
	}
}

func TestConvertRequest_ToolCallsConversion(t *testing.T) {
	p := NewAnthropicProvider("", "")
	req := ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "user", Content: "Check CPU"},
			{
				Role:    "assistant",
				Content: "Let me check.",
				ToolCalls: []ToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: FunctionCall{
							Name:      "host_summary",
							Arguments: `{"host":"server1"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				Content:    "CPU: 45%",
				ToolCallID: "call_1",
			},
			{Role: "assistant", Content: "CPU is at 45%."},
		},
		MaxTokens: 100,
	}
	aReq := p.convertRequest(req)

	// Should have 4 messages: user, assistant(text+tool_use), user(tool_result), assistant(text)
	if len(aReq.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(aReq.Messages))
	}

	// Check assistant message has both text and tool_use blocks.
	assistantMsg := aReq.Messages[1]
	if assistantMsg.Role != "assistant" {
		t.Errorf("expected assistant role, got %q", assistantMsg.Role)
	}
	if len(assistantMsg.Content) != 2 {
		t.Fatalf("expected 2 content blocks in assistant msg, got %d", len(assistantMsg.Content))
	}

	// Verify tool_use block by marshaling and checking.
	blockJSON, _ := json.Marshal(assistantMsg.Content[1])
	var toolUse anthropicToolUseBlock
	json.Unmarshal(blockJSON, &toolUse)
	if toolUse.Type != "tool_use" || toolUse.ID != "call_1" || toolUse.Name != "host_summary" {
		t.Errorf("unexpected tool_use block: %+v", toolUse)
	}

	// Check tool_result is wrapped in a user message.
	toolResultMsg := aReq.Messages[2]
	if toolResultMsg.Role != "user" {
		t.Errorf("tool_result should be in a user message, got role %q", toolResultMsg.Role)
	}
}

func TestConvertRequest_ConsecutiveSameRoleMerging(t *testing.T) {
	p := NewAnthropicProvider("", "")
	req := ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
			{Role: "user", Content: "Are you there?"},
			{Role: "assistant", Content: "Yes"},
			{Role: "assistant", Content: "I'm here"},
		},
		MaxTokens: 100,
	}
	aReq := p.convertRequest(req)

	// After merging, should have 2 messages: user, assistant.
	if len(aReq.Messages) != 2 {
		t.Fatalf("expected 2 messages after merging, got %d", len(aReq.Messages))
	}
	if aReq.Messages[0].Role != "user" {
		t.Errorf("first message should be user, got %q", aReq.Messages[0].Role)
	}
	if aReq.Messages[1].Role != "assistant" {
		t.Errorf("second message should be assistant, got %q", aReq.Messages[1].Role)
	}
	// User message should have 2 content blocks.
	if len(aReq.Messages[0].Content) != 2 {
		t.Errorf("expected 2 content blocks in merged user msg, got %d", len(aReq.Messages[0].Content))
	}
}

func TestConvertRequest_ToolDefinitions(t *testing.T) {
	p := NewAnthropicProvider("", "")
	req := ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
		Tools: []ToolDefinition{
			{
				Type: "function",
				Function: FunctionSpec{
					Name:        "get_weather",
					Description: "Get weather info",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"city": map[string]interface{}{"type": "string"},
						},
					},
				},
			},
		},
		MaxTokens: 100,
	}
	aReq := p.convertRequest(req)

	if len(aReq.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(aReq.Tools))
	}
	tool := aReq.Tools[0]
	if tool.Name != "get_weather" {
		t.Errorf("expected tool name %q, got %q", "get_weather", tool.Name)
	}
	if tool.Description != "Get weather info" {
		t.Errorf("expected tool description %q, got %q", "Get weather info", tool.Description)
	}
	if tool.InputSchema == nil {
		t.Error("expected non-nil input_schema")
	}
}

func TestConvertRequest_DefaultMaxTokens(t *testing.T) {
	p := NewAnthropicProvider("", "")
	req := ChatRequest{
		Model:    "claude-sonnet-4-20250514",
		Messages: []Message{{Role: "user", Content: "Hi"}},
	}
	aReq := p.convertRequest(req)
	if aReq.MaxTokens != 4096 {
		t.Errorf("expected default max_tokens 4096, got %d", aReq.MaxTokens)
	}
}

func TestConvertRequest_EmptyContent(t *testing.T) {
	p := NewAnthropicProvider("", "")
	req := ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "user", Content: ""},
		},
		MaxTokens: 100,
	}
	aReq := p.convertRequest(req)

	// Empty user content should get a space placeholder.
	if len(aReq.Messages) == 0 {
		t.Fatal("expected at least one message")
	}
	blockJSON, _ := json.Marshal(aReq.Messages[0].Content[0])
	var tb anthropicTextBlock
	json.Unmarshal(blockJSON, &tb)
	if tb.Text != " " {
		t.Errorf("expected space placeholder for empty user content, got %q", tb.Text)
	}
}

func TestConvertRequest_AssistantStartsConversation(t *testing.T) {
	p := NewAnthropicProvider("", "")
	req := ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "assistant", Content: "I'll help you."},
		},
		MaxTokens: 100,
	}
	aReq := p.convertRequest(req)

	// Should prepend a user message since Anthropic requires user-first.
	if len(aReq.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(aReq.Messages))
	}
	if aReq.Messages[0].Role != "user" {
		t.Errorf("first message should be user, got %q", aReq.Messages[0].Role)
	}
}

// ---------- Response conversion tests ----------

func TestToUnifiedResponse_TextOnly(t *testing.T) {
	p := NewAnthropicProvider("", "")
	aResp := anthropicResponse{
		Content: []anthropicContentBlock{
			{Type: "text", Text: "Hello there!"},
		},
		Usage: anthropicUsage{InputTokens: 10, OutputTokens: 5},
	}
	resp := p.toUnifiedResponse(aResp)

	if resp.Message.Role != "assistant" {
		t.Errorf("expected role assistant, got %q", resp.Message.Role)
	}
	if resp.Message.Content != "Hello there!" {
		t.Errorf("expected content %q, got %q", "Hello there!", resp.Message.Content)
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("expected prompt_tokens 10, got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 5 {
		t.Errorf("expected completion_tokens 5, got %d", resp.Usage.CompletionTokens)
	}
}

func TestToUnifiedResponse_ToolUse(t *testing.T) {
	p := NewAnthropicProvider("", "")
	aResp := anthropicResponse{
		Content: []anthropicContentBlock{
			{Type: "text", Text: "Let me check."},
			{
				Type:  "tool_use",
				ID:    "toolu_01",
				Name:  "get_weather",
				Input: json.RawMessage(`{"city":"Tokyo"}`),
			},
		},
		Usage: anthropicUsage{InputTokens: 20, OutputTokens: 15},
	}
	resp := p.toUnifiedResponse(aResp)

	if resp.Message.Content != "Let me check." {
		t.Errorf("expected text content %q, got %q", "Let me check.", resp.Message.Content)
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.Message.ToolCalls))
	}
	tc := resp.Message.ToolCalls[0]
	if tc.ID != "toolu_01" {
		t.Errorf("expected tool call ID %q, got %q", "toolu_01", tc.ID)
	}
	if tc.Type != "function" {
		t.Errorf("expected type %q, got %q", "function", tc.Type)
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("expected function name %q, got %q", "get_weather", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"city":"Tokyo"}` {
		t.Errorf("expected arguments %q, got %q", `{"city":"Tokyo"}`, tc.Function.Arguments)
	}
}

func TestToUnifiedResponse_MultipleToolUse(t *testing.T) {
	p := NewAnthropicProvider("", "")
	aResp := anthropicResponse{
		Content: []anthropicContentBlock{
			{Type: "text", Text: "Checking both."},
			{Type: "tool_use", ID: "t1", Name: "tool_a", Input: json.RawMessage(`{"a":1}`)},
			{Type: "tool_use", ID: "t2", Name: "tool_b", Input: json.RawMessage(`{"b":2}`)},
		},
		Usage: anthropicUsage{InputTokens: 30, OutputTokens: 25},
	}
	resp := p.toUnifiedResponse(aResp)

	if len(resp.Message.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(resp.Message.ToolCalls))
	}
	if resp.Message.ToolCalls[0].ID != "t1" || resp.Message.ToolCalls[1].ID != "t2" {
		t.Error("tool call IDs don't match")
	}
}

func TestToUnifiedResponse_EmptyContent(t *testing.T) {
	p := NewAnthropicProvider("", "")
	aResp := anthropicResponse{
		Content: []anthropicContentBlock{},
		Usage:   anthropicUsage{InputTokens: 5, OutputTokens: 0},
	}
	resp := p.toUnifiedResponse(aResp)

	if resp.Message.Content != "" {
		t.Errorf("expected empty content, got %q", resp.Message.Content)
	}
	if len(resp.Message.ToolCalls) != 0 {
		t.Errorf("expected no tool calls, got %d", len(resp.Message.ToolCalls))
	}
}

func TestToUnifiedResponse_MultipleTextBlocks(t *testing.T) {
	p := NewAnthropicProvider("", "")
	aResp := anthropicResponse{
		Content: []anthropicContentBlock{
			{Type: "text", Text: "Part 1. "},
			{Type: "text", Text: "Part 2."},
		},
		Usage: anthropicUsage{InputTokens: 10, OutputTokens: 8},
	}
	resp := p.toUnifiedResponse(aResp)

	if resp.Message.Content != "Part 1. Part 2." {
		t.Errorf("expected concatenated text, got %q", resp.Message.Content)
	}
}

// ---------- ChatCompletion integration test (httptest) ----------

func TestAnthropicChatCompletion_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers.
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("expected x-api-key header, got %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != anthropicAPIVersion {
			t.Errorf("expected anthropic-version header %q", r.Header.Get("anthropic-version"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json")
		}

		// Verify request body.
		body, _ := io.ReadAll(r.Body)
		var aReq anthropicRequest
		if err := json.Unmarshal(body, &aReq); err != nil {
			t.Fatalf("failed to unmarshal request: %v", err)
		}
		if aReq.System != "Be helpful." {
			t.Errorf("expected system %q, got %q", "Be helpful.", aReq.System)
		}
		if aReq.Stream {
			t.Error("expected stream=false for ChatCompletion")
		}

		// Return a response.
		resp := anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "text", Text: "Hi!"},
			},
			Usage: anthropicUsage{InputTokens: 12, OutputTokens: 3},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := NewAnthropicProvider("test-key", server.URL)
	resp, err := p.ChatCompletion(context.Background(), ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "system", Content: "Be helpful."},
			{Role: "user", Content: "Hello"},
		},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Content != "Hi!" {
		t.Errorf("expected content %q, got %q", "Hi!", resp.Message.Content)
	}
	if resp.Usage.PromptTokens != 12 {
		t.Errorf("expected prompt_tokens 12, got %d", resp.Usage.PromptTokens)
	}
}

func TestAnthropicChatCompletion_ToolCallRoundTrip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var aReq anthropicRequest
		json.Unmarshal(body, &aReq)

		// Verify the tool_result is present in the request.
		found := false
		for _, m := range aReq.Messages {
			if m.Role == "user" {
				for _, block := range m.Content {
					blockJSON, _ := json.Marshal(block)
					if strings.Contains(string(blockJSON), "tool_result") {
						found = true
					}
				}
			}
		}
		if !found {
			t.Error("expected tool_result block in request")
		}

		resp := anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "text", Text: "CPU is at 45%."},
			},
			Usage: anthropicUsage{InputTokens: 50, OutputTokens: 10},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := NewAnthropicProvider("key", server.URL)
	resp, err := p.ChatCompletion(context.Background(), ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "user", Content: "Check CPU"},
			{
				Role:    "assistant",
				Content: "Checking.",
				ToolCalls: []ToolCall{{
					ID:       "call_1",
					Type:     "function",
					Function: FunctionCall{Name: "host_summary", Arguments: `{}`},
				}},
			},
			{Role: "tool", Content: "CPU: 45%", ToolCallID: "call_1"},
		},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Content != "CPU is at 45%." {
		t.Errorf("unexpected content: %q", resp.Message.Content)
	}
}

func TestAnthropicChatCompletion_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"type":    "invalid_request_error",
				"message": "max_tokens must be positive",
			},
		})
	}))
	defer server.Close()

	p := NewAnthropicProvider("key", server.URL)
	_, err := p.ChatCompletion(context.Background(), ChatRequest{
		Model:    "claude-sonnet-4-20250514",
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), "max_tokens must be positive") {
		t.Errorf("error should contain API message, got: %v", err)
	}
}

// ---------- Streaming tests ----------

func TestAnthropicStreamChatCompletion_TextOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		events := []string{
			"event: message_start\ndata: {\"message\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n",
			"event: content_block_start\ndata: {\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
			"event: content_block_delta\ndata: {\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n",
			"event: content_block_delta\ndata: {\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n",
			"event: content_block_stop\ndata: {\"index\":0}\n\n",
			"event: message_delta\ndata: {\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n",
			"event: message_stop\ndata: {}\n\n",
		}
		for _, e := range events {
			fmt.Fprint(w, e)
			flusher.Flush()
		}
	}))
	defer server.Close()

	p := NewAnthropicProvider("key", server.URL)
	ch, err := p.StreamChatCompletion(context.Background(), ChatRequest{
		Model:     "claude-sonnet-4-20250514",
		Messages:  []Message{{Role: "user", Content: "Hi"}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Expect: content_delta("Hello"), content_delta(" world"), done
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != "content_delta" || events[0].Delta != "Hello" {
		t.Errorf("event 0: expected content_delta 'Hello', got %+v", events[0])
	}
	if events[1].Type != "content_delta" || events[1].Delta != " world" {
		t.Errorf("event 1: expected content_delta ' world', got %+v", events[1])
	}
	if events[2].Type != "done" {
		t.Errorf("event 2: expected done, got %+v", events[2])
	}
}

func TestAnthropicStreamChatCompletion_ToolUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		events := []string{
			"event: message_start\ndata: {\"message\":{\"usage\":{\"input_tokens\":20,\"output_tokens\":0}}}\n\n",
			// Text block
			"event: content_block_start\ndata: {\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
			"event: content_block_delta\ndata: {\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Let me check.\"}}\n\n",
			"event: content_block_stop\ndata: {\"index\":0}\n\n",
			// Tool use block
			"event: content_block_start\ndata: {\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_01\",\"name\":\"get_weather\"}}\n\n",
			"event: content_block_delta\ndata: {\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"city\\\"\"}}\n\n",
			"event: content_block_delta\ndata: {\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\":\\\"Tokyo\\\"}\"}}\n\n",
			"event: content_block_stop\ndata: {\"index\":1}\n\n",
			"event: message_delta\ndata: {\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":15}}\n\n",
			"event: message_stop\ndata: {}\n\n",
		}
		for _, e := range events {
			fmt.Fprint(w, e)
			flusher.Flush()
		}
	}))
	defer server.Close()

	p := NewAnthropicProvider("key", server.URL)
	ch, err := p.StreamChatCompletion(context.Background(), ChatRequest{
		Model:     "claude-sonnet-4-20250514",
		Messages:  []Message{{Role: "user", Content: "Weather?"}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Expect: content_delta, tool_call_delta(start), tool_call_delta(arg1), tool_call_delta(arg2), done
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d: %+v", len(events), events)
	}

	// First event: text delta
	if events[0].Type != "content_delta" {
		t.Errorf("event 0: expected content_delta, got %q", events[0].Type)
	}

	// Second event: tool_call_delta with ID and name (from content_block_start)
	if events[1].Type != "tool_call_delta" || events[1].ToolCallID != "toolu_01" || events[1].FuncName != "get_weather" {
		t.Errorf("event 1: expected tool_call_delta start, got %+v", events[1])
	}

	// Third and fourth: tool_call_delta with partial JSON args
	if events[2].Type != "tool_call_delta" || events[2].FuncArgs == "" {
		t.Errorf("event 2: expected tool_call_delta with args, got %+v", events[2])
	}
	if events[3].Type != "tool_call_delta" || events[3].FuncArgs == "" {
		t.Errorf("event 3: expected tool_call_delta with args, got %+v", events[3])
	}

	// Last: done
	if events[4].Type != "done" {
		t.Errorf("event 4: expected done, got %+v", events[4])
	}
}

func TestAnthropicStreamChatCompletion_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"type":    "rate_limit_error",
				"message": "Rate limit exceeded",
			},
		})
	}))
	defer server.Close()

	p := NewAnthropicProvider("key", server.URL)
	_, err := p.StreamChatCompletion(context.Background(), ChatRequest{
		Model:     "claude-sonnet-4-20250514",
		Messages:  []Message{{Role: "user", Content: "Hi"}},
		MaxTokens: 100,
	})
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
	if !strings.Contains(err.Error(), "Rate limit exceeded") {
		t.Errorf("error should contain rate limit message, got: %v", err)
	}
}

// ---------- contentToString tests ----------

func TestContentToString_String(t *testing.T) {
	result := contentToString("hello")
	if result != "hello" {
		t.Errorf("expected %q, got %q", "hello", result)
	}
}

func TestContentToString_Nil(t *testing.T) {
	result := contentToString(nil)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestContentToString_ContentBlocks(t *testing.T) {
	blocks := []ContentBlock{
		{Type: "text", Text: "Hello "},
		{Type: "text", Text: "world"},
		{Type: "image_url"},
	}
	result := contentToString(blocks)
	if result != "Hello world" {
		t.Errorf("expected %q, got %q", "Hello world", result)
	}
}

func TestContentToString_MapSlice(t *testing.T) {
	// This simulates JSON-decoded content blocks ([]interface{} of maps).
	blocks := []interface{}{
		map[string]interface{}{"type": "text", "text": "Part A"},
		map[string]interface{}{"type": "text", "text": "Part B"},
	}
	result := contentToString(blocks)
	if result != "Part APart B" {
		t.Errorf("expected %q, got %q", "Part APart B", result)
	}
}

// ---------- mergeConsecutiveSameRole tests ----------

func TestMergeConsecutiveSameRole_Empty(t *testing.T) {
	result := mergeConsecutiveSameRole(nil)
	if len(result) != 0 {
		t.Errorf("expected empty, got %d messages", len(result))
	}
}

func TestMergeConsecutiveSameRole_AlreadyAlternating(t *testing.T) {
	msgs := []anthropicMessage{
		{Role: "user", Content: []interface{}{anthropicTextBlock{Type: "text", Text: "A"}}},
		{Role: "assistant", Content: []interface{}{anthropicTextBlock{Type: "text", Text: "B"}}},
	}
	result := mergeConsecutiveSameRole(msgs)
	if len(result) != 2 {
		t.Errorf("expected 2 messages, got %d", len(result))
	}
}

func TestMergeConsecutiveSameRole_ThreeConsecutive(t *testing.T) {
	msgs := []anthropicMessage{
		{Role: "user", Content: []interface{}{anthropicTextBlock{Type: "text", Text: "A"}}},
		{Role: "user", Content: []interface{}{anthropicTextBlock{Type: "text", Text: "B"}}},
		{Role: "user", Content: []interface{}{anthropicTextBlock{Type: "text", Text: "C"}}},
	}
	result := mergeConsecutiveSameRole(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 merged message, got %d", len(result))
	}
	if len(result[0].Content) != 3 {
		t.Errorf("expected 3 content blocks, got %d", len(result[0].Content))
	}
}
