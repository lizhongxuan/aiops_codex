package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/orchestrator"
)

func TestWorkspaceMissionEndToEndWithApprovalAndBudgetedFanout(t *testing.T) {
	app := newOrchestratorTestApp(t)
	responses := newCapturedCodexResponses()
	app.codexRespondFunc = responses.capture

	var reqMu sync.Mutex
	threadSeq := 0
	turnSeq := 0
	app.codexRequestFunc = func(_ context.Context, method string, _ any, result any) error {
		reqMu.Lock()
		defer reqMu.Unlock()

		payload := map[string]any{}
		switch method {
		case "thread/start":
			threadSeq++
			payload = map[string]any{"thread": map[string]any{"id": fmt.Sprintf("thread-e2e-%02d", threadSeq)}}
		case "turn/start":
			turnSeq++
			payload = map[string]any{"turnId": fmt.Sprintf("turn-e2e-%02d", turnSeq)}
		case "turn/interrupt":
			payload = map[string]any{"ok": true}
		}
		content, _ := json.Marshal(payload)
		return json.Unmarshal(content, result)
	}

	hostIDs := setupBudgetedFanoutHosts(t, app, 32)

	workspaceSessionID := "workspace-e2e"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-e2e",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	mission, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:           "mission-e2e",
		WorkspaceSessionID:  workspaceSessionID,
		Title:               "Budgeted fanout",
		Summary:             "dispatch across 32 hosts with one approval gate",
		GlobalActiveBudget:  32,
		MissionActiveBudget: 4,
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}

	tasks := make([]map[string]any, 0, len(hostIDs))
	for i, hostID := range hostIDs {
		tasks = append(tasks, map[string]any{
			"taskId":      fmt.Sprintf("task-%02d", i+1),
			"hostId":      hostID,
			"title":       "collect nginx status",
			"instruction": fmt.Sprintf("inspect nginx status on %s", hostID),
		})
	}
	app.handleWorkspaceDispatchTasks("raw-dispatch-e2e", workspaceSessionID, map[string]any{"tasks": tasks})

	currentMission := waitForMissionState(t, app, workspaceSessionID, func(m *orchestrator.Mission) bool {
		return m != nil && len(m.Tasks) == len(hostIDs) && countTasksWithStatus(m, orchestrator.TaskStatusRunning) == 4
	}, "initial budgeted dispatch")
	if currentMission.Status != orchestrator.MissionStatusRunning {
		t.Fatalf("expected running mission after dispatch, got %s", currentMission.Status)
	}
	if got := countTasksWithStatus(currentMission, orchestrator.TaskStatusQueued); got != len(hostIDs)-4 {
		t.Fatalf("expected %d queued tasks after dispatch, got %d", len(hostIDs)-4, got)
	}
	if got := len(activeWorkerSessionIDs(app, currentMission.ID)); got != 4 {
		t.Fatalf("expected 4 active worker sessions after dispatch, got %d", got)
	}
	if app.cardByID(workspaceSessionID, "dispatch-"+mission.ID) == nil {
		t.Fatalf("expected dispatch summary card")
	}
	if app.cardByID(workspaceSessionID, "workspace-plan-"+mission.ID) == nil {
		t.Fatalf("expected workspace plan card")
	}
	if app.cardByID(workspaceSessionID, "workspace-worker-"+hostIDs[0]) == nil {
		t.Fatalf("expected first worker progress card")
	}

	approvedHostID := hostIDs[0]
	approvedWorker := currentMission.Workers[approvedHostID]
	if approvedWorker == nil || approvedWorker.Status != orchestrator.WorkerStatusRunning {
		t.Fatalf("expected approved host worker to be running, got %#v", approvedWorker)
	}
	approvedSessionID := approvedWorker.SessionID
	approvedSession := app.store.Session(approvedSessionID)
	if approvedSession == nil || approvedSession.ThreadID == "" || approvedSession.TurnID == "" {
		t.Fatalf("expected approved worker session thread+turn, got %#v", approvedSession)
	}

	approvalCallID := "call-approval-host-01"
	approvalCardID := dynamicToolCardID(approvalCallID)
	approvalID := "approval-e2e-host-01"
	app.store.RememberItem(approvedSessionID, approvalCardID, map[string]any{
		"tool":       "execute_system_mutation",
		"threadId":   approvedSession.ThreadID,
		"turnId":     approvedSession.TurnID,
		"callId":     approvalCallID,
		"command":    "sudo systemctl restart nginx",
		"cwd":        "/tmp",
		"reason":     "restart nginx for validation",
		"timeoutSec": 10,
		"mode":       "command",
		"readonly":   false,
	})
	approval := model.ApprovalRequest{
		ID:           approvalID,
		RequestIDRaw: "raw-approval-e2e",
		HostID:       approvedHostID,
		Fingerprint:  approvalFingerprintForCommand(approvedHostID, "sudo systemctl restart nginx", "/tmp"),
		Type:         "remote_command",
		Status:       "pending",
		ThreadID:     approvedSession.ThreadID,
		TurnID:       approvedSession.TurnID,
		ItemID:       approvalCardID,
		Command:      "sudo systemctl restart nginx",
		Cwd:          "/tmp",
		Reason:       "restart nginx for validation",
		Decisions:    []string{"accept", "accept_session", "decline"},
		RequestedAt:  model.NowString(),
	}
	approvalCard := model.Card{
		ID:      approvalCardID,
		Type:    "CommandApprovalCard",
		Title:   "Remote command approval required",
		Command: approval.Command,
		Cwd:     approval.Cwd,
		Text:    approval.Reason,
		Status:  "pending",
		Approval: &model.ApprovalRef{
			RequestID: approval.ID,
			Type:      approval.Type,
			Decisions: approval.Decisions,
		},
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	}
	applyCardHost(&approvalCard, app.findHost(approvedHostID))
	app.setRuntimeTurnPhase(approvedSessionID, "waiting_approval")
	app.store.AddApproval(approvedSessionID, approval)
	app.store.UpsertCard(approvedSessionID, approvalCard)
	app.recordOrchestratorApprovalRequested(approvedSessionID, approval)
	app.mirrorInternalApprovalToWorkspace(approvedSessionID, approval, approvalCard)

	waitFor(t, 5*time.Second, "workspace mirrored approval", func() bool {
		approval, ok := app.store.Approval(workspaceSessionID, approvalID)
		return ok && approval.Status == "pending"
	})
	if got := len(activeWorkerSessionIDs(app, mission.ID)); got != 4 {
		t.Fatalf("expected waiting approval worker to keep budget occupied, got %d active workers", got)
	}

	activeBeforeApproval := activeWorkerSessionIDs(app, mission.ID)
	for _, sessionID := range activeBeforeApproval {
		if sessionID == approvedSessionID {
			continue
		}
		completeWorkerTask(t, app, sessionID, "readonly inspection done")
	}

	currentMission = waitForMissionState(t, app, workspaceSessionID, func(m *orchestrator.Mission) bool {
		return m != nil &&
			countTasksWithStatus(m, orchestrator.TaskStatusCompleted) == 3 &&
			len(activeWorkerSessionIDs(app, m.ID)) == 4 &&
			m.Workers[approvedHostID] != nil &&
			m.Workers[approvedHostID].Status == orchestrator.WorkerStatusWaiting
	}, "budget refill while one worker waits for approval")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/approvals/"+approvalID+"/decision", strings.NewReader(`{"decision":"accept"}`))
	rec := httptest.NewRecorder()
	app.handleApprovalDecision(rec, req, workspaceSessionID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 approving mirrored worker approval, got %d", rec.Code)
	}

	waitFor(t, 5*time.Second, "approved worker remote command completed", func() bool {
		card := app.cardByID(approvedSessionID, approvalCardID)
		return card != nil && card.Type == "CommandCard" && normalizeCardStatus(card.Status) == "completed"
	})
	waitFor(t, 5*time.Second, "approval raw response", func() bool {
		_, ok := responses.result("raw-approval-e2e")
		return ok
	})
	workspaceApprovalCard := app.cardByID(workspaceSessionID, approvalCardID)
	if workspaceApprovalCard == nil || workspaceApprovalCard.Status != approvalStatusFromDecision("accept") {
		t.Fatalf("expected mirrored workspace approval card accepted, got %#v", workspaceApprovalCard)
	}
	if got := len(activeWorkerSessionIDs(app, mission.ID)); got != 4 {
		t.Fatalf("expected approval acceptance not to exceed mission budget, got %d active workers", got)
	}

	progressDeadline := time.Now().Add(45 * time.Second)
	for {
		currentMission = missionByWorkspaceOrFatal(t, app, workspaceSessionID)
		if currentMission.Status == orchestrator.MissionStatusCompleted {
			break
		}
		active := activeWorkerSessionIDs(app, currentMission.ID)
		if len(active) == 0 {
			if time.Now().After(progressDeadline) {
				t.Fatalf("timed out waiting for queued workers to continue")
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		completedBefore := countTasksWithStatus(currentMission, orchestrator.TaskStatusCompleted)
		for _, sessionID := range active {
			completeWorkerTask(t, app, sessionID, "worker finished successfully")
		}
		waitFor(t, 12*time.Second, "mission batch progress", func() bool {
			next := missionByWorkspaceOrFatal(t, app, workspaceSessionID)
			return next.Status == orchestrator.MissionStatusCompleted || countTasksWithStatus(next, orchestrator.TaskStatusCompleted) > completedBefore
		})
	}

	finalMission := missionByWorkspaceOrFatal(t, app, workspaceSessionID)
	if finalMission.Status != orchestrator.MissionStatusCompleted {
		t.Fatalf("expected completed mission, got %s", finalMission.Status)
	}
	if got := countTasksWithStatus(finalMission, orchestrator.TaskStatusCompleted); got != len(hostIDs) {
		t.Fatalf("expected %d completed tasks, got %d", len(hostIDs), got)
	}
	if got := len(activeWorkerSessionIDs(app, finalMission.ID)); got != 0 {
		t.Fatalf("expected no active worker sessions after completion, got %d", got)
	}

	missionCard := app.cardByID(workspaceSessionID, orchestrator.ProjectMissionCard(finalMission).ID)
	if missionCard == nil || missionCard.Status != "completed" {
		t.Fatalf("expected completed mission projection card, got %#v", missionCard)
	}
	planCard := app.cardByID(workspaceSessionID, "workspace-plan-"+finalMission.ID)
	if planCard == nil || planCard.Status != "completed" {
		t.Fatalf("expected completed plan card, got %#v", planCard)
	}
	if len(planCard.Items) != len(hostIDs) {
		t.Fatalf("expected %d plan items, got %d", len(hostIDs), len(planCard.Items))
	}
	for _, item := range planCard.Items {
		if item.Status != "completed" {
			t.Fatalf("expected every plan item completed, got %#v", planCard.Items)
		}
	}
	firstWorkerCard := app.cardByID(workspaceSessionID, "workspace-worker-"+hostIDs[0])
	lastWorkerCard := app.cardByID(workspaceSessionID, "workspace-worker-"+hostIDs[len(hostIDs)-1])
	if firstWorkerCard == nil || firstWorkerCard.Status != "completed" {
		t.Fatalf("expected first host worker card completed, got %#v", firstWorkerCard)
	}
	if lastWorkerCard == nil || lastWorkerCard.Status != "completed" {
		t.Fatalf("expected last host worker card completed, got %#v", lastWorkerCard)
	}
	resultCard := app.cardByID(workspaceSessionID, "workspace-result-"+finalMission.ID)
	if resultCard == nil || resultCard.Status != "completed" {
		t.Fatalf("expected completed result card, got %#v", resultCard)
	}
	if noticeCard := app.cardByID(workspaceSessionID, "mission-complete-"+finalMission.ID); noticeCard == nil {
		t.Fatalf("expected mission completion notice card")
	}
	dispatchResp, ok := responses.result("raw-dispatch-e2e")
	if !ok || !strings.Contains(asString(dispatchResp["contentItems"]), "accepted=32") {
		t.Fatalf("expected workspace dispatch response summary, got %#v", dispatchResp)
	}
}

func TestWorkspaceChatEndToEndUsesReActLoopAcrossMessages(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.codexRespondFunc = func(_ context.Context, _ string, _ any) error { return nil }

	var reqMu sync.Mutex
	threadSeq := 0
	turnSeq := 0
	var turnInputs []string
	app.codexRequestFunc = func(_ context.Context, method string, params any, result any) error {
		reqMu.Lock()
		defer reqMu.Unlock()

		payload := map[string]any{}
		switch method {
		case "thread/start":
			threadSeq++
			payload = map[string]any{"thread": map[string]any{"id": fmt.Sprintf("thread-chat-flow-%02d", threadSeq)}}
		case "turn/start":
			turnSeq++
			if rawParams, ok := params.(map[string]any); ok {
				if inputItems, ok := rawParams["input"].([]map[string]any); ok && len(inputItems) > 0 {
					turnInputs = append(turnInputs, getStringAny(inputItems[0], "text"))
				}
			}
			payload = map[string]any{"turnId": fmt.Sprintf("turn-chat-flow-%02d", turnSeq)}
		case "turn/interrupt":
			payload = map[string]any{"ok": true}
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
	app.store.UpsertHost(model.Host{
		ID:              "host-2",
		Name:            "host-2",
		Kind:            "linux",
		Status:          "online",
		Executable:      true,
		TerminalCapable: true,
	})

	workspaceSessionID := "workspace-chat-flow"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetSelectedHost(workspaceSessionID, "host-1")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/message", nil)
	rec := httptest.NewRecorder()
	firstMessage := "看下 host-2 的 CPU"
	app.handleWorkspaceChatMessage(rec, req, workspaceSessionID, chatRequest{Message: firstMessage}, time.Now())
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected first chat request accepted, got %d body=%s", rec.Code, rec.Body.String())
	}

	session := app.store.Session(workspaceSessionID)
	if session == nil {
		t.Fatalf("expected workspace session")
	}
	if session.SelectedHostID != "host-1" {
		t.Fatalf("expected first ReAct turn to start from current selected host host-1, got %q", session.SelectedHostID)
	}
	if got := strings.TrimSpace(session.ThreadConfigHash); got != app.workspaceReActThreadConfigHash("host-1") {
		t.Fatalf("expected first ReAct thread config hash, got %q", got)
	}
	if mission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID); ok && mission != nil {
		t.Fatalf("expected no mission before the ReAct agent dispatches work, got %#v", mission)
	}

	now := model.NowString()
	app.store.UpsertCard(workspaceSessionID, model.Card{
		ID:        "assistant-react-readonly",
		Type:      "AssistantMessageCard",
		Role:      "assistant",
		Text:      "host-2 CPU 当前约 12%，没有明显异常。",
		Status:    "completed",
		CreatedAt: now,
		UpdatedAt: now,
	})
	app.handleMissionTurnCompleted(workspaceSessionID, "completed")

	session = app.store.Session(workspaceSessionID)
	if session == nil || session.Runtime.Turn.Active {
		t.Fatalf("expected first ReAct turn to finish before next user message, got %#v", session)
	}

	rec = httptest.NewRecorder()
	secondMessage := "请开始规划一次 host-2 的 nginx 巡检，并在需要时协调 worker"
	app.handleWorkspaceChatMessage(rec, req, workspaceSessionID, chatRequest{Message: secondMessage}, time.Now())
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected second chat request accepted, got %d body=%s", rec.Code, rec.Body.String())
	}

	session = app.store.Session(workspaceSessionID)
	if session == nil {
		t.Fatalf("expected workspace session after second chat")
	}
	if session.SelectedHostID != "host-1" {
		t.Fatalf("expected second ReAct turn to keep the selected host context host-1, got %q", session.SelectedHostID)
	}
	if got := strings.TrimSpace(session.ThreadConfigHash); got != app.workspaceReActThreadConfigHash("host-1") {
		t.Fatalf("expected second turn to reuse the workspace ReAct thread, got %q", got)
	}
	if mission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID); ok && mission != nil {
		t.Fatalf("expected ReAct agent to wait for explicit dispatch tool use before creating a mission, got %#v", mission)
	}

	reqMu.Lock()
	recordedInputs := append([]string(nil), turnInputs...)
	startedThreads := threadSeq
	reqMu.Unlock()
	if startedThreads != 1 {
		t.Fatalf("expected one ReAct thread reused across both turns, got %d", startedThreads)
	}
	if len(recordedInputs) != 2 {
		t.Fatalf("expected 2 started turns in the ReAct loop, got %#v", recordedInputs)
	}
	if recordedInputs[0] != firstMessage || recordedInputs[1] != secondMessage {
		t.Fatalf("unexpected turn inputs sequence: %#v", recordedInputs)
	}
}

type capturedCodexResponses struct {
	mu      sync.Mutex
	results map[string]map[string]any
}

func newCapturedCodexResponses() *capturedCodexResponses {
	return &capturedCodexResponses{
		results: make(map[string]map[string]any),
	}
}

func (c *capturedCodexResponses) capture(_ context.Context, rawID string, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.results[rawID] = normalizeAnyMap(result)
	return nil
}

func (c *capturedCodexResponses) result(rawID string) (map[string]any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	result, ok := c.results[rawID]
	if !ok {
		return nil, false
	}
	return normalizeAnyMap(result), true
}

func setupBudgetedFanoutHosts(t *testing.T, app *App, count int) []string {
	t.Helper()
	hostIDs := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		hostID := fmt.Sprintf("host-%02d", i)
		hostIDs = append(hostIDs, hostID)
		app.store.UpsertHost(model.Host{
			ID:              hostID,
			Name:            hostID,
			Kind:            "linux",
			Status:          "online",
			Executable:      true,
			TerminalCapable: true,
		})
		stream := &remoteStatusUnifyAgentStream{
			onSend: func(currentHostID string) func(msg *agentrpc.Envelope) error {
				return func(msg *agentrpc.Envelope) error {
					if msg.Kind != "exec/start" || msg.ExecStart == nil {
						return nil
					}
					app.handleAgentExecExit(currentHostID, &agentrpc.ExecExit{
						ExecID:   msg.ExecStart.ExecID,
						Status:   "completed",
						ExitCode: 0,
						Stdout:   "ok",
					})
					return nil
				}
			}(hostID),
		}
		app.setAgentConnection(hostID, &agentConnection{hostID: hostID, stream: stream})
	}
	return hostIDs
}

func missionByWorkspaceOrFatal(t *testing.T, app *App, workspaceSessionID string) *orchestrator.Mission {
	t.Helper()
	mission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok || mission == nil {
		t.Fatalf("expected mission for workspace %s", workspaceSessionID)
	}
	return mission
}

func waitForMissionState(t *testing.T, app *App, workspaceSessionID string, fn func(*orchestrator.Mission) bool, desc string) *orchestrator.Mission {
	t.Helper()
	var matched *orchestrator.Mission
	waitFor(t, 15*time.Second, desc, func() bool {
		mission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
		if !ok || mission == nil {
			return false
		}
		if fn(mission) {
			matched = mission
			return true
		}
		return false
	})
	return matched
}

func waitForPendingApproval(t *testing.T, app *App, sessionID, desc string) string {
	t.Helper()
	var approvalID string
	waitFor(t, 5*time.Second, desc, func() bool {
		approvalID = pendingApprovalID(app, sessionID)
		return approvalID != ""
	})
	return approvalID
}

func pendingApprovalID(app *App, sessionID string) string {
	session := app.store.Session(sessionID)
	if session == nil || len(session.Approvals) == 0 {
		return ""
	}
	ids := make([]string, 0, len(session.Approvals))
	for id, approval := range session.Approvals {
		if approval.Status == "pending" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func completeWorkerTask(t *testing.T, app *App, sessionID, text string) {
	t.Helper()
	now := model.NowString()
	app.store.UpsertCard(sessionID, model.Card{
		ID:        model.NewID("assistant"),
		Type:      "AssistantMessageCard",
		Role:      "assistant",
		Text:      text,
		Status:    "completed",
		CreatedAt: now,
		UpdatedAt: now,
	})
	app.handleMissionTurnCompleted(sessionID, "completed")
}

func activeWorkerSessionIDs(app *App, missionID string) []string {
	sessionIDs := make([]string, 0)
	for _, sessionID := range app.store.SessionIDs() {
		meta := app.sessionMeta(sessionID)
		if meta.Kind != model.SessionKindWorker || meta.MissionID != missionID {
			continue
		}
		session := app.store.Session(sessionID)
		if session == nil || !session.Runtime.Turn.Active {
			continue
		}
		sessionIDs = append(sessionIDs, sessionID)
	}
	sort.Strings(sessionIDs)
	return sessionIDs
}

func countTasksWithStatus(mission *orchestrator.Mission, status orchestrator.TaskStatus) int {
	if mission == nil {
		return 0
	}
	count := 0
	for _, task := range mission.Tasks {
		if task != nil && task.Status == status {
			count++
		}
	}
	return count
}

func normalizeAnyMap(value any) map[string]any {
	content, _ := json.Marshal(value)
	if len(content) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(content, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		content, _ := json.Marshal(typed)
		return string(content)
	}
}

func waitFor(t *testing.T, timeout time.Duration, desc string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", desc)
}
