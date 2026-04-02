package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/orchestrator"
)

func TestWorkspaceMissionHistoryEndpoints(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.store.UpsertHost(model.Host{
		ID:              "host-1",
		Name:            "host-1",
		Kind:            "linux",
		Status:          "online",
		Executable:      true,
		TerminalCapable: true,
	})

	ctx := context.Background()
	mission, err := app.orchestrator.StartMission(ctx, orchestrator.StartMissionRequest{
		WorkspaceSessionID: "workspace-1",
		PlannerSessionID:   "planner-1",
		Title:              "Deploy nginx",
		Summary:            "roll out nginx reload",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	_, err = app.orchestrator.Dispatch(ctx, orchestrator.DispatchRequest{
		MissionID:    mission.ID,
		MissionTitle: mission.Title,
		Summary:      mission.Summary,
		Tasks: []orchestrator.DispatchTaskRequest{{
			TaskID:      "task-1",
			HostID:      "host-1",
			Title:       "reload nginx",
			Instruction: "sudo nginx -s reload",
		}},
	})
	if err != nil {
		t.Fatalf("dispatch mission: %v", err)
	}

	now := model.NowString()
	app.store.UpsertCard("planner-1", model.Card{
		ID:        "planner-user-history-1",
		Type:      "UserMessageCard",
		Role:      "user",
		Text:      "先检查 nginx 状态，再决定是否 reload。",
		CreatedAt: now,
		UpdatedAt: now,
	})
	app.store.UpsertCard("planner-1", model.Card{
		ID:        "planner-assistant-history-1",
		Type:      "AssistantMessageCard",
		Role:      "assistant",
		Text:      "已生成单机检查步骤，并准备 reload 审批锚点。",
		CreatedAt: now,
		UpdatedAt: now,
	})

	projectedMission, ok := app.orchestrator.Mission(mission.ID)
	if !ok || projectedMission == nil {
		t.Fatalf("expected projected mission")
	}
	worker := projectedMission.Workers["host-1"]
	if worker == nil {
		t.Fatalf("expected worker for host-1")
	}
	app.store.UpsertCard(worker.SessionID, model.Card{
		ID:        "cmd-history-host-1",
		Type:      "CommandCard",
		Title:     "reload nginx",
		Command:   "sudo nginx -s reload",
		Cwd:       "/srv/app",
		Status:    "pending",
		Summary:   "等待 reload 审批",
		Output:    "reload pending approval",
		CreatedAt: now,
		UpdatedAt: now,
	})
	app.store.UpsertCard(worker.SessionID, model.Card{
		ID:        "assistant-history-host-1",
		Type:      "AssistantMessageCard",
		Role:      "assistant",
		Text:      "当前只差审批即可继续执行 reload。",
		CreatedAt: now,
		UpdatedAt: now,
	})
	app.store.AddApproval(worker.SessionID, model.ApprovalRequest{
		ID:          "approval-history-1",
		HostID:      "host-1",
		Type:        "command",
		Status:      "pending",
		ItemID:      "cmd-history-host-1",
		Command:     "sudo nginx -s reload",
		Reason:      "需要确认 reload 风险",
		RequestedAt: now,
	})

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/workspace/missions?limit=5", nil)
	listRec := httptest.NewRecorder()
	app.withBrowserSession(app.handleWorkspaceMissionHistory)(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from list endpoint, got %d", listRec.Code)
	}

	var listResp workspaceMissionHistoryListResponse
	if err := json.NewDecoder(listRec.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if listResp.Total != 1 || listResp.Limit != 5 || len(listResp.Items) != 1 {
		t.Fatalf("unexpected list response: %#v", listResp)
	}
	if listResp.Items[0].ID != mission.ID || listResp.Items[0].TaskCount != 1 || listResp.Items[0].WorkerCount != 1 {
		t.Fatalf("unexpected mission summary item: %#v", listResp.Items[0])
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/v1/workspace/missions/"+mission.ID, nil)
	detailRec := httptest.NewRecorder()
	app.withBrowserSession(app.handleWorkspaceMissionHistoryDetail)(detailRec, detailReq)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from detail endpoint, got %d", detailRec.Code)
	}

	var detailResp workspaceMissionHistoryDetailResponse
	if err := json.NewDecoder(detailRec.Body).Decode(&detailResp); err != nil {
		t.Fatalf("decode detail response: %v", err)
	}
	if detailResp.Mission.ID != mission.ID {
		t.Fatalf("unexpected mission detail id: %#v", detailResp.Mission)
	}
	if len(detailResp.Mission.Report.OverviewRows) == 0 {
		t.Fatalf("expected non-empty report overview rows")
	}
	if len(detailResp.Mission.Tasks) != 1 || len(detailResp.Mission.Workers) != 1 {
		t.Fatalf("unexpected detail body: %#v", detailResp.Mission)
	}
	if len(detailResp.Mission.TaskBindings) != 1 || detailResp.Mission.TaskBindings[0].TaskID != "task-1" {
		t.Fatalf("expected one task binding, got %#v", detailResp.Mission.TaskBindings)
	}
	if len(detailResp.Mission.Workers[0].Conversation) == 0 {
		t.Fatalf("expected worker conversation excerpts, got %#v", detailResp.Mission.Workers[0].Conversation)
	}
	if detailResp.Mission.Workers[0].Terminal["output"] != "reload pending approval" {
		t.Fatalf("expected enriched worker terminal output, got %#v", detailResp.Mission.Workers[0].Terminal)
	}
	if detailResp.Mission.Workers[0].ApprovalAnchor == nil || detailResp.Mission.Workers[0].ApprovalAnchor.ApprovalID != "approval-history-1" {
		t.Fatalf("expected approval anchor, got %#v", detailResp.Mission.Workers[0].ApprovalAnchor)
	}
}
