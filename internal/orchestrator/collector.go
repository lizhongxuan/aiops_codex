package orchestrator

import (
	"fmt"
	"strings"
)

type Collector struct {
	store *Store
}

type sessionEventContext struct {
	missionID string
	meta      SessionMeta
	hostID    string
	taskID    string
}

func newCollector(store *Store) *Collector {
	return &Collector{store: store}
}

func (c *Collector) RecordEvent(event RelayEvent) (bool, error) {
	if c == nil || c.store == nil {
		return false, nil
	}
	missionID := strings.TrimSpace(event.MissionID)
	if missionID == "" {
		return false, nil
	}
	if event.ID == "" {
		event.ID = newEventID(string(event.Type))
	}
	if event.CreatedAt == "" {
		event.CreatedAt = nowString()
	}
	_, err := c.store.UpdateMission(missionID, func(m *Mission) error {
		m.Events = append(m.Events, event)
		if len(m.Events) > DefaultEventWindowSize {
			m.Events = append([]RelayEvent(nil), m.Events[len(m.Events)-DefaultEventWindowSize:]...)
		}
		m.UpdatedAt = event.CreatedAt
		return nil
	})
	return err == nil, err
}

func (c *Collector) OnTurnPhase(sessionID, phase string) (bool, error) {
	ctx, ok := c.resolveSessionContext(sessionID)
	if !ok {
		return false, nil
	}
	phase = strings.TrimSpace(phase)
	if phase == "" {
		return false, nil
	}
	_, changed := c.store.UpdateSessionSeenState(sessionID, func(state *WorkerSeenState) bool {
		if state.LastTurnPhase == phase {
			return false
		}
		state.LastTurnPhase = phase
		return true
	})
	if !changed {
		return false, nil
	}
	return c.RecordEvent(RelayEvent{
		MissionID: ctx.missionID,
		TaskID:    ctx.taskID,
		HostID:    ctx.hostID,
		SessionID: sessionID,
		Type:      phaseEventType(phase),
		Status:    phase,
		Summary:   phaseSummary(ctx, phase),
		Detail:    phaseDetail(ctx, phase),
	})
}

func (c *Collector) OnReply(sessionID, reply string) (bool, error) {
	ctx, ok := c.resolveSessionContext(sessionID)
	if !ok {
		return false, nil
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return false, nil
	}
	_, changed := c.store.UpdateSessionSeenState(sessionID, func(state *WorkerSeenState) bool {
		if state.LastReplyDigest == reply {
			return false
		}
		state.LastReplyDigest = reply
		return true
	})
	if !changed {
		return false, nil
	}
	return c.RecordEvent(RelayEvent{
		MissionID: ctx.missionID,
		TaskID:    ctx.taskID,
		HostID:    ctx.hostID,
		SessionID: sessionID,
		Type:      EventTypeReply,
		Status:    "completed",
		Summary:   summarizeText(reply, 96),
		Detail:    summarizeText(reply, 400),
	})
}

func (c *Collector) OnApprovalRequested(sessionID, approvalID, summary, detail string) (bool, error) {
	return c.recordApprovalEvent(sessionID, approvalID, "pending", EventTypeApprovalRequested, summary, detail)
}

func (c *Collector) OnApprovalResolved(sessionID, approvalID, status, summary string) (bool, error) {
	return c.recordApprovalEvent(sessionID, approvalID, strings.TrimSpace(status), EventTypeApprovalResolved, summary, "")
}

func (c *Collector) OnChoiceRequested(sessionID, choiceID, summary string) (bool, error) {
	return c.recordChoiceEvent(sessionID, choiceID, "pending", EventTypeChoiceRequested, summary)
}

func (c *Collector) OnChoiceResolved(sessionID, choiceID, summary string) (bool, error) {
	return c.recordChoiceEvent(sessionID, choiceID, "resolved", EventTypeChoiceResolved, summary)
}

func (c *Collector) OnRemoteExecStarted(sessionID, hostID, cardID, command string) (bool, error) {
	return c.recordExecEvent(sessionID, hostID, cardID, "started", EventTypeExecStarted, command, "")
}

func (c *Collector) OnRemoteExecFinished(sessionID, hostID, cardID, status, command, detail string) (bool, error) {
	return c.recordExecEvent(sessionID, hostID, cardID, strings.TrimSpace(status), EventTypeExecFinished, command, detail)
}

func (c *Collector) OnSnapshot(sessionID string, snapshot Snapshot) (bool, error) {
	if c == nil || c.store == nil {
		return false, nil
	}
	if snapshot.SessionID == "" {
		snapshot.SessionID = strings.TrimSpace(sessionID)
	}
	if snapshot.SessionID == "" {
		return false, nil
	}
	phaseChanged, err := c.OnTurnPhase(snapshot.SessionID, snapshot.Status)
	if err != nil {
		return false, err
	}
	replyChanged, err := c.OnReply(snapshot.SessionID, snapshot.Summary)
	if err != nil {
		return false, err
	}
	return phaseChanged || replyChanged, nil
}

func (c *Collector) recordApprovalEvent(sessionID, approvalID, status string, eventType EventType, summary, detail string) (bool, error) {
	ctx, ok := c.resolveSessionContext(sessionID)
	if !ok {
		return false, nil
	}
	approvalID = strings.TrimSpace(approvalID)
	status = strings.TrimSpace(status)
	if approvalID == "" || status == "" {
		return false, nil
	}
	_, changed := c.store.UpdateSessionSeenState(sessionID, func(state *WorkerSeenState) bool {
		if state.SeenApprovalStatus[approvalID] == status {
			return false
		}
		state.SeenApprovalStatus[approvalID] = status
		return true
	})
	if !changed {
		return false, nil
	}
	return c.RecordEvent(RelayEvent{
		MissionID:  ctx.missionID,
		TaskID:     ctx.taskID,
		HostID:     ctx.hostID,
		SessionID:  sessionID,
		Type:       eventType,
		Status:     status,
		Summary:    firstNonEmpty(summary, approvalSummary(ctx, status)),
		Detail:     detail,
		ApprovalID: approvalID,
	})
}

func (c *Collector) recordChoiceEvent(sessionID, choiceID, status string, eventType EventType, summary string) (bool, error) {
	ctx, ok := c.resolveSessionContext(sessionID)
	if !ok {
		return false, nil
	}
	choiceID = strings.TrimSpace(choiceID)
	status = strings.TrimSpace(status)
	if choiceID == "" || status == "" {
		return false, nil
	}
	_, changed := c.store.UpdateSessionSeenState(sessionID, func(state *WorkerSeenState) bool {
		if state.SeenChoiceStatus[choiceID] == status {
			return false
		}
		state.SeenChoiceStatus[choiceID] = status
		return true
	})
	if !changed {
		return false, nil
	}
	return c.RecordEvent(RelayEvent{
		MissionID: ctx.missionID,
		TaskID:    ctx.taskID,
		HostID:    ctx.hostID,
		SessionID: sessionID,
		Type:      eventType,
		Status:    status,
		Summary:   firstNonEmpty(summary, choiceSummary(ctx, status)),
		Detail:    summarizeText(summary, 400),
	})
}

func (c *Collector) recordExecEvent(sessionID, hostID, cardID, status string, eventType EventType, command, detail string) (bool, error) {
	ctx, ok := c.resolveSessionContext(sessionID)
	if !ok {
		return false, nil
	}
	cardID = strings.TrimSpace(cardID)
	status = strings.TrimSpace(status)
	if cardID == "" || status == "" {
		return false, nil
	}
	_, changed := c.store.UpdateSessionSeenState(sessionID, func(state *WorkerSeenState) bool {
		if state.SeenExecStatus[cardID] == status {
			return false
		}
		state.SeenExecStatus[cardID] = status
		return true
	})
	if !changed {
		return false, nil
	}
	if hostID == "" {
		hostID = ctx.hostID
	}
	return c.RecordEvent(RelayEvent{
		MissionID:    ctx.missionID,
		TaskID:       ctx.taskID,
		HostID:       hostID,
		SessionID:    sessionID,
		Type:         eventType,
		Status:       status,
		Summary:      execSummary(ctx, status, command),
		Detail:       summarizeText(detail, 400),
		SourceCardID: cardID,
	})
}

func (c *Collector) resolveSessionContext(sessionID string) (sessionEventContext, bool) {
	if c == nil || c.store == nil {
		return sessionEventContext{}, false
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return sessionEventContext{}, false
	}
	meta, ok := c.store.SessionMeta(sessionID)
	if !ok || strings.TrimSpace(meta.MissionID) == "" {
		return sessionEventContext{}, false
	}
	ctx := sessionEventContext{
		missionID: meta.MissionID,
		meta:      meta,
		hostID:    strings.TrimSpace(meta.WorkerHostID),
	}
	mission, ok := c.store.Mission(meta.MissionID)
	if !ok || mission == nil {
		return sessionEventContext{}, false
	}
	if meta.Kind == SessionKindWorker {
		for _, worker := range mission.Workers {
			if worker == nil || worker.SessionID != sessionID {
				continue
			}
			ctx.hostID = firstNonEmpty(worker.HostID, ctx.hostID)
			ctx.taskID = strings.TrimSpace(worker.ActiveTaskID)
			break
		}
	}
	return ctx, true
}

func phaseEventType(phase string) EventType {
	switch strings.TrimSpace(phase) {
	case "completed":
		return EventTypeCompleted
	case "aborted", "cancelled":
		return EventTypeCancelled
	case "failed":
		return EventTypeFailed
	default:
		return EventTypeProgress
	}
}

func phaseSummary(ctx sessionEventContext, phase string) string {
	subject := sessionSubject(ctx)
	switch strings.TrimSpace(phase) {
	case "planning":
		return subject + " 进入规划阶段"
	case "waiting_approval":
		return subject + " 等待审批"
	case "waiting_input":
		return subject + " 等待输入"
	case "executing":
		return subject + " 正在执行"
	case "finalizing":
		return subject + " 正在收尾"
	case "completed":
		return subject + " 执行完成"
	case "failed":
		return subject + " 执行失败"
	case "aborted", "cancelled":
		return subject + " 已取消"
	default:
		return subject + " 状态更新为 " + phase
	}
}

func phaseDetail(ctx sessionEventContext, phase string) string {
	if ctx.taskID == "" {
		return phase
	}
	return fmt.Sprintf("task=%s phase=%s", ctx.taskID, phase)
}

func approvalSummary(ctx sessionEventContext, status string) string {
	if strings.HasPrefix(status, "accept") {
		return sessionSubject(ctx) + " 审批已通过"
	}
	if strings.HasPrefix(status, "decl") || status == "rejected" {
		return sessionSubject(ctx) + " 审批已拒绝"
	}
	return sessionSubject(ctx) + " 发起审批"
}

func choiceSummary(ctx sessionEventContext, status string) string {
	if status == "resolved" {
		return sessionSubject(ctx) + " 输入已提交"
	}
	return sessionSubject(ctx) + " 请求补充输入"
}

func execSummary(ctx sessionEventContext, status, command string) string {
	command = summarizeText(command, 72)
	if command == "" {
		command = "命令"
	}
	switch status {
	case "started":
		return fmt.Sprintf("%s 启动远程执行: %s", sessionSubject(ctx), command)
	case "completed":
		return fmt.Sprintf("%s 远程执行完成: %s", sessionSubject(ctx), command)
	case "cancelled":
		return fmt.Sprintf("%s 远程执行已取消: %s", sessionSubject(ctx), command)
	default:
		return fmt.Sprintf("%s 远程执行状态=%s: %s", sessionSubject(ctx), status, command)
	}
}

func sessionSubject(ctx sessionEventContext) string {
	if ctx.meta.Kind == SessionKindPlanner {
		return "Planner"
	}
	if ctx.hostID != "" {
		return "Worker[" + ctx.hostID + "]"
	}
	return "Worker"
}

func summarizeText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if text == "" || limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}
