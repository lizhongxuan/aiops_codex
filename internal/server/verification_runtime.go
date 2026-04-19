package server

import (
	"fmt"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func (a *App) syncActionVerification(sessionID string, card model.Card) {
	if a == nil || strings.TrimSpace(sessionID) == "" || !shouldTrackActionVerification(card) {
		return
	}
	finalStatus := normalizeCardStatus(card.Status)
	if finalStatus == "" || finalStatus == "pending" || finalStatus == "inProgress" {
		return
	}

	now := firstNonEmptyValue(strings.TrimSpace(card.UpdatedAt), model.NowString())
	recordID := actionVerificationRecordID(card.ID)
	plan := actionVerificationPlan(card)
	metadata := actionVerificationMetadata(card, plan)
	runID := ""
	if snapshot := a.snapshot(sessionID); snapshot.AgentLoop != nil {
		runID = strings.TrimSpace(snapshot.AgentLoop.ID)
	}

	running := model.VerificationRecord{
		ID:              recordID,
		RunID:           runID,
		ActionEventID:   strings.TrimSpace(card.ID),
		Status:          "running",
		Strategy:        actionVerificationStrategy(plan),
		SuccessCriteria: append([]string(nil), plan.SuccessCriteria()...),
		EvidenceIDs:     actionVerificationEvidenceIDs(card),
		RollbackHint:    strings.TrimSpace(getStringAny(card.Detail, "rollbackHint", "rollback_hint", "rollbackSuggestion", "rollback_suggestion", "rollback")),
		Metadata:        cloneAnyMap(metadata),
		CreatedAt:       now,
	}
	a.store.UpsertVerificationRecord(sessionID, running)
	a.store.UpsertIncidentEvent(sessionID, model.IncidentEvent{
		ID:           "evt-verification-started-" + strings.TrimSpace(card.ID),
		SessionID:    sessionID,
		Stage:        "verifying",
		Type:         "verification.started",
		Status:       "pending",
		Title:        "Verification started",
		Summary:      actionVerificationSummary(card, "running"),
		HostID:       defaultHostID(strings.TrimSpace(card.HostID)),
		ToolName:     actionVerificationToolName(card),
		Verification: recordID,
		Metadata:     cloneAnyMap(metadata),
		CreatedAt:    now,
	})

	finalRecord := running
	finalRecord.Status = actionVerificationFinalStatus(finalStatus)
	finalRecord.Findings = actionVerificationFindings(card, finalRecord.Status)
	finalRecord.Metadata = cloneAnyMap(metadata)
	finalRecord.RollbackHint = actionVerificationRollbackHint(card, finalRecord.Status)
	finalRecord.Metadata["verificationStartedAt"] = now
	finalRecord.Metadata["verificationCompletedAt"] = now
	finalRecord.Metadata["finalCardStatus"] = finalStatus
	if suggestion := actionVerificationNextStepSuggestion(card, finalRecord.Status, finalRecord.RollbackHint); suggestion != "" {
		finalRecord.Metadata["nextStepSuggestion"] = suggestion
	}
	finalRecord.Metadata["verificationCardId"] = actionVerificationCardID(card.ID)
	if finalRecord.Status != "passed" {
		finalRecord.Metadata["rollbackCardId"] = actionRollbackCardID(card.ID)
	}
	a.store.UpsertVerificationRecord(sessionID, finalRecord)
	a.upsertActionVerificationCards(sessionID, card, finalRecord, now)

	eventType := "verification.passed"
	eventStatus := "completed"
	eventTitle := "Verification passed"
	if finalRecord.Status != "passed" {
		eventType = "verification.failed"
		eventStatus = "warning"
		eventTitle = "Verification failed"
	}
	a.store.UpsertIncidentEvent(sessionID, model.IncidentEvent{
		ID:           "evt-" + strings.ReplaceAll(eventType, ".", "-") + "-" + strings.TrimSpace(card.ID),
		SessionID:    sessionID,
		Stage:        "verifying",
		Type:         eventType,
		Status:       eventStatus,
		Title:        eventTitle,
		Summary:      actionVerificationSummary(card, finalRecord.Status),
		HostID:       defaultHostID(strings.TrimSpace(card.HostID)),
		ToolName:     actionVerificationToolName(card),
		Verification: recordID,
		Metadata:     cloneAnyMap(finalRecord.Metadata),
		CreatedAt:    now,
	})
}

func (a *App) upsertActionVerificationCards(sessionID string, card model.Card, record model.VerificationRecord, now string) {
	if a == nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(record.ID) == "" || strings.TrimSpace(card.ID) == "" {
		return
	}

	target := actionVerificationTargetSummary(card)
	verificationDetail := map[string]any{
		"verificationId":     strings.TrimSpace(record.ID),
		"actionCardId":       strings.TrimSpace(card.ID),
		"verificationStatus": strings.TrimSpace(record.Status),
		"targetSummary":      target,
		"strategy":           strings.TrimSpace(record.Strategy),
		"successCriteria":    append([]string(nil), record.SuccessCriteria...),
		"findings":           append([]string(nil), record.Findings...),
		"rollbackHint":       strings.TrimSpace(record.RollbackHint),
	}
	for _, key := range []string{"approvalId", "evidenceId", "hostId", "hostName", "nextStepSuggestion"} {
		if value := strings.TrimSpace(anyToString(record.Metadata[key])); value != "" {
			verificationDetail[key] = value
		}
	}
	if sources := verificationStringSlice(record.Metadata["verificationSources"]); len(sources) > 0 {
		verificationDetail["verificationSources"] = append([]string(nil), sources...)
	}
	if len(verificationStrategyDefinitions(record.Metadata["verifyStrategyDetails"])) > 0 {
		verificationDetail["verifyStrategyDetails"] = record.Metadata["verifyStrategyDetails"]
	}
	if successCriteria := verificationStringSlice(record.Metadata["verificationSuccessCriteria"]); len(successCriteria) > 0 {
		verificationDetail["verificationSuccessCriteria"] = append([]string(nil), successCriteria...)
	}

	verificationText := []string{
		actionVerificationSummary(card, record.Status),
	}
	if len(record.Findings) > 0 {
		verificationText = append(verificationText, "结论："+strings.Join(record.Findings, " / "))
	}
	if strings.TrimSpace(record.Strategy) != "" {
		verificationText = append(verificationText, "策略："+strings.TrimSpace(record.Strategy))
	}
	if suggestion := strings.TrimSpace(anyToString(record.Metadata["nextStepSuggestion"])); suggestion != "" {
		verificationText = append(verificationText, "下一步："+suggestion)
	}

	a.store.UpsertCard(sessionID, model.Card{
		ID:        actionVerificationCardID(card.ID),
		Type:      "VerificationCard",
		Role:      "assistant",
		Title:     actionVerificationCardTitle(record.Status),
		Text:      strings.Join(dedupeCompactStrings(verificationText), "\n\n"),
		Summary:   actionVerificationSummary(card, record.Status),
		Status:    actionVerificationCardStatus(record.Status),
		HostID:    strings.TrimSpace(card.HostID),
		HostName:  strings.TrimSpace(card.HostName),
		Detail:    verificationDetail,
		CreatedAt: actionVerificationCardCreatedAt(a.cardByID(sessionID, actionVerificationCardID(card.ID)), now),
		UpdatedAt: now,
	})

	if record.Status == "passed" {
		return
	}

	rollbackLines := []string{}
	if strings.TrimSpace(record.RollbackHint) != "" {
		rollbackLines = append(rollbackLines, record.RollbackHint)
	}
	if suggestion := strings.TrimSpace(anyToString(record.Metadata["nextStepSuggestion"])); suggestion != "" {
		rollbackLines = append(rollbackLines, "下一步建议："+suggestion)
	}
	if len(rollbackLines) == 0 {
		rollbackLines = append(rollbackLines, "建议先停止继续扩散，确认影响范围后再决定是否人工回退。")
	}

	rollbackDetail := cloneAnyMap(verificationDetail)
	rollbackDetail["rollbackHint"] = strings.TrimSpace(record.RollbackHint)
	if suggestion := strings.TrimSpace(anyToString(record.Metadata["nextStepSuggestion"])); suggestion != "" {
		rollbackDetail["nextStepSuggestion"] = suggestion
	}

	a.store.UpsertCard(sessionID, model.Card{
		ID:        actionRollbackCardID(card.ID),
		Type:      "RollbackCard",
		Role:      "assistant",
		Title:     "回滚建议",
		Text:      strings.Join(dedupeCompactStrings(rollbackLines), "\n\n"),
		Summary:   firstNonEmptyValue(target, "请先确认是否需要回滚"),
		Status:    "warning",
		HostID:    strings.TrimSpace(card.HostID),
		HostName:  strings.TrimSpace(card.HostName),
		Detail:    rollbackDetail,
		CreatedAt: actionVerificationCardCreatedAt(a.cardByID(sessionID, actionRollbackCardID(card.ID)), now),
		UpdatedAt: now,
	})
}

func shouldTrackActionVerification(card model.Card) bool {
	switch card.Type {
	case "FileChangeCard":
		return true
	case "CommandCard":
		if getBool(card.Detail, "readonly") {
			return false
		}
		toolName := strings.TrimSpace(getStringAny(card.Detail, "tool", "toolName"))
		return toolName != "readonly_host_inspect" && toolName != "execute_readonly_query"
	default:
		return false
	}
}

func actionVerificationRecordID(cardID string) string {
	return "verify-" + strings.TrimSpace(cardID)
}

func actionVerificationCardID(cardID string) string {
	return "verification-card-" + strings.TrimSpace(cardID)
}

func actionRollbackCardID(cardID string) string {
	return "rollback-card-" + strings.TrimSpace(cardID)
}

func actionVerificationStrategy(plan verificationPlanDefinition) string {
	if name := strings.TrimSpace(plan.PrimaryStrategy()); name != "" {
		return name
	}
	return "post_action_verification"
}

func actionVerificationCriteria(card model.Card) []string {
	return actionVerificationPlan(card).StrategyNames()
}

func actionVerificationFinalStatus(cardStatus string) string {
	if normalizeCardStatus(cardStatus) == "completed" {
		return "passed"
	}
	return "failed"
}

func actionVerificationSummary(card model.Card, verificationStatus string) string {
	target := actionVerificationTargetSummary(card)
	switch strings.TrimSpace(verificationStatus) {
	case "running":
		return fmt.Sprintf("%s 已进入自动验证。", target)
	case "passed":
		return fmt.Sprintf("%s 自动验证通过。", target)
	default:
		return fmt.Sprintf("%s 自动验证失败。", target)
	}
}

func actionVerificationToolName(card model.Card) string {
	if toolName := strings.TrimSpace(getStringAny(card.Detail, "tool", "toolName")); toolName != "" {
		return toolName
	}
	switch card.Type {
	case "FileChangeCard":
		return "execute_system_mutation"
	case "CommandCard":
		return "commandExecution"
	default:
		return strings.TrimSpace(card.Type)
	}
}

func actionVerificationFindings(card model.Card, verificationStatus string) []string {
	findings := make([]string, 0, 3)
	if summary := strings.TrimSpace(card.Summary); summary != "" {
		findings = append(findings, summary)
	}
	if verificationStatus == "passed" {
		findings = append(findings, "动作执行完成，未发现直接失败信号。")
	} else {
		detail := firstNonEmptyValue(strings.TrimSpace(card.Error), strings.TrimSpace(card.Stderr), strings.TrimSpace(card.Text), strings.TrimSpace(card.Output))
		if detail == "" {
			detail = "动作执行未达到自动验证通过条件。"
		}
		findings = append(findings, truncate(detail, 200))
	}
	return dedupeCompactStrings(findings)
}

func actionVerificationRollbackHint(card model.Card, verificationStatus string) string {
	explicit := strings.TrimSpace(getStringAny(card.Detail, "rollbackHint", "rollback_hint", "rollbackSuggestion", "rollback_suggestion", "rollback"))
	if explicit != "" || verificationStatus == "passed" {
		return explicit
	}

	toolName := actionVerificationToolName(card)
	target := actionVerificationTargetSummary(card)
	filePath := strings.TrimSpace(getStringAny(card.Detail, "filePath", "path"))
	if filePath == "" && card.Type == "FileChangeCard" && len(card.Changes) > 0 {
		filePath = strings.TrimSpace(card.Changes[0].Path)
	}

	switch toolName {
	case serviceRestartToolName, serviceStopToolName:
		return fmt.Sprintf("建议先将 %s 恢复到变更前状态，再复查服务健康、错误日志和关键指标。", target)
	case configApplyToolName, "write_file":
		if filePath != "" {
			return fmt.Sprintf("建议先将 %s 回滚到变更前版本，再执行配置校验和健康检查。", filePath)
		}
		return "建议先回滚到变更前配置，再执行配置校验和健康检查。"
	case packageInstallToolName, packageUpgradeToolName:
		return fmt.Sprintf("建议先回退 %s 到最近稳定版本，再验证关键健康探针和日志。", target)
	}

	if card.Type == "FileChangeCard" {
		if filePath != "" {
			return fmt.Sprintf("建议先将 %s 回滚到变更前版本，再确认服务健康后决定是否重试。", filePath)
		}
		return "建议先回滚最近一次文件变更，再确认服务健康后决定是否重试。"
	}
	return fmt.Sprintf("建议先将 %s 恢复到最近稳定状态，再执行关键健康检查确认影响是否收敛。", target)
}

func actionVerificationNextStepSuggestion(card model.Card, verificationStatus, rollbackHint string) string {
	if verificationStatus == "passed" {
		return ""
	}
	toolName := actionVerificationToolName(card)
	target := actionVerificationTargetSummary(card)
	switch toolName {
	case serviceRestartToolName, serviceStopToolName:
		return fmt.Sprintf("先检查 %s 的健康探针、5xx 指标和错误日志，再决定是否执行回滚。", target)
	case configApplyToolName, "write_file":
		return fmt.Sprintf("先做一次配置校验和差异复核，确认影响范围后再执行回滚或重试。")
	case packageInstallToolName, packageUpgradeToolName:
		return fmt.Sprintf("先确认版本差异和依赖影响，再决定是否回退到上一稳定版本。")
	}
	if card.Type == "FileChangeCard" {
		return "先确认本次变更 diff 与目标主机健康状态，再决定是否立即回滚。"
	}
	if rollbackHint != "" {
		return "先执行一轮健康检查和日志复核，再按回滚建议处理。"
	}
	return "先确认失败是否会继续扩散，再决定回滚、人工接管或重试。"
}

func actionVerificationTargetSummary(card model.Card) string {
	target := strings.TrimSpace(getStringAny(card.Detail, "targetSummary", "target"))
	if target == "" {
		if card.Type == "FileChangeCard" && len(card.Changes) > 0 {
			target = strings.TrimSpace(card.Changes[0].Path)
		}
	}
	if target == "" {
		target = firstNonEmptyValue(strings.TrimSpace(card.Command), strings.TrimSpace(card.Title), strings.TrimSpace(card.HostName), strings.TrimSpace(card.HostID), "目标动作")
	}
	return target
}

func actionVerificationCardTitle(verificationStatus string) string {
	if strings.TrimSpace(verificationStatus) == "passed" {
		return "自动验证通过"
	}
	return "自动验证失败"
}

func actionVerificationCardStatus(verificationStatus string) string {
	if strings.TrimSpace(verificationStatus) == "passed" {
		return "completed"
	}
	return "failed"
}

func actionVerificationCardCreatedAt(existing *model.Card, fallback string) string {
	if existing != nil && strings.TrimSpace(existing.CreatedAt) != "" {
		return strings.TrimSpace(existing.CreatedAt)
	}
	return strings.TrimSpace(fallback)
}

func actionVerificationMetadata(card model.Card, plan verificationPlanDefinition) map[string]any {
	metadata := cloneAnyMap(card.Detail)
	applyVerificationPlanFields(metadata, plan)
	metadata["cardId"] = strings.TrimSpace(card.ID)
	metadata["cardType"] = strings.TrimSpace(card.Type)
	metadata["hostId"] = strings.TrimSpace(card.HostID)
	metadata["hostName"] = strings.TrimSpace(firstNonEmptyValue(card.HostName, card.HostID))
	metadata["command"] = strings.TrimSpace(card.Command)
	metadata["cwd"] = strings.TrimSpace(card.Cwd)
	metadata["status"] = strings.TrimSpace(card.Status)
	metadata["summary"] = strings.TrimSpace(card.Summary)
	metadata["startedAt"] = strings.TrimSpace(card.CreatedAt)
	metadata["endedAt"] = strings.TrimSpace(card.UpdatedAt)
	metadata["durationMs"] = card.DurationMS
	if evidenceID := strings.TrimSpace(getStringAny(card.Detail, "evidenceId")); evidenceID != "" {
		metadata["evidenceId"] = evidenceID
	}
	if card.Type == "FileChangeCard" && len(card.Changes) > 0 {
		metadata["filePath"] = strings.TrimSpace(card.Changes[0].Path)
	}
	return metadata
}

func actionVerificationEvidenceIDs(card model.Card) []string {
	evidenceID := strings.TrimSpace(getStringAny(card.Detail, "evidenceId"))
	if evidenceID == "" {
		return nil
	}
	return []string{evidenceID}
}

func verificationStringSlice(raw any) []string {
	switch value := raw.(type) {
	case []string:
		return dedupeCompactStrings(value)
	case []any:
		out := make([]string, 0, len(value))
		for _, entry := range value {
			if text := strings.TrimSpace(anyToString(entry)); text != "" {
				out = append(out, text)
			}
		}
		return dedupeCompactStrings(out)
	default:
		return nil
	}
}

func dedupeCompactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text == "" {
			continue
		}
		if _, exists := seen[text]; exists {
			continue
		}
		seen[text] = struct{}{}
		out = append(out, text)
	}
	return out
}
