package orchestrator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func ProjectMissionHistorySummary(m *Mission) model.MissionHistorySummary {
	if m == nil {
		return model.MissionHistorySummary{}
	}

	summary := model.MissionHistorySummary{
		ID:                       m.ID,
		WorkspaceSessionID:       m.WorkspaceSessionID,
		Title:                    firstNonEmpty(m.Title, "Mission"),
		Summary:                  strings.TrimSpace(m.Summary),
		Status:                   string(m.Status),
		ProjectionMode:           strings.TrimSpace(m.ProjectionMode),
		CreatedAt:                strings.TrimSpace(m.CreatedAt),
		UpdatedAt:                strings.TrimSpace(m.UpdatedAt),
		TaskCount:                len(m.Tasks),
		WorkerCount:              len(m.Workers),
		WorkspaceCount:           len(m.Workspaces),
		EventCount:               len(m.Events),
		QueuedTaskCount:          countTasksWithStatus(m, TaskStatusQueued),
		ReadyTaskCount:           countTasksWithStatus(m, TaskStatusReady),
		DispatchingTaskCount:     countTasksWithStatus(m, TaskStatusDispatching),
		RunningTaskCount:         countTasksWithStatus(m, TaskStatusRunning),
		WaitingApprovalTaskCount: countTasksWithStatus(m, TaskStatusWaitingApproval),
		WaitingInputTaskCount:    countTasksWithStatus(m, TaskStatusWaitingInput),
		CompletedTaskCount:       countTasksWithStatus(m, TaskStatusCompleted),
		FailedTaskCount:          countTasksWithStatus(m, TaskStatusFailed),
		CancelledTaskCount:       countTasksWithStatus(m, TaskStatusCancelled),
	}
	return summary
}

func ProjectMissionHistorySummaries(missions []*Mission) []model.MissionHistorySummary {
	if len(missions) == 0 {
		return nil
	}
	out := make([]model.MissionHistorySummary, 0, len(missions))
	for _, mission := range missions {
		if mission == nil {
			continue
		}
		out = append(out, ProjectMissionHistorySummary(mission))
	}
	return out
}

func ProjectMissionHistoryDetail(m *Mission) model.MissionHistoryDetail {
	if m == nil {
		return model.MissionHistoryDetail{}
	}

	summary := ProjectMissionHistorySummary(m)
	tasks := projectMissionHistoryTasks(m)
	workers := projectMissionHistoryWorkers(m)
	workspaces := projectMissionHistoryWorkspaces(m)
	events := projectMissionHistoryEvents(m)

	return model.MissionHistoryDetail{
		MissionHistorySummary: summary,
		Report: model.MissionHistoryReport{
			Summary:      missionHistoryReportSummary(m, summary),
			OverviewRows: missionHistoryOverviewRows(m, summary),
			Highlights:   missionHistoryHighlights(m, summary),
			Timeline:     events,
		},
		Tasks:      tasks,
		Workers:    workers,
		Workspaces: workspaces,
		Events:     events,
	}
}

func missionHistoryReportSummary(m *Mission, summary model.MissionHistorySummary) string {
	if m == nil {
		return ""
	}
	base := firstNonEmpty(summary.Summary, summary.Title, "Mission")
	parts := []string{base}
	if summary.Status != "" {
		parts = append(parts, summary.Status)
	}
	if summary.TaskCount > 0 {
		parts = append(parts, fmt.Sprintf("%d tasks", summary.TaskCount))
	}
	if summary.WorkerCount > 0 {
		parts = append(parts, fmt.Sprintf("%d workers", summary.WorkerCount))
	}
	return strings.Join(parts, " · ")
}

func missionHistoryOverviewRows(m *Mission, summary model.MissionHistorySummary) []model.KeyValueRow {
	if m == nil {
		return nil
	}
	return compactKeyValueRows([]model.KeyValueRow{
		{Key: "Mission", Value: firstNonEmpty(summary.ID, m.ID)},
		{Key: "标题", Value: firstNonEmpty(summary.Title, m.Title)},
		{Key: "状态", Value: summary.Status},
		{Key: "WorkspaceSession", Value: summary.WorkspaceSessionID},
		{Key: "ProjectionMode", Value: summary.ProjectionMode},
		{Key: "任务数", Value: fmt.Sprintf("%d", summary.TaskCount)},
		{Key: "主机数", Value: fmt.Sprintf("%d", summary.WorkerCount)},
		{Key: "工作区数", Value: fmt.Sprintf("%d", summary.WorkspaceCount)},
		{Key: "事件数", Value: fmt.Sprintf("%d", summary.EventCount)},
		{Key: "排队", Value: fmt.Sprintf("%d", summary.QueuedTaskCount)},
		{Key: "准备", Value: fmt.Sprintf("%d", summary.ReadyTaskCount)},
		{Key: "执行中", Value: fmt.Sprintf("%d", summary.RunningTaskCount)},
		{Key: "待审批", Value: fmt.Sprintf("%d", summary.WaitingApprovalTaskCount)},
		{Key: "待输入", Value: fmt.Sprintf("%d", summary.WaitingInputTaskCount)},
		{Key: "完成", Value: fmt.Sprintf("%d", summary.CompletedTaskCount)},
		{Key: "失败", Value: fmt.Sprintf("%d", summary.FailedTaskCount)},
		{Key: "取消", Value: fmt.Sprintf("%d", summary.CancelledTaskCount)},
		{Key: "创建时间", Value: summary.CreatedAt},
		{Key: "更新时间", Value: summary.UpdatedAt},
	})
}

func missionHistoryHighlights(m *Mission, summary model.MissionHistorySummary) []string {
	if m == nil {
		return nil
	}
	out := make([]string, 0, 6)
	if summary.WaitingApprovalTaskCount > 0 {
		out = append(out, fmt.Sprintf("%d tasks waiting approval", summary.WaitingApprovalTaskCount))
	}
	if summary.WaitingInputTaskCount > 0 {
		out = append(out, fmt.Sprintf("%d tasks waiting input", summary.WaitingInputTaskCount))
	}
	if summary.RunningTaskCount > 0 {
		out = append(out, fmt.Sprintf("%d tasks running", summary.RunningTaskCount))
	}
	if len(m.Workers) > 0 {
		hostIDs := make([]string, 0, len(m.Workers))
		for hostID, worker := range m.Workers {
			if worker == nil {
				continue
			}
			hostIDs = append(hostIDs, hostID)
		}
		sort.Strings(hostIDs)
		if len(hostIDs) > 0 {
			out = append(out, fmt.Sprintf("hosts: %s", strings.Join(hostIDs, ", ")))
		}
	}
	if len(m.Events) > 0 {
		timeline := projectMissionHistoryEvents(m)
		if len(timeline) > 0 {
			out = append(out, fmt.Sprintf("latest event: %s", firstNonEmpty(timeline[len(timeline)-1].Summary, timeline[len(timeline)-1].Type)))
		}
	}
	return compactStrings(out)
}

func projectMissionHistoryTasks(m *Mission) []model.MissionHistoryTask {
	if m == nil || len(m.Tasks) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m.Tasks))
	for id := range m.Tasks {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	out := make([]model.MissionHistoryTask, 0, len(keys))
	for _, id := range keys {
		task := m.Tasks[id]
		if task == nil {
			continue
		}
		out = append(out, model.MissionHistoryTask{
			ID:             task.ID,
			MissionID:      task.MissionID,
			HostID:         task.HostID,
			WorkerHostID:   task.WorkerHostID,
			SessionID:      task.SessionID,
			ThreadID:       task.ThreadID,
			Title:          task.Title,
			Instruction:    task.Instruction,
			Constraints:    append([]string(nil), task.Constraints...),
			Status:         string(task.Status),
			ExternalNodeID: task.ExternalNodeID,
			Attempt:        task.Attempt,
			CreatedAt:      task.CreatedAt,
			UpdatedAt:      task.UpdatedAt,
			LastError:      task.LastError,
			LastReply:      task.LastReply,
			ApprovalState:  task.ApprovalState,
		})
	}
	return out
}

func compactKeyValueRows(rows []model.KeyValueRow) []model.KeyValueRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]model.KeyValueRow, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Key) == "" || strings.TrimSpace(row.Value) == "" {
			continue
		}
		out = append(out, model.KeyValueRow{
			Key:   strings.TrimSpace(row.Key),
			Value: strings.TrimSpace(row.Value),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func projectMissionHistoryWorkers(m *Mission) []model.MissionHistoryWorker {
	if m == nil || len(m.Workers) == 0 {
		return nil
	}
	hostIDs := make([]string, 0, len(m.Workers))
	for hostID := range m.Workers {
		hostIDs = append(hostIDs, hostID)
	}
	sort.Strings(hostIDs)
	out := make([]model.MissionHistoryWorker, 0, len(hostIDs))
	for _, hostID := range hostIDs {
		worker := m.Workers[hostID]
		if worker == nil {
			continue
		}
		out = append(out, model.MissionHistoryWorker{
			MissionID:    worker.MissionID,
			HostID:       worker.HostID,
			SessionID:    worker.SessionID,
			ThreadID:     worker.ThreadID,
			WorkspaceID:  worker.WorkspaceID,
			ActiveTaskID: worker.ActiveTaskID,
			QueueTaskIDs: append([]string(nil), worker.QueueTaskIDs...),
			Status:       string(worker.Status),
			LastSeenAt:   worker.LastSeenAt,
			IdleSince:    worker.IdleSince,
			UpdatedAt:    worker.UpdatedAt,
		})
	}
	return out
}

func projectMissionHistoryWorkspaces(m *Mission) []model.MissionHistoryWorkspace {
	if m == nil || len(m.Workspaces) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m.Workspaces))
	for id := range m.Workspaces {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	out := make([]model.MissionHistoryWorkspace, 0, len(keys))
	for _, id := range keys {
		lease := m.Workspaces[id]
		if lease == nil {
			continue
		}
		out = append(out, model.MissionHistoryWorkspace{
			ID:         lease.ID,
			MissionID:  lease.MissionID,
			SessionID:  lease.SessionID,
			HostID:     lease.HostID,
			Kind:       string(lease.Kind),
			LocalPath:  lease.LocalPath,
			RemotePath: lease.RemotePath,
			Status:     lease.Status,
			CreatedAt:  lease.CreatedAt,
			UpdatedAt:  lease.UpdatedAt,
		})
	}
	return out
}

func projectMissionHistoryEvents(m *Mission) []model.MissionHistoryEvent {
	if m == nil || len(m.Events) == 0 {
		return nil
	}
	events := append([]RelayEvent(nil), m.Events...)
	sort.SliceStable(events, func(i, j int) bool {
		switch {
		case events[i].CreatedAt > events[j].CreatedAt:
			return false
		case events[i].CreatedAt < events[j].CreatedAt:
			return true
		default:
			return events[i].ID < events[j].ID
		}
	})
	out := make([]model.MissionHistoryEvent, 0, len(events))
	for _, event := range events {
		out = append(out, model.MissionHistoryEvent{
			ID:           event.ID,
			MissionID:    event.MissionID,
			TaskID:       event.TaskID,
			HostID:       event.HostID,
			SessionID:    event.SessionID,
			Type:         string(event.Type),
			Status:       event.Status,
			Summary:      event.Summary,
			Detail:       event.Detail,
			ApprovalID:   event.ApprovalID,
			SourceCardID: event.SourceCardID,
			CreatedAt:    event.CreatedAt,
		})
	}
	return out
}
