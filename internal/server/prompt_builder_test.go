package server

import (
	"strings"
	"testing"
)

func TestStaticSystemPromptSectionNotEmpty(t *testing.T) {
	s := staticSystemPromptSection()
	if s.Content == "" {
		t.Fatal("static system prompt should not be empty")
	}
	if s.Name != "System" {
		t.Fatalf("expected name System, got %s", s.Name)
	}
	if !strings.Contains(s.Content, "ReAct agent loop") {
		t.Fatalf("expected ReAct guidance in static system section, got %q", s.Content)
	}
}

func TestDeveloperInstructionsSectionCarriesOpsConstraints(t *testing.T) {
	s := developerInstructionsSection()
	if !strings.Contains(s.Content, "审批要求") {
		t.Fatalf("expected approval guidance in developer instructions, got %q", s.Content)
	}
	if !strings.Contains(s.Content, "运维约束") {
		t.Fatalf("expected ops constraints in developer instructions, got %q", s.Content)
	}
}

func TestIntentClarificationSectionContainsPatterns(t *testing.T) {
	s := intentClarificationSection()
	patterns := []string{"能不能", "有没有办法", "可以吗", "会不会", "是否能处理"}
	for _, pattern := range patterns {
		if !strings.Contains(s.Content, pattern) {
			t.Fatalf("expected pattern %q in intent clarification section: %q", pattern, s.Content)
		}
	}
}
