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

func containsStringValue(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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
	if len(targetChoice.Answers) != 1 || targetChoice.Answers[0].Value != "safe" {
		t.Fatalf("expected worker choice answer to be saved, got %#v", targetChoice.Answers)
	}
	workspaceCard := app.cardByID(workspaceSessionID, choice.ItemID)
	if workspaceCard == nil || workspaceCard.Status != "completed" {
		t.Fatalf("expected mirrored workspace choice card completed, got %#v", workspaceCard)
	}
	if got := strings.Join(workspaceCard.AnswerSummary, " "); !strings.Contains(got, "保守模式") {
		t.Fatalf("expected answer summary to contain chosen label, got %#v", workspaceCard.AnswerSummary)
	}
	workspaceChoice, ok := app.store.Choice(workspaceSessionID, choice.ID)
	if !ok || len(workspaceChoice.Answers) != 1 || workspaceChoice.Answers[0].Value != "safe" {
		t.Fatalf("expected mirrored workspace choice answer to be saved, got %#v ok=%v", workspaceChoice.Answers, ok)
	}
	session := app.store.Session(workerSessionID)
	if session == nil || session.Runtime.Turn.Phase != "thinking" {
		t.Fatalf("expected worker runtime phase thinking, got %#v", session)
	}
	if respondedRawID != choice.RequestIDRaw {
		t.Fatalf("expected codex response to original raw id, got %q", respondedRawID)
	}
	decodedPayload := decodeStructuredToolResponsePayload(t, respondedPayload)
	answers, ok := decodedPayload["answers"].([]any)
	if !ok || len(answers) != 1 {
		t.Fatalf("expected one answer in codex response, got %#v", decodedPayload["answers"])
	}
}

func TestWorkspaceStateQueryAnswersFromAIServerProjection(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.codexRespondFunc = func(_ context.Context, _ string, _ any) error { return nil }
	(&runtimeStartStub{
		startThread: func(_ context.Context, _ string, _ threadStartSpec) (string, error) {
			return "thread-workspace-state-1", nil
		},
		startTurn: func(_ context.Context, _ string, _ string, _ turnStartSpec) (string, error) {
			return "turn-workspace-state-1", nil
		},
	}).install(app)

	workspaceSessionID := "workspace-state-query"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.UpsertHost(model.Host{ID: "web-01", Name: "web-01", Kind: "remote", Status: "online", Executable: true})
	app.store.UpsertHost(model.Host{ID: "db-04", Name: "db-04", Kind: "remote", Status: "offline", Executable: true})
	app.store.AddApproval(workspaceSessionID, model.ApprovalRequest{
		ID:          "approval-state-1",
		HostID:      "server-local",
		Type:        "command",
		Status:      "pending",
		Command:     `/bin/zsh -lc "find .. -maxdepth 2"`,
		RequestedAt: model.NowString(),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/message", nil)
	rec := httptest.NewRecorder()
	app.handleWorkspaceChatMessage(rec, req, workspaceSessionID, chatRequest{Message: "有哪些主机在线"}, time.Now())
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rec.Code, rec.Body.String())
	}

	if mission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID); ok && mission != nil {
		t.Fatalf("expected state query route without starting mission, got %#v", mission)
	}

	session := app.store.Session(workspaceSessionID)
	if session == nil {
		t.Fatalf("expected workspace session")
	}
	if got := strings.TrimSpace(session.ThreadConfigHash); !strings.HasSuffix(got, ":workspace-"+reActLoopVersion) {
		t.Fatalf("expected workspace ReAct thread config hash, got %q", got)
	}
	for _, card := range session.Cards {
		if card.Type == "NoticeCard" && strings.Contains(card.Title, "主 Agent 正在思考") {
			t.Fatalf("did not expect route-thinking notice, got %#v", card)
		}
	}
}

func TestWorkspaceReadonlyQuestionUsesSelectedRemoteHostDirectly(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.codexRespondFunc = func(_ context.Context, _ string, _ any) error { return nil }
	(&runtimeStartStub{
		startThread: func(_ context.Context, _ string, _ threadStartSpec) (string, error) {
			return "thread-readonly-1", nil
		},
		startTurn: func(_ context.Context, _ string, _ string, _ turnStartSpec) (string, error) {
			return "turn-readonly-1", nil
		},
	}).install(app)

	app.store.UpsertHost(model.Host{
		ID:              "host-1",
		Name:            "host-1",
		Kind:            "linux",
		Status:          "online",
		Executable:      true,
		TerminalCapable: true,
	})
	workspaceSessionID := "workspace-readonly"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetSelectedHost(workspaceSessionID, "host-1")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/message", nil)
	rec := httptest.NewRecorder()
	app.handleWorkspaceChatMessage(rec, req, workspaceSessionID, chatRequest{Message: "看下 CPU"}, time.Now())
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected accepted readonly request, got %d body=%s", rec.Code, rec.Body.String())
	}

	if mission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID); ok && mission != nil {
		t.Fatalf("expected readonly host query without mission, got %#v", mission)
	}
	session := app.store.Session(workspaceSessionID)
	if session == nil || session.SelectedHostID != "host-1" {
		t.Fatalf("expected selected host host-1, got %#v", session)
	}
}

func TestWorkspaceRouteThreadOnlyExposesAIServerStateTool(t *testing.T) {
	app := newOrchestratorTestApp(t)

	spec := app.buildWorkspaceRouteThreadStartSpec(context.Background(), "workspace-route-tools", "host-1")

	var toolNames []string
	for _, tool := range spec.DynamicTools {
		toolNames = append(toolNames, strings.TrimSpace(getStringAny(tool, "name")))
	}
	if len(toolNames) != 1 || toolNames[0] != "query_ai_server_state" {
		t.Fatalf("expected only query_ai_server_state on route thread, got %#v", toolNames)
	}
}

func TestWorkspaceRouteCompletionStartsReadonlyTurnForTargetHost(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.codexRespondFunc = func(_ context.Context, _ string, _ any) error { return nil }

	runtimeStub := &runtimeStartStub{
		startThread: func(_ context.Context, _ string, _ threadStartSpec) (string, error) {
			return "thread-workspace-readonly-target", nil
		},
		startTurn: func(_ context.Context, _ string, _ string, _ turnStartSpec) (string, error) {
			return "turn-workspace-readonly-target", nil
		},
	}
	runtimeStub.install(app)

	workspaceSessionID := "workspace-route-host-readonly"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetSelectedHost(workspaceSessionID, "host-1")
	app.store.SetThread(workspaceSessionID, "thread-workspace-route-old")
	app.store.SetThreadConfigHash(workspaceSessionID, app.workspaceRouteThreadConfigHash("host-1"))

	now := model.NowString()
	app.store.UpsertCard(workspaceSessionID, model.Card{
		ID:        "msg-user-host-readonly",
		Type:      "UserMessageCard",
		Role:      "user",
		Text:      "看下 host-2 的 CPU",
		Status:    "completed",
		CreatedAt: now,
		UpdatedAt: now,
	})
	app.store.UpsertCard(workspaceSessionID, model.Card{
		ID:        "msg-assistant-host-readonly",
		Type:      "AssistantMessageCard",
		Role:      "assistant",
		Text:      "```json\n{\"route\":\"host_readonly\",\"reason\":\"single-host readonly check\",\"targetHostId\":\"host-2\",\"needsPlan\":false,\"needsWorker\":false}\n```\n我先切到目标主机做只读检查。",
		Status:    "completed",
		CreatedAt: now,
		UpdatedAt: now,
	})

	app.handleMissionTurnCompleted(workspaceSessionID, "completed")

	if mission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID); ok && mission != nil {
		t.Fatalf("expected no mission for host_readonly route, got %#v", mission)
	}
	session := app.store.Session(workspaceSessionID)
	if session == nil {
		t.Fatalf("expected workspace session")
	}
	if session.SelectedHostID != "host-2" {
		t.Fatalf("expected selected host to switch to host-2, got %q", session.SelectedHostID)
	}
	if session.ThreadID != "thread-workspace-readonly-target" {
		t.Fatalf("expected readonly thread to replace route thread, got %q", session.ThreadID)
	}
	if got := strings.TrimSpace(session.ThreadConfigHash); got != app.workspaceReadonlyThreadConfigHash("host-2") {
		t.Fatalf("expected readonly thread config hash, got %q", got)
	}
	threadCalls := runtimeStub.threadStartCalls()
	turnCalls := runtimeStub.turnStartCalls()
	if len(threadCalls) == 0 {
		t.Fatalf("expected readonly thread to start")
	}
	if len(turnCalls) == 0 {
		t.Fatalf("expected readonly turn to start")
	}

	dynamicTools := threadCalls[len(threadCalls)-1].Spec.DynamicTools
	var threadToolNames []string
	for _, tool := range dynamicTools {
		threadToolNames = append(threadToolNames, strings.TrimSpace(getStringAny(tool, "name")))
	}
	if !containsStringValue(threadToolNames, "query_ai_server_state") {
		t.Fatalf("expected readonly thread to expose query_ai_server_state, got %#v", threadToolNames)
	}
	if !containsStringValue(threadToolNames, "execute_readonly_query") {
		t.Fatalf("expected readonly thread to expose readonly remote tools, got %#v", threadToolNames)
	}
	if containsStringValue(threadToolNames, "orchestrator_dispatch_tasks") {
		t.Fatalf("did not expect readonly thread to expose orchestrator dispatch, got %#v", threadToolNames)
	}
	if got := turnCalls[len(turnCalls)-1].ThreadID; got != "thread-workspace-readonly-target" {
		t.Fatalf("expected readonly turn to start on readonly thread, got %q", got)
	}
}

func TestWorkspaceRouteCompletionNotificationDoesNotBlockCodexReadLoop(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.codexRespondFunc = func(_ context.Context, _ string, _ any) error { return nil }

	threadStartCalled := make(chan struct{}, 1)
	turnStartCalled := make(chan struct{}, 1)
	allowThreadStart := make(chan struct{})
	(&runtimeStartStub{
		startThread: func(ctx context.Context, _ string, _ threadStartSpec) (string, error) {
			select {
			case threadStartCalled <- struct{}{}:
			default:
			}
			select {
			case <-allowThreadStart:
				return "thread-readonly-after-route", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
		startTurn: func(_ context.Context, _ string, _ string, _ turnStartSpec) (string, error) {
			select {
			case turnStartCalled <- struct{}{}:
			default:
			}
			return "turn-readonly-after-route", nil
		},
	}).install(app)

	workspaceSessionID := "workspace-route-notification-async"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetSelectedHost(workspaceSessionID, model.ServerLocalHostID)
	app.store.SetThread(workspaceSessionID, "thread-workspace-route-async")
	app.store.SetThreadConfigHash(workspaceSessionID, app.workspaceRouteThreadConfigHash(model.ServerLocalHostID))
	app.store.SetTurn(workspaceSessionID, "turn-workspace-route-async")

	now := model.NowString()
	app.store.UpsertCard(workspaceSessionID, model.Card{
		ID:        "msg-user-route-async",
		Type:      "UserMessageCard",
		Role:      "user",
		Text:      "继续查看主机的系统状态",
		Status:    "completed",
		CreatedAt: now,
		UpdatedAt: now,
	})
	app.store.UpsertCard(workspaceSessionID, model.Card{
		ID:        "msg-assistant-route-async",
		Type:      "AssistantMessageCard",
		Role:      "assistant",
		Text:      "```json\n{\"route\":\"host_readonly\",\"reason\":\"single-host readonly check\",\"targetHostId\":\"server-local\",\"needsPlan\":false,\"needsWorker\":false}\n```\n我将继续对 `server-local` 做只读系统状态检查。",
		Status:    "completed",
		CreatedAt: now,
		UpdatedAt: now,
	})
	app.startRuntimeTurn(workspaceSessionID, model.ServerLocalHostID)

	handlerDone := make(chan struct{})
	go func() {
		app.handleMissionTurnCompletedAsync(workspaceSessionID, "completed", true)
		close(handlerDone)
	}()

	select {
	case <-threadStartCalled:
	case <-time.After(time.Second):
		t.Fatal("expected readonly thread/start request")
	}
	select {
	case <-handlerDone:
	case <-time.After(200 * time.Millisecond):
		close(allowThreadStart)
		t.Fatal("turn/completed notification handler blocked on a nested codex request")
	}

	close(allowThreadStart)
	select {
	case <-turnStartCalled:
	case <-time.After(time.Second):
		t.Fatal("expected readonly turn/start request after releasing thread/start")
	}

	session := app.store.Session(workspaceSessionID)
	if session == nil || session.ThreadID != "thread-readonly-after-route" {
		t.Fatalf("expected readonly thread to be bound, got %#v", session)
	}
	if got := strings.TrimSpace(session.ThreadConfigHash); got != app.workspaceReadonlyThreadConfigHash(model.ServerLocalHostID) {
		t.Fatalf("expected readonly thread config hash, got %q", got)
	}
	time.Sleep(500 * time.Millisecond)
}

func TestWorkspaceRouteTurnResetsReadonlyThreadBinding(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.codexRespondFunc = func(_ context.Context, _ string, _ any) error { return nil }
	(&runtimeStartStub{
		startThread: func(_ context.Context, _ string, _ threadStartSpec) (string, error) {
			return "thread-workspace-route-fresh", nil
		},
		startTurn: func(_ context.Context, _ string, _ string, _ turnStartSpec) (string, error) {
			return "turn-workspace-route-fresh", nil
		},
	}).install(app)

	workspaceSessionID := "workspace-readonly-then-route"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetSelectedHost(workspaceSessionID, "host-1")
	app.store.SetThread(workspaceSessionID, "thread-workspace-readonly-old")
	app.store.SetThreadConfigHash(workspaceSessionID, app.workspaceReadonlyThreadConfigHash("host-1"))

	if err := app.startWorkspaceRouteTurn(context.Background(), workspaceSessionID, "host-1", "继续处理后续任务"); err != nil {
		t.Fatalf("start route turn after readonly: %v", err)
	}

	session := app.store.Session(workspaceSessionID)
	if session == nil {
		t.Fatalf("expected workspace session")
	}
	if session.ThreadID != "thread-workspace-route-fresh" {
		t.Fatalf("expected readonly thread binding to be replaced, got %q", session.ThreadID)
	}
	if got := strings.TrimSpace(session.ThreadConfigHash); got != app.workspaceRouteThreadConfigHash("host-1") {
		t.Fatalf("expected route thread config hash after reset, got %q", got)
	}
}

func TestWorkspaceSimpleConversationRepliesDirectlyWithoutMission(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.codexRespondFunc = func(_ context.Context, _ string, _ any) error { return nil }
	(&runtimeStartStub{
		startThread: func(_ context.Context, _ string, _ threadStartSpec) (string, error) {
			return "thread-workspace-direct-1", nil
		},
		startTurn: func(_ context.Context, _ string, _ string, _ turnStartSpec) (string, error) {
			return "turn-workspace-direct-1", nil
		},
	}).install(app)

	workspaceSessionID := "workspace-direct"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/message", nil)
	rec := httptest.NewRecorder()
	app.handleWorkspaceChatMessage(rec, req, workspaceSessionID, chatRequest{Message: "你好"}, time.Now())
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected accepted direct reply request, got %d body=%s", rec.Code, rec.Body.String())
	}

	if mission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID); ok && mission != nil {
		t.Fatalf("expected simple conversation without mission, got %#v", mission)
	}
	session := app.store.Session(workspaceSessionID)
	if session == nil {
		t.Fatalf("expected workspace session")
	}
	if got := strings.TrimSpace(session.ThreadConfigHash); !strings.HasSuffix(got, ":workspace-"+reActLoopVersion) {
		t.Fatalf("expected workspace ReAct thread config hash, got %q", got)
	}
	for _, card := range session.Cards {
		if card.Type == "NoticeCard" && strings.Contains(card.Title, "主 Agent 正在思考") {
			t.Fatalf("did not expect route notice card, got %#v", card)
		}
	}
}

func TestWorkspaceRouteConversationIgnoresPlanUpdatedEvents(t *testing.T) {
	app := newOrchestratorTestApp(t)
	workspaceSessionID := "workspace-route-plan-ignore"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetThread(workspaceSessionID, "thread-workspace-route-ignore")
	app.store.SetThreadConfigHash(workspaceSessionID, app.workspaceRouteThreadConfigHash(model.ServerLocalHostID))

	payload := map[string]any{
		"threadId": "thread-workspace-route-ignore",
		"turnId":   "turn-workspace-route-ignore",
		"plan": []map[string]any{
			{"step": "你好", "status": "completed"},
		},
	}
	app.applyTurnPlanUpdated(payload)

	session := app.store.Session(workspaceSessionID)
	if session == nil {
		t.Fatalf("expected workspace session")
	}
	for _, card := range session.Cards {
		if card.Type == "PlanCard" {
			t.Fatalf("expected route workspace conversation to ignore plan card, got %#v", card)
		}
	}
}

func TestWorkspaceReadonlyConversationIgnoresPlanUpdatedEvents(t *testing.T) {
	app := newOrchestratorTestApp(t)
	workspaceSessionID := "workspace-readonly-plan-ignore"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetThread(workspaceSessionID, "thread-workspace-readonly-ignore")
	app.store.SetThreadConfigHash(workspaceSessionID, app.workspaceReadonlyThreadConfigHash("host-1"))

	payload := map[string]any{
		"threadId": "thread-workspace-readonly-ignore",
		"turnId":   "turn-workspace-readonly-ignore",
		"plan": []map[string]any{
			{"step": "检查 host-1", "status": "completed"},
		},
	}
	app.applyTurnPlanUpdated(payload)

	session := app.store.Session(workspaceSessionID)
	if session == nil {
		t.Fatalf("expected workspace session")
	}
	for _, card := range session.Cards {
		if card.Type == "PlanCard" {
			t.Fatalf("expected readonly workspace conversation to ignore plan card, got %#v", card)
		}
	}
}

func TestWorkspaceRouteCompletionSanitizesDirectReply(t *testing.T) {
	app := newOrchestratorTestApp(t)
	workspaceSessionID := "workspace-route-direct-complete"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetThreadConfigHash(workspaceSessionID, app.workspaceRouteThreadConfigHash(model.ServerLocalHostID))
	now := model.NowString()
	app.store.UpsertCard(workspaceSessionID, model.Card{
		ID:        "msg-user-1",
		Type:      "UserMessageCard",
		Role:      "user",
		Text:      "你好",
		Status:    "completed",
		CreatedAt: now,
		UpdatedAt: now,
	})
	app.store.UpsertCard(workspaceSessionID, model.Card{
		ID:        "msg-assistant-1",
		Type:      "AssistantMessageCard",
		Role:      "assistant",
		Text:      "```json\n{\"route\":\"direct_answer\",\"reason\":\"greeting\",\"targetHostId\":\"\",\"needsPlan\":false,\"needsWorker\":false}\n```\n你好，有什么需要我处理的？",
		Status:    "completed",
		CreatedAt: now,
		UpdatedAt: now,
	})

	app.handleMissionTurnCompleted(workspaceSessionID, "completed")

	if mission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID); ok && mission != nil {
		t.Fatalf("expected no mission for direct answer, got %#v", mission)
	}
	reply := app.latestCompletedAssistantText(workspaceSessionID)
	if strings.Contains(reply, "\"route\"") || strings.Contains(reply, "```json") {
		t.Fatalf("expected sanitized assistant text, got %q", reply)
	}
	if !strings.Contains(reply, "你好，有什么需要我处理的") {
		t.Fatalf("expected visible direct answer, got %q", reply)
	}
}

func TestWorkspaceRouteCompletionStartsPlanningForComplexTask(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.codexRespondFunc = func(_ context.Context, _ string, _ any) error { return nil }
	(&runtimeStartStub{
		startThread: func(_ context.Context, _ string, _ threadStartSpec) (string, error) {
			return "thread-workspace-route-plan-1", nil
		},
		startTurn: func(_ context.Context, _ string, _ string, _ turnStartSpec) (string, error) {
			return "turn-workspace-route-plan-1", nil
		},
	}).install(app)

	workspaceSessionID := "workspace-route-complex-complete"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetThreadConfigHash(workspaceSessionID, app.workspaceRouteThreadConfigHash(model.ServerLocalHostID))
	now := model.NowString()
	app.store.UpsertCard(workspaceSessionID, model.Card{
		ID:        "msg-user-1",
		Type:      "UserMessageCard",
		Role:      "user",
		Text:      "帮我执行一轮全网 nginx 巡检",
		Status:    "completed",
		CreatedAt: now,
		UpdatedAt: now,
	})
	app.store.UpsertCard(workspaceSessionID, model.Card{
		ID:        "msg-assistant-1",
		Type:      "AssistantMessageCard",
		Role:      "assistant",
		Text:      "```json\n{\"route\":\"complex_task\",\"reason\":\"requires multi-step execution\",\"targetHostId\":\"\",\"needsPlan\":true,\"needsWorker\":true}\n```\n我先整理计划，准备在需要时协调 worker。",
		Status:    "completed",
		CreatedAt: now,
		UpdatedAt: now,
	})

	app.handleMissionTurnCompleted(workspaceSessionID, "completed")

	mission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok || mission == nil {
		t.Fatalf("expected mission to start for complex task")
	}
	if got := strings.TrimSpace(app.store.Session(workspaceSessionID).ThreadConfigHash); !strings.HasSuffix(got, ":workspace-orchestration") {
		t.Fatalf("expected orchestration thread config hash, got %q", got)
	}
	foundPlanningNotice := false
	for _, card := range app.store.Session(workspaceSessionID).Cards {
		if card.Type == "NoticeCard" && strings.Contains(card.Title, "plan 正在运行中") {
			foundPlanningNotice = true
			break
		}
	}
	if !foundPlanningNotice {
		t.Fatalf("expected planning notice after complex route")
	}
}

func TestWorkspacePlanReplyDispatchesTasks(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.codexRespondFunc = func(_ context.Context, _ string, _ any) error { return nil }
	(&runtimeStartStub{
		startThread: func(_ context.Context, _ string, _ threadStartSpec) (string, error) {
			return "thread-workspace-plan-1", nil
		},
		startTurn: func(_ context.Context, _ string, _ string, _ turnStartSpec) (string, error) {
			return "turn-workspace-plan-1", nil
		},
	}).install(app)

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

	workspaceSessionID := "workspace-plan-dispatch"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	mission, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		WorkspaceSessionID: workspaceSessionID,
		PlannerSessionID:   "planner-plan-dispatch",
		Title:              "plan dispatch demo",
		Summary:            "plan dispatch demo",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}

	reply := "先完成 host-1 的只读检查，再继续后续操作。\n```json\n" +
		"{\"missionTitle\":\"plan dispatch demo\",\"summary\":\"先完成只读检查\",\"tasks\":[{\"taskId\":\"task-1\",\"hostId\":\"host-1\",\"title\":\"inspect host\",\"instruction\":\"inspect nginx status\"}]}\n" +
		"```"
	app.store.UpsertCard(workspaceSessionID, model.Card{
		ID:        "workspace-plan-reply-1",
		Type:      "AssistantMessageCard",
		Role:      "assistant",
		Text:      reply,
		Status:    "completed",
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	})

	app.handleMissionTurnCompleted(workspaceSessionID, "completed")

	updatedMission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok {
		t.Fatalf("expected mission after workspace plan dispatch")
	}
	if updatedMission.ID != mission.ID {
		t.Fatalf("expected same mission id, got %s vs %s", updatedMission.ID, mission.ID)
	}
	worker := updatedMission.Workers["host-1"]
	if worker == nil {
		t.Fatalf("expected worker for host-1")
	}
	if task := updatedMission.Tasks["task-1"]; task == nil {
		t.Fatalf("expected task-1 after workspace dispatch")
	}
	planCard := app.cardByID(workspaceSessionID, "workspace-plan-"+mission.ID)
	if planCard == nil {
		t.Fatalf("expected workspace plan card after dispatch")
	}
	if _, ok := planCard.Detail["planner_conversation"]; ok {
		t.Fatalf("expected planner conversation to be omitted from workspace plan detail, got %#v", planCard.Detail["planner_conversation"])
	}
}

func TestPlannerDispatchTasksStartsWorkerSession(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.codexRespondFunc = func(_ context.Context, _ string, _ any) error { return nil }
	(&runtimeStartStub{
		startThread: func(_ context.Context, _ string, _ threadStartSpec) (string, error) {
			return "thread-worker-1", nil
		},
		startTurn: func(_ context.Context, _ string, _ string, _ turnStartSpec) (string, error) {
			return "turn-worker-1", nil
		},
	}).install(app)

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
			if want := "mkdir -p /tmp/.aiops_codex/missions/mission-1/host-1"; msg.ExecStart.Command != want {
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
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-1",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})

	_, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:          "mission-1",
		WorkspaceSessionID: workspaceSessionID,
		Title:              "demo",
		Summary:            "demo summary",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	app.handleWorkspaceDispatchTasks("raw-dispatch", workspaceSessionID, map[string]any{
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

func TestWorkspaceTurnKeepsReplyAfterDispatch(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.codexRespondFunc = func(_ context.Context, _ string, _ any) error { return nil }

	app.store.UpsertHost(model.Host{
		ID:         "host-1",
		Name:       "host-1",
		Kind:       "remote",
		Status:     "online",
		Executable: true,
	})

	workspaceSessionID := "workspace-planner-reply"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})

	_, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:          "mission-planner-reply",
		WorkspaceSessionID: workspaceSessionID,
		Title:              "检查主机状态",
		Summary:            "检查主机状态",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	app.handleWorkspaceDispatchTasks("raw-dispatch", workspaceSessionID, map[string]any{
		"summary": "先收集在线主机状态，再决定是否继续执行。",
		"tasks": []map[string]any{
			{
				"taskId":      "task-1",
				"hostId":      "host-1",
				"title":       "检查 host-1",
				"instruction": "检查 nginx 状态",
			},
		},
	})

	app.store.UpsertCard(workspaceSessionID, model.Card{
		ID:        "workspace-assistant-1",
		Type:      "AssistantMessageCard",
		Role:      "assistant",
		Text:      "巡检计划已生成，准备派发到 1 台主机执行。",
		Status:    "completed",
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	})
	app.handleMissionTurnCompleted(workspaceSessionID, "completed")

	session := app.store.Session(workspaceSessionID)
	if session == nil {
		t.Fatalf("expected workspace session")
	}
	foundReply := false
	for _, card := range session.Cards {
		if card.Type == "AssistantMessageCard" && strings.Contains(card.Text, "巡检计划已生成，准备派发到 1 台主机执行") {
			foundReply = true
			break
		}
	}
	if !foundReply {
		t.Fatalf("expected workspace reply to stay visible after dispatch, got %#v", session.Cards)
	}
}

func TestWorkspaceProjectionIncludesRichPlanAndWorkerReadModels(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.codexRespondFunc = func(_ context.Context, _ string, _ any) error { return nil }
	(&runtimeStartStub{
		startThread: func(_ context.Context, _ string, _ threadStartSpec) (string, error) {
			return "thread-worker-rich", nil
		},
		startTurn: func(_ context.Context, _ string, _ string, _ turnStartSpec) (string, error) {
			return "turn-worker-rich", nil
		},
	}).install(app)

	app.store.UpsertHost(model.Host{
		ID:              "host-1",
		Name:            "host-1",
		Kind:            "linux",
		Status:          "online",
		Executable:      true,
		TerminalCapable: true,
	})

	workspaceSessionID := "workspace-rich"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-rich",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})

	_, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:          "mission-rich",
		WorkspaceSessionID: workspaceSessionID,
		Title:              "rich projection",
		Summary:            "collect richer read models",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	app.handleWorkspaceDispatchTasks("raw-dispatch-rich", workspaceSessionID, map[string]any{
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
	if _, ok := planCard.Detail["planner_conversation"]; ok {
		t.Fatalf("expected planner conversation to be omitted, got %#v", planCard.Detail["planner_conversation"])
	}
	dispatchEvents, ok := planCard.Detail["dispatch_events"].([]orchestrator.DispatchEventView)
	if !ok || len(dispatchEvents) == 0 {
		t.Fatalf("expected dispatch events, got %#v", planCard.Detail["dispatch_events"])
	}
	taskBindings, ok := planCard.Detail["task_host_bindings"].([]orchestrator.TaskHostBindingView)
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
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-offline",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})

	_, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:          "mission-offline",
		WorkspaceSessionID: workspaceSessionID,
		Title:              "offline host demo",
		Summary:            "offline host demo",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	app.handleWorkspaceDispatchTasks("raw-dispatch-offline", workspaceSessionID, map[string]any{
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
		t.Fatalf("expected workspace dispatch response")
	}
	if success, _ := resp["success"].(bool); success {
		t.Fatalf("expected failed workspace dispatch response, got %#v", resp)
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
	(&runtimeStartStub{
		startThread: func(_ context.Context, _ string, _ threadStartSpec) (string, error) {
			return "thread-worker-start-fail", nil
		},
		startTurn: func(_ context.Context, _ string, _ string, _ turnStartSpec) (string, error) {
			return "", errors.New("turn/start failed")
		},
	}).install(app)

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
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-start-fail",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})

	_, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:          "mission-start-fail",
		WorkspaceSessionID: workspaceSessionID,
		Title:              "worker start fail demo",
		Summary:            "worker start fail demo",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	app.handleWorkspaceDispatchTasks("raw-dispatch-start-fail", workspaceSessionID, map[string]any{
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
	(&runtimeStartStub{
		startThread: func(_ context.Context, _ string, _ threadStartSpec) (string, error) {
			return "thread-worker-1", nil
		},
		startTurn: func(_ context.Context, _ string, _ string, _ turnStartSpec) (string, error) {
			return "turn-worker-1", nil
		},
	}).install(app)

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
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-1",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})

	_, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:          "mission-1",
		WorkspaceSessionID: workspaceSessionID,
		Title:              "demo",
		Summary:            "demo summary",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	app.handleWorkspaceDispatchTasks("raw-dispatch", workspaceSessionID, map[string]any{
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
	(&runtimeStartStub{
		startThread: func(_ context.Context, _ string, _ threadStartSpec) (string, error) {
			threadSeq++
			return fmt.Sprintf("thread-recycle-%02d", threadSeq), nil
		},
		startTurn: func(_ context.Context, _ string, _ string, _ turnStartSpec) (string, error) {
			turnSeq++
			return fmt.Sprintf("turn-recycle-%02d", turnSeq), nil
		},
	}).install(app)

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
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-recycle",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})

	_, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:          "mission-recycle",
		WorkspaceSessionID: workspaceSessionID,
		Title:              "thread recycle demo",
		Summary:            "thread recycle demo",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	app.handleWorkspaceDispatchTasks("raw-dispatch-initial", workspaceSessionID, map[string]any{
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

	app.handleWorkspaceDispatchTasks("raw-dispatch-follow-up", workspaceSessionID, map[string]any{
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

	workspaceSessionID := "workspace-stop"
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
		Title:              "stop demo",
		Summary:            "stop demo",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	app.store.SetThread(workspaceSessionID, "thread-workspace-stop")
	app.store.SetTurn(workspaceSessionID, "turn-workspace-stop")
	app.startRuntimeTurn(workspaceSessionID, model.ServerLocalHostID)

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

func TestEnsureMissionForWorkspaceSessionStartsFreshMissionWhenRunningMissionIsStale(t *testing.T) {
	app := newOrchestratorTestApp(t)

	workspaceSessionID := "workspace-stale"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          "mission-stale",
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	staleMission, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:          "mission-stale",
		WorkspaceSessionID: workspaceSessionID,
		PlannerSessionID:   "planner-stale",
		Title:              "stale mission",
		Summary:            "stale mission",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}

	mission, err := app.ensureMissionForWorkspaceSession(context.Background(), workspaceSessionID, "看下CPU")
	if err != nil {
		t.Fatalf("ensure mission: %v", err)
	}
	if mission.ID == staleMission.ID {
		t.Fatalf("expected a fresh mission instead of reusing stale mission %s", staleMission.ID)
	}

	updatedStaleMission, ok := app.orchestrator.Mission(staleMission.ID)
	if !ok {
		t.Fatalf("expected stale mission to remain queryable")
	}
	if updatedStaleMission.Status != orchestrator.MissionStatusCancelled {
		t.Fatalf("expected stale mission cancelled, got %s", updatedStaleMission.Status)
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

func TestReconcileOrchestratorAfterLoadFailsLegacyPlannerWithoutThread(t *testing.T) {
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
		t.Fatalf("expected failed mission after legacy planner reconcile, got %s", reconciledMission.Status)
	}
	card := app.cardByID(workspaceSessionID, "workspace-reconcile-"+plannerSessionID)
	if card == nil || card.Status != "failed" {
		t.Fatalf("expected failed planner reconcile card after planner removal, got %#v", card)
	}
}
