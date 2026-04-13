package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/agentloop"
	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/orchestrator"
)

const (
	bifrostApprovalTypeRemoteCommand    = "bifrost_remote_command"
	bifrostApprovalTypeRemoteFileChange = "bifrost_remote_file_change"
)

func (a *App) initBifrostRuntime() error {
	if a == nil || !a.cfg.UseBifrost {
		return nil
	}

	provider := strings.TrimSpace(a.cfg.LLMProvider)
	if provider == "" {
		provider = "openai"
	}

	if providerRequiresAPIKey(provider) && strings.TrimSpace(a.cfg.LLMAPIKey) == "" && len(a.cfg.LLMAPIKeys) == 0 {
		log.Printf("bifrost runtime disabled: provider=%s requires LLM_API_KEY or LLM_API_KEYS", provider)
		return nil
	}

	gateway := bifrost.NewGateway(bifrost.GatewayConfig{
		DefaultProvider: provider,
		DefaultModel:    strings.TrimSpace(a.cfg.LLMModel),
	})

	if err := a.registerBifrostProvider(gateway, provider, strings.TrimSpace(a.cfg.LLMAPIKey), strings.TrimSpace(a.cfg.LLMBaseURL)); err != nil {
		return err
	}
	fallbacks := make([]bifrost.FallbackEntry, 0, 1)
	if fallbackProvider := strings.TrimSpace(a.cfg.LLMFallbackProvider); fallbackProvider != "" {
		if err := a.registerBifrostProvider(gateway, fallbackProvider, strings.TrimSpace(a.cfg.LLMFallbackAPIKey), ""); err != nil {
			return err
		}
		fallbacks = append(fallbacks, bifrost.FallbackEntry{
			Provider: fallbackProvider,
			Model:    strings.TrimSpace(a.cfg.LLMFallbackModel),
		})
	}
	if len(fallbacks) > 0 {
		gateway.SetFallbackChain(bifrost.NewFallbackChain(fallbacks))
	}

	reg := agentloop.NewToolRegistry()
	agentloop.RegisterRemoteHostTools(reg)
	agentloop.RegisterWorkspaceTools(reg)
	agentloop.RegisterApplyPatchTool(reg)
	agentloop.RegisterWebSearchTools(reg)
	if a.corootClient != nil {
		agentloop.RegisterCorootTools(reg)
	}
	a.wireBifrostToolHandlers(reg)

	a.bifrostGateway = gateway
	a.agentLoop = agentloop.NewLoop(gateway, reg, nil).
		SetApprovalHandler(a).
		SetStreamObserver(a)
	return nil
}

func providerRequiresAPIKey(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "ollama":
		return false
	default:
		return true
	}
}

func (a *App) registerBifrostProvider(gateway *bifrost.Gateway, provider, apiKey, baseURL string) error {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai":
		gateway.RegisterProvider("openai", bifrost.NewOpenAIProvider(apiKey, baseURL))
	case "anthropic":
		gateway.RegisterProvider("anthropic", bifrost.NewAnthropicProvider(apiKey, baseURL))
	case "ollama":
		gateway.RegisterProvider("ollama", bifrost.NewOllamaProvider(baseURL))
	case "":
		return errors.New("bifrost provider is required")
	default:
		return fmt.Errorf("unsupported bifrost provider %q", provider)
	}
	return nil
}

func (a *App) useBifrost() bool {
	return a != nil && a.cfg.UseBifrost && a.agentLoop != nil && a.bifrostGateway != nil
}

func (a *App) useBifrostForSession(sessionID string) bool {
	if !a.useBifrost() {
		return false
	}
	switch a.sessionKind(sessionID) {
	case "", model.SessionKindSingleHost, model.SessionKindWorkspace, model.SessionKindPlanner, model.SessionKindWorker:
		return true
	default:
		return false
	}
}

func bifrostSessionSpecFromThreadSpec(spec threadStartSpec, fallbackModel string) agentloop.SessionSpec {
	sessionSpec := agentloop.SessionSpec{
		Model:                 firstNonEmptyValue(strings.TrimSpace(spec.Model), strings.TrimSpace(fallbackModel)),
		Cwd:                   strings.TrimSpace(spec.Cwd),
		DeveloperInstructions: strings.TrimSpace(spec.DeveloperInstructions),
		DynamicTools:          bifrostToolNamesFromDynamicTools(spec.DynamicTools),
		ApprovalPolicy:        strings.TrimSpace(spec.ApprovalPolicy),
		SandboxMode:           strings.TrimSpace(spec.SandboxMode),
	}
	if sessionSpec.Model == "" {
		sessionSpec.Model = strings.TrimSpace(fallbackModel)
	}
	return sessionSpec
}

func (a *App) workspaceRuntimeSessionID(sessionID string) string {
	if a == nil {
		return ""
	}
	meta := a.sessionMeta(sessionID)
	if workspaceSessionID := strings.TrimSpace(meta.WorkspaceSessionID); workspaceSessionID != "" {
		return workspaceSessionID
	}
	if a.sessionKind(sessionID) == model.SessionKindWorkspace {
		return strings.TrimSpace(sessionID)
	}
	return ""
}

func (a *App) hasBifrostSession(sessionID string) bool {
	a.bifrostMu.Lock()
	defer a.bifrostMu.Unlock()
	_, ok := a.bifrostSessions[sessionID]
	return ok
}

func (a *App) bifrostSession(sessionID string) (*agentloop.Session, bool) {
	a.bifrostMu.Lock()
	defer a.bifrostMu.Unlock()
	session, ok := a.bifrostSessions[sessionID]
	return session, ok
}

func (a *App) setBifrostSession(sessionID string, session *agentloop.Session) {
	a.bifrostMu.Lock()
	defer a.bifrostMu.Unlock()
	if session == nil {
		delete(a.bifrostSessions, sessionID)
		return
	}
	a.bifrostSessions[sessionID] = session
}

func (a *App) bifrostWorkspaceRuntime(sessionID string) (*agentloop.WorkspaceRuntime, bool) {
	workspaceSessionID := a.workspaceRuntimeSessionID(sessionID)
	if workspaceSessionID == "" {
		return nil, false
	}
	a.bifrostMu.Lock()
	defer a.bifrostMu.Unlock()
	runtime, ok := a.workspaceRuntimes[workspaceSessionID]
	return runtime, ok
}

func (a *App) getOrCreateBifrostWorkspaceRuntime(sessionID string) *agentloop.WorkspaceRuntime {
	workspaceSessionID := a.workspaceRuntimeSessionID(sessionID)
	if workspaceSessionID == "" {
		return nil
	}
	a.bifrostMu.Lock()
	defer a.bifrostMu.Unlock()
	if runtime, ok := a.workspaceRuntimes[workspaceSessionID]; ok && runtime != nil {
		return runtime
	}
	runtime := agentloop.NewWorkspaceRuntime(a.orchestrator)
	a.workspaceRuntimes[workspaceSessionID] = runtime
	return runtime
}

func (a *App) clearBifrostSession(sessionID string) {
	if runtime, ok := a.bifrostWorkspaceRuntime(sessionID); ok && runtime != nil {
		runtime.ResetSession(sessionID)
	}
	a.setBifrostSession(sessionID, nil)
}

func (a *App) ensureBifrostAuthState(sessionID string) {
	if !a.useBifrostForSession(sessionID) {
		return
	}
	a.store.UpdateAuth(sessionID, func(auth *model.AuthState, tokens *model.ExternalAuthTokens) {
		auth.Connected = true
		auth.Pending = false
		if strings.TrimSpace(auth.Mode) == "" {
			auth.Mode = "bifrost"
		}
		if strings.TrimSpace(tokens.Email) != "" && strings.TrimSpace(auth.Email) == "" {
			auth.Email = strings.TrimSpace(tokens.Email)
		}
		auth.LastError = ""
	})
}

func (a *App) getOrCreateBifrostSingleHostSession(ctx context.Context, sessionID string) (*agentloop.Session, error) {
	spec := a.buildSingleHostReActThreadStartSpec(ctx, sessionID)
	storeSession := a.store.EnsureSession(sessionID)
	expectedHash := strings.TrimSpace(spec.ThreadConfigHash)

	if existing, ok := a.bifrostSession(sessionID); ok && strings.TrimSpace(storeSession.ThreadConfigHash) == expectedHash {
		return existing, nil
	}

	session := agentloop.NewSession(sessionID, bifrostSessionSpecFromThreadSpec(spec, a.cfg.LLMModel))
	log.Printf("[bifrost-debug] new session %s: enabledTools=%v", sessionID, session.EnabledTools())
	a.setBifrostSession(sessionID, session)
	a.store.SetThreadConfigHash(sessionID, expectedHash)
	return session, nil
}

func (a *App) getOrCreateBifrostWorkspaceSession(ctx context.Context, sessionID string) (*agentloop.Session, error) {
	runtime := a.getOrCreateBifrostWorkspaceRuntime(sessionID)
	if runtime == nil {
		return nil, errors.New("workspace bifrost runtime is not initialized")
	}
	storeSession := a.store.EnsureSession(sessionID)
	spec := a.buildWorkspaceReActThreadStartSpec(ctx, sessionID, defaultHostID(storeSession.SelectedHostID))
	expectedHash := strings.TrimSpace(spec.ThreadConfigHash)
	if existing, ok := a.bifrostSession(sessionID); ok && strings.TrimSpace(storeSession.ThreadConfigHash) == expectedHash {
		return existing, nil
	}
	runtime.ResetSession(sessionID)
	session, err := runtime.StartPlannerTurn(ctx, sessionID, bifrostSessionSpecFromThreadSpec(spec, a.cfg.LLMModel), "")
	if err != nil {
		return nil, err
	}
	a.setBifrostSession(sessionID, session)
	a.store.SetThreadConfigHash(sessionID, expectedHash)
	return session, nil
}

func (a *App) getOrCreateBifrostWorkerSession(ctx context.Context, sessionID string) (*agentloop.Session, error) {
	if a.orchestrator == nil {
		return nil, errors.New("orchestrator is not initialized")
	}
	mission, worker, task, ok := a.orchestrator.WorkerTask(sessionID)
	if !ok || mission == nil || worker == nil || task == nil {
		return nil, fmt.Errorf("worker %s has no active task", sessionID)
	}
	runtime := a.getOrCreateBifrostWorkspaceRuntime(sessionID)
	if runtime == nil {
		return nil, errors.New("worker bifrost runtime is not initialized")
	}
	storeSession := a.store.EnsureSession(sessionID)
	hostID := firstNonEmptyValue(strings.TrimSpace(worker.HostID), strings.TrimSpace(storeSession.SelectedHostID), strings.TrimSpace(a.sessionMeta(sessionID).WorkerHostID))
	spec := a.buildWorkerThreadStartSpec(mission, task, defaultHostID(hostID))
	expectedHash := strings.TrimSpace(spec.ThreadConfigHash)
	if existing, ok := a.bifrostSession(sessionID); ok && strings.TrimSpace(storeSession.ThreadConfigHash) == expectedHash {
		return existing, nil
	}
	runtime.ResetSession(sessionID)
	session, err := runtime.StartWorkerTurn(ctx, sessionID, bifrostSessionSpecFromThreadSpec(spec, a.cfg.LLMModel), "")
	if err != nil {
		return nil, err
	}
	a.setBifrostSession(sessionID, session)
	a.store.SetThreadConfigHash(sessionID, expectedHash)
	return session, nil
}

func (a *App) getOrCreateBifrostSession(ctx context.Context, sessionID string) (*agentloop.Session, error) {
	switch a.sessionKind(sessionID) {
	case model.SessionKindWorkspace, model.SessionKindPlanner:
		return a.getOrCreateBifrostWorkspaceSession(ctx, sessionID)
	case model.SessionKindWorker:
		return a.getOrCreateBifrostWorkerSession(ctx, sessionID)
	default:
		return a.getOrCreateBifrostSingleHostSession(ctx, sessionID)
	}
}

func bifrostToolNamesFromDynamicTools(dynamicTools []map[string]any) []string {
	names := make([]string, 0, len(dynamicTools))
	seen := make(map[string]struct{}, len(dynamicTools))
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	for _, tool := range dynamicTools {
		switch strings.TrimSpace(getStringAny(tool, "name")) {
		case "ask_user_question":
			add("ask_user_question")
		case "query_ai_server_state":
			add("query_ai_server_state")
		case "readonly_host_inspect":
			add("readonly_host_inspect")
		case "enter_plan_mode":
			add("enter_plan_mode")
		case "update_plan":
			add("update_plan")
		case "exit_plan_mode":
			add("exit_plan_mode")
		case "request_approval":
			add("request_approval")
		case "orchestrator_dispatch_tasks":
			add("orchestrator_dispatch_tasks")
		case "execute_readonly_query":
			add("execute_readonly_query")
		case "list_remote_files":
			add("list_files")
		case "read_remote_file":
			add("read_file")
		case "search_remote_files":
			add("search_files")
		case "execute_system_mutation":
			add("execute_command")
			add("write_file")
		case corootToolListServices:
			add(corootToolListServices)
		case corootToolServiceOverview:
			add(corootToolServiceOverview)
		case corootToolServiceMetrics:
			add(corootToolServiceMetrics)
		case corootToolServiceAlerts:
			add(corootToolServiceAlerts)
		case corootToolTopology:
			add(corootToolTopology)
		case corootToolIncidentTime:
			add(corootToolIncidentTime)
		case corootToolRCAReport:
			add(corootToolRCAReport)
		case "apply_patch":
			add("apply_patch")
		case "web_search":
			add("web_search")
		case "open_page":
			add("open_page")
		case "find_in_page":
			add("find_in_page")
		}
	}
	return names
}

func (a *App) buildBifrostUserInput(req chatRequest) string {
	message := strings.TrimSpace(req.Message)
	if req.MonitorContext == nil {
		return message
	}
	prefix := strings.TrimSpace(model.MonitorContextPromptPrefix(*req.MonitorContext))
	if prefix == "" {
		return message
	}
	if message == "" {
		return prefix
	}
	return prefix + "\n\n" + message
}

func (a *App) runBifrostTurn(ctx context.Context, sessionID string, req chatRequest) error {
	session, err := a.getOrCreateBifrostSession(ctx, sessionID)
	if err != nil {
		return err
	}
	a.ensureBifrostAuthState(sessionID)

	turnCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	session.SetCancelFunc(cancel)
	defer session.SetCancelFunc(nil)

	if err := a.agentLoop.RunTurn(turnCtx, session, a.buildBifrostUserInput(req)); err != nil {
		return err
	}

	if a.hasPendingApprovals(sessionID) || a.hasPendingChoices(sessionID) {
		a.broadcastSnapshot(sessionID)
		return nil
	}

	switch a.sessionKind(sessionID) {
	case model.SessionKindWorkspace, model.SessionKindPlanner:
		if mission, ok := a.resolveOrchestratorMission(sessionID); ok && mission != nil {
			a.syncWorkspaceMissionRuntime(mission, "completed")
			a.refreshWorkspaceProjection(mission)
		} else {
			a.finishRuntimeTurn(sessionID, "completed")
		}
	default:
		a.finishRuntimeTurn(sessionID, "completed")
	}
	a.finalizeOpenTurnCards(sessionID, "completed")
	a.broadcastSnapshot(sessionID)
	return nil
}

func (a *App) wireBifrostToolHandlers(reg *agentloop.ToolRegistry) {
	mustSet := func(name string, handler agentloop.ToolHandler) {
		entry, ok := reg.Get(name)
		if !ok || entry == nil {
			panic("missing bifrost tool registration: " + name)
		}
		entry.Handler = handler
	}
	setIfPresent := func(name string, handler agentloop.ToolHandler) {
		entry, ok := reg.Get(name)
		if !ok || entry == nil {
			return
		}
		entry.Handler = handler
	}

	mustSet("ask_user_question", a.bifrostAskUserQuestion)
	mustSet("query_ai_server_state", a.bifrostQueryAIServerState)
	mustSet("readonly_host_inspect", a.bifrostReadonlyHostInspect)
	mustSet("enter_plan_mode", a.bifrostEnterPlanMode)
	mustSet("update_plan", a.bifrostUpdatePlan)
	mustSet("exit_plan_mode", a.bifrostExitPlanMode)
	mustSet("request_approval", a.bifrostRequestApproval)
	mustSet("orchestrator_dispatch_tasks", a.bifrostDispatchTasks)
	mustSet("execute_readonly_query", a.bifrostExecuteReadonlyQuery)
	mustSet("execute_command", a.bifrostExecuteRemoteCommand)
	mustSet("list_files", a.bifrostListRemoteFiles)
	mustSet("read_file", a.bifrostReadRemoteFile)
	mustSet("search_files", a.bifrostSearchRemoteFiles)
	mustSet("write_file", a.bifrostWriteRemoteFile)
	setIfPresent(corootToolListServices, a.bifrostExecuteCorootTool(corootToolListServices))
	setIfPresent(corootToolServiceOverview, a.bifrostExecuteCorootTool(corootToolServiceOverview))
	setIfPresent(corootToolServiceMetrics, a.bifrostExecuteCorootTool(corootToolServiceMetrics))
	setIfPresent(corootToolServiceAlerts, a.bifrostExecuteCorootTool(corootToolServiceAlerts))
	setIfPresent(corootToolTopology, a.bifrostExecuteCorootTool(corootToolTopology))
	setIfPresent(corootToolIncidentTime, a.bifrostExecuteCorootTool(corootToolIncidentTime))
	setIfPresent(corootToolRCAReport, a.bifrostExecuteCorootTool(corootToolRCAReport))
	setIfPresent("web_search", nil)
	setIfPresent("open_page", nil)
	setIfPresent("find_in_page", nil)
}

func (a *App) bifrostExecuteCorootTool(toolName string) agentloop.ToolHandler {
	return func(ctx context.Context, session *agentloop.Session, call bifrost.ToolCall, arguments map[string]any) (string, error) {
		toolText, err := a.runCorootTool(ctx, toolName, arguments)
		if err != nil {
			return "", err
		}
		a.upsertCorootResultCard(session.ID, dynamicToolCardID(call.ID), toolName, arguments, toolText)
		a.broadcastSnapshot(session.ID)
		return toolText, nil
	}
}

func (a *App) bifrostAskUserQuestion(_ context.Context, session *agentloop.Session, _ bifrost.ToolCall, arguments map[string]any) (string, error) {
	sessionID := session.ID
	questions := toChoiceQuestions(arguments["questions"])
	if len(questions) == 0 {
		if question := strings.TrimSpace(getStringAny(arguments, "question")); question != "" {
			questions = []model.ChoiceQuestion{{
				Header:   getStringAny(arguments, "header"),
				Question: question,
				IsOther:  getBool(arguments, "isOther"),
				IsSecret: getBool(arguments, "isSecret"),
				Options:  toChoiceOptions(arguments["options"]),
			}}
		}
	}
	if len(questions) == 0 || strings.TrimSpace(questions[0].Question) == "" {
		return "", fmt.Errorf("ask_user_question requires at least one question")
	}
	if observation, ok := a.completedChoiceObservationForQuestions(sessionID, questions); ok {
		return mustJSON(observation), nil
	}
	a.createChoiceRequest("", sessionID, map[string]any{}, questions)
	return "", agentloop.ErrPauseTurn
}

func (a *App) bifrostQueryAIServerState(_ context.Context, session *agentloop.Session, call bifrost.ToolCall, arguments map[string]any) (string, error) {
	focus := strings.TrimSpace(getStringAny(arguments, "focus", "query", "topic"))
	sessionID := session.ID
	storeSession := a.store.EnsureSession(sessionID)

	state := map[string]any{
		"sessionId":      sessionID,
		"kind":           storeSession.Meta.Kind,
		"selectedHostId": storeSession.SelectedHostID,
		"runtime": map[string]any{
			"turnActive": storeSession.Runtime.Turn.Active,
			"phase":      storeSession.Runtime.Turn.Phase,
			"hostId":     storeSession.Runtime.Turn.HostID,
		},
	}
	hosts := a.store.Hosts()
	hostSummaries := make([]map[string]any, 0, len(hosts))
	for _, host := range hosts {
		hostSummaries = append(hostSummaries, map[string]any{
			"id":     host.ID,
			"name":   host.Name,
			"status": host.Status,
			"kind":   host.Kind,
		})
	}
	state["hosts"] = hostSummaries
	state["hostCount"] = len(hosts)

	pendingApprovals := 0
	for _, approval := range storeSession.Approvals {
		if approval.Status == "pending" {
			pendingApprovals++
		}
	}
	state["pendingApprovals"] = pendingApprovals
	state["cardCount"] = len(storeSession.Cards)

	evidenceID := model.NewID("ev")
	a.store.RememberItem(sessionID, evidenceID, map[string]any{
		"kind":  "ai_server_state",
		"focus": focus,
		"state": state,
	})

	stateJSON, _ := json.Marshal(state)
	responseText := fmt.Sprintf("AI Server State (focus=%s):\n%s\n\n[evidence: %s]", focus, string(stateJSON), evidenceID)
	now := model.NowString()
	a.store.UpsertCard(sessionID, model.Card{
		ID:      dynamicToolCardID(call.ID),
		Type:    "WorkspaceResultCard",
		Title:   "AI Server State Query",
		Summary: fmt.Sprintf("查询焦点: %s | Hosts: %d | 待审批: %d", focus, len(hosts), pendingApprovals),
		Text:    responseText,
		Status:  "completed",
		Detail: map[string]any{
			"tool":       "query_ai_server_state",
			"focus":      focus,
			"evidenceId": evidenceID,
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
	a.broadcastSnapshot(sessionID)
	return responseText, nil
}

func (a *App) bifrostReadonlyHostInspect(ctx context.Context, session *agentloop.Session, call bifrost.ToolCall, arguments map[string]any) (string, error) {
	args, err := parseExecToolArgs(arguments)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Reason) == "" {
		return "", errors.New("readonly_host_inspect requires a reason")
	}
	selectedHostID := defaultHostID(a.sessionHostID(session.ID))
	hostID := defaultHostID(args.HostID)
	if hostID != selectedHostID {
		return "", fmt.Errorf("readonly_host_inspect host %s does not match selected host %s", hostID, selectedHostID)
	}
	if err := a.ensureCapabilityAllowedForHost(hostID, "commandExecution"); err != nil {
		return "", err
	}
	if err := validateReadonlyCommand(args.Command); err != nil {
		return "", err
	}
	if isRemoteHostID(hostID) {
		host := a.findHost(hostID)
		if host.Status != "online" || !host.Executable {
			return "", fmt.Errorf("selected host %s is offline or not executable", hostID)
		}
		decision, err := a.evaluateCommandPolicyForHost(hostID, args.Command)
		if err != nil {
			return "", err
		}
		if decision.Mode == model.AgentPermissionModeApprovalRequired {
			approvalID, err := a.requestBifrostToolApproval(ctx, session, call, args, remoteFileChangeArgs{}, true)
			if err != nil {
				return "", err
			}
			approved, denialResult, err := a.awaitBifrostApprovalDecision(ctx, session, approvalID, call.Function.Name)
			if err != nil {
				return "", err
			}
			if !approved {
				return denialResult, nil
			}
		}
		result, err := a.runRemoteExec(ctx, session.ID, hostID, dynamicToolCardID(call.ID), execSpec{
			Command:    args.Command,
			Cwd:        args.Cwd,
			TimeoutSec: args.TimeoutSec,
			Readonly:   true,
			ToolName:   "readonly_host_inspect",
		})
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return formatExecToolResult(args.Command, result), nil
	}

	result, err := a.runLocalReadonlyExec(ctx, session.ID, dynamicToolCardID(call.ID), execSpec{
		Command:    args.Command,
		Cwd:        args.Cwd,
		TimeoutSec: args.TimeoutSec,
		Readonly:   true,
		ToolName:   "readonly_host_inspect",
	})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return "", err
	}
	return formatExecToolResult(args.Command, result), nil
}

func (a *App) bifrostEnterPlanMode(_ context.Context, session *agentloop.Session, call bifrost.ToolCall, arguments map[string]any) (string, error) {
	goal := strings.TrimSpace(getStringAny(arguments, "goal", "title", "summary"))
	reason := strings.TrimSpace(getStringAny(arguments, "reason"))
	if goal == "" {
		return "", errors.New("enter_plan_mode requires goal")
	}
	if reason == "" {
		return "", errors.New("enter_plan_mode requires reason")
	}
	now := model.NowString()
	a.setRuntimeTurnPhase(session.ID, "planning")
	a.store.UpdateRuntime(session.ID, func(rt *model.RuntimeState) {
		rt.PlanMode = true
	})
	a.store.UpsertCard(session.ID, model.Card{
		ID:      "plan-mode-" + firstNonEmptyValue(strings.TrimSpace(call.ID), model.NewID("planmode")),
		Type:    "PlanCard",
		Title:   "进入计划模式",
		Text:    goal,
		Summary: reason,
		Status:  "inProgress",
		Detail: map[string]any{
			"tool":   "enter_plan_mode",
			"mode":   "plan",
			"goal":   goal,
			"reason": reason,
			"scope":  strings.TrimSpace(getStringAny(arguments, "scope")),
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
	a.broadcastSnapshot(session.ID)
	return "Entered plan mode. Continue with read-only planning, update_plan, ask_user_question, or exit_plan_mode for approval.", nil
}

func (a *App) bifrostUpdatePlan(_ context.Context, session *agentloop.Session, call bifrost.ToolCall, arguments map[string]any) (string, error) {
	summary := strings.TrimSpace(getStringAny(arguments, "summary", "plan"))
	if summary == "" {
		return "", errors.New("update_plan requires summary")
	}
	now := model.NowString()
	a.setRuntimeTurnPhase(session.ID, "planning")
	card := model.Card{
		ID:      "plan-update-" + firstNonEmptyValue(strings.TrimSpace(call.ID), model.NewID("plan")),
		Type:    "PlanCard",
		Title:   firstNonEmptyValue(strings.TrimSpace(getStringAny(arguments, "title")), "工作台计划"),
		Text:    summary,
		Summary: summary,
		Status:  "inProgress",
		Items:   planItemsFromArguments(arguments),
		Detail: map[string]any{
			"tool":       "update_plan",
			"mode":       "plan",
			"summary":    summary,
			"background": strings.TrimSpace(getStringAny(arguments, "background")),
			"scope":      strings.TrimSpace(getStringAny(arguments, "scope")),
			"risk":       strings.TrimSpace(getStringAny(arguments, "risk", "risks")),
			"rollback":   strings.TrimSpace(getStringAny(arguments, "rollback")),
			"validation": strings.TrimSpace(getStringAny(arguments, "validation")),
			"steps":      arguments["steps"],
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	a.store.UpsertCard(session.ID, card)
	a.broadcastSnapshot(session.ID)
	return fmt.Sprintf("Plan updated with %d steps. Continue planning or call exit_plan_mode to request approval.", len(card.Items)), nil
}

func (a *App) bifrostExitPlanMode(_ context.Context, session *agentloop.Session, call bifrost.ToolCall, arguments map[string]any) (string, error) {
	if err := a.validateExitPlanModeRequest(session.ID, arguments); err != nil {
		return "", err
	}
	now := model.NowString()
	approvalID := model.NewID("approval")
	cardID := "plan-approval-" + firstNonEmptyValue(strings.TrimSpace(call.ID), approvalID)
	summary := strings.TrimSpace(getStringAny(arguments, "summary", "plan"))
	approval := model.ApprovalRequest{
		ID:          approvalID,
		HostID:      model.ServerLocalHostID,
		Type:        "plan_exit",
		Status:      "pending",
		ItemID:      cardID,
		Reason:      firstNonEmptyValue(strings.TrimSpace(getStringAny(arguments, "title")), "计划审批"),
		Decisions:   []string{"accept", "decline"},
		RequestedAt: now,
	}
	card := model.Card{
		ID:      cardID,
		Type:    "PlanApprovalCard",
		Title:   firstNonEmptyValue(strings.TrimSpace(getStringAny(arguments, "title")), "计划审批"),
		Text:    summary,
		Summary: summary,
		Status:  "pending",
		Approval: &model.ApprovalRef{
			RequestID: approval.ID,
			Type:      approval.Type,
			Decisions: approval.Decisions,
		},
		Detail: map[string]any{
			"tool":       "exit_plan_mode",
			"mode":       "plan",
			"summary":    summary,
			"plan":       strings.TrimSpace(getStringAny(arguments, "plan")),
			"risk":       strings.TrimSpace(getStringAny(arguments, "risk", "risks")),
			"rollback":   strings.TrimSpace(getStringAny(arguments, "rollback")),
			"validation": strings.TrimSpace(getStringAny(arguments, "validation")),
			"tasks":      arguments["tasks"],
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	a.store.AddApproval(session.ID, approval)
	a.store.UpsertCard(session.ID, card)
	a.setRuntimeTurnPhase(session.ID, "waiting_approval")
	a.recordOrchestratorApprovalRequested(session.ID, approval)
	a.auditApprovalRequested(session.ID, approval, map[string]any{
		"planSummary": truncate(summary, 400),
	})
	a.broadcastSnapshot(session.ID)
	return "", agentloop.ErrPauseTurn
}

func (a *App) bifrostRequestApproval(_ context.Context, session *agentloop.Session, call bifrost.ToolCall, arguments map[string]any) (string, error) {
	command := getStringAny(arguments, "command")
	hostID := getStringAny(arguments, "hostId", "host_id")
	cwd := getStringAny(arguments, "cwd")
	riskAssessment := getStringAny(arguments, "riskAssessment", "risk_assessment")
	expectedImpact := getStringAny(arguments, "expectedImpact", "expected_impact")
	rollbackSuggestion := getStringAny(arguments, "rollbackSuggestion", "rollback_suggestion")
	if command == "" {
		return "", errors.New("request_approval requires a command")
	}
	if hostID == "" {
		hostID = a.sessionHostID(session.ID)
	}
	now := model.NowString()
	approvalID := model.NewID("approval")
	cardID := dynamicToolCardID(call.ID)
	approval := model.ApprovalRequest{
		ID:          approvalID,
		Type:        "mutation",
		Status:      "pending",
		HostID:      hostID,
		Command:     command,
		Cwd:         cwd,
		ItemID:      cardID,
		Decisions:   []string{"accept", "decline"},
		RequestedAt: now,
	}
	card := model.Card{
		ID:      cardID,
		Type:    "ApprovalCard",
		Title:   fmt.Sprintf("审批请求: %s", truncate(command, 60)),
		Summary: fmt.Sprintf("Host: %s | Risk: %s", hostID, truncate(riskAssessment, 80)),
		Status:  "pending",
		Detail: map[string]any{
			"riskAssessment":     riskAssessment,
			"expectedImpact":     expectedImpact,
			"rollbackSuggestion": rollbackSuggestion,
			"toolCallId":         call.ID,
		},
		Approval: &model.ApprovalRef{
			RequestID: approval.ID,
			Type:      approval.Type,
			Decisions: approval.Decisions,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	a.setRuntimeTurnPhase(session.ID, "waiting_approval")
	a.store.AddApproval(session.ID, approval)
	a.store.UpsertCard(session.ID, card)
	a.recordOrchestratorApprovalRequested(session.ID, approval)
	if kind := a.sessionKind(session.ID); kind == model.SessionKindPlanner || kind == model.SessionKindWorker {
		a.mirrorInternalApprovalToWorkspace(session.ID, approval, card)
	}
	a.auditApprovalRequested(session.ID, approval, map[string]any{
		"riskAssessment": emptyToNil(strings.TrimSpace(riskAssessment)),
		"expectedImpact": emptyToNil(strings.TrimSpace(expectedImpact)),
	})
	a.broadcastSnapshot(session.ID)
	a.audit("approval.requested", map[string]any{
		"sessionId":  session.ID,
		"approvalId": approval.ID,
		"command":    truncate(command, 120),
	})
	return "", agentloop.ErrPauseTurn
}

func (a *App) bifrostDispatchTasks(_ context.Context, session *agentloop.Session, _ bifrost.ToolCall, arguments map[string]any) (string, error) {
	storeSession := a.store.EnsureSession(session.ID)
	if storeSession.Runtime.PlanMode {
		return "", errors.New("orchestrator_dispatch_tasks is not allowed in plan mode. Use exit_plan_mode to get approval first")
	}
	var req orchestrator.DispatchRequest
	if err := remarshalInto(arguments, &req); err != nil {
		return "", errors.New("dispatch payload 无法解析")
	}
	result, err := a.dispatchOrchestratorTasks(session.ID, req)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("dispatch accepted=%d activated=%d queued=%d", result.Accepted, result.Activated, result.Queued), nil
}

func (a *App) bifrostExecuteReadonlyQuery(ctx context.Context, session *agentloop.Session, call bifrost.ToolCall, arguments map[string]any) (string, error) {
	args, err := parseExecToolArgs(arguments)
	if err != nil {
		return "", err
	}
	hostID := defaultHostID(args.HostID)
	if hostID == "" {
		hostID = defaultHostID(a.sessionHostID(session.ID))
	}

	if err := validateReadonlyCommand(args.Command); err != nil {
		return "", err
	}

	// Local execution for server-local.
	if !isRemoteHostID(hostID) {
		result, err := a.runLocalReadonlyExec(ctx, session.ID, dynamicToolCardID(call.ID), execSpec{
			Command:    args.Command,
			Cwd:        args.Cwd,
			TimeoutSec: args.TimeoutSec,
			Readonly:   true,
			ToolName:   "execute_readonly_query",
		})
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return formatExecToolResult(args.Command, result), nil
	}

	// Remote execution.
	hostID, err = a.validateBifrostSelectedRemoteHost(session.ID, args.HostID)
	if err != nil {
		return "", err
	}
	if err := a.ensureCapabilityAllowedForHost(hostID, "commandExecution"); err != nil {
		return "", err
	}
	decision, err := a.evaluateCommandPolicyForHost(hostID, args.Command)
	if err != nil {
		return "", err
	}
	if decision.Mode == model.AgentPermissionModeApprovalRequired {
		approvalID, err := a.requestBifrostToolApproval(ctx, session, call, args, remoteFileChangeArgs{}, true)
		if err != nil {
			return "", err
		}
		approved, denialResult, err := a.awaitBifrostApprovalDecision(ctx, session, approvalID, call.Function.Name)
		if err != nil {
			return "", err
		}
		if !approved {
			return denialResult, nil
		}
	}
	result, err := a.runRemoteExec(ctx, session.ID, hostID, dynamicToolCardID(call.ID), execSpec{
		Command:    args.Command,
		Cwd:        args.Cwd,
		TimeoutSec: args.TimeoutSec,
		Readonly:   true,
		ToolName:   "execute_readonly_query",
	})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return "", err
	}
	return formatExecToolResult(args.Command, result), nil
}

func (a *App) awaitBifrostApprovalDecision(ctx context.Context, session *agentloop.Session, approvalID, toolName string) (bool, string, error) {
	decision, err := session.WaitForApprovalID(ctx, approvalID)
	if err != nil {
		return false, "", err
	}
	switch strings.ToLower(strings.TrimSpace(decision.Decision)) {
	case "approve", "approved", "accept", "accept_session":
		return true, "", nil
	}
	reason := strings.TrimSpace(decision.Reason)
	if reason == "" {
		reason = strings.TrimSpace(decision.Decision)
	}
	if reason == "" {
		reason = "rejected"
	}
	return false, fmt.Sprintf("Tool %s was not approved: %s", toolName, reason), nil
}

func (a *App) bifrostExecuteRemoteCommand(ctx context.Context, session *agentloop.Session, call bifrost.ToolCall, arguments map[string]any) (string, error) {
	args, err := parseExecToolArgs(arguments)
	if err != nil {
		return "", err
	}
	hostID := defaultHostID(args.HostID)
	if hostID == "" {
		hostID = defaultHostID(a.sessionHostID(session.ID))
	}

	// Local execution for server-local (mutation commands always need approval).
	if !isRemoteHostID(hostID) {
		// Request approval for local mutation.
		approvalID, err := a.requestBifrostToolApproval(ctx, session, call, args, remoteFileChangeArgs{}, false)
		if err != nil {
			return "", err
		}
		approved, denialResult, err := a.awaitBifrostApprovalDecision(ctx, session, approvalID, call.Function.Name)
		if err != nil {
			return "", err
		}
		if !approved {
			return denialResult, nil
		}
		result, err := a.runLocalReadonlyExec(ctx, session.ID, dynamicToolCardID(call.ID), execSpec{
			Command:    args.Command,
			Cwd:        args.Cwd,
			TimeoutSec: args.TimeoutSec,
			Readonly:   false,
			ToolName:   "execute_command",
			Approval:   "accepted",
		})
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return formatExecToolResult(args.Command, result), nil
	}

	// Remote execution.
	hostID, err = a.validateBifrostSelectedRemoteHost(session.ID, args.HostID)
	if err != nil {
		return "", err
	}
	result, err := a.runRemoteExec(ctx, session.ID, hostID, dynamicToolCardID(call.ID), execSpec{
		Command:    args.Command,
		Cwd:        args.Cwd,
		TimeoutSec: args.TimeoutSec,
		Readonly:   false,
		ToolName:   "execute_command",
		Approval:   "accepted",
	})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return "", err
	}
	return formatExecToolResult(args.Command, result), nil
}

func (a *App) bifrostListRemoteFiles(ctx context.Context, session *agentloop.Session, call bifrost.ToolCall, arguments map[string]any) (string, error) {
	args, err := parseRemoteListFilesArgs(arguments)
	if err != nil {
		return "", err
	}
	hostID, err := a.validateBifrostSelectedRemoteHost(session.ID, args.HostID)
	if err != nil {
		return "", err
	}
	processCardID := "process-" + dynamicToolCardID(call.ID)
	startedAt := model.NowString()
	a.beginToolProcess(session.ID, processCardID, "browsing", "现在列出 "+args.Path)
	a.store.UpdateRuntime(session.ID, func(runtime *model.RuntimeState) {
		runtime.Activity.CurrentListingPath = args.Path
	})
	a.auditRemoteToolEvent("remote.file_list.started", session.ID, hostID, "list_files", map[string]any{
		"path":      args.Path,
		"startedAt": startedAt,
	})
	result, err := a.remoteListFiles(ctx, hostID, args.Path, args.Recursive, args.MaxEntries)
	if err != nil {
		a.failToolProcess(session.ID, processCardID, "列目录失败："+err.Error())
		a.store.UpdateRuntime(session.ID, func(runtime *model.RuntimeState) {
			if runtime.Activity.CurrentListingPath == args.Path {
				runtime.Activity.CurrentListingPath = ""
			}
		})
		return "", err
	}
	a.completeToolProcess(session.ID, processCardID, "已列出 "+result.Path)
	a.store.UpdateRuntime(session.ID, func(runtime *model.RuntimeState) {
		if runtime.Activity.CurrentListingPath == args.Path || runtime.Activity.CurrentListingPath == result.Path {
			runtime.Activity.CurrentListingPath = ""
		}
		runtime.Activity.ListCount++
	})
	a.broadcastSnapshot(session.ID)
	return renderFileListMessage(hostID, result.Path, result.Entries, result.Truncated), nil
}

func (a *App) bifrostReadRemoteFile(ctx context.Context, session *agentloop.Session, call bifrost.ToolCall, arguments map[string]any) (string, error) {
	args, err := parseRemoteReadFileArgs(arguments)
	if err != nil {
		return "", err
	}
	hostID, err := a.validateBifrostSelectedRemoteHost(session.ID, args.HostID)
	if err != nil {
		return "", err
	}
	processCardID := "process-" + dynamicToolCardID(call.ID)
	a.beginToolProcess(session.ID, processCardID, "browsing", "现在浏览 "+args.Path)
	a.store.UpdateRuntime(session.ID, func(runtime *model.RuntimeState) {
		runtime.Activity.CurrentReadingFile = args.Path
	})
	result, err := a.remoteReadFile(ctx, hostID, args.Path, args.MaxBytes)
	if err != nil {
		a.failToolProcess(session.ID, processCardID, "浏览文件失败："+err.Error())
		a.store.UpdateRuntime(session.ID, func(runtime *model.RuntimeState) {
			if runtime.Activity.CurrentReadingFile == args.Path {
				runtime.Activity.CurrentReadingFile = ""
			}
		})
		return "", err
	}
	a.completeToolProcess(session.ID, processCardID, "已浏览 "+result.Path)
	a.store.UpdateRuntime(session.ID, func(runtime *model.RuntimeState) {
		if runtime.Activity.CurrentReadingFile == args.Path || runtime.Activity.CurrentReadingFile == result.Path {
			runtime.Activity.CurrentReadingFile = ""
		}
		entry := model.ActivityEntry{Label: filepathBase(result.Path), Path: result.Path}
		appendUniqueActivityEntry(&runtime.Activity.ViewedFiles, entry, func(existing, next model.ActivityEntry) bool {
			return existing.Path != "" && existing.Path == next.Path
		})
		runtime.Activity.FilesViewed = len(runtime.Activity.ViewedFiles)
	})
	a.broadcastSnapshot(session.ID)
	toolText := fmt.Sprintf("Read file %s:\n\n%s", result.Path, result.Content)
	if result.Truncated {
		toolText += "\n\n[truncated]"
	}
	return toolText, nil
}

func (a *App) bifrostSearchRemoteFiles(ctx context.Context, session *agentloop.Session, call bifrost.ToolCall, arguments map[string]any) (string, error) {
	args, err := parseRemoteSearchFilesArgs(arguments)
	if err != nil {
		return "", err
	}
	hostID, err := a.validateBifrostSelectedRemoteHost(session.ID, args.HostID)
	if err != nil {
		return "", err
	}
	processCardID := "process-" + dynamicToolCardID(call.ID)
	a.beginToolProcess(session.ID, processCardID, "searching", "现在搜索内容（"+args.Query+"）")
	a.store.UpdateRuntime(session.ID, func(runtime *model.RuntimeState) {
		runtime.Activity.CurrentSearchKind = "content"
		runtime.Activity.CurrentSearchQuery = args.Query
	})
	result, err := a.remoteSearchFiles(ctx, hostID, args.Path, args.Query, args.MaxMatches)
	if err != nil {
		a.failToolProcess(session.ID, processCardID, "搜索内容失败："+err.Error())
		a.store.UpdateRuntime(session.ID, func(runtime *model.RuntimeState) {
			if runtime.Activity.CurrentSearchKind == "content" && runtime.Activity.CurrentSearchQuery == args.Query {
				runtime.Activity.CurrentSearchKind = ""
				runtime.Activity.CurrentSearchQuery = ""
			}
		})
		return "", err
	}
	a.completeToolProcess(session.ID, processCardID, fmt.Sprintf("已搜索内容（命中 %d 个位置）", len(result.Matches)))
	a.store.UpdateRuntime(session.ID, func(runtime *model.RuntimeState) {
		if runtime.Activity.CurrentSearchKind == "content" && runtime.Activity.CurrentSearchQuery == args.Query {
			runtime.Activity.CurrentSearchKind = ""
			runtime.Activity.CurrentSearchQuery = ""
		}
		runtime.Activity.SearchCount++
		runtime.Activity.SearchLocationCount += len(result.Matches)
		appendUniqueActivityEntry(&runtime.Activity.SearchedContentQueries, model.ActivityEntry{
			Label: fmt.Sprintf("在 %s 中搜索 %s（命中 %d 个位置）", result.Path, result.Query, len(result.Matches)),
			Query: result.Query,
			Path:  result.Path,
		}, func(existing, next model.ActivityEntry) bool {
			return existing.Path == next.Path && existing.Query == next.Query
		})
	})
	a.broadcastSnapshot(session.ID)
	return renderFileSearchMessage(hostID, result.Path, result.Query, result.Matches, result.Truncated), nil
}

func (a *App) bifrostWriteRemoteFile(ctx context.Context, session *agentloop.Session, call bifrost.ToolCall, arguments map[string]any) (string, error) {
	args, err := parseRemoteFileChangeArgs(map[string]any{
		"host":       getStringAny(arguments, "host", "hostId"),
		"mode":       "file_change",
		"path":       getString(arguments, "path"),
		"content":    arguments["content"],
		"write_mode": firstNonEmptyValue(getString(arguments, "write_mode"), getString(arguments, "writeMode"), "overwrite"),
		"reason":     getString(arguments, "reason"),
	})
	if err != nil {
		return "", err
	}
	hostID, err := a.validateBifrostSelectedRemoteHost(session.ID, args.HostID)
	if err != nil {
		return "", err
	}
	processCardID := "process-" + dynamicToolCardID(call.ID)
	a.beginToolProcess(session.ID, processCardID, "executing", "现在修改 "+args.Path)
	a.store.UpdateRuntime(session.ID, func(runtime *model.RuntimeState) {
		runtime.Activity.CurrentChangingFile = args.Path
	})
	result, err := a.remoteWriteFile(ctx, hostID, args.Path, args.Content, args.WriteMode)
	if err != nil {
		annotatedErr := annotateRemoteFileChangeError(args, err)
		a.failToolProcess(session.ID, processCardID, "修改文件失败："+annotatedErr.Error())
		a.store.UpdateRuntime(session.ID, func(runtime *model.RuntimeState) {
			if runtime.Activity.CurrentChangingFile == args.Path {
				runtime.Activity.CurrentChangingFile = ""
			}
		})
		return "", annotatedErr
	}

	diff := renderFileDiff(result.Path, result.OldContent, result.NewContent)
	now := model.NowString()
	a.completeToolProcess(session.ID, processCardID, "已修改 "+result.Path)
	a.store.UpdateRuntime(session.ID, func(runtime *model.RuntimeState) {
		if runtime.Activity.CurrentChangingFile == args.Path || runtime.Activity.CurrentChangingFile == result.Path {
			runtime.Activity.CurrentChangingFile = ""
		}
		runtime.Activity.FilesChanged++
	})
	a.store.UpsertCard(session.ID, model.Card{
		ID:      dynamicToolCardID(call.ID),
		Type:    "FileChangeCard",
		Title:   "Remote file change",
		Status:  "completed",
		Changes: []model.FileChange{{Path: result.Path, Kind: remoteFileChangeKind(result.Created, result.WriteMode), Diff: diff}},
		Text:    fmt.Sprintf("已修改远程文件 %s", result.Path),
		HostID:  hostID,
		HostName: func() string {
			return hostNameOrID(a.findHost(hostID))
		}(),
		CreatedAt: now,
		UpdatedAt: now,
	})
	a.broadcastSnapshot(session.ID)
	return fmt.Sprintf("Updated file %s successfully.", result.Path), nil
}

func (a *App) validateBifrostSelectedRemoteHost(sessionID, requestedHostID string) (string, error) {
	selectedHostID := defaultHostID(a.sessionHostID(sessionID))
	hostID := defaultHostID(strings.TrimSpace(requestedHostID))
	if !isRemoteHostID(selectedHostID) {
		return "", errors.New("selected host is server-local; bifrost single-host currently supports remote execute_* tools only")
	}
	if hostID != selectedHostID {
		return "", fmt.Errorf("tool host %s does not match selected host %s", hostID, selectedHostID)
	}
	host := a.findHost(hostID)
	if host.Status != "online" || !host.Executable {
		return "", fmt.Errorf("selected remote host %s is offline or not executable", hostID)
	}
	return hostID, nil
}

func (a *App) RequestToolApproval(ctx context.Context, session *agentloop.Session, req agentloop.ApprovalRequest) (string, error) {
	args, _ := parseExecToolArgs(req.Arguments)
	fileArgs, _ := parseRemoteFileChangeArgs(map[string]any{
		"host":       getStringAny(req.Arguments, "host", "hostId"),
		"mode":       "file_change",
		"path":       getString(req.Arguments, "path"),
		"content":    req.Arguments["content"],
		"write_mode": firstNonEmptyValue(getString(req.Arguments, "write_mode"), getString(req.Arguments, "writeMode"), "overwrite"),
		"reason":     getString(req.Arguments, "reason"),
	})
	return a.requestBifrostToolApproval(ctx, session, req.ToolCall, args, fileArgs, false)
}

func (a *App) requestBifrostToolApproval(ctx context.Context, session *agentloop.Session, call bifrost.ToolCall, execArgs execToolArgs, fileArgs remoteFileChangeArgs, readonly bool) (string, error) {
	sessionID := session.ID
	switch strings.TrimSpace(call.Function.Name) {
	case "execute_command", "execute_readonly_query", "readonly_host_inspect":
		hostID := defaultHostID(execArgs.HostID)
		if hostID == "" {
			hostID = defaultHostID(a.sessionHostID(sessionID))
		}
		var host model.Host
		if isRemoteHostID(hostID) {
			var err error
			hostID, err = a.validateBifrostSelectedRemoteHost(sessionID, execArgs.HostID)
			if err != nil {
				return "", err
			}
			host = a.findHost(hostID)
			execArgs.Cwd = strings.TrimSpace(execArgs.Cwd)
			if execArgs.Cwd == "" {
				execArgs.Cwd = defaultRemoteExecCwd(host)
			}
		} else {
			host = a.findHost(model.ServerLocalHostID)
			execArgs.Cwd = strings.TrimSpace(execArgs.Cwd)
			if execArgs.Cwd == "" {
				execArgs.Cwd = strings.TrimSpace(a.cfg.DefaultWorkspace)
			}
		}
		decision, err := a.evaluateCommandPolicyForHost(hostID, execArgs.Command)
		if err != nil {
			return "", err
		}
		if maxTimeout := a.effectiveCommandTimeoutSeconds(hostID); maxTimeout > 0 && execArgs.TimeoutSec > 0 && execArgs.TimeoutSec > maxTimeout {
			return "", errors.New("requested timeout exceeds the current effective agent profile limit")
		}
		if decision.Category == "filesystem_mutation" && execArgs.Cwd != "" {
			if err := a.ensureWritableRootsForHost(hostID, []string{execArgs.Cwd}); err != nil {
				return "", err
			}
		}

		approval := model.ApprovalRequest{
			ID:          model.NewID("approval"),
			HostID:      hostID,
			Fingerprint: approvalFingerprintForCommand(hostID, execArgs.Command, execArgs.Cwd),
			Type:        bifrostApprovalTypeRemoteCommand,
			Status:      "pending",
			ItemID:      dynamicToolCardID(call.ID),
			Command:     execArgs.Command,
			Cwd:         execArgs.Cwd,
			Reason:      execArgs.Reason,
			Decisions:   []string{"accept", "accept_session", "decline"},
			RequestedAt: model.NowString(),
		}
		if autoResolved := a.autoApproveBifrostToolApproval(session, approval, decision.Mode == model.AgentPermissionModeAllow && !capabilityNeedsApproval(a.effectiveCapabilityState(hostID, "commandExecution"))); autoResolved {
			return approval.ID, nil
		}

		a.setRuntimeTurnPhase(sessionID, "waiting_approval")
		a.store.AddApproval(sessionID, approval)
		card := model.Card{
			ID:      approval.ItemID,
			Type:    "CommandApprovalCard",
			Title:   "Remote command approval required",
			Command: execArgs.Command,
			Cwd:     execArgs.Cwd,
			Text:    execArgs.Reason,
			Status:  "pending",
			Approval: &model.ApprovalRef{
				RequestID: approval.ID,
				Type:      approval.Type,
				Decisions: approval.Decisions,
			},
			CreatedAt: approval.RequestedAt,
			UpdatedAt: approval.RequestedAt,
		}
		applyCardHost(&card, host)
		a.store.UpsertCard(sessionID, card)
		a.recordOrchestratorApprovalRequested(sessionID, approval)
		if kind := a.sessionKind(sessionID); kind == model.SessionKindPlanner || kind == model.SessionKindWorker {
			a.mirrorInternalApprovalToWorkspace(sessionID, approval, card)
			if kind == model.SessionKindWorker {
				if workspaceSessionID := strings.TrimSpace(a.sessionMeta(sessionID).WorkspaceSessionID); workspaceSessionID != "" {
					a.activateQueuedMissionWorkers(workspaceSessionID)
				}
			}
		}
		a.auditApprovalRequested(sessionID, approval, nil)
		a.broadcastSnapshot(sessionID)
		return approval.ID, nil

	case "write_file":
		hostID, err := a.validateBifrostSelectedRemoteHost(sessionID, fileArgs.HostID)
		if err != nil {
			return "", err
		}
		if err := a.ensureWritableRootsForHost(hostID, []string{fileArgs.Path}); err != nil {
			return "", err
		}
		oldContent := ""
		created := true
		readCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if result, err := a.remoteReadFile(readCtx, hostID, fileArgs.Path, 256*1024); err == nil {
			oldContent = result.Content
			created = false
		}
		newContent := fileArgs.Content
		if strings.EqualFold(fileArgs.WriteMode, "append") {
			newContent = oldContent + fileArgs.Content
		}
		change := model.FileChange{
			Path: fileArgs.Path,
			Kind: remoteFileChangeKind(created, fileArgs.WriteMode),
			Diff: renderFileDiff(fileArgs.Path, oldContent, newContent),
		}
		approval := model.ApprovalRequest{
			ID:          model.NewID("approval"),
			HostID:      hostID,
			Fingerprint: approvalFingerprintForFileChange(hostID, filepath.Dir(fileArgs.Path), []model.FileChange{change}),
			Type:        bifrostApprovalTypeRemoteFileChange,
			Status:      "pending",
			ItemID:      dynamicToolCardID(call.ID),
			Reason:      fileArgs.Reason,
			GrantRoot:   filepath.Dir(fileArgs.Path),
			Changes:     []model.FileChange{change},
			Decisions:   []string{"accept", "accept_session", "decline"},
			RequestedAt: model.NowString(),
		}
		if autoResolved := a.autoApproveBifrostToolApproval(session, approval, !capabilityNeedsApproval(a.effectiveCapabilityState(hostID, "fileChange"))); autoResolved {
			return approval.ID, nil
		}

		a.setRuntimeTurnPhase(sessionID, "waiting_approval")
		a.store.AddApproval(sessionID, approval)
		card := model.Card{
			ID:      approval.ItemID,
			Type:    "FileChangeApprovalCard",
			Title:   "Remote file change approval required",
			Text:    approval.Reason,
			Status:  "pending",
			Changes: approval.Changes,
			Approval: &model.ApprovalRef{
				RequestID: approval.ID,
				Type:      approval.Type,
				Decisions: approval.Decisions,
			},
			CreatedAt: approval.RequestedAt,
			UpdatedAt: approval.RequestedAt,
		}
		applyCardHost(&card, a.findHost(hostID))
		a.store.UpsertCard(sessionID, card)
		a.recordOrchestratorApprovalRequested(sessionID, approval)
		if kind := a.sessionKind(sessionID); kind == model.SessionKindPlanner || kind == model.SessionKindWorker {
			a.mirrorInternalApprovalToWorkspace(sessionID, approval, card)
			if kind == model.SessionKindWorker {
				if workspaceSessionID := strings.TrimSpace(a.sessionMeta(sessionID).WorkspaceSessionID); workspaceSessionID != "" {
					a.activateQueuedMissionWorkers(workspaceSessionID)
				}
			}
		}
		a.auditApprovalRequested(sessionID, approval, map[string]any{"filePath": fileArgs.Path})
		a.broadcastSnapshot(sessionID)
		return approval.ID, nil
	default:
		return "", fmt.Errorf("unsupported bifrost approval tool %q", call.Function.Name)
	}
}

func (a *App) autoApproveBifrostToolApproval(session *agentloop.Session, approval model.ApprovalRequest, allowByPolicy bool) bool {
	sessionID := session.ID
	if approval.Fingerprint != "" {
		if _, ok := a.store.ApprovalGrant(sessionID, approval.Fingerprint); ok {
			a.resolveAutoApprovedBifrostToolApproval(session, approval, "accepted_for_session_auto", "Auto-approved for session", autoApprovalNoticeText(approval), "accept_session")
			return true
		}
	}
	if approval.Fingerprint != "" && approval.HostID != "" && a.approvalGrantStore != nil {
		if _, ok := a.approvalGrantStore.MatchFingerprint(approval.HostID, approval.Fingerprint); ok {
			a.resolveAutoApprovedBifrostToolApproval(session, approval, "accepted_for_host_auto", "Auto-approved by host grant", hostGrantAutoApprovalNoticeText(approval), "accept")
			return true
		}
	}
	if allowByPolicy {
		a.resolveAutoApprovedBifrostToolApproval(session, approval, "accepted_by_policy_auto", "Auto-approved by profile", "当前 main-agent profile 允许该操作直接执行，因此已自动放行。", "accept")
		return true
	}
	return false
}

func (a *App) resolveAutoApprovedBifrostToolApproval(session *agentloop.Session, approval model.ApprovalRequest, status, title, text, decision string) {
	now := model.NowString()
	approval.Status = status
	approval.ResolvedAt = now
	a.store.AddApproval(session.ID, approval)
	a.store.ResolveApproval(session.ID, approval.ID, approval.Status, now)
	a.store.UpsertCard(session.ID, model.Card{
		ID:        "auto-approval-" + approval.ItemID,
		Type:      "NoticeCard",
		Title:     title,
		Text:      text,
		Status:    "notice",
		CreatedAt: now,
		UpdatedAt: now,
	})
	a.setRuntimeTurnPhase(session.ID, "executing")
	if a.orchestrator != nil && a.sessionKind(session.ID) == model.SessionKindWorker {
		_ = a.orchestrator.SyncWorkerPhase(session.ID, "executing")
	}
	a.recordOrchestratorApprovalResolved(session.ID, approval)
	a.broadcastSnapshot(session.ID)
	session.ResolveApproval(agentloop.ApprovalDecision{
		ApprovalID: approval.ID,
		Decision:   decision,
	})
}

func (a *App) OnAssistantDelta(_ context.Context, session *agentloop.Session, delta string) error {
	if strings.TrimSpace(delta) == "" && delta == "" {
		return nil
	}
	cardID := strings.TrimSpace(session.CurrentCardID())
	if cardID == "" {
		cardID = model.NewID("msg")
		now := model.NowString()
		a.store.UpsertCard(session.ID, model.Card{
			ID:        cardID,
			Type:      "AssistantMessageCard",
			Role:      "assistant",
			Status:    "inProgress",
			CreatedAt: now,
			UpdatedAt: now,
		})
		session.SetCurrentCardID(cardID)
		a.markTurnTraceFirstAssistant(session.ID, cardID, "delta")
	}
	a.store.UpdateCard(session.ID, cardID, func(card *model.Card) {
		card.Text += delta
		card.UpdatedAt = model.NowString()
	})
	a.throttledBroadcast(session.ID)
	return nil
}

func (a *App) OnToolCallDelta(_ context.Context, session *agentloop.Session, _ int, _ bifrost.ToolCall) error {
	a.setRuntimeTurnPhase(session.ID, "executing")
	return nil
}

func (a *App) OnStreamComplete(_ context.Context, session *agentloop.Session, result *agentloop.StreamResult) error {
	if cardID := strings.TrimSpace(session.CurrentCardID()); cardID != "" {
		a.store.UpdateCard(session.ID, cardID, func(card *model.Card) {
			card.Status = "completed"
			card.UpdatedAt = model.NowString()
		})
		session.SetCurrentCardID("")
	}
	if result != nil && len(result.ToolCalls) == 0 {
		a.setRuntimeTurnPhase(session.ID, "finalizing")
	}
	a.flushThrottledBroadcast(session.ID)
	a.broadcastSnapshot(session.ID)
	return nil
}

func (a *App) resolveBifrostApproval(targetSessionID string, approval model.ApprovalRequest, decision string) error {
	session, ok := a.bifrostSession(targetSessionID)
	if !ok || session == nil {
		return errors.New("bifrost session not found")
	}
	now := model.NowString()
	cardStatus := approvalStatusFromDecision(decision)
	a.store.ResolveApproval(targetSessionID, approval.ID, cardStatus, now)
	approval.Status = cardStatus
	approval.ResolvedAt = now
	a.store.UpsertCard(targetSessionID, approvalMemoCard(a.findHost(approval.HostID), approval, decision, now))
	a.store.UpdateCard(targetSessionID, approval.ItemID, func(card *model.Card) {
		card.Status = cardStatus
		card.UpdatedAt = now
	})

	nextPhase := "thinking"
	if a.hasPendingApprovals(targetSessionID) {
		nextPhase = "waiting_approval"
	} else if decision == "accept" || decision == "accept_session" {
		nextPhase = "executing"
	}
	a.setRuntimeTurnPhase(targetSessionID, nextPhase)
	if a.orchestrator != nil && a.sessionKind(targetSessionID) == model.SessionKindWorker {
		_ = a.orchestrator.SyncWorkerPhase(targetSessionID, nextPhase)
	}
	a.recordOrchestratorApprovalResolved(targetSessionID, approval)
	a.auditApprovalLifecycleEvent("approval.decision", targetSessionID, approval, decision, cardStatus, approval.RequestedAt, now, nil)
	a.broadcastSnapshot(targetSessionID)
	session.ResolveApproval(agentloop.ApprovalDecision{
		ApprovalID: approval.ID,
		Decision:   decision,
	})
	return nil
}
