package server

import (
	"context"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func TestRegisterDefaultToolHandlersRegistersUnifiedSkillContextTool(t *testing.T) {
	app := &App{toolHandlerRegistry: NewToolHandlerRegistry()}

	app.registerDefaultToolHandlers()

	desc, unified, ok := app.toolHandlerRegistry.LookupUnified(skillContextToolName)
	if !ok || unified == nil {
		t.Fatalf("expected unified tool %q to be registered", skillContextToolName)
	}
	if !desc.IsReadOnly {
		t.Fatalf("expected %q to remain readonly, got %#v", skillContextToolName, desc)
	}
	if desc.Kind != "unified" {
		t.Fatalf("expected %q descriptor kind unified, got %#v", skillContextToolName, desc)
	}
}

func TestSkillContextUnifiedToolUsesPromptRegistryDescription(t *testing.T) {
	if got := (skillContextUnifiedTool{}).Description(ToolDescriptionContext{}); got != toolPromptDescription(skillContextToolName) {
		t.Fatalf("expected %s prompt description, got %q", skillContextToolName, got)
	}
}

func TestSkillContextUnifiedToolBuildsStructuredDisplayForInjectedSkills(t *testing.T) {
	app := newTestApp(t)
	profile := app.mainAgentProfile()
	profile.Skills = []model.AgentSkill{
		{ID: "ops-triage", Name: "Ops Triage", Description: "Default skill", Enabled: true, ActivationMode: model.AgentSkillActivationDefault},
		{ID: "safe-change-review", Name: "Safe Change Review", Description: "Explicit skill", Enabled: true, ActivationMode: model.AgentSkillActivationExplicit},
		{ID: "host-change-review", Name: "Host Change Review", Description: "Disabled skill", Enabled: false, ActivationMode: model.AgentSkillActivationDisabled},
	}
	app.store.UpsertAgentProfile(profile)
	app.skillDiscoveryFunc = func(context.Context, string) ([]installedSkillMetadata, error) {
		return []installedSkillMetadata{
			{Name: "Ops Triage", Path: "/tmp/ops-triage/SKILL.md", Enabled: true},
			{Name: "Safe Change Review", Path: "/tmp/safe-change-review/SKILL.md", Enabled: true},
			{Name: "Host Change Review", Path: "/tmp/host-change-review/SKILL.md", Enabled: true},
		}, nil
	}

	result, err := app.skillContextUnifiedTool().Call(context.Background(), ToolCallRequest{
		Input: map[string]any{
			"message": "Use Safe Change Review before changing nginx.",
		},
	})
	if err != nil {
		t.Fatalf("load skill context: %v", err)
	}
	if result.DisplayOutput == nil {
		t.Fatal("expected display output")
	}
	if got := result.DisplayOutput.Summary; got == "" || !containsAll(got, "Ops Triage", "Safe Change Review") {
		t.Fatalf("expected summary to include injected skills, got %#v", result.DisplayOutput)
	}
	if len(result.DisplayOutput.Blocks) < 3 {
		t.Fatalf("expected result_stats + kv_list + text blocks, got %#v", result.DisplayOutput.Blocks)
	}
	if result.DisplayOutput.Blocks[0].Kind != ToolDisplayBlockResultStats {
		t.Fatalf("expected first block result_stats, got %#v", result.DisplayOutput.Blocks)
	}
	if result.DisplayOutput.Blocks[1].Kind != ToolDisplayBlockKVList {
		t.Fatalf("expected second block kv_list, got %#v", result.DisplayOutput.Blocks)
	}
	if result.DisplayOutput.Blocks[2].Kind != ToolDisplayBlockText {
		t.Fatalf("expected third block text, got %#v", result.DisplayOutput.Blocks)
	}

	if got := getStringAny(result.StructuredContent, "summary"); got == "" || !containsAll(got, "Ops Triage", "Safe Change Review") {
		t.Fatalf("expected structured summary to include injected skills, got %#v", result.StructuredContent)
	}
	if got, _ := getIntAny(result.StructuredContent, "skillCount"); got != 2 {
		t.Fatalf("expected skillCount=2, got %#v", result.StructuredContent)
	}
	if got, _ := getIntAny(result.StructuredContent, "implicitCount"); got != 1 {
		t.Fatalf("expected implicitCount=1, got %#v", result.StructuredContent)
	}
	if got, _ := getIntAny(result.StructuredContent, "explicitCount"); got != 1 {
		t.Fatalf("expected explicitCount=1, got %#v", result.StructuredContent)
	}
	items, ok := result.StructuredContent["items"].([]map[string]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected two injected skill input items, got %#v", result.StructuredContent["items"])
	}
	skills, ok := result.StructuredContent["skills"].([]map[string]any)
	if !ok || len(skills) != 2 {
		t.Fatalf("expected two matched skills, got %#v", result.StructuredContent["skills"])
	}
	if getStringAny(skills[0], "matchMode") != "implicit_default" {
		t.Fatalf("expected first skill to be implicit default, got %#v", skills[0])
	}
	if getStringAny(skills[1], "matchMode") != "explicit_request" {
		t.Fatalf("expected second skill to be explicit request, got %#v", skills[1])
	}
}

func TestSkillContextUnifiedToolWarnsWhenExplicitSkillHasNoDiscoveredPath(t *testing.T) {
	app := newTestApp(t)
	profile := app.mainAgentProfile()
	profile.Skills = []model.AgentSkill{
		{ID: "safe-change-review", Name: "Safe Change Review", Description: "Explicit skill", Enabled: true, ActivationMode: model.AgentSkillActivationExplicit},
	}
	app.store.UpsertAgentProfile(profile)
	app.skillDiscoveryFunc = func(context.Context, string) ([]installedSkillMetadata, error) {
		return nil, nil
	}

	result, err := app.skillContextUnifiedTool().Call(context.Background(), ToolCallRequest{
		Input: map[string]any{
			"message": "Please use Safe Change Review for this rollout.",
		},
	})
	if err != nil {
		t.Fatalf("load skill context: %v", err)
	}
	if result.DisplayOutput == nil {
		t.Fatal("expected display output")
	}
	if len(result.DisplayOutput.Blocks) == 0 || result.DisplayOutput.Blocks[0].Kind != ToolDisplayBlockWarning {
		t.Fatalf("expected warning-first display when no skill path is available, got %#v", result.DisplayOutput.Blocks)
	}
	if got := getStringAny(result.StructuredContent, "summary"); got == "" || !containsAll(got, "Safe Change Review", "未注入") {
		t.Fatalf("expected warning summary for missing explicit skill, got %#v", result.StructuredContent)
	}
	if got, _ := getIntAny(result.StructuredContent, "skillCount"); got != 0 {
		t.Fatalf("expected no injected skills, got %#v", result.StructuredContent)
	}
	missing, ok := result.StructuredContent["missingSkills"].([]map[string]any)
	if !ok || len(missing) != 1 {
		t.Fatalf("expected one missing skill entry, got %#v", result.StructuredContent["missingSkills"])
	}
	if getStringAny(missing[0], "name") != "Safe Change Review" {
		t.Fatalf("expected missing skill name to be preserved, got %#v", missing[0])
	}
}
