package hooks

import (
	"errors"
	"testing"
)

// --- Registry Tests ---

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRegistry()

	h1 := Hook{Name: "h1", Event: EventSessionStart, Handler: func(_ interface{}) (HookOutcome, error) { return HookOutcome{}, nil }}
	h2 := Hook{Name: "h2", Event: EventSessionStart, Handler: func(_ interface{}) (HookOutcome, error) { return HookOutcome{}, nil }}
	h3 := Hook{Name: "h3", Event: EventPreToolUse, Handler: func(_ interface{}) (HookOutcome, error) { return HookOutcome{}, nil }}

	reg.Register(h1)
	reg.Register(h2)
	reg.Register(h3)

	sessionHooks := reg.Get(EventSessionStart)
	if len(sessionHooks) != 2 {
		t.Fatalf("expected 2 session_start hooks, got %d", len(sessionHooks))
	}
	if sessionHooks[0].Name != "h1" || sessionHooks[1].Name != "h2" {
		t.Fatalf("hooks not in registration order: %v, %v", sessionHooks[0].Name, sessionHooks[1].Name)
	}

	preToolHooks := reg.Get(EventPreToolUse)
	if len(preToolHooks) != 1 {
		t.Fatalf("expected 1 pre_tool_use hook, got %d", len(preToolHooks))
	}

	postToolHooks := reg.Get(EventPostToolUse)
	if postToolHooks != nil {
		t.Fatalf("expected nil for unregistered event, got %v", postToolHooks)
	}
}

func TestRegistry_Count(t *testing.T) {
	reg := NewRegistry()
	if reg.Count(EventSessionStart) != 0 {
		t.Fatal("expected 0 for empty registry")
	}
	reg.Register(Hook{Name: "a", Event: EventSessionStart, Handler: func(_ interface{}) (HookOutcome, error) { return HookOutcome{}, nil }})
	if reg.Count(EventSessionStart) != 1 {
		t.Fatal("expected 1 after registration")
	}
}

// --- ExecuteSessionStart Tests ---

func TestExecuteSessionStart_ContextInjection(t *testing.T) {
	rt := NewRuntime()
	rt.Register(Hook{
		Name:  "ctx1",
		Event: EventSessionStart,
		Handler: func(_ interface{}) (HookOutcome, error) {
			return HookOutcome{AdditionalContexts: []string{"context-A"}}, nil
		},
	})
	rt.Register(Hook{
		Name:  "ctx2",
		Event: EventSessionStart,
		Handler: func(_ interface{}) (HookOutcome, error) {
			return HookOutcome{AdditionalContexts: []string{"context-B", "context-C"}}, nil
		},
	})

	result := rt.ExecuteSessionStart()
	if len(result.AdditionalContexts) != 3 {
		t.Fatalf("expected 3 contexts, got %d", len(result.AdditionalContexts))
	}
	expected := []string{"context-A", "context-B", "context-C"}
	for i, ctx := range expected {
		if result.AdditionalContexts[i] != ctx {
			t.Errorf("context[%d]: expected %q, got %q", i, ctx, result.AdditionalContexts[i])
		}
	}
}

func TestExecuteSessionStart_ErrorContinues(t *testing.T) {
	rt := NewRuntime()
	rt.Register(Hook{
		Name:  "failing",
		Event: EventSessionStart,
		Handler: func(_ interface{}) (HookOutcome, error) {
			return HookOutcome{}, errors.New("boom")
		},
	})
	rt.Register(Hook{
		Name:  "succeeding",
		Event: EventSessionStart,
		Handler: func(_ interface{}) (HookOutcome, error) {
			return HookOutcome{AdditionalContexts: []string{"survived"}}, nil
		},
	})

	result := rt.ExecuteSessionStart()
	if len(result.AdditionalContexts) != 1 || result.AdditionalContexts[0] != "survived" {
		t.Fatalf("expected context from second hook after first failed, got %v", result.AdditionalContexts)
	}
}

// --- ExecutePreToolUse Tests ---

func TestExecutePreToolUse_Blocking(t *testing.T) {
	rt := NewRuntime()
	rt.Register(Hook{
		Name:  "blocker",
		Event: EventPreToolUse,
		Handler: func(payload interface{}) (HookOutcome, error) {
			req := payload.(PreToolUseRequest)
			if req.ToolName == "dangerous_tool" {
				return HookOutcome{Block: true, BlockReason: "tool is forbidden"}, nil
			}
			return HookOutcome{}, nil
		},
	})
	rt.Register(Hook{
		Name:  "after-blocker",
		Event: EventPreToolUse,
		Handler: func(_ interface{}) (HookOutcome, error) {
			return HookOutcome{AdditionalContexts: []string{"should-not-appear"}}, nil
		},
	})

	result := rt.ExecutePreToolUse(PreToolUseRequest{ToolName: "dangerous_tool"})
	if !result.Block {
		t.Fatal("expected block")
	}
	if result.BlockReason != "tool is forbidden" {
		t.Fatalf("unexpected block reason: %s", result.BlockReason)
	}
	// The second hook should not have run.
	for _, ctx := range result.AdditionalContexts {
		if ctx == "should-not-appear" {
			t.Fatal("hook after blocker should not have executed")
		}
	}
}

func TestExecutePreToolUse_ContextInjection(t *testing.T) {
	rt := NewRuntime()
	rt.Register(Hook{
		Name:  "injector",
		Event: EventPreToolUse,
		Handler: func(_ interface{}) (HookOutcome, error) {
			return HookOutcome{AdditionalContexts: []string{"extra-info"}}, nil
		},
	})

	result := rt.ExecutePreToolUse(PreToolUseRequest{ToolName: "safe_tool"})
	if result.Block {
		t.Fatal("should not block")
	}
	if len(result.AdditionalContexts) != 1 || result.AdditionalContexts[0] != "extra-info" {
		t.Fatalf("unexpected contexts: %v", result.AdditionalContexts)
	}
}

// --- ExecutePostToolUse Tests ---

func TestExecutePostToolUse_ResultAccess(t *testing.T) {
	rt := NewRuntime()
	var receivedResult interface{}
	rt.Register(Hook{
		Name:  "inspector",
		Event: EventPostToolUse,
		Handler: func(payload interface{}) (HookOutcome, error) {
			req := payload.(PostToolUseRequest)
			receivedResult = req.ToolResult
			return HookOutcome{AdditionalContexts: []string{"inspected"}}, nil
		},
	})

	rt.ExecutePostToolUse(PostToolUseRequest{
		ToolName:   "read_file",
		ToolInput:  map[string]interface{}{"path": "/tmp/x"},
		ToolResult: "file contents here",
	})

	if receivedResult != "file contents here" {
		t.Fatalf("hook did not receive tool result, got: %v", receivedResult)
	}
}

func TestExecutePostToolUse_ContextInjection(t *testing.T) {
	rt := NewRuntime()
	rt.Register(Hook{
		Name:  "ctx-a",
		Event: EventPostToolUse,
		Handler: func(_ interface{}) (HookOutcome, error) {
			return HookOutcome{AdditionalContexts: []string{"post-ctx-1"}}, nil
		},
	})
	rt.Register(Hook{
		Name:  "ctx-b",
		Event: EventPostToolUse,
		Handler: func(_ interface{}) (HookOutcome, error) {
			return HookOutcome{AdditionalContexts: []string{"post-ctx-2"}}, nil
		},
	})

	result := rt.ExecutePostToolUse(PostToolUseRequest{ToolName: "test"})
	if len(result.AdditionalContexts) != 2 {
		t.Fatalf("expected 2 contexts, got %d", len(result.AdditionalContexts))
	}
}

// --- ExecutePromptSubmit Tests ---

func TestExecutePromptSubmit_InputModification(t *testing.T) {
	rt := NewRuntime()
	rt.Register(Hook{
		Name:  "modifier",
		Event: EventPromptSubmit,
		Handler: func(payload interface{}) (HookOutcome, error) {
			input := payload.(string)
			return HookOutcome{ModifiedInput: input + " [modified]"}, nil
		},
	})

	result := rt.ExecutePromptSubmit("hello")
	if result.Block {
		t.Fatal("should not block")
	}
	if result.ModifiedInput != "hello [modified]" {
		t.Fatalf("expected modified input, got: %s", result.ModifiedInput)
	}
}

func TestExecutePromptSubmit_ChainedModification(t *testing.T) {
	rt := NewRuntime()
	rt.Register(Hook{
		Name:  "first",
		Event: EventPromptSubmit,
		Handler: func(payload interface{}) (HookOutcome, error) {
			input := payload.(string)
			return HookOutcome{ModifiedInput: input + "-A"}, nil
		},
	})
	rt.Register(Hook{
		Name:  "second",
		Event: EventPromptSubmit,
		Handler: func(payload interface{}) (HookOutcome, error) {
			input := payload.(string)
			return HookOutcome{ModifiedInput: input + "-B"}, nil
		},
	})

	result := rt.ExecutePromptSubmit("start")
	if result.ModifiedInput != "start-A-B" {
		t.Fatalf("expected chained modification, got: %s", result.ModifiedInput)
	}
}

func TestExecutePromptSubmit_Blocking(t *testing.T) {
	rt := NewRuntime()
	rt.Register(Hook{
		Name:  "blocker",
		Event: EventPromptSubmit,
		Handler: func(_ interface{}) (HookOutcome, error) {
			return HookOutcome{Block: true, BlockReason: "content policy violation"}, nil
		},
	})
	rt.Register(Hook{
		Name:  "after",
		Event: EventPromptSubmit,
		Handler: func(payload interface{}) (HookOutcome, error) {
			return HookOutcome{ModifiedInput: "should not reach"}, nil
		},
	})

	result := rt.ExecutePromptSubmit("bad input")
	if !result.Block {
		t.Fatal("expected block")
	}
	if result.BlockReason != "content policy violation" {
		t.Fatalf("unexpected reason: %s", result.BlockReason)
	}
	if result.ModifiedInput == "should not reach" {
		t.Fatal("hook after blocker should not have executed")
	}
}

func TestExecutePromptSubmit_NoModification(t *testing.T) {
	rt := NewRuntime()
	rt.Register(Hook{
		Name:  "noop",
		Event: EventPromptSubmit,
		Handler: func(_ interface{}) (HookOutcome, error) {
			return HookOutcome{}, nil
		},
	})

	result := rt.ExecutePromptSubmit("original")
	if result.ModifiedInput != "original" {
		t.Fatalf("expected original input preserved, got: %s", result.ModifiedInput)
	}
}

// --- Execution Order Test ---

func TestExecutionOrder(t *testing.T) {
	rt := NewRuntime()
	var order []string

	for _, name := range []string{"first", "second", "third"} {
		n := name
		rt.Register(Hook{
			Name:  n,
			Event: EventSessionStart,
			Handler: func(_ interface{}) (HookOutcome, error) {
				order = append(order, n)
				return HookOutcome{}, nil
			},
		})
	}

	rt.ExecuteSessionStart()
	if len(order) != 3 {
		t.Fatalf("expected 3 executions, got %d", len(order))
	}
	expected := []string{"first", "second", "third"}
	for i, name := range expected {
		if order[i] != name {
			t.Errorf("position %d: expected %q, got %q", i, name, order[i])
		}
	}
}
