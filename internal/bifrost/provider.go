package bifrost

import (
	"context"
	"errors"
	"fmt"
)

// Provider — all LLM vendors implement this interface.
type Provider interface {
	Name() string
	ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	StreamChatCompletion(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
	SupportsToolCalling() bool
}

// ChatRequest is the unified request format sent to any LLM provider.
type ChatRequest struct {
	Model       string           `json:"model"`
	Messages    []Message        `json:"messages"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	Stream      bool             `json:"stream,omitempty"`
}

// Validate checks that the ChatRequest satisfies basic constraints.
func (r ChatRequest) Validate() error {
	if r.Model == "" {
		return errors.New("bifrost: ChatRequest.Model cannot be empty")
	}
	if len(r.Messages) == 0 {
		return errors.New("bifrost: ChatRequest.Messages must have at least one message")
	}
	for i, m := range r.Messages {
		if err := m.Validate(); err != nil {
			return fmt.Errorf("bifrost: Messages[%d]: %w", i, err)
		}
	}
	return nil
}

// ChatResponse is the unified response returned by any LLM provider.
type ChatResponse struct {
	Message Message `json:"message"`
	Usage   Usage   `json:"usage"`
}

// validRoles is the set of allowed message roles.
var validRoles = map[string]bool{
	"system":    true,
	"user":      true,
	"assistant": true,
	"tool":      true,
}

// Message represents a single message in a conversation.
// Content can be a plain string or a slice of ContentBlock for multi-modal input.
type Message struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
}

// Validate checks that the Message has a valid role.
func (m Message) Validate() error {
	if !validRoles[m.Role] {
		return fmt.Errorf("bifrost: Message.Role must be one of system, user, assistant, tool; got %q", m.Role)
	}
	return nil
}

// ToolCall represents a tool invocation requested by the model.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall holds the function name and JSON-encoded arguments for a tool call.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDefinition describes a tool that the model can invoke.
type ToolDefinition struct {
	Type     string       `json:"type"`
	Function FunctionSpec `json:"function"`
}

// FunctionSpec is the schema portion of a ToolDefinition.
type FunctionSpec struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// StreamEvent represents a single event in a streaming LLM response.
type StreamEvent struct {
	Type       string `json:"type"` // content_delta, tool_call_delta, done, error
	Delta      string `json:"delta,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolIndex  int    `json:"tool_index,omitempty"`
	FuncName   string `json:"func_name,omitempty"`
	FuncArgs   string `json:"func_args,omitempty"`
}

// Usage tracks token consumption for a single LLM call.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	CachedTokens     int `json:"cached_tokens,omitempty"`
}

// ContentBlock represents a single block in multi-modal content (text, image, etc.).
type ContentBlock struct {
	Type     string            `json:"type"`
	Text     string            `json:"text,omitempty"`
	ImageURL *ContentImageURL  `json:"image_url,omitempty"`
}

// ContentImageURL holds the URL (or base64 data URI) for an image content block.
type ContentImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}
