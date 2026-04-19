package server

import (
	"context"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/store"
)

func TestToolEventStoreSubscriberAppendsLifecycleEventsBySession(t *testing.T) {
	eventStore := store.NewToolEventStore(4)
	bus := NewToolEventBus()
	bus.Subscribe(NewToolEventStoreSubscriber(eventStore))

	err := bus.Publish(context.Background(), ToolLifecycleEvent{
		Type:         ToolLifecycleEventStarted,
		EventID:      "evt-1",
		InvocationID: "toolinv-1",
		SessionID:    "sess-1",
		ToolName:     "execute_command",
		Payload:      map[string]any{"command": "ls"},
		Metadata:     map[string]any{"host": "linux-01"},
	})
	if err != nil {
		t.Fatalf("publish event: %v", err)
	}

	got := eventStore.SessionEvents("sess-1")
	if len(got) != 1 {
		t.Fatalf("expected one stored event, got %d", len(got))
	}
	if got[0].EventID != "evt-1" || got[0].Type != string(ToolLifecycleEventStarted) {
		t.Fatalf("unexpected stored event: %#v", got[0])
	}
	if got[0].Payload["command"] != "ls" || got[0].Metadata["host"] != "linux-01" {
		t.Fatalf("expected stored payload/metadata, got %#v", got[0])
	}
}

func TestToolEventStoreSubscriberStoresProgressEvents(t *testing.T) {
	eventStore := store.NewToolEventStore(4)
	bus := NewToolEventBus()
	bus.Subscribe(NewToolEventStoreSubscriber(eventStore))

	err := bus.Publish(context.Background(), ToolLifecycleEvent{
		Type:         ToolLifecycleEventProgress,
		EventID:      "evt-progress-1",
		InvocationID: "toolinv-progress-1",
		SessionID:    "sess-progress",
		ToolName:     "test.progress",
		Message:      "processed 2/5 chunks",
		Payload: map[string]any{
			"current": 2,
			"total":   5,
		},
	})
	if err != nil {
		t.Fatalf("publish progress event: %v", err)
	}

	got := eventStore.SessionEvents("sess-progress")
	if len(got) != 1 {
		t.Fatalf("expected one stored progress event, got %d", len(got))
	}
	if got[0].Type != string(ToolLifecycleEventProgress) || got[0].Message != "processed 2/5 chunks" {
		t.Fatalf("unexpected stored progress event: %#v", got[0])
	}
	if got[0].Payload["current"] != 2 || got[0].Payload["total"] != 5 {
		t.Fatalf("expected progress payload to be stored, got %#v", got[0])
	}
}
