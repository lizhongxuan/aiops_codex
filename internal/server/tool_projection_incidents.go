package server

import (
	"context"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type incidentToolProjection struct {
	app *App
}

func NewIncidentToolProjection(app *App) ToolLifecycleSubscriber {
	return incidentToolProjection{app: app}
}

func (p incidentToolProjection) HandleToolLifecycleEvent(_ context.Context, event ToolLifecycleEvent) error {
	if p.app == nil {
		return nil
	}
	p.app.projectToolLifecycleIncidents(event.SessionID, event)
	return nil
}

func (a *App) projectToolLifecycleIncidents(sessionID string, event ToolLifecycleEvent) {
	if a == nil || strings.TrimSpace(sessionID) == "" {
		return
	}

	switch event.Type {
	case ToolLifecycleEventFailed:
		a.projectToolFailureIncidentEvent(sessionID, event)
	case ToolLifecycleEventApprovalRequested:
		a.projectToolApprovalIncidentEvent(sessionID, event)
	case ToolLifecycleEventApprovalResolved:
		a.projectToolApprovalIncidentResolution(sessionID, event)
	}
}

func (a *App) projectToolFailureIncidentEvent(sessionID string, event ToolLifecycleEvent) {
	if a == nil || strings.TrimSpace(sessionID) == "" {
		return
	}

	targetSessionIDs := []string{sessionID}
	if workspaceSessionID := strings.TrimSpace(a.sessionMeta(sessionID).WorkspaceSessionID); workspaceSessionID != "" && workspaceSessionID != sessionID {
		targetSessionIDs = append(targetSessionIDs, workspaceSessionID)
	}

	for _, targetSessionID := range targetSessionIDs {
		current := a.snapshot(targetSessionID)
		runID := ""
		iterationID := ""
		stage := strings.TrimSpace(current.CurrentStage)
		if current.AgentLoop != nil {
			runID = strings.TrimSpace(current.AgentLoop.ID)
			iterationID = strings.TrimSpace(current.AgentLoop.ActiveIterationID)
		}

		metadata := map[string]any{
			"invocationId": emptyToNil(strings.TrimSpace(event.InvocationID)),
			"callId":       emptyToNil(strings.TrimSpace(event.CallID)),
			"cardId":       emptyToNil(strings.TrimSpace(event.CardID)),
			"phase":        emptyToNil(strings.TrimSpace(event.Phase)),
			"error":        emptyToNil(firstNonEmptyValue(strings.TrimSpace(event.Error), strings.TrimSpace(event.Message), strings.TrimSpace(event.Label))),
		}
		if targetSessionID != sessionID {
			metadata["sourceSessionId"] = sessionID
		}
		if args := toolLifecycleArguments(event); len(args) > 0 {
			if path := strings.TrimSpace(getStringAny(args, "path", "filePath")); path != "" {
				metadata["path"] = path
			}
			if query := strings.TrimSpace(getStringAny(args, "query")); query != "" {
				metadata["query"] = query
			}
		}

		a.store.UpsertIncidentEvent(targetSessionID, model.IncidentEvent{
			ID:          firstNonEmptyValue(strings.TrimSpace(event.EventID), model.NewID("evt")),
			SessionID:   targetSessionID,
			RunID:       runID,
			IterationID: iterationID,
			Stage:       stage,
			Type:        "tool.failed",
			Status:      "warning",
			Title:       "Tool failed",
			Summary:     firstNonEmptyValue(strings.TrimSpace(event.Error), strings.TrimSpace(event.Message), strings.TrimSpace(event.Label), strings.TrimSpace(event.ToolName)),
			HostID:      defaultHostID(strings.TrimSpace(event.HostID)),
			ToolName:    strings.TrimSpace(event.ToolName),
			Metadata:    metadata,
			CreatedAt:   eventTimeString(event),
		})
	}
}

func (a *App) projectToolApprovalIncidentEvent(sessionID string, event ToolLifecycleEvent) {
	approval, _ := buildApprovalProjectionObjects(event, true)
	approval.ID = firstNonEmptyValue(
		strings.TrimSpace(approval.ID),
		toolProjectionStringFromMaps(approvalProjectionSources(event), "approvalId", "approvalID", "approval_id"),
		strings.TrimSpace(event.ApprovalID),
	)
	if strings.TrimSpace(approval.ID) == "" {
		return
	}
	approval.Status = firstNonEmptyValue(strings.TrimSpace(approval.Status), "pending")
	approval.RequestedAt = firstNonEmptyValue(
		strings.TrimSpace(approval.RequestedAt),
		toolProjectionStringFromMaps(approvalProjectionSources(event), "requestedAt", "requested_at"),
		eventTimeString(event),
	)
	approval.ItemID = firstNonEmptyValue(strings.TrimSpace(approval.ItemID), strings.TrimSpace(event.CardID))
	a.recordApprovalIncidentEvent(sessionID, "approval.requested", approval, "", approval.Status, approval.RequestedAt, "", nil)
}

func (a *App) projectToolApprovalIncidentResolution(sessionID string, event ToolLifecycleEvent) {
	approval, _ := buildApprovalProjectionObjects(event, false)
	approval.ID = firstNonEmptyValue(
		strings.TrimSpace(approval.ID),
		toolProjectionStringFromMaps(approvalProjectionSources(event), "approvalId", "approvalID", "approval_id"),
		strings.TrimSpace(event.ApprovalID),
	)
	if strings.TrimSpace(approval.ID) == "" {
		return
	}

	status := firstNonEmptyValue(
		strings.TrimSpace(approval.Status),
		toolProjectionStringFromMaps(approvalProjectionSources(event), "status", "approvalStatus", "approval_status", "decision"),
		"accepted",
	)
	decision := firstNonEmptyValue(
		toolProjectionStringFromMaps(approvalProjectionSources(event), "decision", "approvalDecision", "approval_decision"),
		approvalDecisionForStatus(status),
	)
	approval.Status = status
	approval.RequestedAt = firstNonEmptyValue(
		strings.TrimSpace(approval.RequestedAt),
		toolProjectionStringFromMaps(approvalProjectionSources(event), "requestedAt", "requested_at"),
	)
	approval.ResolvedAt = firstNonEmptyValue(
		strings.TrimSpace(approval.ResolvedAt),
		toolProjectionStringFromMaps(approvalProjectionSources(event), "resolvedAt", "resolved_at"),
		eventTimeString(event),
	)
	approval.ItemID = firstNonEmptyValue(strings.TrimSpace(approval.ItemID), strings.TrimSpace(event.CardID))

	eventType := "approval.decision"
	if strings.Contains(strings.TrimSpace(status), "_auto") {
		eventType = "approval.auto_accepted"
	}
	a.recordApprovalIncidentEvent(sessionID, eventType, approval, decision, status, approval.RequestedAt, approval.ResolvedAt, nil)
}
