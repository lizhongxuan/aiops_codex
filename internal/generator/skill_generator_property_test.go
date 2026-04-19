package generator

import (
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"testing/quick"
)

// ---------- Property-Based Tests for Skill Generation Determinism ----------
//
// **Validates: Requirements 5.1, 5.2**

// validCategories are the categories that can appear in CorootToolMeta.
var validCategories = []string{"", "monitoring", "diagnostics", "remediation"}

// sampleNames are representative Coroot tool names used for generation.
var sampleNames = []string{
	"ListServices", "ServiceOverview", "ServiceMetrics", "ServiceAlerts",
	"Topology", "IncidentTimeline", "RCAReport", "ServiceDependencies",
	"HostOverview", "AutoFix", "AlertTriage", "Rollback",
}

// sampleDescs are representative descriptions paired with sampleNames.
var sampleDescs = []string{
	"List all services", "Get service overview", "Query metrics",
	"Get alerts", "Service topology graph", "Incident timeline",
	"Root cause analysis report", "Service dependencies",
	"Host overview", "Remediation action to fix issues",
	"Triage alerts", "Rollback deployment",
}

// toolMetaScenario captures a randomly generated slice of CorootToolMeta inputs.
type toolMetaScenario struct {
	Tools []CorootToolMeta
}

// Generate implements quick.Generator to produce random CorootToolMeta slices.
func (toolMetaScenario) Generate(r *rand.Rand, size int) reflect.Value {
	n := r.Intn(20) // 0..19 tools
	tools := make([]CorootToolMeta, n)
	for i := 0; i < n; i++ {
		nameIdx := r.Intn(len(sampleNames))
		descIdx := r.Intn(len(sampleDescs))
		catIdx := r.Intn(len(validCategories))

		var schema map[string]any
		if r.Intn(2) == 0 {
			// Generate a simple input schema with random property keys.
			props := map[string]any{}
			keys := []string{"serviceId", "hostId", "from", "to", "incidentId", "query"}
			numProps := r.Intn(len(keys)) + 1
			for j := 0; j < numProps; j++ {
				props[keys[r.Intn(len(keys))]] = map[string]any{"type": "string"}
			}
			schema = map[string]any{
				"type":       "object",
				"properties": props,
			}
		}

		tools[i] = CorootToolMeta{
			Name:        sampleNames[nameIdx],
			Description: sampleDescs[descIdx],
			InputSchema: schema,
			Category:    validCategories[catIdx],
		}
	}
	return reflect.ValueOf(toolMetaScenario{Tools: tools})
}

// Property 3: 生成确定性 (Generation Determinism)
// For the same CorootToolMeta input, buildSkillsFromCorootTools always generates
// the same number of Skills and the same categories (in the same order).
//
// **Validates: Requirements 5.1, 5.2**
func TestProperty_SkillGenerationDeterminism(t *testing.T) {
	prop := func(s toolMetaScenario) bool {
		skills1 := buildSkillsFromCorootTools(s.Tools)
		skills2 := buildSkillsFromCorootTools(s.Tools)

		// Same count.
		if len(skills1) != len(skills2) {
			return false
		}

		// Same categories in the same order.
		for i := range skills1 {
			if skills1[i].Category != skills2[i].Category {
				return false
			}
		}

		return true
	}

	cfg := &quick.Config{MaxCount: 500}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("Property_SkillGenerationDeterminism failed: %v", err)
	}
}

// Property 3b: Output count always equals input count.
// buildSkillsFromCorootTools produces exactly one Skill per input tool.
//
// **Validates: Requirements 5.1, 5.2**
func TestProperty_SkillCountMatchesInput(t *testing.T) {
	prop := func(s toolMetaScenario) bool {
		skills := buildSkillsFromCorootTools(s.Tools)
		return len(skills) == len(s.Tools)
	}

	cfg := &quick.Config{MaxCount: 500}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("Property_SkillCountMatchesInput failed: %v", err)
	}
}

// Property 3c: Categories are always from the known set.
// Every generated Skill has a category in {"monitoring", "diagnostics", "remediation"}.
//
// **Validates: Requirements 5.1, 5.2**
func TestProperty_SkillCategoriesInKnownSet(t *testing.T) {
	knownCategories := map[string]bool{
		"monitoring":  true,
		"diagnostics": true,
		"remediation": true,
	}

	prop := func(s toolMetaScenario) bool {
		skills := buildSkillsFromCorootTools(s.Tools)
		for _, sk := range skills {
			if !knownCategories[sk.Category] {
				return false
			}
		}
		return true
	}

	cfg := &quick.Config{MaxCount: 500}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("Property_SkillCategoriesInKnownSet failed: %v", err)
	}
}

// Property 3d: Sorted categories are identical across invocations.
// Even when comparing sorted category lists, two calls produce the same result.
//
// **Validates: Requirements 5.1, 5.2**
func TestProperty_SkillSortedCategoriesDeterministic(t *testing.T) {
	prop := func(s toolMetaScenario) bool {
		skills1 := buildSkillsFromCorootTools(s.Tools)
		skills2 := buildSkillsFromCorootTools(s.Tools)

		cats1 := make([]string, len(skills1))
		cats2 := make([]string, len(skills2))
		for i := range skills1 {
			cats1[i] = skills1[i].Category
		}
		for i := range skills2 {
			cats2[i] = skills2[i].Category
		}
		sort.Strings(cats1)
		sort.Strings(cats2)

		if len(cats1) != len(cats2) {
			return false
		}
		for i := range cats1 {
			if cats1[i] != cats2[i] {
				return false
			}
		}
		return true
	}

	cfg := &quick.Config{MaxCount: 500}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("Property_SkillSortedCategoriesDeterministic failed: %v", err)
	}
}
