package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gorilla/websocket"
	"github.com/lizhongxuan/aiops-codex/internal/agentloop"
	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/coroot"
	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/orchestrator"
	"github.com/lizhongxuan/aiops-codex/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/peer"
)

const (
	stalledTurnTimeout                   = 45 * time.Second
	silentTurnCompletionDelay            = 1200 * time.Millisecond
	autoThreadResetIdleThreshold         = 4 * time.Hour
	autoThreadResetCardThreshold         = 80
	autoThreadResetConversationThreshold = 24
	autoThreadResetShortPromptRuneLimit  = 32
	terminalSubscriberGraceTTL           = 20 * time.Second
	terminalConnectTimeout               = 15 * time.Second
	terminalExitRetention                = 30 * time.Second
)

var contextualFollowupPattern = regexp.MustCompile(`(?i)(继续|刚才|上面|前面|前文|上一|这个|那个|第\s*\d+\s*步|same|above|previous|continue|earlier|step\s*\d+)`)

type App struct {
	cfg                    config.Config
	store                  *store.Store
	agentLoop              *agentloop.Loop
	bifrostGateway         *bifrost.Gateway
	bifrostMu              sync.Mutex
	bifrostSessions        map[string]*agentloop.Session
	workspaceRuntimes      map[string]*agentloop.WorkspaceRuntime
	orchestrator           *orchestrator.Manager
	runtimeStartThreadFunc func(context.Context, string, threadStartSpec) (string, error)
	runtimeStartTurnFunc   func(context.Context, string, string, turnStartSpec) (string, error)
	codexRespondFunc       func(context.Context, string, any) error
	skillDiscoveryFunc     discoverInstalledSkillsFunc
	upgrader               websocket.Upgrader
	agentMu                sync.Mutex
	agents                 map[string]*agentConnection
	wsMu                   sync.Mutex
	wsClients              map[string]map[*websocket.Conn]struct{}
	turnMu                 sync.Mutex
	turnCancels            map[string]context.CancelFunc
	terminalMu             sync.Mutex
	terminals              map[string]*terminalSession
	execMu                 sync.Mutex
	execs                  map[string]*remoteExecSession
	fileReqMu              sync.Mutex
	fileReqs               map[string]*agentResponseWaiter
	approvalMu             sync.Mutex
	fileChangeClaims       map[string]struct{}
	oauthMu                sync.Mutex
	oauthStates            map[string]string
	auditMu                sync.Mutex
	turnTraceMu            sync.Mutex
	turnTraces             map[string]*turnTrace
	orchestratorMu         sync.Mutex
	orchestratorJobs       map[string]string
	broadcastThrotMu       sync.Mutex
	broadcastTimers        map[string]*time.Timer
	threadStarts           []time.Time
	turnStarts             []time.Time
	approvalAuditStore     *store.ApprovalAuditStore
	approvalGrantStore     *store.ApprovalGrantStore
	capabilityBindingStore *store.CapabilityBindingStore
	uiCardStore            *store.UICardStore
	scriptConfigStore      *store.ScriptConfigStore
	labEnvironmentStore    *store.LabEnvironmentStore
	corootClient           *coroot.Client
	dataSourceRouter       *coroot.DataSourceRouter
	rcaEngine              coroot.RCAEngine
	mcpManager             mcpRuntime
	mcpToolBindings        map[string]mcpToolBinding
	httpServer             *http.Server
	grpcServer             *grpc.Server
	commandRunner          commandRunner
	bifrostToolCards       sync.Map // tracks active ProcessLineCard IDs per tool call (key: sessionID+":"+toolName)
}

type authLoginRequest struct {
	Mode             string `json:"mode"`
	APIKey           string `json:"apiKey"`
	AccessToken      string `json:"accessToken"`
	ChatGPTAccountID string `json:"chatgptAccountId"`
	ChatGPTPlanType  string `json:"chatgptPlanType"`
	Email            string `json:"email"`
}

type chatRequest struct {
	Message        string                `json:"message"`
	HostID         string                `json:"hostId"`
	MonitorContext *model.MonitorContext `json:"monitorContext,omitempty"`
}

type turnTrace struct {
	RequestID            string
	SessionID            string
	Kind                 string
	HostID               string
	RequestStartedAt     time.Time
	RuntimeStartedAt     time.Time
	ThreadStartBeganAt   time.Time
	ThreadStartedAt      time.Time
	ThreadID             string
	TurnStartBeganAt     time.Time
	TurnStartedAt        time.Time
	TurnID               string
	FirstItemStartedAt   time.Time
	FirstItemID          string
	FirstItemType        string
	FirstAssistantAt     time.Time
	FirstAssistantItemID string
	FirstAssistantSource string
}

type hostSelectionRequest struct {
	HostID string `json:"hostId"`
}

type approvalDecisionRequest struct {
	Decision string `json:"decision"`
}

type choiceAnswerInput struct {
	Value   string `json:"value"`
	Label   string `json:"label,omitempty"`
	IsOther bool   `json:"isOther,omitempty"`
	Note    string `json:"note,omitempty"`
}

type requiredToolFollowup struct {
	SessionID    string
	ChoiceCardID string
	Tool         string
	HostID       string
	Message      string
}

type choiceAnswerRequest struct {
	Answers []choiceAnswerInput `json:"answers"`
}

func (a *App) answerPendingChoiceFromChatMessage(w http.ResponseWriter, r *http.Request, sessionID, message string) bool {
	choiceID, choice, ok := a.latestPendingChoiceForSession(sessionID)
	if !ok {
		return false
	}
	if len(choice.Questions) != 1 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "当前有多个待回答问题，请先在澄清卡片中完成选择"})
		return true
	}
	answers := buildChoiceAnswersFromText(choice.Questions, message)
	if len(answers) == 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := a.submitChoiceAnswer(ctx, sessionID, choiceID, choice, answers); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"answeredChoice": true,
	})
	return true
}

func (a *App) latestPendingChoiceForSession(sessionID string) (string, model.ChoiceRequest, bool) {
	session := a.store.Session(sessionID)
	if session == nil {
		return "", model.ChoiceRequest{}, false
	}
	for i := len(session.Cards) - 1; i >= 0; i-- {
		card := session.Cards[i]
		if card.Type != "ChoiceCard" || strings.TrimSpace(card.Status) != "pending" {
			continue
		}
		choiceID := firstNonEmptyValue(strings.TrimSpace(card.RequestID), strings.TrimSpace(card.ID))
		if choiceID == "" {
			continue
		}
		_, choice, ok := a.resolveChoiceTargetSession(sessionID, choiceID)
		if ok && strings.TrimSpace(choice.Status) == "pending" {
			return choiceID, choice, true
		}
	}
	return "", model.ChoiceRequest{}, false
}

func buildChoiceAnswersFromText(questions []model.ChoiceQuestion, message string) []choiceAnswerInput {
	text := strings.TrimSpace(message)
	if text == "" || len(questions) == 0 {
		return nil
	}
	answers := make([]choiceAnswerInput, 0, len(questions))
	for _, question := range questions {
		answer := choiceAnswerInput{Value: text, Label: text, IsOther: true}
		for _, option := range question.Options {
			value := strings.TrimSpace(option.Value)
			label := strings.TrimSpace(option.Label)
			if strings.EqualFold(text, value) || strings.EqualFold(text, label) {
				answer = choiceAnswerInput{
					Value: firstNonEmptyValue(value, label),
					Label: firstNonEmptyValue(label, value),
				}
				break
			}
		}
		answers = append(answers, answer)
	}
	return answers
}

func choiceFollowUpPayload(questions []model.ChoiceQuestion, answers []choiceAnswerInput, codexAnswers []map[string]any) map[string]any {
	answerSummary := choiceAnswerSummary(questions, answers)
	payload := map[string]any{
		"answers":        codexAnswers,
		"answer_summary": answerSummary,
		"instruction":    "Use this user clarification to continue. Do not ask the same clarification question again.",
	}
	if choiceAnswersRequirePlanMode(answers) {
		payload["next_required_tool"] = "enter_plan_mode"
		payload["permission_scope"] = "planning_only"
		payload["intent"] = "enter_plan_mode_required"
		payload["instruction"] = "The user explicitly selected a repair or execution-planning path. This authorizes planning only, not mutation. Your next assistant action MUST be a tool call to enter_plan_mode. Do not answer in plain text, do not ask the same intent question again, and do not call orchestrator_dispatch_tasks until exit_plan_mode is approved."
		return payload
	}
	if choiceAnswersRequestReadonly(answers) {
		payload["next_required_tool"] = "readonly_host_inspect"
		payload["permission_scope"] = "readonly_only"
		payload["intent"] = "readonly_requested"
		payload["instruction"] = "The user selected a readonly diagnosis path. Your next assistant action MUST be a tool call to readonly_host_inspect on the selected host. Continue with readonly inspection only, do not mutate state, do not dispatch workers, and do not ask the same intent question again."
	}
	return payload
}

func choiceAnswersRequirePlanMode(answers []choiceAnswerInput) bool {
	for _, answer := range answers {
		if choiceAnswerTextRequiresPlanMode(answer.Value) || choiceAnswerTextRequiresPlanMode(answer.Label) || choiceAnswerTextRequiresPlanMode(answer.Note) {
			return true
		}
	}
	return false
}

func choiceAnswerInputsFromModel(answers []model.ChoiceAnswer) []choiceAnswerInput {
	items := make([]choiceAnswerInput, 0, len(answers))
	for _, answer := range answers {
		items = append(items, choiceAnswerInput{
			Value:   answer.Value,
			Label:   answer.Label,
			IsOther: answer.IsOther,
			Note:    answer.Note,
		})
	}
	return items
}

func choiceAnswerTextRequiresPlanMode(value string) bool {
	normalized := normalizeChoiceIntentText(value)
	if normalized == "" {
		return false
	}
	for _, token := range []string{
		"repair_plan",
		"execute_repair",
		"execution_plan",
		"proceed_to_plan",
		"plan_execute",
		"mutation_plan",
	} {
		if strings.Contains(normalized, token) {
			return true
		}
	}
	for _, phrase := range []string{
		"准备修复",
		"执行修复",
		"开始修复",
		"修复计划",
		"正式计划流程",
		"审批后再执行",
		"按计划执行",
		"进入计划模式",
	} {
		if strings.Contains(normalized, phrase) {
			return true
		}
	}
	return false
}

func choiceAnswersRequestReadonly(answers []choiceAnswerInput) bool {
	for _, answer := range answers {
		normalized := normalizeChoiceIntentText(firstNonEmptyValue(answer.Value, answer.Label, answer.Note))
		if strings.Contains(normalized, "readonly") ||
			strings.Contains(normalized, "read_only") ||
			strings.Contains(normalized, "只读") {
			return true
		}
	}
	return false
}

func workspaceMessageNeedsIntentClarification(message string) bool {
	normalized := normalizeChoiceIntentText(message)
	if normalized == "" {
		return false
	}
	hasCapabilityPhrase := false
	for _, phrase := range []string{
		"有办法",
		"有没有办法",
		"能不能",
		"能否",
		"可以吗",
		"会不会",
		"是否能",
		"can you",
		"do you have a way",
	} {
		if strings.Contains(normalized, normalizeChoiceIntentText(phrase)) {
			hasCapabilityPhrase = true
			break
		}
	}
	if !hasCapabilityPhrase {
		return false
	}
	highRiskTopic := false
	for _, token := range []string{
		"pg",
		"postgres",
		"postgresql",
		"数据库",
		"同步",
		"复制",
		"replication",
		"部署",
		"生产",
		"恢复",
		"重启",
		"修复",
	} {
		if strings.Contains(normalized, normalizeChoiceIntentText(token)) {
			highRiskTopic = true
			break
		}
	}
	if !highRiskTopic {
		return false
	}
	for _, explicit := range []string{
		"先只读",
		"只读检查",
		"只读诊断",
		"不要修改",
		"开始诊断",
		"开始检查",
		"开始修复",
		"帮我修复",
		"直接修复",
		"制定计划",
		"生成计划",
		"先规划",
	} {
		if strings.Contains(normalized, normalizeChoiceIntentText(explicit)) {
			return false
		}
	}
	return true
}

func workspaceIntentClarificationQuestions(message string) []model.ChoiceQuestion {
	return []model.ChoiceQuestion{{
		Header:   "确认意图",
		Question: "可以处理，但数据库同步/修复属于高风险操作。你希望我现在按哪种方式继续？",
		Options: []model.ChoiceOption{
			{
				Label:       "只读诊断",
				Value:       "readonly",
				Description: "先检查当前选中主机的 PostgreSQL 同步状态、日志和复制线索，不做任何修改。",
				Recommended: true,
			},
			{
				Label:       "只问能力",
				Value:       "answer_only",
				Description: "只回答是否能处理，不访问主机、不执行命令、不派发任务。",
			},
			{
				Label:       "准备修复",
				Value:       "repair_plan",
				Description: "进入正式计划流程，列出风险、回滚和验证，待你审批后再执行任何变更。",
			},
		},
		IsOther: true,
	}}
}

func (a *App) requiredToolFollowupAfterTurn(sessionID string) (requiredToolFollowup, bool) {
	if a == nil || a.sessionKind(sessionID) != model.SessionKindWorkspace {
		return requiredToolFollowup{}, false
	}
	session := a.store.Session(sessionID)
	if session == nil {
		return requiredToolFollowup{}, false
	}
	cards := session.Cards
	for i := len(cards) - 1; i >= 0; i-- {
		card := cards[i]
		if card.Type == "UserMessageCard" || (card.Type == "MessageCard" && card.Role == "user") {
			return requiredToolFollowup{}, false
		}
		if card.Type != "ChoiceCard" || normalizeCardStatus(card.Status) != "completed" {
			continue
		}
		if getBool(card.Detail, "requiredToolFollowupStarted") {
			return requiredToolFollowup{}, false
		}
		choice, ok := a.store.Choice(sessionID, card.RequestID)
		if !ok {
			return requiredToolFollowup{}, false
		}
		payload := choiceFollowUpPayload(card.Questions, choiceAnswerInputsFromModel(choice.Answers), choiceAnswerMaps(choice.Answers))
		requiredTool := strings.TrimSpace(getStringAny(payload, "next_required_tool", "required_next_tool"))
		if requiredTool == "" {
			return requiredToolFollowup{}, false
		}
		if requiredToolObservedInCards(cards[i+1:], requiredTool) {
			return requiredToolFollowup{}, false
		}
		hostID := defaultHostID(session.SelectedHostID)
		if hostID == "" {
			hostID = model.ServerLocalHostID
		}
		return requiredToolFollowup{
			SessionID:    sessionID,
			ChoiceCardID: card.ID,
			Tool:         requiredTool,
			HostID:       hostID,
			Message:      requiredToolFollowupMessage(requiredTool, hostID, a.latestCompletedUserText(sessionID)),
		}, true
	}
	return requiredToolFollowup{}, false
}

func requiredToolObservedInCards(cards []model.Card, tool string) bool {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return false
	}
	for _, card := range cards {
		if strings.TrimSpace(getStringAny(card.Detail, "tool", "toolName", "name")) == tool {
			return true
		}
		switch tool {
		case "enter_plan_mode":
			if card.Type == "PlanModeCard" || (card.Type == "NoticeCard" && strings.Contains(card.Title, "计划模式")) {
				return true
			}
		case "exit_plan_mode":
			if card.Type == "PlanApprovalCard" {
				return true
			}
		}
	}
	return false
}

func requiredToolFollowupMessage(tool, hostID, userMessage string) string {
	hostID = defaultHostID(hostID)
	switch strings.TrimSpace(tool) {
	case "readonly_host_inspect":
		return strings.TrimSpace(fmt.Sprintf(
			"Required ReAct continuation after user clarification. The user selected readonly diagnosis for the original request: %q.\nYour next assistant action MUST be a tool call to readonly_host_inspect with host=%s. Do not answer in plain text, do not use built-in commandExecution, do not dispatch workers, and do not mutate state. Use safe read-only commands to inspect PostgreSQL availability, process/port state, service status, and replication clues on the selected host.",
			userMessage,
			hostID,
		))
	case "enter_plan_mode":
		return strings.TrimSpace(fmt.Sprintf(
			"Required ReAct continuation after user clarification. The user selected the repair/planning path for the original request: %q.\nYour next assistant action MUST be a tool call to enter_plan_mode. Do not answer in plain text and do not dispatch workers until exit_plan_mode is approved.",
			userMessage,
		))
	default:
		return strings.TrimSpace(fmt.Sprintf(
			"Required ReAct continuation after user clarification for the original request: %q.\nYour next assistant action MUST be a tool call to %s. Do not answer in plain text and do not repeat the same clarification question.",
			userMessage,
			strings.TrimSpace(tool),
		))
	}
}

func (a *App) startRequiredToolFollowup(followup requiredToolFollowup) {
	if a == nil || strings.TrimSpace(followup.SessionID) == "" || strings.TrimSpace(followup.Tool) == "" {
		return
	}
	now := model.NowString()
	a.store.UpdateCard(followup.SessionID, followup.ChoiceCardID, func(card *model.Card) {
		if card.Detail == nil {
			card.Detail = map[string]any{}
		}
		card.Detail["requiredToolFollowupStarted"] = true
		card.Detail["requiredTool"] = followup.Tool
		card.Detail["requiredToolFollowupAt"] = now
		card.UpdatedAt = now
	})
	a.startRuntimeTurn(followup.SessionID, followup.HostID)
	a.broadcastSnapshot(followup.SessionID)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var err error
	if a.useBifrostForSession(followup.SessionID) {
		err = a.runBifrostTurn(ctx, followup.SessionID, chatRequest{
			Message: followup.Message,
			HostID:  followup.HostID,
		})
	} else {
		err = a.runReActAgentLoop(ctx, reActLoopRequest{
			SessionID:        followup.SessionID,
			Kind:             reActLoopKindWorkspace,
			HostID:           followup.HostID,
			Message:          followup.Message,
			RequestID:        model.NewID("req"),
			RequestStartedAt: time.Now(),
		})
	}
	if err != nil {
		a.finishRuntimeTurn(followup.SessionID, "failed")
		a.store.UpsertCard(followup.SessionID, model.Card{
			ID:        model.NewID("error"),
			Type:      "ErrorCard",
			Title:     "Required tool follow-up failed",
			Message:   err.Error(),
			Text:      err.Error(),
			Status:    "failed",
			CreatedAt: model.NowString(),
			UpdatedAt: model.NowString(),
		})
		a.broadcastSnapshot(followup.SessionID)
	}
}

func normalizeChoiceIntentText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r', '?', '？', ':', '：', '(', ')', '（', '）', '`', '"', '\'', '“', '”':
			return -1
		default:
			return r
		}
	}, value)
}

type agentProfileResetRequest struct {
	ProfileID string `json:"profileId"`
}

type agentProfilePreviewResponse struct {
	ProfileID         string                     `json:"profileId"`
	ProfileType       string                     `json:"profileType"`
	SystemPrompt      string                     `json:"systemPrompt"`
	SystemPromptLines int                        `json:"systemPromptLines"`
	CommandSummary    []string                   `json:"commandSummary"`
	CapabilitySummary []string                   `json:"capabilitySummary"`
	EnabledSkills     []model.AgentSkill         `json:"enabledSkills"`
	EnabledMCPs       []model.AgentMCP           `json:"enabledMcps"`
	Runtime           model.AgentRuntimeSettings `json:"runtime"`
}

type agentProfileUpdateRequest struct {
	model.AgentProfile
	RiskConfirmed bool `json:"riskConfirmed"`
}

type agentProfileErrorResponse struct {
	Error       string            `json:"error"`
	FieldErrors map[string]string `json:"fieldErrors,omitempty"`
}

type agentProfileValidationError struct {
	message     string
	fieldErrors map[string]string
}

func (e agentProfileValidationError) Error() string {
	if strings.TrimSpace(e.message) != "" {
		return e.message
	}
	return "agent profile validation failed"
}

type loginResponse struct {
	AuthURL string `json:"authUrl,omitempty"`
}

func New(cfg config.Config) *App {
	st := store.New()
	st.UpsertHost(model.Host{
		ID:              model.ServerLocalHostID,
		Name:            "server-local",
		Kind:            "server_local",
		Status:          "online",
		Executable:      true,
		TerminalCapable: true,
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
	})

	app := &App{
		cfg:   cfg,
		store: st,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(_ *http.Request) bool { return true },
		},
		agents:            make(map[string]*agentConnection),
		wsClients:         make(map[string]map[*websocket.Conn]struct{}),
		turnCancels:       make(map[string]context.CancelFunc),
		bifrostSessions:   make(map[string]*agentloop.Session),
		workspaceRuntimes: make(map[string]*agentloop.WorkspaceRuntime),
		terminals:         make(map[string]*terminalSession),
		execs:             make(map[string]*remoteExecSession),
		fileReqs:          make(map[string]*agentResponseWaiter),
		fileChangeClaims:  make(map[string]struct{}),
		oauthStates:       make(map[string]string),
		turnTraces:        make(map[string]*turnTrace),
		orchestratorJobs:  make(map[string]string),
		broadcastTimers:   make(map[string]*time.Timer),
		commandRunner:     defaultCommandRunner,
	}
	return app
}

func (a *App) Start(ctx context.Context) error {
	if err := os.MkdirAll(a.cfg.DefaultWorkspace, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.cfg.StatePath), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.cfg.AuditLogPath), 0o755); err != nil {
		return err
	}
	absWorkspace, err := filepath.Abs(a.cfg.DefaultWorkspace)
	if err == nil {
		a.cfg.DefaultWorkspace = absWorkspace
	}
	if absStatePath, err := filepath.Abs(a.cfg.StatePath); err == nil {
		a.cfg.StatePath = absStatePath
	}
	a.store.SetStatePath(a.cfg.StatePath)
	if err := a.store.LoadStableState(a.cfg.StatePath); err != nil {
		return fmt.Errorf("load state store: %w", err)
	}
	if err := a.initOrchestrator(); err != nil {
		return fmt.Errorf("load orchestrator store: %w", err)
	}
	a.approvalAuditStore = store.NewApprovalAuditStore(filepath.Join(filepath.Dir(a.cfg.StatePath), "approval-audits.json"))
	if err := a.approvalAuditStore.Load(); err != nil {
		return fmt.Errorf("load approval audit store: %w", err)
	}
	a.approvalGrantStore = store.NewApprovalGrantStore(filepath.Join(filepath.Dir(a.cfg.StatePath), "approval-grants.json"))
	if err := a.approvalGrantStore.Load(); err != nil {
		return fmt.Errorf("load approval grant store: %w", err)
	}
	a.capabilityBindingStore = store.NewCapabilityBindingStore(filepath.Join(filepath.Dir(a.cfg.StatePath), "capability-bindings.json"))
	if err := a.capabilityBindingStore.Load(); err != nil {
		return fmt.Errorf("load capability binding store: %w", err)
	}
	a.uiCardStore = store.NewUICardStore(filepath.Join(filepath.Dir(a.cfg.StatePath), "ui-cards.json"))
	if err := a.uiCardStore.Load(); err != nil {
		return fmt.Errorf("load ui card store: %w", err)
	}
	a.scriptConfigStore = store.NewScriptConfigStore(filepath.Join(filepath.Dir(a.cfg.StatePath), "script-configs.json"))
	if err := a.scriptConfigStore.Load(); err != nil {
		return fmt.Errorf("load script config store: %w", err)
	}
	a.labEnvironmentStore = store.NewLabEnvironmentStore(filepath.Join(filepath.Dir(a.cfg.StatePath), "lab-environments.json"), a.store)
	if err := a.labEnvironmentStore.Load(); err != nil {
		return fmt.Errorf("load lab environment store: %w", err)
	}
	if a.cfg.CorootConfigured() {
		a.corootClient = coroot.NewClient(a.cfg.CorootBaseURL, a.cfg.CorootToken, a.cfg.CorootTimeout)
		a.dataSourceRouter = coroot.NewDataSourceRouter(
			coroot.NewClientProvider(a.corootClient),
			nil, // fallback provider — will be wired in a later task
			coroot.ParsePriorityStrategy(a.cfg.CorootPriority),
		)
		log.Printf("coroot data source router initialized (strategy=%s)", a.cfg.CorootPriority)
	}
	if a.cfg.CorootRCAEnabled && a.cfg.CorootConfigured() {
		a.rcaEngine = coroot.NewCorootRCAEngine(a.corootClient)
		log.Printf("coroot RCA engine initialized (live)")
	} else {
		a.rcaEngine = coroot.NewStubRCAEngine()
		log.Printf("coroot RCA engine initialized (stub)")
	}
	a.store.UpsertHost(model.Host{
		ID:              model.ServerLocalHostID,
		Name:            "server-local",
		Kind:            "server_local",
		Status:          "online",
		Executable:      true,
		TerminalCapable: true,
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
	})
	if err := a.cfg.ValidateHostAgentSecurity(); err != nil {
		return err
	}
	if grpcAddrExposed(a.cfg.GRPCAddr) && a.cfg.UsesDefaultBootstrapToken() {
		log.Printf("warning: grpc agent endpoint %s is exposed with default bootstrap token; rotate HOST_AGENT_BOOTSTRAP_TOKEN immediately", a.cfg.GRPCAddr)
	}
	if grpcAddrExposed(a.cfg.GRPCAddr) && len(a.cfg.AllowedAgentHostIDs) == 0 {
		log.Printf("warning: grpc agent endpoint %s is exposed without HOST_AGENT_ALLOWED_HOST_IDS allowlist", a.cfg.GRPCAddr)
	}
	if grpcAddrExposed(a.cfg.GRPCAddr) && len(a.cfg.AllowedAgentCIDRs) == 0 {
		log.Printf("warning: grpc agent endpoint %s is exposed without HOST_AGENT_ALLOWED_CIDRS source allowlist", a.cfg.GRPCAddr)
	}
	if grpcAddrExposed(a.cfg.GRPCAddr) && (strings.TrimSpace(a.cfg.GRPCTLSCertFile) == "" || strings.TrimSpace(a.cfg.GRPCTLSKeyFile) == "") {
		log.Printf("warning: grpc agent endpoint %s is exposed without TLS; prefer AIOPS_GRPC_TLS_CERT_FILE/AIOPS_GRPC_TLS_KEY_FILE or keep it behind VPN only", a.cfg.GRPCAddr)
	}
	a.loadLLMConfig()
	if err := a.initBifrostRuntime(); err != nil {
		return err
	}
	a.reconcileOrchestratorRecoveredWorkers()

	httpMux := http.NewServeMux()
	httpMux.HandleFunc("/api/v1/healthz", a.handleHealthz)
	httpMux.HandleFunc("/api/v1/sessions", a.withBrowserSession(a.handleSessions))
	httpMux.HandleFunc("/api/v1/sessions/", a.withBrowserSession(a.handleSessionActivation))
	httpMux.HandleFunc("/api/v1/state", a.withSession(a.handleState))
	httpMux.HandleFunc("/api/v1/workspace/missions", a.withBrowserSession(a.handleWorkspaceMissionHistory))
	httpMux.HandleFunc("/api/v1/workspace/missions/", a.withBrowserSession(a.handleWorkspaceMissionHistoryDetail))
	httpMux.HandleFunc("/api/v1/agent-profiles", a.withSession(a.handleAgentProfiles))
	httpMux.HandleFunc("/api/v1/agent-profiles/", a.withSession(a.handleAgentProfileByID))
	httpMux.HandleFunc("/api/v1/agent-profiles/export", a.withSession(a.handleAgentProfilesExport))
	httpMux.HandleFunc("/api/v1/agent-profiles/import", a.withSession(a.handleAgentProfilesImport))
	httpMux.HandleFunc("/api/v1/agent-skills", a.withSession(a.handleAgentSkills))
	httpMux.HandleFunc("/api/v1/agent-skills/", a.withSession(a.handleAgentSkillByID))
	httpMux.HandleFunc("/api/v1/agent-mcps", a.withSession(a.handleAgentMCPs))
	httpMux.HandleFunc("/api/v1/agent-mcps/", a.withSession(a.handleAgentMCPByID))
	httpMux.HandleFunc("/api/v1/mcp/servers", a.withSession(a.handleMCPServers))
	httpMux.HandleFunc("/api/v1/mcp/servers/", a.withSession(a.handleMCPServerByName))
	httpMux.HandleFunc("/api/v1/agent-profile", a.withSession(a.handleAgentProfile))
	httpMux.HandleFunc("/api/v1/agent-profile/reset", a.withSession(a.handleAgentProfileReset))
	httpMux.HandleFunc("/api/v1/agent-profile/preview", a.withSession(a.handleAgentProfilePreview))
	httpMux.HandleFunc("/api/v1/host/select", a.withSession(a.handleHostSelection))
	httpMux.HandleFunc("/api/v1/hosts", a.withSession(a.handleHosts))
	httpMux.HandleFunc("/api/v1/hosts/", a.withSession(a.handleHostByID))
	httpMux.HandleFunc("/api/v1/thread/reset", a.withSession(a.handleThreadReset))
	httpMux.HandleFunc("/api/v1/auth/login", a.withSession(a.handleAuthLogin))
	httpMux.HandleFunc("/api/v1/auth/logout", a.withSession(a.handleAuthLogout))
	httpMux.HandleFunc("/api/v1/auth/oauth/start", a.withSession(a.handleOAuthStart))
	httpMux.HandleFunc("/api/v1/auth/oauth/callback", a.withSession(a.handleOAuthCallback))
	httpMux.HandleFunc("/api/v1/chat/message", a.withSession(a.handleChatMessage))
	httpMux.HandleFunc("/api/v1/chat/stop", a.withSession(a.handleChatStop))
	httpMux.HandleFunc("/api/v1/approvals/", a.withSession(a.handleApprovalDecision))
	httpMux.HandleFunc("/api/v1/approval-audits", a.withSession(a.handleApprovalAudits))
	httpMux.HandleFunc("/api/v1/approval-audits/", a.withSession(a.handleApprovalAuditByID))
	httpMux.HandleFunc("/api/v1/approval-grants", a.withSession(a.handleApprovalGrants))
	httpMux.HandleFunc("/api/v1/approval-grants/", a.withSession(a.handleApprovalGrantAction))
	httpMux.HandleFunc("/api/v1/capability-bindings", a.withSession(a.handleCapabilityBindings))
	httpMux.HandleFunc("/api/v1/capability-bindings/", a.withSession(a.handleCapabilityBindingByID))
	httpMux.HandleFunc("/api/v1/ui-cards", a.withSession(a.handleUICards))
	httpMux.HandleFunc("/api/v1/ui-cards/", a.withSession(a.handleUICardByID))
	httpMux.HandleFunc("/api/v1/script-configs", a.withSession(a.handleScriptConfigs))
	httpMux.HandleFunc("/api/v1/script-configs/", a.withSession(a.handleScriptConfigByID))
	httpMux.HandleFunc("/api/v1/lab-environments", a.withSession(a.handleLabEnvironments))
	httpMux.HandleFunc("/api/v1/lab-environments/", a.withSession(a.handleLabEnvironmentByID))
	a.registerEvidenceRoutes(httpMux)
	httpMux.HandleFunc("/api/v1/generator/", a.withSession(a.handleGenerator))
	httpMux.HandleFunc("/api/v1/llm-config", a.withSession(a.handleLLMConfig))
	httpMux.HandleFunc("/api/v1/coroot/config", a.withSession(a.handleCorootConfig))
	httpMux.HandleFunc("/api/v1/coroot/rca/", a.withSession(a.handleCorootRCA))
	httpMux.HandleFunc("/api/v1/coroot/hosts/", a.withSession(a.handleCorootHostOverview))
	httpMux.HandleFunc("/api/v1/coroot/", a.withSession(a.handleCorootProxy))
	httpMux.HandleFunc("/api/v1/choices/", a.withSession(a.handleChoiceAnswer))
	httpMux.HandleFunc("/api/v1/terminal/sessions", a.withSession(a.handleTerminalCreate))
	httpMux.HandleFunc("/api/v1/terminal/ws", a.withSession(a.handleTerminalWS))
	httpMux.HandleFunc("/api/v1/files/preview", a.withSession(a.handleFilePreview))
	httpMux.HandleFunc("/ws", a.withSession(a.handleWS))
	httpMux.Handle("/", a.serveFrontend())

	a.httpServer = &http.Server{
		Addr:    a.cfg.HTTPAddr,
		Handler: httpMux,
	}

	grpcServerOptions := make([]grpc.ServerOption, 0, 1)
	if creds, err := a.grpcServerCredentials(); err != nil {
		return err
	} else if creds != nil {
		grpcServerOptions = append(grpcServerOptions, grpc.Creds(creds))
	}
	a.grpcServer = grpc.NewServer(grpcServerOptions...)
	agentrpc.RegisterAgentServiceServer(a.grpcServer, a)

	go a.monitorHosts(ctx)

	return nil
}

func (a *App) Run(ctx context.Context) error {
	httpErrCh := make(chan error, 1)
	grpcErrCh := make(chan error, 1)

	go func() {
		log.Printf("http server listening on %s", a.cfg.HTTPAddr)
		if err := a.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpErrCh <- err
		}
	}()

	go func() {
		listener, err := net.Listen("tcp", a.cfg.GRPCAddr)
		if err != nil {
			grpcErrCh <- err
			return
		}
		log.Printf("grpc server listening on %s", a.cfg.GRPCAddr)
		if err := a.grpcServer.Serve(listener); err != nil {
			grpcErrCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		a.stopAllTerminals(shutdownCtx)
		if a.mcpManager != nil {
			a.mcpManager.DisconnectAll()
		}
		_ = a.httpServer.Shutdown(shutdownCtx)
		a.grpcServer.GracefulStop()
		return ctx.Err()
	case err := <-httpErrCh:
		return err
	case err := <-grpcErrCh:
		return err
	}
}

func (a *App) Connect(stream agentrpc.AgentService_ConnectServer) error {
	var hostID string
	var conn *agentConnection

	defer func() {
		if hostID != "" {
			a.clearAgentConnection(hostID, conn)
			a.failRemoteTerminalsForHost(hostID, "remote host disconnected")
			a.failRemoteExecsForHost(hostID, "remote host disconnected")
			a.failAgentResponseWaitersForHost(hostID, "remote host disconnected")
			a.store.MarkHostOffline(hostID)
			a.reconcileOrchestratorHostUnavailable(hostID, "remote host disconnected")
			a.notifyRemoteHostUnavailable(hostID, "远程主机已断连", "远程主机连接已断开，当前任务可能失败，可稍后重试或刷新。")
			a.audit("agent.disconnect", map[string]any{
				"hostId": hostID,
			})
			a.broadcastAllSnapshots()
		}
	}()

	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}

		switch msg.Kind {
		case "register":
			if msg.Registration == nil {
				_ = stream.Send(&agentrpc.Envelope{
					Kind:  "error",
					Error: "missing registration payload",
				})
				continue
			}
			sourceAddr := agentPeerRemoteAddress(stream.Context())
			if !a.cfg.AgentSourceAllowed(sourceAddr) {
				_ = stream.Send(&agentrpc.Envelope{
					Kind:  "error",
					Error: fmt.Sprintf("agent source address %s is not allowed", defaultString(sourceAddr, "unknown")),
				})
				continue
			}
			registeredHostID := strings.TrimSpace(msg.Registration.HostID)
			if registeredHostID == "" {
				_ = stream.Send(&agentrpc.Envelope{
					Kind:  "error",
					Error: "host id is required",
				})
				continue
			}
			if hostID != "" {
				_ = stream.Send(&agentrpc.Envelope{
					Kind:  "error",
					Error: "host identity is already established for this stream",
				})
				continue
			}
			if !a.cfg.ValidAgentBootstrapToken(msg.Registration.Token) {
				_ = stream.Send(&agentrpc.Envelope{
					Kind:  "error",
					Error: "invalid bootstrap token",
				})
				continue
			}
			if !a.cfg.AgentHostAllowed(registeredHostID) {
				_ = stream.Send(&agentrpc.Envelope{
					Kind:  "error",
					Error: "host id is not allowed",
				})
				continue
			}

			hostID = registeredHostID
			conn = &agentConnection{hostID: hostID, stream: stream}
			a.setAgentConnection(hostID, conn)
			log.Printf("host-agent register host_id=%s hostname=%s remote_addr=%s", msg.Registration.HostID, msg.Registration.Hostname, defaultString(sourceAddr, "unknown"))
			a.audit("agent.register", map[string]any{
				"hostId":       hostID,
				"hostname":     msg.Registration.Hostname,
				"os":           msg.Registration.OS,
				"arch":         msg.Registration.Arch,
				"agentVersion": msg.Registration.AgentVersion,
				"remoteAddr":   sourceAddr,
			})
			a.store.UpsertHost(model.Host{
				ID:              hostID,
				Name:            msg.Registration.Hostname,
				Kind:            "agent",
				Status:          "online",
				Executable:      true,
				TerminalCapable: true,
				OS:              msg.Registration.OS,
				Arch:            msg.Registration.Arch,
				AgentVersion:    msg.Registration.AgentVersion,
				Labels:          msg.Registration.Labels,
				LastHeartbeat:   model.NowString(),
			})
			a.clearRemoteHostUnavailableCards(hostID)
			a.broadcastAllSnapshots()

			_ = conn.send(&agentrpc.Envelope{
				Kind: "ack",
				Ack: &agentrpc.Ack{
					Message:   "registered",
					Timestamp: time.Now().Unix(),
				},
			})
			if err := a.pushHostAgentProfile(conn); err != nil {
				log.Printf("push host-agent profile on register failed host=%s err=%v", hostID, err)
			}
		case "heartbeat":
			if msg.Heartbeat == nil {
				continue
			}
			heartbeatHostID := strings.TrimSpace(msg.Heartbeat.HostID)
			if hostID == "" {
				_ = stream.Send(&agentrpc.Envelope{
					Kind:  "error",
					Error: "agent is not registered",
				})
				continue
			}
			if heartbeatHostID != "" && heartbeatHostID != hostID {
				_ = stream.Send(&agentrpc.Envelope{
					Kind:  "error",
					Error: "host identity mismatch",
				})
				continue
			}
			log.Printf("host-agent heartbeat host_id=%s", hostID)
			host := a.findHost(hostID)
			host.Status = "online"
			host.Executable = true
			host.TerminalCapable = true
			host.LastHeartbeat = model.NowString()
			a.store.UpsertHost(host)
			a.clearRemoteHostUnavailableCards(hostID)
			a.broadcastAllSnapshots()
			target := conn
			if target == nil {
				target = &agentConnection{hostID: hostID, stream: stream}
			}
			_ = target.send(&agentrpc.Envelope{
				Kind: "ack",
				Ack: &agentrpc.Ack{
					Message:   "heartbeat",
					Timestamp: time.Now().Unix(),
				},
			})
		case "ping":
			target := conn
			if target == nil {
				target = &agentConnection{hostID: hostID, stream: stream}
			}
			_ = target.send(&agentrpc.Envelope{
				Kind: "pong",
				Ack: &agentrpc.Ack{
					Message:   "pong",
					Timestamp: time.Now().Unix(),
				},
			})
		case "profile/ack":
			a.handleAgentProfileAck(hostID, msg.ProfileAck)
		case "terminal/ready":
			a.handleAgentTerminalReady(hostID, msg.TerminalReady)
		case "terminal/output":
			a.handleAgentTerminalOutput(hostID, msg.TerminalOutput)
		case "terminal/exit":
			a.handleAgentTerminalExit(hostID, msg.TerminalExit)
		case "terminal/status", "terminal/error":
			a.handleAgentTerminalStatus(hostID, msg.TerminalStatus)
		case "exec/output":
			a.handleAgentExecOutput(hostID, msg.ExecOutput)
		case "exec/exit":
			a.handleAgentExecExit(hostID, msg.ExecExit)
		case "file/list/result":
			a.handleAgentFileListResult(hostID, msg.FileListResult)
		case "file/read/result":
			a.handleAgentFileReadResult(hostID, msg.FileReadResult)
		case "file/search/result":
			a.handleAgentFileSearchResult(hostID, msg.FileSearchResult)
		case "file/write/result":
			a.handleAgentFileWriteResult(hostID, msg.FileWriteResult)
		}
	}
}

func DialAgent(ctx context.Context, addr string) (*grpc.ClientConn, error) {
	dialCreds, err := agentDialCredentialsFromEnv()
	if err != nil {
		return nil, err
	}
	if dialCreds == nil {
		dialCreds = insecure.NewCredentials()
	}
	return grpc.DialContext(
		ctx,
		addr,
		grpc.WithTransportCredentials(dialCreds),
		grpc.WithDefaultCallOptions(grpc.CallContentSubtype("json")),
	)
}

func (a *App) grpcServerCredentials() (credentials.TransportCredentials, error) {
	certFile := strings.TrimSpace(a.cfg.GRPCTLSCertFile)
	keyFile := strings.TrimSpace(a.cfg.GRPCTLSKeyFile)
	if certFile == "" || keyFile == "" {
		return nil, nil
	}

	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load grpc tls key pair: %w", err)
	}
	tlsConfig := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{certificate},
	}

	clientCAFile := strings.TrimSpace(a.cfg.GRPCTLSClientCAFile)
	if clientCAFile != "" {
		caBytes, err := os.ReadFile(clientCAFile)
		if err != nil {
			return nil, fmt.Errorf("read grpc client ca file: %w", err)
		}
		clientPool := x509.NewCertPool()
		if !clientPool.AppendCertsFromPEM(caBytes) {
			return nil, errors.New("append grpc client ca pem failed")
		}
		tlsConfig.ClientCAs = clientPool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return credentials.NewTLS(tlsConfig), nil
}

func agentDialCredentialsFromEnv() (credentials.TransportCredentials, error) {
	caFile := strings.TrimSpace(os.Getenv("AIOPS_AGENT_TLS_CA_FILE"))
	certFile := strings.TrimSpace(os.Getenv("AIOPS_AGENT_TLS_CERT_FILE"))
	keyFile := strings.TrimSpace(os.Getenv("AIOPS_AGENT_TLS_KEY_FILE"))
	serverName := strings.TrimSpace(os.Getenv("AIOPS_AGENT_TLS_SERVER_NAME"))
	skipVerify := strings.EqualFold(strings.TrimSpace(os.Getenv("AIOPS_AGENT_TLS_INSECURE_SKIP_VERIFY")), "true")

	if caFile == "" && certFile == "" && keyFile == "" && serverName == "" && !skipVerify {
		return nil, nil
	}

	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         serverName,
		InsecureSkipVerify: skipVerify,
	}
	if caFile != "" {
		caBytes, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read agent ca file: %w", err)
		}
		rootPool := x509.NewCertPool()
		if !rootPool.AppendCertsFromPEM(caBytes) {
			return nil, errors.New("append agent ca pem failed")
		}
		tlsConfig.RootCAs = rootPool
	}
	if certFile != "" || keyFile != "" {
		if certFile == "" || keyFile == "" {
			return nil, errors.New("both AIOPS_AGENT_TLS_CERT_FILE and AIOPS_AGENT_TLS_KEY_FILE are required for mTLS")
		}
		certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load agent tls key pair: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	return credentials.NewTLS(tlsConfig), nil
}

func grpcAddrExposed(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		host = strings.TrimSpace(addr)
	}
	switch host {
	case "", "127.0.0.1", "::1", "localhost":
		return false
	default:
		return true
	}
}

func agentPeerRemoteAddress(ctx context.Context) string {
	info, ok := peer.FromContext(ctx)
	if !ok || info.Addr == nil {
		return ""
	}
	return info.Addr.String()
}

func (a *App) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	status := http.StatusOK
	if !a.useBifrost() {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{
		"ok":            status == http.StatusOK,
		"codexAlive":    a.useBifrost(),
		"codexLastExit": "",
	})
}

func (a *App) handleSessions(w http.ResponseWriter, r *http.Request, browserID string) {
	activeSessionID := a.store.EnsureActiveSession(browserID)
	switch r.Method {
	case http.MethodGet:
		a.syncAccountState(r.Context(), activeSessionID)
		writeJSON(w, http.StatusOK, map[string]any{
			"activeSessionId": activeSessionID,
			"sessions":        a.store.SessionSummaries(browserID),
		})
	case http.MethodPost:
		if current := a.store.Session(activeSessionID); current != nil && current.Runtime.Turn.Active {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "当前任务执行中，暂时无法新建会话"})
			return
		}
		createReq := sessionCreateRequest{}
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&createReq); err != nil && !errors.Is(err, io.EOF) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
				return
			}
		}
		kind, err := normalizeSessionCreateKind(createReq.Kind)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		session := a.store.CreateSessionWithMeta(browserID, sessionCreateMeta(kind), true)
		a.syncAccountState(r.Context(), session.ID)
		writeJSON(w, http.StatusOK, map[string]any{
			"activeSessionId": session.ID,
			"sessions":        a.store.SessionSummaries(browserID),
			"snapshot":        a.snapshot(session.ID),
		})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (a *App) handleSessionActivation(w http.ResponseWriter, r *http.Request, browserID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	targetSessionID := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	targetSessionID = strings.TrimSuffix(targetSessionID, "/activate")
	targetSessionID = strings.TrimSpace(targetSessionID)
	if targetSessionID == "" || !strings.HasSuffix(r.URL.Path, "/activate") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	currentSessionID := a.store.EnsureActiveSession(browserID)
	if currentSessionID != targetSessionID {
		if current := a.store.Session(currentSessionID); current != nil && current.Runtime.Turn.Active {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "当前任务执行中，完成后再切换会话"})
			return
		}
	}
	if err := a.store.ActivateSession(browserID, targetSessionID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	a.syncAccountState(r.Context(), targetSessionID)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"activeSessionId": targetSessionID,
		"sessions":        a.store.SessionSummaries(browserID),
		"snapshot":        a.snapshot(targetSessionID),
	})
}

func (a *App) handleState(w http.ResponseWriter, r *http.Request, sessionID string) {
	a.store.EnsureSession(sessionID)
	a.store.TouchSession(sessionID)
	a.syncAccountState(r.Context(), sessionID)
	writeJSON(w, http.StatusOK, a.snapshot(sessionID))
}

func (a *App) handleAgentProfiles(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	a.store.EnsureSession(sessionID)
	a.store.TouchSession(sessionID)
	writeJSON(w, http.StatusOK, map[string]any{
		"items":         a.store.AgentProfiles(),
		"skillCatalog":  a.skillCatalog(),
		"mcpCatalog":    a.mcpCatalog(),
		"configVersion": model.AgentProfileConfigVersion,
	})
}

func (a *App) handleAgentProfileByID(w http.ResponseWriter, r *http.Request, sessionID string) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/agent-profiles/"), "/")
	if path == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent profile not found"})
		return
	}
	parts := strings.Split(path, "/")
	profileID := strings.TrimSpace(parts[0])
	if profileID == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent profile not found"})
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			a.handleAgentProfileGet(w, profileID)
		case http.MethodPut:
			a.handleAgentProfilePut(w, r, sessionID, profileID)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}
	if len(parts) == 2 && parts[1] == "reset" {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		a.handleAgentProfileResetWithID(w, sessionID, profileID)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent profile route not found"})
}

func (a *App) handleAgentProfile(w http.ResponseWriter, r *http.Request, sessionID string) {
	switch r.Method {
	case http.MethodGet:
		a.handleAgentProfileGet(w, a.agentProfileIDFromRequest(r))
	case http.MethodPut:
		a.handleAgentProfilePut(w, r, sessionID, a.agentProfileIDFromRequest(r))
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (a *App) handleAgentProfileReset(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req agentProfileResetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	profileID := strings.TrimSpace(req.ProfileID)
	if profileID == "" {
		profileID = a.agentProfileIDFromRequest(r)
	}
	a.handleAgentProfileResetWithID(w, sessionID, profileID)
}

func (a *App) handleAgentProfileGet(w http.ResponseWriter, profileID string) {
	profile, ok := a.store.AgentProfile(profileID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent profile not found"})
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (a *App) handleAgentProfilePut(w http.ResponseWriter, r *http.Request, sessionID, requestedProfileID string) {
	var req agentProfileUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	merged, before, err := a.mergeAndValidateAgentProfile(sessionID, requestedProfileID, req.AgentProfile, req.RiskConfirmed)
	if err != nil {
		a.writeAgentProfileError(w, http.StatusBadRequest, err)
		return
	}
	a.store.UpsertAgentProfile(merged)
	if merged.ID == string(model.AgentProfileTypeHostAgentDefault) {
		a.pushHostAgentProfileToConnectedAgents()
	}
	a.audit("agent_profile.updated", map[string]any{
		"sessionId":     sessionID,
		"profileId":     merged.ID,
		"profileType":   merged.Type,
		"operator":      a.auditOperator(sessionID),
		"before":        a.agentProfileAuditSummary(before),
		"after":         a.agentProfileAuditSummary(merged),
		"configVersion": model.AgentProfileConfigVersion,
	})
	writeJSON(w, http.StatusOK, merged)
}

func (a *App) handleAgentProfileResetWithID(w http.ResponseWriter, sessionID, profileID string) {
	before, ok := a.store.AgentProfile(profileID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent profile not found"})
		return
	}
	a.store.ResetAgentProfile(profileID)
	profile, ok := a.store.AgentProfile(profileID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent profile not found"})
		return
	}
	if profile.ID == string(model.AgentProfileTypeHostAgentDefault) {
		a.pushHostAgentProfileToConnectedAgents()
	}
	a.audit("agent_profile.reset", map[string]any{
		"sessionId":     sessionID,
		"profileId":     profile.ID,
		"profileType":   profile.Type,
		"operator":      a.auditOperator(sessionID),
		"before":        a.agentProfileAuditSummary(before),
		"after":         a.agentProfileAuditSummary(profile),
		"configVersion": model.AgentProfileConfigVersion,
	})
	writeJSON(w, http.StatusOK, profile)
}

func (a *App) writeAgentProfileError(w http.ResponseWriter, status int, err error) {
	var validationErr agentProfileValidationError
	if errors.As(err, &validationErr) {
		writeJSON(w, status, agentProfileErrorResponse{
			Error:       validationErr.Error(),
			FieldErrors: cloneStringMap(validationErr.fieldErrors),
		})
		return
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func (a *App) handleAgentProfilePreview(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	profileID := a.agentProfileIDFromRequest(r)
	profile, ok := a.store.AgentProfile(profileID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent profile not found"})
		return
	}
	selectedHostID := strings.TrimSpace(r.URL.Query().Get("hostId"))
	if selectedHostID == "" {
		session := a.store.Session(sessionID)
		if session != nil {
			selectedHostID = defaultHostID(session.SelectedHostID)
		}
	}
	if selectedHostID == "" {
		selectedHostID = model.ServerLocalHostID
	}
	writeJSON(w, http.StatusOK, a.buildAgentProfilePreview(profile, selectedHostID))
}

func (a *App) handleThreadReset(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	a.store.ResetConversation(sessionID)
	a.clearBifrostSession(sessionID)
	a.audit("thread.reset", map[string]any{
		"sessionId": sessionID,
	})
	a.broadcastSnapshot(sessionID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleAuthLogin(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req authLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	switch req.Mode {
	case "apiKey":
		if req.APIKey == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "apiKey is required"})
			return
		}
		log.Printf("auth login session=%s mode=apiKey", sessionID)
		a.store.SetAuth(sessionID, model.AuthState{
			Connected: true,
			Mode:      "apikey",
			Email:     req.Email,
		}, model.ExternalAuthTokens{Email: req.Email})
		a.audit("auth.login_started", map[string]any{
			"sessionId": sessionID,
			"mode":      req.Mode,
		})
		a.broadcastAllSnapshots()
		writeJSON(w, http.StatusOK, loginResponse{})
	case "chatgpt":
		if !a.cfg.OAuthConfigured() {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "oauth is not configured"})
			return
		}
		loginID := model.NewID("login")
		log.Printf("auth login session=%s mode=chatgpt", sessionID)
		a.store.SetPendingLogin(sessionID, loginID)
		a.audit("auth.login_started", map[string]any{
			"sessionId": sessionID,
			"mode":      req.Mode,
			"loginId":   loginID,
		})
		a.broadcastAllSnapshots()
		writeJSON(w, http.StatusOK, loginResponse{AuthURL: a.cfg.OAuthAuthURL})
	case "chatgptAuthTokens":
		accountID := req.ChatGPTAccountID
		if accountID == "" {
			accountID = a.cfg.OAuthAccountID
		}
		planType := req.ChatGPTPlanType
		if planType == "" {
			planType = a.cfg.OAuthPlanType
		}
		if req.AccessToken == "" || accountID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "accessToken and chatgptAccountId are required"})
			return
		}
		log.Printf("auth login session=%s mode=chatgptAuthTokens", sessionID)
		a.store.SetAuth(sessionID, model.AuthState{
			Connected: true,
			Mode:      "chatgptAuthTokens",
			PlanType:  planType,
			Email:     req.Email,
		}, model.ExternalAuthTokens{
			AccessToken:      req.AccessToken,
			ChatGPTAccountID: accountID,
			ChatGPTPlanType:  planType,
			Email:            req.Email,
		})
		a.audit("auth.login_started", map[string]any{
			"sessionId": sessionID,
			"mode":      req.Mode,
			"planType":  planType,
		})
		a.broadcastAllSnapshots()
		writeJSON(w, http.StatusOK, loginResponse{})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported mode"})
	}
}

func (a *App) handleAuthLogout(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	log.Printf("auth logout session=%s", sessionID)
	a.audit("auth.logout", map[string]any{
		"sessionId": sessionID,
	})
	a.store.ClearAuth(sessionID)
	a.broadcastAllSnapshots()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleOAuthStart(w http.ResponseWriter, r *http.Request, sessionID string) {
	if !a.cfg.OAuthConfigured() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "oauth is not configured"})
		return
	}

	state := model.NewID("oauth")
	a.oauthMu.Lock()
	a.oauthStates[sessionID] = state
	a.oauthMu.Unlock()

	values := url.Values{}
	values.Set("client_id", a.cfg.OAuthClientID)
	values.Set("redirect_uri", a.cfg.OAuthRedirectURL)
	values.Set("response_type", "code")
	values.Set("scope", strings.Join(a.cfg.OAuthScopeList(), " "))
	values.Set("state", state)

	target := a.cfg.OAuthAuthURL
	if strings.Contains(target, "?") {
		target += "&" + values.Encode()
	} else {
		target += "?" + values.Encode()
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (a *App) handleOAuthCallback(w http.ResponseWriter, r *http.Request, sessionID string) {
	if !a.cfg.OAuthConfigured() {
		http.Redirect(w, r, a.cfg.FrontendRedirectURL+"?login=oauth_not_configured", http.StatusFound)
		return
	}

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		a.store.SetAuth(sessionID, model.AuthState{LastError: errParam}, model.ExternalAuthTokens{})
		a.broadcastAllSnapshots()
		http.Redirect(w, r, a.cfg.FrontendRedirectURL+"?login="+url.QueryEscape(errParam), http.StatusFound)
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		http.Redirect(w, r, a.cfg.FrontendRedirectURL+"?login=missing_code", http.StatusFound)
		return
	}

	a.oauthMu.Lock()
	expectedState := a.oauthStates[sessionID]
	delete(a.oauthStates, sessionID)
	a.oauthMu.Unlock()
	if state != expectedState {
		http.Redirect(w, r, a.cfg.FrontendRedirectURL+"?login=invalid_state", http.StatusFound)
		return
	}

	tokenResp, err := a.exchangeOAuthCode(r.Context(), code)
	if err != nil {
		a.store.SetAuth(sessionID, model.AuthState{LastError: err.Error()}, model.ExternalAuthTokens{})
		a.broadcastAllSnapshots()
		http.Redirect(w, r, a.cfg.FrontendRedirectURL+"?login=exchange_failed", http.StatusFound)
		return
	}

	email := tokenResp.Email
	if email == "" {
		email = a.fetchOAuthEmail(r.Context(), tokenResp.AccessToken)
	}

	a.store.SetAuth(sessionID, model.AuthState{
		Connected: true,
		Mode:      "chatgptAuthTokens",
		PlanType:  a.cfg.OAuthPlanType,
		Email:     email,
	}, model.ExternalAuthTokens{
		IDToken:          tokenResp.IDToken,
		AccessToken:      tokenResp.AccessToken,
		ChatGPTAccountID: a.cfg.OAuthAccountID,
		ChatGPTPlanType:  a.cfg.OAuthPlanType,
		Email:            email,
	})
	a.broadcastAllSnapshots()
	http.Redirect(w, r, a.cfg.FrontendRedirectURL+"?login=success", http.StatusFound)
}

func (a *App) handleChatMessage(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
		return
	}
	if req.HostID == "" {
		req.HostID = model.ServerLocalHostID
	}

	if !a.useBifrostForSession(sessionID) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Bifrost runtime is not initialized"})
		return
	}
	a.syncAccountState(r.Context(), sessionID)
	auth := a.store.Auth(sessionID)
	if !auth.Connected {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "LLM 未连接，请先在设置中配置 API Key"})
		return
	}
	if a.answerPendingChoiceFromChatMessage(w, r, sessionID, req.Message) {
		return
	}
	requestStartedAt := time.Now()
	kind := a.sessionKind(sessionID)
	switch kind {
	case model.SessionKindWorkspace:
		a.handleWorkspaceChatMessage(w, r, sessionID, req, requestStartedAt)
		return
	default:
		if a.orchestrator != nil && a.isOrchestratorInternalSession(sessionID) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "internal session 不支持前台直接发送消息"})
			return
		}
	}
	host := a.findHost(req.HostID)
	if host.Status != "online" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "选中的主机当前离线"})
		return
	}
	if !host.Executable {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "选中的主机暂不支持执行"})
		return
	}

	session := a.store.EnsureSession(sessionID)
	previousHostID := defaultHostID(session.SelectedHostID)
	hasRuntimeBinding := session.ThreadID != "" || a.hasBifrostSession(sessionID)
	if previousHostID != req.HostID && hasRuntimeBinding {
		a.clearSessionThreadBinding(sessionID)
		a.appendHostSwitchCard(sessionID, previousHostID, req.HostID)
		session.ThreadID = ""
	} else if a.shouldRefreshThreadForAgentRuntime(session, req.HostID) {
		a.clearSessionThreadBinding(sessionID)
		a.appendAgentProfileRuntimeRefreshCard(sessionID)
		session.ThreadID = ""
	} else if a.shouldAutoResetThread(session, req.Message) {
		log.Printf(
			"auto thread reset session=%s host=%s cards=%d conversationCards=%d lastActivityAt=%s",
			sessionID,
			req.HostID,
			len(session.Cards),
			conversationCardCount(session.Cards),
			session.LastActivityAt,
		)
		a.clearSessionThreadBinding(sessionID)
		a.appendAutoThreadRefreshCard(sessionID)
		session.ThreadID = ""
	}
	a.store.TouchSession(sessionID)
	a.store.SetSelectedHost(sessionID, req.HostID)
	requestID := a.beginTurnTraceRequest(sessionID, req.HostID)
	log.Printf(
		"chat request begin session=%s request=%s kind=%s host=%s text=%q",
		sessionID,
		requestID,
		a.sessionKind(sessionID),
		req.HostID,
		truncate(req.Message, 120),
	)
	a.audit("chat.message", map[string]any{
		"sessionId": sessionID,
		"hostId":    req.HostID,
		"text":      truncate(req.Message, 400),
	})

	userCard := model.Card{
		ID:        model.NewID("msg"),
		Type:      "UserMessageCard",
		Role:      "user",
		Text:      req.Message,
		Status:    "completed",
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	}
	a.store.UpsertCard(sessionID, userCard)
	a.startRuntimeTurn(sessionID, req.HostID)
	a.broadcastSnapshot(sessionID)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	a.setTurnCancel(sessionID, cancel)
	defer func() {
		a.clearTurnCancel(sessionID)
		cancel()
	}()

	err := a.runBifrostTurn(ctx, sessionID, req)
	if err != nil {
		if errors.Is(err, context.Canceled) && a.turnWasInterrupted(sessionID) {
			log.Printf(
				"chat request interrupted session=%s request=%s kind=%s host=%s duration=%s",
				sessionID,
				requestID,
				a.sessionKind(sessionID),
				req.HostID,
				time.Since(requestStartedAt),
			)
			writeJSON(w, http.StatusAccepted, map[string]any{
				"accepted":    false,
				"interrupted": true,
			})
			return
		}
		log.Printf(
			"chat request failed session=%s request=%s kind=%s host=%s duration=%s err=%v",
			sessionID,
			requestID,
			a.sessionKind(sessionID),
			req.HostID,
			time.Since(requestStartedAt),
			err,
		)
		a.finishRuntimeTurn(sessionID, "failed")
		a.store.UpsertCard(sessionID, model.Card{
			ID:        model.NewID("error"),
			Type:      "ErrorCard",
			Title:     "Turn failed",
			Message:   err.Error(),
			Text:      err.Error(),
			Status:    "failed",
			CreatedAt: model.NowString(),
			UpdatedAt: model.NowString(),
		})
		a.broadcastSnapshot(sessionID)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	log.Printf(
		"chat request accepted session=%s request=%s kind=%s host=%s duration=%s",
		sessionID,
		requestID,
		a.sessionKind(sessionID),
		req.HostID,
		time.Since(requestStartedAt),
	)
	writeJSON(w, http.StatusAccepted, map[string]bool{"accepted": true})
}

func (a *App) handleHostSelection(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req hostSelectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	host, switched, err := a.switchSelectedHost(sessionID, req.HostID, true)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "执行中") {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	a.audit("host.selected", map[string]any{
		"sessionId": sessionID,
		"hostId":    host.ID,
		"hostName":  host.Name,
		"switched":  switched,
	})
	a.broadcastSnapshot(sessionID)
	writeJSON(w, http.StatusOK, a.snapshot(sessionID))
}

func (a *App) handleChatStop(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	kind := a.sessionKind(sessionID)
	switch kind {
	case model.SessionKindWorkspace:
		a.handleWorkspaceStop(w, r, sessionID)
		return
	default:
		if a.orchestrator != nil && a.isOrchestratorInternalSession(sessionID) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "internal session 不支持前台直接 stop"})
			return
		}
	}

	session := a.store.Session(sessionID)
	if session == nil || !session.Runtime.Turn.Active {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "当前没有可中断的任务"})
		return
	}
	if err := a.interruptSessionTurn(r.Context(), sessionID); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	a.audit("chat.stop", map[string]any{
		"sessionId": sessionID,
		"threadId":  session.ThreadID,
		"turnId":    session.TurnID,
	})
	a.broadcastSnapshot(sessionID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) appendThreadResetCard(sessionID string) {
	now := model.NowString()
	a.store.UpsertCard(sessionID, model.Card{
		ID:        model.NewID("notice"),
		Type:      "NoticeCard",
		Title:     "Thread restarted",
		Text:      "The previous Codex thread was no longer available, so this request is continuing in a fresh thread.",
		Status:    "notice",
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (a *App) appendAutoThreadRefreshCard(sessionID string) {
	now := model.NowString()
	a.store.UpsertCard(sessionID, model.Card{
		ID:        model.NewID("notice"),
		Type:      "NoticeCard",
		Title:     "Thread refreshed",
		Text:      "当前会话历史较长或间隔过久，已自动切换到新的线程以保持响应速度。",
		Status:    "notice",
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (a *App) appendHostSwitchCard(sessionID, fromHostID, toHostID string) {
	now := model.NowString()
	a.store.UpsertCard(sessionID, model.Card{
		ID:        model.NewID("notice"),
		Type:      "NoticeCard",
		Title:     "Host context switched",
		Text:      fmt.Sprintf("已从 %s 切换到 %s，后续请求会在新的主机线程中继续。", fromHostID, toHostID),
		Status:    "notice",
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (a *App) clearSessionThreadBinding(sessionID string) {
	a.store.ClearThread(sessionID)
	a.store.ClearTurn(sessionID)
	a.clearBifrostSession(sessionID)
}

func (a *App) knownHost(hostID string) (model.Host, bool) {
	hostID = defaultHostID(strings.TrimSpace(hostID))
	for _, host := range a.store.Hosts() {
		if host.ID == hostID {
			return host, true
		}
	}
	return model.Host{}, false
}

func hostNameOrID(host model.Host) string {
	if name := strings.TrimSpace(host.Name); name != "" {
		return name
	}
	return defaultHostID(strings.TrimSpace(host.ID))
}

func applyCardHost(card *model.Card, host model.Host) {
	card.HostID = defaultHostID(strings.TrimSpace(host.ID))
	card.HostName = hostNameOrID(host)
}

func (a *App) sessionTargetHost(sessionID string) model.Host {
	session := a.store.Session(sessionID)
	if session == nil {
		return a.findHost(model.ServerLocalHostID)
	}
	hostID := defaultHostID(session.Runtime.Turn.HostID)
	if hostID == model.ServerLocalHostID && session.SelectedHostID != "" {
		hostID = defaultHostID(session.SelectedHostID)
	}
	return a.findHost(hostID)
}

func (a *App) switchSelectedHost(sessionID, hostID string, appendSwitchCard bool) (model.Host, bool, error) {
	hostID = defaultHostID(strings.TrimSpace(hostID))
	if hostID == "" {
		hostID = model.ServerLocalHostID
	}
	host, ok := a.knownHost(hostID)
	if !ok {
		return model.Host{}, false, errors.New("selected host not found")
	}

	session := a.store.EnsureSession(sessionID)
	currentHostID := defaultHostID(session.SelectedHostID)
	if currentHostID == hostID {
		a.store.SetSelectedHost(sessionID, hostID)
		return host, false, nil
	}
	if session.Runtime.Turn.Active {
		return model.Host{}, false, errors.New("当前任务执行中，完成后再切换主机")
	}

	hadThreadBinding := session.ThreadID != "" || session.TurnID != ""
	a.clearSessionThreadBinding(sessionID)
	a.store.SetSelectedHost(sessionID, hostID)
	if appendSwitchCard && hadThreadBinding {
		a.appendHostSwitchCard(sessionID, currentHostID, hostID)
	}
	return host, true, nil
}

func isThreadNotFoundError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "thread not found")
}

func (a *App) shouldAutoResetThread(session *store.SessionState, message string) bool {
	if session == nil || session.ThreadID == "" || session.Runtime.Turn.Active {
		return false
	}
	if len(session.Cards) >= autoThreadResetCardThreshold {
		return true
	}
	if conversationCardCount(session.Cards) >= autoThreadResetConversationThreshold && isShortStandalonePrompt(message) {
		return true
	}
	lastActivityAt, err := time.Parse(time.RFC3339, session.LastActivityAt)
	if err != nil {
		return false
	}
	return time.Since(lastActivityAt) >= autoThreadResetIdleThreshold
}

func conversationCardCount(cards []model.Card) int {
	count := 0
	for _, card := range cards {
		if card.Type == "UserMessageCard" || card.Type == "MessageCard" {
			count++
		}
	}
	return count
}

func isShortStandalonePrompt(message string) bool {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" || strings.Contains(trimmed, "\n") {
		return false
	}
	if contextualFollowupPattern.MatchString(trimmed) {
		return false
	}
	return len([]rune(trimmed)) <= autoThreadResetShortPromptRuneLimit
}

func (a *App) handleApprovalDecision(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	approvalID := strings.TrimPrefix(r.URL.Path, "/api/v1/approvals/")
	approvalID = strings.TrimSuffix(approvalID, "/decision")
	targetSessionID, approval, ok := a.resolveApprovalTargetSession(sessionID, approvalID)
	if !ok {
		// Clean up any stale pending approval cards that reference this approval
		a.dismissStaleApprovalCards(sessionID, approvalID)
		a.broadcastSnapshot(sessionID)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "approval not found"})
		return
	}

	var req approvalDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	decision := normalizeApprovalDecisionInput(req.Decision)
	log.Printf("approval decision session=%s target=%s approval=%s decision=%s", sessionID, targetSessionID, approvalID, decision)
	if decision == "accept_session" {
		a.store.AddApprovalGrant(targetSessionID, approvalGrantFromApproval(approval))
	}
	if strings.HasPrefix(strings.TrimSpace(approval.Type), "bifrost_") {
		if err := a.resolveBifrostApproval(targetSessionID, approval, decision); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		if sessionID != targetSessionID {
			a.resolveMirroredApprovalCard(sessionID, approval, approvalStatusFromDecision(decision))
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if approval.Type == "remote_command" || approval.Type == "remote_file_change" {
		if approval.Type == "remote_file_change" && approval.Status != "pending" {
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return
		}
		if approval.Type == "remote_file_change" && (decision == "accept" || decision == "accept_session") {
			if !a.claimRemoteFileChangeExecution(targetSessionID, approval.ID) {
				writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
				return
			}
		}
		now := model.NowString()
		cardStatus := approvalStatusFromDecision(decision)
		a.store.ResolveApproval(targetSessionID, approvalID, cardStatus, now)
		approval.Status = cardStatus
		approval.ResolvedAt = now
		a.store.UpsertCard(targetSessionID, approvalMemoCard(a.findHost(approval.HostID), approval, decision, now))
		if sessionID != targetSessionID {
			a.resolveMirroredApprovalCard(sessionID, approval, cardStatus)
		}

		if decision == "decline" {
			a.store.UpdateCard(targetSessionID, approval.ItemID, func(card *model.Card) {
				card.Status = cardStatus
				card.UpdatedAt = now
			})
			a.setRuntimeTurnPhase(targetSessionID, "thinking")
			if a.orchestrator != nil && a.sessionKind(targetSessionID) == model.SessionKindWorker {
				_ = a.orchestrator.SyncWorkerPhase(targetSessionID, "thinking")
			}
			a.recordOrchestratorApprovalResolved(targetSessionID, approval)
			a.auditApprovalLifecycleEvent("approval.decision", targetSessionID, approval, decision, cardStatus, approval.RequestedAt, now, nil)
			a.broadcastSnapshot(targetSessionID)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := a.respondCodex(ctx, approval.RequestIDRaw, toolResponse("User declined the requested system mutation.", false)); err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return
		}

		createdAt := now
		detail := a.approvalCardDetail(targetSessionID, approval, nil)
		if existing := a.cardByID(targetSessionID, approval.ItemID); existing != nil {
			if existing.CreatedAt != "" {
				createdAt = existing.CreatedAt
			}
			if len(existing.Detail) > 0 {
				detail = cloneAnyMap(existing.Detail)
			}
		}
		if approval.Type == "remote_file_change" {
			card := model.Card{
				ID:        approval.ItemID,
				Type:      "FileChangeCard",
				Title:     "Remote file change",
				Status:    "inProgress",
				Changes:   approval.Changes,
				Detail:    detail,
				CreatedAt: createdAt,
				UpdatedAt: now,
			}
			applyCardHost(&card, a.findHost(approval.HostID))
			a.store.UpsertCard(targetSessionID, card)
		} else {
			card := model.Card{
				ID:        approval.ItemID,
				Type:      "CommandCard",
				Title:     "Command execution",
				Command:   approval.Command,
				Cwd:       approval.Cwd,
				Status:    "inProgress",
				Detail:    detail,
				CreatedAt: createdAt,
				UpdatedAt: now,
			}
			applyCardHost(&card, a.findHost(approval.HostID))
			a.store.UpsertCard(targetSessionID, card)
		}
		a.setRuntimeTurnPhase(targetSessionID, "executing")
		if a.orchestrator != nil && a.sessionKind(targetSessionID) == model.SessionKindWorker {
			_ = a.orchestrator.SyncWorkerPhase(targetSessionID, "executing")
		}
		a.recordOrchestratorApprovalResolved(targetSessionID, approval)
		a.auditApprovalLifecycleEvent("approval.decision", targetSessionID, approval, decision, cardStatus, approval.RequestedAt, now, nil)
		a.broadcastSnapshot(targetSessionID)
		go a.executeApprovedRemoteOperation(targetSessionID, approval)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	if strings.TrimSpace(approval.RequestIDRaw) == "" {
		cardStatus := approvalStatusFromDecision(decision)
		resolvedAt := model.NowString()
		a.store.ResolveApproval(targetSessionID, approvalID, cardStatus, resolvedAt)
		approval.Status = cardStatus
		approval.ResolvedAt = resolvedAt

		nextPhase := "thinking"
		if a.hasPendingApprovals(targetSessionID) {
			nextPhase = "waiting_approval"
		} else if approval.Type == "plan_exit" && decision == "decline" {
			nextPhase = "planning"
		} else if decision == "accept" || decision == "accept_session" {
			nextPhase = "executing"
		}
		a.setRuntimeTurnPhase(targetSessionID, nextPhase)
		if approval.Type == "plan_exit" && (decision == "accept" || decision == "accept_session") {
			a.store.UpdateRuntime(targetSessionID, func(rt *model.RuntimeState) {
				rt.PlanMode = false
			})
		}
		if a.orchestrator != nil && a.sessionKind(targetSessionID) == model.SessionKindWorker {
			_ = a.orchestrator.SyncWorkerPhase(targetSessionID, nextPhase)
		}
		a.recordOrchestratorApprovalResolved(targetSessionID, approval)
		a.auditApprovalLifecycleEvent("approval.decision", targetSessionID, approval, decision, cardStatus, approval.RequestedAt, resolvedAt, nil)
		a.store.UpdateCard(targetSessionID, approval.ItemID, func(card *model.Card) {
			card.Status = cardStatus
			card.UpdatedAt = model.NowString()
		})
		a.broadcastSnapshot(targetSessionID)
		if sessionID != targetSessionID {
			a.resolveMirroredApprovalCard(sessionID, approval, cardStatus)
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	codexDecision := mapApprovalDecision(decision, approval)
	codexResponse := map[string]any{
		"decision": codexDecision,
	}
	if approval.Type == "plan_exit" {
		codexResponse = planExitApprovalToolResponse(approval, decision, codexDecision)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	err := a.respondCodex(ctx, approval.RequestIDRaw, codexResponse)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	cardStatus := approvalStatusFromDecision(decision)
	resolvedAt := model.NowString()
	a.store.ResolveApproval(targetSessionID, approvalID, cardStatus, resolvedAt)
	approval.Status = cardStatus
	approval.ResolvedAt = resolvedAt
	nextPhase := "thinking"
	if a.hasPendingApprovals(targetSessionID) {
		nextPhase = "waiting_approval"
	} else if approval.Type == "plan_exit" && codexDecision == "decline" {
		nextPhase = "planning"
	} else if decision == "accept" || decision == "accept_session" {
		nextPhase = "executing"
	}
	a.setRuntimeTurnPhase(targetSessionID, nextPhase)
	if approval.Type == "plan_exit" && (decision == "accept" || decision == "accept_session") {
		a.store.UpdateRuntime(targetSessionID, func(rt *model.RuntimeState) {
			rt.PlanMode = false
		})
	}
	if a.orchestrator != nil && a.sessionKind(targetSessionID) == model.SessionKindWorker {
		_ = a.orchestrator.SyncWorkerPhase(targetSessionID, nextPhase)
	}
	a.recordOrchestratorApprovalResolved(targetSessionID, approval)
	a.auditApprovalLifecycleEvent("approval.decision", targetSessionID, approval, decision, cardStatus, approval.RequestedAt, model.NowString(), nil)
	if approval.Type == "command" {
		a.store.UpdateCard(targetSessionID, approval.ItemID, func(card *model.Card) {
			card.Status = cardStatus
			card.UpdatedAt = model.NowString()
		})
	} else {
		a.store.UpdateCard(targetSessionID, approval.ItemID, func(card *model.Card) {
			card.Status = cardStatus
			card.UpdatedAt = model.NowString()
		})
	}
	a.broadcastSnapshot(targetSessionID)
	if sessionID != targetSessionID {
		a.resolveMirroredApprovalCard(sessionID, approval, cardStatus)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleChoiceAnswer(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	choiceID := strings.TrimPrefix(r.URL.Path, "/api/v1/choices/")
	choiceID = strings.TrimSuffix(choiceID, "/answer")
	_, choice, ok := a.resolveChoiceTargetSession(sessionID, choiceID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "choice not found"})
		return
	}

	var req choiceAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if len(req.Answers) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "answers are required"})
		return
	}
	if len(choice.Questions) > 0 && len(req.Answers) != len(choice.Questions) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "answers count does not match questions"})
		return
	}

	for _, answer := range req.Answers {
		value := strings.TrimSpace(answer.Value)
		if value == "" {
			value = strings.TrimSpace(answer.Label)
		}
		if value == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "all answers must be non-empty"})
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := a.submitChoiceAnswer(ctx, sessionID, choiceID, choice, req.Answers); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) submitChoiceAnswer(ctx context.Context, sessionID, choiceID string, choice model.ChoiceRequest, answers []choiceAnswerInput) error {
	codexAnswers := make([]map[string]any, 0, len(answers))
	for _, answer := range answers {
		value := strings.TrimSpace(answer.Value)
		if value == "" {
			value = strings.TrimSpace(answer.Label)
		}
		if value == "" {
			return errors.New("all answers must be non-empty")
		}
		codexAnswer := map[string]any{
			"value": value,
			"label": emptyToNil(strings.TrimSpace(answer.Label)),
		}
		if answer.IsOther {
			codexAnswer["isOther"] = true
		}
		codexAnswers = append(codexAnswers, codexAnswer)
	}

	targetSessionID, resolvedChoice, ok := a.resolveChoiceTargetSession(sessionID, choiceID)
	if !ok {
		return errors.New("choice not found")
	}
	if strings.TrimSpace(resolvedChoice.ID) != "" {
		choice = resolvedChoice
	}
	followUpPayload := choiceFollowUpPayload(choice.Questions, answers, codexAnswers)
	hasCodexRequest := strings.TrimSpace(choice.RequestIDRaw) != ""
	if hasCodexRequest {
		if err := a.respondCodex(ctx, choice.RequestIDRaw, structuredToolResponse(followUpPayload, true)); err != nil {
			return err
		}
	}

	now := model.NowString()
	modelAnswers := choiceAnswersToModel(answers)
	a.store.ResolveChoiceWithAnswers(targetSessionID, choiceID, "completed", now, modelAnswers)
	a.store.UpdateCard(targetSessionID, choice.ItemID, func(card *model.Card) {
		card.Status = "completed"
		card.AnswerSummary = choiceAnswerSummary(choice.Questions, answers)
		card.UpdatedAt = now
	})
	a.setRuntimeTurnPhase(targetSessionID, "thinking")
	if a.orchestrator != nil && a.sessionKind(targetSessionID) == model.SessionKindWorker {
		_ = a.orchestrator.SyncWorkerPhase(targetSessionID, "thinking")
	}
	a.recordOrchestratorChoiceResolved(targetSessionID, choice, answers)
	a.audit("choice.answer", map[string]any{
		"sessionId": targetSessionID,
		"choiceId":  choiceID,
		"answers":   len(answers),
	})
	a.broadcastSnapshot(targetSessionID)
	if sessionID != targetSessionID {
		a.resolveMirroredChoiceCard(sessionID, choiceID, answers, choice.Questions)
	}
	if !hasCodexRequest {
		a.continuePlatformChoiceAnswer(targetSessionID, choice.ItemID, followUpPayload)
	}
	return nil
}

func (a *App) continuePlatformChoiceAnswer(sessionID, choiceCardID string, payload map[string]any) {
	hostID := model.ServerLocalHostID
	if session := a.store.Session(sessionID); session != nil {
		hostID = defaultHostID(session.SelectedHostID)
	}
	nextTool := strings.TrimSpace(getStringAny(payload, "next_required_tool", "required_next_tool"))
	if nextTool == "" && choicePayloadRequestsAnswerOnly(payload) {
		a.completePlatformAnswerOnlyChoice(sessionID, payload)
		return
	}
	message := ""
	if nextTool != "" {
		message = requiredToolFollowupMessage(nextTool, hostID, a.latestCompletedUserText(sessionID))
	} else {
		message = fmt.Sprintf(
			"The user clarified the original workspace request with this answer summary: %v.\nContinue accordingly. If the user only asked for capability or a solution outline, answer directly and do not inspect hosts, dispatch workers, or mutate state.",
			payload["answer_summary"],
		)
	}
	go a.startRequiredToolFollowup(requiredToolFollowup{
		SessionID:    sessionID,
		ChoiceCardID: choiceCardID,
		Tool:         firstNonEmptyValue(nextTool, "answer_only"),
		HostID:       hostID,
		Message:      message,
	})
}

func choicePayloadRequestsAnswerOnly(payload map[string]any) bool {
	if len(payload) == 0 {
		return false
	}
	switch answers := payload["answers"].(type) {
	case []map[string]any:
		for _, answer := range answers {
			if choiceAnswerTextRequestsAnswerOnly(fmt.Sprint(answer["value"])) ||
				choiceAnswerTextRequestsAnswerOnly(fmt.Sprint(answer["label"])) {
				return true
			}
		}
	case []any:
		for _, item := range answers {
			answer, _ := item.(map[string]any)
			if choiceAnswerTextRequestsAnswerOnly(fmt.Sprint(answer["value"])) ||
				choiceAnswerTextRequestsAnswerOnly(fmt.Sprint(answer["label"])) {
				return true
			}
		}
	}
	switch summary := payload["answer_summary"].(type) {
	case []string:
		for _, item := range summary {
			if choiceAnswerTextRequestsAnswerOnly(item) {
				return true
			}
		}
	case []any:
		for _, item := range summary {
			if choiceAnswerTextRequestsAnswerOnly(fmt.Sprint(item)) {
				return true
			}
		}
	case string:
		return choiceAnswerTextRequestsAnswerOnly(summary)
	}
	return false
}

func choiceAnswerTextRequestsAnswerOnly(value string) bool {
	normalized := normalizeChoiceIntentText(value)
	if normalized == "" {
		return false
	}
	for _, blocked := range []string{
		"readonly",
		"read_only",
		"repair_plan",
		"execute_repair",
		"只读",
		"诊断",
		"修复计划",
		"准备修复",
		"执行修复",
	} {
		if strings.Contains(normalized, normalizeChoiceIntentText(blocked)) {
			return false
		}
	}
	for _, token := range []string{
		"answer_only",
		"capability",
		"plan_only",
		"只问能力",
		"只给方案",
		"只回答",
		"不访问主机",
		"不执行命令",
	} {
		if strings.Contains(normalized, normalizeChoiceIntentText(token)) {
			return true
		}
	}
	return false
}

func (a *App) completePlatformAnswerOnlyChoice(sessionID string, payload map[string]any) {
	now := model.NowString()
	text := "可以处理。PG 不同步这类问题通常可以先用只读诊断确认是物理复制延迟、逻辑复制 slot、WAL 保留、网络/权限还是应用层同步失败，再在你明确授权后进入计划和执行流程。\n\n当前我只回答能力边界：不会访问主机、不会执行命令、不会派发 worker，也不会修改任何状态。"
	if summary := choicePayloadAnswerSummaryText(payload); summary != "" {
		text += "\n\n你的选择：" + summary
	}
	a.store.UpsertCard(sessionID, model.Card{
		ID:        model.NewID("msg"),
		Type:      "AssistantMessageCard",
		Role:      "assistant",
		Text:      text,
		Status:    "completed",
		CreatedAt: now,
		UpdatedAt: now,
	})
	a.finishRuntimeTurn(sessionID, "completed")
	a.recordOrchestratorReply(sessionID)
	a.broadcastSnapshot(sessionID)
}

func choicePayloadAnswerSummaryText(payload map[string]any) string {
	switch summary := payload["answer_summary"].(type) {
	case []string:
		return strings.Join(summary, "；")
	case []any:
		items := make([]string, 0, len(summary))
		for _, item := range summary {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				items = append(items, text)
			}
		}
		return strings.Join(items, "；")
	case string:
		return strings.TrimSpace(summary)
	default:
		return ""
	}
}

func (a *App) handleWS(w http.ResponseWriter, r *http.Request, sessionID string) {
	conn, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("session ws upgrade failed session=%s remote_addr=%s err=%v", sessionID, r.RemoteAddr, err)
		return
	}
	log.Printf("session ws connected session=%s remote_addr=%s", sessionID, r.RemoteAddr)

	a.wsMu.Lock()
	conns := a.wsClients[sessionID]
	if conns == nil {
		conns = make(map[*websocket.Conn]struct{})
		a.wsClients[sessionID] = conns
	}
	conns[conn] = struct{}{}
	a.wsMu.Unlock()

	_ = conn.WriteJSON(a.snapshot(sessionID))

	defer func() {
		a.wsMu.Lock()
		delete(a.wsClients[sessionID], conn)
		a.wsMu.Unlock()
		_ = conn.Close()
		log.Printf("session ws closed session=%s remote_addr=%s", sessionID, r.RemoteAddr)
	}()

	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			log.Printf("session ws read failed session=%s remote_addr=%s err=%v", sessionID, r.RemoteAddr, err)
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var incoming struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(payload, &incoming); err != nil {
			continue
		}
		if incoming.Type == "ping" {
			a.wsMu.Lock()
			writeErr := conn.WriteJSON(map[string]string{"type": "heartbeat"})
			a.wsMu.Unlock()
			if writeErr != nil {
				log.Printf("session ws heartbeat write failed session=%s remote_addr=%s err=%v", sessionID, r.RemoteAddr, writeErr)
				return
			}
		}
	}
}

func (a *App) claimRemoteFileChangeExecution(sessionID string, approvalID string) bool {
	key := sessionID + ":" + approvalID
	a.approvalMu.Lock()
	defer a.approvalMu.Unlock()
	if _, ok := a.fileChangeClaims[key]; ok {
		return false
	}
	a.fileChangeClaims[key] = struct{}{}
	return true
}

func (a *App) applyTurnPlanUpdated(payload map[string]any) {
	sessionID := a.store.SessionIDByThread(getString(payload, "threadId"))
	if sessionID == "" {
		return
	}
	if a.shouldIgnoreTurnPayload(sessionID, payload) {
		return
	}
	a.bindTurnToSession(sessionID, payload)
	if a.isWorkspaceDirectThread(sessionID) {
		return
	}
	a.setRuntimeTurnPhase(sessionID, "planning")
	cardID := "plan-" + getString(payload, "turnId")
	planItems := toPlanItems(payload["plan"])
	card := model.Card{
		ID:        cardID,
		Type:      "PlanCard",
		Title:     "Plan",
		Items:     planItems,
		Status:    "inProgress",
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	}
	a.store.UpsertCard(sessionID, card)
	a.broadcastSnapshot(sessionID)
}

func (a *App) handleLocalCommandApprovalRequest(rawID string, payload map[string]any) {
	sessionID := a.sessionIDFromPayload(payload)
	if sessionID == "" {
		return
	}
	if host, ok := a.selectedRemoteHostForSession(sessionID); ok {
		a.rejectUnexpectedLocalApproval(sessionID, rawID, "commandExecution", getString(payload, "command"), host)
		return
	}
	a.bindTurnToSession(sessionID, payload)
	hostID := model.ServerLocalHostID
	if session := a.store.Session(sessionID); session != nil && session.SelectedHostID != "" {
		hostID = session.SelectedHostID
	}
	approval := model.ApprovalRequest{
		ID:           model.NewID("approval"),
		RequestIDRaw: rawID,
		HostID:       hostID,
		Fingerprint:  approvalFingerprintForCommand(hostID, getString(payload, "command"), getString(payload, "cwd")),
		Type:         "command",
		Status:       "pending",
		ThreadID:     getStringAny(payload, "threadId", "thread_id"),
		TurnID:       getStringAny(payload, "turnId", "turn_id"),
		ItemID:       getString(payload, "itemId"),
		Command:      getString(payload, "command"),
		Cwd:          getString(payload, "cwd"),
		Reason:       getString(payload, "reason"),
		Decisions:    toStringSlice(payload["availableDecisions"]),
		RequestedAt:  model.NowString(),
	}
	commandState := a.mainAgentCapabilityState("commandExecution")
	decision, policyErr := a.evaluateCommandPolicy(approval.Command)
	if capabilityDisabled(commandState) {
		a.rejectApprovalByProfile(sessionID, rawID, approval, "Command execution blocked", "commandExecution capability is disabled by the current main-agent profile")
		return
	}
	if policyErr != nil {
		a.rejectApprovalByProfile(sessionID, rawID, approval, "Command blocked by profile", policyErr.Error())
		return
	}
	if a.workspacePlanModeNeedsApproval(sessionID) && commandPolicyDecisionIsMutation(decision) {
		a.rejectApprovalByPlanMode(sessionID, rawID, approval, "计划模式禁止执行变更命令")
		return
	}
	if decision.Category == "filesystem_mutation" && approval.Cwd != "" {
		if err := a.ensureWritableRoots([]string{approval.Cwd}); err != nil {
			a.rejectApprovalByProfile(sessionID, rawID, approval, "Command blocked by writable roots", err.Error())
			return
		}
	}
	if timeoutSec, ok := getIntAny(payload, "timeoutSec", "timeoutSeconds", "timeout"); ok && timeoutSec > a.mainAgentProfile().CommandPermissions.DefaultTimeoutSeconds {
		a.rejectApprovalByProfile(sessionID, rawID, approval, "Command blocked by timeout policy", "requested timeout exceeds the current main-agent profile limit")
		return
	}
	if a.autoApproveBySessionGrant(sessionID, approval) {
		return
	}
	if a.autoApproveByHostGrant(sessionID, approval) {
		return
	}
	if !capabilityNeedsApproval(commandState) && (decision.Mode == model.AgentPermissionModeAllow || decision.Mode == model.AgentPermissionModeReadonlyOnly) {
		if a.autoApproveLocalApprovalByProfile(sessionID, approval) {
			return
		}
	}
	a.setRuntimeTurnPhase(sessionID, "waiting_approval")
	log.Printf("approval requested type=command session=%s item=%s command=%q", sessionID, approval.ItemID, approval.Command)
	a.auditApprovalRequested(sessionID, approval, nil)
	a.store.AddApproval(sessionID, approval)
	card := model.Card{
		ID:      approval.ItemID,
		Type:    "CommandApprovalCard",
		Title:   "Command approval required",
		Command: approval.Command,
		Cwd:     approval.Cwd,
		Text:    approval.Reason,
		Status:  "pending",
		Approval: &model.ApprovalRef{
			RequestID: approval.ID,
			Type:      approval.Type,
			Decisions: approval.Decisions,
		},
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	}
	applyCardHost(&card, a.findHost(approval.HostID))
	a.store.UpsertCard(sessionID, card)
	a.recordOrchestratorApprovalRequested(sessionID, approval)
	if kind := a.sessionKind(sessionID); kind == model.SessionKindWorker {
		a.mirrorInternalApprovalToWorkspace(sessionID, approval, card)
		if workspaceSessionID := strings.TrimSpace(a.sessionMeta(sessionID).WorkspaceSessionID); workspaceSessionID != "" {
			a.activateQueuedMissionWorkers(workspaceSessionID)
		}
	}
	a.broadcastSnapshot(sessionID)
}

func (a *App) handleBuiltinUserInputRequest(rawID string, payload map[string]any) {
	sessionID := a.sessionIDFromPayload(payload)
	if sessionID == "" {
		_ = a.respondToolError(context.Background(), rawID, -32000, "session not found for request_user_input")
		return
	}
	a.bindTurnToSession(sessionID, payload)
	if a.isReActThread(sessionID) {
		message := "request_user_input is not available in this ReAct runtime; use the platform dynamic tool ask_user_question instead."
		log.Printf("react loop request_user_input rejected session=%s raw=%s", sessionID, rawID)
		a.store.UpsertCard(sessionID, model.Card{
			ID:        model.NewID("error"),
			Type:      "ErrorCard",
			Title:     "ReAct clarification tool misconfigured",
			Message:   message,
			Text:      message,
			Status:    "failed",
			CreatedAt: model.NowString(),
			UpdatedAt: model.NowString(),
		})
		a.broadcastSnapshot(sessionID)
		_ = a.respondToolError(context.Background(), rawID, -32000, message)
		return
	}
	questions := toChoiceQuestions(payload["questions"])
	if len(questions) == 0 {
		_ = a.respondToolError(context.Background(), rawID, -32602, "request_user_input requires questions")
		return
	}
	a.createChoiceRequest(rawID, sessionID, payload, questions)
}

func (a *App) createChoiceRequest(rawID, sessionID string, payload map[string]any, questions []model.ChoiceQuestion) {
	a.setRuntimeTurnPhase(sessionID, "waiting_input")
	now := model.NowString()
	choiceID := model.NewID("choice")
	choice := model.ChoiceRequest{
		ID:           choiceID,
		RequestIDRaw: rawID,
		ThreadID:     getStringAny(payload, "threadId", "thread_id"),
		TurnID:       getStringAny(payload, "turnId", "turn_id"),
		ItemID:       choiceID,
		Status:       "pending",
		Questions:    questions,
		RequestedAt:  now,
	}
	card := model.Card{
		ID:        choice.ItemID,
		Type:      "ChoiceCard",
		Title:     choiceCardTitle(questions),
		RequestID: choice.ID,
		Question:  questions[0].Question,
		Options:   questions[0].Options,
		Questions: questions,
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	}
	a.store.AddChoice(sessionID, choice)
	a.store.UpsertCard(sessionID, card)
	a.recordOrchestratorChoiceRequested(sessionID, choice)
	if kind := a.sessionKind(sessionID); kind == model.SessionKindWorker {
		a.mirrorInternalChoiceToWorkspace(sessionID, choice, card)
	}
	a.audit("choice.requested", map[string]any{
		"sessionId": sessionID,
		"choiceId":  choice.ID,
		"questions": len(questions),
	})
	a.broadcastSnapshot(sessionID)
}

func (a *App) handleItemStarted(payload map[string]any) {
	sessionID := a.sessionIDFromPayload(payload)
	if sessionID == "" {
		return
	}
	if a.shouldIgnoreTurnPayload(sessionID, payload) {
		return
	}
	a.bindTurnToSession(sessionID, payload)
	item := getMap(payload, "item")
	itemID := getString(item, "id")
	itemType := getString(item, "type")
	log.Printf(
		"turn item started session=%s request=%s turn=%s item=%s type=%s",
		sessionID,
		firstNonEmptyValue(a.turnTraceRequestID(sessionID), "-"),
		a.sessionTurnID(sessionID),
		itemID,
		itemType,
	)
	a.markTurnTraceFirstItem(sessionID, itemID, itemType)
	if host, ok := a.selectedRemoteHostForSession(sessionID); ok && (itemType == "commandExecution" || itemType == "fileChange") {
		a.blockUnexpectedLocalExecution(sessionID, payload, item, host)
		return
	}
	a.store.RememberItem(sessionID, itemID, item)
	a.updateActivityFromItem(sessionID, item, false)
	a.syncProcessLineCard(sessionID, itemID, item, false)

	now := model.NowString()
	host := a.sessionTargetHost(sessionID)
	switch itemType {
	case "commandExecution":
		a.setRuntimeTurnPhase(sessionID, "executing")
		a.incrementCommandCount(sessionID)
		card := model.Card{
			ID:        itemID,
			Type:      "CommandCard",
			Title:     "Command execution",
			Command:   getString(item, "command"),
			Cwd:       getString(item, "cwd"),
			Status:    normalizeCardStatus(getString(item, "status")),
			CreatedAt: now,
			UpdatedAt: now,
		}
		applyCardHost(&card, host)
		a.store.UpsertCard(sessionID, card)
	case "fileChange":
		a.setRuntimeTurnPhase(sessionID, "executing")
		card := model.Card{
			ID:        itemID,
			Type:      "FileChangeCard",
			Title:     "File change",
			Status:    normalizeCardStatus(getString(item, "status")),
			Changes:   toChanges(item["changes"]),
			CreatedAt: now,
			UpdatedAt: now,
		}
		applyCardHost(&card, host)
		a.store.UpsertCard(sessionID, card)
	case "agentMessage":
		a.setRuntimeTurnPhase(sessionID, "finalizing")
		card := model.Card{
			ID:        itemID,
			Type:      "AssistantMessageCard",
			Role:      "assistant",
			Status:    "inProgress",
			CreatedAt: now,
			UpdatedAt: now,
		}
		a.store.UpsertCard(sessionID, card)
		a.scheduleFinalizingExecutionCleanup(sessionID, getStringAny(payload, "threadId", "thread_id"))
		a.scheduleSilentTurnCompletionCheck(sessionID, silentTurnCompletionDelay)
	}
	if itemType == "agentMessage" && a.isWorkspaceRouteThread(sessionID) {
		return
	}
	a.broadcastSnapshot(sessionID)
}

func (a *App) handleItemCompleted(payload map[string]any) {
	sessionID := a.sessionIDFromPayload(payload)
	if sessionID == "" {
		return
	}
	if a.shouldIgnoreTurnPayload(sessionID, payload) {
		return
	}
	a.bindTurnToSession(sessionID, payload)
	item := getMap(payload, "item")
	itemID := getString(item, "id")
	itemType := getString(item, "type")
	a.store.RememberItem(sessionID, itemID, item)
	a.updateActivityFromItem(sessionID, item, true)
	a.syncProcessLineCard(sessionID, itemID, item, true)

	now := model.NowString()
	durationMS := a.cardDurationMS(sessionID, itemID, now)
	log.Printf(
		"turn item completed session=%s request=%s turn=%s item=%s type=%s duration_ms=%d status=%s",
		sessionID,
		firstNonEmptyValue(a.turnTraceRequestID(sessionID), "-"),
		a.sessionTurnID(sessionID),
		itemID,
		itemType,
		durationMS,
		normalizeCardStatus(getString(item, "status")),
	)

	switch itemType {
	case "agentMessage":
		a.markTurnTraceFirstAssistant(sessionID, itemID, "completed")
		a.store.UpdateCard(sessionID, itemID, func(card *model.Card) {
			card.Status = "completed"
			if card.Text == "" {
				card.Text = getString(item, "text")
			}
			card.DurationMS = durationMS
			card.UpdatedAt = now
			if isTaskCompletionText(card.Text) {
				card.Type = "TaskDividerCard"
				card.Role = ""
				card.Text = ""
				card.Title = ""
				card.Status = "completed"
			}
		})
	case "commandExecution":
		a.store.UpdateCard(sessionID, itemID, func(card *model.Card) {
			output := card.Output
			if aggregated := getString(item, "aggregatedOutput"); aggregated != "" && len(aggregated) >= len(output) {
				output = aggregated
			}
			result, finalStatus, summary, highlights, kvRows := buildLocalCommandCardPresentation(item, output)
			card.Output = output
			card.Stdout = result.Stdout
			card.Stderr = result.Stderr
			card.ExitCode = result.ExitCode
			card.Timeout = result.Timeout
			card.Cancelled = result.Cancelled
			card.Error = result.Error
			card.Summary = summary
			card.Highlights = highlights
			card.KVRows = kvRows
			card.Status = finalStatus
			if itemDuration, ok := getIntAny(item, "durationMs", "duration_ms"); ok && itemDuration > 0 {
				card.DurationMS = int64(itemDuration)
			} else {
				card.DurationMS = durationMS
			}
			card.UpdatedAt = now
		})
		if card := a.cardByID(sessionID, itemID); card != nil {
			a.bindCardEvidence(sessionID, itemID, evidenceArtifactInput{
				Kind:       "command_execution",
				SourceKind: "command",
				SourceRef:  firstNonEmptyValue(strings.TrimSpace(card.HostID), strings.TrimSpace(card.Command), itemID),
				Title:      firstNonEmptyValue(strings.TrimSpace(card.Title), "Command execution"),
				Summary:    strings.TrimSpace(card.Summary),
				Content: firstNonEmptyValue(
					card.Output,
					strings.TrimSpace(strings.Join([]string{card.Stdout, card.Stderr}, "\n")),
					card.Error,
				),
				Raw: item,
				Metadata: map[string]any{
					"command":  card.Command,
					"cwd":      card.Cwd,
					"status":   card.Status,
					"exitCode": card.ExitCode,
				},
			})
			if card = a.cardByID(sessionID, itemID); card != nil {
				a.syncActionVerification(sessionID, *card)
			}
		}
		a.resumeThinkingAfterExecution(sessionID)
	case "fileChange":
		a.store.UpdateCard(sessionID, itemID, func(card *model.Card) {
			card.Status = completedItemStatus(item)
			card.Changes = toChanges(item["changes"])
			card.DurationMS = durationMS
			card.UpdatedAt = now
		})
		if card := a.cardByID(sessionID, itemID); card != nil {
			target := itemID
			paths := changePaths(card.Changes)
			if len(paths) > 0 {
				target = paths[0]
			}
			summary := strings.TrimSpace(card.Summary)
			if summary == "" {
				summary = truncate(strings.Join(paths, ", "), 120)
			}
			if summary == "" {
				summary = firstNonEmptyValue(strings.TrimSpace(card.Status), strings.TrimSpace(card.Title))
			}
			a.bindCardEvidence(sessionID, itemID, evidenceArtifactInput{
				Kind:       "file_change",
				SourceKind: "config_diff",
				SourceRef:  target,
				Title:      firstNonEmptyValue(strings.TrimSpace(card.Title), "File change"),
				Summary:    summary,
				Content: stableCardJSON(map[string]any{
					"changes": card.Changes,
					"status":  card.Status,
				}),
				Raw: item,
				Metadata: map[string]any{
					"status": card.Status,
					"paths":  paths,
				},
			})
			if card = a.cardByID(sessionID, itemID); card != nil {
				a.syncActionVerification(sessionID, *card)
			}
		}
		a.resumeThinkingAfterExecution(sessionID)
	}
	if itemType == "agentMessage" {
		a.scheduleSilentTurnCompletionCheck(sessionID, silentTurnCompletionDelay)
		if a.isWorkspaceRouteThread(sessionID) {
			a.flushThrottledBroadcast(sessionID)
			a.broadcastSnapshot(sessionID)
			return
		}
	}
	a.broadcastSnapshot(sessionID)
}

func (a *App) autoApproveBySessionGrant(sessionID string, approval model.ApprovalRequest) bool {
	if approval.Fingerprint == "" {
		return false
	}
	if _, ok := a.store.ApprovalGrant(sessionID, approval.Fingerprint); !ok {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := a.respondCodex(ctx, approval.RequestIDRaw, map[string]any{
		"decision": "accept",
	}); err != nil {
		log.Printf("auto approval failed session=%s approval=%s err=%s", sessionID, approval.ID, truncate(err.Error(), 200))
		return false
	}

	now := model.NowString()
	approval.Status = "accepted_for_session_auto"
	approval.ResolvedAt = now
	a.store.AddApproval(sessionID, approval)
	a.store.ResolveApproval(sessionID, approval.ID, approval.Status, now)
	a.setRuntimeTurnPhase(sessionID, "executing")
	a.store.UpsertCard(sessionID, model.Card{
		ID:        "auto-approval-" + approval.ItemID,
		Type:      "NoticeCard",
		Title:     "Auto-approved for session",
		Text:      autoApprovalNoticeText(approval),
		Status:    "notice",
		CreatedAt: now,
		UpdatedAt: now,
	})
	log.Printf("approval auto accepted by session grant session=%s approval=%s type=%s", sessionID, approval.ID, approval.Type)
	a.auditApprovalLifecycleEvent("approval.auto_accepted", sessionID, approval, "accept_session", approval.Status, approval.RequestedAt, now, map[string]any{
		"fingerprint": approval.Fingerprint,
	})
	a.broadcastSnapshot(sessionID)
	return true
}

func (a *App) autoApproveByHostGrant(sessionID string, approval model.ApprovalRequest) bool {
	if approval.Fingerprint == "" || approval.HostID == "" {
		return false
	}
	if a.approvalGrantStore == nil {
		return false
	}
	if _, ok := a.approvalGrantStore.MatchFingerprint(approval.HostID, approval.Fingerprint); !ok {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := a.respondCodex(ctx, approval.RequestIDRaw, map[string]any{
		"decision": "accept",
	}); err != nil {
		log.Printf("auto approval by host grant failed session=%s approval=%s err=%s", sessionID, approval.ID, truncate(err.Error(), 200))
		return false
	}

	now := model.NowString()
	approval.Status = "accepted_for_host_auto"
	approval.ResolvedAt = now
	a.store.AddApproval(sessionID, approval)
	a.store.ResolveApproval(sessionID, approval.ID, approval.Status, now)
	a.setRuntimeTurnPhase(sessionID, "executing")
	a.store.UpsertCard(sessionID, model.Card{
		ID:        "auto-approval-" + approval.ItemID,
		Type:      "NoticeCard",
		Title:     "Auto-approved by host grant",
		Text:      hostGrantAutoApprovalNoticeText(approval),
		Status:    "notice",
		CreatedAt: now,
		UpdatedAt: now,
	})
	log.Printf("approval auto accepted by host grant session=%s approval=%s type=%s host=%s", sessionID, approval.ID, approval.Type, approval.HostID)
	a.auditApprovalLifecycleEvent("approval.auto_accepted", sessionID, approval, "auto_accept", approval.Status, approval.RequestedAt, now, map[string]any{
		"fingerprint": approval.Fingerprint,
		"grantMode":   "host",
	})
	a.broadcastSnapshot(sessionID)
	return true
}

func hostGrantAutoApprovalNoticeText(approval model.ApprovalRequest) string {
	if (approval.Type == "command" || approval.Type == "remote_command") && approval.Command != "" {
		return fmt.Sprintf("已通过主机级授权自动批准命令：%s", truncate(approval.Command, 72))
	}
	if approval.Type == "file_change" || approval.Type == "remote_file_change" {
		return "已通过主机级授权自动批准文件修改。"
	}
	return "已通过主机级授权自动批准操作。"
}

func (a *App) startRuntimeTurn(sessionID, hostID string) {
	previousSnapshot := a.snapshot(sessionID)
	startedAt := model.NowString()
	a.store.ClearTurn(sessionID)
	a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
		runtime.Turn.Active = true
		runtime.Turn.Phase = "thinking"
		runtime.Turn.HostID = defaultHostID(hostID)
		runtime.Turn.StartedAt = startedAt
		runtime.Activity = model.ActivityRuntime{
			ViewedFiles:            make([]model.ActivityEntry, 0),
			SearchedWebQueries:     make([]model.ActivityEntry, 0),
			SearchedContentQueries: make([]model.ActivityEntry, 0),
		}
	})
	a.markTurnTraceRuntimeStart(sessionID, hostID)
	a.syncIncidentStageTransition(sessionID, previousSnapshot, "thinking")
}

func (a *App) setRuntimeTurnPhase(sessionID, phase string) {
	phase = normalizeRuntimeTurnPhase(phase)
	previousSnapshot := a.snapshot(sessionID)
	session := a.store.Session(sessionID)
	if session != nil && session.Runtime.Turn.Phase == phase {
		currentActive := phase != "idle" && phase != "completed" && phase != "failed" && phase != "aborted"
		if session.Runtime.Turn.Active == currentActive && (session.Runtime.Turn.StartedAt != "" || !currentActive) {
			return
		}
	}

	a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
		runtime.Turn.Active = phase != "idle" && phase != "completed" && phase != "failed" && phase != "aborted"
		runtime.Turn.Phase = phase
		if runtime.Turn.StartedAt == "" && runtime.Turn.Active {
			runtime.Turn.StartedAt = model.NowString()
		}
		if runtime.Turn.HostID == "" {
			runtime.Turn.HostID = model.ServerLocalHostID
		}
	})
	a.recordOrchestratorTurnPhase(sessionID, phase)
	a.syncIncidentStageTransition(sessionID, previousSnapshot, phase)
}

func normalizeRuntimeTurnPhase(phase string) string {
	phase = strings.TrimSpace(phase)
	switch phase {
	case "", "idle":
		return "idle"
	case "thinking", "planning", "waiting_approval", "waiting_input", "executing", "finalizing", "completed", "failed", "aborted":
		return phase
	default:
		return "thinking"
	}
}

func (a *App) finishRuntimeTurn(sessionID, phase string) {
	previousSnapshot := a.snapshot(sessionID)
	a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
		runtime.Turn.Active = false
		runtime.Turn.Phase = phase
		runtime.Activity.CurrentReadingFile = ""
		runtime.Activity.CurrentChangingFile = ""
		runtime.Activity.CurrentListingPath = ""
		runtime.Activity.CurrentSearchKind = ""
		runtime.Activity.CurrentSearchQuery = ""
		runtime.Activity.CurrentWebSearchQuery = ""
	})
	a.completeTurnTrace(sessionID, phase)
	a.syncIncidentStageTransition(sessionID, previousSnapshot, phase)
}

func (a *App) beginTurnTraceRequest(sessionID, hostID string) string {
	now := time.Now()
	requestID := model.NewID("req")
	a.turnTraceMu.Lock()
	defer a.turnTraceMu.Unlock()
	trace := &turnTrace{
		RequestID:        requestID,
		SessionID:        sessionID,
		Kind:             a.sessionKind(sessionID),
		HostID:           defaultHostID(hostID),
		RequestStartedAt: now,
	}
	a.turnTraces[sessionID] = trace
	return requestID
}

func (a *App) markTurnTraceRuntimeStart(sessionID, hostID string) {
	now := time.Now()
	a.turnTraceMu.Lock()
	defer a.turnTraceMu.Unlock()
	trace := a.turnTraces[sessionID]
	requestStartedAt := time.Time{}
	if trace != nil {
		requestStartedAt = trace.RequestStartedAt
	}
	a.turnTraces[sessionID] = &turnTrace{
		RequestID:        firstNonEmptyValue(a.turnTraceRequestIDLocked(sessionID), ""),
		SessionID:        sessionID,
		Kind:             a.sessionKind(sessionID),
		HostID:           defaultHostID(hostID),
		RequestStartedAt: requestStartedAt,
		RuntimeStartedAt: now,
	}
}

func (a *App) markTurnTraceThreadStartBegin(sessionID string) {
	now := time.Now()
	a.turnTraceMu.Lock()
	defer a.turnTraceMu.Unlock()
	trace := a.turnTraces[sessionID]
	if trace == nil {
		trace = &turnTrace{
			RequestID:        firstNonEmptyValue(a.turnTraceRequestIDLocked(sessionID), ""),
			SessionID:        sessionID,
			Kind:             a.sessionKind(sessionID),
			HostID:           defaultHostID(a.sessionHostID(sessionID)),
			RuntimeStartedAt: now,
		}
		a.turnTraces[sessionID] = trace
	}
	if trace.ThreadStartBeganAt.IsZero() {
		trace.ThreadStartBeganAt = now
	}
}

func (a *App) markTurnTraceThreadStarted(sessionID, threadID string) {
	now := time.Now()
	a.turnTraceMu.Lock()
	defer a.turnTraceMu.Unlock()
	trace := a.turnTraces[sessionID]
	if trace == nil {
		trace = &turnTrace{
			RequestID:        firstNonEmptyValue(a.turnTraceRequestIDLocked(sessionID), ""),
			SessionID:        sessionID,
			Kind:             a.sessionKind(sessionID),
			HostID:           defaultHostID(a.sessionHostID(sessionID)),
			RuntimeStartedAt: now,
		}
		a.turnTraces[sessionID] = trace
	}
	trace.ThreadID = strings.TrimSpace(threadID)
	if trace.ThreadStartedAt.IsZero() {
		trace.ThreadStartedAt = now
	}
}

func (a *App) markTurnTraceTurnStartBegin(sessionID, threadID string) {
	now := time.Now()
	a.turnTraceMu.Lock()
	defer a.turnTraceMu.Unlock()
	trace := a.turnTraces[sessionID]
	if trace == nil {
		trace = &turnTrace{
			RequestID:        firstNonEmptyValue(a.turnTraceRequestIDLocked(sessionID), ""),
			SessionID:        sessionID,
			Kind:             a.sessionKind(sessionID),
			HostID:           defaultHostID(a.sessionHostID(sessionID)),
			RuntimeStartedAt: now,
		}
		a.turnTraces[sessionID] = trace
	}
	trace.ThreadID = firstNonEmptyValue(strings.TrimSpace(threadID), trace.ThreadID)
	if trace.TurnStartBeganAt.IsZero() {
		trace.TurnStartBeganAt = now
	}
}

func (a *App) markTurnTraceTurnStarted(sessionID, threadID, turnID string) {
	now := time.Now()
	a.turnTraceMu.Lock()
	defer a.turnTraceMu.Unlock()
	trace := a.turnTraces[sessionID]
	if trace == nil {
		trace = &turnTrace{
			RequestID:        firstNonEmptyValue(a.turnTraceRequestIDLocked(sessionID), ""),
			SessionID:        sessionID,
			Kind:             a.sessionKind(sessionID),
			HostID:           defaultHostID(a.sessionHostID(sessionID)),
			RuntimeStartedAt: now,
		}
		a.turnTraces[sessionID] = trace
	}
	trace.ThreadID = firstNonEmptyValue(strings.TrimSpace(threadID), trace.ThreadID)
	trace.TurnID = firstNonEmptyValue(strings.TrimSpace(turnID), trace.TurnID)
	if trace.TurnStartedAt.IsZero() {
		trace.TurnStartedAt = now
	}
}

func (a *App) markTurnTraceFirstItem(sessionID, itemID, itemType string) {
	now := time.Now()
	a.turnTraceMu.Lock()
	trace := a.turnTraces[sessionID]
	if trace == nil {
		trace = &turnTrace{
			SessionID:        sessionID,
			Kind:             a.sessionKind(sessionID),
			HostID:           defaultHostID(a.sessionHostID(sessionID)),
			RuntimeStartedAt: now,
		}
		a.turnTraces[sessionID] = trace
	}
	shouldLog := trace.FirstItemStartedAt.IsZero()
	if shouldLog {
		trace.FirstItemStartedAt = now
		trace.FirstItemID = strings.TrimSpace(itemID)
		trace.FirstItemType = strings.TrimSpace(itemType)
	}
	turnID := trace.TurnID
	requestID := trace.RequestID
	sinceRequest := formatTurnTraceDuration(trace.RequestStartedAt, trace.FirstItemStartedAt)
	sinceTurnStart := formatTurnTraceDuration(trace.TurnStartedAt, trace.FirstItemStartedAt)
	a.turnTraceMu.Unlock()
	if shouldLog {
		log.Printf(
			"turn first progress session=%s request=%s turn=%s item=%s type=%s since_request=%s since_turn_start=%s",
			sessionID,
			firstNonEmptyValue(requestID, "-"),
			firstNonEmptyValue(turnID, a.sessionTurnID(sessionID)),
			itemID,
			itemType,
			sinceRequest,
			sinceTurnStart,
		)
	}
}

func (a *App) markTurnTraceFirstAssistant(sessionID, itemID, source string) {
	now := time.Now()
	a.turnTraceMu.Lock()
	trace := a.turnTraces[sessionID]
	if trace == nil {
		trace = &turnTrace{
			RequestID:        firstNonEmptyValue(a.turnTraceRequestIDLocked(sessionID), ""),
			SessionID:        sessionID,
			Kind:             a.sessionKind(sessionID),
			HostID:           defaultHostID(a.sessionHostID(sessionID)),
			RuntimeStartedAt: now,
		}
		a.turnTraces[sessionID] = trace
	}
	shouldLog := trace.FirstAssistantAt.IsZero()
	if shouldLog {
		trace.FirstAssistantAt = now
		trace.FirstAssistantItemID = strings.TrimSpace(itemID)
		trace.FirstAssistantSource = strings.TrimSpace(source)
	}
	turnID := trace.TurnID
	requestID := trace.RequestID
	sinceRequest := formatTurnTraceDuration(trace.RequestStartedAt, trace.FirstAssistantAt)
	sinceTurnStart := formatTurnTraceDuration(trace.TurnStartedAt, trace.FirstAssistantAt)
	sinceFirstItem := formatTurnTraceDuration(trace.FirstItemStartedAt, trace.FirstAssistantAt)
	a.turnTraceMu.Unlock()
	if shouldLog {
		log.Printf(
			"turn first assistant session=%s request=%s turn=%s item=%s source=%s since_request=%s since_turn_start=%s since_first_item=%s",
			sessionID,
			firstNonEmptyValue(requestID, "-"),
			firstNonEmptyValue(turnID, a.sessionTurnID(sessionID)),
			itemID,
			source,
			sinceRequest,
			sinceTurnStart,
			sinceFirstItem,
		)
	}
}

func (a *App) completeTurnTrace(sessionID, finalPhase string) {
	now := time.Now()
	a.turnTraceMu.Lock()
	trace := a.turnTraces[sessionID]
	if trace == nil {
		a.turnTraceMu.Unlock()
		return
	}
	delete(a.turnTraces, sessionID)
	summary := fmt.Sprintf(
		"turn trace complete session=%s request=%s kind=%s host=%s turn=%s phase=%s total=%s request_to_thread=%s request_to_turn=%s turn_to_first_item=%s first_item_to_first_assistant=%s first_assistant_to_finish=%s",
		sessionID,
		firstNonEmptyValue(trace.RequestID, "-"),
		firstNonEmptyValue(trace.Kind, a.sessionKind(sessionID)),
		firstNonEmptyValue(trace.HostID, defaultHostID(a.sessionHostID(sessionID))),
		firstNonEmptyValue(trace.TurnID, a.sessionTurnID(sessionID)),
		finalPhase,
		formatTurnTraceTotal(trace, now),
		formatTurnTraceStage(trace.RequestStartedAt, trace.ThreadStartedAt),
		formatTurnTraceStage(trace.RequestStartedAt, trace.TurnStartedAt),
		formatTurnTraceStage(trace.TurnStartedAt, trace.FirstItemStartedAt),
		formatTurnTraceStage(trace.FirstItemStartedAt, trace.FirstAssistantAt),
		formatTurnTraceStage(trace.FirstAssistantAt, now),
	)
	a.turnTraceMu.Unlock()
	log.Print(summary)
}

func formatTurnTraceStage(start, end time.Time) string {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return "-"
	}
	return end.Sub(start).String()
}

func formatTurnTraceDuration(start, end time.Time) string {
	return formatTurnTraceStage(start, end)
}

func formatTurnTraceTotal(trace *turnTrace, end time.Time) string {
	if trace == nil {
		return "-"
	}
	start := trace.RequestStartedAt
	if start.IsZero() {
		start = trace.RuntimeStartedAt
	}
	return formatTurnTraceStage(start, end)
}

func (a *App) sessionHostID(sessionID string) string {
	session := a.store.Session(sessionID)
	if session == nil {
		return model.ServerLocalHostID
	}
	return defaultHostID(firstNonEmptyValue(session.Runtime.Turn.HostID, session.SelectedHostID))
}

func (a *App) turnTraceRequestID(sessionID string) string {
	a.turnTraceMu.Lock()
	defer a.turnTraceMu.Unlock()
	return a.turnTraceRequestIDLocked(sessionID)
}

func (a *App) turnTraceRequestIDLocked(sessionID string) string {
	trace := a.turnTraces[sessionID]
	if trace == nil {
		return ""
	}
	return strings.TrimSpace(trace.RequestID)
}

func (a *App) scheduleTurnStallMonitor(sessionID string, delay time.Duration) {
	session := a.store.Session(sessionID)
	if session == nil || session.TurnID == "" {
		return
	}
	turnID := session.TurnID

	go func() {
		ticker := time.NewTicker(delay)
		defer ticker.Stop()

		for range ticker.C {
			current := a.store.Session(sessionID)
			if current == nil || current.TurnID != turnID {
				return
			}
			if !current.Runtime.Turn.Active {
				return
			}
			if !isStallWatchPhase(current.Runtime.Turn.Phase) {
				continue
			}
			if a.hasPendingApprovals(sessionID) || a.hasPendingChoices(sessionID) {
				continue
			}

			lastActivityAt, err := time.Parse(time.RFC3339, current.LastActivityAt)
			if err != nil || time.Since(lastActivityAt) < delay {
				continue
			}

			a.failStalledTurn(sessionID, turnID, delay)
			return
		}
	}()
}

func isStallWatchPhase(phase string) bool {
	switch phase {
	case "thinking", "planning", "finalizing":
		return true
	default:
		return false
	}
}

func (a *App) failStalledTurn(sessionID, turnID string, delay time.Duration) {
	current := a.store.Session(sessionID)
	if current == nil || current.TurnID != turnID || !current.Runtime.Turn.Active {
		return
	}
	if !isStallWatchPhase(current.Runtime.Turn.Phase) {
		return
	}
	log.Printf(
		"turn stalled session=%s request=%s turn=%s phase=%s last_activity_at=%s delay=%s",
		sessionID,
		firstNonEmptyValue(a.turnTraceRequestID(sessionID), "-"),
		turnID,
		current.Runtime.Turn.Phase,
		current.LastActivityAt,
		delay,
	)

	now := model.NowString()
	a.finishRuntimeTurn(sessionID, "failed")
	a.finalizeOpenTurnCards(sessionID, "failed")
	a.resolvePendingTurnRequests(sessionID, now)
	a.store.UpsertCard(sessionID, model.Card{
		ID:        model.NewID("error"),
		Type:      "ErrorCard",
		Title:     "LLM 响应超时",
		Message:   fmt.Sprintf("这次请求在 %.0f 秒内没有返回任何进展，已自动结束。请重试；如果频繁出现，多半是当前 LLM 链路不稳定。", delay.Seconds()),
		Text:      fmt.Sprintf("这次请求在 %.0f 秒内没有返回任何进展，已自动结束。请重试；如果频繁出现，多半是当前 LLM 链路不稳定。", delay.Seconds()),
		Status:    "failed",
		CreatedAt: now,
		UpdatedAt: now,
	})
	a.broadcastSnapshot(sessionID)
}

func (a *App) resumeThinkingAfterExecution(sessionID string) {
	session := a.store.Session(sessionID)
	if session == nil || !session.Runtime.Turn.Active {
		return
	}
	if session.Runtime.Turn.Phase != "executing" {
		return
	}
	a.setRuntimeTurnPhase(sessionID, "thinking")
}

func (a *App) hasPendingApprovals(sessionID string) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}
	for _, approval := range session.Approvals {
		if approval.Status == "pending" {
			return true
		}
	}
	return false
}

// dismissStaleApprovalCards marks any pending approval cards that reference a
// missing approval as "expired" so they no longer appear in the UI after refresh.
func (a *App) dismissStaleApprovalCards(sessionID, approvalID string) {
	now := model.NowString()
	session := a.store.Session(sessionID)
	if session == nil {
		return
	}
	for _, card := range session.Cards {
		if card.Status != "pending" {
			continue
		}
		if card.Type != "CommandApprovalCard" && card.Type != "FileChangeApprovalCard" {
			continue
		}
		if card.Approval == nil || card.Approval.RequestID != approvalID {
			continue
		}
		a.store.UpdateCard(sessionID, card.ID, func(c *model.Card) {
			c.Status = "expired"
			c.UpdatedAt = now
		})
	}
}

func (a *App) hasPendingChoices(sessionID string) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}
	for _, choice := range session.Choices {
		if choice.Status == "pending" {
			return true
		}
	}
	return false
}

func (a *App) hasInProgressExecutionCards(sessionID string) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}
	for _, card := range session.Cards {
		if normalizeCardStatus(card.Status) != "inProgress" {
			continue
		}
		switch card.Type {
		case "CommandCard", "FileChangeCard", "ProcessLineCard":
			return true
		}
	}
	return false
}

func (a *App) hasInProgressCards(sessionID string) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}
	for _, card := range session.Cards {
		if normalizeCardStatus(card.Status) == "inProgress" || card.Status == "pending" {
			return true
		}
	}
	return false
}

func (a *App) hasCompletedAssistantMessage(sessionID string) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}
	for _, card := range session.Cards {
		if card.Type == "AssistantMessageCard" && normalizeCardStatus(card.Status) == "completed" {
			return true
		}
	}
	return false
}

func (a *App) finalizeLingeringExecutionCards(sessionID string) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}

	now := model.NowString()
	changed := false
	for _, existing := range session.Cards {
		if normalizeCardStatus(existing.Status) != "inProgress" {
			continue
		}

		switch existing.Type {
		case "CommandCard":
			item := a.store.Item(sessionID, existing.ID)
			output := existing.Output
			if aggregated := getString(item, "aggregatedOutput"); aggregated != "" && len(aggregated) >= len(output) {
				output = aggregated
			}
			durationMS := existing.DurationMS
			if durationMS == 0 {
				if itemDuration, ok := getIntAny(item, "durationMs", "duration_ms"); ok && itemDuration > 0 {
					durationMS = int64(itemDuration)
				} else {
					durationMS = durationBetween(existing.CreatedAt, now)
				}
			}
			result, status, summary, highlights, kvRows := buildLocalCommandCardPresentation(item, output)
			a.store.UpdateCard(sessionID, existing.ID, func(card *model.Card) {
				card.Output = output
				card.Stdout = result.Stdout
				card.Stderr = result.Stderr
				card.ExitCode = result.ExitCode
				card.Timeout = result.Timeout
				card.Cancelled = result.Cancelled
				card.Error = result.Error
				card.Summary = summary
				card.Highlights = highlights
				card.KVRows = kvRows
				card.Status = status
				card.DurationMS = durationMS
				card.UpdatedAt = now
			})
			changed = true
		case "FileChangeCard", "ProcessLineCard":
			durationMS := existing.DurationMS
			if durationMS == 0 {
				durationMS = durationBetween(existing.CreatedAt, now)
			}
			a.store.UpdateCard(sessionID, existing.ID, func(card *model.Card) {
				card.Status = "completed"
				card.DurationMS = durationMS
				card.UpdatedAt = now
			})
			changed = true
		}
	}
	return changed
}

func (a *App) scheduleFinalizingExecutionCleanup(sessionID, threadID string) {
	session := a.store.Session(sessionID)
	if session == nil || strings.TrimSpace(threadID) == "" {
		return
	}
	turnID := session.TurnID

	go func() {
		timer := time.NewTimer(1500 * time.Millisecond)
		defer timer.Stop()
		<-timer.C

		current := a.store.Session(sessionID)
		if current == nil || current.TurnID != turnID {
			return
		}
		if !current.Runtime.Turn.Active || current.Runtime.Turn.Phase != "finalizing" {
			return
		}
		if a.hasPendingApprovals(sessionID) || a.hasPendingChoices(sessionID) {
			return
		}
		if !a.hasInProgressExecutionCards(sessionID) {
			return
		}

		changed := a.finalizeLingeringExecutionCards(sessionID)
		a.cleanBackgroundTerminalsWithTimeout(threadID, 15*time.Second)
		if changed {
			log.Printf("finalizing cleanup resolved lingering execution cards session=%s turn=%s", sessionID, turnID)
			a.broadcastSnapshot(sessionID)
		}
	}()
}

func (a *App) scheduleSilentTurnCompletionCheck(sessionID string, delay time.Duration) {
	session := a.store.Session(sessionID)
	if session == nil {
		return
	}
	turnID := session.TurnID

	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		<-timer.C

		current := a.store.Session(sessionID)
		if current == nil || current.TurnID != turnID {
			return
		}
		if !current.Runtime.Turn.Active || current.Runtime.Turn.Phase != "finalizing" {
			return
		}
		if a.hasPendingApprovals(sessionID) || a.hasPendingChoices(sessionID) || a.hasInProgressCards(sessionID) {
			return
		}
		if !a.hasCompletedAssistantMessage(sessionID) {
			return
		}

		lastActivityAt, err := time.Parse(time.RFC3339, current.LastActivityAt)
		if err != nil || time.Since(lastActivityAt) < delay {
			return
		}

		a.finishRuntimeTurn(sessionID, "completed")
		a.finalizeOpenTurnCards(sessionID, "completed")
		log.Printf("auto completed silent finalizing turn session=%s turn=%s", sessionID, turnID)
		a.broadcastSnapshot(sessionID)
	}()
}

func (a *App) setTurnCancel(sessionID string, cancel context.CancelFunc) {
	a.turnMu.Lock()
	defer a.turnMu.Unlock()
	a.turnCancels[sessionID] = cancel
}

func (a *App) clearTurnCancel(sessionID string) {
	a.turnMu.Lock()
	defer a.turnMu.Unlock()
	if _, ok := a.turnCancels[sessionID]; ok {
		delete(a.turnCancels, sessionID)
	}
}

func (a *App) cancelTurnStart(sessionID string) bool {
	a.turnMu.Lock()
	cancel := a.turnCancels[sessionID]
	a.turnMu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (a *App) turnWasInterrupted(sessionID string) bool {
	session := a.store.Session(sessionID)
	return session != nil && session.Runtime.Turn.Phase == "aborted"
}

func (a *App) incrementCommandCount(sessionID string) {
	a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
		runtime.Activity.CommandsRun++
	})
}

func (a *App) bindTurnToSession(sessionID string, payload map[string]any) {
	turnID := getTurnID(payload)
	if sessionID == "" || turnID == "" {
		return
	}
	a.store.SetTurn(sessionID, turnID)
}

func (a *App) shouldIgnoreTurnPayload(sessionID string, payload map[string]any) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}
	if session.Runtime.Turn.Active || (session.Runtime.Turn.Phase != "aborted" && session.Runtime.Turn.Phase != "failed") {
		return false
	}
	turnID := getTurnID(payload)
	if turnID != "" && session.TurnID != "" {
		return turnID == session.TurnID
	}
	threadID := getStringAny(payload, "threadId", "thread_id")
	return threadID != "" && threadID == session.ThreadID
}

func (a *App) isWorkspaceRouteThread(sessionID string) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}
	return session.Meta.Kind == model.SessionKindWorkspace && strings.HasSuffix(strings.TrimSpace(session.ThreadConfigHash), ":workspace-route")
}

func (a *App) isWorkspaceReadonlyThread(sessionID string) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}
	return session.Meta.Kind == model.SessionKindWorkspace && strings.HasSuffix(strings.TrimSpace(session.ThreadConfigHash), ":workspace-readonly")
}

func (a *App) isWorkspaceDirectThread(sessionID string) bool {
	return a.isWorkspaceRouteThread(sessionID) || a.isWorkspaceReadonlyThread(sessionID)
}

func (a *App) isReActThread(sessionID string) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}
	hash := strings.TrimSpace(session.ThreadConfigHash)
	return strings.HasSuffix(hash, ":"+reActLoopVersion) || strings.HasSuffix(hash, ":workspace-"+reActLoopVersion)
}

func (a *App) cardIsFinal(sessionID, cardID string) bool {
	session := a.store.Session(sessionID)
	if session == nil {
		return false
	}
	for _, card := range session.Cards {
		if card.ID != cardID {
			continue
		}
		return normalizeCardStatus(card.Status) != "inProgress"
	}
	return false
}

func (a *App) sessionIDFromPayload(payload map[string]any) string {
	if sessionID := a.store.SessionIDByThread(getStringAny(payload, "threadId", "thread_id")); sessionID != "" {
		return sessionID
	}
	if sessionID := a.store.SessionIDByTurn(getTurnID(payload)); sessionID != "" {
		return sessionID
	}
	activeSessionID := ""
	for _, sessionID := range a.store.SessionIDs() {
		session := a.store.Session(sessionID)
		if session == nil || !session.Runtime.Turn.Active {
			continue
		}
		if activeSessionID != "" {
			return ""
		}
		activeSessionID = sessionID
	}
	return activeSessionID
}

func (a *App) updateActivityFromItem(sessionID string, item map[string]any, completed bool) {
	kind, entry, currentLabel, ok := detectActivitySignal(item)
	if !ok {
		return
	}

	a.store.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
		switch kind {
		case "file_read":
			if completed {
				if activityItemStatus(kind, item) != "completed" {
					if runtime.Activity.CurrentReadingFile == currentLabel {
						runtime.Activity.CurrentReadingFile = ""
					}
					return
				}
				if runtime.Activity.CurrentReadingFile == currentLabel {
					runtime.Activity.CurrentReadingFile = ""
				}
				appendUniqueActivityEntry(&runtime.Activity.ViewedFiles, entry, func(existing, next model.ActivityEntry) bool {
					return existing.Path != "" && existing.Path == next.Path
				})
				runtime.Activity.FilesViewed = len(runtime.Activity.ViewedFiles)
				return
			}
			runtime.Activity.CurrentReadingFile = currentLabel
		case "file_search":
			if completed {
				if activityItemStatus(kind, item) != "completed" {
					if runtime.Activity.CurrentSearchKind == "content" && runtime.Activity.CurrentSearchQuery == currentLabel {
						runtime.Activity.CurrentSearchKind = ""
						runtime.Activity.CurrentSearchQuery = ""
					}
					return
				}
				if runtime.Activity.CurrentSearchKind == "content" && runtime.Activity.CurrentSearchQuery == currentLabel {
					runtime.Activity.CurrentSearchKind = ""
					runtime.Activity.CurrentSearchQuery = ""
				}
				appendUniqueActivityEntry(&runtime.Activity.SearchedContentQueries, entry, func(existing, next model.ActivityEntry) bool {
					return existing.Query != "" && existing.Query == next.Query && existing.Path == next.Path
				})
				runtime.Activity.SearchCount = len(runtime.Activity.SearchedWebQueries) + len(runtime.Activity.SearchedContentQueries)
				return
			}
			runtime.Activity.CurrentSearchKind = "content"
			runtime.Activity.CurrentSearchQuery = currentLabel
		case "web_search":
			if completed {
				if activityItemStatus(kind, item) != "completed" {
					if runtime.Activity.CurrentSearchKind == "web" && runtime.Activity.CurrentSearchQuery == currentLabel {
						runtime.Activity.CurrentSearchKind = ""
						runtime.Activity.CurrentSearchQuery = ""
					}
					if runtime.Activity.CurrentWebSearchQuery == currentLabel {
						runtime.Activity.CurrentWebSearchQuery = ""
					}
					return
				}
				if runtime.Activity.CurrentSearchKind == "web" && runtime.Activity.CurrentSearchQuery == currentLabel {
					runtime.Activity.CurrentSearchKind = ""
					runtime.Activity.CurrentSearchQuery = ""
				}
				if runtime.Activity.CurrentWebSearchQuery == currentLabel {
					runtime.Activity.CurrentWebSearchQuery = ""
				}
				appendUniqueActivityEntry(&runtime.Activity.SearchedWebQueries, entry, func(existing, next model.ActivityEntry) bool {
					return existing.Query != "" && existing.Query == next.Query
				})
				runtime.Activity.SearchCount = len(runtime.Activity.SearchedWebQueries) + len(runtime.Activity.SearchedContentQueries)
				return
			}
			runtime.Activity.CurrentSearchKind = "web"
			runtime.Activity.CurrentSearchQuery = currentLabel
			runtime.Activity.CurrentWebSearchQuery = currentLabel
		case "list":
			if completed {
				if activityItemStatus(kind, item) != "completed" {
					if runtime.Activity.CurrentListingPath == currentLabel {
						runtime.Activity.CurrentListingPath = ""
					}
					return
				}
				if runtime.Activity.CurrentListingPath == currentLabel {
					runtime.Activity.CurrentListingPath = ""
				}
				runtime.Activity.ListCount++
				return
			}
			runtime.Activity.CurrentListingPath = currentLabel
		}
	})
}

func activityItemStatus(kind string, item map[string]any) string {
	switch kind {
	case "command":
		output := getStringAny(item, "aggregatedOutput", "output", "stdout", "stderr")
		return completedCommandStatus(item, output)
	default:
		return completedItemStatus(item)
	}
}

func (a *App) syncProcessLineCard(sessionID, itemID string, item map[string]any, completed bool) {
	kind, entry, currentLabel, ok := detectActivitySignal(item)
	if !ok {
		return
	}

	cardID := "process-" + itemID
	now := model.NowString()
	existing := a.cardByID(sessionID, cardID)
	createdAt := now
	if existing != nil && existing.CreatedAt != "" {
		createdAt = existing.CreatedAt
	}

	status := "inProgress"
	durationMS := int64(0)
	if completed {
		status = activityItemStatus(kind, item)
		durationMS = durationBetween(createdAt, now)
	}

	hostID := ""
	if session := a.store.Session(sessionID); session != nil {
		hostID = session.SelectedHostID
	}

	a.store.UpsertCard(sessionID, model.Card{
		ID:         cardID,
		Type:       "ProcessLineCard",
		HostID:     hostID,
		Text:       processLineText(kind, entry, currentLabel, completed),
		Status:     status,
		DurationMS: durationMS,
		CreatedAt:  createdAt,
		UpdatedAt:  now,
	})

	if completed {
		return
	}
}

func (a *App) markTurnInterrupted(sessionID, turnID string) {
	now := model.NowString()
	a.finalizeInterruptedStreamingAssistantCard(sessionID, now)
	a.cancelRemoteExecsForSession(sessionID, "任务已中断")
	a.finishRuntimeTurn(sessionID, "aborted")
	a.appendIncidentEvent(sessionID, "cancel.requested", "completed", "任务已中断", "用户停止了当前任务", map[string]any{
		"turnId": emptyToNil(strings.TrimSpace(turnID)),
	})
	a.finalizeOpenTurnCards(sessionID, "failed")
	a.resolvePendingTurnRequests(sessionID, now)
	a.resolveMirroredPendingTurnRequests(sessionID, "cancelled", "任务已中断")
	cardID := model.NewID("notice")
	if turnID != "" {
		cardID = "turn-aborted-" + turnID
	}
	a.store.UpsertCard(sessionID, model.Card{
		ID:        cardID,
		Type:      "NoticeCard",
		Title:     "任务已中断",
		Text:      "任务已中断",
		Status:    "notice",
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (a *App) finalizeInterruptedStreamingAssistantCard(sessionID, now string) {
	if strings.TrimSpace(now) == "" {
		now = model.NowString()
	}
	if bifrostSession, ok := a.bifrostSession(sessionID); ok && bifrostSession != nil {
		if cardID := strings.TrimSpace(bifrostSession.CurrentCardID()); cardID != "" {
			a.store.UpdateCard(sessionID, cardID, func(card *model.Card) {
				if card.Type != "AssistantMessageCard" || normalizeCardStatus(card.Status) != "inProgress" {
					return
				}
				card.Status = "cancelled"
				card.UpdatedAt = now
			})
			bifrostSession.SetCurrentCardID("")
		}
	}
	session := a.store.Session(sessionID)
	if session == nil {
		return
	}
	for _, existing := range session.Cards {
		if existing.Type != "AssistantMessageCard" || normalizeCardStatus(existing.Status) != "inProgress" {
			continue
		}
		cardID := existing.ID
		a.store.UpdateCard(sessionID, cardID, func(card *model.Card) {
			if card.Type != "AssistantMessageCard" || normalizeCardStatus(card.Status) != "inProgress" {
				return
			}
			card.Status = "cancelled"
			card.UpdatedAt = now
		})
	}
}

func (a *App) cleanBackgroundTerminals(threadID string) {
	a.cleanBackgroundTerminalsWithTimeout(threadID, 5*time.Second)
}

func (a *App) cleanBackgroundTerminalsWithTimeout(threadID string, timeout time.Duration) {
	if strings.TrimSpace(threadID) == "" {
		return
	}
}

func (a *App) finalizeOpenTurnCards(sessionID, finalStatus string) {
	session := a.store.Session(sessionID)
	if session == nil {
		return
	}

	now := model.NowString()
	for _, existing := range session.Cards {
		if normalizeCardStatus(existing.Status) != "inProgress" && existing.Status != "pending" {
			continue
		}
		switch existing.Type {
		case "CommandCard", "FileChangeCard", "ProcessLineCard", "CommandApprovalCard", "FileChangeApprovalCard", "ChoiceCard":
			cardID := existing.ID
			durationMS := durationBetween(existing.CreatedAt, now)
			cardStatus, terminalText := finalizeOpenTurnCardStatus(existing, finalStatus)
			a.store.UpdateCard(sessionID, cardID, func(card *model.Card) {
				card.Status = cardStatus
				if terminalText != "" {
					switch card.Type {
					case "CommandCard":
						card.Output = appendExecMessage(card.Output, terminalText)
						if card.Summary == "" {
							card.Summary = terminalText
						}
					case "FileChangeCard":
						if strings.TrimSpace(card.Text) == "" {
							card.Text = terminalText
						}
					}
				}
				if card.DurationMS == 0 {
					card.DurationMS = durationMS
				}
				card.UpdatedAt = now
			})
		}
	}
}

func finalizeOpenTurnCardStatus(card model.Card, finalStatus string) (string, string) {
	if finalStatus != "completed" {
		return finalStatus, ""
	}
	if normalizeCardStatus(card.Status) != "inProgress" {
		return finalStatus, ""
	}
	switch card.Type {
	case "CommandCard":
		return "failed", "任务已结束，但这条命令没有返回最终结果，已按失败处理。"
	case "FileChangeCard":
		return "failed", "任务已结束，但这次文件修改没有返回最终结果，已按失败处理。"
	default:
		return finalStatus, ""
	}
}

func (a *App) resolvePendingTurnRequests(sessionID, resolvedAt string) {
	session := a.store.Session(sessionID)
	if session == nil {
		return
	}
	for approvalID, approval := range session.Approvals {
		if approval.Status == "pending" {
			a.store.ResolveApproval(sessionID, approvalID, "cancelled", resolvedAt)
		}
	}
	for choiceID, choice := range session.Choices {
		if choice.Status == "pending" {
			a.store.ResolveChoice(sessionID, choiceID, "cancelled", resolvedAt)
		}
	}
}

func (a *App) cardByID(sessionID, cardID string) *model.Card {
	session := a.store.Session(sessionID)
	if session == nil {
		return nil
	}
	for _, card := range session.Cards {
		if card.ID == cardID {
			copyCard := card
			return &copyCard
		}
	}
	return nil
}

func (a *App) cardDurationMS(sessionID, cardID, endedAt string) int64 {
	card := a.cardByID(sessionID, cardID)
	if card == nil {
		return 0
	}
	return durationBetween(card.CreatedAt, endedAt)
}

func processLineText(kind string, entry model.ActivityEntry, currentLabel string, completed bool) string {
	if completed {
		switch kind {
		case "file_read":
			return "已浏览 " + currentLabel
		case "file_search":
			return "已搜索文件（" + currentLabel + "）"
		case "web_search":
			return "已搜索网页（" + currentLabel + "）"
		case "web_open":
			return "已打开网页（" + currentLabel + "）"
		case "web_find":
			return "已页内查找（" + currentLabel + "）"
		case "list":
			return "已列出 " + currentLabel
		case "command":
			return "已处理 1 个命令"
		default:
			return strings.TrimSpace(entry.Label)
		}
	}
	switch kind {
	case "file_read":
		return "现在浏览 " + currentLabel
	case "file_search":
		return "现在搜索文件（" + currentLabel + "）"
	case "web_search":
		return "现在搜索网页（" + currentLabel + "）"
	case "web_open":
		return "现在打开网页（" + currentLabel + "）"
	case "web_find":
		return "现在页内查找（" + currentLabel + "）"
	case "list":
		return "现在列出 " + currentLabel
	case "command":
		return "现在执行命令（" + currentLabel + "）"
	default:
		return strings.TrimSpace(entry.Label)
	}
}

func isTaskCompletionText(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(strings.Trim(value, "- ")))
	switch normalized {
	case "status: completed", "completed", "turn completed":
		return true
	default:
		return false
	}
}

func durationBetween(startedAt, endedAt string) int64 {
	if startedAt == "" || endedAt == "" {
		return 0
	}
	startTime, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		return 0
	}
	endTime, err := time.Parse(time.RFC3339, endedAt)
	if err != nil {
		return 0
	}
	if endTime.Before(startTime) {
		return 0
	}
	return endTime.Sub(startTime).Milliseconds()
}

func autoApprovalNoticeText(approval model.ApprovalRequest) string {
	if (approval.Type == "command" || approval.Type == "remote_command") && approval.Command != "" {
		return fmt.Sprintf("已自动批准本会话内同类命令：%s", truncate(approval.Command, 72))
	}
	if approval.Type == "file_change" || approval.Type == "remote_file_change" {
		return "已自动批准本会话内同类文件修改。"
	}
	return "已自动批准本会话内同类操作。"
}

func approvalMemoCard(host model.Host, approval model.ApprovalRequest, decision, now string) model.Card {
	return model.Card{
		ID:        "approval-memo-" + approval.ID,
		Type:      "NoticeCard",
		Text:      approvalMemoText(host, approval, decision),
		Status:    approvalStatusFromDecision(decision),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func approvalMemoText(host model.Host, approval model.ApprovalRequest, decision string) string {
	hostName := strings.TrimSpace(host.Name)
	if hostName == "" {
		hostName = strings.TrimSpace(approval.HostID)
	}
	if hostName == "" {
		hostName = "当前主机"
	}

	prefix := "已同意在"
	switch decision {
	case "accept_session":
		prefix = "已同意并记住在"
	case "decline":
		prefix = "已拒绝在"
	}

	action := "执行远程操作"
	switch approval.Type {
	case "remote_command", "command":
		if approval.Command != "" {
			action = "执行：" + truncate(approval.Command, 88)
		}
	case "remote_file_change", "file_change":
		if len(approval.Changes) == 1 {
			action = "修改文件：" + truncate(approval.Changes[0].Path, 88)
		} else if len(approval.Changes) > 1 {
			action = fmt.Sprintf("修改 %d 个文件（%s 等）", len(approval.Changes), truncate(approval.Changes[0].Path, 48))
		} else {
			action = "修改远程文件"
		}
	}

	return fmt.Sprintf("%s %s %s", prefix, hostName, action)
}

func (a *App) auditApprovalRequested(sessionID string, approval model.ApprovalRequest, fields map[string]any) {
	a.auditApprovalLifecycleEvent("approval.requested", sessionID, approval, "", approval.Status, approval.RequestedAt, "", fields)
}

func (a *App) auditApprovalLifecycleEvent(event, sessionID string, approval model.ApprovalRequest, approvalDecision, status, startedAt, endedAt string, fields map[string]any) {
	threadID := firstNonEmptyString([]string{approval.ThreadID, a.sessionThreadID(sessionID)})
	turnID := firstNonEmptyString([]string{approval.TurnID, a.sessionTurnID(sessionID)})
	hostID := defaultHostID(strings.TrimSpace(approval.HostID))
	hostName := hostNameOrID(a.findHost(approval.HostID))
	operator := a.auditOperator(sessionID)
	toolName := a.approvalResolvedToolName(sessionID, approval)
	meta := a.approvalLifecycleMetadata(sessionID, approval, fields)

	payload := map[string]any{
		"sessionId":        sessionID,
		"approvalId":       approval.ID,
		"type":             approval.Type,
		"threadId":         threadID,
		"turnId":           turnID,
		"hostId":           hostID,
		"hostName":         hostName,
		"operator":         operator,
		"toolName":         toolName,
		"command":          emptyToNil(strings.TrimSpace(approval.Command)),
		"filePath":         approvalAuditFilePath(approval, fields),
		"cwd":              a.approvalAuditCwd(approval),
		"approvalDecision": emptyToNil(strings.TrimSpace(approvalDecision)),
		"startedAt":        emptyToNil(strings.TrimSpace(startedAt)),
		"endedAt":          emptyToNil(strings.TrimSpace(endedAt)),
		"status":           emptyToNil(strings.TrimSpace(status)),
		"exitCode":         nil,
	}
	if approval.Reason != "" {
		payload["reason"] = emptyToNil(strings.TrimSpace(approval.Reason))
	}
	if approval.GrantRoot != "" {
		payload["grantRoot"] = emptyToNil(strings.TrimSpace(approval.GrantRoot))
	}
	if approval.Fingerprint != "" {
		payload["fingerprint"] = emptyToNil(strings.TrimSpace(approval.Fingerprint))
	}
	for key, value := range fields {
		payload[key] = value
	}
	a.audit(event, payload)

	// Also write structured record to ApprovalAuditStore.
	if a.approvalAuditStore != nil {
		sessionKind := model.SessionKindSingleHost
		if session := a.store.Session(sessionID); session != nil {
			if k := strings.TrimSpace(session.Meta.Kind); k != "" {
				sessionKind = k
			}
		}
		record := model.ApprovalAuditRecord{
			ID:           model.NewID("aaudit"),
			Event:        event,
			SessionID:    sessionID,
			SessionKind:  sessionKind,
			ThreadID:     threadID,
			TurnID:       turnID,
			HostID:       hostID,
			HostName:     hostName,
			Operator:     operator,
			ApprovalID:   approval.ID,
			ApprovalType: approval.Type,
			ToolName:     toolName,
			Command:      strings.TrimSpace(approval.Command),
			Cwd:          anyToString(a.approvalAuditCwd(approval)),
			FilePath:     anyToString(approvalAuditFilePath(approval, fields)),
			Decision:     strings.TrimSpace(approvalDecision),
			Status:       strings.TrimSpace(status),
			GrantMode:    approvalGrantMode(approvalDecision),
			Fingerprint:  strings.TrimSpace(approval.Fingerprint),
			StartedAt:    strings.TrimSpace(startedAt),
			EndedAt:      strings.TrimSpace(endedAt),
			Meta:         meta,
		}
		if err := a.approvalAuditStore.Add(record); err != nil {
			log.Printf("approval audit store add failed event=%s approval=%s err=%s", event, approval.ID, truncate(err.Error(), 200))
		}
	}
	a.recordApprovalIncidentEvent(sessionID, event, approval, approvalDecision, status, startedAt, endedAt, meta)
}

func (a *App) sessionThreadID(sessionID string) string {
	session := a.store.Session(sessionID)
	if session == nil {
		return ""
	}
	return strings.TrimSpace(session.ThreadID)
}

func (a *App) sessionTurnID(sessionID string) string {
	session := a.store.Session(sessionID)
	if session == nil {
		return ""
	}
	return strings.TrimSpace(session.TurnID)
}

func (a *App) auditOperator(sessionID string) string {
	session := a.store.Session(sessionID)
	if session == nil {
		return "unknown"
	}
	if email := strings.TrimSpace(session.Auth.Email); email != "" {
		return email
	}
	if email := strings.TrimSpace(session.Tokens.Email); email != "" {
		return email
	}
	if mode := strings.TrimSpace(session.Auth.Mode); mode != "" {
		return mode
	}
	if session.AuthSessionID != "" {
		return session.AuthSessionID
	}
	return "unknown"
}

func approvalAuditToolName(approval model.ApprovalRequest) string {
	switch approval.Type {
	case "command":
		return "commandExecution"
	case "file_change":
		return "fileChange"
	case "remote_command", "remote_file_change":
		return "execute_system_mutation"
	default:
		return approval.Type
	}
}

func approvalAuditFilePath(approval model.ApprovalRequest, fields map[string]any) any {
	if fields != nil {
		if value, ok := fields["filePath"]; ok {
			return value
		}
	}
	if approval.Type == "file_change" || approval.Type == "remote_file_change" {
		if len(approval.Changes) > 0 {
			return emptyToNil(strings.TrimSpace(approval.Changes[0].Path))
		}
	}
	return nil
}

func (a *App) approvalAuditCwd(approval model.ApprovalRequest) any {
	switch approval.Type {
	case "file_change", "remote_file_change":
		if cwd := strings.TrimSpace(approval.Cwd); cwd != "" {
			return emptyToNil(cwd)
		}
		if grantRoot := strings.TrimSpace(approval.GrantRoot); grantRoot != "" {
			return emptyToNil(grantRoot)
		}
	case "command", "remote_command":
		if cwd := strings.TrimSpace(approval.Cwd); cwd != "" {
			return emptyToNil(cwd)
		}
	}
	return emptyToNil(strings.TrimSpace(approval.Cwd))
}

func approvalGrantFromApproval(approval model.ApprovalRequest) model.ApprovalGrant {
	return model.ApprovalGrant{
		ID:          model.NewID("grant"),
		HostID:      approval.HostID,
		Type:        approval.Type,
		Fingerprint: approval.Fingerprint,
		Command:     approval.Command,
		Cwd:         approval.Cwd,
		CreatedAt:   model.NowString(),
	}
}

func approvalGrantMode(decision string) string {
	switch decision {
	case "accept_session":
		return "session"
	case "accept":
		return "none"
	default:
		return ""
	}
}

func anyToString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func mapApprovalDecision(decision string, approval model.ApprovalRequest) string {
	switch decision {
	case "accept", "accept_session":
		return "accept"
	case "decline", "reject":
		if slices.Contains(approval.Decisions, "decline") {
			return "decline"
		}
		if slices.Contains(approval.Decisions, "cancel") {
			return "cancel"
		}
	}
	return decision
}

func approvalStatusFromDecision(decision string) string {
	if decision == "reject" {
		return "decline"
	}
	if decision == "accept_session" {
		return "accepted_for_session"
	}
	return decision
}

func planExitApprovalToolResponse(approval model.ApprovalRequest, decision, codexDecision string) map[string]any {
	approved := codexDecision == "accept"
	payload := map[string]any{
		"tool":          "exit_plan_mode",
		"approval_type": "plan_exit",
		"approval_id":   approval.ID,
		"decision":      codexDecision,
		"raw_decision":  decision,
		"approved":      approved,
	}
	if approved {
		payload["next_mode"] = "execute"
		payload["instruction"] = "The user approved the plan. You may proceed only within the approved plan scope and dispatch workers only for approved tasks."
		return structuredToolResponse(payload, true)
	}
	payload["next_mode"] = "plan"
	payload["instruction"] = "The user declined the plan. Do not execute or dispatch workers. Continue in plan mode: revise the plan or ask the user what to change."
	return structuredToolResponse(payload, true)
}

func commandPolicyDecisionIsMutation(decision commandPolicyDecision) bool {
	if !decision.Readonly {
		return true
	}
	return strings.HasSuffix(strings.TrimSpace(decision.Category), "_mutation")
}

func (a *App) blockApprovalByPlanMode(sessionID string, approval model.ApprovalRequest, message string) {
	now := model.NowString()
	approval.Status = "blocked_by_plan_mode"
	approval.ResolvedAt = now
	a.store.AddApproval(sessionID, approval)
	a.store.ResolveApproval(sessionID, approval.ID, approval.Status, now)
	a.setRuntimeTurnPhase(sessionID, "planning")
	a.auditApprovalLifecycleEvent("approval.decision", sessionID, approval, "decline", approval.Status, approval.RequestedAt, now, map[string]any{
		"blockedByPlanMode": true,
		"reason":            message,
	})
	a.store.UpsertCard(sessionID, model.Card{
		ID:        model.NewID("error"),
		Type:      "ErrorCard",
		Title:     "Plan mode blocked mutation",
		Message:   message,
		Text:      message,
		Status:    "failed",
		CreatedAt: now,
		UpdatedAt: now,
	})
	a.broadcastSnapshot(sessionID)
}

func (a *App) rejectApprovalByPlanMode(sessionID, rawID string, approval model.ApprovalRequest, message string) {
	a.blockApprovalByPlanMode(sessionID, approval, message)
	_ = a.respondCodex(context.Background(), rawID, map[string]any{
		"decision": "decline",
	})
}

func approvalFingerprintForCommand(hostID, command, cwd string) string {
	return strings.Join([]string{"command", hostID, cwd, command}, "|")
}

func approvalFingerprintForFileChange(hostID, grantRoot string, changes []model.FileChange) string {
	parts := make([]string, 0, len(changes))
	for _, change := range changes {
		parts = append(parts, change.Path+":"+change.Kind)
	}
	slices.Sort(parts)
	return strings.Join([]string{"file_change", hostID, grantRoot, strings.Join(parts, ",")}, "|")
}

func (a *App) ensureThread(ctx context.Context, sessionID string) (string, error) {
	session := a.store.EnsureSession(sessionID)
	if session.ThreadID != "" {
		return session.ThreadID, nil
	}
	return a.ensureThreadWithSpec(ctx, sessionID, a.buildSingleHostReActThreadStartSpec(ctx, sessionID))
}

func (a *App) ensureThreadWithSpec(ctx context.Context, sessionID string, spec threadStartSpec) (string, error) {
	if a.runtimeStartThreadFunc != nil {
		session := a.store.EnsureSession(sessionID)
		if session.ThreadID != "" {
			return session.ThreadID, nil
		}
		startedAt := time.Now()
		requestID := firstNonEmptyValue(a.turnTraceRequestID(sessionID), "-")
		a.markTurnTraceThreadStartBegin(sessionID)
		log.Printf(
			"runtime hook thread start begin session=%s request=%s cwd=%s model=%s sandbox=%s approval=%s dynamic_tools=%d",
			sessionID,
			requestID,
			spec.Cwd,
			spec.Model,
			spec.SandboxMode,
			spec.ApprovalPolicy,
			len(spec.DynamicTools),
		)
		threadID, err := a.runtimeStartThreadFunc(ctx, sessionID, spec)
		if err != nil {
			log.Printf("runtime hook thread start failed session=%s request=%s err=%v duration=%s", sessionID, requestID, err, time.Since(startedAt))
			return "", err
		}
		a.store.SetThread(sessionID, threadID)
		a.store.SetThreadConfigHash(sessionID, spec.ThreadConfigHash)
		a.markTurnTraceThreadStarted(sessionID, threadID)
		log.Printf("runtime hook thread start ok session=%s request=%s thread=%s duration=%s", sessionID, requestID, threadID, time.Since(startedAt))
		a.broadcastSnapshot(sessionID)
		return threadID, nil
	}
	if !a.useBifrost() {
		return "", errors.New("bifrost runtime is not initialized")
	}
	storeSession := a.store.EnsureSession(sessionID)
	expectedHash := strings.TrimSpace(spec.ThreadConfigHash)
	if existing, ok := a.bifrostSession(sessionID); ok && existing != nil && strings.TrimSpace(storeSession.ThreadConfigHash) == expectedHash {
		if binding := strings.TrimSpace(storeSession.ThreadID); binding != "" {
			return binding, nil
		}
	}
	startedAt := time.Now()
	requestID := firstNonEmptyValue(a.turnTraceRequestID(sessionID), "-")
	a.markTurnTraceThreadStartBegin(sessionID)
	log.Printf(
		"bifrost thread bind begin session=%s request=%s cwd=%s model=%s sandbox=%s approval=%s dynamic_tools=%d",
		sessionID,
		requestID,
		spec.Cwd,
		spec.Model,
		spec.SandboxMode,
		spec.ApprovalPolicy,
		len(spec.DynamicTools),
	)
	session := agentloop.NewSession(sessionID, bifrostSessionSpecFromThreadSpec(spec, a.cfg.LLMModel))
	a.setBifrostSession(sessionID, session)
	threadID := syntheticBifrostThreadID(sessionID, expectedHash)
	a.store.SetThread(sessionID, threadID)
	a.store.SetThreadConfigHash(sessionID, expectedHash)
	a.markTurnTraceThreadStarted(sessionID, threadID)
	log.Printf("bifrost thread bind ok session=%s request=%s thread=%s duration=%s", sessionID, requestID, threadID, time.Since(startedAt))
	a.broadcastSnapshot(sessionID)
	return threadID, nil
}

func (a *App) requestTurnWithSpec(ctx context.Context, sessionID, threadID string, spec turnStartSpec) error {
	if a.runtimeStartTurnFunc != nil {
		startedAt := time.Now()
		requestID := firstNonEmptyValue(a.turnTraceRequestID(sessionID), "-")
		a.markTurnTraceTurnStartBegin(sessionID, threadID)
		log.Printf(
			"runtime hook turn start begin session=%s request=%s thread=%s cwd=%s sandbox=%s approval=%s input_items=%d reasoning=%s",
			sessionID,
			requestID,
			threadID,
			spec.Cwd,
			spec.SandboxMode,
			spec.ApprovalPolicy,
			len(spec.Input),
			spec.ReasoningEffort,
		)
		turnID, err := a.runtimeStartTurnFunc(ctx, sessionID, threadID, spec)
		if err != nil {
			log.Printf("runtime hook turn start failed session=%s request=%s thread=%s err=%v duration=%s", sessionID, requestID, threadID, err, time.Since(startedAt))
			return err
		}
		if turnID != "" {
			a.store.SetTurn(sessionID, turnID)
			a.scheduleTurnStallMonitor(sessionID, stalledTurnTimeout)
			a.markTurnTraceTurnStarted(sessionID, threadID, turnID)
			log.Printf("runtime hook turn start ok session=%s request=%s thread=%s turn=%s duration=%s", sessionID, requestID, threadID, turnID, time.Since(startedAt))
			return nil
		}
		a.markTurnTraceTurnStarted(sessionID, threadID, "")
		log.Printf("runtime hook turn start ok session=%s request=%s thread=%s turn=<missing> duration=%s", sessionID, requestID, threadID, time.Since(startedAt))
		return nil
	}
	if !a.useBifrostForSession(sessionID) {
		return errors.New("bifrost runtime is not initialized")
	}
	session, ok := a.bifrostSession(sessionID)
	if !ok || session == nil {
		return errors.New("bifrost session is not initialized")
	}
	inputText := turnStartInputText(spec)
	if strings.TrimSpace(inputText) == "" {
		return errors.New("turn input is required")
	}
	startedAt := time.Now()
	requestID := firstNonEmptyValue(a.turnTraceRequestID(sessionID), "-")
	a.markTurnTraceTurnStartBegin(sessionID, threadID)
	turnID := model.NewID("turn")
	a.store.SetTurn(sessionID, turnID)
	session.SetCancelFunc(nil)
	log.Printf(
		"bifrost turn start begin session=%s request=%s thread=%s cwd=%s sandbox=%s approval=%s input_items=%d reasoning=%s",
		sessionID,
		requestID,
		threadID,
		spec.Cwd,
		spec.SandboxMode,
		spec.ApprovalPolicy,
		len(spec.Input),
		spec.ReasoningEffort,
	)
	a.markTurnTraceTurnStarted(sessionID, threadID, turnID)
	if err := a.agentLoop.RunTurn(ctx, session, inputText); err != nil {
		log.Printf("bifrost turn start failed session=%s request=%s thread=%s err=%v duration=%s", sessionID, requestID, threadID, err, time.Since(startedAt))
		return err
	}
	log.Printf("bifrost turn start ok session=%s request=%s thread=%s turn=%s duration=%s", sessionID, requestID, threadID, turnID, time.Since(startedAt))
	return nil
}

func (a *App) requestTurn(ctx context.Context, sessionID, threadID string, req chatRequest) error {
	return a.requestTurnWithSpec(ctx, sessionID, threadID, a.buildSingleHostReActTurnStartSpec(ctx, sessionID, req))
}

func syntheticBifrostThreadID(sessionID, hash string) string {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		hash = model.NewID("thread")
	}
	if len(hash) > 12 {
		hash = hash[:12]
	}
	return "bifrost:" + strings.TrimSpace(sessionID) + ":" + hash
}

func (a *App) agentProfileIDFromRequest(r *http.Request) string {
	profileID := strings.TrimSpace(r.URL.Query().Get("profileId"))
	if profileID == "" {
		profileID = string(model.AgentProfileTypeMainAgent)
	}
	return profileID
}

func (a *App) mainAgentProfile() model.AgentProfile {
	profile, ok := a.store.AgentProfile(string(model.AgentProfileTypeMainAgent))
	if !ok {
		return model.DefaultAgentProfile(string(model.AgentProfileTypeMainAgent))
	}
	return model.CompleteAgentProfile(profile)
}

func (a *App) mainAgentWritableRoots(profile model.AgentProfile) []string {
	roots := make([]string, 0, len(profile.CommandPermissions.AllowedWritableRoots))
	for _, root := range profile.CommandPermissions.AllowedWritableRoots {
		if trimmed := strings.TrimSpace(root); trimmed != "" {
			roots = append(roots, trimmed)
		}
	}
	if len(roots) == 0 {
		roots = append(roots, a.cfg.DefaultWorkspace)
	}
	return roots
}

func (a *App) renderMainAgentDeveloperInstructions(profile model.AgentProfile, hostID string, turnScoped bool) string {
	hostID = defaultHostID(hostID)
	sections := []string{strings.TrimSpace(profile.SystemPrompt.Content)}
	contextLines := []string{
		"You are embedded inside a web AI ops console.",
		fmt.Sprintf("Operate only on the selected host %q.", hostID),
		fmt.Sprintf("Use %q as the default writable workspace.", a.cfg.DefaultWorkspace),
		fmt.Sprintf("Writable roots: %s.", strings.Join(a.mainAgentWritableRoots(profile), ", ")),
	}
	if turnScoped {
		contextLines = append(contextLines, "Summarize execution results clearly for the web UI.")
	}
	sections = append(sections, strings.Join(contextLines, "\n"))
	if enabled := defaultEnabledAgentSkillNames(profile); len(enabled) > 0 {
		sections = append(sections, "Default-enabled skills:\n- "+strings.Join(enabled, "\n- "))
	}
	if explicit := explicitOnlyAgentSkillNames(profile); len(explicit) > 0 {
		sections = append(sections, "Explicit-only skills (use them only when the user explicitly asks or the workflow requires them):\n- "+strings.Join(explicit, "\n- "))
	}
	if enabled := a.enabledAgentMCPNames(profile, hostID); len(enabled) > 0 {
		sections = append(sections, "Enabled MCP connectors:\n- "+strings.Join(enabled, "\n- "))
	}
	if gated := a.explicitApprovalMCPNames(profile, hostID); len(gated) > 0 {
		sections = append(sections, "The following MCP connectors require explicit user approval before any write or state-changing action:\n- "+strings.Join(gated, "\n- "))
	}
	if isRemoteHostID(hostID) {
		sections = append(sections, remoteThreadDeveloperInstructions(hostID))
	} else {
		sections = append(sections, localThreadDeveloperInstructions())
	}
	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func (a *App) mergeAndValidateAgentProfile(sessionID, requestedProfileID string, incoming model.AgentProfile, riskConfirmed bool) (model.AgentProfile, model.AgentProfile, error) {
	profileID := strings.TrimSpace(incoming.ID)
	if profileID == "" {
		profileID = strings.TrimSpace(requestedProfileID)
	}
	if profileID == "" {
		profileID = string(model.AgentProfileTypeMainAgent)
	}
	current, ok := a.store.AgentProfile(profileID)
	if !ok {
		current = model.DefaultAgentProfile(profileID)
	}
	merged := current
	if strings.TrimSpace(incoming.ID) != "" {
		merged.ID = strings.TrimSpace(incoming.ID)
	}
	if strings.TrimSpace(incoming.Name) != "" {
		merged.Name = strings.TrimSpace(incoming.Name)
	}
	if strings.TrimSpace(incoming.Type) != "" {
		merged.Type = strings.TrimSpace(incoming.Type)
	}
	merged.Description = strings.TrimSpace(incoming.Description)
	if strings.TrimSpace(incoming.Runtime.Model) != "" {
		merged.Runtime.Model = strings.TrimSpace(incoming.Runtime.Model)
	}
	if strings.TrimSpace(incoming.Runtime.ReasoningEffort) != "" {
		merged.Runtime.ReasoningEffort = strings.TrimSpace(incoming.Runtime.ReasoningEffort)
	}
	if strings.TrimSpace(incoming.Runtime.ApprovalPolicy) != "" {
		merged.Runtime.ApprovalPolicy = strings.TrimSpace(incoming.Runtime.ApprovalPolicy)
	}
	if strings.TrimSpace(incoming.Runtime.SandboxMode) != "" {
		merged.Runtime.SandboxMode = strings.TrimSpace(incoming.Runtime.SandboxMode)
	}
	if incoming.SystemPrompt.Content != "" {
		merged.SystemPrompt.Content = strings.TrimSpace(incoming.SystemPrompt.Content)
	}
	if incoming.SystemPrompt.Version != "" {
		merged.SystemPrompt.Version = strings.TrimSpace(incoming.SystemPrompt.Version)
	}
	merged.SystemPrompt.Notes = strings.TrimSpace(incoming.SystemPrompt.Notes)
	if incoming.CommandPermissions.Enabled != nil {
		merged.CommandPermissions.Enabled = cloneBoolPtr(incoming.CommandPermissions.Enabled)
	}
	if incoming.CommandPermissions.AllowShellWrapper != nil {
		merged.CommandPermissions.AllowShellWrapper = cloneBoolPtr(incoming.CommandPermissions.AllowShellWrapper)
	}
	if incoming.CommandPermissions.AllowSudo != nil {
		merged.CommandPermissions.AllowSudo = cloneBoolPtr(incoming.CommandPermissions.AllowSudo)
	}
	if incoming.CommandPermissions.DefaultMode != "" {
		merged.CommandPermissions.DefaultMode = strings.TrimSpace(incoming.CommandPermissions.DefaultMode)
	}
	if incoming.CommandPermissions.DefaultTimeoutSeconds > 0 {
		merged.CommandPermissions.DefaultTimeoutSeconds = incoming.CommandPermissions.DefaultTimeoutSeconds
	}
	if incoming.CommandPermissions.AllowedWritableRoots != nil {
		merged.CommandPermissions.AllowedWritableRoots = append([]string(nil), incoming.CommandPermissions.AllowedWritableRoots...)
	}
	if incoming.CommandPermissions.CategoryPolicies != nil {
		merged.CommandPermissions.CategoryPolicies = cloneStringMap(incoming.CommandPermissions.CategoryPolicies)
	}
	if incoming.CapabilityPermissions != (model.AgentCapabilityPermissions{}) {
		merged.CapabilityPermissions = incoming.CapabilityPermissions
	}
	if incoming.Skills != nil {
		merged.Skills = append([]model.AgentSkill(nil), incoming.Skills...)
	}
	if incoming.MCPs != nil {
		merged.MCPs = append([]model.AgentMCP(nil), incoming.MCPs...)
	}
	merged = model.CompleteAgentProfile(merged)
	merged.UpdatedAt = model.NowString()
	merged.UpdatedBy = a.auditOperator(sessionID)
	if err := a.validateAgentProfile(merged); err != nil {
		return model.AgentProfile{}, model.AgentProfile{}, err
	}
	if err := validateAgentProfileRiskChange(current, merged, riskConfirmed); err != nil {
		return model.AgentProfile{}, model.AgentProfile{}, err
	}
	return merged, current, nil
}

func newAgentProfileValidationError(fieldErrors map[string]string) error {
	if len(fieldErrors) == 0 {
		return nil
	}
	message := "agent profile validation failed"
	if len(fieldErrors) == 1 {
		for _, item := range fieldErrors {
			message = item
		}
	}
	return agentProfileValidationError{
		message:     message,
		fieldErrors: cloneStringMap(fieldErrors),
	}
}

func validateAgentProfileRiskChange(before, after model.AgentProfile, riskConfirmed bool) error {
	fieldErrors := detectHighRiskProfileChanges(before, after)
	if len(fieldErrors) == 0 || riskConfirmed {
		return nil
	}
	return agentProfileValidationError{
		message:     "high-risk profile changes require explicit confirmation",
		fieldErrors: fieldErrors,
	}
}

func detectHighRiskProfileChanges(before, after model.AgentProfile) map[string]string {
	before = model.CompleteAgentProfile(before)
	after = model.CompleteAgentProfile(after)
	fieldErrors := make(map[string]string)
	add := func(field, message string) {
		if _, exists := fieldErrors[field]; exists {
			return
		}
		fieldErrors[field] = message
	}
	if !boolValue(before.CommandPermissions.AllowSudo, false) && boolValue(after.CommandPermissions.AllowSudo, false) {
		add("commandPermissions.allowSudo", "allowSudo 从关闭改为开启，需要显式确认高风险变更")
	}
	if before.Runtime.SandboxMode != "danger-full-access" && after.Runtime.SandboxMode == "danger-full-access" {
		add("runtime.sandboxMode", "sandboxMode 切到 danger-full-access，需要显式确认高风险变更")
	}
	for _, category := range []string{"filesystem_mutation", "service_mutation", "package_mutation"} {
		beforeMode := before.CommandPermissions.CategoryPolicies[category]
		afterMode := after.CommandPermissions.CategoryPolicies[category]
		if beforeMode != model.AgentPermissionModeAllow && afterMode == model.AgentPermissionModeAllow {
			add("commandPermissions.categoryPolicies."+category, fmt.Sprintf("%s 改为 allow，需要显式确认高风险变更", category))
		}
	}
	beforeCaps := map[string]string{
		"commandExecution": before.CapabilityPermissions.CommandExecution,
		"fileChange":       before.CapabilityPermissions.FileChange,
		"terminal":         before.CapabilityPermissions.Terminal,
		"webOpen":          before.CapabilityPermissions.WebOpen,
	}
	afterCaps := map[string]string{
		"commandExecution": after.CapabilityPermissions.CommandExecution,
		"fileChange":       after.CapabilityPermissions.FileChange,
		"terminal":         after.CapabilityPermissions.Terminal,
		"webOpen":          after.CapabilityPermissions.WebOpen,
	}
	for key, afterState := range afterCaps {
		beforeState := beforeCaps[key]
		if beforeState != model.AgentCapabilityEnabled && afterState == model.AgentCapabilityEnabled {
			add("capabilityPermissions."+key, fmt.Sprintf("%s 改为 enabled，需要显式确认高风险变更", key))
		}
	}
	for _, item := range after.MCPs {
		if !item.Enabled {
			continue
		}
		beforeItem, ok := findAgentMCP(before.MCPs, item.ID)
		if !ok {
			beforeItem = model.AgentMCP{}
		}
		if model.NormalizeAgentMCPPermission(beforeItem.Permission) != model.AgentMCPPermissionReadwrite &&
			model.NormalizeAgentMCPPermission(item.Permission) == model.AgentMCPPermissionReadwrite &&
			!item.RequiresExplicitUserApproval {
			add("mcps."+item.ID, fmt.Sprintf("%s 开启 readwrite 且未要求显式审批，需要确认高风险变更", item.ID))
		}
	}
	return fieldErrors
}

func findAgentMCP(items []model.AgentMCP, id string) (model.AgentMCP, bool) {
	for _, item := range items {
		if strings.TrimSpace(item.ID) == strings.TrimSpace(id) {
			return item, true
		}
	}
	return model.AgentMCP{}, false
}

func defaultEnabledAgentSkillNames(profile model.AgentProfile) []string {
	names := make([]string, 0, len(profile.Skills))
	for _, item := range profile.Skills {
		if !item.Enabled || model.NormalizeAgentSkillActivationMode(item.ActivationMode) != model.AgentSkillActivationDefault {
			continue
		}
		if label := strings.TrimSpace(item.Name); label != "" {
			names = append(names, label)
			continue
		}
		if id := strings.TrimSpace(item.ID); id != "" {
			names = append(names, id)
		}
	}
	return names
}

func explicitOnlyAgentSkillNames(profile model.AgentProfile) []string {
	names := make([]string, 0, len(profile.Skills))
	for _, item := range profile.Skills {
		if !item.Enabled || model.NormalizeAgentSkillActivationMode(item.ActivationMode) != model.AgentSkillActivationExplicit {
			continue
		}
		if label := strings.TrimSpace(item.Name); label != "" {
			names = append(names, label)
			continue
		}
		if id := strings.TrimSpace(item.ID); id != "" {
			names = append(names, id)
		}
	}
	return names
}

func requiredCapabilityForMCP(item model.AgentMCP) string {
	switch strings.TrimSpace(item.ID) {
	case "filesystem", "host-files", "host-logs":
		return "fileRead"
	case "docs":
		return "webSearch"
	case "metrics":
		return "commandExecution"
	default:
		return ""
	}
}

func (a *App) effectiveEnabledAgentMCPs(profile model.AgentProfile, hostID string) []model.AgentMCP {
	items := make([]model.AgentMCP, 0, len(profile.MCPs))
	for _, item := range profile.MCPs {
		if !item.Enabled {
			continue
		}
		requiredCapability := requiredCapabilityForMCP(item)
		if requiredCapability != "" && capabilityDisabled(a.effectiveCapabilityState(hostID, requiredCapability)) {
			continue
		}
		items = append(items, item)
	}
	return items
}

func (a *App) enabledAgentMCPNames(profile model.AgentProfile, hostID string) []string {
	names := make([]string, 0, len(profile.MCPs))
	for _, item := range a.effectiveEnabledAgentMCPs(profile, hostID) {
		label := strings.TrimSpace(item.Name)
		if label == "" {
			label = strings.TrimSpace(item.ID)
		}
		if label == "" {
			continue
		}
		names = append(names, fmt.Sprintf("%s (%s)", label, model.NormalizeAgentMCPPermission(item.Permission)))
	}
	return names
}

func (a *App) explicitApprovalMCPNames(profile model.AgentProfile, hostID string) []string {
	names := make([]string, 0, len(profile.MCPs))
	for _, item := range a.effectiveEnabledAgentMCPs(profile, hostID) {
		if !item.RequiresExplicitUserApproval {
			continue
		}
		label := strings.TrimSpace(item.Name)
		if label == "" {
			label = strings.TrimSpace(item.ID)
		}
		if label == "" {
			continue
		}
		names = append(names, label)
	}
	return names
}

func (a *App) buildAgentProfilePreview(profile model.AgentProfile, hostID string) agentProfilePreviewResponse {
	profile = model.CompleteAgentProfile(profile)
	commandSummary := make([]string, 0, len(profile.CommandPermissions.CategoryPolicies)+4)
	commandSummary = append(commandSummary,
		fmt.Sprintf("命令执行: %s", yesNo(boolValue(profile.CommandPermissions.Enabled, true))),
		fmt.Sprintf("默认模式: %s", profile.CommandPermissions.DefaultMode),
		fmt.Sprintf("允许 shell wrapper: %s", yesNo(boolValue(profile.CommandPermissions.AllowShellWrapper, true))),
		fmt.Sprintf("允许 sudo: %s", yesNo(boolValue(profile.CommandPermissions.AllowSudo, false))),
	)
	categories := make([]string, 0, len(profile.CommandPermissions.CategoryPolicies))
	for category := range profile.CommandPermissions.CategoryPolicies {
		categories = append(categories, category)
	}
	slices.Sort(categories)
	for _, category := range categories {
		commandSummary = append(commandSummary, fmt.Sprintf("%s: %s", agentCommandCategoryLabel(category), profile.CommandPermissions.CategoryPolicies[category]))
	}
	capabilitySummary := []string{
		fmt.Sprintf("命令执行: %s", profile.CapabilityPermissions.CommandExecution),
		fmt.Sprintf("文件读取: %s", profile.CapabilityPermissions.FileRead),
		fmt.Sprintf("文件搜索: %s", profile.CapabilityPermissions.FileSearch),
		fmt.Sprintf("文件修改: %s", profile.CapabilityPermissions.FileChange),
		fmt.Sprintf("终端: %s", profile.CapabilityPermissions.Terminal),
		fmt.Sprintf("网页搜索: %s", profile.CapabilityPermissions.WebSearch),
		fmt.Sprintf("网页打开: %s", profile.CapabilityPermissions.WebOpen),
		fmt.Sprintf("审批: %s", profile.CapabilityPermissions.Approval),
		fmt.Sprintf("多 Agent: %s", profile.CapabilityPermissions.MultiAgent),
		fmt.Sprintf("计划: %s", profile.CapabilityPermissions.Plan),
		fmt.Sprintf("总结: %s", profile.CapabilityPermissions.Summary),
	}
	enabledSkills := make([]model.AgentSkill, 0, len(profile.Skills))
	for _, skill := range profile.Skills {
		if skill.Enabled && model.NormalizeAgentSkillActivationMode(skill.ActivationMode) != model.AgentSkillActivationDisabled {
			enabledSkills = append(enabledSkills, skill)
		}
	}
	enabledMCPs := a.effectiveEnabledAgentMCPs(profile, hostID)
	systemPrompt := strings.TrimSpace(profile.SystemPrompt.Content)
	if profile.ID == string(model.AgentProfileTypeMainAgent) {
		systemPrompt = a.renderMainAgentDeveloperInstructions(profile, hostID, true)
	}
	return agentProfilePreviewResponse{
		ProfileID:         profile.ID,
		ProfileType:       profile.Type,
		SystemPrompt:      systemPrompt,
		SystemPromptLines: len(strings.Split(systemPrompt, "\n")),
		CommandSummary:    commandSummary,
		CapabilitySummary: capabilitySummary,
		EnabledSkills:     enabledSkills,
		EnabledMCPs:       enabledMCPs,
		Runtime:           profile.Runtime,
	}
}

func (a *App) agentProfileAuditSummary(profile model.AgentProfile) map[string]any {
	if profile.ID == "" {
		return map[string]any{}
	}
	return map[string]any{
		"id":                profile.ID,
		"type":              profile.Type,
		"name":              profile.Name,
		"model":             profile.Runtime.Model,
		"reasoningEffort":   profile.Runtime.ReasoningEffort,
		"approvalPolicy":    profile.Runtime.ApprovalPolicy,
		"sandboxMode":       profile.Runtime.SandboxMode,
		"systemPrompt":      profile.SystemPrompt.Preview,
		"writableRoots":     append([]string(nil), profile.CommandPermissions.AllowedWritableRoots...),
		"commandEnabled":    boolValue(profile.CommandPermissions.Enabled, true),
		"allowShellWrapper": boolValue(profile.CommandPermissions.AllowShellWrapper, true),
		"allowSudo":         boolValue(profile.CommandPermissions.AllowSudo, false),
	}
}

func agentCommandCategoryLabel(category string) string {
	switch category {
	case "system_inspection":
		return "系统检查"
	case "service_read":
		return "服务读取"
	case "network_read":
		return "网络读取"
	case "file_read":
		return "文件读取"
	case "service_mutation":
		return "服务变更"
	case "filesystem_mutation":
		return "文件系统变更"
	case "package_mutation":
		return "包管理变更"
	default:
		return category
	}
}

func yesNo(value bool) string {
	if value {
		return "是"
	}
	return "否"
}

func cloneBoolPtr(in *bool) *bool {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}

func boolValue(in *bool, fallback bool) bool {
	if in == nil {
		return fallback
	}
	return *in
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return make(map[string]string)
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (a *App) snapshot(sessionID string) model.Snapshot {
	snapshot := a.store.Snapshot(sessionID, model.UIConfig{
		OAuthConfigured: a.cfg.OAuthConfigured(),
		CodexAlive:      a.useBifrost(),
		Provider:        a.cfg.LLMProvider,
		Model:           a.cfg.LLMModel,
	})
	snapshot.Runtime.Codex.RetryMax = 5
	if a.useBifrost() {
		snapshot.Runtime.Codex.Status = "connected"
		snapshot.Runtime.Codex.LastError = ""
	} else {
		snapshot.Runtime.Codex.Status = "stopped"
		snapshot.Runtime.Codex.LastError = "bifrost runtime is not initialized"
	}
	if snapshot.Runtime.Turn.Phase == "" {
		snapshot.Runtime.Turn.Phase = "idle"
	}
	if snapshot.Runtime.Turn.HostID == "" {
		snapshot.Runtime.Turn.HostID = snapshot.SelectedHostID
	}
	return snapshot
}

func (a *App) broadcastSnapshot(sessionID string) {
	snapshot := a.snapshot(sessionID)
	a.relaySnapshotToOrchestrator(sessionID, snapshot)
	a.wsMu.Lock()
	defer a.wsMu.Unlock()
	for conn := range a.wsClients[sessionID] {
		if err := conn.WriteJSON(snapshot); err != nil {
			_ = conn.Close()
			delete(a.wsClients[sessionID], conn)
		}
	}
}

func (a *App) broadcastAllSnapshots() {
	for _, sessionID := range a.store.SessionIDs() {
		a.broadcastSnapshot(sessionID)
	}
}

// throttledBroadcast coalesces rapid delta broadcasts for a session.
// It waits 150ms after the last call before actually broadcasting,
// so token-level deltas don't flood the WebSocket.
func (a *App) throttledBroadcast(sessionID string) {
	a.broadcastThrotMu.Lock()
	defer a.broadcastThrotMu.Unlock()

	if existing, ok := a.broadcastTimers[sessionID]; ok {
		existing.Stop()
	}
	a.broadcastTimers[sessionID] = time.AfterFunc(150*time.Millisecond, func() {
		a.broadcastSnapshot(sessionID)
		a.broadcastThrotMu.Lock()
		delete(a.broadcastTimers, sessionID)
		a.broadcastThrotMu.Unlock()
	})
}

// flushThrottledBroadcast fires any pending throttled broadcast immediately.
func (a *App) flushThrottledBroadcast(sessionID string) {
	a.broadcastThrotMu.Lock()
	if t, ok := a.broadcastTimers[sessionID]; ok {
		t.Stop()
		delete(a.broadcastTimers, sessionID)
		a.broadcastThrotMu.Unlock()
		a.broadcastSnapshot(sessionID)
		return
	}
	a.broadcastThrotMu.Unlock()
}

func (a *App) serveFrontend() http.Handler {
	distPath := filepath.Join("web", "dist")
	if info, err := os.Stat(distPath); err == nil && info.IsDir() {
		fs := http.Dir(distPath)
		fileServer := http.FileServer(fs)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Try to serve the file directly. If it doesn't exist (SPA route),
			// fall back to index.html so Vue Router can handle it.
			path := filepath.Join(distPath, filepath.Clean(r.URL.Path))
			if _, err := os.Stat(path); os.IsNotExist(err) {
				http.ServeFile(w, r, filepath.Join(distPath, "index.html"))
				return
			}
			fileServer.ServeHTTP(w, r)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"message": "frontend build not found; run `cd web && npm install && npm run dev` for development",
		})
	})
}

func (a *App) withSession(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		browserID := a.getOrCreateBrowserSessionID(w, r)
		sessionID := a.store.EnsureActiveSession(browserID)
		next(w, r, sessionID)
	}
}

func (a *App) withBrowserSession(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		browserID := a.getOrCreateBrowserSessionID(w, r)
		next(w, r, browserID)
	}
}

func (a *App) getOrCreateBrowserSessionID(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(a.cfg.SessionCookieName); err == nil && cookie.Value != "" {
		if browserID, ok := a.verifySessionCookie(cookie.Value); ok {
			if a.store.BrowserSessionExists(browserID) {
				a.store.EnsureBrowserSession(browserID)
				return browserID
			}
			if a.store.SessionExists(browserID) {
				a.store.AttachLegacySessionToBrowser(browserID, browserID)
				return browserID
			}
			a.store.EnsureBrowserSession(browserID)
			return browserID
		}
	}
	browserID := model.NewID("browser")
	http.SetCookie(w, &http.Cookie{
		Name:     a.cfg.SessionCookieName,
		Value:    a.signSessionCookie(browserID),
		Path:     "/",
		HttpOnly: true,
		Expires:  time.Now().Add(a.cfg.SessionCookieTTL),
		MaxAge:   int(a.cfg.SessionCookieTTL / time.Second),
		SameSite: http.SameSiteLaxMode,
	})
	a.store.EnsureBrowserSession(browserID)
	return browserID
}

func (a *App) syncAccountState(ctx context.Context, sessionID string) {
	if !a.useBifrostForSession(sessionID) {
		return
	}
	a.ensureBifrostAuthState(sessionID)
}

func (a *App) monitorHosts(ctx context.Context) {
	interval := a.cfg.AgentHeartbeatTimeout / 3
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			changed := a.store.MarkStaleHosts(a.cfg.AgentHeartbeatTimeout)
			if len(changed) == 0 {
				continue
			}
			for _, hostID := range changed {
				log.Printf("host-agent timeout host_id=%s marked offline", hostID)
				a.clearAgentConnection(hostID, nil)
				a.failRemoteTerminalsForHost(hostID, "remote host heartbeat timed out")
				a.failRemoteExecsForHost(hostID, "remote host heartbeat timed out")
				a.failAgentResponseWaitersForHost(hostID, "remote host heartbeat timed out")
				a.reconcileOrchestratorHostUnavailable(hostID, "remote host heartbeat timed out")
				a.notifyRemoteHostUnavailable(hostID, "远程主机连接超时", "远程主机心跳超时，当前操作已中断，可重试或刷新主机状态。")
				a.audit("agent.timeout", map[string]any{
					"hostId": hostID,
				})
			}
			a.broadcastAllSnapshots()
		}
	}
}

func (a *App) signSessionCookie(sessionID string) string {
	return sessionID + "." + a.signatureForSession(sessionID)
}

func (a *App) respondCodex(ctx context.Context, rawID string, result any) error {
	if a.codexRespondFunc != nil {
		return a.codexRespondFunc(ctx, rawID, result)
	}
	return errors.New("runtime responder not configured")
}

func (a *App) respondToolError(ctx context.Context, rawID string, code int, message string) error {
	return a.respondCodex(ctx, rawID, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func (a *App) selectedRemoteHostForSession(sessionID string) (model.Host, bool) {
	session := a.store.Session(sessionID)
	if session == nil {
		return model.Host{}, false
	}
	hostID := defaultHostID(session.SelectedHostID)
	if !isRemoteHostID(hostID) {
		return model.Host{}, false
	}
	return a.findHost(hostID), true
}

func localToolDisplayName(toolName string) string {
	switch strings.TrimSpace(toolName) {
	case "commandExecution":
		return "commandExecution"
	case "fileChange":
		return "fileChange"
	default:
		return defaultHostID(strings.TrimSpace(toolName))
	}
}

func remoteLocalFallbackMessage(host model.Host, toolName, target string) string {
	detail := strings.TrimSpace(target)
	if detail == "" {
		detail = "当前操作"
	}
	return fmt.Sprintf("当前选中的是远程主机 %s（%s），已阻止本地 %s 回退：%s。请改用携带 host=%s 的远程 execute_* 工具，系统不会静默回退到 server-local。",
		hostNameOrID(host),
		defaultHostID(host.ID),
		localToolDisplayName(toolName),
		detail,
		defaultHostID(host.ID),
	)
}

func (a *App) upsertRemoteFallbackErrorCard(sessionID string, host model.Host, title, message string) {
	now := model.NowString()
	retryable := true
	card := model.Card{
		ID:        model.NewID("error"),
		Type:      "ErrorCard",
		Title:     title,
		Message:   message,
		Text:      message,
		Status:    "failed",
		Retryable: &retryable,
		CreatedAt: now,
		UpdatedAt: now,
	}
	applyCardHost(&card, host)
	a.store.UpsertCard(sessionID, card)
}

func (a *App) rejectUnexpectedLocalApproval(sessionID, rawID, toolName, target string, host model.Host) {
	message := remoteLocalFallbackMessage(host, toolName, target)
	a.setRuntimeTurnPhase(sessionID, "thinking")
	a.upsertRemoteFallbackErrorCard(sessionID, host, "已阻止回退到本地执行", message)
	a.audit("remote.local_fallback_blocked", map[string]any{
		"sessionId": sessionID,
		"hostId":    host.ID,
		"hostName":  hostNameOrID(host),
		"toolName":  toolName,
		"target":    emptyToNil(strings.TrimSpace(target)),
		"phase":     "approval",
	})
	a.broadcastSnapshot(sessionID)
	_ = a.respondCodex(context.Background(), rawID, map[string]any{
		"decision": "decline",
	})
}

func (a *App) blockUnexpectedLocalExecution(sessionID string, payload, item map[string]any, host model.Host) {
	itemID := getString(item, "id")
	itemType := getString(item, "type")
	target := getString(item, "command")
	title := "已阻止回退到本地执行"
	if itemType == "fileChange" {
		title = "已阻止本地文件修改回退"
		changes := toChanges(item["changes"])
		if len(changes) > 0 {
			target = changes[0].Path
		}
	}
	message := remoteLocalFallbackMessage(host, itemType, target)
	now := model.NowString()

	if itemID != "" {
		card := model.Card{
			ID:        itemID,
			Type:      "CommandCard",
			Title:     title,
			Status:    "failed",
			Command:   target,
			Output:    message,
			Text:      message,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if itemType == "fileChange" {
			card.Type = "FileChangeCard"
			card.Command = ""
			card.Changes = toChanges(item["changes"])
		}
		applyCardHost(&card, host)
		a.store.UpsertCard(sessionID, card)
	}

	a.finishRuntimeTurn(sessionID, "failed")
	a.upsertRemoteFallbackErrorCard(sessionID, host, title, message)
	a.audit("remote.local_fallback_blocked", map[string]any{
		"sessionId": sessionID,
		"threadId":  getStringAny(payload, "threadId", "thread_id"),
		"turnId":    getTurnID(payload),
		"hostId":    host.ID,
		"hostName":  hostNameOrID(host),
		"toolName":  itemType,
		"target":    emptyToNil(strings.TrimSpace(target)),
		"phase":     "started",
	})
	a.broadcastSnapshot(sessionID)
	go func() {
		if err := a.interruptSessionTurn(context.Background(), sessionID); err != nil {
			log.Printf("interrupt unexpected local fallback failed session=%s err=%s", sessionID, truncate(err.Error(), 200))
		}
	}()
}

func (a *App) verifySessionCookie(value string) (string, bool) {
	sessionID, signature, ok := strings.Cut(value, ".")
	if !ok || sessionID == "" || signature == "" {
		return "", false
	}
	expected := a.signatureForSession(sessionID)
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return "", false
	}
	return sessionID, true
}

func (a *App) signatureForSession(sessionID string) string {
	mac := hmac.New(sha256.New, []byte(a.cfg.SessionSecret))
	_, _ = mac.Write([]byte(sessionID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *App) audit(event string, fields map[string]any) {
	if a.cfg.AuditLogPath == "" {
		return
	}
	record := map[string]any{
		"ts":    model.NowString(),
		"event": event,
	}
	for key, value := range fields {
		record[key] = value
	}

	content, err := json.Marshal(record)
	if err != nil {
		log.Printf("audit marshal failed event=%s err=%s", event, truncate(err.Error(), 200))
		return
	}

	a.auditMu.Lock()
	defer a.auditMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(a.cfg.AuditLogPath), 0o755); err != nil {
		log.Printf("audit mkdir failed path=%s err=%s", a.cfg.AuditLogPath, truncate(err.Error(), 200))
		return
	}
	file, err := os.OpenFile(a.cfg.AuditLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		log.Printf("audit open failed path=%s err=%s", a.cfg.AuditLogPath, truncate(err.Error(), 200))
		return
	}
	defer file.Close()

	if _, err := file.Write(append(content, '\n')); err != nil {
		log.Printf("audit write failed path=%s err=%s", a.cfg.AuditLogPath, truncate(err.Error(), 200))
	}
}

func (a *App) notifyRemoteHostUnavailable(hostID, title, message string) {
	now := model.NowString()
	retryable := true
	host := a.findHost(hostID)
	fullMessage := strings.TrimSpace(message)
	if !strings.Contains(fullMessage, "server-local") {
		fullMessage += " 系统不会静默回退到 server-local。"
	}
	for _, sessionID := range a.store.SessionIDs() {
		session := a.store.Session(sessionID)
		if session == nil {
			continue
		}
		if defaultHostID(session.SelectedHostID) != hostID && defaultHostID(session.Runtime.Turn.HostID) != hostID {
			continue
		}
		if session.Runtime.Turn.Active {
			a.finishRuntimeTurn(sessionID, "failed")
		}
		a.store.UpsertCard(sessionID, model.Card{
			ID:        fmt.Sprintf("remote-host-error-%s", hostID),
			Type:      "ErrorCard",
			Title:     title,
			Message:   fullMessage,
			Text:      fullMessage,
			Status:    "failed",
			Retryable: &retryable,
			HostID:    defaultHostID(host.ID),
			HostName:  hostNameOrID(host),
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
}

func (a *App) clearRemoteHostUnavailableCards(hostID string) {
	hostID = defaultHostID(strings.TrimSpace(hostID))
	if hostID == "" {
		return
	}
	cardID := fmt.Sprintf("remote-host-error-%s", hostID)
	for _, sessionID := range a.store.SessionIDs() {
		if a.store.RemoveCard(sessionID, cardID) {
			log.Printf("remote host recovered host_id=%s session=%s cleared_card=%s", hostID, sessionID, cardID)
		}
	}
}

type oauthTokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	Email       string `json:"email"`
}

func (a *App) exchangeOAuthCode(ctx context.Context, code string) (oauthTokenResponse, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("client_id", a.cfg.OAuthClientID)
	values.Set("client_secret", a.cfg.OAuthClientSecret)
	values.Set("redirect_uri", a.cfg.OAuthRedirectURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.OAuthTokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return oauthTokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return oauthTokenResponse{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return oauthTokenResponse{}, err
	}
	if resp.StatusCode >= 300 {
		return oauthTokenResponse{}, fmt.Errorf("oauth token exchange failed: %s", bytes.TrimSpace(body))
	}
	var tokenResp oauthTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return oauthTokenResponse{}, err
	}
	if tokenResp.AccessToken == "" {
		return oauthTokenResponse{}, errors.New("oauth token response missing access_token")
	}
	return tokenResp, nil
}

func (a *App) fetchOAuthEmail(ctx context.Context, accessToken string) string {
	if a.cfg.OAuthUserInfoURL == "" {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.OAuthUserInfoURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ""
	}
	for _, key := range []string{"email", "preferred_username", "upn"} {
		if value := getString(payload, key); value != "" {
			return value
		}
	}
	return ""
}

func (a *App) findHost(hostID string) model.Host {
	for _, host := range a.store.Hosts() {
		if host.ID == hostID {
			return host
		}
	}
	return model.Host{
		ID:              hostID,
		Name:            hostID,
		Kind:            "inventory",
		Status:          "offline",
		Executable:      false,
		TerminalCapable: false,
	}
}

func toPlanItems(raw any) []model.PlanItem {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	items := make([]model.PlanItem, 0, len(list))
	for _, entry := range list {
		stepMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		items = append(items, model.PlanItem{
			Step:   getString(stepMap, "step"),
			Status: getString(stepMap, "status"),
		})
	}
	return items
}

func toChoiceQuestions(raw any) []model.ChoiceQuestion {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	questions := make([]model.ChoiceQuestion, 0, len(list))
	for _, entry := range list {
		questionMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		questions = append(questions, model.ChoiceQuestion{
			Header:   getString(questionMap, "header"),
			Question: getString(questionMap, "question"),
			IsOther:  getBool(questionMap, "isOther"),
			IsSecret: getBool(questionMap, "isSecret"),
			Options:  toChoiceOptions(questionMap["options"]),
		})
	}
	return questions
}

func toChoiceOptions(raw any) []model.ChoiceOption {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	options := make([]model.ChoiceOption, 0, len(list))
	for _, entry := range list {
		switch value := entry.(type) {
		case string:
			options = append(options, model.ChoiceOption{
				Label: value,
				Value: value,
			})
		case map[string]any:
			label := getString(value, "label")
			if label == "" {
				label = getString(value, "value")
			}
			optionValue := getString(value, "value")
			if optionValue == "" {
				optionValue = label
			}
			options = append(options, model.ChoiceOption{
				Label:       label,
				Value:       optionValue,
				Description: getString(value, "description"),
				Recommended: getBool(value, "recommended"),
			})
		}
	}
	return options
}

func toChanges(raw any) []model.FileChange {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	changes := make([]model.FileChange, 0, len(list))
	for _, entry := range list {
		changeMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		changes = append(changes, model.FileChange{
			Path: getString(changeMap, "path"),
			Kind: kindLabel(changeMap["kind"]),
			Diff: getString(changeMap, "diff"),
		})
	}
	return changes
}

func choiceCardTitle(questions []model.ChoiceQuestion) string {
	if len(questions) == 0 {
		return "需要你的输入"
	}
	if len(questions) == 1 {
		if questions[0].Header != "" {
			return questions[0].Header
		}
	}
	return "需要你的输入"
}

func choiceAnswerSummary(questions []model.ChoiceQuestion, answers []choiceAnswerInput) []string {
	summary := make([]string, 0, len(answers))
	for index, answer := range answers {
		label := strings.TrimSpace(answer.Label)
		if label == "" {
			label = strings.TrimSpace(answer.Value)
		}
		if label == "" {
			continue
		}
		note := strings.TrimSpace(answer.Note)
		if note != "" {
			label = label + "（补充：" + note + "）"
		}
		if index < len(questions) && questions[index].Header != "" {
			summary = append(summary, questions[index].Header+": "+label)
			continue
		}
		summary = append(summary, label)
	}
	return summary
}

func choiceAnswersToModel(answers []choiceAnswerInput) []model.ChoiceAnswer {
	if len(answers) == 0 {
		return nil
	}
	out := make([]model.ChoiceAnswer, 0, len(answers))
	for _, answer := range answers {
		out = append(out, model.ChoiceAnswer{
			Value:   strings.TrimSpace(answer.Value),
			Label:   strings.TrimSpace(answer.Label),
			IsOther: answer.IsOther,
			Note:    strings.TrimSpace(answer.Note),
		})
	}
	return out
}

func getTurnID(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if turnID := getStringAny(payload, "turnId", "turn_id"); turnID != "" {
		return turnID
	}
	turn := getMap(payload, "turn")
	return getString(turn, "id")
}

func kindLabel(raw any) string {
	switch value := raw.(type) {
	case string:
		return value
	case map[string]any:
		for key := range value {
			return key
		}
	}
	return ""
}

func defaultHostID(hostID string) string {
	if hostID == "" {
		return model.ServerLocalHostID
	}
	return hostID
}

func normalizeCardStatus(status string) string {
	switch status {
	case "", "running":
		return "inProgress"
	case "in_progress", "inProgress", "pending":
		return "inProgress"
	case "completed", "success", "accepted", "accepted_for_session", "accepted_for_session_auto":
		return "completed"
	case "failed", "error", "decline", "declined", "cancelled", "canceled", "aborted", "interrupted":
		return "failed"
	default:
		return status
	}
}

func normalizeApprovalDecisionInput(decision string) string {
	normalized := strings.ToLower(strings.TrimSpace(decision))
	switch normalized {
	case "", "accept":
		return "accept"
	case "reject":
		return "decline"
	default:
		return normalized
	}
}

func completedItemStatus(item map[string]any) string {
	status := normalizeCardStatus(getString(item, "status"))
	if status != "inProgress" {
		return status
	}
	return "completed"
}

func completedCommandStatus(item map[string]any, output string) string {
	exitCode, ok := getIntAny(item, "exitCode", "exit_code")
	if ok && exitCode != 0 {
		return "failed"
	}
	if commandOutputLooksFailed(output) {
		return "failed"
	}
	return completedItemStatus(item)
}

func commandOutputLooksFailed(output string) bool {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return false
	}

	lower := strings.ToLower(trimmed)
	strongSignals := []string{
		"operation not permitted",
		"permission denied",
		"command not found",
		"no such file or directory",
		"is not recognized as an internal or external command",
		"unknown option",
		"illegal option",
		"invalid option",
		"traceback (most recent call last):",
	}
	for _, signal := range strongSignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}

	for _, line := range strings.Split(lower, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "zsh:") || strings.HasPrefix(line, "bash:") || strings.HasPrefix(line, "sh:") {
			return true
		}
		if strings.HasPrefix(line, "python: can't open file") || strings.HasPrefix(line, "npm err!") {
			return true
		}
	}

	return false
}

type stringHit struct {
	Key   string
	Value string
}

func detectActivitySignal(item map[string]any) (kind string, entry model.ActivityEntry, currentLabel string, ok bool) {
	if protocolKind, protocolEntry, protocolLabel, protocolOK := detectProtocolActivitySignal(item); protocolOK {
		return protocolKind, protocolEntry, protocolLabel, true
	}

	hits := make([]stringHit, 0, 24)
	collectStringHits("", item, &hits)

	descriptors := make([]string, 0, len(hits))
	var filePath string
	var query string
	for _, hit := range hits {
		key := strings.ToLower(hit.Key)
		value := strings.TrimSpace(hit.Value)
		if value == "" {
			continue
		}
		lowerValue := strings.ToLower(value)
		if isDescriptorKey(key) {
			descriptors = append(descriptors, lowerValue)
		}
		if filePath == "" && isFilePathKey(key) && looksLikePath(value) {
			filePath = value
		}
		if query == "" && isQueryKey(key) {
			query = value
		}
		if query == "" && (strings.Contains(lowerValue, "search the web:") || strings.Contains(lowerValue, "search_query")) {
			query = strings.TrimSpace(strings.TrimPrefix(value, "Search the web:"))
		}
	}

	descriptorText := strings.Join(descriptors, " | ")
	switch {
	case query != "" && isWebSearchDescriptor(descriptorText):
		return "web_search", model.ActivityEntry{
			Label: "Search the web: " + query,
			Query: query,
		}, query, true
	case query != "" && filePath != "" && isFileSearchDescriptor(descriptorText):
		return "file_search", model.ActivityEntry{
			Label: "Search files in " + filePath + " for " + query,
			Path:  filePath,
			Query: query,
		}, query, true
	case filePath != "" && isListDescriptor(descriptorText):
		return "list", model.ActivityEntry{
			Label: "List " + filePath,
			Path:  filePath,
		}, filepath.Base(filePath), true
	case filePath != "" && isReadDescriptor(descriptorText):
		display := filepath.Base(filePath)
		if display == "." || display == "/" || display == "" {
			display = filePath
		}
		return "file_read", model.ActivityEntry{
			Label: "Read " + display,
			Path:  filePath,
		}, display, true
	default:
		return "", model.ActivityEntry{}, "", false
	}
}

func detectProtocolActivitySignal(item map[string]any) (kind string, entry model.ActivityEntry, currentLabel string, ok bool) {
	switch strings.ToLower(getString(item, "type")) {
	case "websearch":
		return detectWebSearchSignal(item)
	case "filesearch":
		return detectFileSearchSignal(item)
	case "filelist":
		return detectFileListSignal(item)
	case "fileread":
		return detectFileReadSignal(item)
	case "commandexecution":
		return detectCommandExecutionSignal(item)
	default:
		return "", model.ActivityEntry{}, "", false
	}
}

func detectCommandExecutionSignal(item map[string]any) (kind string, entry model.ActivityEntry, currentLabel string, ok bool) {
	command := strings.TrimSpace(getStringAny(item, "command", "commandLine", "command_line"))
	if command == "" {
		command = strings.TrimSpace(composeCommandFromProgramArgs(item))
	}
	if command == "" {
		return "", model.ActivityEntry{}, "", false
	}
	label := truncate(command, 96)
	return "command", model.ActivityEntry{
		Label: "Execute command: " + command,
		Query: command,
	}, label, true
}

func detectFileSearchSignal(item map[string]any) (kind string, entry model.ActivityEntry, currentLabel string, ok bool) {
	path := strings.TrimSpace(getStringAny(item, "path", "directory", "root", "scope"))
	query := strings.TrimSpace(getStringAny(item, "query", "searchQuery", "search_query", "pattern"))
	if query == "" || path == "" {
		return "", model.ActivityEntry{}, "", false
	}
	return "file_search", model.ActivityEntry{
		Label: "Search files in " + path + " for " + query,
		Path:  path,
		Query: query,
	}, query, true
}

func detectFileListSignal(item map[string]any) (kind string, entry model.ActivityEntry, currentLabel string, ok bool) {
	path := strings.TrimSpace(getStringAny(item, "path", "directory", "root"))
	if path == "" {
		return "", model.ActivityEntry{}, "", false
	}
	label := filepath.Base(path)
	if label == "." || label == "/" || label == "" {
		label = path
	}
	return "list", model.ActivityEntry{
		Label: "List " + path,
		Path:  path,
	}, label, true
}

func detectFileReadSignal(item map[string]any) (kind string, entry model.ActivityEntry, currentLabel string, ok bool) {
	path := strings.TrimSpace(getStringAny(item, "path", "file", "filePath"))
	if path == "" {
		return "", model.ActivityEntry{}, "", false
	}
	label := filepath.Base(path)
	if label == "." || label == "/" || label == "" {
		label = path
	}
	return "file_read", model.ActivityEntry{
		Label: "Read " + label,
		Path:  path,
	}, label, true
}

func detectWebSearchSignal(item map[string]any) (kind string, entry model.ActivityEntry, currentLabel string, ok bool) {
	action := getMap(item, "action")
	actionType := strings.ToLower(getString(action, "type"))
	query := strings.TrimSpace(getString(action, "query"))
	if query == "" {
		query = strings.TrimSpace(getString(item, "query"))
	}
	if query == "" {
		query = firstNonEmptyString(toStringSlice(action["queries"]))
	}

	switch actionType {
	case "", "search":
		if query == "" {
			return "", model.ActivityEntry{}, "", false
		}
		return "web_search", model.ActivityEntry{
			Label: "Search the web: " + query,
			Query: query,
		}, query, true
	case "openpage":
		rawURL := strings.TrimSpace(getString(action, "url"))
		if rawURL == "" {
			return "", model.ActivityEntry{}, "", false
		}
		display := summarizeWebLocation(rawURL)
		return "web_open", model.ActivityEntry{
			Label: "Open web page: " + rawURL,
			Query: rawURL,
		}, display, true
	case "findinpage":
		pattern := strings.TrimSpace(getString(action, "pattern"))
		rawURL := strings.TrimSpace(getString(action, "url"))
		if pattern == "" && rawURL == "" {
			return "", model.ActivityEntry{}, "", false
		}
		display := pattern
		if display == "" {
			display = summarizeWebLocation(rawURL)
		}
		label := "Find in page: " + display
		if rawURL != "" && pattern != "" {
			label = "Find in page: " + pattern + " @ " + rawURL
		}
		return "web_find", model.ActivityEntry{
			Label: label,
			Query: display,
		}, display, true
	default:
		if query == "" {
			return "", model.ActivityEntry{}, "", false
		}
		return "web_search", model.ActivityEntry{
			Label: "Search the web: " + query,
			Query: query,
		}, query, true
	}
}

func collectStringHits(prefix string, raw any, hits *[]stringHit) {
	switch value := raw.(type) {
	case map[string]any:
		for key, entry := range value {
			nextKey := key
			if prefix != "" {
				nextKey = prefix + "." + key
			}
			collectStringHits(nextKey, entry, hits)
		}
	case []any:
		for _, entry := range value {
			collectStringHits(prefix, entry, hits)
		}
	case string:
		*hits = append(*hits, stringHit{Key: prefix, Value: value})
	}
}

func appendUniqueActivityEntry(entries *[]model.ActivityEntry, entry model.ActivityEntry, match func(model.ActivityEntry, model.ActivityEntry) bool) {
	for _, existing := range *entries {
		if match(existing, entry) {
			return
		}
	}
	*entries = append(*entries, entry)
}

func isDescriptorKey(key string) bool {
	return strings.HasSuffix(key, "type") ||
		strings.HasSuffix(key, "title") ||
		strings.HasSuffix(key, "kind") ||
		strings.HasSuffix(key, "name") ||
		strings.HasSuffix(key, "label") ||
		strings.HasSuffix(key, "action") ||
		strings.HasSuffix(key, "tool") ||
		strings.HasSuffix(key, "toolname") ||
		strings.HasSuffix(key, "method")
}

func isFilePathKey(key string) bool {
	return (strings.Contains(key, "path") || strings.Contains(key, "file") || strings.Contains(key, "filename")) &&
		!strings.Contains(key, "cwd") &&
		!strings.Contains(key, "grantroot")
}

func isQueryKey(key string) bool {
	return strings.HasSuffix(key, "query") ||
		strings.HasSuffix(key, "searchquery") ||
		strings.HasSuffix(key, ".q") ||
		strings.HasSuffix(key, "pattern")
}

func looksLikePath(value string) bool {
	return strings.Contains(value, "/") ||
		strings.HasPrefix(value, "~") ||
		(strings.Contains(filepath.Base(value), ".") && !strings.Contains(value, " "))
}

func isWebSearchDescriptor(text string) bool {
	return strings.Contains(text, "search the web") ||
		strings.Contains(text, "search_query") ||
		strings.Contains(text, "websearch") ||
		strings.Contains(text, "web_search") ||
		strings.Contains(text, "web search")
}

func isFileSearchDescriptor(text string) bool {
	return descriptorHasToken(text, "search") ||
		descriptorHasToken(text, "find") ||
		descriptorHasToken(text, "grep") ||
		strings.Contains(text, "file search") ||
		strings.Contains(text, "search files")
}

func isListDescriptor(text string) bool {
	return descriptorHasToken(text, "list") ||
		descriptorHasToken(text, "glob") ||
		descriptorHasToken(text, "directory") ||
		descriptorHasToken(text, "ls")
}

func isReadDescriptor(text string) bool {
	if strings.Contains(text, "filechange") || strings.Contains(text, "file change") || strings.Contains(text, "edit") || strings.Contains(text, "write") {
		return false
	}
	return descriptorHasToken(text, "read") ||
		descriptorHasToken(text, "open") ||
		descriptorHasToken(text, "view")
}

func descriptorHasToken(text, token string) bool {
	return slices.ContainsFunc(strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}), func(field string) bool {
		return field == token
	})
}

func firstNonEmptyString(values []string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func summarizeWebLocation(rawURL string) string {
	if strings.TrimSpace(rawURL) == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return rawURL
	}
	path := strings.Trim(parsed.Path, "/")
	if path == "" {
		return parsed.Host
	}
	return parsed.Host + "/" + path
}

func getMap(payload map[string]any, key string) map[string]any {
	value, _ := payload[key].(map[string]any)
	return value
}

func getString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func getStringAny(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := getString(payload, key); value != "" {
			return value
		}
	}
	return ""
}

func getBool(payload map[string]any, key string) bool {
	value, _ := payload[key].(bool)
	return value
}

func getFloat(payload map[string]any, key string) float64 {
	value, _ := payload[key].(float64)
	return value
}

func getIntAny(payload map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		switch value := payload[key].(type) {
		case int:
			return value, true
		case int32:
			return int(value), true
		case int64:
			return int(value), true
		case float64:
			return int(value), true
		}
	}
	return 0, false
}

func toStringSlice(raw any) []string {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, entry := range list {
		if value, ok := entry.(string); ok {
			out = append(out, value)
		}
	}
	return out
}

func emptyToNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func truncate(value string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
