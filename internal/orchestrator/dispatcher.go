package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

type DispatchTaskRequest struct {
	TaskID         string   `json:"taskId"`
	HostID         string   `json:"hostId"`
	Title          string   `json:"title,omitempty"`
	Instruction    string   `json:"instruction,omitempty"`
	Constraints    []string `json:"constraints,omitempty"`
	ExternalNodeID string   `json:"externalNodeId,omitempty"`
}

type DispatchRequest struct {
	MissionID    string                `json:"missionId"`
	MissionTitle string                `json:"missionTitle,omitempty"`
	Summary      string                `json:"summary,omitempty"`
	Tasks        []DispatchTaskRequest `json:"tasks,omitempty"`
}

type DispatchWorkerResult struct {
	HostID    string `json:"hostId"`
	SessionID string `json:"sessionId,omitempty"`
	Status    string `json:"status"`
}

type DispatchResult struct {
	MissionID string                 `json:"missionId"`
	Accepted  int                    `json:"accepted"`
	Queued    int                    `json:"queued"`
	Activated int                    `json:"activated"`
	Workers   []DispatchWorkerResult `json:"workers,omitempty"`
}

type TaskCancelResult struct {
	MissionID          string        `json:"missionId"`
	WorkspaceSessionID string        `json:"workspaceSessionId,omitempty"`
	TaskID             string        `json:"taskId"`
	HostID             string        `json:"hostId,omitempty"`
	SessionID          string        `json:"sessionId,omitempty"`
	WasActive          bool          `json:"wasActive"`
	MissionStatus      MissionStatus `json:"missionStatus,omitempty"`
}

type IdleThreadResetResult struct {
	MissionID    string `json:"missionId,omitempty"`
	WorkerHostID string `json:"workerHostId,omitempty"`
	SessionID    string `json:"sessionId,omitempty"`
	ThreadID     string `json:"threadId,omitempty"`
	WasReset     bool   `json:"wasReset,omitempty"`
}

type Dispatcher struct {
	store *Store
	clock func() time.Time
}

func newDispatcher(store *Store) *Dispatcher {
	return &Dispatcher{
		store: store,
		clock: time.Now,
	}
}

func (d *Dispatcher) Dispatch(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	_ = ctx
	if d == nil || d.store == nil {
		return nil, errors.New("dispatcher is not ready")
	}
	req.MissionID = strings.TrimSpace(req.MissionID)
	if req.MissionID == "" {
		return nil, errors.New("mission id is required")
	}
	if err := validateDispatchTasks(req.Tasks); err != nil {
		return nil, err
	}
	if len(req.Tasks) == 0 {
		return &DispatchResult{MissionID: req.MissionID}, nil
	}

	sort.SliceStable(req.Tasks, func(i, j int) bool {
		if req.Tasks[i].HostID == req.Tasks[j].HostID {
			return req.Tasks[i].TaskID < req.Tasks[j].TaskID
		}
		return req.Tasks[i].HostID < req.Tasks[j].HostID
	})

	result := &DispatchResult{
		MissionID: req.MissionID,
		Workers:   make([]DispatchWorkerResult, 0),
	}
	type pendingSessionMeta struct {
		sessionID string
		meta      SessionMeta
	}
	sessionMetas := make([]pendingSessionMeta, 0)

	_, err := d.store.UpdateMission(req.MissionID, func(m *Mission) error {
		if m.Title == "" {
			m.Title = strings.TrimSpace(req.MissionTitle)
		}
		if strings.TrimSpace(m.Title) == "" {
			return errors.New("mission title is required")
		}
		if m.Summary == "" {
			m.Summary = strings.TrimSpace(req.Summary)
		}
		if strings.TrimSpace(m.Summary) == "" {
			return errors.New("mission summary is required")
		}
		if m.Status == MissionStatusPending {
			m.Status = MissionStatusRunning
		}
		now := nowString()
		m.UpdatedAt = now

		activeBudget := m.MissionActiveBudget
		if activeBudget <= 0 {
			activeBudget = DefaultMissionActiveBudget
		}
		activeWorkers := countActiveWorkers(m)

		for _, taskReq := range req.Tasks {
			taskReq.HostID = strings.TrimSpace(taskReq.HostID)
			if taskReq.HostID == "" || taskReq.TaskID == "" {
				continue
			}

			task := upsertTask(m, taskReq, req.MissionID)
			worker := ensureWorker(m, req.MissionID, taskReq.HostID)
			queuedBefore := slices.Contains(worker.QueueTaskIDs, task.ID)
			if worker.Status == "" {
				worker.Status = WorkerStatusQueued
			}
			if worker.SessionID == "" {
				worker.SessionID = newSessionID("worker")
			}
			if worker.ThreadID == "" {
				worker.ThreadID = newSessionID("thread")
			}
			if worker.WorkspaceID == "" {
				worker.WorkspaceID = newWorkspaceID("worker", req.MissionID, taskReq.HostID)
			}
			if task.SessionID == "" {
				task.SessionID = worker.SessionID
			}
			if task.ThreadID == "" {
				task.ThreadID = worker.ThreadID
			}
			if task.WorkerHostID == "" {
				task.WorkerHostID = taskReq.HostID
			}
			if task.Status == "" {
				task.Status = TaskStatusReady
			}

			workerAvailable := worker.ActiveTaskID == "" &&
				worker.Status != WorkerStatusRunning &&
				worker.Status != WorkerStatusDispatching &&
				worker.Status != WorkerStatusWaiting
			shouldActivate := activeWorkers < activeBudget && workerAvailable
			if shouldActivate {
				task.Status = TaskStatusReady
				task.UpdatedAt = now
				worker.Status = WorkerStatusDispatching
				worker.ActiveTaskID = task.ID
				worker.LastSeenAt = now
				worker.UpdatedAt = now
				activeWorkers++
				result.Activated++
			} else {
				if task.ID != worker.ActiveTaskID && !queuedBefore {
					worker.QueueTaskIDs = append(worker.QueueTaskIDs, task.ID)
				}
				if worker.ActiveTaskID != "" {
					activeTask := m.Tasks[worker.ActiveTaskID]
					if activeTask != nil {
						worker.Status = workerStatusForActiveTask(activeTask.Status)
					} else {
						worker.Status = WorkerStatusDispatching
					}
				} else {
					worker.Status = WorkerStatusQueued
				}
				worker.UpdatedAt = now
				task.Status = TaskStatusQueued
				task.UpdatedAt = now
				if !queuedBefore {
					result.Queued++
				}
			}
			result.Accepted++

			meta := SessionMeta{
				Kind:               SessionKindWorker,
				Visible:            false,
				MissionID:          req.MissionID,
				WorkspaceSessionID: m.WorkspaceSessionID,
				WorkerHostID:       taskReq.HostID,
				RuntimePreset:      RuntimePresetWorkerInternal,
				WorkerThreadID:     worker.ThreadID,
				CreatedAt:          now,
				UpdatedAt:          now,
			}
			sessionMetas = append(sessionMetas, pendingSessionMeta{
				sessionID: worker.SessionID,
				meta:      meta,
			})

			m.Workers[taskReq.HostID] = worker
			m.Tasks[task.ID] = task
			m.Workspaces[worker.WorkspaceID] = &WorkspaceLease{
				ID:         worker.WorkspaceID,
				MissionID:  req.MissionID,
				SessionID:  worker.SessionID,
				HostID:     taskReq.HostID,
				Kind:       LeaseKindWorker,
				LocalPath:  WorkerLocalWorkspacePath("", req.MissionID, taskReq.HostID),
				RemotePath: WorkerRemoteWorkspacePath("", req.MissionID, taskReq.HostID),
				Status:     "prepared",
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			result.Workers = append(result.Workers, DispatchWorkerResult{
				HostID:    taskReq.HostID,
				SessionID: worker.SessionID,
				Status:    string(worker.Status),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, item := range sessionMetas {
		d.store.UpsertSessionMeta(item.sessionID, item.meta)
	}
	return result, nil
}

func validateDispatchTasks(tasks []DispatchTaskRequest) error {
	if len(tasks) == 0 {
		return nil
	}
	seenTaskIDs := make(map[string]struct{}, len(tasks))
	for i, taskReq := range tasks {
		taskID := strings.TrimSpace(taskReq.TaskID)
		if taskID == "" {
			return fmt.Errorf("dispatch task %d: task id is required", i+1)
		}
		if _, ok := seenTaskIDs[taskID]; ok {
			return fmt.Errorf("dispatch task %q: duplicate task id", taskID)
		}
		seenTaskIDs[taskID] = struct{}{}

		hostID := strings.TrimSpace(taskReq.HostID)
		if hostID == "" {
			return fmt.Errorf("dispatch task %q: host id is required", taskID)
		}
		if strings.TrimSpace(taskReq.Instruction) == "" && strings.TrimSpace(taskReq.Title) == "" {
			return fmt.Errorf("dispatch task %q: instruction is required", taskID)
		}
	}
	return nil
}

func (d *Dispatcher) CancelMission(missionID string) error {
	_, err := d.store.UpdateMission(strings.TrimSpace(missionID), func(m *Mission) error {
		if m.Status == MissionStatusCancelled {
			return nil
		}
		now := nowString()
		m.Status = MissionStatusCancelled
		m.UpdatedAt = now
		for _, task := range m.Tasks {
			if task == nil {
				continue
			}
			if task.Status == TaskStatusCompleted {
				continue
			}
			task.Status = TaskStatusCancelled
			task.LastError = "mission cancelled"
			task.UpdatedAt = now
		}
		for _, worker := range m.Workers {
			if worker == nil {
				continue
			}
			if worker.Status == WorkerStatusCompleted {
				continue
			}
			worker.ActiveTaskID = ""
			worker.QueueTaskIDs = nil
			worker.Status = WorkerStatusCancelled
			worker.LastSeenAt = now
			worker.UpdatedAt = now
		}
		m.Events = append(m.Events, RelayEvent{
			ID:        newEventID("cancelled"),
			MissionID: m.ID,
			Type:      EventTypeCancelled,
			Status:    string(MissionStatusCancelled),
			Summary:   "mission cancelled",
			CreatedAt: now,
		})
		if len(m.Events) > DefaultEventWindowSize {
			m.Events = append([]RelayEvent(nil), m.Events[len(m.Events)-DefaultEventWindowSize:]...)
		}
		return nil
	})
	return err
}

func (d *Dispatcher) CancelByWorkspaceSession(workspaceSessionID string) error {
	missionID, ok := d.store.MissionIDByWorkspaceSession(workspaceSessionID)
	if !ok {
		return fmt.Errorf("workspace session %q is not linked to a mission", workspaceSessionID)
	}
	return d.CancelMission(missionID)
}

func (d *Dispatcher) CancelTask(missionID, taskID string) (*TaskCancelResult, error) {
	missionID = strings.TrimSpace(missionID)
	taskID = strings.TrimSpace(taskID)
	if missionID == "" {
		return nil, errors.New("mission id is required")
	}
	if taskID == "" {
		return nil, errors.New("task id is required")
	}
	result := &TaskCancelResult{
		MissionID: missionID,
		TaskID:    taskID,
	}
	_, err := d.store.UpdateMission(missionID, func(m *Mission) error {
		task := m.Tasks[taskID]
		if task == nil {
			return fmt.Errorf("task %q not found", taskID)
		}
		result.WorkspaceSessionID = m.WorkspaceSessionID
		result.HostID = task.HostID
		result.SessionID = task.SessionID
		result.MissionStatus = m.Status
		if isTaskTerminal(task.Status) {
			return nil
		}
		now := nowString()
		task.Status = TaskStatusCancelled
		task.LastError = "task cancelled"
		task.UpdatedAt = now

		worker := m.Workers[task.HostID]
		if worker != nil {
			if worker.ActiveTaskID == taskID {
				result.WasActive = true
				worker.ActiveTaskID = ""
			}
			worker.QueueTaskIDs = removeTaskID(worker.QueueTaskIDs, taskID)
			switch {
			case worker.ActiveTaskID != "":
				// keep current runtime status
			case len(worker.QueueTaskIDs) > 0:
				worker.Status = WorkerStatusQueued
			default:
				worker.Status = WorkerStatusCancelled
			}
			worker.LastSeenAt = now
			worker.UpdatedAt = now
		}

		m.UpdatedAt = now
		m.Events = append(m.Events, RelayEvent{
			ID:        newEventID("cancelled"),
			MissionID: m.ID,
			TaskID:    taskID,
			HostID:    task.HostID,
			SessionID: task.SessionID,
			Type:      EventTypeCancelled,
			Status:    string(TaskStatusCancelled),
			Summary:   fmt.Sprintf("task %s cancelled", taskID),
			CreatedAt: now,
		})
		if len(m.Events) > DefaultEventWindowSize {
			m.Events = append([]RelayEvent(nil), m.Events[len(m.Events)-DefaultEventWindowSize:]...)
		}
		if allMissionTasksTerminal(m) && m.Status == MissionStatusRunning {
			m.Status = summarizeMissionTerminalStatus(m)
		}
		result.MissionStatus = m.Status
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (d *Dispatcher) OnSnapshot(snapshot Snapshot) error {
	missionID := strings.TrimSpace(snapshot.MissionID)
	if missionID == "" {
		missionID, _ = d.store.MissionIDBySession(snapshot.SessionID)
	}
	if missionID == "" {
		return nil
	}
	_, err := d.store.UpdateMission(missionID, func(m *Mission) error {
		event := RelayEvent{
			ID:        newEventID("snapshot"),
			MissionID: missionID,
			SessionID: snapshot.SessionID,
			Type:      EventTypeSnapshot,
			Status:    snapshot.Status,
			Summary:   snapshot.Summary,
			Detail:    snapshot.Detail,
			CreatedAt: nowString(),
		}
		m.Events = append(m.Events, event)
		if len(m.Events) > DefaultEventWindowSize {
			m.Events = append([]RelayEvent(nil), m.Events[len(m.Events)-DefaultEventWindowSize:]...)
		}
		m.UpdatedAt = event.CreatedAt
		return nil
	})
	return err
}

func (d *Dispatcher) ResetIdleWorkerThread(sessionID string, idleFor time.Duration) (*IdleThreadResetResult, error) {
	if d == nil || d.store == nil {
		return nil, errors.New("dispatcher is not ready")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	missionID, ok := d.store.MissionIDByWorkerSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("worker session %q is not linked to a mission", sessionID)
	}
	if idleFor < 0 {
		idleFor = 0
	}
	result := &IdleThreadResetResult{MissionID: missionID, SessionID: sessionID}
	_, err := d.store.UpdateMission(missionID, func(m *Mission) error {
		worker := findWorkerBySession(m, sessionID)
		if worker == nil {
			return fmt.Errorf("worker session %q not found in mission %q", sessionID, missionID)
		}
		result.WorkerHostID = worker.HostID
		result.ThreadID = worker.ThreadID
		if worker.ThreadID == "" {
			return nil
		}
		if worker.ActiveTaskID != "" || len(worker.QueueTaskIDs) > 0 {
			return nil
		}
		now := time.Now().UTC()
		if d.clock != nil {
			now = d.clock().UTC()
		}
		if idleFor > 0 {
			if worker.IdleSince == "" {
				return nil
			}
			idleAt, err := time.Parse(time.RFC3339Nano, worker.IdleSince)
			if err != nil {
				return fmt.Errorf("worker %q idleSince is invalid: %w", sessionID, err)
			}
			if now.Sub(idleAt.UTC()) < idleFor {
				return nil
			}
		}
		worker.ThreadID = ""
		worker.Status = WorkerStatusIdle
		worker.UpdatedAt = now.Format(time.RFC3339Nano)
		result.WasReset = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func countActiveWorkers(m *Mission) int {
	if m == nil {
		return 0
	}
	count := 0
	for _, worker := range m.Workers {
		if worker == nil {
			continue
		}
		switch worker.Status {
		case WorkerStatusRunning, WorkerStatusDispatching, WorkerStatusWaiting:
			count++
		}
	}
	return count
}

func upsertTask(m *Mission, req DispatchTaskRequest, missionID string) *TaskRun {
	if m.Tasks == nil {
		m.Tasks = make(map[string]*TaskRun)
	}
	taskID := strings.TrimSpace(req.TaskID)
	task, ok := m.Tasks[taskID]
	if !ok || task == nil {
		task = &TaskRun{
			ID:        taskID,
			MissionID: missionID,
			Status:    TaskStatusQueued,
			CreatedAt: nowString(),
		}
		m.Tasks[taskID] = task
	}
	task.HostID = strings.TrimSpace(req.HostID)
	task.WorkerHostID = strings.TrimSpace(req.HostID)
	task.Title = strings.TrimSpace(req.Title)
	task.Instruction = strings.TrimSpace(req.Instruction)
	if task.Instruction == "" {
		task.Instruction = task.Title
	}
	task.Constraints = append([]string(nil), req.Constraints...)
	task.ExternalNodeID = req.ExternalNodeID
	task.UpdatedAt = nowString()
	if task.Status == "" {
		task.Status = TaskStatusQueued
	}
	return task
}

func ensureWorker(m *Mission, missionID, hostID string) *HostWorker {
	if m.Workers == nil {
		m.Workers = make(map[string]*HostWorker)
	}
	worker, ok := m.Workers[hostID]
	if !ok || worker == nil {
		worker = &HostWorker{
			MissionID:    missionID,
			HostID:       hostID,
			Status:       WorkerStatusQueued,
			QueueTaskIDs: make([]string, 0),
			LastSeenAt:   nowString(),
		}
		m.Workers[hostID] = worker
		return worker
	}
	if worker.QueueTaskIDs == nil {
		worker.QueueTaskIDs = make([]string, 0)
	}
	worker.LastSeenAt = nowString()
	worker.UpdatedAt = worker.LastSeenAt
	return worker
}

func findWorkerBySession(m *Mission, sessionID string) *HostWorker {
	if m == nil {
		return nil
	}
	for _, worker := range m.Workers {
		if worker != nil && worker.SessionID == sessionID {
			return worker
		}
	}
	return nil
}

func removeTaskID(items []string, taskID string) []string {
	if len(items) == 0 || taskID == "" {
		return items
	}
	out := items[:0]
	for _, item := range items {
		if item == taskID {
			continue
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
