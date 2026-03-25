// Code generated from proto/agent.proto. DO NOT EDIT.

package agentrpc

type Registration struct {
	Token        string            `json:"token,omitempty"`
	HostID       string            `json:"hostId,omitempty"`
	Hostname     string            `json:"hostname,omitempty"`
	OS           string            `json:"os,omitempty"`
	Arch         string            `json:"arch,omitempty"`
	AgentVersion string            `json:"agentVersion,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
}

type Heartbeat struct {
	HostID    string `json:"hostId,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

type Ping struct {
	Message   string `json:"message,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

type Ack struct {
	Message   string `json:"message,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

type TerminalOpen struct {
	SessionID string `json:"sessionId,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	Shell     string `json:"shell,omitempty"`
	Cols      int    `json:"cols,omitempty"`
	Rows      int    `json:"rows,omitempty"`
}

type TerminalInput struct {
	SessionID string `json:"sessionId,omitempty"`
	Data      string `json:"data,omitempty"`
}

type TerminalResize struct {
	SessionID string `json:"sessionId,omitempty"`
	Cols      int    `json:"cols,omitempty"`
	Rows      int    `json:"rows,omitempty"`
}

type TerminalSignal struct {
	SessionID string `json:"sessionId,omitempty"`
	Signal    string `json:"signal,omitempty"`
}

type TerminalClose struct {
	SessionID string `json:"sessionId,omitempty"`
}

type TerminalReady struct {
	SessionID string `json:"sessionId,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	Shell     string `json:"shell,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
	Status    string `json:"status,omitempty"`
}

type TerminalOutput struct {
	SessionID string `json:"sessionId,omitempty"`
	Data      string `json:"data,omitempty"`
}

type TerminalExit struct {
	SessionID string `json:"sessionId,omitempty"`
	Code      int    `json:"code,omitempty"`
	Status    string `json:"status,omitempty"`
}

type TerminalStatus struct {
	SessionID string `json:"sessionId,omitempty"`
	Status    string `json:"status,omitempty"`
	Message   string `json:"message,omitempty"`
}

type ExecStart struct {
	ExecID     string `json:"execId,omitempty"`
	Command    string `json:"command,omitempty"`
	Cwd        string `json:"cwd,omitempty"`
	Shell      string `json:"shell,omitempty"`
	TimeoutSec int    `json:"timeoutSec,omitempty"`
	Readonly   bool   `json:"readonly,omitempty"`
}

type ExecCancel struct {
	ExecID string `json:"execId,omitempty"`
}

type ExecOutput struct {
	ExecID string `json:"execId,omitempty"`
	Data   string `json:"data,omitempty"`
}

type ExecExit struct {
	ExecID  string `json:"execId,omitempty"`
	Code    int    `json:"code,omitempty"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

type FileEntry struct {
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
	Kind string `json:"kind,omitempty"`
	Size int64  `json:"size,omitempty"`
}

type FileMatch struct {
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	Preview string `json:"preview,omitempty"`
}

type FileListRequest struct {
	RequestID  string `json:"requestId,omitempty"`
	Path       string `json:"path,omitempty"`
	Recursive  bool   `json:"recursive,omitempty"`
	MaxEntries int    `json:"maxEntries,omitempty"`
}

type FileListResult struct {
	RequestID string      `json:"requestId,omitempty"`
	Path      string      `json:"path,omitempty"`
	Entries   []FileEntry `json:"entries,omitempty"`
	Truncated bool        `json:"truncated,omitempty"`
	Message   string      `json:"message,omitempty"`
}

type FileReadRequest struct {
	RequestID string `json:"requestId,omitempty"`
	Path      string `json:"path,omitempty"`
	MaxBytes  int    `json:"maxBytes,omitempty"`
}

type FileReadResult struct {
	RequestID string `json:"requestId,omitempty"`
	Path      string `json:"path,omitempty"`
	Content   string `json:"content,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Message   string `json:"message,omitempty"`
}

type FileSearchRequest struct {
	RequestID  string `json:"requestId,omitempty"`
	Path       string `json:"path,omitempty"`
	Query      string `json:"query,omitempty"`
	MaxMatches int    `json:"maxMatches,omitempty"`
}

type FileSearchResult struct {
	RequestID string      `json:"requestId,omitempty"`
	Path      string      `json:"path,omitempty"`
	Query     string      `json:"query,omitempty"`
	Matches   []FileMatch `json:"matches,omitempty"`
	Truncated bool        `json:"truncated,omitempty"`
	Message   string      `json:"message,omitempty"`
}

type FileWriteRequest struct {
	RequestID string `json:"requestId,omitempty"`
	Path      string `json:"path,omitempty"`
	Content   string `json:"content,omitempty"`
	WriteMode string `json:"writeMode,omitempty"`
}

type FileWriteResult struct {
	RequestID  string `json:"requestId,omitempty"`
	Path       string `json:"path,omitempty"`
	OldContent string `json:"oldContent,omitempty"`
	NewContent string `json:"newContent,omitempty"`
	Created    bool   `json:"created,omitempty"`
	WriteMode  string `json:"writeMode,omitempty"`
	Message    string `json:"message,omitempty"`
}

type Envelope struct {
	Kind              string             `json:"kind"`
	Registration      *Registration      `json:"registration,omitempty"`
	Heartbeat         *Heartbeat         `json:"heartbeat,omitempty"`
	Ping              *Ping              `json:"ping,omitempty"`
	Ack               *Ack               `json:"ack,omitempty"`
	TerminalOpen      *TerminalOpen      `json:"terminalOpen,omitempty"`
	TerminalInput     *TerminalInput     `json:"terminalInput,omitempty"`
	TerminalResize    *TerminalResize    `json:"terminalResize,omitempty"`
	TerminalSignal    *TerminalSignal    `json:"terminalSignal,omitempty"`
	TerminalClose     *TerminalClose     `json:"terminalClose,omitempty"`
	TerminalReady     *TerminalReady     `json:"terminalReady,omitempty"`
	TerminalOutput    *TerminalOutput    `json:"terminalOutput,omitempty"`
	TerminalExit      *TerminalExit      `json:"terminalExit,omitempty"`
	TerminalStatus    *TerminalStatus    `json:"terminalStatus,omitempty"`
	ExecStart         *ExecStart         `json:"execStart,omitempty"`
	ExecCancel        *ExecCancel        `json:"execCancel,omitempty"`
	ExecOutput        *ExecOutput        `json:"execOutput,omitempty"`
	ExecExit          *ExecExit          `json:"execExit,omitempty"`
	FileListRequest   *FileListRequest   `json:"fileListRequest,omitempty"`
	FileListResult    *FileListResult    `json:"fileListResult,omitempty"`
	FileReadRequest   *FileReadRequest   `json:"fileReadRequest,omitempty"`
	FileReadResult    *FileReadResult    `json:"fileReadResult,omitempty"`
	FileSearchRequest *FileSearchRequest `json:"fileSearchRequest,omitempty"`
	FileSearchResult  *FileSearchResult  `json:"fileSearchResult,omitempty"`
	FileWriteRequest  *FileWriteRequest  `json:"fileWriteRequest,omitempty"`
	FileWriteResult   *FileWriteResult   `json:"fileWriteResult,omitempty"`
	Error             string             `json:"error,omitempty"`
}
