package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/orchestrator"
)

func newOrchestratorTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	app := New(config.Config{
		SessionCookieName: "aiops_codex_session",
		SessionSecret:     "test-session-secret",
		SessionCookieTTL:  time.Hour,
		DefaultWorkspace:  filepath.Join(dir, "workspace"),
		StatePath:         filepath.Join(dir, "ai-server-state.json"),
		AuditLogPath:      filepath.Join(dir, "audit.log"),
	})
	if err := app.initOrchestrator(); err != nil {
		t.Fatalf("init orchestrator: %v", err)
	}
	return app
}

func TestHandleSessionsCanCreateWorkspaceSession(t *testing.T) {
	app := newOrchestratorTestApp(t)
	handler := app.withBrowserSession(app.handleSessions)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(`{"kind":"workspace"}`))
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp struct {
		ActiveSessionID string `json:"activeSessionId"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	session := app.store.Session(resp.ActiveSessionID)
	if session == nil {
		t.Fatalf("expected created session")
	}
	if session.Meta.Kind != model.SessionKindWorkspace {
		t.Fatalf("expected workspace kind, got %q", session.Meta.Kind)
	}
	if !session.Meta.Visible {
		t.Fatalf("expected workspace session to be visible")
	}
	snapshot := app.snapshot(resp.ActiveSessionID)
	if snapshot.Kind != model.SessionKindWorkspace {
		t.Fatalf("expected workspace snapshot kind, got %q", snapshot.Kind)
	}
}

func TestHandleApprovalDecisionRoutesToWorkerSession(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.codexRespondFunc = func(_ context.Context, _ string, _ any) error { return nil }

	workspaceSessionID := "workspace-1"
	workerSessionID := "worker-1"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-1",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.EnsureSessionWithMeta(workerSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorker,
		Visible:            false,
		MissionID:          "mission-1",
		WorkspaceSessionID: workspaceSessionID,
		WorkerHostID:       "host-1",
		RuntimePreset:      model.SessionRuntimePresetWorker,
	})
	if err := app.orchestrator.RegisterApprovalRoute("approval-1", workerSessionID); err != nil {
		t.Fatalf("register approval route: %v", err)
	}
	app.store.AddApproval(workerSessionID, model.ApprovalRequest{
		ID:           "approval-1",
		RequestIDRaw: "raw-1",
		Type:         "command",
		Status:       "pending",
		ItemID:       "item-1",
		RequestedAt:  model.NowString(),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/approvals/approval-1/decision", strings.NewReader(`{"decision":"accept"}`))
	rec := httptest.NewRecorder()
	app.handleApprovalDecision(rec, req, workspaceSessionID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	approval, ok := app.store.Approval(workerSessionID, "approval-1")
	if !ok {
		t.Fatalf("expected approval to stay on worker session")
	}
	if approval.Status == "pending" {
		t.Fatalf("expected approval to be resolved on worker session")
	}
}

func TestHandleChoiceAnswerRoutesToWorkerSessionMirror(t *testing.T) {
	app := newOrchestratorTestApp(t)

	var respondedRawID string
	var respondedPayload map[string]any
	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		respondedRawID = rawID
		payload, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return json.Unmarshal(payload, &respondedPayload)
	}

	workspaceSessionID := "workspace-choice-worker"
	workerSessionID := "worker-choice-worker"
	now := model.NowString()
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-choice-worker",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.EnsureSessionWithMeta(workerSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorker,
		Visible:            false,
		MissionID:          "mission-choice-worker",
		WorkspaceSessionID: workspaceSessionID,
		WorkerHostID:       "host-1",
		RuntimePreset:      model.SessionRuntimePresetWorker,
	})
	app.startRuntimeTurn(workerSessionID, "host-1")

	choice := model.ChoiceRequest{
		ID:           "choice-worker-1",
		RequestIDRaw: "raw-choice-worker-1",
		ThreadID:     "thread-choice-worker-1",
		TurnID:       "turn-choice-worker-1",
		ItemID:       "choice-worker-1",
		Status:       "pending",
		Questions: []model.ChoiceQuestion{{
			Header:   "执行策略",
			Question: "选择执行策略",
			Options: []model.ChoiceOption{
				{Label: "保守模式", Value: "safe"},
				{Label: "激进模式", Value: "fast"},
			},
		}},
		RequestedAt: now,
	}
	card := model.Card{
		ID:        choice.ItemID,
		Type:      "ChoiceCard",
		Title:     "选择执行策略",
		RequestID: choice.ID,
		Question:  choice.Questions[0].Question,
		Options:   choice.Questions[0].Options,
		Questions: choice.Questions,
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	}
	app.store.AddChoice(workerSessionID, choice)
	app.store.UpsertCard(workerSessionID, card)
	app.mirrorInternalChoiceToWorkspace(workerSessionID, choice, card)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/choices/"+choice.ID+"/answer", strings.NewReader(`{"answers":[{"value":"safe","label":"保守模式"}]}`))
	rec := httptest.NewRecorder()
	app.handleChoiceAnswer(rec, req, workspaceSessionID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	targetChoice, ok := app.store.Choice(workerSessionID, choice.ID)
	if !ok || targetChoice.Status != "completed" {
		t.Fatalf("expected worker choice completed, got %#v, %v", targetChoice, ok)
	}
	workspaceCard := app.cardByID(workspaceSessionID, choice.ItemID)
	if workspaceCard == nil || workspaceCard.Status != "completed" {
		t.Fatalf("expected mirrored workspace choice card completed, got %#v", workspaceCard)
	}
	if got := strings.Join(workspaceCard.AnswerSummary, " "); !strings.Contains(got, "保守模式") {
		t.Fatalf("expected answer summary to contain chosen label, got %#v", workspaceCard.AnswerSummary)
	}
	session := app.store.Session(workerSessionID)
	if session == nil || session.Runtime.Turn.Phase != "thinking" {
		t.Fatalf("expected worker runtime phase thinking, got %#v", session)
	}
	if respondedRawID != choice.RequestIDRaw {
		t.Fatalf("expected codex response to original raw id, got %q", respondedRawID)
	}
	answers, ok := respondedPayload["answers"].([]any)
	if !ok || len(answers) != 1 {
		t.Fatalf("expected one answer in codex response, got %#v", respondedPayload["answers"])
	}
}

func TestPlannerDispatchTasksStartsWorkerSession(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.codexRespondFunc = func(_ context.Context, _ string, _ any) error { return nil }
	app.codexRequestFunc = func(_ context.Context, method string, _ any, result any) error {
		payload := map[string]any{}
		switch method {
		case "thread/start":
			payload = map[string]any{"thread": map[string]any{"id": "thread-worker-1"}}
		case "turn/start":
			payload = map[string]any{"turnId": "turn-worker-1"}
		}
		content, _ := json.Marshal(payload)
		return json.Unmarshal(content, result)
	}

	app.store.UpsertHost(model.Host{
		ID:              "host-1",
		Name:            "host-1",
		Kind:            "linux",
		Status:          "online",
		Executable:      true,
		TerminalCapable: true,
	})
	stream := &remoteStatusUnifyAgentStream{
		onSend: func(msg *agentrpc.Envelope) error {
			if msg.Kind != "exec/start" || msg.ExecStart == nil {
				return nil
			}
			if want := "mkdir -p .aiops_codex/missions/mission-1/host-1"; msg.ExecStart.Command != want {
				t.Fatalf("unexpected bootstrap command %q", msg.ExecStart.Command)
			}
			app.handleAgentExecExit("host-1", &agentrpc.ExecExit{
				ExecID:   msg.ExecStart.ExecID,
				Status:   "completed",
				ExitCode: 0,
			})
			return nil
		},
	}
	app.setAgentConnection("host-1", &agentConnection{hostID: "host-1", stream: stream})

	workspaceSessionID := "workspace-1"
	plannerSessionID := "planner-1"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-1",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})

	mission, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:          "mission-1",
		WorkspaceSessionID: workspaceSessionID,
		PlannerSessionID:   plannerSessionID,
		Title:              "demo",
		Summary:            "demo summary",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	app.ensureInternalSessionFromWorkspace(plannerSessionID, workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindPlanner,
		Visible:            false,
		MissionID:          mission.ID,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetPlanner,
	}, model.ServerLocalHostID)

	app.handlePlannerDispatchTasks("raw-dispatch", plannerSessionID, map[string]any{
		"tasks": []map[string]any{
			{
				"taskId":      "task-1",
				"hostId":      "host-1",
				"title":       "inspect host",
				"instruction": "inspect nginx status",
			},
		},
	})

	updatedMission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok {
		t.Fatalf("expected mission after dispatch")
	}
	worker := updatedMission.Workers["host-1"]
	if worker == nil {
		t.Fatalf("expected worker for host-1")
	}
	session := app.store.Session(worker.SessionID)
	if session == nil {
		t.Fatalf("expected worker session to be materialized")
	}
	if session.Meta.Kind != model.SessionKindWorker {
		t.Fatalf("expected worker session kind, got %q", session.Meta.Kind)
	}
	if session.SelectedHostID != "host-1" {
		t.Fatalf("expected selected host host-1, got %q", session.SelectedHostID)
	}
	if !session.Runtime.Turn.Active {
		t.Fatalf("expected worker turn to be active")
	}
	workspace := app.store.Session(workspaceSessionID)
	if workspace == nil {
		t.Fatalf("expected workspace session")
	}
	if app.cardByID(workspaceSessionID, "workspace-plan-mission-1") == nil {
		t.Fatalf("expected workspace plan card after dispatch")
	}
	if app.cardByID(workspaceSessionID, "workspace-worker-host-1") == nil {
		t.Fatalf("expected workspace worker progress card after dispatch")
	}
	records := readAuditRecords(t, app.cfg.AuditLogPath)
	found := false
	for _, record := range records {
		if record["event"] == "orchestrator.workspace_bootstrap" && record["status"] == "completed" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected completed orchestrator bootstrap audit record")
	}
}

func TestWorkspaceProjectionIncludesRichPlanAndWorkerReadModels(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.codexRespondFunc = func(_ context.Context, _ string, _ any) error { return nil }
	app.codexRequestFunc = func(_ context.Context, method string, _ any, result any) error {
		payload := map[string]any{}
		switch method {
		case "thread/start":
			payload = map[string]any{"thread": map[string]any{"id": "thread-worker-rich"}}
		case "turn/start":
			payload = map[string]any{"turnId": "turn-worker-rich"}
		}
		content, _ := json.Marshal(payload)
		return json.Unmarshal(content, result)
	}

	app.store.UpsertHost(model.Host{
		ID:              "host-1",
		Name:            "host-1",
		Kind:            "linux",
		Status:          "online",
		Executable:      true,
		TerminalCapable: true,
	})

	workspaceSessionID := "workspace-rich"
	plannerSessionID := "planner-rich"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-rich",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})

	mission, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:          "mission-rich",
		WorkspaceSessionID: workspaceSessionID,
		PlannerSessionID:   plannerSessionID,
		Title:              "rich projection",
		Summary:            "collect richer read models",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	app.ensureInternalSessionFromWorkspace(plannerSessionID, workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindPlanner,
		Visible:            false,
		MissionID:          mission.ID,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetPlanner,
	}, model.ServerLocalHostID)

	app.handlePlannerDispatchTasks("raw-dispatch-rich", plannerSessionID, map[string]any{
		"tasks": []map[string]any{
			{
				"taskId":      "task-1",
				"hostId":      "host-1",
				"title":       "inspect host",
				"instruction": "systemctl status nginx",
				"constraints": []string{"readonly"},
			},
		},
	})

	projectedMission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok {
		t.Fatalf("expected mission after dispatch")
	}
	worker := projectedMission.Workers["host-1"]
	if worker == nil {
		t.Fatalf("expected worker for host-1")
	}

	now := model.NowString()
	app.store.UpsertCard(plannerSessionID, model.Card{
		ID:        "planner-user-1",
		Type:      "UserMessageCard",
		Role:      "user",
		Text:      "请先检查 nginx 状态再考虑 reload",
		CreatedAt: now,
		UpdatedAt: now,
	})
	app.store.UpsertCard(plannerSessionID, model.Card{
		ID:        "planner-assistant-1",
		Type:      "AssistantMessageCard",
		Role:      "assistant",
		Text:      "已拆成 host-1 检查步骤，并保留审批锚点。",
		CreatedAt: now,
		UpdatedAt: now,
	})
	app.store.UpsertCard(worker.SessionID, model.Card{
		ID:        "cmd-host-1",
		Type:      "CommandCard",
		Title:     "inspect host",
		Command:   "systemctl status nginx",
		Cwd:       "/srv/app",
		Text:      "nginx active (running)",
		Summary:   "已获取 nginx 状态",
		Status:    "completed",
		Stdout:    "active (running)",
		CreatedAt: now,
		UpdatedAt: now,
	})
	app.store.UpsertCard(worker.SessionID, model.Card{
		ID:        "assistant-host-1",
		Type:      "AssistantMessageCard",
		Role:      "assistant",
		Text:      "nginx active，建议下一步 reload。",
		CreatedAt: now,
		UpdatedAt: now,
	})
	app.store.AddApproval(worker.SessionID, model.ApprovalRequest{
		ID:          "approval-rich-1",
		HostID:      "host-1",
		Type:        "command",
		Status:      "pending",
		ItemID:      "cmd-host-1",
		Command:     "sudo nginx -s reload",
		Reason:      "需要用户确认 reload",
		RequestedAt: now,
	})
	app.refreshWorkspaceProjection(projectedMission)

	planCard := app.cardByID(workspaceSessionID, "workspace-plan-mission-rich")
	if planCard == nil {
		t.Fatalf("expected workspace plan card")
	}
	plannerConversation, ok := planCard.Detail["planner_conversation"].([]any)
	if !ok || len(plannerConversation) < 2 {
		t.Fatalf("expected planner conversation excerpts, got %#v", planCard.Detail["planner_conversation"])
	}
	dispatchEvents, ok := planCard.Detail["dispatch_events"].([]any)
	if !ok || len(dispatchEvents) == 0 {
		t.Fatalf("expected dispatch events, got %#v", planCard.Detail["dispatch_events"])
	}
	taskBindings, ok := planCard.Detail["task_host_bindings"].([]any)
	if !ok || len(taskBindings) != 1 {
		t.Fatalf("expected one task binding, got %#v", planCard.Detail["task_host_bindings"])
	}

	workerCard := app.cardByID(workspaceSessionID, "workspace-worker-host-1")
	if workerCard == nil {
		t.Fatalf("expected workspace worker card")
	}
	dispatchDetail, ok := workerCard.Detail["dispatch"].(map[string]any)
	if !ok {
		t.Fatalf("expected dispatch detail map, got %#v", workerCard.Detail["dispatch"])
	}
	if _, ok := dispatchDetail["timeline"].([]any); !ok {
		t.Fatalf("expected dispatch timeline in worker card, got %#v", dispatchDetail["timeline"])
	}
	if taskBinding, ok := dispatchDetail["task_binding"].(map[string]any); !ok || taskBinding["taskId"] != "task-1" {
		t.Fatalf("expected dispatch task binding, got %#v", dispatchDetail["task_binding"])
	}
	workerDetail, ok := workerCard.Detail["worker"].(map[string]any)
	if !ok {
		t.Fatalf("expected worker detail map, got %#v", workerCard.Detail["worker"])
	}
	if conversation, ok := workerDetail["conversation"].([]any); !ok || len(conversation) < 2 {
		t.Fatalf("expected worker conversation excerpts, got %#v", workerDetail["conversation"])
	}
	if terminal, ok := workerDetail["terminal"].(map[string]any); !ok || terminal["output"] != "active (running)" {
		t.Fatalf("expected enriched terminal output, got %#v", workerDetail["terminal"])
	}
	if anchor, ok := workerDetail["approval_anchor"].(map[string]any); !ok || anchor["approvalId"] != "approval-rich-1" || anchor["sourceCardId"] != "cmd-host-1" {
		t.Fatalf("expected approval anchor, got %#v", workerDetail["approval_anchor"])
	}
}

func TestPlannerDispatchTasksRejectsOfflineHost(t *testing.T) {
	app := newOrchestratorTestApp(t)
	responses := newCapturedCodexResponses()
	app.codexRespondFunc = responses.capture

	app.store.UpsertHost(model.Host{
		ID:              "host-offline",
		Name:            "host-offline",
		Kind:            "linux",
		Status:          "offline",
		Executable:      true,
		TerminalCapable: true,
	})

	workspaceSessionID := "workspace-offline"
	plannerSessionID := "planner-offline"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-offline",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})

	mission, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:          "mission-offline",
		WorkspaceSessionID: workspaceSessionID,
		PlannerSessionID:   plannerSessionID,
		Title:              "offline host demo",
		Summary:            "offline host demo",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	app.ensureInternalSessionFromWorkspace(plannerSessionID, workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindPlanner,
		Visible:            false,
		MissionID:          mission.ID,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetPlanner,
	}, model.ServerLocalHostID)

	app.handlePlannerDispatchTasks("raw-dispatch-offline", plannerSessionID, map[string]any{
		"tasks": []map[string]any{
			{
				"taskId":      "task-1",
				"hostId":      "host-offline",
				"title":       "inspect host",
				"instruction": "inspect nginx status",
			},
		},
	})

	resp, ok := responses.result("raw-dispatch-offline")
	if !ok {
		t.Fatalf("expected planner dispatch response")
	}
	if success, _ := resp["success"].(bool); success {
		t.Fatalf("expected failed planner dispatch response, got %#v", resp)
	}
	if !strings.Contains(asString(resp["contentItems"]), "当前离线") {
		t.Fatalf("expected offline host error, got %#v", resp)
	}

	updatedMission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok {
		t.Fatalf("expected mission after dispatch rejection")
	}
	if len(updatedMission.Tasks) != 0 || len(updatedMission.Workers) != 0 {
		t.Fatalf("expected offline host dispatch to leave mission untouched, got %#v", updatedMission)
	}
}

func TestPlannerDispatchTaskStartFailureFailsWorkerSession(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.codexRespondFunc = func(_ context.Context, _ string, _ any) error { return nil }
	app.codexRequestFunc = func(_ context.Context, method string, _ any, result any) error {
		switch method {
		case "thread/start":
			payload := map[string]any{"thread": map[string]any{"id": "thread-worker-start-fail"}}
			content, _ := json.Marshal(payload)
			return json.Unmarshal(content, result)
		case "turn/start":
			return errors.New("turn/start failed")
		default:
			return nil
		}
	}

	app.store.UpsertHost(model.Host{
		ID:              "host-1",
		Name:            "host-1",
		Kind:            "linux",
		Status:          "online",
		Executable:      true,
		TerminalCapable: true,
	})
	stream := &remoteStatusUnifyAgentStream{
		onSend: func(msg *agentrpc.Envelope) error {
			if msg.Kind != "exec/start" || msg.ExecStart == nil {
				return nil
			}
			app.handleAgentExecExit("host-1", &agentrpc.ExecExit{
				ExecID:   msg.ExecStart.ExecID,
				Status:   "completed",
				ExitCode: 0,
			})
			return nil
		},
	}
	app.setAgentConnection("host-1", &agentConnection{hostID: "host-1", stream: stream})

	workspaceSessionID := "workspace-start-fail"
	plannerSessionID := "planner-start-fail"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-start-fail",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})

	mission, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:          "mission-start-fail",
		WorkspaceSessionID: workspaceSessionID,
		PlannerSessionID:   plannerSessionID,
		Title:              "worker start fail demo",
		Summary:            "worker start fail demo",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	app.ensureInternalSessionFromWorkspace(plannerSessionID, workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindPlanner,
		Visible:            false,
		MissionID:          mission.ID,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetPlanner,
	}, model.ServerLocalHostID)

	app.handlePlannerDispatchTasks("raw-dispatch-start-fail", plannerSessionID, map[string]any{
		"tasks": []map[string]any{
			{
				"taskId":      "task-1",
				"hostId":      "host-1",
				"title":       "inspect host",
				"instruction": "inspect nginx status",
			},
		},
	})

	updatedMission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok {
		t.Fatalf("expected mission after dispatch")
	}
	if updatedMission.Status != orchestrator.MissionStatusFailed {
		t.Fatalf("expected failed mission after worker start failure, got %s", updatedMission.Status)
	}
	worker := updatedMission.Workers["host-1"]
	if worker == nil || worker.Status != orchestrator.WorkerStatusFailed {
		t.Fatalf("expected failed worker after start failure, got %#v", worker)
	}
	if got := updatedMission.Tasks["task-1"]; got == nil || got.Status != orchestrator.TaskStatusFailed {
		t.Fatalf("expected failed task after worker start failure, got %#v", got)
	}
	session := app.store.Session(worker.SessionID)
	if session == nil || session.Runtime.Turn.Phase != "failed" {
		t.Fatalf("expected worker runtime failed after start failure, got %#v", session)
	}
	if card := app.cardByID(workspaceSessionID, "workspace-reconcile-host-host-1"); card == nil || card.Status != "failed" {
		t.Fatalf("expected failed workspace reconcile card after start failure, got %#v", card)
	}
}

func TestWorkspaceProjectionUpdatesAfterWorkerCompletion(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.codexRespondFunc = func(_ context.Context, _ string, _ any) error { return nil }
	app.codexRequestFunc = func(_ context.Context, method string, _ any, result any) error {
		payload := map[string]any{}
		switch method {
		case "thread/start":
			payload = map[string]any{"thread": map[string]any{"id": "thread-worker-1"}}
		case "turn/start":
			payload = map[string]any{"turnId": "turn-worker-1"}
		}
		content, _ := json.Marshal(payload)
		return json.Unmarshal(content, result)
	}

	app.store.UpsertHost(model.Host{
		ID:              "host-1",
		Name:            "host-1",
		Kind:            "linux",
		Status:          "online",
		Executable:      true,
		TerminalCapable: true,
	})
	stream := &remoteStatusUnifyAgentStream{
		onSend: func(msg *agentrpc.Envelope) error {
			if msg.Kind != "exec/start" || msg.ExecStart == nil {
				return nil
			}
			app.handleAgentExecExit("host-1", &agentrpc.ExecExit{
				ExecID:   msg.ExecStart.ExecID,
				Status:   "completed",
				ExitCode: 0,
			})
			return nil
		},
	}
	app.setAgentConnection("host-1", &agentConnection{hostID: "host-1", stream: stream})

	workspaceSessionID := "workspace-1"
	plannerSessionID := "planner-1"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-1",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})

	mission, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:          "mission-1",
		WorkspaceSessionID: workspaceSessionID,
		PlannerSessionID:   plannerSessionID,
		Title:              "demo",
		Summary:            "demo summary",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	app.ensureInternalSessionFromWorkspace(plannerSessionID, workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindPlanner,
		Visible:            false,
		MissionID:          mission.ID,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetPlanner,
	}, model.ServerLocalHostID)

	app.handlePlannerDispatchTasks("raw-dispatch", plannerSessionID, map[string]any{
		"tasks": []map[string]any{
			{
				"taskId":      "task-1",
				"hostId":      "host-1",
				"title":       "inspect host",
				"instruction": "inspect nginx status",
			},
		},
	})

	updatedMission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok {
		t.Fatalf("expected mission after dispatch")
	}
	worker := updatedMission.Workers["host-1"]
	if worker == nil {
		t.Fatalf("expected worker for host-1")
	}

	app.store.UpsertCard(worker.SessionID, model.Card{
		ID:        "assistant-complete-1",
		Type:      "AssistantMessageCard",
		Role:      "assistant",
		Text:      "检查完成，nginx 正常",
		Status:    "completed",
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	})
	app.handleMissionTurnCompleted(worker.SessionID, "completed")

	projectedMission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok {
		t.Fatalf("expected mission after completion")
	}
	if projectedMission.Status != orchestrator.MissionStatusCompleted {
		t.Fatalf("expected mission completed, got %s", projectedMission.Status)
	}

	planCard := app.cardByID(workspaceSessionID, "workspace-plan-mission-1")
	if planCard == nil {
		t.Fatalf("expected workspace plan card after completion")
	}
	if planCard.Type != "PlanCard" {
		t.Fatalf("expected plan card type, got %s", planCard.Type)
	}
	if len(planCard.Items) != 1 || planCard.Items[0].Status != "completed" {
		t.Fatalf("expected completed plan item, got %#v", planCard.Items)
	}

	workerCard := app.cardByID(workspaceSessionID, "workspace-worker-host-1")
	if workerCard == nil {
		t.Fatalf("expected workspace worker card after completion")
	}
	if workerCard.Type != "ProcessLineCard" || workerCard.Status != "completed" {
		t.Fatalf("expected completed worker projection, got %#v", workerCard)
	}

	resultCard := app.cardByID(workspaceSessionID, "workspace-result-mission-1")
	if resultCard == nil {
		t.Fatalf("expected workspace result card after completion")
	}
	if resultCard.Type != "ResultSummaryCard" {
		t.Fatalf("expected result summary card, got %s", resultCard.Type)
	}
	if resultCard.Status != "completed" {
		t.Fatalf("expected completed result card, got %s", resultCard.Status)
	}
	if len(resultCard.KVRows) == 0 {
		t.Fatalf("expected result card kv rows")
	}
}

func TestStartWorkerTaskRefreshesExpiredIdleWorkerThread(t *testing.T) {
	app := newOrchestratorTestApp(t)
	idleClock := time.Now().UTC().Add(-(autoThreadResetIdleThreshold + time.Hour))
	manager := orchestrator.NewManagerFromConfig(orchestrator.ManagerConfig{
		Store:         orchestrator.NewStore(filepath.Join(filepath.Dir(app.cfg.StatePath), "orchestrator", "orchestrator.json")),
		WorkspaceRoot: app.cfg.DefaultWorkspace,
		Clock: func() time.Time {
			return idleClock
		},
	})
	if err := manager.Load(); err != nil {
		t.Fatalf("load orchestrator with idle clock: %v", err)
	}
	app.orchestrator = manager
	app.codexRespondFunc = func(_ context.Context, _ string, _ any) error { return nil }

	threadSeq := 0
	turnSeq := 0
	app.codexRequestFunc = func(_ context.Context, method string, _ any, result any) error {
		payload := map[string]any{}
		switch method {
		case "thread/start":
			threadSeq++
			payload = map[string]any{"thread": map[string]any{"id": fmt.Sprintf("thread-recycle-%02d", threadSeq)}}
		case "turn/start":
			turnSeq++
			payload = map[string]any{"turnId": fmt.Sprintf("turn-recycle-%02d", turnSeq)}
		}
		content, _ := json.Marshal(payload)
		return json.Unmarshal(content, result)
	}

	app.store.UpsertHost(model.Host{
		ID:              "host-1",
		Name:            "host-1",
		Kind:            "linux",
		Status:          "online",
		Executable:      true,
		TerminalCapable: true,
	})
	stream := &remoteStatusUnifyAgentStream{
		onSend: func(msg *agentrpc.Envelope) error {
			if msg.Kind != "exec/start" || msg.ExecStart == nil {
				return nil
			}
			app.handleAgentExecExit("host-1", &agentrpc.ExecExit{
				ExecID:   msg.ExecStart.ExecID,
				Status:   "completed",
				ExitCode: 0,
			})
			return nil
		},
	}
	app.setAgentConnection("host-1", &agentConnection{hostID: "host-1", stream: stream})

	workspaceSessionID := "workspace-recycle"
	plannerSessionID := "planner-recycle"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-recycle",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})

	mission, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:          "mission-recycle",
		WorkspaceSessionID: workspaceSessionID,
		PlannerSessionID:   plannerSessionID,
		Title:              "thread recycle demo",
		Summary:            "thread recycle demo",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	app.ensureInternalSessionFromWorkspace(plannerSessionID, workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindPlanner,
		Visible:            false,
		MissionID:          mission.ID,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetPlanner,
	}, model.ServerLocalHostID)

	app.handlePlannerDispatchTasks("raw-dispatch-initial", plannerSessionID, map[string]any{
		"tasks": []map[string]any{
			{
				"taskId":      "task-1",
				"hostId":      "host-1",
				"title":       "inspect host",
				"instruction": "inspect nginx status",
			},
		},
	})

	currentMission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok {
		t.Fatalf("expected mission after initial dispatch")
	}
	worker := currentMission.Workers["host-1"]
	if worker == nil {
		t.Fatalf("expected worker for host-1")
	}
	session := app.store.Session(worker.SessionID)
	if session == nil || session.ThreadID != "thread-recycle-01" {
		t.Fatalf("expected initial worker thread, got %#v", session)
	}

	app.store.UpsertCard(worker.SessionID, model.Card{
		ID:        "assistant-recycle-1",
		Type:      "AssistantMessageCard",
		Role:      "assistant",
		Text:      "initial inspection completed",
		Status:    "completed",
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	})
	app.handleMissionTurnCompleted(worker.SessionID, "completed")

	app.handlePlannerDispatchTasks("raw-dispatch-follow-up", plannerSessionID, map[string]any{
		"tasks": []map[string]any{
			{
				"taskId":      "task-2",
				"hostId":      "host-1",
				"title":       "collect logs",
				"instruction": "collect nginx error logs",
			},
		},
	})

	currentMission, ok = app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok {
		t.Fatalf("expected mission after follow-up dispatch")
	}
	worker = currentMission.Workers["host-1"]
	if worker == nil {
		t.Fatalf("expected worker after follow-up dispatch")
	}
	session = app.store.Session(worker.SessionID)
	if session == nil {
		t.Fatalf("expected worker session after follow-up dispatch")
	}
	if threadSeq != 2 {
		t.Fatalf("expected idle worker to create a fresh thread, got %d thread starts", threadSeq)
	}
	if session.ThreadID != "thread-recycle-02" {
		t.Fatalf("expected refreshed worker thread, got %#v", session)
	}
	if session.TurnID != "turn-recycle-02" {
		t.Fatalf("expected refreshed worker turn, got %#v", session)
	}
}

func TestReconcileOrchestratorHostUnavailableUpdatesWorkspaceProjection(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.store.UpsertHost(model.Host{
		ID:              "host-1",
		Name:            "host-1",
		Kind:            "linux",
		Status:          "offline",
		Executable:      true,
		TerminalCapable: true,
	})

	workspaceSessionID := "workspace-1"
	plannerSessionID := "planner-1"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-1",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})

	mission, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:          "mission-1",
		WorkspaceSessionID: workspaceSessionID,
		PlannerSessionID:   plannerSessionID,
		Title:              "demo",
		Summary:            "demo summary",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	_, err = app.orchestrator.Dispatch(context.Background(), orchestrator.DispatchRequest{
		MissionID: mission.ID,
		Tasks: []orchestrator.DispatchTaskRequest{
			{
				TaskID:      "task-1",
				HostID:      "host-1",
				Title:       "inspect host",
				Instruction: "inspect nginx status",
			},
			{
				TaskID:      "task-2",
				HostID:      "host-1",
				Title:       "follow-up",
				Instruction: "collect logs",
			},
		},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	updatedMission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok {
		t.Fatalf("expected mission after dispatch")
	}
	worker := updatedMission.Workers["host-1"]
	if worker == nil {
		t.Fatalf("expected worker for host-1")
	}
	app.ensureInternalSessionFromWorkspace(worker.SessionID, workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorker,
		Visible:            false,
		MissionID:          mission.ID,
		WorkspaceSessionID: workspaceSessionID,
		WorkerHostID:       "host-1",
		RuntimePreset:      model.SessionRuntimePresetWorker,
	}, "host-1")
	app.startRuntimeTurn(worker.SessionID, "host-1")

	app.reconcileOrchestratorHostUnavailable("host-1", "remote host disconnected")

	reconciledMission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok {
		t.Fatalf("expected reconciled mission")
	}
	if reconciledMission.Status != orchestrator.MissionStatusFailed {
		t.Fatalf("expected failed mission after host unavailable, got %s", reconciledMission.Status)
	}
	if got := reconciledMission.Tasks["task-1"]; got == nil || got.Status != orchestrator.TaskStatusFailed {
		t.Fatalf("expected task-1 failed, got %#v", got)
	}
	if got := reconciledMission.Tasks["task-2"]; got == nil || got.Status != orchestrator.TaskStatusFailed {
		t.Fatalf("expected task-2 failed, got %#v", got)
	}

	workerCard := app.cardByID(workspaceSessionID, "workspace-worker-host-1")
	if workerCard == nil {
		t.Fatalf("expected workspace worker card after reconcile")
	}
	if workerCard.Type != "ProcessLineCard" || workerCard.Status != "failed" {
		t.Fatalf("expected failed worker projection, got %#v", workerCard)
	}

	resultCard := app.cardByID(workspaceSessionID, "workspace-result-mission-1")
	if resultCard == nil {
		t.Fatalf("expected workspace result card after reconcile")
	}
	if resultCard.Status != "failed" {
		t.Fatalf("expected failed result card, got %s", resultCard.Status)
	}

	hostCard := app.cardByID(workspaceSessionID, "workspace-host-unavailable-mission-1-host-1")
	if hostCard == nil {
		t.Fatalf("expected host unavailable card")
	}
	if hostCard.Type != "ErrorCard" || hostCard.Status != "failed" {
		t.Fatalf("expected failed host unavailable card, got %#v", hostCard)
	}
}

func TestHandleWorkspaceStopCancelsMissionAndClearsWorkerQueue(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.codexRequestFunc = func(_ context.Context, method string, _ any, result any) error {
		if method == "turn/interrupt" {
			payload := map[string]any{"ok": true}
			content, _ := json.Marshal(payload)
			return json.Unmarshal(content, result)
		}
		return nil
	}

	workspaceSessionID := "workspace-stop"
	plannerSessionID := "planner-stop"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-stop",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	mission, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:          "mission-stop",
		WorkspaceSessionID: workspaceSessionID,
		PlannerSessionID:   plannerSessionID,
		Title:              "stop demo",
		Summary:            "stop demo",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	app.ensureInternalSessionFromWorkspace(plannerSessionID, workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindPlanner,
		Visible:            false,
		MissionID:          mission.ID,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetPlanner,
	}, model.ServerLocalHostID)
	app.store.SetThread(plannerSessionID, "thread-planner-stop")
	app.store.SetTurn(plannerSessionID, "turn-planner-stop")
	app.startRuntimeTurn(plannerSessionID, model.ServerLocalHostID)

	_, err = app.orchestrator.Dispatch(context.Background(), orchestrator.DispatchRequest{
		MissionID: mission.ID,
		Tasks: []orchestrator.DispatchTaskRequest{
			{TaskID: "task-1", HostID: "host-1", Title: "first", Instruction: "first task"},
			{TaskID: "task-2", HostID: "host-1", Title: "second", Instruction: "second task"},
		},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	currentMission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok {
		t.Fatalf("expected mission after dispatch")
	}
	worker := currentMission.Workers["host-1"]
	if worker == nil {
		t.Fatalf("expected worker for host-1")
	}
	app.ensureInternalSessionFromWorkspace(worker.SessionID, workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorker,
		Visible:            false,
		MissionID:          mission.ID,
		WorkspaceSessionID: workspaceSessionID,
		WorkerHostID:       "host-1",
		RuntimePreset:      model.SessionRuntimePresetWorker,
	}, "host-1")
	app.store.SetThread(worker.SessionID, "thread-worker-stop")
	app.store.SetTurn(worker.SessionID, "turn-worker-stop")
	app.startRuntimeTurn(worker.SessionID, "host-1")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/stop", nil)
	rec := httptest.NewRecorder()
	app.handleWorkspaceStop(rec, req, workspaceSessionID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	currentMission, ok = app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok {
		t.Fatalf("expected mission after stop")
	}
	if currentMission.Status != orchestrator.MissionStatusCancelled {
		t.Fatalf("expected cancelled mission, got %s", currentMission.Status)
	}
	worker = currentMission.Workers["host-1"]
	if worker.ActiveTaskID != "" || len(worker.QueueTaskIDs) != 0 {
		t.Fatalf("expected worker queue cleared after stop, got %#v", worker)
	}
	if task := currentMission.Tasks["task-1"]; task == nil || task.Status != orchestrator.TaskStatusCancelled {
		t.Fatalf("expected task-1 cancelled, got %#v", task)
	}
	if task := currentMission.Tasks["task-2"]; task == nil || task.Status != orchestrator.TaskStatusCancelled {
		t.Fatalf("expected task-2 cancelled, got %#v", task)
	}
	resultCard := app.cardByID(workspaceSessionID, "workspace-result-mission-stop")
	if resultCard == nil || resultCard.Status != "failed" {
		t.Fatalf("expected cancelled workspace result card, got %#v", resultCard)
	}
}

func TestMarkTurnInterruptedCancelsMirroredWorkspaceRequests(t *testing.T) {
	app := newOrchestratorTestApp(t)

	workspaceSessionID := "workspace-1"
	workerSessionID := "worker-1"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-1",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.EnsureSessionWithMeta(workerSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorker,
		Visible:            false,
		MissionID:          "mission-1",
		WorkspaceSessionID: workspaceSessionID,
		WorkerHostID:       "host-1",
		RuntimePreset:      model.SessionRuntimePresetWorker,
	})
	now := model.NowString()
	approval := model.ApprovalRequest{
		ID:          "approval-1",
		HostID:      "host-1",
		Type:        "command",
		Status:      "pending",
		ItemID:      "approval-card-1",
		Command:     "systemctl restart nginx",
		RequestedAt: now,
	}
	choice := model.ChoiceRequest{
		ID:          "choice-1",
		Status:      "pending",
		Questions:   []model.ChoiceQuestion{{Question: "继续执行?"}},
		RequestedAt: now,
	}
	app.store.AddApproval(workerSessionID, approval)
	app.store.AddApproval(workspaceSessionID, approval)
	app.store.UpsertCard(workspaceSessionID, model.Card{
		ID:        approval.ItemID,
		Type:      "CommandApprovalCard",
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	})
	app.store.AddChoice(workerSessionID, choice)
	app.store.AddChoice(workspaceSessionID, choice)
	app.store.UpsertCard(workspaceSessionID, model.Card{
		ID:        choice.ID,
		Type:      "ChoiceCard",
		Status:    "pending",
		Questions: choice.Questions,
		CreatedAt: now,
		UpdatedAt: now,
	})

	app.startRuntimeTurn(workerSessionID, "host-1")
	app.markTurnInterrupted(workerSessionID, "turn-worker-1")

	if got, ok := app.store.Approval(workspaceSessionID, approval.ID); !ok || got.Status != "cancelled" {
		t.Fatalf("expected mirrored approval to be cancelled, got %#v, %v", got, ok)
	}
	if got, ok := app.store.Choice(workspaceSessionID, choice.ID); !ok || got.Status != "cancelled" {
		t.Fatalf("expected mirrored choice to be cancelled, got %#v, %v", got, ok)
	}
	if card := app.cardByID(workspaceSessionID, approval.ItemID); card == nil || card.Status != "cancelled" {
		t.Fatalf("expected mirrored approval card cancelled, got %#v", card)
	}
	if card := app.cardByID(workspaceSessionID, choice.ID); card == nil || card.Status != "cancelled" {
		t.Fatalf("expected mirrored choice card cancelled, got %#v", card)
	}
}

func TestReconcileOrchestratorRecoveredWorkersFailsStaleWorkerTurn(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.store.UpsertHost(model.Host{
		ID:              "host-1",
		Name:            "host-1",
		Kind:            "linux",
		Status:          "online",
		Executable:      true,
		TerminalCapable: true,
	})

	workspaceSessionID := "workspace-1"
	plannerSessionID := "planner-1"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-1",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})

	mission, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:          "mission-1",
		WorkspaceSessionID: workspaceSessionID,
		PlannerSessionID:   plannerSessionID,
		Title:              "demo",
		Summary:            "demo summary",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	_, err = app.orchestrator.Dispatch(context.Background(), orchestrator.DispatchRequest{
		MissionID: mission.ID,
		Tasks: []orchestrator.DispatchTaskRequest{
			{
				TaskID:      "task-1",
				HostID:      "host-1",
				Title:       "inspect host",
				Instruction: "inspect nginx status",
			},
		},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	updatedMission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok {
		t.Fatalf("expected mission after dispatch")
	}
	worker := updatedMission.Workers["host-1"]
	if worker == nil {
		t.Fatalf("expected worker for host-1")
	}
	app.ensureInternalSessionFromWorkspace(worker.SessionID, workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorker,
		Visible:            false,
		MissionID:          mission.ID,
		WorkspaceSessionID: workspaceSessionID,
		WorkerHostID:       "host-1",
		RuntimePreset:      model.SessionRuntimePresetWorker,
	}, "host-1")
	app.startRuntimeTurn(worker.SessionID, "host-1")

	app.reconcileOrchestratorRecoveredWorkers()

	reconciledMission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok {
		t.Fatalf("expected reconciled mission")
	}
	if reconciledMission.Status != orchestrator.MissionStatusFailed {
		t.Fatalf("expected failed mission after restart reconcile, got %s", reconciledMission.Status)
	}
	if got := reconciledMission.Tasks["task-1"]; got == nil || got.Status != orchestrator.TaskStatusFailed {
		t.Fatalf("expected task-1 failed, got %#v", got)
	}
	if got := reconciledMission.Workers["host-1"]; got == nil || got.Status != orchestrator.WorkerStatusFailed {
		t.Fatalf("expected failed worker after restart reconcile, got %#v", got)
	}
	if session := app.store.Session(worker.SessionID); session == nil || session.Runtime.Turn.Phase != "failed" {
		t.Fatalf("expected worker runtime failed, got %#v", session)
	}

	resultCard := app.cardByID(workspaceSessionID, "workspace-result-mission-1")
	if resultCard == nil || resultCard.Status != "failed" {
		t.Fatalf("expected failed workspace result card, got %#v", resultCard)
	}
	workerResultCard := app.cardByID(workspaceSessionID, "worker-result-task-1")
	if workerResultCard == nil || workerResultCard.Status != "failed" {
		t.Fatalf("expected failed worker result card, got %#v", workerResultCard)
	}
}

func TestReconcileOrchestratorAfterLoadFailsWorkerWithoutThread(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.store.UpsertHost(model.Host{
		ID:              "host-1",
		Name:            "host-1",
		Kind:            "linux",
		Status:          "online",
		Executable:      true,
		TerminalCapable: true,
	})

	workspaceSessionID := "workspace-restart-worker"
	plannerSessionID := "planner-restart-worker"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-restart-worker",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})

	mission, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:          "mission-restart-worker",
		WorkspaceSessionID: workspaceSessionID,
		PlannerSessionID:   plannerSessionID,
		Title:              "restart worker",
		Summary:            "restart worker",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	_, err = app.orchestrator.Dispatch(context.Background(), orchestrator.DispatchRequest{
		MissionID: mission.ID,
		Tasks: []orchestrator.DispatchTaskRequest{
			{
				TaskID:      "task-1",
				HostID:      "host-1",
				Title:       "inspect host",
				Instruction: "inspect nginx status",
			},
		},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	updatedMission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok {
		t.Fatalf("expected mission after dispatch")
	}
	worker := updatedMission.Workers["host-1"]
	if worker == nil {
		t.Fatalf("expected worker for host-1")
	}
	app.ensureInternalSessionFromWorkspace(worker.SessionID, workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorker,
		Visible:            false,
		MissionID:          mission.ID,
		WorkspaceSessionID: workspaceSessionID,
		WorkerHostID:       "host-1",
		RuntimePreset:      model.SessionRuntimePresetWorker,
	}, "host-1")
	app.startRuntimeTurn(worker.SessionID, "host-1")
	app.store.ClearThread(worker.SessionID)

	app.reconcileOrchestratorAfterLoad()

	reconciledMission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok {
		t.Fatalf("expected reconciled mission")
	}
	if reconciledMission.Status != orchestrator.MissionStatusFailed {
		t.Fatalf("expected failed mission after restart reconcile, got %s", reconciledMission.Status)
	}
	if got := reconciledMission.Tasks["task-1"]; got == nil || got.Status != orchestrator.TaskStatusFailed {
		t.Fatalf("expected task-1 failed, got %#v", got)
	}
	card := app.cardByID(workspaceSessionID, "workspace-reconcile-host-host-1")
	if card == nil {
		t.Fatalf("expected worker restart reconcile card")
	}
	if card.Type != "ResultSummaryCard" || card.Status != "failed" {
		t.Fatalf("expected failed restart reconcile card, got %#v", card)
	}
}

func TestReconcileOrchestratorAfterLoadFailsPlannerWithoutThread(t *testing.T) {
	app := newOrchestratorTestApp(t)

	workspaceSessionID := "workspace-restart-planner"
	plannerSessionID := "planner-restart-planner"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-restart-planner",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})

	mission, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:          "mission-restart-planner",
		WorkspaceSessionID: workspaceSessionID,
		PlannerSessionID:   plannerSessionID,
		Title:              "restart planner",
		Summary:            "restart planner",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	app.ensureInternalSessionFromWorkspace(plannerSessionID, workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindPlanner,
		Visible:            false,
		MissionID:          mission.ID,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetPlanner,
	}, model.ServerLocalHostID)
	app.startRuntimeTurn(plannerSessionID, model.ServerLocalHostID)
	app.store.ClearThread(plannerSessionID)

	app.reconcileOrchestratorAfterLoad()

	reconciledMission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok {
		t.Fatalf("expected reconciled mission")
	}
	if reconciledMission.Status != orchestrator.MissionStatusFailed {
		t.Fatalf("expected failed mission after planner reconcile, got %s", reconciledMission.Status)
	}
	card := app.cardByID(workspaceSessionID, "workspace-reconcile-"+plannerSessionID)
	if card == nil {
		t.Fatalf("expected planner restart reconcile card")
	}
	if card.Type != "ResultSummaryCard" || card.Status != "failed" {
		t.Fatalf("expected failed planner reconcile card, got %#v", card)
	}
}
