package orchestrator

import "testing"

func TestProjectMissionHistorySummaryAndDetail(t *testing.T) {
	mission := &Mission{
		ID:                 "mission-1",
		WorkspaceSessionID: "workspace-1",
		PlannerSessionID:   "planner-1",
		Title:              "Deploy nginx",
		Summary:            "roll out nginx reload",
		Status:             MissionStatusRunning,
		ProjectionMode:     "front_projection",
		CreatedAt:          "2026-03-30T10:00:00Z",
		UpdatedAt:          "2026-03-30T10:05:00Z",
		Tasks: map[string]*TaskRun{
			"task-b": {
				ID:          "task-b",
				MissionID:   "mission-1",
				HostID:      "host-2",
				Status:      TaskStatusWaitingApproval,
				Title:       "reload nginx",
				Instruction: "sudo nginx -s reload",
			},
			"task-a": {
				ID:          "task-a",
				MissionID:   "mission-1",
				HostID:      "host-1",
				Status:      TaskStatusRunning,
				Title:       "reload nginx",
				Instruction: "sudo nginx -s reload",
			},
		},
		Workers: map[string]*HostWorker{
			"host-2": {
				MissionID:    "mission-1",
				HostID:       "host-2",
				SessionID:    "worker-2",
				ThreadID:     "thread-2",
				WorkspaceID:  "workspace-2",
				ActiveTaskID: "task-b",
				Status:       WorkerStatusWaiting,
				UpdatedAt:    "2026-03-30T10:05:00Z",
			},
			"host-1": {
				MissionID:    "mission-1",
				HostID:       "host-1",
				SessionID:    "worker-1",
				ThreadID:     "thread-1",
				WorkspaceID:  "workspace-1",
				ActiveTaskID: "task-a",
				Status:       WorkerStatusRunning,
				UpdatedAt:    "2026-03-30T10:04:00Z",
			},
		},
		Workspaces: map[string]*WorkspaceLease{
			"planner:mission-1": {
				ID:        "planner:mission-1",
				MissionID: "mission-1",
				SessionID: "planner-1",
				Kind:      LeaseKindPlanner,
				Status:    "prepared",
			},
			"worker:mission-1:host-1": {
				ID:        "worker:mission-1:host-1",
				MissionID: "mission-1",
				SessionID: "worker-1",
				HostID:    "host-1",
				Kind:      LeaseKindWorker,
				Status:    "prepared",
			},
		},
		Events: []RelayEvent{
			{
				ID:        "evt-1",
				MissionID: "mission-1",
				Type:      EventTypeDispatch,
				Summary:   "dispatch accepted",
				CreatedAt: "2026-03-30T10:01:00Z",
			},
			{
				ID:        "evt-2",
				MissionID: "mission-1",
				Type:      EventTypeApprovalRequested,
				Summary:   "approval needed",
				CreatedAt: "2026-03-30T10:02:00Z",
			},
		},
	}

	summary := ProjectMissionHistorySummary(mission)
	if summary.ID != "mission-1" || summary.WorkspaceSessionID != "workspace-1" || summary.PlannerSessionID != "planner-1" {
		t.Fatalf("unexpected summary identity: %#v", summary)
	}
	if summary.TaskCount != 2 || summary.WorkerCount != 2 || summary.WorkspaceCount != 2 || summary.EventCount != 2 {
		t.Fatalf("unexpected summary counts: %#v", summary)
	}
	if summary.RunningTaskCount != 1 || summary.WaitingApprovalTaskCount != 1 || summary.CompletedTaskCount != 0 {
		t.Fatalf("unexpected summary task status counts: %#v", summary)
	}

	detail := ProjectMissionHistoryDetail(mission)
	if detail.Report.Summary == "" {
		t.Fatalf("expected non-empty report summary: %#v", detail.Report)
	}
	if len(detail.Report.OverviewRows) == 0 {
		t.Fatalf("expected report overview rows")
	}
	if len(detail.Tasks) != 2 || detail.Tasks[0].ID != "task-a" || detail.Tasks[1].ID != "task-b" {
		t.Fatalf("unexpected task ordering: %#v", detail.Tasks)
	}
	if len(detail.Workers) != 2 || detail.Workers[0].HostID != "host-1" || detail.Workers[1].HostID != "host-2" {
		t.Fatalf("unexpected worker ordering: %#v", detail.Workers)
	}
	if len(detail.Events) != 2 || detail.Events[0].ID != "evt-1" || detail.Events[1].ID != "evt-2" {
		t.Fatalf("unexpected event ordering: %#v", detail.Events)
	}
	if len(detail.Report.Timeline) != 2 || detail.Report.Timeline[0].ID != "evt-1" || detail.Report.Timeline[1].ID != "evt-2" {
		t.Fatalf("unexpected report timeline ordering: %#v", detail.Report.Timeline)
	}
}
