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
		t.Fatal("prompt should contain static identity section")
	}
}

func TestBuildSystemPromptMinimal(t *testing.T) {
	spec := SessionSpec{
		Model: "test-model",
	}

	prompt := BuildSystemPrompt(spec)

	// Should still have the static identity section.
	if !strings.Contains(prompt, "main agent") {
		t.Fatal("prompt should contain static identity even with minimal spec")
	}
	// Should not contain approval or sandbox sections.
	if strings.Contains(prompt, "Approval policy:") {
		t.Fatal("prompt should not contain approval policy when not set")
	}
	if strings.Contains(prompt, "Sandbox mode:") {
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
