package orchestrator

import "testing"

func TestCollectorDeduplicatesTurnPhaseAndReply(t *testing.T) {
	store := newCollectorTestStore(t)
	collector := newCollector(store)

	changed, err := collector.OnTurnPhase("worker-1", "executing")
	if err != nil {
		t.Fatalf("record turn phase: %v", err)
	}
	if !changed {
		t.Fatalf("expected first turn phase to be recorded")
	}
	if changed, err = collector.OnTurnPhase("worker-1", "executing"); err != nil {
		t.Fatalf("record duplicate turn phase: %v", err)
	} else if changed {
		t.Fatalf("expected duplicate turn phase to be deduplicated")
	}

	changed, err = collector.OnReply("worker-1", "inspection completed")
	if err != nil {
		t.Fatalf("record reply: %v", err)
	}
	if !changed {
		t.Fatalf("expected first reply to be recorded")
	}
	if changed, err = collector.OnReply("worker-1", "inspection completed"); err != nil {
		t.Fatalf("record duplicate reply: %v", err)
	} else if changed {
		t.Fatalf("expected duplicate reply to be deduplicated")
	}

	mission, ok := store.Mission("mission-1")
	if !ok {
		t.Fatalf("expected mission to exist")
	}
	if len(mission.Events) != 2 {
		t.Fatalf("expected 2 relay events, got %d", len(mission.Events))
	}
	if mission.Events[0].Type != EventTypeProgress {
		t.Fatalf("expected first event to be progress, got %s", mission.Events[0].Type)
	}
	if mission.Events[1].Type != EventTypeReply {
		t.Fatalf("expected second event to be reply, got %s", mission.Events[1].Type)
	}
}

func TestCollectorRecordsApprovalChoiceAndExecEvents(t *testing.T) {
	store := newCollectorTestStore(t)
	collector := newCollector(store)

	cases := []struct {
		name string
		fn   func() (bool, error)
		typ  EventType
	}{
		{name: "approval_requested", fn: func() (bool, error) {
			return collector.OnApprovalRequested("worker-1", "approval-1", "need approval", "sudo write")
		}, typ: EventTypeApprovalRequested},
		{name: "approval_resolved", fn: func() (bool, error) {
			return collector.OnApprovalResolved("worker-1", "approval-1", "accepted", "approved")
		}, typ: EventTypeApprovalResolved},
		{name: "choice_requested", fn: func() (bool, error) {
			return collector.OnChoiceRequested("worker-1", "choice-1", "pick strategy")
		}, typ: EventTypeChoiceRequested},
		{name: "choice_resolved", fn: func() (bool, error) {
			return collector.OnChoiceResolved("worker-1", "choice-1", "picked strategy")
		}, typ: EventTypeChoiceResolved},
		{name: "exec_started", fn: func() (bool, error) {
			return collector.OnRemoteExecStarted("worker-1", "host-1", "card-1", "uname -a")
		}, typ: EventTypeExecStarted},
		{name: "exec_finished", fn: func() (bool, error) {
			return collector.OnRemoteExecFinished("worker-1", "host-1", "card-1", "completed", "uname -a", "Linux")
		}, typ: EventTypeExecFinished},
	}

	for _, tc := range cases {
		changed, err := tc.fn()
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !changed {
			t.Fatalf("%s: expected first event to be recorded", tc.name)
		}
		changed, err = tc.fn()
		if err != nil {
			t.Fatalf("%s duplicate: %v", tc.name, err)
		}
		if changed {
			t.Fatalf("%s: expected duplicate event to be deduplicated", tc.name)
		}
	}

	mission, ok := store.Mission("mission-1")
	if !ok {
		t.Fatalf("expected mission to exist")
	}
	if len(mission.Events) != len(cases) {
		t.Fatalf("expected %d events, got %d", len(cases), len(mission.Events))
	}
	for i, tc := range cases {
		if mission.Events[i].Type != tc.typ {
			t.Fatalf("event %d: expected %s, got %s", i, tc.typ, mission.Events[i].Type)
		}
	}
}

func TestCollectorSnapshotFallbackDeduplicates(t *testing.T) {
	store := newCollectorTestStore(t)
	collector := newCollector(store)

	if changed, err := collector.OnSnapshot("worker-1", Snapshot{
		SessionID: "worker-1",
		MissionID: "mission-1",
		Status:    "executing",
		Summary:   "first reply",
	}); err != nil {
		t.Fatalf("first snapshot: %v", err)
	} else if !changed {
		t.Fatalf("expected first snapshot to generate events")
	}
	if changed, err := collector.OnSnapshot("worker-1", Snapshot{
		SessionID: "worker-1",
		MissionID: "mission-1",
		Status:    "executing",
		Summary:   "first reply",
	}); err != nil {
		t.Fatalf("duplicate snapshot: %v", err)
	} else if changed {
		t.Fatalf("expected duplicate snapshot to be deduplicated")
	}
	if changed, err := collector.OnSnapshot("worker-1", Snapshot{
		SessionID: "worker-1",
		MissionID: "mission-1",
		Status:    "completed",
		Summary:   "second reply",
	}); err != nil {
		t.Fatalf("second snapshot: %v", err)
	} else if !changed {
		t.Fatalf("expected second snapshot to generate new events")
	}

	mission, ok := store.Mission("mission-1")
	if !ok {
		t.Fatalf("expected mission to exist")
	}
	if len(mission.Events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(mission.Events))
	}
	if mission.Events[2].Type != EventTypeCompleted {
		t.Fatalf("expected third event to be completed, got %s", mission.Events[2].Type)
	}
	if mission.Events[3].Type != EventTypeReply {
		t.Fatalf("expected fourth event to be reply, got %s", mission.Events[3].Type)
	}
}

func newCollectorTestStore(t *testing.T) *Store {
	t.Helper()
	store := NewStore("")
	_, err := store.UpsertMission(&Mission{
		ID:                  "mission-1",
		WorkspaceSessionID:  "workspace-1",
		PlannerSessionID:    "planner-1",
		Status:              MissionStatusRunning,
		Workers:             map[string]*HostWorker{"host-1": {MissionID: "mission-1", HostID: "host-1", SessionID: "worker-1", ActiveTaskID: "task-1", Status: WorkerStatusRunning}},
		Tasks:               map[string]*TaskRun{"task-1": {ID: "task-1", MissionID: "mission-1", HostID: "host-1", SessionID: "worker-1", Status: TaskStatusRunning}},
		Workspaces:          make(map[string]*WorkspaceLease),
		GlobalActiveBudget:  DefaultGlobalActiveBudget,
		MissionActiveBudget: DefaultMissionActiveBudget,
	})
	if err != nil {
		t.Fatalf("upsert mission: %v", err)
	}
	store.UpsertSessionMeta("workspace-1", SessionMeta{Kind: SessionKindWorkspace, Visible: true, MissionID: "mission-1"})
	store.UpsertSessionMeta("planner-1", SessionMeta{Kind: SessionKindPlanner, Visible: false, MissionID: "mission-1", WorkspaceSessionID: "workspace-1"})
	store.UpsertSessionMeta("worker-1", SessionMeta{Kind: SessionKindWorker, Visible: false, MissionID: "mission-1", WorkspaceSessionID: "workspace-1", WorkerHostID: "host-1"})
	return store
}
