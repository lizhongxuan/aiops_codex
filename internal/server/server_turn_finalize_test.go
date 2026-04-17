package server

import (
	"strings"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func TestFinalizeOpenTurnCardsMarksLingeringCommandAndFileChangeFailed(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-finalize-open-turn"
	now := model.NowString()

	app.store.EnsureSession(sessionID)
	app.store.UpsertCard(sessionID, model.Card{
		ID:        "cmd-1",
		Type:      "CommandCard",
		Status:    "inProgress",
		Command:   "uptime",
		CreatedAt: now,
		UpdatedAt: now,
	})
	app.store.UpsertCard(sessionID, model.Card{
		ID:        "file-1",
		Type:      "FileChangeCard",
		Status:    "inProgress",
		CreatedAt: now,
		UpdatedAt: now,
	})

	app.finalizeOpenTurnCards(sessionID, "completed")

	commandCard := app.cardByID(sessionID, "cmd-1")
	if commandCard == nil {
		t.Fatalf("expected command card to exist")
	}
	if commandCard.Status != "failed" {
		t.Fatalf("expected lingering command card to fail, got %q", commandCard.Status)
	}
	if !strings.Contains(commandCard.Output, "没有返回最终结果") {
		t.Fatalf("expected lingering command card output to explain forced failure, got %q", commandCard.Output)
	}
	if commandCard.Summary == "" {
		t.Fatalf("expected lingering command card summary to be populated")
	}

	fileCard := app.cardByID(sessionID, "file-1")
	if fileCard == nil {
		t.Fatalf("expected file change card to exist")
	}
	if fileCard.Status != "failed" {
		t.Fatalf("expected lingering file change card to fail, got %q", fileCard.Status)
	}
	if !strings.Contains(fileCard.Text, "没有返回最终结果") {
		t.Fatalf("expected lingering file change card text to explain forced failure, got %q", fileCard.Text)
	}
}

func TestHandleItemCompletedLocalCommandAddsPresentation(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-local-command-summary"
	threadID := "thread-local-command-summary"
	turnID := "turn-local-command-summary"
	now := model.NowString()

	app.store.EnsureSession(sessionID)
	app.store.SetThread(sessionID, threadID)
	app.store.SetTurn(sessionID, turnID)
	app.store.UpsertCard(sessionID, model.Card{
		ID:        "cmd-local",
		Type:      "CommandCard",
		Command:   "uptime",
		Status:    "inProgress",
		CreatedAt: now,
		UpdatedAt: now,
	})

	app.handleItemCompleted(map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
		"item": map[string]any{
			"id":               "cmd-local",
			"type":             "commandExecution",
			"status":           "completed",
			"aggregatedOutput": "load average: 0.12 0.18 0.22\nusers: 2\n",
			"exitCode":         float64(0),
			"durationMs":       float64(1200),
		},
	})

	card := app.cardByID(sessionID, "cmd-local")
	if card == nil {
		t.Fatalf("expected local command card to exist")
	}
	if card.Status != "completed" {
		t.Fatalf("expected local command to complete, got %q", card.Status)
	}
	if card.Summary == "" {
		t.Fatalf("expected local command summary to be populated")
	}
	if card.Stdout == "" {
		t.Fatalf("expected local command stdout to be populated")
	}
	if len(card.KVRows) == 0 || card.KVRows[0].Key != "退出码" {
		t.Fatalf("expected local command kv rows to include exit code, got %#v", card.KVRows)
	}
	evidenceID := getStringAny(card.Detail, "evidenceId")
	if evidenceID == "" {
		t.Fatalf("expected local command evidenceId, got %#v", card.Detail)
	}
	item := app.store.Item(sessionID, evidenceID)
	if item == nil {
		t.Fatalf("expected local command evidence artifact %q", evidenceID)
	}
	if got := getStringAny(item, "sourceKind"); got != "command" {
		t.Fatalf("expected command source kind, got %#v", item)
	}
}

func TestHandleItemCompletedLocalCommandMapsPermissionDenied(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-local-command-permission"
	threadID := "thread-local-command-permission"
	turnID := "turn-local-command-permission"
	now := model.NowString()

	app.store.EnsureSession(sessionID)
	app.store.SetThread(sessionID, threadID)
	app.store.SetTurn(sessionID, turnID)
	app.store.UpsertCard(sessionID, model.Card{
		ID:        "cmd-local-failed",
		Type:      "CommandCard",
		Command:   "ps aux",
		Status:    "inProgress",
		CreatedAt: now,
		UpdatedAt: now,
	})

	app.handleItemCompleted(map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
		"item": map[string]any{
			"id":               "cmd-local-failed",
			"type":             "commandExecution",
			"status":           "completed",
			"aggregatedOutput": "zsh:1: operation not permitted: ps\nfull stderr\n",
			"exitCode":         float64(1),
		},
	})

	card := app.cardByID(sessionID, "cmd-local-failed")
	if card == nil {
		t.Fatalf("expected failed local command card to exist")
	}
	if card.Status != "permission_denied" {
		t.Fatalf("expected permission_denied status, got %q", card.Status)
	}
	if card.Stderr == "" {
		t.Fatalf("expected local command stderr to be populated")
	}
	if !strings.Contains(card.Summary, "权限不足") {
		t.Fatalf("expected failed summary to explain permission issue, got %q", card.Summary)
	}
	evidenceID := getStringAny(card.Detail, "evidenceId")
	if evidenceID == "" {
		t.Fatalf("expected failed local command evidenceId, got %#v", card.Detail)
	}
	item := app.store.Item(sessionID, evidenceID)
	if item == nil {
		t.Fatalf("expected failed local command evidence artifact %q", evidenceID)
	}
	if got := getStringAny(item, "summary"); got == "" {
		t.Fatalf("expected failed local command evidence summary, got %#v", item)
	}
}

func TestHandleItemCompletedFileChangeBindsEvidence(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-file-change-evidence"
	threadID := "thread-file-change-evidence"
	turnID := "turn-file-change-evidence"
	now := model.NowString()

	app.store.EnsureSession(sessionID)
	app.store.SetThread(sessionID, threadID)
	app.store.SetTurn(sessionID, turnID)
	app.store.UpsertCard(sessionID, model.Card{
		ID:        "file-local",
		Type:      "FileChangeCard",
		Status:    "inProgress",
		Title:     "File change",
		CreatedAt: now,
		UpdatedAt: now,
	})

	app.handleItemCompleted(map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
		"item": map[string]any{
			"id":     "file-local",
			"type":   "fileChange",
			"status": "completed",
			"changes": []any{
				map[string]any{
					"path": "/etc/nginx/nginx.conf",
					"kind": "update",
					"diff": "@@ -1 +1 @@\n-user nginx\n+user www-data\n",
				},
			},
		},
	})

	card := app.cardByID(sessionID, "file-local")
	if card == nil {
		t.Fatalf("expected file change card to exist")
	}
	evidenceID := getStringAny(card.Detail, "evidenceId")
	if evidenceID == "" {
		t.Fatalf("expected file change evidenceId, got %#v", card.Detail)
	}
	item := app.store.Item(sessionID, evidenceID)
	if item == nil {
		t.Fatalf("expected file change evidence artifact %q", evidenceID)
	}
	if got := getStringAny(item, "sourceKind"); got != "config_diff" {
		t.Fatalf("expected config_diff source kind, got %#v", item)
	}
	if got := getStringAny(item, "sourceRef"); got != "/etc/nginx/nginx.conf" {
		t.Fatalf("expected changed path as sourceRef, got %#v", item)
	}
}
