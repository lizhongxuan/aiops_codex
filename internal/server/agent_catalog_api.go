package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func (a *App) skillCatalog() []model.AgentSkill {
	items := a.store.SkillCatalog()
	if len(items) == 0 {
		return model.SupportedAgentSkills()
	}
	return items
}

func (a *App) mcpCatalog() []model.AgentMCP {
	items := a.store.MCPCatalog()
	if len(items) == 0 {
		return model.SupportedAgentMCPs()
	}
	return items
}

func (a *App) validateAgentProfile(profile model.AgentProfile) error {
	return validateAgentProfileAgainstCatalogs(profile, a.skillCatalog(), a.mcpCatalog())
}

func validateAgentProfileAgainstCatalogs(profile model.AgentProfile, skills []model.AgentSkill, mcps []model.AgentMCP) error {
	fieldErrors := make(map[string]string)
	addFieldError := func(field, message string) {
		if strings.TrimSpace(field) == "" || strings.TrimSpace(message) == "" {
			return
		}
		if _, exists := fieldErrors[field]; exists {
			return
		}
		fieldErrors[field] = message
	}
	profileID := strings.TrimSpace(profile.ID)
	switch profileID {
	case string(model.AgentProfileTypeMainAgent), string(model.AgentProfileTypeHostAgentDefault):
	default:
		addFieldError("id", fmt.Sprintf("unsupported profile id %q", profileID))
	}
	switch strings.TrimSpace(profile.Type) {
	case string(model.AgentProfileTypeMainAgent), string(model.AgentProfileTypeHostAgentDefault):
	default:
		addFieldError("type", fmt.Sprintf("unsupported profile type %q", profile.Type))
	}
	if strings.TrimSpace(profile.Name) == "" {
		addFieldError("name", "profile name is required")
	}
	if prompt := strings.TrimSpace(profile.SystemPrompt.Content); prompt == "" {
		addFieldError("systemPrompt.content", "system prompt is required")
	} else if len([]rune(prompt)) > 12000 {
		addFieldError("systemPrompt.content", "system prompt is too long")
	}
	if timeout := profile.CommandPermissions.DefaultTimeoutSeconds; timeout <= 0 || timeout > 3600 {
		addFieldError("commandPermissions.defaultTimeoutSeconds", "default timeout must be between 1 and 3600 seconds")
	}
	validCommandModes := map[string]struct{}{
		model.AgentPermissionModeAllow:            {},
		model.AgentPermissionModeApprovalRequired: {},
		model.AgentPermissionModeReadonlyOnly:     {},
		model.AgentPermissionModeDeny:             {},
	}
	validCommandCategories := map[string]struct{}{
		"system_inspection":   {},
		"service_read":        {},
		"network_read":        {},
		"file_read":           {},
		"service_mutation":    {},
		"filesystem_mutation": {},
		"package_mutation":    {},
	}
	if _, ok := validCommandModes[profile.CommandPermissions.DefaultMode]; !ok {
		addFieldError("commandPermissions.defaultMode", fmt.Sprintf("unsupported command default mode %q", profile.CommandPermissions.DefaultMode))
	}
	for category, mode := range profile.CommandPermissions.CategoryPolicies {
		if _, ok := validCommandCategories[category]; !ok {
			addFieldError("commandPermissions.categoryPolicies."+category, fmt.Sprintf("unsupported command category %q", category))
			continue
		}
		if _, ok := validCommandModes[mode]; !ok {
			addFieldError("commandPermissions.categoryPolicies."+category, fmt.Sprintf("unsupported command mode %q for %s", mode, category))
		}
	}
	for index, root := range profile.CommandPermissions.AllowedWritableRoots {
		if strings.TrimSpace(root) == "" {
			addFieldError(fmt.Sprintf("commandPermissions.allowedWritableRoots.%d", index), "writable roots must not contain empty paths")
		}
	}
	validCapabilityStates := map[string]struct{}{
		model.AgentCapabilityEnabled:          {},
		model.AgentCapabilityApprovalRequired: {},
		model.AgentCapabilityDisabled:         {},
	}
	capabilityFields := map[string]string{
		"capabilityPermissions.commandExecution": profile.CapabilityPermissions.CommandExecution,
		"capabilityPermissions.fileRead":         profile.CapabilityPermissions.FileRead,
		"capabilityPermissions.fileSearch":       profile.CapabilityPermissions.FileSearch,
		"capabilityPermissions.fileChange":       profile.CapabilityPermissions.FileChange,
		"capabilityPermissions.terminal":         profile.CapabilityPermissions.Terminal,
		"capabilityPermissions.webSearch":        profile.CapabilityPermissions.WebSearch,
		"capabilityPermissions.webOpen":          profile.CapabilityPermissions.WebOpen,
		"capabilityPermissions.approval":         profile.CapabilityPermissions.Approval,
		"capabilityPermissions.multiAgent":       profile.CapabilityPermissions.MultiAgent,
		"capabilityPermissions.plan":             profile.CapabilityPermissions.Plan,
		"capabilityPermissions.summary":          profile.CapabilityPermissions.Summary,
	}
	for field, state := range capabilityFields {
		if _, ok := validCapabilityStates[state]; !ok {
			addFieldError(field, fmt.Sprintf("unsupported capability state %q", state))
		}
	}
	supportedSkillIDs := make(map[string]struct{}, len(skills))
	for _, item := range skills {
		supportedSkillIDs[item.ID] = struct{}{}
	}
	validActivationModes := map[string]struct{}{
		model.AgentSkillActivationDefault:  {},
		model.AgentSkillActivationExplicit: {},
		model.AgentSkillActivationDisabled: {},
	}
	for index, skill := range profile.Skills {
		if _, ok := supportedSkillIDs[strings.TrimSpace(skill.ID)]; !ok {
			addFieldError(fmt.Sprintf("skills.%d.id", index), fmt.Sprintf("unsupported skill id %q", skill.ID))
		}
		if _, ok := validActivationModes[model.NormalizeAgentSkillActivationMode(skill.ActivationMode)]; !ok {
			addFieldError(fmt.Sprintf("skills.%d.activationMode", index), fmt.Sprintf("unsupported activation mode %q", skill.ActivationMode))
		}
	}
	supportedMCPIDs := make(map[string]struct{}, len(mcps))
	for _, item := range mcps {
		supportedMCPIDs[item.ID] = struct{}{}
	}
	validMCPPermissions := map[string]struct{}{
		model.AgentMCPPermissionReadonly:  {},
		model.AgentMCPPermissionReadwrite: {},
	}
	for index, item := range profile.MCPs {
		if _, ok := supportedMCPIDs[strings.TrimSpace(item.ID)]; !ok {
			addFieldError(fmt.Sprintf("mcps.%d.id", index), fmt.Sprintf("unsupported MCP id %q", item.ID))
		}
		if _, ok := validMCPPermissions[model.NormalizeAgentMCPPermission(item.Permission)]; !ok {
			addFieldError(fmt.Sprintf("mcps.%d.permission", index), fmt.Sprintf("unsupported MCP permission %q", item.Permission))
		}
	}
	if len(fieldErrors) > 0 {
		return newAgentProfileValidationError(fieldErrors)
	}
	return nil
}

func validateSkillCatalogItem(item model.AgentSkill) (model.AgentSkill, error) {
	fieldErrors := make(map[string]string)
	item.ID = strings.TrimSpace(item.ID)
	item.Name = strings.TrimSpace(item.Name)
	item.Description = strings.TrimSpace(item.Description)
	item.Source = strings.TrimSpace(item.Source)
	if item.Source == "" {
		item.Source = "local"
	}
	if item.Enabled {
		item.DefaultEnabled = true
	}
	mode := firstNonEmpty(item.DefaultActivationMode, item.ActivationMode)
	if strings.TrimSpace(mode) == "" {
		if item.DefaultEnabled {
			mode = model.AgentSkillActivationDefault
		} else {
			mode = model.AgentSkillActivationExplicit
		}
	}
	item.DefaultActivationMode = model.NormalizeAgentSkillActivationMode(mode)
	item.Enabled = item.DefaultEnabled
	item.ActivationMode = item.DefaultActivationMode
	if item.ID == "" {
		fieldErrors["id"] = "skill id is required"
	}
	if item.Name == "" {
		fieldErrors["name"] = "skill name is required"
	}
	switch item.DefaultActivationMode {
	case model.AgentSkillActivationDefault, model.AgentSkillActivationExplicit, model.AgentSkillActivationDisabled:
	default:
		fieldErrors["defaultActivationMode"] = fmt.Sprintf("unsupported activation mode %q", item.DefaultActivationMode)
	}
	if len(fieldErrors) > 0 {
		return model.AgentSkill{}, newAgentProfileValidationError(fieldErrors)
	}
	return item, nil
}

func validateMCPCatalogItem(item model.AgentMCP) (model.AgentMCP, error) {
	fieldErrors := make(map[string]string)
	item.ID = strings.TrimSpace(item.ID)
	item.Name = strings.TrimSpace(item.Name)
	item.Type = strings.TrimSpace(item.Type)
	item.Source = strings.TrimSpace(item.Source)
	if item.Type == "" {
		item.Type = "stdio"
	}
	if item.Source == "" {
		item.Source = "local"
	}
	if item.Enabled {
		item.DefaultEnabled = true
	}
	item.Enabled = item.DefaultEnabled
	item.Permission = model.NormalizeAgentMCPPermission(item.Permission)
	if item.ID == "" {
		fieldErrors["id"] = "mcp id is required"
	}
	if item.Name == "" {
		fieldErrors["name"] = "mcp name is required"
	}
	switch item.Permission {
	case model.AgentMCPPermissionReadonly, model.AgentMCPPermissionReadwrite:
	default:
		fieldErrors["permission"] = fmt.Sprintf("unsupported permission %q", item.Permission)
	}
	if len(fieldErrors) > 0 {
		return model.AgentMCP{}, newAgentProfileValidationError(fieldErrors)
	}
	return item, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (a *App) handleAgentSkills(w http.ResponseWriter, r *http.Request, sessionID string) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"items": a.skillCatalog(),
		})
	case http.MethodPost:
		var item model.AgentSkill
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		normalized, err := validateSkillCatalogItem(item)
		if err != nil {
			a.writeAgentProfileError(w, http.StatusBadRequest, err)
			return
		}
		a.store.UpsertSkillCatalogItem(normalized)
		a.audit("agent_skill_catalog.upserted", map[string]any{
			"sessionId": sessionID,
			"operator":  a.auditOperator(sessionID),
			"skillId":   normalized.ID,
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"item":  normalized,
			"items": a.skillCatalog(),
		})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (a *App) handleAgentSkillByID(w http.ResponseWriter, r *http.Request, sessionID string) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/agent-skills/"), "/")
	if path == "" || strings.Contains(path, "/") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent skill not found"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		for _, item := range a.skillCatalog() {
			if item.ID == path {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent skill not found"})
	case http.MethodPut:
		var item model.AgentSkill
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		item.ID = path
		normalized, err := validateSkillCatalogItem(item)
		if err != nil {
			a.writeAgentProfileError(w, http.StatusBadRequest, err)
			return
		}
		a.store.UpsertSkillCatalogItem(normalized)
		a.audit("agent_skill_catalog.updated", map[string]any{
			"sessionId": sessionID,
			"operator":  a.auditOperator(sessionID),
			"skillId":   normalized.ID,
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"item":  normalized,
			"items": a.skillCatalog(),
		})
	case http.MethodDelete:
		a.store.DeleteSkillCatalogItem(path)
		a.audit("agent_skill_catalog.deleted", map[string]any{
			"sessionId": sessionID,
			"operator":  a.auditOperator(sessionID),
			"skillId":   path,
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":    true,
			"items": a.skillCatalog(),
		})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (a *App) handleAgentMCPs(w http.ResponseWriter, r *http.Request, sessionID string) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"items": a.mcpCatalog(),
		})
	case http.MethodPost:
		var item model.AgentMCP
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		normalized, err := validateMCPCatalogItem(item)
		if err != nil {
			a.writeAgentProfileError(w, http.StatusBadRequest, err)
			return
		}
		a.store.UpsertMCPCatalogItem(normalized)
		a.audit("agent_mcp_catalog.upserted", map[string]any{
			"sessionId": sessionID,
			"operator":  a.auditOperator(sessionID),
			"mcpId":     normalized.ID,
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"item":  normalized,
			"items": a.mcpCatalog(),
		})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (a *App) handleAgentMCPByID(w http.ResponseWriter, r *http.Request, sessionID string) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/agent-mcps/"), "/")
	if path == "" || strings.Contains(path, "/") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent mcp not found"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		for _, item := range a.mcpCatalog() {
			if item.ID == path {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent mcp not found"})
	case http.MethodPut:
		var item model.AgentMCP
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		item.ID = path
		normalized, err := validateMCPCatalogItem(item)
		if err != nil {
			a.writeAgentProfileError(w, http.StatusBadRequest, err)
			return
		}
		a.store.UpsertMCPCatalogItem(normalized)
		a.audit("agent_mcp_catalog.updated", map[string]any{
			"sessionId": sessionID,
			"operator":  a.auditOperator(sessionID),
			"mcpId":     normalized.ID,
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"item":  normalized,
			"items": a.mcpCatalog(),
		})
	case http.MethodDelete:
		a.store.DeleteMCPCatalogItem(path)
		a.audit("agent_mcp_catalog.deleted", map[string]any{
			"sessionId": sessionID,
			"operator":  a.auditOperator(sessionID),
			"mcpId":     path,
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":    true,
			"items": a.mcpCatalog(),
		})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}
