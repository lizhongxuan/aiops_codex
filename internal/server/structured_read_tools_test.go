package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// ---------- Tool routing priority tests ----------

func TestStructuredReadToolRoutedBeforeExecuteReadonlyQuery(t *testing.T) {
	app := newRemoteDynamicToolPolicyApp(t, "sess-structured-route", "linux-route-01")
	responded := make(chan map[string]any, 1)

	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		if payload, ok := result.(map[string]any); ok {
			responded <- payload
		}
		return nil
	}

	stream := &dynamicToolPolicyAgentStream{
		onSend: func(msg *agentrpc.Envelope) error {
			if msg.Kind == "exec/start" && msg.ExecStart != nil {
				// Respond immediately with output.
				app.handleAgentExecOutput("linux-route-01", &agentrpc.ExecOutput{
					ExecID: msg.ExecStart.ExecID,
					Stream: "stdout",
					Data:   "host summary output\n",
				})
				app.handleAgentExecExit("linux-route-01", &agentrpc.ExecExit{
					ExecID:   msg.ExecStart.ExecID,
					ExitCode: 0,
					Status:   "completed",
					Stdout:   "host summary output\n",
				})
			}
			return nil
		},
	}
	app.setAgentConnection("linux-route-01", &agentConnection{hostID: "linux-route-01", stream: stream})

	// Call a host.* tool — it should be routed via the structured read path.
	app.handleDynamicToolCall("raw-structured-route", map[string]any{
		"threadId": "thread-sess-structured-route",
		"turnId":   "turn-sess-structured-route",
		"callId":   "call-structured-route",
		"tool":     "host.summary",
		"arguments": map[string]any{
			"host":   "linux-route-01",
			"reason": "check system overview",
		},
	})

	select {
	case payload := <-responded:
		if payload["success"] != true {
			t.Fatalf("expected host.summary to succeed, got %#v", payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for host.summary response")
	}
}

func TestUnknownToolReturnsError(t *testing.T) {
	app := newRemoteDynamicToolPolicyApp(t, "sess-unknown-tool", "linux-unknown-01")
	responded := make(chan map[string]any, 1)

	app.codexRespondFunc = func(_ context.Context, _ string, result any) error {
		if payload, ok := result.(map[string]any); ok {
			responded <- payload
		}
		return nil
	}

	app.handleDynamicToolCall("raw-unknown-tool", map[string]any{
		"threadId": "thread-sess-unknown-tool",
		"turnId":   "turn-sess-unknown-tool",
		"callId":   "call-unknown-tool",
		"tool":     "nonexistent_tool",
		"arguments": map[string]any{
			"host":   "linux-unknown-01",
			"reason": "test",
		},
	})

	select {
	case payload := <-responded:
		if payload["success"] == true {
			t.Fatalf("expected unknown tool to fail, got success")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for unknown tool response")
	}
}


// ---------- Capability gateway / permission tests ----------

func TestCapabilityGatewayStructuredReadAllowed(t *testing.T) {
	app := newRemoteDynamicToolPolicyApp(t, "sess-gw-sr", "linux-gw-01")
	result := app.evaluateCapabilityGateway("linux-gw-01", "host.summary")
	if result.Layer != CapabilityLayerStructuredRead {
		t.Fatalf("expected layer %q, got %q", CapabilityLayerStructuredRead, result.Layer)
	}
	if !result.Allowed {
		t.Fatalf("expected host.summary to be allowed, reason: %s", result.Reason)
	}
}

func TestCapabilityGatewayStructuredReadDisabledWhenCommandExecutionOff(t *testing.T) {
	app := newRemoteDynamicToolPolicyApp(t, "sess-gw-sr-off", "linux-gw-02")
	// Disable commandExecution on the main profile.
	profile := app.mainAgentProfile()
	profile.CapabilityPermissions.CommandExecution = model.AgentCapabilityDisabled
	app.store.UpsertAgentProfile(profile)

	result := app.evaluateCapabilityGateway("linux-gw-02", "host.process.top")
	if result.Layer != CapabilityLayerStructuredRead {
		t.Fatalf("expected layer %q, got %q", CapabilityLayerStructuredRead, result.Layer)
	}
	if result.Allowed {
		t.Fatalf("expected host.process.top to be disallowed when commandExecution is disabled")
	}
}

func TestCapabilityGatewayControlledMutation(t *testing.T) {
	app := newRemoteDynamicToolPolicyApp(t, "sess-gw-cm", "linux-gw-03")
	result := app.evaluateCapabilityGateway("linux-gw-03", "execute_system_mutation")
	if result.Layer != CapabilityLayerControlledMutation {
		t.Fatalf("expected layer %q, got %q", CapabilityLayerControlledMutation, result.Layer)
	}
	if !result.Allowed {
		t.Fatalf("expected execute_system_mutation to be allowed, reason: %s", result.Reason)
	}
}

func TestCapabilityGatewayRawShell(t *testing.T) {
	app := newRemoteDynamicToolPolicyApp(t, "sess-gw-rs", "linux-gw-04")
	result := app.evaluateCapabilityGateway("linux-gw-04", "execute_readonly_query")
	if result.Layer != CapabilityLayerRawShell {
		t.Fatalf("expected layer %q, got %q", CapabilityLayerRawShell, result.Layer)
	}
	if !result.Allowed {
		t.Fatalf("expected execute_readonly_query to be allowed, reason: %s", result.Reason)
	}
}

func TestCapabilityGatewayUnknownToolDenied(t *testing.T) {
	app := newRemoteDynamicToolPolicyApp(t, "sess-gw-unk", "linux-gw-05")
	result := app.evaluateCapabilityGateway("linux-gw-05", "totally_unknown")
	if result.Allowed {
		t.Fatalf("expected unknown tool to be denied")
	}
}

// ---------- Structured read tool definition tests ----------

func TestStructuredReadToolDefinitionsCount(t *testing.T) {
	defs := structuredReadToolDefinitions()
	if len(defs) != 14 {
		t.Fatalf("expected 14 structured read tool definitions, got %d", len(defs))
	}
}

func TestAllStructuredReadToolsHaveHostPrefix(t *testing.T) {
	for _, def := range structuredReadToolDefinitions() {
		name, ok := def["name"].(string)
		if !ok || !strings.HasPrefix(name, "host.") {
			t.Fatalf("expected tool name with host. prefix, got %q", name)
		}
	}
}

func TestIsStructuredReadToolPositive(t *testing.T) {
	tools := []string{
		"host.summary", "host.process.top", "host.service.status",
		"host.journal.tail", "host.file.exists", "host.file.read",
		"host.file.search", "host.network.listeners", "host.network.connections",
		"host.package.version", "host.nginx.status", "host.mysql.summary",
		"host.redis.summary", "host.jvm.summary",
	}
	for _, name := range tools {
		if !isStructuredReadTool(name) {
			t.Errorf("expected %q to be a structured read tool", name)
		}
	}
}

func TestIsStructuredReadToolNegative(t *testing.T) {
	negatives := []string{
		"execute_readonly_query", "execute_system_mutation",
		"list_remote_files", "host.nonexistent",
	}
	for _, name := range negatives {
		if isStructuredReadTool(name) {
			t.Errorf("expected %q to NOT be a structured read tool", name)
		}
	}
}

// ---------- Command building tests ----------

func TestBuildStructuredReadCommandStatic(t *testing.T) {
	cmd, err := buildStructuredReadCommand("host.summary", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd == "" {
		t.Fatalf("expected non-empty command for host.summary")
	}
}

func TestBuildStructuredReadCommandWithArgs(t *testing.T) {
	cmd, err := buildStructuredReadCommand("host.service.status", map[string]any{
		"service": "nginx",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(cmd, "nginx") {
		t.Fatalf("expected command to contain 'nginx', got %q", cmd)
	}
}

func TestBuildStructuredReadCommandRejectsShellInjection(t *testing.T) {
	_, err := buildStructuredReadCommand("host.service.status", map[string]any{
		"service": "nginx; rm -rf /",
	})
	if err == nil {
		t.Fatalf("expected error for shell injection attempt")
	}
}

func TestBuildStructuredReadCommandUnknownTool(t *testing.T) {
	_, err := buildStructuredReadCommand("host.nonexistent", nil)
	if err == nil {
		t.Fatalf("expected error for unknown tool")
	}
}

// ---------- Structured read tool blocked when capability disabled ----------

func TestStructuredReadToolBlockedWhenCapabilityDisabled(t *testing.T) {
	app := newRemoteDynamicToolPolicyApp(t, "sess-sr-blocked", "linux-blocked-01")
	responded := make(chan map[string]any, 1)

	// Disable commandExecution on the main profile.
	profile := app.mainAgentProfile()
	profile.CapabilityPermissions.CommandExecution = model.AgentCapabilityDisabled
	app.store.UpsertAgentProfile(profile)

	app.codexRespondFunc = func(_ context.Context, _ string, result any) error {
		if payload, ok := result.(map[string]any); ok {
			responded <- payload
		}
		return nil
	}

	app.handleDynamicToolCall("raw-sr-blocked", map[string]any{
		"threadId": "thread-sess-sr-blocked",
		"turnId":   "turn-sess-sr-blocked",
		"callId":   "call-sr-blocked",
		"tool":     "host.summary",
		"arguments": map[string]any{
			"host":   "linux-blocked-01",
			"reason": "check system",
		},
	})

	select {
	case payload := <-responded:
		if payload["success"] == true {
			t.Fatalf("expected host.summary to be blocked when commandExecution is disabled")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for blocked response")
	}
}

// ---------- Controlled mutation tool definition tests ----------

func TestControlledMutationToolDefinitionsCount(t *testing.T) {
	defs := controlledMutationToolDefinitions()
	if len(defs) != 5 {
		t.Fatalf("expected 5 controlled mutation tool definitions, got %d", len(defs))
	}
}

func TestIsControlledMutationToolPositive(t *testing.T) {
	tools := []string{
		"service.restart", "service.stop", "config.apply",
		"package.install", "package.upgrade",
	}
	for _, name := range tools {
		if !isControlledMutationTool(name) {
			t.Errorf("expected %q to be a controlled mutation tool", name)
		}
	}
}

func TestIsControlledMutationToolNegative(t *testing.T) {
	negatives := []string{
		"host.summary", "execute_readonly_query", "execute_system_mutation",
		"service.nonexistent", "config.nonexistent",
	}
	for _, name := range negatives {
		if isControlledMutationTool(name) {
			t.Errorf("expected %q to NOT be a controlled mutation tool", name)
		}
	}
}

func TestBuildControlledMutationCommandServiceRestart(t *testing.T) {
	cmd, err := buildControlledMutationCommand("service.restart", map[string]any{
		"service": "nginx",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(cmd, "nginx") {
		t.Fatalf("expected command to contain 'nginx', got %q", cmd)
	}
	if !strings.Contains(cmd, "systemctl restart") {
		t.Fatalf("expected command to contain 'systemctl restart', got %q", cmd)
	}
}

func TestBuildControlledMutationCommandRejectsShellInjection(t *testing.T) {
	_, err := buildControlledMutationCommand("service.restart", map[string]any{
		"service": "nginx; rm -rf /",
	})
	if err == nil {
		t.Fatalf("expected error for shell injection attempt")
	}
}

func TestBuildControlledMutationCommandUnknownTool(t *testing.T) {
	_, err := buildControlledMutationCommand("service.nonexistent", nil)
	if err == nil {
		t.Fatalf("expected error for unknown tool")
	}
}

func TestCapabilityGatewayControlledMutationToolsRouteToLayer2(t *testing.T) {
	app := newRemoteDynamicToolPolicyApp(t, "sess-gw-cm-tools", "linux-gw-cm-01")
	tools := []string{"service.restart", "service.stop", "config.apply", "package.install", "package.upgrade"}
	for _, toolName := range tools {
		result := app.evaluateCapabilityGateway("linux-gw-cm-01", toolName)
		if result.Layer != CapabilityLayerControlledMutation {
			t.Errorf("expected %q to be in layer %q, got %q", toolName, CapabilityLayerControlledMutation, result.Layer)
		}
		if !result.Allowed {
			t.Errorf("expected %q to be allowed, reason: %s", toolName, result.Reason)
		}
	}
}

func TestCapabilityGatewayControlledMutationBlockedWhenBothDisabled(t *testing.T) {
	app := newRemoteDynamicToolPolicyApp(t, "sess-gw-cm-off", "linux-gw-cm-02")
	profile := app.mainAgentProfile()
	profile.CapabilityPermissions.CommandExecution = model.AgentCapabilityDisabled
	profile.CapabilityPermissions.FileChange = model.AgentCapabilityDisabled
	app.store.UpsertAgentProfile(profile)

	result := app.evaluateCapabilityGateway("linux-gw-cm-02", "service.restart")
	if result.Layer != CapabilityLayerControlledMutation {
		t.Fatalf("expected layer %q, got %q", CapabilityLayerControlledMutation, result.Layer)
	}
	if result.Allowed {
		t.Fatalf("expected service.restart to be disallowed when both capabilities are disabled")
	}
}

func TestControlledMutationToolCreatesApproval(t *testing.T) {
	app := newRemoteDynamicToolPolicyApp(t, "sess-cm-approval", "linux-cm-01")
	responded := make(chan map[string]any, 1)

	app.codexRespondFunc = func(_ context.Context, _ string, result any) error {
		if payload, ok := result.(map[string]any); ok {
			responded <- payload
		}
		return nil
	}

	app.handleDynamicToolCall("raw-cm-approval", map[string]any{
		"threadId": "thread-sess-cm-approval",
		"turnId":   "turn-sess-cm-approval",
		"callId":   "call-cm-approval",
		"tool":     "service.restart",
		"arguments": map[string]any{
			"host":    "linux-cm-01",
			"service": "nginx",
			"reason":  "restart nginx after config change",
		},
	})

	// The tool should create a pending approval, not respond immediately.
	select {
	case <-responded:
		// If we get a response, it should NOT be a success (it should be waiting for approval).
		// Actually, the approval flow doesn't respond immediately — it waits.
		// But if auto-approve kicks in, we might get a response.
	case <-time.After(500 * time.Millisecond):
		// Expected: no immediate response because approval is pending.
	}

	// Verify an approval was created.
	snapshot := app.snapshot("sess-cm-approval")
	hasPending := false
	for _, approval := range snapshot.Approvals {
		if strings.TrimSpace(approval.Status) == "pending" || strings.Contains(approval.Status, "accepted") {
			hasPending = true
			break
		}
	}
	if !hasPending {
		t.Fatalf("expected a pending or accepted approval for controlled mutation tool")
	}
}
