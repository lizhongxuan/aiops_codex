package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func (a *App) registerDefaultToolHandlers() {
	if a == nil || a.toolHandlerRegistry == nil {
		return
	}
	a.toolHandlerRegistry.MustRegisterUnifiedTool(a.queryAIServerStateUnifiedTool())
	a.toolHandlerRegistry.MustRegisterUnifiedTool(a.webSearchUnifiedTool())
	a.toolHandlerRegistry.MustRegisterUnifiedTool(a.webFetchUnifiedTool())
	a.toolHandlerRegistry.MustRegisterUnifiedTool(a.findInPageUnifiedTool())
	a.toolHandlerRegistry.MustRegisterUnifiedTool(a.shellCommandUnifiedTool())
	a.toolHandlerRegistry.MustRegisterUnifiedTool(a.skillContextUnifiedTool())
	a.toolHandlerRegistry.MustRegisterUnifiedTool(a.remoteFileListUnifiedTool())
	a.toolHandlerRegistry.MustRegisterUnifiedTool(a.remoteFileReadUnifiedTool())
	a.toolHandlerRegistry.MustRegisterUnifiedTool(a.remoteFileSearchUnifiedTool())
	a.toolHandlerRegistry.MustRegisterUnifiedTool(a.remoteFileWriteUnifiedTool())
	a.toolHandlerRegistry.MustRegisterUnifiedTool(a.hostFileReadUnifiedTool())
	a.toolHandlerRegistry.MustRegisterUnifiedTool(a.hostFileSearchUnifiedTool())
	a.toolHandlerRegistry.MustRegisterUnifiedTool(a.executeReadonlyQueryUnifiedTool())
	a.toolHandlerRegistry.MustRegisterUnifiedTool(a.readonlyHostInspectUnifiedTool())
}

func dynamicDispatcherInvocation(sessionID, hostID string, params dynamicToolCallParams) ToolInvocation {
	args := cloneAnyMap(params.Arguments)
	if args == nil {
		args = make(map[string]any)
	}
	processCardID := "process-" + dynamicToolCardID(params.CallID)
	if usesInlineDynamicReadonlyCard(params.Tool) {
		processCardID = dynamicToolCardID(params.CallID)
		args["skipCardProjection"] = true
		args["trackActivityStart"] = false
	}
	args["processCardId"] = processCardID
	if strings.TrimSpace(getStringAny(args, "hostId", "host_id")) == "" && strings.TrimSpace(hostID) != "" {
		args["hostId"] = strings.TrimSpace(hostID)
	}
	return ToolInvocation{
		InvocationID: model.NewID("toolinv"),
		SessionID:    sessionID,
		ThreadID:     strings.TrimSpace(params.ThreadID),
		TurnID:       strings.TrimSpace(params.TurnID),
		ToolName:     strings.TrimSpace(params.Tool),
		ToolKind:     "dynamic",
		Source:       ToolInvocationSourceDynamicToolCall,
		HostID:       defaultHostID(hostID),
		CallID:       strings.TrimSpace(params.CallID),
		Arguments:    args,
		ReadOnly:     true,
		StartedAt:    time.Now(),
	}
}

func (a *App) dispatchDynamicReadonlyTool(sessionID, hostID, rawID string, params dynamicToolCallParams) {
	if a == nil || a.toolDispatcher == nil {
		_ = a.respondCodex(context.Background(), rawID, toolResponse("tool dispatcher is not configured", false))
		return
	}

	result, err := a.toolDispatcher.Dispatch(context.Background(), dynamicDispatcherInvocation(sessionID, hostID, params))
	if err != nil {
		_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
		return
	}
	if a.turnWasInterrupted(sessionID) {
		return
	}
	if result.Status == ToolRunStatusCancelled {
		return
	}

	success := result.Status == ToolRunStatusCompleted
	text := strings.TrimSpace(result.OutputText)
	if success {
		if text == "" {
			text = "工具已完成"
		}
	} else {
		text = firstNonEmptyValue(text, strings.TrimSpace(result.ErrorText), "tool execution failed")
	}
	a.broadcastSnapshot(sessionID)
	_ = a.respondCodex(context.Background(), rawID, toolResponse(text, success))
}

func toolExecutionResultFromExec(inv ToolInvocation, command string, execResult remoteExecResult, err error) ToolExecutionResult {
	status := ToolRunStatusCompleted
	if execResult.Status == "cancelled" {
		status = ToolRunStatusCancelled
	} else if execResultCardStatus(execResult) != "completed" {
		status = ToolRunStatusFailed
	}
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && strings.TrimSpace(execResult.Error) == "" {
		execResult.Error = strings.TrimSpace(err.Error())
	}
	return ToolExecutionResult{
		InvocationID: inv.InvocationID,
		Status:       status,
		OutputText:   formatExecToolResult(command, execResult),
		OutputData: map[string]any{
			"status":    execResult.Status,
			"exitCode":  execResult.ExitCode,
			"timeout":   execResult.Timeout,
			"cancelled": execResult.Cancelled,
		},
		ErrorText: firstNonEmptyValue(strings.TrimSpace(execResult.Error), strings.TrimSpace(execResult.Message), errorString(err)),
		ProjectionPayload: map[string]any{
			"skipCardProjection":      true,
			"trackActivityCompletion": false,
		},
		FinishedAt: time.Now(),
	}
}

func usesInlineDynamicReadonlyCard(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case shellCommandToolName, "execute_readonly_query", "readonly_host_inspect", "query_ai_server_state":
		return true
	default:
		return false
	}
}
