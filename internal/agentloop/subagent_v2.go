package agentloop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ─── Task 11.1: Agent Registry ─────────────────────────────────────────────

// AgentRoleType distinguishes built-in roles from custom ones.
type AgentRoleType string

const (
	AgentRoleBuiltin AgentRoleType = "builtin"
	AgentRoleCustom  AgentRoleType = "custom"
)

// AgentRoleConfig describes a registered agent role with its capabilities.
type AgentRoleConfig struct {
	// Name is the unique identifier for this role.
	Name string `json:"name"`
	// Type indicates whether this is a builtin or custom role.
	Type AgentRoleType `json:"type"`
	// Description explains what this role does.
	Description string `json:"description"`
	// SystemPrompt is the system prompt template for agents of this role.
	SystemPrompt string `json:"system_prompt"`
	// Tools is the set of tools available to this role.
	Tools []string `json:"tools"`
	// MaxIterations is the default iteration budget for this role.
	MaxIterations int `json:"max_iterations,omitempty"`
	// Model overrides the default model for this role (optional).
	Model string `json:"model,omitempty"`
}

// AgentRegistry manages available agent roles (both builtin and custom).
type AgentRegistry struct {
	mu    sync.RWMutex
	roles map[string]*AgentRoleConfig
}

// NewAgentRegistry creates an AgentRegistry pre-populated with builtin roles.
func NewAgentRegistry() *AgentRegistry {
	r := &AgentRegistry{
		roles: make(map[string]*AgentRoleConfig),
	}
	r.registerBuiltins()
	return r
}

// Register adds or replaces a role in the registry.
func (r *AgentRegistry) Register(cfg AgentRoleConfig) error {
	if strings.TrimSpace(cfg.Name) == "" {
		return errors.New("agent role name is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.roles[cfg.Name] = &cfg
	return nil
}

// Get retrieves a role config by name.
func (r *AgentRegistry) Get(name string) (*AgentRoleConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cfg, ok := r.roles[name]
	return cfg, ok
}

// ListRoles returns all registered role names.
func (r *AgentRegistry) ListRoles() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.roles))
	for name := range r.roles {
		names = append(names, name)
	}
	return names
}

// ListByType returns roles filtered by type.
func (r *AgentRegistry) ListByType(roleType AgentRoleType) []*AgentRoleConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*AgentRoleConfig
	for _, cfg := range r.roles {
		if cfg.Type == roleType {
			out = append(out, cfg)
		}
	}
	return out
}

func (r *AgentRegistry) registerBuiltins() {
	builtins := []AgentRoleConfig{
		{
			Name:         "coder",
			Type:         AgentRoleBuiltin,
			Description:  "General-purpose coding agent for file edits and code generation",
			SystemPrompt: "You are a coding assistant. Focus on writing clean, correct code.",
			Tools:        []string{"read_file", "write_file", "shell_exec", "list_files", "search_files"},
		},
		{
			Name:         "researcher",
			Type:         AgentRoleBuiltin,
			Description:  "Research agent for gathering information and analysis",
			SystemPrompt: "You are a research assistant. Gather information and provide analysis.",
			Tools:        []string{"read_file", "list_files", "search_files", "shell_exec"},
		},
		{
			Name:         "reviewer",
			Type:         AgentRoleBuiltin,
			Description:  "Code review agent for analyzing changes and suggesting improvements",
			SystemPrompt: "You are a code reviewer. Analyze code for correctness, style, and potential issues.",
			Tools:        []string{"read_file", "list_files", "search_files"},
		},
		{
			Name:         "ops",
			Type:         AgentRoleBuiltin,
			Description:  "Operations agent for infrastructure and deployment tasks",
			SystemPrompt: "You are an operations assistant. Help with infrastructure, deployment, and monitoring.",
			Tools:        []string{"shell_exec", "read_file", "write_file", "host_summary", "coroot_metrics"},
		},
		{
			Name:         "planner",
			Type:         AgentRoleBuiltin,
			Description:  "Planning agent for breaking down complex tasks",
			SystemPrompt: "You are a planning assistant. Break down complex tasks into actionable steps.",
			Tools:        []string{"read_file", "list_files", "search_files"},
		},
	}
	for _, b := range builtins {
		r.roles[b.Name] = &AgentRoleConfig{
			Name:         b.Name,
			Type:         b.Type,
			Description:  b.Description,
			SystemPrompt: b.SystemPrompt,
			Tools:        b.Tools,
		}
	}
}

// ─── Task 11.2: Agent Mailbox ───────────────────────────────────────────────

// AgentMessage represents a message sent between agents.
type AgentMessage struct {
	// From is the sender agent ID.
	From AgentID `json:"from"`
	// To is the recipient agent ID.
	To AgentID `json:"to"`
	// Content is the message payload.
	Content string `json:"content"`
	// Timestamp is when the message was sent.
	Timestamp time.Time `json:"timestamp"`
	// Type categorizes the message (e.g., "task", "result", "query").
	Type string `json:"type,omitempty"`
}

// AgentMailbox provides FIFO message passing between agents.
type AgentMailbox struct {
	mu       sync.Mutex
	queues   map[AgentID][]AgentMessage
	notifyCh map[AgentID]chan struct{}
}

// NewAgentMailbox creates an empty mailbox system.
func NewAgentMailbox() *AgentMailbox {
	return &AgentMailbox{
		queues:   make(map[AgentID][]AgentMessage),
		notifyCh: make(map[AgentID]chan struct{}),
	}
}

// Send delivers a message to the recipient's queue (FIFO).
func (mb *AgentMailbox) Send(msg AgentMessage) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	mb.queues[msg.To] = append(mb.queues[msg.To], msg)
	// Notify waiting receivers.
	if ch, ok := mb.notifyCh[msg.To]; ok {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Receive retrieves and removes the next message for the given agent.
// Returns nil if no messages are available.
func (mb *AgentMailbox) Receive(agentID AgentID) *AgentMessage {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	q := mb.queues[agentID]
	if len(q) == 0 {
		return nil
	}
	msg := q[0]
	mb.queues[agentID] = q[1:]
	return &msg
}

// ReceiveWait blocks until a message is available or context is cancelled.
func (mb *AgentMailbox) ReceiveWait(ctx context.Context, agentID AgentID) (*AgentMessage, error) {
	// Try immediate receive first.
	if msg := mb.Receive(agentID); msg != nil {
		return msg, nil
	}

	// Set up notification channel.
	mb.mu.Lock()
	ch, ok := mb.notifyCh[agentID]
	if !ok {
		ch = make(chan struct{}, 1)
		mb.notifyCh[agentID] = ch
	}
	mb.mu.Unlock()

	for {
		select {
		case <-ch:
			if msg := mb.Receive(agentID); msg != nil {
				return msg, nil
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Pending returns the number of unread messages for the given agent.
func (mb *AgentMailbox) Pending(agentID AgentID) int {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	return len(mb.queues[agentID])
}

// PeekAll returns all pending messages without removing them.
func (mb *AgentMailbox) PeekAll(agentID AgentID) []AgentMessage {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	q := mb.queues[agentID]
	out := make([]AgentMessage, len(q))
	copy(out, q)
	return out
}

// Clear removes all pending messages for the given agent.
func (mb *AgentMailbox) Clear(agentID AgentID) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	delete(mb.queues, agentID)
}

// ─── Task 11.3: V2 Multi-Agent Protocol ─────────────────────────────────────

// FollowupTask represents a task delegated back to the parent agent.
type FollowupTask struct {
	AgentID     AgentID `json:"agent_id"`
	Description string  `json:"description"`
	Priority    int     `json:"priority,omitempty"`
}

// AgentControlV2 extends AgentControl with V2 protocol features:
// registry-based spawning, mailbox messaging, and followup tasks.
type AgentControlV2 struct {
	*AgentControl
	registry *AgentRegistry
	mailbox  *AgentMailbox
	nickPool *NicknamePool

	mu         sync.Mutex
	followups  []FollowupTask
}

// NewAgentControlV2 creates a V2 agent control with registry and mailbox.
func NewAgentControlV2(loop *Loop) *AgentControlV2 {
	return &AgentControlV2{
		AgentControl: NewAgentControl(loop),
		registry:     NewAgentRegistry(),
		mailbox:      NewAgentMailbox(),
		nickPool:     NewNicknamePool(),
	}
}

// Registry returns the agent registry.
func (ac *AgentControlV2) Registry() *AgentRegistry {
	return ac.registry
}

// Mailbox returns the agent mailbox.
func (ac *AgentControlV2) Mailbox() *AgentMailbox {
	return ac.mailbox
}

// SpawnAgentV2 creates a subagent using a registered role config.
// It applies role defaults (tools, system prompt, model) and assigns a nickname.
func (ac *AgentControlV2) SpawnAgentV2(ctx context.Context, parentSession *Session, req SpawnAgentRequest, roleName string) (*LiveAgent, error) {
	// Look up role config if specified.
	if roleName != "" {
		roleCfg, ok := ac.registry.Get(roleName)
		if !ok {
			return nil, fmt.Errorf("unknown agent role %q", roleName)
		}
		// Apply role defaults where request doesn't override.
		if len(req.Tools) == 0 {
			req.Tools = roleCfg.Tools
		}
		if req.Model == "" && roleCfg.Model != "" {
			req.Model = roleCfg.Model
		}
		if req.MaxIterations == 0 && roleCfg.MaxIterations > 0 {
			req.MaxIterations = roleCfg.MaxIterations
		}
	}

	agent, err := ac.AgentControl.SpawnAgent(ctx, parentSession, req)
	if err != nil {
		return nil, err
	}

	// Assign a nickname.
	nickname := ac.nickPool.Assign(agent.ID)
	if agent.Session.Metadata == nil {
		agent.Session.Metadata = make(map[string]string)
	}
	agent.Session.Metadata["nickname"] = nickname
	if roleName != "" {
		agent.Session.Metadata["role"] = roleName
	}

	return agent, nil
}

// WaitAgentV2 waits for an agent and cleans up its nickname on completion.
func (ac *AgentControlV2) WaitAgentV2(ctx context.Context, id AgentID) (*AgentResult, error) {
	result, err := ac.AgentControl.WaitAgent(ctx, id)
	if err != nil {
		return nil, err
	}
	ac.nickPool.Release(id)
	return result, nil
}

// SendMessageV2 sends a message between agents via the mailbox.
func (ac *AgentControlV2) SendMessageV2(from, to AgentID, content, msgType string) {
	ac.mailbox.Send(AgentMessage{
		From:    from,
		To:      to,
		Content: content,
		Type:    msgType,
	})
}

// CloseAgentV2 closes an agent and releases its nickname.
func (ac *AgentControlV2) CloseAgentV2(id AgentID) error {
	ac.nickPool.Release(id)
	ac.mailbox.Clear(id)
	return ac.AgentControl.CloseAgent(id)
}

// ListAgentsV2 returns all live agents with their nicknames and roles.
func (ac *AgentControlV2) ListAgentsV2(parentID *AgentID) []AgentInfoV2 {
	agents := ac.AgentControl.ListAgents(parentID)
	infos := make([]AgentInfoV2, 0, len(agents))
	for _, a := range agents {
		info := AgentInfoV2{
			ID:        a.ID,
			ParentID:  a.ParentID,
			Status:    a.Status,
			CreatedAt: a.CreatedAt,
		}
		if a.Session != nil && a.Session.Metadata != nil {
			info.Nickname = a.Session.Metadata["nickname"]
			info.Role = a.Session.Metadata["role"]
		}
		infos = append(infos, info)
	}
	return infos
}

// AgentInfoV2 is the V2 agent listing entry with nickname and role.
type AgentInfoV2 struct {
	ID        AgentID     `json:"id"`
	ParentID  AgentID     `json:"parent_id"`
	Status    AgentStatus `json:"status"`
	Nickname  string      `json:"nickname,omitempty"`
	Role      string      `json:"role,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
}

// FollowupTaskV2 registers a followup task to be handled by the parent.
func (ac *AgentControlV2) FollowupTaskV2(agentID AgentID, description string, priority int) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.followups = append(ac.followups, FollowupTask{
		AgentID:     agentID,
		Description: description,
		Priority:    priority,
	})
}

// PendingFollowups returns and clears all pending followup tasks.
func (ac *AgentControlV2) PendingFollowups() []FollowupTask {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	out := ac.followups
	ac.followups = nil
	return out
}

// ─── Task 11.4: Fork Mode ───────────────────────────────────────────────────

// ForkMode specifies how parent history is copied to a child agent.
type ForkMode string

const (
	// ForkModeFull copies the entire parent history to the child.
	ForkModeFull ForkMode = "full"
	// ForkModeLastN copies only the last N messages from parent history.
	ForkModeLastN ForkMode = "last_n"
)

// ForkModeConfig configures how history is forked to child agents.
type ForkModeConfig struct {
	// Mode is either "full" or "last_n".
	Mode ForkMode `json:"mode"`
	// LastN is the number of messages to copy when Mode is "last_n".
	// Ignored when Mode is "full".
	LastN int `json:"last_n,omitempty"`
}

// DefaultForkModeConfig returns the default fork mode (full copy).
func DefaultForkModeConfig() ForkModeConfig {
	return ForkModeConfig{Mode: ForkModeFull}
}

// ForkHistory copies parent session history to a child session based on the fork config.
func ForkHistory(parent *Session, child *Session, cfg ForkModeConfig) {
	msgs := parent.ContextManager().Messages()
	if len(msgs) == 0 {
		return
	}

	switch cfg.Mode {
	case ForkModeLastN:
		if cfg.LastN > 0 && cfg.LastN < len(msgs) {
			msgs = msgs[len(msgs)-cfg.LastN:]
		}
	case ForkModeFull:
		// Copy all messages.
	default:
		// Default to full.
	}

	for _, m := range msgs {
		child.ContextManager().Append(m)
	}
}

// SpawnAgentWithFork creates a subagent and copies parent history per ForkModeConfig.
func (ac *AgentControlV2) SpawnAgentWithFork(ctx context.Context, parentSession *Session, req SpawnAgentRequest, roleName string, forkCfg ForkModeConfig) (*LiveAgent, error) {
	agent, err := ac.SpawnAgentV2(ctx, parentSession, req, roleName)
	if err != nil {
		return nil, err
	}
	ForkHistory(parentSession, agent.Session, forkCfg)
	return agent, nil
}

// ─── Task 11.5: Resume Agent ────────────────────────────────────────────────

// ResumeAgent loads a previously persisted agent session from the SessionStore
// and re-registers it as a live agent in the control.
func (ac *AgentControlV2) ResumeAgent(ctx context.Context, store *SessionStore, sessionID string) (*LiveAgent, error) {
	session, err := store.Load(sessionID)
	if err != nil {
		return nil, fmt.Errorf("resume agent: %w", err)
	}

	id := AgentID(session.ID)
	agentCtx, cancel := context.WithCancel(ctx)

	agent := &LiveAgent{
		ID:        id,
		Session:   session,
		Status:    AgentStatusIdle,
		CreatedAt: time.Now(),
		cancelFn:  cancel,
		doneCh:    make(chan struct{}),
	}

	ac.AgentControl.mu.Lock()
	ac.AgentControl.agents[id] = agent
	ac.AgentControl.mu.Unlock()

	// Assign a nickname for the resumed agent.
	nickname := ac.nickPool.Assign(id)
	if session.Metadata == nil {
		session.Metadata = make(map[string]string)
	}
	session.Metadata["nickname"] = nickname

	_ = agentCtx // context available for future RunTurn calls
	return agent, nil
}

// ─── Task 11.6: Agent Nicknames ─────────────────────────────────────────────

// curatedNicknames is a pool of friendly names for agents.
var curatedNicknames = []string{
	"Atlas", "Bolt", "Cedar", "Dash", "Echo",
	"Flint", "Gale", "Haze", "Iris", "Jade",
	"Kite", "Lark", "Mesa", "Nova", "Onyx",
	"Pike", "Quill", "Reed", "Sage", "Thorn",
	"Umber", "Vale", "Wren", "Xeno", "Yew", "Zinc",
}

// NicknamePool manages assignment and release of friendly agent nicknames.
type NicknamePool struct {
	mu        sync.Mutex
	available []string
	assigned  map[AgentID]string
	counter   int
}

// NewNicknamePool creates a pool pre-loaded with curated nicknames.
func NewNicknamePool() *NicknamePool {
	pool := make([]string, len(curatedNicknames))
	copy(pool, curatedNicknames)
	return &NicknamePool{
		available: pool,
		assigned:  make(map[AgentID]string),
	}
}

// Assign picks a nickname for the given agent. If the curated pool is
// exhausted, it falls back to "Agent-N" naming.
func (np *NicknamePool) Assign(id AgentID) string {
	np.mu.Lock()
	defer np.mu.Unlock()

	// Check if already assigned.
	if nick, ok := np.assigned[id]; ok {
		return nick
	}

	var nickname string
	if len(np.available) > 0 {
		nickname = np.available[0]
		np.available = np.available[1:]
	} else {
		np.counter++
		nickname = fmt.Sprintf("Agent-%d", np.counter)
	}

	np.assigned[id] = nickname
	return nickname
}

// Release returns a nickname to the pool for reuse.
func (np *NicknamePool) Release(id AgentID) {
	np.mu.Lock()
	defer np.mu.Unlock()
	nick, ok := np.assigned[id]
	if !ok {
		return
	}
	delete(np.assigned, id)
	// Only return curated names to the pool.
	for _, cn := range curatedNicknames {
		if cn == nick {
			np.available = append(np.available, nick)
			return
		}
	}
}

// Lookup returns the nickname for a given agent ID, or empty string if not assigned.
func (np *NicknamePool) Lookup(id AgentID) string {
	np.mu.Lock()
	defer np.mu.Unlock()
	return np.assigned[id]
}

// AssignedCount returns the number of currently assigned nicknames.
func (np *NicknamePool) AssignedCount() int {
	np.mu.Lock()
	defer np.mu.Unlock()
	return len(np.assigned)
}
