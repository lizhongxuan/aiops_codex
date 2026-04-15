package bifrost

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

const (
	defaultOpenAIBaseURL = "https://api.openai.com/v1"

	// connectTimeout is the maximum time to establish a TCP+TLS connection.
	// This does NOT limit the total response time — streaming can run indefinitely.
	connectTimeout = 30 * time.Second

	// nonStreamTimeout is the overall timeout for non-streaming requests.
	nonStreamTimeout = 120 * time.Second

	// streamIdleTimeout is the maximum time to wait between SSE chunks.
	// If no data arrives for this long, the stream is considered dead.
	streamIdleTimeout = 90 * time.Second
)

// OpenAIProvider implements Provider for OpenAI-compatible APIs.
// It works with any service that exposes the OpenAI chat completions
// endpoint (vLLM, DeepSeek, Moonshot, etc.) by setting a custom baseURL.
type OpenAIProvider struct {
	apiKey       string
	baseURL      string
	client       *http.Client // for non-streaming requests (has overall timeout)
	streamClient *http.Client // for streaming requests (no overall timeout, only connect timeout)
}

// NewOpenAIProvider creates an OpenAIProvider. If baseURL is empty the
// official OpenAI endpoint is used.
func NewOpenAIProvider(apiKey, baseURL string) *OpenAIProvider {
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	// Transport with connection-level timeouts only.
	transport := &http.Transport{
		TLSHandshakeTimeout:   connectTimeout,
		ResponseHeaderTimeout: connectTimeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   5,
	}

	return &OpenAIProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		// Non-streaming: overall timeout (connection + read body).
		client: &http.Client{Timeout: nonStreamTimeout, Transport: transport},
		// Streaming: NO overall timeout. Only connection-level timeouts via Transport.
		// The stream can run as long as data keeps flowing. Idle detection is done
		// in readSSEStream via per-read deadlines.
		streamClient: &http.Client{Transport: transport},
	}
}

// Name returns the provider identifier.
func (p *OpenAIProvider) Name() string { return "openai" }

// SupportsToolCalling indicates that OpenAI supports function/tool calling.
func (p *OpenAIProvider) SupportsToolCalling() bool { return true }

// Capabilities returns the feature set supported by the OpenAI provider.
func (p *OpenAIProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		SupportsNativeSearch:       true,
		SupportsReasoningContent:   true,
		SupportsStreamingToolCalls: true,
		SupportsToolUseFormat:      false,
		ToolCallingFormat:          "openai_function",
	}
}

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
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
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
	Content          string                   `json:"content,omitempty"`
	ReasoningContent string                   `json:"reasoning_content,omitempty"`
	ToolCalls        []openAIStreamToolCall   `json:"tool_calls,omitempty"`
}

// openAIStreamToolCall is the tool_call object inside a streaming delta.
// It includes an index field that the non-streaming ToolCall does not have.
type openAIStreamToolCall struct {
	Index    int          `json:"index"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}

// retryWithBackoff retries an HTTP request with exponential backoff and jitter.
// maxRetries=2 means up to 3 total attempts. Only retries on 429 and 5xx.
func retryWithBackoff(ctx context.Context, fn func() (*http.Response, error), maxRetries int) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err := fn()
		if err == nil && resp.StatusCode < 500 && resp.StatusCode != 429 {
			return resp, nil
		}
		if err != nil {
			lastErr = err
			// Network errors are retryable.
		} else {
			// We have a response — check if retryable.
			if resp.StatusCode == 429 || resp.StatusCode >= 500 {
				lastErr = fmt.Errorf("bifrost/openai: HTTP %d", resp.StatusCode)
				resp.Body.Close()
			} else {
				// Non-retryable HTTP status (e.g. 4xx other than 429).
				return resp, nil
			}
		}
		if attempt < maxRetries {
			backoff := time.Duration(200*(1<<uint(attempt))) * time.Millisecond
			jitter := time.Duration(rand.Int63n(int64(backoff) / 5)) // 20% jitter
			select {
			case <-time.After(backoff + jitter):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return nil, fmt.Errorf("bifrost/openai: all %d retries failed: %w", maxRetries+1, lastErr)
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

	httpResp, err := retryWithBackoff(ctx, func() (*http.Response, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("bifrost/openai: create request: %w", err)
		}
		p.setHeaders(httpReq)
		return p.client.Do(httpReq)
	}, 2)
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
//
// When req.UseResponsesAPI is true the provider uses the OpenAI Responses API
// (/v1/responses) which supports native web_search.
func (p *OpenAIProvider) StreamChatCompletion(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	if req.UseResponsesAPI {
		return p.streamResponsesAPI(ctx, req)
	}

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

	log.Printf("[bifrost/openai] StreamChatCompletion: url=%s model=%s msgs=%d tools=%d bodyLen=%d",
		p.baseURL+"/chat/completions", oaiReq.Model, len(oaiReq.Messages), len(oaiReq.Tools), len(body))

	httpResp, err := retryWithBackoff(ctx, func() (*http.Response, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("bifrost/openai: create request: %w", err)
		}
		p.setHeaders(httpReq)
		// Use streamClient (no overall timeout) for streaming requests.
		return p.streamClient.Do(httpReq)
	}, 2)
	if err != nil {
		return nil, fmt.Errorf("bifrost/openai: do request: %w", err)
	}

	log.Printf("[bifrost/openai] StreamChatCompletion response: status=%d", httpResp.StatusCode)

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
		resp.ReasoningContent = c.Message.ReasoningContent
	}
	return resp
}

// readSSEStream reads the SSE stream from the response body, converts each
// chunk to StreamEvent values, and sends them on ch. The channel is closed
// when the stream ends.
//
// Idle timeout: if no data arrives for streamIdleTimeout, the stream is
// considered dead and an error event is emitted. This prevents hanging
// indefinitely on a broken connection without using an overall timeout.
func (p *OpenAIProvider) readSSEStream(ctx context.Context, body io.ReadCloser, ch chan<- StreamEvent) {
	defer close(ch)
	defer body.Close()

	// Wrap body with idle timeout detection.
	idleBody := &idleTimeoutReader{r: body, timeout: streamIdleTimeout}
	scanner := bufio.NewScanner(idleBody)
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
			// Reasoning content delta.
			if choice.Delta.ReasoningContent != "" {
				select {
				case ch <- StreamEvent{Type: "reasoning_delta", ReasoningContent: choice.Delta.ReasoningContent}:
				case <-ctx.Done():
					return
				}
			}
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

// idleTimeoutReader wraps an io.Reader and returns an error if no data
// arrives within the timeout period. This prevents streaming from hanging
// indefinitely on a broken connection without imposing an overall time limit.
// As long as data keeps flowing (even slowly), the stream continues forever.
type idleTimeoutReader struct {
	r       io.Reader
	timeout time.Duration
}

func (itr *idleTimeoutReader) Read(p []byte) (int, error) {
	type readResult struct {
		n   int
		err error
	}
	ch := make(chan readResult, 1)
	go func() {
		n, err := itr.r.Read(p)
		ch <- readResult{n, err}
	}()
	select {
	case res := <-ch:
		return res.n, res.err
	case <-time.After(itr.timeout):
		return 0, fmt.Errorf("stream idle timeout: no data received for %v", itr.timeout)
	}
}
