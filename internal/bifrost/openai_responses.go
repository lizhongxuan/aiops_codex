package bifrost

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// responsesAPIRequest is the JSON body sent to the OpenAI Responses API endpoint.
type responsesAPIRequest struct {
	Model       string        `json:"model"`
	Input       []interface{} `json:"input"`
	Tools       []interface{} `json:"tools"`
	Stream      bool          `json:"stream"`
	MaxTokens   int           `json:"max_output_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

// responsesTextDelta is the data payload for response.output_text.delta events.
type responsesTextDelta struct {
	Type  string `json:"type"`
	Delta string `json:"delta"`
}

// responsesItemDone is the data payload for response.output_item.done events.
type responsesItemDone struct {
	Type string              `json:"type"`
	Item responsesItemDoneItem `json:"item"`
}

type responsesItemDoneItem struct {
	ID        string `json:"id"`
	Type      string `json:"type"` // "function_call", "web_search_call", "message", etc.
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	CallID    string `json:"call_id,omitempty"`
}

// responsesCompleted is the data payload for response.completed events.
type responsesCompleted struct {
	Type     string                    `json:"type"`
	Response responsesCompletedPayload `json:"response"`
}

type responsesCompletedPayload struct {
	Usage *responsesUsage `json:"usage,omitempty"`
}

type responsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// streamResponsesAPI performs a streaming request against the OpenAI Responses
// API (/v1/responses). It converts the ChatRequest messages to the Responses
// API input format, adds native web_search tool when requested, and maps the
// SSE events back to our unified StreamEvent types.
func (p *OpenAIProvider) streamResponsesAPI(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	apiReq := responsesAPIRequest{
		Model:       req.Model,
		Input:       convertMessagesToInput(req.Messages),
		Tools:       buildResponsesTools(req.Tools, req.WebSearchEnabled),
		Stream:      true,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("bifrost/openai-responses: marshal request: %w", err)
	}

	log.Printf("[bifrost-responses] POST %s/responses model=%s tools=%d webSearch=%v",
		p.baseURL, req.Model, len(apiReq.Tools), req.WebSearchEnabled)

	// Use a longer timeout for Responses API since web search can take a while.
	client := &http.Client{Timeout: 120 * time.Second}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("bifrost/openai-responses: create request: %w", err)
	}
	p.setHeaders(httpReq)

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("bifrost/openai-responses: do request: %w", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		defer httpResp.Body.Close()
		return nil, p.parseErrorResponse(httpResp)
	}

	ch := make(chan StreamEvent, 16)
	go p.readResponsesSSEStream(ctx, httpResp.Body, ch)
	return ch, nil
}

// convertMessagesToInput converts bifrost Messages to the Responses API input
// format. Each message becomes a map with "role" and "content" fields.
func convertMessagesToInput(messages []Message) []interface{} {
	input := make([]interface{}, 0, len(messages))
	for _, m := range messages {
		// The Responses API uses "developer" instead of "system" for system messages.
		role := m.Role
		if role == "system" {
			role = "developer"
		}

		item := map[string]interface{}{
			"role":    role,
			"content": m.Content,
		}

		// Preserve tool_call_id for tool result messages.
		if m.ToolCallID != "" {
			item["tool_call_id"] = m.ToolCallID
		}

		input = append(input, item)
	}
	return input
}

// buildResponsesTools converts our ToolDefinition slice to the Responses API
// tools format. It skips any tool named "web_search" (since it's added as a
// native tool type) and converts the rest to function tool format.
func buildResponsesTools(tools []ToolDefinition, webSearchEnabled bool) []interface{} {
	result := make([]interface{}, 0, len(tools)+1)

	// Add native web_search tool if enabled.
	if webSearchEnabled {
		result = append(result, map[string]interface{}{
			"type": "web_search",
		})
	}

	// Convert function tools, skipping web_search (it's native now).
	for _, t := range tools {
		if t.Function.Name == "web_search" {
			continue
		}
		ft := map[string]interface{}{
			"type": "function",
			"name": t.Function.Name,
		}
		if t.Function.Description != "" {
			ft["description"] = t.Function.Description
		}
		if t.Function.Parameters != nil {
			ft["parameters"] = t.Function.Parameters
		}
		result = append(result, ft)
	}

	return result
}

// readResponsesSSEStream reads the SSE stream from the Responses API, maps
// events to our StreamEvent types, and sends them on ch.
//
// Responses API SSE format uses "event:" and "data:" lines:
//
//	event: response.output_text.delta
//	data: {"type":"response.output_text.delta","delta":"Hello"}
//
//	event: response.output_item.done
//	data: {"type":"response.output_item.done","item":{"id":"fc_xxx","type":"function_call","name":"fn","arguments":"{}"}}
//
//	event: response.completed
//	data: {"type":"response.completed","response":{...}}
func (p *OpenAIProvider) readResponsesSSEStream(ctx context.Context, body io.ReadCloser, ch chan<- StreamEvent) {
	defer close(ch)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	// Increase buffer for potentially large SSE payloads (web search results).
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

	var currentEvent string

	for scanner.Scan() {
		line := scanner.Text()

		// Track the event type from "event:" lines.
		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
			continue
		}

		// Only process "data:" lines.
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		switch currentEvent {
		case "response.output_text.delta":
			var delta responsesTextDelta
			if err := json.Unmarshal([]byte(data), &delta); err != nil {
				log.Printf("[bifrost-responses] parse output_text.delta error: %v", err)
				continue
			}
			if delta.Delta != "" {
				select {
				case ch <- StreamEvent{Type: "content_delta", Delta: delta.Delta}:
				case <-ctx.Done():
					return
				}
			}

		case "response.output_item.done":
			var item responsesItemDone
			if err := json.Unmarshal([]byte(data), &item); err != nil {
				log.Printf("[bifrost-responses] parse output_item.done error: %v", err)
				continue
			}
			if item.Item.Type == "function_call" {
				select {
				case ch <- StreamEvent{
					Type:       "tool_call_delta",
					ToolCallID: item.Item.CallID,
					FuncName:   item.Item.Name,
					FuncArgs:   item.Item.Arguments,
				}:
				case <-ctx.Done():
					return
				}
			}
			// web_search_call results are informational; the model will
			// incorporate them into its text output automatically.

		case "response.completed":
			select {
			case ch <- StreamEvent{Type: "done"}:
			case <-ctx.Done():
			}
			return

		case "response.failed", "response.incomplete":
			// Terminal error events.
			select {
			case ch <- StreamEvent{Type: "error", Delta: fmt.Sprintf("responses API: %s", currentEvent)}:
			case <-ctx.Done():
			}
			return
		}

		// Reset event type after processing data line.
		currentEvent = ""
	}

	if err := scanner.Err(); err != nil {
		select {
		case ch <- StreamEvent{Type: "error", Delta: fmt.Sprintf("responses SSE read error: %v", err)}:
		case <-ctx.Done():
		}
	}
}
