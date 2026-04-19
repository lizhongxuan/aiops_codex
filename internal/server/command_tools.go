package server

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

const shellCommandToolName = "shell_command"

type shellCommandUnifiedTool struct {
	app *App
}

type shellCommandDisplayAdapter struct{}

func (a *App) shellCommandUnifiedTool() UnifiedTool {
	return shellCommandUnifiedTool{app: a}
}

func (t shellCommandUnifiedTool) Name() string { return shellCommandToolName }

func (t shellCommandUnifiedTool) Aliases() []string { return nil }

func (t shellCommandUnifiedTool) Description(ctx ToolDescriptionContext) string {
	hostID := strings.TrimSpace(ctx.HostID)
	if hostID == "" {
		return toolPromptDescription(shellCommandToolName)
	}
	hostID = defaultHostID(hostID)
	switch {
	case hostID == model.ServerLocalHostID:
		return localToolPromptDescription(shellCommandToolName)
	case isRemoteHostID(hostID):
		return remoteToolPromptDescription(shellCommandToolName)
	default:
		return toolPromptDescription(shellCommandToolName)
	}
}

func (t shellCommandUnifiedTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"host": map[string]any{
				"type":        "string",
				"description": "Required selected host ID.",
			},
			"command": map[string]any{
				"type":        "string",
				"description": "Direct command to execute.",
			},
			"cwd": map[string]any{
				"type":        "string",
				"description": "Optional working directory.",
			},
			"timeout_sec": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     600,
				"description": "Optional timeout in seconds.",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Why this command is needed.",
			},
		},
		"required":             []string{"host", "command", "reason"},
		"additionalProperties": false,
	}
}

func (t shellCommandUnifiedTool) Call(ctx context.Context, req ToolCallRequest) (ToolCallResult, error) {
	req.Normalize()
	args, err := parseExecToolArgs(req.Input)
	if err != nil {
		return ToolCallResult{}, err
	}
	if !shellCommandIsReadonly(args.Command) {
		return ToolCallResult{}, errors.New("shell_command mutations currently route through the existing approval flow")
	}

	hostID := defaultHostID(firstNonEmptyValue(args.HostID, req.Invocation.HostID))
	var execResult remoteExecResult
	if hostID == model.ServerLocalHostID {
		if t.app == nil {
			return ToolCallResult{}, errors.New("shell_command is not configured")
		}
		execResult, err = t.app.runLocalReadonlyExec(ctx, req.Invocation.SessionID, toolLifecycleCardID(req.Invocation), execSpec{
			Command:    args.Command,
			Cwd:        args.Cwd,
			TimeoutSec: args.TimeoutSec,
			Readonly:   true,
			ToolName:   shellCommandToolName,
		})
	} else {
		if t.app == nil {
			return ToolCallResult{}, errors.New("shell_command is not configured")
		}
		execResult, err = t.app.runRemoteExec(ctx, req.Invocation.SessionID, hostID, toolLifecycleCardID(req.Invocation), execSpec{
			Command:    args.Command,
			Cwd:        args.Cwd,
			TimeoutSec: args.TimeoutSec,
			Readonly:   true,
			ToolName:   shellCommandToolName,
		})
	}
	finalStatus := execResultCardStatus(execResult)
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return ToolCallResult{}, err
	}
	if finalStatus != "completed" {
		return ToolCallResult{}, errors.New(firstNonEmptyValue(strings.TrimSpace(execResult.Error), strings.TrimSpace(execResult.Message), formatExecToolResult(args.Command, execResult)))
	}

	display := commandResultDisplayPayload(args.Command, args.Cwd, execResult, finalStatus)
	return ToolCallResult{
		Output:            formatExecToolResult(args.Command, execResult),
		DisplayOutput:     display,
		StructuredContent: structuredShellCommandContent(hostID, args, execResult, finalStatus),
		Metadata: map[string]any{
			"skipCardProjection":      true,
			"trackActivityCompletion": false,
			"lifecycleMessage":        firstNonEmptyValue(display.Summary, "已执行命令"),
		},
	}, nil
}

func (t shellCommandUnifiedTool) CheckPermissions(_ context.Context, req ToolCallRequest) (PermissionResult, error) {
	req.Normalize()
	args, err := parseExecToolArgs(req.Input)
	if err != nil {
		return PermissionResult{}, err
	}
	hostID := defaultHostID(firstNonEmptyValue(args.HostID, req.Invocation.HostID))
	readonly := shellCommandIsReadonly(args.Command)
	if t.app != nil && isRemoteHostID(hostID) {
		if err := t.app.ensureCapabilityAllowedForHost(hostID, "commandExecution"); err != nil {
			return PermissionResult{}, err
		}
		decision, err := t.app.evaluateCommandPolicyForHost(hostID, args.Command)
		if err != nil {
			return PermissionResult{}, err
		}
		if readonly && decision.Mode != model.AgentPermissionModeApprovalRequired {
			return PermissionResult{Allowed: true, ApprovalType: "readonly"}, nil
		}
		return PermissionResult{
			Allowed:           false,
			RequiresApproval:  true,
			ApprovalType:      "execute_command",
			ApprovalDecisions: []string{"accept", "accept_session", "decline"},
		}, nil
	}
	if readonly {
		return PermissionResult{Allowed: true, ApprovalType: "readonly"}, nil
	}
	return PermissionResult{
		Allowed:           false,
		RequiresApproval:  true,
		ApprovalType:      "execute_command",
		ApprovalDecisions: []string{"accept", "accept_session", "decline"},
	}, nil
}

func (t shellCommandUnifiedTool) IsConcurrencySafe(req ToolCallRequest) bool {
	return t.IsReadOnly(req)
}

func (t shellCommandUnifiedTool) IsReadOnly(req ToolCallRequest) bool {
	req.Normalize()
	command := strings.TrimSpace(getStringAny(req.Input, "command"))
	if command == "" {
		command = strings.TrimSpace(composeCommandFromProgramArgs(req.Input))
	}
	return shellCommandIsReadonly(command)
}

func (t shellCommandUnifiedTool) IsDestructive(req ToolCallRequest) bool {
	return !t.IsReadOnly(req)
}

func (t shellCommandUnifiedTool) Display() ToolDisplayAdapter {
	return shellCommandDisplayAdapter{}
}

func (shellCommandDisplayAdapter) RenderUse(req ToolCallRequest) *ToolDisplayPayload {
	req.Normalize()
	command := strings.TrimSpace(getStringAny(req.Input, "command"))
	if command == "" {
		command = strings.TrimSpace(composeCommandFromProgramArgs(req.Input))
	}
	if command == "" {
		return nil
	}
	return &ToolDisplayPayload{
		Summary:  "准备执行命令：" + command,
		Activity: command,
		Blocks: []ToolDisplayBlock{{
			Kind:  ToolDisplayBlockCommand,
			Title: "命令",
			Text:  command,
		}},
	}
}

func (shellCommandDisplayAdapter) RenderProgress(ToolProgressEvent) *ToolDisplayPayload {
	return nil
}

func (shellCommandDisplayAdapter) RenderResult(ToolCallResult) *ToolDisplayPayload {
	return nil
}

func shellCommandIsReadonly(command string) bool {
	return validateReadonlyCommand(strings.TrimSpace(command)) == nil
}

func structuredShellCommandContent(hostID string, args execToolArgs, result remoteExecResult, finalStatus string) map[string]any {
	return map[string]any{
		"hostId":          hostID,
		"command":         args.Command,
		"cwd":             strings.TrimSpace(args.Cwd),
		"status":          finalStatus,
		"exitCode":        result.ExitCode,
		"stdoutLineCount": countMeaningfulLines(result.Stdout),
		"stderrLineCount": countMeaningfulLines(result.Stderr),
		"readonly":        shellCommandIsReadonly(args.Command),
	}
}

func commandResultDisplayPayload(command, cwd string, result remoteExecResult, finalStatus string) *ToolDisplayPayload {
	command = strings.TrimSpace(command)
	cwd = strings.TrimSpace(cwd)
	summary := commandDisplaySummary(command, finalStatus)
	blocks := []ToolDisplayBlock{
		{
			Kind:  ToolDisplayBlockCommand,
			Title: "命令",
			Text:  command,
		},
		{
			Kind:  ToolDisplayBlockResultStats,
			Title: "执行结果",
			Items: commandResultStatItems(cwd, result, finalStatus),
		},
	}
	if stdout := strings.TrimSpace(previewFileContent(result.Stdout, 20)); stdout != "" {
		blocks = append(blocks, ToolDisplayBlock{
			Kind:  ToolDisplayBlockText,
			Title: "stdout 摘要",
			Text:  stdout,
		})
	}
	if stderr := strings.TrimSpace(previewFileContent(firstNonEmptyValue(result.Stderr, result.Error, result.Message), 20)); stderr != "" {
		blocks = append(blocks, ToolDisplayBlock{
			Kind:  ToolDisplayBlockWarning,
			Title: "stderr 摘要",
			Text:  stderr,
		})
	}
	return &ToolDisplayPayload{
		Summary:  summary,
		Activity: command,
		Blocks:   blocks,
	}
}

func commandResultStatItems(cwd string, result remoteExecResult, finalStatus string) []map[string]any {
	items := []map[string]any{
		{"label": "状态", "value": finalStatus},
		{"label": "退出码", "value": strconv.Itoa(result.ExitCode)},
		{"label": "stdout 行", "value": strconv.Itoa(countMeaningfulLines(result.Stdout))},
		{"label": "stderr 行", "value": strconv.Itoa(countMeaningfulLines(result.Stderr))},
	}
	if cwd != "" {
		items = append(items, map[string]any{
			"label": "工作目录",
			"value": cwd,
		})
	}
	return items
}

func commandDisplaySummary(command, finalStatus string) string {
	command = strings.TrimSpace(command)
	switch strings.TrimSpace(finalStatus) {
	case "completed":
		return fmt.Sprintf("已执行命令：%s", command)
	case "cancelled":
		return fmt.Sprintf("命令已取消：%s", command)
	case "timeout":
		return fmt.Sprintf("命令已超时：%s", command)
	default:
		return fmt.Sprintf("命令执行失败：%s", command)
	}
}
