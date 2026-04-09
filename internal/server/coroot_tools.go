package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/coroot"
)

const (
	corootToolListServices    = "coroot_list_services"
	corootToolServiceOverview = "coroot_service_overview"
	corootToolServiceMetrics  = "coroot_service_metrics"
	corootToolServiceAlerts   = "coroot_service_alerts"
	corootToolTopology        = "coroot_topology"
	corootToolIncidentTime    = "coroot_incident_timeline"
	corootToolRCAReport       = "coroot_rca_report"
)

// corootDynamicTools returns the 7 Coroot MCP dynamic tool definitions in the
// same map[string]any format used by remoteDynamicTools().
func (a *App) corootDynamicTools() []map[string]any {
	if a.corootClient == nil {
		return nil
	}
	return []map[string]any{
		{
			"name":        corootToolListServices,
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
			"name":        corootToolServiceOverview,
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
			"name":        corootToolServiceMetrics,
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
			"name":        corootToolServiceAlerts,
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
			"name":        corootToolTopology,
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
			"name":        corootToolIncidentTime,
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
			"name":        corootToolRCAReport,
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

func normalizeCorootToolName(name string) string {
	switch strings.TrimSpace(name) {
	case "coroot.list_services", corootToolListServices:
		return corootToolListServices
	case "coroot.service_overview", corootToolServiceOverview:
		return corootToolServiceOverview
	case "coroot.service_metrics", corootToolServiceMetrics:
		return corootToolServiceMetrics
	case "coroot.service_alerts", corootToolServiceAlerts:
		return corootToolServiceAlerts
	case "coroot.topology", corootToolTopology:
		return corootToolTopology
	case "coroot.incident_timeline", corootToolIncidentTime:
		return corootToolIncidentTime
	case "coroot.rca_report", corootToolRCAReport:
		return corootToolRCAReport
	default:
		return strings.TrimSpace(name)
	}
}

// isCorootTool returns true for both the current underscore names and the
// legacy dot-separated aliases kept for compatibility with old transcripts.
func isCorootTool(name string) bool {
	switch normalizeCorootToolName(name) {
	case corootToolListServices,
		corootToolServiceOverview,
		corootToolServiceMetrics,
		corootToolServiceAlerts,
		corootToolTopology,
		corootToolIncidentTime,
		corootToolRCAReport:
		return true
	default:
		return false
	}
}

// executeCorootTool handles the execution of a Coroot dynamic tool call.
func (a *App) executeCorootTool(sessionID, rawID string, params dynamicToolCallParams) {
	if a.corootClient == nil {
		_ = a.respondCodex(context.Background(), rawID, toolResponse("Coroot is not configured.", false))
		return
	}

	ctx := context.Background()
	toolName := normalizeCorootToolName(params.Tool)

	switch toolName {
	case corootToolListServices:
		services, err := a.corootClient.ListServices(ctx)
		if err != nil {
			_ = a.respondCodex(ctx, rawID, toolResponse(fmt.Sprintf("%s failed: %v", toolName, err), false))
			return
		}
		card := formatServicesForCard(services)
		_ = a.respondCodex(ctx, rawID, toolResponse(mustJSON(card), true))

	case corootToolServiceOverview:
		serviceID := strings.TrimSpace(getStringAny(params.Arguments, "service_id"))
		if serviceID == "" {
			_ = a.respondCodex(ctx, rawID, toolResponse("service_id is required", false))
			return
		}
		result, err := a.corootClient.ServiceOverview(ctx, serviceID)
		if err != nil {
			_ = a.respondCodex(ctx, rawID, toolResponse(fmt.Sprintf("%s failed: %v", toolName, err), false))
			return
		}
		card := formatServiceOverviewForCard(result)
		_ = a.respondCodex(ctx, rawID, toolResponse(mustJSON(card), true))

	case corootToolServiceMetrics:
		serviceID := strings.TrimSpace(getStringAny(params.Arguments, "service_id"))
		from := strings.TrimSpace(getStringAny(params.Arguments, "from"))
		to := strings.TrimSpace(getStringAny(params.Arguments, "to"))
		if serviceID == "" || from == "" || to == "" {
			_ = a.respondCodex(ctx, rawID, toolResponse("service_id, from, and to are required", false))
			return
		}
		result, err := a.corootClient.ServiceMetrics(ctx, serviceID, coroot.TimeRange{From: from, To: to})
		if err != nil {
			_ = a.respondCodex(ctx, rawID, toolResponse(fmt.Sprintf("%s failed: %v", toolName, err), false))
			return
		}
		card := formatMetricsForCard(result)
		_ = a.respondCodex(ctx, rawID, toolResponse(mustJSON(card), true))

	case corootToolServiceAlerts:
		serviceID := strings.TrimSpace(getStringAny(params.Arguments, "service_id"))
		if serviceID == "" {
			_ = a.respondCodex(ctx, rawID, toolResponse("service_id is required", false))
			return
		}
		alerts, err := a.corootClient.ServiceAlerts(ctx, serviceID)
		if err != nil {
			_ = a.respondCodex(ctx, rawID, toolResponse(fmt.Sprintf("%s failed: %v", toolName, err), false))
			return
		}
		card := formatAlertsForCard(alerts)
		_ = a.respondCodex(ctx, rawID, toolResponse(mustJSON(card), true))

	case corootToolTopology:
		result, err := a.corootClient.Topology(ctx)
		if err != nil {
			_ = a.respondCodex(ctx, rawID, toolResponse(fmt.Sprintf("%s failed: %v", toolName, err), false))
			return
		}
		_ = a.respondCodex(ctx, rawID, toolResponse(mustJSON(result), true))

	case corootToolIncidentTime:
		incidentID := strings.TrimSpace(getStringAny(params.Arguments, "incident_id"))
		if incidentID == "" {
			_ = a.respondCodex(ctx, rawID, toolResponse("incident_id is required", false))
			return
		}
		result, err := a.corootClient.IncidentTimeline(ctx, incidentID)
		if err != nil {
			_ = a.respondCodex(ctx, rawID, toolResponse(fmt.Sprintf("%s failed: %v", toolName, err), false))
			return
		}
		_ = a.respondCodex(ctx, rawID, toolResponse(mustJSON(result), true))

	case corootToolRCAReport:
		incidentID := strings.TrimSpace(getStringAny(params.Arguments, "incident_id"))
		if incidentID == "" {
			_ = a.respondCodex(ctx, rawID, toolResponse("incident_id is required", false))
			return
		}
		result, err := a.corootClient.RCAReport(ctx, incidentID)
		if err != nil {
			_ = a.respondCodex(ctx, rawID, toolResponse(fmt.Sprintf("%s failed: %v", toolName, err), false))
			return
		}
		_ = a.respondCodex(ctx, rawID, toolResponse(mustJSON(result), true))

	default:
		_ = a.respondCodex(ctx, rawID, toolResponse("Unknown coroot tool: "+params.Tool, false))
	}
}

// ---------- formatForCard helpers ----------

// formatServiceOverviewForCard converts a ServiceOverviewResult into an
// McpSummaryCard payload (map[string]any) with uiKind "readonly_summary".
func formatServiceOverviewForCard(result *coroot.ServiceOverviewResult) map[string]any {
	name := result.Name
	if name == "" {
		name = "N/A"
	}
	status := result.Status
	if status == "" {
		status = "N/A"
	}
	id := result.ID
	if id == "" {
		id = "N/A"
	}

	rows := []map[string]any{
		{"label": "服务 ID", "value": id},
		{"label": "状态", "value": status, "highlight": true},
	}

	// Append summary fields as additional rows.
	if result.Summary != nil {
		for k, v := range result.Summary {
			val := "N/A"
			if v != nil {
				val = fmt.Sprintf("%v", v)
			}
			rows = append(rows, map[string]any{"label": k, "value": val})
		}
	}

	return map[string]any{
		"uiKind": "readonly_summary",
		"title":  name + " 服务概览",
		"status": status,
		"rows":   rows,
	}
}

// formatMetricsForCard converts a MetricsResult into an
// McpTimeseriesChartCard payload with uiKind "readonly_chart".
func formatMetricsForCard(result *coroot.MetricsResult) map[string]any {
	series := make([]map[string]any, 0, len(result.Metrics))
	for _, m := range result.Metrics {
		name := "N/A"
		if n, ok := m["name"]; ok && n != nil {
			name = fmt.Sprintf("%v", n)
		}

		var data []map[string]any
		if vals, ok := m["values"]; ok && vals != nil {
			if arr, ok := vals.([]any); ok {
				data = make([]map[string]any, 0, len(arr))
				for _, point := range arr {
					ts, v := extractTimeseriesPoint(point)
					data = append(data, map[string]any{"timestamp": ts, "value": v})
				}
			}
		}
		if data == nil {
			data = []map[string]any{}
		}

		series = append(series, map[string]any{"name": name, "data": data})
	}

	return map[string]any{
		"uiKind": "readonly_chart",
		"title":  "指标趋势",
		"visual": map[string]any{
			"kind":   "timeseries",
			"series": series,
		},
	}
}

// extractTimeseriesPoint extracts (timestamp, value) from a single data point.
// Coroot returns points as [timestamp, value] arrays.
func extractTimeseriesPoint(point any) (float64, float64) {
	switch p := point.(type) {
	case []any:
		var ts, v float64
		if len(p) > 0 {
			ts = toFloat64(p[0])
		}
		if len(p) > 1 {
			v = toFloat64(p[1])
		}
		return ts, v
	default:
		return 0, 0
	}
}

// toFloat64 converts a numeric any value to float64, returning 0 on failure.
func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}

// formatAlertsForCard converts a slice of Alert into an
// McpStatusTableCard payload with uiKind "readonly_chart".
func formatAlertsForCard(alerts []coroot.Alert) map[string]any {
	rows := make([]map[string]any, 0, len(alerts))
	for _, a := range alerts {
		id := a.ID
		if id == "" {
			id = "N/A"
		}
		name := a.Name
		if name == "" {
			name = "N/A"
		}
		severity := strings.ToLower(a.Severity)
		if severity == "" {
			severity = "N/A"
		}
		status := a.Status
		if status == "" {
			status = "N/A"
		}
		rows = append(rows, map[string]any{
			"cells":  []string{id, name, severity, status},
			"status": severity,
		})
	}

	return map[string]any{
		"uiKind": "readonly_chart",
		"title":  "告警列表",
		"visual": map[string]any{
			"kind":    "status_table",
			"columns": []string{"ID", "名称", "严重程度", "状态"},
			"rows":    rows,
		},
	}
}

// formatServicesForCard converts a slice of Service into an
// McpKpiStripCard payload with uiKind "readonly_summary".
func formatServicesForCard(services []coroot.Service) map[string]any {
	total := len(services)
	healthy, warning, critical := 0, 0, 0
	for _, s := range services {
		switch strings.ToLower(s.Status) {
		case "ok", "healthy":
			healthy++
		case "warning":
			warning++
		case "critical", "error":
			critical++
		default:
			// Unknown statuses count toward the total but not any bucket.
			// Treat unknown statuses as critical (conservative approach,
			// consistent with the frontend corootCardAdapter).
			critical++
		}
	}

	return map[string]any{
		"uiKind": "readonly_summary",
		"title":  "服务健康概览",
		"kpis": []map[string]any{
			{"label": "总服务数", "value": total},
			{"label": "健康", "value": healthy, "color": "green"},
			{"label": "告警", "value": warning, "color": "amber"},
			{"label": "异常", "value": critical, "color": "red"},
		},
		"visual": map[string]any{"kind": "kpi_strip"},
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
