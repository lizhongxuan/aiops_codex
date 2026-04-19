package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func TestRegisterDefaultToolHandlersRegistersUnifiedShellCommandTool(t *testing.T) {
	app := &App{toolHandlerRegistry: NewToolHandlerRegistry()}

	app.registerDefaultToolHandlers()

	desc, unified, ok := app.toolHandlerRegistry.LookupUnified(shellCommandToolName)
	if !ok || unified == nil {
		t.Fatalf("expected unified tool %q to be registered", shellCommandToolName)
	}
	if desc.Kind != "unified" {
		t.Fatalf("expected %q descriptor kind unified, got %#v", shellCommandToolName, desc)
	}
	if unified.Name() != shellCommandToolName {
		t.Fatalf("expected unified tool name %q, got %q", shellCommandToolName, unified.Name())
	}
}

func TestShellCommandUnifiedToolUsesPromptRegistryDescriptions(t *testing.T) {
	tool := shellCommandUnifiedTool{}

	if got := tool.Description(ToolDescriptionContext{}); got != toolPromptDescription(shellCommandToolName) {
		t.Fatalf("expected shared prompt description, got %q", got)
	}
	if got := tool.Description(ToolDescriptionContext{HostID: model.ServerLocalHostID}); got != localToolPromptDescription(shellCommandToolName) {
		t.Fatalf("expected local prompt description, got %q", got)
	}
	if got := tool.Description(ToolDescriptionContext{HostID: "linux-01"}); got != remoteToolPromptDescription(shellCommandToolName) {
		t.Fatalf("expected remote prompt description, got %q", got)
	}
}

func TestRemoteDynamicToolReadOnlyShellCommandDoesNotCreateApprovalAndProjectsDisplay(t *testing.T) {
	app := newRemoteDynamicToolPolicyApp(t, "sess-shell-readonly-policy", "linux-shell-01")
	responded := make(chan any, 1)

	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		if rawID != "raw-shell-readonly-policy" {
			t.Fatalf("unexpected raw id %q", rawID)
		}
		responded <- result
		return nil
	}

	stream := &dynamicToolPolicyAgentStream{
		onSend: func(msg *agentrpc.Envelope) error {
			if msg.Kind != "exec/start" || msg.ExecStart == nil {
				t.Fatalf("expected exec/start envelope, got %#v", msg)
			}
			if !msg.ExecStart.Readonly {
				t.Fatalf("expected readonly exec start")
			}
			if msg.ExecStart.Command != "uptime" {
				t.Fatalf("expected uptime command, got %q", msg.ExecStart.Command)
			}
			app.handleAgentExecOutput("linux-shell-01", &agentrpc.ExecOutput{
				ExecID: msg.ExecStart.ExecID,
				Stream: "stdout",
				Data:   "load average: 0.20 0.15 0.10\n",
			})
			app.handleAgentExecExit("linux-shell-01", &agentrpc.ExecExit{
				ExecID:   msg.ExecStart.ExecID,
				ExitCode: 0,
				Status:   "completed",
				Stdout:   "load average: 0.20 0.15 0.10\n",
			})
			return nil
		},
	}
	app.setAgentConnection("linux-shell-01", &agentConnection{hostID: "linux-shell-01", stream: stream})

	app.handleDynamicToolCall("raw-shell-readonly-policy", map[string]any{
		"threadId": "thread-sess-shell-readonly-policy",
		"turnId":   "turn-sess-shell-readonly-policy",
		"callId":   "call-shell-readonly-policy",
		"tool":     shellCommandToolName,
		"arguments": map[string]any{
			"host":    "linux-shell-01",
			"command": "uptime",
			"reason":  "check load",
		},
	})

	select {
	case result := <-responded:
		payload, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected tool response map, got %#v", result)
		}
		if payload["success"] != true {
			t.Fatalf("expected shell_command readonly query to succeed, got %#v", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for shell_command readonly response")
	}

	session := app.store.Session("sess-shell-readonly-policy")
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if len(session.Approvals) != 0 {
		t.Fatalf("expected no approvals for readonly shell_command, got %#v", session.Approvals)
	}
	card := app.cardByID("sess-shell-readonly-policy", dynamicToolCardID("call-shell-readonly-policy"))
	if card == nil {
		t.Fatalf("expected command card to exist")
	}
	if card.Type != "CommandCard" || card.Status != "completed" {
		t.Fatalf("expected completed CommandCard, got %#v", card)
	}
	display := toolProjectionDisplayMapFromDetail(card.Detail)
	if len(display) == 0 {
		t.Fatalf("expected structured display on command card, got %#v", card.Detail)
	}
	if got := getStringAny(display, "summary"); got == "" || !strings.Contains(got, "uptime") {
		t.Fatalf("expected display summary to mention command, got %#v", display)
	}
	blocks, ok := display["blocks"].([]map[string]any)
	if !ok || len(blocks) < 3 {
		t.Fatalf("expected command/result/text blocks, got %#v", display["blocks"])
	}
	if getStringAny(blocks[0], "kind") != ToolDisplayBlockCommand {
		t.Fatalf("expected first block command, got %#v", blocks)
	}
	if getStringAny(blocks[1], "kind") != ToolDisplayBlockResultStats {
		t.Fatalf("expected second block result_stats, got %#v", blocks)
	}
	if getStringAny(blocks[2], "kind") != ToolDisplayBlockText {
		t.Fatalf("expected third block text, got %#v", blocks)
	}
}

func TestRemoteDynamicToolMutationShellCommandCreatesPendingApproval(t *testing.T) {
	app := newRemoteDynamicToolPolicyApp(t, "sess-shell-command-policy", "linux-shell-02")
	responded := make(chan any, 1)

	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		responded <- map[string]any{
			"rawID":  rawID,
			"result": result,
		}
		return nil
	}

	app.handleDynamicToolCall("raw-shell-command-policy", map[string]any{
		"threadId": "thread-sess-shell-command-policy",
		"turnId":   "turn-sess-shell-command-policy",
		"callId":   "call-shell-command-policy",
		"tool":     shellCommandToolName,
		"arguments": map[string]any{
			"host":    "linux-shell-02",
			"command": "systemctl restart nginx",
			"cwd":     "/etc/nginx",
			"reason":  "restart nginx",
		},
	})

	select {
	case got := <-responded:
		t.Fatalf("expected no immediate tool response for pending approval, got %#v", got)
	default:
	}

	session := app.store.Session("sess-shell-command-policy")
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if len(session.Approvals) != 1 {
		t.Fatalf("expected one pending approval, got %#v", session.Approvals)
	}
	var approval model.ApprovalRequest
	for _, item := range session.Approvals {
		approval = item
		break
	}
	if approval.Status != "pending" {
		t.Fatalf("expected pending approval, got %#v", approval)
	}
	card := app.cardByID("sess-shell-command-policy", dynamicToolCardID("call-shell-command-policy"))
	if card == nil {
		t.Fatal("expected shell_command approval card")
	}
	if card.Type != "CommandApprovalCard" || card.Status != "pending" {
		t.Fatalf("unexpected pending approval card: %#v", card)
	}
	item := app.store.Item("sess-shell-command-policy", dynamicToolCardID("call-shell-command-policy"))
	if got := getStringAny(item, "tool"); got != shellCommandToolName {
		t.Fatalf("expected remembered tool %q, got %#v", shellCommandToolName, item)
	}
}
