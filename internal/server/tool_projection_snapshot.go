package server

import (
	"context"
	"strings"
)

type snapshotBroadcastProjection struct {
	schedule  func(string)
	immediate func(string)
}

func NewSnapshotBroadcastSubscriber(app *App) ToolLifecycleSubscriber {
	if app == nil {
		return snapshotBroadcastProjection{}
	}
	return snapshotBroadcastProjection{
		schedule: func(sessionID string) {
			app.throttledBroadcast(sessionID)
		},
		immediate: func(sessionID string) {
			app.flushThrottledBroadcast(sessionID)
			app.broadcastSnapshot(sessionID)
		},
	}
}

func (p snapshotBroadcastProjection) HandleToolLifecycleEvent(_ context.Context, event ToolLifecycleEvent) error {
	sessionID := strings.TrimSpace(event.SessionID)
	if sessionID == "" {
		return nil
	}

	switch event.Type {
	case ToolLifecycleEventStarted, ToolLifecycleEventProgress:
		if toolProjectionDisplayMapFromEvent(event) != nil {
			if p.immediate != nil {
				p.immediate(sessionID)
			}
			return nil
		}
		if p.schedule != nil {
			p.schedule(sessionID)
		}
	default:
		if p.immediate != nil {
			p.immediate(sessionID)
		}
	}
	return nil
}
