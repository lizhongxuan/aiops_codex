package server

import (
	"net/http"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/coroot"
)

// handleCorootHostOverview handles GET /api/v1/coroot/hosts/{hostID}/overview
// and returns the host overview data from Coroot via the DataSourceRouter.
func (a *App) handleCorootHostOverview(w http.ResponseWriter, r *http.Request, _ string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	if a.dataSourceRouter == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "coroot data source not configured"})
		return
	}

	// Extract hostID from the path: /api/v1/coroot/hosts/{hostID}/overview
	const prefix = "/api/v1/coroot/hosts/"
	const suffix = "/overview"
	path := strings.TrimPrefix(r.URL.Path, prefix)
	hostID := strings.TrimSuffix(path, suffix)
	hostID = strings.TrimSpace(hostID)
	if hostID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hostID is required"})
		return
	}

	result, err := a.dataSourceRouter.Route(r.Context(), coroot.DataQuery{
		Kind:   coroot.QueryHostOverview,
		HostID: hostID,
	})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error":      err.Error(),
			"dataSource": string(result.DataSource),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":       result.Data,
		"dataSource": string(result.DataSource),
		"latency":    result.Latency.String(),
	})
}
