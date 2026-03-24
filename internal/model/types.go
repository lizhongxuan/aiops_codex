package model

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"time"
)

const (
	ServerLocalHostID = "server-local"
)

type Host struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Kind          string            `json:"kind"`
	Status        string            `json:"status"`
	Executable    bool              `json:"executable"`
	OS            string            `json:"os,omitempty"`
	Arch          string            `json:"arch,omitempty"`
	AgentVersion  string            `json:"agentVersion,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	LastHeartbeat string            `json:"lastHeartbeat,omitempty"`
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
	FilesViewed           int             `json:"filesViewed,omitempty"`
	SearchCount           int             `json:"searchCount,omitempty"`
	ListCount             int             `json:"listCount,omitempty"`
	CommandsRun           int             `json:"commandsRun,omitempty"`
	CurrentReadingFile    string          `json:"currentReadingFile,omitempty"`
	CurrentWebSearchQuery string          `json:"currentWebSearchQuery,omitempty"`
	ViewedFiles           []ActivityEntry `json:"viewedFiles,omitempty"`
	SearchedWebQueries    []ActivityEntry `json:"searchedWebQueries,omitempty"`
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
	Output        string           `json:"output,omitempty"`
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
	SelectedHostID string            `json:"selectedHostId"`
	Auth           AuthState         `json:"auth"`
	Hosts          []Host            `json:"hosts"`
	Cards          []Card            `json:"cards"`
	Approvals      []ApprovalRequest `json:"approvals"`
	Runtime        RuntimeState      `json:"runtime"`
	LastActivityAt string            `json:"lastActivityAt,omitempty"`
	Config         UIConfig          `json:"config"`
}

type UIConfig struct {
	OAuthConfigured bool `json:"oauthConfigured"`
	CodexAlive      bool `json:"codexAlive"`
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
