package agentloop

import (
	"context"
	"errors"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/orchestrator"
)

type fakeWorkspaceCompleter struct {
	calls []struct {
		sessionID string
		phase     string
		reply     string
	}
	outcome *orchestrator.WorkerTurnOutcome
	err     error
}

func (f *fakeWorkspaceCompleter) CompleteWorkerTurn(sessionID string, phase string, reply string) (*orchestrator.WorkerTurnOutcome, error) {
	f.calls = append(f.calls, struct {
		sessionID string
		phase     string
		reply     string
	}{
		sessionID: sessionID,
		phase:     phase,
		reply:     reply,
	})
	if f.err != nil {
		return nil, f.err
	}
	if f.outcome != nil {
		return f.outcome, nil
	}
	return &orchestrator.WorkerTurnOutcome{WorkerSessionID: sessionID}, nil
}

func TestWorkspaceRuntimeStartPlannerTurnCreatesAndReusesSession(t *testing.T) {
	rt := NewWorkspaceRuntime(nil)
	spec := SessionSpec{Model: "gpt-4o-mini", DynamicTools: []string{"tool-a"}}

	session, err := rt.StartPlannerTurn(context.Background(), "planner-1", spec, "plan this")
	if err != nil {
		t.Fatalf("StartPlannerTurn returned error: %v", err)
	}
	if session == nil {
		t.Fatal("expected non-nil session")
	}
	if session.ID != "planner-1" {
		t.Fatalf("expected session id planner-1, got %q", session.ID)
	}

	msgs := session.ContextManager().Messages()
	if len(msgs) != 1 || msgs[0].Role != "user" || msgs[0].Content != "plan this" {
		t.Fatalf("unexpected planner messages: %#v", msgs)
	}

	again, err := rt.StartPlannerTurn(context.Background(), "planner-1", SessionSpec{Model: "other"}, "ignored")
	if err != nil {
		t.Fatalf("second StartPlannerTurn returned error: %v", err)
	}
	if again != session {
		t.Fatal("expected planner session to be reused")
	}
	if got := len(again.ContextManager().Messages()); got != 2 {
		t.Fatalf("expected second planner turn to append user message, got %d messages", got)
	}
}

func TestWorkspaceRuntimeStartWorkerTurnAndCompleteWorkerTurn(t *testing.T) {
	completer := &fakeWorkspaceCompleter{
		outcome: &orchestrator.WorkerTurnOutcome{
			WorkerSessionID:    "worker-1",
			WorkspaceSessionID: "workspace-1",
			MissionID:          "mission-1",
			CompletedTaskID:    "task-1",
		},
	}
	rt := NewWorkspaceRuntime(completer)

	session, err := rt.StartWorkerTurn(context.Background(), "worker-1", SessionSpec{Model: "gpt-4o-mini"}, "do work")
	if err != nil {
		t.Fatalf("StartWorkerTurn returned error: %v", err)
	}
	if session == nil {
		t.Fatal("expected non-nil worker session")
	}
	msgs := session.ContextManager().Messages()
	if len(msgs) != 1 || msgs[0].Role != "user" || msgs[0].Content != "do work" {
		t.Fatalf("unexpected worker messages: %#v", msgs)
	}

	outcome, err := rt.CompleteWorkerTurn("worker-1", "completed", "task-1 done")
	if err != nil {
		t.Fatalf("CompleteWorkerTurn returned error: %v", err)
	}
	if outcome == nil || outcome.WorkerSessionID != "worker-1" {
		t.Fatalf("unexpected outcome: %#v", outcome)
	}
	if len(completer.calls) != 1 {
		t.Fatalf("expected one completer call, got %d", len(completer.calls))
	}
	call := completer.calls[0]
	if call.sessionID != "worker-1" || call.phase != "completed" || call.reply != "task-1 done" {
		t.Fatalf("unexpected completer call: %#v", call)
	}
}

func TestWorkspaceRuntimeRejectsCrossRoleSessionReuse(t *testing.T) {
	rt := NewWorkspaceRuntime(nil)
	if _, err := rt.StartPlannerTurn(context.Background(), "shared-id", SessionSpec{Model: "planner"}, "plan"); err != nil {
		t.Fatalf("planner start failed: %v", err)
	}
	if _, err := rt.StartWorkerTurn(context.Background(), "shared-id", SessionSpec{Model: "worker"}, "work"); err == nil {
		t.Fatal("expected worker start to reject planner-owned session id")
	}
}

func TestWorkspaceRuntimeContextCancellation(t *testing.T) {
	rt := NewWorkspaceRuntime(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := rt.StartPlannerTurn(ctx, "planner-cancel", SessionSpec{Model: "gpt-4o-mini"}, "noop"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestWorkspaceRuntimeCompleteWorkerTurnWithoutCompleter(t *testing.T) {
	rt := NewWorkspaceRuntime(nil)
	if _, err := rt.StartWorkerTurn(context.Background(), "worker-nil", SessionSpec{Model: "gpt-4o-mini"}, "work"); err != nil {
		t.Fatalf("StartWorkerTurn returned error: %v", err)
	}
	outcome, err := rt.CompleteWorkerTurn("worker-nil", "completed", "done")
	if err != nil {
		t.Fatalf("CompleteWorkerTurn returned error: %v", err)
	}
	if outcome != nil {
		t.Fatalf("expected nil outcome without completer, got %#v", outcome)
	}
}

func TestWorkspaceRuntimeResetSessionClearsCachedSessions(t *testing.T) {
	rt := NewWorkspaceRuntime(nil)

	planner, err := rt.StartPlannerTurn(context.Background(), "shared", SessionSpec{Model: "planner"}, "plan")
	if err != nil {
		t.Fatalf("StartPlannerTurn returned error: %v", err)
	}
	worker, err := rt.StartWorkerTurn(context.Background(), "worker-shared", SessionSpec{Model: "worker"}, "work")
	if err != nil {
		t.Fatalf("StartWorkerTurn returned error: %v", err)
	}

	rt.ResetPlannerTurn("shared")
	rt.ResetWorkerTurn("worker-shared")

	if got, ok := rt.PlannerSession("shared"); ok || got != nil {
		t.Fatalf("expected planner session to be cleared, got %#v ok=%v", got, ok)
	}
	if got, ok := rt.WorkerSession("worker-shared"); ok || got != nil {
		t.Fatalf("expected worker session to be cleared, got %#v ok=%v", got, ok)
	}

	freshPlanner, err := rt.StartPlannerTurn(context.Background(), "shared", SessionSpec{Model: "planner-v2"}, "plan again")
	if err != nil {
		t.Fatalf("fresh StartPlannerTurn returned error: %v", err)
	}
	freshWorker, err := rt.StartWorkerTurn(context.Background(), "worker-shared", SessionSpec{Model: "worker-v2"}, "work again")
	if err != nil {
		t.Fatalf("fresh StartWorkerTurn returned error: %v", err)
	}

	if freshPlanner == planner {
		t.Fatal("expected planner session to be recreated after reset")
	}
	if freshWorker == worker {
		t.Fatal("expected worker session to be recreated after reset")
	}

	if msgs := freshPlanner.ContextManager().Messages(); len(msgs) != 1 || msgs[0].Content != "plan again" {
		t.Fatalf("unexpected fresh planner messages: %#v", msgs)
	}
	if msgs := freshWorker.ContextManager().Messages(); len(msgs) != 1 || msgs[0].Content != "work again" {
		t.Fatalf("unexpected fresh worker messages: %#v", msgs)
	}
}

func TestWorkspaceRuntimeResetSessionRejectsCompletionOnClearedWorker(t *testing.T) {
	rt := NewWorkspaceRuntime(nil)
	if _, err := rt.StartWorkerTurn(context.Background(), "worker-reset", SessionSpec{Model: "gpt-4o-mini"}, "work"); err != nil {
		t.Fatalf("StartWorkerTurn returned error: %v", err)
	}

	rt.ResetSession("worker-reset")

	if _, err := rt.CompleteWorkerTurn("worker-reset", "completed", "done"); err == nil {
		t.Fatal("expected completion on cleared worker session to fail")
	}
}
