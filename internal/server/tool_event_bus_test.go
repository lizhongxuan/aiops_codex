package server

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestToolEventBusFanOutIgnoresSubscriberErrors(t *testing.T) {
	bus := NewToolEventBus()

	var calls []string
	bus.Subscribe(func(context.Context, ToolLifecycleEvent) error {
		calls = append(calls, "first")
		return errors.New("first failed")
	})
	bus.Subscribe(func(context.Context, ToolLifecycleEvent) error {
		calls = append(calls, "second")
		return nil
	})

	err := bus.Publish(context.Background(), ToolLifecycleEvent{Type: ToolLifecycleEventStarted, SessionID: "sess-1"})
	if err == nil {
		t.Fatal("expected publish error")
	}
	if !strings.Contains(err.Error(), "first failed") {
		t.Fatalf("expected joined error to include first subscriber failure, got %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected both subscribers to run, got %v", calls)
	}
}

func TestToolEventBusUnsubscribeStopsDelivery(t *testing.T) {
	bus := NewToolEventBus()

	var calls int
	unsubscribe := bus.Subscribe(func(context.Context, ToolLifecycleEvent) error {
		calls++
		return nil
	})
	unsubscribe()

	if err := bus.Publish(context.Background(), ToolLifecycleEvent{Type: ToolLifecycleEventCompleted}); err != nil {
		t.Fatalf("unexpected publish error: %v", err)
	}
	if calls != 0 {
		t.Fatalf("expected unsubscribed subscriber to stay silent, got %d calls", calls)
	}
}
