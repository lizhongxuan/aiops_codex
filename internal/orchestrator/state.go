package orchestrator

import "time"

type SessionKind string

const (
	SessionKindSingleHost SessionKind = "single_host"
	SessionKindWorkspace  SessionKind = "workspace"
	SessionKindPlanner    SessionKind = "planner"
	SessionKindWorker     SessionKind = "worker"
)

type RuntimePreset string

const (
	RuntimePresetSingleHostDefault RuntimePreset = "single_host_default"
	RuntimePresetWorkspaceFront    RuntimePreset = "workspace_front"
	RuntimePresetPlannerInternal   RuntimePreset = "planner_internal"
	RuntimePresetWorkerInternal    RuntimePreset = "worker_internal"
)

type MissionStatus string

const (
	MissionStatusPending   MissionStatus = "pending"
	MissionStatusRunning   MissionStatus = "running"
	MissionStatusPaused    MissionStatus = "paused"
	MissionStatusCompleted MissionStatus = "completed"
	MissionStatusFailed    MissionStatus = "failed"
	MissionStatusCancelled MissionStatus = "cancelled"
)

type TaskStatus string

const (
	TaskStatusQueued          TaskStatus = "queued"
	TaskStatusReady           TaskStatus = "ready"
	TaskStatusDispatching     TaskStatus = "dispatching"
	TaskStatusRunning         TaskStatus = "running"
	TaskStatusWaitingApproval TaskStatus = "waiting_approval"
	TaskStatusWaitingInput    TaskStatus = "waiting_input"
	TaskStatusCompleted       TaskStatus = "completed"
	TaskStatusFailed          TaskStatus = "failed"
	TaskStatusCancelled       TaskStatus = "cancelled"
)

type WorkerStatus string

const (
	WorkerStatusIdle        WorkerStatus = "idle"
	WorkerStatusQueued      WorkerStatus = "queued"
	WorkerStatusDispatching WorkerStatus = "dispatching"
	WorkerStatusRunning     WorkerStatus = "running"
	WorkerStatusWaiting     WorkerStatus = "waiting"
	WorkerStatusCompleted   WorkerStatus = "completed"
	WorkerStatusFailed      WorkerStatus = "failed"
	WorkerStatusCancelled   WorkerStatus = "cancelled"
)

type LeaseKind string

const (
	LeaseKindPlanner LeaseKind = "planner"
	LeaseKindWorker  LeaseKind = "worker"
)

type EventType string

const (
	EventTypeDispatch          EventType = "dispatch"
	EventTypeProgress          EventType = "progress"
	EventTypeApprovalRequested EventType = "approval_requested"
	EventTypeApprovalResolved  EventType = "approval_resolved"
	EventTypeChoiceRequested   EventType = "choice_requested"
	EventTypeChoiceResolved    EventType = "choice_resolved"
	EventTypeExecStarted       EventType = "exec_started"
	EventTypeExecFinished      EventType = "exec_finished"
	EventTypeReply             EventType = "reply"
	EventTypeCompleted         EventType = "completed"
	EventTypeFailed            EventType = "failed"
	EventTypeCancelled         EventType = "cancelled"
	EventTypeSnapshot          EventType = "snapshot"
)

type SessionMeta struct {
	Kind               SessionKind   `json:"kind"`
	Visible            bool          `json:"visible"`
	MissionID          string        `json:"missionId,omitempty"`
	WorkspaceSessionID string        `json:"workspaceSessionId,omitempty"`
	WorkerHostID       string        `json:"workerHostId,omitempty"`
	RuntimePreset      RuntimePreset `json:"runtimePreset,omitempty"`
	PlannerThreadID    string        `json:"plannerThreadId,omitempty"`
	WorkerThreadID     string        `json:"workerThreadId,omitempty"`
	CreatedAt          string        `json:"createdAt,omitempty"`
	UpdatedAt          string        `json:"updatedAt,omitempty"`
}

type Mission struct {
	ID                  string                     `json:"id"`
	WorkspaceSessionID  string                     `json:"workspaceSessionId,omitempty"`
	PlannerSessionID    string                     `json:"plannerSessionId,omitempty"`
	PlannerThreadID     string                     `json:"plannerThreadId,omitempty"`
	Title               string                     `json:"title,omitempty"`
	Summary             string                     `json:"summary,omitempty"`
	Status              MissionStatus              `json:"status"`
	ProjectionMode      string                     `json:"projectionMode,omitempty"`
	CreatedAt           string                     `json:"createdAt,omitempty"`
	UpdatedAt           string                     `json:"updatedAt,omitempty"`
	Workers             map[string]*HostWorker     `json:"workers,omitempty"`
	Tasks               map[string]*TaskRun        `json:"tasks,omitempty"`
	Workspaces          map[string]*WorkspaceLease `json:"workspaces,omitempty"`
	Events              []RelayEvent               `json:"events,omitempty"`
	GlobalActiveBudget  int                        `json:"globalActiveBudget,omitempty"`
	MissionActiveBudget int                        `json:"missionActiveBudget,omitempty"`
}

type TaskRun struct {
	ID             string     `json:"id"`
	MissionID      string     `json:"missionId,omitempty"`
	HostID         string     `json:"hostId,omitempty"`
	WorkerHostID   string     `json:"workerHostId,omitempty"`
	SessionID      string     `json:"sessionId,omitempty"`
	ThreadID       string     `json:"threadId,omitempty"`
	Title          string     `json:"title,omitempty"`
	Instruction    string     `json:"instruction,omitempty"`
	Constraints    []string   `json:"constraints,omitempty"`
	Status         TaskStatus `json:"status"`
	ExternalNodeID string     `json:"externalNodeId,omitempty"`
	Attempt        int        `json:"attempt,omitempty"`
	CreatedAt      string     `json:"createdAt,omitempty"`
	UpdatedAt      string     `json:"updatedAt,omitempty"`
	LastError      string     `json:"lastError,omitempty"`
	LastReply      string     `json:"lastReply,omitempty"`
	ApprovalState  string     `json:"approvalState,omitempty"`
}

type HostWorker struct {
	MissionID    string       `json:"missionId,omitempty"`
	HostID       string       `json:"hostId,omitempty"`
	SessionID    string       `json:"sessionId,omitempty"`
	ThreadID     string       `json:"threadId,omitempty"`
	WorkspaceID  string       `json:"workspaceId,omitempty"`
	ActiveTaskID string       `json:"activeTaskId,omitempty"`
	QueueTaskIDs []string     `json:"queueTaskIds,omitempty"`
	Status       WorkerStatus `json:"status"`
	LastSeenAt   string       `json:"lastSeenAt,omitempty"`
	IdleSince    string       `json:"idleSince,omitempty"`
	UpdatedAt    string       `json:"updatedAt,omitempty"`
}

type WorkspaceLease struct {
	ID         string    `json:"id"`
	MissionID  string    `json:"missionId,omitempty"`
	SessionID  string    `json:"sessionId,omitempty"`
	HostID     string    `json:"hostId,omitempty"`
	Kind       LeaseKind `json:"kind"`
	LocalPath  string    `json:"localPath,omitempty"`
	RemotePath string    `json:"remotePath,omitempty"`
	Status     string    `json:"status,omitempty"`
	CreatedAt  string    `json:"createdAt,omitempty"`
	UpdatedAt  string    `json:"updatedAt,omitempty"`
}

type RelayEvent struct {
	ID           string    `json:"id"`
	MissionID    string    `json:"missionId,omitempty"`
	TaskID       string    `json:"taskId,omitempty"`
	HostID       string    `json:"hostId,omitempty"`
	SessionID    string    `json:"sessionId,omitempty"`
	Type         EventType `json:"type"`
	Status       string    `json:"status,omitempty"`
	Summary      string    `json:"summary,omitempty"`
	Detail       string    `json:"detail,omitempty"`
	ApprovalID   string    `json:"approvalId,omitempty"`
	SourceCardID string    `json:"sourceCardId,omitempty"`
	CreatedAt    string    `json:"createdAt,omitempty"`
}

type WorkerSeenState struct {
	LastTurnPhase      string            `json:"lastTurnPhase,omitempty"`
	LastReplyDigest    string            `json:"lastReplyDigest,omitempty"`
	SeenCardIDs        map[string]string `json:"seenCardIds,omitempty"`
	SeenApprovalStatus map[string]string `json:"seenApprovalStatus,omitempty"`
	SeenChoiceStatus   map[string]string `json:"seenChoiceStatus,omitempty"`
	SeenExecStatus     map[string]string `json:"seenExecStatus,omitempty"`
	LastReplyCardID    string            `json:"lastReplyCardId,omitempty"`
}

type Snapshot struct {
	SessionID   string       `json:"sessionId"`
	Kind        SessionKind  `json:"kind,omitempty"`
	Visible     bool         `json:"visible,omitempty"`
	MissionID   string       `json:"missionId,omitempty"`
	Status      string       `json:"status,omitempty"`
	Summary     string       `json:"summary,omitempty"`
	Detail      string       `json:"detail,omitempty"`
	UpdatedAt   time.Time    `json:"updatedAt,omitempty"`
	RelayEvents []RelayEvent `json:"relayEvents,omitempty"`
}

type ApprovalRoute struct {
	MissionID       string `json:"missionId,omitempty"`
	WorkerSessionID string `json:"workerSessionId,omitempty"`
	WorkerHostID    string `json:"workerHostId,omitempty"`
	TaskID          string `json:"taskId,omitempty"`
	ApprovalID      string `json:"approvalId,omitempty"`
	OK              bool   `json:"ok,omitempty"`
}

type ChoiceRoute struct {
	MissionID string `json:"missionId,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	ChoiceID  string `json:"choiceId,omitempty"`
	OK        bool   `json:"ok,omitempty"`
}

func defaultMission() *Mission {
	now := nowString()
	return &Mission{
		Status:              MissionStatusPending,
		ProjectionMode:      "front_projection",
		CreatedAt:           now,
		UpdatedAt:           now,
		Workers:             make(map[string]*HostWorker),
		Tasks:               make(map[string]*TaskRun),
		Workspaces:          make(map[string]*WorkspaceLease),
		Events:              make([]RelayEvent, 0),
		GlobalActiveBudget:  DefaultGlobalActiveBudget,
		MissionActiveBudget: DefaultMissionActiveBudget,
	}
}

const (
	DefaultGlobalActiveBudget  = 32
	DefaultMissionActiveBudget = 8
	DefaultEventWindowSize     = 128
)
