package server

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type agentProfilesExportResponse struct {
	Version       int                  `json:"version"`
	ConfigVersion int                  `json:"configVersion"`
	ExportedAt    string               `json:"exportedAt"`
	ExportedBy    string               `json:"exportedBy,omitempty"`
	Profiles      []model.AgentProfile `json:"profiles"`
}

type agentProfilesImportRequest struct {
	Replace  bool                 `json:"replace"`
	Profiles []model.AgentProfile `json:"profiles"`
	Items    []model.AgentProfile `json:"items"`
}

func (a *App) handleAgentProfilesExport(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, agentProfilesExportResponse{
		Version:       model.AgentProfileConfigVersion,
		ConfigVersion: model.AgentProfileConfigVersion,
		ExportedAt:    model.NowString(),
		ExportedBy:    a.auditOperator(sessionID),
		Profiles:      a.store.AgentProfiles(),
	})
}

func (a *App) handleAgentProfilesImport(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req agentProfilesImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	items := req.Profiles
	if len(items) == 0 {
		items = req.Items
	}
	if len(items) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "import payload must include profiles"})
		return
	}

	beforeProfiles := a.store.AgentProfiles()
	afterByID := make(map[string]model.AgentProfile, len(beforeProfiles))
	for _, profile := range beforeProfiles {
		afterByID[profile.ID] = profile
	}
	if req.Replace {
		afterByID = make(map[string]model.AgentProfile, len(model.DefaultAgentProfileIDs()))
		for _, profileID := range model.DefaultAgentProfileIDs() {
			afterByID[profileID] = model.DefaultAgentProfile(profileID)
		}
	}

	for _, incoming := range items {
		profileID := strings.TrimSpace(incoming.ID)
		if profileID == "" {
			profileID = strings.TrimSpace(incoming.Type)
		}
		if profileID == "" {
			a.writeAgentProfileError(w, http.StatusBadRequest, newAgentProfileValidationError(map[string]string{
				"id": "profile id is required for import",
			}))
			return
		}
		incoming.ID = profileID
		incoming = model.CompleteAgentProfile(incoming)
		if err := validateAgentProfile(incoming); err != nil {
			a.writeAgentProfileError(w, http.StatusBadRequest, err)
			return
		}
		incoming.UpdatedAt = model.NowString()
		incoming.UpdatedBy = a.auditOperator(sessionID)
		afterByID[profileID] = incoming
	}

	afterProfiles := make([]model.AgentProfile, 0, len(afterByID))
	for _, profile := range afterByID {
		afterProfiles = append(afterProfiles, model.CompleteAgentProfile(profile))
	}
	model.SortAgentProfiles(afterProfiles)
	if req.Replace {
		a.store.ResetAgentProfiles()
	}
	for _, profile := range afterProfiles {
		a.store.UpsertAgentProfile(profile)
	}

	importedHostProfile := false
	for _, profile := range afterProfiles {
		if profile.ID == string(model.AgentProfileTypeHostAgentDefault) {
			importedHostProfile = true
			break
		}
	}
	if importedHostProfile {
		a.pushHostAgentProfileToConnectedAgents()
	}

	a.audit("agent_profile.imported", map[string]any{
		"sessionId":     sessionID,
		"operator":      a.auditOperator(sessionID),
		"replace":       req.Replace,
		"before":        a.agentProfilesAuditSummary(beforeProfiles),
		"after":         a.agentProfilesAuditSummary(afterProfiles),
		"configVersion": model.AgentProfileConfigVersion,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"version":       model.AgentProfileConfigVersion,
		"items":         afterProfiles,
		"profiles":      afterProfiles,
		"configVersion": model.AgentProfileConfigVersion,
	})
}

func (a *App) agentProfilesAuditSummary(items []model.AgentProfile) []map[string]any {
	summary := make([]map[string]any, 0, len(items))
	slices.SortFunc(items, func(left, right model.AgentProfile) int {
		return strings.Compare(left.ID, right.ID)
	})
	for _, item := range items {
		summary = append(summary, a.agentProfileAuditSummary(item))
	}
	return summary
}
