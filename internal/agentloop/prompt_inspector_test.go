package agentloop

import (
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

func TestPromptInspector_LogPrompt_DebugEnabled(t *testing.T) {
	inspector := NewPromptInspector(true)

	req := bifrost.ChatRequest{
		Model: "gpt-4o",
		Messages: []bifrost.Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
		},
	}

	inspector.LogPrompt(req)

	logs := inspector.Logs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}
	if logs[0].Model != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %s", logs[0].Model)
	}
	if logs[0].TokenCount == 0 {
		t.Error("expected non-zero token count")
	}
}

func TestPromptInspector_LogPrompt_DebugDisabled(t *testing.T) {
	inspector := NewPromptInspector(false)

	req := bifrost.ChatRequest{
		Model:    "gpt-4o",
		Messages: []bifrost.Message{{Role: "user", Content: "Hello"}},
	}

	inspector.LogPrompt(req)

	logs := inspector.Logs()
	if len(logs) != 0 {
		t.Errorf("expected 0 logs when debug disabled, got %d", len(logs))
	}
}

func TestPromptInspector_InspectCurrentPrompt(t *testing.T) {
	session := NewSession("test", SessionSpec{
		Cwd:                   "/tmp",
		DeveloperInstructions: "Be helpful",
	})
	session.ContextManager().AppendUser("What is Go?")

	inspector := NewPromptInspector(true)
	state := inspector.InspectCurrentPrompt(session)

	if state.SystemPrompt == "" {
		t.Error("expected non-empty system prompt")
	}
	if len(state.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(state.Messages))
	}
	if state.TokenCount == 0 {
		t.Error("expected non-zero token count")
	}
}

func TestPromptInspector_InspectCurrentPrompt_NilSession(t *testing.T) {
	inspector := NewPromptInspector(true)
	state := inspector.InspectCurrentPrompt(nil)

	if state.SystemPrompt != "" {
		t.Error("expected empty state for nil session")
	}
}

func TestPromptInspector_DryRun(t *testing.T) {
	session := NewSession("test", SessionSpec{Cwd: "/tmp"})
	session.ContextManager().AppendUser("Test prompt")

	inspector := NewPromptInspector(true)
	state, err := inspector.DryRun(session)
	if err != nil {
		t.Fatalf("DryRun failed: %v", err)
	}

	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if state.SystemPrompt == "" {
		t.Error("expected system prompt in dry run")
	}
}

func TestPromptInspector_SetDebugMode(t *testing.T) {
	inspector := NewPromptInspector(false)

	if inspector.IsDebugMode() {
		t.Error("expected debug mode off initially")
	}

	inspector.SetDebugMode(true)
	if !inspector.IsDebugMode() {
		t.Error("expected debug mode on after SetDebugMode(true)")
	}
}

func TestPromptInspector_ClearLogs(t *testing.T) {
	inspector := NewPromptInspector(true)
	inspector.LogPrompt(bifrost.ChatRequest{
		Model:    "test",
		Messages: []bifrost.Message{{Role: "user", Content: "hi"}},
	})

	inspector.ClearLogs()
	if len(inspector.Logs()) != 0 {
		t.Error("expected 0 logs after clear")
	}
}
