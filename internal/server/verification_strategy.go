package server

import (
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

const (
	verificationSourceCorootHealth = "coroot_health"
	verificationSourceMetricCheck  = "metric_check"
	verificationSourceHealthProbe  = "health_probe"
	verificationSourceLogCheck     = "log_check"
	verificationSourceAlertState   = "alert_state"
	verificationSourceWorkerStatus = "worker_status"
	verificationSourceMissionState = "mission_summary"
)

const (
	verificationPlanServiceLifecycle = "service_lifecycle"
	verificationPlanConfigApply      = "config_apply"
	verificationPlanRolloutRestart   = "rollout_restart"
	verificationPlanRolloutRollback  = "rollout_rollback"
	verificationPlanScaleChange      = "scale_change"
	verificationPlanPackageChange    = "package_change"
	verificationPlanGenericMutation  = "generic_mutation"
	verificationPlanWorkerDispatch   = "worker_dispatch"
)

type verificationStrategyDefinition struct {
	Name            string
	Label           string
	SourcePriority  []string
	SuccessCriteria []string
}

type verificationPlanDefinition struct {
	Key        string
	Label      string
	Strategies []verificationStrategyDefinition
}

type verificationPlanContext struct {
	ToolName    string
	CardType    string
	Mode        string
	Command     string
	FilePath    string
	ServiceName string
	PackageName string
	Detail      map[string]any
}

func (p verificationPlanDefinition) StrategyNames() []string {
	if len(p.Strategies) == 0 {
		return nil
	}
	names := make([]string, 0, len(p.Strategies))
	for _, strategy := range p.Strategies {
		if name := strings.TrimSpace(strategy.Name); name != "" {
			names = append(names, name)
		}
	}
	return dedupeCompactStrings(names)
}

func (p verificationPlanDefinition) SourcePriority() []string {
	if len(p.Strategies) == 0 {
		return nil
	}
	out := make([]string, 0, len(p.Strategies)*2)
	for _, strategy := range p.Strategies {
		out = append(out, strategy.SourcePriority...)
	}
	return dedupeCompactStrings(out)
}

func (p verificationPlanDefinition) SuccessCriteria() []string {
	if len(p.Strategies) == 0 {
		return nil
	}
	out := make([]string, 0, len(p.Strategies)*2)
	for _, strategy := range p.Strategies {
		out = append(out, strategy.SuccessCriteria...)
	}
	return dedupeCompactStrings(out)
}

func (p verificationPlanDefinition) PrimaryStrategy() string {
	for _, strategy := range p.Strategies {
		if name := strings.TrimSpace(strategy.Name); name != "" {
			return name
		}
	}
	return ""
}

func (p verificationPlanDefinition) StrategyDetails() []map[string]any {
	if len(p.Strategies) == 0 {
		return nil
	}
	details := make([]map[string]any, 0, len(p.Strategies))
	for _, strategy := range p.Strategies {
		name := strings.TrimSpace(strategy.Name)
		if name == "" {
			continue
		}
		detail := map[string]any{
			"name":             name,
			"source_priority":  append([]string(nil), dedupeCompactStrings(strategy.SourcePriority)...),
			"success_criteria": append([]string(nil), dedupeCompactStrings(strategy.SuccessCriteria)...),
		}
		if label := strings.TrimSpace(strategy.Label); label != "" {
			detail["label"] = label
		}
		details = append(details, detail)
	}
	if len(details) == 0 {
		return nil
	}
	return details
}

func (p verificationPlanDefinition) schemaMetadata() map[string]any {
	return map[string]any{
		"verification_plan_key": firstNonEmptyValue(strings.TrimSpace(p.Key), ""),
		"verification_sources":  append([]string(nil), p.SourcePriority()...),
		"verify_strategy_details": func() []map[string]any {
			details := p.StrategyDetails()
			if len(details) == 0 {
				return []map[string]any{}
			}
			return details
		}(),
	}
}

func applyVerificationPlanFields(target map[string]any, plan verificationPlanDefinition) {
	if target == nil || len(plan.Strategies) == 0 {
		return
	}
	if _, exists := target["verifyStrategies"]; !exists {
		target["verifyStrategies"] = append([]string(nil), plan.StrategyNames()...)
	}
	if _, exists := target["verificationSources"]; !exists {
		target["verificationSources"] = append([]string(nil), plan.SourcePriority()...)
	}
	if _, exists := target["verifyStrategyDetails"]; !exists {
		target["verifyStrategyDetails"] = plan.StrategyDetails()
	}
	if _, exists := target["verificationPlanKey"]; !exists && strings.TrimSpace(plan.Key) != "" {
		target["verificationPlanKey"] = strings.TrimSpace(plan.Key)
	}
	if _, exists := target["verificationPlanLabel"]; !exists && strings.TrimSpace(plan.Label) != "" {
		target["verificationPlanLabel"] = strings.TrimSpace(plan.Label)
	}
	if _, exists := target["verificationSuccessCriteria"]; !exists {
		target["verificationSuccessCriteria"] = append([]string(nil), plan.SuccessCriteria()...)
	}
}

func verificationPlanFromFields(fields map[string]any) verificationPlanDefinition {
	if len(fields) == 0 {
		return verificationPlanDefinition{}
	}
	if strategies := verificationStrategyDefinitions(fields["verifyStrategyDetails"]); len(strategies) > 0 {
		return verificationPlanDefinition{
			Key:        strings.TrimSpace(getStringAny(fields, "verificationPlanKey", "verification_plan_key")),
			Label:      strings.TrimSpace(getStringAny(fields, "verificationPlanLabel", "verification_plan_label")),
			Strategies: strategies,
		}
	}
	if strategies := verificationStrategyDefinitions(fields["verify_strategy_details"]); len(strategies) > 0 {
		return verificationPlanDefinition{
			Key:        strings.TrimSpace(getStringAny(fields, "verificationPlanKey", "verification_plan_key")),
			Label:      strings.TrimSpace(getStringAny(fields, "verificationPlanLabel", "verification_plan_label")),
			Strategies: strategies,
		}
	}

	names := verificationStringSlice(fields["verifyStrategies"])
	if len(names) == 0 {
		names = verificationStringSlice(fields["verify_strategies"])
	}
	if len(names) == 0 {
		return verificationPlanDefinition{}
	}

	sources := verificationStringSlice(fields["verificationSources"])
	if len(sources) == 0 {
		sources = verificationStringSlice(fields["verification_sources"])
	}
	successCriteria := verificationStringSlice(fields["verificationSuccessCriteria"])
	if len(successCriteria) == 0 {
		successCriteria = verificationStringSlice(fields["verification_success_criteria"])
	}
	if len(successCriteria) == 0 {
		successCriteria = verificationStringSlice(fields["successCriteria"])
	}
	if len(successCriteria) == 0 {
		successCriteria = verificationStringSlice(fields["success_criteria"])
	}

	strategies := make([]verificationStrategyDefinition, 0, len(names))
	for idx, name := range names {
		definition := verificationStrategyDefinition{Name: name}
		if idx == 0 {
			definition.SourcePriority = append([]string(nil), sources...)
			definition.SuccessCriteria = append([]string(nil), successCriteria...)
		}
		strategies = append(strategies, definition)
	}
	return verificationPlanDefinition{
		Key:        strings.TrimSpace(getStringAny(fields, "verificationPlanKey", "verification_plan_key")),
		Label:      strings.TrimSpace(getStringAny(fields, "verificationPlanLabel", "verification_plan_label")),
		Strategies: strategies,
	}
}

func verificationStrategyDefinitions(raw any) []verificationStrategyDefinition {
	switch value := raw.(type) {
	case []map[string]any:
		out := make([]verificationStrategyDefinition, 0, len(value))
		for _, entry := range value {
			if definition := verificationStrategyDefinitionFromMap(entry); strings.TrimSpace(definition.Name) != "" {
				out = append(out, definition)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]verificationStrategyDefinition, 0, len(value))
		for _, entry := range value {
			item, _ := entry.(map[string]any)
			if definition := verificationStrategyDefinitionFromMap(item); strings.TrimSpace(definition.Name) != "" {
				out = append(out, definition)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func verificationStrategyDefinitionFromMap(fields map[string]any) verificationStrategyDefinition {
	if len(fields) == 0 {
		return verificationStrategyDefinition{}
	}
	sources := verificationStringSlice(fields["sourcePriority"])
	if len(sources) == 0 {
		sources = verificationStringSlice(fields["source_priority"])
	}
	if len(sources) == 0 {
		sources = verificationStringSlice(fields["verificationSources"])
	}
	if len(sources) == 0 {
		sources = verificationStringSlice(fields["verification_sources"])
	}
	if len(sources) == 0 {
		sources = verificationStringSlice(fields["sources"])
	}
	criteria := verificationStringSlice(fields["successCriteria"])
	if len(criteria) == 0 {
		criteria = verificationStringSlice(fields["success_criteria"])
	}
	if len(criteria) == 0 {
		criteria = verificationStringSlice(fields["criteria"])
	}
	return verificationStrategyDefinition{
		Name:            strings.TrimSpace(getStringAny(fields, "name", "id", "key")),
		Label:           strings.TrimSpace(getStringAny(fields, "label", "title")),
		SourcePriority:  append([]string(nil), sources...),
		SuccessCriteria: append([]string(nil), criteria...),
	}
}

func verificationPlanForTool(toolName string) verificationPlanDefinition {
	switch strings.TrimSpace(toolName) {
	case serviceRestartToolName, serviceStopToolName:
		return verificationPlanByKey(verificationPlanServiceLifecycle)
	case configApplyToolName, "write_file":
		return verificationPlanByKey(verificationPlanConfigApply)
	case packageInstallToolName, packageUpgradeToolName:
		return verificationPlanByKey(verificationPlanPackageChange)
	case "execute_system_mutation", "execute_command":
		return verificationPlanByKey(verificationPlanGenericMutation)
	case "orchestrator_dispatch_tasks":
		return verificationPlanByKey(verificationPlanWorkerDispatch)
	default:
		return verificationPlanDefinition{}
	}
}

func actionVerificationPlan(card model.Card) verificationPlanDefinition {
	filePath := strings.TrimSpace(getStringAny(card.Detail, "filePath", "path"))
	if filePath == "" && card.Type == "FileChangeCard" && len(card.Changes) > 0 {
		filePath = strings.TrimSpace(card.Changes[0].Path)
	}
	return resolveVerificationPlan(verificationPlanContext{
		ToolName:    actionVerificationToolName(card),
		CardType:    strings.TrimSpace(card.Type),
		Mode:        strings.TrimSpace(getStringAny(card.Detail, "mode", "operationMode")),
		Command:     firstNonEmptyValue(strings.TrimSpace(card.Command), strings.TrimSpace(getStringAny(card.Detail, "command"))),
		FilePath:    filePath,
		ServiceName: strings.TrimSpace(getStringAny(card.Detail, "service", "serviceName")),
		PackageName: strings.TrimSpace(getStringAny(card.Detail, "package", "packageName")),
		Detail:      cloneAnyMap(card.Detail),
	})
}

func resolveVerificationPlan(ctx verificationPlanContext) verificationPlanDefinition {
	if plan := verificationPlanFromFields(ctx.Detail); len(plan.Strategies) > 0 {
		return plan
	}
	if key := classifyVerificationPlan(ctx); key != "" {
		return verificationPlanByKey(key)
	}
	if plan := verificationPlanForTool(ctx.ToolName); len(plan.Strategies) > 0 {
		return plan
	}
	return verificationPlanDefinition{}
}

func classifyVerificationPlan(ctx verificationPlanContext) string {
	if strings.TrimSpace(ctx.FilePath) != "" || strings.EqualFold(strings.TrimSpace(ctx.Mode), "file_change") || strings.TrimSpace(ctx.CardType) == "FileChangeCard" {
		return verificationPlanConfigApply
	}
	if strings.TrimSpace(ctx.ServiceName) != "" {
		return verificationPlanServiceLifecycle
	}
	if key := verificationPlanFromCommand(ctx.Command); key != "" {
		return key
	}
	if strings.TrimSpace(ctx.PackageName) != "" {
		return verificationPlanPackageChange
	}
	switch strings.TrimSpace(ctx.ToolName) {
	case serviceRestartToolName, serviceStopToolName:
		return verificationPlanServiceLifecycle
	case configApplyToolName, "write_file":
		return verificationPlanConfigApply
	case packageInstallToolName, packageUpgradeToolName:
		return verificationPlanPackageChange
	case "execute_system_mutation", "execute_command":
		return verificationPlanGenericMutation
	case "orchestrator_dispatch_tasks":
		return verificationPlanWorkerDispatch
	default:
		if strings.TrimSpace(ctx.CardType) == "CommandCard" {
			return verificationPlanGenericMutation
		}
		return ""
	}
}

func verificationPlanFromCommand(command string) string {
	lower := strings.ToLower(strings.TrimSpace(command))
	if lower == "" {
		return ""
	}
	switch {
	case strings.Contains(lower, "kubectl rollout undo"), strings.Contains(lower, "helm rollback"):
		return verificationPlanRolloutRollback
	case strings.Contains(lower, "kubectl rollout restart"), strings.Contains(lower, "rollout restart"):
		return verificationPlanRolloutRestart
	case strings.Contains(lower, "kubectl scale "), strings.Contains(lower, " scale deployment "), strings.Contains(lower, " scale deploy "), strings.Contains(lower, " scale statefulset "), strings.Contains(lower, " scale sts "), strings.Contains(lower, " scale replicaset "), strings.Contains(lower, " scale rs "):
		return verificationPlanScaleChange
	case strings.Contains(lower, "systemctl restart"), strings.Contains(lower, "systemctl reload"), strings.Contains(lower, " service ") && (strings.Contains(lower, " restart") || strings.Contains(lower, " reload")):
		return verificationPlanServiceLifecycle
	default:
		return ""
	}
}

func verificationPlanByKey(key string) verificationPlanDefinition {
	switch strings.TrimSpace(key) {
	case verificationPlanServiceLifecycle:
		return verificationPlanDefinition{
			Key:   verificationPlanServiceLifecycle,
			Label: "服务重启验证",
			Strategies: []verificationStrategyDefinition{
				{
					Name:            "service_health",
					Label:           "服务健康",
					SourcePriority:  []string{verificationSourceCorootHealth, verificationSourceMetricCheck, verificationSourceAlertState},
					SuccessCriteria: []string{"Coroot 健康状态恢复正常", "关键错误率/延迟指标未继续恶化"},
				},
				{
					Name:            "health_probe",
					Label:           "健康探针",
					SourcePriority:  []string{verificationSourceHealthProbe},
					SuccessCriteria: []string{"关键健康探针连续通过"},
				},
				{
					Name:            "log_check",
					Label:           "关键日志",
					SourcePriority:  []string{verificationSourceLogCheck},
					SuccessCriteria: []string{"关键错误日志不再持续新增"},
				},
			},
		}
	case verificationPlanConfigApply:
		return verificationPlanDefinition{
			Key:   verificationPlanConfigApply,
			Label: "配置变更验证",
			Strategies: []verificationStrategyDefinition{
				{
					Name:            "config_validation",
					Label:           "配置校验",
					SuccessCriteria: []string{"配置语法或渲染校验通过"},
				},
				{
					Name:            "service_health",
					Label:           "服务健康",
					SourcePriority:  []string{verificationSourceCorootHealth, verificationSourceMetricCheck, verificationSourceAlertState},
					SuccessCriteria: []string{"变更后关键错误率/延迟未恶化"},
				},
				{
					Name:            "health_probe",
					Label:           "健康探针",
					SourcePriority:  []string{verificationSourceHealthProbe},
					SuccessCriteria: []string{"相关服务健康探针恢复通过"},
				},
				{
					Name:            "log_check",
					Label:           "关键日志",
					SourcePriority:  []string{verificationSourceLogCheck},
					SuccessCriteria: []string{"关键错误日志无持续新增"},
				},
			},
		}
	case verificationPlanRolloutRestart:
		return verificationPlanDefinition{
			Key:   verificationPlanRolloutRestart,
			Label: "Rollout 验证",
			Strategies: []verificationStrategyDefinition{
				{
					Name:            "rollout_status",
					Label:           "Rollout 状态",
					SourcePriority:  []string{verificationSourceCorootHealth, verificationSourceMetricCheck, verificationSourceAlertState},
					SuccessCriteria: []string{"rollout 状态达到 completed", "新副本错误率/延迟未继续恶化"},
				},
				{
					Name:            "health_probe",
					Label:           "健康探针",
					SourcePriority:  []string{verificationSourceHealthProbe},
					SuccessCriteria: []string{"新副本健康探针连续通过"},
				},
				{
					Name:            "log_check",
					Label:           "关键日志",
					SourcePriority:  []string{verificationSourceLogCheck},
					SuccessCriteria: []string{"关键错误日志未持续放大"},
				},
			},
		}
	case verificationPlanRolloutRollback:
		return verificationPlanDefinition{
			Key:   verificationPlanRolloutRollback,
			Label: "Rollback 验证",
			Strategies: []verificationStrategyDefinition{
				{
					Name:            "rollback_stability",
					Label:           "回滚稳定性",
					SourcePriority:  []string{verificationSourceCorootHealth, verificationSourceMetricCheck, verificationSourceAlertState},
					SuccessCriteria: []string{"回滚后的 workload 已稳定", "关键错误率/延迟恢复到基线"},
				},
				{
					Name:            "health_probe",
					Label:           "健康探针",
					SourcePriority:  []string{verificationSourceHealthProbe},
					SuccessCriteria: []string{"回滚后的实例健康探针连续通过"},
				},
				{
					Name:            "log_check",
					Label:           "关键日志",
					SourcePriority:  []string{verificationSourceLogCheck},
					SuccessCriteria: []string{"回滚后关键错误日志不再新增"},
				},
			},
		}
	case verificationPlanScaleChange:
		return verificationPlanDefinition{
			Key:   verificationPlanScaleChange,
			Label: "扩缩容验证",
			Strategies: []verificationStrategyDefinition{
				{
					Name:            "capacity_recovery",
					Label:           "容量恢复",
					SourcePriority:  []string{verificationSourceCorootHealth, verificationSourceMetricCheck, verificationSourceAlertState},
					SuccessCriteria: []string{"目标副本或容量达到预期", "关键延迟/错误率未继续恶化"},
				},
				{
					Name:            "health_probe",
					Label:           "健康探针",
					SourcePriority:  []string{verificationSourceHealthProbe},
					SuccessCriteria: []string{"扩缩容后的实例健康探针连续通过"},
				},
				{
					Name:            "log_check",
					Label:           "关键日志",
					SourcePriority:  []string{verificationSourceLogCheck},
					SuccessCriteria: []string{"扩缩容后关键错误日志未持续放大"},
				},
			},
		}
	case verificationPlanPackageChange:
		return verificationPlanDefinition{
			Key:   verificationPlanPackageChange,
			Label: "软件包变更验证",
			Strategies: []verificationStrategyDefinition{
				{
					Name:            "package_version",
					Label:           "版本确认",
					SuccessCriteria: []string{"目标软件包版本已安装到预期版本"},
				},
				{
					Name:            "service_health",
					Label:           "服务健康",
					SourcePriority:  []string{verificationSourceCorootHealth, verificationSourceMetricCheck, verificationSourceAlertState},
					SuccessCriteria: []string{"变更后服务健康状态未恶化"},
				},
				{
					Name:            "health_probe",
					Label:           "健康探针",
					SourcePriority:  []string{verificationSourceHealthProbe},
					SuccessCriteria: []string{"关键健康探针连续通过"},
				},
				{
					Name:            "log_check",
					Label:           "关键日志",
					SourcePriority:  []string{verificationSourceLogCheck},
					SuccessCriteria: []string{"关键错误日志未持续放大"},
				},
			},
		}
	case verificationPlanWorkerDispatch:
		return verificationPlanDefinition{
			Key:   verificationPlanWorkerDispatch,
			Label: "任务派发验证",
			Strategies: []verificationStrategyDefinition{
				{
					Name:            "worker_status",
					Label:           "Worker 状态",
					SourcePriority:  []string{verificationSourceWorkerStatus},
					SuccessCriteria: []string{"worker 状态进入 running 或 completed"},
				},
				{
					Name:            "mission_summary",
					Label:           "任务摘要",
					SourcePriority:  []string{verificationSourceMissionState},
					SuccessCriteria: []string{"mission 摘要能确认任务被成功接管"},
				},
			},
		}
	case verificationPlanGenericMutation:
		return verificationPlanDefinition{
			Key:   verificationPlanGenericMutation,
			Label: "通用变更验证",
			Strategies: []verificationStrategyDefinition{
				{
					Name:            "post_action_verification",
					Label:           "动作后验证",
					SourcePriority:  []string{verificationSourceCorootHealth, verificationSourceMetricCheck, verificationSourceAlertState},
					SuccessCriteria: []string{"关键健康状态未继续恶化", "关键错误率/延迟指标稳定"},
				},
				{
					Name:            "health_probe",
					Label:           "健康探针",
					SourcePriority:  []string{verificationSourceHealthProbe},
					SuccessCriteria: []string{"关键健康探针连续通过"},
				},
				{
					Name:            "log_check",
					Label:           "关键日志",
					SourcePriority:  []string{verificationSourceLogCheck},
					SuccessCriteria: []string{"关键错误日志未持续放大"},
				},
			},
		}
	default:
		return verificationPlanDefinition{}
	}
}
