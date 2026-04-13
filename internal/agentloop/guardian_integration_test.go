package agentloop

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
	"github.com/lizhongxuan/aiops-codex/internal/guardian"
)

func TestAwaitToolApproval_GuardianCacheHit(t *testing.T) {
	// Setup: a tool that requires approval.
	callCount := 0
	tp := &testProvider{
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			callCount++
			if callCount == 1 {
				return makeStreamCh([]bifrost.StreamEvent{
					{Type: "tool_call_delta", ToolIndex: 0, ToolCallID: "call-g1", FuncName: "deploy"},
					{Type: "tool_call_delta", ToolIndex: 0, FuncArgs: `{"env":"staging"}`},
					{Type: "done"},
				}), nil
			}
			return makeStreamCh([]bifrost.StreamEvent{
				{Type: "content_delta", Delta: "deployed"},
				{Type: "done"},
			}), nil
		},
	}

	loop := newLoopWithProvider(tp, nil)
	var executed atomic.Bool
	loop.toolReg.Register(ToolEntry{
		Name:             "deploy",
		Description:      "Deploy to environment",
		RequiresApproval: true,
		Handler: func(_ context.Context, _ *Session, _ bifrost.ToolCall, _ map[string]interface{}) (string, error) {
			executed.Store(true)
			return "deployed ok", nil
		},
	})

	session := NewSession("guardian-cache-session", SessionSpec{Model: "test-model"})

	// Pre-populate the approval cache with an allow decision.
	cache := guardian.NewApprovalCache()
	cache.Store("deploy:{\"env\":\"staging\"}", guardian.ApprovalDecision{
		Outcome:   guardian.OutcomeAllow,
		Rationale: "previously approved",
	})
	session.SetGuardian(nil, cache)

	if err := loop.RunTurn(context.Background(), session, "deploy staging"); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if !executed.Load() {
		t.Fatal("expected tool to execute via cache hit")
	}
}

func TestAwaitToolApproval_GuardianCacheDeny(t *testing.T) {
	callCount := 0
	tp := &testProvider{
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			callCount++
			if callCount == 1 {
				return makeStreamCh([]bifrost.StreamEvent{
					{Type: "tool_call_delta", ToolIndex: 0, ToolCallID: "call-g2", FuncName: "deploy"},
					{Type: "tool_call_delta", ToolIndex: 0, FuncArgs: `{"env":"prod"}`},
					{Type: "done"},
				}), nil
			}
			return makeStreamCh([]bifrost.StreamEvent{
				{Type: "content_delta", Delta: "denied"},
				{Type: "done"},
			}), nil
		},
	}

	loop := newLoopWithProvider(tp, nil)
	var executed atomic.Bool
	loop.toolReg.Register(ToolEntry{
		Name:             "deploy",
		Description:      "Deploy to environment",
		RequiresApproval: true,
		Handler: func(_ context.Context, _ *Session, _ bifrost.ToolCall, _ map[string]interface{}) (string, error) {
			executed.Store(true)
			return "deployed", nil
		},
	})

	session := NewSession("guardian-deny-session", SessionSpec{Model: "test-model"})

	// Pre-populate cache with a deny decision.
	cache := guardian.NewApprovalCache()
	cache.Store("deploy:{\"env\":\"prod\"}", guardian.ApprovalDecision{
		Outcome:   guardian.OutcomeDeny,
		Rationale: "production deploy blocked",
	})
	session.SetGuardian(nil, cache)

	if err := loop.RunTurn(context.Background(), session, "deploy prod"); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if executed.Load() {
		t.Fatal("expected tool NOT to execute when cache denies")
	}
}

func TestAwaitToolApproval_FallsBackToHandler(t *testing.T) {
	// When guardian is nil and cache is nil, falls back to approval handler.
	callCount := 0
	tp := &testProvider{
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			callCount++
			if callCount == 1 {
				return makeStreamCh([]bifrost.StreamEvent{
					{Type: "tool_call_delta", ToolIndex: 0, ToolCallID: "call-fb", FuncName: "rm_file"},
					{Type: "tool_call_delta", ToolIndex: 0, FuncArgs: `{"path":"/tmp/x"}`},
					{Type: "done"},
				}), nil
			}
			return makeStreamCh([]bifrost.StreamEvent{
				{Type: "content_delta", Delta: "removed"},
				{Type: "done"},
			}), nil
		},
	}

	loop := newLoopWithProvider(tp, nil)
	var executed atomic.Bool
	loop.toolReg.Register(ToolEntry{
		Name:             "rm_file",
		Description:      "Remove a file",
		RequiresApproval: true,
		Handler: func(_ context.Context, _ *Session, _ bifrost.ToolCall, _ map[string]interface{}) (string, error) {
			executed.Store(true)
			return "removed", nil
		},
	})
	loop.SetApprovalHandler(approvalHandlerFunc(func(_ context.Context, session *Session, _ ApprovalRequest) (string, error) {
		go func() {
			session.ResolveApproval(ApprovalDecision{ApprovalID: "ap-fb", Decision: "approve"})
		}()
		return "ap-fb", nil
	}))

	session := NewSession("fallback-session", SessionSpec{Model: "test-model"})
	// No guardian, no cache — should fall through to handler.

	if err := loop.RunTurn(context.Background(), session, "remove file"); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if !executed.Load() {
		t.Fatal("expected tool to execute via fallback approval handler")
	}
}
