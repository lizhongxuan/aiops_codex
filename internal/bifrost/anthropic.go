package bifrost

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultAnthropicBaseURL = "https://api.anthropic.com"
	anthropicAPIVersion     = "2023-06-01"
)

// AnthropicProvider implements Provider for the Anthropic Messages API.
// It converts between the unified OpenAI-style format and Anthropic's
// native message format (system extraction, tool_use/tool_result blocks,
// consecutive same-role merging).
type AnthropicProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewAnthropicProvider creates an AnthropicProvider. If baseURL is empty
// the official Anthropic endpoint is used.
func NewAnthropicProvider(apiKey, baseURL string) *AnthropicProvider {
	if baseURL == "" {
		baseURL = defaultAnthropicBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	return &AnthropicProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// Name returns the provider identifier.
func (p *AnthropicProvider) Name() string { return "anthropic" }

// SupportsToolCalling indicates that Anthropic supports tool calling.
func (p *AnthropicProvider) SupportsToolCalling() bool { return true }

// Capabilities returns the feature set supported by the Anthropic provider.
func (p *AnthropicProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		SupportsNativeSearch:       true,
		SupportsReasoningContent:   false,
		SupportsStreamingToolCalls: true,
		SupportsToolUseFormat:      true,
		ToolCallingFormat:          "anthropic_tool_use",
	}
}

// ---------- Anthropic API types ----------

// anthropicRequest is the JSON body sent to the Anthropic Messages API.
type anthropicRequest struct {
	Model       string              `json:"model"`
	System      string              `json:"system,omitempty"`
	Messages    []anthropicMessage  `json:"messages"`
	Tools       []anthropicTool     `json:"tools,omitempty"`
	MaxTokens   int                 `json:"max_tokens"`
	Temperature float64             `json:"temperature,omitempty"`
	Stream      bool                `json:"stream,omitempty"`
}

// anthropicMessage is a single message in the Anthropic format.
type anthropicMessage struct {
	Role    string        `json:"role"`
	Content []interface{} `json:"content"`
}

// anthropicTextBlock is a text content block.
type anthropicTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// anthropicToolUseBlock is a tool_use content block (assistant requesting a tool call).
type anthropicToolUseBlock struct {
	Type  string      `json:"type"`
	ID    string      `json:"id"`
	Name  string      `json:"name"`
	Input interface{} `json:"input"`
}

// anthropicToolResultBlock is a tool_result content block (user providing tool output).
type anthropicToolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

// anthropicTool is the Anthropic tool definition format.
type anthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// anthropicResponse is the JSON body returned by the Anthropic Messages API.
type anthropicResponse struct {
	Content []anthropicContentBlock `json:"content"`
	Usage   anthropicUsage          `json:"usage"`
	StopReason string               `json:"stop_reason"`
}

// anthropicContentBlock is a single block in the response content array.
type anthropicContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// anthropicErrorResponse represents an error payload from the Anthropic API.
type anthropicErrorResponse struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// ---------- Streaming types ----------

// anthropicSSEEvent represents a parsed SSE event from the Anthropic stream.
type anthropicSSEEvent struct {
	Event string          `json:"-"`
	Data  json.RawMessage `json:"-"`
}

type anthropicMessageStart struct {
	Message struct {
		Usage anthropicUsage `json:"usage"`
	} `json:"message"`
}

type anthropicContentBlockStart struct {
	Index        int                   `json:"index"`
	ContentBlock anthropicContentBlock `json:"content_block"`
}

type anthropicContentBlockDelta struct {
	Index int `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
	} `json:"delta"`
}

type anthropicMessageDelta struct {
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage anthropicUsage `json:"usage"`
}

// ---------- Request conversion (OpenAI → Anthropic) ----------

// convertRequest transforms a unified ChatRequest into an Anthropic API request.
func (p *AnthropicProvider) convertRequest(req ChatRequest) anthropicRequest {
	// Extract system messages and concatenate them.
	var systemParts []string
	var nonSystemMessages []Message
	for _, m := range req.Messages {
		if m.Role == "system" {
			text := contentToString(m.Content)
			if text != "" {
				systemParts = append(systemParts, text)
			}
		} else {
			nonSystemMessages = append(nonSystemMessages, m)
		}
	}

	// Convert each non-system message to Anthropic format.
	var anthropicMsgs []anthropicMessage
	for _, m := range nonSystemMessages {
		converted := p.convertMessage(m)
		anthropicMsgs = append(anthropicMsgs, converted...)
	}

	// Merge consecutive messages with the same role (Anthropic requires alternating).
	anthropicMsgs = mergeConsecutiveSameRole(anthropicMsgs)

	// Ensure messages start with a user message (Anthropic requirement).
	if len(anthropicMsgs) > 0 && anthropicMsgs[0].Role != "user" {
		anthropicMsgs = append([]anthropicMessage{{
			Role:    "user",
			Content: []interface{}{anthropicTextBlock{Type: "text", Text: "Continue."}},
		}}, anthropicMsgs...)
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	aReq := anthropicRequest{
		Model:       req.Model,
		System:      strings.Join(systemParts, "\n\n"),
		Messages:    anthropicMsgs,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
	}

	// Convert tool definitions.
	for _, td := range req.Tools {
		aReq.Tools = append(aReq.Tools, anthropicTool{
			Name:        td.Function.Name,
			Description: td.Function.Description,
			InputSchema: td.Function.Parameters,
		})
	}

	return aReq
}

// convertMessage converts a single unified Message to one or more Anthropic messages.
// An assistant message with tool_calls becomes an assistant message with tool_use blocks.
// A tool message becomes a user message with a tool_result block.
func (p *AnthropicProvider) convertMessage(m Message) []anthropicMessage {
	switch m.Role {
	case "assistant":
		var blocks []interface{}
		text := contentToString(m.Content)
		if text != "" {
			blocks = append(blocks, anthropicTextBlock{Type: "text", Text: text})
		}
		for _, tc := range m.ToolCalls {
			var input interface{}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
				input = map[string]interface{}{}
			}
			blocks = append(blocks, anthropicToolUseBlock{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: input,
			})
		}
		if len(blocks) == 0 {
			blocks = append(blocks, anthropicTextBlock{Type: "text", Text: ""})
		}
		return []anthropicMessage{{Role: "assistant", Content: blocks}}

	case "tool":
		text := contentToString(m.Content)
		return []anthropicMessage{{
			Role: "user",
			Content: []interface{}{anthropicToolResultBlock{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   text,
			}},
		}}

	case "user":
		text := contentToString(m.Content)
		if text == "" {
			text = " "
		}
		return []anthropicMessage{{
			Role:    "user",
			Content: []interface{}{anthropicTextBlock{Type: "text", Text: text}},
		}}

	default:
		// Fallback: treat as user message.
		text := contentToString(m.Content)
		if text == "" {
			text = " "
		}
		return []anthropicMessage{{
			Role:    "user",
			Content: []interface{}{anthropicTextBlock{Type: "text", Text: text}},
		}}
	}
}

// contentToString extracts a plain string from a Message.Content value,
// which may be a string or a []ContentBlock.
func contentToString(content interface{}) string {
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "")
	case []ContentBlock:
		var parts []string
		for _, b := range v {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "")
	default:
		return fmt.Sprintf("%v", v)
	}
}

// mergeConsecutiveSameRole merges consecutive messages with the same role
// by concatenating their content blocks. Anthropic requires strictly
// alternating user/assistant messages.
func mergeConsecutiveSameRole(msgs []anthropicMessage) []anthropicMessage {
	if len(msgs) == 0 {
		return msgs
	}
	merged := []anthropicMessage{msgs[0]}
	for i := 1; i < len(msgs); i++ {
		last := &merged[len(merged)-1]
		if msgs[i].Role == last.Role {
			last.Content = append(last.Content, msgs[i].Content...)
		} else {
			merged = append(merged, msgs[i])
		}
	}
	return merged
}

// ---------- Response conversion (Anthropic → OpenAI) ----------

// toUnifiedResponse converts an Anthropic API response to the unified ChatResponse.
func (p *AnthropicProvider) toUnifiedResponse(aResp anthropicResponse) *ChatResponse {
	resp := &ChatResponse{
		Usage: Usage{
			PromptTokens:     aResp.Usage.InputTokens,
			CompletionTokens: aResp.Usage.OutputTokens,
		},
	}

	resp.Message.Role = "assistant"

	var textParts []string
	for _, block := range aResp.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			argsJSON := "{}"
			if block.Input != nil {
				argsJSON = string(block.Input)
			}
			resp.Message.ToolCalls = append(resp.Message.ToolCalls, ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: FunctionCall{
					Name:      block.Name,
					Arguments: argsJSON,
				},
			})
		}
	}
	resp.Message.Content = strings.Join(textParts, "")

	return resp
}

// ---------- ChatCompletion (non-streaming) ----------

// ChatCompletion performs a non-streaming chat completion request against the Anthropic API.
func (p *AnthropicProvider) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	aReq := p.convertRequest(req)
	aReq.Stream = false

	body, err := json.Marshal(aReq)
	if err != nil {
		return nil, fmt.Errorf("bifrost/anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("bifrost/anthropic: create request: %w", err)
	}
	p.setHeaders(httpReq)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("bifrost/anthropic: do request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, p.parseErrorResponse(httpResp)
	}

	var aResp anthropicResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&aResp); err != nil {
		return nil, fmt.Errorf("bifrost/anthropic: decode response: %w", err)
	}

	return p.toUnifiedResponse(aResp), nil
}

// ---------- StreamChatCompletion (streaming) ----------

// StreamChatCompletion performs a streaming chat completion request and
// returns a channel of StreamEvent values.
func (p *AnthropicProvider) StreamChatCompletion(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	aReq := p.convertRequest(req)
	aReq.Stream = true

	body, err := json.Marshal(aReq)
	if err != nil {
		return nil, fmt.Errorf("bifrost/anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("bifrost/anthropic: create request: %w", err)
	}
	p.setHeaders(httpReq)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("bifrost/anthropic: do request: %w", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		defer httpResp.Body.Close()
		return nil, p.parseErrorResponse(httpResp)
	}

	ch := make(chan StreamEvent, 16)
	go p.readAnthropicStream(ctx, httpResp.Body, ch)
	return ch, nil
}

// ---------- Streaming implementation ----------

// readAnthropicStream reads the Anthropic SSE stream, converts events to
// unified StreamEvent values, and sends them on ch.
func (p *AnthropicProvider) readAnthropicStream(ctx context.Context, body io.ReadCloser, ch chan<- StreamEvent) {
	defer close(ch)
	defer body.Close()

	// Track active tool_use blocks by index for ID/name association.
	type toolBlock struct {
		id   string
		name string
	}
	activeTools := make(map[int]toolBlock)

	scanner := bufio.NewScanner(body)
	var currentEvent string

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		switch currentEvent {
		case "content_block_start":
			var cbs anthropicContentBlockStart
			if err := json.Unmarshal([]byte(data), &cbs); err != nil {
				continue
			}
			if cbs.ContentBlock.Type == "tool_use" {
				activeTools[cbs.Index] = toolBlock{
					id:   cbs.ContentBlock.ID,
					name: cbs.ContentBlock.Name,
				}
				// Emit an initial tool_call_delta with ID and name.
				select {
				case ch <- StreamEvent{
					Type:       "tool_call_delta",
					ToolCallID: cbs.ContentBlock.ID,
					ToolIndex:  cbs.Index,
					FuncName:   cbs.ContentBlock.Name,
				}:
				case <-ctx.Done():
					return
				}
			}

		case "content_block_delta":
			var cbd anthropicContentBlockDelta
			if err := json.Unmarshal([]byte(data), &cbd); err != nil {
				continue
			}
			switch cbd.Delta.Type {
			case "text_delta":
				select {
				case ch <- StreamEvent{Type: "content_delta", Delta: cbd.Delta.Text}:
				case <-ctx.Done():
					return
				}
			case "input_json_delta":
				tb := activeTools[cbd.Index]
				select {
				case ch <- StreamEvent{
					Type:       "tool_call_delta",
					ToolCallID: tb.id,
					ToolIndex:  cbd.Index,
					FuncName:   tb.name,
					FuncArgs:   cbd.Delta.PartialJSON,
				}:
				case <-ctx.Done():
					return
				}
			}

		case "message_stop":
			select {
			case ch <- StreamEvent{Type: "done"}:
			case <-ctx.Done():
			}
			return

		case "message_start", "content_block_stop", "message_delta", "ping":
			// These events don't produce StreamEvents but are valid.
			continue
		}

		currentEvent = ""
	}
}

// ---------- HTTP helpers ----------

// setHeaders applies Anthropic-specific headers to an outgoing HTTP request.
func (p *AnthropicProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)
}

// parseErrorResponse reads a non-2xx response body and returns a descriptive error.
func (p *AnthropicProvider) parseErrorResponse(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	var errResp anthropicErrorResponse
	if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
		return fmt.Errorf("bifrost/anthropic: API error %d: %s (%s)", resp.StatusCode, errResp.Error.Message, errResp.Error.Type)
	}
	return fmt.Errorf("bifrost/anthropic: API error %d: %s", resp.StatusCode, string(body))
}
