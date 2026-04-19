package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/orchestrator"
)

func TestCreateChoiceRequestProjectsWorkerMirrorViaLifecycleEvent(t *testing.T) {
	app, workspaceSessionID, workerSessionID := setupWorkerMissionForToolProjection(t)
	app.startRuntimeTurn(workerSessionID, "host-1")

	questions := []model.ChoiceQuestion{{
		Header:   "执行策略",
		Question: "选择执行策略",
		Options: []model.ChoiceOption{
			{Label: "保守模式", Value: "safe"},
			{Label: "激进模式", Value: "fast"},
		},
	}}

	app.createChoiceRequest("raw-choice-event", workerSessionID, map[string]any{
		"threadId": "thread-choice-event",
		"turnId":   "turn-choice-event",
	}, questions)

	choiceID, _, ok := app.latestPendingChoiceForSession(workerSessionID)
	if !ok || strings.TrimSpace(choiceID) == "" {
		t.Fatalf("expected worker pending choice after request, got %q ok=%v", choiceID, ok)
	}

	targetSessionID, _, ok := app.resolveChoiceTargetSession(workspaceSessionID, choiceID)
	if !ok || targetSessionID != workerSessionID {
		t.Fatalf("expected workspace choice route to resolve to worker session, got target=%q ok=%v", targetSessionID, ok)
	}

	mission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok || mission == nil {
		t.Fatalf("expected mission for workspace %s", workspaceSessionID)
	}
	worker := mission.Workers["host-1"]
	if worker == nil || worker.Status != orchestrator.WorkerStatusWaiting {
		t.Fatalf("expected worker waiting after choice request, got %#v", worker)
	}
	task := mission.Tasks["task-1"]
	if task == nil || task.Status != orchestrator.TaskStatusWaitingInput {
		t.Fatalf("expected task waiting_input after choice request, got %#v", task)
	}

	workspace := app.store.Session(workspaceSessionID)
	if workspace == nil || workspace.Runtime.Turn.Phase != "waiting_input" {
		t.Fatalf("expected workspace runtime waiting_input, got %#v", workspace)
	}
	if mirrored := app.cardByID(workspaceSessionID, choiceID); mirrored == nil || mirrored.Status != "pending" {
		t.Fatalf("expected mirrored workspace choice card, got %#v", mirrored)
	}
}

func TestHandleChoiceAnswerProjectsWorkerMirrorViaLifecycleEvent(t *testing.T) {
	app, workspaceSessionID, workerSessionID := setupWorkerMissionForToolProjection(t)
	app.startRuntimeTurn(workerSessionID, "host-1")

	var respondedRawID string
	var respondedPayload any
	app.codexRespondFunc = func(_ context.Context, rawID string, payload any) error {
		respondedRawID = rawID
		respondedPayload = payload
		return nil
	}

	questions := []model.ChoiceQuestion{{
		Header:   "执行策略",
		Question: "选择执行策略",
		Options: []model.ChoiceOption{
			{Label: "保守模式", Value: "safe"},
			{Label: "激进模式", Value: "fast"},
		},
	}}
	app.createChoiceRequest("raw-choice-answer-event", workerSessionID, map[string]any{
		"threadId": "thread-choice-answer-event",
		"turnId":   "turn-choice-answer-event",
	}, questions)

	choiceID, choice, ok := app.latestPendingChoiceForSession(workerSessionID)
	if !ok {
		t.Fatal("expected pending worker choice")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/choices/"+choiceID+"/answer", strings.NewReader(`{"answers":[{"value":"safe","label":"保守模式"}]}`))
	rec := httptest.NewRecorder()
	app.handleChoiceAnswer(rec, req, workspaceSessionID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 answering worker choice from workspace, got %d body=%s", rec.Code, rec.Body.String())
	}

	targetChoice, ok := app.store.Choice(workerSessionID, choiceID)
	if !ok || targetChoice.Status != "completed" {
		t.Fatalf("expected worker choice completed, got %#v ok=%v", targetChoice, ok)
	}
	if len(targetChoice.Answers) != 1 || targetChoice.Answers[0].Value != "safe" {
		t.Fatalf("expected worker choice answer saved, got %#v", targetChoice.Answers)
	}

	workspaceChoice, ok := app.store.Choice(workspaceSessionID, choiceID)
	if !ok || workspaceChoice.Status != "completed" {
		t.Fatalf("expected mirrored workspace choice completed, got %#v ok=%v", workspaceChoice, ok)
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
	mission, ok := app.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
	if !ok || mission == nil {
		t.Fatalf("expected mission for workspace %s", workspaceSessionID)
	}
	worker := mission.Workers["host-1"]
	if worker == nil || worker.Status != orchestrator.WorkerStatusRunning {
		t.Fatalf("expected worker running after answer, got %#v", worker)
	}
	task := mission.Tasks["task-1"]
	if task == nil || task.Status != orchestrator.TaskStatusRunning {
		t.Fatalf("expected task running after answer, got %#v", task)
	}

	if respondedRawID != choice.RequestIDRaw {
		t.Fatalf("expected codex response to original raw choice id, got %q", respondedRawID)
	}
	responseMap, ok := respondedPayload.(map[string]any)
	if !ok {
		t.Fatalf("expected structured response payload map, got %#v", respondedPayload)
	}
	decodedPayload := decodeStructuredToolResponsePayload(t, responseMap)
	if answers, ok := decodedPayload["answers"].([]any); !ok || len(answers) != 1 {
		t.Fatalf("expected structured choice answers in codex response, got %#v", decodedPayload["answers"])
	}
}
