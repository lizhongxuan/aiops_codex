package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/coroot"
	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/orchestrator"
)

func TestDefaultReActLoopStagesAreExplicitPipeline(t *testing.T) {
	want := []string{
		reActStageContextPreprocess,
		reActStageAttachmentInject,
		reActStageModelStreamCall,
		reActStageErrorRecovery,
		reActStageToolExecution,
		reActStagePostprocess,
		reActStageLoopDecision,
	}
	if got := defaultReActStageNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected ReAct stage order: got %#v want %#v", got, want)
	}
}

func decodeStructuredToolResponsePayload(t *testing.T, response map[string]any) map[string]any {
	t.Helper()
	var firstItem map[string]any
	switch items := response["contentItems"].(type) {
	case []map[string]any:
		if len(items) > 0 {
			firstItem = items[0]
		}
	case []any:
		if len(items) > 0 {
			firstItem, _ = items[0].(map[string]any)
		}
	}
	if len(firstItem) == 0 {
		t.Fatalf("expected dynamic tool response contentItems, got %#v", response)
	}
	text := strings.TrimSpace(fmt.Sprint(firstItem["text"]))
	if text == "" {
		t.Fatalf("expected dynamic tool response text payload, got %#v", response)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("expected JSON dynamic tool response payload, got %q: %v", text, err)
	}
	return payload
}

func toolResponseText(t *testing.T, response map[string]any) string {
	t.Helper()
	var firstItem map[string]any
	switch items := response["contentItems"].(type) {
	case []map[string]any:
		if len(items) > 0 {
			firstItem = items[0]
		}
	case []any:
		if len(items) > 0 {
			firstItem, _ = items[0].(map[string]any)
		}
	}
	if len(firstItem) == 0 {
		t.Fatalf("expected dynamic tool response contentItems, got %#v", response)
	}
	return strings.TrimSpace(fmt.Sprint(firstItem["text"]))
}

func TestSingleHostReActThreadSpecIncludesRuntimeAttachment(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "single-host-react-spec"
	app.store.EnsureSession(sessionID)

	spec := app.buildSingleHostReActThreadStartSpec(context.Background(), sessionID)
	if got := strings.TrimSpace(spec.ThreadConfigHash); !strings.HasSuffix(got, ":"+reActLoopVersion) {
		t.Fatalf("expected single-host ReAct config hash, got %q", got)
	}
	for _, needle := range []string{
		"ReAct agent loop runtime attachment",
		"context_preprocess",
		"attachment_injection",
		"model_stream_call",
		"error_recovery",
		"tool_execution",
		"postprocess",
		"loop_decision",
		"Single-host policy",
	} {
		if !strings.Contains(spec.DeveloperInstructions, needle) {
			t.Fatalf("expected single-host ReAct instructions to contain %q, got:\n%s", needle, spec.DeveloperInstructions)
		}
	}
	if strings.Contains(spec.DeveloperInstructions, "request_user_input") {
		t.Fatalf("single-host ReAct instructions must use ask_user_question instead of request_user_input:\n%s", spec.DeveloperInstructions)
	}
	foundAskTool := false
	for _, tool := range spec.DynamicTools {
		if strings.TrimSpace(getStringAny(tool, "name")) == "ask_user_question" {
			foundAskTool = true
			break
		}
	}
	if !foundAskTool {
		t.Fatalf("expected single-host ReAct thread to expose ask_user_question, got %#v", spec.DynamicTools)
	}
}

func TestSingleHostReActThreadSpecUsesCodexSafeCorootToolNames(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.corootClient = coroot.NewClient("http://coroot.internal:8080", "test-token", time.Second)
	sessionID := "single-host-react-coroot-tools"
	app.store.EnsureSession(sessionID)

	spec := app.buildSingleHostReActThreadStartSpec(context.Background(), sessionID)
	validName := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	foundCoroot := 0
	for _, tool := range spec.DynamicTools {
		name := strings.TrimSpace(getStringAny(tool, "name"))
		if strings.HasPrefix(name, "coroot") {
			foundCoroot++
			if !validName.MatchString(name) {
				t.Fatalf("expected coroot tool name to satisfy Codex pattern, got %q", name)
			}
		}
	}
	if foundCoroot == 0 {
		t.Fatalf("expected coroot tools to be exposed when coroot is configured, got %#v", spec.DynamicTools)
	}
}

func TestWorkspaceReActThreadSpecIncludesPromptAndTools(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-react-spec"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})

	spec := app.buildWorkspaceReActThreadStartSpec(context.Background(), sessionID, model.ServerLocalHostID)
	if got := strings.TrimSpace(spec.ThreadConfigHash); !strings.HasSuffix(got, ":workspace-"+reActLoopVersion) {
		t.Fatalf("expected workspace ReAct config hash, got %q", got)
	}
	for _, needle := range []string{
		"Claude Code 式 ReAct agent loop",
		"不要再输出 route JSON",
		"ask_user_question",
		"readonly_host_inspect",
		"must not use built-in commandExecution",
		"next_required_tool",
		"ReAct agent loop runtime attachment",
		"Workspace policy",
	} {
		if !strings.Contains(spec.DeveloperInstructions, needle) {
			t.Fatalf("expected workspace ReAct instructions to contain %q, got:\n%s", needle, spec.DeveloperInstructions)
		}
	}
	if strings.Contains(spec.DeveloperInstructions, "request_user_input") {
		t.Fatalf("workspace ReAct instructions must use ask_user_question instead of request_user_input:\n%s", spec.DeveloperInstructions)
	}

	toolNames := make(map[string]bool, len(spec.DynamicTools))
	toolDescriptions := make(map[string]string, len(spec.DynamicTools))
	for _, tool := range spec.DynamicTools {
		name := strings.TrimSpace(getStringAny(tool, "name"))
		toolNames[name] = true
		toolDescriptions[name] = strings.TrimSpace(getStringAny(tool, "description"))
	}
	for _, name := range []string{"ask_user_question", "query_ai_server_state", "readonly_host_inspect", "enter_plan_mode", "update_plan", "exit_plan_mode", "orchestrator_dispatch_tasks"} {
		if !toolNames[name] {
			t.Fatalf("expected workspace ReAct tool %q in %#v", name, toolNames)
		}
	}
	dispatchDescription := toolDescriptions["orchestrator_dispatch_tasks"]
	if !strings.Contains(dispatchDescription, "exit_plan_mode") || !strings.Contains(dispatchDescription, "approved") || !strings.Contains(dispatchDescription, "unavailable") {
		t.Fatalf("DispatchWorkers tool description must state approval gate, got %q", dispatchDescription)
	}
}

func TestWorkspaceAndSingleHostReActThreadSpecsExposeOnlyCodexSafeToolNames(t *testing.T) {
	app := newOrchestratorTestApp(t)
	app.corootClient = coroot.NewClient("http://coroot.internal:8080", "test-token", time.Second)
	validName := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

	singleSessionID := "single-host-react-safe-tools"
	app.store.EnsureSession(singleSessionID)
	singleSpec := app.buildSingleHostReActThreadStartSpec(context.Background(), singleSessionID)
	for _, tool := range singleSpec.DynamicTools {
		name := strings.TrimSpace(getStringAny(tool, "name"))
		if !validName.MatchString(name) {
			t.Fatalf("expected single-host tool name to satisfy Codex pattern, got %q", name)
		}
	}

	workspaceSessionID := "workspace-react-safe-tools"
	app.store.EnsureSessionWithMeta(workspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: workspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	workspaceSpec := app.buildWorkspaceReActThreadStartSpec(context.Background(), workspaceSessionID, "remote-safe-01")
	for _, tool := range workspaceSpec.DynamicTools {
		name := strings.TrimSpace(getStringAny(tool, "name"))
		if !validName.MatchString(name) {
			t.Fatalf("expected workspace tool name to satisfy Codex pattern, got %q", name)
		}
	}
}

func TestChoiceReadonlyAnswerRequiresReadonlyHostInspect(t *testing.T) {
	payload := choiceFollowUpPayload(
		[]model.ChoiceQuestion{{Header: "确认意图", Question: "你希望我怎么处理 PG 不同步问题？"}},
		[]choiceAnswerInput{{Value: "readonly", Label: "开始只读诊断"}},
		[]map[string]any{{"value": "readonly", "label": "开始只读诊断"}},
	)
	if got := fmt.Sprint(payload["next_required_tool"]); got != "readonly_host_inspect" {
		t.Fatalf("expected readonly answer to require readonly_host_inspect, got %#v", payload)
	}
	if got := fmt.Sprint(payload["permission_scope"]); got != "readonly_only" {
		t.Fatalf("expected readonly permission scope, got %#v", payload)
	}
	if instruction := fmt.Sprint(payload["instruction"]); !strings.Contains(instruction, "readonly_host_inspect") || !strings.Contains(instruction, "do not dispatch workers") {
		t.Fatalf("expected readonly instruction to force readonly_host_inspect and block dispatch, got %#v", payload)
	}
}

func TestRequiredReadonlyToolFollowupWhenModelAnsweredPlainText(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-react-readonly-followup"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	now := model.NowString()
	questions := []model.ChoiceQuestion{{
		Header:   "确认意图",
		Question: "你希望我怎么处理 PostgreSQL 不同步的问题？",
		Options: []model.ChoiceOption{
			{Label: "只读诊断", Value: "readonly"},
			{Label: "准备修复", Value: "repair_plan"},
		},
	}}
	app.store.UpsertCard(sessionID, model.Card{
		ID:        "user-readonly-followup",
		Type:      "UserMessageCard",
		Status:    "completed",
		Text:      "你有办法修复 pg 不同步的问题吗?",
		CreatedAt: now,
		UpdatedAt: now,
	})
	app.store.AddChoice(sessionID, model.ChoiceRequest{
		ID:          "choice-readonly-followup",
		ItemID:      "choice-readonly-followup",
		Status:      "completed",
		Questions:   questions,
		Answers:     []model.ChoiceAnswer{{Value: "readonly", Label: "只读诊断"}},
		RequestedAt: now,
		ResolvedAt:  now,
	})
	app.store.UpsertCard(sessionID, model.Card{
		ID:            "choice-readonly-followup",
		Type:          "ChoiceCard",
		RequestID:     "choice-readonly-followup",
		Status:        "completed",
		Questions:     questions,
		AnswerSummary: []string{"确认意图: 只读诊断"},
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	app.store.UpsertCard(sessionID, model.Card{
		ID:        "assistant-ignored-readonly-tool",
		Type:      "AssistantMessageCard",
		Status:    "completed",
		Text:      "我先做只读诊断。",
		CreatedAt: now,
		UpdatedAt: now,
	})

	followup, ok := app.requiredToolFollowupAfterTurn(sessionID)
	if !ok {
		t.Fatalf("expected required readonly follow-up")
	}
	if followup.Tool != "readonly_host_inspect" {
		t.Fatalf("expected readonly_host_inspect follow-up, got %#v", followup)
	}
	if !strings.Contains(followup.Message, "Do not answer in plain text") || !strings.Contains(followup.Message, "built-in commandExecution") {
		t.Fatalf("expected hard follow-up instruction, got %q", followup.Message)
	}

	app.store.UpsertCard(sessionID, model.Card{
		ID:      "readonly-command-after-choice",
		Type:    "CommandCard",
		Status:  "completed",
		Command: "pwd",
		Detail:  map[string]any{"tool": "readonly_host_inspect"},
	})
	if _, ok := app.requiredToolFollowupAfterTurn(sessionID); ok {
		t.Fatalf("expected follow-up to be suppressed once readonly_host_inspect was observed")
	}
}

func TestWorkspaceIntentGuardCreatesPlatformChoice(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-react-intent-guard"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.runtimeStartThreadFunc = func(_ context.Context, _ string, _ threadStartSpec) (string, error) {
		t.Fatal("intent guard must not start runtime before user clarification")
		return "", nil
	}
	app.runtimeStartTurnFunc = func(_ context.Context, _ string, _ string, _ turnStartSpec) (string, error) {
		t.Fatal("intent guard must not start runtime turn before user clarification")
		return "", nil
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/message", nil)
	app.handleWorkspaceChatMessage(rec, req, sessionID, chatRequest{Message: "你有办法修复 pg 不同步的问题吗?"}, time.Now())
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected accepted guarded message, got %d body=%s", rec.Code, rec.Body.String())
	}
	session := app.store.Session(sessionID)
	if session == nil || session.Runtime.Turn.Phase != "waiting_input" {
		t.Fatalf("expected waiting_input runtime, got %#v", session)
	}
	var choiceCard *model.Card
	for i := range session.Cards {
		if session.Cards[i].Type == "ChoiceCard" {
			choiceCard = &session.Cards[i]
			break
		}
	}
	if choiceCard == nil {
		t.Fatalf("expected platform ChoiceCard, cards=%#v", session.Cards)
	}
	if len(choiceCard.Options) < 3 || choiceCard.Options[0].Value != "readonly" || choiceCard.Options[2].Value != "repair_plan" {
		t.Fatalf("expected readonly/answer/repair options, got %#v", choiceCard.Options)
	}
	if choice, ok := app.store.Choice(sessionID, choiceCard.RequestID); !ok || choice.RequestIDRaw != "" {
		t.Fatalf("expected platform choice without raw Codex request, got %#v ok=%v", choice, ok)
	}
}

func TestPlatformCapabilityChoiceAnswersDirectlyWithoutTools(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-react-capability-answer"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.runtimeStartThreadFunc = func(_ context.Context, _ string, _ threadStartSpec) (string, error) {
		t.Fatal("capability-only answer must not start runtime follow-up")
		return "", nil
	}
	app.runtimeStartTurnFunc = func(_ context.Context, _ string, _ string, _ turnStartSpec) (string, error) {
		t.Fatal("capability-only answer must not start runtime turn")
		return "", nil
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/message", nil)
	app.handleWorkspaceChatMessage(rec, req, sessionID, chatRequest{Message: "你有办法修复 pg 不同步的问题吗?"}, time.Now())
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected accepted guarded message, got %d body=%s", rec.Code, rec.Body.String())
	}

	session := app.store.Session(sessionID)
	var choiceID string
	for _, card := range session.Cards {
		if card.Type == "ChoiceCard" {
			choiceID = card.RequestID
			break
		}
	}
	if choiceID == "" {
		t.Fatalf("expected platform choice card, got %#v", session.Cards)
	}

	answerReq := httptest.NewRequest(http.MethodPost, "/api/v1/choices/"+choiceID+"/answer", strings.NewReader(`{"answers":[{"value":"answer_only","label":"只问能力"}]}`))
	answerRec := httptest.NewRecorder()
	app.handleChoiceAnswer(answerRec, answerReq, sessionID)
	if answerRec.Code != http.StatusOK {
		t.Fatalf("expected 200 answering capability-only choice, got %d body=%s", answerRec.Code, answerRec.Body.String())
	}

	session = app.store.Session(sessionID)
	if session == nil || session.Runtime.Turn.Active || session.Runtime.Turn.Phase != "completed" {
		t.Fatalf("expected completed inactive runtime, got %#v", session.Runtime.Turn)
	}
	assistantReplies := 0
	for _, card := range session.Cards {
		switch card.Type {
		case "AssistantMessageCard":
			assistantReplies++
			if !strings.Contains(card.Text, "可以处理") || !strings.Contains(card.Text, "不会访问主机") || !strings.Contains(card.Text, "不会执行命令") {
				t.Fatalf("expected direct capability answer with no-tool boundary, got %#v", card)
			}
		case "CommandCard", "PlanCard", "PlanApprovalCard":
			t.Fatalf("capability-only answer must not create tool or plan cards, got %#v", card)
		}
	}
	if assistantReplies != 1 {
		t.Fatalf("expected exactly one assistant capability answer, got %d cards=%#v", assistantReplies, session.Cards)
	}
	snapshot := app.snapshot(sessionID)
	for _, invocation := range snapshot.ToolInvocations {
		if invocation.Name != "ask_user_question" {
			t.Fatalf("capability-only answer must not create tool invocation %q: %#v", invocation.Name, snapshot.ToolInvocations)
		}
	}
}

func TestWorkspaceDispatchRequiresClarificationForCapabilityQuestion(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-react-clarify"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.UpsertCard(sessionID, model.Card{
		ID:        "msg-capability-question",
		Type:      "UserMessageCard",
		Role:      "user",
		Text:      "你有办法修复 pg 不同步的问题吗?",
		Status:    "completed",
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	})

	_, err := app.dispatchOrchestratorTasks(sessionID, orchestrator.DispatchRequest{
		Tasks: []orchestrator.DispatchTaskRequest{
			{
				TaskID:      "task-1",
				HostID:      "host-1",
				Title:       "检查 PG 同步",
				Instruction: "检查 PostgreSQL 同步状态",
			},
		},
	})
	if err == nil {
		t.Fatalf("expected ambiguous capability question to block dispatch")
	}
	if !strings.Contains(err.Error(), "用户意图仍不明确") {
		t.Fatalf("expected clarification error, got %v", err)
	}
}

func TestAskUserQuestionDynamicToolCreatesChoiceCard(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-react-ask-user"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetThread(sessionID, "thread-ask-user")
	app.store.SetTurn(sessionID, "turn-ask-user")
	app.startRuntimeTurn(sessionID, model.ServerLocalHostID)

	app.handleDynamicToolCall("raw-ask-user", map[string]any{
		"threadId": "thread-ask-user",
		"turnId":   "turn-ask-user",
		"tool":     "ask_user_question",
		"arguments": map[string]any{
			"questions": []any{
				map[string]any{
					"header":   "确认意图",
					"question": "你是只问能力，还是要我开始只读诊断？",
					"options": []any{
						map[string]any{"label": "只问能力", "value": "capability", "recommended": true},
						map[string]any{"label": "开始只读诊断", "value": "readonly"},
						map[string]any{"label": "先给修复思路", "value": "plan_only"},
						map[string]any{"label": "授权执行修复", "value": "execute"},
					},
					"isOther": true,
				},
			},
		},
	})

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session")
	}
	if session.Runtime.Turn.Phase != "waiting_input" {
		t.Fatalf("expected waiting_input phase, got %q", session.Runtime.Turn.Phase)
	}
	var choiceCard *model.Card
	for i := range session.Cards {
		if session.Cards[i].Type == "ChoiceCard" {
			choiceCard = &session.Cards[i]
			break
		}
	}
	if choiceCard == nil {
		t.Fatalf("expected ChoiceCard, got %#v", session.Cards)
	}
	if choiceCard.Status != "pending" || !strings.Contains(choiceCard.Question, "只问能力") {
		t.Fatalf("unexpected ChoiceCard: %#v", choiceCard)
	}
	if len(choiceCard.Questions) != 1 || len(choiceCard.Questions[0].Options) != 4 {
		t.Fatalf("expected one question with four options, got %#v", choiceCard.Questions)
	}
	if !choiceCard.Questions[0].Options[0].Recommended || !choiceCard.Questions[0].IsOther {
		t.Fatalf("expected recommended option and other input to be preserved, got %#v", choiceCard.Questions[0])
	}
	if choice, ok := app.store.Choice(sessionID, choiceCard.RequestID); !ok || choice.RequestIDRaw != "raw-ask-user" {
		t.Fatalf("expected pending choice bound to raw tool request, got %#v ok=%v", choice, ok)
	}
	snapshot := app.snapshot(sessionID)
	foundInvocation := false
	for _, invocation := range snapshot.ToolInvocations {
		if invocation.Name == "ask_user_question" && invocation.Status == "waiting_user" && strings.Contains(invocation.InputSummary, "只问能力") {
			foundInvocation = true
			break
		}
	}
	if !foundInvocation {
		t.Fatalf("expected pending ask_user_question invocation in snapshot, got %#v", snapshot.ToolInvocations)
	}
}

func TestReActThreadRejectsBuiltinRequestUserInput(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-react-reject-request-user-input"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetThread(sessionID, "thread-react-request-user-input")
	app.store.SetThreadConfigHash(sessionID, app.workspaceReActThreadConfigHash(model.ServerLocalHostID))
	app.store.SetTurn(sessionID, "turn-react-request-user-input")

	payload := map[string]any{
		"threadId": "thread-react-request-user-input",
		"turnId":   "turn-react-request-user-input",
		"questions": []any{
			map[string]any{
				"question": "是否开始诊断？",
				"options": []any{
					map[string]any{"label": "只问能力", "value": "capability"},
					map[string]any{"label": "开始诊断", "value": "diagnose"},
				},
			},
		},
	}
	app.handleBuiltinUserInputRequest("raw-request-user-input", payload)

	session := app.store.Session(sessionID)
	for _, card := range session.Cards {
		if card.Type == "ChoiceCard" {
			t.Fatalf("expected built-in request_user_input to be rejected, got choice card %#v", card)
		}
	}
	var errorCard *model.Card
	for i := range session.Cards {
		if session.Cards[i].Type == "ErrorCard" {
			errorCard = &session.Cards[i]
			break
		}
	}
	if errorCard == nil || !strings.Contains(errorCard.Text, "ask_user_question") {
		t.Fatalf("expected ask_user_question configuration error card, got %#v", session.Cards)
	}
}

func TestPendingAskUserQuestionCanBeAnsweredFromChatInput(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-react-ask-user-chat-input"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetThread(sessionID, "thread-ask-user-chat-input")
	app.store.SetTurn(sessionID, "turn-ask-user-chat-input")
	app.startRuntimeTurn(sessionID, model.ServerLocalHostID)
	var respondedRawID string
	var respondedPayload map[string]any
	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		respondedRawID = rawID
		respondedPayload, _ = result.(map[string]any)
		return nil
	}

	app.handleDynamicToolCall("raw-ask-user-chat-input", map[string]any{
		"threadId": "thread-ask-user-chat-input",
		"turnId":   "turn-ask-user-chat-input",
		"tool":     "ask_user_question",
		"arguments": map[string]any{
			"questions": []any{
				map[string]any{
					"header":   "确认意图",
					"question": "你是只问能力，还是要我开始只读诊断？",
					"options": []any{
						map[string]any{"label": "只问能力", "value": "capability"},
						map[string]any{"label": "开始只读诊断", "value": "readonly"},
					},
					"isOther": true,
				},
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", nil)
	rec := httptest.NewRecorder()
	if !app.answerPendingChoiceFromChatMessage(rec, req, sessionID, "只问能力") {
		t.Fatalf("expected pending choice to be answered from chat input")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	session := app.store.Session(sessionID)
	if session == nil || session.Runtime.Turn.Phase != "thinking" {
		t.Fatalf("expected runtime phase thinking, got %#v", session)
	}
	if respondedRawID != "raw-ask-user-chat-input" {
		t.Fatalf("expected response to raw tool id, got %q", respondedRawID)
	}
	decodedPayload := decodeStructuredToolResponsePayload(t, respondedPayload)
	answers, ok := decodedPayload["answers"].([]any)
	if !ok || len(answers) != 1 {
		t.Fatalf("expected codex answer capability, got %#v", decodedPayload)
	}
	answer, ok := answers[0].(map[string]any)
	if !ok || answer["value"] != "capability" {
		t.Fatalf("expected codex answer capability, got %#v", decodedPayload)
	}
	if _, ok := decodedPayload["next_required_tool"]; ok {
		t.Fatalf("capability answer must not require plan mode, got %#v", decodedPayload)
	}
	var completedChoice model.ChoiceRequest
	for _, choice := range session.Choices {
		completedChoice = choice
		break
	}
	if completedChoice.Status != "completed" || len(completedChoice.Answers) != 1 || completedChoice.Answers[0].Value != "capability" {
		t.Fatalf("expected stored completed choice answer, got %#v", completedChoice)
	}
}

func TestDuplicateAskUserQuestionReusesCompletedAnswer(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-react-ask-user-duplicate"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetThread(sessionID, "thread-ask-user-duplicate")
	app.store.SetTurn(sessionID, "turn-ask-user-duplicate")
	app.startRuntimeTurn(sessionID, model.ServerLocalHostID)

	var respondedRawID string
	var respondedPayload map[string]any
	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		respondedRawID = rawID
		respondedPayload, _ = result.(map[string]any)
		return nil
	}

	args := map[string]any{
		"threadId": "thread-ask-user-duplicate",
		"turnId":   "turn-ask-user-duplicate",
		"tool":     "ask_user_question",
		"arguments": map[string]any{
			"questions": []any{
				map[string]any{
					"header":   "确认意图",
					"question": "你希望我怎么处理这个 PostgreSQL（pg）不同步问题？",
					"options": []any{
						map[string]any{"label": "只读诊断", "value": "readonly"},
						map[string]any{"label": "准备修复", "value": "repair_plan"},
					},
				},
			},
		},
	}
	app.handleDynamicToolCall("raw-ask-user-duplicate-1", args)

	var choiceID string
	session := app.store.Session(sessionID)
	for _, card := range session.Cards {
		if card.Type == "ChoiceCard" {
			choiceID = card.RequestID
			break
		}
	}
	if choiceID == "" {
		t.Fatalf("expected first ask_user_question to create a choice card, got %#v", session.Cards)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/choices/"+choiceID+"/answer", strings.NewReader(`{"answers":[{"value":"repair_plan","label":"准备修复"}]}`))
	rec := httptest.NewRecorder()
	app.handleChoiceAnswer(rec, req, sessionID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 answering choice, got %d body=%s", rec.Code, rec.Body.String())
	}

	respondedRawID = ""
	respondedPayload = nil
	app.handleDynamicToolCall("raw-ask-user-duplicate-2", args)

	if respondedRawID != "raw-ask-user-duplicate-2" {
		t.Fatalf("expected duplicate ask to be answered immediately, got raw id %q", respondedRawID)
	}
	decodedPayload := decodeStructuredToolResponsePayload(t, respondedPayload)
	summaryAny, ok := decodedPayload["answer_summary"].([]any)
	summary := make([]string, 0, len(summaryAny))
	for _, item := range summaryAny {
		summary = append(summary, fmt.Sprint(item))
	}
	if !ok || len(summary) != 1 || !strings.Contains(summary[0], "准备修复") {
		t.Fatalf("expected duplicate ask to reuse completed answer summary, got %#v", decodedPayload)
	}
	if instruction := fmt.Sprint(decodedPayload["instruction"]); !strings.Contains(instruction, "do not ask the same clarification question again") {
		t.Fatalf("expected anti-repeat instruction, got %#v", decodedPayload)
	}
	if got := fmt.Sprint(decodedPayload["next_required_tool"]); got != "enter_plan_mode" {
		t.Fatalf("expected duplicate repair ask to require enter_plan_mode, got %#v", decodedPayload)
	}
	choiceCards := 0
	for _, card := range app.store.Session(sessionID).Cards {
		if card.Type == "ChoiceCard" {
			choiceCards++
		}
	}
	if choiceCards != 1 {
		t.Fatalf("expected duplicate ask not to create another choice card, got %d cards=%#v", choiceCards, app.store.Session(sessionID).Cards)
	}
}

func TestChoiceAnswerRepairIntentRequiresEnterPlanModeInstruction(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-react-repair-choice"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	now := model.NowString()
	choice := model.ChoiceRequest{
		ID:           "choice-repair-plan",
		RequestIDRaw: "raw-choice-repair-plan",
		ItemID:       "choice-repair-plan",
		Status:       "pending",
		Questions: []model.ChoiceQuestion{{
			Header:   "确认意图",
			Question: "你希望我怎么处理 PostgreSQL 不同步的问题？",
			Options: []model.ChoiceOption{
				{Label: "只读诊断", Value: "readonly"},
				{Label: "准备修复", Value: "repair_plan"},
			},
		}},
		RequestedAt: now,
	}
	app.store.AddChoice(sessionID, choice)
	app.store.UpsertCard(sessionID, model.Card{
		ID:        choice.ItemID,
		Type:      "ChoiceCard",
		RequestID: choice.ID,
		Status:    "pending",
		Questions: choice.Questions,
		CreatedAt: now,
		UpdatedAt: now,
	})

	var respondedPayload map[string]any
	app.codexRespondFunc = func(_ context.Context, _ string, result any) error {
		respondedPayload, _ = result.(map[string]any)
		return nil
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/choices/"+choice.ID+"/answer", strings.NewReader(`{"answers":[{"value":"repair_plan","label":"准备修复"}]}`))
	rec := httptest.NewRecorder()
	app.handleChoiceAnswer(rec, req, sessionID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 answering repair choice, got %d body=%s", rec.Code, rec.Body.String())
	}
	decodedPayload := decodeStructuredToolResponsePayload(t, respondedPayload)
	if got := fmt.Sprint(decodedPayload["next_required_tool"]); got != "enter_plan_mode" {
		t.Fatalf("expected repair choice to require enter_plan_mode, got %#v", decodedPayload)
	}
	if got := fmt.Sprint(decodedPayload["permission_scope"]); got != "planning_only" {
		t.Fatalf("expected planning-only scope, got %#v", decodedPayload)
	}
	if instruction := fmt.Sprint(decodedPayload["instruction"]); !strings.Contains(instruction, "MUST be a tool call to enter_plan_mode") {
		t.Fatalf("expected hard plan-mode instruction, got %#v", decodedPayload)
	}
}

func TestWorkspacePlanModeToolsCreateApprovalAndGateDispatch(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-react-plan-mode"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetThread(sessionID, "thread-plan-mode")
	app.store.SetTurn(sessionID, "turn-plan-mode")
	app.startRuntimeTurn(sessionID, model.ServerLocalHostID)

	var respondedRawID string
	var respondedPayload map[string]any
	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		respondedRawID = rawID
		respondedPayload, _ = result.(map[string]any)
		return nil
	}

	app.handleDynamicToolCall("raw-enter-plan", map[string]any{
		"threadId": "thread-plan-mode",
		"turnId":   "turn-plan-mode",
		"callId":   "enter-plan-call",
		"tool":     "enter_plan_mode",
		"arguments": map[string]any{
			"goal":   "修复 PG 同步问题",
			"reason": "涉及数据库同步和潜在生产风险，需要先计划。",
			"scope":  "只读定位和计划审批。",
		},
	})
	if respondedRawID != "raw-enter-plan" {
		t.Fatalf("expected enter_plan_mode response, got rawID=%q payload=%#v", respondedRawID, respondedPayload)
	}
	if phase := app.store.Session(sessionID).Runtime.Turn.Phase; phase != "planning" {
		t.Fatalf("expected planning phase after enter_plan_mode, got %q", phase)
	}

	app.handleDynamicToolCall("raw-update-plan", map[string]any{
		"threadId": "thread-plan-mode",
		"turnId":   "turn-plan-mode",
		"callId":   "update-plan-call",
		"tool":     "update_plan",
		"arguments": map[string]any{
			"title":      "PG 同步修复计划",
			"summary":    "先只读确认复制状态，再根据结果选择修复路径。",
			"risk":       "错误操作可能影响数据库复制。",
			"rollback":   "保留现状，不执行 mutation。",
			"validation": "确认 replication lag 和 slot 状态恢复正常。",
			"steps": []any{
				map[string]any{"id": "step-1", "title": "只读检查复制状态", "status": "pending", "hostId": "server-local"},
			},
		},
	})
	if respondedRawID != "raw-update-plan" {
		t.Fatalf("expected update_plan response, got rawID=%q payload=%#v", respondedRawID, respondedPayload)
	}

	app.handleDynamicToolCall("raw-exit-plan", map[string]any{
		"threadId": "thread-plan-mode",
		"turnId":   "turn-plan-mode",
		"callId":   "exit-plan-call",
		"tool":     "exit_plan_mode",
		"arguments": map[string]any{
			"title":      "批准 PG 同步修复计划",
			"summary":    "计划先做只读确认，审批后再派发执行 worker。",
			"validation": "执行后确认 PG 同步恢复。",
			"risk":       "数据库同步操作有生产风险。",
			"rollback":   "若发现风险则停止执行并回到只读诊断。",
			"tasks": []any{
				map[string]any{"taskId": "task-pg", "hostId": "host-1", "title": "诊断 PG 同步", "instruction": "只读检查 PG 同步状态"},
			},
		},
	})
	session := app.store.Session(sessionID)
	if session.Runtime.Turn.Phase != "waiting_approval" {
		t.Fatalf("expected waiting_approval phase after exit_plan_mode, got %q", session.Runtime.Turn.Phase)
	}
	if !app.workspacePlanApprovalPending(sessionID) || !app.workspacePlanModeNeedsApproval(sessionID) {
		t.Fatalf("expected pending plan approval to gate dispatch")
	}
	var approvalID string
	for _, approval := range session.Approvals {
		if approval.Type == "plan_exit" {
			approvalID = approval.ID
		}
	}
	if approvalID == "" {
		t.Fatalf("expected plan_exit approval, got %#v", session.Approvals)
	}
	snapshot := app.snapshot(sessionID)
	foundExitInvocation := false
	for _, invocation := range snapshot.ToolInvocations {
		if invocation.Name == "exit_plan_mode" && invocation.Status == "waiting_approval" {
			foundExitInvocation = true
			break
		}
	}
	if !foundExitInvocation {
		t.Fatalf("expected exit_plan_mode waiting approval invocation, got %#v", snapshot.ToolInvocations)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/approvals/"+approvalID+"/decision", strings.NewReader(`{"decision":"accept"}`))
	rec := httptest.NewRecorder()
	app.handleApprovalDecision(rec, req, sessionID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected approval decision 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	decodedApprovalPayload := decodeStructuredToolResponsePayload(t, respondedPayload)
	if respondedRawID != "raw-exit-plan" || decodedApprovalPayload["decision"] != "accept" {
		t.Fatalf("expected approval to respond to exit_plan_mode, got rawID=%q payload=%#v", respondedRawID, respondedPayload)
	}
	if got := fmt.Sprint(decodedApprovalPayload["next_mode"]); got != "execute" {
		t.Fatalf("expected accepted plan approval to return execute next_mode, got %#v", decodedApprovalPayload)
	}
	if app.workspacePlanApprovalPending(sessionID) || app.workspacePlanModeNeedsApproval(sessionID) {
		t.Fatalf("expected accepted plan approval to allow dispatch")
	}
	if phase := app.store.Session(sessionID).Runtime.Turn.Phase; phase != "executing" {
		t.Fatalf("expected executing phase after plan approval, got %q", phase)
	}
}

func TestWorkspaceExitPlanModeRequiresCompletePlanSections(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-react-exit-plan-validation"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetThread(sessionID, "thread-exit-plan-validation")
	app.store.SetTurn(sessionID, "turn-exit-plan-validation")
	app.startRuntimeTurn(sessionID, model.ServerLocalHostID)

	var respondedPayload map[string]any
	app.codexRespondFunc = func(_ context.Context, _ string, result any) error {
		respondedPayload, _ = result.(map[string]any)
		return nil
	}

	app.handleDynamicToolCall("raw-enter-plan-validation", map[string]any{
		"threadId": "thread-exit-plan-validation",
		"turnId":   "turn-exit-plan-validation",
		"callId":   "enter-plan-validation",
		"tool":     "enter_plan_mode",
		"arguments": map[string]any{
			"goal":   "修复 PG 同步问题",
			"reason": "需要先计划。",
		},
	})
	app.handleDynamicToolCall("raw-update-plan-validation", map[string]any{
		"threadId": "thread-exit-plan-validation",
		"turnId":   "turn-exit-plan-validation",
		"callId":   "update-plan-validation",
		"tool":     "update_plan",
		"arguments": map[string]any{
			"title":      "PG 同步修复计划",
			"summary":    "先只读确认复制状态。",
			"risk":       "数据库同步操作有生产风险。",
			"rollback":   "停止执行并保持现状。",
			"validation": "确认复制状态恢复。",
			"steps": []any{
				map[string]any{"id": "step-1", "title": "只读检查复制状态"},
			},
		},
	})

	app.handleDynamicToolCall("raw-exit-plan-incomplete", map[string]any{
		"threadId": "thread-exit-plan-validation",
		"turnId":   "turn-exit-plan-validation",
		"callId":   "exit-plan-incomplete",
		"tool":     "exit_plan_mode",
		"arguments": map[string]any{
			"title":      "批准 PG 同步修复计划",
			"summary":    "计划先做只读确认，审批后再派发执行 worker。",
			"validation": "执行后确认 PG 同步恢复。",
			"risk":       "数据库同步操作有生产风险。",
			"tasks": []any{
				map[string]any{"taskId": "task-pg", "hostId": "host-1", "instruction": "只读检查 PG 同步状态"},
			},
		},
	})
	if respondedPayload["success"] != false {
		t.Fatalf("expected incomplete exit_plan_mode to fail, got %#v", respondedPayload)
	}
	if text := toolResponseText(t, respondedPayload); !strings.Contains(text, "requires rollback") {
		t.Fatalf("expected rollback validation error, got %q", text)
	}
	session := app.store.Session(sessionID)
	if phase := session.Runtime.Turn.Phase; phase != "planning" {
		t.Fatalf("expected incomplete exit_plan_mode to keep planning phase, got %q", phase)
	}
	for _, approval := range session.Approvals {
		if approval.Type == "plan_exit" && approval.Status == "pending" {
			t.Fatalf("incomplete exit_plan_mode must not create pending plan approval: %#v", approval)
		}
	}
}

func TestWorkspacePlanModeRejectsMutationApproval(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-react-plan-blocks-mutation"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetThread(sessionID, "thread-plan-blocks-mutation")
	app.store.SetTurn(sessionID, "turn-plan-blocks-mutation")
	app.startRuntimeTurn(sessionID, model.ServerLocalHostID)

	var respondedRawID string
	var respondedPayload map[string]any
	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		respondedRawID = rawID
		respondedPayload, _ = result.(map[string]any)
		return nil
	}

	app.handleDynamicToolCall("raw-enter-plan-block-mutation", map[string]any{
		"threadId": "thread-plan-blocks-mutation",
		"turnId":   "turn-plan-blocks-mutation",
		"callId":   "enter-plan-block-mutation",
		"tool":     "enter_plan_mode",
		"arguments": map[string]any{
			"goal":   "修复 PG 同步问题",
			"reason": "数据库同步修复属于高风险操作，需要计划审批。",
		},
	})
	if phase := app.store.Session(sessionID).Runtime.Turn.Phase; phase != "planning" {
		t.Fatalf("expected planning phase after enter_plan_mode, got %q", phase)
	}

	payload := map[string]any{
		"threadId":           "thread-plan-blocks-mutation",
		"turnId":             "turn-plan-blocks-mutation",
		"itemId":             "cmd-plan-block-mutation",
		"command":            "touch /tmp/aiops-codex-plan-mode-blocked",
		"cwd":                t.TempDir(),
		"reason":             "尝试在计划审批前修改系统状态",
		"availableDecisions": []any{"accept", "decline"},
	}
	app.handleLocalCommandApprovalRequest("raw-plan-mode-mutation-approval", payload)

	if strings.Trim(respondedRawID, `"`) != "raw-plan-mode-mutation-approval" || respondedPayload["decision"] != "decline" {
		t.Fatalf("expected plan mode mutation approval to be declined, got rawID=%q payload=%#v", respondedRawID, respondedPayload)
	}
	session := app.store.Session(sessionID)
	if session.Runtime.Turn.Phase != "planning" {
		t.Fatalf("expected plan mode to remain planning after mutation rejection, got %q", session.Runtime.Turn.Phase)
	}
	var blockedApproval *model.ApprovalRequest
	for _, approval := range session.Approvals {
		if approval.Type == "command" {
			copyApproval := approval
			blockedApproval = &copyApproval
			break
		}
	}
	if blockedApproval == nil || blockedApproval.Status != "blocked_by_plan_mode" {
		t.Fatalf("expected command approval blocked by plan mode, got %#v", session.Approvals)
	}
	for _, card := range session.Cards {
		if card.Type == "CommandApprovalCard" && card.Status == "pending" {
			t.Fatalf("plan mode mutation must not create pending command approval card, got %#v", card)
		}
	}
	foundError := false
	for _, card := range session.Cards {
		if card.Type == "ErrorCard" && strings.Contains(card.Text, "计划模式") {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Fatalf("expected plan-mode block error card, got %#v", session.Cards)
	}
}

func TestWorkspacePlanExitApprovalDeclineReturnsToPlanning(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-react-plan-decline"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetThread(sessionID, "thread-plan-decline")
	app.store.SetTurn(sessionID, "turn-plan-decline")
	app.startRuntimeTurn(sessionID, model.ServerLocalHostID)

	now := model.NowString()
	app.store.UpsertCard(sessionID, model.Card{
		ID:      "plan-card-decline",
		Type:    "PlanCard",
		Title:   "PG 同步修复计划",
		Summary: "先只读确认复制状态，再申请执行。",
		Status:  "planning",
		Detail: map[string]any{
			"tool": "update_plan",
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
	approval := model.ApprovalRequest{
		ID:           "approval-plan-decline",
		RequestIDRaw: "raw-exit-plan-decline",
		HostID:       model.ServerLocalHostID,
		Type:         "plan_exit",
		Status:       "pending",
		ThreadID:     "thread-plan-decline",
		TurnID:       "turn-plan-decline",
		ItemID:       "plan-approval-decline",
		Reason:       "批准 PG 同步修复计划",
		Decisions:    []string{"accept", "decline"},
		RequestedAt:  now,
	}
	app.store.AddApproval(sessionID, approval)
	app.store.UpsertCard(sessionID, model.Card{
		ID:      approval.ItemID,
		Type:    "PlanApprovalCard",
		Title:   "批准 PG 同步修复计划",
		Text:    "先只读确认复制状态，审批后派发 worker。",
		Summary: "PG 同步修复计划",
		Status:  "pending",
		Approval: &model.ApprovalRef{
			RequestID: approval.ID,
			Type:      approval.Type,
			Decisions: approval.Decisions,
		},
		Detail: map[string]any{
			"tool":       "exit_plan_mode",
			"summary":    "先只读确认复制状态，审批后派发 worker。",
			"validation": "确认 replication lag 恢复正常。",
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
	app.setRuntimeTurnPhase(sessionID, "waiting_approval")

	var respondedRawID string
	var respondedPayload map[string]any
	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		respondedRawID = rawID
		respondedPayload, _ = result.(map[string]any)
		return nil
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/approvals/"+approval.ID+"/decision", strings.NewReader(`{"decision":"decline"}`))
	rec := httptest.NewRecorder()
	app.handleApprovalDecision(rec, req, sessionID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected approval decision 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	decodedPayload := decodeStructuredToolResponsePayload(t, respondedPayload)
	if respondedRawID != approval.RequestIDRaw || decodedPayload["decision"] != "decline" {
		t.Fatalf("expected decline response to exit_plan_mode, got rawID=%q payload=%#v", respondedRawID, decodedPayload)
	}
	if got := fmt.Sprint(decodedPayload["next_mode"]); got != "plan" {
		t.Fatalf("expected declined plan approval to return plan next_mode, got %#v", decodedPayload)
	}
	if instruction := fmt.Sprint(decodedPayload["instruction"]); !strings.Contains(instruction, "Do not execute") || !strings.Contains(instruction, "Continue in plan mode") {
		t.Fatalf("expected decline instruction to keep planning and block execution, got %#v", decodedPayload)
	}
	if phase := app.store.Session(sessionID).Runtime.Turn.Phase; phase != "planning" {
		t.Fatalf("expected planning phase after plan rejection, got %q", phase)
	}
	if app.workspacePlanApprovalPending(sessionID) {
		t.Fatalf("expected declined plan approval to clear pending approval")
	}
	if !app.workspacePlanModeNeedsApproval(sessionID) {
		t.Fatalf("expected declined plan approval to keep plan-mode approval gate active")
	}
}

func TestReadonlyHostInspectRunsServerLocalReadOnlyCommand(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-react-readonly-local"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetThread(sessionID, "thread-readonly-local")
	app.store.SetTurn(sessionID, "turn-readonly-local")
	app.startRuntimeTurn(sessionID, model.ServerLocalHostID)

	var respondedPayload map[string]any
	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		if rawID != "raw-readonly-local" {
			t.Fatalf("unexpected raw id %q", rawID)
		}
		respondedPayload, _ = result.(map[string]any)
		return nil
	}

	cwd := t.TempDir()
	app.handleDynamicToolCall("raw-readonly-local", map[string]any{
		"threadId": "thread-readonly-local",
		"turnId":   "turn-readonly-local",
		"callId":   "call-readonly-local",
		"tool":     "readonly_host_inspect",
		"arguments": map[string]any{
			"host":    model.ServerLocalHostID,
			"target":  "system_load",
			"command": "pwd",
			"cwd":     cwd,
			"reason":  "confirm readonly local execution context",
		},
	})

	if respondedPayload["success"] != true {
		t.Fatalf("expected readonly local inspect to succeed, got %#v", respondedPayload)
	}
	if text := toolResponseText(t, respondedPayload); !strings.Contains(text, "Host command `pwd` completed") || !strings.Contains(text, cwd) {
		t.Fatalf("expected readonly local command output, got %q", text)
	}
	card := app.cardByID(sessionID, dynamicToolCardID("call-readonly-local"))
	if card == nil || card.Type != "CommandCard" || card.Status != "completed" {
		t.Fatalf("expected completed CommandCard, got %#v", card)
	}
	if tool := getStringAny(card.Detail, "tool"); tool != "readonly_host_inspect" {
		t.Fatalf("expected card detail tool readonly_host_inspect, got %#v", card.Detail)
	}
	snapshot := app.snapshot(sessionID)
	foundInvocation := false
	for _, invocation := range snapshot.ToolInvocations {
		if invocation.Name == "readonly_host_inspect" && invocation.Status == "completed" && strings.Contains(invocation.InputSummary, "pwd") {
			foundInvocation = true
			break
		}
	}
	if !foundInvocation {
		t.Fatalf("expected readonly_host_inspect invocation, got %#v", snapshot.ToolInvocations)
	}
}

func TestReadonlyHostInspectRejectsMutationCommand(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-react-readonly-reject-mutation"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetThread(sessionID, "thread-readonly-reject-mutation")
	app.store.SetTurn(sessionID, "turn-readonly-reject-mutation")
	app.startRuntimeTurn(sessionID, model.ServerLocalHostID)

	var respondedPayload map[string]any
	app.codexRespondFunc = func(_ context.Context, rawID string, result any) error {
		if rawID != "raw-readonly-reject-mutation" {
			t.Fatalf("unexpected raw id %q", rawID)
		}
		respondedPayload, _ = result.(map[string]any)
		return nil
	}

	app.handleDynamicToolCall("raw-readonly-reject-mutation", map[string]any{
		"threadId": "thread-readonly-reject-mutation",
		"turnId":   "turn-readonly-reject-mutation",
		"callId":   "call-readonly-reject-mutation",
		"tool":     "readonly_host_inspect",
		"arguments": map[string]any{
			"host":    model.ServerLocalHostID,
			"target":  "filesystem",
			"command": "touch /tmp/aiops-codex-readonly-reject",
			"cwd":     t.TempDir(),
			"reason":  "attempt mutation",
		},
	})

	if respondedPayload["success"] != false {
		t.Fatalf("expected mutation command to be rejected, got %#v", respondedPayload)
	}
	if text := toolResponseText(t, respondedPayload); !strings.Contains(text, "not read-only") {
		t.Fatalf("expected readonly rejection message, got %q", text)
	}
	if card := app.cardByID(sessionID, dynamicToolCardID("call-readonly-reject-mutation")); card != nil {
		t.Fatalf("mutation rejection must not create a command card, got %#v", card)
	}
}
func TestReActLoopMaxTokensRecoveryAction(t *testing.T) {
	state := &reActLoopState{
		Request: reActLoopRequest{
			SessionID: "test-max-tokens",
		},
		Iteration:     1,
		RecoveryCount: 0,
	}

	err := fmt.Errorf("max_tokens exceeded")
	action := recoverFromError(err, state, defaultErrorRecoveryConfig())

	if action.Action != "inject_message" {
		t.Fatalf("expected inject_message action for max_tokens, got %q", action.Action)
	}
	if action.Message == "" {
		t.Fatal("expected non-empty recovery message")
	}
	if !strings.Contains(action.Message, "Resume directly") {
		t.Fatalf("expected recovery message to contain 'Resume directly', got %q", action.Message)
	}
}

func TestReActLoopPromptTooLongCompactRetry(t *testing.T) {
	state := &reActLoopState{
		Request: reActLoopRequest{
			SessionID: "test-prompt-long",
		},
		Iteration:     1,
		RecoveryCount: 0,
	}

	err := fmt.Errorf("prompt_too_long: context exceeds limit")
	action := recoverFromError(err, state, defaultErrorRecoveryConfig())

	if action.Action != "compact_retry" {
		t.Fatalf("expected compact_retry action for prompt_too_long, got %q", action.Action)
	}

	// After max retries, should circuit break
	state.RecoveryCount = 3
	action = recoverFromError(err, state, defaultErrorRecoveryConfig())
	if action.Action != "circuit_break" {
		t.Fatalf("expected circuit_break after max retries, got %q", action.Action)
	}
}

func TestReActLoopAppServerDisconnectTerminates(t *testing.T) {
	state := &reActLoopState{
		Request: reActLoopRequest{
			SessionID: "test-disconnect",
		},
		Iteration:     1,
		RecoveryCount: 0,
	}

	err := fmt.Errorf("connection refused: app-server unavailable")
	action := recoverFromError(err, state, defaultErrorRecoveryConfig())

	if action.Action != "terminate" {
		t.Fatalf("expected terminate action for disconnect, got %q", action.Action)
	}
	if !strings.Contains(action.Evidence, "disconnected") {
		t.Fatalf("expected disconnect evidence, got %q", action.Evidence)
	}
}

func TestReActLoopModelOverloadFallback(t *testing.T) {
	state := &reActLoopState{
		Request: reActLoopRequest{
			SessionID: "test-overload",
		},
		Iteration:     1,
		RecoveryCount: 0,
	}

	err := fmt.Errorf("model overloaded, please retry")
	action := recoverFromError(err, state, defaultErrorRecoveryConfig())

	if action.Action != "fallback_model" {
		t.Fatalf("expected fallback_model action for overload, got %q", action.Action)
	}
	if action.FallbackModel == "" {
		t.Fatal("expected non-empty fallback model")
	}
}

func TestApplyRecoveryActionInjectsMessage(t *testing.T) {
	state := &reActLoopState{
		Request: reActLoopRequest{
			SessionID: "test-apply-recovery",
		},
		Messages: []map[string]any{
			{"role": "user", "content": "hello"},
		},
		RecoveryCount: 0,
	}

	action := errorRecoveryAction{
		Action:  "inject_message",
		Message: "Resume directly.",
	}

	applyRecoveryAction(action, state, nil)

	if !state.NeedsFollowUp {
		t.Fatal("expected NeedsFollowUp=true after inject_message")
	}
	if state.RecoveryCount != 1 {
		t.Fatalf("expected RecoveryCount=1, got %d", state.RecoveryCount)
	}
	if len(state.Messages) != 2 {
		t.Fatalf("expected 2 messages after injection, got %d", len(state.Messages))
	}
	injected := state.Messages[1]["content"].(string)
	if injected != "Resume directly." {
		t.Fatalf("expected injected message, got %q", injected)
	}
}

func TestToolDispatcherCategorization(t *testing.T) {
	tests := []struct {
		tool     string
		expected toolDispatchCategory
	}{
		{"ask_user_question", toolCategoryBlocking},
		{"exit_plan_mode", toolCategoryApproval},
		{"request_approval", toolCategoryApproval},
		{"query_ai_server_state", toolCategoryReadonly},
		{"readonly_host_inspect", toolCategoryReadonly},
		{"enter_plan_mode", toolCategoryReadonly},
		{"update_plan", toolCategoryReadonly},
		{"orchestrator_dispatch_tasks", toolCategoryMutation},
		{"execute_system_mutation", toolCategoryMutation},
		{"unknown_tool", toolCategoryMutation},
	}

	for _, tt := range tests {
		got := categorizeToolForDispatch(tt.tool)
		if got != tt.expected {
			t.Errorf("categorizeToolForDispatch(%q) = %q, want %q", tt.tool, got, tt.expected)
		}
	}
}
