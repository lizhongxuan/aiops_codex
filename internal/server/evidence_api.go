package server

import (
	"net/http"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// handleEvidenceDetail returns a single evidence record by ID.
// GET /api/sessions/{sessionID}/evidence/{evidenceID}
func (a *App) handleEvidenceDetail(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	evidenceID := r.PathValue("evidenceID")
	if evidenceID == "" {
		// Fallback: extract from URL path
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) > 0 {
			evidenceID = parts[len(parts)-1]
		}
	}

	if evidenceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "evidence ID required"})
		return
	}

	if item := a.store.Item(sessionID, evidenceID); item != nil {
		writeJSON(w, http.StatusOK, item)
		return
	}
	if detail := a.buildEvidenceDetailPayload(sessionID, evidenceID); detail != nil {
		writeJSON(w, http.StatusOK, detail)
		return
	}

	writeJSON(w, http.StatusNotFound, map[string]string{"error": "evidence not found"})
}

// handleInvocationDetail returns a single tool invocation by ID.
// GET /api/sessions/{sessionID}/invocations/{invocationID}
func (a *App) handleInvocationDetail(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	invocationID := r.PathValue("invocationID")
	if invocationID == "" {
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) > 0 {
			invocationID = parts[len(parts)-1]
		}
	}

	if invocationID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invocation ID required"})
		return
	}

	snapshot := a.snapshot(sessionID)
	for _, inv := range snapshot.ToolInvocations {
		if inv.ID == invocationID {
			writeJSON(w, http.StatusOK, inv)
			return
		}
	}

	writeJSON(w, http.StatusNotFound, map[string]string{"error": "invocation not found"})
}

// handleIncidentTimeline returns the incident timeline for a session.
// GET /api/v1/sessions/{sessionID}/timeline
func (a *App) handleIncidentTimeline(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	snapshot := a.snapshot(sessionID)
	items := make([]model.IncidentEvent, 0, len(snapshot.IncidentEvents))
	typeFilter := strings.TrimSpace(r.URL.Query().Get("type"))
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	stageFilter := strings.TrimSpace(r.URL.Query().Get("stage"))
	for _, event := range snapshot.IncidentEvents {
		if typeFilter != "" && strings.TrimSpace(event.Type) != typeFilter {
			continue
		}
		if statusFilter != "" && strings.TrimSpace(event.Status) != statusFilter {
			continue
		}
		if stageFilter != "" && strings.TrimSpace(event.Stage) != stageFilter {
			continue
		}
		items = append(items, event)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"sessionId":    sessionID,
		"currentMode":  snapshot.CurrentMode,
		"currentStage": snapshot.CurrentStage,
		"items":        items,
	})
}

// handleVerificationDetail returns a single verification record by ID.
// GET /api/v1/sessions/{sessionID}/verification/{verificationID}
func (a *App) handleVerificationDetail(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	verificationID := r.PathValue("verificationID")
	if verificationID == "" {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) > 0 {
			verificationID = parts[len(parts)-1]
		}
	}
	if verificationID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "verification ID required"})
		return
	}

	snapshot := a.snapshot(sessionID)
	for _, record := range snapshot.VerificationRecords {
		if strings.TrimSpace(record.ID) == verificationID {
			writeJSON(w, http.StatusOK, record)
			return
		}
	}

	writeJSON(w, http.StatusNotFound, map[string]string{"error": "verification not found"})
}

// handlePromptDebug returns the current prompt envelope / turn policy debug payload.
// GET /api/v1/sessions/{sessionID}/prompt-debug
func (a *App) handlePromptDebug(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	snapshot := a.snapshot(sessionID)
	writeJSON(w, http.StatusOK, map[string]any{
		"sessionId":           sessionID,
		"currentMode":         snapshot.CurrentMode,
		"currentStage":        snapshot.CurrentStage,
		"currentLane":         snapshot.CurrentLane,
		"requiredNextTool":    snapshot.RequiredNextTool,
		"finalGateStatus":     snapshot.FinalGateStatus,
		"missingRequirements": snapshot.MissingRequirements,
		"turnPolicy":          snapshot.TurnPolicy,
		"promptEnvelope":      snapshot.PromptEnvelope,
	})
}

func sessionIDFromDetailPath(path string) string {
	trimmed := strings.TrimSpace(path)
	for _, prefix := range []string{"/api/v1/sessions/", "/api/sessions/"} {
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		rest := strings.Trim(strings.TrimPrefix(trimmed, prefix), "/")
		if rest == "" {
			return ""
		}
		parts := strings.Split(rest, "/")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	return ""
}

func (a *App) withOwnedSessionFromPath(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return a.withBrowserSession(func(w http.ResponseWriter, r *http.Request, browserID string) {
		sessionID := strings.TrimSpace(r.PathValue("sessionID"))
		if sessionID == "" {
			sessionID = sessionIDFromDetailPath(r.URL.Path)
		}
		if sessionID == "" || !a.store.BrowserOwnsSession(browserID, sessionID) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		a.store.TouchSession(sessionID)
		next(w, r, sessionID)
	})
}

// registerEvidenceRoutes registers the evidence and invocation API routes.
// Call this from the main route setup.
func (a *App) registerEvidenceRoutes(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	handlerEvidence := a.withOwnedSessionFromPath(a.handleEvidenceDetail)
	handlerInvocation := a.withOwnedSessionFromPath(a.handleInvocationDetail)
	handlerTimeline := a.withOwnedSessionFromPath(a.handleIncidentTimeline)
	handlerVerification := a.withOwnedSessionFromPath(a.handleVerificationDetail)
	handlerPromptDebug := a.withOwnedSessionFromPath(a.handlePromptDebug)

	for _, pattern := range []string{
		"GET /api/v1/sessions/{sessionID}/evidence/{evidenceID}",
		"GET /api/sessions/{sessionID}/evidence/{evidenceID}",
	} {
		mux.HandleFunc(pattern, handlerEvidence)
	}
	for _, pattern := range []string{
		"GET /api/v1/sessions/{sessionID}/invocations/{invocationID}",
		"GET /api/sessions/{sessionID}/invocations/{invocationID}",
	} {
		mux.HandleFunc(pattern, handlerInvocation)
	}
	for _, pattern := range []string{
		"GET /api/v1/sessions/{sessionID}/timeline",
		"GET /api/sessions/{sessionID}/timeline",
	} {
		mux.HandleFunc(pattern, handlerTimeline)
	}
	for _, pattern := range []string{
		"GET /api/v1/sessions/{sessionID}/verification/{verificationID}",
		"GET /api/sessions/{sessionID}/verification/{verificationID}",
	} {
		mux.HandleFunc(pattern, handlerVerification)
	}
	for _, pattern := range []string{
		"GET /api/v1/sessions/{sessionID}/prompt-debug",
		"GET /api/sessions/{sessionID}/prompt-debug",
	} {
		mux.HandleFunc(pattern, handlerPromptDebug)
	}
}
