package tools

import "github.com/lizhongxuan/aiops-codex/internal/filepatch"

// ToolContext provides the session context that tool handlers need.
// This interface breaks the circular dependency between tools and agentloop.
type ToolContext interface {
	Cwd() string
	SessionID() string
	Model() string
	EnabledTools() []string
	DiffTracker() *filepatch.TurnDiffTracker
}
