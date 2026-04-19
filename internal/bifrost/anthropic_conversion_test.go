package bifrost

import (
	"encoding/json"
	"testing"

	"pgregory.net/rapid"
)

// ---------- Generators ----------

// genAnthropicNonEmptyString generates a non-empty alphanumeric string for Anthropic tests.
func genAnthropicNonEmptyString() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9_]{0,19}`)
}

// genToolDefinition generates a valid ToolDefinition with non-empty name, description, and parameters.
func genToolDefinition() *rapid.Generator[ToolDefinition] {
	return rapid.Custom(func(t *rapid.T) ToolDefinition {
		name := genAnthropicNonEmptyString().Draw(t, "name")
		desc := genAnthropicNonEmptyString().Draw(t, "description")

		// Generate a simple JSON-schema-like parameters map.
		numProps := rapid.IntRange(0, 5).Draw(t, "numProps")
		props := make(map[string]interface{})
		for i := 0; i < numProps; i++ {
			propName := genAnthropicNonEmptyString().Draw(t, "propName")
			props[propName] = map[string]interface{}{"type": "string"}
		}
		params := map[string]interface{}{
			"type":       "object",
			"properties": props,
		}

		return ToolDefinition{
			Type: "function",
			Function: FunctionSpec{
				Name:        name,
				Description: desc,
				Parameters:  params,
			},
		}
	})
}

// genToolUseContentBlock generates an anthropicContentBlock of type "tool_use".
func genToolUseContentBlock() *rapid.Generator[anthropicContentBlock] {
	return rapid.Custom(func(t *rapid.T) anthropicContentBlock {
		id := genAnthropicNonEmptyString().Draw(t, "id")
		name := genAnthropicNonEmptyString().Draw(t, "name")

		// Generate a simple JSON input object.
		numFields := rapid.IntRange(0, 3).Draw(t, "numFields")
		inputMap := make(map[string]interface{})
		for i := 0; i < numFields; i++ {
			key := genAnthropicNonEmptyString().Draw(t, "key")
			val := genAnthropicNonEmptyString().Draw(t, "val")
			inputMap[key] = val
		}
		inputJSON, _ := json.Marshal(inputMap)

		return anthropicContentBlock{
			Type:  "tool_use",
			ID:    id,
			Name:  name,
			Input: json.RawMessage(inputJSON),
		}
	})
}

// genToolMessage generates a unified Message with Role="tool" and a non-empty ToolCallID.
func genToolMessage() *rapid.Generator[Message] {
	return rapid.Custom(func(t *rapid.T) Message {
		toolCallID := genAnthropicNonEmptyString().Draw(t, "toolCallID")
		content := rapid.String().Draw(t, "content")
		return Message{
			Role:       "tool",
			Content:    content,
			ToolCallID: toolCallID,
		}
	})
}

// genAnthropicMessage generates an anthropicMessage with a random role and content.
func genAnthropicMessage() *rapid.Generator[anthropicMessage] {
	return rapid.Custom(func(t *rapid.T) anthropicMessage {
		role := rapid.SampledFrom([]string{"user", "assistant"}).Draw(t, "role")
		text := rapid.String().Draw(t, "text")
		return anthropicMessage{
			Role:    role,
			Content: []interface{}{anthropicTextBlock{Type: "text", Text: text}},
		}
	})
}

// genAnthropicMessageSlice generates a non-empty slice of anthropicMessages.
func genAnthropicMessageSlice() *rapid.Generator[[]anthropicMessage] {
	return rapid.Custom(func(t *rapid.T) []anthropicMessage {
		n := rapid.IntRange(1, 20).Draw(t, "len")
		msgs := make([]anthropicMessage, n)
		for i := 0; i < n; i++ {
			msgs[i] = genAnthropicMessage().Draw(t, "msg")
		}
		return msgs
	})
}

// genChatRequestWithSystemMessages generates a ChatRequest that contains at least one system message.
func genChatRequestWithSystemMessages() *rapid.Generator[ChatRequest] {
	return rapid.Custom(func(t *rapid.T) ChatRequest {
		numSystem := rapid.IntRange(1, 5).Draw(t, "numSystem")
		numUser := rapid.IntRange(1, 5).Draw(t, "numUser")

		var messages []Message

		// Add system messages.
		for i := 0; i < numSystem; i++ {
			text := genAnthropicNonEmptyString().Draw(t, "sysText")
			messages = append(messages, Message{Role: "system", Content: text})
		}

		// Add user/assistant messages.
		for i := 0; i < numUser; i++ {
			role := rapid.SampledFrom([]string{"user", "assistant"}).Draw(t, "role")
			text := genAnthropicNonEmptyString().Draw(t, "msgText")
			messages = append(messages, Message{Role: role, Content: text})
		}

		return ChatRequest{
			Model:     "claude-sonnet-4-20250514",
			Messages:  messages,
			MaxTokens: 1024,
		}
	})
}

// ---------- Property Tests ----------

// Feature: bifrost-provider-capabilities, Property 7: Anthropic tool definition conversion
// Validates: Requirements 4.1
func TestProperty7_AnthropicToolDefinitionConversion(t *testing.T) {
	p := NewAnthropicProvider("", "")

	rapid.Check(t, func(t *rapid.T) {
		td := genToolDefinition().Draw(t, "toolDef")

		req := ChatRequest{
			Model:     "claude-sonnet-4-20250514",
			Messages:  []Message{{Role: "user", Content: "test"}},
			Tools:     []ToolDefinition{td},
			MaxTokens: 100,
		}

		aReq := p.convertRequest(req)

		if len(aReq.Tools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(aReq.Tools))
		}

		tool := aReq.Tools[0]

		// Name must match.
		if tool.Name != td.Function.Name {
			t.Fatalf("Name mismatch: got %q, want %q", tool.Name, td.Function.Name)
		}

		// Description must match.
		if tool.Description != td.Function.Description {
			t.Fatalf("Description mismatch: got %q, want %q", tool.Description, td.Function.Description)
		}

		// InputSchema must deeply equal the original parameters.
		originalJSON, _ := json.Marshal(td.Function.Parameters)
		convertedJSON, _ := json.Marshal(tool.InputSchema)
		if string(originalJSON) != string(convertedJSON) {
			t.Fatalf("InputSchema mismatch:\n  got:  %s\n  want: %s", convertedJSON, originalJSON)
		}
	})
}

// Feature: bifrost-provider-capabilities, Property 8: Anthropic tool_use response conversion
// Validates: Requirements 4.2
func TestProperty8_AnthropicToolUseResponseConversion(t *testing.T) {
	p := NewAnthropicProvider("", "")

	rapid.Check(t, func(t *rapid.T) {
		block := genToolUseContentBlock().Draw(t, "block")

		aResp := anthropicResponse{
			Content: []anthropicContentBlock{block},
			Usage:   anthropicUsage{InputTokens: 10, OutputTokens: 5},
		}

		resp := p.toUnifiedResponse(aResp)

		if len(resp.Message.ToolCalls) != 1 {
			t.Fatalf("expected 1 tool call, got %d", len(resp.Message.ToolCalls))
		}

		tc := resp.Message.ToolCalls[0]

		// ID must match the block's id.
		if tc.ID != block.ID {
			t.Fatalf("ID mismatch: got %q, want %q", tc.ID, block.ID)
		}

		// Function.Name must match the block's name.
		if tc.Function.Name != block.Name {
			t.Fatalf("Function.Name mismatch: got %q, want %q", tc.Function.Name, block.Name)
		}

		// Function.Arguments must be the JSON serialization of the block's input.
		if tc.Function.Arguments != string(block.Input) {
			t.Fatalf("Function.Arguments mismatch: got %q, want %q", tc.Function.Arguments, string(block.Input))
		}
	})
}

// Feature: bifrost-provider-capabilities, Property 9: Anthropic tool result conversion
// Validates: Requirements 4.3
func TestProperty9_AnthropicToolResultConversion(t *testing.T) {
	p := NewAnthropicProvider("", "")

	rapid.Check(t, func(t *rapid.T) {
		msg := genToolMessage().Draw(t, "toolMsg")

		converted := p.convertMessage(msg)

		if len(converted) != 1 {
			t.Fatalf("expected 1 anthropic message, got %d", len(converted))
		}

		aMsg := converted[0]

		// Tool messages become user-role messages in Anthropic format.
		if aMsg.Role != "user" {
			t.Fatalf("expected role 'user', got %q", aMsg.Role)
		}

		if len(aMsg.Content) != 1 {
			t.Fatalf("expected 1 content block, got %d", len(aMsg.Content))
		}

		// Marshal and unmarshal to check the tool_result block.
		blockJSON, err := json.Marshal(aMsg.Content[0])
		if err != nil {
			t.Fatalf("failed to marshal content block: %v", err)
		}

		var toolResult anthropicToolResultBlock
		if err := json.Unmarshal(blockJSON, &toolResult); err != nil {
			t.Fatalf("failed to unmarshal tool_result block: %v", err)
		}

		if toolResult.Type != "tool_result" {
			t.Fatalf("expected type 'tool_result', got %q", toolResult.Type)
		}

		// ToolUseID must equal the original ToolCallID.
		if toolResult.ToolUseID != msg.ToolCallID {
			t.Fatalf("ToolUseID mismatch: got %q, want %q", toolResult.ToolUseID, msg.ToolCallID)
		}
	})
}

// Feature: bifrost-provider-capabilities, Property 10: Anthropic consecutive role merging
// Validates: Requirements 4.4
func TestProperty10_AnthropicConsecutiveRoleMerging(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		msgs := genAnthropicMessageSlice().Draw(t, "msgs")

		merged := mergeConsecutiveSameRole(msgs)

		// No two adjacent messages should have the same role.
		for i := 1; i < len(merged); i++ {
			if merged[i].Role == merged[i-1].Role {
				t.Fatalf("adjacent messages at index %d and %d have same role %q", i-1, i, merged[i].Role)
			}
		}

		// The merged result should not be longer than the input.
		if len(merged) > len(msgs) {
			t.Fatalf("merged length %d exceeds input length %d", len(merged), len(msgs))
		}

		// Total content blocks should be preserved.
		originalBlocks := 0
		for _, m := range msgs {
			originalBlocks += len(m.Content)
		}
		mergedBlocks := 0
		for _, m := range merged {
			mergedBlocks += len(m.Content)
		}
		if mergedBlocks != originalBlocks {
			t.Fatalf("content block count mismatch: merged=%d, original=%d", mergedBlocks, originalBlocks)
		}
	})
}

// Feature: bifrost-provider-capabilities, Property 11: Anthropic system message extraction
// Validates: Requirements 4.5
func TestProperty11_AnthropicSystemMessageExtraction(t *testing.T) {
	p := NewAnthropicProvider("", "")

	rapid.Check(t, func(t *rapid.T) {
		req := genChatRequestWithSystemMessages().Draw(t, "req")

		aReq := p.convertRequest(req)

		// The System field must contain all system message text.
		// Collect expected system texts.
		var expectedParts []string
		for _, m := range req.Messages {
			if m.Role == "system" {
				text := contentToString(m.Content)
				if text != "" {
					expectedParts = append(expectedParts, text)
				}
			}
		}

		// System field should be non-empty since we have at least one system message.
		if aReq.System == "" && len(expectedParts) > 0 {
			t.Fatal("System field is empty but system messages were present")
		}

		// Verify each system text part appears in the System field.
		for _, part := range expectedParts {
			if !containsSubstring(aReq.System, part) {
				t.Fatalf("System field %q does not contain expected part %q", aReq.System, part)
			}
		}

		// The Messages array must not contain any system-role messages.
		// Note: Anthropic only has "user" and "assistant" roles.
		for i, m := range aReq.Messages {
			if m.Role == "system" {
				t.Fatalf("Messages[%d] has system role, should have been extracted", i)
			}
		}
	})
}

// containsSubstring checks if s contains substr.
func containsSubstring(s, substr string) bool {
	return len(substr) == 0 || len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
