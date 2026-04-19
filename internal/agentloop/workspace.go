package agentloop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/lizhongxuan/aiops-codex/internal/orchestrator"
)

// WorkspaceWorkerCompleter is the minimal orchestrator surface required by the
// workspace runtime. It is intentionally narrow so this package does not need
// the full server/orchestrator dependency graph.
type WorkspaceWorkerCompleter interface {
	CompleteWorkerTurn(sessionID string, phase string, reply string) (*orchestrator.WorkerTurnOutcome, error)
}

type workspaceTurnKind string

const (
	workspaceTurnKindPlanner workspaceTurnKind = "planner"
	workspaceTurnKindWorker  workspaceTurnKind = "worker"
)

type workspaceTurn struct {
	kind      workspaceTurnKind
	session   *Session
	phase     string
	reply     string
	completed bool
}

// WorkspaceRuntime owns planner and worker sessions for a workspace. It keeps
// the sessions isolated while allowing the caller to notify an orchestrator
// when a worker turn is complete.
type WorkspaceRuntime struct {
	mu           sync.Mutex
	orchestrator WorkspaceWorkerCompleter
	plannerTurns map[string]*workspaceTurn
	workerTurns  map[string]*workspaceTurn
}

// NewWorkspaceRuntime creates a runtime with an optional orchestrator callback.
func NewWorkspaceRuntime(orchestrator WorkspaceWorkerCompleter) *WorkspaceRuntime {
	return &WorkspaceRuntime{
		orchestrator: orchestrator,
		plannerTurns: make(map[string]*workspaceTurn),
		workerTurns:  make(map[string]*workspaceTurn),
	}
}

// StartPlannerTurn creates or reuses a planner session and appends the user
// input to its context.
func (r *WorkspaceRuntime) StartPlannerTurn(ctx context.Context, sessionID string, spec SessionSpec, userInput string) (*Session, error) {
	return r.startTurn(ctx, workspaceTurnKindPlanner, sessionID, spec, userInput)
}

// StartWorkerTurn creates or reuses a worker session and appends the user input
// to its context.
func (r *WorkspaceRuntime) StartWorkerTurn(ctx context.Context, sessionID string, spec SessionSpec, userInput string) (*Session, error) {
	return r.startTurn(ctx, workspaceTurnKindWorker, sessionID, spec, userInput)
}

// CompleteWorkerTurn marks the worker session as finished and forwards the
// result to the injected orchestrator if present.
func (r *WorkspaceRuntime) CompleteWorkerTurn(sessionID string, phase string, reply string) (*orchestrator.WorkerTurnOutcome, error) {
	r.mu.Lock()
	turn, ok := r.workerTurns[sessionID]
	if !ok || turn == nil {
		r.mu.Unlock()
		return nil, fmt.Errorf("worker session %q not found", sessionID)
	}
	turn.phase = strings.TrimSpace(phase)
	turn.reply = strings.TrimSpace(reply)
	turn.completed = true
	completer := r.orchestrator
	r.mu.Unlock()

	if completer == nil {
		return nil, nil
	}
	return completer.CompleteWorkerTurn(sessionID, phase, reply)
}

// ResetPlannerTurn drops the cached planner session for sessionID.
// The next StartPlannerTurn call will create a fresh Session.
func (r *WorkspaceRuntime) ResetPlannerTurn(sessionID string) {
	r.resetTurn(workspaceTurnKindPlanner, sessionID)
}

// ResetWorkerTurn drops the cached worker session for sessionID.
// The next StartWorkerTurn call will create a fresh Session.
func (r *WorkspaceRuntime) ResetWorkerTurn(sessionID string) {
	r.resetTurn(workspaceTurnKindWorker, sessionID)
}

// ResetSession clears any cached planner and worker sessions associated with
// sessionID. This is the safe hook for callers that clear thread bindings.
func (r *WorkspaceRuntime) ResetSession(sessionID string) {
	r.ResetPlannerTurn(sessionID)
	r.ResetWorkerTurn(sessionID)
}

// PlannerSession returns the cached planner session if present.
func (r *WorkspaceRuntime) PlannerSession(sessionID string) (*Session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	turn, ok := r.plannerTurns[sessionID]
	if !ok || turn == nil {
		return nil, false
	}
	return turn.session, true
}

// WorkerSession returns the cached worker session if present.
func (r *WorkspaceRuntime) WorkerSession(sessionID string) (*Session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	turn, ok := r.workerTurns[sessionID]
	if !ok || turn == nil {
		return nil, false
	}
	return turn.session, true
}

func (r *WorkspaceRuntime) startTurn(ctx context.Context, kind workspaceTurnKind, sessionID string, spec SessionSpec, userInput string) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("workspace session id is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	switch kind {
	case workspaceTurnKindPlanner:
		if existing, ok := r.workerTurns[sessionID]; ok && existing != nil {
			return nil, fmt.Errorf("session %q is already registered as a worker session", sessionID)
		}
	case workspaceTurnKindWorker:
		if existing, ok := r.plannerTurns[sessionID]; ok && existing != nil {
			return nil, fmt.Errorf("session %q is already registered as a planner session", sessionID)
		}
	}

	turns := r.turnsForKind(kind)
	turn, ok := turns[sessionID]
	if !ok || turn == nil {
		turn = &workspaceTurn{
			kind:    kind,
			session: NewSession(sessionID, spec),
		}
		turns[sessionID] = turn
	}
	if userInput != "" {
		turn.session.ContextManager().AppendUser(userInput)
	}
	return turn.session, nil
}

func (r *WorkspaceRuntime) turnsForKind(kind workspaceTurnKind) map[string]*workspaceTurn {
	switch kind {
	case workspaceTurnKindPlanner:
		return r.plannerTurns
	default:
		return r.workerTurns
	}
}

func (r *WorkspaceRuntime) resetTurn(kind workspaceTurnKind, sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	turns := r.turnsForKind(kind)
	delete(turns, sessionID)
}
