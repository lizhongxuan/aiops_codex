// Package skills provides a generic skills management system inspired by
// Codex's SKILL.md-based skill discovery. Skills are reusable agent capabilities
// that can be explicitly triggered ($skill-name) or implicitly matched by
// description.
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Scope defines where a skill is loaded from.
type Scope string

const (
	ScopeUser   Scope = "user"   // ~/.aiops_codex/skills/
	ScopeRepo   Scope = "repo"   // .aiops_codex/skills/
	ScopeSystem Scope = "system" // built-in
)

// SkillMetadata describes a skill loaded from a SKILL.md file.
type SkillMetadata struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description" json:"description"`
	Triggers    []string `yaml:"triggers" json:"triggers,omitempty"`
	Scope       Scope    `yaml:"-" json:"scope"`
	Path        string   `yaml:"-" json:"path"`
	// Instructions is the body of the SKILL.md after the front-matter.
	Instructions string `yaml:"-" json:"instructions"`
	// Scripts lists executable scripts bundled with the skill.
	Scripts []string `yaml:"-" json:"scripts,omitempty"`
	// References lists additional files the skill references.
	References []string `yaml:"references" json:"references,omitempty"`
	// Enabled controls whether the skill is active.
	Enabled bool `yaml:"enabled" json:"enabled"`
}

// Manager discovers, loads, and manages skills from multiple roots.
type Manager struct {
	mu     sync.RWMutex
	skills map[string]*SkillMetadata
	roots  []skillRoot
}

type skillRoot struct {
	path  string
	scope Scope
}

// NewManager creates a new skills Manager.
func NewManager() *Manager {
	return &Manager{
		skills: make(map[string]*SkillMetadata),
	}
}

// AddRoot adds a skill discovery root directory.
func (m *Manager) AddRoot(path string, scope Scope) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.roots = append(m.roots, skillRoot{path: path, scope: scope})
}

// Discover scans all roots for SKILL.md files and loads them.
func (m *Manager) Discover() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.skills = make(map[string]*SkillMetadata)
	for _, root := range m.roots {
		if err := m.discoverRoot(root); err != nil {
			return fmt.Errorf("discover skills in %s: %w", root.path, err)
		}
	}
	return nil
}

func (m *Manager) discoverRoot(root skillRoot) error {
	entries, err := os.ReadDir(root.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(root.path, entry.Name())
		skillFile := filepath.Join(skillDir, "SKILL.md")
		data, err := os.ReadFile(skillFile)
		if err != nil {
			continue // No SKILL.md, skip.
		}

		skill, err := parseSkillFile(string(data))
		if err != nil {
			continue
		}
		if skill.Name == "" {
			skill.Name = entry.Name()
		}
		skill.Scope = root.scope
		skill.Path = skillDir
		skill.Enabled = true

		// Discover scripts.
		scriptsDir := filepath.Join(skillDir, "scripts")
		if scriptEntries, err := os.ReadDir(scriptsDir); err == nil {
			for _, se := range scriptEntries {
				if !se.IsDir() {
					skill.Scripts = append(skill.Scripts, filepath.Join(scriptsDir, se.Name()))
				}
			}
		}

		m.skills[skill.Name] = skill
	}
	return nil
}

// parseSkillFile parses a SKILL.md file with optional YAML front-matter.
func parseSkillFile(content string) (*SkillMetadata, error) {
	skill := &SkillMetadata{}

	// Check for YAML front-matter (--- ... ---).
	if strings.HasPrefix(content, "---\n") {
		end := strings.Index(content[4:], "\n---")
		if end >= 0 {
			frontMatter := content[4 : 4+end]
			if err := yaml.Unmarshal([]byte(frontMatter), skill); err != nil {
				return nil, fmt.Errorf("parse skill front-matter: %w", err)
			}
			skill.Instructions = strings.TrimSpace(content[4+end+4:])
		} else {
			skill.Instructions = content
		}
	} else {
		skill.Instructions = content
	}

	return skill, nil
}

// Get returns a skill by name.
func (m *Manager) Get(name string) (*SkillMetadata, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.skills[name]
	return s, ok
}

// All returns all loaded skills.
func (m *Manager) All() []*SkillMetadata {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*SkillMetadata, 0, len(m.skills))
	for _, s := range m.skills {
		out = append(out, s)
	}
	return out
}

// Enabled returns only enabled skills.
func (m *Manager) Enabled() []*SkillMetadata {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*SkillMetadata
	for _, s := range m.skills {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out
}

// MatchExplicit checks if the user input contains an explicit skill trigger
// ($skill-name) and returns the matching skill.
func (m *Manager) MatchExplicit(input string) *SkillMetadata {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.skills {
		if !s.Enabled {
			continue
		}
		trigger := "$" + s.Name
		if strings.Contains(input, trigger) {
			return s
		}
	}
	return nil
}

// MatchImplicit checks if the user input matches any skill's triggers or
// description keywords.
func (m *Manager) MatchImplicit(input string) *SkillMetadata {
	m.mu.RLock()
	defer m.mu.RUnlock()
	lower := strings.ToLower(input)
	for _, s := range m.skills {
		if !s.Enabled {
			continue
		}
		for _, trigger := range s.Triggers {
			if strings.Contains(lower, strings.ToLower(trigger)) {
				return s
			}
		}
	}
	return nil
}

// InjectSkillContext returns the skill instructions to inject into the system
// prompt when a skill is matched.
func (m *Manager) InjectSkillContext(skill *SkillMetadata) string {
	if skill == nil || skill.Instructions == "" {
		return ""
	}
	return fmt.Sprintf("<skill name=%q scope=%q>\n%s\n</skill>", skill.Name, skill.Scope, skill.Instructions)
}

// Enable enables a skill by name.
func (m *Manager) Enable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.skills[name]
	if !ok {
		return fmt.Errorf("skill %q not found", name)
	}
	s.Enabled = true
	return nil
}

// Disable disables a skill by name.
func (m *Manager) Disable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.skills[name]
	if !ok {
		return fmt.Errorf("skill %q not found", name)
	}
	s.Enabled = false
	return nil
}
