package agentloop

import (
	"context"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
	"github.com/lizhongxuan/aiops-codex/internal/hooks"
)

func TestRunTurn_PromptSubmitHookModifiesInput(t *testing.T) {
	tp := &testProvider{
		streamFn: func(_ context.Context, req bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			return makeStreamCh([]bifrost.StreamEvent{
				{Type: "content_delta", Delta: "ok"},
				{Type: "done"},
			}), nil
		},
	}

	rt := hooks.NewRuntime()
	rt.Register(hooks.Hook{
		Name:  "rewrite",
		Event: hooks.EventPromptSubmit,
		Handler: func(payload interface{}) (hooks.HookOutcome, error) {
			return hooks.HookOutcome{ModifiedInput: "modified-input"}, nil
		},
	})

	loop := newLoopWithProvider(tp, nil).SetHookRuntime(rt)
	session := NewSession("hook-test", SessionSpec{Model: "test-model"})

	if err := loop.RunTurn(context.Background(), session, "original-input"); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	msgs := session.ContextManager().Messages()
	if len(msgs) < 1 {
		t.Fatal("expected at least 1 message")
	}
	// The user message should contain the modified input.
	if msgs[0].Role != "user" || msgs[0].Content != "modified-input" {
		t.Errorf("expected user message with modified-input, got role=%s content=%q", msgs[0].Role, msgs[0].Content)
	}
}

func TestRunTurn_PromptSubmitHookBlocks(t *testing.T) {
	tp := &testProvider{
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			t.Fatal("LLM should not be called when prompt is blocked")
			return nil, nil
		},
	}

	rt := hooks.NewRuntime()
	rt.Register(hooks.Hook{
		Name:  "blocker",
		Event: hooks.EventPromptSubmit,
		Handler: func(payload interface{}) (hooks.HookOutcome, error) {
			return hooks.HookOutcome{Block: true, BlockReason: "not allowed"}, nil
		},
	})

	loop := newLoopWithProvider(tp, nil).SetHookRuntime(rt)
	session := NewSession("hook-block-test", SessionSpec{Model: "test-model"})

	if err := loop.RunTurn(context.Background(), session, "blocked input"); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	msgs := session.ContextManager().Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (user + assistant block), got %d", len(msgs))
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "[Hook blocked prompt]: not allowed" {
		t.Errorf("unexpected block message: role=%s content=%q", msgs[1].Role, msgs[1].Content)
	}
}

func TestExecuteTool_PreToolUseHookBlocks(t *testing.T) {
	callCount := 0
	tp := &testProvider{
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			callCount++
			if callCount == 1 {
				return makeStreamCh([]bifrost.StreamEvent{
					{Type: "tool_call_delta", ToolIndex: 0, ToolCallID: "call-1", FuncName: "dangerous"},
					{Type: "tool_call_delta", ToolIndex: 0, FuncArgs: `{"cmd":"rm -rf"}`},
					{Type: "done"},
				}), nil
			}
			return makeStreamCh([]bifrost.StreamEvent{
				{Type: "content_delta", Delta: "blocked"},
				{Type: "done"},
			}), nil
		},
	}

	rt := hooks.NewRuntime()
	rt.Register(hooks.Hook{
		Name:  "tool-blocker",
		Event: hooks.EventPreToolUse,
		Handler: func(payload interface{}) (hooks.HookOutcome, error) {
			req := payload.(hooks.PreToolUseRequest)
			if req.ToolName == "dangerous" {
				return hooks.HookOutcome{Block: true, BlockReason: "tool is dangerous"}, nil
			}
			return hooks.HookOutcome{}, nil
		},
	})

	var toolExecuted bool
	loop := newLoopWithProvider(tp, nil).SetHookRuntime(rt)
	loop.toolReg.Register(ToolEntry{
		Name: "dangerous",
		Handler: func(_ context.Context, _ *Session, _ bifrost.ToolCall, _ map[string]interface{}) (string, error) {
			toolExecuted = true
			return "executed", nil
		},
	})

	session := NewSession("pre-hook-test", SessionSpec{Model: "test-model"})
	if err := loop.RunTurn(context.Background(), session, "run dangerous"); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	if toolExecuted {
		t.Fatal("tool should not have been executed when pre_tool_use hook blocks")
	}
}

func TestExecuteTool_PostToolUseHookAddsContext(t *testing.T) {
	callCount := 0
	tp := &testProvider{
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			callCount++
			if callCount == 1 {
				return makeStreamCh([]bifrost.StreamEvent{
					{Type: "tool_call_delta", ToolIndex: 0, ToolCallID: "call-1", FuncName: "echo"},
					{Type: "tool_call_delta", ToolIndex: 0, FuncArgs: `{"msg":"hi"}`},
					{Type: "done"},
				}), nil
			}
			return makeStreamCh([]bifrost.StreamEvent{
				{Type: "content_delta", Delta: "done"},
				{Type: "done"},
			}), nil
		},
	}

	rt := hooks.NewRuntime()
	rt.Register(hooks.Hook{
		Name:  "post-context",
		Event: hooks.EventPostToolUse,
		Handler: func(payload interface{}) (hooks.HookOutcome, error) {
			return hooks.HookOutcome{
				AdditionalContexts: []string{"extra context from post hook"},
			}, nil
		},
	})

	loop := newLoopWithProvider(tp, nil).SetHookRuntime(rt)
	loop.toolReg.Register(ToolEntry{
		Name: "echo",
		Handler: func(_ context.Context, _ *Session, _ bifrost.ToolCall, args map[string]interface{}) (string, error) {
			return "echoed: " + args["msg"].(string), nil
		},
	})

	session := NewSession("post-hook-test", SessionSpec{Model: "test-model"})
	if err := loop.RunTurn(context.Background(), session, "call echo"); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	// Check that the additional context was appended.
	msgs := session.ContextManager().Messages()
	found := false
	for _, m := range msgs {
		if m.Role == "user" && m.Content == "extra context from post hook" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected additional context from post_tool_use hook to be appended")
	}
}

func TestInitSession_SessionStartHookAddsContext(t *testing.T) {
	rt := hooks.NewRuntime()
	rt.Register(hooks.Hook{
		Name:  "session-init",
		Event: hooks.EventSessionStart,
		Handler: func(payload interface{}) (hooks.HookOutcome, error) {
			return hooks.HookOutcome{
				AdditionalContexts: []string{"session start context"},
			}, nil
		},
	})

	gw := bifrost.NewGateway(bifrost.GatewayConfig{DefaultProvider: "test"})
	loop := NewLoop(gw, NewToolRegistry(), nil).SetHookRuntime(rt)
	session := NewSession("init-test", SessionSpec{Model: "test-model"})

	loop.InitSession(session)

	msgs := session.ContextManager().Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message from session start hook, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "session start context" {
		t.Errorf("unexpected message: role=%s content=%q", msgs[0].Role, msgs[0].Content)
	}
}

func TestInitSession_NilHookRuntime(t *testing.T) {
	gw := bifrost.NewGateway(bifrost.GatewayConfig{DefaultProvider: "test"})
	loop := NewLoop(gw, NewToolRegistry(), nil)
	session := NewSession("no-hooks", SessionSpec{Model: "test-model"})

	// Should not panic.
	loop.InitSession(session)

	msgs := session.ContextManager().Messages()
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages without hook runtime, got %d", len(msgs))
	}
}
