package server

import (
	"context"
	"strings"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/agentloop"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func newWorkspaceTurnPolicyTestSession(app *App, sessionID string) {
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetSelectedHost(sessionID, model.ServerLocalHostID)
}

func TestBuildWorkspaceTurnPolicyClassifiesRealtimeFactual(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-policy-realtime"
	newWorkspaceTurnPolicyTestSession(app, sessionID)

	policy := app.buildWorkspaceTurnPolicy(sessionID, model.ServerLocalHostID, "最新 BTC 价格是多少？")
	if policy.IntentClass != string(model.TurnIntentFactual) {
		t.Fatalf("expected factual intent, got %#v", policy)
	}
	if policy.Lane != string(model.TurnLaneReadonly) {
		t.Fatalf("expected readonly lane, got %#v", policy)
	}
	if !policy.RequiresRealtimeData || !policy.RequiresExternalFacts {
		t.Fatalf("expected realtime external fact requirement, got %#v", policy)
	}
	if len(policy.RequiredTools) != 1 || policy.RequiredTools[0] != "web_search" {
		t.Fatalf("expected web_search requirement, got %#v", policy.RequiredTools)
	}
}

func TestBuildWorkspaceTurnPolicyClassifiesDesignIntoPlanLane(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-policy-design"
	newWorkspaceTurnPolicyTestSession(app, sessionID)

	policy := app.buildWorkspaceTurnPolicy(sessionID, model.ServerLocalHostID, "给我一个订单服务延迟排障方案，要求有回滚和 10 分钟窗口。")
	if policy.IntentClass != string(model.TurnIntentDesign) {
		t.Fatalf("expected design intent, got %#v", policy)
	}
	if policy.Lane != string(model.TurnLanePlan) {
		t.Fatalf("expected plan lane, got %#v", policy)
	}
	if !policy.NeedsPlanArtifact || !policy.NeedsAssumptions {
		t.Fatalf("expected plan artifact and assumptions requirements, got %#v", policy)
	}
	if policy.RequiredNextTool != "update_plan" {
		t.Fatalf("expected update_plan as required tool, got %#v", policy)
	}
}

func TestBuildWorkspaceTurnPolicyClassifiesRiskyExec(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-policy-risky"
	newWorkspaceTurnPolicyTestSession(app, sessionID)

	policy := app.buildWorkspaceTurnPolicy(sessionID, model.ServerLocalHostID, "重启 payment 服务")
	if policy.IntentClass != string(model.TurnIntentRiskyExec) {
		t.Fatalf("expected risky_exec intent, got %#v", policy)
	}
	if policy.Lane != string(model.TurnLanePlan) {
		t.Fatalf("expected risky_exec to enter plan lane, got %#v", policy)
	}
	if !policy.NeedsApproval || !policy.NeedsPlanArtifact {
		t.Fatalf("expected approval + plan requirements, got %#v", policy)
	}
}

func TestBuildWorkspaceTurnPolicyClassifiesAmbiguous(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-policy-ambiguous"
	newWorkspaceTurnPolicyTestSession(app, sessionID)

	policy := app.buildWorkspaceTurnPolicy(sessionID, model.ServerLocalHostID, "你有办法处理线上 pg 同步问题吗？")
	if policy.IntentClass != string(model.TurnIntentAmbiguous) {
		t.Fatalf("expected ambiguous intent, got %#v", policy)
	}
	if !policy.NeedsDisambiguation || policy.RequiredNextTool != "ask_user_question" {
		t.Fatalf("expected ask_user_question requirement, got %#v", policy)
	}
}

func TestWorkspaceVisibleToolNamesFollowLane(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-policy-tools"
	newWorkspaceTurnPolicyTestSession(app, sessionID)

	planTools := app.workspaceVisibleToolNames(sessionID, model.TurnPolicy{
		IntentClass: string(model.TurnIntentDesign),
		Lane:        string(model.TurnLanePlan),
	})
	if !containsStringValue(planTools, "update_plan") || containsStringValue(planTools, "orchestrator_dispatch_tasks") {
		t.Fatalf("expected plan lane tool exposure, got %#v", planTools)
	}

	executeTools := app.workspaceVisibleToolNames(sessionID, model.TurnPolicy{
		IntentClass: string(model.TurnIntentRiskyExec),
		Lane:        string(model.TurnLaneExecute),
	})
	if !containsStringValue(executeTools, "orchestrator_dispatch_tasks") {
		t.Fatalf("expected execute lane to expose dispatch, got %#v", executeTools)
	}
}

func TestBuildWorkspacePromptEnvelopeIncludesRuntimePolicyAndToolVisibility(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-policy-envelope"
	newWorkspaceTurnPolicyTestSession(app, sessionID)
	policy := app.buildWorkspaceTurnPolicy(sessionID, model.ServerLocalHostID, "给我一个订单服务延迟排障方案")

	envelope := app.buildWorkspacePromptEnvelope(sessionID, model.ServerLocalHostID, "给我一个订单服务延迟排障方案", policy, true)
	if envelope == nil {
		t.Fatal("expected prompt envelope")
	}
	if envelope.IntentClass != string(model.TurnIntentDesign) || envelope.CurrentLane != string(model.TurnLanePlan) {
		t.Fatalf("unexpected envelope routing: %#v", envelope)
	}
	if envelope.RuntimePolicy == nil || !strings.Contains(envelope.RuntimePolicy.Content, "intentClass=design") {
		t.Fatalf("expected runtime policy section, got %#v", envelope.RuntimePolicy)
	}
	if len(envelope.VisibleTools) == 0 || len(envelope.HiddenTools) == 0 {
		t.Fatalf("expected visible and hidden tool views, got %#v", envelope)
	}
	rendered := renderPromptEnvelope(envelope)
	if !strings.Contains(rendered, "[VisibleTools]") || !strings.Contains(rendered, "[RuntimePolicy]") {
		t.Fatalf("expected rendered prompt to contain debug sections, got:\n%s", rendered)
	}
}

func TestBuildWorkspacePromptEnvelopeAddsLaneRehydrateContextOnTransition(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-policy-rehydrate"
	newWorkspaceTurnPolicyTestSession(app, sessionID)
	app.store.UpdateRuntime(sessionID, func(rt *model.RuntimeState) {
		rt.TurnPolicy = model.TurnPolicy{
			IntentClass: string(model.TurnIntentFactual),
			Lane:        string(model.TurnLaneReadonly),
		}
	})

	policy := app.buildWorkspaceTurnPolicy(sessionID, model.ServerLocalHostID, "给我一个订单服务延迟排障方案")
	envelope := app.buildWorkspacePromptEnvelope(sessionID, model.ServerLocalHostID, "给我一个订单服务延迟排障方案", policy, true)
	if envelope == nil {
		t.Fatal("expected prompt envelope")
	}
	var rehydrate *model.PromptEnvelopeSection
	for i := range envelope.ContextAttachments {
		if envelope.ContextAttachments[i].Name == "LaneRehydrate" {
			rehydrate = &envelope.ContextAttachments[i]
			break
		}
	}
	if rehydrate == nil || !strings.Contains(rehydrate.Content, "transition=readonly->plan") {
		t.Fatalf("expected lane rehydrate attachment, got %#v", envelope.ContextAttachments)
	}
	if !strings.Contains(envelope.CompressionState, "rehydrated") {
		t.Fatalf("expected compression state to record rehydrate, got %#v", envelope.CompressionState)
	}
}

func TestValidateTurnCompletionBlocksWithoutWebSearchEvidence(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-policy-final-gate-search"
	newWorkspaceTurnPolicyTestSession(app, sessionID)
	app.store.UpdateRuntime(sessionID, func(rt *model.RuntimeState) {
		rt.TurnPolicy = model.TurnPolicy{
			IntentClass:           string(model.TurnIntentFactual),
			Lane:                  string(model.TurnLaneReadonly),
			RequiredTools:         []string{"web_search"},
			RequiresExternalFacts: true,
			RequiresRealtimeData:  true,
			RequiredNextTool:      "web_search",
			FinalGateStatus:       turnFinalGatePending,
		}
	})

	decision := app.ValidateTurnCompletion(context.Background(), &agentloop.Session{ID: sessionID}, "最新 BTC 价格", "先直接回答")
	if decision.Action != "continue" || !strings.Contains(decision.RepairMessage, "web_search") {
		t.Fatalf("expected blocked final gate with web_search repair, got %#v", decision)
	}
	snapshot := app.snapshot(sessionID)
	if snapshot.FinalGateStatus != turnFinalGateBlocked {
		t.Fatalf("expected blocked final gate status, got %#v", snapshot)
	}
	if !containsStringValue(snapshot.MissingRequirements, "缺少外部实时证据") {
		t.Fatalf("expected missing realtime evidence, got %#v", snapshot.MissingRequirements)
	}
}

func TestValidateTurnCompletionPassesWithPlanArtifactAndAssumptions(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-policy-final-gate-plan"
	newWorkspaceTurnPolicyTestSession(app, sessionID)
	app.store.UpsertCard(sessionID, model.Card{
		ID:      "plan-card-fg",
		Type:    "PlanCard",
		Status:  "completed",
		Summary: "分批升级方案",
		Detail: map[string]any{
			"tool":        "update_plan",
			"assumptions": "维护窗口 10 分钟，可回滚到上一版本。",
		},
		CreatedAt: "2026-04-17T10:00:00Z",
		UpdatedAt: "2026-04-17T10:00:00Z",
	})
	app.store.UpdateRuntime(sessionID, func(rt *model.RuntimeState) {
		rt.TurnPolicy = model.TurnPolicy{
			IntentClass:       string(model.TurnIntentDesign),
			Lane:              string(model.TurnLanePlan),
			NeedsPlanArtifact: true,
			NeedsAssumptions:  true,
			FinalGateStatus:   turnFinalGatePending,
		}
	})

	decision := app.ValidateTurnCompletion(context.Background(), &agentloop.Session{ID: sessionID}, "给我一个升级方案", "计划如下")
	if decision.Action != "pass" {
		t.Fatalf("expected final gate pass, got %#v", decision)
	}
	snapshot := app.snapshot(sessionID)
	if snapshot.FinalGateStatus != turnFinalGatePassed {
		t.Fatalf("expected passed final gate status, got %#v", snapshot)
	}
}

func TestPrepareWorkspaceTurnRuntimeProjectsLaneAndPromptEnvelope(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "workspace-policy-prepare"
	newWorkspaceTurnPolicyTestSession(app, sessionID)
	app.store.UpdateRuntime(sessionID, func(rt *model.RuntimeState) {
		rt.TurnPolicy = model.TurnPolicy{
			IntentClass: string(model.TurnIntentFactual),
			Lane:        string(model.TurnLaneAnswer),
		}
	})

	session := agentloop.NewSession(sessionID, agentloop.SessionSpec{
		Model:        "test-model",
		DynamicTools: []string{"query_ai_server_state", "update_plan"},
	})
	app.prepareWorkspaceTurnRuntime(context.Background(), session, chatRequest{
		Message: "给我一个订单服务延迟排障方案",
		HostID:  model.ServerLocalHostID,
	})
	if !containsStringValue(session.EnabledTools(), "update_plan") {
		t.Fatalf("expected update_plan to be visible after preparation, got %#v", session.EnabledTools())
	}
	if strings.TrimSpace(session.SystemPrompt()) == "" {
		t.Fatal("expected turn system prompt to be configured")
	}
	snapshot := app.snapshot(sessionID)
	if snapshot.CurrentLane != string(model.TurnLanePlan) {
		t.Fatalf("expected current lane projection, got %#v", snapshot)
	}
	if snapshot.PromptEnvelope == nil || snapshot.TurnPolicy == nil {
		t.Fatalf("expected prompt envelope + turn policy projection, got %#v", snapshot)
	}
	if !containsIncidentEventType(snapshot.IncidentEvents, "turn.lane.changed") {
		t.Fatalf("expected lane change incident event, got %#v", snapshot.IncidentEvents)
	}
}

func containsIncidentEventType(events []model.IncidentEvent, eventType string) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}
