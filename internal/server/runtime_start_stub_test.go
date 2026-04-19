package server

import (
	"context"
	"fmt"
	"sync"
)

type runtimeThreadStartCall struct {
	SessionID string
	Spec      threadStartSpec
}

type runtimeTurnStartCall struct {
	SessionID string
	ThreadID  string
	Spec      turnStartSpec
}

type runtimeStartStub struct {
	mu          sync.Mutex
	threadCalls []runtimeThreadStartCall
	turnCalls   []runtimeTurnStartCall
	startThread func(context.Context, string, threadStartSpec) (string, error)
	startTurn   func(context.Context, string, string, turnStartSpec) (string, error)
}

func (s *runtimeStartStub) install(app *App) {
	app.runtimeStartThreadFunc = func(ctx context.Context, sessionID string, spec threadStartSpec) (string, error) {
		s.mu.Lock()
		s.threadCalls = append(s.threadCalls, runtimeThreadStartCall{
			SessionID: sessionID,
			Spec:      spec,
		})
		callIndex := len(s.threadCalls)
		startThread := s.startThread
		s.mu.Unlock()
		if startThread != nil {
			return startThread(ctx, sessionID, spec)
		}
		return fmt.Sprintf("thread-stub-%02d", callIndex), nil
	}
	app.runtimeStartTurnFunc = func(ctx context.Context, sessionID, threadID string, spec turnStartSpec) (string, error) {
		s.mu.Lock()
		s.turnCalls = append(s.turnCalls, runtimeTurnStartCall{
			SessionID: sessionID,
			ThreadID:  threadID,
			Spec:      spec,
		})
		callIndex := len(s.turnCalls)
		startTurn := s.startTurn
		s.mu.Unlock()
		if startTurn != nil {
			return startTurn(ctx, sessionID, threadID, spec)
		}
		return fmt.Sprintf("turn-stub-%02d", callIndex), nil
	}
}

func (s *runtimeStartStub) threadStartCalls() []runtimeThreadStartCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]runtimeThreadStartCall(nil), s.threadCalls...)
}

func (s *runtimeStartStub) turnStartCalls() []runtimeTurnStartCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]runtimeTurnStartCall(nil), s.turnCalls...)
}
