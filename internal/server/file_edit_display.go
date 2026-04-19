package server

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/filepatch"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func fileChangeDisplayPayload(change model.FileChange, writeMode, summary, warningText string) *ToolDisplayPayload {
	path := strings.TrimSpace(change.Path)
	changeKind := strings.TrimSpace(change.Kind)
	if changeKind == "" {
		changeKind = "update"
	}
	mode := normalizedFileChangeWriteMode(changeKind, writeMode)
	added, removed := diffLineStats(change.Diff)
	display := &ToolDisplayPayload{
		Summary:  strings.TrimSpace(summary),
		Activity: path,
		Blocks: []ToolDisplayBlock{
			{
				Kind:  ToolDisplayBlockResultStats,
				Title: "变更摘要",
				Items: []map[string]any{
					{"label": "路径", "value": path},
					{"label": "操作", "value": fileChangeOperationLabel(changeKind)},
					{"label": "模式", "value": fileChangeModeLabel(mode)},
				},
			},
		},
	}
	if warning := strings.TrimSpace(warningText); warning != "" {
		display.Blocks = append(display.Blocks, ToolDisplayBlock{
			Kind:  ToolDisplayBlockWarning,
			Title: "说明",
			Text:  warning,
		})
	}
	display.Blocks = append(display.Blocks, ToolDisplayBlock{
		Kind:  ToolDisplayBlockFileDiffSummary,
		Title: "变更详情",
		Items: []map[string]any{{
			"path":    path,
			"summary": fileChangeSummaryText(changeKind, mode, added, removed),
			"added":   added,
			"removed": removed,
		}},
	})
	return display
}

func fileChangeDisplayDetail(baseDetail map[string]any, display *ToolDisplayPayload) map[string]any {
	detail := cloneAnyMap(baseDetail)
	if detail == nil {
		detail = make(map[string]any)
	}
	if display != nil {
		detail["display"] = toolDisplayPayloadToProjectionMap(display)
	}
	return detail
}

func normalizedFileChangeWriteMode(changeKind, writeMode string) string {
	mode := strings.TrimSpace(writeMode)
	if mode != "" {
		return mode
	}
	if strings.EqualFold(strings.TrimSpace(changeKind), "append") {
		return "append"
	}
	return "overwrite"
}

func fileChangeOperationLabel(changeKind string) string {
	switch strings.TrimSpace(changeKind) {
	case "create":
		return "新建"
	case "append":
		return "追加"
	case "delete":
		return "删除"
	default:
		return "更新"
	}
}

func fileChangeModeLabel(writeMode string) string {
	switch strings.TrimSpace(writeMode) {
	case "append":
		return "追加"
	default:
		return "覆盖"
	}
}

func fileChangeSummaryText(changeKind, writeMode string, added, removed int) string {
	parts := []string{fileChangeOperationLabel(changeKind)}
	if changeKind != "delete" {
		parts = append(parts, fileChangeModeLabel(writeMode))
	}
	if added > 0 || removed > 0 {
		parts = append(parts, fmt.Sprintf("+%d/-%d", added, removed))
	}
	return strings.Join(parts, " · ")
}

func pendingRemoteFileChangeSummary(path string) string {
	return "等待审批：修改远程文件 " + strings.TrimSpace(path)
}

func declinedRemoteFileChangeSummary(path string) string {
	return "已拒绝修改远程文件 " + strings.TrimSpace(path)
}

func failedRemoteFileChangeSummary(path string) string {
	return "修改远程文件失败：" + strings.TrimSpace(path)
}

func patchExecutionResult(processCardID string, action *filepatch.PatchAction, outputText string, execErr error, finishedAt time.Time) (ToolExecutionResult, bool) {
	if action == nil || len(action.Changes) == 0 {
		return ToolExecutionResult{}, false
	}
	if finishedAt.IsZero() {
		finishedAt = time.Now()
	}
	failed := execErr != nil
	summary := patchSummary(action, failed)
	display := patchDisplayPayload(action, summary, errorString(execErr))
	card := model.Card{
		ID:      applyPatchResultCardID(processCardID),
		Type:    "FileChangeCard",
		Title:   "Patch edit",
		Text:    firstNonEmptyValue(errorString(execErr), summary),
		Summary: summary,
		Status:  patchCardStatus(failed),
		Changes: patchCardChanges(action),
		Detail: map[string]any{
			"display":     toolDisplayPayloadToProjectionMap(display),
			"changeCount": len(action.Changes),
		},
		HostID:    model.ServerLocalHostID,
		HostName:  model.ServerLocalHostID,
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	}

	result := ToolExecutionResult{
		Status:     patchResultStatus(failed),
		OutputText: outputText,
		ErrorText:  errorString(execErr),
		OutputData: patchStructuredContent(action, summary, failed),
		ProjectionPayload: map[string]any{
			"display":             toolDisplayPayloadToProjectionMap(display),
			"finalCard":           lifecycleCardPayload(card),
			"syncActionArtifacts": true,
		},
		LifecycleMessage: summary,
		FinishedAt:       finishedAt,
	}
	if failed && result.OutputText == "" {
		result.OutputText = card.Text
	}
	return result, true
}

func applyPatchResultCardID(processCardID string) string {
	if id := strings.TrimSpace(processCardID); id != "" {
		return "result-" + id
	}
	return model.NewID("patch")
}

func patchResultStatus(failed bool) ToolRunStatus {
	if failed {
		return ToolRunStatusFailed
	}
	return ToolRunStatusCompleted
}

func patchCardStatus(failed bool) string {
	if failed {
		return "failed"
	}
	return "completed"
}

func patchSummary(action *filepatch.PatchAction, failed bool) string {
	if action == nil || len(action.Changes) == 0 {
		if failed {
			return "应用补丁失败"
		}
		return "已应用补丁"
	}
	if len(action.Changes) == 1 {
		path := patchChangePath(action.Changes[0])
		if failed {
			return "应用补丁失败：" + path
		}
		return "已应用补丁到 " + path
	}
	if failed {
		return fmt.Sprintf("应用补丁失败（%d 个文件）", len(action.Changes))
	}
	return fmt.Sprintf("已应用补丁到 %d 个文件", len(action.Changes))
}

func patchDisplayPayload(action *filepatch.PatchAction, summary, warningText string) *ToolDisplayPayload {
	items := make([]map[string]any, 0, len(action.Changes))
	totalAdded := 0
	totalRemoved := 0
	for _, change := range action.Changes {
		diff := renderPatchChangeDiff(change)
		added, removed := diffLineStats(diff)
		totalAdded += added
		totalRemoved += removed
		items = append(items, map[string]any{
			"path":    patchChangePath(change),
			"summary": patchChangeSummaryText(change, added, removed),
			"added":   added,
			"removed": removed,
		})
	}
	display := &ToolDisplayPayload{
		Summary:  strings.TrimSpace(summary),
		Activity: patchActivity(action),
		Blocks: []ToolDisplayBlock{
			{
				Kind:  ToolDisplayBlockResultStats,
				Title: "补丁结果",
				Items: []map[string]any{
					{"label": "文件数", "value": strconv.Itoa(len(action.Changes))},
					{"label": "新增行", "value": strconv.Itoa(totalAdded)},
					{"label": "删除行", "value": strconv.Itoa(totalRemoved)},
				},
			},
		},
	}
	if warning := strings.TrimSpace(warningText); warning != "" {
		display.Blocks = append(display.Blocks, ToolDisplayBlock{
			Kind:  ToolDisplayBlockWarning,
			Title: "说明",
			Text:  warning,
		})
	}
	display.Blocks = append(display.Blocks, ToolDisplayBlock{
		Kind:  ToolDisplayBlockFileDiffSummary,
		Title: "补丁摘要",
		Items: items,
	})
	return display
}

func patchActivity(action *filepatch.PatchAction) string {
	if action == nil || len(action.Changes) == 0 {
		return ""
	}
	if len(action.Changes) == 1 {
		return patchChangePath(action.Changes[0])
	}
	return fmt.Sprintf("%d 个文件", len(action.Changes))
}

func patchStructuredContent(action *filepatch.PatchAction, summary string, failed bool) map[string]any {
	changes := make([]map[string]any, 0, len(action.Changes))
	for _, change := range action.Changes {
		diff := renderPatchChangeDiff(change)
		added, removed := diffLineStats(diff)
		changes = append(changes, map[string]any{
			"path":       patchChangePath(change),
			"changeKind": patchChangeKind(change.Mode),
			"added":      added,
			"removed":    removed,
			"summary":    patchChangeSummaryText(change, added, removed),
		})
	}
	return map[string]any{
		"summary":     strings.TrimSpace(summary),
		"status":      patchCardStatus(failed),
		"changeCount": len(action.Changes),
		"changes":     changes,
	}
}

func patchCardChanges(action *filepatch.PatchAction) []model.FileChange {
	changes := make([]model.FileChange, 0, len(action.Changes))
	for _, change := range action.Changes {
		changes = append(changes, model.FileChange{
			Path: patchChangePath(change),
			Kind: patchChangeKind(change.Mode),
			Diff: renderPatchChangeDiff(change),
		})
	}
	return changes
}

func patchChangePath(change filepatch.FileChange) string {
	if path := strings.TrimSpace(change.NewPath); path != "" {
		return path
	}
	return strings.TrimSpace(change.OldPath)
}

func patchChangeKind(mode filepatch.ChangeMode) string {
	switch mode {
	case filepatch.ModeCreate:
		return "create"
	case filepatch.ModeDelete:
		return "delete"
	default:
		return "update"
	}
}

func patchChangeSummaryText(change filepatch.FileChange, added, removed int) string {
	return fileChangeSummaryText(patchChangeKind(change.Mode), normalizedFileChangeWriteMode(patchChangeKind(change.Mode), ""), added, removed)
}

func renderPatchChangeDiff(change filepatch.FileChange) string {
	var b strings.Builder
	oldPath := strings.TrimSpace(change.OldPath)
	newPath := strings.TrimSpace(change.NewPath)
	if change.Mode == filepatch.ModeCreate {
		oldPath = "/dev/null"
	}
	if change.Mode == filepatch.ModeDelete {
		newPath = "/dev/null"
	}
	if oldPath == "" {
		oldPath = patchChangePath(change)
	}
	if newPath == "" {
		newPath = patchChangePath(change)
	}
	fmt.Fprintf(&b, "--- %s\n", oldPath)
	fmt.Fprintf(&b, "+++ %s\n", newPath)
	for _, hunk := range change.Hunks {
		fmt.Fprintf(&b, "%s\n", renderPatchHunkHeader(hunk))
		for _, line := range hunk.Lines {
			b.WriteByte(byte(line.Op))
			b.WriteString(line.Content)
			b.WriteByte('\n')
		}
	}
	return strings.TrimSpace(b.String())
}

func renderPatchHunkHeader(hunk filepatch.Hunk) string {
	return fmt.Sprintf("@@ -%s +%s @@", renderPatchRange(hunk.OldStart, hunk.OldCount), renderPatchRange(hunk.NewStart, hunk.NewCount))
}

func renderPatchRange(start, count int) string {
	if count == 1 {
		return strconv.Itoa(start)
	}
	return fmt.Sprintf("%d,%d", start, count)
}
