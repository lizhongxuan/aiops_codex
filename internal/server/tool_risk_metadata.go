package server

import (
	"fmt"
	"strings"
)

type toolRiskMetadata struct {
	Name              string
	DispatchCategory  toolDispatchCategory
	RiskLevel         string
	Readonly          bool
	Mutation          bool
	Dangerous         bool
	RequiresPlan      bool
	RequiresApproval  bool
	DryRunSupported   bool
	RollbackSupported bool
	VerifyPlan        verificationPlanDefinition
	VerifyStrategies  []string
	AllowedInPlanMode bool
}

func lookupToolRiskMetadata(toolName string) toolRiskMetadata {
	name := strings.TrimSpace(toolName)
	switch {
	case name == "":
		return toolRiskMetadata{}
	case isCorootTool(name):
		return readonlyToolRiskMetadata(name)
	case name == "ask_user_question":
		return toolRiskMetadata{
			Name:              name,
			DispatchCategory:  toolCategoryBlocking,
			RiskLevel:         "low",
			AllowedInPlanMode: true,
		}
	case name == "exit_plan_mode":
		return toolRiskMetadata{
			Name:              name,
			DispatchCategory:  toolCategoryApproval,
			RiskLevel:         "medium",
			RequiresApproval:  true,
			AllowedInPlanMode: true,
		}
	case name == "request_approval":
		return toolRiskMetadata{
			Name:             name,
			DispatchCategory: toolCategoryApproval,
			RiskLevel:        "high",
			Dangerous:        true,
			RequiresPlan:     true,
			RequiresApproval: true,
		}
	case name == "enter_plan_mode", name == "update_plan":
		return readonlyToolRiskMetadata(name)
	case name == "orchestrator_dispatch_tasks":
		plan := verificationPlanForTool(name)
		return toolRiskMetadata{
			Name:             name,
			DispatchCategory: toolCategoryMutation,
			RiskLevel:        "high",
			Mutation:         true,
			Dangerous:        true,
			RequiresPlan:     true,
			VerifyPlan:       plan,
			VerifyStrategies: plan.StrategyNames(),
		}
	case name == "execute_system_mutation" || isControlledMutationTool(name):
		return mutationToolRiskMetadata(name)
	case isReadonlyToolName(name):
		return readonlyToolRiskMetadata(name)
	default:
		return toolRiskMetadata{
			Name:             name,
			DispatchCategory: toolCategoryMutation,
			RiskLevel:        "high",
			Mutation:         true,
			Dangerous:        true,
			RequiresPlan:     true,
			RequiresApproval: true,
		}
	}
}

func readonlyToolRiskMetadata(name string) toolRiskMetadata {
	return toolRiskMetadata{
		Name:              name,
		DispatchCategory:  toolCategoryReadonly,
		RiskLevel:         "low",
		Readonly:          true,
		AllowedInPlanMode: true,
	}
}

func mutationToolRiskMetadata(name string) toolRiskMetadata {
	plan := verificationPlanForTool(name)
	meta := toolRiskMetadata{
		Name:             name,
		DispatchCategory: toolCategoryMutation,
		RiskLevel:        "high",
		Mutation:         true,
		Dangerous:        true,
		RequiresPlan:     true,
		RequiresApproval: true,
		VerifyPlan:       plan,
		VerifyStrategies: plan.StrategyNames(),
	}
	switch strings.TrimSpace(name) {
	case serviceRestartToolName, serviceStopToolName:
		meta.RollbackSupported = true
	case configApplyToolName, "write_file":
		meta.DryRunSupported = true
		meta.RollbackSupported = true
	case packageInstallToolName, packageUpgradeToolName:
		meta.DryRunSupported = true
	}
	return meta
}

func isReadonlyToolName(name string) bool {
	switch strings.TrimSpace(name) {
	case "query_ai_server_state",
		"readonly_host_inspect",
		"execute_readonly_query",
		"list_remote_files",
		"read_remote_file",
		"search_remote_files",
		"web_search",
		"open_page",
		"find_in_page":
		return true
	default:
		return false
	}
}

func bifrostToolRiskMetadata(toolName string, readonly bool) toolRiskMetadata {
	name := strings.TrimSpace(toolName)
	switch name {
	case "execute_command":
		if readonly {
			return readonlyToolRiskMetadata(name)
		}
		return mutationToolRiskMetadata(name)
	case "write_file":
		return mutationToolRiskMetadata(name)
	default:
		return lookupToolRiskMetadata(name)
	}
}

func (m toolRiskMetadata) schemaMetadata() map[string]any {
	if strings.TrimSpace(m.Name) == "" {
		return nil
	}
	verifyStrategies := append([]string(nil), m.VerifyStrategies...)
	meta := map[string]any{
		"category":             string(m.DispatchCategory),
		"risk_level":           firstNonEmptyValue(strings.TrimSpace(m.RiskLevel), "low"),
		"readonly":             m.Readonly,
		"mutation":             m.Mutation,
		"dangerous":            m.Dangerous,
		"requires_plan":        m.RequiresPlan,
		"requires_approval":    m.RequiresApproval,
		"dry_run_supported":    m.DryRunSupported,
		"rollback_supported":   m.RollbackSupported,
		"verify_strategies":    verifyStrategies,
		"allowed_in_plan_mode": m.AllowedInPlanMode,
	}
	for key, value := range m.VerifyPlan.schemaMetadata() {
		meta[key] = value
	}
	return meta
}

func withToolRiskMetadata(tool map[string]any) map[string]any {
	if len(tool) == 0 {
		return map[string]any{}
	}
	out := cloneAnyMap(tool)
	name := strings.TrimSpace(getStringAny(out, "name"))
	if name == "" {
		return out
	}
	if meta := lookupToolRiskMetadata(name).schemaMetadata(); meta != nil {
		out["riskMetadata"] = meta
	}
	return out
}

func withToolRiskMetadataAll(tools []map[string]any) []map[string]any {
	if len(tools) == 0 {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		out = append(out, withToolRiskMetadata(tool))
	}
	return out
}

func (a *App) validateToolRiskGate(sessionID string, meta toolRiskMetadata) error {
	if a == nil || strings.TrimSpace(meta.Name) == "" {
		return nil
	}
	session := a.store.Session(sessionID)
	if session == nil {
		return nil
	}
	if !a.workspaceTurnPolicyAllowsTool(sessionID, meta.Name) {
		return fmt.Errorf("tool %q is hidden by the current turn policy (lane=%s, intent=%s)", meta.Name, strings.TrimSpace(session.Runtime.TurnPolicy.Lane), strings.TrimSpace(session.Runtime.TurnPolicy.IntentClass))
	}
	if meta.RequiresPlan && a.workspacePlanModeNeedsApproval(sessionID) {
		return fmt.Errorf("%s", toolRiskPlanGateMessage(meta))
	}
	if session.Runtime.PlanMode && !meta.AllowedInPlanMode {
		return fmt.Errorf("tool %q is not allowed in plan mode. Only read-only tools, planning tools, and exit_plan_mode are permitted", meta.Name)
	}
	return nil
}

func toolRiskPlanGateMessage(meta toolRiskMetadata) string {
	switch meta.Name {
	case "request_approval":
		return "计划审批通过前不能请求动作审批"
	case "write_file":
		return "计划审批通过前不能执行文件变更"
	case "execute_system_mutation", "execute_command":
		return "计划审批通过前不能执行变更命令"
	case "orchestrator_dispatch_tasks":
		return "计划审批通过前不能派发执行任务"
	default:
		if meta.RequiresApproval {
			return fmt.Sprintf("tool %q is blocked until the plan is approved. This %s-risk action cannot request approval or execute yet", meta.Name, meta.RiskLevel)
		}
		return fmt.Sprintf("tool %q is blocked until the plan is approved. This %s-risk action cannot execute yet", meta.Name, meta.RiskLevel)
	}
}
