package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/store"
)

type stubToolApprovalCoordinator struct {
	autoApproveFn func(context.Context, ToolApprovalRequest) (ApprovalResolution, bool)
	requestFn     func(context.Context, ToolApprovalRequest) (ApprovalResolution, error)
}

func (s stubToolApprovalCoordinator) AutoApprove(ctx context.Context, req ToolApprovalRequest) (ApprovalResolution, bool) {
	if s.autoApproveFn == nil {
		return ApprovalResolution{}, false
	}
	return s.autoApproveFn(ctx, req)
}

func (s stubToolApprovalCoordinator) Request(ctx context.Context, req ToolApprovalRequest) (ApprovalResolution, error) {
	if s.requestFn == nil {
		return ApprovalResolution{}, nil
	}
	return s.requestFn(ctx, req)
}

func TestDynamicRemoteCommandApprovalCreatesPendingApprovalAndCard(t *testing.T) {
	app := newDynamicApprovalEventsTestApp(t)
	sessionID := "sess-dynamic-remote-command-pending"
	hostID := "linux-01"
	callID := "call-remote-command-pending"
	cardID := dynamicToolCardID(callID)

	app.store.EnsureSession(sessionID)
	app.store.SetSelectedHost(sessionID, hostID)
	app.store.SetThread(sessionID, "thread-"+sessionID)
	app.store.SetTurn(sessionID, "turn-"+sessionID)
	app.store.UpsertHost(model.Host{
		ID:         hostID,
		Name:       hostID,
		Kind:       "agent",
		Status:     "online",
		Executable: true,
	})

	app.requestRemoteCommandApproval(sessionID, hostID, "raw-remote-command-pending", dynamicToolCallParams{
		ThreadID: "thread-" + sessionID,
		TurnID:   "turn-" + sessionID,
		CallID:   callID,
		Tool:     "execute_system_mutation",
		Arguments: map[string]any{
			"host":    hostID,
			"mode":    "command",
			"command": "systemctl restart nginx",
			"cwd":     "/tmp",
			"reason":  "restart nginx",
		},
	}, execToolArgs{
		HostID:  hostID,
		Command: "systemctl restart nginx",
		Cwd:     "/tmp",
		Reason:  "restart nginx",
	}, false)

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatal("expected session to exist")
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
		t.Fatalf("expected pending remote command approval, got %#v", approval)
	}
	if approval.ItemID != cardID {
		t.Fatalf("expected approval item id %q, got %#v", cardID, approval)
	}
	if session.Runtime.Turn.Phase != "waiting_approval" {
		t.Fatalf("expected waiting_approval phase, got %q", session.Runtime.Turn.Phase)
	}
	card := app.cardByID(sessionID, cardID)
	if card == nil {
		t.Fatal("expected remote command approval card")
	}
	if card.Type != "CommandApprovalCard" || card.Status != "pending" {
		t.Fatalf("unexpected pending approval card: %#v", card)
	}
	if card.Approval == nil || card.Approval.RequestID != approval.ID {
		t.Fatalf("expected approval ref to point at %q, got %#v", approval.ID, card.Approval)
	}
}

func TestDynamicRemoteCommandApprovalUsesCoordinatorRequestForPendingApproval(t *testing.T) {
	app := newDynamicApprovalEventsTestApp(t)
	sessionID := "sess-dynamic-remote-command-coordinator"
	hostID := "linux-01"
	callID := "call-remote-command-coordinator"
	cardID := dynamicToolCardID(callID)

	app.store.EnsureSession(sessionID)
	app.store.SetSelectedHost(sessionID, hostID)
	app.store.SetThread(sessionID, "thread-"+sessionID)
	app.store.SetTurn(sessionID, "turn-"+sessionID)
	app.store.UpsertHost(model.Host{
		ID:         hostID,
		Name:       hostID,
		Kind:       "agent",
		Status:     "online",
		Executable: true,
	})
	app.toolApprovalCoordinator = stubToolApprovalCoordinator{
		requestFn: func(_ context.Context, req ToolApprovalRequest) (ApprovalResolution, error) {
			if req.ToolName != "execute_command" {
				t.Fatalf("expected execute_command tool name, got %#v", req)
			}
			return ApprovalResolution{
				ApprovalID:             "approval-from-coordinator",
				Status:                 ApprovalResolutionStatusPending,
				SessionID:              req.SessionID,
				HostID:                 req.HostID,
				ToolName:               req.ToolName,
				Reason:                 "manual approval required",
				RequiresManualApproval: true,
				RequestedAt:            time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
			}, nil
		},
	}

	app.requestRemoteCommandApproval(sessionID, hostID, "raw-remote-command-coordinator", dynamicToolCallParams{
		ThreadID: "thread-" + sessionID,
		TurnID:   "turn-" + sessionID,
		CallID:   callID,
		Tool:     "execute_system_mutation",
		Arguments: map[string]any{
			"host":    hostID,
			"mode":    "command",
			"command": "systemctl restart nginx",
			"cwd":     "/tmp",
			"reason":  "restart nginx",
		},
	}, execToolArgs{
		HostID:  hostID,
		Command: "systemctl restart nginx",
		Cwd:     "/tmp",
		Reason:  "restart nginx",
	}, false)

	session := app.store.Session(sessionID)
	if session == nil || len(session.Approvals) != 1 {
		t.Fatalf("expected one pending approval, got %#v", session)
	}
	var approval model.ApprovalRequest
	for _, item := range session.Approvals {
		approval = item
		break
	}
	if approval.ID != "approval-from-coordinator" {
		t.Fatalf("expected approval id from coordinator, got %#v", approval)
	}
	if approval.ItemID != cardID {
		t.Fatalf("expected approval item id %q, got %#v", cardID, approval)
	}
}

func TestDynamicRemoteFileChangeApprovalAutoResolvesWithNoticeCard(t *testing.T) {
	app := newDynamicApprovalEventsTestApp(t)
	sessionID := "sess-dynamic-remote-file-auto"
	hostID := "linux-file-01"
	callID := "call-remote-file-auto"
	cardID := dynamicToolCardID(callID)
	path := "/etc/nginx/nginx.conf"

	app.store.EnsureSession(sessionID)
	app.store.SetSelectedHost(sessionID, hostID)
	app.store.SetThread(sessionID, "thread-"+sessionID)
	app.store.SetTurn(sessionID, "turn-"+sessionID)
	app.store.UpsertHost(model.Host{
		ID:         hostID,
		Name:       hostID,
		Kind:       "agent",
		Status:     "online",
		Executable: true,
	})
	responded := make(chan map[string]any, 1)
	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		if rawID != "raw-remote-file-auto" {
			t.Fatalf("unexpected raw id %q", rawID)
		}
		payload, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected tool response payload, got %#v", result)
		}
		responded <- payload
		return nil
	}
	stream := &fileChangeAgentStream{
		onSend: func(msg *agentrpc.Envelope) error {
			switch msg.Kind {
			case "file/read":
				app.handleAgentFileReadResult(hostID, &agentrpc.FileReadResult{
					RequestID: msg.FileReadRequest.RequestID,
					Path:      msg.FileReadRequest.Path,
					Content:   "user nginx;\n",
				})
			case "file/write":
				app.handleAgentFileWriteResult(hostID, &agentrpc.FileWriteResult{
					RequestID:  msg.FileWriteRequest.RequestID,
					Path:       msg.FileWriteRequest.Path,
					OldContent: "user nginx;\n",
					NewContent: "user www-data;\n",
					Created:    false,
					WriteMode:  msg.FileWriteRequest.WriteMode,
				})
			}
			return nil
		},
	}
	app.setAgentConnection(hostID, &agentConnection{hostID: hostID, stream: stream})
	app.store.AddApprovalGrant(sessionID, model.ApprovalGrant{
		ID:          "grant-file-auto",
		HostID:      hostID,
		Type:        "remote_file_change",
		Fingerprint: approvalFingerprintForFileChange(hostID, filepath.Dir(path), []model.FileChange{{Path: path, Kind: "update"}}),
		Command:     "",
		Cwd:         "",
		CreatedAt:   model.NowString(),
	})

	app.requestRemoteFileChangeApproval(sessionID, hostID, "raw-remote-file-auto", dynamicToolCallParams{
		ThreadID: "thread-" + sessionID,
		TurnID:   "turn-" + sessionID,
		CallID:   callID,
		Tool:     "execute_system_mutation",
		Arguments: map[string]any{
			"host":       hostID,
			"mode":       "file_change",
			"path":       path,
			"content":    "user www-data;\n",
			"write_mode": "overwrite",
			"reason":     "switch nginx runtime user",
		},
	}, remoteFileChangeArgs{
		HostID:    hostID,
		Mode:      "file_change",
		Path:      path,
		Content:   "user www-data;\n",
		WriteMode: "overwrite",
		Reason:    "switch nginx runtime user",
	})

	select {
	case payload := <-responded:
		if got := payload["success"]; got != true {
			t.Fatalf("expected successful file change response, got %#v", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for remote file change response")
	}

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatal("expected session to exist")
	}
	if len(session.Approvals) != 1 {
		t.Fatalf("expected one auto-resolved approval, got %#v", session.Approvals)
	}
	var approval model.ApprovalRequest
	for _, item := range session.Approvals {
		approval = item
		break
	}
	if approval.Status != "accepted_for_session_auto" {
		t.Fatalf("expected session auto approval, got %#v", approval)
	}
	if approval.ResolvedAt == "" {
		t.Fatalf("expected resolved approval timestamp, got %#v", approval)
	}
	card := app.cardByID(sessionID, "auto-approval-"+cardID)
	if card == nil {
		t.Fatal("expected auto approval notice card")
	}
	if card.Type != "NoticeCard" || card.Status != "notice" {
		t.Fatalf("unexpected notice card: %#v", card)
	}
}

func TestHandleLocalCommandApprovalRequestAutoApprovesByProfile(t *testing.T) {
	app := newDynamicApprovalEventsTestApp(t)
	sessionID := "sess-dynamic-local-auto"
	threadID := "thread-dynamic-local-auto"
	turnID := "turn-dynamic-local-auto"
	itemID := "cmd-dynamic-local-auto"

	app.store.EnsureSession(sessionID)
	app.store.SetThread(sessionID, threadID)
	app.store.SetTurn(sessionID, turnID)

	profile := app.mainAgentProfile()
	profile.CapabilityPermissions.CommandExecution = model.AgentCapabilityEnabled
	if profile.CommandPermissions.CategoryPolicies == nil {
		profile.CommandPermissions.CategoryPolicies = make(map[string]string)
	}
	profile.CommandPermissions.CategoryPolicies["system_inspection"] = model.AgentPermissionModeAllow
	app.store.UpsertAgentProfile(profile)

	responded := make(chan map[string]any, 1)
	app.codexRespondFunc = func(_ context.Context, _ string, result any) error {
		payload, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected codex response payload, got %#v", result)
		}
		responded <- payload
		return nil
	}

	app.handleLocalCommandApprovalRequest("raw-local-auto", map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
		"itemId":   itemID,
		"command":  "uptime",
		"cwd":      "/tmp",
		"reason":   "check load",
	})

	select {
	case payload := <-responded:
		if got := payload["decision"]; got != "accept" {
			t.Fatalf("expected accept codex response, got %#v", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for codex response")
	}

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatal("expected session to exist")
	}
	if len(session.Approvals) != 1 {
		t.Fatalf("expected one auto-resolved local approval, got %#v", session.Approvals)
	}
	var approval model.ApprovalRequest
	for _, item := range session.Approvals {
		approval = item
		break
	}
	if approval.Status != "accepted_by_profile_auto" {
		t.Fatalf("expected profile auto approval, got %#v", approval)
	}
	if session.Runtime.Turn.Phase != "executing" {
		t.Fatalf("expected executing phase after local auto approval, got %q", session.Runtime.Turn.Phase)
	}
	card := app.cardByID(sessionID, "auto-approval-"+itemID)
	if card == nil {
		t.Fatal("expected local auto approval notice card")
	}
	if card.Type != "NoticeCard" || card.Status != "notice" {
		t.Fatalf("unexpected local auto approval card: %#v", card)
	}
}

func TestHandleLocalCommandApprovalRequestUsesCoordinatorRequestForAutoApprove(t *testing.T) {
	app := newDynamicApprovalEventsTestApp(t)
	sessionID := "sess-dynamic-local-coordinator"
	threadID := "thread-dynamic-local-coordinator"
	turnID := "turn-dynamic-local-coordinator"
	itemID := "cmd-dynamic-local-coordinator"

	app.store.EnsureSession(sessionID)
	app.store.SetThread(sessionID, threadID)
	app.store.SetTurn(sessionID, turnID)
	app.toolApprovalCoordinator = stubToolApprovalCoordinator{
		requestFn: func(_ context.Context, req ToolApprovalRequest) (ApprovalResolution, error) {
			if req.ToolName != "execute_command" {
				t.Fatalf("expected execute_command tool name, got %#v", req)
			}
			return ApprovalResolution{
				ApprovalID:   "approval-local-coordinator",
				Status:       ApprovalResolutionStatusApproved,
				RuleName:     toolApprovalRuleProfilePolicy,
				Reason:       "matched profile policy",
				AutoApproved: true,
				SessionID:    req.SessionID,
				HostID:       req.HostID,
				ToolName:     req.ToolName,
				RequestedAt:  time.Date(2026, 4, 17, 13, 0, 0, 0, time.UTC),
				ResolvedAt:   time.Date(2026, 4, 17, 13, 0, 0, 0, time.UTC),
				Request:      req.Clone(),
			}, nil
		},
	}

	responded := make(chan map[string]any, 1)
	app.codexRespondFunc = func(_ context.Context, _ string, result any) error {
		payload, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected codex response payload, got %#v", result)
		}
		responded <- payload
		return nil
	}

	app.handleLocalCommandApprovalRequest("raw-local-coordinator", map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
		"itemId":   itemID,
		"command":  "systemctl restart nginx",
		"cwd":      "/tmp",
		"reason":   "restart nginx",
		"availableDecisions": []any{
			"accept",
			"accept_session",
			"decline",
		},
	})

	select {
	case payload := <-responded:
		if got := payload["decision"]; got != "accept" {
			t.Fatalf("expected accept codex response, got %#v", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for codex response")
	}

	session := app.store.Session(sessionID)
	if session == nil || len(session.Approvals) != 1 {
		t.Fatalf("expected one auto-resolved local approval, got %#v", session)
	}
	var approval model.ApprovalRequest
	for _, item := range session.Approvals {
		approval = item
		break
	}
	if approval.ID != "approval-local-coordinator" {
		t.Fatalf("expected approval id from coordinator, got %#v", approval)
	}
	if approval.Status != "accepted_by_profile_auto" {
		t.Fatalf("expected profile auto approval, got %#v", approval)
	}
}

func TestHandleRequestApprovalUsesLifecycleEvents(t *testing.T) {
	app := newDynamicApprovalEventsTestApp(t)
	sessionID := "sess-dynamic-request-approval-events"
	hostID := "linux-request-approval-01"
	callID := "call-request-approval-events"

	app.store.EnsureSession(sessionID)
	app.store.SetSelectedHost(sessionID, hostID)
	app.store.SetThread(sessionID, "thread-"+sessionID)
	app.store.SetTurn(sessionID, "turn-"+sessionID)
	app.store.UpsertHost(model.Host{
		ID:         hostID,
		Name:       hostID,
		Kind:       "agent",
		Status:     "online",
		Executable: true,
	})

	app.handleRequestApproval("raw-request-approval-events", sessionID, dynamicToolCallParams{
		ThreadID: "thread-" + sessionID,
		TurnID:   "turn-" + sessionID,
		CallID:   callID,
		Tool:     "request_approval",
		Arguments: map[string]any{
			"command":            "systemctl restart nginx",
			"hostId":             hostID,
			"cwd":                "/tmp",
			"riskAssessment":     "high",
			"expectedImpact":     "brief nginx restart",
			"rollbackSuggestion": "systemctl restart nginx",
		},
	})

	session := app.store.Session(sessionID)
	if session == nil || len(session.Approvals) != 1 {
		t.Fatalf("expected one pending approval, got %#v", session)
	}
	var approval model.ApprovalRequest
	for _, item := range session.Approvals {
		approval = item
		break
	}
	if approval.Status != "pending" {
		t.Fatalf("expected pending approval, got %#v", approval)
	}
	requested := findToolLifecycleEvent(app.toolEventStore.SessionEvents(sessionID), ToolLifecycleEventApprovalRequested, approval.ID)
	if requested == nil {
		t.Fatalf("expected approval_requested lifecycle event, got %#v", app.toolEventStore.SessionEvents(sessionID))
	}
	if requested.ToolName != "request_approval" {
		t.Fatalf("expected request_approval tool name, got %#v", requested)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/approvals/"+approval.ID+"/decision", strings.NewReader(`{"decision":"decline"}`))
	recorder := httptest.NewRecorder()
	app.handleApprovalDecision(recorder, req, sessionID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	resolved := findToolLifecycleEvent(app.toolEventStore.SessionEvents(sessionID), ToolLifecycleEventApprovalResolved, approval.ID)
	if resolved == nil {
		t.Fatalf("expected approval_resolved lifecycle event, got %#v", app.toolEventStore.SessionEvents(sessionID))
	}
	storedApproval, ok := app.store.Approval(sessionID, approval.ID)
	if !ok {
		t.Fatalf("expected stored approval %q", approval.ID)
	}
	if storedApproval.Status != "decline" {
		t.Fatalf("expected declined approval status, got %#v", storedApproval)
	}
	card := app.cardByID(sessionID, approval.ItemID)
	if card == nil || card.Status != "failed" {
		t.Fatalf("expected declined approval card to be failed, got %#v", card)
	}
}

func TestHandleExitPlanModeUsesLifecycleEvents(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-exit-plan-events"
	responded := make(chan map[string]any, 1)

	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetThread(sessionID, "thread-"+sessionID)
	app.store.SetTurn(sessionID, "turn-"+sessionID)
	app.startRuntimeTurn(sessionID, model.ServerLocalHostID)
	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		payload, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected map payload, got %#v", result)
		}
		if rawID == "raw-exit-plan-events" {
			responded <- payload
		}
		return nil
	}

	app.handleDynamicToolCall("raw-enter-plan-events", map[string]any{
		"threadId": "thread-" + sessionID,
		"turnId":   "turn-" + sessionID,
		"callId":   "call-enter-plan-events",
		"tool":     "enter_plan_mode",
		"arguments": map[string]any{
			"goal":   "修复 replication lag",
			"reason": "涉及生产风险，需要先计划。",
			"scope":  "只读定位和计划审批。",
		},
	})
	app.handleDynamicToolCall("raw-update-plan-events", map[string]any{
		"threadId": "thread-" + sessionID,
		"turnId":   "turn-" + sessionID,
		"callId":   "call-update-plan-events",
		"tool":     "update_plan",
		"arguments": map[string]any{
			"title":      "复制修复计划",
			"summary":    "先只读检查，再审批执行。",
			"risk":       "错误操作会放大故障。",
			"rollback":   "保持只读并回到计划阶段。",
			"validation": "确认 replication lag 恢复。",
			"steps": []any{
				map[string]any{"id": "step-1", "title": "检查 lag", "status": "pending"},
			},
		},
	})
	app.handleDynamicToolCall("raw-exit-plan-events", map[string]any{
		"threadId": "thread-" + sessionID,
		"turnId":   "turn-" + sessionID,
		"callId":   "call-exit-plan-events",
		"tool":     "exit_plan_mode",
		"arguments": map[string]any{
			"title":      "批准执行计划",
			"summary":    "先只读确认，再申请执行。",
			"risk":       "worker 误操作会放大变更范围",
			"rollback":   "回到 plan mode，调整任务拆分。",
			"validation": "确认 replication lag 恢复。",
			"tasks": []any{
				map[string]any{"instruction": "检查 replication lag"},
			},
		},
	})

	session := app.store.Session(sessionID)
	if session == nil || len(session.Approvals) != 1 {
		t.Fatalf("expected one pending plan approval, got %#v", session)
	}
	var approval model.ApprovalRequest
	for _, item := range session.Approvals {
		approval = item
		break
	}
	if approval.Type != "plan_exit" || approval.Status != "pending" {
		t.Fatalf("expected pending plan_exit approval, got %#v", approval)
	}
	requested := findToolLifecycleEvent(app.toolEventStore.SessionEvents(sessionID), ToolLifecycleEventApprovalRequested, approval.ID)
	if requested == nil {
		t.Fatalf("expected approval_requested lifecycle event, got %#v", app.toolEventStore.SessionEvents(sessionID))
	}
	if requested.ToolName != "exit_plan_mode" {
		t.Fatalf("expected exit_plan_mode tool name, got %#v", requested)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/approvals/"+approval.ID+"/decision", strings.NewReader(`{"decision":"decline"}`))
	recorder := httptest.NewRecorder()
	app.handleApprovalDecision(recorder, req, sessionID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	select {
	case payload := <-responded:
		decoded := decodeStructuredToolResponsePayload(t, payload)
		if got := decoded["decision"]; got != "decline" {
			t.Fatalf("expected decline codex decision, got %#v", decoded)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for plan exit codex response")
	}

	resolved := findToolLifecycleEvent(app.toolEventStore.SessionEvents(sessionID), ToolLifecycleEventApprovalResolved, approval.ID)
	if resolved == nil {
		t.Fatalf("expected approval_resolved lifecycle event, got %#v", app.toolEventStore.SessionEvents(sessionID))
	}
	storedApproval, ok := app.store.Approval(sessionID, approval.ID)
	if !ok {
		t.Fatalf("expected stored approval %q", approval.ID)
	}
	if storedApproval.Status != "decline" {
		t.Fatalf("expected declined plan approval status, got %#v", storedApproval)
	}
	card := app.cardByID(sessionID, approval.ItemID)
	if card == nil || card.Status != "failed" {
		t.Fatalf("expected declined plan approval card to be failed, got %#v", card)
	}
}

func findToolLifecycleEvent(events []store.ToolEventRecord, eventType ToolLifecycleEventType, approvalID string) *store.ToolEventRecord {
	for i := range events {
		event := &events[i]
		if event.Type == string(eventType) && event.ApprovalID == approvalID {
			return event
		}
	}
	return nil
}

func newDynamicApprovalEventsTestApp(t *testing.T) *App {
	t.Helper()

	dir := t.TempDir()
	return New(config.Config{
		DefaultWorkspace: filepath.Join(dir, "workspace"),
		StatePath:        filepath.Join(dir, "state.json"),
		AuditLogPath:     filepath.Join(dir, "audit.log"),
	})
}
