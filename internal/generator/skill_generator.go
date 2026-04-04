package generator

import (
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// buildSkillFromMCP generates a draft AgentSkill from MCP tool metadata.
// The resulting skill defaults to "draft" status and "explicit_only" activation.
func buildSkillFromMCP(toolName, toolDesc string, inputSchema map[string]any) model.AgentSkill {
	now := model.NowString()
	id := model.NewID("skill")

	category := inferSkillCategory(toolName, toolDesc)
	deps := inferSkillDependencies(inputSchema)

	skill := model.AgentSkill{
		ID:                    id,
		Name:                  toolName,
		Description:           toolDesc,
		Source:                "mcp-generated",
		Enabled:               false,
		ActivationMode:        model.AgentSkillActivationExplicit,
		DefaultActivationMode: model.AgentSkillActivationExplicit,
		DefaultEnabled:        false,
		Category:              category,
		Version:               "v1-draft",
		Status:                "draft",
		Dependencies:          deps,
	}
	_ = now // timestamp available for future audit fields
	return skill
}

// inferSkillCategory guesses a category from the tool name and description.
func inferSkillCategory(name, desc string) string {
	combined := strings.ToLower(name + " " + desc)
	switch {
	case strings.Contains(combined, "monitor") || strings.Contains(combined, "metric") || strings.Contains(combined, "alert"):
		return "monitoring"
	case strings.Contains(combined, "deploy") || strings.Contains(combined, "rollout"):
		return "deployment"
	case strings.Contains(combined, "diagnos") || strings.Contains(combined, "triage") || strings.Contains(combined, "rca"):
		return "diagnostics"
	case strings.Contains(combined, "remediat") || strings.Contains(combined, "fix") || strings.Contains(combined, "repair"):
		return "remediation"
	case strings.Contains(combined, "config") || strings.Contains(combined, "setting"):
		return "configuration"
	default:
		return "general"
	}
}

// inferSkillDependencies extracts dependency hints from the input schema.
func inferSkillDependencies(inputSchema map[string]any) []string {
	if len(inputSchema) == 0 {
		return nil
	}
	var deps []string
	props, _ := inputSchema["properties"].(map[string]any)
	for key := range props {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "service") || strings.Contains(lower, "host") {
			deps = append(deps, key)
		}
	}
	return deps
}
