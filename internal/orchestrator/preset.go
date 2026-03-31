package orchestrator

import "strings"

type Preset struct {
	Name            string        `json:"name"`
	Kind            SessionKind   `json:"kind"`
	RuntimePreset   RuntimePreset `json:"runtimePreset"`
	Model           string        `json:"model"`
	ReasoningEffort string        `json:"reasoningEffort,omitempty"`
	ApprovalPolicy  string        `json:"approvalPolicy,omitempty"`
	SandboxMode     string        `json:"sandboxMode,omitempty"`
	WorkspacePath   string        `json:"workspacePath,omitempty"`
	Prompt          string        `json:"prompt,omitempty"`
	DynamicTools    []string      `json:"dynamicTools,omitempty"`
}

type RuntimeSpec struct {
	Preset                Preset
	Model                 string
	ReasoningEffort       string
	ApprovalPolicy        string
	SandboxMode           string
	Cwd                   string
	DeveloperInstructions string
	DynamicTools          []string
}

func PlannerPreset(workspacePath string) Preset {
	return Preset{
		Name:            "planner",
		Kind:            SessionKindPlanner,
		RuntimePreset:   RuntimePresetPlannerInternal,
		Model:           "gpt-5.4",
		ReasoningEffort: "medium",
		ApprovalPolicy:  "untrusted",
		SandboxMode:     "workspace-write",
		WorkspacePath:   workspacePath,
		DynamicTools:    []string{"metrics", "skills", "orchestrator_dispatch_tasks"},
	}
}

func WorkerPreset(workspacePath string, hostID string) Preset {
	_ = hostID
	return Preset{
		Name:            "worker",
		Kind:            SessionKindWorker,
		RuntimePreset:   RuntimePresetWorkerInternal,
		Model:           "gpt-5.4-mini",
		ReasoningEffort: "low",
		ApprovalPolicy:  "untrusted",
		SandboxMode:     "workspace-write",
		WorkspacePath:   workspacePath,
		DynamicTools:    []string{"execute_readonly_query", "list_remote_files", "read_remote_file", "search_remote_files", "execute_system_mutation"},
	}
}

func WorkspacePreset(workspacePath string) Preset {
	return Preset{
		Name:            "workspace",
		Kind:            SessionKindWorkspace,
		RuntimePreset:   RuntimePresetWorkspaceFront,
		Model:           "gpt-5.4",
		ReasoningEffort: "medium",
		ApprovalPolicy:  "untrusted",
		SandboxMode:     "workspace-write",
		WorkspacePath:   workspacePath,
	}
}

func SingleHostPreset(workspacePath string) Preset {
	return Preset{
		Name:            "single_host",
		Kind:            SessionKindSingleHost,
		RuntimePreset:   RuntimePresetSingleHostDefault,
		Model:           "gpt-5.4",
		ReasoningEffort: "medium",
		ApprovalPolicy:  "untrusted",
		SandboxMode:     "workspace-write",
		WorkspacePath:   workspacePath,
	}
}

func (p Preset) RuntimeSpec() RuntimeSpec {
	return RuntimeSpec{
		Preset:                p,
		Model:                 p.Model,
		ReasoningEffort:       p.ReasoningEffort,
		ApprovalPolicy:        p.ApprovalPolicy,
		SandboxMode:           p.SandboxMode,
		Cwd:                   strings.TrimSpace(p.WorkspacePath),
		DeveloperInstructions: "",
		DynamicTools:          append([]string(nil), p.DynamicTools...),
	}
}
