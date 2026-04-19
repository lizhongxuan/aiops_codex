package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/store"
)

type projectionProbe struct {
	name   string
	events []ToolLifecycleEvent
	err    error
}

func (p *projectionProbe) HandleToolLifecycleEvent(_ context.Context, event ToolLifecycleEvent) error {
	p.events = append(p.events, event)
	return p.err
}

func TestProductProjectionSubscriberFanOutsToEachProjection(t *testing.T) {
	first := &projectionProbe{name: "runtime"}
	second := &projectionProbe{name: "card"}
	third := &projectionProbe{name: "approval", err: errors.New("approval failed")}

	subscriber := ProductProjectionSubscriber{
		projections: []ToolLifecycleSubscriber{first, second, third},
	}

	err := subscriber.HandleToolLifecycleEvent(context.Background(), ToolLifecycleEvent{
		Type:      ToolLifecycleEventStarted,
		SessionID: "sess-1",
	})
	if err == nil {
		t.Fatal("expected fan-out error")
	}
	if !strings.Contains(err.Error(), "approval failed") {
		t.Fatalf("expected joined error to include approval failure, got %v", err)
	}
	if len(first.events) != 1 || len(second.events) != 1 || len(third.events) != 1 {
		t.Fatalf("expected one event per projection, got runtime=%d card=%d approval=%d", len(first.events), len(second.events), len(third.events))
	}
}

func TestProductProjectionSubscriberUsesDefaultProjections(t *testing.T) {
	app := &App{store: store.New()}
	subscriber := NewProductProjectionSubscriber(app)

	if len(subscriber.projections) != 7 {
		t.Fatalf("expected seven default projections, got %d", len(subscriber.projections))
	}
}

func TestApprovalProjectionRequestedCreatesApprovalAndCardFromPayloadAndMetadata(t *testing.T) {
	app := &App{store: store.New()}
	sessionID := "sess-approval-requested"

	projection := NewApprovalToolProjection(app)
	if err := projection.HandleToolLifecycleEvent(context.Background(), ToolLifecycleEvent{
		Type:      ToolLifecycleEventApprovalRequested,
		SessionID: sessionID,
		Payload: map[string]any{
			"approval": map[string]any{
				"approvalId": "approval-1",
				"cardId":     "card-1",
				"title":      "需要审批",
				"text":       "变更配置",
				"summary":    "apply patch",
				"command":    "apply_patch",
				"cwd":        "/tmp/demo",
				"decisions":  []any{"accept", "decline"},
			},
		},
		Metadata: map[string]any{
			"approval": map[string]any{
				"approvalType": "mutation",
				"hostId":       "server-local",
				"threadId":     "thread-1",
				"turnId":       "turn-1",
				"requestedAt":  "2026-04-17T08:00:00Z",
			},
			"card": map[string]any{
				"cardType": "ApprovalCard",
				"status":   "pending",
			},
		},
	}); err != nil {
		t.Fatalf("project requested event: %v", err)
	}

	approval, ok := app.store.Approval(sessionID, "approval-1")
	if !ok {
		t.Fatal("expected approval to be stored")
	}
	if approval.Type != "mutation" || approval.HostID != "server-local" {
		t.Fatalf("unexpected approval contents: %#v", approval)
	}
	if approval.ItemID != "card-1" || approval.RequestedAt != "2026-04-17T08:00:00Z" {
		t.Fatalf("expected approval to capture card and requested time, got %#v", approval)
	}

	card := app.cardByID(sessionID, "card-1")
	if card == nil {
		t.Fatal("expected approval card to be stored")
	}
	if card.Status != "pending" || card.Title != "需要审批" {
		t.Fatalf("unexpected approval card: %#v", card)
	}
	if card.Approval == nil || card.Approval.RequestID != "approval-1" {
		t.Fatalf("expected card approval ref to be populated, got %#v", card.Approval)
	}
}

func TestApprovalProjectionResolvedUpdatesApprovalAndCardFromMetadataFallback(t *testing.T) {
	app := &App{store: store.New()}
	sessionID := "sess-approval-resolved"

	_ = NewApprovalToolProjection(app).HandleToolLifecycleEvent(context.Background(), ToolLifecycleEvent{
		Type:      ToolLifecycleEventApprovalRequested,
		SessionID: sessionID,
		Payload: map[string]any{
			"approval": map[string]any{
				"approvalId": "approval-2",
				"cardId":     "card-2",
				"title":      "文件变更审批",
				"text":       "等待决策",
			},
		},
	})

	if err := NewApprovalToolProjection(app).HandleToolLifecycleEvent(context.Background(), ToolLifecycleEvent{
		Type:      ToolLifecycleEventApprovalResolved,
		SessionID: sessionID,
		Metadata: map[string]any{
			"approval": map[string]any{
				"approvalId": "approval-2",
				"cardId":     "card-2",
				"decision":   "accepted",
				"resolvedAt": "2026-04-17T09:00:00Z",
				"summary":    "已批准",
			},
		},
	}); err != nil {
		t.Fatalf("project resolved event: %v", err)
	}

	approval, ok := app.store.Approval(sessionID, "approval-2")
	if !ok {
		t.Fatal("expected approval to remain stored")
	}
	if approval.Status != "accepted" || approval.ResolvedAt != "2026-04-17T09:00:00Z" {
		t.Fatalf("unexpected approval resolution: %#v", approval)
	}

	card := app.cardByID(sessionID, "card-2")
	if card == nil {
		t.Fatal("expected card to exist after resolution")
	}
	if card.Status != "completed" {
		t.Fatalf("expected resolved approval to map to completed card status, got %#v", card)
	}
	if card.Text != "已批准" {
		t.Fatalf("expected resolved summary to update card text, got %#v", card)
	}
}

func TestApprovalStatusToCardStatusMapsAcceptedForSession(t *testing.T) {
	if got := approvalStatusToCardStatus("accepted_for_session"); got != "completed" {
		t.Fatalf("expected accepted_for_session to map to completed, got %q", got)
	}
}
