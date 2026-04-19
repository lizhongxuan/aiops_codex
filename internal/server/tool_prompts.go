package server

import (
	"strings"

	toolprompts "github.com/lizhongxuan/aiops-codex/internal/toolprompts"
)

type ToolPromptSpec = toolprompts.Spec

type toolPromptVariant string

const (
	toolPromptVariantShared toolPromptVariant = "shared"
	toolPromptVariantLocal  toolPromptVariant = "local"
	toolPromptVariantRemote toolPromptVariant = "remote"
)

type toolPromptEntry struct {
	shared ToolPromptSpec
	local  *ToolPromptSpec
	remote *ToolPromptSpec
}

func (e toolPromptEntry) Description(variant toolPromptVariant) string {
	switch variant {
	case toolPromptVariantLocal:
		if e.local != nil {
			return e.local.Description()
		}
	case toolPromptVariantRemote:
		if e.remote != nil {
			return e.remote.Description()
		}
	}
	return e.shared.Description()
}

var toolPromptRegistry = map[string]toolPromptEntry{
	"web_search": {
		shared: ToolPromptSpec{
			Name:       "web_search",
			Capability: "Search the web for up-to-date external information and candidate sources.",
			Constraints: []string{
				"Use it for realtime or external facts when current information matters.",
				"Search results are evidence inputs; continue with follow-up reading when the initial results are insufficient or conflicting.",
			},
			ResultShape: []string{
				"Returns ranked results with titles, URLs, and snippets to support source attribution.",
			},
		},
	},
	"open_page": {
		shared: ToolPromptSpec{
			Name:       "open_page",
			Capability: "Fetch a web page and read its text content.",
			Constraints: []string{
				"Use it after web_search when you need to inspect a source page directly.",
				"It reads content only and does not generate conclusions on its own.",
			},
			ResultShape: []string{
				"Returns the page text content for further inspection.",
			},
		},
	},
	"find_in_page": {
		shared: ToolPromptSpec{
			Name:       "find_in_page",
			Capability: "Search within a fetched page's content.",
			Constraints: []string{
				"Use it to locate specific terms or figures inside a page you already fetched.",
				"It only narrows relevant passages within existing page content.",
			},
			ResultShape: []string{
				"Returns matching sections from the fetched page.",
			},
		},
	},
	"load_skill_context": {
		shared: ToolPromptSpec{
			Name:       "load_skill_context",
			Capability: "Load configured skill context for the main conversation from the active agent profile and discovered SKILL.md files.",
			Constraints: []string{
				"Use it as a compatibility wrapper over the current skill catalog and implicit injection flow, not as full Claude slash-command execution.",
				"It reports which skills were injected for the current request and which explicit requests could not be satisfied from discovered skill paths.",
			},
			ResultShape: []string{
				"Returns matched skills, explicit or implicit activation mode, injected skill input items, and an injected-context summary.",
			},
		},
	},
	"ask_user_question": {
		shared: toolprompts.AskUserQuestion,
	},
	"list_remote_files": {
		shared: ToolPromptSpec{
			Name:       "list_remote_files",
			Capability: "List filesystem entries under a known directory.",
			Constraints: []string{
				"Use it when you already know the directory path and need a bounded listing result.",
				"It currently provides directory listing parity, not full Claude glob-pattern semantics.",
			},
			ResultShape: []string{
				"Returns entry counts plus file and directory items with paths and kinds.",
			},
		},
		remote: &ToolPromptSpec{
			Name:       "list_remote_files",
			Capability: "List filesystem entries under a known directory on the currently selected remote host.",
			Constraints: []string{
				"Host must exactly match the current selected remote host.",
				"It currently provides directory listing parity, not full Claude glob-pattern semantics.",
			},
			ResultShape: []string{
				"Returns entry counts plus file and directory items with paths and kinds.",
			},
		},
	},
	"read_remote_file": {
		shared: ToolPromptSpec{
			Name:       "read_remote_file",
			Capability: "Read a text file when you already know the path.",
			Constraints: []string{
				"Use it for text inspection only; it does not edit files or infer missing paths.",
				"Prefer it over shell cat/head when a direct file-read tool is available.",
			},
			ResultShape: []string{
				"Returns file text plus preview-oriented metadata for the requested path.",
			},
		},
		remote: &ToolPromptSpec{
			Name:       "read_remote_file",
			Capability: "Read a text file on the currently selected remote host when you already know the path.",
			Constraints: []string{
				"Host must exactly match the current selected remote host.",
				"Use it for text inspection only; it does not edit files or infer missing paths.",
			},
			ResultShape: []string{
				"Returns file text plus preview-oriented metadata for the requested path.",
			},
		},
	},
	"search_remote_files": {
		shared: ToolPromptSpec{
			Name:       "search_remote_files",
			Capability: "Search for text within files under a known path.",
			Constraints: []string{
				"Use it when you know the directory or file scope and need content matches.",
				"It currently exposes grep-style text search results, not the full Claude grep option surface.",
			},
			ResultShape: []string{
				"Returns the searched pattern, match counts, and file-hit locations.",
			},
		},
		remote: &ToolPromptSpec{
			Name:       "search_remote_files",
			Capability: "Search for text within files on the currently selected remote host under a known path.",
			Constraints: []string{
				"Host must exactly match the current selected remote host.",
				"It currently exposes grep-style text search results, not the full Claude grep option surface.",
			},
			ResultShape: []string{
				"Returns the searched pattern, match counts, and file-hit locations.",
			},
		},
	},
	"write_file": {
		shared: ToolPromptSpec{
			Name:       "write_file",
			Capability: "Write content to a file through a single FileWriteTool-style facade.",
			Constraints: []string{
				"Use it when you already know the target path and want create, overwrite, or append semantics rather than patch-style editing.",
				"It keeps write intent separate from apply_patch and other diff-oriented edit flows.",
			},
			ResultShape: []string{
				"Returns file-write summaries, change metadata, and FileChangeCard-friendly descriptors after approval and execution.",
			},
			ApprovalNote: "This tool always requires user approval before execution.",
		},
		remote: &ToolPromptSpec{
			Name:       "write_file",
			Capability: "Write content to a file on the currently selected remote host through a single FileWriteTool-style facade.",
			Constraints: []string{
				"Host must exactly match the current selected remote host.",
				"Omit write_mode to default to overwrite, or use append when you need additive writes.",
				"Keep patch-style edits in apply_patch or other diff-specific edit flows rather than overloading write intent.",
			},
			ResultShape: []string{
				"Returns approval context first, then file-write summaries, change metadata, and FileChangeCard-friendly descriptors after approval and execution.",
			},
			ApprovalNote: "This tool always requires user approval before execution.",
		},
	},
	"apply_patch": {
		shared: ToolPromptSpec{
			Name:       "apply_patch",
			Capability: "Edit existing files through a single diff-oriented patch facade.",
			Constraints: []string{
				"Use it for targeted edits to existing files when patch-style changes are clearer than whole-file overwrite.",
				"Provide a valid unified diff patch and keep the patch as small as possible while still uniquely locating the intended edit.",
				"Prefer write_file for create, overwrite, or append flows instead of overloading patch intent.",
			},
			ResultShape: []string{
				"Returns patch summaries, per-file diff metadata, and FileChangeCard-friendly descriptors after approval and execution.",
			},
			ApprovalNote: "This tool always requires user approval before execution.",
		},
	},
	"host_file_read": {
		shared: ToolPromptSpec{
			Name:       "host_file_read",
			Capability: "Read a text file through the structured host inspection channel.",
			Constraints: []string{
				"Use it when only the host structured-read path is available.",
				"It is line-limited and remains a compatibility subset of FileReadTool.",
			},
			ResultShape: []string{
				"Returns preview-oriented file content for the requested path.",
			},
		},
	},
	"host_file_search": {
		shared: ToolPromptSpec{
			Name:       "host_file_search",
			Capability: "Search for text through the structured host inspection channel.",
			Constraints: []string{
				"Use it when only the host structured-read path is available.",
				"It is a compatibility subset of GrepTool built on bounded grep output.",
			},
			ResultShape: []string{
				"Returns the searched pattern plus matched file locations.",
			},
		},
	},
	"query_ai_server_state": {
		shared: toolprompts.QueryAIServerState,
	},
	"readonly_host_inspect": {
		shared: toolprompts.ReadonlyHostInspect,
	},
	"enter_plan_mode": {
		shared: toolprompts.EnterPlanMode,
	},
	"update_plan": {
		shared: toolprompts.UpdatePlan,
	},
	"exit_plan_mode": {
		shared: toolprompts.ExitPlanMode,
	},
	"orchestrator_dispatch_tasks": {
		shared: toolprompts.OrchestratorDispatchTasks,
	},
	"request_approval": {
		shared: toolprompts.RequestApproval,
	},
	"execute_readonly_query": {
		shared: ToolPromptSpec{
			Name:       "execute_readonly_query",
			Capability: "Run a read-only shell command for system inspection.",
			Constraints: []string{
				"Use it for inspection only; do not install packages, restart services, write files, delete data, or send process signals.",
				"Keep commands narrow and explain what you are checking.",
			},
			ResultShape: []string{
				"Returns the command output needed for diagnosis and evidence.",
			},
		},
		local: &ToolPromptSpec{
			Name:       "execute_readonly_query",
			Capability: "Run a read-only shell command on server-local for system inspection.",
			Constraints: []string{
				`Always set host="server-local".`,
				"Use it for inspection only; do not install packages, restart services, write files, delete data, or send process signals.",
				"Keep commands narrow and explain what you are checking.",
			},
			ResultShape: []string{
				"Returns the command output needed for diagnosis and evidence.",
			},
		},
		remote: &ToolPromptSpec{
			Name:       "execute_readonly_query",
			Capability: "Run a read-only shell command on the currently selected remote host.",
			Constraints: []string{
				"Host must exactly match the current selected remote host.",
				"Use it for inspection only; do not install packages, restart services, write files, delete data, or send process signals.",
				"Remote commands run without a shell wrapper; split complex work into multiple direct commands.",
			},
			ResultShape: []string{
				"Returns the command output needed for diagnosis and evidence.",
			},
		},
	},
	"execute_system_mutation": {
		shared: ToolPromptSpec{
			Name:       "execute_system_mutation",
			Capability: "Run a state-changing shell command or file change for controlled mutation.",
			Constraints: []string{
				"Use it for restarts, installs, config changes, process control, or other write operations.",
			},
			ResultShape: []string{
				"Returns execution results after approval and execution.",
			},
			ApprovalNote: "This tool always requires user approval before execution.",
		},
		local: &ToolPromptSpec{
			Name:       "execute_system_mutation",
			Capability: "Run a state-changing shell command on server-local.",
			Constraints: []string{
				`Always set host="server-local".`,
				"Use it for restarts, installs, config changes, process control, or other write operations.",
			},
			ResultShape: []string{
				"Returns execution results after approval and execution.",
			},
			ApprovalNote: "This tool always requires user approval before execution.",
		},
		remote: &ToolPromptSpec{
			Name:       "execute_system_mutation",
			Capability: "Run a state-changing command or file change on the currently selected remote host.",
			Constraints: []string{
				"Host must exactly match the current selected remote host.",
				"Use command mode for shell-based mutations and file_change mode for direct file edits.",
			},
			ResultShape: []string{
				"Returns approval context first, then execution results after approval and execution.",
			},
			ApprovalNote: "This tool always requires user approval before execution.",
		},
	},
	shellCommandToolName: {
		shared: ToolPromptSpec{
			Name:       shellCommandToolName,
			Capability: "Run a shell command through a single BashTool-style facade.",
			Constraints: []string{
				"Use it when you want one command tool that keeps read-only inspection immediate and routes state-changing commands into the existing approval flow.",
				"Do not use it to bypass approval, readonly validation, or host selection rules.",
			},
			ResultShape: []string{
				"Returns command execution output plus command-card-friendly summaries and stdout or stderr highlights.",
			},
			ApprovalNote: "State-changing commands still require user approval before execution.",
		},
		local: &ToolPromptSpec{
			Name:       shellCommandToolName,
			Capability: "Run a shell command on server-local through a single BashTool-style facade.",
			Constraints: []string{
				`Always set host="server-local".`,
				"Read-only inspection commands run immediately; state-changing commands enter the existing approval flow.",
			},
			ResultShape: []string{
				"Returns command execution output plus command-card-friendly summaries and stdout or stderr highlights.",
			},
			ApprovalNote: "State-changing commands still require user approval before execution.",
		},
		remote: &ToolPromptSpec{
			Name:       shellCommandToolName,
			Capability: "Run a shell command on the currently selected remote host through a single BashTool-style facade.",
			Constraints: []string{
				"Host must exactly match the current selected remote host.",
				"Remote commands run without a shell wrapper; split complex work into multiple direct commands.",
				"Read-only inspection commands run immediately; state-changing commands enter the existing approval flow.",
			},
			ResultShape: []string{
				"Returns command execution output plus command-card-friendly summaries and stdout or stderr highlights.",
			},
			ApprovalNote: "State-changing commands still require user approval before execution.",
		},
	},
}

func lookupToolPromptEntry(name string) (toolPromptEntry, bool) {
	entry, ok := toolPromptRegistry[strings.TrimSpace(name)]
	return entry, ok
}

func toolPromptDescription(name string) string {
	if entry, ok := lookupToolPromptEntry(name); ok {
		return entry.Description(toolPromptVariantShared)
	}
	return ""
}

func localToolPromptDescription(name string) string {
	if entry, ok := lookupToolPromptEntry(name); ok {
		return entry.Description(toolPromptVariantLocal)
	}
	return toolPromptDescription(name)
}

func remoteToolPromptDescription(name string) string {
	if entry, ok := lookupToolPromptEntry(name); ok {
		return entry.Description(toolPromptVariantRemote)
	}
	return toolPromptDescription(name)
}
