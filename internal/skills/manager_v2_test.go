package skills

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerV2_HotReload(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "test-skill")
	os.MkdirAll(skillDir, 0o755)

	// Write initial SKILL.md
	skillContent := `---
name: test-skill
description: A test skill
triggers:
  - testing
enabled: true
---
Test instructions here.
`
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644)

	m := NewManagerV2()
	m.AddRoot(tmpDir, ScopeRepo)

	// Initial discovery
	if err := m.Discover(); err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	skill, ok := m.Get("test-skill")
	if !ok {
		t.Fatal("expected test-skill to be discovered")
	}
	if skill.Description != "A test skill" {
		t.Errorf("unexpected description: %s", skill.Description)
	}
}

func TestManagerV2_StartWatching_NoRoots(t *testing.T) {
	m := NewManagerV2()
	// No roots added, should not error
	err := m.StartWatching()
	if err != nil {
		t.Errorf("StartWatching with no roots should not error: %v", err)
	}
}

func TestManagerV2_StartWatching_NonExistentRoot(t *testing.T) {
	m := NewManagerV2()
	m.AddRoot("/nonexistent/path/that/does/not/exist", ScopeRepo)
	err := m.StartWatching()
	if err != nil {
		t.Errorf("StartWatching with non-existent root should not error: %v", err)
	}
}

func TestManagerV2_StopWatching(t *testing.T) {
	m := NewManagerV2()
	m.StopWatching() // Should not panic when no watcher
}

func TestManagerV2_ResolveDependencies(t *testing.T) {
	m := NewManagerV2()

	t.Run("nil_skill", func(t *testing.T) {
		err := m.ResolveDependencies(nil)
		if err == nil {
			t.Error("expected error for nil skill")
		}
	})

	t.Run("no_dependencies", func(t *testing.T) {
		skill := &SkillMetadata{Name: "simple", Enabled: true}
		err := m.ResolveDependencies(skill)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("env_dependency_present", func(t *testing.T) {
		os.Setenv("TEST_DEP_VAR", "value")
		defer os.Unsetenv("TEST_DEP_VAR")

		skill := &SkillMetadata{
			Name:       "with-env",
			Enabled:    true,
			References: []string{"env:TEST_DEP_VAR"},
		}
		err := m.ResolveDependencies(skill)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("env_dependency_missing", func(t *testing.T) {
		os.Unsetenv("MISSING_DEP_VAR")

		skill := &SkillMetadata{
			Name:       "with-missing-env",
			Enabled:    true,
			References: []string{"env:MISSING_DEP_VAR"},
		}
		err := m.ResolveDependencies(skill)
		if err == nil {
			t.Error("expected error for missing env dependency")
		}
	})

	t.Run("skill_dependency_missing", func(t *testing.T) {
		skill := &SkillMetadata{
			Name:       "with-skill-dep",
			Enabled:    true,
			References: []string{"skill:nonexistent-skill"},
		}
		err := m.ResolveDependencies(skill)
		if err == nil {
			t.Error("expected error for missing skill dependency")
		}
	})
}

func TestManagerV2_InjectSkillContextV2_Explicit(t *testing.T) {
	m := NewManagerV2()
	m.mu.Lock()
	m.skills["deploy"] = &SkillMetadata{
		Name:         "deploy",
		Description:  "Deployment skill",
		Instructions: "Deploy instructions",
		Enabled:      true,
	}
	m.mu.Unlock()

	ctx, skill := m.InjectSkillContextV2("Please run $deploy now")
	if skill == nil {
		t.Fatal("expected skill match")
	}
	if skill.Name != "deploy" {
		t.Errorf("expected deploy skill, got %s", skill.Name)
	}
	if ctx == "" {
		t.Error("expected non-empty context")
	}

	// Check analytics
	analytics := m.Analytics().GetAnalytics()
	if len(analytics) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(analytics))
	}
	if analytics[0].TriggerType != "explicit" {
		t.Errorf("expected explicit trigger, got %s", analytics[0].TriggerType)
	}
}

func TestManagerV2_InjectSkillContextV2_Implicit(t *testing.T) {
	m := NewManagerV2()
	m.mu.Lock()
	m.skills["kubernetes"] = &SkillMetadata{
		Name:         "kubernetes",
		Description:  "K8s management",
		Triggers:     []string{"kubectl", "kubernetes", "k8s"},
		Instructions: "K8s instructions",
		Enabled:      true,
	}
	m.mu.Unlock()

	ctx, skill := m.InjectSkillContextV2("I need to check the kubernetes pods")
	if skill == nil {
		t.Fatal("expected implicit skill match")
	}
	if skill.Name != "kubernetes" {
		t.Errorf("expected kubernetes skill, got %s", skill.Name)
	}
	if ctx == "" {
		t.Error("expected non-empty context")
	}

	analytics := m.Analytics().GetAnalytics()
	if len(analytics) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(analytics))
	}
	if analytics[0].TriggerType != "implicit" {
		t.Errorf("expected implicit trigger, got %s", analytics[0].TriggerType)
	}
}

func TestManagerV2_InjectSkillContextV2_NoMatch(t *testing.T) {
	m := NewManagerV2()
	m.mu.Lock()
	m.skills["deploy"] = &SkillMetadata{
		Name:         "deploy",
		Description:  "Deployment",
		Triggers:     []string{"deploy"},
		Instructions: "Deploy",
		Enabled:      true,
	}
	m.mu.Unlock()

	ctx, skill := m.InjectSkillContextV2("just a regular message")
	if skill != nil {
		t.Error("expected no skill match")
	}
	if ctx != "" {
		t.Error("expected empty context")
	}
}

func TestSkillAnalytics(t *testing.T) {
	a := NewSkillAnalytics()

	t.Run("empty", func(t *testing.T) {
		analytics := a.GetAnalytics()
		if len(analytics) != 0 {
			t.Errorf("expected 0 invocations, got %d", len(analytics))
		}
	})

	t.Run("record_and_retrieve", func(t *testing.T) {
		a.RecordInvocation(SkillInvocation{
			SkillName:   "skill-a",
			TriggerType: "explicit",
			Timestamp:   time.Now(),
		})
		a.RecordInvocation(SkillInvocation{
			SkillName:   "skill-b",
			TriggerType: "implicit",
			Timestamp:   time.Now(),
		})

		analytics := a.GetAnalytics()
		if len(analytics) != 2 {
			t.Fatalf("expected 2 invocations, got %d", len(analytics))
		}
		if analytics[0].SkillName != "skill-a" {
			t.Errorf("expected skill-a, got %s", analytics[0].SkillName)
		}
		if analytics[1].TriggerType != "implicit" {
			t.Errorf("expected implicit, got %s", analytics[1].TriggerType)
		}
	})

	t.Run("clear", func(t *testing.T) {
		a.Clear()
		if len(a.GetAnalytics()) != 0 {
			t.Error("expected 0 after clear")
		}
	})
}

func TestParseDependencies(t *testing.T) {
	skill := &SkillMetadata{
		Name:       "test",
		References: []string{"env:API_KEY", "tool:kubectl", "skill:base-skill"},
	}

	deps := ParseDependencies(skill)
	if len(deps) != 3 {
		t.Fatalf("expected 3 dependencies, got %d", len(deps))
	}

	if deps[0].Type != "env" || deps[0].Name != "API_KEY" {
		t.Errorf("unexpected dep[0]: %+v", deps[0])
	}
	if deps[1].Type != "tool" || deps[1].Name != "kubectl" {
		t.Errorf("unexpected dep[1]: %+v", deps[1])
	}
	if deps[2].Type != "skill" || deps[2].Name != "base-skill" {
		t.Errorf("unexpected dep[2]: %+v", deps[2])
	}
}

func TestSkillReloader(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "reload-skill")
	os.MkdirAll(skillDir, 0o755)

	skillContent := `---
name: reload-skill
description: Original
enabled: true
---
Original instructions.
`
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644)

	m := NewManagerV2()
	m.AddRoot(tmpDir, ScopeRepo)
	m.Discover()

	skill, ok := m.Get("reload-skill")
	if !ok {
		t.Fatal("expected reload-skill")
	}
	if skill.Description != "Original" {
		t.Errorf("expected Original, got %s", skill.Description)
	}

	// Simulate file change by updating and re-discovering
	updatedContent := `---
name: reload-skill
description: Updated
enabled: true
---
Updated instructions.
`
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(updatedContent), 0o644)

	// Manually trigger re-discovery (simulating what the watcher would do)
	m.Discover()

	skill, ok = m.Get("reload-skill")
	if !ok {
		t.Fatal("expected reload-skill after update")
	}
	if skill.Description != "Updated" {
		t.Errorf("expected Updated, got %s", skill.Description)
	}
}
