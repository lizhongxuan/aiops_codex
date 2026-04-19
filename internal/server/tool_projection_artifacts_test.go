package server

import (
	"context"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/store"
)

func TestArtifactProjectionBindsEvidenceAndVerificationForFileChange(t *testing.T) {
	app := &App{store: store.New()}
	sessionID := "sess-artifact-projection"
	subscriber := NewProductProjectionSubscriber(app)

	err := subscriber.HandleToolLifecycleEvent(context.Background(), ToolLifecycleEvent{
		Type:      ToolLifecycleEventCompleted,
		SessionID: sessionID,
		HostID:    "server-1",
		Payload: map[string]any{
			"syncActionArtifacts": true,
			"finalCard": map[string]any{
				"cardId":   "file-change-1",
				"cardType": "FileChangeCard",
				"title":    "Remote file change",
				"text":     "已修改 /etc/app.conf",
				"status":   "completed",
				"hostId":   "server-1",
				"detail": map[string]any{
					"filePath":      "/etc/app.conf",
					"dryRunSummary": "@@ -1 +1 @@\n-old\n+new\n",
				},
				"changes": []map[string]any{{
					"path": "/etc/app.conf",
					"kind": "update",
					"diff": "@@ -1 +1 @@\n-old\n+new\n",
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("project artifact event: %v", err)
	}

	card := app.cardByID(sessionID, "file-change-1")
	if card == nil {
		t.Fatal("expected file change card to exist")
	}
	evidenceID := getStringAny(card.Detail, "evidenceId")
	if evidenceID == "" {
		t.Fatalf("expected evidenceId on projected file change card, got %#v", card.Detail)
	}
	item := app.store.Item(sessionID, evidenceID)
	if item == nil {
		t.Fatalf("expected evidence artifact %q", evidenceID)
	}
	if got := getStringAny(item, "sourceKind"); got != "config_diff" {
		t.Fatalf("expected config_diff evidence source kind, got %#v", item)
	}

	record := findVerificationRecord(app.snapshot(sessionID).VerificationRecords, "verify-file-change-1")
	if record == nil {
		t.Fatalf("expected verification record for projected file change, got %#v", app.snapshot(sessionID).VerificationRecords)
	}
	if record.Status != "passed" {
		t.Fatalf("expected verification status passed, got %#v", record)
	}
	if got := anyToString(record.Metadata["evidenceId"]); got != evidenceID {
		t.Fatalf("expected verification metadata evidenceId %q, got %#v", evidenceID, record.Metadata)
	}
}
