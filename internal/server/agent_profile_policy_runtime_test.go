package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func TestLocalCommandApprovalBlockedByDisabledCapability(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-profile-local-blocked"
	threadID := "thread-profile-local-blocked"
	app.store.EnsureSession(sessionID)
	app.store.SetThread(sessionID, threadID)

	profile := app.mainAgentProfile()
	profile.CapabilityPermissions.CommandExecution = model.AgentCapabilityDisabled
	app.store.UpsertAgentProfile(profile)

	responded := make(chan any, 1)
	app.codexRespondFunc = func(_ context.Context, _ string, result any) error {
		responded <- result
		return nil
	}

	payload := map[string]any{
		"threadId": threadID,
		"turnId":   "turn-profile-local-blocked",
		"itemId":   "cmd-profile-local-blocked",
		"command":  "uptime",
		"cwd":      "/tmp",
		"reason":   "check load",
	}
	app.handleLocalCommandApprovalRequest("1", payload)

	select {
	case got := <-responded:
		payload, ok := got.(map[string]any)
		if !ok || payload["decision"] != "decline" {
			t.Fatalf("expected decline response, got %#v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for decline response")
	}

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session")
	}
	if session.Runtime.Turn.Phase != "thinking" {
		t.Fatalf("expected phase thinking, got %q", session.Runtime.Turn.Phase)
	}
	found := false
	for _, card := range session.Cards {
		if card.Type == "ErrorCard" && strings.Contains(card.Message, "commandExecution capability is disabled") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected profile block error card, got %#v", session.Cards)
	}
}

func TestCapabilityDisabledOverridesAllowedCommandCategory(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-profile-local-disabled-priority"
	threadID := "thread-profile-local-disabled-priority"
	app.store.EnsureSession(sessionID)
	app.store.SetThread(sessionID, threadID)

	profile := app.mainAgentProfile()
	profile.CapabilityPermissions.CommandExecution = model.AgentCapabilityDisabled
	profile.CommandPermissions.CategoryPolicies["system_inspection"] = model.AgentPermissionModeAllow
	app.store.UpsertAgentProfile(profile)

	responded := make(chan any, 1)
	app.codexRespondFunc = func(_ context.Context, _ string, result any) error {
		responded <- result
		return nil
	}

	payload := map[string]any{
		"threadId": threadID,
		"turnId":   "turn-profile-local-disabled-priority",
		"itemId":   "cmd-profile-local-disabled-priority",
		"command":  "uptime",
		"cwd":      "/tmp",
		"reason":   "check load",
	}
	app.handleLocalCommandApprovalRequest("1b", payload)

	select {
	case got := <-responded:
		payload, ok := got.(map[string]any)
		if !ok || payload["decision"] != "decline" {
			t.Fatalf("expected decline response, got %#v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for decline response")
	}

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session")
	}
	found := false
	for _, card := range session.Cards {
		if card.Type == "ErrorCard" && strings.Contains(card.Message, "commandExecution capability is disabled") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected disabled capability to win over allow policy, got %#v", session.Cards)
	}
}

func TestLocalCommandApprovalAutoAcceptedWhenAllowedByProfile(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-profile-local-auto"
	threadID := "thread-profile-local-auto"
	app.store.EnsureSession(sessionID)
	app.store.SetThread(sessionID, threadID)

	profile := app.mainAgentProfile()
	profile.CapabilityPermissions.CommandExecution = model.AgentCapabilityEnabled
	profile.CommandPermissions.DefaultMode = model.AgentPermissionModeApprovalRequired
	profile.CommandPermissions.CategoryPolicies["system_inspection"] = model.AgentPermissionModeAllow
	app.store.UpsertAgentProfile(profile)

	responded := make(chan any, 1)
	app.codexRespondFunc = func(_ context.Context, _ string, result any) error {
		responded <- result
		return nil
	}

	payload := map[string]any{
		"threadId": threadID,
		"turnId":   "turn-profile-local-auto",
		"itemId":   "cmd-profile-local-auto",
		"command":  "uptime",
		"cwd":      "/tmp",
		"reason":   "check load",
	}
	app.handleLocalCommandApprovalRequest("2", payload)

	select {
	case got := <-responded:
		payload, ok := got.(map[string]any)
		if !ok || payload["decision"] != "accept" {
			t.Fatalf("expected accept response, got %#v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for accept response")
	}

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session")
	}
	if session.Runtime.Turn.Phase != "executing" {
		t.Fatalf("expected phase executing, got %q", session.Runtime.Turn.Phase)
	}
	if len(session.Approvals) != 1 {
		t.Fatalf("expected one resolved approval, got %#v", session.Approvals)
	}
	var approval model.ApprovalRequest
	for _, item := range session.Approvals {
		approval = item
		break
	}
	if approval.Status != "accepted_by_profile_auto" {
		t.Fatalf("expected auto accepted approval, got %#v", approval.Status)
	}
}

func TestRemoteMutationCommandAutoApprovedWhenAllowedByProfile(t *testing.T) {
	app := newRemoteDynamicToolPolicyApp(t, "sess-profile-remote-auto", "linux-auto")
	profile := app.mainAgentProfile()
	profile.CapabilityPermissions.CommandExecution = model.AgentCapabilityEnabled
	profile.CommandPermissions.CategoryPolicies["service_mutation"] = model.AgentPermissionModeAllow
	app.store.UpsertAgentProfile(profile)
	hostProfile, ok := app.store.AgentProfile(string(model.AgentProfileTypeHostAgentDefault))
	if !ok {
		t.Fatalf("expected host-agent-default profile")
	}
	hostProfile.CapabilityPermissions.CommandExecution = model.AgentCapabilityEnabled
	hostProfile.CommandPermissions.CategoryPolicies["service_mutation"] = model.AgentPermissionModeAllow
	app.store.UpsertAgentProfile(hostProfile)

	responded := make(chan any, 1)
	app.codexRespondFunc = func(_ context.Context, _ string, result any) error {
		responded <- result
		return nil
	}

	stream := &dynamicToolPolicyAgentStream{
		onSend: func(msg *agentrpc.Envelope) error {
			if msg.Kind != "exec/start" || msg.ExecStart == nil {
				t.Fatalf("expected exec/start envelope, got %#v", msg)
			}
			app.handleAgentExecExit("linux-auto", &agentrpc.ExecExit{
				ExecID:   msg.ExecStart.ExecID,
				ExitCode: 0,
				Status:   "completed",
				Stdout:   "ok\n",
			})
			return nil
		},
	}
	app.setAgentConnection("linux-auto", &agentConnection{hostID: "linux-auto", stream: stream})

	app.handleDynamicToolCall("raw-profile-remote-auto", map[string]any{
		"threadId": "thread-sess-profile-remote-auto",
		"turnId":   "turn-sess-profile-remote-auto",
		"callId":   "call-profile-remote-auto",
		"tool":     "execute_system_mutation",
		"arguments": map[string]any{
			"host":    "linux-auto",
			"mode":    "command",
			"command": "systemctl restart nginx",
			"cwd":     "/etc/nginx",
			"reason":  "restart service",
		},
	})

	select {
	case got := <-responded:
		payload, ok := got.(map[string]any)
		if !ok || payload["success"] != true {
			t.Fatalf("expected successful remote auto-approved mutation, got %#v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for remote command response")
	}

	session := app.store.Session("sess-profile-remote-auto")
	if session == nil {
		t.Fatalf("expected session")
	}
	if len(session.Approvals) != 1 {
		t.Fatalf("expected one resolved approval, got %#v", session.Approvals)
	}
}

func TestRemoteMutationCommandBlockedByPolicy(t *testing.T) {
	app := newRemoteDynamicToolPolicyApp(t, "sess-profile-remote-deny", "linux-deny")
	responded := make(chan any, 1)
	app.codexRespondFunc = func(_ context.Context, _ string, result any) error {
		responded <- result
		return nil
	}

	app.handleDynamicToolCall("raw-profile-remote-deny", map[string]any{
		"threadId": "thread-sess-profile-remote-deny",
		"turnId":   "turn-sess-profile-remote-deny",
		"callId":   "call-profile-remote-deny",
		"tool":     "execute_system_mutation",
		"arguments": map[string]any{
			"host":    "linux-deny",
			"mode":    "command",
			"command": "apt install nginx -y",
			"cwd":     "/tmp",
			"reason":  "install nginx",
		},
	})

	select {
	case got := <-responded:
		payload, ok := got.(map[string]any)
		if !ok || payload["success"] != false {
			t.Fatalf("expected failed tool response, got %#v", got)
		}
		items, ok := payload["contentItems"].([]map[string]any)
		if !ok || len(items) == 0 {
			t.Fatalf("expected tool response content items, got %#v", payload)
		}
		text, _ := items[0]["text"].(string)
		if !strings.Contains(text, "package_mutation") {
			t.Fatalf("expected package mutation denial, got %#v", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for remote deny response")
	}

	session := app.store.Session("sess-profile-remote-deny")
	if session == nil {
		t.Fatalf("expected session")
	}
	if len(session.Approvals) != 0 {
		t.Fatalf("expected no approvals when command is denied, got %#v", session.Approvals)
	}
}

func TestRemoteMutationUsesMostRestrictiveHostAgentPolicy(t *testing.T) {
	app := newRemoteDynamicToolPolicyApp(t, "sess-profile-remote-host-policy", "linux-host-policy")
	mainProfile := app.mainAgentProfile()
	mainProfile.CapabilityPermissions.CommandExecution = model.AgentCapabilityEnabled
	mainProfile.CommandPermissions.CategoryPolicies["service_mutation"] = model.AgentPermissionModeAllow
	app.store.UpsertAgentProfile(mainProfile)

	hostProfile, ok := app.store.AgentProfile(string(model.AgentProfileTypeHostAgentDefault))
	if !ok {
		t.Fatalf("expected host-agent-default profile")
	}
	hostProfile.CapabilityPermissions.CommandExecution = model.AgentCapabilityEnabled
	hostProfile.CommandPermissions.CategoryPolicies["service_mutation"] = model.AgentPermissionModeDeny
	app.store.UpsertAgentProfile(hostProfile)

	responded := make(chan any, 1)
	app.codexRespondFunc = func(_ context.Context, _ string, result any) error {
		responded <- result
		return nil
	}

	app.handleDynamicToolCall("raw-profile-remote-host-policy", map[string]any{
		"threadId": "thread-sess-profile-remote-host-policy",
		"turnId":   "turn-sess-profile-remote-host-policy",
		"callId":   "call-profile-remote-host-policy",
		"tool":     "execute_system_mutation",
		"arguments": map[string]any{
			"host":    "linux-host-policy",
			"mode":    "command",
			"command": "systemctl restart nginx",
			"cwd":     "/etc/nginx",
			"reason":  "restart service",
		},
	})

	select {
	case got := <-responded:
		payload, ok := got.(map[string]any)
		if !ok || payload["success"] != false {
			t.Fatalf("expected failed tool response, got %#v", got)
		}
		items, ok := payload["contentItems"].([]map[string]any)
		if !ok || len(items) == 0 {
			t.Fatalf("expected tool response content items, got %#v", payload)
		}
		text, _ := items[0]["text"].(string)
		if !strings.Contains(text, "effective agent profile") || !strings.Contains(text, "service_mutation") {
			t.Fatalf("expected host policy denial to win, got %#v", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for remote host policy response")
	}
}

func TestTerminalCreateBlockedByDisabledCapability(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-profile-terminal-disabled"
	app.store.EnsureSession(sessionID)

	profile := app.mainAgentProfile()
	profile.CapabilityPermissions.Terminal = model.AgentCapabilityDisabled
	app.store.UpsertAgentProfile(profile)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/terminal/sessions", strings.NewReader(`{"hostId":"server-local"}`))
	rec := httptest.NewRecorder()
	app.handleTerminalCreate(rec, req, sessionID)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRemoteTerminalBlockedByHostAgentDefaultCapability(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-profile-remote-terminal-disabled"
	app.store.EnsureSession(sessionID)
	app.store.UpsertHost(model.Host{
		ID:              "linux-term-disabled",
		Name:            "linux-term-disabled",
		Kind:            "inventory",
		Status:          "online",
		Executable:      true,
		TerminalCapable: true,
	})

	mainProfile := app.mainAgentProfile()
	mainProfile.CapabilityPermissions.Terminal = model.AgentCapabilityEnabled
	app.store.UpsertAgentProfile(mainProfile)

	hostProfile, ok := app.store.AgentProfile(string(model.AgentProfileTypeHostAgentDefault))
	if !ok {
		t.Fatalf("expected host-agent-default profile")
	}
	hostProfile.CapabilityPermissions.Terminal = model.AgentCapabilityDisabled
	app.store.UpsertAgentProfile(hostProfile)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/terminal/sessions", strings.NewReader(`{"hostId":"linux-term-disabled"}`))
	rec := httptest.NewRecorder()
	app.handleTerminalCreate(rec, req, sessionID)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "effective agent profile") {
		t.Fatalf("expected host-agent-default restriction message, got %s", rec.Body.String())
	}
}

func TestFilePreviewBlockedByDisabledCapability(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-profile-preview-disabled"
	app.store.EnsureSession(sessionID)

	profile := app.mainAgentProfile()
	profile.CapabilityPermissions.FileRead = model.AgentCapabilityDisabled
	app.store.UpsertAgentProfile(profile)

	dir := t.TempDir()
	target := filepath.Join(dir, "preview.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write preview file: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/files/preview?hostId=server-local&path="+target, nil)
	rec := httptest.NewRecorder()
	app.handleFilePreview(rec, req, sessionID)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRemoteReadFileBlockedByDisabledCapability(t *testing.T) {
	app := newRemoteDynamicToolPolicyApp(t, "sess-profile-remote-read-disabled", "linux-read-disabled")
	profile := app.mainAgentProfile()
	profile.CapabilityPermissions.FileRead = model.AgentCapabilityDisabled
	app.store.UpsertAgentProfile(profile)

	responded := make(chan any, 1)
	app.codexRespondFunc = func(_ context.Context, _ string, result any) error {
		responded <- result
		return nil
	}

	app.handleDynamicToolCall("raw-profile-remote-read-disabled", map[string]any{
		"threadId": "thread-sess-profile-remote-read-disabled",
		"turnId":   "turn-sess-profile-remote-read-disabled",
		"callId":   "call-profile-remote-read-disabled",
		"tool":     "read_remote_file",
		"arguments": map[string]any{
			"host":   "linux-read-disabled",
			"path":   "/etc/nginx/nginx.conf",
			"reason": "inspect config",
		},
	})

	select {
	case got := <-responded:
		payload, ok := got.(map[string]any)
		if !ok || payload["success"] != false {
			t.Fatalf("expected failed tool response, got %#v", got)
		}
		items, ok := payload["contentItems"].([]map[string]any)
		if !ok || len(items) == 0 {
			t.Fatalf("expected content items, got %#v", payload)
		}
		text, _ := items[0]["text"].(string)
		if !strings.Contains(text, "fileRead capability is disabled") {
			t.Fatalf("expected fileRead disabled message, got %#v", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for tool response")
	}
}
