package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/orchestrator"
)

type workspaceMissionHistoryListResponse struct {
	Items []model.MissionHistorySummary `json:"items"`
	Total int                           `json:"total"`
	Limit int                           `json:"limit"`
}

type workspaceMissionHistoryDetailResponse struct {
	Mission    model.MissionHistoryDetail  `json:"mission"`
	PlanDetail orchestrator.PlanDetailView `json:"planDetail"`
}

func workerConversationModelItems(hostID string, taskID string, threadID string, items []orchestrator.WorkerConversationExcerptView) []model.ConversationExcerpt {
	if len(items) == 0 {
		return nil
	}
	out := make([]model.ConversationExcerpt, 0, len(items))
	for _, item := range items {
		out = append(out, model.ConversationExcerpt{
			ID:        strings.TrimSpace(item.ID),
			SessionID: strings.TrimSpace(item.SessionID),
			ThreadID:  strings.TrimSpace(threadID),
			HostID:    strings.TrimSpace(hostID),
			TaskID:    strings.TrimSpace(taskID),
			Role:      strings.TrimSpace(item.Role),
			Source:    strings.TrimSpace(item.Source),
			CardType:  strings.TrimSpace(item.Type),
			Title:     strings.TrimSpace(item.Summary),
			Text:      strings.TrimSpace(item.Text),
			Summary:   strings.TrimSpace(item.Summary),
			CreatedAt: strings.TrimSpace(item.CreatedAt),
			UpdatedAt: strings.TrimSpace(item.CreatedAt),
		})
	}
	return out
}

func dispatchEventModelItems(missionID string, items []orchestrator.DispatchEventView) []model.DispatchEvent {
	if len(items) == 0 {
		return nil
	}
	out := make([]model.DispatchEvent, 0, len(items))
	for _, item := range items {
		out = append(out, model.DispatchEvent{
			ID:           strings.TrimSpace(item.ID),
			MissionID:    strings.TrimSpace(missionID),
			TaskID:       strings.TrimSpace(item.TaskID),
			HostID:       strings.TrimSpace(item.HostID),
			SessionID:    strings.TrimSpace(item.SessionID),
			Type:         strings.TrimSpace(item.Type),
			Status:       strings.TrimSpace(item.Status),
			Summary:      strings.TrimSpace(item.Summary),
			Detail:       strings.TrimSpace(item.Detail),
			ApprovalID:   strings.TrimSpace(item.ApprovalID),
			SourceCardID: strings.TrimSpace(item.SourceCardID),
			CreatedAt:    strings.TrimSpace(item.CreatedAt),
		})
	}
	return out
}

func taskHostBindingModelItems(missionID string, items []orchestrator.TaskHostBindingView) []model.TaskHostBinding {
	if len(items) == 0 {
		return nil
	}
	out := make([]model.TaskHostBinding, 0, len(items))
	for _, item := range items {
		out = append(out, model.TaskHostBinding{
			TaskID:        strings.TrimSpace(item.TaskID),
			MissionID:     strings.TrimSpace(missionID),
			HostID:        strings.TrimSpace(item.HostID),
			WorkerHostID:  strings.TrimSpace(item.WorkerHostID),
			SessionID:     strings.TrimSpace(item.SessionID),
			ThreadID:      strings.TrimSpace(item.ThreadID),
			Title:         strings.TrimSpace(item.Title),
			Instruction:   strings.TrimSpace(item.Instruction),
			Status:        strings.TrimSpace(item.Status),
			Constraints:   append([]string(nil), item.Constraints...),
			ApprovalState: strings.TrimSpace(item.ApprovalState),
			LastReply:     strings.TrimSpace(item.LastReply),
			LastError:     strings.TrimSpace(item.LastError),
			CreatedAt:     strings.TrimSpace(item.CreatedAt),
			UpdatedAt:     strings.TrimSpace(item.UpdatedAt),
		})
	}
	return out
}

func approvalAnchorModelItem(anchor *orchestrator.ApprovalTerminalAnchorView, approval *model.ApprovalRequest, sessionID string, threadID string) *model.ApprovalTerminalAnchor {
	if anchor == nil {
		return nil
	}
	item := &model.ApprovalTerminalAnchor{
		ApprovalID:      strings.TrimSpace(anchor.ApprovalID),
		ItemID:          strings.TrimSpace(anchor.ItemID),
		HostID:          strings.TrimSpace(anchor.HostID),
		SessionID:       strings.TrimSpace(sessionID),
		ThreadID:        strings.TrimSpace(threadID),
		Command:         strings.TrimSpace(anchor.Command),
		Cwd:             strings.TrimSpace(anchor.Cwd),
		Reason:          strings.TrimSpace(anchor.Summary),
		RequestedAt:     strings.TrimSpace(anchor.CreatedAt),
		TerminalCardID:  strings.TrimSpace(anchor.SourceCardID),
		TerminalTitle:   strings.TrimSpace(anchor.Title),
		TerminalStatus:  strings.TrimSpace(anchor.Status),
		TerminalSummary: strings.TrimSpace(anchor.Summary),
	}
	if approval != nil {
		if reason := strings.TrimSpace(approval.Reason); reason != "" {
			item.Reason = reason
		}
		if requestedAt := strings.TrimSpace(approval.RequestedAt); requestedAt != "" {
			item.RequestedAt = requestedAt
		}
		if item.Command == "" {
			item.Command = strings.TrimSpace(approval.Command)
		}
		if item.Cwd == "" {
			item.Cwd = strings.TrimSpace(approval.Cwd)
		}
	}
	return item
}

func (a *App) buildMissionHistoryDetail(mission *orchestrator.Mission) (model.MissionHistoryDetail, orchestrator.PlanDetailView) {
	detail := orchestrator.ProjectMissionHistoryDetail(mission)
	planDetail := a.buildWorkspacePlanDetail(mission)
	if mission == nil {
		return detail, planDetail
	}

	detail.DispatchEvents = dispatchEventModelItems(mission.ID, planDetail.DispatchEvents)
	detail.TaskBindings = taskHostBindingModelItems(mission.ID, planDetail.TaskHostBindings)

	focusTaskByHostID := make(map[string]*orchestrator.TaskRun, len(mission.Workers))
	for hostID, worker := range mission.Workers {
		if worker == nil {
			continue
		}
		focusTaskByHostID[hostID] = workerFocusTask(mission, worker)
	}

	for index := range detail.Workers {
		workerSummary := &detail.Workers[index]
		worker := mission.Workers[workerSummary.HostID]
		if worker == nil {
			continue
		}
		workerDetail := a.buildWorkerReadonlyDetail(worker)
		task := focusTaskByHostID[workerSummary.HostID]
		taskID := ""
		threadID := strings.TrimSpace(workerSummary.ThreadID)
		if task != nil {
			taskID = strings.TrimSpace(task.ID)
			if threadID == "" {
				threadID = strings.TrimSpace(task.ThreadID)
			}
		}
		workerSummary.Conversation = workerConversationModelItems(workerSummary.HostID, taskID, threadID, workerDetail.Conversation)
		workerSummary.Terminal = workerDetail.Terminal
		var latestApproval *model.ApprovalRequest
		if session := a.store.Session(worker.SessionID); session != nil {
			latestApproval = latestApprovalRequest(session.Approvals)
		}
		workerSummary.ApprovalAnchor = approvalAnchorModelItem(workerDetail.ApprovalAnchor, latestApproval, workerSummary.SessionID, threadID)
	}

	return detail, planDetail
}

func (a *App) handleWorkspaceMissionHistory(w http.ResponseWriter, r *http.Request, _ string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if a.orchestrator == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "orchestrator is not initialized"})
		return
	}

	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 100 {
		limit = 100
	}

	filterWorkspaceSessionID := strings.TrimSpace(r.URL.Query().Get("workspaceSessionId"))
	missions := a.orchestrator.Missions()
	items := make([]model.MissionHistorySummary, 0, len(missions))
	for _, mission := range missions {
		if mission == nil {
			continue
		}
		if filterWorkspaceSessionID != "" && strings.TrimSpace(mission.WorkspaceSessionID) != filterWorkspaceSessionID {
			continue
		}
		items = append(items, orchestrator.ProjectMissionHistorySummary(mission))
	}

	total := len(items)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}

	writeJSON(w, http.StatusOK, workspaceMissionHistoryListResponse{
		Items: items,
		Total: total,
		Limit: limit,
	})
}

func (a *App) handleWorkspaceMissionHistoryDetail(w http.ResponseWriter, r *http.Request, _ string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if a.orchestrator == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "orchestrator is not initialized"})
		return
	}

	missionID := strings.TrimPrefix(r.URL.Path, "/api/v1/workspace/missions/")
	missionID = strings.Trim(missionID, "/")
	if missionID == "" || strings.Contains(missionID, "/") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "mission not found"})
		return
	}

	mission, ok := a.orchestrator.Mission(missionID)
	if !ok || mission == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "mission not found"})
		return
	}

	detail, planDetail := a.buildMissionHistoryDetail(mission)
	writeJSON(w, http.StatusOK, workspaceMissionHistoryDetailResponse{
		Mission:    detail,
		PlanDetail: planDetail,
	})
}
