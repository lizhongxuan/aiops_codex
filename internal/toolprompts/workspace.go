package toolprompts

var (
	AskUserQuestion = Spec{
		Name:       "ask_user_question",
		Capability: "Ask the user to clarify ambiguous intent, scope, or authorization before inspection, worker dispatch, or mutation.",
		Constraints: []string{
			"Use it when the user might be asking about capabilities rather than authorizing execution.",
			"Ask one to three concise structured questions instead of freeform approval prose.",
		},
		ResultShape: []string{
			"Returns a choice-style clarification card for the current workspace turn.",
		},
	}
	QueryAIServerState = Spec{
		Name:       "query_ai_server_state",
		Capability: "Read the current ai-server workspace, session, host, and approval state.",
		Constraints: []string{
			"Use it before filesystem inspection when the user asks about current project-local state.",
			"Do not substitute shell traversal or directory guesses for this tool.",
		},
		ResultShape: []string{
			"Returns a structured workspace state snapshot for the current project.",
		},
	}
	ReadonlyHostInspect = Spec{
		Name:       "readonly_host_inspect",
		Capability: "Run a bounded read-only inspection command on the currently selected host.",
		Constraints: []string{
			"Host must exactly match the current selected host.",
			"Never write files, restart services, kill processes, install packages, or mutate configuration.",
			"Use it after the user explicitly asks for readonly diagnosis.",
		},
		ResultShape: []string{
			"Returns inspection output suitable for evidence and diagnosis.",
		},
	}
	EnterPlanMode = Spec{
		Name:       "enter_plan_mode",
		Capability: "Enter formal plan mode for a complex or risky workspace task.",
		Constraints: []string{
			"While in plan mode, clarify intent, inspect read-only context, and update the plan only.",
			"Do not dispatch workers or perform mutation until exit_plan_mode is approved.",
		},
		ResultShape: []string{
			"Returns a plan-mode transition for the current workspace turn.",
		},
	}
	UpdatePlan = Spec{
		Name:       "update_plan",
		Capability: "Update the current workspace plan while in plan mode.",
		Constraints: []string{
			"Use it for planning only; it does not authorize execution.",
			"Include concrete steps, evidence, risks, rollback, and validation details in the plan payload.",
		},
		ResultShape: []string{
			"Returns a structured plan card with normalized steps for the current workspace turn.",
		},
	}
	ExitPlanMode = Spec{
		Name:       "exit_plan_mode",
		Capability: "Submit the completed plan for user approval before execution.",
		Constraints: []string{
			"This is the only plan-mode exit into execution.",
			"It must create a plan approval instead of asking for approval in plain text.",
		},
		ResultShape: []string{
			"Returns a pending plan approval referencing the approved summary, risks, rollback, validation, and tasks.",
		},
		ApprovalNote: "This tool creates a plan approval request before any execution begins.",
	}
	OrchestratorDispatchTasks = Spec{
		Name:       "orchestrator_dispatch_tasks",
		Capability: "Dispatch structured worker subtasks from the workspace session through the orchestrator AgentTool-style facade.",
		Constraints: []string{
			"This tool is unavailable until exit_plan_mode is approved.",
			"Use it only after exit_plan_mode is approved and execution is explicitly authorized.",
			"Do not call it while still in plan mode, while waiting for plan approval, or for ambiguous capability questions.",
			"Each task must target an online executable remote host; server-local is not a worker target.",
		},
		ResultShape: []string{
			"Returns agent-target summaries plus accepted, activated, and queued worker counts.",
			"Projects workspace worker status and subtask summaries without changing existing mission or worker semantics.",
		},
	}
	RequestApproval = Spec{
		Name:       "request_approval",
		Capability: "Request approval for a state-changing command or operation with explicit host, risk, impact, and rollback context.",
		Constraints: []string{
			"Use it for mutation operations such as config changes, restarts, package installs, process control, or destructive actions.",
			"Do not ask for approval in plain text; include the exact command, target host, risk assessment, expected impact, and rollback suggestion.",
		},
		ResultShape: []string{
			"Returns a pending approval request and does not execute the mutation until the user approves it.",
		},
		ApprovalNote: "This tool always requires user approval before execution.",
	}
)
