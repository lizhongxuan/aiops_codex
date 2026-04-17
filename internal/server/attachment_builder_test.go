package server

import (
	"strings"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	return newOrchestratorTestApp(t)
}

func TestAttachmentSectionFormatsHeader(t *testing.T) {
	result := attachmentSection("test_header", "content here")
	if !strings.HasPrefix(result, "[test_header]") {
		t.Error("expected header prefix")
	}
	if !strings.Contains(result, "content here") {
		t.Error("expected content")
	}
}

func TestAttachmentSectionEmptyContent(t *testing.T) {
	result := attachmentSection("test", "")
	if result != "" {
		t.Error("empty content should return empty string")
	}
}

func TestPlanModeAttachmentActive(t *testing.T) {
	app := newTestApp(t)
	sessionID := "test-session"
	app.store.EnsureSession(sessionID)

	result := app.planModeAttachment(sessionID, true)
	if !strings.Contains(result, "active: true") {
		t.Error("expected active: true")
	}
	if !strings.Contains(result, "readonly") {
		t.Error("expected readonly constraint")
	}
}

func TestPlanModeAttachmentInactive(t *testing.T) {
	app := newTestApp(t)
	sessionID := "test-session"
	app.store.EnsureSession(sessionID)

	result := app.planModeAttachment(sessionID, false)
	if !strings.Contains(result, "active: false") {
		t.Error("expected active: false")
	}
}

func TestToolSchemaDeltaAttachment(t *testing.T) {
	app := newTestApp(t)
	tools := []string{"ask_user_question", "readonly_host_inspect", "enter_plan_mode"}
	result := app.toolSchemaDeltaAttachment(tools)
	for _, tool := range tools {
		if !strings.Contains(result, tool) {
			t.Errorf("expected tool %q in attachment", tool)
		}
	}
}

func TestToolSchemaDeltaAttachmentEmpty(t *testing.T) {
	app := newTestApp(t)
	result := app.toolSchemaDeltaAttachment(nil)
	if result != "" {
		t.Error("empty tools should return empty string")
	}
}

func TestBuildAllAttachmentsReturnsNonEmpty(t *testing.T) {
	app := newTestApp(t)
	sessionID := "test-session"
	app.store.EnsureSession(sessionID)

	result := app.buildAllAttachments(sessionID, "server-local", "normal", false, []string{"ask_user_question"})
	if len(result) == 0 {
		t.Error("expected at least one attachment")
	}
}

func TestEvidenceSummaryAttachmentIncludesCitationKeys(t *testing.T) {
	app := newTestApp(t)
	sessionID := "test-evidence-attachment"
	app.store.EnsureSession(sessionID)
	app.store.UpsertCard(sessionID, model.Card{
		ID:        "command-card-evidence",
		Type:      "CommandCard",
		Command:   "uptime",
		Output:    "load average: 0.22",
		Status:    "completed",
		CreatedAt: "2026-04-15T10:00:00Z",
		UpdatedAt: "2026-04-15T10:00:01Z",
	})

	result := app.evidenceSummaryAttachment(sessionID)
	if !strings.Contains(result, "[evidence_summary]") {
		t.Fatalf("expected evidence_summary attachment header, got %q", result)
	}
	if !strings.Contains(result, "E-EVIDENCE-COMMAND-CARD-EVIDENCE") {
		t.Fatalf("expected citation key in attachment, got %q", result)
	}
	if !strings.Contains(result, "load average: 0.22") {
		t.Fatalf("expected evidence summary text in attachment, got %q", result)
	}
}
