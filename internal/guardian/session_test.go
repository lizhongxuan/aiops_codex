package guardian

import (
	"testing"
)

func TestReviewSessionManager_CreateSession(t *testing.T) {
	mgr := NewReviewSessionManager()

	config := SessionConfig{
		NetworkProxy:    "http://proxy:8080",
		AllowedHosts:    []string{"api.example.com", "cdn.example.com"},
		ParentSessionID: "sess-123",
	}

	session := mgr.CreateSession(config)

	if session == nil {
		t.Fatal("CreateSession returned nil")
	}
	if session.ID == "" {
		t.Error("session ID should not be empty")
	}
	if session.Config.NetworkProxy != "http://proxy:8080" {
		t.Errorf("NetworkProxy = %q, want %q", session.Config.NetworkProxy, "http://proxy:8080")
	}
	if len(session.Config.AllowedHosts) != 2 {
		t.Fatalf("AllowedHosts length = %d, want 2", len(session.Config.AllowedHosts))
	}
	if session.Config.AllowedHosts[0] != "api.example.com" {
		t.Errorf("AllowedHosts[0] = %q, want %q", session.Config.AllowedHosts[0], "api.example.com")
	}
}

func TestReviewSessionManager_InheritsConfig(t *testing.T) {
	mgr := NewReviewSessionManager()

	config := SessionConfig{
		NetworkProxy:    "http://proxy:3128",
		AllowedHosts:    []string{"host1.com"},
		ParentSessionID: "parent-1",
	}

	session := mgr.CreateSession(config)

	// Modify original slice to verify clone was made.
	config.AllowedHosts[0] = "modified.com"

	if session.Config.AllowedHosts[0] == "modified.com" {
		t.Error("session should have a cloned AllowedHosts, not a shared reference")
	}
}

func TestReviewSessionManager_CleanupSession(t *testing.T) {
	mgr := NewReviewSessionManager()

	session := mgr.CreateSession(SessionConfig{ParentSessionID: "p1"})
	id := session.ID

	if mgr.ActiveCount() != 1 {
		t.Fatalf("ActiveCount = %d, want 1", mgr.ActiveCount())
	}

	mgr.CleanupSession(id)

	if mgr.ActiveCount() != 0 {
		t.Errorf("ActiveCount after cleanup = %d, want 0", mgr.ActiveCount())
	}
	if mgr.GetSession(id) != nil {
		t.Error("GetSession should return nil after cleanup")
	}
}

func TestReviewSessionManager_GetSession(t *testing.T) {
	mgr := NewReviewSessionManager()

	session := mgr.CreateSession(SessionConfig{ParentSessionID: "p2"})

	got := mgr.GetSession(session.ID)
	if got == nil {
		t.Fatal("GetSession returned nil for existing session")
	}
	if got.ID != session.ID {
		t.Errorf("GetSession ID = %q, want %q", got.ID, session.ID)
	}

	// Non-existent session.
	if mgr.GetSession("nonexistent") != nil {
		t.Error("GetSession should return nil for non-existent ID")
	}
}

func TestReviewSessionManager_MultipleSessions(t *testing.T) {
	mgr := NewReviewSessionManager()

	s1 := mgr.CreateSession(SessionConfig{ParentSessionID: "p1"})
	s2 := mgr.CreateSession(SessionConfig{ParentSessionID: "p1"})

	if s1.ID == s2.ID {
		t.Error("sessions should have unique IDs")
	}
	if mgr.ActiveCount() != 2 {
		t.Errorf("ActiveCount = %d, want 2", mgr.ActiveCount())
	}
}

func TestGenerateSessionID(t *testing.T) {
	id := generateSessionID(1, "parent-abc")
	if id != "parent-abc-guardian-1" {
		t.Errorf("generateSessionID = %q, want %q", id, "parent-abc-guardian-1")
	}

	id = generateSessionID(5, "")
	if id != "orphan-guardian-5" {
		t.Errorf("generateSessionID with empty parent = %q, want %q", id, "orphan-guardian-5")
	}
}
