package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDispatcherDispatchValidatesMissionMetadata(t *testing.T) {
	store := NewStore("")
	if _, err := store.UpsertMission(&Mission{
		ID:                  "mission-metadata",
		Status:              MissionStatusRunning,
		Workers:             make(map[string]*HostWorker),
		Tasks:               make(map[string]*TaskRun),
		Workspaces:          make(map[string]*WorkspaceLease),
		GlobalActiveBudget:  DefaultGlobalActiveBudget,
		MissionActiveBudget: DefaultMissionActiveBudget,
	}); err != nil {
		t.Fatalf("upsert mission: %v", err)
	}
	dispatcher := newDispatcher(store)

	_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		MissionID: "mission-metadata",
		Tasks: []DispatchTaskRequest{
			{TaskID: "task-1", HostID: "host-1", Instruction: "inspect"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "mission title is required") {
		t.Fatalf("expected missing mission title error, got %v", err)
	}

	_, err = dispatcher.Dispatch(context.Background(), DispatchRequest{
		MissionID:    "mission-metadata",
		MissionTitle: "mission title",
		Tasks: []DispatchTaskRequest{
			{TaskID: "task-1", HostID: "host-1", Instruction: "inspect"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "mission summary is required") {
		t.Fatalf("expected missing mission summary error, got %v", err)
	}

	result, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		MissionID:    "mission-metadata",
		MissionTitle: "mission title",
		Summary:      "mission summary",
		Tasks: []DispatchTaskRequest{
			{TaskID: "task-1", HostID: "host-1", Instruction: "inspect"},
		},
	})
	if err != nil {
		t.Fatalf("dispatch with metadata: %v", err)
	}
	if result.Accepted != 1 || result.Activated != 1 {
		t.Fatalf("unexpected dispatch result: %#v", result)
	}
}

func TestDispatcherDispatchValidatesTaskFieldsAndDuplicates(t *testing.T) {
	cases := []struct {
		name  string
		tasks []DispatchTaskRequest
		want  string
	}{
		{
			name:  "missing task id",
			tasks: []DispatchTaskRequest{{HostID: "host-1", Instruction: "inspect"}},
			want:  "task id is required",
		},
		{
			name:  "missing host id",
			tasks: []DispatchTaskRequest{{TaskID: "task-1", Instruction: "inspect"}},
			want:  "host id is required",
		},
		{
			name:  "missing instruction",
			tasks: []DispatchTaskRequest{{TaskID: "task-1", HostID: "host-1"}},
			want:  "instruction is required",
		},
		{
			name: "duplicate task id",
			tasks: []DispatchTaskRequest{
				{TaskID: "task-1", HostID: "host-1", Instruction: "inspect"},
				{TaskID: "task-1", HostID: "host-2", Instruction: "inspect again"},
			},
			want: "duplicate task id",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dispatcher := newDispatcherTestDispatcher(t, "mission-validations", "title", "summary", DefaultMissionActiveBudget)
			_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
				MissionID:    "mission-validations",
				MissionTitle: "title",
				Summary:      "summary",
				Tasks:        tc.tasks,
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func TestDispatcherReusesWorkerSessionAndHonorsBudget(t *testing.T) {
	dispatcher := newDispatcherTestDispatcher(t, "mission-budget", "budget title", "budget summary", 2)

	result, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		MissionID:    "mission-budget",
		MissionTitle: "budget title",
		Summary:      "budget summary",
		Tasks: []DispatchTaskRequest{
			{TaskID: "task-1", HostID: "host-1", Instruction: "first"},
			{TaskID: "task-2", HostID: "host-1", Instruction: "second"},
			{TaskID: "task-3", HostID: "host-2", Instruction: "third"},
			{TaskID: "task-4", HostID: "host-3", Instruction: "fourth"},
		},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.Accepted != 4 || result.Activated != 2 || result.Queued != 2 {
		t.Fatalf("unexpected dispatch result: %#v", result)
	}

	mission, ok := dispatcher.store.Mission("mission-budget")
	if !ok {
		t.Fatalf("expected mission")
	}
	workerOne := mission.Workers["host-1"]
	workerTwo := mission.Workers["host-2"]
	workerThree := mission.Workers["host-3"]
	if workerOne == nil || workerTwo == nil || workerThree == nil {
		t.Fatalf("expected workers for all hosts, got %#v", mission.Workers)
	}
	if workerOne.SessionID == "" || workerTwo.SessionID == "" || workerThree.SessionID == "" {
		t.Fatalf("expected worker sessions to be allocated, got %#v", mission.Workers)
	}
	if got := mission.Tasks["task-1"]; got == nil || got.SessionID != workerOne.SessionID || got.Status != TaskStatusReady {
		t.Fatalf("expected task-1 to be ready on host-1 worker, got %#v", got)
	}
	if got := mission.Tasks["task-2"]; got == nil || got.SessionID != workerOne.SessionID || got.Status != TaskStatusQueued {
		t.Fatalf("expected task-2 to queue behind host-1 worker, got %#v", got)
	}
	if got := mission.Tasks["task-3"]; got == nil || got.Status != TaskStatusReady {
		t.Fatalf("expected task-3 ready, got %#v", got)
	}
	if got := mission.Tasks["task-4"]; got == nil || got.Status != TaskStatusQueued {
		t.Fatalf("expected task-4 queued, got %#v", got)
	}
	if workerOne.ActiveTaskID != "task-1" || len(workerOne.QueueTaskIDs) != 1 || workerOne.QueueTaskIDs[0] != "task-2" {
		t.Fatalf("unexpected host-1 worker state: %#v", workerOne)
	}
	if workerTwo.ActiveTaskID != "task-3" || len(workerTwo.QueueTaskIDs) != 0 {
		t.Fatalf("unexpected host-2 worker state: %#v", workerTwo)
	}
	if workerThree.ActiveTaskID != "" || len(workerThree.QueueTaskIDs) != 1 || workerThree.QueueTaskIDs[0] != "task-4" {
		t.Fatalf("unexpected host-3 worker state: %#v", workerThree)
	}
}

func TestDispatcherDispatchCapsInitialActivationForLargeFanout(t *testing.T) {
	const (
		hostCount      = 1000
		missionBudget  = 4
		expectedQueued = hostCount - missionBudget
	)

	dispatcher := newDispatcherTestDispatcher(t, "mission-large-fanout", "fanout title", "fanout summary", missionBudget)
	tasks := make([]DispatchTaskRequest, 0, hostCount)
	for i := 0; i < hostCount; i++ {
		tasks = append(tasks, DispatchTaskRequest{
			TaskID:      fmt.Sprintf("task-%04d", i+1),
			HostID:      fmt.Sprintf("host-%04d", i+1),
			Instruction: "inspect host state",
		})
	}

	result, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		MissionID:    "mission-large-fanout",
		MissionTitle: "fanout title",
		Summary:      "fanout summary",
		Tasks:        tasks,
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.Accepted != hostCount || result.Activated != missionBudget || result.Queued != expectedQueued {
		t.Fatalf("unexpected dispatch counts: %#v", result)
	}

	mission, ok := dispatcher.store.Mission("mission-large-fanout")
	if !ok {
		t.Fatalf("expected mission")
	}
	ready := 0
	queued := 0
	for _, task := range mission.Tasks {
		if task == nil {
			continue
		}
		switch task.Status {
		case TaskStatusReady:
			ready++
		case TaskStatusQueued:
			queued++
		}
	}
	if ready != missionBudget {
		t.Fatalf("expected %d ready tasks, got %d", missionBudget, ready)
	}
	if queued != expectedQueued {
		t.Fatalf("expected %d queued tasks, got %d", expectedQueued, queued)
	}
}

func TestDispatcherResetIdleWorkerThread(t *testing.T) {
	dispatcher := newDispatcherTestDispatcher(t, "mission-idle", "idle title", "idle summary", DefaultMissionActiveBudget)
	_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		MissionID:    "mission-idle",
		MissionTitle: "idle title",
		Summary:      "idle summary",
		Tasks: []DispatchTaskRequest{
			{TaskID: "task-1", HostID: "host-1", Instruction: "inspect"},
		},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	mission, ok := dispatcher.store.Mission("mission-idle")
	if !ok {
		t.Fatalf("expected mission")
	}
	worker := mission.Workers["host-1"]
	if worker == nil {
		t.Fatalf("expected worker")
	}
	idleSince := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano)
	_, err = dispatcher.store.UpdateMission("mission-idle", func(m *Mission) error {
		current := m.Workers["host-1"]
		current.ActiveTaskID = ""
		current.QueueTaskIDs = nil
		current.Status = WorkerStatusIdle
		current.ThreadID = "thread-idle"
		current.IdleSince = idleSince
		return nil
	})
	if err != nil {
		t.Fatalf("update mission: %v", err)
	}

	result, err := dispatcher.ResetIdleWorkerThread(worker.SessionID, time.Hour)
	if err != nil {
		t.Fatalf("reset idle worker thread: %v", err)
	}
	if !result.WasReset {
		t.Fatalf("expected thread reset, got %#v", result)
	}

	updatedMission, ok := dispatcher.store.Mission("mission-idle")
	if !ok {
		t.Fatalf("expected updated mission")
	}
	updatedWorker := updatedMission.Workers["host-1"]
	if updatedWorker == nil {
		t.Fatalf("expected updated worker")
	}
	if updatedWorker.ThreadID != "" {
		t.Fatalf("expected thread to be cleared, got %#v", updatedWorker)
	}
	if updatedWorker.Status != WorkerStatusIdle {
		t.Fatalf("expected worker to stay idle, got %s", updatedWorker.Status)
	}
	if updatedWorker.SessionID != worker.SessionID || updatedWorker.HostID != worker.HostID {
		t.Fatalf("expected worker identity to be preserved, got %#v", updatedWorker)
	}
}

func newDispatcherTestDispatcher(t *testing.T, missionID, title, summary string, missionBudget int) *Dispatcher {
	t.Helper()
	store := NewStore("")
	_, err := store.UpsertMission(&Mission{
		ID:                  missionID,
		Title:               title,
		Summary:             summary,
		Status:              MissionStatusRunning,
		Workers:             make(map[string]*HostWorker),
		Tasks:               make(map[string]*TaskRun),
		Workspaces:          make(map[string]*WorkspaceLease),
		GlobalActiveBudget:  DefaultGlobalActiveBudget,
		MissionActiveBudget: missionBudget,
	})
	if err != nil {
		t.Fatalf("upsert mission: %v", err)
	}
	store.UpsertSessionMeta("workspace-"+missionID, SessionMeta{
		Kind:               SessionKindWorkspace,
		Visible:            true,
		MissionID:          missionID,
		WorkspaceSessionID: "workspace-" + missionID,
		RuntimePreset:      RuntimePresetWorkspaceFront,
	})
	return newDispatcher(store)
}
