package model

// ApprovalAuditRecord represents a single approval audit event
// (approval requested, decision made, or auto-accepted).
type ApprovalAuditRecord struct {
	ID                 string         `json:"id"`
	Event              string         `json:"event"`              // approval.requested | approval.decision | approval.auto_accepted
	SessionID          string         `json:"sessionId"`
	SessionKind        string         `json:"sessionKind"`        // single_host | workspace
	ThreadID           string         `json:"threadId"`
	TurnID             string         `json:"turnId"`
	WorkspaceSessionID string         `json:"workspaceSessionId,omitempty"`
	HostID             string         `json:"hostId"`
	HostName           string         `json:"hostName"`
	Operator           string         `json:"operator"`
	ApprovalID         string         `json:"approvalId"`
	ApprovalType       string         `json:"approvalType"`       // command | file_change | remote_command | remote_file_change
	ToolName           string         `json:"toolName"`
	Command            string         `json:"command,omitempty"`
	Cwd                string         `json:"cwd,omitempty"`
	FilePath           string         `json:"filePath,omitempty"`
	Decision           string         `json:"decision"`           // accept | reject | decline | auto_accept
	Status             string         `json:"status"`
	GrantMode          string         `json:"grantMode"`          // none | session | host
	Fingerprint        string         `json:"fingerprint"`
	StartedAt          string         `json:"startedAt"`
	EndedAt            string         `json:"endedAt,omitempty"`
	CreatedAt          string         `json:"createdAt"`
	Meta               map[string]any `json:"meta,omitempty"`
}
