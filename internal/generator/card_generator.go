package generator

import (
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// buildCardFromScript generates a draft UICardDefinition from a ScriptConfigProfile.
// The card kind is inferred from the script's approval policy and argument schema.
func buildCardFromScript(cfg model.ScriptConfigProfile) model.UICardDefinition {
	now := model.NowString()
	id := model.NewID("uicard")

	kind := inferCardKind(cfg)
	renderer := inferCardRenderer(kind)
	capabilities := inferCardCapabilities(kind, cfg)
	triggerTypes := inferCardTriggerTypes(cfg)

	card := model.UICardDefinition{
		ID:                id,
		Name:              cfg.ScriptName + " 卡片",
		Kind:              kind,
		Renderer:          renderer,
		BundleSupport:     inferBundleSupport(kind),
		PlacementDefaults: []string{"chat"},
		Summary:           cfg.Description,
		Capabilities:      capabilities,
		TriggerTypes:      triggerTypes,
		InputSchema:       cfg.ArgSchema,
		EditableFields:    []string{"name", "summary", "placementDefaults"},
		Status:            "draft",
		BuiltIn:           false,
		Version:           1,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if kind == "form_panel" || kind == "action_panel" {
		card.ActionSchema = buildActionSchema(cfg)
		card.EditableFields = append(card.EditableFields, "inputSchema", "actionSchema")
	}

	return card
}

// inferCardKind determines the card kind based on the script config.
func inferCardKind(cfg model.ScriptConfigProfile) string {
	hasArgs := len(cfg.ArgSchema) > 0
	needsApproval := cfg.ApprovalPolicy == "required"

	switch {
	case hasArgs && needsApproval:
		return "form_panel"
	case hasArgs:
		return "action_panel"
	case needsApproval:
		return "action_panel"
	default:
		return "readonly_summary"
	}
}

// inferCardRenderer returns the default Vue renderer component for a kind.
func inferCardRenderer(kind string) string {
	switch kind {
	case "readonly_summary":
		return "McpSummaryCard"
	case "readonly_chart":
		return "McpTimeseriesChartCard"
	case "action_panel":
		return "McpControlPanelCard"
	case "form_panel":
		return "McpActionFormCard"
	case "monitor_bundle":
		return "McpMonitorBundleCard"
	case "remediation_bundle":
		return "McpRemediationBundleCard"
	default:
		return "GenericMcpActionCard"
	}
}

// inferCardCapabilities derives capabilities from the kind and config.
func inferCardCapabilities(kind string, cfg model.ScriptConfigProfile) []string {
	var caps []string
	switch kind {
	case "form_panel":
		caps = append(caps, "form_fields", "dry_run")
	case "action_panel":
		caps = append(caps, "action_buttons")
	case "readonly_summary":
		caps = append(caps, "kv_rows")
	}
	if cfg.ApprovalPolicy == "required" {
		caps = append(caps, "approval_required")
	}
	return caps
}

// inferCardTriggerTypes derives trigger types from the config.
func inferCardTriggerTypes(cfg model.ScriptConfigProfile) []string {
	triggers := []string{"script_config"}
	lower := strings.ToLower(cfg.ScriptName + " " + cfg.Description)
	if strings.Contains(lower, "monitor") || strings.Contains(lower, "metric") {
		triggers = append(triggers, "coroot_metrics")
	}
	if strings.Contains(lower, "alert") {
		triggers = append(triggers, "coroot_alerts")
	}
	return triggers
}

// inferBundleSupport returns which bundle kinds this card can be embedded in.
func inferBundleSupport(kind string) []string {
	switch kind {
	case "readonly_summary", "readonly_chart":
		return []string{"monitor_bundle"}
	case "action_panel", "form_panel":
		return []string{"remediation_bundle"}
	default:
		return nil
	}
}

// buildActionSchema creates a basic action schema from the script config.
func buildActionSchema(cfg model.ScriptConfigProfile) map[string]any {
	schema := map[string]any{
		"type":       "script_execution",
		"scriptName": cfg.ScriptName,
	}
	if cfg.ApprovalPolicy != "" {
		schema["approvalPolicy"] = cfg.ApprovalPolicy
	}
	if cfg.RunnerProfile != "" {
		schema["runnerProfile"] = cfg.RunnerProfile
	}
	if cfg.EnvironmentRef != "" {
		schema["environmentRef"] = cfg.EnvironmentRef
	}
	return schema
}
