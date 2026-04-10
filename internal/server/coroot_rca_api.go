package server

import (
	"net/http"
	"strings"
)

// handleCorootRCA handles GET /api/v1/coroot/rca/{incidentID} and returns
// the root-cause analysis result for the given incident.
func (a *App) handleCorootRCA(w http.ResponseWriter, r *http.Request, _ string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	if a.rcaEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "rca engine not available"})
		return
	}

	// Extract incidentID from the path: /api/v1/coroot/rca/{incidentID}
	const prefix = "/api/v1/coroot/rca/"
	incidentID := strings.TrimPrefix(r.URL.Path, prefix)
	incidentID = strings.TrimSpace(incidentID)
	if incidentID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "incidentID is required"})
		return
	}

	result, err := a.rcaEngine.Analyze(r.Context(), incidentID)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}
