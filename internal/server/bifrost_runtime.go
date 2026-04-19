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
	"github.com/lizhongxuan/aiops-codex/internal/filepatch"
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

	// Web search handler selection based on WebSearchMode config.
	webSearchMode := strings.ToLower(strings.TrimSpace(a.cfg.WebSearchMode))
	switch webSearchMode {
	case "disabled":
		// Skip web_search tool registration entirely.
		log.Printf("[bifrost] web search disabled by configuration")
	case "native":
		// Register the tool (it may be filtered out at request time if provider supports native search).
		// Check if the default provider supports native search; if not, fall back to DuckDuckGo.
		caps := gateway.ProviderCapabilities(strings.TrimSpace(a.cfg.LLMModel))
		if caps.SupportsNativeSearch {
			agentloop.RegisterWebSearchTools(reg)
			log.Printf("[bifrost] web search mode: native (provider supports native search)")
		} else {
			agentloop.RegisterWebSearchTools(reg)
			log.Printf("[bifrost] warning: web search mode is 'native' but provider does not support native search; falling back to DuckDuckGo")
			webSearchMode = "duckduckgo"
		}
	case "brave":
		if strings.TrimSpace(a.cfg.BraveAPIKey) != "" {
			agentloop.RegisterWebSearchTools(reg)
			// Override the web_search handler with BraveSearchHandler.
			if entry, ok := reg.Get("web_search"); ok && entry != nil {
				entry.Handler = agentloop.BraveSearchHandler(strings.TrimSpace(a.cfg.BraveAPIKey))
			}
			log.Printf("[bifrost] web search mode: brave")
		} else {
			agentloop.RegisterWebSearchTools(reg)
			log.Printf("[bifrost] warning: web search mode is 'brave' but BRAVE_API_KEY is not set; falling back to DuckDuckGo")
			webSearchMode = "duckduckgo"
		}
	default:
		// "duckduckgo" or any unrecognized value — use default DuckDuckGo handler.
		agentloop.RegisterWebSearchTools(reg)
		webSearchMode = "duckduckgo"
	}

	if a.corootClient != nil {
		agentloop.RegisterCorootTools(reg)
	}
	a.wireBifrostToolHandlers(reg)
	if err := a.registerBifrostMCPTools(reg); err != nil {
		return err
	}

	a.bifrostGateway = gateway
	a.agentLoop = agentloop.NewLoop(gateway, reg, nil).
		SetWebSearchMode(webSearchMode).
		SetApprovalHandler(a).
		SetStreamObserver(a).
		SetToolObserver(a).
		SetTurnCompletionValidator(a)
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
		if existing.Metadata == nil {
			existing.Metadata = make(map[string]string)
		}
		existing.Metadata["session_kind"] = model.SessionKindSingleHost
		existing.Metadata["prefer_explicit_web_search"] = "true"
		return existing, nil
	}

	session := agentloop.NewSession(sessionID, bifrostSessionSpecFromThreadSpec(spec, a.cfg.LLMModel))
	if session.Metadata == nil {
		session.Metadata = make(map[string]string)
	}
	session.Metadata["session_kind"] = model.SessionKindSingleHost
	session.Metadata["prefer_explicit_web_search"] = "true"
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
		default:
			if strings.HasPrefix(strings.TrimSpace(getStringAny(tool, "name")), "mcp_") {
				add(getStringAny(tool, "name"))
			}
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
	if a.sessionKind(sessionID) == model.SessionKindWorkspace {
		a.prepareWorkspaceTurnRuntime(ctx, session, req)
	}

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
	mustSet := func(name string, handler agentloop.SessionToolHandler) {
		entry, ok := reg.Get(name)
		if !ok || entry == nil {
			panic("missing bifrost tool registration: " + name)
		}
		entry.Handler = agentloop.WrapSessionHandler(handler)
	}
	setIfPresent := func(name string, handler agentloop.SessionToolHandler) {
		entry, ok := reg.Get(name)
		if !ok || entry == nil {
			return
		}
		if handler == nil {
			entry.Handler = nil
			return
		}
		entry.Handler = agentloop.WrapSessionHandler(handler)
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
}

func (a *App) bifrostExecuteCorootTool(toolName string) agentloop.SessionToolHandler {
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

	now := model.NowString()
	cardID := dynamicToolCardID(call.ID)
	card := model.Card{
		ID:      dynamicToolCardID(call.ID),
		Type:    "WorkspaceResultCard",
		Title:   "AI Server State Query",
		Summary: fmt.Sprintf("查询焦点: %s | Hosts: %d | 待审批: %d", focus, len(hosts), pendingApprovals),
		Text:    "",
		Status:  "completed",
		Detail: map[string]any{
			"tool":  "query_ai_server_state",
			"focus": focus,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	a.store.UpsertCard(sessionID, card)
	stateJSON, _ := json.Marshal(state)
	evidenceID := a.bindCardEvidence(sessionID, cardID, evidenceArtifactInput{
		Kind:       "ai_server_state",
		SourceKind: "state_snapshot",
		SourceRef:  focus,
		Title:      card.Title,
		Summary:    card.Summary,
		Content:    string(stateJSON),
		Raw:        state,
		Metadata: map[string]any{
			"focus": focus,
		},
	})
	responseText := fmt.Sprintf("AI Server State (focus=%s):\n%s\n\n[evidence: %s]", focus, string(stateJSON), evidenceID)
	a.store.UpdateCard(sessionID, cardID, func(card *model.Card) {
		card.Text = responseText
		card.UpdatedAt = model.NowString()
	})
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
			"tool":        "enter_plan_mode",
			"displayName": "进入计划模式",
			"toolKind":    "plan",
			"mode":        "plan",
			"goal":        goal,
			"reason":      reason,
			"scope":       strings.TrimSpace(getStringAny(arguments, "scope")),
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
			"tool":        "update_plan",
			"displayName": "计划更新",
			"toolKind":    "plan",
			"mode":        "plan",
			"summary":     summary,
			"background":  strings.TrimSpace(getStringAny(arguments, "background")),
			"scope":       strings.TrimSpace(getStringAny(arguments, "scope")),
			"assumptions": strings.TrimSpace(getStringAny(arguments, "assumptions")),
			"risk":        strings.TrimSpace(getStringAny(arguments, "risk", "risks")),
			"rollback":    strings.TrimSpace(getStringAny(arguments, "rollback")),
			"validation":  strings.TrimSpace(getStringAny(arguments, "validation")),
			"steps":       arguments["steps"],
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
		Detail: a.approvalCardDetail(session.ID, approval, map[string]any{
			"tool":        "exit_plan_mode",
			"displayName": "计划审批",
			"toolKind":    "approval",
			"mode":        "plan",
			"summary":     summary,
			"plan":        strings.TrimSpace(getStringAny(arguments, "plan")),
			"assumptions": strings.TrimSpace(getStringAny(arguments, "assumptions")),
			"risk":        strings.TrimSpace(getStringAny(arguments, "risk", "risks")),
			"rollback":    strings.TrimSpace(getStringAny(arguments, "rollback")),
			"validation":  strings.TrimSpace(getStringAny(arguments, "validation")),
			"tasks":       arguments["tasks"],
		}),
		CreatedAt: now,
		UpdatedAt: now,
	}
	a.setRuntimeTurnPhase(session.ID, "waiting_approval")
	emitted := a.emitBifrostApprovalRequestedEvent(context.Background(), session.ID, "exit_plan_mode", approval, card)
	if !emitted {
		a.store.AddApproval(session.ID, approval)
		a.store.UpsertCard(session.ID, card)
		a.projectApprovalRequestedFallback(session.ID, approval, card, false)
	}
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
	if a.workspacePlanModeNeedsApproval(session.ID) {
		a.blockApprovalByPlanMode(session.ID, approval, "计划审批通过前不能请求动作审批")
		return "", errors.New("计划审批通过前不能请求动作审批")
	}
	card := model.Card{
		ID:      cardID,
		Type:    "ApprovalCard",
		Title:   fmt.Sprintf("审批请求: %s", truncate(command, 60)),
		Summary: fmt.Sprintf("Host: %s | Risk: %s", hostID, truncate(riskAssessment, 80)),
		Status:  "pending",
		Detail: a.approvalCardDetail(session.ID, approval, map[string]any{
			"riskAssessment":     riskAssessment,
			"expectedImpact":     expectedImpact,
			"rollbackSuggestion": rollbackSuggestion,
			"toolCallId":         call.ID,
		}),
		Approval: &model.ApprovalRef{
			RequestID: approval.ID,
			Type:      approval.Type,
			Decisions: approval.Decisions,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	a.setRuntimeTurnPhase(session.ID, "waiting_approval")
	emitted := a.emitBifrostApprovalRequestedEvent(context.Background(), session.ID, call.Function.Name, approval, card)
	if !emitted {
		a.store.AddApproval(session.ID, approval)
		a.store.UpsertCard(session.ID, card)
		a.projectApprovalRequestedFallback(session.ID, approval, card, false)
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

func (a *App) bifrostDispatchTasks(_ context.Context, session *agentloop.Session, call bifrost.ToolCall, arguments map[string]any) (string, error) {
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
	now := model.NowString()
	cardID := ""
	if strings.TrimSpace(call.ID) != "" {
		cardID = dynamicToolCardID(call.ID)
	}
	if strings.TrimSpace(cardID) == "" {
		cardID = model.NewID("dispatch")
	}
	card := model.Card{
		ID:      cardID,
		Type:    "DispatchSummaryCard",
		Title:   fmt.Sprintf("派发 %d 个任务", result.Accepted),
		Summary: fmt.Sprintf("Activated: %d | Queued: %d", result.Activated, result.Queued),
		Status:  "inProgress",
		Detail: map[string]any{
			"tool":                "orchestrator_dispatch_tasks",
			"displayName":         "任务派发",
			"toolKind":            "agent",
			"accepted":            result.Accepted,
			"activated":           result.Activated,
			"queued":              result.Queued,
			"targetSummary":       dispatchTaskTargetSummary(req),
			"workerStatusSummary": dispatchWorkerStatusSummary(result),
			"subtaskSummary":      dispatchTaskTitlesSummary(req),
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	a.store.UpsertCard(session.ID, card)
	a.bindCardEvidence(session.ID, cardID, evidenceArtifactInput{
		Kind:       "dispatch_workers",
		SourceKind: "orchestration",
		SourceRef:  firstNonEmptyValue(cardID, "dispatch"),
		Title:      card.Title,
		Summary:    card.Summary,
		Content:    stableCardJSON(arguments),
		Raw: map[string]any{
			"accepted":  result.Accepted,
			"activated": result.Activated,
			"queued":    result.Queued,
			"tasks":     arguments,
		},
	})
	a.broadcastSnapshot(session.ID)
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
	return a.executeBifrostUnifiedToolResult(ctx, session, call, "list_files", "list_remote_files", hostID, arguments, a.remoteFileListUnifiedTool())
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
	return a.executeBifrostUnifiedToolResult(ctx, session, call, "read_file", "read_remote_file", hostID, arguments, a.remoteFileReadUnifiedTool())
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
	return a.executeBifrostUnifiedToolResult(ctx, session, call, "search_files", "search_remote_files", hostID, arguments, a.remoteFileSearchUnifiedTool())
}

func (a *App) executeBifrostUnifiedToolResult(ctx context.Context, session *agentloop.Session, call bifrost.ToolCall, toolName, projectedToolName, hostID string, arguments map[string]any, tool UnifiedTool) (string, error) {
	if a == nil || tool == nil {
		return "", errors.New("bifrost tool is not configured")
	}

	invocationArgs := cloneAnyMap(arguments)
	if invocationArgs == nil {
		invocationArgs = make(map[string]any)
	}
	if strings.TrimSpace(getStringAny(invocationArgs, "hostId", "host_id")) == "" && strings.TrimSpace(hostID) != "" {
		invocationArgs["hostId"] = strings.TrimSpace(hostID)
	}

	req := ToolCallRequest{
		Invocation: ToolInvocation{
			InvocationID: model.NewID("toolinv"),
			SessionID:    session.ID,
			ThreadID:     a.sessionThreadID(session.ID),
			TurnID:       a.sessionTurnID(session.ID),
			ToolName:     toolName,
			ToolKind:     "bifrost",
			Source:       ToolInvocationSourceAgentloopToolCall,
			HostID:       defaultHostID(hostID),
			CallID:       strings.TrimSpace(call.ID),
			Arguments:    invocationArgs,
			ReadOnly:     true,
			StartedAt:    time.Now(),
		},
		Input: invocationArgs,
	}
	req.Normalize()

	callResult, err := tool.Call(ctx, req)
	if err != nil {
		return "", err
	}
	display := callResult.DisplayOutput
	if display == nil && tool.Display() != nil {
		display = tool.Display().RenderResult(callResult)
	}
	result := toolExecutionResultFromCallResult(req.Invocation, callResult, display)
	if projectedToolName != "" && projectedToolName != toolName {
		if result.ProjectionPayload == nil {
			result.ProjectionPayload = make(map[string]any)
		}
		result.ProjectionPayload["toolNameOverride"] = projectedToolName
	}
	a.storeBifrostToolResult(session.ID, toolName, result)

	switch result.Status {
	case ToolRunStatusCompleted:
		return result.OutputText, nil
	case ToolRunStatusCancelled:
		return "", errors.New(firstNonEmptyValue(strings.TrimSpace(result.ErrorText), "tool execution cancelled"))
	default:
		return "", errors.New(firstNonEmptyValue(strings.TrimSpace(result.ErrorText), strings.TrimSpace(result.OutputText), "tool execution failed"))
	}
}

func (a *App) storeBifrostToolResult(sessionID, toolName string, result ToolExecutionResult) {
	if a == nil {
		return
	}
	a.bifrostToolResults.Store(sessionID+":"+toolName, result)
}

func (a *App) consumeBifrostToolResult(sessionID, toolName string) (ToolExecutionResult, bool) {
	if a == nil {
		return ToolExecutionResult{}, false
	}
	key := sessionID + ":" + toolName
	raw, ok := a.bifrostToolResults.LoadAndDelete(key)
	if !ok {
		return ToolExecutionResult{}, false
	}
	result, ok := raw.(ToolExecutionResult)
	return result, ok
}

func synthesizeBifrostApplyPatchResult(processCardID string, args map[string]interface{}, outputText string, execErr error) (ToolExecutionResult, bool) {
	patchText, _ := args["patch"].(string)
	if strings.TrimSpace(patchText) == "" {
		return ToolExecutionResult{}, false
	}
	action, err := filepatch.ParsePatch(patchText)
	if err != nil {
		return ToolExecutionResult{}, false
	}
	return patchExecutionResult(processCardID, action, outputText, execErr, time.Now())
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

	now := model.NowString()
	a.completeToolProcess(session.ID, processCardID, "已修改 "+result.Path)
	a.store.UpdateRuntime(session.ID, func(runtime *model.RuntimeState) {
		if runtime.Activity.CurrentChangingFile == args.Path || runtime.Activity.CurrentChangingFile == result.Path {
			runtime.Activity.CurrentChangingFile = ""
		}
		runtime.Activity.FilesChanged++
	})
	card := fileWriteResultCard(
		dynamicToolCardID(call.ID),
		hostID,
		hostNameOrID(a.findHost(hostID)),
		result,
		nil,
		now,
		now,
	)
	a.store.UpsertCard(session.ID, card)
	a.syncActionArtifacts(session.ID, card)
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
		if a.workspacePlanModeNeedsApproval(sessionID) && !readonly {
			a.blockApprovalByPlanMode(sessionID, approval, "计划审批通过前不能执行变更命令")
			return "", errors.New("计划审批通过前不能执行变更命令")
		}
		resolution, err := a.toolApprovalCoordinator.Request(ctx, buildToolApprovalRequestForExistingApproval(
			sessionID,
			call.Function.Name,
			approval,
			decision.Mode == model.AgentPermissionModeAllow && !capabilityNeedsApproval(a.effectiveCapabilityState(hostID, "commandExecution")),
			readonly,
		))
		if err != nil {
			return "", err
		}
		if resolution.IsApproved() {
			a.resolveAutoApprovedBifrostToolApproval(ctx, session, approval, resolution)
			return approval.ID, nil
		}

		card := model.Card{
			ID:      approval.ItemID,
			Type:    "CommandApprovalCard",
			Title:   "Remote command approval required",
			Command: execArgs.Command,
			Cwd:     execArgs.Cwd,
			Text:    execArgs.Reason,
			Status:  "pending",
			Detail:  a.approvalCardDetail(sessionID, approval, nil),
			Approval: &model.ApprovalRef{
				RequestID: approval.ID,
				Type:      approval.Type,
				Decisions: approval.Decisions,
			},
			CreatedAt: approval.RequestedAt,
			UpdatedAt: approval.RequestedAt,
		}
		applyCardHost(&card, host)
		a.setRuntimeTurnPhase(sessionID, "waiting_approval")
		emitted := a.emitBifrostApprovalRequestedEventWithOptions(ctx, sessionID, call.Function.Name, approval, card, approvalRequestedEventOptions{
			activateQueuedWorkers: a.sessionKind(sessionID) == model.SessionKindWorker,
		})
		if !emitted {
			a.store.AddApproval(sessionID, approval)
			a.store.UpsertCard(sessionID, card)
			a.projectApprovalRequestedFallback(sessionID, approval, card, true)
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
		if a.workspacePlanModeNeedsApproval(sessionID) {
			a.blockApprovalByPlanMode(sessionID, approval, "计划审批通过前不能执行文件变更")
			return "", errors.New("计划审批通过前不能执行文件变更")
		}
		resolution, err := a.toolApprovalCoordinator.Request(ctx, buildToolApprovalRequestForExistingApproval(
			sessionID,
			call.Function.Name,
			approval,
			!capabilityNeedsApproval(a.effectiveCapabilityState(hostID, "fileChange")),
			false,
		))
		if err != nil {
			return "", err
		}
		if resolution.IsApproved() {
			a.resolveAutoApprovedBifrostToolApproval(ctx, session, approval, resolution)
			return approval.ID, nil
		}

		card := model.Card{
			ID:      approval.ItemID,
			Type:    "FileChangeApprovalCard",
			Title:   "Remote file change approval required",
			Text:    approval.Reason,
			Status:  "pending",
			Changes: approval.Changes,
			Detail: a.approvalCardDetail(sessionID, approval, map[string]any{
				"filePath": fileArgs.Path,
			}),
			Approval: &model.ApprovalRef{
				RequestID: approval.ID,
				Type:      approval.Type,
				Decisions: approval.Decisions,
			},
			CreatedAt: approval.RequestedAt,
			UpdatedAt: approval.RequestedAt,
		}
		applyCardHost(&card, a.findHost(hostID))
		a.setRuntimeTurnPhase(sessionID, "waiting_approval")
		emitted := a.emitBifrostApprovalRequestedEventWithOptions(ctx, sessionID, call.Function.Name, approval, card, approvalRequestedEventOptions{
			activateQueuedWorkers: a.sessionKind(sessionID) == model.SessionKindWorker,
		})
		if !emitted {
			a.store.AddApproval(sessionID, approval)
			a.store.UpsertCard(sessionID, card)
			a.projectApprovalRequestedFallback(sessionID, approval, card, true)
		}
		a.auditApprovalRequested(sessionID, approval, map[string]any{"filePath": fileArgs.Path})
		a.broadcastSnapshot(sessionID)
		return approval.ID, nil
	default:
		return "", fmt.Errorf("unsupported bifrost approval tool %q", call.Function.Name)
	}
}

func (a *App) resolveAutoApprovedBifrostToolApproval(ctx context.Context, session *agentloop.Session, approval model.ApprovalRequest, resolution ApprovalResolution) {
	status, title, text, decision := bifrostAutoApprovalPresentation(approval, resolution.RuleName)
	now := model.NowString()
	approval.Status = status
	approval.ResolvedAt = now
	card := model.Card{
		ID:        "auto-approval-" + approval.ItemID,
		Type:      "NoticeCard",
		Title:     title,
		Text:      text,
		Status:    "notice",
		CreatedAt: now,
		UpdatedAt: now,
	}
	emitted := a.emitBifrostApprovalResolvedEvent(ctx, session.ID, resolution.ToolName, "executing", approval, card)
	if !emitted {
		a.store.AddApproval(session.ID, approval)
		a.store.ResolveApproval(session.ID, approval.ID, approval.Status, now)
		a.store.UpsertCard(session.ID, card)
	}
	a.setRuntimeTurnPhase(session.ID, "executing")
	if !emitted {
		a.syncWorkerPhaseAndRefreshWorkspace(session.ID, "executing")
	}
	a.recordOrchestratorApprovalResolved(session.ID, approval)
	a.broadcastSnapshot(session.ID)
	session.ResolveApproval(agentloop.ApprovalDecision{
		ApprovalID: approval.ID,
		Decision:   decision,
	})
}

func bifrostAutoApprovalPresentation(approval model.ApprovalRequest, ruleName string) (status, title, text, decision string) {
	return approvalAutoApprovalPresentation(approval, ruleName)
}

func (a *App) emitBifrostApprovalRequestedEvent(ctx context.Context, sessionID, toolName string, approval model.ApprovalRequest, card model.Card) bool {
	return a.emitBifrostApprovalRequestedEventWithOptions(ctx, sessionID, toolName, approval, card, approvalRequestedEventOptions{})
}

func (a *App) emitBifrostApprovalRequestedEventWithOptions(ctx context.Context, sessionID, toolName string, approval model.ApprovalRequest, card model.Card, opts approvalRequestedEventOptions) bool {
	return a.emitApprovalRequestedEventWithOptions(ctx, sessionID, toolName, approval, card, opts)
}

func (a *App) emitBifrostApprovalResolvedEvent(ctx context.Context, sessionID, toolName, phase string, approval model.ApprovalRequest, card model.Card) bool {
	return a.emitApprovalResolvedEvent(ctx, sessionID, toolName, phase, approval, card)
}

func newBifrostApprovalRequestedEvent(sessionID, toolName string, approval model.ApprovalRequest, card model.Card) ToolLifecycleEvent {
	return ToolLifecycleEvent{
		EventID:    model.NewID("toolevent"),
		SessionID:  sessionID,
		ToolName:   firstNonEmptyValue(strings.TrimSpace(toolName), bifrostApprovalToolName(approval.Type)),
		Type:       ToolLifecycleEventApprovalRequested,
		Phase:      "waiting_approval",
		HostID:     defaultHostID(approval.HostID),
		CardID:     card.ID,
		ApprovalID: approval.ID,
		Label:      firstNonEmptyValue(strings.TrimSpace(card.Title), strings.TrimSpace(approval.Reason), strings.TrimSpace(approval.Command), "需要审批"),
		Message:    firstNonEmptyValue(strings.TrimSpace(card.Text), strings.TrimSpace(approval.Reason), strings.TrimSpace(approval.Command), "需要审批"),
		CreatedAt:  firstNonEmptyValue(strings.TrimSpace(approval.RequestedAt), strings.TrimSpace(card.CreatedAt), model.NowString()),
		Payload: map[string]any{
			"approval": bifrostApprovalEventPayload(approval),
			"card":     bifrostApprovalCardEventPayload(card),
		},
	}
}

func newBifrostApprovalResolvedEvent(sessionID, toolName, phase string, approval model.ApprovalRequest, card model.Card) ToolLifecycleEvent {
	return ToolLifecycleEvent{
		EventID:    model.NewID("toolevent"),
		SessionID:  sessionID,
		ToolName:   firstNonEmptyValue(strings.TrimSpace(toolName), bifrostApprovalToolName(approval.Type)),
		Type:       ToolLifecycleEventApprovalResolved,
		Phase:      firstNonEmptyValue(strings.TrimSpace(phase), "thinking"),
		HostID:     defaultHostID(approval.HostID),
		CardID:     card.ID,
		ApprovalID: approval.ID,
		Label:      firstNonEmptyValue(strings.TrimSpace(card.Title), strings.TrimSpace(card.Text), strings.TrimSpace(approval.Reason), "审批已处理"),
		Message:    firstNonEmptyValue(strings.TrimSpace(card.Text), strings.TrimSpace(card.Title), strings.TrimSpace(approval.Reason), "审批已处理"),
		CreatedAt:  firstNonEmptyValue(strings.TrimSpace(approval.ResolvedAt), strings.TrimSpace(card.UpdatedAt), model.NowString()),
		Payload: map[string]any{
			"approval": bifrostApprovalEventPayload(approval),
			"card":     bifrostApprovalCardEventPayload(card),
		},
	}
}

func bifrostApprovalEventPayload(approval model.ApprovalRequest) map[string]any {
	return map[string]any{
		"approvalId":   approval.ID,
		"requestIdRaw": approval.RequestIDRaw,
		"hostId":       defaultHostID(approval.HostID),
		"fingerprint":  approval.Fingerprint,
		"approvalType": approval.Type,
		"status":       approval.Status,
		"threadId":     approval.ThreadID,
		"turnId":       approval.TurnID,
		"itemId":       approval.ItemID,
		"command":      approval.Command,
		"cwd":          approval.Cwd,
		"reason":       approval.Reason,
		"grantRoot":    approval.GrantRoot,
		"changes":      append([]model.FileChange(nil), approval.Changes...),
		"requestedAt":  approval.RequestedAt,
		"resolvedAt":   approval.ResolvedAt,
		"decisions":    append([]string(nil), approval.Decisions...),
	}
}

func bifrostApprovalCardEventPayload(card model.Card) map[string]any {
	return map[string]any{
		"cardId":    card.ID,
		"cardType":  card.Type,
		"title":     card.Title,
		"text":      card.Text,
		"summary":   card.Summary,
		"status":    card.Status,
		"command":   card.Command,
		"cwd":       card.Cwd,
		"hostId":    card.HostID,
		"hostName":  card.HostName,
		"changes":   append([]model.FileChange(nil), card.Changes...),
		"detail":    cloneAnyMap(card.Detail),
		"createdAt": card.CreatedAt,
		"updatedAt": card.UpdatedAt,
	}
}

func bifrostApprovalToolName(approvalType string) string {
	switch strings.TrimSpace(approvalType) {
	case bifrostApprovalTypeRemoteFileChange:
		return "write_file"
	case bifrostApprovalTypeRemoteCommand:
		return "execute_command"
	default:
		return strings.TrimSpace(approvalType)
	}
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
	approval.Status = cardStatus
	approval.ResolvedAt = now

	nextPhase := "thinking"
	if a.hasPendingApprovals(targetSessionID) {
		nextPhase = "waiting_approval"
	} else if decision == "accept" || decision == "accept_session" {
		nextPhase = "executing"
	}
	memoCard := approvalMemoCard(a.findHost(approval.HostID), approval, decision, now)
	emitted := a.emitBifrostApprovalResolvedEvent(context.Background(), targetSessionID, bifrostApprovalToolName(approval.Type), nextPhase, approval, memoCard)
	if !emitted {
		a.store.ResolveApproval(targetSessionID, approval.ID, cardStatus, now)
		a.store.UpsertCard(targetSessionID, memoCard)
		a.store.UpdateCard(targetSessionID, approval.ItemID, func(card *model.Card) {
			card.Status = cardStatus
			card.UpdatedAt = now
		})
	}
	a.setRuntimeTurnPhase(targetSessionID, nextPhase)
	if !emitted {
		a.syncWorkerPhaseAndRefreshWorkspace(targetSessionID, nextPhase)
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

// ---------------------------------------------------------------------------
// ToolExecutionObserver implementation — creates ProcessLineCards and updates
// runtime.Activity so the frontend shows real-time tool status.
// ---------------------------------------------------------------------------

func (a *App) OnToolStart(ctx context.Context, session *agentloop.Session, toolName string, args map[string]interface{}) {
	sessionID := session.ID

	// For single_host sessions, keep the UI lightweight, but still update
	// runtime.Activity so the frontend can stream Codex-style progress lines.
	kind := a.sessionKind(sessionID)
	if isSingleHostSessionKind(kind) {
		if a.emitBifrostToolStartEvent(ctx, sessionID, kind, toolName, args) {
			return
		}
		a.store.UpdateRuntime(sessionID, func(rt *model.RuntimeState) {
			switch toolName {
			case "web_search":
				query, _ := args["query"].(string)
				rt.Activity.CurrentSearchKind = "web"
				rt.Activity.CurrentSearchQuery = query
				rt.Activity.CurrentWebSearchQuery = query
			case "open_page", "find_in_page":
				url, _ := args["url"].(string)
				rt.Activity.CurrentReadingFile = url
			case "list_files", "list_dir":
				path, _ := args["path"].(string)
				rt.Activity.CurrentListingPath = path
			case "read_file":
				path, _ := args["path"].(string)
				rt.Activity.CurrentReadingFile = path
			case "search_files":
				query, _ := args["query"].(string)
				rt.Activity.CurrentSearchKind = "content"
				rt.Activity.CurrentSearchQuery = query
			}
		})
		a.setRuntimeTurnPhase(sessionID, "thinking")
		a.broadcastSnapshot(sessionID)
		return
	}

	if a.emitBifrostToolStartEvent(ctx, sessionID, kind, toolName, args) {
		return
	}

	// Workspace / worker sessions get full activity tracking.
	phase, label := toolPhaseAndLabel(toolName, args)
	a.setRuntimeTurnPhase(sessionID, phase)

	// Update activity tracking.
	a.store.UpdateRuntime(sessionID, func(rt *model.RuntimeState) {
		switch toolName {
		case "web_search":
			query, _ := args["query"].(string)
			rt.Activity.CurrentSearchKind = "web"
			rt.Activity.CurrentSearchQuery = query
			rt.Activity.CurrentWebSearchQuery = query
		case "open_page", "find_in_page":
			url, _ := args["url"].(string)
			rt.Activity.CurrentReadingFile = url
		case "list_files", "list_dir":
			path, _ := args["path"].(string)
			rt.Activity.CurrentListingPath = path
		case "read_file":
			path, _ := args["path"].(string)
			rt.Activity.CurrentReadingFile = path
		case "search_files":
			query, _ := args["query"].(string)
			rt.Activity.CurrentSearchKind = "content"
			rt.Activity.CurrentSearchQuery = query
		}
	})

	// Create a ProcessLineCard for this tool execution.
	cardID := model.NewID("proc")
	a.beginToolProcess(sessionID, cardID, phase, label)
	a.bifrostToolCards.Store(sessionID+":"+toolName, cardID)
}

func (a *App) emitBifrostToolStartEvent(ctx context.Context, sessionID, sessionKind, toolName string, args map[string]interface{}) bool {
	if a == nil || a.toolEventBus == nil {
		return false
	}
	if sessionKind != model.SessionKindWorkspace && sessionKind != model.SessionKindWorker && !isSingleHostSessionKind(sessionKind) {
		return false
	}
	if !isBifrostLifecycleProjectionTool(toolName) {
		return false
	}

	invocationArgs := make(map[string]any, len(args))
	for key, value := range args {
		invocationArgs[key] = value
	}

	invocation := ToolInvocation{
		InvocationID: model.NewID("toolinv"),
		SessionID:    sessionID,
		ToolName:     toolName,
		ToolKind:     "bifrost",
		Source:       ToolInvocationSourceAgentloopToolCall,
		HostID:       defaultHostID(a.sessionHostID(sessionID)),
		Arguments:    invocationArgs,
		ReadOnly:     isBifrostLifecycleReadOnlyTool(toolName),
		StartedAt:    time.Now(),
	}
	phase, label := toolPhaseAndLabel(toolName, args)
	if isSingleHostSessionKind(sessionKind) {
		phase = "thinking"
	}
	event := newToolStartedEvent(invocation, ToolDescriptor{
		Name:         toolName,
		Domain:       "bifrost",
		Kind:         "bifrost",
		DisplayLabel: label,
		StartPhase:   phase,
		IsReadOnly:   isBifrostLifecycleReadOnlyTool(toolName),
	})
	if event.Payload == nil {
		event.Payload = make(map[string]any)
	}
	event.Payload["trackActivityStart"] = shouldBifrostLifecycleTrackActivityStart(toolName)
	event.Payload["skipCardProjection"] = shouldBifrostLifecycleSkipCardProjection(toolName) || isSingleHostSessionKind(sessionKind)
	if err := a.toolEventBus.Emit(ctx, event); err != nil {
		log.Printf("[bifrost] failed to emit tool start event session=%s tool=%s err=%v", sessionID, toolName, err)
	}
	if !isSingleHostSessionKind(sessionKind) && !shouldBifrostLifecycleSkipCardProjection(toolName) {
		a.bifrostToolCards.Store(sessionID+":"+toolName, event.CardID)
	}
	return true
}

func isBifrostLifecycleProjectionTool(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "web_search", "open_page", "find_in_page", "list_files", "list_dir", "read_file", "search_files", "execute_command", "execute_readonly_query", "query_ai_server_state", "readonly_host_inspect", "write_file", "apply_patch":
		return true
	default:
		return false
	}
}

func shouldBifrostLifecycleTrackActivityStart(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "web_search", "open_page", "find_in_page", "list_files", "list_dir", "read_file", "search_files":
		return true
	default:
		return false
	}
}

func isBifrostLifecycleReadOnlyTool(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "web_search", "open_page", "find_in_page", "list_files", "list_dir", "read_file", "search_files", "execute_readonly_query", "query_ai_server_state", "readonly_host_inspect":
		return true
	default:
		return false
	}
}

func shouldBifrostLifecycleSkipCardProjection(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "execute_readonly_query", "query_ai_server_state":
		return true
	default:
		return false
	}
}

func (a *App) OnToolComplete(ctx context.Context, session *agentloop.Session, toolName string, args map[string]interface{}, result string, err error) {
	sessionID := session.ID
	processCardID := ""
	storedResult, hasStoredResult := a.consumeBifrostToolResult(sessionID, toolName)

	// For single_host sessions, keep updating runtime.Activity, but skip
	// ProcessLineCards so the UI stays in the lightweight Codex-style mode.
	kind := a.sessionKind(sessionID)
	if isSingleHostSessionKind(kind) {
		if a.emitBifrostToolCompletionEvent(ctx, sessionID, kind, toolName, args, processCardID, result, err, storedResult, hasStoredResult) {
			a.bifrostToolCards.Delete(sessionID + ":" + toolName)
			a.maybeCreateMCPResultCard(ctx, sessionID, toolName, processCardID, result, err)
			return
		}
		a.store.UpdateRuntime(sessionID, func(rt *model.RuntimeState) {
			switch toolName {
			case "web_search":
				query, _ := args["query"].(string)
				rt.Activity.CurrentSearchKind = ""
				rt.Activity.CurrentSearchQuery = ""
				rt.Activity.CurrentWebSearchQuery = ""
				rt.Activity.SearchedWebQueries = append(rt.Activity.SearchedWebQueries, model.ActivityEntry{Query: query})
				rt.Activity.SearchCount = len(rt.Activity.SearchedWebQueries) + len(rt.Activity.SearchedContentQueries)
			case "open_page", "find_in_page":
				rt.Activity.CurrentReadingFile = ""
				rt.Activity.FilesViewed++
			case "list_files", "list_dir":
				rt.Activity.CurrentListingPath = ""
				rt.Activity.ListCount++
			case "read_file":
				rt.Activity.CurrentReadingFile = ""
				rt.Activity.FilesViewed++
			case "search_files":
				query, _ := args["query"].(string)
				rt.Activity.CurrentSearchKind = ""
				rt.Activity.CurrentSearchQuery = ""
				rt.Activity.SearchedContentQueries = append(rt.Activity.SearchedContentQueries, model.ActivityEntry{Query: query})
				rt.Activity.SearchCount = len(rt.Activity.SearchedWebQueries) + len(rt.Activity.SearchedContentQueries)
			case "execute_command", "readonly_host_inspect", "shell_command":
				rt.Activity.CommandsRun++
			case "write_file", "apply_patch":
				rt.Activity.FilesChanged++
			}
		})
		a.maybeCreateMCPResultCard(ctx, sessionID, toolName, processCardID, result, err)
		a.setRuntimeTurnPhase(sessionID, "thinking")
		a.broadcastSnapshot(sessionID)
		return
	}

	if raw, ok := a.bifrostToolCards.Load(sessionID + ":" + toolName); ok {
		processCardID, _ = raw.(string)
	}
	if !hasStoredResult && toolName == "apply_patch" {
		storedResult, hasStoredResult = synthesizeBifrostApplyPatchResult(processCardID, args, result, err)
	}
	if a.emitBifrostToolCompletionEvent(ctx, sessionID, kind, toolName, args, processCardID, result, err, storedResult, hasStoredResult) {
		a.bifrostToolCards.Delete(sessionID + ":" + toolName)
		a.maybeCreateMCPResultCard(ctx, sessionID, toolName, processCardID, result, err)
		a.broadcastSnapshot(sessionID)
		return
	}

	// Workspace / worker sessions: complete the ProcessLineCard.
	if processCardID != "" {
		a.bifrostToolCards.Delete(sessionID + ":" + toolName)
		if processCardID != "" {
			if err != nil {
				a.failToolProcess(sessionID, processCardID, fmt.Sprintf("工具 %s 执行失败", toolName))
			} else {
				_, label := toolPhaseAndLabel(toolName, args)
				a.completeToolProcess(sessionID, processCardID, label)
			}
		}
	}

	// Update activity counts.
	a.store.UpdateRuntime(sessionID, func(rt *model.RuntimeState) {
		switch toolName {
		case "web_search":
			query, _ := args["query"].(string)
			rt.Activity.CurrentSearchKind = ""
			rt.Activity.CurrentSearchQuery = ""
			rt.Activity.CurrentWebSearchQuery = ""
			rt.Activity.SearchedWebQueries = append(rt.Activity.SearchedWebQueries, model.ActivityEntry{Query: query})
			rt.Activity.SearchCount = len(rt.Activity.SearchedWebQueries) + len(rt.Activity.SearchedContentQueries)
		case "open_page", "find_in_page":
			rt.Activity.CurrentReadingFile = ""
			rt.Activity.FilesViewed++
		case "list_files", "list_dir":
			rt.Activity.CurrentListingPath = ""
			rt.Activity.ListCount++
		case "read_file":
			rt.Activity.CurrentReadingFile = ""
			rt.Activity.FilesViewed++
		case "search_files":
			query, _ := args["query"].(string)
			rt.Activity.CurrentSearchKind = ""
			rt.Activity.CurrentSearchQuery = ""
			rt.Activity.SearchedContentQueries = append(rt.Activity.SearchedContentQueries, model.ActivityEntry{Query: query})
			rt.Activity.SearchCount = len(rt.Activity.SearchedWebQueries) + len(rt.Activity.SearchedContentQueries)
		case "execute_command", "readonly_host_inspect", "shell_command":
			rt.Activity.CommandsRun++
		case "write_file", "apply_patch":
			rt.Activity.FilesChanged++
		}
	})

	a.maybeCreateMCPResultCard(ctx, sessionID, toolName, processCardID, result, err)
	a.setRuntimeTurnPhase(sessionID, "thinking")
	a.broadcastSnapshot(sessionID)
}

func (a *App) emitBifrostToolCompletionEvent(ctx context.Context, sessionID, sessionKind, toolName string, args map[string]interface{}, processCardID, result string, execErr error, storedResult ToolExecutionResult, hasStoredResult bool) bool {
	if a == nil || a.toolEventBus == nil {
		return false
	}
	if sessionKind != model.SessionKindWorkspace && sessionKind != model.SessionKindWorker && !isSingleHostSessionKind(sessionKind) {
		return false
	}
	if !isBifrostLifecycleProjectionTool(toolName) {
		return false
	}

	invocationArgs := make(map[string]any, len(args))
	for key, value := range args {
		invocationArgs[key] = value
	}

	invocation := ToolInvocation{
		InvocationID: model.NewID("toolinv"),
		SessionID:    sessionID,
		ToolName:     toolName,
		ToolKind:     "bifrost",
		Source:       ToolInvocationSourceAgentloopToolCall,
		HostID:       defaultHostID(a.sessionHostID(sessionID)),
		Arguments:    invocationArgs,
		ReadOnly:     isBifrostLifecycleReadOnlyTool(toolName),
		StartedAt:    time.Now(),
	}
	_, label := toolPhaseAndLabel(toolName, args)

	var event ToolLifecycleEvent
	if execErr != nil {
		event = newToolFailedEvent(invocation, execErr)
		failureText := fmt.Sprintf("工具 %s 执行失败", toolName)
		if hasStoredResult && strings.TrimSpace(storedResult.ErrorText) != "" {
			failureText = strings.TrimSpace(storedResult.ErrorText)
		}
		event.Label = failureText
		event.Message = failureText
		event.Error = failureText
	} else {
		event = newToolCompletedEvent(invocation, ToolExecutionResult{
			InvocationID: invocation.InvocationID,
			Status:       ToolRunStatusCompleted,
			OutputText:   result,
			FinishedAt:   time.Now(),
		})
		event.Label = label
		event.Message = label
		if hasStoredResult && strings.TrimSpace(storedResult.LifecycleMessage) != "" {
			event.Label = strings.TrimSpace(storedResult.LifecycleMessage)
			event.Message = strings.TrimSpace(storedResult.LifecycleMessage)
		}
	}
	event.CardID = strings.TrimSpace(processCardID)
	if event.CardID == "" && !shouldBifrostLifecycleSkipCardProjection(toolName) && !isSingleHostSessionKind(sessionKind) {
		if raw, ok := a.bifrostToolCards.Load(sessionID + ":" + toolName); ok {
			if cardID, ok := raw.(string); ok {
				event.CardID = strings.TrimSpace(cardID)
			}
		}
	}
	if event.Payload == nil {
		event.Payload = make(map[string]any)
	}
	event.Payload["trackActivityCompletion"] = shouldBifrostLifecycleTrackActivityCompletion(toolName)
	event.Payload["skipCardProjection"] = shouldBifrostLifecycleSkipCardProjection(toolName) || isSingleHostSessionKind(sessionKind)
	event.Payload["arguments"] = cloneToolPayload(invocationArgs)
	if hasStoredResult {
		if outputData := cloneToolPayload(storedResult.OutputData); len(outputData) > 0 {
			event.Payload["outputData"] = outputData
		}
		for key, value := range cloneToolPayload(storedResult.ProjectionPayload) {
			if _, exists := event.Payload[key]; !exists {
				event.Payload[key] = value
			}
		}
		if override := strings.TrimSpace(getStringAny(storedResult.ProjectionPayload, "toolNameOverride")); override != "" {
			event.ToolName = override
		}
	}
	if result != "" {
		event.Payload["resultText"] = result
	}
	if err := a.toolEventBus.Emit(ctx, event); err != nil {
		log.Printf("[bifrost] failed to emit tool completion event session=%s tool=%s err=%v", sessionID, toolName, err)
	}
	return true
}

func isSingleHostSessionKind(kind string) bool {
	return strings.TrimSpace(kind) == "" || strings.TrimSpace(kind) == model.SessionKindSingleHost
}

// toolPhaseAndLabel maps a tool name to a UI phase and a human-readable label.
func toolPhaseAndLabel(toolName string, args map[string]interface{}) (phase, label string) {
	switch toolName {
	case "web_search":
		query, _ := args["query"].(string)
		return "searching", fmt.Sprintf("搜索网页：%s", truncateLabel(query, 60))
	case "open_page":
		url, _ := args["url"].(string)
		return "browsing", fmt.Sprintf("浏览网页：%s", truncateLabel(url, 60))
	case "find_in_page":
		query, _ := args["query"].(string)
		return "searching", fmt.Sprintf("在页面中搜索：%s", truncateLabel(query, 60))
	case "query_ai_server_state":
		focus := firstNonEmptyValue(getStringAny(args, "focus", "query", "topic"), "workspace")
		return "thinking", fmt.Sprintf("查询工作台状态：%s", truncateLabel(focus, 60))
	case "execute_command", "readonly_host_inspect":
		cmd, _ := args["command"].(string)
		return "executing", fmt.Sprintf("执行命令：%s", truncateLabel(cmd, 60))
	case "execute_readonly_query":
		cmd, _ := args["command"].(string)
		return "executing", fmt.Sprintf("执行只读命令：%s", truncateLabel(cmd, 60))
	case "list_remote_files":
		path, _ := args["path"].(string)
		return "browsing", fmt.Sprintf("现在列出 %s", truncateLabel(path, 60))
	case "read_remote_file":
		path, _ := args["path"].(string)
		return "browsing", fmt.Sprintf("现在浏览 %s", truncateLabel(path, 60))
	case "search_remote_files":
		query, _ := args["query"].(string)
		return "searching", fmt.Sprintf("现在搜索内容（%s）", truncateLabel(query, 60))
	case "list_files", "list_dir":
		path, _ := args["path"].(string)
		return "browsing", fmt.Sprintf("浏览目录：%s", truncateLabel(path, 60))
	case "read_file":
		path, _ := args["path"].(string)
		return "browsing", fmt.Sprintf("读取文件：%s", truncateLabel(path, 60))
	case "search_files":
		query, _ := args["query"].(string)
		return "searching", fmt.Sprintf("搜索文件：%s", truncateLabel(query, 60))
	case "write_file":
		path, _ := args["path"].(string)
		return "editing", fmt.Sprintf("写入文件：%s", truncateLabel(path, 60))
	case "apply_patch":
		return "editing", "应用代码补丁"
	case "ask_user_question":
		return "waiting_input", "等待用户输入"
	case "enter_plan_mode", "update_plan", "exit_plan_mode":
		return "planning", "规划中"
	default:
		return "executing", fmt.Sprintf("执行工具：%s", toolName)
	}
}

func shouldBifrostLifecycleTrackActivityCompletion(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "execute_readonly_query", "query_ai_server_state":
		return false
	default:
		return true
	}
}

// truncateLabel truncates a string to maxLen runes, appending "..." if needed.
func truncateLabel(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}
