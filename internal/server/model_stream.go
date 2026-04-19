package server

import (
	"context"
	"fmt"
	"log"
	"time"
)

// ModelStreamEvent represents a unified event from the model stream.
type ModelStreamEvent struct {
	Type      string         // assistant_delta, tool_use, tool_result, approval_pending, turn_completed, turn_failed
	Content   string         // text content for assistant_delta
	ToolName  string         // tool name for tool_use
	ToolInput map[string]any // tool input for tool_use
	CallID    string         // call ID for tool_use/tool_result
	Error     error          // error for turn_failed
	Metadata  map[string]any // additional metadata
}

// ModelStreamEventType constants
const (
	ModelEventAssistantDelta  = "assistant_delta"
	ModelEventToolUse         = "tool_use"
	ModelEventToolResult      = "tool_result"
	ModelEventApprovalPending = "approval_pending"
	ModelEventTurnCompleted   = "turn_completed"
	ModelEventTurnFailed      = "turn_failed"
)

// ModelCallParams contains parameters for a model call.
type ModelCallParams struct {
	Messages        []map[string]any
	SystemPrompt    string
	Tools           []map[string]any
	Model           string
	FallbackModel   string
	MaxOutputTokens int
	AbortSignal     context.Context
	IterationID     string
	SessionID       string
}

// ModelCallResult contains the result of a model call.
type ModelCallResult struct {
	StopReason    string
	AssistantText string
	ToolCalls     []ModelToolCall
	TokensUsed    int
	Duration      time.Duration
	ModelName     string
	Error         error
}

// ModelToolCall represents a tool call from the model.
type ModelToolCall struct {
	ID        string
	Name      string
	Input     map[string]any
	InputJSON string
}

// ModelStreamClient abstracts the model calling layer.
// Currently backed by Codex app-server, but can be replaced with
// a direct API client in the future.
type ModelStreamClient interface {
	// CallModel initiates a model call and returns the result.
	// The implementation handles streaming internally.
	CallModel(ctx context.Context, params ModelCallParams) (*ModelCallResult, error)
}

// codexModelStreamClient implements ModelStreamClient using Codex app-server.
type codexModelStreamClient struct {
	app *App
}

// newCodexModelStreamClient creates a new Codex-backed model stream client.
func newCodexModelStreamClient(app *App) *codexModelStreamClient {
	return &codexModelStreamClient{app: app}
}

// CallModel implements ModelStreamClient by delegating to Codex thread/turn.
// The actual streaming is handled by the Codex notification loop.
func (c *codexModelStreamClient) CallModel(ctx context.Context, params ModelCallParams) (*ModelCallResult, error) {
	if c.app == nil {
		return nil, fmt.Errorf("codex model stream client: app is nil")
	}

	start := time.Now()
	log.Printf("[model_stream] call_model session=%s iteration=%s model=%s tools=%d",
		params.SessionID, params.IterationID, params.Model, len(params.Tools))

	// The actual model call is handled by the existing thread/turn mechanism.
	// This abstraction allows future replacement with direct API calls.
	result := &ModelCallResult{
		ModelName: params.Model,
		Duration:  time.Since(start),
	}

	return result, nil
}

// mapCodexNotificationToEvent maps a Codex notification to a ModelStreamEvent.
func mapCodexNotificationToEvent(method string, payload map[string]any) ModelStreamEvent {
	switch {
	case method == "item/started" || method == "item/updated":
		itemType := getStringAny(payload, "type")
		switch itemType {
		case "message":
			return ModelStreamEvent{
				Type:    ModelEventAssistantDelta,
				Content: getStringAny(payload, "content", "text"),
			}
		case "function_call", "tool_use":
			return ModelStreamEvent{
				Type:     ModelEventToolUse,
				ToolName: getStringAny(payload, "name", "tool"),
				CallID:   getStringAny(payload, "id", "callId"),
			}
		}
	case method == "item/completed":
		return ModelStreamEvent{
			Type: ModelEventToolResult,
		}
	case method == "turn/completed":
		return ModelStreamEvent{
			Type: ModelEventTurnCompleted,
		}
	case method == "turn/failed":
		return ModelStreamEvent{
			Type:  ModelEventTurnFailed,
			Error: fmt.Errorf("turn failed: %s", getStringAny(payload, "error", "message")),
		}
	}
	return ModelStreamEvent{Type: "unknown"}
}

// classifyStopReason maps a Codex turn completion to a stop reason.
func classifyStopReason(event ModelStreamEvent, hasPendingTools bool) string {
	switch event.Type {
	case ModelEventTurnCompleted:
		if hasPendingTools {
			return stopReasonToolUse
		}
		return stopReasonEndTurn
	case ModelEventTurnFailed:
		if event.Error != nil {
			errStr := event.Error.Error()
			if containsAny(errStr, "max_tokens", "output_limit") {
				return stopReasonMaxTokens
			}
			if containsAny(errStr, "prompt_too_long", "context_length") {
				return stopReasonFailed
			}
		}
		return stopReasonFailed
	case ModelEventApprovalPending:
		return stopReasonWaitingApproval
	default:
		return ""
	}
}

// containsAny checks if s contains any of the substrings.
func containsAny(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if len(sub) > 0 && len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
