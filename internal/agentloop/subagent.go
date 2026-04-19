package agentloop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---------- Subagent types ----------

// AgentID is a unique identifier for a subagent.
type AgentID string

// AgentStatus represents the lifecycle state of a subagent.
type AgentStatus string

const (
	AgentStatusIdle      AgentStatus = "idle"
	AgentStatusRunning   AgentStatus = "running"
	AgentStatusCompleted AgentStatus = "completed"
	AgentStatusFailed    AgentStatus = "failed"
	AgentStatusCancelled AgentStatus = "cancelled"
)

// SpawnAgentRequest describes how to create a new subagent.
type SpawnAgentRequest struct {
	// Prompt is the initial instruction for the subagent.
	Prompt string `json:"prompt"`
	// Model overrides the parent session model (optional).
	Model string `json:"model,omitempty"`
	// Tools restricts the subagent to a subset of tools (optional, empty = inherit parent).
	Tools []string `json:"tools,omitempty"`
	// Cwd overrides the working directory (optional).
	Cwd string `json:"cwd,omitempty"`
	// MaxIterations overrides the iteration budget (optional, 0 = inherit parent).
	MaxIterations int `json:"max_iterations,omitempty"`
	// ParentID links to the spawning agent (set automatically).
	ParentID AgentID `json:"-"`
}

// AgentResult holds the outcome of a completed subagent.
type AgentResult struct {
	AgentID AgentID     `json:"agent_id"`
	Status  AgentStatus `json:"status"`
	Output  string      `json:"output"`
	Error   string      `json:"error,omitempty"`
}

// LiveAgent tracks a running subagent.
type LiveAgent struct {
	ID        AgentID
	ParentID  AgentID
	Session   *Session
	Status    AgentStatus
	CreatedAt time.Time
	Result    *AgentResult
	cancelFn  context.CancelFunc
	doneCh    chan struct{}
}

// ---------- AgentControl ----------

var agentCounter uint64

func nextAgentID() AgentID {
	n := atomic.AddUint64(&agentCounter, 1)
	return AgentID(fmt.Sprintf("agent-%d-%d", time.Now().UnixMilli(), n))
}

// MaxSpawnDepth limits how deep the agent tree can go.
const MaxSpawnDepth = 5

// AgentControl manages the lifecycle of subagents within a workspace.
// It is inspired by Codex's AgentControl (core/src/agent/control.rs).
type AgentControl struct {
	mu     sync.Mutex
	agents map[AgentID]*LiveAgent
	loop   *Loop
}

// NewAgentControl creates a new AgentControl bound to the given Loop.
func NewAgentControl(loop *Loop) *AgentControl {
	return &AgentControl{
		agents: make(map[AgentID]*LiveAgent),
		loop:   loop,
	}
}

// SpawnAgent creates and starts a new subagent. The subagent runs in its own
// goroutine with an independent Session and returns its result via the
// LiveAgent.doneCh channel.
func (ac *AgentControl) SpawnAgent(ctx context.Context, parentSession *Session, req SpawnAgentRequest) (*LiveAgent, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, errors.New("subagent prompt is required")
	}

	depth := ac.spawnDepth(req.ParentID)
	if depth >= MaxSpawnDepth {
		return nil, fmt.Errorf("subagent spawn depth limit (%d) exceeded", MaxSpawnDepth)
	}

	id := nextAgentID()

	// Build subagent session spec from parent, with overrides.
	model := parentSession.Model()
	if req.Model != "" {
		model = req.Model
	}
	tools := parentSession.EnabledTools()
	if len(req.Tools) > 0 {
		tools = req.Tools
	}
	maxIter := parentSession.MaxIterations()
	if req.MaxIterations > 0 {
		maxIter = req.MaxIterations
	}

	spec := SessionSpec{
		Model:                 model,
		DynamicTools:          tools,
		MaxIterations:         maxIter,
		ContextWindow:         parentSession.ctxMgr.contextWindow,
		DeveloperInstructions: fmt.Sprintf("You are a subagent spawned to handle a specific subtask. Focus on completing the assigned task and report back concisely.\n\nParent agent ID: %s", parentSession.ID),
	}

	session := NewSession(string(id), spec)

	agentCtx, cancel := context.WithCancel(ctx)
	agent := &LiveAgent{
		ID:        id,
		ParentID:  req.ParentID,
		Session:   session,
		Status:    AgentStatusRunning,
		CreatedAt: time.Now(),
		cancelFn:  cancel,
		doneCh:    make(chan struct{}),
	}

	ac.mu.Lock()
	ac.agents[id] = agent
	ac.mu.Unlock()

	// Run the subagent in a goroutine.
	go func() {
		defer close(agent.doneCh)
		err := ac.loop.RunTurn(agentCtx, session, req.Prompt)

		ac.mu.Lock()
		defer ac.mu.Unlock()

		result := &AgentResult{AgentID: id}
		if err != nil {
			if agentCtx.Err() != nil {
				agent.Status = AgentStatusCancelled
				result.Status = AgentStatusCancelled
				result.Error = "cancelled"
			} else {
				agent.Status = AgentStatusFailed
				result.Status = AgentStatusFailed
				result.Error = err.Error()
			}
		} else {
			agent.Status = AgentStatusCompleted
			result.Status = AgentStatusCompleted
			// Extract the last assistant message as output.
			msgs := session.ContextManager().Messages()
			for i := len(msgs) - 1; i >= 0; i-- {
				if msgs[i].Role == "assistant" {
					if s, ok := msgs[i].Content.(string); ok {
						result.Output = s
						break
					}
				}
			}
		}
		agent.Result = result
	}()

	return agent, nil
}

// WaitAgent blocks until the specified agent completes or the context is cancelled.
func (ac *AgentControl) WaitAgent(ctx context.Context, id AgentID) (*AgentResult, error) {
	ac.mu.Lock()
	agent, ok := ac.agents[id]
	ac.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("agent %q not found", id)
	}

	select {
	case <-agent.doneCh:
		return agent.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// WaitMultiple waits for all specified agents to complete.
func (ac *AgentControl) WaitMultiple(ctx context.Context, ids []AgentID) ([]AgentResult, error) {
	results := make([]AgentResult, 0, len(ids))
	for _, id := range ids {
		r, err := ac.WaitAgent(ctx, id)
		if err != nil {
			return results, err
		}
		results = append(results, *r)
	}
	return results, nil
}

// SendInput sends additional input to a running subagent.
func (ac *AgentControl) SendInput(ctx context.Context, id AgentID, input string) error {
	ac.mu.Lock()
	agent, ok := ac.agents[id]
	ac.mu.Unlock()
	if !ok {
		return fmt.Errorf("agent %q not found", id)
	}
	if agent.Status != AgentStatusRunning {
		return fmt.Errorf("agent %q is not running (status: %s)", id, agent.Status)
	}
	agent.Session.ContextManager().AppendUser(input)
	return nil
}

// InterruptAgent cancels a running subagent.
func (ac *AgentControl) InterruptAgent(id AgentID) error {
	ac.mu.Lock()
	agent, ok := ac.agents[id]
	ac.mu.Unlock()
	if !ok {
		return fmt.Errorf("agent %q not found", id)
	}
	if agent.cancelFn != nil {
		agent.cancelFn()
	}
	return nil
}

// CloseAgent cancels and removes a subagent and all its descendants.
func (ac *AgentControl) CloseAgent(id AgentID) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.closeAgentLocked(id)
}

func (ac *AgentControl) closeAgentLocked(id AgentID) error {
	agent, ok := ac.agents[id]
	if !ok {
		return fmt.Errorf("agent %q not found", id)
	}
	// Cancel the agent.
	if agent.cancelFn != nil {
		agent.cancelFn()
	}
	// Close all children recursively.
	for childID, child := range ac.agents {
		if child.ParentID == id {
			_ = ac.closeAgentLocked(childID)
		}
	}
	delete(ac.agents, id)
	return nil
}

// GetStatus returns the current status of an agent.
func (ac *AgentControl) GetStatus(id AgentID) (AgentStatus, error) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	agent, ok := ac.agents[id]
	if !ok {
		return "", fmt.Errorf("agent %q not found", id)
	}
	return agent.Status, nil
}

// ListAgents returns all live agents, optionally filtered by parent.
func (ac *AgentControl) ListAgents(parentID *AgentID) []LiveAgent {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	var out []LiveAgent
	for _, a := range ac.agents {
		if parentID != nil && a.ParentID != *parentID {
			continue
		}
		out = append(out, *a)
	}
	return out
}

// spawnDepth calculates how deep in the agent tree a given parent is.
func (ac *AgentControl) spawnDepth(parentID AgentID) int {
	depth := 0
	current := parentID
	for current != "" {
		ac.mu.Lock()
		agent, ok := ac.agents[current]
		ac.mu.Unlock()
		if !ok {
			break
		}
		depth++
		current = agent.ParentID
	}
	return depth
}
