package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func TestMainAgentThreadStartCarriesProfileGeneratedAppConfigOverrides(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "agent-profile-test",
		SessionSecret:     "agent-profile-secret",
		SessionCookieTTL:  time.Hour,
		DefaultWorkspace:  "/workspace",
	})
	sessionID := "sess-runtime-exposure-thread"
	app.store.EnsureSession(sessionID)
	app.store.SetSelectedHost(sessionID, "server-local")

	profile := app.mainAgentProfile()
	profile.Skills = []model.AgentSkill{
		{ID: "ops-triage", Name: "Ops Triage", Enabled: true, ActivationMode: model.AgentSkillActivationDefault},
		{ID: "safe-change-review", Name: "Safe Change Review", Enabled: false, ActivationMode: model.AgentSkillActivationDisabled},
	}
	profile.MCPs = []model.AgentMCP{
		{ID: "filesystem", Name: "Filesystem MCP", Enabled: false, Permission: model.AgentMCPPermissionReadonly},
		{ID: "docs", Name: "Docs MCP", Enabled: true, Permission: model.AgentMCPPermissionReadonly, RequiresExplicitUserApproval: true},
		{ID: "metrics", Name: "Metrics MCP", Enabled: true, Permission: model.AgentMCPPermissionReadwrite, RequiresExplicitUserApproval: true},
	}
	app.store.UpsertAgentProfile(profile)
	app.skillDiscoveryFunc = func(context.Context, string) ([]installedSkillMetadata, error) {
		return []installedSkillMetadata{
			{Name: "Ops Triage", Path: "/tmp/ops-triage/SKILL.md", Enabled: true},
			{Name: "Safe Change Review", Path: "/tmp/safe-change-review/SKILL.md", Enabled: true},
			{Name: "Unmanaged Skill", Path: "/tmp/unmanaged-skill/SKILL.md", Enabled: true},
		}, nil
	}

	spec := app.buildSingleHostReActThreadStartSpec(context.Background(), sessionID)
	configValue := spec.Config
	if len(configValue) == 0 {
		t.Fatalf("expected thread config overrides, got %#v", spec)
	}
	if got := boolValueFromAny(t, configValue["apps._default.enabled"], "config.apps._default.enabled"); got {
		t.Fatalf("expected default app config to stay disabled, got %#v", configValue["apps._default.enabled"])
	}
	if _, exists := configValue["apps.filesystem.enabled"]; exists {
		t.Fatalf("expected disabled filesystem MCP to stay out of thread config, got %#v", configValue["apps.filesystem.enabled"])
	}
	if got := boolValueFromAny(t, configValue["apps.docs.enabled"], "config.apps.docs.enabled"); !got {
		t.Fatalf("expected docs MCP/app override to stay enabled, got %#v", configValue["apps.docs.enabled"])
	}
	if got := stringValueFromAny(configValue["apps.docs.default_tools_approval_mode"]); got != "prompt" {
		t.Fatalf("expected docs MCP/app approval mode to be prompt, got %#v", configValue["apps.docs.default_tools_approval_mode"])
	}
	if got := boolValueFromAny(t, configValue["apps.metrics.enabled"], "config.apps.metrics.enabled"); !got {
		t.Fatalf("expected metrics MCP/app override to stay enabled, got %#v", configValue["apps.metrics.enabled"])
	}
	if got := boolValueFromAny(t, configValue["apps.metrics.destructive_enabled"], "config.apps.metrics.destructive_enabled"); !got {
		t.Fatalf("expected writable metrics MCP to be destructive-enabled, got %#v", configValue["apps.metrics.destructive_enabled"])
	}
	skillConfig := mustSliceOfMap(t, configValue["skills.config"], "config.skills.config")
	skillEnabledByPath := make(map[string]bool, len(skillConfig))
	for _, entry := range skillConfig {
		skillEnabledByPath[stringValueFromAny(entry["path"])] = boolValueFromAny(t, entry["enabled"], "config.skills.config.enabled")
	}
	if !skillEnabledByPath["/tmp/ops-triage/SKILL.md"] {
		t.Fatalf("expected ops-triage skill to stay enabled, got %#v", skillConfig)
	}
	if skillEnabledByPath["/tmp/safe-change-review/SKILL.md"] {
		t.Fatalf("expected disabled safe-change-review skill to stay disabled, got %#v", skillConfig)
	}
	if skillEnabledByPath["/tmp/unmanaged-skill/SKILL.md"] {
		t.Fatalf("expected unmanaged skill to stay disabled, got %#v", skillConfig)
	}
}

func TestMainAgentTurnStartIncludesDefaultAndExplicitSkills(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "agent-profile-test",
		SessionSecret:     "agent-profile-secret",
		SessionCookieTTL:  time.Hour,
		DefaultWorkspace:  "/workspace",
	})
	sessionID := "sess-runtime-exposure-turn"
	threadID := "thread-runtime-exposure-turn"
	app.store.EnsureSession(sessionID)
	app.store.SetSelectedHost(sessionID, "server-local")
	app.store.SetThread(sessionID, threadID)

	profile := app.mainAgentProfile()
	profile.Skills = []model.AgentSkill{
		{ID: "ops-triage", Name: "Ops Triage", Description: "Default skill", Enabled: true, ActivationMode: model.AgentSkillActivationDefault},
		{ID: "safe-change-review", Name: "Safe Change Review", Description: "Explicit skill", Enabled: true, ActivationMode: model.AgentSkillActivationExplicit},
		{ID: "host-change-review", Name: "Host Change Review", Description: "Disabled skill", Enabled: false, ActivationMode: model.AgentSkillActivationDisabled},
	}
	app.store.UpsertAgentProfile(profile)
	app.skillDiscoveryFunc = func(context.Context, string) ([]installedSkillMetadata, error) {
		return []installedSkillMetadata{
			{Name: "Ops Triage", Path: "/tmp/ops-triage/SKILL.md", Enabled: true},
			{Name: "Safe Change Review", Path: "/tmp/safe-change-review/SKILL.md", Enabled: true},
			{Name: "Host Change Review", Path: "/tmp/host-change-review/SKILL.md", Enabled: true},
		}, nil
	}

	spec := app.buildSingleHostReActTurnStartSpec(context.Background(), sessionID, chatRequest{
		Message: "Use Safe Change Review before changing nginx.",
		HostID:  "server-local",
	})
	inputs := mustSliceOfMap(t, spec.Input, "input")
	var skillNames []string
	for _, item := range inputs {
		if strings.TrimSpace(stringValueFromAny(item["type"])) != "skill" {
			continue
		}
		skillNames = append(skillNames, stringValueFromAny(item["name"]))
	}
	if len(skillNames) < 2 {
		t.Fatalf("expected default and explicit skills to be injected, got %#v", inputs)
	}
	if !containsString(skillNames, "Ops Triage") {
		t.Fatalf("expected default-enabled skill to be present, got %#v", skillNames)
	}
	if !containsString(skillNames, "Safe Change Review") {
		t.Fatalf("expected explicit-only skill to be present after explicit mention, got %#v", skillNames)
	}
	if containsString(skillNames, "Host Change Review") {
		t.Fatalf("expected disabled skill to stay out of turn input, got %#v", skillNames)
	}
}

func TestMainAgentProfileExposureHashChangeRefreshesThreadBeforeNextTurn(t *testing.T) {
	app := New(config.Config{
		SessionCookieName: "agent-profile-test",
		SessionSecret:     "agent-profile-secret",
		SessionCookieTTL:  time.Hour,
		DefaultWorkspace:  "/workspace",
	})
	sessionID := "sess-runtime-exposure-refresh"
	app.store.EnsureSession(sessionID)
	app.store.SetSelectedHost(sessionID, "server-local")
	app.store.UpdateAuth(sessionID, func(auth *model.AuthState, _ *model.ExternalAuthTokens) {
		auth.Connected = true
		auth.Email = "operator@example.com"
		auth.Mode = "chatgpt"
	})
	app.store.UpsertHost(model.Host{ID: "server-local", Name: "server-local", Kind: "local", Status: "online", Executable: true})

	baseProfile := app.mainAgentProfile()
	baseProfile.Skills = []model.AgentSkill{
		{ID: "ops-triage", Name: "Ops Triage", Enabled: true, ActivationMode: model.AgentSkillActivationDefault},
	}
	baseProfile.MCPs = []model.AgentMCP{
		{ID: "filesystem", Name: "Filesystem MCP", Enabled: true, Permission: model.AgentMCPPermissionReadonly},
	}
	app.store.UpsertAgentProfile(baseProfile)
	app.skillDiscoveryFunc = func(context.Context, string) ([]installedSkillMetadata, error) {
		return []installedSkillMetadata{
			{Name: "Ops Triage", Path: "/tmp/ops-triage/SKILL.md", Enabled: true},
			{Name: "Incident Summary", Path: "/tmp/incident-summary/SKILL.md", Enabled: true},
		}, nil
	}

	var calls []string
	threadStartCount := 0
	runtimeStub := &runtimeStartStub{
		startThread: func(_ context.Context, _ string, _ threadStartSpec) (string, error) {
			calls = append(calls, "thread/start")
			threadStartCount++
			if threadStartCount > 1 {
				return "thread-runtime-exposure-refresh-updated", nil
			}
			return "thread-runtime-exposure-refresh-initial", nil
		},
		startTurn: func(_ context.Context, _ string, _ string, _ turnStartSpec) (string, error) {
			calls = append(calls, "turn/start")
			return "turn-runtime-exposure-refresh", nil
		},
	}
	runtimeStub.install(app)

	if _, err := app.ensureThread(context.Background(), sessionID); err != nil {
		t.Fatalf("seed ensureThread: %v", err)
	}
	initialCalls := filterNonSkillsListCalls(calls)
	if len(initialCalls) != 1 || initialCalls[0] != "thread/start" {
		t.Fatalf("expected initial thread/start only, got %#v", initialCalls)
	}

	calls = nil

	updatedProfile := baseProfile
	updatedProfile.Skills = []model.AgentSkill{
		{ID: "ops-triage", Name: "Ops Triage", Enabled: true, ActivationMode: model.AgentSkillActivationDefault},
		{ID: "incident-summary", Name: "Incident Summary", Enabled: true, ActivationMode: model.AgentSkillActivationDefault},
	}
	updatedProfile.MCPs = []model.AgentMCP{
		{ID: "filesystem", Name: "Filesystem MCP", Enabled: true, Permission: model.AgentMCPPermissionReadonly},
		{ID: "docs", Name: "Docs MCP", Enabled: true, Permission: model.AgentMCPPermissionReadonly, RequiresExplicitUserApproval: true},
	}
	app.store.UpsertAgentProfile(updatedProfile)

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if !app.shouldRefreshThreadForAgentRuntime(session, model.ServerLocalHostID) {
		t.Fatalf("expected updated agent profile to require thread refresh")
	}
	app.clearSessionThreadBinding(sessionID)
	session.ThreadID = ""

	threadID, err := app.ensureThread(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("refresh ensureThread: %v", err)
	}
	if err := app.requestTurnWithSpec(context.Background(), sessionID, threadID, app.buildSingleHostReActTurnStartSpec(context.Background(), sessionID, chatRequest{
		Message: "Follow up after the profile changed.",
		HostID:  model.ServerLocalHostID,
	})); err != nil {
		t.Fatalf("refresh requestTurnWithSpec: %v", err)
	}

	filteredCalls := filterNonSkillsListCalls(calls)
	if len(filteredCalls) < 2 {
		t.Fatalf("expected thread refresh before turn/start, got %#v", calls)
	}
	if filteredCalls[0] != "thread/start" || filteredCalls[1] != "turn/start" {
		t.Fatalf("expected refresh sequence thread/start then turn/start, got %#v", filteredCalls)
	}
	if threadID != "thread-runtime-exposure-refresh-updated" {
		t.Fatalf("expected refreshed thread id, got %q", threadID)
	}
	if session := app.store.Session(sessionID); session == nil || strings.TrimSpace(session.ThreadConfigHash) == "" {
		t.Fatalf("expected session thread config hash to remain bound, got %#v", session)
	}
}

func filterNonSkillsListCalls(calls []string) []string {
	out := make([]string, 0, len(calls))
	for _, method := range calls {
		if method == "skills/list" {
			continue
		}
		out = append(out, method)
	}
	return out
}

func mustSliceOfMap(t *testing.T, value any, label string) []map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %s: %v", label, err)
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", label, err)
	}
	return out
}

func stringValueFromAny(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	var out string
	if err := json.Unmarshal(raw, &out); err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func boolValueFromAny(t *testing.T, value any, label string) bool {
	t.Helper()
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "enabled":
			return true
		case "false", "0", "no", "disabled":
			return false
		}
	case float64:
		return v != 0
	}
	t.Fatalf("expected boolean-like value for %s, got %#v", label, value)
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
