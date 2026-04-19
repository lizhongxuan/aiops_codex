package server

import (
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/model"
	"github.com/lizhongxuan/aiops-codex/internal/store"
)

func TestToolProjectionCardsCreatesAndCompletesProcessCard(t *testing.T) {
	app := &App{store: store.New()}
	sessionID := "sess-card-projection"

	app.projectToolLifecycleCards(sessionID, ToolLifecycleEvent{
		Type:           ToolLifecycleEventStarted,
		SessionID:      sessionID,
		ToolName:       "read_file",
		CardID:         "proc-1",
		Label:          "现在浏览 /etc/hosts",
		ActivityTarget: "/etc/hosts",
		Phase:          "browsing",
	})

	card := app.cardByID(sessionID, "proc-1")
	if card == nil {
		t.Fatal("expected process card to be created")
	}
	if card.Status != "inProgress" {
		t.Fatalf("expected inProgress card, got %#v", card)
	}
	if card.Text != "现在浏览 /etc/hosts" {
		t.Fatalf("expected label to be used as card text, got %q", card.Text)
	}

	app.projectToolLifecycleCards(sessionID, ToolLifecycleEvent{
		Type:      ToolLifecycleEventCompleted,
		SessionID: sessionID,
		CardID:    "proc-1",
		Message:   "已浏览 /etc/hosts",
	})

	card = app.cardByID(sessionID, "proc-1")
	if card == nil || card.Status != "completed" {
		t.Fatalf("expected completed card, got %#v", card)
	}
	if card.Text != "已浏览 /etc/hosts" {
		t.Fatalf("expected completed text to be stored, got %q", card.Text)
	}
}

func TestToolProjectionCardsMarksFailures(t *testing.T) {
	app := &App{store: store.New()}
	sessionID := "sess-card-failure"

	app.projectToolLifecycleCards(sessionID, ToolLifecycleEvent{
		Type:      ToolLifecycleEventStarted,
		SessionID: sessionID,
		ToolName:  "write_file",
		CardID:    "proc-2",
	})
	app.projectToolLifecycleCards(sessionID, ToolLifecycleEvent{
		Type:      ToolLifecycleEventFailed,
		SessionID: sessionID,
		CardID:    "proc-2",
		Error:     "permission denied",
	})

	card := app.cardByID(sessionID, "proc-2")
	if card == nil || card.Status != "failed" {
		t.Fatalf("expected failed card, got %#v", card)
	}
	if card.Text != "permission denied" {
		t.Fatalf("expected failure text to be stored, got %q", card.Text)
	}
}

func TestToolProjectionCardsUpdatesProcessCardOnProgress(t *testing.T) {
	app := newTestApp(t)
	sessionID := "sess-card-progress"

	app.projectToolLifecycleCards(sessionID, ToolLifecycleEvent{
		Type:      ToolLifecycleEventStarted,
		SessionID: sessionID,
		ToolName:  "read_file",
		CardID:    "proc-progress",
		Label:     "开始读取日志",
		Phase:     "executing",
	})
	app.projectToolLifecycleCards(sessionID, ToolLifecycleEvent{
		Type:      ToolLifecycleEventProgress,
		SessionID: sessionID,
		ToolName:  "read_file",
		CardID:    "proc-progress",
		Message:   "已处理 1/3 个分片",
	})

	card := app.cardByID(sessionID, "proc-progress")
	if card == nil {
		t.Fatal("expected process card to exist")
	}
	if card.Status != "inProgress" {
		t.Fatalf("expected progress to keep inProgress status, got %#v", card)
	}
	if card.Text != "已处理 1/3 个分片" {
		t.Fatalf("expected progress text to be updated, got %#v", card)
	}
}

func TestToolProjectionCardsProjectsFinalCardFromLifecyclePayload(t *testing.T) {
	app := &App{store: store.New()}
	sessionID := "sess-card-final"

	app.projectToolLifecycleCards(sessionID, ToolLifecycleEvent{
		Type:      ToolLifecycleEventStarted,
		SessionID: sessionID,
		ToolName:  "write_file",
		CardID:    "process-card-1",
		Label:     "现在修改 /etc/app.conf",
		Phase:     "executing",
	})

	app.projectToolLifecycleCards(sessionID, ToolLifecycleEvent{
		Type:      ToolLifecycleEventCompleted,
		SessionID: sessionID,
		ToolName:  "write_file",
		CardID:    "process-card-1",
		Message:   "已修改 /etc/app.conf",
		Payload: map[string]any{
			"display": map[string]any{
				"summary":  "已修改远程文件 /etc/app.conf",
				"activity": "/etc/app.conf",
				"blocks": []map[string]any{
					{
						"kind":  ToolDisplayBlockFilePreview,
						"title": "Preview",
						"text":  "now updated",
						"items": []map[string]any{
							{"path": "/etc/app.conf"},
						},
					},
				},
				"finalCard": map[string]any{
					"cardId":   "item-card-1",
					"cardType": "FileChangeCard",
					"title":    "Remote file change",
					"text":     "已修改远程文件 /etc/app.conf",
					"status":   "completed",
					"hostId":   "linux-01",
					"changes": []any{
						map[string]any{
							"path": "/etc/app.conf",
							"kind": "update",
							"diff": "@@",
						},
					},
					"detail": map[string]any{
						"filePath": "/etc/app.conf",
					},
					"createdAt": "2026-04-17T00:00:00Z",
					"updatedAt": "2026-04-17T00:00:01Z",
				},
			},
		},
	})

	process := app.cardByID(sessionID, "process-card-1")
	if process == nil || process.Status != "completed" {
		t.Fatalf("expected completed process card, got %#v", process)
	}

	card := app.cardByID(sessionID, "item-card-1")
	if card == nil {
		t.Fatal("expected final file change card to be projected")
	}
	if card.Type != "FileChangeCard" || card.Status != "completed" {
		t.Fatalf("unexpected final card: %#v", card)
	}
	if card.HostID != "linux-01" {
		t.Fatalf("expected host id linux-01, got %#v", card)
	}
	if len(card.Changes) != 1 || card.Changes[0].Path != "/etc/app.conf" {
		t.Fatalf("expected change path to be projected, got %#v", card.Changes)
	}
	if got := getStringAny(card.Detail, "filePath"); got != "/etc/app.conf" {
		t.Fatalf("expected detail filePath to be projected, got %#v", card.Detail)
	}
	display, ok := card.Detail["display"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured display to be preserved in detail, got %#v", card.Detail)
	}
	if got := getStringAny(display, "summary"); got != "已修改远程文件 /etc/app.conf" {
		t.Fatalf("expected display summary to be projected, got %#v", display)
	}
	if got := getStringAny(display, "activity"); got != "/etc/app.conf" {
		t.Fatalf("expected display activity to be projected, got %#v", display)
	}
	blocks, ok := display["blocks"].([]map[string]any)
	if !ok || len(blocks) != 1 {
		t.Fatalf("expected display blocks to be projected, got %#v", display["blocks"])
	}
	if got := getStringAny(blocks[0], "kind"); got != ToolDisplayBlockFilePreview {
		t.Fatalf("expected block kind to be preserved, got %#v", blocks[0])
	}
	finalCard, ok := display["finalCard"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested finalCard to be preserved, got %#v", display)
	}
	if got := getStringAny(finalCard, "cardId"); got != "item-card-1" {
		t.Fatalf("expected nested finalCard to retain card id, got %#v", finalCard)
	}
}

func TestToolProjectionCardsSkipsProjectionWhenDisplayRequestsSkip(t *testing.T) {
	app := &App{store: store.New()}
	sessionID := "sess-card-skip-display"

	display := map[string]any{
		"skipCards": true,
		"finalCard": map[string]any{
			"cardId":   "item-card-skip",
			"cardType": "FileChangeCard",
			"title":    "Skipped file change",
			"text":     "should not appear",
		},
	}

	app.projectToolLifecycleCards(sessionID, ToolLifecycleEvent{
		Type:      ToolLifecycleEventStarted,
		SessionID: sessionID,
		ToolName:  "write_file",
		CardID:    "process-card-skip",
		Payload: map[string]any{
			"display": display,
		},
	})
	app.projectToolLifecycleCards(sessionID, ToolLifecycleEvent{
		Type:      ToolLifecycleEventCompleted,
		SessionID: sessionID,
		ToolName:  "write_file",
		CardID:    "process-card-skip",
		Payload: map[string]any{
			"display": display,
		},
	})

	if card := app.cardByID(sessionID, "process-card-skip"); card != nil {
		t.Fatalf("expected process card to be skipped, got %#v", card)
	}
	if card := app.cardByID(sessionID, "item-card-skip"); card != nil {
		t.Fatalf("expected final card to be skipped, got %#v", card)
	}
}

func TestToolProjectionCardsSkipsProjectionWhenRequested(t *testing.T) {
	app := &App{store: store.New()}
	sessionID := "sess-card-skip"
	app.store.UpsertCard(sessionID, model.Card{
		ID:        "toolcmd-readonly",
		Type:      "CommandCard",
		Status:    "completed",
		Output:    "load average: 0.20 0.15 0.10\n",
		CreatedAt: "2026-04-17T00:00:00Z",
		UpdatedAt: "2026-04-17T00:00:01Z",
	})

	app.projectToolLifecycleCards(sessionID, ToolLifecycleEvent{
		Type:      ToolLifecycleEventStarted,
		SessionID: sessionID,
		ToolName:  "execute_readonly_query",
		CardID:    "toolcmd-readonly",
		Payload: map[string]any{
			"skipCardProjection": true,
		},
	})
	app.projectToolLifecycleCards(sessionID, ToolLifecycleEvent{
		Type:      ToolLifecycleEventCompleted,
		SessionID: sessionID,
		ToolName:  "execute_readonly_query",
		CardID:    "toolcmd-readonly",
		Message:   "已执行只读命令：uptime",
		Payload: map[string]any{
			"skipCardProjection": true,
		},
	})

	card := app.cardByID(sessionID, "toolcmd-readonly")
	if card == nil {
		t.Fatal("expected command card to remain")
	}
	if card.Type != "CommandCard" {
		t.Fatalf("expected command card type to remain unchanged, got %#v", card)
	}
	if card.Output != "load average: 0.20 0.15 0.10\n" {
		t.Fatalf("expected command output to remain unchanged, got %#v", card)
	}
}
