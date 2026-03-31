package server

import (
	"context"

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
	return spec
}

func (a *App) buildSingleHostTurnStartSpec(ctx context.Context, req chatRequest) turnStartSpec {
	profile := a.mainAgentProfile()
	return turnStartSpec{
		Cwd:                   a.cfg.DefaultWorkspace,
		ApprovalPolicy:        profile.Runtime.ApprovalPolicy,
		SandboxMode:           profile.Runtime.SandboxMode,
		WritableRoots:         a.mainAgentWritableRoots(profile),
		DeveloperInstructions: a.renderMainAgentDeveloperInstructions(profile, req.HostID, true),
		Input:                 a.buildMainAgentTurnInput(ctx, profile, req.Message),
		ReasoningEffort:       profile.Runtime.ReasoningEffort,
	}
}

func (a *App) buildPlannerThreadStartSpec(mission *orchestrator.Mission) threadStartSpec {
	workspacePath := orchestrator.PlannerWorkspacePath(a.cfg.DefaultWorkspace, mission.ID)
	preset := orchestrator.PlannerPreset(workspacePath)
	return threadStartSpec{
		Model:                 preset.Model,
		Cwd:                   workspacePath,
		ApprovalPolicy:        preset.ApprovalPolicy,
		SandboxMode:           preset.SandboxMode,
		DeveloperInstructions: orchestrator.BuildPlannerPrompt(mission.Title, mission.Summary, len(mission.Tasks)),
		DynamicTools:          a.plannerDynamicTools(),
		ThreadConfigHash:      mission.ID + ":" + string(preset.RuntimePreset),
	}
}

func (a *App) buildPlannerTurnStartSpec(mission *orchestrator.Mission, message string) turnStartSpec {
	workspacePath := orchestrator.PlannerWorkspacePath(a.cfg.DefaultWorkspace, mission.ID)
	preset := orchestrator.PlannerPreset(workspacePath)
	return turnStartSpec{
		Cwd:                   workspacePath,
		ApprovalPolicy:        preset.ApprovalPolicy,
		SandboxMode:           preset.SandboxMode,
		WritableRoots:         []string{workspacePath},
		DeveloperInstructions: orchestrator.BuildPlannerPrompt(mission.Title, mission.Summary, len(mission.Tasks)),
		Input: []map[string]any{
			{"type": "text", "text": message},
		},
		ReasoningEffort: preset.ReasoningEffort,
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
