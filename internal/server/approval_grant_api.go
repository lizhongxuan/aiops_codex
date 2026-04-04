package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// handleApprovalGrants handles GET (list by hostId) and POST (create) on
// /api/v1/approval-grants.
func (a *App) handleApprovalGrants(w http.ResponseWriter, r *http.Request, _ string) {
	if a.approvalGrantStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "approval grant store is not initialized"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		hostID := strings.TrimSpace(r.URL.Query().Get("hostId"))
		if hostID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hostId query parameter is required"})
			return
		}
		records := a.approvalGrantStore.ListByHost(hostID)
		items := make([]any, len(records))
		for i, rec := range records {
			items[i] = rec
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})

	case http.MethodPost:
		var rec model.ApprovalGrantRecord
		if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if rec.ID == "" {
			rec.ID = model.NewID("agrant")
		}
		if err := a.approvalGrantStore.Add(rec); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, rec)

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleApprovalGrantAction handles single-grant operations under
// /api/v1/approval-grants/{id}[/{action}].
func (a *App) handleApprovalGrantAction(w http.ResponseWriter, r *http.Request, _ string) {
	if a.approvalGrantStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "approval grant store is not initialized"})
		return
	}

	// Strip the prefix and split: expect "id" or "id/action".
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/approval-grants/")
	path = strings.Trim(path, "/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]
	if id == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "approval grant not found"})
		return
	}

	// GET /api/v1/approval-grants/{id} — return single record.
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		rec, ok := a.approvalGrantStore.Get(id)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "approval grant not found"})
			return
		}
		writeJSON(w, http.StatusOK, rec)
		return
	}

	// POST /api/v1/approval-grants/{id}/{action}
	action := parts[1]
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var err error
	switch action {
	case "revoke":
		err = a.approvalGrantStore.Revoke(id)
	case "disable":
		err = a.approvalGrantStore.Disable(id)
	case "enable":
		err = a.approvalGrantStore.Enable(id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown action"})
		return
	}

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	rec, _ := a.approvalGrantStore.Get(id)
	writeJSON(w, http.StatusOK, rec)
}
