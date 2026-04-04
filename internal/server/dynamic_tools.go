package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/orchestrator"
)

type dynamicToolCallParams struct {
	ThreadID  string         `json:"threadId"`
	TurnID    string         `json:"turnId"`
	CallID    string         `json:"callId"`
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
}

type execToolArgs struct {
	HostID     string
	Command    string
	Cwd        string
	Reason     string
	TimeoutSec int
	Mode       string
}

type remoteListFilesArgs struct {
	HostID     string `json:"host"`
	Path       string `json:"path"`
	Recursive  bool   `json:"recursive"`
	MaxEntries int    `json:"max_entries"`
	Reason     string `json:"reason"`
}

type remoteReadFileArgs struct {
	HostID   string `json:"host"`
	Path     string `json:"path"`
	MaxBytes int    `json:"max_bytes"`
	Reason   string `json:"reason"`
}

type remoteSearchFilesArgs struct {
	HostID     string `json:"host"`
	Path       string `json:"path"`
	Query      string `json:"query"`
	MaxMatches int    `json:"max_matches"`
	Reason     string `json:"reason"`
}

type remoteFileChangeArgs struct {
	HostID    string `json:"host"`
	Mode      string `json:"mode"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	WriteMode string `json:"write_mode"`
	Reason    string `json:"reason"`
}

func (a *App) remoteDynamicTools() []map[string]any {
	tools := make([]map[string]any, 0, 5)
	commandState := mergeCapabilityStates(
		a.mainAgentProfile().CapabilityPermissions.CommandExecution,
		a.hostAgentDefaultProfile().CapabilityPermissions.CommandExecution,
	)
	fileReadState := mergeCapabilityStates(
		a.mainAgentProfile().CapabilityPermissions.FileRead,
		a.hostAgentDefaultProfile().CapabilityPermissions.FileRead,
	)
	fileSearchState := mergeCapabilityStates(
		a.mainAgentProfile().CapabilityPermissions.FileSearch,
		a.hostAgentDefaultProfile().CapabilityPermissions.FileSearch,
	)
	fileChangeState := mergeCapabilityStates(
		a.mainAgentProfile().CapabilityPermissions.FileChange,
		a.hostAgentDefaultProfile().CapabilityPermissions.FileChange,
	)
	if !capabilityDisabled(commandState) {
		tools = append(tools, map[string]any{
			"name":        "execute_readonly_query",
			"description": "Run a read-only shell command on the currently selected remote host. Use it for inspection only, such as uptime, df, ps, ss, systemctl status, cat, grep, tail, find, journalctl, or simple read-only pipelines. Never use it for installs, restarts, file writes, deletes, or process signals.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"host": map[string]any{
						"type":        "string",
						"description": "Required selected remote host ID. Must exactly match the current selected host.",
					},
					"command": map[string]any{
						"type":        "string",
						"description": "Read-only shell command to run on the selected remote host.",
					},
					"cwd": map[string]any{
						"type":        "string",
						"description": "Optional working directory on the selected remote host.",
					},
					"timeout_sec": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"maximum":     120,
						"description": "Optional timeout in seconds.",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "One-sentence explanation of what you are checking.",
					},
				},
				"required":             []string{"host", "command", "reason"},
				"additionalProperties": false,
			},
		})
	}
	if !capabilityDisabled(fileReadState) || !capabilityDisabled(fileSearchState) {
		tools = append(tools, map[string]any{
			"name":        "list_remote_files",
			"description": "List files or directories on the currently selected remote host. Prefer this over shell commands when you need to inspect a directory tree.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"host": map[string]any{
						"type":        "string",
						"description": "Required selected remote host ID. Must exactly match the current selected host.",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Directory path to inspect on the selected remote host.",
					},
					"recursive": map[string]any{
						"type":        "boolean",
						"description": "Whether to recursively list descendant entries.",
					},
					"max_entries": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"maximum":     500,
						"description": "Maximum number of entries to return.",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "One-sentence explanation of what you are trying to inspect.",
					},
				},
				"required":             []string{"host", "path", "reason"},
				"additionalProperties": false,
			},
		})
	}
	if !capabilityDisabled(fileReadState) {
		tools = append(tools, map[string]any{
			"name":        "read_remote_file",
			"description": "Read a file from the currently selected remote host. Prefer this over shell cat/sed when you need file contents.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"host": map[string]any{
						"type":        "string",
						"description": "Required selected remote host ID. Must exactly match the current selected host.",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Absolute or relative file path on the selected remote host.",
					},
					"max_bytes": map[string]any{
						"type":        "integer",
						"minimum":     256,
						"maximum":     262144,
						"description": "Optional maximum bytes to read.",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "One-sentence explanation of what you are checking in this file.",
					},
				},
				"required":             []string{"host", "path", "reason"},
				"additionalProperties": false,
			},
		})
	}
	if !capabilityDisabled(fileSearchState) {
		tools = append(tools, map[string]any{
			"name":        "search_remote_files",
			"description": "Search for text in files on the currently selected remote host. Prefer this over grep when you need structured search results.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"host": map[string]any{
						"type":        "string",
						"description": "Required selected remote host ID. Must exactly match the current selected host.",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "File or directory path to search.",
					},
					"query": map[string]any{
						"type":        "string",
						"description": "Text to search for.",
					},
					"max_matches": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"maximum":     200,
						"description": "Maximum number of matches to return.",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "One-sentence explanation of what you are searching for.",
					},
				},
				"required":             []string{"host", "path", "query", "reason"},
				"additionalProperties": false,
			},
		})
	}
	if !capabilityDisabled(commandState) || !capabilityDisabled(fileChangeState) {
		tools = append(tools, map[string]any{
			"name":        "execute_system_mutation",
			"description": "Run a shell command that changes system state on the currently selected remote host. Use it for installs, service restarts, file edits, starting or stopping processes, or any write operation. This tool always requires user approval before execution.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"host": map[string]any{
						"type":        "string",
						"description": "Required selected remote host ID. Must exactly match the current selected host.",
					},
					"mode": map[string]any{
						"type":        "string",
						"enum":        []string{"command", "file_change"},
						"description": "Use command mode for shell-based mutations, or file_change mode for direct file editing.",
					},
					"command": map[string]any{
						"type":        "string",
						"description": "Shell command to run after the user approves it.",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Target file path when mode=file_change.",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "Final file content to write when mode=file_change.",
					},
					"write_mode": map[string]any{
						"type":        "string",
						"enum":        []string{"overwrite", "append"},
						"description": "Optional file write mode when mode=file_change. Use append to append content to an existing file.",
					},
					"cwd": map[string]any{
						"type":        "string",
						"description": "Optional working directory on the selected remote host.",
					},
					"timeout_sec": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"maximum":     600,
						"description": "Optional timeout in seconds.",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "Short explanation of why this change is needed.",
					},
				},
				"required":             []string{"host", "mode", "reason"},
				"additionalProperties": false,
			},
		})
	}
	// Merge structured read tools (host.*) into the dynamic tool list.
	if !capabilityDisabled(commandState) {
		tools = append(tools, structuredReadToolDefinitions()...)
	}
	// Merge controlled mutation tools (service.*, config.*, package.*) into the dynamic tool list.
	if !capabilityDisabled(commandState) || !capabilityDisabled(fileChangeState) {
		tools = append(tools, controlledMutationToolDefinitions()...)
	}
	return tools
}

func (a *App) workspaceDynamicTools(sessionID string) []map[string]any {
	tools := []map[string]any{
		workspaceStateQueryDynamicTool(),
		{
			"name":        "orchestrator_dispatch_tasks",
			"description": "Submit structured host tasks to the ai-server orchestrator from the main workspace session. Use this after you finish planning and have per-host execution tasks ready.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"missionTitle": map[string]any{
						"type":        "string",
						"description": "Optional mission title shown in the workspace projection.",
					},
					"summary": map[string]any{
						"type":        "string",
						"description": "Optional mission summary shown in the workspace projection.",
					},
					"tasks": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"taskId": map[string]any{
									"type":        "string",
									"description": "Stable task identifier.",
								},
								"hostId": map[string]any{
									"type":        "string",
									"description": "Target host ID. Must be an online executable remote host.",
								},
								"title": map[string]any{
									"type":        "string",
									"description": "Short task title.",
								},
								"instruction": map[string]any{
									"type":        "string",
									"description": "Concrete worker instruction for the target host.",
								},
								"constraints": map[string]any{
									"type":        "array",
									"items":       map[string]any{"type": "string"},
									"description": "Optional flat constraint list.",
								},
								"externalNodeId": map[string]any{
									"type":        "string",
									"description": "Optional node identifier for debugging.",
								},
							},
							"required":             []string{"taskId", "hostId", "instruction"},
							"additionalProperties": false,
						},
						"minItems":    1,
						"description": "Structured task list grouped by target host.",
					},
				},
				"required":             []string{"tasks"},
				"additionalProperties": false,
			},
		},
	}
	session := a.store.Session(sessionID)
	selectedHostID := defaultHostID("")
	if session != nil {
		selectedHostID = defaultHostID(session.SelectedHostID)
	}
	if isRemoteHostID(selectedHostID) {
		tools = append(tools, a.workspaceReadonlyRemoteDynamicTools()...)
	}
	return tools
}

func workspaceStateQueryDynamicTool() map[string]any {
	return map[string]any{
		"name":        "query_ai_server_state",
		"description": "Read the current ai-server workspace/session/host state for questions about online hosts, mission progress, pending approvals, runtime phase, or other project-local status. Use this before any filesystem inspection when the user asks about current state.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"focus": map[string]any{
					"type":        "string",
					"enum":        []string{"summary", "hosts", "runtime", "approvals", "plan", "all"},
					"description": "Optional area to emphasize in the returned ai-server state snapshot.",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "Short explanation of what state you are checking for the user.",
				},
			},
			"additionalProperties": false,
		},
	}
}

func (a *App) workspaceRouteDynamicTools() []map[string]any {
	return []map[string]any{
		workspaceStateQueryDynamicTool(),
	}
}

func (a *App) workspaceDirectDynamicTools(sessionID string) []map[string]any {
	tools := []map[string]any{
		workspaceStateQueryDynamicTool(),
	}
	session := a.store.Session(sessionID)
	selectedHostID := defaultHostID("")
	if session != nil {
		selectedHostID = defaultHostID(session.SelectedHostID)
	}
	if isRemoteHostID(selectedHostID) {
		tools = append(tools, a.workspaceReadonlyRemoteDynamicTools()...)
	}
	return tools
}

func (a *App) workspaceReadonlyRemoteDynamicTools() []map[string]any {
	readonlyTools := make([]map[string]any, 0, 4)
	for _, tool := range a.remoteDynamicTools() {
		name := strings.TrimSpace(getStringAny(tool, "name"))
		if name == "" || name == "execute_system_mutation" {
			continue
		}
		readonlyTools = append(readonlyTools, tool)
	}
	return readonlyTools
}

func isWorkspaceReadonlyRemoteTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "execute_readonly_query", "list_remote_files", "read_remote_file", "search_remote_files":
		return true
	default:
		return false
	}
}

func remoteThreadDeveloperInstructions(selectedHostID string) string {
	return fmt.Sprintf(strings.TrimSpace(`
You are embedded inside a web AI ops console.
The selected target host for this thread is %q.
This host is remote. Do not use built-in local commandExecution or fileChange tools, because those affect the ai-server machine rather than the selected remote host.
Every remote tool call must include host=%q exactly. Never omit it and never substitute another host ID.
Use list_remote_files, read_remote_file, and search_remote_files for remote filesystem inspection.
Use execute_readonly_query for general read-only system inspection that is not a file browse operation.
Use execute_system_mutation(mode=command) for state-changing commands and execute_system_mutation(mode=file_change) for direct file edits.
Keep each tool call narrow, explain what you are checking, and summarize results clearly for the web UI.
`), selectedHostID, selectedHostID)
}

func remoteTurnDeveloperInstructions(hostID string) string {
	return fmt.Sprintf(
		"Current selected host is %s. This is a remote host. Do not use local built-in commandExecution or fileChange tools. Every remote tool call must include host=%s exactly. Prefer list_remote_files, read_remote_file, and search_remote_files for filesystem inspection. Use execute_readonly_query for other read-only checks, execute_system_mutation(mode=command) for state-changing commands, and execute_system_mutation(mode=file_change) for remote file edits on the selected host only.",
		hostID, hostID,
	)
}

func isRemoteHostID(hostID string) bool {
	return strings.TrimSpace(hostID) != "" && hostID != model.ServerLocalHostID
}

func dynamicToolCardID(callID string) string {
	return "toolcmd-" + strings.TrimSpace(callID)
}

func remoteToolTargetHost(arguments map[string]any) string {
	return strings.TrimSpace(getStringAny(arguments, "host", "hostId"))
}

func validateSelectedRemoteToolHost(arguments map[string]any, selectedHostID string) error {
	targetHostID := remoteToolTargetHost(arguments)
	if targetHostID == "" {
		return fmt.Errorf("tool requires host and it must equal selected host %s", selectedHostID)
	}
	if targetHostID != selectedHostID {
		return fmt.Errorf("tool host %s does not match selected host %s", targetHostID, selectedHostID)
	}
	return nil
}

func (a *App) handleDynamicToolCall(rawID string, payload map[string]any) {
	var params dynamicToolCallParams
	if err := remarshalInto(payload, &params); err != nil {
		_ = a.respondCodex(context.Background(), rawID, toolResponse("Dynamic tool payload was invalid.", false))
		return
	}

	sessionID := a.sessionIDFromPayload(payload)
	if sessionID == "" {
		_ = a.codex.RespondError(context.Background(), rawID, -32000, "session not found for dynamic tool call")
		return
	}
	a.bindTurnToSession(sessionID, payload)

	session := a.store.Session(sessionID)
	if session == nil {
		_ = a.codex.RespondError(context.Background(), rawID, -32000, "session not found for dynamic tool call")
		return
	}
	switch a.sessionKind(sessionID) {
	case model.SessionKindPlanner, model.SessionKindWorkspace:
		if a.handleOrchestratorDynamicToolCall(rawID, payload, params, sessionID) {
			return
		}
	}

	// Route coroot.* tools — these don't require a remote host selection.
	if isCorootTool(params.Tool) {
		a.executeCorootTool(sessionID, rawID, params)
		return
	}

	hostID := defaultHostID(session.SelectedHostID)
	if !isRemoteHostID(hostID) {
		_ = a.respondCodex(context.Background(), rawID, toolResponse("The selected host is server-local. Use Codex built-in local tools instead of remote execute_* tools.", false))
		return
	}
	if a.sessionKind(sessionID) == model.SessionKindWorkspace && !isWorkspaceReadonlyRemoteTool(params.Tool) && !isStructuredReadTool(params.Tool) {
		_ = a.respondCodex(context.Background(), rawID, toolResponse("Workspace 主 Agent 只允许直接调用只读远程工具；任何变更都必须通过 worker 派发。", false))
		return
	}
	if err := validateSelectedRemoteToolHost(params.Arguments, hostID); err != nil {
		_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
		return
	}

	// Route host.* structured read tools before the legacy switch.
	if isStructuredReadTool(params.Tool) {
		if err := a.ensureCapabilityAllowedForHost(hostID, "commandExecution"); err != nil {
			_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
			return
		}
		a.executeStructuredReadTool(sessionID, hostID, rawID, params)
		return
	}

	// Route controlled mutation tools (service.*, config.*, package.*) — always require approval.
	if isControlledMutationTool(params.Tool) {
		gw := a.evaluateCapabilityGateway(hostID, params.Tool)
		if !gw.Allowed {
			_ = a.respondCodex(context.Background(), rawID, toolResponse(gw.Reason, false))
			return
		}
		a.executeControlledMutationTool(sessionID, hostID, rawID, params)
		return
	}

	switch params.Tool {
	case "execute_readonly_query":
		if err := a.ensureCapabilityAllowedForHost(hostID, "commandExecution"); err != nil {
			_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
			return
		}
		args, err := parseExecToolArgs(params.Arguments)
		if err != nil {
			_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
			return
		}
		decision, err := a.evaluateCommandPolicyForHost(hostID, args.Command)
		if err != nil {
			_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
			return
		}
		if err := validateReadonlyCommand(args.Command); err != nil {
			_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
			return
		}
		if decision.Mode == model.AgentPermissionModeApprovalRequired {
			a.requestRemoteCommandApproval(sessionID, hostID, rawID, params, args, true)
			return
		}
		a.executeReadonlyDynamicTool(sessionID, hostID, rawID, params, args)
	case "list_remote_files":
		if capabilityDisabled(a.effectiveCapabilityState(hostID, "fileRead")) && capabilityDisabled(a.effectiveCapabilityState(hostID, "fileSearch")) {
			_ = a.respondCodex(context.Background(), rawID, toolResponse("list_remote_files is disabled by the current effective agent profile", false))
			return
		}
		args, err := parseRemoteListFilesArgs(params.Arguments)
		if err != nil {
			_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
			return
		}
		a.executeRemoteListFilesTool(sessionID, hostID, rawID, params, args)
	case "read_remote_file":
		if err := a.ensureCapabilityAllowedForHost(hostID, "fileRead"); err != nil {
			_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
			return
		}
		args, err := parseRemoteReadFileArgs(params.Arguments)
		if err != nil {
			_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
			return
		}
		a.executeRemoteReadFileTool(sessionID, hostID, rawID, params, args)
	case "search_remote_files":
		if err := a.ensureCapabilityAllowedForHost(hostID, "fileSearch"); err != nil {
			_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
			return
		}
		args, err := parseRemoteSearchFilesArgs(params.Arguments)
		if err != nil {
			_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
			return
		}
		a.executeRemoteSearchFilesTool(sessionID, hostID, rawID, params, args)
	case "execute_system_mutation":
		mode := strings.TrimSpace(getString(params.Arguments, "mode"))
		switch mode {
		case "command":
			if err := a.ensureCapabilityAllowedForHost(hostID, "commandExecution"); err != nil {
				_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
				return
			}
			args, err := parseExecToolArgs(params.Arguments)
			if err != nil {
				_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
				return
			}
			a.requestRemoteCommandApproval(sessionID, hostID, rawID, params, args, false)
		case "file_change":
			if err := a.ensureCapabilityAllowedForHost(hostID, "fileChange"); err != nil {
				_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
				return
			}
			if err := validateRemoteFileChangeArguments(params.Arguments); err != nil {
				_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
				return
			}
			args, err := parseRemoteFileChangeArgs(params.Arguments)
			if err != nil {
				_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
				return
			}
			a.requestRemoteFileChangeApproval(sessionID, hostID, rawID, params, args)
		default:
			_ = a.respondCodex(context.Background(), rawID, toolResponse("execute_system_mutation requires mode=command or mode=file_change.", false))
			return
		}
	default:
		_ = a.respondCodex(context.Background(), rawID, toolResponse("Unknown dynamic tool request.", false))
	}
}

func (a *App) handleOrchestratorDynamicToolCall(rawID string, _ map[string]any, params dynamicToolCallParams, sessionID string) bool {
	switch params.Tool {
	case "query_ai_server_state":
		a.handleWorkspaceQueryAIServerState(rawID, sessionID, params.Arguments)
		return true
	case "orchestrator_dispatch_tasks":
		a.handleWorkspaceDispatchTasks(rawID, sessionID, params.Arguments)
		return true
	default:
		return false
	}
}

func (a *App) handleWorkspaceQueryAIServerState(rawID, sessionID string, arguments map[string]any) {
	if a.orchestrator == nil {
		_ = a.respondCodex(context.Background(), rawID, toolResponse("orchestrator 未初始化。", false))
		return
	}
	mission, ok := a.resolveOrchestratorMission(sessionID)
	if !ok || mission == nil {
		if a.sessionKind(sessionID) != model.SessionKindWorkspace {
			_ = a.respondCodex(context.Background(), rawID, toolResponse("当前会话没有关联 workspace mission。", false))
			return
		}
	}
	focus := strings.TrimSpace(getStringAny(arguments, "focus"))
	workspaceSessionID := strings.TrimSpace(sessionID)
	if mission != nil && strings.TrimSpace(mission.WorkspaceSessionID) != "" {
		workspaceSessionID = strings.TrimSpace(mission.WorkspaceSessionID)
	}
	_ = a.respondCodex(context.Background(), rawID, toolResponse(a.renderPlannerAIServerState(workspaceSessionID, mission, focus), true))
}

func (a *App) renderPlannerAIServerState(workspaceSessionID string, mission *orchestrator.Mission, focus string) string {
	snapshot := a.snapshot(workspaceSessionID)
	selectedHostID := defaultHostID(snapshot.SelectedHostID)

	remoteOnline := make([]string, 0)
	remoteOffline := make([]string, 0)
	localState := "unknown"
	for _, host := range snapshot.Hosts {
		hostID := defaultHostID(host.ID)
		if hostID == model.ServerLocalHostID {
			if strings.TrimSpace(host.Status) != "" {
				localState = strings.TrimSpace(host.Status)
			}
			continue
		}
		label := hostDisplayName(host)
		if label == "" {
			label = hostID
		}
		if strings.TrimSpace(host.Status) == "online" {
			remoteOnline = append(remoteOnline, label)
		} else {
			remoteOffline = append(remoteOffline, label)
		}
	}
	slices.Sort(remoteOnline)
	slices.Sort(remoteOffline)

	pendingApprovals := make([]string, 0)
	for _, approval := range snapshot.Approvals {
		if strings.TrimSpace(approval.Status) != "pending" {
			continue
		}
		hostLabel := strings.TrimSpace(approval.HostID)
		if hostLabel == "" {
			hostLabel = "unknown-host"
		}
		commandLabel := strings.TrimSpace(approval.Command)
		if commandLabel == "" {
			commandLabel = strings.TrimSpace(approval.Reason)
		}
		if commandLabel == "" {
			commandLabel = "待确认操作"
		}
		pendingApprovals = append(pendingApprovals, fmt.Sprintf("%s: %s", hostLabel, truncate(commandLabel, 120)))
	}

	taskTotal := 0
	taskRunning := 0
	taskWaitingApproval := 0
	taskCompleted := 0
	taskFailed := 0
	activeWorkers := make([]string, 0)
	if mission != nil {
		taskTotal = len(mission.Tasks)
		for _, task := range mission.Tasks {
			if task == nil {
				continue
			}
			switch task.Status {
			case orchestrator.TaskStatusRunning, orchestrator.TaskStatusDispatching, orchestrator.TaskStatusReady, orchestrator.TaskStatusQueued:
				taskRunning++
			case orchestrator.TaskStatusWaitingApproval:
				taskWaitingApproval++
			case orchestrator.TaskStatusCompleted:
				taskCompleted++
			case orchestrator.TaskStatusFailed:
				taskFailed++
			}
		}
		for hostID, worker := range mission.Workers {
			if worker == nil {
				continue
			}
			switch worker.Status {
			case orchestrator.WorkerStatusRunning, orchestrator.WorkerStatusDispatching, orchestrator.WorkerStatusQueued, orchestrator.WorkerStatusWaiting:
				activeWorkers = append(activeWorkers, fmt.Sprintf("%s(%s)", hostID, workerStatusLabel(worker.Status)))
			}
		}
		slices.Sort(activeWorkers)
	}

	lines := []string{
		fmt.Sprintf("主 Agent 当前状态快照（focus=%s）", firstNonEmptyValue(focus, "summary")),
		fmt.Sprintf("workspaceSession=%s selectedHost=%s", workspaceSessionID, selectedHostID),
		fmt.Sprintf("runtime phase=%s", firstNonEmptyValue(strings.TrimSpace(snapshot.Runtime.Turn.Phase), "idle")),
	}

	if mission != nil {
		lines = append(lines,
			fmt.Sprintf("mission=%s status=%s", mission.ID, missionStatusLabel(mission.Status)),
			fmt.Sprintf("task summary: total=%d running_or_queued=%d waiting_approval=%d completed=%d failed=%d", taskTotal, taskRunning, taskWaitingApproval, taskCompleted, taskFailed),
		)
	}

	switch focus {
	case "hosts":
		lines = append(lines,
			fmt.Sprintf("remote online hosts (%d): %s", len(remoteOnline), joinOrDash(remoteOnline)),
			fmt.Sprintf("remote offline hosts (%d): %s", len(remoteOffline), joinOrDash(remoteOffline)),
			fmt.Sprintf("local host server-local: %s", localState),
		)
	case "approvals":
		lines = append(lines, fmt.Sprintf("pending approvals (%d): %s", len(pendingApprovals), joinOrDash(pendingApprovals)))
	case "runtime":
		lines = append(lines, fmt.Sprintf("active workers (%d): %s", len(activeWorkers), joinOrDash(activeWorkers)))
	case "plan":
		if mission != nil {
			planDetail := a.buildWorkspacePlanDetail(mission)
			lines = append(lines,
				fmt.Sprintf("plan goal: %s", firstNonEmptyValue(strings.TrimSpace(planDetail.Goal), strings.TrimSpace(mission.Summary), "-")),
				fmt.Sprintf("plan version: %s", firstNonEmptyValue(strings.TrimSpace(planDetail.Version), "plan-v1")),
			)
		}
	default:
		lines = append(lines,
			fmt.Sprintf("remote online hosts (%d): %s", len(remoteOnline), joinOrDash(remoteOnline)),
			fmt.Sprintf("pending approvals (%d): %s", len(pendingApprovals), joinOrDash(pendingApprovals)),
			fmt.Sprintf("active workers (%d): %s", len(activeWorkers), joinOrDash(activeWorkers)),
		)
	}

	lines = append(lines, "这是 ai-server 的当前投影结果；回答当前状态问题时优先基于这份信息，不要再去遍历目录或远程主机。")
	return strings.Join(lines, "\n")
}

func joinOrDash(items []string) string {
	if len(items) == 0 {
		return "-"
	}
	return strings.Join(items, ", ")
}

func (a *App) dispatchOrchestratorTasks(sessionID string, req orchestrator.DispatchRequest) (*orchestrator.DispatchResult, error) {
	if a.orchestrator == nil {
		return nil, errors.New("orchestrator 未初始化")
	}
	mission, ok := a.resolveOrchestratorMission(sessionID)
	if !ok || mission == nil {
		return nil, errors.New("当前会话没有关联 mission")
	}
	req.MissionID = mission.ID
	if len(req.Tasks) == 0 {
		return nil, errors.New("dispatch tasks 不能为空")
	}
	for _, task := range req.Tasks {
		host := a.findHost(task.HostID)
		switch {
		case strings.TrimSpace(task.HostID) == "":
			return nil, errors.New("所有任务都必须提供 hostId")
		case task.HostID == model.ServerLocalHostID:
			return nil, errors.New("worker 任务当前只支持 remote host，不支持 server-local")
		case host.Status != "online":
			return nil, fmt.Errorf("host %s 当前离线", task.HostID)
		case !host.Executable:
			return nil, fmt.Errorf("host %s 当前不支持执行", task.HostID)
		}
	}

	result, err := a.orchestrator.Dispatch(context.Background(), req)
	if err != nil {
		return nil, err
	}

	if workspaceSessionID := strings.TrimSpace(mission.WorkspaceSessionID); workspaceSessionID != "" {
		view := orchestrator.ProjectDispatchSummary(result, mission)
		a.store.UpsertCard(workspaceSessionID, model.Card{
			ID:      "dispatch-" + mission.ID,
			Type:    "ResultSummaryCard",
			Title:   view.Label,
			Summary: firstNonEmptyValue(view.Caption, "当前批次任务已提交到调度器。"),
			Text:    fmt.Sprintf("accepted=%d activated=%d queued=%d", view.Accepted, view.Activated, view.Queued),
			Status:  "completed",
			KVRows: []model.KeyValueRow{
				{Key: "Accepted", Value: fmt.Sprintf("%d", view.Accepted)},
				{Key: "Activated", Value: fmt.Sprintf("%d", view.Activated)},
				{Key: "Queued", Value: fmt.Sprintf("%d", view.Queued)},
			},
			Detail: map[string]any{
				"dispatchSummary": map[string]any{
					"accepted":  view.Accepted,
					"activated": view.Activated,
					"queued":    view.Queued,
				},
			},
			CreatedAt: model.NowString(),
			UpdatedAt: model.NowString(),
		})
		a.broadcastSnapshot(workspaceSessionID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	_ = a.activateDispatchResult(ctx, mission, result)

	return result, nil
}

func (a *App) resolveOrchestratorMission(sessionID string) (*orchestrator.Mission, bool) {
	if a == nil || a.orchestrator == nil {
		return nil, false
	}
	if mission, ok := a.orchestrator.MissionBySession(sessionID); ok && mission != nil {
		return mission, true
	}
	if mission, ok := a.orchestrator.MissionByWorkspaceSession(sessionID); ok && mission != nil {
		return mission, true
	}
	if meta := a.sessionMeta(sessionID); meta.MissionID != "" {
		if mission, ok := a.orchestrator.Mission(meta.MissionID); ok && mission != nil {
			return mission, true
		}
	}
	if meta := a.sessionMeta(sessionID); strings.TrimSpace(meta.WorkspaceSessionID) != "" {
		if mission, ok := a.orchestrator.MissionByWorkspaceSession(strings.TrimSpace(meta.WorkspaceSessionID)); ok && mission != nil {
			return mission, true
		}
	}
	return nil, false
}

func (a *App) handleWorkspaceDispatchTasks(rawID, sessionID string, arguments map[string]any) {
	var req orchestrator.DispatchRequest
	if err := remarshalInto(arguments, &req); err != nil {
		if rawID != "" {
			_ = a.respondCodex(context.Background(), rawID, toolResponse("dispatch payload 无法解析。", false))
		}
		return
	}
	result, err := a.dispatchOrchestratorTasks(sessionID, req)
	if err != nil {
		if rawID != "" {
			_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
		}
		return
	}
	if rawID != "" {
		_ = a.respondCodex(context.Background(), rawID, toolResponse(
			fmt.Sprintf("dispatch accepted=%d activated=%d queued=%d", result.Accepted, result.Activated, result.Queued),
			true,
		))
	}
}

func (a *App) executeReadonlyDynamicTool(sessionID, hostID, rawID string, params dynamicToolCallParams, args execToolArgs) {
	cardID := dynamicToolCardID(params.CallID)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(clampExecTimeout(args.TimeoutSec, true)+15)*time.Second)
	defer cancel()

	result, err := a.runRemoteExec(ctx, sessionID, hostID, cardID, execSpec{
		Command:    args.Command,
		Cwd:        args.Cwd,
		TimeoutSec: args.TimeoutSec,
		Readonly:   true,
	})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
		return
	}
	if a.turnWasInterrupted(sessionID) {
		return
	}

	success := execResultCardStatus(result) == "completed"
	_ = a.respondCodex(context.Background(), rawID, toolResponse(formatExecToolResult(args.Command, result), success))
}

func (a *App) executeRemoteListFilesTool(sessionID, hostID, rawID string, params dynamicToolCallParams, args remoteListFilesArgs) {
	processCardID := "process-" + dynamicToolCardID(params.CallID)
	startedAt := model.NowString()
	a.beginToolProcess(sessionID, processCardID, "browsing", "现在列出 "+args.Path)
	a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
		runtime.Activity.CurrentListingPath = args.Path
	})
	a.auditRemoteToolEvent("remote.file_list.started", sessionID, hostID, "list_remote_files", map[string]any{
		"path":      args.Path,
		"startedAt": startedAt,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := a.remoteListFiles(ctx, hostID, args.Path, args.Recursive, args.MaxEntries)
	if err != nil {
		a.failToolProcess(sessionID, processCardID, "列目录失败："+err.Error())
		a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
			if runtime.Activity.CurrentListingPath == args.Path {
				runtime.Activity.CurrentListingPath = ""
			}
		})
		a.auditRemoteToolEvent("remote.file_list.finished", sessionID, hostID, "list_remote_files", map[string]any{
			"path":      args.Path,
			"status":    "failed",
			"error":     truncate(err.Error(), 200),
			"startedAt": startedAt,
			"endedAt":   model.NowString(),
		})
		_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
		return
	}
	if a.turnWasInterrupted(sessionID) {
		return
	}

	a.completeToolProcess(sessionID, processCardID, "已列出 "+result.Path)
	a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
		if runtime.Activity.CurrentListingPath == args.Path || runtime.Activity.CurrentListingPath == result.Path {
			runtime.Activity.CurrentListingPath = ""
		}
		runtime.Activity.ListCount++
	})
	a.auditRemoteToolEvent("remote.file_list.finished", sessionID, hostID, "list_remote_files", map[string]any{
		"path":      result.Path,
		"status":    "completed",
		"entries":   len(result.Entries),
		"startedAt": startedAt,
		"endedAt":   model.NowString(),
	})
	a.setRuntimeTurnPhase(sessionID, "thinking")
	a.broadcastSnapshot(sessionID)
	_ = a.respondCodex(context.Background(), rawID, toolResponse(renderFileListMessage(hostID, result.Path, result.Entries, result.Truncated), true))
}

func (a *App) executeRemoteReadFileTool(sessionID, hostID, rawID string, params dynamicToolCallParams, args remoteReadFileArgs) {
	processCardID := "process-" + dynamicToolCardID(params.CallID)
	startedAt := model.NowString()
	a.beginToolProcess(sessionID, processCardID, "browsing", "现在浏览 "+args.Path)
	a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
		runtime.Activity.CurrentReadingFile = args.Path
	})
	a.auditRemoteToolEvent("remote.file_read.started", sessionID, hostID, "read_remote_file", map[string]any{
		"filePath":  args.Path,
		"startedAt": startedAt,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := a.remoteReadFile(ctx, hostID, args.Path, args.MaxBytes)
	if err != nil {
		a.failToolProcess(sessionID, processCardID, "浏览文件失败："+err.Error())
		a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
			if runtime.Activity.CurrentReadingFile == args.Path {
				runtime.Activity.CurrentReadingFile = ""
			}
		})
		a.auditRemoteToolEvent("remote.file_read.finished", sessionID, hostID, "read_remote_file", map[string]any{
			"filePath":  args.Path,
			"status":    "failed",
			"error":     truncate(err.Error(), 200),
			"startedAt": startedAt,
			"endedAt":   model.NowString(),
		})
		_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
		return
	}
	if a.turnWasInterrupted(sessionID) {
		return
	}

	a.completeToolProcess(sessionID, processCardID, "已浏览 "+result.Path)
	a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
		if runtime.Activity.CurrentReadingFile == args.Path || runtime.Activity.CurrentReadingFile == result.Path {
			runtime.Activity.CurrentReadingFile = ""
		}
		entry := model.ActivityEntry{Label: filepathBase(result.Path), Path: result.Path}
		appendUniqueActivityEntry(&runtime.Activity.ViewedFiles, entry, func(existing, next model.ActivityEntry) bool {
			return existing.Path != "" && existing.Path == next.Path
		})
		runtime.Activity.FilesViewed = len(runtime.Activity.ViewedFiles)
	})
	a.auditRemoteToolEvent("remote.file_read.finished", sessionID, hostID, "read_remote_file", map[string]any{
		"filePath":  result.Path,
		"status":    "completed",
		"truncated": result.Truncated,
		"startedAt": startedAt,
		"endedAt":   model.NowString(),
	})
	a.setRuntimeTurnPhase(sessionID, "thinking")
	a.broadcastSnapshot(sessionID)
	toolText := fmt.Sprintf("Read file %s:\n\n%s", result.Path, result.Content)
	if result.Truncated {
		toolText += "\n\n[truncated]"
	}
	_ = a.respondCodex(context.Background(), rawID, toolResponse(toolText, true))
}

func (a *App) executeRemoteSearchFilesTool(sessionID, hostID, rawID string, params dynamicToolCallParams, args remoteSearchFilesArgs) {
	processCardID := "process-" + dynamicToolCardID(params.CallID)
	startedAt := model.NowString()
	a.beginToolProcess(sessionID, processCardID, "searching", "现在搜索内容（"+args.Query+"）")
	a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
		runtime.Activity.CurrentSearchKind = "content"
		runtime.Activity.CurrentSearchQuery = args.Query
	})
	a.auditRemoteToolEvent("remote.file_search.started", sessionID, hostID, "search_remote_files", map[string]any{
		"path":      args.Path,
		"query":     args.Query,
		"startedAt": startedAt,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := a.remoteSearchFiles(ctx, hostID, args.Path, args.Query, args.MaxMatches)
	if err != nil {
		a.failToolProcess(sessionID, processCardID, "搜索内容失败："+err.Error())
		a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
			if runtime.Activity.CurrentSearchKind == "content" && runtime.Activity.CurrentSearchQuery == args.Query {
				runtime.Activity.CurrentSearchKind = ""
				runtime.Activity.CurrentSearchQuery = ""
			}
		})
		a.auditRemoteToolEvent("remote.file_search.finished", sessionID, hostID, "search_remote_files", map[string]any{
			"path":      args.Path,
			"query":     args.Query,
			"status":    "failed",
			"error":     truncate(err.Error(), 200),
			"startedAt": startedAt,
			"endedAt":   model.NowString(),
		})
		_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
		return
	}
	if a.turnWasInterrupted(sessionID) {
		return
	}

	a.completeToolProcess(sessionID, processCardID, fmt.Sprintf("已搜索内容（命中 %d 个位置）", len(result.Matches)))
	a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
		if runtime.Activity.CurrentSearchKind == "content" && runtime.Activity.CurrentSearchQuery == args.Query {
			runtime.Activity.CurrentSearchKind = ""
			runtime.Activity.CurrentSearchQuery = ""
		}
		runtime.Activity.SearchCount++
		runtime.Activity.SearchLocationCount += len(result.Matches)
		appendUniqueActivityEntry(&runtime.Activity.SearchedContentQueries, model.ActivityEntry{
			Label: fmt.Sprintf("在 %s 中搜索 %s（命中 %d 个位置）", result.Path, result.Query, len(result.Matches)),
			Query: result.Query,
			Path:  result.Path,
		}, func(existing, next model.ActivityEntry) bool {
			return existing.Path == next.Path && existing.Query == next.Query
		})
	})
	a.auditRemoteToolEvent("remote.file_search.finished", sessionID, hostID, "search_remote_files", map[string]any{
		"path":      result.Path,
		"query":     result.Query,
		"status":    "completed",
		"matches":   len(result.Matches),
		"startedAt": startedAt,
		"endedAt":   model.NowString(),
	})
	a.setRuntimeTurnPhase(sessionID, "thinking")
	a.broadcastSnapshot(sessionID)
	_ = a.respondCodex(context.Background(), rawID, toolResponse(renderFileSearchMessage(hostID, result.Path, result.Query, result.Matches, result.Truncated), true))
}

func (a *App) requestRemoteCommandApproval(sessionID, hostID, rawID string, params dynamicToolCallParams, args execToolArgs, readonly bool) {
	cardID := dynamicToolCardID(params.CallID)
	now := model.NowString()
	host := a.findHost(hostID)
	decision, err := a.evaluateCommandPolicyForHost(hostID, args.Command)
	if err != nil {
		_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
		return
	}
	if maxTimeout := a.effectiveCommandTimeoutSeconds(hostID); maxTimeout > 0 && args.TimeoutSec > 0 && args.TimeoutSec > maxTimeout {
		_ = a.respondCodex(context.Background(), rawID, toolResponse("requested timeout exceeds the current effective agent profile limit", false))
		return
	}
	if decision.Category == "filesystem_mutation" && args.Cwd != "" {
		if err := a.ensureWritableRootsForHost(hostID, []string{args.Cwd}); err != nil {
			_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
			return
		}
	}
	a.store.RememberItem(sessionID, cardID, map[string]any{
		"tool":       params.Tool,
		"threadId":   params.ThreadID,
		"turnId":     params.TurnID,
		"callId":     params.CallID,
		"command":    args.Command,
		"cwd":        args.Cwd,
		"reason":     args.Reason,
		"timeoutSec": clampExecTimeout(args.TimeoutSec, false),
		"mode": func() string {
			if readonly {
				return "readonly_command"
			}
			return "command"
		}(),
		"readonly": readonly,
	})

	approval := model.ApprovalRequest{
		ID:           model.NewID("approval"),
		RequestIDRaw: rawID,
		HostID:       hostID,
		Fingerprint:  approvalFingerprintForCommand(hostID, args.Command, args.Cwd),
		Type:         "remote_command",
		Status:       "pending",
		ThreadID:     params.ThreadID,
		TurnID:       params.TurnID,
		ItemID:       cardID,
		Command:      args.Command,
		Cwd:          args.Cwd,
		Reason:       args.Reason,
		Decisions:    []string{"accept", "accept_session", "decline"},
		RequestedAt:  now,
	}

	if a.autoApproveRemoteOperationBySessionGrant(sessionID, approval) {
		return
	}
	if a.autoApproveRemoteOperationByHostGrant(sessionID, approval) {
		return
	}
	if readonly == false && decision.Mode == model.AgentPermissionModeAllow && !capabilityNeedsApproval(a.effectiveCapabilityState(hostID, "commandExecution")) {
		if a.autoApproveRemoteOperationByPolicy(sessionID, approval) {
			return
		}
	}
	if readonly && decision.Mode == model.AgentPermissionModeAllow && !capabilityNeedsApproval(a.effectiveCapabilityState(hostID, "commandExecution")) {
		if a.autoApproveRemoteOperationByPolicy(sessionID, approval) {
			return
		}
	}

	a.setRuntimeTurnPhase(sessionID, "waiting_approval")
	a.store.AddApproval(sessionID, approval)
	card := model.Card{
		ID:      cardID,
		Type:    "CommandApprovalCard",
		Title:   "Remote command approval required",
		Command: args.Command,
		Cwd:     args.Cwd,
		Text:    args.Reason,
		Status:  "pending",
		Approval: &model.ApprovalRef{
			RequestID: approval.ID,
			Type:      approval.Type,
			Decisions: approval.Decisions,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	applyCardHost(&card, host)
	a.store.UpsertCard(sessionID, card)
	a.recordOrchestratorApprovalRequested(sessionID, approval)
	if kind := a.sessionKind(sessionID); kind == model.SessionKindPlanner || kind == model.SessionKindWorker {
		a.mirrorInternalApprovalToWorkspace(sessionID, approval, card)
	}
	a.auditApprovalRequested(sessionID, approval, nil)
	a.broadcastSnapshot(sessionID)
}

func (a *App) requestRemoteFileChangeApproval(sessionID, hostID, rawID string, params dynamicToolCallParams, args remoteFileChangeArgs) {
	cardID := dynamicToolCardID(params.CallID)
	now := model.NowString()
	host := a.findHost(hostID)
	if err := a.ensureWritableRootsForHost(hostID, []string{args.Path}); err != nil {
		_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	oldContent := ""
	created := true
	if result, err := a.remoteReadFile(ctx, hostID, args.Path, 256*1024); err == nil {
		oldContent = result.Content
		created = false
	}
	newContent := args.Content
	if strings.EqualFold(args.WriteMode, "append") {
		newContent = oldContent + args.Content
	}
	diff := renderFileDiff(args.Path, oldContent, newContent)
	change := model.FileChange{
		Path: args.Path,
		Kind: remoteFileChangeKind(created, args.WriteMode),
		Diff: diff,
	}
	a.store.RememberItem(sessionID, cardID, map[string]any{
		"tool":       params.Tool,
		"threadId":   params.ThreadID,
		"turnId":     params.TurnID,
		"callId":     params.CallID,
		"host":       hostID,
		"mode":       "file_change",
		"path":       args.Path,
		"content":    args.Content,
		"write_mode": args.WriteMode,
		"writeMode":  args.WriteMode,
		"reason":     args.Reason,
		"diff":       diff,
	})

	approval := model.ApprovalRequest{
		ID:           model.NewID("approval"),
		RequestIDRaw: rawID,
		HostID:       hostID,
		Fingerprint:  approvalFingerprintForFileChange(hostID, filepath.Dir(args.Path), []model.FileChange{change}),
		Type:         "remote_file_change",
		Status:       "pending",
		ThreadID:     params.ThreadID,
		TurnID:       params.TurnID,
		ItemID:       cardID,
		Reason:       args.Reason,
		GrantRoot:    filepath.Dir(args.Path),
		Changes:      []model.FileChange{change},
		Decisions:    []string{"accept", "accept_session", "decline"},
		RequestedAt:  now,
	}

	if a.autoApproveRemoteOperationBySessionGrant(sessionID, approval) {
		return
	}
	if a.autoApproveRemoteOperationByHostGrant(sessionID, approval) {
		return
	}
	if !capabilityNeedsApproval(a.effectiveCapabilityState(hostID, "fileChange")) {
		if a.autoApproveRemoteOperationByPolicy(sessionID, approval) {
			return
		}
	}

	a.setRuntimeTurnPhase(sessionID, "waiting_approval")
	a.store.AddApproval(sessionID, approval)
	card := model.Card{
		ID:      cardID,
		Type:    "FileChangeApprovalCard",
		Title:   "Remote file change approval required",
		Text:    args.Reason,
		Status:  "pending",
		Changes: approval.Changes,
		Approval: &model.ApprovalRef{
			RequestID: approval.ID,
			Type:      approval.Type,
			Decisions: approval.Decisions,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	applyCardHost(&card, host)
	a.store.UpsertCard(sessionID, card)
	a.recordOrchestratorApprovalRequested(sessionID, approval)
	if kind := a.sessionKind(sessionID); kind == model.SessionKindPlanner || kind == model.SessionKindWorker {
		a.mirrorInternalApprovalToWorkspace(sessionID, approval, card)
	}
	a.auditApprovalRequested(sessionID, approval, map[string]any{
		"filePath": args.Path,
	})
	a.broadcastSnapshot(sessionID)
}

func (a *App) autoApproveRemoteOperationBySessionGrant(sessionID string, approval model.ApprovalRequest) bool {
	if approval.Fingerprint == "" {
		return false
	}
	if _, ok := a.store.ApprovalGrant(sessionID, approval.Fingerprint); !ok {
		return false
	}

	now := model.NowString()
	approval.Status = "accepted_for_session_auto"
	approval.ResolvedAt = now
	a.store.AddApproval(sessionID, approval)
	a.store.ResolveApproval(sessionID, approval.ID, approval.Status, now)
	a.setRuntimeTurnPhase(sessionID, "executing")
	a.store.UpsertCard(sessionID, model.Card{
		ID:        "auto-approval-" + approval.ItemID,
		Type:      "NoticeCard",
		Title:     "Auto-approved for session",
		Text:      autoApprovalNoticeText(approval),
		Status:    "notice",
		CreatedAt: now,
		UpdatedAt: now,
	})
	a.broadcastSnapshot(sessionID)

	go a.executeApprovedRemoteOperation(sessionID, approval)
	return true
}

func (a *App) autoApproveRemoteOperationByHostGrant(sessionID string, approval model.ApprovalRequest) bool {
	if approval.Fingerprint == "" || approval.HostID == "" {
		return false
	}
	if a.approvalGrantStore == nil {
		return false
	}
	if _, ok := a.approvalGrantStore.MatchFingerprint(approval.HostID, approval.Fingerprint); !ok {
		return false
	}

	now := model.NowString()
	approval.Status = "accepted_for_host_auto"
	approval.ResolvedAt = now
	a.store.AddApproval(sessionID, approval)
	a.store.ResolveApproval(sessionID, approval.ID, approval.Status, now)
	a.setRuntimeTurnPhase(sessionID, "executing")
	a.store.UpsertCard(sessionID, model.Card{
		ID:        "auto-approval-" + approval.ItemID,
		Type:      "NoticeCard",
		Title:     "Auto-approved by host grant",
		Text:      hostGrantAutoApprovalNoticeText(approval),
		Status:    "notice",
		CreatedAt: now,
		UpdatedAt: now,
	})
	log.Printf("approval auto accepted by host grant session=%s approval=%s type=%s host=%s", sessionID, approval.ID, approval.Type, approval.HostID)
	a.auditApprovalLifecycleEvent("approval.auto_accepted", sessionID, approval, "auto_accept", approval.Status, approval.RequestedAt, now, map[string]any{
		"fingerprint": approval.Fingerprint,
		"grantMode":   "host",
	})
	a.broadcastSnapshot(sessionID)

	go a.executeApprovedRemoteOperation(sessionID, approval)
	return true
}

func (a *App) autoApproveRemoteOperationByPolicy(sessionID string, approval model.ApprovalRequest) bool {
	now := model.NowString()
	approval.Status = "accepted_by_policy_auto"
	approval.ResolvedAt = now
	a.store.AddApproval(sessionID, approval)
	a.store.ResolveApproval(sessionID, approval.ID, approval.Status, now)
	a.setRuntimeTurnPhase(sessionID, "executing")
	a.store.UpsertCard(sessionID, model.Card{
		ID:        "auto-approval-" + approval.ItemID,
		Type:      "NoticeCard",
		Title:     "Auto-approved by profile",
		Text:      "当前 main-agent profile 允许该操作直接执行，因此已自动放行。",
		Status:    "notice",
		CreatedAt: now,
		UpdatedAt: now,
	})
	a.auditApprovalLifecycleEvent("approval.decision", sessionID, approval, "accept", approval.Status, approval.RequestedAt, now, map[string]any{
		"autoApprovedByProfile": true,
	})
	a.broadcastSnapshot(sessionID)
	go a.executeApprovedRemoteOperation(sessionID, approval)
	return true
}

func (a *App) executeApprovedRemoteOperation(sessionID string, approval model.ApprovalRequest) {
	switch approval.Type {
	case "remote_file_change":
		a.executeApprovedRemoteFileChange(sessionID, approval)
	default:
		a.executeApprovedRemoteMutation(sessionID, approval)
	}
}

func (a *App) executeApprovedRemoteMutation(sessionID string, approval model.ApprovalRequest) {
	item := a.store.Item(sessionID, approval.ItemID)
	args, err := parseExecToolArgs(item)
	if err != nil {
		a.store.UpdateCard(sessionID, approval.ItemID, func(card *model.Card) {
			card.Type = "CommandCard"
			card.Command = approval.Command
			card.Cwd = approval.Cwd
			card.Status = "failed"
			card.Output = err.Error()
			card.UpdatedAt = model.NowString()
		})
		a.broadcastSnapshot(sessionID)
		_ = a.respondCodex(context.Background(), approval.RequestIDRaw, toolResponse(err.Error(), false))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(clampExecTimeout(args.TimeoutSec, false)+15)*time.Second)
	defer cancel()
	result, runErr := a.runRemoteExec(ctx, sessionID, approval.HostID, approval.ItemID, execSpec{
		Command:    args.Command,
		Cwd:        args.Cwd,
		TimeoutSec: args.TimeoutSec,
		Readonly:   getBool(item, "readonly") || strings.TrimSpace(getString(item, "mode")) == "readonly_command",
		Approval:   approval.Status,
	})
	if a.turnWasInterrupted(sessionID) {
		return
	}
	if runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, context.DeadlineExceeded) {
		_ = a.respondCodex(context.Background(), approval.RequestIDRaw, toolResponse(runErr.Error(), false))
		return
	}
	success := execResultCardStatus(result) == "completed"
	_ = a.respondCodex(context.Background(), approval.RequestIDRaw, toolResponse(formatExecToolResult(args.Command, result), success))
}

func (a *App) executeApprovedRemoteFileChange(sessionID string, approval model.ApprovalRequest) {
	item := a.store.Item(sessionID, approval.ItemID)
	args, err := parseRemoteFileChangeArgs(item)
	startedAt := model.NowString()
	processCardID := "process-" + approval.ItemID
	if err != nil {
		a.store.UpdateCard(sessionID, approval.ItemID, func(card *model.Card) {
			card.Type = "FileChangeCard"
			card.Status = "failed"
			card.Text = err.Error()
			card.UpdatedAt = model.NowString()
		})
		a.broadcastSnapshot(sessionID)
		a.auditRemoteToolEvent("remote.file_change.finished", sessionID, approval.HostID, "execute_system_mutation", map[string]any{
			"filePath":         args.Path,
			"cwd":              filepath.Dir(args.Path),
			"status":           "failed",
			"approvalDecision": approval.Status,
			"error":            truncate(err.Error(), 200),
			"startedAt":        startedAt,
			"endedAt":          model.NowString(),
			"exitCode":         nil,
		})
		_ = a.respondCodex(context.Background(), approval.RequestIDRaw, toolResponse(err.Error(), false))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	a.beginToolProcess(sessionID, processCardID, "executing", "现在修改 "+args.Path)
	a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
		runtime.Activity.CurrentChangingFile = args.Path
	})
	a.auditRemoteToolEvent("remote.file_change.started", sessionID, approval.HostID, "execute_system_mutation", map[string]any{
		"filePath":         args.Path,
		"cwd":              filepath.Dir(args.Path),
		"approvalDecision": approval.Status,
		"status":           "inProgress",
		"startedAt":        startedAt,
		"endedAt":          nil,
		"exitCode":         nil,
	})
	result, writeErr := a.remoteWriteFile(ctx, approval.HostID, args.Path, args.Content, args.WriteMode)
	if a.turnWasInterrupted(sessionID) {
		a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
			if runtime.Activity.CurrentChangingFile == args.Path {
				runtime.Activity.CurrentChangingFile = ""
			}
		})
		return
	}
	if writeErr != nil && !errors.Is(writeErr, context.Canceled) && !errors.Is(writeErr, context.DeadlineExceeded) {
		annotatedErr := annotateRemoteFileChangeError(args, writeErr)
		a.failToolProcess(sessionID, processCardID, "修改文件失败："+annotatedErr.Error())
		a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
			if runtime.Activity.CurrentChangingFile == args.Path {
				runtime.Activity.CurrentChangingFile = ""
			}
		})
		a.store.UpdateCard(sessionID, approval.ItemID, func(card *model.Card) {
			card.Type = "FileChangeCard"
			card.Status = "failed"
			card.Text = annotatedErr.Error()
			card.UpdatedAt = model.NowString()
		})
		a.broadcastSnapshot(sessionID)
		a.auditRemoteToolEvent("remote.file_change.finished", sessionID, approval.HostID, "execute_system_mutation", map[string]any{
			"filePath":         args.Path,
			"cwd":              filepath.Dir(args.Path),
			"status":           "failed",
			"approvalDecision": approval.Status,
			"error":            truncate(annotatedErr.Error(), 200),
			"startedAt":        startedAt,
			"endedAt":          model.NowString(),
			"exitCode":         nil,
		})
		_ = a.respondCodex(context.Background(), approval.RequestIDRaw, toolResponse(annotatedErr.Error(), false))
		return
	}

	diff := renderFileDiff(result.Path, result.OldContent, result.NewContent)
	now := model.NowString()
	a.completeToolProcess(sessionID, processCardID, "已修改 "+result.Path)
	a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
		if runtime.Activity.CurrentChangingFile == args.Path || runtime.Activity.CurrentChangingFile == result.Path {
			runtime.Activity.CurrentChangingFile = ""
		}
		runtime.Activity.FilesChanged++
	})
	a.store.UpsertCard(sessionID, model.Card{
		ID:      approval.ItemID,
		Type:    "FileChangeCard",
		Title:   "Remote file change",
		Status:  "completed",
		Changes: []model.FileChange{{Path: result.Path, Kind: remoteFileChangeKind(result.Created, result.WriteMode), Diff: diff}},
		Text:    fmt.Sprintf("已修改远程文件 %s", result.Path),
		HostID:  approval.HostID,
		HostName: func() string {
			return hostNameOrID(a.findHost(approval.HostID))
		}(),
		CreatedAt: func() string {
			if existing := a.cardByID(sessionID, approval.ItemID); existing != nil && existing.CreatedAt != "" {
				return existing.CreatedAt
			}
			return now
		}(),
		UpdatedAt: now,
	})
	a.setRuntimeTurnPhase(sessionID, "thinking")
	a.broadcastSnapshot(sessionID)
	a.auditRemoteToolEvent("remote.file_change.finished", sessionID, approval.HostID, "execute_system_mutation", map[string]any{
		"filePath":         result.Path,
		"cwd":              filepath.Dir(result.Path),
		"status":           "completed",
		"approvalDecision": approval.Status,
		"startedAt":        startedAt,
		"endedAt":          model.NowString(),
		"exitCode":         nil,
	})
	_ = a.respondCodex(context.Background(), approval.RequestIDRaw, toolResponse(fmt.Sprintf("Updated file %s successfully.", result.Path), true))
}

func parseExecToolArgs(arguments map[string]any) (execToolArgs, error) {
	command := strings.TrimSpace(getString(arguments, "command"))
	if command == "" {
		command = strings.TrimSpace(composeCommandFromProgramArgs(arguments))
	}
	if command == "" {
		return execToolArgs{}, errors.New("tool requires a command")
	}

	timeoutSec, _ := getIntAny(arguments, "timeout_sec", "timeoutSec")
	return execToolArgs{
		HostID:     remoteToolTargetHost(arguments),
		Command:    command,
		Cwd:        strings.TrimSpace(getString(arguments, "cwd")),
		Reason:     strings.TrimSpace(getString(arguments, "reason")),
		TimeoutSec: timeoutSec,
		Mode:       strings.TrimSpace(getString(arguments, "mode")),
	}, nil
}

func parseRemoteListFilesArgs(arguments map[string]any) (remoteListFilesArgs, error) {
	args := remoteListFilesArgs{
		HostID:     remoteToolTargetHost(arguments),
		Path:       strings.TrimSpace(getString(arguments, "path")),
		Recursive:  getBool(arguments, "recursive"),
		MaxEntries: getInt(arguments, "max_entries", "maxEntries"),
		Reason:     strings.TrimSpace(getString(arguments, "reason")),
	}
	if args.Path == "" {
		return remoteListFilesArgs{}, errors.New("tool requires a path")
	}
	if args.Reason == "" {
		return remoteListFilesArgs{}, errors.New("tool requires a reason")
	}
	return args, nil
}

func parseRemoteReadFileArgs(arguments map[string]any) (remoteReadFileArgs, error) {
	args := remoteReadFileArgs{
		HostID:   remoteToolTargetHost(arguments),
		Path:     strings.TrimSpace(getString(arguments, "path")),
		MaxBytes: getInt(arguments, "max_bytes", "maxBytes"),
		Reason:   strings.TrimSpace(getString(arguments, "reason")),
	}
	if args.Path == "" {
		return remoteReadFileArgs{}, errors.New("tool requires a path")
	}
	if args.Reason == "" {
		return remoteReadFileArgs{}, errors.New("tool requires a reason")
	}
	return args, nil
}

func parseRemoteSearchFilesArgs(arguments map[string]any) (remoteSearchFilesArgs, error) {
	args := remoteSearchFilesArgs{
		HostID:     remoteToolTargetHost(arguments),
		Path:       strings.TrimSpace(getString(arguments, "path")),
		Query:      strings.TrimSpace(getString(arguments, "query")),
		MaxMatches: getInt(arguments, "max_matches", "maxMatches"),
		Reason:     strings.TrimSpace(getString(arguments, "reason")),
	}
	if args.Path == "" {
		return remoteSearchFilesArgs{}, errors.New("tool requires a path")
	}
	if args.Query == "" {
		return remoteSearchFilesArgs{}, errors.New("tool requires a query")
	}
	if args.Reason == "" {
		return remoteSearchFilesArgs{}, errors.New("tool requires a reason")
	}
	return args, nil
}

func parseRemoteFileChangeArgs(arguments map[string]any) (remoteFileChangeArgs, error) {
	hostID := strings.TrimSpace(getStringAny(arguments, "host", "hostId"))
	args := remoteFileChangeArgs{
		HostID: hostID,
		Mode:   strings.TrimSpace(getString(arguments, "mode")),
		Path:   strings.TrimSpace(getString(arguments, "path")),
		Reason: strings.TrimSpace(getString(arguments, "reason")),
	}
	content, ok := arguments["content"]
	if !ok {
		return remoteFileChangeArgs{}, errors.New("file_change requires content")
	}
	contentText, ok := content.(string)
	if !ok {
		return remoteFileChangeArgs{}, errors.New("file_change content must be a string")
	}
	args.Content = contentText

	writeModeRaw, ok := arguments["write_mode"]
	if !ok {
		writeModeRaw, ok = arguments["writeMode"]
	}
	if !ok {
		args.WriteMode = "overwrite"
	} else {
		writeMode, ok := writeModeRaw.(string)
		if !ok {
			return remoteFileChangeArgs{}, errors.New("file_change write_mode must be a string")
		}
		args.WriteMode = strings.TrimSpace(writeMode)
	}

	if args.HostID == "" {
		return remoteFileChangeArgs{}, errors.New("file_change requires host")
	}
	if args.Mode != "file_change" {
		return remoteFileChangeArgs{}, errors.New("file_change requires mode=file_change")
	}
	if args.Path == "" {
		return remoteFileChangeArgs{}, errors.New("file_change requires a path")
	}
	if args.Reason == "" {
		return remoteFileChangeArgs{}, errors.New("file_change requires a reason")
	}
	if args.WriteMode != "overwrite" && args.WriteMode != "append" {
		return remoteFileChangeArgs{}, errors.New("file_change write_mode must be overwrite or append")
	}
	return args, nil
}

func validateRemoteFileChangeArguments(arguments map[string]any) error {
	if strings.TrimSpace(getStringAny(arguments, "host", "hostId")) == "" {
		return errors.New("file_change requires host")
	}
	if mode := strings.TrimSpace(getString(arguments, "mode")); mode != "file_change" {
		return errors.New("file_change requires mode=file_change")
	}
	if strings.TrimSpace(getString(arguments, "path")) == "" {
		return errors.New("file_change requires a path")
	}
	if _, ok := arguments["content"]; !ok {
		return errors.New("file_change requires content")
	}
	if _, ok := arguments["write_mode"]; !ok {
		if _, ok := arguments["writeMode"]; !ok {
			return errors.New("file_change requires write_mode")
		}
	}
	if strings.TrimSpace(getString(arguments, "reason")) == "" {
		return errors.New("file_change requires a reason")
	}
	return nil
}

func annotateRemoteFileChangeError(args remoteFileChangeArgs, err error) error {
	detail := strings.TrimSpace(err.Error())
	if detail == "" {
		detail = "unknown error"
	}
	lower := strings.ToLower(detail)
	prefix := fmt.Sprintf("file_change failed for %s (write_mode=%s)", args.Path, args.WriteMode)
	switch {
	case strings.Contains(lower, "permission denied") || strings.Contains(lower, "operation not permitted"):
		return fmt.Errorf("%s: permission denied: %w", prefix, err)
	case strings.Contains(lower, "no such file or directory") || strings.Contains(lower, "not found"):
		return fmt.Errorf("%s: path not found: %w", prefix, err)
	case strings.Contains(lower, "read-only file system"):
		return fmt.Errorf("%s: read-only file system: %w", prefix, err)
	case strings.Contains(lower, "input/output error") || strings.Contains(lower, "i/o error"):
		return fmt.Errorf("%s: i/o error: %w", prefix, err)
	case strings.Contains(lower, "is a directory"):
		return fmt.Errorf("%s: path is a directory: %w", prefix, err)
	default:
		return fmt.Errorf("%s: %w", prefix, err)
	}
}

func (a *App) auditRemoteToolEvent(event, sessionID, hostID, toolName string, fields map[string]any) {
	session := a.store.Session(sessionID)
	host := a.findHost(hostID)
	payload := map[string]any{
		"sessionId": sessionID,
		"hostId":    hostID,
		"hostName":  hostNameOrID(host),
		"operator":  a.auditOperator(sessionID),
		"toolName":  toolName,
	}
	if session != nil {
		payload["threadId"] = session.ThreadID
		payload["turnId"] = session.TurnID
	}
	for key, value := range fields {
		payload[key] = value
	}
	a.audit(event, payload)
}

func (a *App) beginToolProcess(sessionID, cardID, phase, text string) {
	now := model.NowString()
	a.setRuntimeTurnPhase(sessionID, phase)
	a.store.UpsertCard(sessionID, model.Card{
		ID:        cardID,
		Type:      "ProcessLineCard",
		Text:      text,
		Status:    "inProgress",
		CreatedAt: now,
		UpdatedAt: now,
	})
	a.broadcastSnapshot(sessionID)
}

func (a *App) completeToolProcess(sessionID, cardID, text string) {
	now := model.NowString()
	durationMS := a.cardDurationMS(sessionID, cardID, now)
	a.store.UpdateCard(sessionID, cardID, func(card *model.Card) {
		card.Text = text
		card.Status = "completed"
		card.DurationMS = durationMS
		card.UpdatedAt = now
	})
}

func (a *App) failToolProcess(sessionID, cardID, text string) {
	now := model.NowString()
	durationMS := a.cardDurationMS(sessionID, cardID, now)
	a.store.UpdateCard(sessionID, cardID, func(card *model.Card) {
		card.Text = text
		card.Status = "failed"
		card.DurationMS = durationMS
		card.UpdatedAt = now
	})
	a.setRuntimeTurnPhase(sessionID, "thinking")
	a.broadcastSnapshot(sessionID)
}

func composeCommandFromProgramArgs(arguments map[string]any) string {
	program := strings.TrimSpace(getString(arguments, "program"))
	if program == "" {
		return ""
	}
	args := toStringSlice(arguments["args"])
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shellQuote(program))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n'\"`$|&;<>*?()[]{}!") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func validateReadonlyCommand(command string) error {
	clean := strings.TrimSpace(command)
	if clean == "" {
		return errors.New("read-only command cannot be empty")
	}
	lower := strings.ToLower(clean)

	forbiddenFragments := []string{
		";", "&&", "||", ">>", ">", "<", "`", "$(",
		" sudo ", "\nsudo ", "\tsudo ", "rm ", " mv ", " cp ", " chmod ", " chown ", " mkdir ", " touch ",
		" systemctl start", " systemctl stop", " systemctl restart", " service ", " kill ", " pkill ", " killall ",
		" apt ", " apt-get ", " yum ", " dnf ", " apk ", " pip install", " npm install", " tee ",
	}
	padded := " " + lower + " "
	for _, fragment := range forbiddenFragments {
		if strings.Contains(padded, fragment) || strings.HasPrefix(lower, strings.TrimSpace(fragment)) {
			return errors.New("this request is not read-only. Use execute_system_mutation instead.")
		}
	}

	segments := strings.Split(clean, "|")
	for _, segment := range segments {
		fields := strings.Fields(strings.TrimSpace(segment))
		if len(fields) == 0 {
			continue
		}
		if err := validateReadonlyProgram(fields); err != nil {
			return err
		}
	}
	return nil
}

func validateReadonlyProgram(fields []string) error {
	program := strings.ToLower(filepathBase(fields[0]))
	allowed := map[string]bool{
		"cat": true, "ls": true, "find": true, "grep": true, "rg": true, "sed": true,
		"head": true, "tail": true, "wc": true, "cut": true, "sort": true, "uniq": true,
		"df": true, "du": true, "free": true, "uptime": true, "top": true, "ps": true,
		"ss": true, "netstat": true, "iostat": true, "vmstat": true, "journalctl": true,
		"dmesg": true, "uname": true, "env": true, "printenv": true, "which": true, "whereis": true,
		"hostname": true, "id": true, "whoami": true, "pwd": true, "date": true,
		"lsblk": true, "blkid": true,
		"docker": true, "kubectl": true, "git": true, "systemctl": true,
	}
	if !allowed[program] {
		return fmt.Errorf("`%s` is not allowed in execute_readonly_query. Use a simpler read-only command or execute_system_mutation instead.", program)
	}

	switch program {
	case "find":
		for _, arg := range fields[1:] {
			value := strings.ToLower(strings.TrimSpace(arg))
			switch {
			case value == "-delete",
				value == "-exec",
				value == "-execdir",
				value == "-ok",
				value == "-okdir",
				value == "-fprint",
				value == "-fprint0",
				value == "-fprintf",
				value == "-fls":
				return errors.New("find mutations must use execute_system_mutation")
			}
		}
		return nil
	case "sed":
		for _, arg := range fields[1:] {
			value := strings.ToLower(strings.TrimSpace(arg))
			switch {
			case value == "-i",
				strings.HasPrefix(value, "-i"),
				value == "--in-place",
				strings.HasPrefix(value, "--in-place="):
				return errors.New("sed in-place edits must use execute_system_mutation")
			}
		}
		return nil
	case "sort":
		for _, arg := range fields[1:] {
			value := strings.ToLower(strings.TrimSpace(arg))
			switch {
			case value == "-o",
				value == "--output",
				strings.HasPrefix(value, "--output="):
				return errors.New("sort output writes must use execute_system_mutation")
			}
		}
		return nil
	case "journalctl":
		for _, arg := range fields[1:] {
			value := strings.ToLower(strings.TrimSpace(arg))
			switch {
			case value == "--rotate",
				value == "--flush",
				value == "--sync",
				value == "--relinquish-var",
				value == "--smart-relinquish-var",
				value == "--setup-keys",
				strings.HasPrefix(value, "--vacuum-"):
				return errors.New("journalctl mutations must use execute_system_mutation")
			}
		}
		return nil
	case "dmesg":
		for _, arg := range fields[1:] {
			value := strings.ToLower(strings.TrimSpace(arg))
			switch value {
			case "-c", "-C", "--clear", "--read-clear":
				return errors.New("dmesg mutations must use execute_system_mutation")
			}
		}
		return nil
	case "hostname":
		for _, arg := range fields[1:] {
			value := strings.TrimSpace(arg)
			if value == "" {
				continue
			}
			if strings.HasPrefix(value, "-") {
				continue
			}
			return errors.New("hostname changes must use execute_system_mutation")
		}
		return nil
	case "systemctl":
		action := firstCommandVerb(fields[1:], map[string]bool{
			"--type": true,
			"--host": true,
			"-H":     true,
			"--user": false,
		})
		if action == "" {
			return nil
		}
		switch action {
		case "status", "show", "list-units", "list-unit-files", "is-active", "is-enabled", "cat", "list-dependencies":
			return nil
		default:
			return errors.New("systemctl mutations must use execute_system_mutation")
		}
	case "docker":
		dockerArgs := fields[1:]
		actionIndex := firstCommandVerbIndex(dockerArgs, map[string]bool{
			"--context": true,
			"-H":        true,
			"--host":    true,
		})
		action := verbAt(dockerArgs, actionIndex)
		if action == "" {
			return nil
		}
		switch action {
		case "ps", "inspect", "stats", "logs", "images", "version", "info", "events":
			return nil
		case "container", "image", "network", "volume", "system", "compose":
			subcommand := firstCommandVerb(dockerArgs[actionIndex+1:], map[string]bool{
				"--format": true,
				"--filter": true,
				"-f":       true,
				"--tail":   true,
				"-n":       true,
			})
			switch action {
			case "container":
				if subcommand == "ls" || subcommand == "inspect" || subcommand == "logs" {
					return nil
				}
			case "image":
				if subcommand == "ls" || subcommand == "inspect" || subcommand == "history" {
					return nil
				}
			case "network", "volume":
				if subcommand == "ls" || subcommand == "inspect" {
					return nil
				}
			case "system":
				if subcommand == "df" || subcommand == "events" || subcommand == "info" {
					return nil
				}
			case "compose":
				if subcommand == "ps" || subcommand == "logs" || subcommand == "config" || subcommand == "images" || subcommand == "ls" {
					return nil
				}
			}
			return errors.New("docker mutations must use execute_system_mutation")
		default:
			return errors.New("docker mutations must use execute_system_mutation")
		}
	case "kubectl":
		kubectlArgs := fields[1:]
		actionIndex := firstCommandVerbIndex(kubectlArgs, map[string]bool{
			"-n":               true,
			"--namespace":      true,
			"--context":        true,
			"--cluster":        true,
			"--user":           true,
			"--kubeconfig":     true,
			"--server":         true,
			"--selector":       true,
			"-l":               true,
			"--field-selector": true,
		})
		action := verbAt(kubectlArgs, actionIndex)
		if action == "" {
			return nil
		}
		switch action {
		case "get", "describe", "logs", "top", "version", "cluster-info", "api-resources", "api-versions", "explain":
			return nil
		case "config":
			subcommand := firstCommandVerb(kubectlArgs[actionIndex+1:], map[string]bool{})
			switch subcommand {
			case "view", "get-contexts", "current-context":
				return nil
			default:
				return errors.New("kubectl mutations must use execute_system_mutation")
			}
		case "auth":
			subcommand := firstCommandVerb(fields[2:], map[string]bool{})
			if subcommand == "can-i" {
				return nil
			}
			return errors.New("kubectl mutations must use execute_system_mutation")
		default:
			return errors.New("kubectl mutations must use execute_system_mutation")
		}
	case "git":
		action := firstCommandVerb(fields[1:], map[string]bool{
			"-C":          true,
			"--git-dir":   true,
			"--work-tree": true,
		})
		if action == "" {
			return nil
		}
		switch action {
		case "status", "log", "show", "diff", "branch", "rev-parse", "ls-files", "remote", "grep", "blame", "tag", "reflog":
			return nil
		case "config":
			subcommand := firstCommandVerb(fields[2:], map[string]bool{
				"--get":         false,
				"--get-all":     false,
				"--show-origin": false,
			})
			if slices.Contains(fields[2:], "--get") || slices.Contains(fields[2:], "--get-all") || subcommand == "get" || subcommand == "get-all" {
				return nil
			}
			return errors.New("git write operations must use execute_system_mutation")
		default:
			return errors.New("git write operations must use execute_system_mutation")
		}
	}
	return nil
}

func firstCommandVerb(fields []string, flagsWithValue map[string]bool) string {
	index := firstCommandVerbIndex(fields, flagsWithValue)
	return verbAt(fields, index)
}

func firstCommandVerbIndex(fields []string, flagsWithValue map[string]bool) int {
	skipNext := false
	for index, field := range fields {
		value := strings.TrimSpace(field)
		if value == "" {
			continue
		}
		if skipNext {
			skipNext = false
			continue
		}
		if flagsWithValue[value] {
			skipNext = true
			continue
		}
		if strings.HasPrefix(value, "-") {
			continue
		}
		return index
	}
	return -1
}

func verbAt(fields []string, index int) string {
	if index < 0 || index >= len(fields) {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(fields[index]))
}

func filepathBase(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

func getInt(arguments map[string]any, keys ...string) int {
	value, ok := getIntAny(arguments, keys...)
	if !ok {
		return 0
	}
	return value
}

func remarshalInto(input any, out any) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, out)
}
