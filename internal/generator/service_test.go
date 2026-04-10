package generator

import (
	"testing"
)

func TestGenerateSkillsFromCoroot_BatchGeneration(t *testing.T) {
	svc := NewGeneratorService()
	tools := DefaultCorootTools()

	skills, err := svc.GenerateSkillsFromCoroot(tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) == 0 {
		t.Fatal("expected at least one skill")
	}
	// All default tools have names, so all should pass lint.
	if len(skills) != len(tools) {
		t.Errorf("expected %d skills, got %d", len(tools), len(skills))
	}
	for _, s := range skills {
		if s.Source != "coroot-generated" {
			t.Errorf("skill %q: expected source coroot-generated, got %q", s.Name, s.Source)
		}
		if s.Status != "draft" {
			t.Errorf("skill %q: expected status draft, got %q", s.Name, s.Status)
		}
	}
}

func TestGenerateSkillsFromCoroot_EmptyList(t *testing.T) {
	svc := NewGeneratorService()
	_, err := svc.GenerateSkillsFromCoroot(nil)
	if err == nil {
		t.Fatal("expected error for empty tools list")
	}
}

func TestGenerateSkillsFromCoroot_FiltersLintErrors(t *testing.T) {
	svc := NewGeneratorService()
	// A tool with empty name should produce a skill that fails lint (name required).
	tools := []CorootToolMeta{
		{Name: "ValidTool", Description: "A valid tool"},
		{Name: "", Description: "Missing name tool"},
	}
	skills, err := svc.GenerateSkillsFromCoroot(tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the valid tool should survive lint filtering.
	if len(skills) != 1 {
		t.Errorf("expected 1 skill after lint filtering, got %d", len(skills))
	}
	if len(skills) > 0 && skills[0].Name != "ValidTool" {
		t.Errorf("expected surviving skill to be ValidTool, got %q", skills[0].Name)
	}
}

func TestGenerateCardFromCoroot_Success(t *testing.T) {
	svc := NewGeneratorService()
	schema := map[string]any{
		"properties": map[string]any{
			"serviceId": map[string]any{"type": "string"},
		},
	}
	card, err := svc.GenerateCardFromCoroot("web-api", schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if card == nil {
		t.Fatal("expected non-nil card")
	}
	if card.Status != "draft" {
		t.Errorf("expected status draft, got %q", card.Status)
	}
	if card.Kind != "monitor_bundle" && card.Kind != "remediation_bundle" {
		t.Errorf("expected a bundle kind, got %q", card.Kind)
	}
}

func TestGenerateCardFromCoroot_EmptyServiceType(t *testing.T) {
	svc := NewGeneratorService()
	_, err := svc.GenerateCardFromCoroot("", nil)
	if err == nil {
		t.Fatal("expected error for empty serviceType")
	}
}

func TestHasErrorIssues(t *testing.T) {
	noErrors := []LintIssue{
		{Field: "desc", Level: "warning", Message: "missing"},
	}
	if hasErrorIssues(noErrors) {
		t.Error("expected no error issues")
	}

	withErrors := []LintIssue{
		{Field: "name", Level: "error", Message: "required"},
	}
	if !hasErrorIssues(withErrors) {
		t.Error("expected error issues")
	}

	if hasErrorIssues(nil) {
		t.Error("nil slice should have no error issues")
	}
}
