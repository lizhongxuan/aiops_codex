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

type Card struct {
	ID        string       `json:"id"`
	Type      string       `json:"type"`
	Role      string       `json:"role,omitempty"`
	Title     string       `json:"title,omitempty"`
	Text      string       `json:"text,omitempty"`
	Status    string       `json:"status,omitempty"`
	Command   string       `json:"command,omitempty"`
	Cwd       string       `json:"cwd,omitempty"`
	Output    string       `json:"output,omitempty"`
	Items     []PlanItem   `json:"items,omitempty"`
	Changes   []FileChange `json:"changes,omitempty"`
	Approval  *ApprovalRef `json:"approval,omitempty"`
	CreatedAt string       `json:"createdAt"`
	UpdatedAt string       `json:"updatedAt"`
}

type ApprovalRequest struct {
	ID           string       `json:"id"`
	RequestIDRaw string       `json:"-"`
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

type Snapshot struct {
	SessionID      string            `json:"sessionId"`
	SelectedHostID string            `json:"selectedHostId"`
	Auth           AuthState         `json:"auth"`
	Hosts          []Host            `json:"hosts"`
	Cards          []Card            `json:"cards"`
	Approvals      []ApprovalRequest `json:"approvals"`
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
