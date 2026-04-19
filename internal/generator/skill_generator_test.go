package generator

import (
	"testing"
)

func TestBuildSkillsFromCorootTools_Empty(t *testing.T) {
	skills := buildSkillsFromCorootTools(nil)
	if len(skills) != 0 {
		t.Fatalf("expected 0 skills for nil input, got %d", len(skills))
	}
	skills = buildSkillsFromCorootTools([]CorootToolMeta{})
	if len(skills) != 0 {
		t.Fatalf("expected 0 skills for empty input, got %d", len(skills))
	}
}

func TestBuildSkillsFromCorootTools_CountMatchesInput(t *testing.T) {
	tools := DefaultCorootTools()
	skills := buildSkillsFromCorootTools(tools)
	if len(skills) != len(tools) {
		t.Fatalf("expected %d skills, got %d", len(tools), len(skills))
	}
}

func TestBuildSkillsFromCorootTools_FieldsPopulated(t *testing.T) {
	tools := []CorootToolMeta{
		{
			Name:        "TestTool",
			Description: "A test tool",
			Category:    "monitoring",
		},
	}
	skills := buildSkillsFromCorootTools(tools)
	s := skills[0]
	if s.Name != "TestTool" {
		t.Errorf("expected Name=TestTool, got %s", s.Name)
	}
	if s.Description != "A test tool" {
		t.Errorf("expected Description='A test tool', got %s", s.Description)
	}
	if s.Source != "coroot-generated" {
		t.Errorf("expected Source=coroot-generated, got %s", s.Source)
	}
	if s.Category != "monitoring" {
		t.Errorf("expected Category=monitoring, got %s", s.Category)
	}
	if s.Status != "draft" {
		t.Errorf("expected Status=draft, got %s", s.Status)
	}
	if s.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestBuildSkillsFromCorootTools_AutoInferCategory(t *testing.T) {
	tools := []CorootToolMeta{
		{Name: "RCAReport", Description: "Root cause analysis report"},
		{Name: "ListServices", Description: "List all services"},
		{Name: "AutoFix", Description: "Remediation action to fix issues"},
	}
	skills := buildSkillsFromCorootTools(tools)

	if skills[0].Category != "diagnostics" {
		t.Errorf("RCAReport: expected diagnostics, got %s", skills[0].Category)
	}
	if skills[1].Category != "monitoring" {
		t.Errorf("ListServices: expected monitoring, got %s", skills[1].Category)
	}
	if skills[2].Category != "remediation" {
		t.Errorf("AutoFix: expected remediation, got %s", skills[2].Category)
	}
}

func TestBuildSkillsFromCorootTools_ExplicitCategoryOverridesInfer(t *testing.T) {
	tools := []CorootToolMeta{
		{Name: "RCAReport", Description: "Root cause analysis", Category: "monitoring"},
	}
	skills := buildSkillsFromCorootTools(tools)
	if skills[0].Category != "monitoring" {
		t.Errorf("expected explicit category monitoring, got %s", skills[0].Category)
	}
}

func TestBuildSkillsFromCorootTools_DependenciesFromSchema(t *testing.T) {
	tools := []CorootToolMeta{
		{
			Name:        "ServiceOverview",
			Description: "Overview for a service",
			InputSchema: map[string]any{
				"properties": map[string]any{
					"serviceId": map[string]any{"type": "string"},
				},
			},
		},
	}
	skills := buildSkillsFromCorootTools(tools)
	found := false
	for _, d := range skills[0].Dependencies {
		if d == "serviceId" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected serviceId in dependencies, got %v", skills[0].Dependencies)
	}
}

func TestInferCorootCategory(t *testing.T) {
	tests := []struct {
		name, desc, want string
	}{
		{"RCAReport", "root cause analysis", "diagnostics"},
		{"IncidentTimeline", "timeline for incident", "diagnostics"},
		{"AlertTriage", "triage alerts", "diagnostics"},
		{"AutoRemediate", "remediation steps", "remediation"},
		{"QuickFix", "fix the issue", "remediation"},
		{"ListServices", "list all services", "monitoring"},
		{"Topology", "service topology graph", "monitoring"},
		{"ServiceMetrics", "query metrics", "monitoring"},
	}
	for _, tc := range tests {
		got := inferCorootCategory(tc.name, tc.desc)
		if got != tc.want {
			t.Errorf("inferCorootCategory(%q, %q) = %q, want %q", tc.name, tc.desc, got, tc.want)
		}
	}
}

func TestDefaultCorootTools_CoverAllEndpoints(t *testing.T) {
	tools := DefaultCorootTools()
	expected := map[string]bool{
		"ListServices":        false,
		"ServiceOverview":     false,
		"ServiceMetrics":      false,
		"ServiceAlerts":       false,
		"Topology":            false,
		"IncidentTimeline":    false,
		"RCAReport":           false,
		"ServiceDependencies": false,
		"HostOverview":        false,
	}
	for _, t2 := range tools {
		if _, ok := expected[t2.Name]; ok {
			expected[t2.Name] = true
		}
	}
	for name, found := range expected {
		if !found {
			t.Errorf("DefaultCorootTools missing endpoint: %s", name)
		}
	}
}

func TestBuildSkillsFromCorootTools_UniqueIDs(t *testing.T) {
	tools := DefaultCorootTools()
	skills := buildSkillsFromCorootTools(tools)
	seen := make(map[string]bool)
	for _, s := range skills {
		if seen[s.ID] {
			t.Errorf("duplicate skill ID: %s", s.ID)
		}
		seen[s.ID] = true
	}
}
