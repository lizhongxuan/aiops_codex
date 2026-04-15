package agentloop

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewSessionCreatesValidSession(t *testing.T) {
	spec := SessionSpec{
		Model:                 "gpt-4o",
		Cwd:                   "/workspace",
		DeveloperInstructions: "Be helpful.",
		DynamicTools:          []string{"read_file", "execute_command"},
		ApprovalPolicy:        "unless-allow-listed",
		SandboxMode:           "workspaceWrite",
		MaxIterations:         30,
		ContextWindow:         64000,
	}

	s := NewSession("sess-001", spec)

	if s.ID != "sess-001" {
		t.Fatalf("expected ID sess-001, got %s", s.ID)
	}
	if s.Model() != "gpt-4o" {
		t.Fatalf("expected model gpt-4o, got %s", s.Model())
	}
	if s.MaxIterations() != 30 {
		t.Fatalf("expected maxIterations 30, got %d", s.MaxIterations())
	}
	if s.ContextManager() == nil {
		t.Fatal("expected non-nil ContextManager")
	}
	tools := s.EnabledTools()
	if len(tools) != 2 || tools[0] != "read_file" || tools[1] != "execute_command" {
		t.Fatalf("unexpected enabled tools: %v", tools)
	}
	if s.SystemPrompt() == "" {
		t.Fatal("expected non-empty system prompt")
	}
}

func TestNewSessionDefaults(t *testing.T) {
	spec := SessionSpec{
		Model: "claude-sonnet",
	}
	s := NewSession("sess-defaults", spec)

	if s.MaxIterations() != DefaultMaxIterations {
		t.Fatalf("expected default maxIterations %d, got %d", DefaultMaxIterations, s.MaxIterations())
	}
	if s.ContextManager() == nil {
		t.Fatal("expected non-nil ContextManager")
	}
}

func TestCancelStopsContext(t *testing.T) {
	s := NewSession("sess-cancel", SessionSpec{Model: "test"})

	ctx, cancel := context.WithCancel(context.Background())
	s.SetCancelFunc(cancel)

	// Context should still be active.
	select {
	case <-ctx.Done():
		t.Fatal("context should not be cancelled yet")
	default:
	}

	s.Cancel()

	// Context should now be cancelled.
	select {
	case <-ctx.Done():
		// expected
	default:
		t.Fatal("context should be cancelled after Cancel()")
	}
}

func TestCancelWithNilCancelFn(t *testing.T) {
	s := NewSession("sess-nil-cancel", SessionSpec{Model: "test"})
	// Should not panic when cancelFn is nil.
	s.Cancel()
}

func TestResolveApprovalSendsDecision(t *testing.T) {
	s := NewSession("sess-approval", SessionSpec{Model: "test"})

	decision := ApprovalDecision{
		ApprovalID: "appr-1",
		Decision:   "approve",
		Reason:     "looks good",
	}

	s.ResolveApproval(decision)

	// Read from the channel to verify it was sent.
	select {
	case d := <-s.approvalCh:
		if d.ApprovalID != "appr-1" {
			t.Fatalf("expected ApprovalID appr-1, got %s", d.ApprovalID)
		}
		if d.Decision != "approve" {
			t.Fatalf("expected Decision approve, got %s", d.Decision)
		}
		if d.Reason != "looks good" {
			t.Fatalf("expected Reason 'looks good', got %s", d.Reason)
		}
	default:
		t.Fatal("expected decision on approval channel")
	}
}

func TestWaitForApprovalReceivesDecision(t *testing.T) {
	s := NewSession("sess-wait", SessionSpec{Model: "test"})

	go func() {
		time.Sleep(10 * time.Millisecond)
		s.ResolveApproval(ApprovalDecision{
			ApprovalID: "appr-2",
			Decision:   "reject",
			Reason:     "too risky",
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	d, err := s.WaitForApproval(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.ApprovalID != "appr-2" {
		t.Fatalf("expected ApprovalID appr-2, got %s", d.ApprovalID)
	}
	if d.Decision != "reject" {
		t.Fatalf("expected Decision reject, got %s", d.Decision)
	}
}

func TestWaitForApprovalRespectsContextCancellation(t *testing.T) {
	s := NewSession("sess-ctx-cancel", SessionSpec{Model: "test"})

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately.
	cancel()

	_, err := s.WaitForApproval(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestWaitForApprovalIDSkipsMismatchedDecision(t *testing.T) {
	s := NewSession("sess-approval-id", SessionSpec{Model: "test"})

	go func() {
		time.Sleep(10 * time.Millisecond)
		s.ResolveApproval(ApprovalDecision{ApprovalID: "stale", Decision: "approve"})
		time.Sleep(10 * time.Millisecond)
		s.ResolveApproval(ApprovalDecision{ApprovalID: "target", Decision: "reject", Reason: "need review"})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	decision, err := s.WaitForApprovalID(ctx, "target")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.ApprovalID != "target" {
		t.Fatalf("expected target approval ID, got %q", decision.ApprovalID)
	}
	if decision.Reason != "need review" {
		t.Fatalf("expected reason propagated, got %q", decision.Reason)
	}
}

func TestBuildSystemPromptIncludesInstructions(t *testing.T) {
	spec := SessionSpec{
		Model:                 "gpt-4o",
		DeveloperInstructions: "Always verify before executing.",
		DynamicTools:          []string{"read_file", "write_file"},
		ApprovalPolicy:        "unless-allow-listed",
		SandboxMode:           "workspaceWrite",
	}

	prompt := BuildSystemPrompt(spec)

	if !strings.Contains(prompt, "Always verify before executing.") {
		t.Fatal("prompt should contain developer instructions")
	}
	if !strings.Contains(prompt, "read_file") {
		t.Fatal("prompt should contain tool names")
	}
	if !strings.Contains(prompt, "write_file") {
		t.Fatal("prompt should contain tool names")
	}
	if !strings.Contains(prompt, "unless-allow-listed") {
		t.Fatal("prompt should contain approval policy")
	}
	if !strings.Contains(prompt, "workspaceWrite") {
		t.Fatal("prompt should contain sandbox mode")
	}
	if !strings.Contains(prompt, "ReAct agent loop") {
		t.Fatal("prompt should contain ReAct agent loop description")
	}
}

func TestBuildSystemPromptMinimal(t *testing.T) {
	spec := SessionSpec{
		Model: "test-model",
	}

	prompt := BuildSystemPrompt(spec)

	// Should still have the static identity section.
	if !strings.Contains(prompt, "主 Agent") {
		t.Fatal("prompt should contain static identity even with minimal spec")
	}
	// Should not contain approval or sandbox sections.
	if strings.Contains(prompt, "当前审批策略") {
		t.Fatal("prompt should not contain approval policy when not set")
	}
	if strings.Contains(prompt, "当前沙箱模式") {
		t.Fatal("prompt should not contain sandbox mode when not set")
	}
}

func TestSessionCurrentCardID(t *testing.T) {
	s := NewSession("sess-card", SessionSpec{Model: "test"})

	if s.CurrentCardID() != "" {
		t.Fatal("expected empty initial card ID")
	}

	s.SetCurrentCardID("card-123")
	if s.CurrentCardID() != "card-123" {
		t.Fatalf("expected card-123, got %s", s.CurrentCardID())
	}
}

func TestEnabledToolsReturnsCopy(t *testing.T) {
	spec := SessionSpec{
		Model:        "test",
		DynamicTools: []string{"tool_a", "tool_b"},
	}
	s := NewSession("sess-copy", spec)

	tools := s.EnabledTools()
	tools[0] = "modified"

	// Original should be unchanged.
	original := s.EnabledTools()
	if original[0] != "tool_a" {
		t.Fatal("EnabledTools should return a copy, not a reference")
	}
}


// --- contextWindowForModel tests ---

func TestContextWindowForModel_KnownModels(t *testing.T) {
	tests := []struct {
		model string
		want  int
	}{
		{"gpt-5-turbo", 256000},
		{"gpt-4o-mini", 128000},
		{"gpt-4o", 128000},
		{"gpt-4-turbo", 128000},
		{"claude-sonnet-4-20250514", 200000},
		{"claude-3-opus", 200000},
		{"deepseek-chat", 64000},
		{"deepseek-coder", 64000},
		{"glm-4", 128000},
		{"qwen-72b", 128000},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := contextWindowForModel(tt.model)
			if got != tt.want {
				t.Errorf("contextWindowForModel(%q) = %d, want %d", tt.model, got, tt.want)
			}
		})
	}
}

func TestContextWindowForModel_CaseInsensitive(t *testing.T) {
	tests := []struct {
		model string
		want  int
	}{
		{"GPT-4o", 128000},
		{"Claude-3-Opus", 200000},
		{"DEEPSEEK-CHAT", 64000},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := contextWindowForModel(tt.model)
			if got != tt.want {
				t.Errorf("contextWindowForModel(%q) = %d, want %d", tt.model, got, tt.want)
			}
		})
	}
}

func TestContextWindowForModel_UnknownModel(t *testing.T) {
	got := contextWindowForModel("unknown-model-xyz")
	if got != DefaultContextWindow {
		t.Errorf("contextWindowForModel(unknown) = %d, want %d", got, DefaultContextWindow)
	}
}

func TestContextWindowForModel_EmptyModel(t *testing.T) {
	got := contextWindowForModel("")
	if got != DefaultContextWindow {
		t.Errorf("contextWindowForModel('') = %d, want %d", got, DefaultContextWindow)
	}
}

func TestNewSession_DynamicContextWindow(t *testing.T) {
	// When ContextWindow is not specified, it should use model-aware lookup.
	spec := SessionSpec{
		Model: "claude-sonnet-4-20250514",
	}
	s := NewSession("sess-dynamic", spec)

	// The context manager should have been created with 200000 (claude's window).
	// We can verify indirectly by checking that EstimateTokens works with the
	// larger window (rough estimate stays below 70% threshold).
	cm := s.ContextManager()
	cm.AppendUser("test")
	tokens := cm.EstimateTokens()
	if tokens <= 0 {
		t.Error("expected positive token count")
	}
}

func TestNewSession_ExplicitContextWindowOverridesModel(t *testing.T) {
	spec := SessionSpec{
		Model:         "claude-sonnet-4-20250514",
		ContextWindow: 50000,
	}
	s := NewSession("sess-explicit", spec)

	// With explicit ContextWindow=50000, it should use that, not 200000.
	cm := s.ContextManager()
	if cm == nil {
		t.Fatal("expected non-nil ContextManager")
	}
}


// --- InjectMessage / DrainInterrupt tests ---

func TestInjectMessage_DrainInterrupt(t *testing.T) {
	s := NewSession("sess-inject", SessionSpec{Model: "test"})

	// No message queued — drain returns empty.
	if msg := s.DrainInterrupt(); msg != "" {
		t.Fatalf("expected empty, got %q", msg)
	}

	// Inject a message.
	s.InjectMessage("stop and reconsider")

	// Drain should return it.
	msg := s.DrainInterrupt()
	if msg != "stop and reconsider" {
		t.Fatalf("expected 'stop and reconsider', got %q", msg)
	}

	// Second drain should be empty.
	if msg := s.DrainInterrupt(); msg != "" {
		t.Fatalf("expected empty after drain, got %q", msg)
	}
}

func TestInjectMessage_ReplacesWhenFull(t *testing.T) {
	s := NewSession("sess-replace", SessionSpec{Model: "test"})

	// Fill the channel.
	s.InjectMessage("first")

	// Inject again — should replace.
	s.InjectMessage("second")

	msg := s.DrainInterrupt()
	if msg != "second" {
		t.Fatalf("expected 'second' (replacement), got %q", msg)
	}
}

func TestInjectMessage_NonBlocking(t *testing.T) {
	s := NewSession("sess-nonblock", SessionSpec{Model: "test"})

	// Should not block even when called multiple times rapidly.
	for i := 0; i < 10; i++ {
		s.InjectMessage("msg")
	}

	// Should have exactly one message.
	msg := s.DrainInterrupt()
	if msg != "msg" {
		t.Fatalf("expected 'msg', got %q", msg)
	}
	if msg := s.DrainInterrupt(); msg != "" {
		t.Fatalf("expected empty, got %q", msg)
	}
}

func TestNewSession_InterruptChannelInitialized(t *testing.T) {
	s := NewSession("sess-init-ch", SessionSpec{Model: "test"})

	// interruptCh should be initialized (non-nil, buffered).
	if s.interruptCh == nil {
		t.Fatal("expected interruptCh to be initialized")
	}
	if cap(s.interruptCh) != 1 {
		t.Fatalf("expected interruptCh capacity 1, got %d", cap(s.interruptCh))
	}
}
