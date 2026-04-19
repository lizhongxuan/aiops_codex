# Legacy Route Analysis — ReAct Loop Migration

## Entry Points (all now routed to WorkspaceAgentLoop)

### handleWorkspaceChatMessage (orchestrator_integration.go:296)
- New main entry: Creates/resumes AgentLoopRun, calls runReActAgentLoop
- Intent clarification guard: workspaceMessageNeedsIntentClarification → createChoiceRequest
- No longer calls startWorkspaceRouteTurn for new requests

### startWorkspaceRouteTurn (orchestrator_integration.go:482)
- LEGACY: Only retained for historical data recovery and rollback
- Was the old entry point that started a route thread with BuildWorkspaceRoutePrompt
- New requests use buildWorkspaceReActThreadStartSpec instead

### BuildWorkspaceRoutePrompt (orchestrator/prompt.go)
- LEGACY: Generates the 4-route classification prompt
- Not used by new ReAct loop path
- BuildWorkspaceReActPrompt is the replacement

### parseWorkspaceRouteReply (orchestrator_integration.go:1581)
- LEGACY: Parses JSON route decision from model output
- Not used by new ReAct loop path — model uses tools instead of route JSON

## Auto-progression Logic (disabled for new requests)

### complex_task route
- Was triggered when model chose complex_task in route reply
- Would auto-create mission and start planning turn
- Now: Complex tasks go through enter_plan_mode → update_plan → exit_plan_mode tool flow

### host_readonly route
- Was triggered when model chose host_readonly
- Would start a readonly thread on target host
- Now: Uses readonly_host_inspect dynamic tool directly

### state_query route
- Was triggered when model chose state_query
- Would query ai-server state
- Now: Uses query_ai_server_state dynamic tool directly

## Data Dependencies

- mission / worker / approval: Preserved, accessed via orchestrator_dispatch_tasks tool
- card / snapshot: Preserved, extended with AgentLoopRun, ToolInvocation, EvidenceRecord
- WebSocket: Preserved, extended with loop lifecycle events
- Persistence: State saved via store.SaveStableState, extended with loop fields

## Route-related Tests

### Tests to keep as legacy compatibility:
- TestWorkspaceRouteThreadOnlyExposesAIServerStateTool
- TestWorkspaceRouteCompletionStartsReadonlyTurnForTargetHost
- TestWorkspaceRouteCompletionNotificationDoesNotBlockCodexReadLoop

### Tests already converted to ReAct loop:
- TestWorkspaceIntentGuardCreatesPlatformChoice
- TestPlatformCapabilityChoiceAnswersDirectlyWithoutTools
- TestWorkspacePlanModeToolsCreateApprovalAndGateDispatch

## Switch Point

New workspace chat requests enter `handleWorkspaceChatMessage` → `runReActAgentLoop`.
The `workspace_react_loop_enabled` config (default: true) is available for emergency rollback only.
