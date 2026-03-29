package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/store"
)

type codexSkillsListResponse struct {
	Data []codexSkillsListEntry `json:"data"`
}

type codexSkillsListEntry struct {
	Cwd    string               `json:"cwd"`
	Skills []codexSkillMetadata `json:"skills"`
}

type codexSkillMetadata struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
}

func normalizeSkillLookupKey(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}

func skillEnabledByProfile(item model.AgentSkill) bool {
	if !item.Enabled {
		return false
	}
	return model.NormalizeAgentSkillActivationMode(item.ActivationMode) != model.AgentSkillActivationDisabled
}

func explicitSkillRequested(message string, item model.AgentSkill) bool {
	message = normalizeSkillLookupKey(message)
	if message == "" {
		return false
	}
	for _, candidate := range []string{item.ID, item.Name} {
		candidate = normalizeSkillLookupKey(candidate)
		if candidate == "" {
			continue
		}
		if strings.Contains(message, candidate) {
			return true
		}
	}
	return false
}

func (a *App) listCodexSkills(ctx context.Context) ([]codexSkillMetadata, error) {
	var result codexSkillsListResponse
	if err := a.codexRequest(ctx, "skills/list", map[string]any{
		"cwds":        []string{a.cfg.DefaultWorkspace},
		"forceReload": false,
	}, &result); err != nil {
		return nil, err
	}
	items := make([]codexSkillMetadata, 0)
	for _, entry := range result.Data {
		items = append(items, entry.Skills...)
	}
	return items, nil
}

func buildManagedSkillPathMap(profile model.AgentProfile, discovered []codexSkillMetadata) map[string]string {
	managed := make(map[string]string)
	allowed := make(map[string]struct{})
	for _, item := range profile.Skills {
		if id := normalizeSkillLookupKey(item.ID); id != "" {
			allowed[id] = struct{}{}
		}
		if name := normalizeSkillLookupKey(item.Name); name != "" {
			allowed[name] = struct{}{}
		}
	}
	for _, item := range discovered {
		path := strings.TrimSpace(item.Path)
		if path == "" {
			continue
		}
		dirName := filepath.Base(filepath.Dir(path))
		for _, candidate := range []string{item.Name, dirName} {
			key := normalizeSkillLookupKey(candidate)
			if key == "" {
				continue
			}
			if _, ok := allowed[key]; ok {
				managed[key] = path
			}
		}
	}
	return managed
}

func (a *App) buildMainAgentThreadConfig(ctx context.Context, profile model.AgentProfile, hostID string) map[string]any {
	config := map[string]any{
		"apps._default.enabled":             false,
		"apps._default.destructive_enabled": false,
	}

	if discovered, err := a.listCodexSkills(ctx); err != nil {
		log.Printf("main-agent skills/list skipped while building thread config: %v", err)
	} else {
		enabledKeys := make(map[string]struct{})
		for _, item := range profile.Skills {
			if !skillEnabledByProfile(item) {
				continue
			}
			for _, candidate := range []string{item.ID, item.Name} {
				key := normalizeSkillLookupKey(candidate)
				if key != "" {
					enabledKeys[key] = struct{}{}
				}
			}
		}
		entries := make([]map[string]any, 0, len(discovered))
		for _, item := range discovered {
			path := strings.TrimSpace(item.Path)
			if path == "" {
				continue
			}
			_, enabled := enabledKeys[normalizeSkillLookupKey(item.Name)]
			if !enabled {
				dirName := normalizeSkillLookupKey(filepath.Base(filepath.Dir(path)))
				_, enabled = enabledKeys[dirName]
			}
			entries = append(entries, map[string]any{
				"path":    path,
				"enabled": enabled,
			})
		}
		if len(entries) > 0 {
			config["skills.config"] = entries
		}
	}

	for _, item := range a.effectiveEnabledAgentMCPs(profile, hostID) {
		appID := strings.TrimSpace(item.ID)
		if appID == "" {
			continue
		}
		config[fmt.Sprintf("apps.%s.enabled", appID)] = true
		config[fmt.Sprintf("apps.%s.default_tools_enabled", appID)] = true
		config[fmt.Sprintf("apps.%s.destructive_enabled", appID)] = model.NormalizeAgentMCPPermission(item.Permission) == model.AgentMCPPermissionReadwrite
		if item.RequiresExplicitUserApproval {
			config[fmt.Sprintf("apps.%s.default_tools_approval_mode", appID)] = "prompt"
			continue
		}
		config[fmt.Sprintf("apps.%s.default_tools_approval_mode", appID)] = "auto"
	}

	return config
}

func (a *App) mainAgentThreadConfigHash(hostID string) string {
	type skillState struct {
		ID             string `json:"id"`
		Name           string `json:"name,omitempty"`
		Enabled        bool   `json:"enabled"`
		ActivationMode string `json:"activationMode,omitempty"`
	}
	profile := a.mainAgentProfile()
	payload := struct {
		HostID string           `json:"hostId"`
		Skills []skillState     `json:"skills"`
		MCPs   []model.AgentMCP `json:"mcps"`
	}{
		HostID: defaultHostID(hostID),
		Skills: make([]skillState, 0, len(profile.Skills)),
		MCPs:   append([]model.AgentMCP(nil), a.effectiveEnabledAgentMCPs(profile, hostID)...),
	}
	for _, item := range profile.Skills {
		payload.Skills = append(payload.Skills, skillState{
			ID:             strings.TrimSpace(item.ID),
			Name:           strings.TrimSpace(item.Name),
			Enabled:        skillEnabledByProfile(item),
			ActivationMode: model.NormalizeAgentSkillActivationMode(item.ActivationMode),
		})
	}
	content, _ := json.Marshal(payload)
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func (a *App) shouldRefreshThreadForAgentRuntime(session *store.SessionState, hostID string) bool {
	if session == nil || session.ThreadID == "" {
		return false
	}
	expected := a.mainAgentThreadConfigHash(hostID)
	return strings.TrimSpace(session.ThreadConfigHash) != strings.TrimSpace(expected)
}

func (a *App) appendAgentProfileRuntimeRefreshCard(sessionID string) {
	now := model.NowString()
	a.store.UpsertCard(sessionID, model.Card{
		ID:        model.NewID("notice"),
		Type:      "NoticeCard",
		Title:     "Agent runtime refreshed",
		Text:      "Agent Profile 的 skills / MCP runtime 配置已更新，已切换到新的线程以应用最新暴露范围。",
		Status:    "notice",
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (a *App) buildMainAgentTurnInput(ctx context.Context, profile model.AgentProfile, message string) []map[string]any {
	items := []map[string]any{
		{"type": "text", "text": message},
	}
	discovered, err := a.listCodexSkills(ctx)
	if err != nil {
		log.Printf("main-agent skills/list skipped while building turn input: %v", err)
		return items
	}
	pathMap := buildManagedSkillPathMap(profile, discovered)
	selectedPaths := make(map[string]struct{})
	for _, item := range profile.Skills {
		if !skillEnabledByProfile(item) {
			continue
		}
		mode := model.NormalizeAgentSkillActivationMode(item.ActivationMode)
		if mode == model.AgentSkillActivationExplicit && !explicitSkillRequested(message, item) {
			continue
		}
		path := ""
		for _, candidate := range []string{item.ID, item.Name} {
			path = strings.TrimSpace(pathMap[normalizeSkillLookupKey(candidate)])
			if path != "" {
				break
			}
		}
		if path == "" {
			continue
		}
		if _, exists := selectedPaths[path]; exists {
			continue
		}
		selectedPaths[path] = struct{}{}
		items = append(items, map[string]any{
			"type": "skill",
			"name": strings.TrimSpace(item.Name),
			"path": path,
		})
	}
	return items
}
