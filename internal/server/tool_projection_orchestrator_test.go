package server

import (
	"context"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/orchestrator"
)

func TestOrchestratorProjectionSyncsWorkerWaitingApprovalFromEvent(t *testing.T) {
	app, workspaceSessionID, workerSessionID := setupWorkerMissionForToolProjection(t)

	if err := NewOrchestratorToolProjection(app).HandleToolLifecycleEvent(context.Background(), ToolLifecycleEvent{
		Type:      ToolLifecycleEventApprovalRequested,
		SessionID: workerSessionID,
		Phase:     "waiting_approval",
	}); err != nil {
		t.Fatalf("project orchestrator approval requested event: %v", err)
	}

	mission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok || mission == nil {
		t.Fatalf("expected mission for workspace %s", workspaceSessionID)
	}
	worker := mission.Workers["host-1"]
	if worker == nil || worker.Status != orchestrator.WorkerStatusWaiting {
		t.Fatalf("expected worker status waiting, got %#v", worker)
	}
	task := mission.Tasks["task-1"]
	if task == nil || task.Status != orchestrator.TaskStatusWaitingApproval {
		t.Fatalf("expected task waiting approval, got %#v", task)
	}
	if phase := app.store.Session(workspaceSessionID).Runtime.Turn.Phase; phase != "waiting_approval" {
		t.Fatalf("expected workspace runtime phase waiting_approval, got %q", phase)
	}
}

func TestOrchestratorProjectionRegistersApprovalRouteAndMirrorsWorkerApproval(t *testing.T) {
	app, workspaceSessionID, workerSessionID := setupWorkerMissionForToolProjection(t)
	approvalID := "approval-tool-projection-route"
	cardID := "approval-card-tool-projection-route"

	if err := NewOrchestratorToolProjection(app).HandleToolLifecycleEvent(context.Background(), ToolLifecycleEvent{
		Type:       ToolLifecycleEventApprovalRequested,
		SessionID:  workerSessionID,
		Phase:      "waiting_approval",
		ToolName:   "execute_command",
		ApprovalID: approvalID,
		CardID:     cardID,
		Payload: map[string]any{
			"approval": map[string]any{
				"approvalId":   approvalID,
				"approvalType": "remote_command",
				"hostId":       "host-1",
				"status":       "pending",
				"itemId":       cardID,
				"command":      "sudo systemctl restart nginx",
				"reason":       "restart nginx",
				"requestedAt":  "2026-04-17T10:00:00Z",
				"decisions":    []any{"accept", "accept_session", "decline"},
			},
			"card": map[string]any{
				"cardId":    cardID,
				"cardType":  "CommandApprovalCard",
				"title":     "Remote command approval required",
				"text":      "restart nginx",
				"status":    "pending",
				"command":   "sudo systemctl restart nginx",
				"createdAt": "2026-04-17T10:00:00Z",
				"updatedAt": "2026-04-17T10:00:00Z",
			},
		},
	}); err != nil {
		t.Fatalf("project orchestrator approval request event: %v", err)
	}

	route, ok := app.orchestrator.ResolveApprovalRoute(approvalID)
	if !ok || route.WorkerSessionID != workerSessionID {
		t.Fatalf("expected approval route for %s -> %s, got %#v ok=%t", approvalID, workerSessionID, route, ok)
	}
	approval, ok := app.store.Approval(workspaceSessionID, approvalID)
	if !ok {
		t.Fatalf("expected mirrored workspace approval %s", approvalID)
	}
	if approval.ItemID != cardID || approval.Status != "pending" {
		t.Fatalf("unexpected mirrored approval: %#v", approval)
	}
	card := app.cardByID(workspaceSessionID, cardID)
	if card == nil {
		t.Fatalf("expected mirrored workspace approval card %s", cardID)
	}
	if card.Type != "CommandApprovalCard" || card.Status != "pending" {
		t.Fatalf("unexpected mirrored workspace approval card: %#v", card)
	}
}

func TestOrchestratorProjectionSyncsWorkerExecutingFromApprovalResolution(t *testing.T) {
	app, workspaceSessionID, workerSessionID := setupWorkerMissionForToolProjection(t)

	projection := NewOrchestratorToolProjection(app)
	_ = projection.HandleToolLifecycleEvent(context.Background(), ToolLifecycleEvent{
		Type:      ToolLifecycleEventApprovalRequested,
		SessionID: workerSessionID,
		Phase:     "waiting_approval",
	})
	if err := projection.HandleToolLifecycleEvent(context.Background(), ToolLifecycleEvent{
		Type:      ToolLifecycleEventApprovalResolved,
		SessionID: workerSessionID,
		Phase:     "executing",
	}); err != nil {
		t.Fatalf("project orchestrator approval resolved event: %v", err)
	}

	mission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok || mission == nil {
		t.Fatalf("expected mission for workspace %s", workspaceSessionID)
	}
	worker := mission.Workers["host-1"]
	if worker == nil || worker.Status != orchestrator.WorkerStatusRunning {
		t.Fatalf("expected worker status running, got %#v", worker)
	}
	task := mission.Tasks["task-1"]
	if task == nil || task.Status != orchestrator.TaskStatusRunning {
		t.Fatalf("expected task running, got %#v", task)
	}
	if phase := app.store.Session(workspaceSessionID).Runtime.Turn.Phase; phase != "executing" {
		t.Fatalf("expected workspace runtime phase executing, got %q", phase)
	}
}

func setupWorkerMissionForToolProjection(t *testing.T) (*App, string, string) {
	t.Helper()

	app := newOrchestratorTestApp(t)
	workspaceSessionID := "workspace-tool-projection"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-tool-projection",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	mission, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:          "mission-tool-projection",
		WorkspaceSessionID: workspaceSessionID,
		Title:              "demo",
		Summary:            "demo summary",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	_, err = app.orchestrator.Dispatch(context.Background(), orchestrator.DispatchRequest{
		MissionID: mission.ID,
		Tasks: []orchestrator.DispatchTaskRequest{{
			TaskID:      "task-1",
			HostID:      "host-1",
			Title:       "inspect host",
			Instruction: "inspect nginx status",
		}},
	})
	if err != nil {
		t.Fatalf("dispatch mission task: %v", err)
	}

	updatedMission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok || updatedMission == nil {
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

	return app, workspaceSessionID, worker.SessionID
}
