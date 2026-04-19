package server

import (
	"strings"
	"testing"
)

func TestLocalThreadDeveloperInstructionsKeepRoutingButDropMarketAnswerStyle(t *testing.T) {
	instructions := localThreadDeveloperInstructions()
	if !strings.Contains(instructions, "ALWAYS use web_search") {
		t.Fatalf("expected local instructions to keep realtime routing guidance, got %q", instructions)
	}
	if strings.Contains(instructions, "1. execute_readonly_query:") {
		t.Fatalf("expected local instructions to drop tool-by-tool prose, got %q", instructions)
	}
	if strings.Contains(instructions, "compact snapshot style") {
		t.Fatalf("expected market answer style guidance to be removed, got %q", instructions)
	}
	if strings.Contains(instructions, "1-2 source links") {
		t.Fatalf("expected source-count guidance to be removed, got %q", instructions)
	}
}

func TestRemoteThreadDeveloperInstructionsFocusOnHostShellAndPathConstraints(t *testing.T) {
	instructions := remoteThreadDeveloperInstructions("prod-db-1")
	if !strings.Contains(instructions, `host="prod-db-1"`) {
		t.Fatalf("expected host binding guidance, got %q", instructions)
	}
	if !strings.Contains(instructions, "without a shell wrapper") {
		t.Fatalf("expected shell constraint guidance, got %q", instructions)
	}
	if strings.Contains(instructions, "Use list_remote_files") {
		t.Fatalf("expected remote instructions to drop tool-by-tool prose, got %q", instructions)
	}
}

func TestDynamicToolSchemasKeepTargetedFieldDescriptionsWiringOnly(t *testing.T) {
	app := newTestApp(t)

	remoteReadonly := findDynamicToolByName(t, app.remoteDynamicTools(), "execute_readonly_query")
	assertSchemaFieldDescription(t, remoteReadonly, "command",
		"Direct command to execute.",
		"selected remote host",
		"Read-only shell command to run",
	)
	assertSchemaFieldDescription(t, remoteReadonly, "reason",
		"Why this check is needed.",
		"what you are checking",
		"One-sentence explanation",
	)

	remoteMutation := findDynamicToolByName(t, app.remoteDynamicTools(), "execute_system_mutation")
	assertSchemaFieldDescription(t, remoteMutation, "command",
		"Command to run after approval when mode=command.",
		"Shell command to run",
		"file change",
	)
	assertSchemaFieldDescription(t, remoteMutation, "reason",
		"Why this change is needed.",
		"why this change is needed",
		"Short explanation",
	)

	remoteShell := findDynamicToolByName(t, app.remoteDynamicTools(), shellCommandToolName)
	if got := getStringAny(remoteShell, "description"); got != remoteToolPromptDescription(shellCommandToolName) {
		t.Fatalf("expected shell_command remote prompt description, got %q", got)
	}
	assertSchemaFieldDescription(t, remoteShell, "command",
		"Direct command to execute.",
		"Read-only shell command to run",
		"state-changing",
	)
	assertSchemaFieldDescription(t, remoteShell, "reason",
		"Why this command is needed.",
		"why this change is needed",
		"One-sentence explanation",
	)

	localReadonly := findDynamicToolByName(t, app.localDynamicTools(), "execute_readonly_query")
	assertSchemaFieldDescription(t, localReadonly, "command",
		"Direct command to execute.",
		"Read-only shell command to run",
		"server-local",
	)
	assertSchemaFieldDescription(t, localReadonly, "reason",
		"Why this check is needed.",
		"what you are checking",
		"One-sentence explanation",
	)

	localMutation := findDynamicToolByName(t, app.localDynamicTools(), "execute_system_mutation")
	assertSchemaFieldDescription(t, localMutation, "command",
		"Command to run after approval.",
		"Shell command to run after user approval",
		"state-changing",
	)
	assertSchemaFieldDescription(t, localMutation, "reason",
		"Why this change is needed.",
		"Short explanation",
		"why this change is needed",
	)

	localShell := findDynamicToolByName(t, app.localDynamicTools(), shellCommandToolName)
	if got := getStringAny(localShell, "description"); got != localToolPromptDescription(shellCommandToolName) {
		t.Fatalf("expected shell_command local prompt description, got %q", got)
	}
	assertSchemaFieldDescription(t, localShell, "command",
		"Direct command to execute.",
		"Read-only shell command to run",
		"server-local",
	)
	assertSchemaFieldDescription(t, localShell, "reason",
		"Why this command is needed.",
		"Short explanation",
		"why this change is needed",
	)

	readonlyInspect := readonlyHostInspectDynamicTool()
	askUserQuestion := askUserQuestionDynamicTool()
	if got := getStringAny(askUserQuestion, "description"); got != toolPromptDescription("ask_user_question") {
		t.Fatalf("expected ask_user_question prompt description, got %q", got)
	}

	enterPlanMode := enterPlanModeDynamicTool()
	if got := getStringAny(enterPlanMode, "description"); got != toolPromptDescription("enter_plan_mode") {
		t.Fatalf("expected enter_plan_mode prompt description, got %q", got)
	}

	updatePlan := updatePlanDynamicTool()
	if got := getStringAny(updatePlan, "description"); got != toolPromptDescription("update_plan") {
		t.Fatalf("expected update_plan prompt description, got %q", got)
	}

	exitPlanMode := exitPlanModeDynamicTool()
	if got := getStringAny(exitPlanMode, "description"); got != toolPromptDescription("exit_plan_mode") {
		t.Fatalf("expected exit_plan_mode prompt description, got %q", got)
	}

	requestApproval := requestApprovalDynamicTool()
	if got := getStringAny(requestApproval, "description"); got != toolPromptDescription("request_approval") {
		t.Fatalf("expected request_approval prompt description, got %q", got)
	}

	assertSchemaFieldDescription(t, readonlyInspect, "command",
		"Single command to execute.",
		"Single read-only shell command",
		"readonly command checks",
	)
	assertSchemaFieldDescription(t, readonlyInspect, "reason",
		"Why this inspection is needed.",
		"One-sentence explanation",
		"readonly command checks",
	)

	remoteList := findDynamicToolByName(t, app.remoteDynamicTools(), "list_remote_files")
	if got := getStringAny(remoteList, "description"); got != remoteToolPromptDescription("list_remote_files") {
		t.Fatalf("expected list_remote_files prompt description, got %q", got)
	}
	assertSchemaFieldDescription(t, remoteList, "path",
		"Directory path to inspect.",
		"directory tree",
		"currently selected remote host",
	)
	assertSchemaFieldDescription(t, remoteList, "reason",
		"Why this listing is needed.",
		"what you are trying to inspect",
		"Prefer this over shell commands",
	)

	remoteRead := findDynamicToolByName(t, app.remoteDynamicTools(), "read_remote_file")
	if got := getStringAny(remoteRead, "description"); got != remoteToolPromptDescription("read_remote_file") {
		t.Fatalf("expected read_remote_file prompt description, got %q", got)
	}
	assertSchemaFieldDescription(t, remoteRead, "path",
		"File path to inspect.",
		"selected remote host",
		"Prefer this over shell",
	)
	assertSchemaFieldDescription(t, remoteRead, "reason",
		"Why this read is needed.",
		"what you are checking in this file",
		"Prefer this over shell",
	)

	remoteSearch := findDynamicToolByName(t, app.remoteDynamicTools(), "search_remote_files")
	if got := getStringAny(remoteSearch, "description"); got != remoteToolPromptDescription("search_remote_files") {
		t.Fatalf("expected search_remote_files prompt description, got %q", got)
	}
	assertSchemaFieldDescription(t, remoteSearch, "query",
		"Pattern to search for.",
		"Text to search for",
		"structured search results",
	)
	assertSchemaFieldDescription(t, remoteSearch, "reason",
		"Why this search is needed.",
		"inspect search hits",
		"Prefer this over grep",
	)

	hostFileRead := findDynamicToolByName(t, structuredReadToolDefinitions(), hostFileReadToolName)
	if got := getStringAny(hostFileRead, "description"); got != toolPromptDescription("host_file_read") {
		t.Fatalf("expected host_file_read prompt description, got %q", got)
	}

	hostFileSearch := findDynamicToolByName(t, structuredReadToolDefinitions(), hostFileSearchToolName)
	if got := getStringAny(hostFileSearch, "description"); got != toolPromptDescription("host_file_search") {
		t.Fatalf("expected host_file_search prompt description, got %q", got)
	}
}

func findDynamicToolByName(t *testing.T, tools []map[string]any, name string) map[string]any {
	t.Helper()
	for _, tool := range tools {
		if getStringAny(tool, "name") == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

func assertSchemaFieldDescription(t *testing.T, tool map[string]any, field, want string, rejectSubstrings ...string) {
	t.Helper()
	schema, _ := tool["inputSchema"].(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	entry, _ := properties[field].(map[string]any)
	got := getStringAny(entry, "description")
	if got != want {
		t.Fatalf("tool %q field %q description mismatch: got %q want %q", getStringAny(tool, "name"), field, got, want)
	}
	for _, reject := range rejectSubstrings {
		if reject != "" && strings.Contains(got, reject) {
			t.Fatalf("tool %q field %q description should stay wiring-only, got %q", getStringAny(tool, "name"), field, got)
		}
	}
}
