package server

import (
	"strings"
	"testing"
)

func TestBuildEffectivePromptJoinsSections(t *testing.T) {
	sections := []PromptSection{
		{Name: "A", Content: "content a"},
		{Name: "B", Content: ""},
		{Name: "C", Content: "content c"},
	}
	result := buildEffectivePrompt(sections)
	if !strings.Contains(result, "[A]") {
		t.Error("expected section A header")
	}
	if strings.Contains(result, "[B]") {
		t.Error("empty section B should be omitted")
	}
	if !strings.Contains(result, "[C]") {
		t.Error("expected section C header")
	}
}

func TestStaticSystemPromptSectionNotEmpty(t *testing.T) {
	s := staticSystemPromptSection()
	if s.Content == "" {
		t.Error("static system prompt should not be empty")
	}
	if s.Name != "System" {
		t.Errorf("expected name System, got %s", s.Name)
	}
}

func TestIntentClarificationSectionContainsPatterns(t *testing.T) {
	s := intentClarificationSection()
	patterns := []string{"能不能", "有没有办法", "可以吗", "会不会", "是否能处理"}
	for _, p := range patterns {
		if !strings.Contains(s.Content, p) {
			t.Errorf("expected pattern %q in intent clarification", p)
		}
	}
}

func TestPlanModeSectionActiveVsInactive(t *testing.T) {
	active := planModeSection(true)
	if active.Content == "" {
		t.Error("active plan mode should have content")
	}
	if !strings.Contains(active.Content, "MUST NOT") {
		t.Error("active plan mode should contain MUST NOT constraint")
	}

	inactive := planModeSection(false)
	if inactive.Content != "" {
		t.Error("inactive plan mode should have empty content")
	}
}

func TestToolPromptsSectionContainsAllTools(t *testing.T) {
	s := toolPromptsSection()
	tools := []string{"ask_user_question", "enter_plan_mode", "update_plan", "exit_plan_mode", "orchestrator_dispatch_tasks", "readonly_host_inspect", "request_approval"}
	for _, tool := range tools {
		if !strings.Contains(s.Content, tool) {
			t.Errorf("expected tool %q in tool prompts", tool)
		}
	}
}

func TestRequestApprovalToolPromptNotEmpty(t *testing.T) {
	prompt := requestApprovalToolPrompt()
	if prompt == "" {
		t.Error("request approval prompt should not be empty")
	}
	if !strings.Contains(prompt, "request_approval") {
		t.Error("should contain tool name")
	}
}

func TestExplicitExecutionSectionNotEmpty(t *testing.T) {
	s := explicitExecutionSection()
	if s.Content == "" {
		t.Error("explicit execution section should not be empty")
	}
}

func TestPromptBuilderSnapshotNormalMode(t *testing.T) {
	sections := []PromptSection{
		staticSystemPromptSection(),
		developerInstructionsSection(),
		intentClarificationSection(),
		planModeSection(false),
		toolPromptsSection(),
		explicitExecutionSection(),
	}
	result := buildEffectivePrompt(sections)
	// Normal mode should NOT contain plan mode constraints
	if strings.Contains(result, "Plan mode is active") {
		t.Error("normal mode should not contain plan mode constraints")
	}
	// Should contain ask_user_question
	if !strings.Contains(result, "ask_user_question") {
		t.Error("should contain ask_user_question tool prompt")
	}
	// Should NOT contain request_user_input
	if strings.Contains(result, "request_user_input") {
		t.Error("should not reference request_user_input")
	}
}

func TestPromptBuilderSnapshotPlanMode(t *testing.T) {
	sections := []PromptSection{
		staticSystemPromptSection(),
		developerInstructionsSection(),
		intentClarificationSection(),
		planModeSection(true),
		toolPromptsSection(),
	}
	result := buildEffectivePrompt(sections)
	if !strings.Contains(result, "Plan mode is active") {
		t.Error("plan mode should contain plan mode constraints")
	}
	if !strings.Contains(result, "MUST NOT") {
		t.Error("plan mode should contain MUST NOT constraint")
	}
}
