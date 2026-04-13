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
	defaultOpenAIBaseURL = "https://api.openai.com/v1"
	defaultTimeout       = 30 * time.Second
)

// OpenAIProvider implements Provider for OpenAI-compatible APIs.
// It works with any service that exposes the OpenAI chat completions
// endpoint (vLLM, DeepSeek, Moonshot, etc.) by setting a custom baseURL.
type OpenAIProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewOpenAIProvider creates an OpenAIProvider. If baseURL is empty the
// official OpenAI endpoint is used.
func NewOpenAIProvider(apiKey, baseURL string) *OpenAIProvider {
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	// Strip trailing slash for consistent URL joining.
	baseURL = strings.TrimRight(baseURL, "/")

	return &OpenAIProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{Timeout: defaultTimeout},
	}
}

// Name returns the provider identifier.
func (p *OpenAIProvider) Name() string { return "openai" }

// SupportsToolCalling indicates that OpenAI supports function/tool calling.
func (p *OpenAIProvider) SupportsToolCalling() bool { return true }

// openAIRequest is the JSON body sent to the OpenAI chat completions endpoint.
type openAIRequest struct {
	Model       string           `json:"model"`
	Messages    []Message        `json:"messages"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	Stream      bool             `json:"stream,omitempty"`
}

// openAIResponse is the JSON body returned by the OpenAI chat completions endpoint.
type openAIResponse struct {
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
}

type openAIChoice struct {
	Message openAIMessage `json:"message"`
}

type openAIMessage struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	CachedTokens     int `json:"cached_tokens,omitempty"`
}

// openAIErrorResponse represents an error payload from the API.
type openAIErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// openAIStreamChunk is a single SSE data payload during streaming.
type openAIStreamChunk struct {
	Choices []openAIStreamChoice `json:"choices"`
}

type openAIStreamChoice struct {
	Delta openAIStreamDelta `json:"delta"`
}

type openAIStreamDelta struct {
	Content   string                   `json:"content,omitempty"`
	ToolCalls []openAIStreamToolCall   `json:"tool_calls,omitempty"`
}

// openAIStreamToolCall is the tool_call object inside a streaming delta.
// It includes an index field that the non-streaming ToolCall does not have.
type openAIStreamToolCall struct {
	Index    int          `json:"index"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}

// ChatCompletion performs a non-streaming chat completion request.
func (p *OpenAIProvider) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	oaiReq := openAIRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		Tools:       req.Tools,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      false,
	}

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("bifrost/openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("bifrost/openai: create request: %w", err)
	}
	p.setHeaders(httpReq)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("bifrost/openai: do request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, p.parseErrorResponse(httpResp)
	}

	var oaiResp openAIResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("bifrost/openai: decode response: %w", err)
	}

	return p.toChatResponse(oaiResp), nil
}

// StreamChatCompletion performs a streaming chat completion request and
// returns a channel of StreamEvent values. The channel is closed when the
// stream ends or an error occurs.
func (p *OpenAIProvider) StreamChatCompletion(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	oaiReq := openAIRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		Tools:       req.Tools,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      true,
	}

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("bifrost/openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("bifrost/openai: create request: %w", err)
	}
	p.setHeaders(httpReq)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("bifrost/openai: do request: %w", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		defer httpResp.Body.Close()
		return nil, p.parseErrorResponse(httpResp)
	}

	ch := make(chan StreamEvent, 16)
	go p.readSSEStream(ctx, httpResp.Body, ch)
	return ch, nil
}

// setHeaders applies common headers to an outgoing HTTP request.
func (p *OpenAIProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
}

// parseErrorResponse reads a non-2xx response body and returns a descriptive error.
func (p *OpenAIProvider) parseErrorResponse(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	var errResp openAIErrorResponse
	if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
		return fmt.Errorf("bifrost/openai: API error %d: %s (%s)", resp.StatusCode, errResp.Error.Message, errResp.Error.Type)
	}
	return fmt.Errorf("bifrost/openai: API error %d: %s", resp.StatusCode, string(body))
}

// toChatResponse converts the OpenAI API response to the unified ChatResponse.
func (p *OpenAIProvider) toChatResponse(oai openAIResponse) *ChatResponse {
	resp := &ChatResponse{
		Usage: Usage{
			PromptTokens:     oai.Usage.PromptTokens,
			CompletionTokens: oai.Usage.CompletionTokens,
			CachedTokens:     oai.Usage.CachedTokens,
		},
	}
	if len(oai.Choices) > 0 {
		c := oai.Choices[0]
		resp.Message = Message{
			Role:      c.Message.Role,
			Content:   c.Message.Content,
			ToolCalls: c.Message.ToolCalls,
		}
	}
	return resp
}

// readSSEStream reads the SSE stream from the response body, converts each
// chunk to StreamEvent values, and sends them on ch. The channel is closed
// when the stream ends.
func (p *OpenAIProvider) readSSEStream(ctx context.Context, body io.ReadCloser, ch chan<- StreamEvent) {
	defer close(ch)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()

		// SSE lines that don't start with "data: " are ignored (comments, empty keep-alives).
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		// The [DONE] marker signals end of stream.
		if data == "[DONE]" {
			select {
			case ch <- StreamEvent{Type: "done"}:
			case <-ctx.Done():
			}
			return
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			select {
			case ch <- StreamEvent{Type: "error", Delta: fmt.Sprintf("parse error: %v", err)}:
			case <-ctx.Done():
			}
			return
		}

		for _, choice := range chunk.Choices {
			// Content delta.
			if choice.Delta.Content != "" {
				select {
				case ch <- StreamEvent{Type: "content_delta", Delta: choice.Delta.Content}:
				case <-ctx.Done():
					return
				}
			}
			// Tool call deltas.
			for _, tc := range choice.Delta.ToolCalls {
				select {
				case ch <- StreamEvent{
					Type:       "tool_call_delta",
					ToolCallID: tc.ID,
					ToolIndex:  tc.Index,
					FuncName:   tc.Function.Name,
					FuncArgs:   tc.Function.Arguments,
				}:

				case <-ctx.Done():
					return
				}
			}
		}
	}
}
