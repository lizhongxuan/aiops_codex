package server

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func (a *App) emitDynamicRemoteFileChangeStartedEvent(ctx context.Context, sessionID string, approval model.ApprovalRequest, args remoteFileChangeArgs, processCardID string, startedAt time.Time) bool {
	if a == nil || a.toolEventBus == nil {
		return false
	}
	event := newDynamicRemoteFileChangeStartedEvent(sessionID, approval, args, processCardID, startedAt)
	if err := a.toolEventBus.Emit(ctx, event); err != nil {
		log.Printf("failed to emit dynamic file change started event session=%s approval=%s err=%v", sessionID, approval.ID, err)
		return false
	}
	return true
}

func (a *App) emitDynamicRemoteFileChangeCompletedEvent(ctx context.Context, sessionID string, approval model.ApprovalRequest, args remoteFileChangeArgs, processCardID string, card model.Card, outputText string, finishedAt time.Time) bool {
	if a == nil || a.toolEventBus == nil {
		return false
	}
	event := newDynamicRemoteFileChangeCompletedEvent(sessionID, approval, args, processCardID, card, outputText, finishedAt)
	if err := a.toolEventBus.Emit(ctx, event); err != nil {
		log.Printf("failed to emit dynamic file change completed event session=%s approval=%s err=%v", sessionID, approval.ID, err)
		return false
	}
	return true
}

func (a *App) emitDynamicRemoteFileChangeFailedEvent(ctx context.Context, sessionID string, approval model.ApprovalRequest, args remoteFileChangeArgs, processCardID string, card model.Card, errorText string, finishedAt time.Time) bool {
	if a == nil || a.toolEventBus == nil {
		return false
	}
	event := newDynamicRemoteFileChangeFailedEvent(sessionID, approval, args, processCardID, card, errorText, finishedAt)
	if err := a.toolEventBus.Emit(ctx, event); err != nil {
		log.Printf("failed to emit dynamic file change failed event session=%s approval=%s err=%v", sessionID, approval.ID, err)
		return false
	}
	return true
}

func newDynamicRemoteFileChangeStartedEvent(sessionID string, approval model.ApprovalRequest, args remoteFileChangeArgs, processCardID string, startedAt time.Time) ToolLifecycleEvent {
	invocation := dynamicRemoteFileChangeInvocation(sessionID, approval, args, startedAt)
	event := newToolStartedEvent(invocation, ToolDescriptor{
		Name:         "write_file",
		Domain:       "dynamic",
		Kind:         "dynamic",
		DisplayLabel: "现在修改 " + strings.TrimSpace(args.Path),
		StartPhase:   "executing",
	})
	event.CardID = strings.TrimSpace(processCardID)
	if event.Payload == nil {
		event.Payload = make(map[string]any)
	}
	event.Payload["trackActivityStart"] = true
	event.Payload["approval"] = approvalLifecycleEventPayload(approval)
	return event
}

func newDynamicRemoteFileChangeCompletedEvent(sessionID string, approval model.ApprovalRequest, args remoteFileChangeArgs, processCardID string, card model.Card, outputText string, finishedAt time.Time) ToolLifecycleEvent {
	invocation := dynamicRemoteFileChangeInvocation(sessionID, approval, args, finishedAt)
	event := newToolCompletedEvent(invocation, ToolExecutionResult{
		InvocationID: invocation.InvocationID,
		Status:       ToolRunStatusCompleted,
		OutputText:   strings.TrimSpace(outputText),
		FinishedAt:   finishedAt,
	})
	event.CardID = strings.TrimSpace(processCardID)
	if event.Payload == nil {
		event.Payload = make(map[string]any)
	}
	event.Payload["trackActivityCompletion"] = true
	if display := toolProjectionDisplayMapFromDetail(card.Detail); len(display) > 0 {
		event.Payload["display"] = display
	}
	event.Payload["finalCard"] = lifecycleCardPayload(card)
	event.Payload["syncActionArtifacts"] = true
	event.Payload["approval"] = approvalLifecycleEventPayload(approval)
	return event
}

func newDynamicRemoteFileChangeFailedEvent(sessionID string, approval model.ApprovalRequest, args remoteFileChangeArgs, processCardID string, card model.Card, errorText string, finishedAt time.Time) ToolLifecycleEvent {
	invocation := dynamicRemoteFileChangeInvocation(sessionID, approval, args, finishedAt)
	event := newToolFailedResultEvent(invocation, ToolExecutionResult{
		InvocationID: invocation.InvocationID,
		Status:       ToolRunStatusFailed,
		ErrorText:    strings.TrimSpace(errorText),
		FinishedAt:   finishedAt,
	})
	event.CardID = strings.TrimSpace(processCardID)
	if event.Payload == nil {
		event.Payload = make(map[string]any)
	}
	event.Payload["trackActivityCompletion"] = false
	event.Payload["finalCard"] = lifecycleCardPayload(card)
	event.Payload["syncActionArtifacts"] = true
	event.Payload["approval"] = approvalLifecycleEventPayload(approval)
	return event
}

func dynamicRemoteFileChangeInvocation(sessionID string, approval model.ApprovalRequest, args remoteFileChangeArgs, startedAt time.Time) ToolInvocation {
	return ToolInvocation{
		InvocationID: model.NewID("toolinv"),
		SessionID:    sessionID,
		ThreadID:     strings.TrimSpace(approval.ThreadID),
		TurnID:       strings.TrimSpace(approval.TurnID),
		ToolName:     "write_file",
		ToolKind:     "dynamic",
		Source:       ToolInvocationSourceApprovalResume,
		HostID:       defaultHostID(approval.HostID),
		CallID:       strings.TrimSpace(approval.ItemID),
		Arguments: map[string]any{
			"hostId":     defaultHostID(approval.HostID),
			"host":       defaultHostID(approval.HostID),
			"mode":       "file_change",
			"path":       strings.TrimSpace(args.Path),
			"writeMode":  strings.TrimSpace(args.WriteMode),
			"write_mode": strings.TrimSpace(args.WriteMode),
			"reason":     strings.TrimSpace(args.Reason),
		},
		StartedAt: startedAt,
	}
}

func lifecycleCardPayload(card model.Card) map[string]any {
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
