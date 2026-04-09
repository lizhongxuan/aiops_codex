package server

import (
	"net/http"
	"strings"
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

	item := a.store.Item(sessionID, evidenceID)
	if item == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "evidence not found"})
		return
	}

	writeJSON(w, http.StatusOK, item)
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

// registerEvidenceRoutes registers the evidence and invocation API routes.
// Call this from the main route setup.
func (a *App) registerEvidenceRoutes(mux *http.ServeMux) {
	// These are registered as sub-paths under the session handler.
	// The actual registration happens in the main server setup.
}
