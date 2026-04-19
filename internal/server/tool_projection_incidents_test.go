package server

import (
	"context"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/store"
)

func TestIncidentProjectionWritesToolFailureIncidentEvent(t *testing.T) {
	app := &App{store: store.New()}
	sessionID := "sess-incident-tool-failed"

	err := NewIncidentToolProjection(app).HandleToolLifecycleEvent(context.Background(), ToolLifecycleEvent{
		EventID:      "evt-tool-failed-1",
		Type:         ToolLifecycleEventFailed,
		SessionID:    sessionID,
		ToolName:     "write_file",
		HostID:       "linux-01",
		InvocationID: "toolinv-failed-1",
		CallID:       "call-failed-1",
		CardID:       "card-failed-1",
		Message:      "permission denied",
		Error:        "permission denied",
		Payload: map[string]any{
			"arguments": map[string]any{
				"path": "/etc/app.conf",
			},
		},
		CreatedAt: "2026-04-17T08:00:00Z",
	})
	if err != nil {
		t.Fatalf("project failed event: %v", err)
	}

	event := findIncidentEvent(app.snapshot(sessionID).IncidentEvents, "tool.failed")
	if event == nil {
		t.Fatalf("expected tool.failed incident event, got %#v", app.snapshot(sessionID).IncidentEvents)
	}
	if event.ID != "evt-tool-failed-1" {
		t.Fatalf("expected incident event id to follow lifecycle event id, got %#v", event)
	}
	if event.ToolName != "write_file" || event.HostID != "linux-01" {
		t.Fatalf("unexpected incident event projection: %#v", event)
	}
	if got := anyToString(event.Metadata["cardId"]); got != "card-failed-1" {
		t.Fatalf("expected card id in metadata, got %#v", event.Metadata)
	}
	if got := anyToString(event.Metadata["path"]); got != "/etc/app.conf" {
		t.Fatalf("expected path metadata, got %#v", event.Metadata)
	}
}

func TestIncidentProjectionWritesApprovalLifecycleIncidentEvents(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "sess-incident-approval-lifecycle"
	now := "2026-04-17T09:00:00Z"

	projection := NewIncidentToolProjection(app)
	requestedEvent := ToolLifecycleEvent{
		Type:       ToolLifecycleEventApprovalRequested,
		SessionID:  sessionID,
		ToolName:   "request_approval",
		ApprovalID: "approval-incident-1",
		CardID:     "approval-card-incident-1",
		Payload: map[string]any{
			"approval": map[string]any{
				"approvalId":   "approval-incident-1",
				"approvalType": "command",
				"status":       "pending",
				"hostId":       model.ServerLocalHostID,
				"command":      "systemctl restart nginx",
				"reason":       "restart nginx",
				"itemId":       "approval-card-incident-1",
				"requestedAt":  now,
			},
			"card": map[string]any{
				"cardId":   "approval-card-incident-1",
				"cardType": "ApprovalCard",
				"status":   "pending",
			},
		},
	}
	if err := projection.HandleToolLifecycleEvent(context.Background(), requestedEvent); err != nil {
		t.Fatalf("project approval requested event: %v", err)
	}

	requested := findIncidentEvent(app.snapshot(sessionID).IncidentEvents, "approval.requested")
	if requested == nil {
		t.Fatalf("expected approval.requested incident event, got %#v", app.snapshot(sessionID).IncidentEvents)
	}
	if requested.ApprovalID != "approval-incident-1" {
		t.Fatalf("expected approval id to be projected, got %#v", requested)
	}

	resolvedEvent := ToolLifecycleEvent{
		Type:       ToolLifecycleEventApprovalResolved,
		SessionID:  sessionID,
		ToolName:   "request_approval",
		ApprovalID: "approval-incident-1",
		CardID:     "approval-card-incident-1",
		Payload: map[string]any{
			"approval": map[string]any{
				"approvalId":   "approval-incident-1",
				"approvalType": "command",
				"status":       "decline",
				"decision":     "decline",
				"hostId":       model.ServerLocalHostID,
				"command":      "systemctl restart nginx",
				"reason":       "restart nginx",
				"itemId":       "approval-card-incident-1",
				"requestedAt":  now,
				"resolvedAt":   "2026-04-17T09:00:30Z",
			},
		},
	}
	if err := projection.HandleToolLifecycleEvent(context.Background(), resolvedEvent); err != nil {
		t.Fatalf("project approval resolved event: %v", err)
	}

	decision := findIncidentEvent(app.snapshot(sessionID).IncidentEvents, "approval.decision")
	if decision == nil {
		t.Fatalf("expected approval.decision incident event, got %#v", app.snapshot(sessionID).IncidentEvents)
	}
	if decision.ApprovalID != "approval-incident-1" || decision.Status != "warning" {
		t.Fatalf("unexpected approval decision projection: %#v", decision)
	}
}
