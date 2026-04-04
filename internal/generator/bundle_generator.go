package generator

import (
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

// buildBundleFromCoroot generates a draft UICardDefinition of bundle kind
// from Coroot service type and query schema metadata.
func buildBundleFromCoroot(serviceType string, querySchema map[string]any) model.UICardDefinition {
	now := model.NowString()
	id := model.NewID("uicard")

	kind := inferBundleKind(serviceType)
	renderer := inferBundleRenderer(kind)
	capabilities := inferBundleCapabilities(kind, serviceType)
	triggerTypes := inferBundleTriggerTypes(serviceType)
	inputSchema := buildBundleInputSchema(serviceType, querySchema)

	card := model.UICardDefinition{
		ID:                id,
		Name:              serviceType + " 监控聚合",
		Kind:              kind,
		Renderer:          renderer,
		PlacementDefaults: []string{"chat", "workspace"},
		Summary:           "从 Coroot " + serviceType + " 服务自动生成的聚合卡片。",
		Capabilities:      capabilities,
		TriggerTypes:      triggerTypes,
		InputSchema:       inputSchema,
		EditableFields:    []string{"name", "summary", "placementDefaults"},
		Status:            "draft",
		BuiltIn:           false,
		Version:           1,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	return card
}

// inferBundleKind determines the bundle kind from the service type.
func inferBundleKind(serviceType string) string {
	lower := strings.ToLower(serviceType)
	if strings.Contains(lower, "remediat") || strings.Contains(lower, "fix") || strings.Contains(lower, "repair") {
		return "remediation_bundle"
	}
	return "monitor_bundle"
}

// inferBundleRenderer returns the renderer for the bundle kind.
func inferBundleRenderer(kind string) string {
	if kind == "remediation_bundle" {
		return "McpRemediationBundleCard"
	}
	return "McpMonitorBundleCard"
}

// inferBundleCapabilities derives capabilities from the bundle kind and service type.
func inferBundleCapabilities(kind, serviceType string) []string {
	caps := []string{"sub_cards", "auto_refresh"}
	lower := strings.ToLower(serviceType)
	if strings.Contains(lower, "topology") || strings.Contains(lower, "network") {
		caps = append(caps, "topology_aware")
	}
	if kind == "remediation_bundle" {
		caps = append(caps, "step_flow", "approval_required")
	}
	return caps
}

// inferBundleTriggerTypes derives trigger types from the service type.
func inferBundleTriggerTypes(serviceType string) []string {
	triggers := []string{"coroot_metrics"}
	lower := strings.ToLower(serviceType)
	if strings.Contains(lower, "alert") {
		triggers = append(triggers, "coroot_alerts")
	}
	if strings.Contains(lower, "rca") || strings.Contains(lower, "incident") {
		triggers = append(triggers, "coroot_rca")
	}
	return triggers
}

// buildBundleInputSchema merges the Coroot query schema with service type context.
func buildBundleInputSchema(serviceType string, querySchema map[string]any) map[string]any {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"serviceType": map[string]any{
				"type":    "string",
				"default": serviceType,
			},
		},
	}
	// Merge in any properties from the provided query schema.
	if props, ok := querySchema["properties"].(map[string]any); ok {
		target := schema["properties"].(map[string]any)
		for k, v := range props {
			target[k] = v
		}
	}
	return schema
}
