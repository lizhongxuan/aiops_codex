package server

import (
	"context"

	"github.com/lizhongxuan/aiops-codex/internal/store"
)

type toolEventStoreSubscriber struct {
	store *store.ToolEventStore
}

func NewToolEventStoreSubscriber(eventStore *store.ToolEventStore) ToolLifecycleSubscriber {
	return toolEventStoreSubscriber{store: eventStore}
}

func (s toolEventStoreSubscriber) HandleToolLifecycleEvent(_ context.Context, event ToolLifecycleEvent) error {
	if s.store == nil {
		return nil
	}
	s.store.Append(store.ToolEventRecord{
		EventID:        event.EventID,
		InvocationID:   event.InvocationID,
		Type:           string(event.Type),
		SessionID:      event.SessionID,
		ToolName:       event.ToolName,
		HostID:         event.HostID,
		CallID:         event.CallID,
		CardID:         event.CardID,
		ApprovalID:     event.ApprovalID,
		Phase:          event.Phase,
		Label:          event.Label,
		Message:        event.Message,
		Error:          event.Error,
		ActivityKind:   event.ActivityKind,
		ActivityTarget: event.ActivityTarget,
		ActivityQuery:  event.ActivityQuery,
		CreatedAt:      event.CreatedAt,
		Payload:        cloneToolEventAnyMap(event.Payload),
		Metadata:       cloneToolEventAnyMap(event.Metadata),
	})
	return nil
}

func cloneToolEventAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
