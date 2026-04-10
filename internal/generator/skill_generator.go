package generator

import (
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// CorootToolMeta describes a single Coroot API tool for batch skill generation.
type CorootToolMeta struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
	Category    string         `json:"category,omitempty"` // monitoring, diagnostics, remediation, or empty for auto-infer
}

// buildSkillsFromCorootTools generates a slice of draft AgentSkills from
// Coroot tool metadata. If a tool's Category is empty the category is
// automatically inferred from the tool name and description.
func buildSkillsFromCorootTools(tools []CorootToolMeta) []model.AgentSkill {
	skills := make([]model.AgentSkill, 0, len(tools))
	for _, t := range tools {
		category := t.Category
		if strings.TrimSpace(category) == "" {
			category = inferCorootCategory(t.Name, t.Description)
		}
		deps := inferSkillDependencies(t.InputSchema)

		skill := model.AgentSkill{
			ID:                    model.NewID("skill"),
			Name:                  t.Name,
			Description:           t.Description,
			Source:                "coroot-generated",
			Enabled:               false,
			ActivationMode:        model.AgentSkillActivationExplicit,
			DefaultActivationMode: model.AgentSkillActivationExplicit,
			DefaultEnabled:        false,
			Category:              category,
			Version:               "v1-draft",
			Status:                "draft",
			Dependencies:          deps,
		}
		skills = append(skills, skill)
	}
	return skills
}

// inferCorootCategory guesses a Coroot-specific category from the tool name
// and description. It returns one of "monitoring", "diagnostics", or
// "remediation", falling back to "monitoring" as the default for Coroot tools.
func inferCorootCategory(name, desc string) string {
	combined := strings.ToLower(name + " " + desc)
	switch {
	case strings.Contains(combined, "rca") ||
		strings.Contains(combined, "root cause") ||
		strings.Contains(combined, "diagnos") ||
		strings.Contains(combined, "triage") ||
		strings.Contains(combined, "incident"):
		return "diagnostics"
	case strings.Contains(combined, "remediat") ||
		strings.Contains(combined, "fix") ||
		strings.Contains(combined, "repair") ||
		strings.Contains(combined, "rollback"):
		return "remediation"
	default:
		// Most Coroot endpoints are observability-oriented.
		return "monitoring"
	}
}

// DefaultCorootTools returns the predefined CorootToolMeta entries that
// correspond to the known Coroot API endpoints (see internal/coroot/client.go).
func DefaultCorootTools() []CorootToolMeta {
	return []CorootToolMeta{
		{
			Name:        "ListServices",
			Description: "List all services known to Coroot.",
			InputSchema: nil,
			Category:    "monitoring",
		},
		{
			Name:        "ServiceOverview",
			Description: "Get the overview for a single Coroot service including status and summary.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"serviceId": map[string]any{"type": "string"},
				},
				"required": []any{"serviceId"},
			},
			Category: "monitoring",
		},
		{
			Name:        "ServiceMetrics",
			Description: "Query metrics for a Coroot service within a time range.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"serviceId": map[string]any{"type": "string"},
					"from":      map[string]any{"type": "string"},
					"to":        map[string]any{"type": "string"},
				},
				"required": []any{"serviceId", "from", "to"},
			},
			Category: "monitoring",
		},
		{
			Name:        "ServiceAlerts",
			Description: "Get alerts for a Coroot service.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"serviceId": map[string]any{"type": "string"},
				},
				"required": []any{"serviceId"},
			},
			Category: "monitoring",
		},
		{
			Name:        "Topology",
			Description: "Get the full Coroot service topology graph showing service dependencies.",
			InputSchema: nil,
			Category:    "monitoring",
		},
		{
			Name:        "IncidentTimeline",
			Description: "Get the timeline for a specific Coroot incident for diagnostics.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"incidentId": map[string]any{"type": "string"},
				},
				"required": []any{"incidentId"},
			},
			Category: "diagnostics",
		},
		{
			Name:        "RCAReport",
			Description: "Get the root-cause analysis report for a Coroot incident.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"incidentId": map[string]any{"type": "string"},
				},
				"required": []any{"incidentId"},
			},
			Category: "diagnostics",
		},
		{
			Name:        "ServiceDependencies",
			Description: "Get upstream and downstream dependencies for a Coroot service.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"serviceId": map[string]any{"type": "string"},
				},
				"required": []any{"serviceId"},
			},
			Category: "monitoring",
		},
		{
			Name:        "HostOverview",
			Description: "Get the overview for a single host including CPU, memory, disk and network metrics.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"hostId": map[string]any{"type": "string"},
				},
				"required": []any{"hostId"},
			},
			Category: "monitoring",
		},
	}
}

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
