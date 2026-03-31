package model

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

const (
	ServerLocalHostID = "server-local"
)

type Host struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Kind            string            `json:"kind"`
	Address         string            `json:"address,omitempty"`
	Transport       string            `json:"transport,omitempty"`
	Status          string            `json:"status"`
	Executable      bool              `json:"executable"`
	TerminalCapable bool              `json:"terminalCapable,omitempty"`
	OS              string            `json:"os,omitempty"`
	Arch            string            `json:"arch,omitempty"`
	AgentVersion    string            `json:"agentVersion,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	LastHeartbeat   string            `json:"lastHeartbeat,omitempty"`
	LastError       string            `json:"lastError,omitempty"`
	ProfileHash     string            `json:"profileHash,omitempty"`
	ProfileStatus   string            `json:"profileStatus,omitempty"`
	ProfileLoadedAt string            `json:"profileLoadedAt,omitempty"`
	ProfileVersion  int               `json:"profileVersion,omitempty"`
	ProfileSummary  string            `json:"profileSummary,omitempty"`
	EnabledSkills   []string          `json:"enabledSkills,omitempty"`
	EnabledMCPs     []string          `json:"enabledMCPs,omitempty"`
	SSHUser         string            `json:"sshUser,omitempty"`
	SSHPort         int               `json:"sshPort,omitempty"`
	InstallState    string            `json:"installState,omitempty"`
	ControlMode     string            `json:"controlMode,omitempty"`
}

type SessionMessageExcerpt struct {
	Role      string `json:"role"`
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt,omitempty"`
}

type HostSessionSummary struct {
	SessionID      string                  `json:"sessionId"`
	Title          string                  `json:"title"`
	Status         string                  `json:"status"`
	LastActivityAt string                  `json:"lastActivityAt"`
	MessageCount   int                     `json:"messageCount"`
	TaskSummary    string                  `json:"taskSummary,omitempty"`
	ReplySummary   string                  `json:"replySummary,omitempty"`
	Messages       []SessionMessageExcerpt `json:"messages,omitempty"`
}

type AgentProfileType string

const (
	AgentProfileTypeMainAgent         AgentProfileType = "main-agent"
	AgentProfileTypeHostAgentDefault  AgentProfileType = "host-agent-default"
	AgentProfileTypeHostAgentOverride AgentProfileType = "host-agent-override"
)

const (
	AgentPermissionModeAllow            = "allow"
	AgentPermissionModeApprovalRequired = "approval_required"
	AgentPermissionModeReadonlyOnly     = "readonly_only"
	AgentPermissionModeDeny             = "deny"
)

const (
	AgentCapabilityEnabled          = "enabled"
	AgentCapabilityApprovalRequired = "approval_required"
	AgentCapabilityDisabled         = "disabled"
	AgentProfileUpdatedBySystem     = "system"
	AgentProfileConfigVersion       = 1
	AgentSkillActivationDefault     = "default_enabled"
	AgentSkillActivationExplicit    = "explicit_only"
	AgentSkillActivationDisabled    = "disabled"
	AgentMCPPermissionReadonly      = "readonly"
	AgentMCPPermissionReadwrite     = "readwrite"
	SessionKindSingleHost           = "single_host"
	SessionKindWorkspace            = "workspace"
	SessionKindPlanner              = "planner"
	SessionKindWorker               = "worker"
	SessionRuntimePresetSingleHost  = "single_host_default"
	SessionRuntimePresetWorkspace   = "workspace_front"
	SessionRuntimePresetPlanner     = "planner_internal"
	SessionRuntimePresetWorker      = "worker_internal"
)

type AgentProfile struct {
	ID                    string                     `json:"id"`
	Name                  string                     `json:"name"`
	Type                  string                     `json:"type"`
	Description           string                     `json:"description,omitempty"`
	Runtime               AgentRuntimeSettings       `json:"runtime"`
	SystemPrompt          AgentSystemPrompt          `json:"systemPrompt"`
	CommandPermissions    AgentCommandPermissions    `json:"commandPermissions"`
	CapabilityPermissions AgentCapabilityPermissions `json:"capabilityPermissions"`
	Skills                []AgentSkill               `json:"skills,omitempty"`
	MCPs                  []AgentMCP                 `json:"mcps,omitempty"`
	UpdatedAt             string                     `json:"updatedAt"`
	UpdatedBy             string                     `json:"updatedBy,omitempty"`
}

type AgentRuntimeSettings struct {
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	ApprovalPolicy  string `json:"approvalPolicy,omitempty"`
	SandboxMode     string `json:"sandboxMode,omitempty"`
}

type AgentSystemPrompt struct {
	Content string `json:"content"`
	Preview string `json:"preview,omitempty"`
	Version string `json:"version,omitempty"`
	Notes   string `json:"notes,omitempty"`
}

type AgentCommandPermissions struct {
	Enabled               *bool             `json:"enabled,omitempty"`
	DefaultMode           string            `json:"defaultMode"`
	AllowShellWrapper     *bool             `json:"allowShellWrapper,omitempty"`
	AllowSudo             *bool             `json:"allowSudo,omitempty"`
	DefaultTimeoutSeconds int               `json:"defaultTimeoutSeconds"`
	AllowedWritableRoots  []string          `json:"allowedWritableRoots,omitempty"`
	CategoryPolicies      map[string]string `json:"categoryPolicies,omitempty"`
}

type AgentCapabilityPermissions struct {
	CommandExecution string `json:"commandExecution"`
	FileRead         string `json:"fileRead"`
	FileSearch       string `json:"fileSearch"`
	FileChange       string `json:"fileChange"`
	Terminal         string `json:"terminal"`
	WebSearch        string `json:"webSearch"`
	WebOpen          string `json:"webOpen"`
	Approval         string `json:"approval"`
	MultiAgent       string `json:"multiAgent"`
	Plan             string `json:"plan"`
	Summary          string `json:"summary"`
}

type AgentSkill struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	Source         string `json:"source,omitempty"`
	Enabled        bool   `json:"enabled"`
	ActivationMode string `json:"activationMode,omitempty"`
}

type AgentMCP struct {
	ID                           string `json:"id"`
	Name                         string `json:"name"`
	Type                         string `json:"type,omitempty"`
	Source                       string `json:"source,omitempty"`
	Enabled                      bool   `json:"enabled"`
	Permission                   string `json:"permission,omitempty"`
	RequiresExplicitUserApproval bool   `json:"requiresExplicitUserApproval,omitempty"`
}

type AuthState struct {
	Connected      bool   `json:"connected"`
	Pending        bool   `json:"pending"`
	Mode           string `json:"mode,omitempty"`
	PlanType       string `json:"planType,omitempty"`
	Email          string `json:"email,omitempty"`
	PendingLoginID string `json:"pendingLoginId,omitempty"`
	LastError      string `json:"lastError,omitempty"`
}

type ExternalAuthTokens struct {
	IDToken          string `json:"-"`
	AccessToken      string `json:"-"`
	ChatGPTAccountID string `json:"-"`
	ChatGPTPlanType  string `json:"-"`
	Email            string `json:"email,omitempty"`
}

type PlanItem struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

type FileChange struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Diff string `json:"diff"`
}

type ApprovalRef struct {
	RequestID string   `json:"requestId"`
	Type      string   `json:"type"`
	Decisions []string `json:"decisions,omitempty"`
}

type ChoiceOption struct {
	Label       string `json:"label"`
	Value       string `json:"value,omitempty"`
	Description string `json:"description,omitempty"`
}

type ChoiceQuestion struct {
	Header   string         `json:"header,omitempty"`
	Question string         `json:"question,omitempty"`
	IsOther  bool           `json:"isOther,omitempty"`
	IsSecret bool           `json:"isSecret,omitempty"`
	Options  []ChoiceOption `json:"options,omitempty"`
}

type ChoiceAnswer struct {
	Value   string `json:"value,omitempty"`
	Label   string `json:"label,omitempty"`
	IsOther bool   `json:"isOther,omitempty"`
}

type KeyValueRow struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type FileItem struct {
	Label   string `json:"label"`
	Path    string `json:"path,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Meta    string `json:"meta,omitempty"`
	Preview string `json:"preview,omitempty"`
}

type ActivityEntry struct {
	Label string `json:"label,omitempty"`
	Path  string `json:"path,omitempty"`
	Query string `json:"query,omitempty"`
}

type TurnRuntime struct {
	Active    bool   `json:"active"`
	Phase     string `json:"phase,omitempty"`
	HostID    string `json:"hostId,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
}

type CodexRuntime struct {
	Status       string `json:"status,omitempty"`
	RetryAttempt int    `json:"retryAttempt,omitempty"`
	RetryMax     int    `json:"retryMax,omitempty"`
	LastError    string `json:"lastError,omitempty"`
}

type ActivityRuntime struct {
	FilesViewed            int             `json:"filesViewed,omitempty"`
	SearchCount            int             `json:"searchCount,omitempty"`
	SearchLocationCount    int             `json:"searchLocationCount,omitempty"`
	ListCount              int             `json:"listCount,omitempty"`
	CommandsRun            int             `json:"commandsRun,omitempty"`
	FilesChanged           int             `json:"filesChanged,omitempty"`
	CurrentReadingFile     string          `json:"currentReadingFile,omitempty"`
	CurrentChangingFile    string          `json:"currentChangingFile,omitempty"`
	CurrentListingPath     string          `json:"currentListingPath,omitempty"`
	CurrentSearchKind      string          `json:"currentSearchKind,omitempty"`
	CurrentSearchQuery     string          `json:"currentSearchQuery,omitempty"`
	CurrentWebSearchQuery  string          `json:"currentWebSearchQuery,omitempty"`
	ViewedFiles            []ActivityEntry `json:"viewedFiles,omitempty"`
	SearchedWebQueries     []ActivityEntry `json:"searchedWebQueries,omitempty"`
	SearchedContentQueries []ActivityEntry `json:"searchedContentQueries,omitempty"`
}

type SessionMeta struct {
	Kind               string `json:"kind,omitempty"`
	Visible            bool   `json:"visible"`
	MissionID          string `json:"missionId,omitempty"`
	WorkspaceSessionID string `json:"workspaceSessionId,omitempty"`
	WorkerHostID       string `json:"workerHostId,omitempty"`
	RuntimePreset      string `json:"runtimePreset,omitempty"`
}

type RuntimeState struct {
	Turn     TurnRuntime     `json:"turn"`
	Codex    CodexRuntime    `json:"codex"`
	Activity ActivityRuntime `json:"activity"`
}

type Card struct {
	ID            string           `json:"id"`
	Type          string           `json:"type"`
	Role          string           `json:"role,omitempty"`
	Title         string           `json:"title,omitempty"`
	Text          string           `json:"text,omitempty"`
	Message       string           `json:"message,omitempty"`
	Summary       string           `json:"summary,omitempty"`
	Status        string           `json:"status,omitempty"`
	Command       string           `json:"command,omitempty"`
	Cwd           string           `json:"cwd,omitempty"`
	HostID        string           `json:"hostId,omitempty"`
	HostName      string           `json:"hostName,omitempty"`
	Output        string           `json:"output,omitempty"`
	Stdout        string           `json:"stdout,omitempty"`
	Stderr        string           `json:"stderr,omitempty"`
	ExitCode      int              `json:"exitCode,omitempty"`
	Timeout       bool             `json:"timeout,omitempty"`
	Cancelled     bool             `json:"cancelled,omitempty"`
	Error         string           `json:"error,omitempty"`
	Items         []PlanItem       `json:"items,omitempty"`
	Changes       []FileChange     `json:"changes,omitempty"`
	Approval      *ApprovalRef     `json:"approval,omitempty"`
	RequestID     string           `json:"requestId,omitempty"`
	Question      string           `json:"question,omitempty"`
	Options       []ChoiceOption   `json:"options,omitempty"`
	Questions     []ChoiceQuestion `json:"questions,omitempty"`
	AnswerSummary []string         `json:"answerSummary,omitempty"`
	Retryable     *bool            `json:"retryable,omitempty"`
	DurationMS    int64            `json:"durationMs,omitempty"`
	KVRows        []KeyValueRow    `json:"kvRows,omitempty"`
	Highlights    []string         `json:"highlights,omitempty"`
	FileItems     []FileItem       `json:"fileItems,omitempty"`
	Detail        map[string]any   `json:"detail,omitempty"`
	CreatedAt     string           `json:"createdAt"`
	UpdatedAt     string           `json:"updatedAt"`
}

type ApprovalRequest struct {
	ID           string       `json:"id"`
	RequestIDRaw string       `json:"-"`
	HostID       string       `json:"hostId,omitempty"`
	Fingerprint  string       `json:"-"`
	Type         string       `json:"type"`
	Status       string       `json:"status"`
	ThreadID     string       `json:"threadId"`
	TurnID       string       `json:"turnId"`
	ItemID       string       `json:"itemId,omitempty"`
	Command      string       `json:"command,omitempty"`
	Cwd          string       `json:"cwd,omitempty"`
	Reason       string       `json:"reason,omitempty"`
	GrantRoot    string       `json:"grantRoot,omitempty"`
	Decisions    []string     `json:"decisions,omitempty"`
	Changes      []FileChange `json:"changes,omitempty"`
	RequestedAt  string       `json:"requestedAt"`
	ResolvedAt   string       `json:"resolvedAt,omitempty"`
}

type ApprovalGrant struct {
	ID          string `json:"id"`
	HostID      string `json:"hostId,omitempty"`
	Type        string `json:"type"`
	Fingerprint string `json:"fingerprint"`
	Command     string `json:"command,omitempty"`
	Cwd         string `json:"cwd,omitempty"`
	CreatedAt   string `json:"createdAt"`
}

type ChoiceRequest struct {
	ID           string           `json:"id"`
	RequestIDRaw string           `json:"-"`
	ThreadID     string           `json:"threadId,omitempty"`
	TurnID       string           `json:"turnId,omitempty"`
	ItemID       string           `json:"itemId,omitempty"`
	Status       string           `json:"status"`
	Questions    []ChoiceQuestion `json:"questions,omitempty"`
	RequestedAt  string           `json:"requestedAt"`
	ResolvedAt   string           `json:"resolvedAt,omitempty"`
}

type Snapshot struct {
	SessionID      string            `json:"sessionId"`
	Kind           string            `json:"kind,omitempty"`
	SelectedHostID string            `json:"selectedHostId"`
	Auth           AuthState         `json:"auth"`
	Hosts          []Host            `json:"hosts"`
	Cards          []Card            `json:"cards"`
	Approvals      []ApprovalRequest `json:"approvals"`
	Runtime        RuntimeState      `json:"runtime"`
	LastActivityAt string            `json:"lastActivityAt,omitempty"`
	Config         UIConfig          `json:"config"`
}

type SessionSummary struct {
	ID             string `json:"id"`
	Kind           string `json:"kind,omitempty"`
	Title          string `json:"title"`
	Preview        string `json:"preview"`
	SelectedHostID string `json:"selectedHostId"`
	Status         string `json:"status"`
	MessageCount   int    `json:"messageCount"`
	CreatedAt      string `json:"createdAt"`
	LastActivityAt string `json:"lastActivityAt"`
}

type MissionHistorySummary struct {
	ID                       string `json:"id"`
	WorkspaceSessionID       string `json:"workspaceSessionId,omitempty"`
	PlannerSessionID         string `json:"plannerSessionId,omitempty"`
	Title                    string `json:"title,omitempty"`
	Summary                  string `json:"summary,omitempty"`
	Status                   string `json:"status,omitempty"`
	ProjectionMode           string `json:"projectionMode,omitempty"`
	CreatedAt                string `json:"createdAt,omitempty"`
	UpdatedAt                string `json:"updatedAt,omitempty"`
	TaskCount                int    `json:"taskCount,omitempty"`
	WorkerCount              int    `json:"workerCount,omitempty"`
	WorkspaceCount           int    `json:"workspaceCount,omitempty"`
	EventCount               int    `json:"eventCount,omitempty"`
	QueuedTaskCount          int    `json:"queuedTaskCount,omitempty"`
	ReadyTaskCount           int    `json:"readyTaskCount,omitempty"`
	DispatchingTaskCount     int    `json:"dispatchingTaskCount,omitempty"`
	RunningTaskCount         int    `json:"runningTaskCount,omitempty"`
	WaitingApprovalTaskCount int    `json:"waitingApprovalTaskCount,omitempty"`
	WaitingInputTaskCount    int    `json:"waitingInputTaskCount,omitempty"`
	CompletedTaskCount       int    `json:"completedTaskCount,omitempty"`
	FailedTaskCount          int    `json:"failedTaskCount,omitempty"`
	CancelledTaskCount       int    `json:"cancelledTaskCount,omitempty"`
}

type MissionHistoryTask struct {
	ID             string   `json:"id"`
	MissionID      string   `json:"missionId,omitempty"`
	HostID         string   `json:"hostId,omitempty"`
	WorkerHostID   string   `json:"workerHostId,omitempty"`
	SessionID      string   `json:"sessionId,omitempty"`
	ThreadID       string   `json:"threadId,omitempty"`
	Title          string   `json:"title,omitempty"`
	Instruction    string   `json:"instruction,omitempty"`
	Constraints    []string `json:"constraints,omitempty"`
	Status         string   `json:"status,omitempty"`
	ExternalNodeID string   `json:"externalNodeId,omitempty"`
	Attempt        int      `json:"attempt,omitempty"`
	CreatedAt      string   `json:"createdAt,omitempty"`
	UpdatedAt      string   `json:"updatedAt,omitempty"`
	LastError      string   `json:"lastError,omitempty"`
	LastReply      string   `json:"lastReply,omitempty"`
	ApprovalState  string   `json:"approvalState,omitempty"`
}

type MissionHistoryWorker struct {
	MissionID      string                 `json:"missionId,omitempty"`
	HostID         string                 `json:"hostId,omitempty"`
	SessionID      string                 `json:"sessionId,omitempty"`
	ThreadID       string                 `json:"threadId,omitempty"`
	WorkspaceID    string                 `json:"workspaceId,omitempty"`
	ActiveTaskID   string                 `json:"activeTaskId,omitempty"`
	QueueTaskIDs   []string               `json:"queueTaskIds,omitempty"`
	Status         string                 `json:"status,omitempty"`
	LastSeenAt     string                 `json:"lastSeenAt,omitempty"`
	IdleSince      string                 `json:"idleSince,omitempty"`
	UpdatedAt      string                 `json:"updatedAt,omitempty"`
	Conversation   []ConversationExcerpt  `json:"conversation,omitempty"`
	Terminal       map[string]any         `json:"terminal,omitempty"`
	ApprovalAnchor *ApprovalTerminalAnchor `json:"approvalAnchor,omitempty"`
}

type MissionHistoryWorkspace struct {
	ID         string `json:"id"`
	MissionID  string `json:"missionId,omitempty"`
	SessionID  string `json:"sessionId,omitempty"`
	HostID     string `json:"hostId,omitempty"`
	Kind       string `json:"kind,omitempty"`
	LocalPath  string `json:"localPath,omitempty"`
	RemotePath string `json:"remotePath,omitempty"`
	Status     string `json:"status,omitempty"`
	CreatedAt  string `json:"createdAt,omitempty"`
	UpdatedAt  string `json:"updatedAt,omitempty"`
}

type MissionHistoryEvent struct {
	ID           string `json:"id"`
	MissionID    string `json:"missionId,omitempty"`
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

type ConversationExcerpt struct {
	ID        string `json:"id,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	ThreadID  string `json:"threadId,omitempty"`
	HostID    string `json:"hostId,omitempty"`
	TaskID    string `json:"taskId,omitempty"`
	Role      string `json:"role,omitempty"`
	Source    string `json:"source,omitempty"`
	CardType  string `json:"cardType,omitempty"`
	Title     string `json:"title,omitempty"`
	Text      string `json:"text,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Status    string `json:"status,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type DispatchEvent struct {
	ID           string `json:"id,omitempty"`
	MissionID    string `json:"missionId,omitempty"`
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

type TaskHostBinding struct {
	TaskID         string   `json:"taskId,omitempty"`
	MissionID      string   `json:"missionId,omitempty"`
	HostID         string   `json:"hostId,omitempty"`
	WorkerHostID   string   `json:"workerHostId,omitempty"`
	SessionID      string   `json:"sessionId,omitempty"`
	ThreadID       string   `json:"threadId,omitempty"`
	Title          string   `json:"title,omitempty"`
	Instruction    string   `json:"instruction,omitempty"`
	Status         string   `json:"status,omitempty"`
	Constraints    []string `json:"constraints,omitempty"`
	Attempt        int      `json:"attempt,omitempty"`
	ApprovalState  string   `json:"approvalState,omitempty"`
	LastReply      string   `json:"lastReply,omitempty"`
	LastError      string   `json:"lastError,omitempty"`
	ExternalNodeID string   `json:"externalNodeId,omitempty"`
	CreatedAt      string   `json:"createdAt,omitempty"`
	UpdatedAt      string   `json:"updatedAt,omitempty"`
}

type ApprovalTerminalAnchor struct {
	ApprovalID      string `json:"approvalId,omitempty"`
	ItemID          string `json:"itemId,omitempty"`
	HostID          string `json:"hostId,omitempty"`
	SessionID       string `json:"sessionId,omitempty"`
	ThreadID        string `json:"threadId,omitempty"`
	Command         string `json:"command,omitempty"`
	Cwd             string `json:"cwd,omitempty"`
	Reason          string `json:"reason,omitempty"`
	RequestedAt     string `json:"requestedAt,omitempty"`
	TerminalCardID  string `json:"terminalCardId,omitempty"`
	TerminalTitle   string `json:"terminalTitle,omitempty"`
	TerminalStatus  string `json:"terminalStatus,omitempty"`
	TerminalSummary string `json:"terminalSummary,omitempty"`
}

type MissionHistoryReport struct {
	Summary      string                `json:"summary,omitempty"`
	OverviewRows []KeyValueRow         `json:"overviewRows,omitempty"`
	Highlights   []string              `json:"highlights,omitempty"`
	Timeline     []MissionHistoryEvent `json:"timeline,omitempty"`
}

type MissionHistoryDetail struct {
	MissionHistorySummary
	Report              MissionHistoryReport  `json:"report"`
	Tasks               []MissionHistoryTask  `json:"tasks,omitempty"`
	Workers             []MissionHistoryWorker `json:"workers,omitempty"`
	Workspaces          []MissionHistoryWorkspace `json:"workspaces,omitempty"`
	Events              []MissionHistoryEvent `json:"events,omitempty"`
	PlannerConversation []ConversationExcerpt `json:"plannerConversation,omitempty"`
	DispatchEvents      []DispatchEvent       `json:"dispatchEvents,omitempty"`
	TaskBindings        []TaskHostBinding     `json:"taskBindings,omitempty"`
}

func DefaultSessionMeta() SessionMeta {
	return SessionMeta{
		Kind:          SessionKindSingleHost,
		Visible:       true,
		RuntimePreset: SessionRuntimePresetSingleHost,
	}
}

func NormalizeSessionMeta(meta SessionMeta) SessionMeta {
	if strings.TrimSpace(meta.Kind) == "" {
		meta.Kind = SessionKindSingleHost
	}
	switch strings.TrimSpace(meta.RuntimePreset) {
	case "", SessionRuntimePresetSingleHost, SessionRuntimePresetWorkspace, SessionRuntimePresetPlanner, SessionRuntimePresetWorker:
		if meta.RuntimePreset == "" {
			switch meta.Kind {
			case SessionKindWorkspace:
				meta.RuntimePreset = SessionRuntimePresetWorkspace
			case SessionKindPlanner:
				meta.RuntimePreset = SessionRuntimePresetPlanner
			case SessionKindWorker:
				meta.RuntimePreset = SessionRuntimePresetWorker
			default:
				meta.RuntimePreset = SessionRuntimePresetSingleHost
			}
		}
	default:
		meta.RuntimePreset = SessionRuntimePresetSingleHost
	}
	return meta
}

type UIConfig struct {
	OAuthConfigured bool `json:"oauthConfigured"`
	CodexAlive      bool `json:"codexAlive"`
}

func DefaultAgentProfiles() []AgentProfile {
	return []AgentProfile{
		defaultAgentProfile(AgentProfileTypeMainAgent),
		defaultAgentProfile(AgentProfileTypeHostAgentDefault),
	}
}

func SupportedAgentSkills() []AgentSkill {
	return []AgentSkill{
		{
			ID:             "ops-triage",
			Name:           "Ops Triage",
			Description:    "快速归类问题并给出最小干预路径。",
			Source:         "built-in",
			ActivationMode: AgentSkillActivationDefault,
		},
		{
			ID:             "incident-summary",
			Name:           "Incident Summary",
			Description:    "把诊断过程整理成可交付摘要。",
			Source:         "local",
			ActivationMode: AgentSkillActivationDefault,
		},
		{
			ID:             "safe-change-review",
			Name:           "Safe Change Review",
			Description:    "在执行前做变更影响检查。",
			Source:         "built-in",
			ActivationMode: AgentSkillActivationExplicit,
		},
		{
			ID:             "host-diagnostics",
			Name:           "Host Diagnostics",
			Description:    "收集主机健康与日志摘要。",
			Source:         "local",
			ActivationMode: AgentSkillActivationDefault,
		},
		{
			ID:             "host-change-review",
			Name:           "Host Change Review",
			Description:    "对主机变更做安全复核。",
			Source:         "built-in",
			ActivationMode: AgentSkillActivationExplicit,
		},
	}
}

func SupportedAgentMCPs() []AgentMCP {
	return []AgentMCP{
		{
			ID:         "filesystem",
			Name:       "Filesystem MCP",
			Type:       "stdio",
			Source:     "built-in",
			Permission: AgentMCPPermissionReadonly,
		},
		{
			ID:                           "docs",
			Name:                         "Docs MCP",
			Type:                         "http",
			Source:                       "local",
			Permission:                   AgentMCPPermissionReadonly,
			RequiresExplicitUserApproval: true,
		},
		{
			ID:                           "metrics",
			Name:                         "Metrics MCP",
			Type:                         "http",
			Source:                       "built-in",
			Permission:                   AgentMCPPermissionReadwrite,
			RequiresExplicitUserApproval: true,
		},
		{
			ID:         "host-files",
			Name:       "Host Files MCP",
			Type:       "stdio",
			Source:     "built-in",
			Permission: AgentMCPPermissionReadonly,
		},
		{
			ID:                           "host-logs",
			Name:                         "Host Logs MCP",
			Type:                         "http",
			Source:                       "local",
			Permission:                   AgentMCPPermissionReadonly,
			RequiresExplicitUserApproval: true,
		},
	}
}

func DefaultAgentProfileIDs() []string {
	return []string{
		string(AgentProfileTypeMainAgent),
		string(AgentProfileTypeHostAgentDefault),
	}
}

func DefaultAgentProfile(profileType string) AgentProfile {
	return defaultAgentProfile(AgentProfileType(profileType))
}

func CompleteAgentProfile(profile AgentProfile) AgentProfile {
	if profile.Type == "" {
		switch profile.ID {
		case string(AgentProfileTypeHostAgentDefault):
			profile.Type = string(AgentProfileTypeHostAgentDefault)
		case string(AgentProfileTypeHostAgentOverride):
			profile.Type = string(AgentProfileTypeHostAgentOverride)
		default:
			profile.Type = string(AgentProfileTypeMainAgent)
		}
	}
	defaultProfile := defaultAgentProfile(AgentProfileType(profile.Type))
	if profile.ID == "" {
		profile.ID = defaultProfile.ID
	}
	if profile.Name == "" {
		profile.Name = defaultProfile.Name
	}
	if profile.Description == "" {
		profile.Description = defaultProfile.Description
	}
	if profile.Runtime.Model == "" {
		profile.Runtime.Model = defaultProfile.Runtime.Model
	}
	if profile.Runtime.ReasoningEffort == "" {
		profile.Runtime.ReasoningEffort = defaultProfile.Runtime.ReasoningEffort
	}
	if profile.Runtime.ApprovalPolicy == "" {
		profile.Runtime.ApprovalPolicy = defaultProfile.Runtime.ApprovalPolicy
	}
	if profile.Runtime.SandboxMode == "" {
		profile.Runtime.SandboxMode = defaultProfile.Runtime.SandboxMode
	}
	if profile.SystemPrompt.Content == "" {
		profile.SystemPrompt.Content = defaultProfile.SystemPrompt.Content
	}
	if profile.SystemPrompt.Preview == "" {
		profile.SystemPrompt.Preview = defaultSystemPromptPreview(profile.SystemPrompt.Content)
	}
	if profile.SystemPrompt.Version == "" {
		profile.SystemPrompt.Version = defaultProfile.SystemPrompt.Version
	}
	if profile.SystemPrompt.Notes == "" {
		profile.SystemPrompt.Notes = defaultProfile.SystemPrompt.Notes
	}
	if profile.CommandPermissions.Enabled == nil {
		profile.CommandPermissions.Enabled = boolPtr(boolValue(defaultProfile.CommandPermissions.Enabled, true))
	}
	if profile.CommandPermissions.DefaultMode == "" {
		profile.CommandPermissions.DefaultMode = defaultProfile.CommandPermissions.DefaultMode
	}
	if profile.CommandPermissions.AllowShellWrapper == nil {
		profile.CommandPermissions.AllowShellWrapper = boolPtr(boolValue(defaultProfile.CommandPermissions.AllowShellWrapper, true))
	}
	if profile.CommandPermissions.AllowSudo == nil {
		profile.CommandPermissions.AllowSudo = boolPtr(boolValue(defaultProfile.CommandPermissions.AllowSudo, false))
	}
	if profile.CommandPermissions.DefaultTimeoutSeconds == 0 {
		profile.CommandPermissions.DefaultTimeoutSeconds = defaultProfile.CommandPermissions.DefaultTimeoutSeconds
	}
	if len(profile.CommandPermissions.AllowedWritableRoots) == 0 {
		profile.CommandPermissions.AllowedWritableRoots = append([]string(nil), defaultProfile.CommandPermissions.AllowedWritableRoots...)
	}
	if profile.CommandPermissions.CategoryPolicies == nil {
		profile.CommandPermissions.CategoryPolicies = cloneStringMap(defaultProfile.CommandPermissions.CategoryPolicies)
	}
	profile.CapabilityPermissions = completeCapabilityPermissions(profile.CapabilityPermissions, defaultProfile.CapabilityPermissions)
	if len(profile.Skills) == 0 {
		profile.Skills = append([]AgentSkill(nil), defaultProfile.Skills...)
	} else {
		profile.Skills = normalizeAgentSkills(profile.Skills)
	}
	if len(profile.MCPs) == 0 {
		profile.MCPs = append([]AgentMCP(nil), defaultProfile.MCPs...)
	} else {
		profile.MCPs = normalizeAgentMCPs(profile.MCPs)
	}
	if profile.UpdatedAt == "" {
		profile.UpdatedAt = NowString()
	}
	if profile.UpdatedBy == "" {
		profile.UpdatedBy = AgentProfileUpdatedBySystem
	}
	return profile
}

func SortAgentProfiles(profiles []AgentProfile) {
	sort.SliceStable(profiles, func(i, j int) bool {
		if profiles[i].Type != profiles[j].Type {
			return profiles[i].Type < profiles[j].Type
		}
		if profiles[i].Name != profiles[j].Name {
			return profiles[i].Name < profiles[j].Name
		}
		return profiles[i].ID < profiles[j].ID
	})
}

func defaultAgentProfile(profileType AgentProfileType) AgentProfile {
	now := NowString()
	profile := AgentProfile{
		ID:        string(profileType),
		Type:      string(profileType),
		UpdatedAt: now,
		UpdatedBy: AgentProfileUpdatedBySystem,
		Runtime: AgentRuntimeSettings{
			Model:           "gpt-5.4",
			ReasoningEffort: "medium",
			ApprovalPolicy:  "untrusted",
			SandboxMode:     "workspace-write",
		},
		SystemPrompt: AgentSystemPrompt{
			Version: "v1",
		},
		CommandPermissions: AgentCommandPermissions{
			Enabled:               boolPtr(true),
			DefaultMode:           AgentPermissionModeApprovalRequired,
			AllowShellWrapper:     boolPtr(true),
			AllowSudo:             boolPtr(false),
			DefaultTimeoutSeconds: 120,
			CategoryPolicies: map[string]string{
				"system_inspection":   AgentPermissionModeAllow,
				"service_read":        AgentPermissionModeAllow,
				"network_read":        AgentPermissionModeAllow,
				"file_read":           AgentPermissionModeAllow,
				"service_mutation":    AgentPermissionModeApprovalRequired,
				"filesystem_mutation": AgentPermissionModeApprovalRequired,
				"package_mutation":    AgentPermissionModeDeny,
			},
		},
		CapabilityPermissions: defaultCapabilityPermissions(),
	}
	switch profileType {
	case AgentProfileTypeHostAgentDefault:
		profile.Name = "Host Agent Default"
		profile.Description = "Default profile for the host agent runtime"
		profile.Runtime.Model = "gpt-5.4-mini"
		profile.Runtime.ReasoningEffort = "low"
		profile.SystemPrompt.Content = strings.TrimSpace(`
You are the default host-side agent.
Follow server instructions, keep actions scoped to the selected host, and avoid assuming extra privileges.
`)
	case AgentProfileTypeHostAgentOverride:
		profile.Name = "Host Agent Override"
		profile.Description = "Override profile for a specific host agent"
		profile.SystemPrompt.Content = strings.TrimSpace(`
You are a host agent override profile.
Apply the smallest necessary change set and keep execution localized.
`)
	default:
		profile.Type = string(AgentProfileTypeMainAgent)
		profile.ID = string(AgentProfileTypeMainAgent)
		profile.Name = "Main Agent"
		profile.Description = "Default profile for the primary Codex agent"
		profile.SystemPrompt.Content = strings.TrimSpace(`
Operate only on the selected host.
Use the default writable workspace for changes.
Do not assume access outside the workspace unless explicitly requested and approved.
`)
	}
	profile.Skills = defaultProfileSkills(profileType)
	profile.MCPs = defaultProfileMCPs(profileType)
	profile.SystemPrompt.Preview = defaultSystemPromptPreview(profile.SystemPrompt.Content)
	return profile
}

func NormalizeAgentSkillActivationMode(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "default", "default_enabled":
		return AgentSkillActivationDefault
	case "explicit", "explicit_only":
		return AgentSkillActivationExplicit
	case "disabled":
		return AgentSkillActivationDisabled
	default:
		return strings.TrimSpace(strings.ToLower(value))
	}
}

func NormalizeAgentMCPPermission(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "readonly", "read-only":
		return AgentMCPPermissionReadonly
	case "readwrite", "read-write":
		return AgentMCPPermissionReadwrite
	default:
		return strings.TrimSpace(strings.ToLower(value))
	}
}

func defaultCapabilityPermissions() AgentCapabilityPermissions {
	return AgentCapabilityPermissions{
		CommandExecution: AgentCapabilityApprovalRequired,
		FileRead:         AgentCapabilityEnabled,
		FileSearch:       AgentCapabilityEnabled,
		FileChange:       AgentCapabilityApprovalRequired,
		Terminal:         AgentCapabilityEnabled,
		WebSearch:        AgentCapabilityEnabled,
		WebOpen:          AgentCapabilityEnabled,
		Approval:         AgentCapabilityEnabled,
		MultiAgent:       AgentCapabilityEnabled,
		Plan:             AgentCapabilityEnabled,
		Summary:          AgentCapabilityEnabled,
	}
}

func completeCapabilityPermissions(in AgentCapabilityPermissions, defaults AgentCapabilityPermissions) AgentCapabilityPermissions {
	if in.CommandExecution == "" {
		in.CommandExecution = defaults.CommandExecution
	}
	if in.FileRead == "" {
		in.FileRead = defaults.FileRead
	}
	if in.FileSearch == "" {
		in.FileSearch = defaults.FileSearch
	}
	if in.FileChange == "" {
		in.FileChange = defaults.FileChange
	}
	if in.Terminal == "" {
		in.Terminal = defaults.Terminal
	}
	if in.WebSearch == "" {
		in.WebSearch = defaults.WebSearch
	}
	if in.WebOpen == "" {
		in.WebOpen = defaults.WebOpen
	}
	if in.Approval == "" {
		in.Approval = defaults.Approval
	}
	if in.MultiAgent == "" {
		in.MultiAgent = defaults.MultiAgent
	}
	if in.Plan == "" {
		in.Plan = defaults.Plan
	}
	if in.Summary == "" {
		in.Summary = defaults.Summary
	}
	return in
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return make(map[string]string)
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func defaultProfileSkills(profileType AgentProfileType) []AgentSkill {
	items := make([]AgentSkill, 0)
	switch profileType {
	case AgentProfileTypeHostAgentDefault:
		items = append(items,
			AgentSkill{
				ID:             "host-diagnostics",
				Name:           "Host Diagnostics",
				Description:    "收集主机健康与日志摘要。",
				Source:         "local",
				Enabled:        true,
				ActivationMode: AgentSkillActivationDefault,
			},
			AgentSkill{
				ID:             "host-change-review",
				Name:           "Host Change Review",
				Description:    "对主机变更做安全复核。",
				Source:         "built-in",
				Enabled:        false,
				ActivationMode: AgentSkillActivationExplicit,
			},
		)
	default:
		items = append(items,
			AgentSkill{
				ID:             "ops-triage",
				Name:           "Ops Triage",
				Description:    "快速归类问题并给出最小干预路径。",
				Source:         "built-in",
				Enabled:        true,
				ActivationMode: AgentSkillActivationDefault,
			},
			AgentSkill{
				ID:             "incident-summary",
				Name:           "Incident Summary",
				Description:    "把诊断过程整理成可交付摘要。",
				Source:         "local",
				Enabled:        true,
				ActivationMode: AgentSkillActivationDefault,
			},
			AgentSkill{
				ID:             "safe-change-review",
				Name:           "Safe Change Review",
				Description:    "在执行前做变更影响检查。",
				Source:         "built-in",
				Enabled:        false,
				ActivationMode: AgentSkillActivationExplicit,
			},
		)
	}
	return items
}

func defaultProfileMCPs(profileType AgentProfileType) []AgentMCP {
	items := make([]AgentMCP, 0)
	switch profileType {
	case AgentProfileTypeHostAgentDefault:
		items = append(items,
			AgentMCP{
				ID:         "host-files",
				Name:       "Host Files MCP",
				Type:       "stdio",
				Source:     "built-in",
				Enabled:    true,
				Permission: AgentMCPPermissionReadonly,
			},
			AgentMCP{
				ID:                           "host-logs",
				Name:                         "Host Logs MCP",
				Type:                         "http",
				Source:                       "local",
				Enabled:                      true,
				Permission:                   AgentMCPPermissionReadonly,
				RequiresExplicitUserApproval: true,
			},
		)
	default:
		items = append(items,
			AgentMCP{
				ID:         "filesystem",
				Name:       "Filesystem MCP",
				Type:       "stdio",
				Source:     "built-in",
				Enabled:    true,
				Permission: AgentMCPPermissionReadonly,
			},
			AgentMCP{
				ID:                           "docs",
				Name:                         "Docs MCP",
				Type:                         "http",
				Source:                       "local",
				Enabled:                      true,
				Permission:                   AgentMCPPermissionReadonly,
				RequiresExplicitUserApproval: true,
			},
			AgentMCP{
				ID:                           "metrics",
				Name:                         "Metrics MCP",
				Type:                         "http",
				Source:                       "built-in",
				Enabled:                      false,
				Permission:                   AgentMCPPermissionReadwrite,
				RequiresExplicitUserApproval: true,
			},
		)
	}
	return items
}

func normalizeAgentSkills(current []AgentSkill) []AgentSkill {
	if len(current) == 0 {
		return make([]AgentSkill, 0)
	}
	items := make([]AgentSkill, 0, len(current))
	for _, currentItem := range current {
		merged := currentItem
		merged.ActivationMode = NormalizeAgentSkillActivationMode(merged.ActivationMode)
		items = append(items, merged)
	}
	return items
}

func normalizeAgentMCPs(current []AgentMCP) []AgentMCP {
	if len(current) == 0 {
		return make([]AgentMCP, 0)
	}
	items := make([]AgentMCP, 0, len(current))
	for _, currentItem := range current {
		merged := currentItem
		merged.Permission = NormalizeAgentMCPPermission(merged.Permission)
		items = append(items, merged)
	}
	return items
}

func defaultSystemPromptPreview(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	preview := strings.TrimSpace(lines[0])
	if len(preview) > 120 {
		preview = preview[:120]
	}
	return preview
}

func boolPtr(value bool) *bool {
	v := value
	return &v
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func NewID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		now := time.Now().UnixNano()
		return prefix + "-" + time.Unix(0, now).Format("20060102150405")
	}
	return prefix + "-" + hex.EncodeToString(buf)
}

func NowString() string {
	return time.Now().Format(time.RFC3339)
}

func SortHosts(hosts []Host) {
	sort.SliceStable(hosts, func(i, j int) bool {
		if hosts[i].ID == ServerLocalHostID {
			return true
		}
		if hosts[j].ID == ServerLocalHostID {
			return false
		}
		if hosts[i].Status != hosts[j].Status {
			return hosts[i].Status == "online"
		}
		return hosts[i].Name < hosts[j].Name
	})
}
