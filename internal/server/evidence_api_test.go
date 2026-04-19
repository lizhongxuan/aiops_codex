package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/store"
)

func TestHandleEvidenceDetailFallsBackToProjectedEvidence(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "sess-evidence-detail-fallback"
	app.store.EnsureSession(sessionID)
	app.store.UpsertCard(sessionID, model.Card{
		ID:        "command-card-api",
		Type:      "CommandCard",
		Command:   "kubectl get pods",
		Output:    "pod-a Running\npod-b CrashLoopBackOff\n",
		Status:    "completed",
		CreatedAt: "2026-04-15T10:00:00Z",
		UpdatedAt: "2026-04-15T10:00:02Z",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID+"/evidence/evidence-command-card-api", nil)
	req.SetPathValue("evidenceID", "evidence-command-card-api")
	rec := httptest.NewRecorder()

	app.handleEvidenceDetail(rec, req, sessionID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := payload["citationKey"]; got != "E-EVIDENCE-COMMAND-CARD-API" {
		t.Fatalf("expected stable citation key, got %#v", got)
	}
	if got := payload["content"]; got == nil || got == "" {
		t.Fatalf("expected full content in evidence detail, got %#v", payload)
	}
	if got := payload["card"]; got == nil {
		t.Fatalf("expected source card to be included in fallback payload, got %#v", payload)
	}
}

func sessionDetailRequest(app *App, browserID, path string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{
		Name:  app.cfg.SessionCookieName,
		Value: app.signSessionCookie(browserID),
		Path:  "/",
	})
	return req
}

func TestSessionDetailRoutesServeLegacyEvidenceAndVerificationAPIs(t *testing.T) {
	app := newOrchestratorTestApp(t)
	browserID := "browser-detail-routes"
	session := app.store.CreateSessionWithMeta(browserID, model.SessionMeta{
		Kind:    model.SessionKindWorkspace,
		Visible: true,
	}, true)
	sessionID := session.ID
	app.store.UpsertCard(sessionID, model.Card{
		ID:        "command-card-route",
		Type:      "CommandCard",
		Command:   "kubectl get pods",
		Output:    "pod-a Running\npod-b CrashLoopBackOff\n",
		Status:    "completed",
		CreatedAt: "2026-04-15T10:00:00Z",
		UpdatedAt: "2026-04-15T10:00:02Z",
	})
	app.store.UpsertVerificationRecord(sessionID, model.VerificationRecord{
		ID:        "verify-route-1",
		RunID:     "loop-" + sessionID,
		Status:    "passed",
		Strategy:  "health_probe",
		CreatedAt: "2026-04-15T10:00:03Z",
	})

	mux := http.NewServeMux()
	app.registerEvidenceRoutes(mux)

	evidenceReq := sessionDetailRequest(app, browserID, "/api/sessions/"+sessionID+"/evidence/evidence-command-card-route")
	evidenceRec := httptest.NewRecorder()
	mux.ServeHTTP(evidenceRec, evidenceReq)
	if evidenceRec.Code != http.StatusOK {
		t.Fatalf("expected legacy evidence route 200, got %d body=%s", evidenceRec.Code, evidenceRec.Body.String())
	}

	verificationReq := sessionDetailRequest(app, browserID, "/api/v1/sessions/"+sessionID+"/verification/verify-route-1")
	verificationRec := httptest.NewRecorder()
	mux.ServeHTTP(verificationRec, verificationReq)
	if verificationRec.Code != http.StatusOK {
		t.Fatalf("expected verification route 200, got %d body=%s", verificationRec.Code, verificationRec.Body.String())
	}
}

func TestIncidentTimelineRouteReturnsOwnedSessionEvents(t *testing.T) {
	app := newOrchestratorTestApp(t)
	browserID := "browser-timeline-route"
	session := app.store.CreateSessionWithMeta(browserID, model.SessionMeta{
		Kind:    model.SessionKindWorkspace,
		Visible: true,
	}, true)
	sessionID := session.ID
	app.store.UpsertIncidentEvent(sessionID, model.IncidentEvent{
		ID:        "evt-timeline-1",
		Type:      "cancel.requested",
		Status:    "completed",
		Stage:     "canceled",
		Summary:   "用户停止了当前任务",
		CreatedAt: "2026-04-15T10:00:00Z",
	})
	app.store.UpsertIncidentEvent(sessionID, model.IncidentEvent{
		ID:        "evt-timeline-2",
		Type:      "cancel.signal_failed",
		Status:    "warning",
		Stage:     "canceled",
		Summary:   "远端未确认取消信号",
		CreatedAt: "2026-04-15T10:00:01Z",
	})

	mux := http.NewServeMux()
	app.registerEvidenceRoutes(mux)

	req := sessionDetailRequest(app, browserID, "/api/v1/sessions/"+sessionID+"/timeline?status=warning")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected timeline route 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode timeline response: %v", err)
	}
	items, ok := payload["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected one filtered timeline item, got %#v", payload)
	}
	item, _ := items[0].(map[string]any)
	if got := item["type"]; got != "cancel.signal_failed" {
		t.Fatalf("expected warning timeline event, got %#v", item)
	}
}

func TestToolEventTimelineRouteReturnsOwnedSessionEvents(t *testing.T) {
	app := newOrchestratorTestApp(t)
	browserID := "browser-tool-event-route"
	session := app.store.CreateSessionWithMeta(browserID, model.SessionMeta{
		Kind:    model.SessionKindWorkspace,
		Visible: true,
	}, true)
	sessionID := session.ID

	app.toolEventStore.Append(store.ToolEventRecord{
		SessionID: sessionID,
		EventID:   "evt-tool-1",
		Type:      string(ToolLifecycleEventStarted),
		ToolName:  "read_file",
	})
	app.toolEventStore.Append(store.ToolEventRecord{
		SessionID: sessionID,
		EventID:   "evt-tool-2",
		Type:      string(ToolLifecycleEventChoiceResolved),
		ToolName:  "ask_user_question",
	})
	app.toolEventStore.Append(store.ToolEventRecord{
		SessionID: "other-session",
		EventID:   "evt-tool-3",
		Type:      string(ToolLifecycleEventCompleted),
		ToolName:  "execute_command",
	})

	mux := http.NewServeMux()
	app.registerEvidenceRoutes(mux)

	req := sessionDetailRequest(app, browserID, "/api/v1/sessions/"+sessionID+"/tool-events?type=choice_resolved&limit=1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected tool-events route 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode tool event response: %v", err)
	}
	items, ok := payload["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected one filtered tool event, got %#v", payload)
	}
	item, _ := items[0].(map[string]any)
	if got := item["eventId"]; got != "evt-tool-2" {
		t.Fatalf("expected choice_resolved tool event, got %#v", item)
	}
	if got := item["toolName"]; got != "ask_user_question" {
		t.Fatalf("expected ask_user_question tool name, got %#v", item)
	}
}

func TestSessionDetailRoutesRejectForeignBrowserSession(t *testing.T) {
	app := newOrchestratorTestApp(t)
	session := app.store.CreateSessionWithMeta("browser-owner", model.SessionMeta{
		Kind:    model.SessionKindWorkspace,
		Visible: true,
	}, true)
	mux := http.NewServeMux()
	app.registerEvidenceRoutes(mux)

	req := sessionDetailRequest(app, "browser-other", "/api/v1/sessions/"+session.ID+"/timeline")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected foreign browser to get 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPromptDebugRouteReturnsTurnPolicyAndPromptEnvelope(t *testing.T) {
	app := newOrchestratorTestApp(t)
	browserID := "browser-prompt-debug"
	session := app.store.CreateSessionWithMeta(browserID, model.SessionMeta{
		Kind:    model.SessionKindWorkspace,
		Visible: true,
	}, true)
	sessionID := session.ID
	app.store.UpdateRuntime(sessionID, func(rt *model.RuntimeState) {
		rt.TurnPolicy = model.TurnPolicy{
			IntentClass:         string(model.TurnIntentDesign),
			Lane:                string(model.TurnLanePlan),
			RequiredNextTool:    "update_plan",
			FinalGateStatus:     "blocked",
			MissingRequirements: []string{"缺少计划产物"},
		}
		rt.PromptEnvelope = &model.PromptEnvelope{
			IntentClass:     string(model.TurnIntentDesign),
			CurrentLane:     string(model.TurnLanePlan),
			FinalGateStatus: "blocked",
			StaticSections: []model.PromptEnvelopeSection{{
				Name:    "System",
				Content: "system prompt",
			}},
		}
	})

	mux := http.NewServeMux()
	app.registerEvidenceRoutes(mux)
	req := sessionDetailRequest(app, browserID, "/api/v1/sessions/"+sessionID+"/prompt-debug")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected prompt debug route 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode prompt debug response: %v", err)
	}
	if got := payload["currentLane"]; got != "plan" {
		t.Fatalf("expected plan lane, got %#v", payload)
	}
	policy, _ := payload["turnPolicy"].(map[string]any)
	if policy["requiredNextTool"] != "update_plan" {
		t.Fatalf("expected required next tool in turn policy, got %#v", payload)
	}
	envelope, _ := payload["promptEnvelope"].(map[string]any)
	if envelope["finalGateStatus"] != "blocked" {
		t.Fatalf("expected prompt envelope in payload, got %#v", payload)
	}
}
