package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/store"
)

type approvalAuditListResponse struct {
	Items []any                       `json:"items"`
	Stats store.ApprovalAuditStats    `json:"stats"`
	Total int                         `json:"total"`
	Limit int                         `json:"limit"`
}

func (a *App) handleApprovalAudits(w http.ResponseWriter, r *http.Request, _ string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if a.approvalAuditStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "approval audit store is not initialized"})
		return
	}

	q := r.URL.Query()
	filter := store.ApprovalAuditFilter{
		TimeFrom:    strings.TrimSpace(q.Get("timeFrom")),
		TimeTo:      strings.TrimSpace(q.Get("timeTo")),
		SessionKind: strings.TrimSpace(q.Get("sessionKind")),
		HostID:      strings.TrimSpace(q.Get("hostId")),
		Operator:    strings.TrimSpace(q.Get("operator")),
		Decision:    strings.TrimSpace(q.Get("decision")),
		ToolName:    strings.TrimSpace(q.Get("toolName")),
	}

	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			filter.Limit = parsed
		}
	}
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}

	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			filter.Offset = parsed
		}
	}

	records := a.approvalAuditStore.List(filter)

	// Convert to []any so JSON always emits [] instead of null.
	items := make([]any, len(records))
	for i, rec := range records {
		items[i] = rec
	}

	writeJSON(w, http.StatusOK, approvalAuditListResponse{
		Items: items,
		Stats: a.approvalAuditStore.Stats(),
		Total: len(items),
		Limit: filter.Limit,
	})
}

func (a *App) handleApprovalAuditByID(w http.ResponseWriter, r *http.Request, _ string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if a.approvalAuditStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "approval audit store is not initialized"})
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/v1/approval-audits/")
	id = strings.Trim(id, "/")
	if id == "" || strings.Contains(id, "/") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "approval audit not found"})
		return
	}

	record, ok := a.approvalAuditStore.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "approval audit not found"})
		return
	}

	writeJSON(w, http.StatusOK, record)
}
