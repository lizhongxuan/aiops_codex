package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestStoreRoundTripAndFilters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orchestrator.json")
	store := NewStore(path)

	_, err := store.UpsertMission(&Mission{
		ID:                  "mission-1",
		Title:               "demo",
		Status:              MissionStatusRunning,
		Workers:             make(map[string]*HostWorker),
		Tasks:               make(map[string]*TaskRun),
		Workspaces:          make(map[string]*WorkspaceLease),
		GlobalActiveBudget:  DefaultGlobalActiveBudget,
		MissionActiveBudget: DefaultMissionActiveBudget,
	})
	if err != nil {
		t.Fatalf("upsert mission: %v", err)
	}
	store.UpsertSessionMeta("workspace-1", SessionMeta{Kind: SessionKindWorkspace, Visible: true, MissionID: "mission-1"})
	store.UpsertSessionMeta("worker-1", SessionMeta{Kind: SessionKindWorker, Visible: false, MissionID: "mission-1", WorkerHostID: "host-1"})
	if err := store.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded := NewStore(path)
	if err := loaded.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := loaded.SessionMeta("workspace-1"); !ok {
		t.Fatalf("expected workspace session meta after load")
	}
	if got := loaded.SessionIDsByKind(SessionKindWorker); len(got) != 1 || got[0] != "worker-1" {
		t.Fatalf("unexpected worker session ids: %#v", got)
	}
}

func TestStoreSaveConcurrentUsesUniqueTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orchestrator.json")
	store := NewStore(path)

	if _, err := store.UpsertMission(&Mission{
		ID:                  "mission-concurrent-save",
		Title:               "demo",
		Summary:             "demo",
		Status:              MissionStatusRunning,
		Workers:             make(map[string]*HostWorker),
		Tasks:               make(map[string]*TaskRun),
		Workspaces:          make(map[string]*WorkspaceLease),
		GlobalActiveBudget:  DefaultGlobalActiveBudget,
		MissionActiveBudget: DefaultMissionActiveBudget,
	}); err != nil {
		t.Fatalf("upsert mission: %v", err)
	}

	const saves = 8
	errs := make(chan error, saves)
	var wg sync.WaitGroup
	for i := 0; i < saves; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- store.Save()
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent save failed: %v", err)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected saved store file, got %v", err)
	}
}

func TestStoreLoadRebuildsMissionSessionIndexesFromLegacyState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orchestrator.json")
	content := `{
  "version": 1,
  "missions": {
    "mission-legacy": {
      "id": "mission-legacy",
      "workspaceSessionId": "workspace-legacy",
      "plannerSessionId": "planner-legacy",
      "plannerThreadId": "thread-planner-legacy",
      "title": "legacy",
      "summary": "legacy summary",
      "status": "running",
      "workers": {
        "host-1": {
          "missionId": "mission-legacy",
          "hostId": "host-1",
          "sessionId": "worker-legacy",
          "threadId": "thread-worker-legacy",
          "status": "running"
        }
      },
      "tasks": {
        "task-1": {
          "id": "task-1",
          "missionId": "mission-legacy",
          "hostId": "host-1",
          "workerHostId": "host-1",
          "sessionId": "worker-legacy",
          "threadId": "thread-worker-legacy",
          "instruction": "inspect host",
          "status": "running"
        }
      }
    }
  },
  "sessions": {},
  "seenBySession": {},
  "missionByWorkspace": {},
  "missionByPlanner": {},
  "missionByWorker": {},
  "workerByHost": {},
  "approvalToWorker": {},
  "choiceToSession": {}
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write legacy state: %v", err)
	}

	store := NewStore(path)
	if err := store.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}

	if missionID, ok := store.MissionIDByWorkspaceSession("workspace-legacy"); !ok || missionID != "mission-legacy" {
		t.Fatalf("expected workspace mission index rebuilt, got %q, %v", missionID, ok)
	}
	if missionID, ok := store.MissionIDByPlannerSession("planner-legacy"); !ok || missionID != "mission-legacy" {
		t.Fatalf("expected planner mission index rebuilt, got %q, %v", missionID, ok)
	}
	if missionID, ok := store.MissionIDByWorkerSession("worker-legacy"); !ok || missionID != "mission-legacy" {
		t.Fatalf("expected worker mission index rebuilt, got %q, %v", missionID, ok)
	}
	if meta, ok := store.SessionMeta("worker-legacy"); !ok || meta.WorkerHostID != "host-1" || meta.WorkerThreadID != "thread-worker-legacy" {
		t.Fatalf("expected worker session meta rebuilt, got %#v, %v", meta, ok)
	}
}

func TestManagerDispatchAndCancel(t *testing.T) {
	store := NewStore("")
	mgr := NewManager(WithStore(store))
	mission, err := mgr.StartMission(context.Background(), StartMissionRequest{
		MissionID:          "mission-1",
		WorkspaceSessionID: "workspace-1",
		Title:              "demo",
		Summary:            "summary",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	if mission.Status != MissionStatusRunning {
		t.Fatalf("expected running mission, got %s", mission.Status)
	}
	result, err := mgr.Dispatch(context.Background(), DispatchRequest{
		MissionID: "mission-1",
		Tasks: []DispatchTaskRequest{
			{TaskID: "task-1", HostID: "host-1", Title: "inspect"},
			{TaskID: "task-2", HostID: "host-1", Title: "follow-up"},
		},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.Accepted != 2 {
		t.Fatalf("expected 2 accepted, got %d", result.Accepted)
	}
	mission, ok := mgr.MissionBySession("workspace-1")
	if !ok {
		t.Fatalf("expected mission by workspace session")
	}
	if len(mission.Workers) != 1 {
		t.Fatalf("expected one worker, got %d", len(mission.Workers))
	}
	if err := mgr.CancelByWorkspaceSession(context.Background(), "workspace-1"); err != nil {
		t.Fatalf("cancel by workspace: %v", err)
	}
	mission, _ = mgr.MissionBySession("workspace-1")
	if mission.Status != MissionStatusCancelled {
		t.Fatalf("expected cancelled mission, got %s", mission.Status)
	}
	worker := mission.Workers["host-1"]
	if worker == nil {
		t.Fatalf("expected worker for host-1")
	}
	if worker.ActiveTaskID != "" || len(worker.QueueTaskIDs) != 0 {
		t.Fatalf("expected cancelled worker to clear active and queue, got %#v", worker)
	}
	if got := mission.Tasks["task-1"]; got == nil || got.Status != TaskStatusCancelled {
		t.Fatalf("expected task-1 cancelled, got %#v", got)
	}
	if got := mission.Tasks["task-2"]; got == nil || got.Status != TaskStatusCancelled {
		t.Fatalf("expected task-2 cancelled, got %#v", got)
	}
}

func TestManagerReconcileAfterLoadReturnsPlannerAndWorkerFailures(t *testing.T) {
	mgr := newTestOrchestratorManager(t)

	_, err := mgr.StartMission(context.Background(), StartMissionRequest{
		MissionID:          "mission-reconcile-planner",
		WorkspaceSessionID: "workspace-reconcile-planner",
		PlannerSessionID:   "planner-reconcile-planner",
		Title:              "planner reconcile",
		Summary:            "planner reconcile",
	})
	if err != nil {
		t.Fatalf("start planner mission: %v", err)
	}

	_, err = mgr.StartMission(context.Background(), StartMissionRequest{
		MissionID:          "mission-reconcile-worker",
		WorkspaceSessionID: "workspace-reconcile-worker",
		PlannerSessionID:   "planner-reconcile-worker",
		Title:              "worker reconcile",
		Summary:            "worker reconcile",
	})
	if err != nil {
		t.Fatalf("start worker mission: %v", err)
	}
	_, err = mgr.Dispatch(context.Background(), DispatchRequest{
		MissionID: "mission-reconcile-worker",
		Tasks: []DispatchTaskRequest{
			{TaskID: "task-1", HostID: "host-1", Title: "inspect", Instruction: "inspect host"},
		},
	})
	if err != nil {
		t.Fatalf("dispatch worker mission: %v", err)
	}

	result, err := mgr.ReconcileAfterLoad(RuntimeRecoveryProbe{
		SessionHasThread: func(string) bool { return false },
		HostAvailable: func(hostID string) bool {
			return hostID != "host-1"
		},
	})
	if err != nil {
		t.Fatalf("reconcile after load: %v", err)
	}
	if len(result.Failures) != 2 {
		t.Fatalf("expected two failure outcomes, got %#v", result.Failures)
	}

	var plannerFailed, workerFailed bool
	for _, outcome := range result.Failures {
		if outcome == nil {
			continue
		}
		switch outcome.Kind {
		case SessionKindPlanner:
			plannerFailed = outcome.MissionID == "mission-reconcile-planner"
		case SessionKindWorker:
			workerFailed = outcome.MissionID == "mission-reconcile-worker" && outcome.HostID == "host-1"
		}
	}
	if !plannerFailed || !workerFailed {
		t.Fatalf("expected planner and worker failures, got %#v", result.Failures)
	}
}

func TestManagerActivateQueuedWorkersAfterReloadPreservesBudgetOccupancy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orchestrator.json")
	store := NewStore(path)
	mgr := NewManager(WithStore(store))

	_, err := mgr.StartMission(context.Background(), StartMissionRequest{
		MissionID:           "mission-reload-budget",
		WorkspaceSessionID:  "workspace-reload-budget",
		PlannerSessionID:    "planner-reload-budget",
		Title:               "reload budget",
		Summary:             "reload budget",
		MissionActiveBudget: 1,
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	_, err = mgr.Dispatch(context.Background(), DispatchRequest{
		MissionID: "mission-reload-budget",
		Tasks: []DispatchTaskRequest{
			{TaskID: "task-1", HostID: "host-1", Title: "first", Instruction: "first task"},
			{TaskID: "task-2", HostID: "host-2", Title: "second", Instruction: "second task"},
		},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if err := mgr.Save(); err != nil {
		t.Fatalf("save before reload: %v", err)
	}

	loaded := NewStore(path)
	if err := loaded.Load(); err != nil {
		t.Fatalf("load after save: %v", err)
	}
	reloadedMgr := NewManager(WithStore(loaded))

	mission, ok := reloadedMgr.MissionByWorkspaceSession("workspace-reload-budget")
	if !ok {
		t.Fatalf("expected mission after reload")
	}
	if mission.Workers["host-1"] == nil || mission.Workers["host-2"] == nil {
		t.Fatalf("expected both workers after reload, got %#v", mission.Workers)
	}
	if mission.Workers["host-1"].ActiveTaskID != "task-1" || mission.Workers["host-2"].ActiveTaskID != "" {
		t.Fatalf("expected active worker state to survive reload, got %#v", mission.Workers)
	}

	result, err := reloadedMgr.ReconcileAfterLoad(RuntimeRecoveryProbe{
		SessionHasThread: func(string) bool { return true },
		HostAvailable:    func(string) bool { return true },
	})
	if err != nil {
		t.Fatalf("reconcile after load: %v", err)
	}
	if len(result.Failures) != 0 {
		t.Fatalf("expected no failures during reload reconcile, got %#v", result.Failures)
	}

	activations, err := reloadedMgr.ActivateQueuedWorkers("workspace-reload-budget")
	if err != nil {
		t.Fatalf("activate queued workers after reload: %v", err)
	}
	if len(activations) != 0 {
		t.Fatalf("expected no queued worker activation after reload budget check, got %#v", activations)
	}

	mission, ok = reloadedMgr.MissionByWorkspaceSession("workspace-reload-budget")
	if !ok {
		t.Fatalf("expected mission after reload activation attempt")
	}
	if mission.Workers["host-1"].ActiveTaskID != "task-1" {
		t.Fatalf("expected host-1 to remain active, got %#v", mission.Workers["host-1"])
	}
	if mission.Workers["host-2"].ActiveTaskID != "" || len(mission.Workers["host-2"].QueueTaskIDs) != 1 {
		t.Fatalf("expected host-2 to remain queued, got %#v", mission.Workers["host-2"])
	}
}

func TestManagerLookupRoutes(t *testing.T) {
	store := NewStore("")
	mgr := NewManager(WithStore(store))
	_, err := mgr.StartMission(context.Background(), StartMissionRequest{
		MissionID:          "mission-2",
		WorkspaceSessionID: "workspace-2",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	if mission, ok := mgr.MissionByWorkspaceSession("workspace-2"); !ok || mission.ID != "mission-2" {
		t.Fatalf("unexpected mission lookup: %#v, %v", mission, ok)
	}
	store.LinkApprovalToWorker("approval-1", "worker-1")
	store.UpsertSessionMeta("worker-1", SessionMeta{Kind: SessionKindWorker, MissionID: "mission-2", WorkerHostID: "host-1"})
	approval, ok := mgr.ResolveApprovalRoute("approval-1")
	if !ok || approval.WorkerSessionID != "worker-1" {
		t.Fatalf("unexpected approval route: %#v, %v", approval, ok)
	}
	store.LinkChoiceToSession("choice-1", "workspace-2")
	choice, ok := mgr.ResolveChoiceRoute("choice-1")
	if !ok || choice.SessionID != "workspace-2" {
		t.Fatalf("unexpected choice route: %#v, %v", choice, ok)
	}
}

func TestManagerDispatchQueuesSameHostTasksAndReusesWorkerSession(t *testing.T) {
	mgr := newTestOrchestratorManager(t)
	_, err := mgr.StartMission(context.Background(), StartMissionRequest{
		MissionID:          "mission-queue",
		WorkspaceSessionID: "workspace-queue",
		PlannerSessionID:   "planner-queue",
		Title:              "queue demo",
		Summary:            "queue demo",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}

	result, err := mgr.Dispatch(context.Background(), DispatchRequest{
		MissionID: "mission-queue",
		Tasks: []DispatchTaskRequest{
			{TaskID: "task-1", HostID: "host-1", Title: "first", Instruction: "first task"},
			{TaskID: "task-2", HostID: "host-1", Title: "second", Instruction: "second task"},
		},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.Accepted != 2 || result.Activated != 1 || result.Queued != 1 {
		t.Fatalf("unexpected dispatch counts: %#v", result)
	}

	mission, ok := mgr.MissionByWorkspaceSession("workspace-queue")
	if !ok {
		t.Fatalf("expected mission by workspace session")
	}
	if len(mission.Workers) != 1 {
		t.Fatalf("expected exactly one worker, got %d", len(mission.Workers))
	}
	worker := mission.Workers["host-1"]
	if worker == nil {
		t.Fatalf("expected worker for host-1")
	}
	if worker.ActiveTaskID != "task-1" {
		t.Fatalf("expected task-1 active, got %q", worker.ActiveTaskID)
	}
	if len(worker.QueueTaskIDs) != 1 || worker.QueueTaskIDs[0] != "task-2" {
		t.Fatalf("unexpected queue: %#v", worker.QueueTaskIDs)
	}
	if got := mission.Tasks["task-1"]; got == nil || got.SessionID != worker.SessionID {
		t.Fatalf("expected task-1 to share worker session, got %#v", got)
	}
	if got := mission.Tasks["task-2"]; got == nil || got.SessionID != worker.SessionID {
		t.Fatalf("expected task-2 to share worker session, got %#v", got)
	}
	if got := mission.Tasks["task-2"]; got == nil || got.Status != TaskStatusQueued {
		t.Fatalf("expected task-2 queued, got %#v", got)
	}
}

func TestManagerCompleteWorkerTurnPromotesQueuedTask(t *testing.T) {
	mgr := newTestOrchestratorManager(t)
	_, err := mgr.StartMission(context.Background(), StartMissionRequest{
		MissionID:          "mission-promote",
		WorkspaceSessionID: "workspace-promote",
		PlannerSessionID:   "planner-promote",
		Title:              "promote demo",
		Summary:            "promote demo",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	_, err = mgr.Dispatch(context.Background(), DispatchRequest{
		MissionID: "mission-promote",
		Tasks: []DispatchTaskRequest{
			{TaskID: "task-1", HostID: "host-1", Title: "first", Instruction: "first task"},
			{TaskID: "task-2", HostID: "host-1", Title: "second", Instruction: "second task"},
		},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	mission, _ := mgr.MissionByWorkspaceSession("workspace-promote")
	worker := mission.Workers["host-1"]
	if worker == nil {
		t.Fatalf("expected worker for host-1")
	}

	outcome, err := mgr.CompleteWorkerTurn(worker.SessionID, "completed", "task-1 done")
	if err != nil {
		t.Fatalf("complete worker turn: %v", err)
	}
	if outcome.CompletedTaskID != "task-1" {
		t.Fatalf("expected task-1 to complete, got %q", outcome.CompletedTaskID)
	}
	if outcome.CompletedTaskStatus != TaskStatusCompleted {
		t.Fatalf("expected completed status, got %s", outcome.CompletedTaskStatus)
	}
	if outcome.NextTask == nil || outcome.NextTask.ID != "task-2" {
		t.Fatalf("expected task-2 to be promoted, got %#v", outcome.NextTask)
	}
	if outcome.NextTask.Status != TaskStatusReady {
		t.Fatalf("expected task-2 to be ready before runtime start, got %#v", outcome.NextTask)
	}
	if outcome.MissionCompleted {
		t.Fatalf("mission should still be running after first completion")
	}
	if outcome.MissionStatus != MissionStatusRunning {
		t.Fatalf("expected running mission, got %s", outcome.MissionStatus)
	}

	mission, _ = mgr.MissionByWorkspaceSession("workspace-promote")
	worker = mission.Workers["host-1"]
	if worker.ActiveTaskID != "task-2" {
		t.Fatalf("expected task-2 to become active, got %q", worker.ActiveTaskID)
	}
	if len(worker.QueueTaskIDs) != 0 {
		t.Fatalf("expected queue to be empty after promotion, got %#v", worker.QueueTaskIDs)
	}
	if got := mission.Tasks["task-1"]; got == nil || got.Status != TaskStatusCompleted {
		t.Fatalf("expected task-1 completed, got %#v", got)
	}
	if worker.Status != WorkerStatusDispatching {
		t.Fatalf("expected worker to enter dispatching for task-2, got %#v", worker)
	}
	if got := mission.Tasks["task-2"]; got == nil || got.Status != TaskStatusReady {
		t.Fatalf("expected task-2 ready before runtime start, got %#v", got)
	}
}

func TestManagerActivateQueuedWorkersPromotesAcrossHostsWhenBudgetFrees(t *testing.T) {
	mgr := newTestOrchestratorManager(t)
	_, err := mgr.StartMission(context.Background(), StartMissionRequest{
		MissionID:           "mission-cross-host",
		WorkspaceSessionID:  "workspace-cross-host",
		PlannerSessionID:    "planner-cross-host",
		Title:               "cross host demo",
		Summary:             "cross host demo",
		MissionActiveBudget: 1,
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	_, err = mgr.Dispatch(context.Background(), DispatchRequest{
		MissionID: "mission-cross-host",
		Tasks: []DispatchTaskRequest{
			{TaskID: "task-1", HostID: "host-1", Title: "first", Instruction: "first task"},
			{TaskID: "task-2", HostID: "host-2", Title: "second", Instruction: "second task"},
		},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	mission, ok := mgr.MissionByWorkspaceSession("workspace-cross-host")
	if !ok {
		t.Fatalf("expected mission after dispatch")
	}
	workerOne := mission.Workers["host-1"]
	workerTwo := mission.Workers["host-2"]
	if workerOne == nil || workerTwo == nil {
		t.Fatalf("expected two workers, got %#v", mission.Workers)
	}
	if workerOne.ActiveTaskID != "task-1" {
		t.Fatalf("expected host-1 active first, got %#v", workerOne)
	}
	if workerTwo.ActiveTaskID != "" || len(workerTwo.QueueTaskIDs) != 1 || workerTwo.QueueTaskIDs[0] != "task-2" {
		t.Fatalf("expected host-2 queued, got %#v", workerTwo)
	}

	outcome, err := mgr.CompleteWorkerTurn(workerOne.SessionID, "completed", "done")
	if err != nil {
		t.Fatalf("complete worker one: %v", err)
	}
	if outcome.NextTask != nil {
		t.Fatalf("expected no same-worker promotion, got %#v", outcome.NextTask)
	}

	activations, err := mgr.ActivateQueuedWorkers("workspace-cross-host")
	if err != nil {
		t.Fatalf("activate queued workers: %v", err)
	}
	if len(activations) != 1 {
		t.Fatalf("expected one activation, got %#v", activations)
	}
	if activations[0].WorkerHostID != "host-2" || activations[0].ActivatedTaskID != "task-2" {
		t.Fatalf("unexpected activation %#v", activations[0])
	}

	mission, _ = mgr.MissionByWorkspaceSession("workspace-cross-host")
	workerOne = mission.Workers["host-1"]
	workerTwo = mission.Workers["host-2"]
	if workerOne.Status != WorkerStatusCompleted {
		t.Fatalf("expected host-1 completed, got %#v", workerOne)
	}
	if workerTwo.ActiveTaskID != "task-2" || workerTwo.Status != WorkerStatusDispatching {
		t.Fatalf("expected host-2 promoted to dispatching, got %#v", workerTwo)
	}
	if got := mission.Tasks["task-2"]; got == nil || got.Status != TaskStatusReady {
		t.Fatalf("expected task-2 ready before runtime start, got %#v", got)
	}
}

func TestManagerCancelTaskHandlesActiveAndQueuedTasks(t *testing.T) {
	mgr := newTestOrchestratorManager(t)
	_, err := mgr.StartMission(context.Background(), StartMissionRequest{
		MissionID:          "mission-task-cancel",
		WorkspaceSessionID: "workspace-task-cancel",
		PlannerSessionID:   "planner-task-cancel",
		Title:              "task cancel demo",
		Summary:            "task cancel demo",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	_, err = mgr.Dispatch(context.Background(), DispatchRequest{
		MissionID: "mission-task-cancel",
		Tasks: []DispatchTaskRequest{
			{TaskID: "task-1", HostID: "host-1", Title: "first", Instruction: "first task"},
			{TaskID: "task-2", HostID: "host-1", Title: "second", Instruction: "second task"},
		},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	result, err := mgr.CancelTask(context.Background(), "mission-task-cancel", "task-2")
	if err != nil {
		t.Fatalf("cancel queued task: %v", err)
	}
	if result.WasActive {
		t.Fatalf("expected queued task cancel to report WasActive=false")
	}

	mission, ok := mgr.MissionByWorkspaceSession("workspace-task-cancel")
	if !ok {
		t.Fatalf("expected mission lookup")
	}
	worker := mission.Workers["host-1"]
	if worker == nil {
		t.Fatalf("expected worker for host-1")
	}
	if worker.ActiveTaskID != "task-1" || len(worker.QueueTaskIDs) != 0 {
		t.Fatalf("expected task-1 to stay active and queue cleared, got %#v", worker)
	}
	if task := mission.Tasks["task-2"]; task == nil || task.Status != TaskStatusCancelled {
		t.Fatalf("expected task-2 cancelled, got %#v", task)
	}
	if mission.Status != MissionStatusRunning {
		t.Fatalf("expected mission to stay running after queued task cancel, got %s", mission.Status)
	}

	result, err = mgr.CancelTask(context.Background(), "mission-task-cancel", "task-1")
	if err != nil {
		t.Fatalf("cancel active task: %v", err)
	}
	if !result.WasActive {
		t.Fatalf("expected active task cancel to report WasActive=true")
	}

	mission, _ = mgr.MissionByWorkspaceSession("workspace-task-cancel")
	worker = mission.Workers["host-1"]
	if worker.ActiveTaskID != "" || len(worker.QueueTaskIDs) != 0 {
		t.Fatalf("expected active task cancel to clear worker binding, got %#v", worker)
	}
	if task := mission.Tasks["task-1"]; task == nil || task.Status != TaskStatusCancelled {
		t.Fatalf("expected task-1 cancelled, got %#v", task)
	}
	if mission.Status != MissionStatusCancelled {
		t.Fatalf("expected mission cancelled after all tasks cancelled, got %s", mission.Status)
	}
}

func TestManagerCompleteWorkerTurnIgnoresLateCompletionAfterWorkerFailure(t *testing.T) {
	mgr := newTestOrchestratorManager(t)
	_, err := mgr.StartMission(context.Background(), StartMissionRequest{
		MissionID:          "mission-late",
		WorkspaceSessionID: "workspace-late",
		PlannerSessionID:   "planner-late",
		Title:              "late completion demo",
		Summary:            "late completion demo",
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	_, err = mgr.Dispatch(context.Background(), DispatchRequest{
		MissionID: "mission-late",
		Tasks: []DispatchTaskRequest{
			{TaskID: "task-1", HostID: "host-1", Title: "first", Instruction: "first task"},
		},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	mission, ok := mgr.MissionByWorkspaceSession("workspace-late")
	if !ok {
		t.Fatalf("expected mission lookup")
	}
	worker := mission.Workers["host-1"]
	if worker == nil {
		t.Fatalf("expected worker for host-1")
	}

	failure, err := mgr.FailWorkerSession(worker.SessionID, "remote host unavailable")
	if err != nil {
		t.Fatalf("fail worker session: %v", err)
	}
	if failure == nil || len(failure.FailedTaskIDs) != 1 || failure.FailedTaskIDs[0] != "task-1" {
		t.Fatalf("expected failed task-1, got %#v", failure)
	}

	outcome, err := mgr.CompleteWorkerTurn(worker.SessionID, "aborted", "")
	if err != nil {
		t.Fatalf("late complete worker turn: %v", err)
	}
	if outcome.CompletedTaskID != "" {
		t.Fatalf("expected late completion to be ignored, got %#v", outcome)
	}

	mission, _ = mgr.MissionByWorkspaceSession("workspace-late")
	worker = mission.Workers["host-1"]
	if worker.Status != WorkerStatusFailed {
		t.Fatalf("expected worker to stay failed, got %s", worker.Status)
	}
	if task := mission.Tasks["task-1"]; task == nil || task.Status != TaskStatusFailed {
		t.Fatalf("expected task-1 to stay failed, got %#v", task)
	}
}

func TestManagerSummarizesMissionTerminalStatuses(t *testing.T) {
	t.Run("completed", func(t *testing.T) {
		mgr := newTestOrchestratorManager(t)
		missionID, workspaceSessionID := mustStartTerminalMission(t, mgr, "mission-completed", "workspace-completed", "planner-completed")
		outcome1 := mustCompleteHostTask(t, mgr, workspaceSessionID, "host-1", "completed", "first done")
		if outcome1.NextTask == nil || outcome1.NextTask.ID != "task-2" {
			t.Fatalf("expected second task to be promoted, got %#v", outcome1.NextTask)
		}
		outcome2 := mustCompleteHostTask(t, mgr, workspaceSessionID, "host-1", "completed", "second done")
		if !outcome2.MissionCompleted {
			t.Fatalf("expected mission to be completed")
		}
		if outcome2.MissionStatus != MissionStatusCompleted {
			t.Fatalf("expected completed mission, got %s", outcome2.MissionStatus)
		}
		mission, ok := mgr.MissionByWorkspaceSession(workspaceSessionID)
		if !ok || mission.ID != missionID {
			t.Fatalf("unexpected mission lookup: %#v, %v", mission, ok)
		}
		if mission.Status != MissionStatusCompleted {
			t.Fatalf("expected mission status completed, got %s", mission.Status)
		}
	})

	t.Run("failed", func(t *testing.T) {
		mgr := newTestOrchestratorManager(t)
		_, workspaceSessionID := mustStartTerminalMission(t, mgr, "mission-failed", "workspace-failed", "planner-failed")
		mustCompleteHostTask(t, mgr, workspaceSessionID, "host-1", "completed", "first done")
		outcome := mustCompleteHostTask(t, mgr, workspaceSessionID, "host-1", "failed", "second failed")
		if !outcome.MissionCompleted {
			t.Fatalf("expected mission to be terminal after second task")
		}
		if outcome.MissionStatus != MissionStatusFailed {
			t.Fatalf("expected failed mission, got %s", outcome.MissionStatus)
		}
		mission, _ := mgr.MissionByWorkspaceSession(workspaceSessionID)
		if mission.Status != MissionStatusFailed {
			t.Fatalf("expected mission status failed, got %s", mission.Status)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		mgr := newTestOrchestratorManager(t)
		_, workspaceSessionID := mustStartTerminalMission(t, mgr, "mission-cancelled", "workspace-cancelled", "planner-cancelled")
		mustCompleteHostTask(t, mgr, workspaceSessionID, "host-1", "completed", "first done")
		outcome := mustCompleteHostTask(t, mgr, workspaceSessionID, "host-1", "cancelled", "second cancelled")
		if !outcome.MissionCompleted {
			t.Fatalf("expected mission to be terminal after second task")
		}
		if outcome.MissionStatus != MissionStatusCancelled {
			t.Fatalf("expected cancelled mission, got %s", outcome.MissionStatus)
		}
		mission, _ := mgr.MissionByWorkspaceSession(workspaceSessionID)
		if mission.Status != MissionStatusCancelled {
			t.Fatalf("expected mission status cancelled, got %s", mission.Status)
		}
	})
}

func TestManagerFailWorkersByHostMarksQueuedAndActiveTasksFailed(t *testing.T) {
	mgr := newTestOrchestratorManager(t)
	_, workspaceSessionID := mustStartTerminalMission(t, mgr, "mission-host-failure", "workspace-host-failure", "planner-host-failure")

	outcomes, err := mgr.FailWorkersByHost("host-1", "remote host disconnected")
	if err != nil {
		t.Fatalf("fail workers by host: %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("expected one outcome, got %d", len(outcomes))
	}
	outcome := outcomes[0]
	if outcome.CompletedTaskID != "task-1" {
		t.Fatalf("expected active task to fail, got %q", outcome.CompletedTaskID)
	}
	if outcome.CompletedTaskStatus != TaskStatusFailed {
		t.Fatalf("expected failed task status, got %s", outcome.CompletedTaskStatus)
	}
	if !outcome.MissionCompleted {
		t.Fatalf("expected mission to become terminal after host failure")
	}
	if outcome.MissionStatus != MissionStatusFailed {
		t.Fatalf("expected failed mission status, got %s", outcome.MissionStatus)
	}

	mission, ok := mgr.MissionByWorkspaceSession(workspaceSessionID)
	if !ok {
		t.Fatalf("expected mission for workspace session")
	}
	worker := mission.Workers["host-1"]
	if worker == nil {
		t.Fatalf("expected worker for host-1")
	}
	if worker.Status != WorkerStatusFailed {
		t.Fatalf("expected worker failed, got %s", worker.Status)
	}
	if worker.ActiveTaskID != "" {
		t.Fatalf("expected worker active task to be cleared, got %q", worker.ActiveTaskID)
	}
	if len(worker.QueueTaskIDs) != 0 {
		t.Fatalf("expected worker queue to be cleared, got %#v", worker.QueueTaskIDs)
	}
	if got := mission.Tasks["task-1"]; got == nil || got.Status != TaskStatusFailed || got.LastReply != "remote host disconnected" {
		t.Fatalf("expected task-1 failed with reply, got %#v", got)
	}
	if got := mission.Tasks["task-2"]; got == nil || got.Status != TaskStatusFailed || got.LastReply != "remote host disconnected" {
		t.Fatalf("expected task-2 failed with reply, got %#v", got)
	}
}

func newTestOrchestratorManager(t *testing.T) *Manager {
	t.Helper()
	return NewManager(
		WithStore(NewStore("")),
		WithWorkspaceRoot(t.TempDir()),
	)
}

func mustStartTerminalMission(t *testing.T, mgr *Manager, missionID, workspaceSessionID, plannerSessionID string) (string, string) {
	t.Helper()
	mission, err := mgr.StartMission(context.Background(), StartMissionRequest{
		MissionID:          missionID,
		WorkspaceSessionID: workspaceSessionID,
		PlannerSessionID:   plannerSessionID,
		Title:              missionID,
		Summary:            missionID,
	})
	if err != nil {
		t.Fatalf("start mission: %v", err)
	}
	_, err = mgr.Dispatch(context.Background(), DispatchRequest{
		MissionID: missionID,
		Tasks: []DispatchTaskRequest{
			{TaskID: "task-1", HostID: "host-1", Title: "first", Instruction: "first task"},
			{TaskID: "task-2", HostID: "host-1", Title: "second", Instruction: "second task"},
		},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	return mission.ID, workspaceSessionID
}

func mustCompleteHostTask(t *testing.T, mgr *Manager, workspaceSessionID, hostID, phase, reply string) *WorkerTurnOutcome {
	t.Helper()
	mission, ok := mgr.MissionByWorkspaceSession(workspaceSessionID)
	if !ok {
		t.Fatalf("expected mission for workspace session %s", workspaceSessionID)
	}
	worker := mission.Workers[hostID]
	if worker == nil {
		t.Fatalf("expected worker for host %s", hostID)
	}
	outcome, err := mgr.CompleteWorkerTurn(worker.SessionID, phase, reply)
	if err != nil {
		t.Fatalf("complete worker turn: %v", err)
	}
	return outcome
}
