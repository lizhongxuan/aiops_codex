package agentloop

// RegisterWorkspaceTools registers the workspace-level tool definitions into
// the given ToolRegistry. Handlers are placeholder stubs — actual
// implementations are wired during server integration (task 15).
func RegisterWorkspaceTools(reg *ToolRegistry) {
	reg.Register(ToolEntry{
		Name:        "ask_user_question",
		Description: "Ask the user to clarify ambiguous intent, scope, or authorization before inspecting hosts, dispatching workers, or making changes.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"questions": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"header": map[string]interface{}{
								"type":        "string",
								"description": "Short UI header.",
							},
							"question": map[string]interface{}{
								"type":        "string",
								"description": "Concrete question for the user.",
							},
						},
						"required": []string{"question"},
					},
					"minItems":    1,
					"maxItems":    3,
					"description": "One to three concise clarification questions.",
				},
			},
			"required":             []string{"questions"},
			"additionalProperties": false,
		},
	})

	reg.Register(ToolEntry{
		Name:        "query_ai_server_state",
		Description: "Read the current ai-server workspace, host, approval, and runtime state for project-local status questions.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"focus": map[string]interface{}{
					"type":        "string",
					"description": "Optional area to emphasize in the returned ai-server state snapshot.",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Short explanation of what state is being checked.",
				},
			},
			"additionalProperties": false,
		},
		IsReadOnly: true,
	})

	reg.Register(ToolEntry{
		Name:        "readonly_host_inspect",
		Description: "Run a bounded read-only inspection command on the currently selected host, including server-local or an online remote host-agent.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"host": map[string]interface{}{
					"type":        "string",
					"description": "Required selected host ID.",
				},
				"target": map[string]interface{}{
					"type":        "string",
					"description": "Short inspection target.",
				},
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Single read-only shell command.",
				},
				"cwd": map[string]interface{}{
					"type":        "string",
					"description": "Optional working directory on the selected host.",
				},
				"timeout_sec": map[string]interface{}{
					"type":        "integer",
					"minimum":     1,
					"maximum":     120,
					"description": "Optional timeout in seconds.",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "One-sentence explanation of what this readonly command checks.",
				},
			},
			"required":             []string{"host", "target", "command", "reason"},
			"additionalProperties": false,
		},
		IsReadOnly: true,
	})

	reg.Register(ToolEntry{
		Name:        "enter_plan_mode",
		Description: "Enter formal plan mode for a complex or risky workspace task. In plan mode the agent may clarify, inspect read-only context, and update the plan, but must not dispatch workers or perform mutation until exit_plan_mode is approved.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"goal": map[string]interface{}{
					"type":        "string",
					"description": "The user-facing goal of the plan.",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Why plan mode is needed.",
				},
				"scope": map[string]interface{}{
					"type":        "string",
					"description": "What is in scope for planning.",
				},
			},
			"required":             []string{"goal", "reason"},
			"additionalProperties": false,
		},
		IsReadOnly: true,
	})

	reg.Register(ToolEntry{
		Name:        "update_plan",
		Description: "Update the current workspace plan while in plan mode. This is a planning tool only and does not authorize execution.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title":      map[string]interface{}{"type": "string"},
				"summary":    map[string]interface{}{"type": "string"},
				"background": map[string]interface{}{"type": "string"},
				"scope":      map[string]interface{}{"type": "string"},
				"risk":       map[string]interface{}{"type": "string"},
				"rollback":   map[string]interface{}{"type": "string"},
				"validation": map[string]interface{}{"type": "string"},
				"steps": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"id":          map[string]interface{}{"type": "string"},
							"title":       map[string]interface{}{"type": "string"},
							"description": map[string]interface{}{"type": "string"},
							"status":      map[string]interface{}{"type": "string"},
							"hostId":      map[string]interface{}{"type": "string"},
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
		Description: "Submit the completed plan for user approval. This is the only plan-mode exit into execution.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"summary": map[string]interface{}{
					"type":        "string",
					"description": "Plan summary.",
				},
				"risk": map[string]interface{}{
					"type":        "string",
					"description": "Risk assessment.",
				},
				"rollback": map[string]interface{}{
					"type":        "string",
					"description": "Rollback strategy.",
				},
				"validation": map[string]interface{}{
					"type":        "string",
					"description": "Validation steps.",
				},
				"tasks": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"taskId":      map[string]interface{}{"type": "string"},
							"hostId":      map[string]interface{}{"type": "string"},
							"title":       map[string]interface{}{"type": "string"},
							"instruction": map[string]interface{}{"type": "string"},
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
		Description: "Dispatch structured host tasks to the orchestrator from the main workspace session. Only available after plan approval.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"missionTitle": map[string]interface{}{
					"type":        "string",
					"description": "Optional mission title.",
				},
				"summary": map[string]interface{}{
					"type":        "string",
					"description": "Optional mission summary.",
				},
				"tasks": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"taskId": map[string]interface{}{
								"type":        "string",
								"description": "Stable task identifier.",
							},
							"hostId": map[string]interface{}{
								"type":        "string",
								"description": "Target host ID.",
							},
							"instruction": map[string]interface{}{
								"type":        "string",
								"description": "Concrete worker instruction.",
							},
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
		Description: "Request approval for a mutation operation with command, host, risk assessment, expected impact, and rollback suggestion.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "The command or operation that requires approval.",
				},
				"hostId": map[string]interface{}{
					"type":        "string",
					"description": "Target host ID for the operation.",
				},
				"cwd": map[string]interface{}{
					"type":        "string",
					"description": "Working directory for the command.",
				},
				"riskAssessment": map[string]interface{}{
					"type":        "string",
					"description": "Assessment of risks involved in this operation.",
				},
				"expectedImpact": map[string]interface{}{
					"type":        "string",
					"description": "Expected impact of the operation on the system.",
				},
				"rollbackSuggestion": map[string]interface{}{
					"type":        "string",
					"description": "Suggested rollback steps if the operation fails.",
				},
			},
			"required":             []string{"command", "hostId", "riskAssessment"},
			"additionalProperties": false,
		},
	})
}
