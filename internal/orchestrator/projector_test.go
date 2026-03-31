package orchestrator

import (
	"strings"
	"testing"
)

func TestProjectWorkerProgressUsesStableIdentity(t *testing.T) {
	idle := ProjectWorkerProgress("host-1", nil)
	if idle.ID != "worker:host-1:progress" {
		t.Fatalf("unexpected idle worker progress id: %q", idle.ID)
	}
	if idle.HostID != "host-1" || idle.Label != "host-1" {
		t.Fatalf("unexpected idle worker labels: %#v", idle)
	}
	if idle.Status != string(WorkerStatusIdle) {
		t.Fatalf("expected idle status, got %q", idle.Status)
	}
	if idle.Caption != "等待 worker 连接" {
		t.Fatalf("unexpected idle caption: %q", idle.Caption)
	}
	if idle.Summary != "worker offline" {
		t.Fatalf("unexpected idle summary: %q", idle.Summary)
	}

	worker := &HostWorker{
		MissionID:    "mission-1",
		HostID:       "host-1",
		Status:       WorkerStatusRunning,
		ActiveTaskID: "task-1",
		QueueTaskIDs: []string{"task-2", "task-3"},
		IdleSince:    "2026-03-30T10:00:00Z",
	}
	running := ProjectWorkerProgress("host-1", worker)
	if running.ID != idle.ID {
		t.Fatalf("expected same host to keep the same progress id, got %q and %q", idle.ID, running.ID)
	}
	if running.Status != string(WorkerStatusRunning) {
		t.Fatalf("unexpected running status: %q", running.Status)
	}
	if running.Caption != "active task-1 · queue 2" {
		t.Fatalf("unexpected running caption: %q", running.Caption)
	}
	if running.Summary != "active=task-1 queue=task-2, task-3" {
		t.Fatalf("unexpected running summary: %q", running.Summary)
	}
}

func TestProjectMissionAndPlanViews(t *testing.T) {
	mission := &Mission{
		ID:               "mission-1",
		Title:            "Deploy nginx",
		Summary:          "roll out cert fix",
		Status:           MissionStatusPending,
		PlannerSessionID: "planner-1",
		PlannerThreadID:  "thread-1",
		UpdatedAt:        "2026-03-30T10:00:00Z",
		Tasks: map[string]*TaskRun{
			"task-b": {
				ID:          "task-b",
				HostID:      "host-2",
				Status:      TaskStatusRunning,
				Title:       "rotate certs",
				Instruction: "restart nginx",
			},
			"task-a": {
				ID:          "task-a",
				HostID:      "host-1",
				Status:      TaskStatusWaitingApproval,
				Instruction: "reboot host",
			},
		},
	}

	card := ProjectMissionCard(mission)
	if card.ID != "mission:mission-1" {
		t.Fatalf("unexpected mission card id: %q", card.ID)
	}
	if card.Label != "Deploy nginx" {
		t.Fatalf("unexpected mission label: %q", card.Label)
	}
	if !strings.Contains(card.Caption, "roll out cert fix") || !strings.Contains(card.Caption, "2 个任务") || !strings.Contains(card.Caption, "pending") {
		t.Fatalf("unexpected mission caption: %q", card.Caption)
	}
	if card.Status != string(MissionStatusPending) || card.StepCount != 2 {
		t.Fatalf("unexpected mission card fields: %#v", card)
	}

	summary := ProjectPlanSummary(mission)
	if summary.Label != "Deploy nginx" {
		t.Fatalf("unexpected plan summary label: %q", summary.Label)
	}
	if summary.Caption != card.Caption {
		t.Fatalf("expected plan summary caption to match mission caption, got %q and %q", summary.Caption, card.Caption)
	}
	if summary.Tone != "info" || summary.Status != string(MissionStatusPending) || summary.StepCount != 2 || summary.PlannerSessionID != "planner-1" {
		t.Fatalf("unexpected plan summary fields: %#v", summary)
	}

	detail := ProjectPlanDetail(mission)
	if detail.Title != "PlannerSession 计划详情" {
		t.Fatalf("unexpected plan detail title: %q", detail.Title)
	}
	if detail.Goal != "roll out cert fix" {
		t.Fatalf("unexpected plan detail goal: %q", detail.Goal)
	}
	if detail.GeneratedAt != mission.UpdatedAt || detail.RawPlannerTraceRef.SessionID != "planner-1" || detail.RawPlannerTraceRef.ThreadID != "thread-1" {
		t.Fatalf("unexpected planner trace metadata: %#v", detail.RawPlannerTraceRef)
	}
	if detail.DAGSummary.Nodes != 2 || detail.DAGSummary.Running != 1 || detail.DAGSummary.WaitingApproval != 1 || detail.DAGSummary.Queued != 0 {
		t.Fatalf("unexpected dag summary: %#v", detail.DAGSummary)
	}
	expected := []string{
		"task-a [waiting_approval] @host-1 reboot host",
		"task-b [running] @host-2 rotate certs",
	}
	if len(detail.StructuredProcess) != len(expected) {
		t.Fatalf("unexpected structured process length: %#v", detail.StructuredProcess)
	}
	for i, line := range expected {
		if detail.StructuredProcess[i] != line {
			t.Fatalf("unexpected structured process line %d: %q", i, detail.StructuredProcess[i])
		}
	}
}

func TestProjectWorkerApprovalCompletionReadonlyAndDispatchHostDetail(t *testing.T) {
	approval := ProjectWorkerApproval("host-1", "approval-42")
	if approval.ID != "worker:host-1:approval:approval-42" {
		t.Fatalf("unexpected approval card id: %q", approval.ID)
	}
	if approval.Label != "host-1" || approval.Caption != "approval-42" || approval.Status != "pending" {
		t.Fatalf("unexpected approval card fields: %#v", approval)
	}

	completion := ProjectWorkerCompletion("host-1", string(TaskStatusFailed))
	if completion.ID != "worker:host-1:completion" {
		t.Fatalf("unexpected completion card id: %q", completion.ID)
	}
	if completion.Caption != "任务失败" || completion.Status != string(TaskStatusFailed) {
		t.Fatalf("unexpected completion card fields: %#v", completion)
	}

	task := &TaskRun{
		ID:          "task-1",
		HostID:      "host-1",
		Status:      TaskStatusRunning,
		Title:       "rotate certs",
		Instruction: "restart nginx",
		Constraints: []string{"readonly", "safe"},
	}
	worker := &HostWorker{
		HostID:       "host-1",
		Status:       WorkerStatusWaiting,
		ActiveTaskID: "task-1",
		QueueTaskIDs: []string{"task-2", "task-3"},
		MissionID:    "mission-1",
		WorkspaceID:  "workspace-1",
		LastSeenAt:   "2026-03-30T10:00:00Z",
		IdleSince:    "2026-03-30T09:30:00Z",
		UpdatedAt:    "2026-03-30T10:00:01Z",
	}

	dispatch := ProjectDispatchSummary(&DispatchResult{Accepted: 3, Activated: 1, Queued: 2}, &Mission{Title: "Deploy nginx"})
	if dispatch.Label != "Deploy nginx" || dispatch.Caption != "accepted=3 activated=1 queued=2" {
		t.Fatalf("unexpected dispatch summary: %#v", dispatch)
	}

	hostDetail := ProjectDispatchHostDetail(task, worker)
	if hostDetail.HostID != "host-1" || hostDetail.Host != "host-1" {
		t.Fatalf("unexpected dispatch host detail identity: %#v", hostDetail)
	}
	if hostDetail.Status != string(TaskStatusRunning) {
		t.Fatalf("unexpected dispatch host detail status: %#v", hostDetail)
	}
	if hostDetail.Request.Title != "rotate certs" || hostDetail.Request.Summary != "restart nginx" {
		t.Fatalf("unexpected dispatch host request: %#v", hostDetail.Request)
	}
	if len(hostDetail.Request.Constraints) != 2 || hostDetail.Request.Constraints[0] != "readonly" || hostDetail.Request.Constraints[1] != "safe" {
		t.Fatalf("unexpected dispatch host constraints: %#v", hostDetail.Request.Constraints)
	}
	if hostDetail.TaskBinding == nil || hostDetail.TaskBinding.TaskID != "task-1" || !hostDetail.TaskBinding.Active {
		t.Fatalf("expected dispatch host detail to expose active task binding, got %#v", hostDetail.TaskBinding)
	}

	readonly := ProjectWorkerReadonlyDetail(worker)
	if readonly.HostID != "host-1" || readonly.Mode != "readonly" {
		t.Fatalf("unexpected readonly detail identity: %#v", readonly)
	}
	if readonly.JumpTarget.Type != "single_host_chat" || readonly.JumpTarget.HostID != "host-1" {
		t.Fatalf("unexpected readonly jump target: %#v", readonly.JumpTarget)
	}
	if len(readonly.Transcript) != 3 {
		t.Fatalf("unexpected readonly transcript: %#v", readonly.Transcript)
	}
	if readonly.Transcript[0] != "active task: task-1" || readonly.Transcript[1] != "queued tasks: task-2, task-3" || readonly.Transcript[2] != "idle since: 2026-03-30T09:30:00Z" {
		t.Fatalf("unexpected readonly transcript lines: %#v", readonly.Transcript)
	}
	if readonly.Terminal["status"] != string(WorkerStatusWaiting) || readonly.Terminal["activeTaskId"] != "task-1" {
		t.Fatalf("unexpected readonly terminal state: %#v", readonly.Terminal)
	}
	if queue, ok := readonly.Terminal["queueTaskIds"].([]string); !ok || len(queue) != 2 || queue[0] != "task-2" || queue[1] != "task-3" {
		t.Fatalf("unexpected readonly terminal queue: %#v", readonly.Terminal["queueTaskIds"])
	}
	if readonly.Terminal["workspaceId"] != "workspace-1" || readonly.Terminal["missionId"] != "mission-1" {
		t.Fatalf("unexpected readonly terminal metadata: %#v", readonly.Terminal)
	}
	if len(readonly.Approval) != 0 {
		t.Fatalf("expected empty approval payload, got %#v", readonly.Approval)
	}

	binding := ProjectTaskHostBinding(&TaskRun{
		ID:            "task-2",
		HostID:        "host-1",
		WorkerHostID:  "host-1",
		SessionID:     "worker-session-1",
		ThreadID:      "thread-1",
		Title:         "reload nginx",
		Instruction:   "sudo nginx -s reload",
		Constraints:   []string{"approval"},
		Status:        TaskStatusWaitingApproval,
		ApprovalState: "pending",
		LastReply:     "waiting for approval",
		LastError:     "",
	}, worker)
	if binding.TaskID != "task-2" || binding.QueuePosition != 1 || binding.Active {
		t.Fatalf("unexpected projected task binding: %#v", binding)
	}

	event := ProjectDispatchEvent(RelayEvent{
		ID:           "evt-1",
		MissionID:    "mission-1",
		TaskID:       "task-1",
		HostID:       "host-1",
		SessionID:    "worker-session-1",
		Type:         EventTypeApprovalRequested,
		Status:       "pending",
		Summary:      "host-1 waiting approval",
		Detail:       "sudo nginx -s reload",
		ApprovalID:   "approval-1",
		SourceCardID: "card-1",
		CreatedAt:    "2026-03-30T10:05:00Z",
	})
	if event.ID != "evt-1" || event.TaskID != "task-1" || event.ApprovalID != "approval-1" || event.SourceCardID != "card-1" {
		t.Fatalf("unexpected projected dispatch event: %#v", event)
	}
}
