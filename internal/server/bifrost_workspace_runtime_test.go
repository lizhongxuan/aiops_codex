package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/agentloop"
	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/orchestrator"
)

type workspaceFakeBifrostProvider struct {
	onRequest func(req bifrost.ChatRequest)
	streamFn  func(ctx context.Context, req bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error)
}

func (p *workspaceFakeBifrostProvider) Name() string { return "openai" }

func (p *workspaceFakeBifrostProvider) ChatCompletion(context.Context, bifrost.ChatRequest) (*bifrost.ChatResponse, error) {
	return nil, nil
}

func (p *workspaceFakeBifrostProvider) StreamChatCompletion(ctx context.Context, req bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
	if p.onRequest != nil {
		p.onRequest(req)
	}
	return p.streamFn(ctx, req)
}

func (p *workspaceFakeBifrostProvider) SupportsToolCalling() bool { return true }
func (p *workspaceFakeBifrostProvider) Capabilities() bifrost.ProviderCapabilities {
	return bifrost.ProviderCapabilities{ToolCallingFormat: "openai_function"}
}

func newBifrostWorkspaceTestApp(t *testing.T) *App {
	t.Helper()
	app := newOrchestratorTestApp(t)
	app.cfg.UseBifrost = true
	app.cfg.LLMProvider = "openai"
	app.cfg.LLMModel = "test-model"
	app.cfg.LLMAPIKey = "test-key"
	if err := app.initBifrostRuntime(); err != nil {
		t.Fatalf("init bifrost runtime: %v", err)
	}
	return app
}

func workspaceBifrostToolNames(req bifrost.ChatRequest) []string {
	names := make([]string, 0, len(req.Tools))
	for _, tool := range req.Tools {
		names = append(names, tool.Function.Name)
	}
	return names
}

func makeWorkspaceBifrostStream(events []bifrost.StreamEvent) <-chan bifrost.StreamEvent {
	ch := make(chan bifrost.StreamEvent, len(events))
	for _, event := range events {
		ch <- event
	}
	close(ch)
	return ch
}

func TestWorkspaceBifrostMainSessionProducesAssistantContent(t *testing.T) {
	app := newBifrostWorkspaceTestApp(t)
	app.bifrostGateway.RegisterProvider("openai", &workspaceFakeBifrostProvider{
		onRequest: func(req bifrost.ChatRequest) {
			if got := strings.Join(workspaceBifrostToolNames(req), ","); !strings.Contains(got, "ask_user_question") {
				t.Fatalf("expected workspace Bifrost tools to include ask_user_question, got %q", got)
			}
		},
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			return makeWorkspaceBifrostStream([]bifrost.StreamEvent{
				{Type: "content_delta", Delta: "workspace "},
				{Type: "content_delta", Delta: "assistant"},
				{Type: "done"},
			}), nil
		},
	})

	sessionID := "workspace-bifrost-main"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.startRuntimeTurn(sessionID, model.ServerLocalHostID)

	if err := app.runBifrostTurn(context.Background(), sessionID, chatRequest{Message: "summarize the workspace state"}); err != nil {
		t.Fatalf("run bifrost turn: %v", err)
	}

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatal("expected workspace session to exist")
	}
	if session.Meta.Kind != model.SessionKindWorkspace {
		t.Fatalf("expected workspace kind, got %q", session.Meta.Kind)
	}
	if session.Runtime.Turn.Active {
		t.Fatalf("expected workspace bifrost turn to finish, runtime=%+v", session.Runtime.Turn)
	}
	if session.Runtime.Turn.Phase != "completed" {
		t.Fatalf("expected completed runtime phase, got %q", session.Runtime.Turn.Phase)
	}

	assistantCardFound := false
	for _, card := range session.Cards {
		if card.Type == "AssistantMessageCard" && strings.TrimSpace(card.Text) == "workspace assistant" {
			assistantCardFound = true
			break
		}
	}
	if !assistantCardFound {
		t.Fatalf("expected workspace assistant card, cards=%#v", session.Cards)
	}
	if _, ok := app.bifrostSession(sessionID); !ok {
		t.Fatal("expected workspace bifrost session cache to be populated")
	}
}

func TestWorkspaceWorkerBifrostCompletionSyncsProjection(t *testing.T) {
	app := newBifrostWorkspaceTestApp(t)
	app.bifrostGateway.RegisterProvider("openai", &workspaceFakeBifrostProvider{
		onRequest: func(req bifrost.ChatRequest) {
			if got := strings.Join(workspaceBifrostToolNames(req), ","); !strings.Contains(got, "execute_readonly_query") {
				t.Fatalf("expected worker Bifrost tools to include execute_readonly_query, got %q", got)
			}
		},
		streamFn: func(_ context.Context, _ bifrost.ChatRequest) (<-chan bifrost.StreamEvent, error) {
			return makeWorkspaceBifrostStream([]bifrost.StreamEvent{
				{Type: "content_delta", Delta: "worker "},
				{Type: "content_delta", Delta: "done"},
				{Type: "done"},
			}), nil
		},
	})

	app.store.UpsertHost(model.Host{
		ID:              "host-1",
		Name:            "host-1",
		Kind:            "linux",
		Status:          "online",
		Executable:      true,
		TerminalCapable: true,
	})

	workspaceSessionID := "workspace-bifrost-worker"
	mission, err := app.orchestrator.StartMission(context.Background(), orchestrator.StartMissionRequest{
		MissionID:           "mission-bifrost-worker",
		WorkspaceSessionID:  workspaceSessionID,
		Title:               "worker sync",
		Summary:             "verify worker completion sync",
		GlobalActiveBudget:  1,
		MissionActiveBudget: 1,
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	if _, err := app.orchestrator.Dispatch(context.Background(), orchestrator.DispatchRequest{
		MissionID: mission.ID,
		Tasks: []orchestrator.DispatchTaskRequest{{
			TaskID:      "task-1",
			HostID:      "host-1",
			Title:       "collect status",
			Instruction: "collect worker status on host-1",
		}},
	}); err != nil {
		t.Fatalf("dispatch mission: %v", err)
	}

	updatedMission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok || updatedMission == nil {
		t.Fatal("expected mission linked to workspace session")
	}
	worker := updatedMission.Workers["host-1"]
	if worker == nil {
		t.Fatalf("expected worker for host-1, mission=%#v", updatedMission.Workers)
	}
	workerSessionID := worker.SessionID
	if workerSessionID == "" {
		t.Fatal("expected worker session id to be assigned")
	}
	app.store.EnsureSessionWithMeta(workerSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorker,
		Visible:            false,
		MissionID:          mission.ID,
		WorkspaceSessionID: workspaceSessionID,
		WorkerHostID:       "host-1",
		RuntimePreset:      model.SessionRuntimePresetWorker,
	})
	app.store.SetSelectedHost(workerSessionID, "host-1")
	app.startRuntimeTurn(workerSessionID, "host-1")

	if err := app.runBifrostTurn(context.Background(), workerSessionID, chatRequest{Message: "worker finished"}); err != nil {
		t.Fatalf("run worker bifrost turn: %v", err)
	}
	app.handleMissionTurnCompleted(workerSessionID, "completed")

	waitFor(t, 5*time.Second, "workspace worker projection sync", func() bool {
		mission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
		return ok && mission != nil && mission.Status == orchestrator.MissionStatusCompleted
	})

	finalMission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok || finalMission == nil {
		t.Fatal("expected final mission projection")
	}
	if finalMission.Status != orchestrator.MissionStatusCompleted {
		t.Fatalf("expected completed mission, got %s", finalMission.Status)
	}
	if task := finalMission.Tasks["task-1"]; task == nil || task.Status != orchestrator.TaskStatusCompleted {
		t.Fatalf("expected task-1 completed, got %#v", task)
	}

	workerCard := app.cardByID(workspaceSessionID, "worker-result-task-1")
	if workerCard == nil || workerCard.Status != "completed" {
		t.Fatalf("expected worker result card completed, got %#v", workerCard)
	}
	resultCard := app.cardByID(workspaceSessionID, "workspace-result-"+finalMission.ID)
	if resultCard == nil || resultCard.Status != "completed" {
		t.Fatalf("expected workspace result card completed, got %#v", resultCard)
	}
	if noticeCard := app.cardByID(workspaceSessionID, "mission-complete-"+finalMission.ID); noticeCard == nil {
		t.Fatalf("expected mission complete notice card, cards=%#v", app.store.Session(workspaceSessionID).Cards)
	}

	workerSession := app.store.Session(workerSessionID)
	if workerSession == nil {
		t.Fatal("expected worker session to exist")
	}
	if workerSession.Runtime.Turn.Phase != "completed" {
		t.Fatalf("expected worker bifrost turn to complete, got %q", workerSession.Runtime.Turn.Phase)
	}
	if len(workerSession.Cards) == 0 {
		t.Fatal("expected worker session to contain assistant cards")
	}
	if got := strings.TrimSpace(workerSession.Cards[len(workerSession.Cards)-1].Text); got != "worker done" {
		t.Fatalf("expected worker assistant card content, got %q", got)
	}
}

func TestExecuteBifrostUnifiedToolResultUsesUnifiedTool(t *testing.T) {
	app := newBifrostWorkspaceTestApp(t)
	session := &agentloop.Session{ID: "bifrost-unified-tool"}
	tool := scriptedUnifiedTool{
		name: "list_remote_files",
		callFn: func(_ context.Context, req ToolCallRequest) (ToolCallResult, error) {
			if got := getStringAny(req.Input, "path"); got != "/srv/app" {
				t.Fatalf("unexpected input passed to unified tool: %#v", req.Input)
			}
			return ToolCallResult{Output: "listed /srv/app"}, nil
		},
	}

	result, err := app.executeBifrostUnifiedToolResult(context.Background(), session, bifrost.ToolCall{
		ID: "call-bifrost-unified",
		Function: bifrost.FunctionCall{
			Name: "list_files",
		},
	}, "list_files", "list_remote_files", "host-1", map[string]any{
		"host":   "host-1",
		"path":   "/srv/app",
		"reason": "inspect",
	}, tool)
	if err != nil {
		t.Fatalf("execute bifrost unified tool: %v", err)
	}
	if result != "listed /srv/app" {
		t.Fatalf("unexpected bifrost result: %q", result)
	}
	stored, ok := app.consumeBifrostToolResult(session.ID, "list_files")
	if !ok {
		t.Fatal("expected bifrost result to be stored")
	}
	if stored.OutputText != "listed /srv/app" {
		t.Fatalf("expected stored output text, got %#v", stored)
	}
	if got := getStringAny(stored.ProjectionPayload, "toolNameOverride"); got != "list_remote_files" {
		t.Fatalf("expected tool name override to be recorded, got %#v", stored.ProjectionPayload)
	}
}
