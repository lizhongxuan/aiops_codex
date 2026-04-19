package server

import (
	"context"
	"log"
	"strings"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

type orchestratorToolProjection struct {
	app *App
}

func NewOrchestratorToolProjection(app *App) ToolLifecycleSubscriber {
	return orchestratorToolProjection{app: app}
}

func (p orchestratorToolProjection) HandleToolLifecycleEvent(_ context.Context, event ToolLifecycleEvent) error {
	if p.app == nil {
		return nil
	}
	p.app.projectToolLifecycleOrchestrator(event)
	return nil
}

func (a *App) projectToolLifecycleOrchestrator(event ToolLifecycleEvent) {
	if a == nil || a.orchestrator == nil {
		return
	}
	sessionID := strings.TrimSpace(event.SessionID)
	if sessionID == "" {
		return
	}

	switch event.Type {
	case ToolLifecycleEventApprovalRequested:
		a.projectOrchestratorApprovalRequested(sessionID, event)
	case ToolLifecycleEventApprovalResolved:
		if a.sessionKind(sessionID) != model.SessionKindWorker {
			return
		}
		phase, ok := orchestratorWorkerPhaseFromToolLifecycle(event)
		if !ok {
			return
		}
		a.syncWorkerPhaseAndRefreshWorkspace(sessionID, phase)
	}
}

func (a *App) projectOrchestratorApprovalRequested(sessionID string, event ToolLifecycleEvent) {
	if a == nil || a.orchestrator == nil {
		return
	}
	approval, card := buildApprovalProjectionObjects(event, true)
	if strings.TrimSpace(approval.ID) == "" {
		return
	}
	a.recordOrchestratorApprovalRequested(sessionID, approval)

	meta := a.sessionMeta(sessionID)
	switch meta.Kind {
	case model.SessionKindWorker:
		phase := firstNonEmptyValue(strings.TrimSpace(event.Phase), "waiting_approval")
		a.syncWorkerPhaseAndRefreshWorkspace(sessionID, phase)
		if err := a.orchestrator.RegisterApprovalRoute(approval.ID, sessionID); err != nil {
			log.Printf("orchestrator approval route failed approval=%s session=%s err=%v", approval.ID, sessionID, err)
		}
		a.mirrorInternalApprovalToWorkspace(sessionID, approval, card)
		if approvalRequestedShouldActivateQueuedWorkers(event) {
			if workspaceSessionID := strings.TrimSpace(meta.WorkspaceSessionID); workspaceSessionID != "" {
				a.activateQueuedMissionWorkers(workspaceSessionID)
			}
		}
	case model.SessionKindPlanner:
		a.mirrorInternalApprovalToWorkspace(sessionID, approval, card)
	}
}

func approvalRequestedShouldActivateQueuedWorkers(event ToolLifecycleEvent) bool {
	if event.Payload == nil {
		return false
	}
	value, ok := event.Payload["activateQueuedWorkers"]
	if !ok {
		return false
	}
	activate, ok := value.(bool)
	return ok && activate
}

func orchestratorWorkerPhaseFromToolLifecycle(event ToolLifecycleEvent) (string, bool) {
	switch event.Type {
	case ToolLifecycleEventApprovalRequested, ToolLifecycleEventApprovalResolved:
		phase := strings.TrimSpace(event.Phase)
		if phase == "" {
			phase = "thinking"
		}
		return phase, true
	default:
		return "", false
	}
}
