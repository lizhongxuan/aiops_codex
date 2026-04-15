package agentloop

import (
	"context"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
	"github.com/lizhongxuan/aiops-codex/internal/tools"
)

// Re-export core types from internal/tools for backward compatibility.
// Consumers should migrate to importing internal/tools directly.
type ToolHandler = tools.ToolHandler
type ToolEntry = tools.ToolEntry
type ToolRegistry = tools.ToolRegistry
type ToolContext = tools.ToolContext

// NewToolRegistry creates an empty ToolRegistry.
var NewToolRegistry = tools.NewToolRegistry

// Re-export tool registration functions.
var RegisterRemoteHostTools = tools.RegisterRemoteHostTools
var RegisterWorkspaceTools = tools.RegisterWorkspaceTools
var RegisterApplyPatchTool = tools.RegisterApplyPatchTool
var RegisterWebSearchTools = tools.RegisterWebSearchTools
var RegisterCorootTools = tools.RegisterCorootTools
var RegisterShellCommandTool = tools.RegisterShellCommandTool
var RegisterCodeModeTool = tools.RegisterCodeModeTool
var RegisterListDirTool = tools.RegisterListDirTool
var RegisterViewImageTool = tools.RegisterViewImageTool
var RegisterToolSuggestTool = tools.RegisterToolSuggestTool
var RegisterRequestUserInputTool = tools.RegisterRequestUserInputTool
var RegisterRequestPermissionsTool = tools.RegisterRequestPermissionsTool
var RegisterAgentJobsTool = tools.RegisterAgentJobsTool
var RegisterJSReplTool = tools.RegisterJSReplTool
var RegisterUnifiedExecTool = tools.RegisterUnifiedExecTool

// Re-export types used by other packages.
type WebSearchResult = tools.WebSearchResult
type DynamicToolSpec = tools.DynamicToolSpec
type ShellBackend = tools.ShellBackend

var BraveSearchHandler = tools.BraveSearchHandler
var RebuildToolSearchIndex = tools.RebuildToolSearchIndex
var DefaultShellBackend = tools.DefaultShellBackend
var SetShellBackend = tools.SetShellBackend

// SessionToolHandler is a handler that takes *Session directly instead of ToolContext.
// This is used by the server layer which needs full Session access.
type SessionToolHandler func(ctx context.Context, session *Session, call bifrost.ToolCall, args map[string]interface{}) (string, error)

// WrapSessionHandler wraps a SessionToolHandler into a ToolHandler by type-asserting
// the ToolContext back to *Session. Panics if the ToolContext is not a *Session.
func WrapSessionHandler(fn SessionToolHandler) ToolHandler {
	return func(ctx context.Context, tc tools.ToolContext, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
		session := tc.(*Session)
		return fn(ctx, session, call, args)
	}
}

// Ensure Session implements tools.ToolContext.
var _ tools.ToolContext = (*Session)(nil)

// SessionID returns the session ID, implementing tools.ToolContext.
func (s *Session) SessionID() string {
	return s.ID
}
