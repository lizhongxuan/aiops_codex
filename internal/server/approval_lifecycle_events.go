package server

import (
	"context"
	"log"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type approvalRequestedEventOptions struct {
	activateQueuedWorkers bool
}

func (a *App) emitApprovalRequestedEvent(ctx context.Context, sessionID, toolName string, approval model.ApprovalRequest, card model.Card) bool {
	return a.emitApprovalRequestedEventWithOptions(ctx, sessionID, toolName, approval, card, approvalRequestedEventOptions{})
}

func (a *App) emitApprovalRequestedEventWithOptions(ctx context.Context, sessionID, toolName string, approval model.ApprovalRequest, card model.Card, opts approvalRequestedEventOptions) bool {
	if a == nil || a.toolEventBus == nil {
		return false
	}
	event := newApprovalRequestedLifecycleEvent(sessionID, toolName, approval, card)
	enrichApprovalRequestedLifecycleEvent(&event, a.sessionMeta(sessionID), opts)
	if err := a.toolEventBus.Emit(ctx, event); err != nil {
		log.Printf("failed to emit approval requested event session=%s approval=%s tool=%s err=%v", sessionID, approval.ID, toolName, err)
		return false
	}
	return true
}

func enrichApprovalRequestedLifecycleEvent(event *ToolLifecycleEvent, meta model.SessionMeta, opts approvalRequestedEventOptions) {
	if event == nil {
		return
	}
	if event.Payload == nil {
		event.Payload = make(map[string]any)
	}
	if kind := strings.TrimSpace(meta.Kind); kind != "" {
		event.Payload["sessionKind"] = kind
	}
	if workspaceSessionID := strings.TrimSpace(meta.WorkspaceSessionID); workspaceSessionID != "" {
		event.Payload["workspaceSessionId"] = workspaceSessionID
	}
	if opts.activateQueuedWorkers {
		event.Payload["activateQueuedWorkers"] = true
	}
}

func (a *App) emitApprovalResolvedEvent(ctx context.Context, sessionID, toolName, phase string, approval model.ApprovalRequest, card model.Card) bool {
	if a == nil || a.toolEventBus == nil {
		return false
	}
	event := newApprovalResolvedLifecycleEvent(sessionID, toolName, phase, approval, card)
	if err := a.toolEventBus.Emit(ctx, event); err != nil {
		log.Printf("failed to emit approval resolved event session=%s approval=%s tool=%s err=%v", sessionID, approval.ID, toolName, err)
		return false
	}
	return true
}

func newApprovalRequestedLifecycleEvent(sessionID, toolName string, approval model.ApprovalRequest, card model.Card) ToolLifecycleEvent {
	return ToolLifecycleEvent{
		EventID:    model.NewID("toolevent"),
		SessionID:  sessionID,
		ToolName:   firstNonEmptyValue(strings.TrimSpace(toolName), approvalEventToolName(approval.Type)),
		Type:       ToolLifecycleEventApprovalRequested,
		Phase:      "waiting_approval",
		HostID:     defaultHostID(approval.HostID),
		CardID:     card.ID,
		ApprovalID: approval.ID,
		Label:      firstNonEmptyValue(strings.TrimSpace(card.Title), strings.TrimSpace(approval.Reason), strings.TrimSpace(approval.Command), "需要审批"),
		Message:    firstNonEmptyValue(strings.TrimSpace(card.Text), strings.TrimSpace(approval.Reason), strings.TrimSpace(approval.Command), "需要审批"),
		CreatedAt:  firstNonEmptyValue(strings.TrimSpace(approval.RequestedAt), strings.TrimSpace(card.CreatedAt), model.NowString()),
		Payload: map[string]any{
			"approval": approvalLifecycleEventPayload(approval),
			"card":     approvalLifecycleCardPayload(card),
		},
	}
}

func newApprovalResolvedLifecycleEvent(sessionID, toolName, phase string, approval model.ApprovalRequest, card model.Card) ToolLifecycleEvent {
	return ToolLifecycleEvent{
		EventID:    model.NewID("toolevent"),
		SessionID:  sessionID,
		ToolName:   firstNonEmptyValue(strings.TrimSpace(toolName), approvalEventToolName(approval.Type)),
		Type:       ToolLifecycleEventApprovalResolved,
		Phase:      firstNonEmptyValue(strings.TrimSpace(phase), "thinking"),
		HostID:     defaultHostID(approval.HostID),
		CardID:     card.ID,
		ApprovalID: approval.ID,
		Label:      firstNonEmptyValue(strings.TrimSpace(card.Title), strings.TrimSpace(card.Text), strings.TrimSpace(approval.Reason), "审批已处理"),
		Message:    firstNonEmptyValue(strings.TrimSpace(card.Text), strings.TrimSpace(card.Title), strings.TrimSpace(approval.Reason), "审批已处理"),
		CreatedAt:  firstNonEmptyValue(strings.TrimSpace(approval.ResolvedAt), strings.TrimSpace(card.UpdatedAt), model.NowString()),
		Payload: map[string]any{
			"approval": approvalLifecycleEventPayload(approval),
			"card":     approvalLifecycleCardPayload(card),
		},
	}
}

func approvalLifecycleEventPayload(approval model.ApprovalRequest) map[string]any {
	return map[string]any{
		"approvalId":   approval.ID,
		"requestIdRaw": approval.RequestIDRaw,
		"hostId":       defaultHostID(approval.HostID),
		"fingerprint":  approval.Fingerprint,
		"approvalType": approval.Type,
		"status":       approval.Status,
		"threadId":     approval.ThreadID,
		"turnId":       approval.TurnID,
		"itemId":       approval.ItemID,
		"command":      approval.Command,
		"cwd":          approval.Cwd,
		"reason":       approval.Reason,
		"grantRoot":    approval.GrantRoot,
		"changes":      append([]model.FileChange(nil), approval.Changes...),
		"requestedAt":  approval.RequestedAt,
		"resolvedAt":   approval.ResolvedAt,
		"decisions":    append([]string(nil), approval.Decisions...),
	}
}

func approvalLifecycleCardPayload(card model.Card) map[string]any {
	return map[string]any{
		"cardId":    card.ID,
		"cardType":  card.Type,
		"title":     card.Title,
		"text":      card.Text,
		"summary":   card.Summary,
		"status":    card.Status,
		"command":   card.Command,
		"cwd":       card.Cwd,
		"hostId":    card.HostID,
		"hostName":  card.HostName,
		"changes":   append([]model.FileChange(nil), card.Changes...),
		"detail":    cloneAnyMap(card.Detail),
		"createdAt": card.CreatedAt,
		"updatedAt": card.UpdatedAt,
	}
}

func approvalEventToolName(approvalType string) string {
	switch strings.TrimSpace(approvalType) {
	case bifrostApprovalTypeRemoteFileChange, "remote_file_change", "file_change":
		return "write_file"
	case bifrostApprovalTypeRemoteCommand, "remote_command", "command":
		return "execute_command"
	default:
		return strings.TrimSpace(approvalType)
	}
}

func approvalAutoApprovalPresentation(approval model.ApprovalRequest, ruleName string) (status, title, text, decision string) {
	switch strings.TrimSpace(ruleName) {
	case toolApprovalRuleSessionGrant:
		return "accepted_for_session_auto", "Auto-approved for session", autoApprovalNoticeText(approval), "accept_session"
	case toolApprovalRuleHostGrant:
		return "accepted_for_host_auto", "Auto-approved by host grant", hostGrantAutoApprovalNoticeText(approval), "accept"
	case toolApprovalRuleProfilePolicy:
		return "accepted_by_policy_auto", "Auto-approved by profile", "当前 main-agent profile 允许该操作直接执行，因此已自动放行。", "accept"
	default:
		return "accepted", "Auto-approved", "当前操作已自动放行。", "accept"
	}
}
