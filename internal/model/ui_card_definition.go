package model

// UICardDefinition describes a UI card type that can be rendered in the
// frontend. The system ships with a set of built-in definitions that are
// automatically registered on startup; users may also create custom ones.
type UICardDefinition struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	Kind              string         `json:"kind"`                          // readonly_summary, readonly_chart, action_panel, form_panel, monitor_bundle, remediation_bundle
	Renderer          string         `json:"renderer"`                     // Vue component name
	BundleSupport     []string       `json:"bundleSupport,omitempty"`
	PlacementDefaults []string       `json:"placementDefaults,omitempty"`
	Summary           string         `json:"summary"`
	Capabilities      []string       `json:"capabilities,omitempty"`
	TriggerTypes      []string       `json:"triggerTypes,omitempty"`
	InputSchema       map[string]any `json:"inputSchema,omitempty"`
	ActionSchema      map[string]any `json:"actionSchema,omitempty"`
	EditableFields    []string       `json:"editableFields,omitempty"`
	Status            string         `json:"status"`                       // active | draft | disabled
	BuiltIn           bool           `json:"builtIn"`
	Version           int            `json:"version"`
	CreatedAt         string         `json:"createdAt"`
	UpdatedAt         string         `json:"updatedAt"`
}

// DefaultUICardDefinitions returns the 9 built-in card definitions that are
// automatically registered when the system starts.
func DefaultUICardDefinitions() []UICardDefinition {
	now := NowString()
	return []UICardDefinition{
		{
			ID:                "mcp-summary",
			Name:              "摘要卡片",
			Kind:              "readonly_summary",
			Renderer:          "McpSummaryCard",
			BundleSupport:     []string{"monitor_bundle", "remediation_bundle"},
			PlacementDefaults: []string{"chat", "workspace"},
			Summary:           "展示结构化摘要信息，支持 KV 行和高亮条目。",
			Capabilities:      []string{"kv_rows", "highlights"},
			TriggerTypes:      []string{"mcp_tool_result", "ai_summary"},
			EditableFields:    []string{"name", "summary", "placementDefaults"},
			Status:            "active",
			BuiltIn:           true,
			Version:           1,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		{
			ID:                "mcp-kpi-strip",
			Name:              "KPI 指标条",
			Kind:              "readonly_summary",
			Renderer:          "McpKpiStripCard",
			BundleSupport:     []string{"monitor_bundle"},
			PlacementDefaults: []string{"chat", "workspace"},
			Summary:           "横向展示关键 KPI 指标，适合监控总览场景。",
			Capabilities:      []string{"kpi_values"},
			TriggerTypes:      []string{"mcp_tool_result"},
			EditableFields:    []string{"name", "summary", "placementDefaults"},
			Status:            "active",
			BuiltIn:           true,
			Version:           1,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		{
			ID:                "mcp-timeseries-chart",
			Name:              "时序图卡片",
			Kind:              "readonly_chart",
			Renderer:          "McpTimeseriesChartCard",
			BundleSupport:     []string{"monitor_bundle"},
			PlacementDefaults: []string{"chat", "workspace"},
			Summary:           "渲染时序数据图表，支持多指标叠加。",
			Capabilities:      []string{"timeseries", "multi_metric"},
			TriggerTypes:      []string{"mcp_tool_result", "coroot_metrics"},
			EditableFields:    []string{"name", "summary", "placementDefaults"},
			Status:            "active",
			BuiltIn:           true,
			Version:           1,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		{
			ID:                "mcp-status-table",
			Name:              "状态表卡片",
			Kind:              "readonly_chart",
			Renderer:          "McpStatusTableCard",
			BundleSupport:     []string{"monitor_bundle"},
			PlacementDefaults: []string{"chat", "workspace"},
			Summary:           "以表格形式展示服务或资源状态列表。",
			Capabilities:      []string{"status_rows", "sortable"},
			TriggerTypes:      []string{"mcp_tool_result", "coroot_alerts"},
			EditableFields:    []string{"name", "summary", "placementDefaults"},
			Status:            "active",
			BuiltIn:           true,
			Version:           1,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		{
			ID:                "mcp-control-panel",
			Name:              "控制面板卡片",
			Kind:              "action_panel",
			Renderer:          "McpControlPanelCard",
			BundleSupport:     []string{"remediation_bundle"},
			PlacementDefaults: []string{"chat"},
			Summary:           "提供操作按钮面板，支持服务重启、配置应用等受控变更。",
			Capabilities:      []string{"action_buttons", "approval_required"},
			TriggerTypes:      []string{"user_action", "ai_recommendation"},
			EditableFields:    []string{"name", "summary", "placementDefaults", "actionSchema"},
			Status:            "active",
			BuiltIn:           true,
			Version:           1,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		{
			ID:                "mcp-action-form",
			Name:              "操作表单卡片",
			Kind:              "form_panel",
			Renderer:          "McpActionFormCard",
			BundleSupport:     []string{"remediation_bundle"},
			PlacementDefaults: []string{"chat"},
			Summary:           "表单式操作卡片，支持参数输入和提交前预览。",
			Capabilities:      []string{"form_fields", "dry_run", "approval_required"},
			TriggerTypes:      []string{"user_action", "script_config"},
			EditableFields:    []string{"name", "summary", "placementDefaults", "inputSchema", "actionSchema"},
			Status:            "active",
			BuiltIn:           true,
			Version:           1,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		{
			ID:                "mcp-generic-action",
			Name:              "通用操作卡片",
			Kind:              "action_panel",
			Renderer:          "GenericMcpActionCard",
			BundleSupport:     []string{"remediation_bundle"},
			PlacementDefaults: []string{"chat", "workspace"},
			Summary:           "通用 MCP 工具操作卡片，适配任意 MCP 工具调用结果。",
			Capabilities:      []string{"generic_action"},
			TriggerTypes:      []string{"mcp_tool_result"},
			EditableFields:    []string{"name", "summary", "placementDefaults"},
			Status:            "active",
			BuiltIn:           true,
			Version:           1,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		{
			ID:                "mcp-monitor-bundle",
			Name:              "监控聚合卡片",
			Kind:              "monitor_bundle",
			Renderer:          "McpMonitorBundleCard",
			BundleSupport:     nil,
			PlacementDefaults: []string{"chat", "workspace"},
			Summary:           "聚合多个监控子卡片，提供服务级监控总览。",
			Capabilities:      []string{"sub_cards", "auto_refresh", "topology_aware"},
			TriggerTypes:      []string{"coroot_metrics", "mcp_tool_result"},
			EditableFields:    []string{"name", "summary", "placementDefaults"},
			Status:            "active",
			BuiltIn:           true,
			Version:           1,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		{
			ID:                "mcp-remediation-bundle",
			Name:              "修复聚合卡片",
			Kind:              "remediation_bundle",
			Renderer:          "McpRemediationBundleCard",
			BundleSupport:     nil,
			PlacementDefaults: []string{"chat"},
			Summary:           "聚合诊断与修复操作，提供端到端故障修复流程。",
			Capabilities:      []string{"sub_cards", "step_flow", "approval_required"},
			TriggerTypes:      []string{"ai_recommendation", "coroot_rca"},
			EditableFields:    []string{"name", "summary", "placementDefaults"},
			Status:            "active",
			BuiltIn:           true,
			Version:           1,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
	}
}
