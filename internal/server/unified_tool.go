package server

import (
	"context"
	"strings"
)

// ToolDescriptionContext carries runtime context for tool-owned descriptions.
type ToolDescriptionContext struct {
	SessionID   string
	SessionKind string
	HostID      string
	TurnLane    string
	Metadata    map[string]any
}

// Clone returns a copy with copied map fields.
func (ctx ToolDescriptionContext) Clone() ToolDescriptionContext {
	ctx.SessionID = strings.TrimSpace(ctx.SessionID)
	ctx.SessionKind = strings.TrimSpace(ctx.SessionKind)
	ctx.HostID = strings.TrimSpace(ctx.HostID)
	ctx.TurnLane = strings.TrimSpace(ctx.TurnLane)
	ctx.Metadata = cloneNestedAnyMap(ctx.Metadata)
	return ctx
}

// ToolCallRequest is the unified request envelope passed to tools.
type ToolCallRequest struct {
	Invocation ToolInvocation
	Input      map[string]any
	RawInput   string
	Metadata   map[string]any
}

// Clone returns a copy with copied nested maps.
func (req ToolCallRequest) Clone() ToolCallRequest {
	req.Invocation = req.Invocation.Clone()
	req.Input = cloneNestedAnyMap(req.Input)
	req.RawInput = strings.TrimSpace(req.RawInput)
	req.Metadata = cloneNestedAnyMap(req.Metadata)
	return req
}

// Normalize trims identifiers and guarantees a non-nil input map. When the
// explicit input is missing, it falls back to invocation arguments.
func (req *ToolCallRequest) Normalize() {
	req.Invocation.Normalize()
	req.RawInput = strings.TrimSpace(req.RawInput)
	req.Metadata = cloneNestedAnyMap(req.Metadata)
	if len(req.Input) == 0 && len(req.Invocation.Arguments) > 0 {
		req.Input = cloneNestedAnyMap(req.Invocation.Arguments)
		return
	}
	req.Input = cloneNestedAnyMap(req.Input)
}

// ToolCallResult is the unified execution result shape returned by tools.
type ToolCallResult struct {
	Output            any
	DisplayOutput     *ToolDisplayPayload
	StructuredContent map[string]any
	Metadata          map[string]any
	EvidenceRefs      []string
}

// Clone returns a copy with copied nested containers.
func (res ToolCallResult) Clone() ToolCallResult {
	if res.DisplayOutput != nil {
		cloned := res.DisplayOutput.Clone()
		res.DisplayOutput = &cloned
	}
	res.StructuredContent = cloneNestedAnyMap(res.StructuredContent)
	res.Metadata = cloneNestedAnyMap(res.Metadata)
	if res.EvidenceRefs != nil {
		res.EvidenceRefs = append([]string(nil), res.EvidenceRefs...)
	}
	return res
}

// PermissionResult captures the unified permission/safety decision for a tool call.
type PermissionResult struct {
	Allowed           bool
	RequiresApproval  bool
	Reason            string
	ApprovalType      string
	ApprovalDecisions []string
	Metadata          map[string]any
}

// Clone returns a copy with copied slices/maps.
func (result PermissionResult) Clone() PermissionResult {
	result.Reason = strings.TrimSpace(result.Reason)
	result.ApprovalType = strings.TrimSpace(result.ApprovalType)
	if result.ApprovalDecisions != nil {
		result.ApprovalDecisions = append([]string(nil), result.ApprovalDecisions...)
	}
	result.Metadata = cloneNestedAnyMap(result.Metadata)
	return result
}

// UnifiedTool is the Claude-style unified tool contract adapted to Go and the
// existing aiops-codex lifecycle/projection architecture.
type UnifiedTool interface {
	Name() string
	Aliases() []string

	Description(ctx ToolDescriptionContext) string
	InputSchema() map[string]any

	Call(ctx context.Context, req ToolCallRequest) (ToolCallResult, error)
	CheckPermissions(ctx context.Context, req ToolCallRequest) (PermissionResult, error)

	IsConcurrencySafe(req ToolCallRequest) bool
	IsReadOnly(req ToolCallRequest) bool
	IsDestructive(req ToolCallRequest) bool

	Display() ToolDisplayAdapter
}

// StreamingUnifiedTool is an optional capability interface for unified tools
// that emit lifecycle progress updates via ReportToolProgress.
type StreamingUnifiedTool interface {
	SupportsStreamingProgress() bool
}
