package sandbox

import (
	"context"
	"runtime"
	"testing"
)

func TestNewSandbox(t *testing.T) {
	sb := NewSandbox()
	if sb == nil {
		t.Fatal("NewSandbox returned nil")
	}

	switch runtime.GOOS {
	case "linux":
		if sb.Platform() != "landlock" {
			t.Errorf("expected landlock on linux, got %s", sb.Platform())
		}
	case "darwin":
		if sb.Platform() != "seatbelt" {
			t.Errorf("expected seatbelt on darwin, got %s", sb.Platform())
		}
	default:
		if sb.Platform() != "noop" {
			t.Errorf("expected noop on %s, got %s", runtime.GOOS, sb.Platform())
		}
	}
}

func TestSandboxPolicy_Modes(t *testing.T) {
	tests := []struct {
		name string
		mode SandboxMode
	}{
		{"read_only", ModeReadOnly},
		{"write_local", ModeWriteLocal},
		{"full_access", ModeFullAccess},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := SandboxPolicy{Mode: tt.mode}
			sb := &NoopSandbox{}
			if err := sb.Apply(policy); err != nil {
				t.Errorf("NoopSandbox.Apply failed: %v", err)
			}
		})
	}
}

func TestLandlockSandbox_Apply(t *testing.T) {
	sb := &LandlockSandbox{}

	t.Run("full_access", func(t *testing.T) {
		err := sb.Apply(SandboxPolicy{Mode: ModeFullAccess})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("write_local", func(t *testing.T) {
		err := sb.Apply(SandboxPolicy{
			Mode:          ModeWriteLocal,
			WritableRoots: []string{"/tmp/project"},
			ReadableRoots: []string{"/usr/lib"},
		})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("read_only_with_writable_roots_error", func(t *testing.T) {
		err := sb.Apply(SandboxPolicy{
			Mode:          ModeReadOnly,
			WritableRoots: []string{"/tmp"},
			ReadableRoots: []string{"/usr"},
		})
		if err == nil {
			t.Error("expected error for read_only with writable roots")
		}
	})
}

func TestLandlockSandbox_GenerateRuleset(t *testing.T) {
	sb := &LandlockSandbox{}

	t.Run("read_only", func(t *testing.T) {
		rules := sb.GenerateRuleset(SandboxPolicy{
			Mode:          ModeReadOnly,
			ReadableRoots: []string{"/usr", "/opt"},
		})
		if len(rules) != 2 {
			t.Errorf("expected 2 rules, got %d", len(rules))
		}
		for _, r := range rules {
			if r.Perms&PermWrite != 0 {
				t.Error("read_only mode should not have write permissions")
			}
		}
	})

	t.Run("write_local", func(t *testing.T) {
		rules := sb.GenerateRuleset(SandboxPolicy{
			Mode:          ModeWriteLocal,
			ReadableRoots: []string{"/usr"},
			WritableRoots: []string{"/tmp/project"},
			NetworkAllowed: []string{"api.example.com:443"},
		})
		// 1 readable + 1 writable + 1 network = 3
		if len(rules) != 3 {
			t.Errorf("expected 3 rules, got %d", len(rules))
		}
	})
}

func TestSeatbeltSandbox_Apply(t *testing.T) {
	sb := &SeatbeltSandbox{}

	t.Run("full_access", func(t *testing.T) {
		err := sb.Apply(SandboxPolicy{Mode: ModeFullAccess})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("write_local", func(t *testing.T) {
		err := sb.Apply(SandboxPolicy{
			Mode:          ModeWriteLocal,
			WritableRoots: []string{"/tmp/project"},
			ReadableRoots: []string{"/usr/lib"},
		})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if sb.Profile() == "" {
			t.Error("expected non-empty profile")
		}
	})
}

func TestSeatbeltSandbox_GenerateProfile(t *testing.T) {
	sb := &SeatbeltSandbox{}

	t.Run("read_only", func(t *testing.T) {
		profile := sb.GenerateProfile(SandboxPolicy{
			Mode:          ModeReadOnly,
			ReadableRoots: []string{"/usr/lib"},
		})
		if profile == "" {
			t.Error("expected non-empty profile")
		}
		if !contains(profile, "(deny default)") {
			t.Error("read_only profile should deny by default")
		}
		if !contains(profile, "(deny file-write*)") {
			t.Error("read_only profile should deny writes")
		}
		if !contains(profile, `(allow file-read* (subpath "/usr/lib"))`) {
			t.Error("read_only profile should allow reads to specified roots")
		}
	})

	t.Run("write_local", func(t *testing.T) {
		profile := sb.GenerateProfile(SandboxPolicy{
			Mode:          ModeWriteLocal,
			WritableRoots: []string{"/tmp/project"},
			ReadableRoots: []string{"/usr/lib"},
		})
		if !contains(profile, `(allow file-write* (subpath "/tmp/project"))`) {
			t.Error("write_local profile should allow writes to writable roots")
		}
	})

	t.Run("full_access", func(t *testing.T) {
		profile := sb.GenerateProfile(SandboxPolicy{Mode: ModeFullAccess})
		if !contains(profile, "(allow default)") {
			t.Error("full_access profile should allow all")
		}
	})

	t.Run("network_denied", func(t *testing.T) {
		profile := sb.GenerateProfile(SandboxPolicy{
			Mode:           ModeWriteLocal,
			WritableRoots:  []string{"/tmp"},
			NetworkDenied:  []string{"evil.com"},
		})
		if !contains(profile, "(deny network*)") {
			t.Error("profile with denied hosts should deny network")
		}
	})
}

func TestSeatbeltSandbox_ExecArgs(t *testing.T) {
	sb := &SeatbeltSandbox{}

	t.Run("no_profile", func(t *testing.T) {
		args := sb.SeatbeltExecArgs("ls -la")
		if args[0] != "sh" {
			t.Errorf("expected sh, got %s", args[0])
		}
	})

	t.Run("with_profile", func(t *testing.T) {
		sb.Apply(SandboxPolicy{
			Mode:          ModeWriteLocal,
			WritableRoots: []string{"/tmp"},
			ReadableRoots: []string{"/usr"},
		})
		args := sb.SeatbeltExecArgs("ls -la")
		if args[0] != "sandbox-exec" {
			t.Errorf("expected sandbox-exec, got %s", args[0])
		}
	})
}

func TestEscalationManager_RequestEscalation(t *testing.T) {
	t.Run("approved", func(t *testing.T) {
		em := NewEscalationManager()
		em.ApprovalFunc = func(_ context.Context, _ EscalationRequest) (bool, error) {
			return true, nil
		}

		approved, err := em.RequestEscalation(context.Background(), "sess-1", EscalationRequest{
			Operation: "write",
			Paths:     []string{"/etc/config"},
			Reason:    "need to update config",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !approved {
			t.Error("expected approval")
		}

		events := em.Events()
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}
		if !events[0].Approved {
			t.Error("event should be marked approved")
		}
	})

	t.Run("denied", func(t *testing.T) {
		em := NewEscalationManager()
		em.ApprovalFunc = func(_ context.Context, _ EscalationRequest) (bool, error) {
			return false, nil
		}

		approved, err := em.RequestEscalation(context.Background(), "sess-2", EscalationRequest{
			Operation: "network",
			Hosts:     []string{"evil.com"},
			Reason:    "need to connect",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if approved {
			t.Error("expected denial")
		}
	})

	t.Run("no_approval_func", func(t *testing.T) {
		em := NewEscalationManager()
		approved, err := em.RequestEscalation(context.Background(), "sess-3", EscalationRequest{
			Operation: "write",
			Paths:     []string{"/tmp"},
			Reason:    "test",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if approved {
			t.Error("expected denial when no approval func")
		}
	})

	t.Run("empty_operation_error", func(t *testing.T) {
		em := NewEscalationManager()
		_, err := em.RequestEscalation(context.Background(), "sess-4", EscalationRequest{})
		if err == nil {
			t.Error("expected error for empty operation")
		}
	})

	t.Run("audit_logging", func(t *testing.T) {
		em := NewEscalationManager()
		em.ApprovalFunc = func(_ context.Context, _ EscalationRequest) (bool, error) {
			return true, nil
		}

		em.RequestEscalation(context.Background(), "s1", EscalationRequest{Operation: "op1", Reason: "r1"})
		em.RequestEscalation(context.Background(), "s2", EscalationRequest{Operation: "op2", Reason: "r2"})

		events := em.Events()
		if len(events) != 2 {
			t.Fatalf("expected 2 events, got %d", len(events))
		}
		if events[0].SessionID != "s1" || events[1].SessionID != "s2" {
			t.Error("events should preserve session IDs")
		}

		em.ClearEvents()
		if len(em.Events()) != 0 {
			t.Error("expected 0 events after clear")
		}
	})
}

func TestNetworkApprovalManager_EvaluateNetworkAccess(t *testing.T) {
	t.Run("explicitly_allowed", func(t *testing.T) {
		m := NewNetworkApprovalManager(SandboxPolicy{
			NetworkAllowed: []string{"api.example.com"},
		})
		err := m.EvaluateNetworkAccess(context.Background(), NetworkApprovalRequest{
			Host: "api.example.com", Port: 443, Protocol: "https",
		})
		if err != nil {
			t.Errorf("expected allowed, got: %v", err)
		}
	})

	t.Run("explicitly_denied", func(t *testing.T) {
		m := NewNetworkApprovalManager(SandboxPolicy{
			NetworkDenied: []string{"evil.com"},
		})
		err := m.EvaluateNetworkAccess(context.Background(), NetworkApprovalRequest{
			Host: "evil.com", Port: 80, Protocol: "tcp",
		})
		if err == nil {
			t.Error("expected denial for explicitly denied host")
		}
	})

	t.Run("no_lists_allows_all", func(t *testing.T) {
		m := NewNetworkApprovalManager(SandboxPolicy{})
		err := m.EvaluateNetworkAccess(context.Background(), NetworkApprovalRequest{
			Host: "anything.com", Port: 443, Protocol: "https",
		})
		if err == nil {
			// No lists means allow by default
		}
	})

	t.Run("approval_with_caching", func(t *testing.T) {
		callCount := 0
		m := NewNetworkApprovalManager(SandboxPolicy{
			NetworkAllowed: []string{"known.com"},
		})
		m.ApprovalFunc = func(_ context.Context, _ NetworkApprovalRequest) (bool, error) {
			callCount++
			return true, nil
		}

		req := NetworkApprovalRequest{Host: "unknown.com", Port: 443, Protocol: "https"}

		// First call should invoke approval
		err := m.EvaluateNetworkAccess(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 1 {
			t.Errorf("expected 1 approval call, got %d", callCount)
		}

		// Second call should use cache
		err = m.EvaluateNetworkAccess(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error on cached call: %v", err)
		}
		if callCount != 1 {
			t.Errorf("expected still 1 approval call (cached), got %d", callCount)
		}

		if m.CacheSize() != 1 {
			t.Errorf("expected cache size 1, got %d", m.CacheSize())
		}
	})

	t.Run("wildcard_matching", func(t *testing.T) {
		m := NewNetworkApprovalManager(SandboxPolicy{
			NetworkAllowed: []string{"*.example.com"},
		})
		err := m.EvaluateNetworkAccess(context.Background(), NetworkApprovalRequest{
			Host: "api.example.com", Port: 443, Protocol: "https",
		})
		if err != nil {
			t.Errorf("expected wildcard match to allow, got: %v", err)
		}
	})

	t.Run("empty_host_error", func(t *testing.T) {
		m := NewNetworkApprovalManager(SandboxPolicy{})
		err := m.EvaluateNetworkAccess(context.Background(), NetworkApprovalRequest{})
		if err == nil {
			t.Error("expected error for empty host")
		}
	})

	t.Run("clear_cache", func(t *testing.T) {
		m := NewNetworkApprovalManager(SandboxPolicy{
			NetworkAllowed: []string{"known.com"},
		})
		m.ApprovalFunc = func(_ context.Context, _ NetworkApprovalRequest) (bool, error) {
			return true, nil
		}
		m.EvaluateNetworkAccess(context.Background(), NetworkApprovalRequest{
			Host: "other.com", Port: 80, Protocol: "tcp",
		})
		if m.CacheSize() != 1 {
			t.Fatal("expected cache size 1")
		}
		m.ClearCache()
		if m.CacheSize() != 0 {
			t.Error("expected cache size 0 after clear")
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
