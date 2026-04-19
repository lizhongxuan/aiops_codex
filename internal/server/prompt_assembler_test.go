package server

import (
	"strings"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/model"
)

func TestBuildEffectivePromptOmitsEmptySectionsAndPreservesOrder(t *testing.T) {
	sections := []PromptSection{
		{Name: "A", Content: "alpha"},
		{Name: "B", Content: ""},
		{Name: "C", Content: "charlie"},
	}

	rendered := buildEffectivePrompt(sections)

	if strings.Contains(rendered, "[B]") {
		t.Fatal("expected empty section to be omitted")
	}
	if !strings.Contains(rendered, "[A]\nalpha") {
		t.Fatal("expected section A to be rendered")
	}
	if !strings.Contains(rendered, "[C]\ncharlie") {
		t.Fatal("expected section C to be rendered")
	}
	if strings.Index(rendered, "[A]") > strings.Index(rendered, "[C]") {
		t.Fatal("expected prompt section order to be preserved")
	}
}

func TestDynamicPromptSectionsIncludePolicyAndHostAttachments(t *testing.T) {
	app := newTestApp(t)
	sessionID := "prompt-assembler-dynamic"
	app.store.EnsureSession(sessionID)

	policy := modelTurnPolicyForTest()
	sections := app.dynamicPromptSections(sessionID, "server-local", policy)
	if len(sections) == 0 {
		t.Fatal("expected dynamic prompt sections")
	}

	var hasHostContext bool
	var hasRuntimePolicy bool
	for _, section := range sections {
		switch section.Name {
		case "HostContext":
			hasHostContext = true
		case "RuntimePolicy":
			hasRuntimePolicy = true
			if !strings.Contains(section.Content, "lane=readonly") {
				t.Fatalf("expected runtime policy to include lane, got %q", section.Content)
			}
		}
		if section.Name == "" {
			t.Fatal("dynamic prompt section should have a name")
		}
	}

	if !hasHostContext {
		t.Fatal("expected HostContext section")
	}
	if !hasRuntimePolicy {
		t.Fatal("expected RuntimePolicy section")
	}
}

func modelTurnPolicyForTest() model.TurnPolicy {
	return model.TurnPolicy{
		IntentClass:           "factual",
		Lane:                  "readonly",
		RequiredTools:         []string{"web_search"},
		RequiredEvidenceKinds: []string{"web_search"},
		FinalGateStatus:       "pending",
	}
}
