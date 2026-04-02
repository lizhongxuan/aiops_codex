package orchestrator

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

type MissionCardView struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Caption   string `json:"caption,omitempty"`
	Status    string `json:"status,omitempty"`
	StepCount int    `json:"stepCount,omitempty"`
}

type WorkerProgressCardView struct {
	ID      string `json:"id"`
	HostID  string `json:"hostId"`
	Label   string `json:"label"`
	Caption string `json:"caption,omitempty"`
	Status  string `json:"status,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type WorkerApprovalCardView struct {
	ID         string `json:"id"`
	HostID     string `json:"hostId"`
	ApprovalID string `json:"approvalId"`
	Label      string `json:"label"`
	Caption    string `json:"caption,omitempty"`
	Status     string `json:"status,omitempty"`
}

type WorkerCompletionCardView struct {
	ID      string `json:"id"`
	HostID  string `json:"hostId"`
	Label   string `json:"label"`
	Caption string `json:"caption,omitempty"`
	Status  string `json:"status,omitempty"`
}

type PlanSummaryView struct {
	Label     string `json:"label"`
	Caption   string `json:"caption,omitempty"`
	Tone      string `json:"tone,omitempty"`
	Status    string `json:"status,omitempty"`
	StepCount int    `json:"stepCount,omitempty"`
	// Deprecated: kept only so legacy internal tests can compile while public JSON stays planner-free.
	PlannerSessionID string `json:"-"`
}

type PlanDetailView struct {
	Title             string `json:"title"`
	Goal              string `json:"goal,omitempty"`
	Version           string `json:"version,omitempty"`
	GeneratedAt       string `json:"generatedAt,omitempty"`
	OwnerSessionLabel string `json:"ownerSessionLabel,omitempty"`
	DAGSummary        struct {
		Nodes           int `json:"nodes,omitempty"`
		Running         int `json:"running,omitempty"`
		WaitingApproval int `json:"waitingApproval,omitempty"`
		Queued          int `json:"queued,omitempty"`
	} `json:"dagSummary,omitempty"`
	StructuredProcess []string `json:"structured_process,omitempty"`
	DispatchEvents    []DispatchEventView   `json:"dispatch_events,omitempty"`
	TaskHostBindings  []TaskHostBindingView `json:"task_host_bindings,omitempty"`
	// Deprecated: kept only so legacy internal tests can compile while public JSON stays planner-free.
	RawPlannerTraceRef struct {
		SessionID string `json:"sessionId,omitempty"`
		ThreadID  string `json:"threadId,omitempty"`
	} `json:"-"`
}

type DispatchSummaryView struct {
	Label     string `json:"label"`
	Caption   string `json:"caption,omitempty"`
	Accepted  int    `json:"accepted,omitempty"`
	Activated int    `json:"activated,omitempty"`
	Queued    int    `json:"queued,omitempty"`
}

type DispatchHostDetailView struct {
	HostID  string `json:"hostId"`
	Host    string `json:"host,omitempty"`
	Status  string `json:"status,omitempty"`
	Request struct {
		Title       string   `json:"title,omitempty"`
		Summary     string   `json:"summary,omitempty"`
		Constraints []string `json:"constraints,omitempty"`
	} `json:"request,omitempty"`
	Events      []string             `json:"events,omitempty"`
	Timeline    []DispatchEventView  `json:"timeline,omitempty"`
	TaskBinding *TaskHostBindingView `json:"task_binding,omitempty"`
}

type WorkerReadonlyDetailView struct {
	HostID     string `json:"hostId"`
	Mode       string `json:"mode,omitempty"`
	JumpTarget struct {
		Type   string `json:"type,omitempty"`
		HostID string `json:"hostId,omitempty"`
	} `json:"jumpTarget,omitempty"`
	Transcript     []string                        `json:"transcript,omitempty"`
	Conversation   []WorkerConversationExcerptView `json:"conversation,omitempty"`
	Terminal       map[string]any                  `json:"terminal,omitempty"`
	Approval       map[string]any                  `json:"approval,omitempty"`
	ApprovalAnchor *ApprovalTerminalAnchorView     `json:"approval_anchor,omitempty"`
}

type DispatchEventView struct {
	ID           string `json:"id,omitempty"`
	TaskID       string `json:"taskId,omitempty"`
	HostID       string `json:"hostId,omitempty"`
	SessionID    string `json:"sessionId,omitempty"`
	Type         string `json:"type,omitempty"`
	Status       string `json:"status,omitempty"`
	Summary      string `json:"summary,omitempty"`
	Detail       string `json:"detail,omitempty"`
	ApprovalID   string `json:"approvalId,omitempty"`
	SourceCardID string `json:"sourceCardId,omitempty"`
	CreatedAt    string `json:"createdAt,omitempty"`
}

type TaskHostBindingView struct {
	TaskID        string   `json:"taskId,omitempty"`
	HostID        string   `json:"hostId,omitempty"`
	WorkerHostID  string   `json:"workerHostId,omitempty"`
	SessionID     string   `json:"sessionId,omitempty"`
	ThreadID      string   `json:"threadId,omitempty"`
	Title         string   `json:"title,omitempty"`
	Instruction   string   `json:"instruction,omitempty"`
	Constraints   []string `json:"constraints,omitempty"`
	Status        string   `json:"status,omitempty"`
	ApprovalState string   `json:"approvalState,omitempty"`
	LastReply     string   `json:"lastReply,omitempty"`
	LastError     string   `json:"lastError,omitempty"`
	Active        bool     `json:"active,omitempty"`
	QueuePosition int      `json:"queuePosition,omitempty"`
	CreatedAt     string   `json:"createdAt,omitempty"`
	UpdatedAt     string   `json:"updatedAt,omitempty"`
}

type WorkerConversationExcerptView struct {
	ID        string `json:"id,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	Role      string `json:"role,omitempty"`
	Type      string `json:"type,omitempty"`
	Source    string `json:"source,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Text      string `json:"text,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

type ApprovalTerminalAnchorView struct {
	ApprovalID   string `json:"approvalId,omitempty"`
	ItemID       string `json:"itemId,omitempty"`
	SourceCardID string `json:"sourceCardId,omitempty"`
	HostID       string `json:"hostId,omitempty"`
	Type         string `json:"type,omitempty"`
	Title        string `json:"title,omitempty"`
	Command      string `json:"command,omitempty"`
	Cwd          string `json:"cwd,omitempty"`
	Status       string `json:"status,omitempty"`
	Summary      string `json:"summary,omitempty"`
	CreatedAt    string `json:"createdAt,omitempty"`
	UpdatedAt    string `json:"updatedAt,omitempty"`
}

func ProjectMissionCard(m *Mission) MissionCardView {
	if m == nil {
		return MissionCardView{}
	}
	caption := missionCardCaption(m)
	return MissionCardView{
		ID:        "mission:" + m.ID,
		Label:     firstNonEmpty(m.Title, "Mission"),
		Caption:   caption,
		Status:    string(m.Status),
		StepCount: len(m.Tasks),
	}
}

func ProjectWorkerProgress(hostID string, worker *HostWorker) WorkerProgressCardView {
	cardID := workerProgressCardID(hostID)
	if worker == nil {
		return WorkerProgressCardView{
			ID:      cardID,
			HostID:  hostID,
			Label:   firstNonEmpty(hostID, "worker"),
			Caption: "等待 worker 连接",
			Status:  string(WorkerStatusIdle),
			Summary: "worker offline",
		}
	}
	status := strings.TrimSpace(string(worker.Status))
	if status == "" {
		status = string(WorkerStatusIdle)
	}
	return WorkerProgressCardView{
		ID:      cardID,
		HostID:  hostID,
		Label:   firstNonEmpty(hostID, worker.HostID, "worker"),
		Caption: workerProgressCaption(worker),
		Status:  status,
		Summary: workerProgressSummary(worker),
	}
}

func ProjectWorkerApproval(hostID, approvalID string) WorkerApprovalCardView {
	return WorkerApprovalCardView{
		ID:         workerApprovalCardID(hostID, approvalID),
		HostID:     hostID,
		ApprovalID: approvalID,
		Label:      firstNonEmpty(hostID, "worker"),
		Caption:    firstNonEmpty(approvalID, "等待审批"),
		Status:     "pending",
	}
}

func ProjectWorkerCompletion(hostID string, status string) WorkerCompletionCardView {
	return WorkerCompletionCardView{
		ID:      workerCompletionCardID(hostID),
		HostID:  hostID,
		Label:   firstNonEmpty(hostID, "worker"),
		Caption: workerCompletionCaption(status),
		Status:  strings.TrimSpace(status),
	}
}

func ProjectPlanSummary(m *Mission) PlanSummaryView {
	if m == nil {
		return PlanSummaryView{}
	}
	stepCount := len(m.Tasks)
	return PlanSummaryView{
		Label:            firstNonEmpty(m.Title, "主 Agent 已生成计划"),
		Caption:          planSummaryCaption(m, stepCount),
		Tone:             "info",
		Status:           string(m.Status),
		StepCount:        stepCount,
		PlannerSessionID: m.PlannerSessionID,
	}
}

func ProjectPlanDetail(m *Mission) PlanDetailView {
	var view PlanDetailView
	if m == nil {
		return view
	}
	view.Title = "主 Agent 计划详情"
	view.Goal = firstNonEmpty(m.Summary, m.Title)
	view.Version = "plan-v1"
	view.GeneratedAt = m.UpdatedAt
	view.OwnerSessionLabel = "主 Agent 工作台会话（前台投影）"
	view.DAGSummary.Nodes = len(m.Tasks)
	view.DAGSummary.Queued = countTasksWithStatus(m, TaskStatusQueued)
	view.DAGSummary.Running = countTasksWithStatuses(m, TaskStatusReady, TaskStatusDispatching, TaskStatusRunning)
	view.DAGSummary.WaitingApproval = countTasksWithStatus(m, TaskStatusWaitingApproval)
	view.StructuredProcess = make([]string, 0, len(m.Tasks))
	keys := make([]string, 0, len(m.Tasks))
	for id := range m.Tasks {
		keys = append(keys, id)
	}
	slices.Sort(keys)
	for _, id := range keys {
		task := m.Tasks[id]
		if task == nil {
			continue
		}
		view.StructuredProcess = append(view.StructuredProcess, taskDetailLine(task))
	}
	view.RawPlannerTraceRef.SessionID = m.PlannerSessionID
	view.RawPlannerTraceRef.ThreadID = m.PlannerThreadID
	view.DispatchEvents = ProjectMissionDispatchEvents(m, "", 0)
	view.TaskHostBindings = ProjectMissionTaskHostBindings(m)
	return view
}

func ProjectDispatchSummary(result *DispatchResult, m *Mission) DispatchSummaryView {
	if result == nil {
		return DispatchSummaryView{}
	}
	label := "已派发给主机"
	if m != nil && m.Title != "" {
		label = m.Title
	}
	return DispatchSummaryView{
		Label:     label,
		Caption:   fmt.Sprintf("accepted=%d activated=%d queued=%d", result.Accepted, result.Activated, result.Queued),
		Accepted:  result.Accepted,
		Activated: result.Activated,
		Queued:    result.Queued,
	}
}

func ProjectDispatchHostDetail(task *TaskRun, worker *HostWorker) DispatchHostDetailView {
	view := DispatchHostDetailView{}
	if task != nil {
		view.HostID = task.HostID
		view.Host = task.HostID
		view.Status = string(task.Status)
		view.Request.Title = task.Title
		view.Request.Summary = task.Instruction
		view.Request.Constraints = append([]string(nil), task.Constraints...)
	}
	if worker != nil && view.HostID == "" {
		view.HostID = worker.HostID
		view.Host = worker.HostID
		view.Status = string(worker.Status)
	}
	if task != nil {
		binding := ProjectTaskHostBinding(task, worker)
		view.TaskBinding = &binding
	}
	return view
}

func ProjectWorkerReadonlyDetail(worker *HostWorker) WorkerReadonlyDetailView {
	if worker == nil {
		return WorkerReadonlyDetailView{}
	}
	view := WorkerReadonlyDetailView{
		HostID:     worker.HostID,
		Mode:       "readonly",
		Transcript: workerReadonlyTranscript(worker),
		Terminal:   workerReadonlyTerminal(worker),
		Approval:   map[string]any{},
	}
	view.JumpTarget.Type = "single_host_chat"
	view.JumpTarget.HostID = worker.HostID
	return view
}

func ProjectDispatchEvent(event RelayEvent) DispatchEventView {
	return DispatchEventView{
		ID:           strings.TrimSpace(event.ID),
		TaskID:       strings.TrimSpace(event.TaskID),
		HostID:       strings.TrimSpace(event.HostID),
		SessionID:    strings.TrimSpace(event.SessionID),
		Type:         strings.TrimSpace(string(event.Type)),
		Status:       strings.TrimSpace(event.Status),
		Summary:      strings.TrimSpace(event.Summary),
		Detail:       strings.TrimSpace(event.Detail),
		ApprovalID:   strings.TrimSpace(event.ApprovalID),
		SourceCardID: strings.TrimSpace(event.SourceCardID),
		CreatedAt:    strings.TrimSpace(event.CreatedAt),
	}
}

func ProjectTaskHostBinding(task *TaskRun, worker *HostWorker) TaskHostBindingView {
	if task == nil {
		return TaskHostBindingView{}
	}
	view := TaskHostBindingView{
		TaskID:        strings.TrimSpace(task.ID),
		HostID:        strings.TrimSpace(task.HostID),
		WorkerHostID:  strings.TrimSpace(task.WorkerHostID),
		SessionID:     strings.TrimSpace(task.SessionID),
		ThreadID:      strings.TrimSpace(task.ThreadID),
		Title:         strings.TrimSpace(task.Title),
		Instruction:   strings.TrimSpace(task.Instruction),
		Constraints:   compactStrings(task.Constraints),
		Status:        strings.TrimSpace(string(task.Status)),
		ApprovalState: strings.TrimSpace(task.ApprovalState),
		LastReply:     strings.TrimSpace(task.LastReply),
		LastError:     strings.TrimSpace(task.LastError),
		CreatedAt:     strings.TrimSpace(task.CreatedAt),
		UpdatedAt:     strings.TrimSpace(task.UpdatedAt),
	}
	if worker == nil {
		return view
	}
	if view.WorkerHostID == "" {
		view.WorkerHostID = strings.TrimSpace(worker.HostID)
	}
	if view.SessionID == "" {
		view.SessionID = strings.TrimSpace(worker.SessionID)
	}
	if view.ThreadID == "" {
		view.ThreadID = strings.TrimSpace(worker.ThreadID)
	}
	view.Active = strings.TrimSpace(worker.ActiveTaskID) == view.TaskID
	if queueIndex := slices.Index(worker.QueueTaskIDs, view.TaskID); queueIndex >= 0 {
		view.QueuePosition = queueIndex + 1
	}
	return view
}

func ProjectMissionTaskHostBindings(m *Mission) []TaskHostBindingView {
	if m == nil || len(m.Tasks) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m.Tasks))
	for id := range m.Tasks {
		keys = append(keys, id)
	}
	slices.Sort(keys)
	out := make([]TaskHostBindingView, 0, len(keys))
	for _, id := range keys {
		task := m.Tasks[id]
		if task == nil {
			continue
		}
		out = append(out, ProjectTaskHostBinding(task, missionWorkerForTask(m, task)))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func ProjectMissionDispatchEvents(m *Mission, hostID string, limit int) []DispatchEventView {
	if m == nil || len(m.Events) == 0 {
		return nil
	}
	events := append([]RelayEvent(nil), m.Events...)
	sort.SliceStable(events, func(i, j int) bool {
		switch {
		case events[i].CreatedAt > events[j].CreatedAt:
			return false
		case events[i].CreatedAt < events[j].CreatedAt:
			return true
		default:
			return events[i].ID < events[j].ID
		}
	})
	out := make([]DispatchEventView, 0, len(events))
	for _, event := range events {
		if strings.TrimSpace(hostID) != "" && strings.TrimSpace(event.HostID) != strings.TrimSpace(hostID) {
			continue
		}
		out = append(out, ProjectDispatchEvent(event))
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func missionWorkerForTask(m *Mission, task *TaskRun) *HostWorker {
	if m == nil || task == nil {
		return nil
	}
	if worker := m.Workers[strings.TrimSpace(task.HostID)]; worker != nil {
		return worker
	}
	for _, worker := range m.Workers {
		if worker == nil {
			continue
		}
		if strings.TrimSpace(worker.SessionID) == strings.TrimSpace(task.SessionID) {
			return worker
		}
	}
	return nil
}

func countTasksWithStatus(m *Mission, status TaskStatus) int {
	return countTasksWithStatuses(m, status)
}

func countTasksWithStatuses(m *Mission, statuses ...TaskStatus) int {
	if m == nil {
		return 0
	}
	allowed := make(map[TaskStatus]struct{}, len(statuses))
	for _, status := range statuses {
		allowed[status] = struct{}{}
	}
	count := 0
	for _, task := range m.Tasks {
		if task == nil {
			continue
		}
		if _, ok := allowed[task.Status]; ok {
			count++
		}
	}
	return count
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func missionCardCaption(m *Mission) string {
	if m == nil {
		return ""
	}
	base := firstNonEmpty(m.Summary, "前台投影任务")
	count := len(m.Tasks)
	if count > 0 {
		base = fmt.Sprintf("%s · %d 个任务", base, count)
	}
	if status := strings.TrimSpace(string(m.Status)); status != "" && status != string(MissionStatusRunning) {
		base = fmt.Sprintf("%s · %s", base, status)
	}
	return base
}

func workerProgressCardID(hostID string) string {
	return fmt.Sprintf("worker:%s:progress", strings.TrimSpace(hostID))
}

func workerApprovalCardID(hostID, approvalID string) string {
	return fmt.Sprintf("worker:%s:approval:%s", strings.TrimSpace(hostID), strings.TrimSpace(approvalID))
}

func workerCompletionCardID(hostID string) string {
	return fmt.Sprintf("worker:%s:completion", strings.TrimSpace(hostID))
}

func workerProgressCaption(worker *HostWorker) string {
	if worker == nil {
		return "等待 worker 连接"
	}
	activeTask := strings.TrimSpace(worker.ActiveTaskID)
	queue := compactStrings(worker.QueueTaskIDs)
	queueCount := len(queue)
	switch {
	case activeTask != "" && queueCount > 0:
		return fmt.Sprintf("active %s · queue %d", activeTask, queueCount)
	case activeTask != "":
		return fmt.Sprintf("active %s", activeTask)
	case queueCount > 0:
		return fmt.Sprintf("queue %d", queueCount)
	case strings.TrimSpace(worker.IdleSince) != "":
		return fmt.Sprintf("idle since %s", strings.TrimSpace(worker.IdleSince))
	case strings.TrimSpace(string(worker.Status)) != "":
		return strings.TrimSpace(string(worker.Status))
	default:
		return string(WorkerStatusIdle)
	}
}

func workerProgressSummary(worker *HostWorker) string {
	if worker == nil {
		return "worker offline"
	}
	activeTask := strings.TrimSpace(worker.ActiveTaskID)
	queue := compactStrings(worker.QueueTaskIDs)
	switch {
	case activeTask != "" && len(queue) > 0:
		return fmt.Sprintf("active=%s queue=%s", activeTask, strings.Join(queue, ", "))
	case activeTask != "":
		return fmt.Sprintf("active=%s", activeTask)
	case len(queue) > 0:
		return fmt.Sprintf("queue=%s", strings.Join(queue, ", "))
	case strings.TrimSpace(worker.IdleSince) != "":
		return fmt.Sprintf("idleSince=%s", strings.TrimSpace(worker.IdleSince))
	default:
		return strings.TrimSpace(string(worker.Status))
	}
}

func workerCompletionCaption(status string) string {
	switch strings.TrimSpace(status) {
	case "completed", "":
		return "任务已完成"
	case "failed":
		return "任务失败"
	case "cancelled":
		return "任务已取消"
	default:
		return strings.TrimSpace(status)
	}
}

func planSummaryCaption(m *Mission, stepCount int) string {
	if m == nil {
		return ""
	}
	base := firstNonEmpty(m.Summary, "前台工作台只展示摘要，详细过程放到计划详情里。")
	if stepCount > 0 {
		base = fmt.Sprintf("%s · %d 个任务", base, stepCount)
	}
	if status := strings.TrimSpace(string(m.Status)); status != "" && status != string(MissionStatusRunning) {
		base = fmt.Sprintf("%s · %s", base, status)
	}
	return base
}

func taskDetailLine(task *TaskRun) string {
	if task == nil {
		return ""
	}
	head := fmt.Sprintf("%s [%s]", task.ID, task.Status)
	label := firstNonEmpty(task.Title, task.Instruction)
	if label == "" {
		return head
	}
	if host := strings.TrimSpace(task.HostID); host != "" {
		return fmt.Sprintf("%s @%s %s", head, host, label)
	}
	return fmt.Sprintf("%s %s", head, label)
}

func workerReadonlyTranscript(worker *HostWorker) []string {
	if worker == nil {
		return nil
	}
	lines := make([]string, 0, 3)
	if active := strings.TrimSpace(worker.ActiveTaskID); active != "" {
		lines = append(lines, "active task: "+active)
	}
	if queue := compactStrings(worker.QueueTaskIDs); len(queue) > 0 {
		lines = append(lines, "queued tasks: "+strings.Join(queue, ", "))
	}
	if idleSince := strings.TrimSpace(worker.IdleSince); idleSince != "" {
		lines = append(lines, "idle since: "+idleSince)
	}
	return lines
}

func workerReadonlyTerminal(worker *HostWorker) map[string]any {
	if worker == nil {
		return map[string]any{}
	}
	return map[string]any{
		"status":       strings.TrimSpace(string(worker.Status)),
		"activeTaskId": strings.TrimSpace(worker.ActiveTaskID),
		"queueTaskIds": compactStrings(worker.QueueTaskIDs),
		"lastSeenAt":   strings.TrimSpace(worker.LastSeenAt),
		"idleSince":    strings.TrimSpace(worker.IdleSince),
		"updatedAt":    strings.TrimSpace(worker.UpdatedAt),
		"workspaceId":  strings.TrimSpace(worker.WorkspaceID),
		"missionId":    strings.TrimSpace(worker.MissionID),
	}
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
