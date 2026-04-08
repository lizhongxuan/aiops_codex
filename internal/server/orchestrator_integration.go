package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/orchestrator"
)

const orchestratorRemoteWorkspaceRoot = ".aiops_codex/missions"

type sessionCreateRequest struct {
	Kind string `json:"kind"`
}

func (a *App) initOrchestrator() error {
	if a == nil {
		return nil
	}
	storePath := filepath.Join(filepath.Dir(a.cfg.StatePath), "orchestrator", "orchestrator.json")
	manager := orchestrator.NewManagerFromConfig(orchestrator.ManagerConfig{
		Store:         orchestrator.NewStore(storePath),
		WorkspaceRoot: a.cfg.DefaultWorkspace,
		WorkspaceBootstrapper: func(_ context.Context, path string) error {
			if strings.TrimSpace(path) == "" {
				return nil
			}
			return os.MkdirAll(path, 0o755)
		},
	})
	if err := manager.Load(); err != nil {
		return err
	}
	a.orchestrator = manager
	a.reconcileOrchestratorAfterLoad()
	return nil
}

func (a *App) reconcileOrchestratorAfterLoad() {
	if a == nil || a.orchestrator == nil {
		return
	}
	result, err := a.orchestrator.ReconcileAfterLoad(orchestrator.RuntimeRecoveryProbe{
		SessionHasThread: func(sessionID string) bool {
			session := a.store.Session(sessionID)
			return session != nil && strings.TrimSpace(session.ThreadID) != ""
		},
		HostAvailable: func(hostID string) bool {
			host := a.findHost(hostID)
			return host.Status == "online" && host.Executable
		},
	})
	if err != nil {
		log.Printf("orchestrator restart reconcile failed err=%v", err)
		return
	}
	for _, outcome := range result.Failures {
		if outcome == nil {
			continue
		}
		switch outcome.Kind {
		case orchestrator.SessionKindPlanner:
			a.applyOrchestratorFailureOutcome(outcome, "旧版计划会话不再支持", "检测到 legacy planner mission，当前版本会直接将其收敛为失败。", false)
		case orchestrator.SessionKindWorker:
			if strings.TrimSpace(outcome.Reason) == "remote host unavailable after restart" {
				a.applyOrchestratorFailureOutcome(
					outcome,
					"Worker 已失联",
					fmt.Sprintf("host=%s 当前不可用，相关任务已收敛为失败。", firstNonEmptyValue(outcome.HostID, "-")),
					false,
				)
			} else {
				a.applyOrchestratorFailureOutcome(outcome, "Worker 未恢复", "server 重启后 worker 会话线程未恢复，相关任务已标记失败。", false)
			}
		}
	}
}

func (a *App) handleOrchestratorHostUnavailable(hostID, reason string, interrupt bool) {
	if a == nil || a.orchestrator == nil {
		return
	}
	outcomes, err := a.orchestrator.MarkHostUnavailable(hostID, reason)
	if err != nil {
		log.Printf("orchestrator host unavailable reconcile failed host=%s err=%v", hostID, err)
		return
	}
	for _, outcome := range outcomes {
		a.applyOrchestratorFailureOutcome(
			outcome,
			"Worker 已失联",
			fmt.Sprintf("host=%s 当前不可用，相关任务已收敛为失败。", firstNonEmptyValue(outcome.HostID, hostID)),
			interrupt,
		)
	}
}

func (a *App) applyOrchestratorFailureOutcome(outcome *orchestrator.SessionFailureOutcome, title, text string, interrupt bool) {
	if a == nil || outcome == nil {
		return
	}
	sessionID := strings.TrimSpace(outcome.SessionID)
	if sessionID != "" {
		if interrupt {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = a.interruptSessionTurn(ctx, sessionID)
			cancel()
		}
		a.finishRuntimeTurn(sessionID, "failed")
		a.broadcastSnapshot(sessionID)
	}

	workspaceSessionID := strings.TrimSpace(outcome.WorkspaceSessionID)
	if workspaceSessionID == "" {
		return
	}
	cardID := "workspace-reconcile-" + sessionID
	if outcome.HostID != "" {
		cardID = "workspace-reconcile-host-" + outcome.HostID
	}
	a.store.UpsertCard(workspaceSessionID, model.Card{
		ID:      cardID,
		Type:    "ResultSummaryCard",
		Title:   title,
		Summary: fmt.Sprintf("mission=%s session=%s status=%s", outcome.MissionID, firstNonEmptyValue(sessionID, "n/a"), outcome.MissionStatus),
		Text:    text,
		Status:  "failed",
		HostID:  defaultHostID(outcome.HostID),
		KVRows: compactKVRows([]model.KeyValueRow{
			{Key: "主机", Value: firstNonEmptyValue(outcome.HostID, "-")},
			{Key: "Session", Value: firstNonEmptyValue(sessionID, "-")},
			{Key: "Kind", Value: string(outcome.Kind)},
			{Key: "Mission", Value: firstNonEmptyValue(outcome.MissionID, "-")},
		}),
		Highlights: compactStrings([]string{
			strings.TrimSpace(outcome.Reason),
			strings.Join(outcome.FailedTaskIDs, ", "),
		}),
		Detail: compactDetailMap(map[string]any{
			"missionId":     outcome.MissionID,
			"sessionId":     sessionID,
			"hostId":        outcome.HostID,
			"kind":          outcome.Kind,
			"reason":        strings.TrimSpace(outcome.Reason),
			"failedTaskIds": append([]string(nil), outcome.FailedTaskIDs...),
			"missionStatus": outcome.MissionStatus,
		}),
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	})
	if outcome.MissionStatus == orchestrator.MissionStatusCompleted {
		a.finishRuntimeTurn(workspaceSessionID, "completed")
	} else if outcome.MissionStatus == orchestrator.MissionStatusCancelled {
		a.finishRuntimeTurn(workspaceSessionID, "aborted")
	} else if outcome.MissionStatus == orchestrator.MissionStatusFailed {
		a.finishRuntimeTurn(workspaceSessionID, "failed")
	}
	if mission, ok := a.orchestrator.MissionByWorkspaceSession(workspaceSessionID); ok {
		a.refreshWorkspaceProjection(mission)
	}
	a.broadcastSnapshot(workspaceSessionID)
}

func (a *App) sessionMeta(sessionID string) model.SessionMeta {
	return a.store.SessionMeta(sessionID)
}

func (a *App) sessionKind(sessionID string) string {
	return a.sessionMeta(sessionID).Kind
}

func (a *App) isOrchestratorInternalSession(sessionID string) bool {
	switch a.sessionKind(sessionID) {
	case model.SessionKindWorkspace, model.SessionKindPlanner, model.SessionKindWorker:
		return true
	default:
		return false
	}
}

func (a *App) recordOrchestratorTurnPhase(sessionID, phase string) {
	if a.orchestrator == nil || !a.isOrchestratorInternalSession(sessionID) {
		return
	}
	if err := a.orchestrator.RecordTurnPhase(sessionID, phase); err != nil {
		log.Printf("orchestrator turn phase record failed session=%s phase=%s err=%v", sessionID, phase, err)
	}
}

func (a *App) recordOrchestratorReply(sessionID string) {
	if a.orchestrator == nil || !a.isOrchestratorInternalSession(sessionID) {
		return
	}
	reply := a.latestCompletedAssistantText(sessionID)
	if reply == "" {
		return
	}
	if err := a.orchestrator.RecordReply(sessionID, reply); err != nil {
		log.Printf("orchestrator reply record failed session=%s err=%v", sessionID, err)
	}
}

func (a *App) recordOrchestratorApprovalRequested(sessionID string, approval model.ApprovalRequest) {
	if a.orchestrator == nil || !a.isOrchestratorInternalSession(sessionID) {
		return
	}
	summary := approvalRequestSummaryText(a.findHost(approval.HostID), approval)
	if err := a.orchestrator.RecordApprovalRequested(sessionID, approval.ID, summary, approval.Reason); err != nil {
		log.Printf("orchestrator approval requested record failed session=%s approval=%s err=%v", sessionID, approval.ID, err)
	}
}

func (a *App) recordOrchestratorApprovalResolved(sessionID string, approval model.ApprovalRequest) {
	if a.orchestrator == nil || !a.isOrchestratorInternalSession(sessionID) {
		return
	}
	summary := approvalMemoText(a.findHost(approval.HostID), approval, approvalDecisionForStatus(approval.Status))
	if err := a.orchestrator.RecordApprovalResolved(sessionID, approval.ID, approval.Status, summary); err != nil {
		log.Printf("orchestrator approval resolved record failed session=%s approval=%s err=%v", sessionID, approval.ID, err)
	}
}

func (a *App) recordOrchestratorChoiceRequested(sessionID string, choice model.ChoiceRequest) {
	if a.orchestrator == nil || !a.isOrchestratorInternalSession(sessionID) {
		return
	}
	summary := choiceCardTitle(choice.Questions)
	if err := a.orchestrator.RecordChoiceRequested(sessionID, choice.ID, summary); err != nil {
		log.Printf("orchestrator choice requested record failed session=%s choice=%s err=%v", sessionID, choice.ID, err)
	}
}

func (a *App) recordOrchestratorChoiceResolved(sessionID string, choice model.ChoiceRequest, answers []choiceAnswerInput) {
	if a.orchestrator == nil || !a.isOrchestratorInternalSession(sessionID) {
		return
	}
	summary := strings.Join(choiceAnswerSummary(choice.Questions, answers), "; ")
	if err := a.orchestrator.RecordChoiceResolved(sessionID, choice.ID, summary); err != nil {
		log.Printf("orchestrator choice resolved record failed session=%s choice=%s err=%v", sessionID, choice.ID, err)
	}
}

func (a *App) recordOrchestratorRemoteExecStarted(sessionID, hostID, cardID, command string) {
	if a.orchestrator == nil || !a.isOrchestratorInternalSession(sessionID) {
		return
	}
	if err := a.orchestrator.RecordRemoteExecStarted(sessionID, hostID, cardID, command); err != nil {
		log.Printf("orchestrator exec start record failed session=%s card=%s err=%v", sessionID, cardID, err)
	}
}

func (a *App) recordOrchestratorRemoteExecFinished(sessionID, hostID, cardID, status, command, detail string) {
	if a.orchestrator == nil || !a.isOrchestratorInternalSession(sessionID) {
		return
	}
	if err := a.orchestrator.RecordRemoteExecFinished(sessionID, hostID, cardID, status, command, detail); err != nil {
		log.Printf("orchestrator exec finish record failed session=%s card=%s err=%v", sessionID, cardID, err)
	}
}

func normalizeSessionCreateKind(kind string) (string, error) {
	switch strings.TrimSpace(kind) {
	case "", model.SessionKindSingleHost:
		return model.SessionKindSingleHost, nil
	case model.SessionKindWorkspace:
		return model.SessionKindWorkspace, nil
	default:
		return "", fmt.Errorf("unsupported session kind %q", kind)
	}
}

func sessionCreateMeta(kind string) model.SessionMeta {
	switch kind {
	case model.SessionKindWorkspace:
		return model.NormalizeSessionMeta(model.SessionMeta{
			Kind:          model.SessionKindWorkspace,
			Visible:       true,
			RuntimePreset: model.SessionRuntimePresetWorkspace,
		})
	default:
		return model.DefaultSessionMeta()
	}
}

func (a *App) handleWorkspaceChatMessage(w http.ResponseWriter, r *http.Request, sessionID string, req chatRequest, requestStartedAt time.Time) {
	session := a.store.EnsureSession(sessionID)
	if session.Runtime.Turn.Active {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "当前 mission 执行中，完成后再发送新消息"})
		return
	}

	now := model.NowString()
	a.store.UpsertCard(sessionID, model.Card{
		ID:        model.NewID("msg"),
		Type:      "UserMessageCard",
		Role:      "user",
		Text:      req.Message,
		Status:    "completed",
		CreatedAt: now,
		UpdatedAt: now,
	})
	hostID := a.workspaceDirectTargetHost(sessionID, req)
	a.store.SetSelectedHost(sessionID, hostID)
	if workspaceMessageNeedsIntentClarification(req.Message) {
		log.Printf(
			"workspace intent guard waiting for clarification session=%s host=%s text=%q",
			sessionID,
			hostID,
			truncate(req.Message, 120),
		)
		a.startRuntimeTurn(sessionID, hostID)
		a.createChoiceRequest("", sessionID, map[string]any{}, workspaceIntentClarificationQuestions(req.Message))
		writeJSON(w, http.StatusAccepted, map[string]bool{"accepted": true})
		return
	}
	requestID := a.beginTurnTraceRequest(sessionID, hostID)
	log.Printf(
		"workspace react loop request begin session=%s request=%s kind=%s host=%s text=%q",
		sessionID,
		requestID,
		a.sessionKind(sessionID),
		hostID,
		truncate(req.Message, 120),
	)
	a.startRuntimeTurn(sessionID, hostID)
	a.broadcastSnapshot(sessionID)

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	a.setTurnCancel(sessionID, cancel)
	defer func() {
		a.clearTurnCancel(sessionID)
		cancel()
	}()

	if err := a.runReActAgentLoop(ctx, reActLoopRequest{
		SessionID:        sessionID,
		Kind:             reActLoopKindWorkspace,
		HostID:           hostID,
		Message:          req.Message,
		MonitorContext:   req.MonitorContext,
		RequestID:        requestID,
		RequestStartedAt: requestStartedAt,
	}); err != nil {
		log.Printf(
			"workspace react loop request failed session=%s request=%s kind=%s host=%s duration=%s err=%v",
			sessionID,
			requestID,
			a.sessionKind(sessionID),
			hostID,
			time.Since(requestStartedAt),
			err,
		)
		if errors.Is(err, context.Canceled) && a.turnWasInterrupted(sessionID) {
			writeJSON(w, http.StatusAccepted, map[string]any{
				"accepted":    false,
				"interrupted": true,
			})
			return
		}
		a.finishRuntimeTurn(sessionID, "failed")
		a.store.UpsertCard(sessionID, model.Card{
			ID:        model.NewID("error"),
			Type:      "ErrorCard",
			Title:     "主 Agent ReAct loop failed",
			Message:   err.Error(),
			Text:      err.Error(),
			Status:    "failed",
			CreatedAt: model.NowString(),
			UpdatedAt: model.NowString(),
		})
		a.broadcastSnapshot(sessionID)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	log.Printf(
		"workspace react loop request accepted session=%s request=%s kind=%s host=%s duration=%s",
		sessionID,
		requestID,
		a.sessionKind(sessionID),
		hostID,
		time.Since(requestStartedAt),
	)
	writeJSON(w, http.StatusAccepted, map[string]bool{"accepted": true})
}

func (a *App) workspaceDirectTargetHost(sessionID string, req chatRequest) string {
	if hostID := strings.TrimSpace(req.HostID); hostID != "" {
		return defaultHostID(hostID)
	}
	if session := a.store.Session(sessionID); session != nil {
		if hostID := strings.TrimSpace(session.SelectedHostID); hostID != "" {
			return defaultHostID(hostID)
		}
	}
	return model.ServerLocalHostID
}

func (a *App) ensureMissionForWorkspaceSession(ctx context.Context, workspaceSessionID, message string) (*orchestrator.Mission, error) {
	if a.orchestrator == nil {
		return nil, fmt.Errorf("orchestrator is not initialized")
	}
	if mission, ok := a.orchestrator.MissionByWorkspaceSession(workspaceSessionID); ok && mission != nil && mission.Status == orchestrator.MissionStatusRunning {
		if a.canReuseRunningWorkspaceMission(workspaceSessionID, mission) {
			return mission, nil
		}
		log.Printf("workspace mission stale session=%s mission=%s status=%s; starting a fresh mission", workspaceSessionID, mission.ID, mission.Status)
		_ = a.orchestrator.CancelByWorkspaceSession(ctx, workspaceSessionID)
	}

	mission, err := a.orchestrator.StartMission(ctx, orchestrator.StartMissionRequest{
		WorkspaceSessionID: workspaceSessionID,
		Title:              truncate(strings.TrimSpace(message), 120),
		Summary:            strings.TrimSpace(message),
	})
	if err != nil {
		return nil, err
	}

	a.store.UpdateSessionMeta(workspaceSessionID, func(meta *model.SessionMeta) {
		meta.Kind = model.SessionKindWorkspace
		meta.Visible = true
		meta.MissionID = mission.ID
		meta.WorkspaceSessionID = workspaceSessionID
		meta.RuntimePreset = model.SessionRuntimePresetWorkspace
	})
	a.upsertWorkspaceMissionCard(workspaceSessionID, mission)
	return mission, nil
}

func (a *App) canReuseRunningWorkspaceMission(workspaceSessionID string, mission *orchestrator.Mission) bool {
	if a == nil || mission == nil || mission.Status != orchestrator.MissionStatusRunning {
		return false
	}
	session := a.store.Session(workspaceSessionID)
	if session != nil && session.Runtime.Turn.Active {
		return true
	}
	if a.hasPendingApprovals(workspaceSessionID) || a.hasPendingChoices(workspaceSessionID) {
		return true
	}
	for _, worker := range mission.Workers {
		if worker == nil {
			continue
		}
		if strings.TrimSpace(worker.ActiveTaskID) != "" || len(worker.QueueTaskIDs) > 0 {
			return true
		}
		switch worker.Status {
		case orchestrator.WorkerStatusQueued, orchestrator.WorkerStatusDispatching, orchestrator.WorkerStatusRunning, orchestrator.WorkerStatusWaiting:
			return true
		}
	}
	for _, task := range mission.Tasks {
		if task == nil {
			continue
		}
		switch task.Status {
		case orchestrator.TaskStatusQueued,
			orchestrator.TaskStatusReady,
			orchestrator.TaskStatusDispatching,
			orchestrator.TaskStatusRunning,
			orchestrator.TaskStatusWaitingApproval,
			orchestrator.TaskStatusWaitingInput:
			return true
		}
	}
	return false
}

func (a *App) startWorkspaceRouteTurn(ctx context.Context, sessionID, hostID, message string) error {
	session := a.store.EnsureSession(sessionID)
	expectedHash := a.workspaceRouteThreadConfigHash(defaultHostID(hostID))
	if session.ThreadID != "" && strings.TrimSpace(session.ThreadConfigHash) != strings.TrimSpace(expectedHash) {
		a.clearSessionThreadBinding(sessionID)
		session.ThreadID = ""
	}

	a.startRuntimeTurn(sessionID, hostID)
	threadID, err := a.ensureThreadWithSpec(ctx, sessionID, a.buildWorkspaceRouteThreadStartSpec(ctx, sessionID, hostID))
	if err != nil {
		a.finishRuntimeTurn(sessionID, "failed")
		return err
	}
	if err := a.requestTurnWithSpec(ctx, sessionID, threadID, a.buildWorkspaceRouteTurnStartSpec(ctx, hostID, message)); err != nil {
		a.finishRuntimeTurn(sessionID, "failed")
		return err
	}
	return nil
}

func (a *App) startWorkspacePlanningTurn(ctx context.Context, mission *orchestrator.Mission, message string) error {
	if mission == nil {
		return fmt.Errorf("mission is nil")
	}
	needThread := true
	if session := a.store.Session(mission.WorkspaceSessionID); session != nil {
		expectedHash := a.workspaceOrchestrationThreadConfigHash(defaultHostID(session.SelectedHostID))
		if session.ThreadID != "" && strings.TrimSpace(session.ThreadConfigHash) != strings.TrimSpace(expectedHash) {
			a.clearSessionThreadBinding(mission.WorkspaceSessionID)
			session.ThreadID = ""
		}
		if session.ThreadID != "" {
			needThread = false
		}
	}
	if err := a.acquireOrchestratorPermit(ctx, mission.ID, mission.WorkspaceSessionID, mission.GlobalActiveBudget, mission.MissionActiveBudget, needThread); err != nil {
		return err
	}
	defer a.releaseOrchestratorPermit(mission.WorkspaceSessionID)
	a.ensureInternalSessionFromWorkspace(mission.WorkspaceSessionID, mission.WorkspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		MissionID:          mission.ID,
		WorkspaceSessionID: mission.WorkspaceSessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	}, model.ServerLocalHostID)
	a.startRuntimeTurn(mission.WorkspaceSessionID, model.ServerLocalHostID)
	a.setRuntimeTurnPhase(mission.WorkspaceSessionID, "planning")
	threadID, err := a.ensureThreadWithSpec(ctx, mission.WorkspaceSessionID, a.buildWorkspaceOrchestrationThreadStartSpec(ctx, mission.WorkspaceSessionID, mission))
	if err != nil {
		a.finishRuntimeTurn(mission.WorkspaceSessionID, "failed")
		return err
	}
	if err := a.requestTurnWithSpec(ctx, mission.WorkspaceSessionID, threadID, a.buildWorkspaceOrchestrationTurnStartSpec(ctx, mission.WorkspaceSessionID, mission, message)); err != nil {
		a.finishRuntimeTurn(mission.WorkspaceSessionID, "failed")
		return err
	}
	a.refreshWorkspaceProjection(mission)
	return nil
}

func (a *App) startWorkspaceReadonlyTurn(ctx context.Context, sessionID, hostID, message string) error {
	session := a.store.EnsureSession(sessionID)
	expectedHash := a.workspaceReadonlyThreadConfigHash(defaultHostID(hostID))
	if session.ThreadID != "" && strings.TrimSpace(session.ThreadConfigHash) != strings.TrimSpace(expectedHash) {
		a.clearSessionThreadBinding(sessionID)
		session.ThreadID = ""
	}

	a.startRuntimeTurn(sessionID, hostID)
	threadID, err := a.ensureThreadWithSpec(ctx, sessionID, a.buildWorkspaceReadonlyThreadStartSpec(ctx, sessionID, hostID))
	if err != nil {
		a.finishRuntimeTurn(sessionID, "failed")
		return fmt.Errorf("workspace readonly thread/start failed for host %s: %w", defaultHostID(hostID), err)
	}
	if err := a.requestTurnWithSpec(ctx, sessionID, threadID, a.buildWorkspaceReadonlyTurnStartSpec(ctx, hostID, message)); err != nil {
		a.finishRuntimeTurn(sessionID, "failed")
		return fmt.Errorf("workspace readonly turn/start failed for host %s: %w", defaultHostID(hostID), err)
	}
	return nil
}

func (a *App) ensureInternalSessionFromWorkspace(targetSessionID, workspaceSessionID string, meta model.SessionMeta, hostID string) {
	a.store.EnsureSessionWithMeta(targetSessionID, meta)
	a.store.SetSelectedHost(targetSessionID, hostID)
	auth := a.store.Auth(workspaceSessionID)
	if auth.Connected || auth.Pending {
		a.store.SetAuth(targetSessionID, auth, a.store.Tokens(workspaceSessionID))
	}
}

func (a *App) handleWorkspaceStop(w http.ResponseWriter, r *http.Request, sessionID string) {
	if a.orchestrator == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "orchestrator is not initialized"})
		return
	}
	mission, ok := a.orchestrator.MissionByWorkspaceSession(sessionID)
	session := a.store.Session(sessionID)
	if !ok || mission == nil || mission.Status != orchestrator.MissionStatusRunning {
		if session == nil || !session.Runtime.Turn.Active {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "当前没有可中断的 mission"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		_ = a.interruptSessionTurn(ctx, sessionID)
		a.finishRuntimeTurn(sessionID, "aborted")
		a.store.UpsertCard(sessionID, model.Card{
			ID:        model.NewID("notice"),
			Type:      "NoticeCard",
			Title:     "Workspace stopped",
			Text:      "当前工作台会话已停止。",
			Status:    "notice",
			CreatedAt: model.NowString(),
			UpdatedAt: model.NowString(),
		})
		a.broadcastSnapshot(sessionID)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	_ = a.cancelTurnStart(sessionID)
	if err := a.orchestrator.CancelByWorkspaceSession(r.Context(), sessionID); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_ = a.interruptSessionTurn(ctx, sessionID)
	for _, worker := range mission.Workers {
		if worker == nil {
			continue
		}
		_ = a.interruptSessionTurn(ctx, worker.SessionID)
	}
	a.finishRuntimeTurn(sessionID, "aborted")
	if currentMission, ok := a.orchestrator.MissionByWorkspaceSession(sessionID); ok {
		a.refreshWorkspaceProjection(currentMission)
	}
	a.store.UpsertCard(sessionID, model.Card{
		ID:        model.NewID("notice"),
		Type:      "NoticeCard",
		Title:     "Mission stopped",
		Text:      "当前工作台 mission 已停止，相关 worker 会话已收到取消信号。",
		Status:    "notice",
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	})
	a.broadcastSnapshot(sessionID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) reconcileOrchestratorHostUnavailable(hostID, reason string) {
	if a == nil || a.orchestrator == nil {
		return
	}
	outcomes, err := a.orchestrator.FailWorkersByHost(hostID, reason)
	if err != nil {
		log.Printf("orchestrator host unavailable reconcile failed host=%s err=%v", hostID, err)
		return
	}
	now := model.NowString()
	for _, outcome := range outcomes {
		if outcome == nil {
			continue
		}
		if outcome.WorkerSessionID != "" {
			session := a.store.Session(outcome.WorkerSessionID)
			if session != nil && strings.TrimSpace(session.ThreadID) != "" {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = a.interruptSessionTurn(ctx, outcome.WorkerSessionID)
				cancel()
			} else {
				a.cancelTurnStart(outcome.WorkerSessionID)
				a.finalizeOpenTurnCards(outcome.WorkerSessionID, "failed")
				a.resolvePendingTurnRequests(outcome.WorkerSessionID, now)
			}
			a.finishRuntimeTurn(outcome.WorkerSessionID, "failed")
			a.resolveMirroredPendingTurnRequests(outcome.WorkerSessionID, "cancelled", reason)
			a.store.UpsertCard(outcome.WorkerSessionID, model.Card{
				ID:        model.NewID("error"),
				Type:      "ErrorCard",
				Title:     "远程主机已离线",
				Message:   firstNonEmptyValue(strings.TrimSpace(reason), "remote host became unavailable"),
				Text:      firstNonEmptyValue(strings.TrimSpace(reason), "remote host became unavailable"),
				Status:    "failed",
				CreatedAt: now,
				UpdatedAt: now,
			})
		}
		if outcome.CompletedTaskID != "" {
			a.projectWorkerOutcome(outcome)
		}
		if outcome.MissionCompleted {
			a.finalizeWorkspaceMissionOutcome(outcome)
		}
		workspaceSessionID := strings.TrimSpace(outcome.WorkspaceSessionID)
		if workspaceSessionID == "" {
			continue
		}
		message := strings.TrimSpace(reason)
		if message == "" {
			message = fmt.Sprintf("host %s 当前不可用，相关任务已标记失败。", outcome.WorkerHostID)
		}
		a.store.UpsertCard(workspaceSessionID, model.Card{
			ID:        fmt.Sprintf("workspace-host-unavailable-%s-%s", outcome.MissionID, outcome.WorkerHostID),
			Type:      "ErrorCard",
			Title:     fmt.Sprintf("%s unavailable", outcome.WorkerHostID),
			Message:   message,
			Text:      message,
			Status:    "failed",
			HostID:    outcome.WorkerHostID,
			CreatedAt: now,
			UpdatedAt: now,
		})
		if mission, ok := a.orchestrator.MissionByWorkspaceSession(workspaceSessionID); ok {
			a.refreshWorkspaceProjection(mission)
		}
		a.broadcastSnapshot(workspaceSessionID)
	}
}

func (a *App) interruptSessionTurn(ctx context.Context, sessionID string) error {
	session := a.store.Session(sessionID)
	if session == nil {
		return nil
	}
	threadID := session.ThreadID
	turnID := session.TurnID
	cancelledPending := a.cancelTurnStart(sessionID)
	if threadID == "" && !cancelledPending {
		return nil
	}
	if threadID != "" {
		params := map[string]any{
			"threadId":                   threadID,
			"clean_background_terminals": true,
		}
		if turnID != "" {
			params["turnId"] = turnID
		}
		var result map[string]any
		if err := a.codexRequest(ctx, "turn/interrupt", params, &result); err != nil && !cancelledPending {
			return err
		}
		a.cleanBackgroundTerminals(threadID)
	}
	a.markTurnInterrupted(sessionID, turnID)
	a.finishRuntimeTurn(sessionID, "aborted")
	a.broadcastSnapshot(sessionID)
	return nil
}

func (a *App) resolveApprovalTargetSession(sessionID, approvalID string) (string, model.ApprovalRequest, bool) {
	targetSessionID := sessionID
	if a.sessionKind(sessionID) == model.SessionKindWorkspace && a.orchestrator != nil {
		if route, ok := a.orchestrator.ResolveApprovalRoute(approvalID); ok && route.WorkerSessionID != "" {
			targetSessionID = route.WorkerSessionID
		}
	}
	approval, ok := a.store.Approval(targetSessionID, approvalID)
	return targetSessionID, approval, ok
}

func (a *App) resolveChoiceTargetSession(sessionID, choiceID string) (string, model.ChoiceRequest, bool) {
	targetSessionID := sessionID
	if a.sessionKind(sessionID) == model.SessionKindWorkspace && a.orchestrator != nil {
		if route, ok := a.orchestrator.ResolveChoiceRoute(choiceID); ok && route.SessionID != "" {
			targetSessionID = route.SessionID
		}
	}
	choice, ok := a.store.Choice(targetSessionID, choiceID)
	return targetSessionID, choice, ok
}

func (a *App) mirrorInternalApprovalToWorkspace(sourceSessionID string, approval model.ApprovalRequest, card model.Card) {
	meta := a.sessionMeta(sourceSessionID)
	workspaceSessionID := strings.TrimSpace(meta.WorkspaceSessionID)
	if workspaceSessionID == "" || workspaceSessionID == sourceSessionID {
		return
	}
	if a.orchestrator != nil && meta.Kind == model.SessionKindWorker {
		_ = a.orchestrator.SyncWorkerPhase(sourceSessionID, "waiting_approval")
		if err := a.orchestrator.RegisterApprovalRoute(approval.ID, sourceSessionID); err != nil {
			log.Printf("orchestrator approval route failed approval=%s session=%s err=%v", approval.ID, sourceSessionID, err)
		}
	}
	a.store.AddApproval(workspaceSessionID, approval)
	a.store.UpsertCard(workspaceSessionID, card)
	a.setRuntimeTurnPhase(workspaceSessionID, "waiting_approval")
	if a.orchestrator != nil {
		if mission, ok := a.orchestrator.MissionByWorkspaceSession(workspaceSessionID); ok {
			a.refreshWorkspaceProjection(mission)
		}
	}
	a.broadcastSnapshot(workspaceSessionID)
}

func (a *App) mirrorInternalChoiceToWorkspace(sourceSessionID string, choice model.ChoiceRequest, card model.Card) {
	meta := a.sessionMeta(sourceSessionID)
	workspaceSessionID := strings.TrimSpace(meta.WorkspaceSessionID)
	if workspaceSessionID == "" || workspaceSessionID == sourceSessionID {
		return
	}
	if a.orchestrator != nil {
		if meta.Kind == model.SessionKindWorker {
			_ = a.orchestrator.SyncWorkerPhase(sourceSessionID, "waiting_input")
		}
		if err := a.orchestrator.RegisterChoiceRoute(choice.ID, sourceSessionID); err != nil {
			log.Printf("orchestrator choice route failed choice=%s session=%s err=%v", choice.ID, sourceSessionID, err)
		}
	}
	a.store.AddChoice(workspaceSessionID, choice)
	a.store.UpsertCard(workspaceSessionID, card)
	a.setRuntimeTurnPhase(workspaceSessionID, "waiting_input")
	if a.orchestrator != nil {
		if mission, ok := a.orchestrator.MissionByWorkspaceSession(workspaceSessionID); ok {
			a.refreshWorkspaceProjection(mission)
		}
	}
	a.broadcastSnapshot(workspaceSessionID)
}

func (a *App) resolveMirroredApprovalCard(workspaceSessionID string, approval model.ApprovalRequest, status string) {
	if workspaceSessionID == "" {
		return
	}
	now := model.NowString()
	a.store.ResolveApproval(workspaceSessionID, approval.ID, status, now)
	a.store.UpdateCard(workspaceSessionID, approval.ItemID, func(card *model.Card) {
		card.Status = status
		card.UpdatedAt = now
	})
	if a.orchestrator != nil {
		if mission, ok := a.orchestrator.MissionByWorkspaceSession(workspaceSessionID); ok {
			a.refreshWorkspaceProjection(mission)
		}
	}
	a.broadcastSnapshot(workspaceSessionID)
}

func (a *App) resolveMirroredChoiceCard(workspaceSessionID, choiceID string, answers []choiceAnswerInput, questions []model.ChoiceQuestion) {
	if workspaceSessionID == "" {
		return
	}
	now := model.NowString()
	a.store.ResolveChoiceWithAnswers(workspaceSessionID, choiceID, "completed", now, choiceAnswersToModel(answers))
	a.store.UpdateCard(workspaceSessionID, choiceID, func(card *model.Card) {
		card.Status = "completed"
		card.AnswerSummary = choiceAnswerSummary(questions, answers)
		card.UpdatedAt = now
	})
	if a.orchestrator != nil {
		if mission, ok := a.orchestrator.MissionByWorkspaceSession(workspaceSessionID); ok {
			a.refreshWorkspaceProjection(mission)
		}
	}
	a.broadcastSnapshot(workspaceSessionID)
}

func (a *App) resolveMirroredPendingTurnRequests(sourceSessionID, status, summary string) {
	meta := a.sessionMeta(sourceSessionID)
	workspaceSessionID := strings.TrimSpace(meta.WorkspaceSessionID)
	if workspaceSessionID == "" || workspaceSessionID == sourceSessionID {
		return
	}
	session := a.store.Session(sourceSessionID)
	if session == nil {
		return
	}
	now := model.NowString()
	for approvalID, approval := range session.Approvals {
		mirrored, ok := a.store.Approval(workspaceSessionID, approvalID)
		if !ok || mirrored.Status != "pending" {
			continue
		}
		a.store.ResolveApproval(workspaceSessionID, approvalID, status, now)
		a.store.UpdateCard(workspaceSessionID, approval.ItemID, func(card *model.Card) {
			card.Status = status
			if strings.TrimSpace(summary) != "" && strings.TrimSpace(card.Summary) == "" {
				card.Summary = strings.TrimSpace(summary)
			}
			card.UpdatedAt = now
		})
	}
	for choiceID := range session.Choices {
		_, ok := a.store.Choice(sourceSessionID, choiceID)
		mirrored, mirroredOK := a.store.Choice(workspaceSessionID, choiceID)
		if !ok || !mirroredOK || mirrored.Status != "pending" {
			continue
		}
		a.store.ResolveChoice(workspaceSessionID, choiceID, status, now)
		a.store.UpdateCard(workspaceSessionID, choiceID, func(card *model.Card) {
			card.Status = status
			if strings.TrimSpace(summary) != "" {
				card.AnswerSummary = []string{strings.TrimSpace(summary)}
			}
			card.UpdatedAt = now
		})
	}
	if a.orchestrator != nil {
		if mission, ok := a.orchestrator.MissionByWorkspaceSession(workspaceSessionID); ok {
			a.refreshWorkspaceProjection(mission)
		}
	}
	a.broadcastSnapshot(workspaceSessionID)
}

func (a *App) activateDispatchResult(ctx context.Context, mission *orchestrator.Mission, result *orchestrator.DispatchResult) error {
	if mission == nil || result == nil {
		return nil
	}
	started := make(map[string]struct{}, len(result.Workers))
	for _, workerResult := range result.Workers {
		switch workerResult.Status {
		case string(orchestrator.WorkerStatusDispatching), string(orchestrator.WorkerStatusRunning):
		default:
			continue
		}
		if workerResult.SessionID != "" {
			if _, ok := started[workerResult.SessionID]; ok {
				continue
			}
			started[workerResult.SessionID] = struct{}{}
		}
		if err := a.startWorkerTask(ctx, mission, workerResult.SessionID, workerResult.HostID); err != nil {
			log.Printf("dispatch worker start failed mission=%s host=%s err=%v", mission.ID, workerResult.HostID, err)
		}
	}
	if currentMission, ok := a.orchestrator.MissionByWorkspaceSession(mission.WorkspaceSessionID); ok {
		a.refreshWorkspaceProjection(currentMission)
	}
	return nil
}

func (a *App) recycleIdleWorkerThreadBinding(workerSessionID string, worker *orchestrator.HostWorker) {
	if a == nil || worker == nil {
		return
	}
	idleSince := strings.TrimSpace(worker.IdleSince)
	if idleSince == "" {
		return
	}
	session := a.store.Session(workerSessionID)
	if session == nil || strings.TrimSpace(session.ThreadID) == "" || session.Runtime.Turn.Active {
		return
	}
	idleAt, err := time.Parse(time.RFC3339Nano, idleSince)
	if err != nil {
		return
	}
	if time.Since(idleAt.UTC()) < autoThreadResetIdleThreshold {
		return
	}
	a.clearSessionThreadBinding(workerSessionID)
}

func (a *App) failWorkerStart(workerSessionID, hostID string, err error) error {
	if err == nil || a == nil || a.orchestrator == nil {
		return err
	}
	a.cancelTurnStart(workerSessionID)
	a.finishRuntimeTurn(workerSessionID, "failed")
	outcome, outcomeErr := a.orchestrator.FailWorkerSession(workerSessionID, err.Error())
	if outcomeErr != nil {
		return err
	}
	if outcome != nil {
		a.applyOrchestratorFailureOutcome(
			outcome,
			"Worker start failed",
			fmt.Sprintf("host=%s: %s", firstNonEmptyValue(hostID, outcome.HostID), err.Error()),
			false,
		)
	}
	return err
}

func (a *App) startWorkerTask(ctx context.Context, mission *orchestrator.Mission, workerSessionID, hostID string) error {
	if mission == nil {
		return fmt.Errorf("mission is nil")
	}
	currentMission, worker, task, ok := a.orchestrator.WorkerTask(workerSessionID)
	if ok && currentMission != nil {
		mission = currentMission
	}
	if worker == nil || task == nil {
		return fmt.Errorf("worker %s has no active task", workerSessionID)
	}
	a.recycleIdleWorkerThreadBinding(workerSessionID, worker)
	needThread := true
	if session := a.store.Session(workerSessionID); session != nil && session.ThreadID != "" {
		needThread = false
	}
	if err := a.acquireOrchestratorPermit(ctx, mission.ID, workerSessionID, mission.GlobalActiveBudget, mission.MissionActiveBudget, needThread); err != nil {
		return err
	}
	defer a.releaseOrchestratorPermit(workerSessionID)
	if a.orchestrator != nil {
		if err := a.orchestrator.MarkWorkerDispatching(workerSessionID); err != nil {
			return err
		}
	}
	localWorkspace := orchestrator.WorkerLocalWorkspacePath(a.cfg.DefaultWorkspace, mission.ID, hostID)
	if err := os.MkdirAll(localWorkspace, 0o755); err != nil {
		return a.failWorkerStart(workerSessionID, hostID, err)
	}
	a.ensureInternalSessionFromWorkspace(workerSessionID, mission.WorkspaceSessionID, model.SessionMeta{
		Kind:               model.SessionKindWorker,
		Visible:            false,
		MissionID:          mission.ID,
		WorkspaceSessionID: mission.WorkspaceSessionID,
		WorkerHostID:       hostID,
		RuntimePreset:      model.SessionRuntimePresetWorker,
	}, hostID)
	a.startRuntimeTurn(workerSessionID, hostID)
	if err := a.bootstrapWorkerRemoteWorkspace(ctx, mission, workerSessionID, hostID); err != nil {
		return a.failWorkerStart(workerSessionID, hostID, err)
	}
	threadID, err := a.ensureThreadWithSpec(ctx, workerSessionID, a.buildWorkerThreadStartSpec(mission, task, hostID))
	if err != nil {
		return a.failWorkerStart(workerSessionID, hostID, err)
	}
	if err := a.requestTurnWithSpec(ctx, workerSessionID, threadID, a.buildWorkerTurnStartSpec(mission, task, hostID)); err != nil {
		return a.failWorkerStart(workerSessionID, hostID, err)
	}
	if a.orchestrator != nil {
		_ = a.orchestrator.SyncWorkerPhase(workerSessionID, "executing")
	}
	if currentMission, ok := a.orchestrator.MissionByWorkspaceSession(mission.WorkspaceSessionID); ok {
		a.refreshWorkspaceProjection(currentMission)
	}
	return nil
}

func (a *App) bootstrapWorkerRemoteWorkspace(ctx context.Context, mission *orchestrator.Mission, sessionID, hostID string) error {
	if mission == nil {
		return fmt.Errorf("mission is nil")
	}
	remotePath := orchestrator.WorkerRemoteWorkspacePath(orchestratorRemoteWorkspaceRoot, mission.ID, hostID)
	command := orchestrator.BootstrapRemoteWorkspaceCommand(remotePath)
	a.audit("orchestrator.workspace_bootstrap", map[string]any{
		"missionId": mission.ID,
		"sessionId": sessionID,
		"hostId":    hostID,
		"command":   command,
		"status":    "started",
	})
	result, err := a.runRemoteExec(ctx, sessionID, hostID, "bootstrap-"+mission.ID+"-"+hostID, execSpec{
		Command:    command,
		TimeoutSec: 20,
		Readonly:   false,
		Approval:   "orchestrator_bootstrap",
	})
	finalStatus := execResultCardStatus(result)
	a.audit("orchestrator.workspace_bootstrap", map[string]any{
		"missionId": mission.ID,
		"sessionId": sessionID,
		"hostId":    hostID,
		"command":   command,
		"status":    finalStatus,
		"exitCode":  result.ExitCode,
		"error":     emptyToNil(strings.TrimSpace(result.Error)),
	})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if finalStatus != "completed" {
		if strings.TrimSpace(result.Error) != "" {
			return fmt.Errorf("workspace bootstrap failed: %s", result.Error)
		}
		return fmt.Errorf("workspace bootstrap failed on host %s", hostID)
	}
	return nil
}

func (a *App) handleMissionTurnCompleted(sessionID, phase string) {
	if a.orchestrator == nil {
		return
	}
	kind := a.sessionKind(sessionID)
	if kind != model.SessionKindWorkspace {
		a.finishRuntimeTurn(sessionID, phase)
	}
	switch kind {
	case model.SessionKindWorkspace:
		if a.isWorkspaceRouteThread(sessionID) {
			reply := strings.TrimSpace(a.latestCompletedAssistantText(sessionID))
			decision, visibleReply, foundRoute, err := parseWorkspaceRouteReply(reply)
			if err != nil {
				log.Printf("workspace route parse failed session=%s err=%v", sessionID, err)
				a.finishRuntimeTurn(sessionID, "failed")
				a.store.UpsertCard(sessionID, model.Card{
					ID:        model.NewID("error"),
					Type:      "ErrorCard",
					Title:     "主 Agent 路由解析失败",
					Message:   err.Error(),
					Text:      err.Error(),
					Status:    "failed",
					CreatedAt: model.NowString(),
					UpdatedAt: model.NowString(),
				})
				a.broadcastSnapshot(sessionID)
				return
			}
			if foundRoute {
				if strings.TrimSpace(visibleReply) == "" {
					switch decision.Route {
					case "complex_task":
						visibleReply = "我先整理计划，准备在需要时协调 worker。"
					case "state_query":
						visibleReply = "我正在读取当前工作台状态。"
					case "host_readonly":
						visibleReply = "我正在读取目标主机的只读状态。"
					default:
						visibleReply = "我已收到你的问题，正在直接回复。"
					}
				}
				a.replaceLatestCompletedAssistantText(sessionID, visibleReply)
			}
			switch decision.Route {
			case "complex_task":
				userMessage := a.latestCompletedUserText(sessionID)
				if userMessage == "" {
					userMessage = visibleReply
				}
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				mission, err := a.ensureMissionForWorkspaceSession(ctx, sessionID, userMessage)
				if err != nil {
					a.finishRuntimeTurn(sessionID, "failed")
					a.store.UpsertCard(sessionID, model.Card{
						ID:        model.NewID("error"),
						Type:      "ErrorCard",
						Title:     "Mission failed",
						Message:   err.Error(),
						Text:      err.Error(),
						Status:    "failed",
						CreatedAt: model.NowString(),
						UpdatedAt: model.NowString(),
					})
					a.broadcastSnapshot(sessionID)
					return
				}
				a.store.UpsertCard(sessionID, model.Card{
					ID:        model.NewID("notice"),
					Type:      "NoticeCard",
					Title:     "plan 正在运行中",
					Text:      "主 Agent 正在生成计划，并准备在需要时派发给 worker。",
					Status:    "notice",
					CreatedAt: model.NowString(),
					UpdatedAt: model.NowString(),
				})
				if err := a.startWorkspacePlanningTurn(ctx, mission, userMessage); err != nil {
					a.finishRuntimeTurn(sessionID, "failed")
					a.store.UpsertCard(sessionID, model.Card{
						ID:        model.NewID("error"),
						Type:      "ErrorCard",
						Title:     "Workspace planning failed",
						Message:   err.Error(),
						Text:      err.Error(),
						Status:    "failed",
						CreatedAt: model.NowString(),
						UpdatedAt: model.NowString(),
					})
					a.broadcastSnapshot(sessionID)
					return
				}
				a.broadcastSnapshot(sessionID)
				return
			case "host_readonly":
				targetHostID := model.ServerLocalHostID
				if session := a.store.Session(sessionID); session != nil {
					targetHostID = defaultHostID(session.SelectedHostID)
				}
				if decision.TargetHost != "" {
					targetHostID = decision.TargetHost
					a.store.SetSelectedHost(sessionID, decision.TargetHost)
				}
				userMessage := a.latestCompletedUserText(sessionID)
				if userMessage == "" {
					userMessage = visibleReply
				}
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				if err := a.startWorkspaceReadonlyTurn(ctx, sessionID, targetHostID, userMessage); err != nil {
					a.finishRuntimeTurn(sessionID, "failed")
					a.store.UpsertCard(sessionID, model.Card{
						ID:        model.NewID("error"),
						Type:      "ErrorCard",
						Title:     "Workspace readonly failed",
						Message:   err.Error(),
						Text:      err.Error(),
						Status:    "failed",
						CreatedAt: model.NowString(),
						UpdatedAt: model.NowString(),
					})
					a.broadcastSnapshot(sessionID)
					return
				}
				a.broadcastSnapshot(sessionID)
				return
			}
			a.finishRuntimeTurn(sessionID, phase)
			a.broadcastSnapshot(sessionID)
			return
		}
		mission, ok := a.orchestrator.MissionBySession(sessionID)
		if !ok || mission == nil {
			a.finishRuntimeTurn(sessionID, phase)
			return
		}
		reply := strings.TrimSpace(a.latestCompletedAssistantText(sessionID))
		dispatchReq, found, err := parseWorkspaceDispatchRequest(reply)
		if err != nil {
			log.Printf("workspace dispatch parse failed session=%s err=%v", sessionID, err)
			a.finishRuntimeTurn(sessionID, "failed")
			a.store.UpsertCard(sessionID, model.Card{
				ID:        model.NewID("error"),
				Type:      "ErrorCard",
				Title:     "Plan 解析失败",
				Message:   err.Error(),
				Text:      err.Error(),
				Status:    "failed",
				CreatedAt: model.NowString(),
				UpdatedAt: model.NowString(),
			})
			a.broadcastSnapshot(sessionID)
			return
		}
		if !found || len(dispatchReq.Tasks) == 0 {
			a.syncWorkspaceMissionRuntime(mission, phase)
			a.refreshWorkspaceProjection(mission)
			a.broadcastSnapshot(sessionID)
			return
		}
		existingTasks := make(map[string]struct{}, len(mission.Tasks))
		for taskID := range mission.Tasks {
			existingTasks[taskID] = struct{}{}
		}
		nextTasks := make([]orchestrator.DispatchTaskRequest, 0, len(dispatchReq.Tasks))
		for _, task := range dispatchReq.Tasks {
			if strings.TrimSpace(task.TaskID) != "" {
				if _, ok := existingTasks[strings.TrimSpace(task.TaskID)]; ok {
					continue
				}
			}
			nextTasks = append(nextTasks, task)
		}
		if len(nextTasks) == 0 {
			a.syncWorkspaceMissionRuntime(mission, phase)
			a.refreshWorkspaceProjection(mission)
			a.broadcastSnapshot(sessionID)
			return
		}
		dispatchReq.Tasks = nextTasks
		if _, err := a.dispatchOrchestratorTasks(sessionID, dispatchReq); err != nil {
			log.Printf("workspace dispatch failed session=%s err=%v", sessionID, err)
			a.finishRuntimeTurn(sessionID, "failed")
			a.store.UpsertCard(sessionID, model.Card{
				ID:        model.NewID("error"),
				Type:      "ErrorCard",
				Title:     "Dispatch failed",
				Message:   err.Error(),
				Text:      err.Error(),
				Status:    "failed",
				CreatedAt: model.NowString(),
				UpdatedAt: model.NowString(),
			})
			a.broadcastSnapshot(sessionID)
			return
		}
		if refreshed, ok := a.orchestrator.MissionByWorkspaceSession(mission.WorkspaceSessionID); ok && refreshed != nil {
			mission = refreshed
		}
		a.syncWorkspaceMissionRuntime(mission, phase)
		a.refreshWorkspaceProjection(mission)
		a.broadcastSnapshot(sessionID)
	case model.SessionKindWorker:
		outcome, err := a.orchestrator.CompleteWorkerTurn(sessionID, phase, a.latestCompletedAssistantText(sessionID))
		if err != nil {
			log.Printf("worker turn completion sync failed session=%s err=%v", sessionID, err)
			return
		}
		a.projectWorkerOutcome(outcome)
		if outcome != nil && outcome.NextTask != nil {
			go func(missionID, workspaceSessionID, workerSessionID, hostID string) {
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				mission, ok := a.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
				if !ok {
					return
				}
				if err := a.startWorkerTask(ctx, mission, workerSessionID, hostID); err != nil {
					log.Printf("queued worker task start failed mission=%s host=%s err=%v", missionID, hostID, err)
				}
			}(outcome.MissionID, outcome.WorkspaceSessionID, outcome.WorkerSessionID, outcome.WorkerHostID)
		}
		if outcome != nil {
			a.activateQueuedMissionWorkers(outcome.WorkspaceSessionID)
		}
	}
}

func (a *App) handleMissionTurnCompletedAsync(sessionID, phase string, recordReplyAfter bool) {
	go func() {
		a.handleMissionTurnCompleted(sessionID, phase)
		if recordReplyAfter {
			a.recordOrchestratorReply(sessionID)
			a.broadcastSnapshot(sessionID)
		}
	}()
}

func (a *App) syncWorkspaceMissionRuntime(mission *orchestrator.Mission, fallbackPhase string) {
	if mission == nil || strings.TrimSpace(mission.WorkspaceSessionID) == "" {
		return
	}
	workspaceSessionID := strings.TrimSpace(mission.WorkspaceSessionID)
	switch mission.Status {
	case orchestrator.MissionStatusFailed:
		a.finishRuntimeTurn(workspaceSessionID, "failed")
		return
	case orchestrator.MissionStatusCancelled:
		a.finishRuntimeTurn(workspaceSessionID, "aborted")
		return
	case orchestrator.MissionStatusCompleted:
		a.finishRuntimeTurn(workspaceSessionID, "completed")
		return
	}

	hasWaitingApproval := a.hasPendingApprovals(workspaceSessionID)
	hasWaitingInput := a.hasPendingChoices(workspaceSessionID)
	hasActiveTasks := false
	for _, task := range mission.Tasks {
		if task == nil {
			continue
		}
		switch task.Status {
		case orchestrator.TaskStatusWaitingApproval:
			hasWaitingApproval = true
		case orchestrator.TaskStatusWaitingInput:
			hasWaitingInput = true
		case orchestrator.TaskStatusQueued,
			orchestrator.TaskStatusReady,
			orchestrator.TaskStatusDispatching,
			orchestrator.TaskStatusRunning:
			hasActiveTasks = true
		}
	}
	for _, worker := range mission.Workers {
		if worker == nil {
			continue
		}
		switch worker.Status {
		case orchestrator.WorkerStatusQueued, orchestrator.WorkerStatusDispatching, orchestrator.WorkerStatusRunning:
			hasActiveTasks = true
		case orchestrator.WorkerStatusWaiting:
			hasWaitingApproval = true
		}
	}

	switch {
	case hasWaitingApproval:
		a.setRuntimeTurnPhase(workspaceSessionID, "waiting_approval")
	case hasWaitingInput:
		a.setRuntimeTurnPhase(workspaceSessionID, "waiting_input")
	case hasActiveTasks:
		a.setRuntimeTurnPhase(workspaceSessionID, "executing")
	case len(mission.Tasks) > 0:
		a.finishRuntimeTurn(workspaceSessionID, "completed")
	case strings.TrimSpace(fallbackPhase) != "":
		a.finishRuntimeTurn(workspaceSessionID, fallbackPhase)
	default:
		a.finishRuntimeTurn(workspaceSessionID, "completed")
	}
}

func (a *App) activateQueuedMissionWorkers(workspaceSessionID string) {
	if a.orchestrator == nil || strings.TrimSpace(workspaceSessionID) == "" {
		return
	}
	activations, err := a.orchestrator.ActivateQueuedWorkers(workspaceSessionID)
	if err != nil {
		log.Printf("activate queued workers failed workspace=%s err=%v", workspaceSessionID, err)
		return
	}
	for _, activation := range activations {
		activation := activation
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			mission, ok := a.orchestrator.MissionByWorkspaceSession(activation.WorkspaceSessionID)
			if !ok || mission == nil {
				return
			}
			if err := a.startWorkerTask(ctx, mission, activation.WorkerSessionID, activation.WorkerHostID); err != nil {
				log.Printf("activate queued worker start failed mission=%s host=%s task=%s err=%v", activation.MissionID, activation.WorkerHostID, activation.ActivatedTaskID, err)
			}
		}()
	}
}

func (a *App) reconcileOrchestratorRecoveredWorkers() {
	if a.orchestrator == nil {
		return
	}
	reason := "server restarted before worker session could continue"
	for _, sessionID := range a.store.SessionIDs() {
		if a.sessionKind(sessionID) != model.SessionKindWorker {
			continue
		}
		session := a.store.Session(sessionID)
		if session != nil && strings.TrimSpace(session.ThreadID) != "" {
			continue
		}
		_, worker, task, ok := a.orchestrator.WorkerTask(sessionID)
		if !ok || worker == nil || task == nil || orchestratorTaskTerminal(task.Status) {
			continue
		}
		now := model.NowString()
		a.cancelTurnStart(sessionID)
		a.finishRuntimeTurn(sessionID, "failed")
		a.finalizeOpenTurnCards(sessionID, "failed")
		a.resolvePendingTurnRequests(sessionID, now)
		a.resolveMirroredPendingTurnRequests(sessionID, "cancelled", reason)
		a.store.UpsertCard(sessionID, model.Card{
			ID:        model.NewID("error"),
			Type:      "ErrorCard",
			Title:     "Worker 会话已恢复为失败",
			Message:   reason,
			Text:      reason,
			Status:    "failed",
			CreatedAt: now,
			UpdatedAt: now,
		})
		outcome, err := a.orchestrator.CompleteWorkerTurn(sessionID, "failed", reason)
		if err != nil {
			log.Printf("orchestrator restart reconcile failed session=%s err=%v", sessionID, err)
			continue
		}
		if outcome != nil {
			a.projectWorkerOutcome(outcome)
			if outcome.NextTask != nil {
				go func(workspaceSessionID, workerSessionID, hostID string) {
					ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
					defer cancel()
					mission, ok := a.orchestrator.MissionByWorkspaceSession(workspaceSessionID)
					if !ok {
						return
					}
					if err := a.startWorkerTask(ctx, mission, workerSessionID, hostID); err != nil {
						log.Printf("restart queued worker task start failed host=%s err=%v", hostID, err)
					}
				}(outcome.WorkspaceSessionID, outcome.WorkerSessionID, outcome.WorkerHostID)
			}
		}
	}
}

func (a *App) projectWorkerOutcome(outcome *orchestrator.WorkerTurnOutcome) {
	if outcome == nil || outcome.WorkspaceSessionID == "" {
		return
	}
	reply := strings.TrimSpace(a.latestCompletedAssistantText(outcome.WorkerSessionID))
	detail := compactDetailMap(map[string]any{
		"workerSessionId": outcome.WorkerSessionID,
		"hostId":          outcome.WorkerHostID,
		"taskId":          outcome.CompletedTaskID,
		"taskStatus":      string(outcome.CompletedTaskStatus),
		"missionId":       outcome.MissionID,
		"reply":           reply,
	})
	a.store.UpsertCard(outcome.WorkspaceSessionID, model.Card{
		ID:      "worker-result-" + outcome.CompletedTaskID,
		Type:    "ResultSummaryCard",
		Title:   fmt.Sprintf("%s 执行结果", outcome.WorkerHostID),
		Summary: fmt.Sprintf("task=%s status=%s", outcome.CompletedTaskID, outcome.CompletedTaskStatus),
		Text:    firstNonEmptyValue(reply, workerOutcomeFallbackText(outcome.CompletedTaskStatus)),
		Status:  normalizeCardStatus(string(outcome.CompletedTaskStatus)),
		KVRows: compactKVRows([]model.KeyValueRow{
			{Key: "主机", Value: outcome.WorkerHostID},
			{Key: "任务", Value: outcome.CompletedTaskID},
			{Key: "状态", Value: string(outcome.CompletedTaskStatus)},
			{Key: "WorkerSession", Value: outcome.WorkerSessionID},
		}),
		Highlights: compactStrings([]string{
			firstNonEmptyValue(reply, ""),
		}),
		Detail:    detail,
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	})
	if outcome.MissionCompleted {
		a.finalizeWorkspaceMissionOutcome(outcome)
	}
	if mission, ok := a.orchestrator.MissionByWorkspaceSession(outcome.WorkspaceSessionID); ok {
		a.refreshWorkspaceProjection(mission)
	}
	a.broadcastSnapshot(outcome.WorkspaceSessionID)
}

func (a *App) finalizeWorkspaceMissionOutcome(outcome *orchestrator.WorkerTurnOutcome) {
	if outcome == nil || outcome.WorkspaceSessionID == "" {
		return
	}
	finalPhase := "completed"
	if outcome.MissionStatus == orchestrator.MissionStatusFailed {
		finalPhase = "failed"
	} else if outcome.MissionStatus == orchestrator.MissionStatusCancelled {
		finalPhase = "aborted"
	}
	a.finishRuntimeTurn(outcome.WorkspaceSessionID, finalPhase)
	a.store.UpsertCard(outcome.WorkspaceSessionID, model.Card{
		ID:        "mission-complete-" + outcome.MissionID,
		Type:      "NoticeCard",
		Title:     "Mission finished",
		Text:      fmt.Sprintf("mission=%s status=%s", outcome.MissionID, outcome.MissionStatus),
		Status:    "notice",
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	})
}

func workerOutcomeFallbackText(status orchestrator.TaskStatus) string {
	switch status {
	case orchestrator.TaskStatusFailed:
		return "worker 任务失败。"
	case orchestrator.TaskStatusCancelled:
		return "worker 任务已取消。"
	default:
		return "worker 已完成当前任务。"
	}
}

func orchestratorTaskTerminal(status orchestrator.TaskStatus) bool {
	switch status {
	case orchestrator.TaskStatusCompleted, orchestrator.TaskStatusFailed, orchestrator.TaskStatusCancelled:
		return true
	default:
		return false
	}
}

func (a *App) latestCompletedAssistantText(sessionID string) string {
	session := a.store.Session(sessionID)
	if session == nil {
		return ""
	}
	for i := len(session.Cards) - 1; i >= 0; i-- {
		card := session.Cards[i]
		if card.Type == "AssistantMessageCard" && normalizeCardStatus(card.Status) == "completed" && strings.TrimSpace(card.Text) != "" {
			return card.Text
		}
	}
	return ""
}

func (a *App) latestCompletedUserText(sessionID string) string {
	session := a.store.Session(sessionID)
	if session == nil {
		return ""
	}
	for i := len(session.Cards) - 1; i >= 0; i-- {
		card := session.Cards[i]
		if card.Type == "UserMessageCard" && normalizeCardStatus(card.Status) == "completed" && strings.TrimSpace(card.Text) != "" {
			return strings.TrimSpace(card.Text)
		}
	}
	return ""
}

func (a *App) replaceLatestCompletedAssistantText(sessionID, text string) {
	session := a.store.Session(sessionID)
	if session == nil {
		return
	}
	trimmed := strings.TrimSpace(text)
	for i := len(session.Cards) - 1; i >= 0; i-- {
		card := session.Cards[i]
		if card.Type != "AssistantMessageCard" || normalizeCardStatus(card.Status) != "completed" {
			continue
		}
		a.store.UpdateCard(sessionID, card.ID, func(target *model.Card) {
			target.Text = trimmed
			target.UpdatedAt = model.NowString()
		})
		return
	}
}

type workspaceRouteDecision struct {
	Route       string `json:"route"`
	Reason      string `json:"reason"`
	TargetHost  string `json:"targetHostId"`
	NeedsPlan   bool   `json:"needsPlan"`
	NeedsWorker bool   `json:"needsWorker"`
}

func parseWorkspaceRouteReply(reply string) (workspaceRouteDecision, string, bool, error) {
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return workspaceRouteDecision{}, "", false, nil
	}
	block := extractWorkspaceJSONBlock(reply)
	if block == "" {
		return workspaceRouteDecision{}, strings.TrimSpace(reply), false, nil
	}
	var decision workspaceRouteDecision
	if err := json.Unmarshal([]byte(block), &decision); err != nil {
		return workspaceRouteDecision{}, "", true, err
	}
	decision.Route = strings.TrimSpace(decision.Route)
	decision.Reason = strings.TrimSpace(decision.Reason)
	if target := strings.TrimSpace(decision.TargetHost); target != "" {
		decision.TargetHost = defaultHostID(target)
	} else {
		decision.TargetHost = ""
	}
	visible := stripWorkspaceJSONBlock(reply)
	return decision, visible, true, nil
}

func parseWorkspaceDispatchRequest(reply string) (orchestrator.DispatchRequest, bool, error) {
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return orchestrator.DispatchRequest{}, false, nil
	}
	block := extractWorkspaceJSONBlock(reply)
	if block == "" {
		return orchestrator.DispatchRequest{}, false, nil
	}
	var req orchestrator.DispatchRequest
	if err := json.Unmarshal([]byte(block), &req); err != nil {
		return orchestrator.DispatchRequest{}, true, err
	}
	return req, true, nil
}

func extractWorkspaceJSONBlock(reply string) string {
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return ""
	}
	if strings.HasPrefix(reply, "{") && strings.HasSuffix(reply, "}") {
		return reply
	}
	if start := strings.Index(reply, "```json"); start >= 0 {
		start += len("```json")
		rest := reply[start:]
		if end := strings.Index(rest, "```"); end >= 0 {
			return strings.TrimSpace(rest[:end])
		}
	}
	if start := strings.Index(reply, "```"); start >= 0 {
		start += len("```")
		rest := reply[start:]
		if end := strings.Index(rest, "```"); end >= 0 {
			return strings.TrimSpace(rest[:end])
		}
	}
	return ""
}

func stripWorkspaceJSONBlock(reply string) string {
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return ""
	}
	if start := strings.Index(reply, "```json"); start >= 0 {
		start += len("```json")
		rest := reply[start:]
		if end := strings.Index(rest, "```"); end >= 0 {
			return strings.TrimSpace(reply[:strings.Index(reply, "```json")] + "\n" + rest[end+3:])
		}
	}
	if start := strings.Index(reply, "```"); start >= 0 {
		start += len("```")
		rest := reply[start:]
		if end := strings.Index(rest, "```"); end >= 0 {
			return strings.TrimSpace(reply[:strings.Index(reply, "```")] + "\n" + rest[end+3:])
		}
	}
	return reply
}

func cardDetailValue(value any) map[string]any {
	if value == nil {
		return nil
	}
	content, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var detail map[string]any
	if err := json.Unmarshal(content, &detail); err != nil {
		return nil
	}
	if len(detail) == 0 {
		return nil
	}
	return detail
}

func workspacePlanDetailValue(detail orchestrator.PlanDetailView) map[string]any {
	return compactDetailMap(map[string]any{
		"title":              strings.TrimSpace(detail.Title),
		"goal":               strings.TrimSpace(detail.Goal),
		"version":            strings.TrimSpace(detail.Version),
		"generatedAt":        strings.TrimSpace(detail.GeneratedAt),
		"ownerSessionLabel":  strings.TrimSpace(detail.OwnerSessionLabel),
		"dagSummary":         detail.DAGSummary,
		"structured_process": append([]string(nil), detail.StructuredProcess...),
		"dispatch_events":    detail.DispatchEvents,
		"task_host_bindings": detail.TaskHostBindings,
	})
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func compactKVRows(rows []model.KeyValueRow) []model.KeyValueRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]model.KeyValueRow, 0, len(rows))
	for _, row := range rows {
		key := strings.TrimSpace(row.Key)
		value := strings.TrimSpace(row.Value)
		if key == "" || value == "" {
			continue
		}
		out = append(out, model.KeyValueRow{Key: key, Value: value})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func missionDispatchEventViews(mission *orchestrator.Mission, hostID string, limit int) []orchestrator.DispatchEventView {
	if mission == nil || len(mission.Events) == 0 || limit <= 0 {
		return nil
	}
	filterHostID := strings.TrimSpace(hostID)
	out := make([]orchestrator.DispatchEventView, 0, limit)
	for i := len(mission.Events) - 1; i >= 0 && len(out) < limit; i-- {
		event := mission.Events[i]
		if filterHostID != "" && strings.TrimSpace(event.HostID) != filterHostID {
			continue
		}
		out = append(out, orchestrator.ProjectDispatchEvent(event))
	}
	slices.Reverse(out)
	return out
}

func missionTaskHostBindings(mission *orchestrator.Mission) []orchestrator.TaskHostBindingView {
	if mission == nil || len(mission.Tasks) == 0 {
		return nil
	}
	taskIDs := make([]string, 0, len(mission.Tasks))
	for taskID := range mission.Tasks {
		taskIDs = append(taskIDs, taskID)
	}
	slices.Sort(taskIDs)
	out := make([]orchestrator.TaskHostBindingView, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		task := mission.Tasks[taskID]
		if task == nil {
			continue
		}
		out = append(out, orchestrator.ProjectTaskHostBinding(task, mission.Workers[task.HostID]))
	}
	return out
}

func conversationExcerptText(card model.Card) string {
	switch card.Type {
	case "UserMessageCard", "AssistantMessageCard", "ErrorCard":
		return firstNonEmptyValue(card.Text, card.Message, card.Summary, card.Title)
	case "CommandCard":
		return firstNonEmptyValue(card.Text, card.Summary, card.Command, card.Stdout, card.Stderr, card.Output)
	case "FileChangeCard":
		return firstNonEmptyValue(card.Text, card.Summary, card.Title)
	case "NoticeCard", "ResultSummaryCard":
		return firstNonEmptyValue(card.Text, card.Summary, card.Title, card.Message)
	default:
		return ""
	}
}

func conversationExcerptSummary(card model.Card) string {
	switch card.Type {
	case "CommandCard":
		return firstNonEmptyValue(card.Summary, card.Command, card.Title)
	case "FileChangeCard":
		return firstNonEmptyValue(card.Summary, card.Title, card.Text)
	default:
		return firstNonEmptyValue(card.Summary, card.Text, card.Message, card.Title)
	}
}

func conversationExcerptRole(card model.Card) string {
	if role := strings.TrimSpace(card.Role); role != "" {
		return role
	}
	switch card.Type {
	case "UserMessageCard":
		return "user"
	case "AssistantMessageCard":
		return "assistant"
	default:
		return "system"
	}
}

func workerConversationExcerpts(sessionID string, cards []model.Card, limit int) []orchestrator.WorkerConversationExcerptView {
	if len(cards) == 0 || limit <= 0 {
		return nil
	}
	out := make([]orchestrator.WorkerConversationExcerptView, 0, limit)
	for i := len(cards) - 1; i >= 0 && len(out) < limit; i-- {
		card := cards[i]
		text := strings.TrimSpace(conversationExcerptText(card))
		if text == "" {
			continue
		}
		switch card.Type {
		case "AssistantMessageCard", "CommandCard", "FileChangeCard", "NoticeCard", "ErrorCard", "ResultSummaryCard":
		default:
			continue
		}
		out = append(out, orchestrator.WorkerConversationExcerptView{
			ID:        strings.TrimSpace(card.ID),
			SessionID: strings.TrimSpace(sessionID),
			Role:      conversationExcerptRole(card),
			Type:      strings.TrimSpace(card.Type),
			Source:    "worker",
			Summary:   truncate(strings.TrimSpace(conversationExcerptSummary(card)), 160),
			Text:      truncate(text, 320),
			CreatedAt: firstNonEmptyValue(card.UpdatedAt, card.CreatedAt),
		})
	}
	slices.Reverse(out)
	return out
}

func latestApprovalRequest(approvals map[string]model.ApprovalRequest) *model.ApprovalRequest {
	if len(approvals) == 0 {
		return nil
	}
	var latest *model.ApprovalRequest
	for _, approval := range approvals {
		current := approval
		if latest == nil || current.RequestedAt > latest.RequestedAt {
			latest = &current
		}
	}
	return latest
}

func approvalTerminalAnchor(cards []model.Card, approval *model.ApprovalRequest) *orchestrator.ApprovalTerminalAnchorView {
	if approval == nil {
		return nil
	}
	build := func(card *model.Card) *orchestrator.ApprovalTerminalAnchorView {
		anchor := &orchestrator.ApprovalTerminalAnchorView{
			ApprovalID: strings.TrimSpace(approval.ID),
			ItemID:     strings.TrimSpace(approval.ItemID),
			HostID:     strings.TrimSpace(approval.HostID),
			Type:       strings.TrimSpace(approval.Type),
			Command:    strings.TrimSpace(approval.Command),
			Summary:    truncate(strings.TrimSpace(firstNonEmptyValue(approval.Reason, approval.Command)), 180),
		}
		if card != nil {
			anchor.SourceCardID = strings.TrimSpace(card.ID)
			anchor.Title = strings.TrimSpace(card.Title)
			if anchor.Command == "" {
				anchor.Command = strings.TrimSpace(card.Command)
			}
			anchor.Cwd = strings.TrimSpace(card.Cwd)
			anchor.Status = strings.TrimSpace(card.Status)
			anchor.Summary = truncate(strings.TrimSpace(firstNonEmptyValue(card.Summary, card.Text, card.Command, anchor.Summary)), 180)
			anchor.CreatedAt = firstNonEmptyValue(card.CreatedAt, approval.RequestedAt)
			anchor.UpdatedAt = firstNonEmptyValue(card.UpdatedAt, approval.ResolvedAt, approval.RequestedAt)
		} else {
			anchor.CreatedAt = strings.TrimSpace(approval.RequestedAt)
			anchor.UpdatedAt = firstNonEmptyValue(approval.ResolvedAt, approval.RequestedAt)
		}
		return anchor
	}

	var fallback *model.Card
	for i := len(cards) - 1; i >= 0; i-- {
		card := cards[i]
		switch card.Type {
		case "CommandCard", "FileChangeCard", "ProcessLineCard":
		default:
			continue
		}
		copyCard := card
		if strings.TrimSpace(approval.ItemID) != "" && strings.TrimSpace(copyCard.ID) == strings.TrimSpace(approval.ItemID) {
			return build(&copyCard)
		}
		if strings.TrimSpace(approval.Command) != "" && strings.TrimSpace(copyCard.Command) == strings.TrimSpace(approval.Command) {
			return build(&copyCard)
		}
		if fallback == nil {
			fallback = &copyCard
		}
	}
	return build(fallback)
}

func (a *App) buildWorkspacePlanDetail(mission *orchestrator.Mission) orchestrator.PlanDetailView {
	detail := orchestrator.ProjectPlanDetail(mission)
	if mission == nil {
		return detail
	}
	detail.DispatchEvents = missionDispatchEventViews(mission, "", 24)
	detail.TaskHostBindings = missionTaskHostBindings(mission)
	return detail
}

func taskIDOrEmpty(task *orchestrator.TaskRun) string {
	if task == nil {
		return ""
	}
	return strings.TrimSpace(task.ID)
}

func missionStatusLabel(status orchestrator.MissionStatus) string {
	switch status {
	case orchestrator.MissionStatusCompleted:
		return "已完成"
	case orchestrator.MissionStatusFailed:
		return "失败"
	case orchestrator.MissionStatusCancelled:
		return "已取消"
	case orchestrator.MissionStatusPaused:
		return "暂停"
	default:
		return "进行中"
	}
}

func workerStatusLabel(status orchestrator.WorkerStatus) string {
	switch status {
	case orchestrator.WorkerStatusCompleted:
		return "已完成"
	case orchestrator.WorkerStatusFailed:
		return "失败"
	case orchestrator.WorkerStatusCancelled:
		return "已取消"
	case orchestrator.WorkerStatusWaiting:
		return "等待审批/输入"
	case orchestrator.WorkerStatusQueued:
		return "排队中"
	default:
		return "执行中"
	}
}

func hostDisplayName(host model.Host) string {
	if name := strings.TrimSpace(host.Name); name != "" {
		return name
	}
	return strings.TrimSpace(host.ID)
}

func workerFocusTask(mission *orchestrator.Mission, worker *orchestrator.HostWorker) *orchestrator.TaskRun {
	if mission == nil || worker == nil {
		return nil
	}
	if task := mission.Tasks[worker.ActiveTaskID]; task != nil {
		return task
	}
	var latest *orchestrator.TaskRun
	for _, task := range mission.Tasks {
		if task == nil || task.HostID != worker.HostID {
			continue
		}
		if latest == nil || task.UpdatedAt > latest.UpdatedAt {
			latest = task
		}
	}
	return latest
}

func latestMissionHostEvents(mission *orchestrator.Mission, hostID string, limit int) []string {
	if mission == nil || limit <= 0 {
		return nil
	}
	out := make([]string, 0, limit)
	for i := len(mission.Events) - 1; i >= 0 && len(out) < limit; i-- {
		event := mission.Events[i]
		if strings.TrimSpace(event.HostID) != strings.TrimSpace(hostID) {
			continue
		}
		text := firstNonEmptyValue(event.Summary, event.Detail)
		if strings.TrimSpace(text) == "" {
			continue
		}
		out = append(out, text)
	}
	slices.Reverse(out)
	return compactStrings(out)
}

func latestWorkerTranscript(cards []model.Card, limit int) []string {
	if len(cards) == 0 || limit <= 0 {
		return nil
	}
	lines := make([]string, 0, limit)
	for i := len(cards) - 1; i >= 0 && len(lines) < limit; i-- {
		card := cards[i]
		var line string
		switch card.Type {
		case "AssistantMessageCard":
			line = firstNonEmptyValue(card.Text, card.Summary)
		case "CommandCard":
			line = firstNonEmptyValue(card.Summary, card.Command)
		case "FileChangeCard":
			line = firstNonEmptyValue(card.Summary, card.Title)
		case "NoticeCard", "ErrorCard":
			line = firstNonEmptyValue(card.Text, card.Title)
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, truncate(strings.TrimSpace(line), 180))
	}
	slices.Reverse(lines)
	return compactStrings(lines)
}

func latestTerminalCard(cards []model.Card) *model.Card {
	for i := len(cards) - 1; i >= 0; i-- {
		card := cards[i]
		if card.Type == "CommandCard" {
			copyCard := card
			return &copyCard
		}
	}
	return nil
}

func latestApprovalDetail(approvals map[string]model.ApprovalRequest) map[string]any {
	latest := latestApprovalRequest(approvals)
	if latest == nil {
		return nil
	}
	return compactDetailMap(map[string]any{
		"id":     latest.ID,
		"type":   latest.Type,
		"status": latest.Status,
		"reason": latest.Reason,
		"hostId": latest.HostID,
		"itemId": latest.ItemID,
		"command": func() any {
			if strings.TrimSpace(latest.Command) != "" {
				return latest.Command
			}
			if len(latest.Changes) > 0 {
				return latest.Changes[0].Path
			}
			return nil
		}(),
	})
}

func compactDetailMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		switch v := value.(type) {
		case string:
			if strings.TrimSpace(v) == "" {
				continue
			}
			out[key] = v
		case nil:
			continue
		default:
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (a *App) buildWorkerReadonlyDetail(worker *orchestrator.HostWorker) orchestrator.WorkerReadonlyDetailView {
	view := orchestrator.ProjectWorkerReadonlyDetail(worker)
	if worker == nil {
		return view
	}
	session := a.store.Session(worker.SessionID)
	if session == nil {
		return view
	}
	view.Transcript = latestWorkerTranscript(session.Cards, 6)
	view.Conversation = workerConversationExcerpts(worker.SessionID, session.Cards, 12)
	if terminal := latestTerminalCard(session.Cards); terminal != nil {
		view.Terminal = compactDetailMap(map[string]any{
			"id":        terminal.ID,
			"title":     terminal.Title,
			"command":   terminal.Command,
			"cwd":       terminal.Cwd,
			"status":    terminal.Status,
			"exitCode":  terminal.ExitCode,
			"output":    firstNonEmptyValue(terminal.Output, terminal.Stdout, terminal.Stderr),
			"stdout":    terminal.Stdout,
			"stderr":    terminal.Stderr,
			"summary":   firstNonEmptyValue(terminal.Summary, terminal.Text),
			"createdAt": terminal.CreatedAt,
			"updatedAt": terminal.UpdatedAt,
		})
	}
	view.Approval = latestApprovalDetail(session.Approvals)
	view.ApprovalAnchor = approvalTerminalAnchor(session.Cards, latestApprovalRequest(session.Approvals))
	return view
}

func missionWorkerHighlights(mission *orchestrator.Mission) []string {
	if mission == nil || len(mission.Workers) == 0 {
		return nil
	}
	hostIDs := make([]string, 0, len(mission.Workers))
	for hostID := range mission.Workers {
		hostIDs = append(hostIDs, hostID)
	}
	slices.Sort(hostIDs)
	out := make([]string, 0, len(hostIDs))
	for _, hostID := range hostIDs {
		worker := mission.Workers[hostID]
		if worker == nil {
			continue
		}
		out = append(out, fmt.Sprintf("%s: %s", hostID, workerStatusLabel(worker.Status)))
	}
	return out
}

func (a *App) missionWorkspaceItems(mission *orchestrator.Mission) []model.FileItem {
	if mission == nil || len(mission.Workers) == 0 {
		return nil
	}
	hostIDs := make([]string, 0, len(mission.Workers))
	for hostID := range mission.Workers {
		hostIDs = append(hostIDs, hostID)
	}
	slices.Sort(hostIDs)
	items := make([]model.FileItem, 0, len(hostIDs)*2)
	for _, hostID := range hostIDs {
		items = append(items, model.FileItem{
			Label:   hostID + " remote workspace",
			Path:    "remote://" + hostID + "/" + orchestrator.WorkerRemoteWorkspacePath(orchestratorRemoteWorkspaceRoot, mission.ID, hostID),
			Kind:    "dir",
			Meta:    workerStatusLabel(mission.Workers[hostID].Status),
			Preview: strings.Join(latestMissionHostEvents(mission, hostID, 2), "\n"),
		})
		items = append(items, model.FileItem{
			Label: hostID + " local workspace",
			Path:  orchestrator.WorkerLocalWorkspacePath(a.cfg.DefaultWorkspace, mission.ID, hostID),
			Kind:  "dir",
			Meta:  "local",
		})
	}
	return items
}

func (a *App) upsertWorkspaceMissionCard(workspaceSessionID string, mission *orchestrator.Mission) {
	if mission == nil {
		return
	}
	view := orchestrator.ProjectMissionCard(mission)
	a.store.UpsertCard(workspaceSessionID, model.Card{
		ID:      view.ID,
		Type:    "NoticeCard",
		Title:   view.Label,
		Text:    view.Caption,
		Summary: fmt.Sprintf("状态 %s，任务 %d 项", missionStatusLabel(mission.Status), view.StepCount),
		Status:  normalizeCardStatus(view.Status),
		KVRows: []model.KeyValueRow{
			{Key: "Mission", Value: mission.ID},
			{Key: "状态", Value: missionStatusLabel(mission.Status)},
			{Key: "任务数", Value: strconv.Itoa(view.StepCount)},
		},
		Highlights: compactStrings([]string{
			firstNonEmptyValue(mission.Title, mission.Summary),
			"工作台只读投影",
		}),
		Detail:    cardDetailValue(view),
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	})
}

func (a *App) refreshWorkspaceProjection(mission *orchestrator.Mission) {
	if mission == nil || mission.WorkspaceSessionID == "" {
		return
	}
	a.upsertWorkspaceMissionCard(mission.WorkspaceSessionID, mission)
	a.upsertWorkspacePlanCard(mission.WorkspaceSessionID, mission)
	a.upsertWorkspaceWorkerCards(mission.WorkspaceSessionID, mission)
	a.upsertWorkspaceResultCard(mission.WorkspaceSessionID, mission)
	a.broadcastSnapshot(mission.WorkspaceSessionID)
}

func (a *App) upsertWorkspacePlanCard(workspaceSessionID string, mission *orchestrator.Mission) {
	if mission == nil {
		return
	}
	planSummary := orchestrator.ProjectPlanSummary(mission)
	planDetail := a.buildWorkspacePlanDetail(mission)
	keys := make([]string, 0, len(mission.Tasks))
	for id := range mission.Tasks {
		keys = append(keys, id)
	}
	slices.Sort(keys)
	items := make([]model.PlanItem, 0, len(keys))
	for _, id := range keys {
		task := mission.Tasks[id]
		if task == nil {
			continue
		}
		step := firstNonEmptyValue(task.Title, task.Instruction, id)
		items = append(items, model.PlanItem{
			Step:   fmt.Sprintf("%s [%s] %s", task.HostID, id, truncate(strings.TrimSpace(step), 120)),
			Status: planItemStatus(task.Status),
		})
	}
	a.store.UpsertCard(workspaceSessionID, model.Card{
		ID:      "workspace-plan-" + mission.ID,
		Type:    "PlanCard",
		Title:   firstNonEmptyValue(planSummary.Label, mission.Title),
		Text:    firstNonEmptyValue(planDetail.Goal, planSummary.Caption),
		Summary: firstNonEmptyValue(planSummary.Caption, "工作台只展示计划摘要。"),
		Items:   items,
		Status:  normalizeCardStatus(string(mission.Status)),
		KVRows: []model.KeyValueRow{
			{Key: "节点", Value: strconv.Itoa(planDetail.DAGSummary.Nodes)},
			{Key: "运行中", Value: strconv.Itoa(planDetail.DAGSummary.Running)},
			{Key: "待审批", Value: strconv.Itoa(planDetail.DAGSummary.WaitingApproval)},
			{Key: "排队", Value: strconv.Itoa(planDetail.DAGSummary.Queued)},
		},
		Highlights: compactStrings([]string{
			planDetail.OwnerSessionLabel,
			firstNonEmptyValue(planDetail.Version, "plan-v1"),
		}),
		Detail:    workspacePlanDetailValue(planDetail),
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	})
}

func (a *App) upsertWorkspaceWorkerCards(workspaceSessionID string, mission *orchestrator.Mission) {
	if mission == nil {
		return
	}
	keys := make([]string, 0, len(mission.Workers))
	for hostID := range mission.Workers {
		keys = append(keys, hostID)
	}
	slices.Sort(keys)
	for _, hostID := range keys {
		worker := mission.Workers[hostID]
		if worker == nil {
			continue
		}
		task := workerFocusTask(mission, worker)
		text := workspaceWorkerSummary(hostID, worker, task)
		dispatchDetail := orchestrator.ProjectDispatchHostDetail(task, worker)
		dispatchDetail.Host = hostDisplayName(a.findHost(hostID))
		dispatchDetail.Events = latestMissionHostEvents(mission, hostID, 4)
		dispatchDetail.Timeline = missionDispatchEventViews(mission, hostID, 8)
		workerDetail := a.buildWorkerReadonlyDetail(worker)
		status := "inProgress"
		switch worker.Status {
		case orchestrator.WorkerStatusCompleted:
			status = "completed"
		case orchestrator.WorkerStatusFailed, orchestrator.WorkerStatusCancelled:
			status = "failed"
		}
		a.store.UpsertCard(workspaceSessionID, model.Card{
			ID:      "workspace-worker-" + hostID,
			Type:    "ProcessLineCard",
			Title:   dispatchDetail.Host,
			Text:    text,
			Summary: firstNonEmptyValue(dispatchDetail.Request.Summary, dispatchDetail.Request.Title),
			Status:  status,
			HostID:  hostID,
			KVRows: compactKVRows([]model.KeyValueRow{
				{Key: "主机", Value: dispatchDetail.Host},
				{Key: "状态", Value: workerStatusLabel(worker.Status)},
				{Key: "任务", Value: firstNonEmptyValue(dispatchDetail.Request.Title, taskIDOrEmpty(task))},
				{Key: "排队", Value: strconv.Itoa(len(worker.QueueTaskIDs))},
				{Key: "WorkerSession", Value: worker.SessionID},
			}),
			Highlights: compactStrings(dispatchDetail.Events),
			Detail: map[string]any{
				"dispatch": cardDetailValue(dispatchDetail),
				"worker":   cardDetailValue(workerDetail),
			},
			CreatedAt: model.NowString(),
			UpdatedAt: model.NowString(),
		})
	}
}

func (a *App) upsertWorkspaceResultCard(workspaceSessionID string, mission *orchestrator.Mission) {
	if mission == nil {
		return
	}
	status := strings.TrimSpace(string(mission.Status))
	if status != string(orchestrator.MissionStatusCompleted) && status != string(orchestrator.MissionStatusFailed) && status != string(orchestrator.MissionStatusCancelled) {
		return
	}
	completed := 0
	failed := 0
	cancelled := 0
	for _, task := range mission.Tasks {
		if task == nil {
			continue
		}
		switch task.Status {
		case orchestrator.TaskStatusCompleted:
			completed++
		case orchestrator.TaskStatusFailed:
			failed++
		case orchestrator.TaskStatusCancelled:
			cancelled++
		}
	}
	a.store.UpsertCard(workspaceSessionID, model.Card{
		ID:      "workspace-result-" + mission.ID,
		Type:    "ResultSummaryCard",
		Title:   firstNonEmptyValue(mission.Title, "Mission result"),
		Summary: fmt.Sprintf("mission=%s status=%s", mission.ID, mission.Status),
		Text:    firstNonEmptyValue(mission.Summary, "工作台 mission 已结束。"),
		Status:  normalizeCardStatus(status),
		KVRows: []model.KeyValueRow{
			{Key: "完成", Value: fmt.Sprintf("%d", completed)},
			{Key: "失败", Value: fmt.Sprintf("%d", failed)},
			{Key: "取消", Value: fmt.Sprintf("%d", cancelled)},
			{Key: "Worker", Value: strconv.Itoa(len(mission.Workers))},
		},
		Highlights: missionWorkerHighlights(mission),
		FileItems:  a.missionWorkspaceItems(mission),
		Detail: map[string]any{
			"mission": cardDetailValue(orchestrator.ProjectMissionCard(mission)),
			"plan":    workspacePlanDetailValue(a.buildWorkspacePlanDetail(mission)),
		},
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	})
}

func (a *App) relaySnapshotToOrchestrator(sessionID string, snapshot model.Snapshot) {
	if a.orchestrator == nil {
		return
	}
	meta := a.sessionMeta(sessionID)
	if strings.TrimSpace(meta.MissionID) != "" {
		if _, ok := a.orchestrator.MissionBySession(sessionID); !ok {
			if meta.Kind != model.SessionKindWorkspace {
				return
			}
			if _, ok := a.orchestrator.MissionByWorkspaceSession(sessionID); !ok {
				return
			}
		}
	}
	relay := orchestrator.Snapshot{
		SessionID: sessionID,
		Kind:      orchestrator.SessionKind(meta.Kind),
		Visible:   meta.Visible,
		MissionID: meta.MissionID,
		Status:    snapshot.Runtime.Turn.Phase,
		Summary:   a.latestCompletedAssistantText(sessionID),
		UpdatedAt: time.Now().UTC(),
	}
	if err := a.orchestrator.OnSnapshot(sessionID, relay); err != nil {
		log.Printf("orchestrator snapshot relay failed session=%s err=%v", sessionID, err)
	}
}

func planItemStatus(status orchestrator.TaskStatus) string {
	switch status {
	case orchestrator.TaskStatusRunning, orchestrator.TaskStatusWaitingApproval, orchestrator.TaskStatusWaitingInput, orchestrator.TaskStatusDispatching:
		return "inProgress"
	case orchestrator.TaskStatusCompleted:
		return "completed"
	default:
		return "pending"
	}
}

func workspaceWorkerSummary(hostID string, worker *orchestrator.HostWorker, activeTask *orchestrator.TaskRun) string {
	if worker == nil {
		return hostID
	}
	activeLabel := worker.ActiveTaskID
	if activeTask != nil {
		activeLabel = firstNonEmptyValue(activeTask.Title, activeTask.Instruction, activeTask.ID)
	}
	switch worker.Status {
	case orchestrator.WorkerStatusCompleted:
		return fmt.Sprintf("%s: 已完成 %s", hostID, activeLabel)
	case orchestrator.WorkerStatusFailed:
		return fmt.Sprintf("%s: 执行失败 %s", hostID, activeLabel)
	case orchestrator.WorkerStatusCancelled:
		return fmt.Sprintf("%s: 已取消 %s", hostID, activeLabel)
	case orchestrator.WorkerStatusWaiting:
		return fmt.Sprintf("%s: 等待审批或输入 %s", hostID, activeLabel)
	default:
		if len(worker.QueueTaskIDs) > 0 {
			return fmt.Sprintf("%s: 正在执行 %s，排队 %d 项", hostID, activeLabel, len(worker.QueueTaskIDs))
		}
		if activeLabel != "" {
			return fmt.Sprintf("%s: 正在执行 %s", hostID, activeLabel)
		}
		return fmt.Sprintf("%s: 已就绪", hostID)
	}
}

func firstNonEmptyValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func approvalRequestSummaryText(host model.Host, approval model.ApprovalRequest) string {
	hostName := strings.TrimSpace(host.Name)
	if hostName == "" {
		hostName = strings.TrimSpace(approval.HostID)
	}
	if hostName == "" {
		hostName = "当前主机"
	}

	switch approval.Type {
	case "remote_command", "command":
		if strings.TrimSpace(approval.Command) != "" {
			return fmt.Sprintf("%s 等待命令审批：%s", hostName, truncate(approval.Command, 88))
		}
	case "remote_file_change", "file_change":
		if len(approval.Changes) == 1 {
			return fmt.Sprintf("%s 等待文件审批：%s", hostName, truncate(approval.Changes[0].Path, 88))
		}
		if len(approval.Changes) > 1 {
			return fmt.Sprintf("%s 等待 %d 个文件变更审批", hostName, len(approval.Changes))
		}
	}
	return hostName + " 等待审批"
}

func approvalDecisionForStatus(status string) string {
	status = strings.TrimSpace(status)
	switch {
	case strings.HasPrefix(status, "accept"):
		if strings.Contains(status, "session") {
			return "accept_session"
		}
		return "accept"
	case strings.HasPrefix(status, "decl"), status == "rejected":
		return "decline"
	default:
		return "accept"
	}
}
