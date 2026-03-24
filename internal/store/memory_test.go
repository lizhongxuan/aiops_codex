package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func TestMarkStaleHostsMarksOffline(t *testing.T) {
	st := New()
	st.UpsertHost(model.Host{
		ID:            "agent-timeout",
		Name:          "agent-timeout",
		Kind:          "agent",
		Status:        "online",
		Executable:    false,
		LastHeartbeat: time.Now().Add(-2 * time.Minute).Format(time.RFC3339),
	})

	changed := st.MarkStaleHosts(45 * time.Second)
	if len(changed) != 1 || changed[0] != "agent-timeout" {
		t.Fatalf("expected stale host to be marked offline, got %#v", changed)
	}

	hosts := st.Hosts()
	for _, host := range hosts {
		if host.ID == "agent-timeout" && host.Status != "offline" {
			t.Fatalf("expected agent-timeout to be offline, got %q", host.Status)
		}
	}
}

func TestApprovalGrantPersistsAndRestores(t *testing.T) {
	st := New()
	sessionID := "sess-test"
	st.EnsureSession(sessionID)

	grant := model.ApprovalGrant{
		ID:          "grant-1",
		HostID:      "server-local",
		Type:        "command",
		Fingerprint: "command|server-local|/tmp|rm /tmp/demo.txt",
		Command:     "rm /tmp/demo.txt",
		Cwd:         "/tmp",
		CreatedAt:   model.NowString(),
	}
	st.AddApprovalGrant(sessionID, grant)

	if _, ok := st.ApprovalGrant(sessionID, grant.Fingerprint); !ok {
		t.Fatalf("expected approval grant to be found before persistence")
	}

	statePath := filepath.Join(t.TempDir(), "state.json")
	st.SetStatePath(statePath)
	if err := st.SaveStableState(statePath); err != nil {
		t.Fatalf("save state: %v", err)
	}

	reloaded := New()
	reloaded.SetStatePath(statePath)
	if err := reloaded.LoadStableState(statePath); err != nil {
		t.Fatalf("load state: %v", err)
	}

	got, ok := reloaded.ApprovalGrant(sessionID, grant.Fingerprint)
	if !ok {
		t.Fatalf("expected approval grant to be restored")
	}
	if got.Command != grant.Command || got.HostID != grant.HostID {
		t.Fatalf("unexpected restored grant: %#v", got)
	}
}

func TestThreadIDIsNotRestoredFromStableState(t *testing.T) {
	st := New()
	sessionID := "sess-thread"
	st.EnsureSession(sessionID)
	st.SetThread(sessionID, "thread-stale")

	statePath := filepath.Join(t.TempDir(), "state.json")
	st.SetStatePath(statePath)
	if err := st.SaveStableState(statePath); err != nil {
		t.Fatalf("save state: %v", err)
	}

	reloaded := New()
	reloaded.SetStatePath(statePath)
	if err := reloaded.LoadStableState(statePath); err != nil {
		t.Fatalf("load state: %v", err)
	}

	session := reloaded.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session to be restored")
	}
	if session.ThreadID != "" {
		t.Fatalf("expected thread id to be cleared after reload, got %q", session.ThreadID)
	}
	if got := reloaded.SessionIDByThread("thread-stale"); got != "" {
		t.Fatalf("expected stale thread mapping to be cleared, got %q", got)
	}
}

func TestResetConversationClearsThreadCardsAndApprovals(t *testing.T) {
	st := New()
	sessionID := "sess-reset"
	st.EnsureSession(sessionID)
	st.SetThread(sessionID, "thread-live")
	st.UpsertCard(sessionID, model.Card{
		ID:        "card-1",
		Type:      "MessageCard",
		Text:      "hello",
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	})
	st.AddApproval(sessionID, model.ApprovalRequest{
		ID:          "approval-1",
		Type:        "command",
		Status:      "pending",
		ThreadID:    "thread-live",
		RequestedAt: model.NowString(),
	})
	st.AddApprovalGrant(sessionID, model.ApprovalGrant{
		ID:          "grant-1",
		Type:        "command",
		Fingerprint: "command|server-local|/tmp|rm /tmp/demo.txt",
		CreatedAt:   model.NowString(),
	})

	st.ResetConversation(sessionID)

	session := st.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session to exist after reset")
	}
	if session.ThreadID != "" {
		t.Fatalf("expected thread to be cleared, got %q", session.ThreadID)
	}
	if len(session.Cards) != 0 {
		t.Fatalf("expected cards to be cleared, got %d", len(session.Cards))
	}
	if len(session.Approvals) != 0 {
		t.Fatalf("expected approvals to be cleared, got %d", len(session.Approvals))
	}
	if len(session.ApprovalGrants) != 0 {
		t.Fatalf("expected approval grants to be cleared, got %d", len(session.ApprovalGrants))
	}
	if got := st.SessionIDByThread("thread-live"); got != "" {
		t.Fatalf("expected thread mapping to be removed, got %q", got)
	}
}
