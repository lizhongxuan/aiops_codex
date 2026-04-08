package store

import (
	"os"
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

func TestSessionMetaDefaultsAndPersistence(t *testing.T) {
	st := New()
	sessionID := "sess-meta"

	session := st.EnsureSession(sessionID)
	if session == nil {
		t.Fatalf("expected session to be created")
	}
	if got := session.Meta; got != model.DefaultSessionMeta() {
		t.Fatalf("expected default session meta, got %#v", got)
	}

	created := st.EnsureSessionWithMeta("sess-planner", model.SessionMeta{
		Kind:      model.SessionKindPlanner,
		Visible:   false,
		MissionID: "mission-1",
	})
	if created == nil {
		t.Fatalf("expected session with meta to be created")
	}
	if created.Meta.Kind != model.SessionKindPlanner || created.Meta.Visible {
		t.Fatalf("expected hidden planner session, got %#v", created.Meta)
	}
	if created.Meta.RuntimePreset != model.SessionRuntimePresetWorkspace {
		t.Fatalf("expected workspace runtime preset for planner (legacy planner_internal no longer used), got %#v", created.Meta)
	}

	st.UpdateSessionMeta(sessionID, func(meta *model.SessionMeta) {
		meta.Kind = model.SessionKindWorker
		meta.Visible = false
		meta.MissionID = "mission-2"
		meta.WorkspaceSessionID = "sess-workspace"
		meta.WorkerHostID = "host-1"
	})

	got := st.SessionMeta(sessionID)
	if got.Kind != model.SessionKindWorker || got.Visible {
		t.Fatalf("expected updated hidden worker meta, got %#v", got)
	}
	if got.RuntimePreset != model.SessionRuntimePresetWorker {
		t.Fatalf("expected worker runtime preset, got %#v", got)
	}
	if got.MissionID != "mission-2" || got.WorkspaceSessionID != "sess-workspace" || got.WorkerHostID != "host-1" {
		t.Fatalf("expected updated linkage fields, got %#v", got)
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

	loaded := reloaded.SessionMeta(sessionID)
	if loaded != got {
		t.Fatalf("expected worker meta to persist, got %#v want %#v", loaded, got)
	}
	planner := reloaded.SessionMeta("sess-planner")
	if planner.Kind != model.SessionKindPlanner || planner.Visible {
		t.Fatalf("expected hidden planner meta to persist, got %#v", planner)
	}
	if planner.RuntimePreset != model.SessionRuntimePresetWorkspace {
		t.Fatalf("expected workspace runtime preset for planner to persist, got %#v", planner)
	}
}

func TestLegacyStableStateDefaultsSessionMeta(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "legacy-state.json")
	legacy := []byte(`{
  "sessions": {
    "sess-legacy": {
      "id": "sess-legacy",
      "selectedHostId": "server-local",
      "createdAt": "2026-03-27T10:00:00Z",
      "lastActivityAt": "2026-03-27T10:00:00Z"
    }
  }
}`)
	if err := os.WriteFile(statePath, legacy, 0o600); err != nil {
		t.Fatalf("write legacy state: %v", err)
	}

	st := New()
	if err := st.LoadStableState(statePath); err != nil {
		t.Fatalf("load legacy state: %v", err)
	}

	meta := st.SessionMeta("sess-legacy")
	if meta.Kind != model.SessionKindSingleHost || !meta.Visible {
		t.Fatalf("expected legacy session meta to default to visible single_host, got %#v", meta)
	}
	if meta.RuntimePreset != model.SessionRuntimePresetSingleHost {
		t.Fatalf("expected legacy session runtime preset to default, got %#v", meta)
	}
}

func TestApprovalGrantDoesNotPersistAcrossStableState(t *testing.T) {
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

	if got, ok := reloaded.ApprovalGrant(sessionID, grant.Fingerprint); ok {
		t.Fatalf("expected approval grant not to be restored, got %#v", got)
	}
}

func TestApprovalGrantIsClearedOnHostSwitch(t *testing.T) {
	st := New()
	sessionID := "sess-host-switch"
	st.EnsureSession(sessionID)
	st.AddApprovalGrant(sessionID, model.ApprovalGrant{
		ID:          "grant-1",
		HostID:      model.ServerLocalHostID,
		Type:        "command",
		Fingerprint: "command|server-local|/tmp|rm /tmp/demo.txt",
		Command:     "rm /tmp/demo.txt",
		Cwd:         "/tmp",
		CreatedAt:   model.NowString(),
	})

	if _, ok := st.ApprovalGrant(sessionID, "command|server-local|/tmp|rm /tmp/demo.txt"); !ok {
		t.Fatalf("expected approval grant to exist before host switch")
	}

	st.SetSelectedHost(sessionID, "linux-01")

	if _, ok := st.ApprovalGrant(sessionID, "command|server-local|/tmp|rm /tmp/demo.txt"); ok {
		t.Fatalf("expected approval grant to be cleared after host switch")
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

func TestSessionSummariesAndHostSessionsHideInternalSessions(t *testing.T) {
	st := New()
	browserID := "browser-meta"

	visible := st.CreateSessionWithMeta(browserID, model.DefaultSessionMeta(), true)
	hidden := st.CreateSessionWithMeta(browserID, model.SessionMeta{
		Kind:    model.SessionKindWorker,
		Visible: false,
	}, true)

	st.SetSelectedHost(visible.ID, "web-01")
	st.SetSelectedHost(hidden.ID, "web-01")

	summaries := st.SessionSummaries(browserID)
	if len(summaries) != 1 {
		t.Fatalf("expected only visible session summary, got %d", len(summaries))
	}
	if summaries[0].ID != visible.ID {
		t.Fatalf("expected visible session summary, got %#v", summaries[0])
	}

	hostSessions := st.HostSessions("web-01", 10)
	if len(hostSessions) != 1 {
		t.Fatalf("expected only visible host session, got %d", len(hostSessions))
	}
	if hostSessions[0].SessionID != visible.ID {
		t.Fatalf("expected visible host session, got %#v", hostSessions[0])
	}
}

func TestResetConversationClearsThreadCardsAndApprovals(t *testing.T) {
	st := New()
	sessionID := "sess-reset"
	st.EnsureSession(sessionID)
	st.SetThread(sessionID, "thread-live")
	st.SetTurn(sessionID, "turn-live")
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
	st.AddChoice(sessionID, model.ChoiceRequest{
		ID:          "choice-1",
		TurnID:      "turn-live",
		Status:      "pending",
		RequestedAt: model.NowString(),
	})
	st.AddApprovalGrant(sessionID, model.ApprovalGrant{
		ID:          "grant-1",
		Type:        "command",
		Fingerprint: "command|server-local|/tmp|rm /tmp/demo.txt",
		CreatedAt:   model.NowString(),
	})
	st.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
		runtime.Turn.Active = true
		runtime.Turn.Phase = "executing"
		runtime.Activity.CommandsRun = 2
		runtime.Activity.CurrentReadingFile = "design_ui_0324.md"
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
	if len(session.Choices) != 0 {
		t.Fatalf("expected choices to be cleared, got %d", len(session.Choices))
	}
	if len(session.ApprovalGrants) != 0 {
		t.Fatalf("expected approval grants to be cleared, got %d", len(session.ApprovalGrants))
	}
	if session.Runtime.Turn.Active {
		t.Fatalf("expected turn runtime to be inactive after reset")
	}
	if session.Runtime.Turn.Phase != "idle" {
		t.Fatalf("expected turn phase to reset to idle, got %q", session.Runtime.Turn.Phase)
	}
	if session.Runtime.Activity.CommandsRun != 0 || session.Runtime.Activity.CurrentReadingFile != "" {
		t.Fatalf("expected runtime activity to be cleared, got %#v", session.Runtime.Activity)
	}
	if got := st.SessionIDByThread("thread-live"); got != "" {
		t.Fatalf("expected thread mapping to be removed, got %q", got)
	}
	if got := st.SessionIDByTurn("turn-live"); got != "" {
		t.Fatalf("expected turn mapping to be removed, got %q", got)
	}
}

func TestChoiceLifecycleInMemory(t *testing.T) {
	st := New()
	sessionID := "sess-choice"
	st.EnsureSession(sessionID)

	choice := model.ChoiceRequest{
		ID:          "choice-1",
		TurnID:      "turn-1",
		Status:      "pending",
		RequestedAt: model.NowString(),
		Questions: []model.ChoiceQuestion{
			{
				Header:   "Environment",
				Question: "请选择环境",
				Options: []model.ChoiceOption{
					{Label: "dev", Value: "dev"},
					{Label: "prod", Value: "prod"},
				},
			},
		},
	}
	st.AddChoice(sessionID, choice)

	got, ok := st.Choice(sessionID, choice.ID)
	if !ok {
		t.Fatalf("expected choice to be found")
	}
	if got.Status != "pending" || len(got.Questions) != 1 {
		t.Fatalf("unexpected choice payload: %#v", got)
	}

	st.ResolveChoiceWithAnswers(sessionID, choice.ID, "completed", "2026-03-24T12:00:00Z", []model.ChoiceAnswer{{
		Value: "prod",
		Label: "prod",
	}})

	resolved, ok := st.Choice(sessionID, choice.ID)
	if !ok {
		t.Fatalf("expected resolved choice to be found")
	}
	if resolved.Status != "completed" || resolved.ResolvedAt == "" || len(resolved.Answers) != 1 || resolved.Answers[0].Value != "prod" {
		t.Fatalf("expected choice to be resolved, got %#v", resolved)
	}
}

func TestSnapshotProjectsAgentLoopToolInvocationsAndEvidence(t *testing.T) {
	st := New()
	sessionID := "sess-loop-projection"
	st.EnsureSession(sessionID)
	st.SetTurn(sessionID, "turn-loop-projection")
	st.UpdateRuntime(sessionID, func(runtime *model.RuntimeState) {
		runtime.Turn.Active = true
		runtime.Turn.Phase = "waiting_input"
		runtime.Turn.StartedAt = "2026-04-08T10:00:00Z"
	})
	st.UpsertCard(sessionID, model.Card{
		ID:        "choice-card",
		Type:      "ChoiceCard",
		Title:     "确认意图",
		Question:  "你是只问能力，还是要开始只读诊断？",
		Status:    "pending",
		Questions: []model.ChoiceQuestion{{Question: "你是只问能力，还是要开始只读诊断？"}},
		CreatedAt: "2026-04-08T10:00:01Z",
		UpdatedAt: "2026-04-08T10:00:01Z",
	})
	st.UpsertCard(sessionID, model.Card{
		ID:        "command-card",
		Type:      "CommandCard",
		Command:   "uptime",
		Output:    "10:00 up 1 day",
		Status:    "completed",
		CreatedAt: "2026-04-08T10:00:02Z",
		UpdatedAt: "2026-04-08T10:00:03Z",
	})
	st.UpsertCard(sessionID, model.Card{
		ID:        "plan-approval-card",
		Type:      "PlanApprovalCard",
		Title:     "计划审批",
		Text:      "批准后派发 worker。",
		Status:    "pending",
		Detail:    map[string]any{"tool": "exit_plan_mode", "summary": "批准后派发 worker。"},
		CreatedAt: "2026-04-08T10:00:04Z",
		UpdatedAt: "2026-04-08T10:00:04Z",
	})

	snapshot := st.Snapshot(sessionID, model.UIConfig{})
	if snapshot.AgentLoop == nil {
		t.Fatalf("expected agent loop projection")
	}
	if snapshot.AgentLoop.Status != "waiting_user" || snapshot.AgentLoop.ActiveIterationID != "iter-turn-loop-projection" {
		t.Fatalf("unexpected loop projection: %#v", snapshot.AgentLoop)
	}
	if len(snapshot.AgentLoopIterations) != 1 || snapshot.AgentLoopIterations[0].StopReason != "waiting_user" {
		t.Fatalf("unexpected iterations: %#v", snapshot.AgentLoopIterations)
	}

	toolByName := make(map[string]model.ToolInvocation)
	for _, invocation := range snapshot.ToolInvocations {
		toolByName[invocation.Name] = invocation
	}
	if got := toolByName["ask_user_question"]; got.Status != "waiting_user" || got.EvidenceID == "" {
		t.Fatalf("expected ask_user_question waiting invocation, got %#v", got)
	}
	if got := toolByName["command"]; got.InputSummary != "uptime" || got.OutputSummary == "" {
		t.Fatalf("expected command invocation summary, got %#v", got)
	}
	if got := toolByName["exit_plan_mode"]; got.Status != "waiting_approval" || got.InputSummary != "计划审批" {
		t.Fatalf("expected exit_plan_mode waiting invocation, got %#v", got)
	}
	if len(snapshot.EvidenceSummaries) != len(snapshot.ToolInvocations) {
		t.Fatalf("expected evidence for each invocation, got evidence=%d tools=%d", len(snapshot.EvidenceSummaries), len(snapshot.ToolInvocations))
	}
}

func TestBrowserSessionTracksMultipleChatSessions(t *testing.T) {
	st := New()
	browserID := "browser-test"

	first := st.CreateSession(browserID)
	second := st.CreateSession(browserID)

	browser := st.BrowserSession(browserID)
	if browser == nil {
		t.Fatalf("expected browser session to exist")
	}
	if len(browser.SessionIDs) != 2 {
		t.Fatalf("expected 2 chat sessions, got %d", len(browser.SessionIDs))
	}
	if browser.ActiveSessionID != second.ID {
		t.Fatalf("expected latest session to be active, got %q", browser.ActiveSessionID)
	}

	if err := st.ActivateSession(browserID, first.ID); err != nil {
		t.Fatalf("activate session: %v", err)
	}
	browser = st.BrowserSession(browserID)
	if browser.ActiveSessionID != first.ID {
		t.Fatalf("expected first session to become active, got %q", browser.ActiveSessionID)
	}

	summaries := st.SessionSummaries(browserID)
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}
}

func TestSessionTranscriptRestoresAfterReload(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := New()
	st.SetStatePath(statePath)

	browserID := "browser-persist"
	session := st.CreateSession(browserID)
	st.UpsertCard(session.ID, model.Card{
		ID:        "card-1",
		Type:      "AssistantMessageCard",
		Text:      "hello history",
		Status:    "completed",
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	})
	st.flushSessionPersistence(session.ID)

	reloaded := New()
	reloaded.SetStatePath(statePath)
	if err := reloaded.LoadStableState(statePath); err != nil {
		t.Fatalf("load state: %v", err)
	}

	restoredBrowser := reloaded.BrowserSession(browserID)
	if restoredBrowser == nil {
		t.Fatalf("expected browser session to be restored")
	}
	if restoredBrowser.ActiveSessionID != session.ID {
		t.Fatalf("expected active session %q, got %q", session.ID, restoredBrowser.ActiveSessionID)
	}

	restoredSession := reloaded.Session(session.ID)
	if restoredSession == nil {
		t.Fatalf("expected session to be restored")
	}
	if len(restoredSession.Cards) != 1 {
		t.Fatalf("expected 1 restored card, got %d", len(restoredSession.Cards))
	}
	if restoredSession.Cards[0].Text != "hello history" {
		t.Fatalf("unexpected restored card: %#v", restoredSession.Cards[0])
	}
}

func TestAgentProfilesBackfillDefaultsOnLoad(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{"browserSessions":{},"sessions":{},"authSessions":{},"threadToSession":{},"loginToSession":{},"hosts":{}}`), 0o600); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	st := New()
	st.SetStatePath(statePath)
	if err := st.LoadStableState(statePath); err != nil {
		t.Fatalf("load state: %v", err)
	}

	profiles := st.AgentProfiles()
	if len(profiles) < 2 {
		t.Fatalf("expected default profiles to be backfilled, got %d", len(profiles))
	}

	mainProfile, ok := st.AgentProfile(string(model.AgentProfileTypeMainAgent))
	if !ok {
		t.Fatalf("expected main-agent profile to exist")
	}
	if mainProfile.Name != "Main Agent" || mainProfile.Type != string(model.AgentProfileTypeMainAgent) {
		t.Fatalf("unexpected main-agent profile: %#v", mainProfile)
	}
	if mainProfile.SystemPrompt.Content == "" || mainProfile.CommandPermissions.DefaultMode == "" {
		t.Fatalf("expected main-agent defaults to be populated: %#v", mainProfile)
	}

	hostProfile, ok := st.AgentProfile(string(model.AgentProfileTypeHostAgentDefault))
	if !ok {
		t.Fatalf("expected host-agent-default profile to exist")
	}
	if hostProfile.Name != "Host Agent Default" || hostProfile.Type != string(model.AgentProfileTypeHostAgentDefault) {
		t.Fatalf("unexpected host-agent-default profile: %#v", hostProfile)
	}
	if len(st.SkillCatalog()) == 0 {
		t.Fatalf("expected default skill catalog to be backfilled")
	}
	if len(st.MCPCatalog()) == 0 {
		t.Fatalf("expected default mcp catalog to be backfilled")
	}
}

func TestAgentProfileUpsertPersistsAndReloads(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := New()
	st.SetStatePath(statePath)

	st.UpsertAgentProfile(model.AgentProfile{
		ID:          string(model.AgentProfileTypeMainAgent),
		Type:        string(model.AgentProfileTypeMainAgent),
		Name:        "Primary Agent",
		Description: "Customized main profile",
		SystemPrompt: model.AgentSystemPrompt{
			Content: "Be concise and concrete.",
			Version: "v2",
		},
		Runtime: model.AgentRuntimeSettings{
			Model:           "gpt-5.4-mini",
			ReasoningEffort: "high",
			ApprovalPolicy:  "untrusted",
			SandboxMode:     "workspace-write",
		},
		CommandPermissions: model.AgentCommandPermissions{
			Enabled:               boolPtr(true),
			DefaultMode:           model.AgentPermissionModeAllow,
			AllowShellWrapper:     boolPtr(true),
			AllowSudo:             boolPtr(true),
			DefaultTimeoutSeconds: 45,
			AllowedWritableRoots:  []string{"/tmp/work"},
			CategoryPolicies: map[string]string{
				"filesystem_mutation": model.AgentPermissionModeApprovalRequired,
			},
		},
		CapabilityPermissions: model.AgentCapabilityPermissions{
			CommandExecution: model.AgentCapabilityEnabled,
			FileRead:         model.AgentCapabilityEnabled,
			FileSearch:       model.AgentCapabilityEnabled,
			FileChange:       model.AgentCapabilityDisabled,
			Terminal:         model.AgentCapabilityEnabled,
			WebSearch:        model.AgentCapabilityEnabled,
			WebOpen:          model.AgentCapabilityEnabled,
			Approval:         model.AgentCapabilityEnabled,
			MultiAgent:       model.AgentCapabilityDisabled,
			Plan:             model.AgentCapabilityEnabled,
			Summary:          model.AgentCapabilityEnabled,
		},
		Skills: []model.AgentSkill{
			{
				ID:             "skill-1",
				Name:           "review",
				Description:    "Review code changes",
				Source:         "builtin",
				Enabled:        true,
				ActivationMode: "manual",
			},
		},
		MCPs: []model.AgentMCP{
			{
				ID:         "mcp-1",
				Name:       "local-files",
				Type:       "filesystem",
				Source:     "builtin",
				Enabled:    true,
				Permission: "read_write",
			},
		},
		UpdatedBy: "tester",
	})

	beforeSave, ok := st.AgentProfile(string(model.AgentProfileTypeMainAgent))
	if !ok {
		t.Fatalf("expected profile to exist before save")
	}
	if beforeSave.Name != "Primary Agent" || !boolValue(beforeSave.CommandPermissions.AllowSudo, false) {
		t.Fatalf("unexpected profile before save: %#v", beforeSave)
	}

	if err := st.SaveStableState(statePath); err != nil {
		t.Fatalf("save state: %v", err)
	}

	reloaded := New()
	reloaded.SetStatePath(statePath)
	if err := reloaded.LoadStableState(statePath); err != nil {
		t.Fatalf("reload state: %v", err)
	}

	afterSave, ok := reloaded.AgentProfile(string(model.AgentProfileTypeMainAgent))
	if !ok {
		t.Fatalf("expected profile to survive reload")
	}
	if afterSave.Name != "Primary Agent" || afterSave.Description != "Customized main profile" {
		t.Fatalf("unexpected reloaded profile: %#v", afterSave)
	}
	if afterSave.Runtime.Model != "gpt-5.4-mini" || afterSave.CommandPermissions.DefaultTimeoutSeconds != 45 || !boolValue(afterSave.CommandPermissions.AllowSudo, false) {
		t.Fatalf("expected command permissions to persist, got %#v", afterSave.CommandPermissions)
	}
	if afterSave.CapabilityPermissions.FileChange != model.AgentCapabilityDisabled {
		t.Fatalf("expected capability permissions to persist, got %#v", afterSave.CapabilityPermissions)
	}
	if !containsSkill(afterSave.Skills, "skill-1") {
		t.Fatalf("expected custom skill to persist, got %#v", afterSave.Skills)
	}
	if !containsMCP(afterSave.MCPs, "mcp-1") {
		t.Fatalf("expected custom mcp to persist, got %#v", afterSave.MCPs)
	}
}

func TestResetAgentProfileRestoresDefaultProfile(t *testing.T) {
	st := New()
	st.UpsertAgentProfile(model.AgentProfile{
		ID:   string(model.AgentProfileTypeHostAgentDefault),
		Type: string(model.AgentProfileTypeHostAgentDefault),
		Name: "Custom Host Profile",
	})

	st.ResetAgentProfile(string(model.AgentProfileTypeHostAgentDefault))

	profile, ok := st.AgentProfile(string(model.AgentProfileTypeHostAgentDefault))
	if !ok {
		t.Fatalf("expected host-agent-default profile to exist")
	}
	if profile.Name != "Host Agent Default" {
		t.Fatalf("expected host-agent-default name to be restored, got %q", profile.Name)
	}
	if profile.SystemPrompt.Content == "" {
		t.Fatalf("expected restored profile to have system prompt content")
	}
}

func TestCatalogDeleteRemovesProfileReferencesAndPersists(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := New()
	st.SetStatePath(statePath)
	st.UpsertSkillCatalogItem(model.AgentSkill{
		ID:                    "custom-skill",
		Name:                  "Custom Skill",
		Source:                "local",
		DefaultEnabled:        false,
		DefaultActivationMode: model.AgentSkillActivationExplicit,
	})
	st.UpsertMCPCatalogItem(model.AgentMCP{
		ID:             "custom-mcp",
		Name:           "Custom MCP",
		Type:           "http",
		Source:         "local",
		DefaultEnabled: false,
		Permission:     model.AgentMCPPermissionReadonly,
	})
	profile := model.DefaultAgentProfile(string(model.AgentProfileTypeMainAgent))
	profile.Skills = append(profile.Skills, model.AgentSkill{
		ID:             "custom-skill",
		Name:           "Custom Skill",
		Enabled:        true,
		ActivationMode: model.AgentSkillActivationExplicit,
	})
	profile.MCPs = append(profile.MCPs, model.AgentMCP{
		ID:         "custom-mcp",
		Name:       "Custom MCP",
		Enabled:    true,
		Permission: model.AgentMCPPermissionReadonly,
	})
	st.UpsertAgentProfile(profile)

	st.DeleteSkillCatalogItem("custom-skill")
	st.DeleteMCPCatalogItem("custom-mcp")

	updated, ok := st.AgentProfile(string(model.AgentProfileTypeMainAgent))
	if !ok {
		t.Fatalf("expected profile to exist")
	}
	if containsSkill(updated.Skills, "custom-skill") {
		t.Fatalf("expected deleted skill binding to be removed, got %#v", updated.Skills)
	}
	if containsMCP(updated.MCPs, "custom-mcp") {
		t.Fatalf("expected deleted mcp binding to be removed, got %#v", updated.MCPs)
	}
	if err := st.SaveStableState(statePath); err != nil {
		t.Fatalf("save state: %v", err)
	}

	reloaded := New()
	reloaded.SetStatePath(statePath)
	if err := reloaded.LoadStableState(statePath); err != nil {
		t.Fatalf("reload state: %v", err)
	}
	for _, item := range reloaded.SkillCatalog() {
		if item.ID == "custom-skill" {
			t.Fatalf("expected deleted skill catalog item to stay removed")
		}
	}
	for _, item := range reloaded.MCPCatalog() {
		if item.ID == "custom-mcp" {
			t.Fatalf("expected deleted mcp catalog item to stay removed")
		}
	}
}

func boolPtr(value bool) *bool {
	v := value
	return &v
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func containsSkill(items []model.AgentSkill, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func containsMCP(items []model.AgentMCP, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}
