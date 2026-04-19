package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type queryAIServerStateUnifiedTool struct {
	app *App
}

type readonlyCommandUnifiedTool struct {
	app  *App
	name string
}

func (a *App) queryAIServerStateUnifiedTool() UnifiedTool {
	return queryAIServerStateUnifiedTool{app: a}
}

func (a *App) executeReadonlyQueryUnifiedTool() UnifiedTool {
	return readonlyCommandUnifiedTool{app: a, name: "execute_readonly_query"}
}

func (a *App) readonlyHostInspectUnifiedTool() UnifiedTool {
	return readonlyCommandUnifiedTool{app: a, name: "readonly_host_inspect"}
}

func (t queryAIServerStateUnifiedTool) Name() string { return "query_ai_server_state" }

func (t queryAIServerStateUnifiedTool) Aliases() []string { return nil }

func (t queryAIServerStateUnifiedTool) Description(ToolDescriptionContext) string {
	return toolPromptDescription("query_ai_server_state")
}

func (t queryAIServerStateUnifiedTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"focus": map[string]any{
				"type":        "string",
				"description": "Optional focus area for the workspace state query.",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Compatibility alias for focus.",
			},
			"topic": map[string]any{
				"type":        "string",
				"description": "Compatibility alias for focus.",
			},
		},
		"additionalProperties": true,
	}
}

func (t queryAIServerStateUnifiedTool) Call(_ context.Context, req ToolCallRequest) (ToolCallResult, error) {
	req.Normalize()
	if t.app == nil {
		return ToolCallResult{}, errors.New("query_ai_server_state is not configured")
	}

	cardID := toolLifecycleCardID(req.Invocation)
	state, focus, evidenceID := t.app.queryAIServerStateSnapshot(req.Invocation.SessionID, cardID, req.Input)
	stateJSON, err := json.Marshal(state)
	if err != nil {
		exec := ToolExecutionResult{
			InvocationID: req.Invocation.InvocationID,
			Status:       ToolRunStatusFailed,
			OutputText:   err.Error(),
			ErrorText:    "序列化状态快照失败：" + err.Error(),
			ProjectionPayload: map[string]any{
				"skipCardProjection":      true,
				"trackActivityCompletion": false,
			},
			FinishedAt: time.Now(),
		}
		return toolCallResultFromExecutionResult(exec), nil
	}

	evidenceRefs := []string(nil)
	if strings.TrimSpace(evidenceID) != "" {
		evidenceRefs = []string{strings.TrimSpace(evidenceID)}
	}
	exec := ToolExecutionResult{
		InvocationID: req.Invocation.InvocationID,
		Status:       ToolRunStatusCompleted,
		OutputText:   fmt.Sprintf("AI Server State (focus=%s):\n%s\n\n[evidence: %s]", focus, string(stateJSON), evidenceID),
		OutputData: map[string]any{
			"focus":            focus,
			"hostCount":        state["hostCount"],
			"pendingApprovals": state["pendingApprovals"],
			"cardCount":        state["cardCount"],
		},
		ProjectionPayload: map[string]any{
			"skipCardProjection":      true,
			"trackActivityCompletion": false,
		},
		EvidenceRefs: evidenceRefs,
		FinishedAt:   time.Now(),
	}
	return toolCallResultFromExecutionResult(exec), nil
}

func (t queryAIServerStateUnifiedTool) CheckPermissions(context.Context, ToolCallRequest) (PermissionResult, error) {
	return PermissionResult{Allowed: true, ApprovalType: "readonly"}, nil
}

func (t queryAIServerStateUnifiedTool) IsConcurrencySafe(ToolCallRequest) bool { return true }

func (t queryAIServerStateUnifiedTool) IsReadOnly(ToolCallRequest) bool { return true }

func (t queryAIServerStateUnifiedTool) IsDestructive(ToolCallRequest) bool { return false }

func (t queryAIServerStateUnifiedTool) Display() ToolDisplayAdapter { return nil }

func (t readonlyCommandUnifiedTool) Name() string { return t.name }

func (t readonlyCommandUnifiedTool) Aliases() []string { return nil }

func (t readonlyCommandUnifiedTool) Description(ToolDescriptionContext) string {
	return toolPromptDescription(t.name)
}

func (t readonlyCommandUnifiedTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"host": map[string]any{
				"type":        "string",
				"description": "Selected host ID. Use server-local for local inspection and the selected remote host for remote inspection.",
			},
			"command": map[string]any{
				"type":        "string",
				"description": "Read-only shell command to execute.",
			},
			"cwd": map[string]any{
				"type":        "string",
				"description": "Optional working directory.",
			},
			"timeout_sec": map[string]any{
				"type":        "integer",
				"description": "Optional timeout in seconds.",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Why this read-only command is needed.",
			},
		},
		"required":             []string{"command"},
		"additionalProperties": true,
	}
}

func (t readonlyCommandUnifiedTool) Call(ctx context.Context, req ToolCallRequest) (ToolCallResult, error) {
	req.Normalize()
	if t.app == nil {
		return ToolCallResult{}, fmt.Errorf("%s is not configured", t.name)
	}

	args, err := parseExecToolArgs(req.Input)
	if err != nil {
		return toolCallResultFromExecutionResult(readonlyCommandFailure(req.Invocation, err)), nil
	}
	hostID := defaultHostID(firstNonEmptyValue(args.HostID, req.Invocation.HostID))
	if hostID == "" {
		hostID = model.ServerLocalHostID
	}
	if t.name == "readonly_host_inspect" && strings.TrimSpace(args.Reason) == "" {
		return toolCallResultFromExecutionResult(readonlyCommandFailure(req.Invocation, errors.New("readonly_host_inspect requires a reason"))), nil
	}
	if err := validateReadonlyCommand(args.Command); err != nil {
		return toolCallResultFromExecutionResult(readonlyCommandFailure(req.Invocation, err)), nil
	}
	if t.name == "execute_readonly_query" {
		if err := t.app.ensureCapabilityAllowedForHost(hostID, "commandExecution"); err != nil {
			return toolCallResultFromExecutionResult(readonlyCommandFailure(req.Invocation, err)), nil
		}
	}

	cardID := toolLifecycleCardID(req.Invocation)
	var (
		execResult remoteExecResult
		runErr     error
	)
	switch {
	case !isRemoteHostID(hostID):
		if t.name == "readonly_host_inspect" {
			if err := t.app.ensureCapabilityAllowedForHost(hostID, "commandExecution"); err != nil {
				return toolCallResultFromExecutionResult(readonlyCommandFailure(req.Invocation, err)), nil
			}
		}
		execResult, runErr = t.app.runLocalReadonlyExec(ctx, req.Invocation.SessionID, cardID, execSpec{
			Command:    args.Command,
			Cwd:        args.Cwd,
			TimeoutSec: clampExecTimeout(args.TimeoutSec, true),
			Readonly:   true,
			ToolName:   t.name,
		})
	default:
		if t.name == "readonly_host_inspect" {
			selectedHostID := defaultHostID(t.app.sessionHostID(req.Invocation.SessionID))
			if hostID != selectedHostID {
				return toolCallResultFromExecutionResult(readonlyCommandFailure(req.Invocation, fmt.Errorf("readonly_host_inspect host %s does not match selected host %s", hostID, selectedHostID))), nil
			}
			if host := t.app.findHost(hostID); host.Status != "online" || !host.Executable {
				return toolCallResultFromExecutionResult(readonlyCommandFailure(req.Invocation, fmt.Errorf("selected host %s is offline or not executable", hostID))), nil
			}
			if err := t.app.ensureCapabilityAllowedForHost(hostID, "commandExecution"); err != nil {
				return toolCallResultFromExecutionResult(readonlyCommandFailure(req.Invocation, err)), nil
			}
		}
		execResult, runErr = t.app.runRemoteExec(ctx, req.Invocation.SessionID, hostID, cardID, execSpec{
			Command:    args.Command,
			Cwd:        args.Cwd,
			TimeoutSec: clampExecTimeout(args.TimeoutSec, true),
			Readonly:   true,
			ToolName:   t.name,
		})
	}
	exec := toolExecutionResultFromExec(req.Invocation, args.Command, execResult, runErr)
	return toolCallResultFromExecutionResult(exec), nil
}

func (t readonlyCommandUnifiedTool) CheckPermissions(context.Context, ToolCallRequest) (PermissionResult, error) {
	return PermissionResult{Allowed: true, ApprovalType: "readonly"}, nil
}

func (t readonlyCommandUnifiedTool) IsConcurrencySafe(ToolCallRequest) bool { return true }

func (t readonlyCommandUnifiedTool) IsReadOnly(ToolCallRequest) bool { return true }

func (t readonlyCommandUnifiedTool) IsDestructive(ToolCallRequest) bool { return false }

func (t readonlyCommandUnifiedTool) Display() ToolDisplayAdapter { return nil }

func readonlyCommandFailure(inv ToolInvocation, err error) ToolExecutionResult {
	text := strings.TrimSpace(errorString(err))
	if text == "" {
		text = "tool execution failed"
	}
	return ToolExecutionResult{
		InvocationID: inv.InvocationID,
		Status:       ToolRunStatusFailed,
		OutputText:   text,
		ErrorText:    text,
		ProjectionPayload: map[string]any{
			"skipCardProjection":      true,
			"trackActivityCompletion": false,
		},
		FinishedAt: time.Now(),
	}
}
