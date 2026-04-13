package agentloop

// RegisterCorootTools registers the 7 Coroot MCP dynamic tool definitions into
// the given ToolRegistry. Handlers are placeholder stubs — actual
// implementations are wired during server integration (task 15).
func RegisterCorootTools(reg *ToolRegistry) {
	reg.Register(ToolEntry{
		Name:        "coroot_list_services",
		Description: "List all services monitored by Coroot. Returns service IDs, names, and health status.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "One-sentence explanation of why you are listing services.",
				},
			},
			"required":             []string{"reason"},
			"additionalProperties": false,
		},
		IsReadOnly: true,
	})

	reg.Register(ToolEntry{
		Name:        "coroot_service_overview",
		Description: "Get the overview for a single Coroot service including health status and summary metrics.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"service_id": map[string]interface{}{
					"type":        "string",
					"description": "The Coroot service ID to inspect.",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "One-sentence explanation of what you are checking.",
				},
			},
			"required":             []string{"service_id", "reason"},
			"additionalProperties": false,
		},
		IsReadOnly: true,
	})

	reg.Register(ToolEntry{
		Name:        "coroot_service_metrics",
		Description: "Query metrics for a Coroot service within a time range.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"service_id": map[string]interface{}{
					"type":        "string",
					"description": "The Coroot service ID to query metrics for.",
				},
				"from": map[string]interface{}{
					"type":        "string",
					"description": "Start of the time range (ISO 8601 or relative like -1h).",
				},
				"to": map[string]interface{}{
					"type":        "string",
					"description": "End of the time range (ISO 8601 or relative like now).",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "One-sentence explanation of what metrics you need.",
				},
			},
			"required":             []string{"service_id", "from", "to", "reason"},
			"additionalProperties": false,
		},
		IsReadOnly: true,
	})

	reg.Register(ToolEntry{
		Name:        "coroot_service_alerts",
		Description: "Get active alerts for a Coroot service.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"service_id": map[string]interface{}{
					"type":        "string",
					"description": "The Coroot service ID to check alerts for.",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "One-sentence explanation of what alerts you are checking.",
				},
			},
			"required":             []string{"service_id", "reason"},
			"additionalProperties": false,
		},
		IsReadOnly: true,
	})

	reg.Register(ToolEntry{
		Name:        "coroot_topology",
		Description: "Get the full service topology graph from Coroot showing service dependencies and connections.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "One-sentence explanation of why you need the topology.",
				},
			},
			"required":             []string{"reason"},
			"additionalProperties": false,
		},
		IsReadOnly: true,
	})

	reg.Register(ToolEntry{
		Name:        "coroot_incident_timeline",
		Description: "Get the timeline of events for a specific Coroot incident.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"incident_id": map[string]interface{}{
					"type":        "string",
					"description": "The Coroot incident ID to get the timeline for.",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "One-sentence explanation of what you are investigating.",
				},
			},
			"required":             []string{"incident_id", "reason"},
			"additionalProperties": false,
		},
		IsReadOnly: true,
	})

	reg.Register(ToolEntry{
		Name:        "coroot_rca_report",
		Description: "Get the root-cause analysis report for a Coroot incident including identified root causes and remediation suggestions.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"incident_id": map[string]interface{}{
					"type":        "string",
					"description": "The Coroot incident ID to get the RCA report for.",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "One-sentence explanation of what root cause you are investigating.",
				},
			},
			"required":             []string{"incident_id", "reason"},
			"additionalProperties": false,
		},
		IsReadOnly: true,
	})
}
