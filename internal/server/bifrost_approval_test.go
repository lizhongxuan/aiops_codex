package server

import (
	"context"
	"errors"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/agentloop"
	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/store"
)

func TestEmitBifrostApprovalRequestedEventProjectsApprovalAndCard(t *testing.T) {
	app := newBifrostApprovalTestApp(t)
	sessionID := "sess-bifrost-event-requested"
	approval := model.ApprovalRequest{
		ID:          "approval-bifrost-event-requested",
		HostID:      model.ServerLocalHostID,
		Type:        bifrostApprovalTypeRemoteCommand,
		Status:      "pending",
		ItemID:      "toolcmd-bifrost-requested",
		Command:     "uptime",
		Cwd:         "/tmp",
		Reason:      "need approval",
		RequestedAt: "2026-04-17T10:00:00Z",
		Decisions:   []string{"accept", "accept_session", "decline"},
	}
	card := model.Card{
		ID:        approval.ItemID,
		Type:      "CommandApprovalCard",
		Title:     "Remote command approval required",
		Command:   approval.Command,
		Cwd:       approval.Cwd,
		Text:      approval.Reason,
		Status:    "pending",
		HostID:    model.ServerLocalHostID,
		HostName:  "server-local",
		Detail:    map[string]any{"toolName": "execute_command", "riskLevel": "high"},
		CreatedAt: approval.RequestedAt,
		UpdatedAt: approval.RequestedAt,
	}

	if ok := app.emitBifrostApprovalRequestedEvent(context.Background(), sessionID, "execute_command", approval, card); !ok {
		t.Fatal("expected approval requested event to be emitted")
	}

	storedApproval, ok := app.store.Approval(sessionID, approval.ID)
	if !ok {
		t.Fatal("expected approval to be projected into store")
	}
	if storedApproval.ItemID != approval.ItemID || storedApproval.Command != approval.Command {
		t.Fatalf("unexpected approval projection: %#v", storedApproval)
	}
	storedCard := app.cardByID(sessionID, approval.ItemID)
	if storedCard == nil {
		t.Fatal("expected approval card to be projected")
	}
	if storedCard.Type != "CommandApprovalCard" || storedCard.Status != "pending" {
		t.Fatalf("unexpected approval card projection: %#v", storedCard)
	}
	if got := getStringAny(storedCard.Detail, "riskLevel"); got != "high" {
		t.Fatalf("expected detail riskLevel to be preserved, got %#v", storedCard.Detail)
	}
}

func TestEmitBifrostApprovalResolvedEventProjectsAutoApprovedCardState(t *testing.T) {
	app := newBifrostApprovalTestApp(t)
	sessionID := "sess-bifrost-event-resolved"
	approval := model.ApprovalRequest{
		ID:          "approval-bifrost-event-resolved",
		HostID:      model.ServerLocalHostID,
		Type:        bifrostApprovalTypeRemoteCommand,
		Status:      "accepted_for_host_auto",
		ItemID:      "toolcmd-bifrost-resolved",
		Command:     "uptime",
		Cwd:         "/tmp",
		Reason:      "auto approved",
		RequestedAt: "2026-04-17T10:00:00Z",
		ResolvedAt:  "2026-04-17T10:01:00Z",
	}
	card := model.Card{
		ID:        "auto-approval-" + approval.ItemID,
		Type:      "NoticeCard",
		Title:     "Auto-approved by host grant",
		Text:      "host grant auto-approved",
		Status:    "notice",
		CreatedAt: approval.ResolvedAt,
		UpdatedAt: approval.ResolvedAt,
	}

	if ok := app.emitBifrostApprovalResolvedEvent(context.Background(), sessionID, "execute_command", "executing", approval, card); !ok {
		t.Fatal("expected approval resolved event to be emitted")
	}

	storedApproval, ok := app.store.Approval(sessionID, approval.ID)
	if !ok {
		t.Fatal("expected resolved approval to be projected into store")
	}
	if storedApproval.Status != "accepted_for_host_auto" || storedApproval.ResolvedAt != approval.ResolvedAt {
		t.Fatalf("unexpected resolved approval projection: %#v", storedApproval)
	}
	storedCard := app.cardByID(sessionID, card.ID)
	if storedCard == nil {
		t.Fatal("expected auto approval notice card to be projected")
	}
	if storedCard.Type != "NoticeCard" || storedCard.Status != "notice" {
		t.Fatalf("unexpected auto approval card projection: %#v", storedCard)
	}
}

func TestBifrostRequestToolApprovalAutoApprovesBySessionGrant(t *testing.T) {
	app := newBifrostApprovalTestApp(t)
	sessionID := "sess-bifrost-session-grant"
	hostID := model.ServerLocalHostID

	configureBifrostCommandApprovalProfile(app, model.AgentPermissionModeApprovalRequired)
	app.store.AddApprovalGrant(sessionID, model.ApprovalGrant{
		ID:          "grant-session-1",
		HostID:      hostID,
		Type:        "command",
		Fingerprint: approvalFingerprintForCommand(hostID, "uptime", "/tmp"),
		Command:     "uptime",
		Cwd:         "/tmp",
		CreatedAt:   model.NowString(),
	})

	approvalID := requestBifrostCommandApproval(t, app, sessionID, "call-session-grant", "uptime", "/tmp")

	approval, ok := app.store.Approval(sessionID, approvalID)
	if !ok {
		t.Fatalf("expected approval %q to be stored", approvalID)
	}
	if approval.Status != "accepted_for_session_auto" {
		t.Fatalf("expected session grant auto approval, got %#v", approval.Status)
	}
	if approval.ResolvedAt == "" {
		t.Fatalf("expected resolved time to be recorded, got %#v", approval)
	}
	if phase := app.store.Session(sessionID).Runtime.Turn.Phase; phase != "executing" {
		t.Fatalf("expected executing phase after session grant auto approval, got %q", phase)
	}
	card := app.cardByID(sessionID, "auto-approval-toolcmd-call-session-grant")
	if card == nil {
		t.Fatal("expected auto approval notice card to be created")
	}
	if card.Title != "Auto-approved for session" || card.Status != "notice" {
		t.Fatalf("unexpected auto approval notice card: %#v", card)
	}
}

func TestBifrostRequestToolApprovalAutoApprovesByHostGrant(t *testing.T) {
	app := newBifrostApprovalTestApp(t)
	sessionID := "sess-bifrost-host-grant"
	hostID := model.ServerLocalHostID

	configureBifrostCommandApprovalProfile(app, model.AgentPermissionModeApprovalRequired)
	app.approvalGrantStore = store.NewApprovalGrantStore("")
	if err := app.approvalGrantStore.Add(model.ApprovalGrantRecord{
		ID:          "grant-host-1",
		HostID:      hostID,
		HostScope:   "host",
		GrantType:   "command",
		Fingerprint: approvalFingerprintForCommand(hostID, "uptime", "/tmp"),
		Command:     "uptime",
		Cwd:         "/tmp",
		CreatedBy:   "test",
		Status:      "active",
	}); err != nil {
		t.Fatalf("add host grant: %v", err)
	}

	approvalID := requestBifrostCommandApproval(t, app, sessionID, "call-host-grant", "uptime", "/tmp")

	approval, ok := app.store.Approval(sessionID, approvalID)
	if !ok {
		t.Fatalf("expected approval %q to be stored", approvalID)
	}
	if approval.Status != "accepted_for_host_auto" {
		t.Fatalf("expected host grant auto approval, got %#v", approval.Status)
	}
	if phase := app.store.Session(sessionID).Runtime.Turn.Phase; phase != "executing" {
		t.Fatalf("expected executing phase after host grant auto approval, got %q", phase)
	}
	card := app.cardByID(sessionID, "auto-approval-toolcmd-call-host-grant")
	if card == nil {
		t.Fatal("expected host grant notice card to be created")
	}
	if card.Title != "Auto-approved by host grant" || card.Status != "notice" {
		t.Fatalf("unexpected host grant notice card: %#v", card)
	}
}

func TestBifrostRequestToolApprovalAutoApprovesByPolicy(t *testing.T) {
	app := newBifrostApprovalTestApp(t)
	sessionID := "sess-bifrost-policy-auto"

	configureBifrostCommandApprovalProfile(app, model.AgentPermissionModeAllow)

	approvalID := requestBifrostCommandApproval(t, app, sessionID, "call-policy-auto", "uptime", "/tmp")

	approval, ok := app.store.Approval(sessionID, approvalID)
	if !ok {
		t.Fatalf("expected approval %q to be stored", approvalID)
	}
	if approval.Status != "accepted_by_policy_auto" {
		t.Fatalf("expected policy auto approval, got %#v", approval.Status)
	}
	if phase := app.store.Session(sessionID).Runtime.Turn.Phase; phase != "executing" {
		t.Fatalf("expected executing phase after policy auto approval, got %q", phase)
	}
	card := app.cardByID(sessionID, "auto-approval-toolcmd-call-policy-auto")
	if card == nil {
		t.Fatal("expected policy auto approval notice card to be created")
	}
	if card.Title != "Auto-approved by profile" || card.Status != "notice" {
		t.Fatalf("unexpected policy auto approval notice card: %#v", card)
	}
}

func TestBifrostRequestToolApprovalKeepsPendingApproval(t *testing.T) {
	app := newBifrostApprovalTestApp(t)
	sessionID := "sess-bifrost-pending"

	configureBifrostCommandApprovalProfile(app, model.AgentPermissionModeApprovalRequired)

	approvalID := requestBifrostCommandApproval(t, app, sessionID, "call-pending", "uptime", "/tmp")

	approval, ok := app.store.Approval(sessionID, approvalID)
	if !ok {
		t.Fatalf("expected approval %q to be stored", approvalID)
	}
	if approval.Status != "pending" {
		t.Fatalf("expected pending approval to remain pending, got %#v", approval.Status)
	}
	if approval.ResolvedAt != "" {
		t.Fatalf("expected pending approval to remain unresolved, got %#v", approval)
	}
	if phase := app.store.Session(sessionID).Runtime.Turn.Phase; phase != "waiting_approval" {
		t.Fatalf("expected waiting_approval phase for pending approval, got %q", phase)
	}
	card := app.cardByID(sessionID, "toolcmd-call-pending")
	if card == nil {
		t.Fatal("expected pending approval card to be created")
	}
	if card.Type != "CommandApprovalCard" || card.Status != "pending" {
		t.Fatalf("unexpected pending approval card: %#v", card)
	}
}

func TestBifrostRequestApprovalEmitsLifecycleEventForWorkerSession(t *testing.T) {
	app, workspaceSessionID, workerSessionID := setupWorkerMissionForToolProjection(t)
	session := agentloop.NewSession(workerSessionID, agentloop.SessionSpec{Model: "test-model"})

	_, err := app.bifrostRequestApproval(context.Background(), session, bifrost.ToolCall{
		ID:       "call-bifrost-request-approval-worker",
		Function: bifrost.FunctionCall{Name: "request_approval"},
	}, map[string]any{
		"command":            "sudo systemctl restart nginx",
		"riskAssessment":     "high",
		"expectedImpact":     "brief nginx reload",
		"rollbackSuggestion": "systemctl restart nginx",
	})
	if !errors.Is(err, agentloop.ErrPauseTurn) {
		t.Fatalf("expected pending approval to pause turn, got %v", err)
	}

	events := app.toolEventStore.SessionEvents(workerSessionID)
	foundRequested := false
	for _, event := range events {
		if event.Type != string(ToolLifecycleEventApprovalRequested) {
			continue
		}
		foundRequested = true
		break
	}
	if !foundRequested {
		t.Fatalf("expected approval_requested lifecycle event, got %#v", events)
	}

	var approval model.ApprovalRequest
	for _, item := range app.store.Session(workerSessionID).Approvals {
		approval = item
		break
	}
	if approval.ID == "" {
		t.Fatal("expected pending approval on worker session")
	}
	route, ok := app.orchestrator.ResolveApprovalRoute(approval.ID)
	if !ok || route.WorkerSessionID != workerSessionID {
		t.Fatalf("expected worker approval route %s -> %s, got %#v ok=%t", approval.ID, workerSessionID, route, ok)
	}
	if _, ok := app.store.Approval(workspaceSessionID, approval.ID); !ok {
		t.Fatalf("expected mirrored workspace approval %s", approval.ID)
	}
}

func TestBifrostApprovalRequestedEventProjectsApprovalAndCard(t *testing.T) {
	app := newBifrostApprovalTestApp(t)
	sessionID := "sess-bifrost-requested-event"
	approvalID := "approval-requested-event"
	cardID := "card-requested-event"

	app.store.EnsureSession(sessionID)
	if err := app.toolEventBus.Emit(context.Background(), ToolLifecycleEvent{
		Type:       ToolLifecycleEventApprovalRequested,
		SessionID:  sessionID,
		ToolName:   "execute_command",
		CallID:     "call-requested-event",
		ApprovalID: approvalID,
		CardID:     cardID,
		Payload: map[string]any{
			"approval": map[string]any{
				"approvalId":   approvalID,
				"cardId":       cardID,
				"approvalType": bifrostApprovalTypeRemoteCommand,
				"hostId":       model.ServerLocalHostID,
				"title":        "Command approval",
				"text":         "Run uptime in /tmp",
				"command":      "uptime",
				"cwd":          "/tmp",
				"decisions":    []any{"accept", "decline"},
			},
			"card": map[string]any{
				"cardId":   cardID,
				"cardType": "CommandApprovalCard",
				"status":   "pending",
			},
		},
		Metadata: map[string]any{
			"approval": map[string]any{
				"requestedAt": "2026-04-17T10:00:00Z",
			},
		},
	}); err != nil {
		t.Fatalf("emit approval requested event: %v", err)
	}

	approval, ok := app.store.Approval(sessionID, approvalID)
	if !ok {
		t.Fatalf("expected approval %q to be stored", approvalID)
	}
	if approval.Status != "pending" {
		t.Fatalf("expected projected approval to remain pending, got %#v", approval.Status)
	}
	if approval.ItemID != cardID || approval.RequestedAt != "2026-04-17T10:00:00Z" {
		t.Fatalf("expected projected approval to capture card and request time, got %#v", approval)
	}

	card := app.cardByID(sessionID, cardID)
	if card == nil {
		t.Fatal("expected approval card to be stored")
	}
	if card.Type != "CommandApprovalCard" || card.Status != "pending" {
		t.Fatalf("unexpected approval card projection: %#v", card)
	}
	if card.Approval == nil || card.Approval.RequestID != approvalID {
		t.Fatalf("expected approval reference to be projected, got %#v", card.Approval)
	}
}

func TestBifrostApprovalResolvedEventProjectsAutoApprovedCardState(t *testing.T) {
	cases := []struct {
		name         string
		status       string
		expectedCard string
	}{
		{name: "host grant", status: "accepted_for_host_auto", expectedCard: "completed"},
		{name: "policy auto", status: "accepted_by_policy_auto", expectedCard: "completed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := newBifrostApprovalTestApp(t)
			sessionID := "sess-bifrost-resolved-" + tc.status
			approvalID := "approval-resolved-" + tc.status
			cardID := "card-resolved-" + tc.status

			app.store.EnsureSession(sessionID)
			if err := app.toolEventBus.Emit(context.Background(), ToolLifecycleEvent{
				Type:       ToolLifecycleEventApprovalRequested,
				SessionID:  sessionID,
				ToolName:   "execute_command",
				CallID:     "call-requested-" + tc.status,
				ApprovalID: approvalID,
				CardID:     cardID,
				Payload: map[string]any{
					"approval": map[string]any{
						"approvalId":   approvalID,
						"cardId":       cardID,
						"approvalType": bifrostApprovalTypeRemoteCommand,
						"hostId":       model.ServerLocalHostID,
						"title":        "Command approval",
						"text":         "Run uptime in /tmp",
						"command":      "uptime",
						"cwd":          "/tmp",
					},
					"card": map[string]any{
						"cardId":   cardID,
						"cardType": "CommandApprovalCard",
						"status":   "pending",
					},
				},
			}); err != nil {
				t.Fatalf("emit approval requested event: %v", err)
			}

			if err := app.toolEventBus.Emit(context.Background(), ToolLifecycleEvent{
				Type:       ToolLifecycleEventApprovalResolved,
				SessionID:  sessionID,
				ToolName:   "execute_command",
				CallID:     "call-resolved-" + tc.status,
				ApprovalID: approvalID,
				CardID:     cardID,
				Payload: map[string]any{
					"approval": map[string]any{
						"approvalId": approvalID,
						"cardId":     cardID,
						"status":     tc.status,
						"resolvedAt": "2026-04-17T10:05:00Z",
					},
					"card": map[string]any{
						"cardId": cardID,
					},
				},
			}); err != nil {
				t.Fatalf("emit approval resolved event: %v", err)
			}

			approval, ok := app.store.Approval(sessionID, approvalID)
			if !ok {
				t.Fatalf("expected approval %q to be stored", approvalID)
			}
			if approval.Status != tc.status {
				t.Fatalf("expected approval status %q, got %#v", tc.status, approval.Status)
			}
			if approval.ResolvedAt != "2026-04-17T10:05:00Z" {
				t.Fatalf("expected resolved time to be projected, got %#v", approval.ResolvedAt)
			}

			card := app.cardByID(sessionID, cardID)
			if card == nil {
				t.Fatal("expected approval card to be stored")
			}
			if card.Status != tc.expectedCard {
				t.Fatalf("expected auto-approved card to resolve to %q, got %#v", tc.expectedCard, card.Status)
			}
		})
	}
}

func newBifrostApprovalTestApp(t *testing.T) *App {
	t.Helper()
	return New(config.Config{})
}

func configureBifrostCommandApprovalProfile(app *App, systemInspectionMode string) {
	if app == nil {
		return
	}

	profile := app.mainAgentProfile()
	profile.CapabilityPermissions.CommandExecution = model.AgentCapabilityEnabled
	profile.CommandPermissions.DefaultMode = model.AgentPermissionModeApprovalRequired
	if profile.CommandPermissions.CategoryPolicies == nil {
		profile.CommandPermissions.CategoryPolicies = make(map[string]string)
	}
	profile.CommandPermissions.CategoryPolicies["system_inspection"] = systemInspectionMode
	app.store.UpsertAgentProfile(profile)

	if hostProfile, ok := app.store.AgentProfile(string(model.AgentProfileTypeHostAgentDefault)); ok {
		hostProfile.CapabilityPermissions.CommandExecution = model.AgentCapabilityEnabled
		hostProfile.CommandPermissions.DefaultMode = model.AgentPermissionModeApprovalRequired
		if hostProfile.CommandPermissions.CategoryPolicies == nil {
			hostProfile.CommandPermissions.CategoryPolicies = make(map[string]string)
		}
		hostProfile.CommandPermissions.CategoryPolicies["system_inspection"] = model.AgentPermissionModeApprovalRequired
		app.store.UpsertAgentProfile(hostProfile)
	}
}

func requestBifrostCommandApproval(t *testing.T, app *App, sessionID, callID, command, cwd string) string {
	t.Helper()
	app.store.EnsureSession(sessionID)

	session := agentloop.NewSession(sessionID, agentloop.SessionSpec{Model: "test-model"})
	approvalID, err := app.requestBifrostToolApproval(context.Background(), session, bifrost.ToolCall{
		ID:       callID,
		Function: bifrost.FunctionCall{Name: "execute_command"},
	}, execToolArgs{
		Command: command,
		Cwd:     cwd,
		Reason:  "bifrost approval test",
	}, remoteFileChangeArgs{}, false)
	if err != nil {
		t.Fatalf("request bifrost approval: %v", err)
	}
	if approvalID == "" {
		t.Fatal("expected approval id to be returned")
	}
	return approvalID
}
