package tools

import toolprompts "github.com/lizhongxuan/aiops-codex/internal/toolprompts"

// RegisterWorkspaceTools registers the workspace-level tool definitions into
// the given ToolRegistry. Handlers are placeholder stubs — actual
// implementations are wired during server integration (task 15).
func RegisterWorkspaceTools(reg *ToolRegistry) {
	reg.Register(ToolEntry{
		Name:        "ask_user_question",
		Description: toolprompts.AskUserQuestion.Description(),
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"questions": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"header":   map[string]interface{}{"type": "string", "description": "Short UI header."},
							"question": map[string]interface{}{"type": "string", "description": "Concrete question for the user."},
						},
						"required": []string{"question"},
					},
					"minItems": 1, "maxItems": 3,
					"description": "One to three concise clarification questions.",
				},
			},
			"required":             []string{"questions"},
			"additionalProperties": false,
		},
	})

	reg.Register(ToolEntry{
		Name:        "query_ai_server_state",
		Description: toolprompts.QueryAIServerState.Description(),
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"focus":  map[string]interface{}{"type": "string", "description": "Optional area to emphasize in the returned ai-server state snapshot."},
				"reason": map[string]interface{}{"type": "string", "description": "Short explanation of what state is being checked."},
			},
			"additionalProperties": false,
		},
		IsReadOnly: true,
	})

	reg.Register(ToolEntry{
		Name:        "readonly_host_inspect",
		Description: toolprompts.ReadonlyHostInspect.Description(),
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"host":        map[string]interface{}{"type": "string", "description": "Required selected host ID."},
				"target":      map[string]interface{}{"type": "string", "description": "Short inspection target."},
				"command":     map[string]interface{}{"type": "string", "description": "Single read-only shell command."},
				"cwd":         map[string]interface{}{"type": "string", "description": "Optional working directory on the selected host."},
				"timeout_sec": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 120, "description": "Optional timeout in seconds."},
				"reason":      map[string]interface{}{"type": "string", "description": "One-sentence explanation of what this readonly command checks."},
			},
			"required":             []string{"host", "target", "command", "reason"},
			"additionalProperties": false,
		},
		IsReadOnly: true,
	})

	reg.Register(ToolEntry{
		Name:        "enter_plan_mode",
		Description: toolprompts.EnterPlanMode.Description(),
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"goal":   map[string]interface{}{"type": "string", "description": "The user-facing goal of the plan."},
				"reason": map[string]interface{}{"type": "string", "description": "Why plan mode is needed."},
				"scope":  map[string]interface{}{"type": "string", "description": "What is in scope for planning."},
			},
			"required":             []string{"goal", "reason"},
			"additionalProperties": false,
		},
		IsReadOnly: true,
	})

	reg.Register(ToolEntry{
		Name:        "update_plan",
		Description: toolprompts.UpdatePlan.Description(),
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title": map[string]interface{}{"type": "string"}, "summary": map[string]interface{}{"type": "string"},
				"background": map[string]interface{}{"type": "string"}, "scope": map[string]interface{}{"type": "string"},
				"risk": map[string]interface{}{"type": "string"}, "rollback": map[string]interface{}{"type": "string"},
				"validation": map[string]interface{}{"type": "string"},
				"steps": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"id": map[string]interface{}{"type": "string"}, "title": map[string]interface{}{"type": "string"},
							"description": map[string]interface{}{"type": "string"}, "status": map[string]interface{}{"type": "string"},
							"hostId": map[string]interface{}{"type": "string"},
						},
						"additionalProperties": false,
					},
				},
			},
			"required":             []string{"summary"},
			"additionalProperties": false,
		},
		IsReadOnly: true,
	})

	reg.Register(ToolEntry{
		Name:        "exit_plan_mode",
		Description: toolprompts.ExitPlanMode.Description(),
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"summary":    map[string]interface{}{"type": "string", "description": "Plan summary."},
				"risk":       map[string]interface{}{"type": "string", "description": "Risk assessment."},
				"rollback":   map[string]interface{}{"type": "string", "description": "Rollback strategy."},
				"validation": map[string]interface{}{"type": "string", "description": "Validation steps."},
				"tasks": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"taskId": map[string]interface{}{"type": "string"}, "hostId": map[string]interface{}{"type": "string"},
							"title": map[string]interface{}{"type": "string"}, "instruction": map[string]interface{}{"type": "string"},
						},
					},
				},
			},
			"required":             []string{"summary", "risk", "rollback", "validation", "tasks"},
			"additionalProperties": false,
		},
	})

	reg.Register(ToolEntry{
		Name:        "orchestrator_dispatch_tasks",
		Description: toolprompts.OrchestratorDispatchTasks.Description(),
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"missionTitle": map[string]interface{}{"type": "string", "description": "Optional mission title."},
				"summary":      map[string]interface{}{"type": "string", "description": "Optional mission summary."},
				"tasks": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"taskId":      map[string]interface{}{"type": "string", "description": "Stable task identifier."},
							"hostId":      map[string]interface{}{"type": "string", "description": "Target host ID."},
							"instruction": map[string]interface{}{"type": "string", "description": "Concrete worker instruction."},
						},
						"required": []string{"taskId", "hostId", "instruction"},
					},
					"minItems": 1,
				},
			},
			"required":             []string{"tasks"},
			"additionalProperties": false,
		},
	})

	reg.Register(ToolEntry{
		Name:        "request_approval",
		Description: toolprompts.RequestApproval.Description(),
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command":            map[string]interface{}{"type": "string", "description": "The command or operation that requires approval."},
				"hostId":             map[string]interface{}{"type": "string", "description": "Target host ID for the operation."},
				"cwd":                map[string]interface{}{"type": "string", "description": "Working directory for the command."},
				"riskAssessment":     map[string]interface{}{"type": "string", "description": "Assessment of risks involved in this operation."},
				"expectedImpact":     map[string]interface{}{"type": "string", "description": "Expected impact of the operation on the system."},
				"rollbackSuggestion": map[string]interface{}{"type": "string", "description": "Suggested rollback steps if the operation fails."},
			},
			"required":             []string{"command", "hostId", "riskAssessment"},
			"additionalProperties": false,
		},
	})
}
