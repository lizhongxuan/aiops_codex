package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/coroot"
)

// corootDynamicTools returns the 7 Coroot MCP dynamic tool definitions in the
// same map[string]any format used by remoteDynamicTools().
func (a *App) corootDynamicTools() []map[string]any {
	if a.corootClient == nil {
		return nil
	}
	return []map[string]any{
		{
			"name":        "coroot.list_services",
			"description": "List all services monitored by Coroot. Returns service IDs, names, and health status.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"reason": map[string]any{
						"type":        "string",
						"description": "One-sentence explanation of why you are listing services.",
					},
				},
				"required":             []string{"reason"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "coroot.service_overview",
			"description": "Get the overview for a single Coroot service including health status and summary metrics.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"service_id": map[string]any{
						"type":        "string",
						"description": "The Coroot service ID to inspect.",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "One-sentence explanation of what you are checking.",
					},
				},
				"required":             []string{"service_id", "reason"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "coroot.service_metrics",
			"description": "Query metrics for a Coroot service within a time range.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"service_id": map[string]any{
						"type":        "string",
						"description": "The Coroot service ID to query metrics for.",
					},
					"from": map[string]any{
						"type":        "string",
						"description": "Start of the time range (ISO 8601 or relative like -1h).",
					},
					"to": map[string]any{
						"type":        "string",
						"description": "End of the time range (ISO 8601 or relative like now).",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "One-sentence explanation of what metrics you need.",
					},
				},
				"required":             []string{"service_id", "from", "to", "reason"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "coroot.service_alerts",
			"description": "Get active alerts for a Coroot service.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"service_id": map[string]any{
						"type":        "string",
						"description": "The Coroot service ID to check alerts for.",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "One-sentence explanation of what alerts you are checking.",
					},
				},
				"required":             []string{"service_id", "reason"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "coroot.topology",
			"description": "Get the full service topology graph from Coroot showing service dependencies and connections.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"reason": map[string]any{
						"type":        "string",
						"description": "One-sentence explanation of why you need the topology.",
					},
				},
				"required":             []string{"reason"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "coroot.incident_timeline",
			"description": "Get the timeline of events for a specific Coroot incident.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"incident_id": map[string]any{
						"type":        "string",
						"description": "The Coroot incident ID to get the timeline for.",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "One-sentence explanation of what you are investigating.",
					},
				},
				"required":             []string{"incident_id", "reason"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "coroot.rca_report",
			"description": "Get the root-cause analysis report for a Coroot incident including identified root causes and remediation suggestions.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"incident_id": map[string]any{
						"type":        "string",
						"description": "The Coroot incident ID to get the RCA report for.",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "One-sentence explanation of what root cause you are investigating.",
					},
				},
				"required":             []string{"incident_id", "reason"},
				"additionalProperties": false,
			},
		},
	}
}

// isCorootTool returns true if the tool name has the coroot.* prefix.
func isCorootTool(name string) bool {
	return strings.HasPrefix(name, "coroot.")
}

// executeCorootTool handles the execution of a coroot.* dynamic tool call.
func (a *App) executeCorootTool(sessionID, rawID string, params dynamicToolCallParams) {
	if a.corootClient == nil {
		_ = a.respondCodex(context.Background(), rawID, toolResponse("Coroot is not configured.", false))
		return
	}

	ctx := context.Background()

	switch params.Tool {
	case "coroot.list_services":
		services, err := a.corootClient.ListServices(ctx)
		if err != nil {
			_ = a.respondCodex(ctx, rawID, toolResponse(fmt.Sprintf("coroot.list_services failed: %v", err), false))
			return
		}
		_ = a.respondCodex(ctx, rawID, toolResponse(mustJSON(services), true))

	case "coroot.service_overview":
		serviceID := strings.TrimSpace(getStringAny(params.Arguments, "service_id"))
		if serviceID == "" {
			_ = a.respondCodex(ctx, rawID, toolResponse("service_id is required", false))
			return
		}
		result, err := a.corootClient.ServiceOverview(ctx, serviceID)
		if err != nil {
			_ = a.respondCodex(ctx, rawID, toolResponse(fmt.Sprintf("coroot.service_overview failed: %v", err), false))
			return
		}
		_ = a.respondCodex(ctx, rawID, toolResponse(mustJSON(result), true))

	case "coroot.service_metrics":
		serviceID := strings.TrimSpace(getStringAny(params.Arguments, "service_id"))
		from := strings.TrimSpace(getStringAny(params.Arguments, "from"))
		to := strings.TrimSpace(getStringAny(params.Arguments, "to"))
		if serviceID == "" || from == "" || to == "" {
			_ = a.respondCodex(ctx, rawID, toolResponse("service_id, from, and to are required", false))
			return
		}
		result, err := a.corootClient.ServiceMetrics(ctx, serviceID, coroot.TimeRange{From: from, To: to})
		if err != nil {
			_ = a.respondCodex(ctx, rawID, toolResponse(fmt.Sprintf("coroot.service_metrics failed: %v", err), false))
			return
		}
		_ = a.respondCodex(ctx, rawID, toolResponse(mustJSON(result), true))

	case "coroot.service_alerts":
		serviceID := strings.TrimSpace(getStringAny(params.Arguments, "service_id"))
		if serviceID == "" {
			_ = a.respondCodex(ctx, rawID, toolResponse("service_id is required", false))
			return
		}
		alerts, err := a.corootClient.ServiceAlerts(ctx, serviceID)
		if err != nil {
			_ = a.respondCodex(ctx, rawID, toolResponse(fmt.Sprintf("coroot.service_alerts failed: %v", err), false))
			return
		}
		_ = a.respondCodex(ctx, rawID, toolResponse(mustJSON(alerts), true))

	case "coroot.topology":
		result, err := a.corootClient.Topology(ctx)
		if err != nil {
			_ = a.respondCodex(ctx, rawID, toolResponse(fmt.Sprintf("coroot.topology failed: %v", err), false))
			return
		}
		_ = a.respondCodex(ctx, rawID, toolResponse(mustJSON(result), true))

	case "coroot.incident_timeline":
		incidentID := strings.TrimSpace(getStringAny(params.Arguments, "incident_id"))
		if incidentID == "" {
			_ = a.respondCodex(ctx, rawID, toolResponse("incident_id is required", false))
			return
		}
		result, err := a.corootClient.IncidentTimeline(ctx, incidentID)
		if err != nil {
			_ = a.respondCodex(ctx, rawID, toolResponse(fmt.Sprintf("coroot.incident_timeline failed: %v", err), false))
			return
		}
		_ = a.respondCodex(ctx, rawID, toolResponse(mustJSON(result), true))

	case "coroot.rca_report":
		incidentID := strings.TrimSpace(getStringAny(params.Arguments, "incident_id"))
		if incidentID == "" {
			_ = a.respondCodex(ctx, rawID, toolResponse("incident_id is required", false))
			return
		}
		result, err := a.corootClient.RCAReport(ctx, incidentID)
		if err != nil {
			_ = a.respondCodex(ctx, rawID, toolResponse(fmt.Sprintf("coroot.rca_report failed: %v", err), false))
			return
		}
		_ = a.respondCodex(ctx, rawID, toolResponse(mustJSON(result), true))

	default:
		_ = a.respondCodex(ctx, rawID, toolResponse("Unknown coroot tool: "+params.Tool, false))
	}
}

// mustJSON marshals v to JSON; on error returns a fallback string.
func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}
