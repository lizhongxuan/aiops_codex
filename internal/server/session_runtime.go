package server

import (
	"context"
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

func (a *App) workspaceRouteThreadConfigHash(hostID string) string {
	return a.mainAgentThreadConfigHash(hostID) + ":workspace-route"
}

func (a *App) workspaceReadonlyThreadConfigHash(hostID string) string {
	return a.mainAgentThreadConfigHash(hostID) + ":workspace-readonly"
}

func (a *App) workspaceOrchestrationThreadConfigHash(hostID string) string {
	return a.mainAgentThreadConfigHash(hostID) + ":workspace-orchestration"
}

func (a *App) buildWorkspaceRouteThreadStartSpec(ctx context.Context, sessionID, hostID string) threadStartSpec {
	selectedHostID := defaultHostID(hostID)
	profile := a.mainAgentProfile()
	developerInstructions := orchestrator.BuildWorkspaceRoutePrompt()
	if base := a.renderMainAgentDeveloperInstructions(profile, selectedHostID, false); base != "" {
		developerInstructions = developerInstructions + "\n\n" + base
	}
	spec := threadStartSpec{
		Model:                 profile.Runtime.Model,
		Cwd:                   a.cfg.DefaultWorkspace,
		ApprovalPolicy:        profile.Runtime.ApprovalPolicy,
		SandboxMode:           profile.Runtime.SandboxMode,
		DeveloperInstructions: developerInstructions,
		DynamicTools:          a.workspaceRouteDynamicTools(),
		ThreadConfigHash:      a.workspaceRouteThreadConfigHash(selectedHostID),
	}
	if threadConfig := a.buildMainAgentThreadConfig(ctx, profile, selectedHostID); len(threadConfig) > 0 {
		spec.Config = threadConfig
	}
	return spec
}

func (a *App) buildWorkspaceRouteTurnStartSpec(ctx context.Context, hostID, message string) turnStartSpec {
	selectedHostID := defaultHostID(hostID)
	profile := a.mainAgentProfile()
	developerInstructions := orchestrator.BuildWorkspaceRoutePrompt()
	if base := a.renderMainAgentDeveloperInstructions(profile, selectedHostID, true); base != "" {
		developerInstructions = developerInstructions + "\n\n" + base
	}
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

func (a *App) buildWorkspaceReadonlyThreadStartSpec(ctx context.Context, sessionID, hostID string) threadStartSpec {
	selectedHostID := defaultHostID(hostID)
	profile := a.mainAgentProfile()
	developerInstructions := orchestrator.BuildWorkspaceReadonlyPrompt()
	if base := a.renderMainAgentDeveloperInstructions(profile, selectedHostID, false); base != "" {
		developerInstructions = developerInstructions + "\n\n" + base
	}
	spec := threadStartSpec{
		Model:                 profile.Runtime.Model,
		Cwd:                   a.cfg.DefaultWorkspace,
		ApprovalPolicy:        profile.Runtime.ApprovalPolicy,
		SandboxMode:           profile.Runtime.SandboxMode,
		DeveloperInstructions: developerInstructions,
		DynamicTools:          a.workspaceDirectDynamicTools(sessionID),
		ThreadConfigHash:      a.workspaceReadonlyThreadConfigHash(selectedHostID),
	}
	if threadConfig := a.buildMainAgentThreadConfig(ctx, profile, selectedHostID); len(threadConfig) > 0 {
		spec.Config = threadConfig
	}
	return spec
}

func (a *App) buildWorkspaceReadonlyTurnStartSpec(ctx context.Context, hostID, message string) turnStartSpec {
	selectedHostID := defaultHostID(hostID)
	profile := a.mainAgentProfile()
	developerInstructions := orchestrator.BuildWorkspaceReadonlyPrompt()
	if base := a.renderMainAgentDeveloperInstructions(profile, selectedHostID, true); base != "" {
		developerInstructions = developerInstructions + "\n\n" + base
	}
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

func (a *App) buildWorkspaceOrchestrationThreadStartSpec(ctx context.Context, sessionID string, mission *orchestrator.Mission) threadStartSpec {
	session := a.store.EnsureSession(sessionID)
	selectedHostID := defaultHostID(session.SelectedHostID)
	profile := a.mainAgentProfile()
	developerInstructions := orchestrator.BuildWorkspacePrompt(strings.TrimSpace(mission.Title), strings.TrimSpace(mission.Summary))
	if base := a.renderMainAgentDeveloperInstructions(profile, selectedHostID, false); base != "" {
		developerInstructions = developerInstructions + "\n\n" + base
	}
	spec := threadStartSpec{
		Model:                 profile.Runtime.Model,
		Cwd:                   a.cfg.DefaultWorkspace,
		ApprovalPolicy:        profile.Runtime.ApprovalPolicy,
		SandboxMode:           profile.Runtime.SandboxMode,
		DeveloperInstructions: developerInstructions,
		DynamicTools:          a.workspaceDynamicTools(sessionID),
		ThreadConfigHash:      a.workspaceOrchestrationThreadConfigHash(selectedHostID),
	}
	if threadConfig := a.buildMainAgentThreadConfig(ctx, profile, selectedHostID); len(threadConfig) > 0 {
		spec.Config = threadConfig
	}
	return spec
}

func (a *App) buildWorkspaceOrchestrationTurnStartSpec(ctx context.Context, sessionID string, mission *orchestrator.Mission, message string) turnStartSpec {
	session := a.store.EnsureSession(sessionID)
	selectedHostID := defaultHostID(session.SelectedHostID)
	profile := a.mainAgentProfile()
	developerInstructions := orchestrator.BuildWorkspacePrompt(strings.TrimSpace(mission.Title), strings.TrimSpace(mission.Summary))
	if base := a.renderMainAgentDeveloperInstructions(profile, selectedHostID, true); base != "" {
		developerInstructions = developerInstructions + "\n\n" + base
	}
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
