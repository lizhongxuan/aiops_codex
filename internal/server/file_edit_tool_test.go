package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/agentloop"
	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func TestRegisterApplyPatchToolUsesPromptRegistryDescription(t *testing.T) {
	reg := agentloop.NewToolRegistry()
	agentloop.RegisterApplyPatchTool(reg)

	entry, ok := reg.Get("apply_patch")
	if !ok || entry == nil {
		t.Fatal("expected apply_patch tool to be registered")
	}
	if got, want := entry.Description, toolPromptDescription("apply_patch"); got != want {
		t.Fatalf("expected apply_patch description to match prompt registry\n got: %q\nwant: %q", got, want)
	}
}

func TestWorkspaceOnApplyPatchLifecycleProjectsStructuredFileChangeCard(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-workspace-apply-patch"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetSelectedHost(sessionID, model.ServerLocalHostID)

	subscriber := &collectingToolSubscriber{}
	app.toolEventBus.Subscribe(subscriber)

	args := map[string]interface{}{
		"patch": `diff --git a/demo.txt b/demo.txt
--- a/demo.txt
+++ b/demo.txt
@@ -1,2 +1,2 @@
-hello
+hello world
 keep
`,
	}

	app.OnToolStart(context.Background(), &agentloop.Session{ID: sessionID}, "apply_patch", args)
	app.OnToolComplete(context.Background(), &agentloop.Session{ID: sessionID}, "apply_patch", args, "Patch applied successfully (1 file(s) changed):\nmodify demo.txt\n", nil)

	if len(subscriber.events) != 2 {
		t.Fatalf("expected apply_patch lifecycle to emit two events, got %d", len(subscriber.events))
	}
	if subscriber.events[1].Type != ToolLifecycleEventCompleted {
		t.Fatalf("expected completed event, got %#v", subscriber.events[1])
	}
	display, ok := subscriber.events[1].Payload["display"].(map[string]any)
	if !ok {
		t.Fatalf("expected completed event display payload, got %#v", subscriber.events[1].Payload)
	}
	if got := getStringAny(display, "summary"); got == "" || !strings.Contains(got, "demo.txt") {
		t.Fatalf("expected display summary to mention patched file, got %#v", display)
	}

	processCardID := subscriber.events[0].CardID
	process := app.cardByID(sessionID, processCardID)
	if process == nil || process.Status != "completed" {
		t.Fatalf("expected completed process card, got %#v", process)
	}
	processDisplay := toolProjectionDisplayMapFromDetail(process.Detail)
	blocks, ok := processDisplay["blocks"].([]map[string]any)
	if !ok || len(blocks) < 2 {
		t.Fatalf("expected structured process display blocks, got %#v", processDisplay["blocks"])
	}
	if getStringAny(blocks[0], "kind") != ToolDisplayBlockResultStats {
		t.Fatalf("expected first process block result_stats, got %#v", blocks)
	}
	if getStringAny(blocks[1], "kind") != ToolDisplayBlockFileDiffSummary {
		t.Fatalf("expected second process block file_diff_summary, got %#v", blocks)
	}

	card := app.cardByID(sessionID, applyPatchResultCardID(processCardID))
	if card == nil || card.Type != "FileChangeCard" || card.Status != "completed" {
		t.Fatalf("expected completed file change card, got %#v", card)
	}
	if len(card.Changes) != 1 || card.Changes[0].Path != "demo.txt" || card.Changes[0].Kind != "update" {
		t.Fatalf("unexpected patch card changes: %#v", card.Changes)
	}
	evidenceID := getStringAny(card.Detail, "evidenceId")
	if evidenceID == "" {
		t.Fatalf("expected patch card evidenceId, got %#v", card.Detail)
	}
	if item := app.store.Item(sessionID, evidenceID); item == nil {
		t.Fatalf("expected evidence artifact %q", evidenceID)
	}
	record := findVerificationRecord(app.snapshot(sessionID).VerificationRecords, "verify-"+card.ID)
	if record == nil {
		t.Fatalf("expected verification record for apply_patch, got %#v", app.snapshot(sessionID).VerificationRecords)
	}
}

func TestWorkspaceOnApplyPatchFailureProjectsStructuredWarning(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-workspace-apply-patch-failed"
	app.store.EnsureSessionWithMeta(sessionID, model.SessionMeta{
		Kind:               model.SessionKindWorkspace,
		Visible:            true,
		WorkspaceSessionID: sessionID,
		RuntimePreset:      model.SessionRuntimePresetWorkspace,
	})
	app.store.SetSelectedHost(sessionID, model.ServerLocalHostID)

	subscriber := &collectingToolSubscriber{}
	app.toolEventBus.Subscribe(subscriber)

	args := map[string]interface{}{
		"patch": `diff --git a/demo.txt b/demo.txt
--- a/demo.txt
+++ b/demo.txt
@@ -1 +1 @@
-hello
+hello world
`,
	}

	app.OnToolStart(context.Background(), &agentloop.Session{ID: sessionID}, "apply_patch", args)
	app.OnToolComplete(context.Background(), &agentloop.Session{ID: sessionID}, "apply_patch", args, "", errors.New("failed to apply patch: permission denied"))

	if len(subscriber.events) != 2 {
		t.Fatalf("expected apply_patch failure lifecycle to emit two events, got %d", len(subscriber.events))
	}
	if subscriber.events[1].Type != ToolLifecycleEventFailed {
		t.Fatalf("expected failed event, got %#v", subscriber.events[1])
	}

	processCardID := subscriber.events[0].CardID
	process := app.cardByID(sessionID, processCardID)
	if process == nil || process.Status != "failed" {
		t.Fatalf("expected failed process card, got %#v", process)
	}
	processDisplay := toolProjectionDisplayMapFromDetail(process.Detail)
	blocks, ok := processDisplay["blocks"].([]map[string]any)
	if !ok || len(blocks) < 3 {
		t.Fatalf("expected structured failed display blocks, got %#v", processDisplay["blocks"])
	}
	if getStringAny(blocks[1], "kind") != ToolDisplayBlockWarning {
		t.Fatalf("expected warning block on failure display, got %#v", blocks)
	}
	if getStringAny(blocks[2], "kind") != ToolDisplayBlockFileDiffSummary {
		t.Fatalf("expected file_diff_summary block on failure display, got %#v", blocks)
	}

	card := app.cardByID(sessionID, applyPatchResultCardID(processCardID))
	if card == nil || card.Type != "FileChangeCard" || card.Status != "failed" {
		t.Fatalf("expected failed file change card, got %#v", card)
	}
	cardDisplay := toolProjectionDisplayMapFromDetail(card.Detail)
	if got := getStringAny(cardDisplay, "summary"); got == "" || !strings.Contains(got, "demo.txt") {
		t.Fatalf("expected failed patch summary to mention file, got %#v", cardDisplay)
	}
	cardBlocks, ok := cardDisplay["blocks"].([]map[string]any)
	if !ok || len(cardBlocks) < 3 {
		t.Fatalf("expected failed patch display blocks, got %#v", cardDisplay["blocks"])
	}
	if getStringAny(cardBlocks[1], "kind") != ToolDisplayBlockWarning {
		t.Fatalf("expected warning block on failed patch card, got %#v", cardBlocks)
	}
}
