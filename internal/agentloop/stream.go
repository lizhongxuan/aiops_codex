package agentloop

import (
	"context"
	"fmt"
	"sort"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// StreamResult holds the accumulated output from consuming a streaming LLM response.
type StreamResult struct {
	Content          string
	ReasoningContent string // accumulated reasoning content from reasoning_delta events
	ToolCalls        []bifrost.ToolCall
}

// StreamObserver receives incremental assistant deltas while a stream is consumed.
type StreamObserver interface {
	OnAssistantDelta(ctx context.Context, session *Session, delta string) error
	OnToolCallDelta(ctx context.Context, session *Session, index int, toolCall bifrost.ToolCall) error
	OnStreamComplete(ctx context.Context, session *Session, result *StreamResult) error
}

type noopStreamObserver struct{}

func (noopStreamObserver) OnAssistantDelta(context.Context, *Session, string) error {
	return nil
}

func (noopStreamObserver) OnToolCallDelta(context.Context, *Session, int, bifrost.ToolCall) error {
	return nil
}

func (noopStreamObserver) OnStreamComplete(context.Context, *Session, *StreamResult) error {
	return nil
}

// consumeStream reads events from the stream channel and accumulates the
// assistant's content and tool calls.
func (l *Loop) consumeStream(ctx context.Context, session *Session, stream <-chan bifrost.StreamEvent) (*StreamResult, error) {
	result := &StreamResult{}
	toolCallMap := make(map[int]*bifrost.ToolCall)

	for {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case event, ok := <-stream:
			if !ok {
				result.ToolCalls = orderedToolCalls(toolCallMap)
				if err := l.streamObserver.OnStreamComplete(ctx, session, result); err != nil {
					return result, err
				}
				return result, nil
			}

			switch event.Type {
			case "content_delta":
				result.Content += event.Delta
				if err := l.streamObserver.OnAssistantDelta(ctx, session, event.Delta); err != nil {
					return result, err
				}

			case "reasoning_delta":
				result.ReasoningContent += event.ReasoningContent

			case "tool_call_delta":
				tc := mergeToolCallDelta(toolCallMap, event)
				if err := l.streamObserver.OnToolCallDelta(ctx, session, event.ToolIndex, *tc); err != nil {
					return result, err
				}

			case "error":
				return result, fmt.Errorf("stream error: %s", event.Delta)

			case "done":
				result.ToolCalls = orderedToolCalls(toolCallMap)
				if err := l.streamObserver.OnStreamComplete(ctx, session, result); err != nil {
					return result, err
				}
				return result, nil
			}
		}
	}
}

func mergeToolCallDelta(toolCallMap map[int]*bifrost.ToolCall, event bifrost.StreamEvent) *bifrost.ToolCall {
	tc, ok := toolCallMap[event.ToolIndex]
	if !ok {
		tc = &bifrost.ToolCall{
			ID:   event.ToolCallID,
			Type: "function",
		}
		toolCallMap[event.ToolIndex] = tc
	}
	if event.ToolCallID != "" {
		tc.ID = event.ToolCallID
	}
	if event.FuncName != "" {
		tc.Function.Name += event.FuncName
	}
	if event.FuncArgs != "" {
		tc.Function.Arguments += event.FuncArgs
	}
	return tc
}

func orderedToolCalls(toolCallMap map[int]*bifrost.ToolCall) []bifrost.ToolCall {
	if len(toolCallMap) == 0 {
		return nil
	}

	indexes := make([]int, 0, len(toolCallMap))
	for idx := range toolCallMap {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)

	out := make([]bifrost.ToolCall, 0, len(indexes))
	for _, idx := range indexes {
		out = append(out, *toolCallMap[idx])
	}
	return out
}
