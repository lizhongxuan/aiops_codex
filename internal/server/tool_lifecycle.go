package server

import (
	stdstrings "strings"
	"time"
)

// ToolRunStatus describes the terminal or in-flight state of a tool execution.
type ToolRunStatus string

const (
	ToolRunStatusPending         ToolRunStatus = "pending"
	ToolRunStatusRunning         ToolRunStatus = "running"
	ToolRunStatusWaitingApproval ToolRunStatus = "waiting_approval"
	ToolRunStatusCompleted       ToolRunStatus = "completed"
	ToolRunStatusFailed          ToolRunStatus = "failed"
	ToolRunStatusCancelled       ToolRunStatus = "cancelled"
)

// ToolInvocationSource identifies where a tool invocation originated.
type ToolInvocationSource string

const (
	ToolInvocationSourceAgentloopToolCall    ToolInvocationSource = "agentloop_tool_call"
	ToolInvocationSourceDynamicToolCall      ToolInvocationSource = "dynamic_tool_call"
	ToolInvocationSourceOrchestratorDispatch ToolInvocationSource = "orchestrator_dispatch"
	ToolInvocationSourceApprovalResume       ToolInvocationSource = "approval_resume"
	ToolInvocationSourceSystemAutoReplay     ToolInvocationSource = "system_auto_replay"
)

// ToolInvocation describes a single tool call in a source-agnostic way.
type ToolInvocation struct {
	InvocationID     string
	SessionID        string
	ThreadID         string
	TurnID           string
	ToolName         string
	ToolKind         string
	Source           ToolInvocationSource
	HostID           string
	WorkspaceID      string
	CallID           string
	Arguments        map[string]any
	RawArguments     string
	RequiresApproval bool
	ReadOnly         bool
	StartedAt        time.Time
}

// Clone returns a shallow copy of the invocation with copied argument map.
func (inv ToolInvocation) Clone() ToolInvocation {
	if inv.Arguments != nil {
		args := make(map[string]any, len(inv.Arguments))
		for k, v := range inv.Arguments {
			args[k] = v
		}
		inv.Arguments = args
	}
	return inv
}

// Normalize trims canonical string fields and guarantees a non-nil argument map.
func (inv *ToolInvocation) Normalize() {
	inv.InvocationID = stdstrings.TrimSpace(inv.InvocationID)
	inv.SessionID = stdstrings.TrimSpace(inv.SessionID)
	inv.ThreadID = stdstrings.TrimSpace(inv.ThreadID)
	inv.TurnID = stdstrings.TrimSpace(inv.TurnID)
	inv.ToolName = stdstrings.TrimSpace(inv.ToolName)
	inv.ToolKind = stdstrings.TrimSpace(inv.ToolKind)
	inv.HostID = stdstrings.TrimSpace(inv.HostID)
	inv.WorkspaceID = stdstrings.TrimSpace(inv.WorkspaceID)
	inv.CallID = stdstrings.TrimSpace(inv.CallID)
	inv.RawArguments = stdstrings.TrimSpace(inv.RawArguments)
	if inv.Arguments == nil {
		inv.Arguments = make(map[string]any)
	}
}

// ToolExecutionResult describes the outcome of a tool invocation.
type ToolExecutionResult struct {
	InvocationID      string
	Status            ToolRunStatus
	OutputText        string
	OutputData        map[string]any
	LifecycleMessage  string
	ProjectionPayload map[string]any
	ErrorText         string
	EvidenceRefs      []string
	FinishedAt        time.Time
}

// Clone returns a shallow copy of the execution result with copied containers.
func (res ToolExecutionResult) Clone() ToolExecutionResult {
	if res.OutputData != nil {
		data := make(map[string]any, len(res.OutputData))
		for k, v := range res.OutputData {
			data[k] = v
		}
		res.OutputData = data
	}
	if res.ProjectionPayload != nil {
		payload := make(map[string]any, len(res.ProjectionPayload))
		for k, v := range res.ProjectionPayload {
			payload[k] = v
		}
		res.ProjectionPayload = payload
	}
	if res.EvidenceRefs != nil {
		res.EvidenceRefs = append([]string(nil), res.EvidenceRefs...)
	}
	return res
}
