package server

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/agentloop"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func (a *App) syncIncidentStageTransition(sessionID string, previous model.Snapshot, phase string) {
	if a == nil || strings.TrimSpace(sessionID) == "" {
		return
	}

	current := a.snapshot(sessionID)
	prevMode := strings.TrimSpace(previous.CurrentMode)
	prevStage := strings.TrimSpace(previous.CurrentStage)
	nextMode := strings.TrimSpace(current.CurrentMode)
	nextStage := strings.TrimSpace(current.CurrentStage)
	if nextMode == "" && nextStage == "" {
		return
	}
	if prevMode == nextMode && prevStage == nextStage {
		return
	}

	status := "completed"
	if err := agentloop.ValidateIncidentTransition(prevMode, nextMode, prevStage, nextStage); err != nil {
		status = "warning"
		log.Printf("incident transition validation failed session=%s mode=%s->%s stage=%s->%s phase=%s err=%v", sessionID, prevMode, nextMode, prevStage, nextStage, phase, err)
	}

	runID := ""
	iterationID := ""
	if current.AgentLoop != nil {
		runID = strings.TrimSpace(current.AgentLoop.ID)
		iterationID = strings.TrimSpace(current.AgentLoop.ActiveIterationID)
	}
	if iterationID == "" && len(current.AgentLoopIterations) > 0 {
		iterationID = strings.TrimSpace(current.AgentLoopIterations[0].ID)
	}

	summary := "进入阶段 " + nextStage
	if prevStage != "" {
		summary = prevStage + " -> " + nextStage
	}

	a.store.UpsertIncidentEvent(sessionID, model.IncidentEvent{
		ID:          model.NewID("evt"),
		SessionID:   sessionID,
		RunID:       runID,
		IterationID: iterationID,
		Stage:       nextStage,
		Type:        "stage.changed",
		Status:      status,
		Title:       "Stage changed",
		Summary:     summary,
		Metadata: map[string]any{
			"previousMode":  emptyToNil(prevMode),
			"currentMode":   emptyToNil(nextMode),
			"previousStage": emptyToNil(prevStage),
			"currentStage":  emptyToNil(nextStage),
			"phase":         emptyToNil(strings.TrimSpace(phase)),
		},
		CreatedAt: model.NowString(),
	})
}

func (a *App) appendIncidentEvent(sessionID, eventType, status, title, summary string, metadata map[string]any) {
	if a == nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(eventType) == "" {
		return
	}
	current := a.snapshot(sessionID)
	runID := ""
	iterationID := ""
	stage := strings.TrimSpace(current.CurrentStage)
	if current.AgentLoop != nil {
		runID = strings.TrimSpace(current.AgentLoop.ID)
		iterationID = strings.TrimSpace(current.AgentLoop.ActiveIterationID)
	}
	if stage == "" && strings.HasPrefix(strings.TrimSpace(eventType), "cancel.") {
		stage = string(agentloop.IncidentStageCanceled)
	}
	a.store.UpsertIncidentEvent(sessionID, model.IncidentEvent{
		ID:          model.NewID("evt"),
		SessionID:   sessionID,
		RunID:       runID,
		IterationID: iterationID,
		Stage:       stage,
		Type:        strings.TrimSpace(eventType),
		Status:      firstNonEmptyValue(strings.TrimSpace(status), "completed"),
		Title:       strings.TrimSpace(title),
		Summary:     strings.TrimSpace(summary),
		Metadata:    cloneAnyMap(metadata),
		CreatedAt:   model.NowString(),
	})
}

func (a *App) approvalLifecycleMetadata(sessionID string, approval model.ApprovalRequest, fields map[string]any) map[string]any {
	meta := cloneAnyMap(fields)
	toolName := strings.TrimSpace(a.approvalResolvedToolName(sessionID, approval))
	risk := lookupToolRiskMetadata(toolName)
	host := a.findHost(approval.HostID)

	if _, exists := meta["riskLevel"]; !exists {
		if level := strings.TrimSpace(risk.RiskLevel); level != "" {
			meta["riskLevel"] = level
		}
	}
	if _, exists := meta["targetSummary"]; !exists {
		if summary := strings.TrimSpace(a.approvalTargetSummary(sessionID, approval, meta)); summary != "" {
			meta["targetSummary"] = summary
		}
	}
	if _, exists := meta["targetEnvironment"]; !exists {
		if targetEnvironment := strings.TrimSpace(a.approvalTargetEnvironment(sessionID, approval, meta)); targetEnvironment != "" {
			meta["targetEnvironment"] = targetEnvironment
		}
	}
	if _, exists := meta["blastRadius"]; !exists {
		if blastRadius := a.approvalBlastRadius(sessionID, approval, meta); blastRadius != "" {
			meta["blastRadius"] = blastRadius
		}
	}
	if _, exists := meta["dryRunSupported"]; !exists {
		if a.approvalDryRunSupported(sessionID, approval, meta) {
			meta["dryRunSupported"] = true
		}
	}
	if _, exists := meta["dryRunSummary"]; !exists {
		if dryRunSummary := strings.TrimSpace(a.approvalDryRunSummary(sessionID, approval, meta)); dryRunSummary != "" {
			meta["dryRunSummary"] = dryRunSummary
		}
	}
	if _, exists := meta["rollbackHint"]; !exists {
		if rollbackHint := a.approvalRollbackHint(sessionID, approval, meta); rollbackHint != "" {
			meta["rollbackHint"] = rollbackHint
		}
	}
	applyVerificationPlanFields(meta, a.approvalVerificationPlan(sessionID, approval, meta))
	if _, exists := meta["toolName"]; !exists && toolName != "" {
		meta["toolName"] = toolName
	}
	if _, exists := meta["hostName"]; !exists {
		if hostName := strings.TrimSpace(hostNameOrID(host)); hostName != "" {
			meta["hostName"] = hostName
		}
	}
	return meta
}

func (a *App) approvalStoredItem(sessionID string, approval model.ApprovalRequest) map[string]any {
	if a == nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(approval.ItemID) == "" {
		return nil
	}
	return a.store.Item(sessionID, approval.ItemID)
}

func approvalStoredArguments(item map[string]any) map[string]any {
	if len(item) == 0 {
		return nil
	}
	arguments, _ := item["arguments"].(map[string]any)
	if len(arguments) == 0 {
		return nil
	}
	return arguments
}

func (a *App) approvalVerificationPlan(sessionID string, approval model.ApprovalRequest, fields map[string]any) verificationPlanDefinition {
	item := a.approvalStoredItem(sessionID, approval)
	arguments := approvalStoredArguments(item)
	filePath := strings.TrimSpace(getStringAny(fields, "filePath", "path"))
	if filePath == "" {
		filePath = strings.TrimSpace(getStringAny(item, "path", "filePath"))
	}
	if filePath == "" && len(approval.Changes) > 0 {
		filePath = strings.TrimSpace(approval.Changes[0].Path)
	}
	return resolveVerificationPlan(verificationPlanContext{
		ToolName:    strings.TrimSpace(a.approvalResolvedToolName(sessionID, approval)),
		Mode:        strings.TrimSpace(getStringAny(item, "mode", "operationMode")),
		Command:     firstNonEmptyValue(strings.TrimSpace(approval.Command), strings.TrimSpace(getStringAny(item, "command"))),
		FilePath:    filePath,
		ServiceName: strings.TrimSpace(getStringAny(arguments, "service", "serviceName")),
		PackageName: strings.TrimSpace(getStringAny(arguments, "package", "packageName")),
		Detail:      cloneAnyMap(fields),
	})
}

func (a *App) approvalResolvedToolName(sessionID string, approval model.ApprovalRequest) string {
	item := a.approvalStoredItem(sessionID, approval)
	if item != nil {
		if toolName := strings.TrimSpace(getStringAny(item, "tool", "toolName")); toolName != "" {
			return toolName
		}
	}
	return approvalAuditToolName(approval)
}

func (a *App) approvalTargetSummary(sessionID string, approval model.ApprovalRequest, fields map[string]any) string {
	if summary := strings.TrimSpace(getStringAny(fields, "targetSummary", "target_summary", "target")); summary != "" {
		return summary
	}

	host := a.findHost(approval.HostID)
	hostLabel := firstNonEmptyValue(strings.TrimSpace(host.Name), strings.TrimSpace(approval.HostID), "当前主机")
	item := a.approvalStoredItem(sessionID, approval)
	arguments := approvalStoredArguments(item)
	toolName := strings.ReplaceAll(strings.TrimSpace(a.approvalResolvedToolName(sessionID, approval)), ".", "_")

	switch toolName {
	case serviceRestartToolName, serviceStopToolName:
		service := strings.TrimSpace(getStringAny(arguments, "service", "serviceName"))
		if service != "" {
			return fmt.Sprintf("%s / service %s", hostLabel, service)
		}
	case packageInstallToolName, packageUpgradeToolName:
		pkg := strings.TrimSpace(getStringAny(arguments, "package", "packageName"))
		if pkg != "" {
			return fmt.Sprintf("%s / package %s", hostLabel, pkg)
		}
	case configApplyToolName, "write_file":
		path := strings.TrimSpace(getStringAny(arguments, "path", "filePath"))
		if path != "" {
			return fmt.Sprintf("%s / %s", hostLabel, path)
		}
	case "execute_system_mutation":
		if strings.EqualFold(strings.TrimSpace(getStringAny(item, "mode", "operationMode")), "file_change") {
			path := strings.TrimSpace(getStringAny(item, "path", "filePath"))
			if path != "" {
				return fmt.Sprintf("%s / %s", hostLabel, path)
			}
		}
	}

	switch approval.Type {
	case "plan_exit":
		planTitle := strings.TrimSpace(getStringAny(fields, "title", "planTitle"))
		if planTitle == "" {
			planTitle = strings.TrimSpace(approval.Reason)
		}
		if planTitle != "" {
			return "工作台计划 / " + planTitle
		}
	case "remote_file_change", "file_change":
		if len(approval.Changes) == 1 && strings.TrimSpace(approval.Changes[0].Path) != "" {
			return fmt.Sprintf("%s / %s", hostLabel, strings.TrimSpace(approval.Changes[0].Path))
		}
	}

	return approvalRequestSummaryText(host, approval)
}

func (a *App) approvalTargetEnvironment(sessionID string, approval model.ApprovalRequest, fields map[string]any) string {
	for _, value := range []string{
		getStringAny(fields, "targetEnvironment", "target_environment", "environment", "env"),
		getStringAny(a.approvalStoredItem(sessionID, approval), "targetEnvironment", "target_environment", "environment", "env"),
	} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	host := a.findHost(approval.HostID)
	parts := make([]string, 0, 3)
	for _, key := range []string{"environment", "env", "cluster"} {
		if host.Labels == nil {
			continue
		}
		if value := strings.TrimSpace(host.Labels[key]); value != "" {
			parts = append(parts, value)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, " / ")
	}
	if strings.TrimSpace(approval.HostID) == model.ServerLocalHostID {
		return "server-local"
	}
	if kind := strings.TrimSpace(host.Kind); kind != "" {
		return kind
	}
	return ""
}

func (a *App) approvalDryRunSupported(sessionID string, approval model.ApprovalRequest, fields map[string]any) bool {
	if getBool(fields, "dryRunSupported") || getBool(fields, "dry_run_supported") {
		return true
	}
	item := a.approvalStoredItem(sessionID, approval)
	if getBool(item, "dryRunSupported") || getBool(item, "dry_run_supported") {
		return true
	}

	toolName := strings.ReplaceAll(strings.TrimSpace(a.approvalResolvedToolName(sessionID, approval)), ".", "_")
	switch toolName {
	case configApplyToolName, packageInstallToolName, packageUpgradeToolName, "write_file":
		return true
	}
	if toolName == "execute_system_mutation" && strings.EqualFold(strings.TrimSpace(getStringAny(item, "mode", "operationMode")), "file_change") {
		return true
	}
	return approval.Type == "remote_file_change" || approval.Type == "file_change"
}

func (a *App) approvalDryRunSummary(sessionID string, approval model.ApprovalRequest, fields map[string]any) string {
	for _, value := range []string{
		getStringAny(fields, "dryRunSummary", "dry_run_summary"),
		getStringAny(a.approvalStoredItem(sessionID, approval), "dryRunSummary", "dry_run_summary"),
	} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	item := a.approvalStoredItem(sessionID, approval)
	arguments := approvalStoredArguments(item)
	toolName := strings.ReplaceAll(strings.TrimSpace(a.approvalResolvedToolName(sessionID, approval)), ".", "_")
	diff := strings.TrimSpace(getStringAny(fields, "diff"))
	if diff == "" {
		diff = strings.TrimSpace(getStringAny(item, "diff"))
	}
	if diff == "" && len(approval.Changes) > 0 {
		diff = strings.TrimSpace(approval.Changes[0].Diff)
	}
	if diff != "" && (approval.Type == "remote_file_change" || approval.Type == "file_change" || toolName == configApplyToolName || toolName == "write_file") {
		return truncate(diff, 600)
	}

	pkg := strings.TrimSpace(getStringAny(arguments, "package", "packageName"))
	switch toolName {
	case packageInstallToolName:
		if pkg != "" {
			return fmt.Sprintf("建议先用包管理器模拟安装：apt-get -s install %s，或 yum/dnf --assumeno install %s。", pkg, pkg)
		}
	case packageUpgradeToolName:
		if pkg != "" {
			return fmt.Sprintf("建议先用包管理器模拟升级：apt-get -s install --only-upgrade %s，或 yum/dnf --assumeno upgrade %s。", pkg, pkg)
		}
	}
	return ""
}

func (a *App) approvalBlastRadius(sessionID string, approval model.ApprovalRequest, fields map[string]any) string {
	if blastRadius := strings.TrimSpace(getStringAny(fields, "blastRadius", "blast_radius")); blastRadius != "" {
		return blastRadius
	}
	if expectedImpact := strings.TrimSpace(getStringAny(fields, "expectedImpact", "expected_impact")); expectedImpact != "" {
		return expectedImpact
	}

	host := a.findHost(approval.HostID)
	hostLabel := firstNonEmptyValue(strings.TrimSpace(host.Name), strings.TrimSpace(approval.HostID), "当前主机")
	switch approval.Type {
	case "plan_exit":
		return "工作台执行模式切换与后续计划步骤"
	case "remote_file_change", "file_change":
		if len(approval.Changes) == 1 {
			dir := strings.TrimSpace(filepath.Dir(approval.Changes[0].Path))
			if dir != "" && dir != "." {
				return hostLabel + " / " + dir
			}
		}
		if len(approval.Changes) > 1 {
			return hostLabel + " / 多文件变更"
		}
		return hostLabel + " / 文件变更"
	case "remote_command", "command", "mutation":
		return hostLabel
	default:
		if hostLabel != "" {
			return hostLabel
		}
		return ""
	}
}

func (a *App) approvalRollbackHint(sessionID string, approval model.ApprovalRequest, fields map[string]any) string {
	for _, value := range []string{
		getStringAny(fields, "rollbackHint", "rollback_hint"),
		getStringAny(fields, "rollbackSuggestion", "rollback_suggestion"),
		getStringAny(fields, "rollback"),
		a.approvalCardDetailValue(sessionID, approval, "rollback", "rollbackSuggestion", "rollback_hint"),
	} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	switch approval.Type {
	case "remote_file_change", "file_change":
		if len(approval.Changes) > 0 {
			return "回滚到变更前文件内容，并重新执行配置验证。"
		}
		return "撤销文件修改并恢复变更前版本。"
	case "remote_command", "command", "mutation":
		return "执行前确认对应回退命令；失败时恢复变更前服务或配置状态。"
	case "plan_exit":
		return "拒绝计划审批后继续保持分析模式，不进入执行态。"
	default:
		return ""
	}
}

func (a *App) approvalCardDetailValue(sessionID string, approval model.ApprovalRequest, keys ...string) string {
	if a == nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(approval.ItemID) == "" {
		return ""
	}
	card := a.cardByID(sessionID, approval.ItemID)
	if card == nil || len(card.Detail) == 0 {
		return ""
	}
	return strings.TrimSpace(getStringAny(card.Detail, keys...))
}

func (a *App) approvalCardDetail(sessionID string, approval model.ApprovalRequest, fields map[string]any) map[string]any {
	detail := a.approvalLifecycleMetadata(sessionID, approval, fields)
	detail["approvalId"] = approval.ID
	detail["approvalType"] = approval.Type
	if command := strings.TrimSpace(approval.Command); command != "" {
		detail["command"] = command
	}
	if cwd := strings.TrimSpace(approval.Cwd); cwd != "" {
		detail["cwd"] = cwd
	}
	if reason := strings.TrimSpace(approval.Reason); reason != "" {
		detail["reason"] = reason
	}
	if hostID := strings.TrimSpace(approval.HostID); hostID != "" {
		detail["hostId"] = hostID
	}
	if len(approval.Changes) > 0 {
		detail["changes"] = append([]model.FileChange(nil), approval.Changes...)
	}
	return detail
}

func (a *App) recordApprovalIncidentEvent(sessionID, eventType string, approval model.ApprovalRequest, approvalDecision, status, startedAt, endedAt string, fields map[string]any) {
	if a == nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(eventType) == "" || strings.TrimSpace(approval.ID) == "" {
		return
	}

	baseMetadata := a.approvalLifecycleMetadata(sessionID, approval, fields)
	baseMetadata["approvalType"] = strings.TrimSpace(approval.Type)
	if cardID := strings.TrimSpace(approval.ItemID); cardID != "" {
		baseMetadata["approvalCardId"] = cardID
		if approval.Type == "remote_command" || approval.Type == "command" || approval.Type == "remote_file_change" || approval.Type == "file_change" {
			baseMetadata["executionCardId"] = cardID
		}
	}
	if decision := strings.TrimSpace(approvalDecision); decision != "" {
		baseMetadata["approvalDecision"] = decision
	}
	if approvalStatus := strings.TrimSpace(status); approvalStatus != "" {
		baseMetadata["approvalStatus"] = approvalStatus
	}
	if started := strings.TrimSpace(startedAt); started != "" {
		baseMetadata["startedAt"] = started
	}
	if ended := strings.TrimSpace(endedAt); ended != "" {
		baseMetadata["endedAt"] = ended
	}
	if strings.TrimSpace(status) != "" && strings.Contains(strings.TrimSpace(status), "_auto") && strings.TrimSpace(approval.ItemID) != "" {
		baseMetadata["resolutionCardId"] = "auto-approval-" + strings.TrimSpace(approval.ItemID)
	}
	if eventType == "approval.decision" && (approval.Type == "remote_command" || approval.Type == "command" || approval.Type == "remote_file_change" || approval.Type == "file_change") {
		baseMetadata["resolutionCardId"] = "approval-memo-" + approval.ID
	}

	eventID := "evt-" + strings.ReplaceAll(strings.TrimSpace(eventType), ".", "-") + "-" + approval.ID
	createdAt := firstNonEmptyValue(strings.TrimSpace(endedAt), strings.TrimSpace(startedAt), model.NowString())

	targetSessionIDs := []string{sessionID}
	if workspaceSessionID := strings.TrimSpace(a.sessionMeta(sessionID).WorkspaceSessionID); workspaceSessionID != "" && workspaceSessionID != sessionID {
		targetSessionIDs = append(targetSessionIDs, workspaceSessionID)
	}

	for _, targetSessionID := range targetSessionIDs {
		current := a.snapshot(targetSessionID)
		metadata := cloneAnyMap(baseMetadata)
		if targetSessionID != sessionID {
			metadata["sourceSessionId"] = sessionID
		}

		runID := ""
		iterationID := ""
		stage := strings.TrimSpace(current.CurrentStage)
		if current.AgentLoop != nil {
			runID = strings.TrimSpace(current.AgentLoop.ID)
			iterationID = strings.TrimSpace(current.AgentLoop.ActiveIterationID)
		}

		a.store.UpsertIncidentEvent(targetSessionID, model.IncidentEvent{
			ID:          eventID,
			SessionID:   targetSessionID,
			RunID:       runID,
			IterationID: iterationID,
			Stage:       stage,
			Type:        strings.TrimSpace(eventType),
			Status:      approvalIncidentEventStatus(eventType, approvalDecision, status),
			Title:       approvalIncidentEventTitle(eventType, approvalDecision, status),
			Summary:     approvalIncidentEventSummary(a.findHost(approval.HostID), approval, approvalDecision, status),
			HostID:      defaultHostID(strings.TrimSpace(approval.HostID)),
			ToolName:    strings.TrimSpace(a.approvalResolvedToolName(sessionID, approval)),
			ApprovalID:  approval.ID,
			Metadata:    metadata,
			CreatedAt:   createdAt,
		})
	}
}

func approvalIncidentEventStatus(eventType, approvalDecision, approvalStatus string) string {
	switch strings.TrimSpace(eventType) {
	case "approval.requested":
		return "pending"
	case "approval.auto_accepted":
		return "completed"
	case "approval.decision":
		if approvalDecisionForStatus(approvalStatus) == "decline" || strings.TrimSpace(approvalDecision) == "decline" {
			return "warning"
		}
		return "completed"
	default:
		return "completed"
	}
}

func approvalIncidentEventTitle(eventType, approvalDecision, approvalStatus string) string {
	switch strings.TrimSpace(eventType) {
	case "approval.requested":
		return "Approval requested"
	case "approval.auto_accepted":
		return "Approval auto-accepted"
	case "approval.decision":
		if approvalDecisionForStatus(approvalStatus) == "decline" || strings.TrimSpace(approvalDecision) == "decline" {
			return "Approval declined"
		}
		if strings.TrimSpace(approvalDecision) == "accept_session" {
			return "Approval accepted for session"
		}
		return "Approval accepted"
	default:
		return "Approval updated"
	}
}

func approvalIncidentEventSummary(host model.Host, approval model.ApprovalRequest, approvalDecision, approvalStatus string) string {
	switch strings.TrimSpace(approvalStatus) {
	case "accepted_for_session_auto":
		return autoApprovalNoticeText(approval)
	case "accepted_for_host_auto":
		return hostGrantAutoApprovalNoticeText(approval)
	case "accepted_by_policy_auto":
		return "当前 main-agent profile 允许该操作直接执行，因此已自动放行。"
	}

	switch strings.TrimSpace(approvalDecision) {
	case "":
		return approvalRequestSummaryText(host, approval)
	case "decline", "accept", "accept_session", "auto_accept":
		return approvalMemoText(host, approval, approvalDecision)
	default:
		return approvalRequestSummaryText(host, approval)
	}
}
