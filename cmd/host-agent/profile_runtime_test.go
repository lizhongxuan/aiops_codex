package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func TestHostAgentProfileStoreApplyUpdateAndSwitch(t *testing.T) {
	store := newTestHostAgentProfileStore(t)

	initialProfile, initialRevision, _ := store.snapshot()
	if initialProfile.ID != string(model.AgentProfileTypeHostAgentDefault) {
		t.Fatalf("expected host-agent-default profile, got %#v", initialProfile)
	}

	updated := initialProfile
	updated.CommandPermissions.AllowSudo = boolPtr(true)
	updated.CommandPermissions.DefaultTimeoutSeconds = 33

	ack, err := store.applyUpdate(profileUpdateEnvelope(updated, "rev-1"))
	if err != nil {
		t.Fatalf("apply update: %v", err)
	}
	if ack.Status != "applied" {
		t.Fatalf("expected applied status, got %#v", ack)
	}
	if ack.Revision != "rev-1" {
		t.Fatalf("expected revision rev-1, got %#v", ack)
	}

	currentProfile, currentRevision, unsupported := store.snapshot()
	if currentRevision != "rev-1" {
		t.Fatalf("expected revision rev-1, got %q", currentRevision)
	}
	if currentProfile.CommandPermissions.AllowSudo == nil || !*currentProfile.CommandPermissions.AllowSudo {
		t.Fatalf("expected updated allowSudo=true, got %#v", currentProfile.CommandPermissions.AllowSudo)
	}
	if currentProfile.CommandPermissions.DefaultTimeoutSeconds != 33 {
		t.Fatalf("expected updated timeout 33, got %d", currentProfile.CommandPermissions.DefaultTimeoutSeconds)
	}
	if len(unsupported) == 0 {
		t.Fatalf("expected unsupported capability list to remain populated")
	}
	if !strings.Contains(ack.Summary, "gates=") {
		t.Fatalf("expected runtime gates to be reflected in summary, got %#v", ack)
	}
	if !containsString(ack.EnabledSkills, "host-diagnostics") {
		t.Fatalf("expected enabled skills to be returned in ack, got %#v", ack)
	}
	if !containsString(ack.EnabledMCPs, "host-files") || !containsString(ack.EnabledMCPs, "host-logs") {
		t.Fatalf("expected enabled MCPs to be returned in ack, got %#v", ack)
	}

	ack, err = store.applyUpdate(profileUpdateEnvelope(updated, "rev-1"))
	if err != nil {
		t.Fatalf("apply duplicate update: %v", err)
	}
	if ack.Status != "unchanged" {
		t.Fatalf("expected unchanged status for duplicate revision, got %#v", ack)
	}
	if storeRevision(t, store) != "rev-1" {
		t.Fatalf("expected revision to remain rev-1 after duplicate update")
	}
	if initialRevision == "" {
		t.Fatalf("expected initial revision to be populated")
	}
}

func TestHostAgentProfileStoreRejectsDisabledCapabilities(t *testing.T) {
	store := newTestHostAgentProfileStore(t)
	profile, _, _ := store.snapshot()
	profile.CapabilityPermissions.CommandExecution = model.AgentCapabilityDisabled
	profile.CapabilityPermissions.FileRead = model.AgentCapabilityDisabled
	profile.CapabilityPermissions.FileChange = model.AgentCapabilityDisabled
	profile.CapabilityPermissions.Terminal = model.AgentCapabilityDisabled
	if _, err := store.applyUpdate(profileUpdateEnvelope(profile, "cap-disabled")); err != nil {
		t.Fatalf("apply update: %v", err)
	}

	t.Run("exec", func(t *testing.T) {
		if err := store.ensureCapabilityAllowed("commandExecution"); err == nil || !containsAny(strings.ToLower(err.Error()), "commandexecution", "command execution") {
			t.Fatalf("expected commandExecution gate error, got %v", err)
		}
	})

	t.Run("file read", func(t *testing.T) {
		if err := store.ensureCapabilityAllowed("fileRead"); err == nil || !containsAny(strings.ToLower(err.Error()), "fileread", "file read") {
			t.Fatalf("expected fileRead gate error, got %v", err)
		}
	})

	t.Run("file change", func(t *testing.T) {
		if err := store.ensureCapabilityAllowed("fileChange"); err == nil || !containsAny(strings.ToLower(err.Error()), "filechange", "file change") {
			t.Fatalf("expected fileChange gate error, got %v", err)
		}
	})

	t.Run("terminal", func(t *testing.T) {
		if err := store.ensureCapabilityAllowed("terminal"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "terminal") {
			t.Fatalf("expected terminal gate error, got %v", err)
		}
	})
}

func TestHostAgentProfileStoreRejectsSudoAndShellWrapper(t *testing.T) {
	store := newTestHostAgentProfileStore(t)
	profile, _, _ := store.snapshot()
	profile.Skills = []model.AgentSkill{
		{ID: "host-diagnostics", Name: "Host Diagnostics", Enabled: true, ActivationMode: model.AgentSkillActivationDefault},
		{ID: "host-change-review", Name: "Host Change Review", Enabled: true, ActivationMode: model.AgentSkillActivationDefault},
	}
	profile.CommandPermissions.AllowSudo = boolPtr(false)
	profile.CommandPermissions.AllowShellWrapper = boolPtr(false)
	if _, err := store.applyUpdate(profileUpdateEnvelope(profile, "shell-sudo")); err != nil {
		t.Fatalf("apply update: %v", err)
	}

	t.Run("sudo", func(t *testing.T) {
		_, err := store.commandPolicy("sudo systemctl restart nginx")
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "sudo") {
			t.Fatalf("expected sudo gate error, got %v", err)
		}
	})

	t.Run("shell wrapper", func(t *testing.T) {
		_, err := store.commandPolicy("/bin/bash -lc 'echo hello'")
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "shell wrapper") {
			t.Fatalf("expected shell wrapper gate error, got %v", err)
		}
	})
}

func TestHostAgentProfileStoreBlocksReadonlyOnlyMutations(t *testing.T) {
	store := newTestHostAgentProfileStore(t)
	profile, _, _ := store.snapshot()
	profile.Skills = []model.AgentSkill{
		{ID: "host-diagnostics", Name: "Host Diagnostics", Enabled: true, ActivationMode: model.AgentSkillActivationDefault},
		{ID: "host-change-review", Name: "Host Change Review", Enabled: true, ActivationMode: model.AgentSkillActivationDefault},
	}
	profile.CommandPermissions.CategoryPolicies["service_mutation"] = model.AgentPermissionModeReadonlyOnly
	if _, err := store.applyUpdate(profileUpdateEnvelope(profile, "readonly-only")); err != nil {
		t.Fatalf("apply update: %v", err)
	}

	_, err := store.commandPolicy("systemctl restart nginx")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "readonly_only") {
		t.Fatalf("expected readonly_only gate error, got %v", err)
	}
}

func TestHostAgentProfileStoreHostFilesMCPControlsFileAccessAndWritePermission(t *testing.T) {
	store := newTestHostAgentProfileStore(t)
	profile, _, _ := store.snapshot()
	profile.Skills = []model.AgentSkill{
		{ID: "host-diagnostics", Name: "Host Diagnostics", Enabled: true, ActivationMode: model.AgentSkillActivationDefault},
		{ID: "host-change-review", Name: "Host Change Review", Enabled: true, ActivationMode: model.AgentSkillActivationDefault},
	}
	profile.MCPs = []model.AgentMCP{
		{ID: "host-files", Name: "Host Files MCP", Enabled: false, Permission: model.AgentMCPPermissionReadonly},
		{ID: "host-logs", Name: "Host Logs MCP", Enabled: true, Permission: model.AgentMCPPermissionReadonly},
	}
	if _, err := store.applyUpdate(profileUpdateEnvelope(profile, "host-files-off")); err != nil {
		t.Fatalf("apply update: %v", err)
	}

	for _, capability := range []string{"fileRead", "fileSearch"} {
		if err := store.ensureCapabilityAllowed(capability); err == nil || !strings.Contains(strings.ToLower(err.Error()), "host-files") {
			t.Fatalf("expected host-files gate error for %s, got %v", capability, err)
		}
	}

	profile.MCPs[0].Enabled = true
	profile.MCPs[0].Permission = model.AgentMCPPermissionReadonly
	if _, err := store.applyUpdate(profileUpdateEnvelope(profile, "host-files-readonly")); err != nil {
		t.Fatalf("apply update: %v", err)
	}
	if err := store.ensureCapabilityAllowed("fileRead"); err != nil {
		t.Fatalf("expected fileRead to be allowed with host-files enabled, got %v", err)
	}
	if err := store.ensureCapabilityAllowed("fileSearch"); err != nil {
		t.Fatalf("expected fileSearch to be allowed with host-files enabled, got %v", err)
	}
	if err := store.ensureCapabilityAllowed("fileChange"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "readwrite") {
		t.Fatalf("expected host-files readwrite gate error for fileChange, got %v", err)
	}

	profile.MCPs[0].Permission = model.AgentMCPPermissionReadwrite
	if _, err := store.applyUpdate(profileUpdateEnvelope(profile, "host-files-readwrite")); err != nil {
		t.Fatalf("apply update: %v", err)
	}
	if err := store.ensureCapabilityAllowed("fileChange"); err != nil {
		t.Fatalf("expected fileChange to be allowed with host-files readwrite, got %v", err)
	}
}

func TestHostAgentProfileStoreHostLogsAndDiagnosticsSkillsGateCommands(t *testing.T) {
	store := newTestHostAgentProfileStore(t)
	profile, _, _ := store.snapshot()
	profile.Skills = []model.AgentSkill{
		{ID: "host-diagnostics", Name: "Host Diagnostics", Enabled: false, ActivationMode: model.AgentSkillActivationDisabled},
		{ID: "host-change-review", Name: "Host Change Review", Enabled: true, ActivationMode: model.AgentSkillActivationDefault},
	}
	profile.MCPs = []model.AgentMCP{
		{ID: "host-files", Name: "Host Files MCP", Enabled: true, Permission: model.AgentMCPPermissionReadonly},
		{ID: "host-logs", Name: "Host Logs MCP", Enabled: false, Permission: model.AgentMCPPermissionReadonly},
	}
	if _, err := store.applyUpdate(profileUpdateEnvelope(profile, "host-logs-off")); err != nil {
		t.Fatalf("apply update: %v", err)
	}

	if _, err := store.commandPolicy("journalctl -n 20 --no-pager"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "host-logs") {
		t.Fatalf("expected host-logs gate error, got %v", err)
	}
	if _, err := store.commandPolicy("uptime"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "host-diagnostics") {
		t.Fatalf("expected host-diagnostics gate error for readonly diagnostics, got %v", err)
	}
	if err := store.ensureCapabilityAllowed("terminal"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "host-diagnostics") {
		t.Fatalf("expected host-diagnostics gate error for terminal, got %v", err)
	}

	profile.Skills[0].Enabled = true
	profile.Skills[0].ActivationMode = model.AgentSkillActivationDefault
	profile.MCPs[1].Enabled = true
	if _, err := store.applyUpdate(profileUpdateEnvelope(profile, "host-logs-on")); err != nil {
		t.Fatalf("apply update: %v", err)
	}
	if _, err := store.commandPolicy("journalctl -n 20 --no-pager"); err != nil {
		t.Fatalf("expected journalctl to be allowed with host-logs enabled, got %v", err)
	}
	if err := store.ensureCapabilityAllowed("terminal"); err != nil {
		t.Fatalf("expected terminal to be allowed with host-diagnostics enabled, got %v", err)
	}
}

func TestHostAgentProfileStoreRejectsWritesOutsideWritableRoots(t *testing.T) {
	store := newTestHostAgentProfileStore(t)
	profile, _, _ := store.snapshot()
	profile.CommandPermissions.AllowedWritableRoots = []string{filepath.Join("/tmp", "agent-profile-allowed")}
	if _, err := store.applyUpdate(profileUpdateEnvelope(profile, "roots")); err != nil {
		t.Fatalf("apply update: %v", err)
	}

	if err := store.ensureWritableRoots([]string{"/etc/hosts"}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "writable roots") {
		t.Fatalf("expected writable roots gate error, got %v", err)
	}
}

func TestHostAgentProfileStoreClampsTimeoutToProfileUpperBound(t *testing.T) {
	store := newTestHostAgentProfileStore(t)
	profile, _, _ := store.snapshot()
	profile.CommandPermissions.DefaultTimeoutSeconds = 45
	if _, err := store.applyUpdate(profileUpdateEnvelope(profile, "timeout")); err != nil {
		t.Fatalf("apply update: %v", err)
	}

	if got := store.effectiveCommandTimeoutSeconds(300, true); got != 45 {
		t.Fatalf("expected timeout to clamp to 45, got %d", got)
	}
}

func newTestHostAgentProfileStore(t *testing.T) *hostAgentProfileStore {
	t.Helper()
	t.Setenv("AIOPS_AGENT_PROFILE_JSON", "")
	t.Setenv("AIOPS_AGENT_PROFILE_PATH", filepath.Join(t.TempDir(), "host-agent-profile.json"))
	rt, err := newHostAgentRuntime()
	if err != nil {
		t.Fatalf("new host agent runtime: %v", err)
	}
	if rt == nil || rt.profile == nil {
		t.Fatalf("expected profile runtime to be initialized")
	}
	return rt.profile
}

func storeRevision(t *testing.T, store *hostAgentProfileStore) string {
	t.Helper()
	_, revision, _ := store.snapshot()
	return revision
}

func boolPtr(v bool) *bool {
	return &v
}

func containsAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}
