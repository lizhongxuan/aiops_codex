package server

import (
	"context"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/store"
)

func TestSnapshotBroadcastProjectionThrottlesStartedAndProgressEvents(t *testing.T) {
	var scheduled []string
	var immediate []string

	projection := snapshotBroadcastProjection{
		schedule: func(sessionID string) {
			scheduled = append(scheduled, sessionID)
		},
		immediate: func(sessionID string) {
			immediate = append(immediate, sessionID)
		},
	}

	for _, eventType := range []ToolLifecycleEventType{
		ToolLifecycleEventStarted,
		ToolLifecycleEventProgress,
	} {
		if err := projection.HandleToolLifecycleEvent(context.Background(), ToolLifecycleEvent{
			Type:      eventType,
			SessionID: "sess-throttled",
		}); err != nil {
			t.Fatalf("handle %s: %v", eventType, err)
		}
	}

	if len(scheduled) != 2 {
		t.Fatalf("expected two throttled broadcasts, got %d", len(scheduled))
	}
	if len(immediate) != 0 {
		t.Fatalf("expected no immediate broadcasts, got %d", len(immediate))
	}
}

func TestSnapshotBroadcastProjectionImmediatelyBroadcastsTerminalEvents(t *testing.T) {
	var scheduled []string
	var immediate []string

	projection := snapshotBroadcastProjection{
		schedule: func(sessionID string) {
			scheduled = append(scheduled, sessionID)
		},
		immediate: func(sessionID string) {
			immediate = append(immediate, sessionID)
		},
	}

	for _, eventType := range []ToolLifecycleEventType{
		ToolLifecycleEventCompleted,
		ToolLifecycleEventFailed,
		ToolLifecycleEventCancelled,
		ToolLifecycleEventApprovalRequested,
		ToolLifecycleEventApprovalResolved,
		ToolLifecycleEventChoiceRequested,
		ToolLifecycleEventChoiceResolved,
	} {
		if err := projection.HandleToolLifecycleEvent(context.Background(), ToolLifecycleEvent{
			Type:      eventType,
			SessionID: "sess-immediate",
		}); err != nil {
			t.Fatalf("handle %s: %v", eventType, err)
		}
	}

	if len(scheduled) != 0 {
		t.Fatalf("expected no throttled broadcasts, got %d", len(scheduled))
	}
	if len(immediate) != 7 {
		t.Fatalf("expected seven immediate broadcasts, got %d", len(immediate))
	}
}

func TestSnapshotBroadcastProjectionImmediatelyBroadcastsStartedAndProgressWithDisplay(t *testing.T) {
	var scheduled []string
	var immediate []string

	projection := snapshotBroadcastProjection{
		schedule: func(sessionID string) {
			scheduled = append(scheduled, sessionID)
		},
		immediate: func(sessionID string) {
			immediate = append(immediate, sessionID)
		},
	}

	for _, eventType := range []ToolLifecycleEventType{
		ToolLifecycleEventStarted,
		ToolLifecycleEventProgress,
	} {
		if err := projection.HandleToolLifecycleEvent(context.Background(), ToolLifecycleEvent{
			Type:      eventType,
			SessionID: "sess-displayed",
			Payload: map[string]any{
				"display": map[string]any{
					"summary": "structured display",
				},
			},
		}); err != nil {
			t.Fatalf("handle %s: %v", eventType, err)
		}
	}

	if len(scheduled) != 0 {
		t.Fatalf("expected no throttled broadcasts for displayed events, got %d", len(scheduled))
	}
	if len(immediate) != 2 {
		t.Fatalf("expected immediate broadcasts for displayed started/progress events, got %d", len(immediate))
	}
}

func TestSnapshotBroadcastProjectionRunsAfterProductProjection(t *testing.T) {
	app := &App{store: store.New()}
	sessionID := "sess-broadcast-order"

	bus := NewToolEventBus()
	bus.Subscribe(NewProductProjectionSubscriber(app))
	bus.Subscribe(snapshotBroadcastProjection{
		schedule: func(gotSessionID string) {
			if gotSessionID != sessionID {
				t.Fatalf("expected session %q, got %q", sessionID, gotSessionID)
			}
			card := app.cardByID(sessionID, "proc-1")
			if card == nil {
				t.Fatal("expected process card to exist before snapshot scheduling")
			}
			if card.Text != "现在浏览 /etc/hosts" {
				t.Fatalf("expected projected card text before snapshot scheduling, got %#v", card)
			}
		},
		immediate: func(string) {
			t.Fatal("did not expect immediate broadcast for started event")
		},
	})

	if err := bus.Emit(context.Background(), ToolLifecycleEvent{
		Type:      ToolLifecycleEventStarted,
		SessionID: sessionID,
		ToolName:  "read_file",
		CardID:    "proc-1",
		Label:     "现在浏览 /etc/hosts",
		Phase:     "browsing",
	}); err != nil {
		t.Fatalf("emit lifecycle event: %v", err)
	}
}
