package server

import (
	"context"
	"errors"
	"sync"
	"time"
)

type ToolLifecycleEventType string

const (
	ToolLifecycleEventStarted           ToolLifecycleEventType = "started"
	ToolLifecycleEventProgress          ToolLifecycleEventType = "progress"
	ToolLifecycleEventCompleted         ToolLifecycleEventType = "completed"
	ToolLifecycleEventFailed            ToolLifecycleEventType = "failed"
	ToolLifecycleEventCancelled         ToolLifecycleEventType = "cancelled"
	ToolLifecycleEventApprovalRequested ToolLifecycleEventType = "approval_requested"
	ToolLifecycleEventApprovalResolved  ToolLifecycleEventType = "approval_resolved"
	ToolLifecycleEventChoiceRequested   ToolLifecycleEventType = "choice_requested"
	ToolLifecycleEventChoiceResolved    ToolLifecycleEventType = "choice_resolved"
)

type ToolLifecycleEvent struct {
	EventID        string
	InvocationID   string
	Type           ToolLifecycleEventType
	SessionID      string
	ToolName       string
	HostID         string
	CallID         string
	CardID         string
	ApprovalID     string
	Phase          string
	Label          string
	Message        string
	Error          string
	ActivityKind   string
	ActivityTarget string
	ActivityQuery  string
	CreatedAt      string
	Timestamp      time.Time
	Payload        map[string]any
	Metadata       map[string]any
}

type ToolLifecycleSubscriber interface {
	HandleToolLifecycleEvent(context.Context, ToolLifecycleEvent) error
}

type ToolLifecycleSubscriberFunc func(context.Context, ToolLifecycleEvent) error

func (f ToolLifecycleSubscriberFunc) HandleToolLifecycleEvent(ctx context.Context, event ToolLifecycleEvent) error {
	if f == nil {
		return nil
	}
	return f(ctx, event)
}

type ToolEventEmitter interface {
	Emit(context.Context, ToolLifecycleEvent) error
}

type InProcessToolEventBus struct {
	mu     sync.RWMutex
	nextID int64
	ids    []int64
	subs   map[int64]ToolLifecycleSubscriber
}

func NewInProcessToolEventBus() *InProcessToolEventBus {
	return &InProcessToolEventBus{
		subs: make(map[int64]ToolLifecycleSubscriber),
	}
}

func NewToolEventBus() *InProcessToolEventBus {
	return NewInProcessToolEventBus()
}

func (b *InProcessToolEventBus) Subscribe(sub any) func() {
	if b == nil || sub == nil {
		return func() {}
	}

	subscriber, ok := coerceToolLifecycleSubscriber(sub)
	if !ok {
		return func() {}
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subs == nil {
		b.subs = make(map[int64]ToolLifecycleSubscriber)
	}
	b.nextID++
	id := b.nextID
	b.subs[id] = subscriber
	b.ids = append(b.ids, id)

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.subs == nil {
			return
		}
		delete(b.subs, id)
		for i, current := range b.ids {
			if current != id {
				continue
			}
			b.ids = append(b.ids[:i], b.ids[i+1:]...)
			break
		}
	}
}

func (b *InProcessToolEventBus) Emit(ctx context.Context, event ToolLifecycleEvent) error {
	if b == nil {
		return nil
	}

	b.mu.RLock()
	if len(b.ids) == 0 {
		b.mu.RUnlock()
		return nil
	}
	snapshot := make([]ToolLifecycleSubscriber, 0, len(b.ids))
	for _, id := range b.ids {
		if sub, ok := b.subs[id]; ok && sub != nil {
			snapshot = append(snapshot, sub)
		}
	}
	b.mu.RUnlock()

	var errs []error
	for _, sub := range snapshot {
		if err := sub.HandleToolLifecycleEvent(ctx, event); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (b *InProcessToolEventBus) Publish(ctx context.Context, event ToolLifecycleEvent) error {
	return b.Emit(ctx, event)
}

func coerceToolLifecycleSubscriber(sub any) (ToolLifecycleSubscriber, bool) {
	switch candidate := sub.(type) {
	case ToolLifecycleSubscriber:
		return candidate, true
	case func(context.Context, ToolLifecycleEvent) error:
		return ToolLifecycleSubscriberFunc(candidate), true
	default:
		return nil, false
	}
}
