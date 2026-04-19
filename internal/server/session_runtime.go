package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/orchestrator"
)

type threadStartSpec struct {
	Model                 string
	Cwd                   string
	ApprovalPolicy        string
	SandboxMode           string
	DeveloperInstructions string
	DynamicTools          []map[string]any
	Config                map[string]any
	ThreadConfigHash      string
}

type turnStartSpec struct {
	Cwd                   string
	ApprovalPolicy        string
	SandboxMode           string
	WritableRoots         []string
	DeveloperInstructions string
	Input                 []map[string]any
	ReasoningEffort       string
}

func (a *App) buildSingleHostThreadStartSpec(ctx context.Context, sessionID string) threadStartSpec {
	session := a.store.EnsureSession(sessionID)
	selectedHostID := session.SelectedHostID
	if selectedHostID == "" {
		selectedHostID = model.ServerLocalHostID
	}

	profile := a.mainAgentProfile()
	spec := threadStartSpec{
		Model:                 profile.Runtime.Model,
		Cwd:                   a.cfg.DefaultWorkspace,
		ApprovalPolicy:        profile.Runtime.ApprovalPolicy,
		SandboxMode:           profile.Runtime.SandboxMode,
		DeveloperInstructions: a.renderMainAgentDeveloperInstructions(profile, selectedHostID, false),
		ThreadConfigHash:      a.mainAgentThreadConfigHash(selectedHostID),
	}
	if threadConfig := a.buildMainAgentThreadConfig(ctx, profile, selectedHostID); len(threadConfig) > 0 {
		spec.Config = threadConfig
	}
	if isRemoteHostID(selectedHostID) {
		spec.DynamicTools = a.remoteDynamicTools()
	} else {
		// server-local: add local execution tools so the agent can run commands locally.
		spec.DynamicTools = a.localDynamicTools()
	}
	// Merge Coroot tools when configured.
	if corootTools := a.corootDynamicTools(); len(corootTools) > 0 {
		spec.DynamicTools = append(spec.DynamicTools, corootTools...)
	}
	return spec
}

func (a *App) buildSingleHostTurnStartSpec(ctx context.Context, req chatRequest) turnStartSpec {
	profile := a.mainAgentProfile()
	devInstructions := a.renderMainAgentDeveloperInstructions(profile, req.HostID, true)
	if req.MonitorContext != nil {
		if prefix := model.MonitorContextPromptPrefix(*req.MonitorContext); prefix != "" {
			devInstructions = prefix + "\n\n" + devInstructions
		}
	}
	return turnStartSpec{
		Cwd:                   a.cfg.DefaultWorkspace,
		ApprovalPolicy:        profile.Runtime.ApprovalPolicy,
		SandboxMode:           profile.Runtime.SandboxMode,
		WritableRoots:         a.mainAgentWritableRoots(profile),
		DeveloperInstructions: devInstructions,
		Input:                 a.buildMainAgentTurnInput(ctx, profile, req.Message),
		ReasoningEffort:       profile.Runtime.ReasoningEffort,
	}
}

func (a *App) singleHostReActThreadConfigHash(hostID string) string {
	return a.mainAgentThreadConfigHash(hostID) + ":" + reActLoopVersion
}

func (a *App) workspaceReActThreadConfigHash(hostID string) string {
	return a.mainAgentThreadConfigHash(hostID) + ":workspace-" + reActLoopVersion
}

func (a *App) buildSingleHostReActThreadStartSpec(ctx context.Context, sessionID string) threadStartSpec {
	spec := a.buildSingleHostThreadStartSpec(ctx, sessionID)
	session := a.store.EnsureSession(sessionID)
	selectedHostID := defaultHostID(session.SelectedHostID)
	spec.DeveloperInstructions = appendReActLoopInstructions(
		spec.DeveloperInstructions,
		a.buildReActLoopInstructions(reActLoopKindSingleHost, sessionID, selectedHostID, false),
	)
	spec.DynamicTools = append([]map[string]any{askUserQuestionDynamicTool()}, spec.DynamicTools...)
	spec.ThreadConfigHash = a.singleHostReActThreadConfigHash(selectedHostID)
	return spec
}

func (a *App) buildSingleHostReActTurnStartSpec(ctx context.Context, sessionID string, req chatRequest) turnStartSpec {
	spec := a.buildSingleHostTurnStartSpec(ctx, req)
	spec.DeveloperInstructions = appendReActLoopInstructions(
		spec.DeveloperInstructions,
		a.buildReActLoopInstructions(reActLoopKindSingleHost, sessionID, defaultHostID(req.HostID), true),
	)
	return spec
}

func (a *App) buildWorkspaceReActThreadStartSpec(ctx context.Context, sessionID, hostID string) threadStartSpec {
	selectedHostID := defaultHostID(hostID)
	profile := a.mainAgentProfile()
	developerInstructions := a.buildWorkspaceReActDeveloperInstructions(sessionID, selectedHostID, "", "", false)
	spec := threadStartSpec{
		Model:                 profile.Runtime.Model,
		Cwd:                   a.cfg.DefaultWorkspace,
		ApprovalPolicy:        profile.Runtime.ApprovalPolicy,
		SandboxMode:           profile.Runtime.SandboxMode,
		DeveloperInstructions: developerInstructions,
		DynamicTools:          a.workspaceDynamicTools(sessionID),
		ThreadConfigHash:      a.workspaceReActThreadConfigHash(selectedHostID),
	}
	if threadConfig := a.buildMainAgentThreadConfig(ctx, profile, selectedHostID); len(threadConfig) > 0 {
		spec.Config = threadConfig
	}
	return spec
}

func (a *App) buildWorkspaceReActTurnStartSpec(ctx context.Context, sessionID, hostID, message string) turnStartSpec {
	selectedHostID := defaultHostID(hostID)
	profile := a.mainAgentProfile()
	developerInstructions := a.buildWorkspaceReActDeveloperInstructions(sessionID, selectedHostID, "", "", true)
	return turnStartSpec{
		Cwd:                   a.cfg.DefaultWorkspace,
		ApprovalPolicy:        profile.Runtime.ApprovalPolicy,
		SandboxMode:           profile.Runtime.SandboxMode,
		WritableRoots:         a.mainAgentWritableRoots(profile),
		DeveloperInstructions: developerInstructions,
		Input: []map[string]any{
			{"type": "text", "text": message},
		},
		ReasoningEffort: profile.Runtime.ReasoningEffort,
	}
}

func appendReActLoopInstructions(base, extra string) string {
	base = strings.TrimSpace(base)
	extra = strings.TrimSpace(extra)
	switch {
	case base == "":
		return extra
	case extra == "":
		return base
	default:
		return base + "\n\n" + extra
	}
}

func (a *App) buildWorkspaceReActDeveloperInstructions(sessionID, hostID, title, summary string, turnScoped bool) string {
	selectedHostID := defaultHostID(hostID)
	profile := a.mainAgentProfile()
	developerInstructions := orchestrator.BuildWorkspaceReActPrompt(strings.TrimSpace(title), strings.TrimSpace(summary))
	if base := a.renderMainAgentDeveloperInstructions(profile, selectedHostID, turnScoped); base != "" {
		developerInstructions = developerInstructions + "\n\n" + base
	}
	return appendReActLoopInstructions(
		developerInstructions,
		a.buildReActLoopInstructions(reActLoopKindWorkspace, sessionID, selectedHostID, turnScoped),
	)
}

func (a *App) buildReActLoopInstructions(kind, sessionID, hostID string, turnScoped bool) string {
	scope := "thread"
	if turnScoped {
		scope = "turn"
	}
	lines := []string{
		"ReAct agent loop runtime attachment:",
		"- Loop stages are explicit and replaceable: 1 context_preprocess, 2 attachment_injection, 3 model_stream_call, 4 error_recovery, 5 tool_execution, 6 postprocess, 7 loop_decision.",
		fmt.Sprintf("- Scope=%s session=%s kind=%s selectedHost=%s.", scope, sessionID, kind, defaultHostID(hostID)),
		"- Use a while-style ReAct loop mentally: reason, call tools only when needed, observe tool results, then continue or finish.",
		"- If the next action would inspect hosts, start worker dispatch, or mutate state and the user's intent is ambiguous, ask the user first with ask_user_question (the platform AskUserQuestion equivalent).",
		"- For max-token continuation, resume directly with no apology and no recap.",
	}
	if kind == reActLoopKindWorkspace {
		lines = append(lines,
			"- Workspace policy: do not expose route/planner internals; do not dispatch workers until the user has clearly authorized execution or approved the plan.",
			"- Workspace policy: single-host readonly checks must use readonly_host_inspect or other readonly tools only; server-local readonly diagnosis must not use built-in commandExecution because it bypasses workspace evidence projection.",
			"- Workspace plan mode: use enter_plan_mode for complex or high-risk planning, update_plan for the plan evidence, and exit_plan_mode to request approval before DispatchWorkers/orchestrator_dispatch_tasks.",
			"- Workspace tool-result contract: if a tool result contains next_required_tool or required_next_tool, call that tool next; do not answer in plain text or repeat the same clarification.",
		)
	} else {
		lines = append(lines,
			"- Single-host policy: act only on the selected host context; ask before high-risk or ambiguous operations.",
		)
	}
	return strings.Join(lines, "\n")
}

func (a *App) buildWorkerThreadStartSpec(mission *orchestrator.Mission, task *orchestrator.TaskRun, hostID string) threadStartSpec {
	localWorkspace := orchestrator.WorkerLocalWorkspacePath(a.cfg.DefaultWorkspace, mission.ID, hostID)
	remoteWorkspace := orchestrator.WorkerRemoteWorkspacePath(orchestratorRemoteWorkspaceRoot, mission.ID, hostID)
	preset := orchestrator.WorkerPreset(localWorkspace, hostID)
	return threadStartSpec{
		Model:                 preset.Model,
		Cwd:                   localWorkspace,
		ApprovalPolicy:        preset.ApprovalPolicy,
		SandboxMode:           preset.SandboxMode,
		DeveloperInstructions: orchestrator.BuildWorkerPrompt(hostID, task.Title, task.Instruction, task.Constraints, remoteWorkspace),
		DynamicTools:          a.remoteDynamicTools(),
		ThreadConfigHash:      mission.ID + ":" + hostID + ":" + string(preset.RuntimePreset),
	}
}

func (a *App) buildWorkerTurnStartSpec(mission *orchestrator.Mission, task *orchestrator.TaskRun, hostID string) turnStartSpec {
	localWorkspace := orchestrator.WorkerLocalWorkspacePath(a.cfg.DefaultWorkspace, mission.ID, hostID)
	remoteWorkspace := orchestrator.WorkerRemoteWorkspacePath(orchestratorRemoteWorkspaceRoot, mission.ID, hostID)
	preset := orchestrator.WorkerPreset(localWorkspace, hostID)
	instruction := orchestrator.BuildWorkerPrompt(hostID, task.Title, task.Instruction, task.Constraints, remoteWorkspace)
	return turnStartSpec{
		Cwd:                   localWorkspace,
		ApprovalPolicy:        preset.ApprovalPolicy,
		SandboxMode:           preset.SandboxMode,
		WritableRoots:         []string{localWorkspace},
		DeveloperInstructions: remoteTurnDeveloperInstructions(hostID),
		Input: []map[string]any{
			{"type": "text", "text": instruction},
		},
		ReasoningEffort: preset.ReasoningEffort,
	}
}
