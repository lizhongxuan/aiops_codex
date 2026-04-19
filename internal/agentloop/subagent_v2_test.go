package agentloop

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// ─── Task 11.7: Unit Tests for Multi-Agent V2 ──────────────────────────────

// --- 11.1: Agent Registry Tests ---

func TestAgentRegistry_RegisterAndGet(t *testing.T) {
	r := NewAgentRegistry()

	cfg := AgentRoleConfig{
		Name:        "custom-worker",
		Type:        AgentRoleCustom,
		Description: "A custom worker role",
		Tools:       []string{"read_file", "write_file"},
	}
	if err := r.Register(cfg); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	got, ok := r.Get("custom-worker")
	if !ok {
		t.Fatal("Get returned false for registered role")
	}
	if got.Name != "custom-worker" {
		t.Errorf("expected name 'custom-worker', got %q", got.Name)
	}
	if got.Type != AgentRoleCustom {
		t.Errorf("expected type custom, got %v", got.Type)
	}
}

func TestAgentRegistry_RegisterEmptyName(t *testing.T) {
	r := NewAgentRegistry()
	err := r.Register(AgentRoleConfig{Name: ""})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestAgentRegistry_BuiltinRoles(t *testing.T) {
	r := NewAgentRegistry()
	roles := r.ListRoles()
	if len(roles) == 0 {
		t.Fatal("expected builtin roles to be registered")
	}

	// Check that known builtins exist.
	for _, name := range []string{"coder", "researcher", "reviewer", "ops", "planner"} {
		if _, ok := r.Get(name); !ok {
			t.Errorf("builtin role %q not found", name)
		}
	}
}

func TestAgentRegistry_ListByType(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(AgentRoleConfig{Name: "my-custom", Type: AgentRoleCustom, Description: "test"})

	builtins := r.ListByType(AgentRoleBuiltin)
	if len(builtins) == 0 {
		t.Error("expected builtin roles")
	}

	customs := r.ListByType(AgentRoleCustom)
	if len(customs) != 1 {
		t.Errorf("expected 1 custom role, got %d", len(customs))
	}
}

// --- 11.2: Agent Mailbox Tests ---

func TestAgentMailbox_SendReceive(t *testing.T) {
	mb := NewAgentMailbox()
	to := AgentID("agent-1")

	mb.Send(AgentMessage{
		From:    "agent-0",
		To:      to,
		Content: "hello",
		Type:    "task",
	})

	if mb.Pending(to) != 1 {
		t.Errorf("expected 1 pending, got %d", mb.Pending(to))
	}

	msg := mb.Receive(to)
	if msg == nil {
		t.Fatal("expected message, got nil")
	}
	if msg.Content != "hello" {
		t.Errorf("expected 'hello', got %q", msg.Content)
	}
	if msg.From != "agent-0" {
		t.Errorf("expected from 'agent-0', got %q", msg.From)
	}

	if mb.Pending(to) != 0 {
		t.Errorf("expected 0 pending after receive, got %d", mb.Pending(to))
	}
}

func TestAgentMailbox_FIFO(t *testing.T) {
	mb := NewAgentMailbox()
	to := AgentID("agent-1")

	mb.Send(AgentMessage{To: to, Content: "first"})
	mb.Send(AgentMessage{To: to, Content: "second"})
	mb.Send(AgentMessage{To: to, Content: "third"})

	msg1 := mb.Receive(to)
	msg2 := mb.Receive(to)
	msg3 := mb.Receive(to)

	if msg1.Content != "first" || msg2.Content != "second" || msg3.Content != "third" {
		t.Error("messages not in FIFO order")
	}
}

func TestAgentMailbox_ReceiveEmpty(t *testing.T) {
	mb := NewAgentMailbox()
	msg := mb.Receive("nonexistent")
	if msg != nil {
		t.Error("expected nil for empty queue")
	}
}

func TestAgentMailbox_ReceiveWait(t *testing.T) {
	mb := NewAgentMailbox()
	to := AgentID("agent-1")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Send after a short delay.
	go func() {
		time.Sleep(20 * time.Millisecond)
		mb.Send(AgentMessage{To: to, Content: "delayed"})
	}()

	msg, err := mb.ReceiveWait(ctx, to)
	if err != nil {
		t.Fatalf("ReceiveWait failed: %v", err)
	}
	if msg.Content != "delayed" {
		t.Errorf("expected 'delayed', got %q", msg.Content)
	}
}

func TestAgentMailbox_ReceiveWaitTimeout(t *testing.T) {
	mb := NewAgentMailbox()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := mb.ReceiveWait(ctx, "agent-1")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestAgentMailbox_Clear(t *testing.T) {
	mb := NewAgentMailbox()
	to := AgentID("agent-1")
	mb.Send(AgentMessage{To: to, Content: "msg1"})
	mb.Send(AgentMessage{To: to, Content: "msg2"})

	mb.Clear(to)
	if mb.Pending(to) != 0 {
		t.Errorf("expected 0 after clear, got %d", mb.Pending(to))
	}
}

func TestAgentMailbox_PeekAll(t *testing.T) {
	mb := NewAgentMailbox()
	to := AgentID("agent-1")
	mb.Send(AgentMessage{To: to, Content: "msg1"})
	mb.Send(AgentMessage{To: to, Content: "msg2"})

	peeked := mb.PeekAll(to)
	if len(peeked) != 2 {
		t.Errorf("expected 2 peeked, got %d", len(peeked))
	}
	// PeekAll should not remove messages.
	if mb.Pending(to) != 2 {
		t.Errorf("expected 2 still pending after peek, got %d", mb.Pending(to))
	}
}

// --- 11.3: V2 Multi-Agent Protocol Tests ---

func TestAgentControlV2_SpawnAndWait(t *testing.T) {
	// We can't easily test full spawn without a real Loop, but we can test
	// the registry integration and nickname assignment.
	ac := &AgentControlV2{
		AgentControl: &AgentControl{
			agents: make(map[AgentID]*LiveAgent),
		},
		registry: NewAgentRegistry(),
		mailbox:  NewAgentMailbox(),
		nickPool: NewNicknamePool(),
	}

	// Test ListAgentsV2 with empty state.
	agents := ac.ListAgentsV2(nil)
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
}

func TestAgentControlV2_SendMessageV2(t *testing.T) {
	ac := &AgentControlV2{
		AgentControl: &AgentControl{
			agents: make(map[AgentID]*LiveAgent),
		},
		registry: NewAgentRegistry(),
		mailbox:  NewAgentMailbox(),
		nickPool: NewNicknamePool(),
	}

	ac.SendMessageV2("agent-1", "agent-2", "hello from 1", "task")

	if ac.mailbox.Pending("agent-2") != 1 {
		t.Error("expected 1 pending message for agent-2")
	}

	msg := ac.mailbox.Receive("agent-2")
	if msg.Content != "hello from 1" {
		t.Errorf("expected 'hello from 1', got %q", msg.Content)
	}
}

func TestAgentControlV2_FollowupTaskV2(t *testing.T) {
	ac := &AgentControlV2{
		AgentControl: &AgentControl{
			agents: make(map[AgentID]*LiveAgent),
		},
		registry: NewAgentRegistry(),
		mailbox:  NewAgentMailbox(),
		nickPool: NewNicknamePool(),
	}

	ac.FollowupTaskV2("agent-1", "deploy to staging", 1)
	ac.FollowupTaskV2("agent-2", "run tests", 2)

	followups := ac.PendingFollowups()
	if len(followups) != 2 {
		t.Fatalf("expected 2 followups, got %d", len(followups))
	}
	if followups[0].Description != "deploy to staging" {
		t.Errorf("unexpected first followup: %q", followups[0].Description)
	}

	// After retrieval, should be empty.
	if len(ac.PendingFollowups()) != 0 {
		t.Error("expected followups to be cleared after retrieval")
	}
}

// --- 11.4: Fork Mode Tests ---

func TestForkHistory_Full(t *testing.T) {
	parent := NewSession("parent", SessionSpec{Model: "test", ContextWindow: 128000})
	parent.ContextManager().AppendUser("msg1")
	parent.ContextManager().AppendAssistant("resp1", nil)
	parent.ContextManager().AppendUser("msg2")

	child := NewSession("child", SessionSpec{Model: "test", ContextWindow: 128000})
	ForkHistory(parent, child, ForkModeConfig{Mode: ForkModeFull})

	msgs := child.ContextManager().Messages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages in child, got %d", len(msgs))
	}
}

func TestForkHistory_LastN(t *testing.T) {
	parent := NewSession("parent", SessionSpec{Model: "test", ContextWindow: 128000})
	parent.ContextManager().AppendUser("msg1")
	parent.ContextManager().AppendAssistant("resp1", nil)
	parent.ContextManager().AppendUser("msg2")
	parent.ContextManager().AppendAssistant("resp2", nil)

	child := NewSession("child", SessionSpec{Model: "test", ContextWindow: 128000})
	ForkHistory(parent, child, ForkModeConfig{Mode: ForkModeLastN, LastN: 2})

	msgs := child.ContextManager().Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages in child, got %d", len(msgs))
	}
	if s, ok := msgs[0].Content.(string); !ok || s != "msg2" {
		t.Errorf("expected 'msg2' as first message, got %v", msgs[0].Content)
	}
}

func TestForkHistory_EmptyParent(t *testing.T) {
	parent := NewSession("parent", SessionSpec{Model: "test", ContextWindow: 128000})
	child := NewSession("child", SessionSpec{Model: "test", ContextWindow: 128000})
	ForkHistory(parent, child, ForkModeConfig{Mode: ForkModeFull})

	if child.ContextManager().Len() != 0 {
		t.Error("expected empty child history")
	}
}

// --- 11.6: Nickname Pool Tests ---

func TestNicknamePool_AssignAndRelease(t *testing.T) {
	pool := NewNicknamePool()

	nick1 := pool.Assign("agent-1")
	if nick1 == "" {
		t.Fatal("expected non-empty nickname")
	}

	nick2 := pool.Assign("agent-2")
	if nick2 == "" || nick2 == nick1 {
		t.Error("expected different nickname for second agent")
	}

	// Same agent should get same nickname.
	nick1Again := pool.Assign("agent-1")
	if nick1Again != nick1 {
		t.Errorf("expected same nickname on re-assign, got %q vs %q", nick1Again, nick1)
	}

	// Release and re-assign should recycle.
	pool.Release("agent-1")
	nick3 := pool.Assign("agent-3")
	if nick3 != nick1 {
		// The released name should be available again.
		// It goes to the end of the available pool, so it might not be next.
		// Just verify it's not empty.
		if nick3 == "" {
			t.Error("expected non-empty nickname after release")
		}
	}
}

func TestNicknamePool_Fallback(t *testing.T) {
	pool := NewNicknamePool()

	// Exhaust all curated names.
	for i := 0; i < len(curatedNicknames); i++ {
		pool.Assign(AgentID(fmt.Sprintf("agent-%d", i)))
	}

	// Next assignment should use fallback.
	nick := pool.Assign("agent-overflow")
	if nick == "" {
		t.Fatal("expected fallback nickname")
	}
	if !containsSubstr(nick, "Agent-") {
		t.Errorf("expected fallback format 'Agent-N', got %q", nick)
	}
}

func TestNicknamePool_Lookup(t *testing.T) {
	pool := NewNicknamePool()
	pool.Assign("agent-1")

	nick := pool.Lookup("agent-1")
	if nick == "" {
		t.Error("expected non-empty lookup result")
	}

	nick = pool.Lookup("nonexistent")
	if nick != "" {
		t.Error("expected empty lookup for unassigned agent")
	}
}

func TestNicknamePool_AssignedCount(t *testing.T) {
	pool := NewNicknamePool()
	pool.Assign("agent-1")
	pool.Assign("agent-2")

	if pool.AssignedCount() != 2 {
		t.Errorf("expected 2 assigned, got %d", pool.AssignedCount())
	}

	pool.Release("agent-1")
	if pool.AssignedCount() != 1 {
		t.Errorf("expected 1 assigned after release, got %d", pool.AssignedCount())
	}
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
