package skills

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/filewatcher"
)

// SkillDependency describes a prerequisite for a skill.
type SkillDependency struct {
	Type     string `yaml:"type" json:"type"`         // "env", "tool", "skill"
	Name     string `yaml:"name" json:"name"`
	Required bool   `yaml:"required" json:"required"`
}

// SkillInvocation records a skill invocation event.
type SkillInvocation struct {
	SkillName   string    `json:"skill_name"`
	TriggerType string    `json:"trigger_type"` // "explicit" or "implicit"
	Timestamp   time.Time `json:"timestamp"`
}

// SkillAnalytics tracks skill usage data.
type SkillAnalytics struct {
	mu          sync.Mutex
	invocations []SkillInvocation
}

// NewSkillAnalytics creates a new SkillAnalytics instance.
func NewSkillAnalytics() *SkillAnalytics {
	return &SkillAnalytics{}
}

// RecordInvocation logs a skill invocation event.
func (a *SkillAnalytics) RecordInvocation(inv SkillInvocation) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.invocations = append(a.invocations, inv)
}

// GetAnalytics returns all recorded skill invocation events.
func (a *SkillAnalytics) GetAnalytics() []SkillInvocation {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]SkillInvocation, len(a.invocations))
	copy(out, a.invocations)
	return out
}

// Clear removes all recorded analytics.
func (a *SkillAnalytics) Clear() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.invocations = nil
}

// ManagerV2 extends the base Manager with hot-reload, dependency resolution,
// enhanced prompt injection, and analytics.
type ManagerV2 struct {
	*Manager
	watcher   *filewatcher.Watcher
	analytics *SkillAnalytics
}

// NewManagerV2 creates a new ManagerV2 with analytics support.
func NewManagerV2() *ManagerV2 {
	return &ManagerV2{
		Manager:   NewManager(),
		analytics: NewSkillAnalytics(),
	}
}

// Analytics returns the skill analytics tracker.
func (m *ManagerV2) Analytics() *SkillAnalytics {
	return m.analytics
}

// StartWatching begins monitoring skill root directories for changes.
// When SKILL.md files are created, modified, or deleted, skills are re-discovered.
func (m *ManagerV2) StartWatching() error {
	m.mu.RLock()
	roots := make([]string, len(m.roots))
	for i, r := range m.roots {
		roots[i] = r.path
	}
	m.mu.RUnlock()

	if len(roots) == 0 {
		return nil
	}

	// Filter to existing directories
	var existingRoots []string
	for _, root := range roots {
		if _, err := os.Stat(root); err == nil {
			existingRoots = append(existingRoots, root)
		}
	}

	if len(existingRoots) == 0 {
		return nil
	}

	w := filewatcher.NewWatcher(500 * time.Millisecond)
	m.watcher = w

	// Subscribe to file changes
	w.Subscribe(&skillReloader{manager: m})

	return w.Watch(existingRoots...)
}

// StopWatching stops the file watcher.
func (m *ManagerV2) StopWatching() {
	if m.watcher != nil {
		m.watcher.Stop()
		m.watcher = nil
	}
}

// skillReloader implements filewatcher.Subscriber to reload skills on changes.
type skillReloader struct {
	manager *ManagerV2
}

func (r *skillReloader) OnFileChange(event filewatcher.Event) {
	// Only react to SKILL.md changes
	if !strings.HasSuffix(event.Path, "SKILL.md") {
		return
	}
	// Re-discover all skills
	_ = r.manager.Discover()
}

// ResolveDependencies validates skill prerequisites.
// Returns an error listing missing required dependencies.
func (m *ManagerV2) ResolveDependencies(skill *SkillMetadata) error {
	if skill == nil {
		return fmt.Errorf("nil skill")
	}

	deps := ParseDependencies(skill)
	var missing []string

	for _, dep := range deps {
		if !dep.Required {
			continue
		}

		switch dep.Type {
		case "env":
			if os.Getenv(dep.Name) == "" {
				missing = append(missing, fmt.Sprintf("env:%s", dep.Name))
			}
		case "tool":
			// Tool dependencies are validated at runtime by the tool registry
		case "skill":
			if _, ok := m.Get(dep.Name); !ok {
				missing = append(missing, fmt.Sprintf("skill:%s", dep.Name))
			}
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required dependencies for skill %q: %s",
			skill.Name, strings.Join(missing, ", "))
	}
	return nil
}

// ParseDependencies extracts dependency declarations from skill metadata.
// Dependencies are declared in the SKILL.md front-matter under "dependencies".
func ParseDependencies(skill *SkillMetadata) []SkillDependency {
	// Dependencies are stored in the References field as "type:name" format
	// or parsed from front-matter during skill loading.
	var deps []SkillDependency
	for _, ref := range skill.References {
		parts := strings.SplitN(ref, ":", 2)
		if len(parts) == 2 {
			deps = append(deps, SkillDependency{
				Type:     parts[0],
				Name:     parts[1],
				Required: true,
			})
		}
	}
	return deps
}

// InjectSkillContextV2 supports both explicit $skill-name and implicit trigger matching.
// Returns the injected context string and the matched skill (if any).
func (m *ManagerV2) InjectSkillContextV2(input string) (string, *SkillMetadata) {
	// Try explicit match first ($skill-name)
	if skill := m.MatchExplicit(input); skill != nil {
		m.analytics.RecordInvocation(SkillInvocation{
			SkillName:   skill.Name,
			TriggerType: "explicit",
			Timestamp:   time.Now(),
		})
		return m.InjectSkillContext(skill), skill
	}

	// Try implicit trigger matching
	if skill := m.MatchImplicit(input); skill != nil {
		m.analytics.RecordInvocation(SkillInvocation{
			SkillName:   skill.Name,
			TriggerType: "implicit",
			Timestamp:   time.Now(),
		})
		return m.InjectSkillContext(skill), skill
	}

	return "", nil
}
