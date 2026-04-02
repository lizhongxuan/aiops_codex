package server

import (
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func TestSwitchSelectedHostClearsApprovalGrants(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-host-switch-grant"
	app.store.EnsureSession(sessionID)
	app.store.UpsertHost(model.Host{
		ID:         "linux-01",
		Name:       "linux-01",
		Kind:       "agent",
		Status:     "online",
		Executable: true,
	})

	grant := model.ApprovalGrant{
		ID:          "grant-1",
		HostID:      model.ServerLocalHostID,
		Type:        "command",
		Fingerprint: approvalFingerprintForCommand(model.ServerLocalHostID, "rm /tmp/demo.txt", "/tmp"),
		Command:     "rm /tmp/demo.txt",
		Cwd:         "/tmp",
		CreatedAt:   model.NowString(),
	}
	app.store.AddApprovalGrant(sessionID, grant)

	if _, ok := app.store.ApprovalGrant(sessionID, grant.Fingerprint); !ok {
		t.Fatalf("expected approval grant to exist before host switch")
	}

	if _, switched, err := app.switchSelectedHost(sessionID, "linux-01", false); err != nil {
		t.Fatalf("switch selected host: %v", err)
	} else if !switched {
		t.Fatalf("expected host switch to report a change")
	}

	if _, ok := app.store.ApprovalGrant(sessionID, grant.Fingerprint); ok {
		t.Fatalf("expected approval grant to be cleared after host switch")
	}
}
