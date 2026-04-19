package bifrost

import (
	"encoding/json"
	"testing"
)

func TestChatRequestValidate_EmptyModel(t *testing.T) {
	req := ChatRequest{
		Model:    "",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for empty model")
	}
}

func TestChatRequestValidate_NoMessages(t *testing.T) {
	req := ChatRequest{
		Model:    "gpt-4o",
		Messages: nil,
	}
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for empty messages")
	}
}

func TestChatRequestValidate_InvalidRole(t *testing.T) {
	req := ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "invalid", Content: "hi"}},
	}
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for invalid role")
	}
}

func TestChatRequestValidate_ValidRoles(t *testing.T) {
	for _, role := range []string{"system", "user", "assistant", "tool"} {
		req := ChatRequest{
			Model:    "gpt-4o",
			Messages: []Message{{Role: role, Content: "hi"}},
		}
		if err := req.Validate(); err != nil {
			t.Errorf("unexpected error for role %q: %v", role, err)
		}
	}
}

func TestChatRequestValidate_OK(t *testing.T) {
	req := ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
		},
		MaxTokens:   1024,
		Temperature: 0.7,
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMessageValidate(t *testing.T) {
	valid := Message{Role: "user", Content: "test"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invalid := Message{Role: "moderator", Content: "test"}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected error for invalid role")
	}
}

func TestChatRequestJSON(t *testing.T) {
	req := ChatRequest{
		Model: "gpt-4o",
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
		Tools: []ToolDefinition{
			{
				Type: "function",
				Function: FunctionSpec{
					Name:        "get_weather",
					Description: "Get weather for a location",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"location": map[string]interface{}{
								"type": "string",
							},
						},
					},
				},
			},
		},
		MaxTokens:   512,
		Temperature: 0.5,
		Stream:      true,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded ChatRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Model != req.Model {
		t.Errorf("Model: got %q, want %q", decoded.Model, req.Model)
	}
	if len(decoded.Messages) != 1 {
		t.Fatalf("Messages length: got %d, want 1", len(decoded.Messages))
	}
	if decoded.Messages[0].Role != "user" {
		t.Errorf("Messages[0].Role: got %q, want %q", decoded.Messages[0].Role, "user")
	}
	if len(decoded.Tools) != 1 {
		t.Fatalf("Tools length: got %d, want 1", len(decoded.Tools))
	}
	if decoded.Tools[0].Function.Name != "get_weather" {
		t.Errorf("Tools[0].Function.Name: got %q, want %q", decoded.Tools[0].Function.Name, "get_weather")
	}
	if decoded.MaxTokens != 512 {
		t.Errorf("MaxTokens: got %d, want 512", decoded.MaxTokens)
	}
	if !decoded.Stream {
		t.Error("Stream: got false, want true")
	}
}

func TestChatResponseJSON(t *testing.T) {
	resp := ChatResponse{
		Message: Message{
			Role:    "assistant",
			Content: "Hello!",
			ToolCalls: []ToolCall{
				{
					ID:   "call_123",
					Type: "function",
					Function: FunctionCall{
						Name:      "get_weather",
						Arguments: `{"location":"Tokyo"}`,
					},
				},
			},
		},
		Usage: Usage{
			PromptTokens:     100,
			CompletionTokens: 50,
			CachedTokens:     20,
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded ChatResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Message.Role != "assistant" {
		t.Errorf("Message.Role: got %q, want %q", decoded.Message.Role, "assistant")
	}
	if len(decoded.Message.ToolCalls) != 1 {
		t.Fatalf("ToolCalls length: got %d, want 1", len(decoded.Message.ToolCalls))
	}
	tc := decoded.Message.ToolCalls[0]
	if tc.ID != "call_123" || tc.Function.Name != "get_weather" {
		t.Errorf("ToolCall: got ID=%q Name=%q", tc.ID, tc.Function.Name)
	}
	if decoded.Usage.PromptTokens != 100 || decoded.Usage.CompletionTokens != 50 || decoded.Usage.CachedTokens != 20 {
		t.Errorf("Usage: got %+v", decoded.Usage)
	}
}

func TestStreamEventJSON(t *testing.T) {
	event := StreamEvent{
		Type:       "tool_call_delta",
		Delta:      "",
		ToolCallID: "call_456",
		ToolIndex:  0,
		FuncName:   "read_file",
		FuncArgs:   `{"path":"/etc/hosts"}`,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded StreamEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Type != "tool_call_delta" {
		t.Errorf("Type: got %q, want %q", decoded.Type, "tool_call_delta")
	}
	if decoded.ToolCallID != "call_456" {
		t.Errorf("ToolCallID: got %q, want %q", decoded.ToolCallID, "call_456")
	}
	if decoded.FuncName != "read_file" {
		t.Errorf("FuncName: got %q, want %q", decoded.FuncName, "read_file")
	}
}

func TestContentBlockJSON(t *testing.T) {
	blocks := []ContentBlock{
		{Type: "text", Text: "Describe this image"},
		{Type: "image_url", ImageURL: &ContentImageURL{URL: "https://example.com/img.png", Detail: "high"}},
	}

	data, err := json.Marshal(blocks)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded []ContentBlock
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(decoded) != 2 {
		t.Fatalf("length: got %d, want 2", len(decoded))
	}
	if decoded[0].Type != "text" || decoded[0].Text != "Describe this image" {
		t.Errorf("block[0]: got %+v", decoded[0])
	}
	if decoded[1].ImageURL == nil || decoded[1].ImageURL.URL != "https://example.com/img.png" {
		t.Errorf("block[1]: got %+v", decoded[1])
	}
}

func TestMessageWithToolCallID(t *testing.T) {
	msg := Message{
		Role:       "tool",
		Content:    "result data",
		ToolCallID: "call_789",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Role != "tool" {
		t.Errorf("Role: got %q, want %q", decoded.Role, "tool")
	}
	if decoded.ToolCallID != "call_789" {
		t.Errorf("ToolCallID: got %q, want %q", decoded.ToolCallID, "call_789")
	}
}
