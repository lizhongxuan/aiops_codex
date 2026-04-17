package server

import (
	"strings"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func TestHandleWorkspaceQueryAIServerStateBindsEvidenceArtifact(t *testing.T) {
	app := newOrchestratorTestApp(t)
	sessionID := "sess-state-evidence"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.UpsertHost(model.Host{ID: "db-01", Name: "db-01", Kind: "remote", Status: "online", Executable: true})

	app.handleWorkspaceQueryAIServerState("raw-state-evidence", sessionID, map[string]any{
		"focus": "runtime",
	})

	cardID := dynamicToolCardID("raw-state-evidence")
	card := app.cardByID(sessionID, cardID)
	if card == nil {
		t.Fatalf("expected state query card to exist")
	}
	evidenceID := getStringAny(card.Detail, "evidenceId")
	if evidenceID == "" {
		t.Fatalf("expected evidenceId on state query card, got %#v", card.Detail)
	}
	if !strings.Contains(card.Text, evidenceID) {
		t.Fatalf("expected state query card text to reference evidence, got %q", card.Text)
	}

	item := app.store.Item(sessionID, evidenceID)
	if item == nil {
		t.Fatalf("expected evidence artifact %q to exist", evidenceID)
	}
	if got := getStringAny(item, "citationKey"); got != stableEvidenceCitationKey(evidenceID) {
		t.Fatalf("expected stable citation key, got %#v", item)
	}
	if got := getStringAny(item, "sourceKind"); got != "state_snapshot" {
		t.Fatalf("expected state_snapshot source kind, got %#v", item)
	}
	metadata, _ := item["metadata"].(map[string]any)
	if got := getStringAny(metadata, "cardId"); got != cardID {
		t.Fatalf("expected evidence metadata to retain cardId, got %#v", item)
	}
}

func TestFinalizeExecCardStoresEvidenceArtifact(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-finalize-evidence"
	cardID := "card-finalize-evidence"
	createdAt := model.NowString()
	app.store.EnsureSession(sessionID)
	app.store.UpsertCard(sessionID, model.Card{
		ID:        cardID,
		Type:      "CommandCard",
		Status:    "inProgress",
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	})

	exec := &remoteExecSession{
		ID:        "exec-finalize-evidence",
		SessionID: sessionID,
		HostID:    "linux-02",
		CardID:    cardID,
		ToolName:  "execute_readonly_query",
		Command:   "journalctl -u nginx -n 20",
	}
	app.finalizeExecCard(exec, createdAt, remoteExecResult{
		Output:   "nginx healthy\n",
		Stdout:   "nginx healthy\n",
		ExitCode: 0,
		Status:   "completed",
	})

	card := app.cardByID(sessionID, cardID)
	if card == nil {
		t.Fatalf("expected finalized exec card to exist")
	}
	evidenceID := getStringAny(card.Detail, "evidenceId")
	if evidenceID == "" {
		t.Fatalf("expected evidenceId on finalized exec card, got %#v", card.Detail)
	}
	item := app.store.Item(sessionID, evidenceID)
	if item == nil {
		t.Fatalf("expected evidence artifact %q to exist", evidenceID)
	}
	if got := getStringAny(item, "sourceKind"); got != "command" {
		t.Fatalf("expected command source kind, got %#v", item)
	}
	if got := getStringAny(item, "sourceRef"); got != "linux-02" {
		t.Fatalf("expected host to be retained as sourceRef, got %#v", item)
	}
	metadata, _ := item["metadata"].(map[string]any)
	if got := getStringAny(metadata, "cardId"); got != cardID {
		t.Fatalf("expected metadata.cardId to match card, got %#v", item)
	}
}

func TestCreateRecoveryEvidenceCardBindsEvidenceArtifact(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-recovery-evidence"
	app.store.EnsureSession(sessionID)

	createRecoveryEvidenceCard(app, sessionID, "failed", "45 秒内没有返回任何进展")

	session := app.store.Session(sessionID)
	if session == nil || len(session.Cards) == 0 {
		t.Fatalf("expected recovery card to be created")
	}
	var card *model.Card
	for i := range session.Cards {
		if session.Cards[i].Type == "ErrorCard" {
			card = &session.Cards[i]
			break
		}
	}
	if card == nil {
		t.Fatalf("expected error recovery card, got %#v", session.Cards)
	}
	evidenceID := getStringAny(card.Detail, "evidenceId")
	if evidenceID == "" {
		t.Fatalf("expected evidenceId on recovery card, got %#v", card.Detail)
	}
	item := app.store.Item(sessionID, evidenceID)
	if item == nil {
		t.Fatalf("expected recovery evidence artifact %q to exist", evidenceID)
	}
	if got := getStringAny(item, "kind"); got != "error_recovery" {
		t.Fatalf("expected error_recovery kind, got %#v", item)
	}
	metadata, _ := item["metadata"].(map[string]any)
	if got := getStringAny(metadata, "cardId"); got == "" {
		t.Fatalf("expected recovery evidence metadata to include cardId, got %#v", item)
	}
}
