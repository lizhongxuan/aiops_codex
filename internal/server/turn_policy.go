package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/agentloop"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

const (
	turnFinalGatePending = "pending"
	turnFinalGatePassed  = "passed"
	turnFinalGateBlocked = "blocked"
)

type workspaceTurnSignals struct {
	normalized                string
	intentClass               string
	lane                      string
	classificationReason      string
	knowledgeFreshness        string
	evidenceContract          string
	answerContract            string
	freshnessDeadline         string
	needsRealtimeData         bool
	requiresExternalFacts     bool
	needsPlanArtifact         bool
	needsApproval             bool
	needsAssumptions          bool
	needsDisambiguation       bool
	minimumEvidenceCount      int
	minimumIndependentSources int
	requireSourceAttribution  bool
	preferredAnswerStyle      string
	allowEarlyStop            bool
	requiredNextTool          string
	requiredTools             []string
	requiredEvidenceKinds     []string
	requiredCitationKinds     []string
	evidenceDiversityRules    []string
}

func (a *App) prepareWorkspaceTurnRuntime(_ context.Context, session *agentloop.Session, req chatRequest) {
	if a == nil || session == nil {
		return
	}
	sessionID := session.ID
	previous := a.snapshot(sessionID)
	previousLane := strings.TrimSpace(previous.CurrentLane)
	hostID := defaultHostID(a.workspaceDirectTargetHost(sessionID, req))
	policy := a.buildWorkspaceTurnPolicy(sessionID, hostID, req.Message)
	envelope := a.buildWorkspacePromptEnvelope(sessionID, hostID, req.Message, policy, true)
	visibleTools := a.workspaceVisibleToolNames(sessionID, policy)

	systemPrompt := agentloop.BuildSystemPrompt(agentloop.SessionSpec{
		Model:                 session.Model(),
		Cwd:                   a.cfg.DefaultWorkspace,
		DeveloperInstructions: renderPromptEnvelope(envelope),
		DynamicTools:          visibleTools,
		ApprovalPolicy:        a.mainAgentProfile().Runtime.ApprovalPolicy,
		SandboxMode:           a.mainAgentProfile().Runtime.SandboxMode,
	})
	session.ApplyTurnConfiguration(systemPrompt, visibleTools)
	if session.Metadata == nil {
		session.Metadata = make(map[string]string)
	}
	session.Metadata["session_kind"] = model.SessionKindWorkspace
	session.Metadata["turn_lane"] = strings.TrimSpace(policy.Lane)
	session.Metadata["turn_intent_class"] = strings.TrimSpace(policy.IntentClass)

	a.store.UpdateRuntime(sessionID, func(rt *model.RuntimeState) {
		rt.Turn.HostID = hostID
		rt.TurnPolicy = copyTurnPolicy(policy)
		rt.PromptEnvelope = copyPromptEnvelope(envelope)
		rt.PlanMode = strings.TrimSpace(policy.Lane) == string(model.TurnLanePlan)
	})

	switch strings.TrimSpace(policy.Lane) {
	case string(model.TurnLanePlan):
		a.setRuntimeTurnPhase(sessionID, "planning")
	case string(model.TurnLaneExecute):
		a.setRuntimeTurnPhase(sessionID, "executing")
	default:
		a.setRuntimeTurnPhase(sessionID, "thinking")
	}
	a.appendIncidentEvent(sessionID, "turn.policy.updated", "completed", "Turn policy updated", fmt.Sprintf("intent=%s lane=%s", policy.IntentClass, policy.Lane), map[string]any{
		"intentClass":         emptyToNil(strings.TrimSpace(policy.IntentClass)),
		"lane":                emptyToNil(strings.TrimSpace(policy.Lane)),
		"requiredTools":       append([]string(nil), policy.RequiredTools...),
		"requiredNextTool":    emptyToNil(strings.TrimSpace(policy.RequiredNextTool)),
		"finalGateStatus":     emptyToNil(strings.TrimSpace(policy.FinalGateStatus)),
		"missingRequirements": append([]string(nil), policy.MissingRequirements...),
	})
	if previousLane != "" && previousLane != strings.TrimSpace(policy.Lane) {
		a.appendIncidentEvent(sessionID, "turn.lane.changed", "completed", "Turn lane changed", fmt.Sprintf("%s -> %s", previousLane, policy.Lane), map[string]any{
			"fromLane": emptyToNil(previousLane),
			"toLane":   emptyToNil(strings.TrimSpace(policy.Lane)),
		})
	}
}

func (a *App) previewWorkspaceTurnPolicy(sessionID, hostID, message string, phase string) model.TurnPolicy {
	policy := a.buildWorkspaceTurnPolicy(sessionID, hostID, message)
	envelope := a.buildWorkspacePromptEnvelope(sessionID, hostID, message, policy, true)
	a.store.UpdateRuntime(sessionID, func(rt *model.RuntimeState) {
		rt.Turn.HostID = defaultHostID(hostID)
		rt.TurnPolicy = copyTurnPolicy(policy)
		rt.PromptEnvelope = copyPromptEnvelope(envelope)
		rt.PlanMode = strings.TrimSpace(policy.Lane) == string(model.TurnLanePlan)
	})
	if strings.TrimSpace(phase) != "" {
		a.setRuntimeTurnPhase(sessionID, phase)
	}
	return policy
}

func (a *App) buildWorkspaceTurnPolicy(sessionID, hostID, message string) model.TurnPolicy {
	signals := a.detectWorkspaceTurnSignals(sessionID, hostID, message)
	policy := model.TurnPolicy{
		IntentClass:               signals.intentClass,
		Lane:                      signals.lane,
		RequiredTools:             append([]string(nil), signals.requiredTools...),
		RequiredEvidenceKinds:     append([]string(nil), signals.requiredEvidenceKinds...),
		RequiredCitationKinds:     append([]string(nil), signals.requiredCitationKinds...),
		NeedsPlanArtifact:         signals.needsPlanArtifact,
		NeedsApproval:             signals.needsApproval,
		NeedsAssumptions:          signals.needsAssumptions,
		NeedsDisambiguation:       signals.needsDisambiguation,
		RequiresExternalFacts:     signals.requiresExternalFacts,
		RequiresRealtimeData:      signals.needsRealtimeData,
		MinimumEvidenceCount:      signals.minimumEvidenceCount,
		MinimumIndependentSources: signals.minimumIndependentSources,
		RequireSourceAttribution:  signals.requireSourceAttribution,
		PreferredAnswerStyle:      signals.preferredAnswerStyle,
		AllowEarlyStop:            signals.allowEarlyStop,
		KnowledgeFreshness:        signals.knowledgeFreshness,
		EvidenceContract:          signals.evidenceContract,
		AnswerContract:            signals.answerContract,
		FreshnessDeadline:         signals.freshnessDeadline,
		EvidenceDiversityRules:    append([]string(nil), signals.evidenceDiversityRules...),
		RequiredNextTool:          signals.requiredNextTool,
		FinalGateStatus:           turnFinalGatePending,
		ClassificationReason:      signals.classificationReason,
		UpdatedAt:                 model.NowString(),
	}
	if policy.RequiredNextTool != "" {
		policy.MissingRequirements = append(policy.MissingRequirements, requiredToolRequirement(policy.RequiredNextTool))
	}
	return policy
}

func (a *App) detectWorkspaceTurnSignals(sessionID, hostID, message string) workspaceTurnSignals {
	normalized := normalizeChoiceIntentText(message)
	signals := workspaceTurnSignals{
		normalized:           normalized,
		intentClass:          string(model.TurnIntentFactual),
		lane:                 string(model.TurnLaneAnswer),
		knowledgeFreshness:   "stable",
		evidenceContract:     "none",
		answerContract:       "normal",
		allowEarlyStop:       true,
		classificationReason: "默认按事实问答处理",
	}

	isAmbiguous := workspaceMessageNeedsIntentClarification(message)
	explicitExecution := containsExplicitExecutionAuthorization(message)
	planApproved := a.workspacePlanApproved(sessionID)
	hasFreshnessCue := containsFreshnessCue(normalized)
	asksForCanonicalResource := containsCanonicalResourceCue(normalized)
	asksForSourceAttribution := containsSourceAttributionCue(normalized)
	isExternalFactual := hasFreshnessCue || asksForCanonicalResource || asksForSourceAttribution
	isSourcedSnapshot := hasFreshnessCue && !asksForCanonicalResource && !asksForSourceAttribution
	isWorkspaceSnapshot := containsAnyToken(normalized,
		"在线主机", "哪些主机", "当前状态", "工作台状态", "待审批", "runtime", "phase", "告警状态",
		"summary", "hosts", "approvals", "plan status",
	)
	isResearch := containsAnyToken(normalized, "调研", "research", "compare", "比较", "对比", "benchmark", "survey")
	isDesign := containsAnyToken(normalized,
		"方案", "设计", "架构", "runbook", "sop", "排障思路", "排障方案", "修复计划", "升级计划",
		"怎么排查", "如何排查", "如何设计", "设计一个", "给我一个方案",
	)
	isImplementation := containsAnyToken(normalized, "实现", "修改代码", "patch", "refactor", "重构", "代码实现")
	isRiskyExec := containsAnyToken(normalized,
		"重启", "回滚", "执行修复", "开始修复", "部署", "发布", "切流", "扩容", "缩容",
		"restart", "rollback", "deploy", "apply", "migrate", "kill", "删除", "修改配置",
	)
	needsAssumptions := containsAnyToken(normalized, "窗口", "分钟", "回滚", "约束", "限制", "assumption", "假设")

	switch {
	case isAmbiguous:
		signals.intentClass = string(model.TurnIntentAmbiguous)
		signals.lane = string(model.TurnLaneAnswer)
		signals.needsDisambiguation = true
		signals.allowEarlyStop = false
		signals.requiredTools = []string{"ask_user_question"}
		signals.requiredNextTool = "ask_user_question"
		signals.classificationReason = "检测到高风险能力询问，必须先澄清意图"
	case explicitExecution && planApproved:
		signals.intentClass = string(model.TurnIntentRiskyExec)
		signals.lane = string(model.TurnLaneExecute)
		signals.needsApproval = true
		signals.evidenceContract = "execution_evidence"
		signals.answerContract = "verify"
		signals.allowEarlyStop = false
		signals.requiredTools = []string{"orchestrator_dispatch_tasks"}
		signals.requiredNextTool = "orchestrator_dispatch_tasks"
		signals.classificationReason = "已有批准计划且用户明确授权执行"
	case isDesign:
		signals.intentClass = string(model.TurnIntentDesign)
		signals.lane = string(model.TurnLanePlan)
		signals.needsPlanArtifact = true
		signals.needsAssumptions = needsAssumptions
		signals.answerContract = "plan"
		signals.preferredAnswerStyle = "plan"
		signals.allowEarlyStop = false
		signals.requiredTools = []string{"update_plan"}
		signals.requiredNextTool = "update_plan"
		signals.classificationReason = "检测到方案/设计类请求，直接进入 plan lane"
	case isRiskyExec:
		signals.intentClass = string(model.TurnIntentRiskyExec)
		signals.lane = string(model.TurnLanePlan)
		signals.needsPlanArtifact = true
		signals.needsApproval = true
		signals.answerContract = "plan"
		signals.preferredAnswerStyle = "plan"
		signals.allowEarlyStop = false
		signals.requiredTools = []string{"update_plan"}
		signals.requiredNextTool = "update_plan"
		signals.classificationReason = "检测到高风险执行请求，必须先出计划再审批"
	case isResearch:
		signals.intentClass = string(model.TurnIntentResearch)
		signals.lane = string(model.TurnLaneReadonly)
		signals.requiresExternalFacts = true
		signals.minimumEvidenceCount = 2
		signals.minimumIndependentSources = 2
		signals.requireSourceAttribution = true
		signals.knowledgeFreshness = "external"
		signals.evidenceContract = "external_facts"
		signals.answerContract = "sourced_facts"
		signals.requiredCitationKinds = []string{"url"}
		signals.evidenceDiversityRules = []string{"independent_sources"}
		signals.allowEarlyStop = false
		signals.requiredTools = []string{"web_search"}
		signals.requiredEvidenceKinds = []string{"web_search"}
		signals.requiredNextTool = "web_search"
		signals.classificationReason = "检测到调研/比较类请求，需要先搜集外部证据"
	case isWorkspaceSnapshot:
		signals.intentClass = string(model.TurnIntentSnapshot)
		signals.lane = string(model.TurnLaneReadonly)
		signals.evidenceContract = "execution_evidence"
		signals.answerContract = "sourced_facts"
		signals.allowEarlyStop = false
		signals.requiredTools = []string{"query_ai_server_state"}
		signals.requiredEvidenceKinds = []string{"ai_server_state"}
		signals.requiredNextTool = "query_ai_server_state"
		signals.classificationReason = "检测到工作台当前状态查询，优先读取 ai-server 状态"
	case isExternalFactual:
		signals.intentClass = string(model.TurnIntentFactual)
		signals.lane = string(model.TurnLaneReadonly)
		signals.requiresExternalFacts = true
		signals.minimumEvidenceCount = 2
		signals.minimumIndependentSources = 2
		signals.requireSourceAttribution = true
		signals.requiredCitationKinds = []string{"url"}
		signals.evidenceDiversityRules = []string{"independent_sources"}
		signals.allowEarlyStop = false
		if hasFreshnessCue {
			signals.freshnessDeadline = "now"
		}
		if isSourcedSnapshot {
			signals.needsRealtimeData = true
			signals.knowledgeFreshness = "realtime"
			signals.evidenceContract = "sourced_snapshot"
			signals.answerContract = "sourced_snapshot"
			signals.preferredAnswerStyle = "compact_snapshot"
		} else {
			signals.knowledgeFreshness = "external"
			signals.evidenceContract = "external_facts"
			signals.answerContract = "sourced_facts"
		}
		signals.requiredTools = []string{"web_search"}
		signals.requiredEvidenceKinds = []string{"web_search"}
		signals.requiredNextTool = "web_search"
		signals.classificationReason = "检测到需要外部最新或可验证事实的请求，必须先搜索证据"
	case isImplementation:
		signals.intentClass = string(model.TurnIntentImplementation)
		signals.lane = string(model.TurnLaneAnswer)
		signals.knowledgeFreshness = "stable"
		signals.evidenceContract = "none"
		signals.answerContract = "normal"
		signals.classificationReason = "检测到实现类请求，允许直答或先补上下文"
	default:
		signals.intentClass = string(model.TurnIntentFactual)
		signals.lane = string(model.TurnLaneAnswer)
		signals.knowledgeFreshness = "stable"
		signals.evidenceContract = "none"
		signals.answerContract = "normal"
		signals.classificationReason = "归类为普通事实问答"
	}

	return signals
}

func containsFreshnessCue(text string) bool {
	return containsAnyToken(text,
		"最新", "最近", "当前", "现在", "今日", "今天", "实时", "截至", "本周", "本月",
		"latest", "current", "today", "now", "up-to-date", "recent", "as of", "this week", "this month",
	)
}

func containsCanonicalResourceCue(text string) bool {
	return containsAnyToken(text,
		"官网", "官方", "文档", "documentation", "docs", "manual",
		"地址", "链接", "url", "link", "官方文档", "官方地址",
	)
}

func containsSourceAttributionCue(text string) bool {
	return containsAnyToken(text,
		"来源", "出处", "引用", "source", "sources", "citation", "cite",
	)
}

func containsAnyToken(text string, tokens ...string) bool {
	for _, token := range tokens {
		if token == "" {
			continue
		}
		if strings.Contains(text, normalizeChoiceIntentText(token)) {
			return true
		}
	}
	return false
}

func requiredToolRequirement(toolName string) string {
	switch strings.TrimSpace(toolName) {
	case "web_search":
		return "缺少外部实时证据"
	case "update_plan":
		return "缺少计划产物"
	case "ask_user_question":
		return "缺少实体澄清"
	case "query_ai_server_state":
		return "缺少工作台状态快照"
	case "orchestrator_dispatch_tasks":
		return "缺少执行派发动作"
	default:
		return "缺少必需工具调用"
	}
}

func (a *App) workspaceVisibleToolNames(sessionID string, policy model.TurnPolicy) []string {
	allTools := bifrostToolNamesFromDynamicTools(a.workspaceDynamicTools(sessionID))
	visible := make([]string, 0, len(allTools))
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || slices.Contains(visible, name) {
			return
		}
		if slices.Contains(allTools, name) {
			visible = append(visible, name)
		}
	}

	add("ask_user_question")
	switch strings.TrimSpace(policy.Lane) {
	case string(model.TurnLanePlan):
		for _, name := range []string{"query_ai_server_state", "readonly_host_inspect", "enter_plan_mode", "update_plan", "exit_plan_mode"} {
			add(name)
		}
	case string(model.TurnLaneExecute):
		for _, name := range []string{"query_ai_server_state", "readonly_host_inspect", "orchestrator_dispatch_tasks", "request_approval"} {
			add(name)
		}
	case string(model.TurnLaneVerify):
		for _, name := range []string{"query_ai_server_state", "readonly_host_inspect", "web_search", "open_page", "find_in_page"} {
			add(name)
		}
	case string(model.TurnLaneReadonly):
		for _, name := range []string{"query_ai_server_state", "readonly_host_inspect", "web_search", "open_page", "find_in_page"} {
			add(name)
		}
	default:
		for _, name := range []string{"query_ai_server_state", "readonly_host_inspect"} {
			add(name)
		}
	}

	selectedHostID := defaultHostID(a.sessionHostID(sessionID))
	if selectedHostID == "" {
		if session := a.store.Session(sessionID); session != nil {
			selectedHostID = defaultHostID(session.SelectedHostID)
		}
	}
	if isRemoteHostID(selectedHostID) {
		for _, name := range []string{"execute_readonly_query", "list_files", "read_file", "search_files"} {
			add(name)
		}
	}
	for _, name := range policy.RequiredTools {
		add(name)
	}
	return visible
}

func (a *App) workspaceTurnPolicyAllowsTool(sessionID, toolName string) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return true
	}
	policy := session.Runtime.TurnPolicy
	if strings.TrimSpace(policy.IntentClass) == "" {
		return true
	}
	visible := a.workspaceVisibleToolNames(sessionID, policy)
	for _, alias := range turnPolicyToolAliases(toolName) {
		if slices.Contains(visible, alias) {
			return true
		}
	}
	return false
}

func turnPolicyToolAliases(toolName string) []string {
	switch strings.TrimSpace(toolName) {
	case "execute_system_mutation":
		return []string{"execute_command", "write_file", "execute_system_mutation"}
	case shellCommandToolName:
		return []string{shellCommandToolName, "execute_readonly_query", "execute_command"}
	case "list_remote_files":
		return []string{"list_files", "list_remote_files"}
	case "read_remote_file":
		return []string{"read_file", "read_remote_file"}
	case "search_remote_files":
		return []string{"search_files", "search_remote_files"}
	default:
		name := strings.TrimSpace(toolName)
		if name == "" {
			return nil
		}
		return []string{name}
	}
}

func (a *App) buildWorkspacePromptEnvelope(sessionID, hostID, message string, policy model.TurnPolicy, turnScoped bool) *model.PromptEnvelope {
	staticSections := toEnvelopeSections(defaultPromptSections())
	dynamicSections := a.dynamicPromptSections(sessionID, hostID, policy)
	laneSections := []model.PromptEnvelopeSection{{
		Name:    "Lane",
		Content: a.workspaceLaneInstructions(policy),
	}}
	var runtimePolicy *model.PromptEnvelopeSection
	contextAttachments := make([]model.PromptEnvelopeSection, 0, len(dynamicSections)+4)
	for _, section := range dynamicSections {
		envelopeSection := toEnvelopeSection(section)
		switch envelopeSection.Name {
		case "RuntimePolicy":
			runtimePolicy = &envelopeSection
		default:
			contextAttachments = append(contextAttachments, envelopeSection)
		}
	}
	if runtimePolicy == nil {
		runtimePolicy = &model.PromptEnvelopeSection{
			Name: "RuntimePolicy",
			Content: strings.Join([]string{
				fmt.Sprintf("intentClass=%s", firstNonEmptyValue(strings.TrimSpace(policy.IntentClass), "factual")),
				fmt.Sprintf("lane=%s", firstNonEmptyValue(strings.TrimSpace(policy.Lane), "answer")),
				fmt.Sprintf("requiredTools=%s", firstNonEmptyValue(strings.Join(policy.RequiredTools, ", "), "-")),
				fmt.Sprintf("requiredEvidenceKinds=%s", firstNonEmptyValue(strings.Join(policy.RequiredEvidenceKinds, ", "), "-")),
				fmt.Sprintf("requiredCitationKinds=%s", firstNonEmptyValue(strings.Join(policy.RequiredCitationKinds, ", "), "-")),
				fmt.Sprintf("knowledgeFreshness=%s", firstNonEmptyValue(strings.TrimSpace(policy.KnowledgeFreshness), "stable")),
				fmt.Sprintf("evidenceContract=%s", firstNonEmptyValue(strings.TrimSpace(policy.EvidenceContract), "none")),
				fmt.Sprintf("answerContract=%s", firstNonEmptyValue(strings.TrimSpace(policy.AnswerContract), "normal")),
				fmt.Sprintf("finalGateStatus=%s", firstNonEmptyValue(strings.TrimSpace(policy.FinalGateStatus), turnFinalGatePending)),
			}, "\n"),
		}
	}
	contextAttachments = append(contextAttachments, a.workspaceContextAttachments(sessionID, message, policy)...)
	visibleTools, hiddenTools := a.workspacePromptToolViews(sessionID, policy)
	tokenEstimate := 0
	for _, section := range append(append(append([]model.PromptEnvelopeSection{}, staticSections...), laneSections...), contextAttachments...) {
		tokenEstimate += len([]rune(section.Content)) / 4
	}
	if runtimePolicy != nil {
		tokenEstimate += len([]rune(runtimePolicy.Content)) / 4
	}
	compressionState := workspacePromptCompressionState(tokenEstimate, contextAttachments)
	return &model.PromptEnvelope{
		StaticSections:      staticSections,
		LaneSections:        laneSections,
		RuntimePolicy:       runtimePolicy,
		ContextAttachments:  contextAttachments,
		VisibleTools:        visibleTools,
		HiddenTools:         hiddenTools,
		TokenEstimate:       tokenEstimate,
		CompressionState:    compressionState,
		CurrentLane:         strings.TrimSpace(policy.Lane),
		IntentClass:         strings.TrimSpace(policy.IntentClass),
		FinalGateStatus:     strings.TrimSpace(policy.FinalGateStatus),
		MissingRequirements: append([]string(nil), policy.MissingRequirements...),
		UpdatedAt:           model.NowString(),
	}
}

func toEnvelopeSection(section PromptSection) model.PromptEnvelopeSection {
	return model.PromptEnvelopeSection{
		Name:    strings.TrimSpace(section.Name),
		Content: strings.TrimSpace(section.Content),
	}
}

func toEnvelopeSections(sections []PromptSection) []model.PromptEnvelopeSection {
	out := make([]model.PromptEnvelopeSection, 0, len(sections))
	for _, section := range sections {
		if trimmed := strings.TrimSpace(section.Content); trimmed == "" {
			continue
		}
		out = append(out, toEnvelopeSection(section))
	}
	return out
}

func (a *App) workspaceLaneInstructions(policy model.TurnPolicy) string {
	switch strings.TrimSpace(policy.Lane) {
	case string(model.TurnLanePlan):
		return strings.TrimSpace(`
当前处于 plan lane。
- 先产出 update_plan 计划卡，再考虑 exit_plan_mode 提交计划审批。
- 允许只读取证和澄清，不允许直接执行变更或派发任务。
- 如果问题带有多约束，请把 assumptions 明确写进计划产物。`)
	case string(model.TurnLaneExecute):
		return strings.TrimSpace(`
当前处于 execute lane。
- 只允许在已批准的计划范围内执行。
- 优先使用 orchestrator_dispatch_tasks 或受控审批链路，不要越过审批边界。`)
	case string(model.TurnLaneReadonly):
		return strings.TrimSpace(`
当前处于 readonly lane。
- 只能先收集证据，再形成结论。
- 实时/外部事实问题必须先使用 web_search；工作台状态问题必须先使用 query_ai_server_state。`)
	case string(model.TurnLaneVerify):
		return strings.TrimSpace(`
当前处于 verify lane。
- 回答前先核对最新执行结果、验证结论和回滚提示。
- 如果验证未完成，不要宣称任务已成功。`)
	default:
		return strings.TrimSpace(`
当前处于 answer lane。
- 允许直接回答，但若本轮 policy 要求工具或澄清，则必须先满足这些前置条件。`)
	}
}

func (a *App) workspaceContextAttachments(sessionID, message string, policy model.TurnPolicy) []model.PromptEnvelopeSection {
	snapshot := a.snapshot(sessionID)
	attachments := []model.PromptEnvelopeSection{{
		Name: "PinnedContext",
		Content: fmt.Sprintf(
			"selectedHost=%s\ncurrentMode=%s\ncurrentStage=%s\ncurrentLane=%s\npendingApprovals=%d",
			defaultHostID(snapshot.SelectedHostID),
			snapshot.CurrentMode,
			snapshot.CurrentStage,
			firstNonEmptyValue(strings.TrimSpace(snapshot.CurrentLane), strings.TrimSpace(policy.Lane)),
			len(snapshot.Approvals),
		),
	}}
	if hook := a.workspaceLaneHookSection(snapshot, policy); hook != nil {
		attachments = append(attachments, *hook)
	}
	if rehydrate := a.workspaceLaneRehydrateSection(sessionID, snapshot, message, policy); rehydrate != nil {
		attachments = append(attachments, *rehydrate)
	}
	if latestPlan := a.latestPlanSummary(sessionID); latestPlan != "" {
		attachments = append(attachments, model.PromptEnvelopeSection{
			Name:    "PlanSummary",
			Content: latestPlan,
		})
	}
	if approvals := len(snapshot.Approvals); approvals > 0 {
		attachments = append(attachments, model.PromptEnvelopeSection{
			Name:    "PendingApprovals",
			Content: fmt.Sprintf("pendingApprovals=%d", approvals),
		})
	}
	if verification := a.latestVerificationSummary(sessionID); verification != "" {
		attachments = append(attachments, model.PromptEnvelopeSection{
			Name:    "Verification",
			Content: verification,
		})
	}
	if evidence := a.latestEvidenceSummary(sessionID); evidence != "" {
		attachments = append(attachments, model.PromptEnvelopeSection{
			Name:    "EvidenceSummary",
			Content: evidence,
		})
	}
	if strings.TrimSpace(message) != "" {
		attachments = append(attachments, model.PromptEnvelopeSection{
			Name:    "CurrentRequest",
			Content: strings.TrimSpace(message),
		})
	}
	if strings.TrimSpace(policy.RequiredNextTool) != "" {
		attachments = append(attachments, model.PromptEnvelopeSection{
			Name:    "RequiredNextTool",
			Content: strings.TrimSpace(policy.RequiredNextTool),
		})
	}
	return attachments
}

func workspacePromptCompressionState(tokenEstimate int, attachments []model.PromptEnvelopeSection) string {
	state := "inline"
	switch {
	case tokenEstimate > 12000:
		state = "summary_only"
	case len(attachments) > 6:
		state = "pinned_summary"
	}
	for _, section := range attachments {
		if strings.TrimSpace(section.Name) == "LaneRehydrate" {
			return state + "+rehydrated"
		}
	}
	return state
}

func (a *App) workspaceLaneHookSection(snapshot model.Snapshot, policy model.TurnPolicy) *model.PromptEnvelopeSection {
	switch strings.TrimSpace(policy.Lane) {
	case string(model.TurnLaneReadonly):
		lines := []string{
			fmt.Sprintf("selectedHost=%s", defaultHostID(snapshot.SelectedHostID)),
			fmt.Sprintf("pendingApprovals=%d", len(snapshot.Approvals)),
		}
		if evidence := a.latestEvidenceSummary(snapshot.SessionID); evidence != "" {
			lines = append(lines, "latestEvidence="+evidence)
		}
		return &model.PromptEnvelopeSection{
			Name:    "EnvSnapshotHook",
			Content: strings.Join(lines, "\n"),
		}
	case string(model.TurnLanePlan), string(model.TurnLaneExecute):
		lines := []string{
			"Any plan or execution answer must stay within approved scope and include risk, validation and rollback considerations.",
		}
		if assumptions, validation, rollback := a.latestPlanOperationalContext(snapshot.SessionID); assumptions != "" || validation != "" || rollback != "" {
			if assumptions != "" {
				lines = append(lines, "assumptions="+assumptions)
			}
			if validation != "" {
				lines = append(lines, "validation="+validation)
			}
			if rollback != "" {
				lines = append(lines, "rollback="+rollback)
			}
		}
		return &model.PromptEnvelopeSection{
			Name:    "OperationalConstraints",
			Content: strings.Join(lines, "\n"),
		}
	case string(model.TurnLaneVerify):
		if verification := a.latestVerificationSummary(snapshot.SessionID); verification != "" {
			return &model.PromptEnvelopeSection{
				Name:    "VerificationHook",
				Content: "latestVerification=" + verification,
			}
		}
	}
	return nil
}

func (a *App) workspaceLaneRehydrateSection(sessionID string, snapshot model.Snapshot, message string, policy model.TurnPolicy) *model.PromptEnvelopeSection {
	previousLane := firstNonEmptyValue(strings.TrimSpace(snapshot.CurrentLane), strings.TrimSpace(getTurnPolicyLane(snapshot.TurnPolicy)))
	nextLane := strings.TrimSpace(policy.Lane)
	if previousLane == "" || nextLane == "" || previousLane == nextLane {
		return nil
	}
	lines := []string{fmt.Sprintf("transition=%s->%s", previousLane, nextLane)}
	switch nextLane {
	case string(model.TurnLanePlan):
		lines = append(lines, "rehydrate=carry user goal, constraints and known evidence into planning lane")
		if strings.TrimSpace(message) != "" {
			lines = append(lines, "goal="+strings.TrimSpace(message))
		}
		if evidence := a.latestEvidenceSummary(sessionID); evidence != "" {
			lines = append(lines, "knownEvidence="+evidence)
		}
	case string(model.TurnLaneExecute):
		lines = append(lines, "rehydrate=carry approved plan, validation strategy and rollback hints into execute lane")
		if latestPlan := a.latestPlanSummary(sessionID); latestPlan != "" {
			lines = append(lines, "approvedPlan="+latestPlan)
		}
		if assumptions, validation, rollback := a.latestPlanOperationalContext(sessionID); assumptions != "" || validation != "" || rollback != "" {
			if assumptions != "" {
				lines = append(lines, "assumptions="+assumptions)
			}
			if validation != "" {
				lines = append(lines, "validation="+validation)
			}
			if rollback != "" {
				lines = append(lines, "rollback="+rollback)
			}
		}
	case string(model.TurnLaneVerify):
		lines = append(lines, "rehydrate=carry latest execution evidence, verification result and fallback hints into verify lane")
		if verification := a.latestVerificationSummary(sessionID); verification != "" {
			lines = append(lines, "verification="+verification)
		}
		if _, _, rollback := a.latestPlanOperationalContext(sessionID); rollback != "" {
			lines = append(lines, "rollback="+rollback)
		}
	default:
		return nil
	}
	if len(lines) <= 1 {
		return nil
	}
	return &model.PromptEnvelopeSection{
		Name:    "LaneRehydrate",
		Content: strings.Join(lines, "\n"),
	}
}

func (a *App) workspacePromptToolViews(sessionID string, policy model.TurnPolicy) ([]model.PromptEnvelopeTool, []model.PromptEnvelopeTool) {
	allTools := bifrostToolNamesFromDynamicTools(a.workspaceDynamicTools(sessionID))
	visibleNames := a.workspaceVisibleToolNames(sessionID, policy)
	visible := make([]model.PromptEnvelopeTool, 0, len(visibleNames))
	for _, name := range visibleNames {
		reason := "当前 lane 可见"
		if slices.Contains(policy.RequiredTools, name) {
			reason = "本轮 policy 必需工具"
		}
		desc := a.workspaceToolDescriptor(name)
		visible = append(visible, model.PromptEnvelopeTool{
			Name:        name,
			DisplayName: desc.DisplayName,
			Kind:        desc.Kind,
			Description: desc.Description,
			Aliases:     append([]string(nil), desc.Aliases...),
			Reason:      reason,
		})
	}
	hidden := make([]model.PromptEnvelopeTool, 0)
	for _, name := range allTools {
		if slices.Contains(visibleNames, name) {
			continue
		}
		desc := a.workspaceToolDescriptor(name)
		hidden = append(hidden, model.PromptEnvelopeTool{
			Name:        name,
			DisplayName: desc.DisplayName,
			Kind:        desc.Kind,
			Description: desc.Description,
			Aliases:     append([]string(nil), desc.Aliases...),
			Reason:      fmt.Sprintf("当前 lane=%s，工具未对模型暴露", firstNonEmptyValue(strings.TrimSpace(policy.Lane), "answer")),
		})
	}
	return visible, hidden
}

func renderPromptEnvelope(envelope *model.PromptEnvelope) string {
	if envelope == nil {
		return ""
	}
	sections := make([]PromptSection, 0, len(envelope.StaticSections)+len(envelope.LaneSections)+len(envelope.ContextAttachments)+1)
	for _, section := range envelope.StaticSections {
		sections = append(sections, PromptSection{Name: section.Name, Content: section.Content})
	}
	for _, section := range envelope.LaneSections {
		sections = append(sections, PromptSection{Name: section.Name, Content: section.Content})
	}
	if envelope.RuntimePolicy != nil {
		sections = append(sections, PromptSection{Name: envelope.RuntimePolicy.Name, Content: envelope.RuntimePolicy.Content})
	}
	for _, section := range envelope.ContextAttachments {
		sections = append(sections, PromptSection{Name: section.Name, Content: section.Content})
	}
	if len(envelope.VisibleTools) > 0 {
		lines := make([]string, 0, len(envelope.VisibleTools))
		for _, tool := range envelope.VisibleTools {
			lines = append(lines, fmt.Sprintf("- %s: %s", tool.Name, firstNonEmptyValue(strings.TrimSpace(tool.Reason), "可用工具")))
		}
		sections = append(sections, PromptSection{
			Name:    "VisibleTools",
			Content: strings.Join(lines, "\n"),
		})
	}
	if len(envelope.HiddenTools) > 0 {
		lines := make([]string, 0, len(envelope.HiddenTools))
		for _, tool := range envelope.HiddenTools {
			lines = append(lines, fmt.Sprintf("- %s: %s", tool.Name, firstNonEmptyValue(strings.TrimSpace(tool.Reason), "已隐藏")))
		}
		sections = append(sections, PromptSection{
			Name:    "HiddenTools",
			Content: strings.Join(lines, "\n"),
		})
	}
	return buildEffectivePrompt(sections)
}

func (a *App) latestPlanSummary(sessionID string) string {
	session := a.store.Session(sessionID)
	if session == nil {
		return ""
	}
	for i := len(session.Cards) - 1; i >= 0; i-- {
		card := session.Cards[i]
		if card.Type != "PlanCard" && card.Type != "PlanApprovalCard" {
			continue
		}
		return firstNonEmptyValue(strings.TrimSpace(card.Summary), strings.TrimSpace(card.Text), strings.TrimSpace(card.Title))
	}
	return ""
}

func (a *App) latestVerificationSummary(sessionID string) string {
	snapshot := a.snapshot(sessionID)
	for _, record := range snapshot.VerificationRecords {
		if summary := firstNonEmptyValue(strings.Join(record.Findings, " / "), strings.TrimSpace(record.RollbackHint)); summary != "" {
			return summary
		}
	}
	return ""
}

func (a *App) latestEvidenceSummary(sessionID string) string {
	snapshot := a.snapshot(sessionID)
	parts := make([]string, 0, 3)
	for _, evidence := range snapshot.EvidenceSummaries {
		summary := firstNonEmptyValue(strings.TrimSpace(evidence.Summary), strings.TrimSpace(evidence.Title))
		if summary == "" {
			continue
		}
		parts = append(parts, summary)
		if len(parts) >= 3 {
			break
		}
	}
	return strings.Join(parts, "\n")
}

func (a *App) latestPlanOperationalContext(sessionID string) (assumptions, validation, rollback string) {
	session := a.store.Session(sessionID)
	if session == nil {
		return "", "", ""
	}
	for i := len(session.Cards) - 1; i >= 0; i-- {
		card := session.Cards[i]
		if card.Type != "PlanCard" && card.Type != "PlanApprovalCard" {
			continue
		}
		return strings.TrimSpace(getStringAny(card.Detail, "assumptions")),
			strings.TrimSpace(getStringAny(card.Detail, "validation", "verify")),
			strings.TrimSpace(getStringAny(card.Detail, "rollback"))
	}
	return "", "", ""
}

func getTurnPolicyLane(policy *model.TurnPolicy) string {
	if policy == nil {
		return ""
	}
	return policy.Lane
}

func (a *App) workspacePlanApproved(sessionID string) bool {
	if a.workspacePlanApprovalPending(sessionID) || a.workspacePlanModeNeedsApproval(sessionID) {
		return false
	}
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}
	for _, approval := range session.Approvals {
		if strings.TrimSpace(approval.Type) != "plan_exit" {
			continue
		}
		switch strings.TrimSpace(approval.Status) {
		case "accept", "accepted", "accepted_for_session":
			return true
		}
	}
	return false
}

type externalEvidenceStats struct {
	SearchCount        int
	EvidenceCount      int
	IndependentSources map[string]struct{}
	URLCitations       map[string]struct{}
	DomainCitations    map[string]struct{}
	HasAttribution     bool
}

func newExternalEvidenceStats() externalEvidenceStats {
	return externalEvidenceStats{
		IndependentSources: make(map[string]struct{}),
		URLCitations:       make(map[string]struct{}),
		DomainCitations:    make(map[string]struct{}),
	}
}

func (s externalEvidenceStats) independentSourceCount() int {
	return len(s.IndependentSources)
}

func (s externalEvidenceStats) hasCitationKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "url", "page":
		return len(s.URLCitations) > 0
	case "domain":
		return len(s.DomainCitations) > 0
	default:
		return s.HasAttribution
	}
}

func collectExternalEvidenceStats(snapshot model.Snapshot) externalEvidenceStats {
	stats := newExternalEvidenceStats()
	stats.EvidenceCount = len(snapshot.EvidenceSummaries)
	if len(snapshot.Runtime.Activity.SearchedWebQueries) > 0 {
		stats.SearchCount = len(snapshot.Runtime.Activity.SearchedWebQueries)
	}
	for _, evidence := range snapshot.EvidenceSummaries {
		if strings.TrimSpace(evidence.CitationKey) != "" || strings.TrimSpace(evidence.SourceRef) != "" {
			stats.HasAttribution = true
		}
		if source := normalizeIndependentSource(evidence.SourceRef); source != "" {
			stats.IndependentSources[source] = struct{}{}
		}
		for _, citation := range extractCitationRefs(evidence.SourceRef) {
			recordCitation(&stats, citation)
		}
	}
	for _, invocation := range snapshot.ToolInvocations {
		if strings.TrimSpace(invocation.Status) == "failed" {
			continue
		}
		switch strings.TrimSpace(invocation.Name) {
		case "web_search":
			if stats.SearchCount == 0 {
				stats.SearchCount = 1
			}
			for _, citation := range extractWebSearchCitations(invocation) {
				recordCitation(&stats, citation)
			}
		case "open_page", "find_in_page":
			for _, citation := range extractInvocationURLCitations(invocation) {
				recordCitation(&stats, citation)
			}
		}
	}
	if sourceCount := len(stats.IndependentSources); sourceCount > stats.EvidenceCount {
		stats.EvidenceCount = sourceCount
	}
	return stats
}

func recordCitation(stats *externalEvidenceStats, raw string) {
	normalizedSource := normalizeIndependentSource(raw)
	if normalizedSource != "" {
		stats.IndependentSources[normalizedSource] = struct{}{}
		stats.HasAttribution = true
	}
	normalizedURL := normalizeCitationURL(raw)
	if normalizedURL != "" {
		stats.URLCitations[normalizedURL] = struct{}{}
		stats.HasAttribution = true
	}
	if domain := normalizeCitationDomain(raw); domain != "" {
		stats.DomainCitations[domain] = struct{}{}
		stats.HasAttribution = true
	}
}

func normalizeIndependentSource(raw string) string {
	return normalizeCitationDomain(raw)
}

func normalizeCitationURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	u, err := parseCitationURL(trimmed)
	if err != nil || strings.TrimSpace(u.Host) == "" {
		return ""
	}
	u.Fragment = ""
	return strings.ToLower(u.String())
}

func normalizeCitationDomain(raw string) string {
	u, err := parseCitationURL(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	host = strings.TrimPrefix(host, "www.")
	return host
}

func parseCitationURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("empty url")
	}
	if strings.Contains(trimmed, "://") {
		return url.Parse(trimmed)
	}
	if strings.Contains(trimmed, ".") && !strings.ContainsAny(trimmed, " \n\t") {
		return url.Parse("https://" + trimmed)
	}
	return nil, fmt.Errorf("not a url")
}

func extractCitationRefs(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	return []string{trimmed}
}

func extractWebSearchCitations(invocation model.ToolInvocation) []string {
	return extractCitationRefsFromPayload(invocation.OutputJSON)
}

func extractInvocationURLCitations(invocation model.ToolInvocation) []string {
	return extractCitationRefsFromPayload(invocation.InputJSON)
}

func extractCitationRefsFromPayload(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	var payload any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil
	}
	return collectCitationRefsFromAny(payload)
}

func collectCitationRefsFromAny(value any) []string {
	out := make([]string, 0)
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range []string{"url", "sourceRef"} {
			if raw := strings.TrimSpace(getStringAny(typed, key)); raw != "" {
				out = append(out, raw)
			}
		}
		for _, key := range []string{"output", "content", "raw"} {
			switch nested := typed[key].(type) {
			case string:
				out = append(out, extractCitationRefsFromPayload(nested)...)
			default:
				out = append(out, collectCitationRefsFromAny(nested)...)
			}
		}
		for _, key := range []string{"results", "items", "matches", "sources", "entries"} {
			out = append(out, collectCitationRefsFromAny(typed[key])...)
		}
	case []any:
		for _, item := range typed {
			out = append(out, collectCitationRefsFromAny(item)...)
		}
	case string:
		if normalized := normalizeCitationURL(typed); normalized != "" {
			out = append(out, typed)
		}
	}
	return out
}

func (a *App) ValidateTurnCompletion(_ context.Context, session *agentloop.Session, _ string, _ string) agentloop.TurnCompletionDecision {
	if a == nil || session == nil || a.sessionKind(session.ID) != model.SessionKindWorkspace {
		return agentloop.TurnCompletionDecision{Action: "pass"}
	}
	snapshot := a.snapshot(session.ID)
	policy := snapshot.TurnPolicy
	if policy == nil || strings.TrimSpace(policy.IntentClass) == "" {
		return agentloop.TurnCompletionDecision{Action: "pass"}
	}

	missing := make([]string, 0)
	requiredNextTool := strings.TrimSpace(policy.RequiredNextTool)
	evidenceStats := collectExternalEvidenceStats(snapshot)

	if policy.NeedsDisambiguation && !a.hasCompletedChoiceAfterLatestUser(session.ID) {
		missing = append(missing, "缺少实体澄清")
		requiredNextTool = firstNonEmptyValue(requiredNextTool, "ask_user_question")
	}
	if policy.NeedsPlanArtifact && !a.workspaceHasUpdatedPlan(session.ID) {
		missing = append(missing, "缺少计划产物")
		requiredNextTool = firstNonEmptyValue(requiredNextTool, "update_plan")
	}
	if policy.NeedsAssumptions && !a.workspaceHasPlanAssumptions(session.ID) {
		missing = append(missing, "缺少 assumptions")
		requiredNextTool = firstNonEmptyValue(requiredNextTool, "update_plan")
	}
	if policy.NeedsApproval && strings.TrimSpace(policy.Lane) == string(model.TurnLaneExecute) && !a.workspacePlanApproved(session.ID) {
		missing = append(missing, "缺少已审批计划")
	}
	if policy.RequiresRealtimeData || policy.RequiresExternalFacts || strings.TrimSpace(policy.KnowledgeFreshness) == "external" || strings.TrimSpace(policy.KnowledgeFreshness) == "realtime" || strings.TrimSpace(policy.EvidenceContract) == "external_facts" || strings.TrimSpace(policy.EvidenceContract) == "sourced_snapshot" {
		if evidenceStats.SearchCount == 0 {
			missing = append(missing, "缺少外部实时证据")
			requiredNextTool = firstNonEmptyValue(requiredNextTool, "web_search")
		}
	}
	if policy.MinimumEvidenceCount > 0 && evidenceStats.EvidenceCount < policy.MinimumEvidenceCount {
		missing = append(missing, "证据数量不足")
		requiredNextTool = firstNonEmptyValue(requiredNextTool, "web_search")
	}
	if policy.MinimumIndependentSources > 0 && evidenceStats.independentSourceCount() < policy.MinimumIndependentSources {
		missing = append(missing, "缺少独立来源")
		requiredNextTool = firstNonEmptyValue(requiredNextTool, "web_search")
	}
	if policy.RequireSourceAttribution && !evidenceStats.HasAttribution {
		missing = append(missing, "缺少来源归因")
		requiredNextTool = firstNonEmptyValue(requiredNextTool, "web_search")
	}
	for _, kind := range policy.RequiredCitationKinds {
		if evidenceStats.hasCitationKind(kind) {
			continue
		}
		missing = append(missing, fmt.Sprintf("缺少%s归因", strings.TrimSpace(kind)))
		requiredNextTool = firstNonEmptyValue(requiredNextTool, "web_search")
	}
	if len(missing) == 0 && len(policy.RequiredTools) > 0 {
		for _, toolName := range policy.RequiredTools {
			if a.snapshotHasToolEvidence(snapshot, toolName) {
				continue
			}
			missing = append(missing, requiredToolRequirement(toolName))
			requiredNextTool = firstNonEmptyValue(requiredNextTool, toolName)
			break
		}
	}

	status := turnFinalGatePassed
	action := "pass"
	repair := ""
	if len(missing) > 0 {
		status = turnFinalGateBlocked
		action = "continue"
		repair = a.turnCompletionRepairMessage(*policy, missing, requiredNextTool)
	}
	a.store.UpdateRuntime(session.ID, func(rt *model.RuntimeState) {
		rt.TurnPolicy.FinalGateStatus = status
		rt.TurnPolicy.RequiredNextTool = requiredNextTool
		rt.TurnPolicy.MissingRequirements = append([]string(nil), missing...)
		if rt.PromptEnvelope != nil {
			rt.PromptEnvelope.FinalGateStatus = status
			rt.PromptEnvelope.MissingRequirements = append([]string(nil), missing...)
			rt.PromptEnvelope.CurrentLane = rt.TurnPolicy.Lane
			rt.PromptEnvelope.IntentClass = rt.TurnPolicy.IntentClass
		}
	})
	if len(missing) > 0 {
		a.appendIncidentEvent(session.ID, "turn.final_gate.blocked", "warning", "Final answer gate blocked", strings.Join(missing, " / "), map[string]any{
			"requiredNextTool":    emptyToNil(requiredNextTool),
			"missingRequirements": append([]string(nil), missing...),
			"intentClass":         emptyToNil(strings.TrimSpace(policy.IntentClass)),
			"lane":                emptyToNil(strings.TrimSpace(policy.Lane)),
		})
	} else {
		a.appendIncidentEvent(session.ID, "turn.final_gate.passed", "completed", "Final answer gate passed", fmt.Sprintf("intent=%s lane=%s", policy.IntentClass, policy.Lane), nil)
	}
	return agentloop.TurnCompletionDecision{
		Action:        action,
		RepairMessage: repair,
	}
}

func (a *App) turnCompletionRepairMessage(policy model.TurnPolicy, missing []string, requiredNextTool string) string {
	lines := []string{
		fmt.Sprintf("Runtime final-answer gate blocked this turn. intentClass=%s lane=%s", policy.IntentClass, policy.Lane),
		fmt.Sprintf("missingRequirements=%s", strings.Join(missing, ", ")),
	}
	if requiredNextTool != "" {
		lines = append(lines, fmt.Sprintf("next_required_tool=%s", requiredNextTool))
		lines = append(lines, fmt.Sprintf("Call %s next. Do not produce a final plain-text answer before the missing requirements are satisfied.", requiredNextTool))
	} else {
		lines = append(lines, "Do not produce a final plain-text answer before the missing requirements are satisfied.")
	}
	return strings.Join(lines, "\n")
}

func (a *App) workspaceHasPlanAssumptions(sessionID string) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}
	for i := len(session.Cards) - 1; i >= 0; i-- {
		card := session.Cards[i]
		if card.Type != "PlanCard" && card.Type != "PlanApprovalCard" {
			continue
		}
		if strings.TrimSpace(getStringAny(card.Detail, "assumptions")) != "" {
			return true
		}
		if assumptions, ok := card.Detail["assumptions"].([]any); ok && len(assumptions) > 0 {
			return true
		}
	}
	return false
}

func (a *App) snapshotHasToolEvidence(snapshot model.Snapshot, toolName string) bool {
	aliases := turnPolicyToolAliases(toolName)
	for _, invocation := range snapshot.ToolInvocations {
		if !slices.Contains(aliases, strings.TrimSpace(invocation.Name)) {
			continue
		}
		if strings.TrimSpace(invocation.Status) == "failed" {
			continue
		}
		return true
	}
	if strings.TrimSpace(toolName) == "web_search" && len(snapshot.Runtime.Activity.SearchedWebQueries) > 0 {
		return true
	}
	if strings.TrimSpace(toolName) == "query_ai_server_state" {
		for _, evidence := range snapshot.EvidenceSummaries {
			if strings.TrimSpace(evidence.Kind) == "query_ai_server_state" || strings.TrimSpace(evidence.SourceKind) == "state_snapshot" {
				return true
			}
		}
	}
	return false
}

func copyTurnPolicy(policy model.TurnPolicy) model.TurnPolicy {
	out := policy
	out.RequiredTools = append([]string(nil), policy.RequiredTools...)
	out.RequiredEvidenceKinds = append([]string(nil), policy.RequiredEvidenceKinds...)
	out.RequiredCitationKinds = append([]string(nil), policy.RequiredCitationKinds...)
	out.EvidenceDiversityRules = append([]string(nil), policy.EvidenceDiversityRules...)
	out.MissingRequirements = append([]string(nil), policy.MissingRequirements...)
	return out
}

func copyPromptEnvelope(envelope *model.PromptEnvelope) *model.PromptEnvelope {
	if envelope == nil {
		return nil
	}
	out := *envelope
	out.StaticSections = append([]model.PromptEnvelopeSection(nil), envelope.StaticSections...)
	out.LaneSections = append([]model.PromptEnvelopeSection(nil), envelope.LaneSections...)
	out.ContextAttachments = append([]model.PromptEnvelopeSection(nil), envelope.ContextAttachments...)
	if envelope.RuntimePolicy != nil {
		runtimePolicy := *envelope.RuntimePolicy
		out.RuntimePolicy = &runtimePolicy
	}
	out.VisibleTools = append([]model.PromptEnvelopeTool(nil), envelope.VisibleTools...)
	out.HiddenTools = append([]model.PromptEnvelopeTool(nil), envelope.HiddenTools...)
	out.MissingRequirements = append([]string(nil), envelope.MissingRequirements...)
	return &out
}
