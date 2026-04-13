package execpolicy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Policy holds the complete set of rules and runtime amendments.
type Policy struct {
	mu         sync.RWMutex
	rules      []Rule
	amendments []Amendment
	// Default decision when no rule matches.
	defaultDecision Decision
}

// NewPolicy creates a new Policy with the given default decision.
func NewPolicy(defaultDecision Decision) *Policy {
	return &Policy{
		defaultDecision: defaultDecision,
	}
}

// DefaultPolicy creates a policy with sensible defaults for an ops environment:
// - Common read-only commands are allowed
// - Dangerous commands are forbidden
// - Everything else requires approval
func DefaultPolicy() *Policy {
	p := NewPolicy(DecisionPrompt)

	// Allow common read-only commands.
	readOnlyCommands := []string{
		"ls", "cat", "head", "tail", "grep", "find", "wc", "sort", "uniq",
		"df", "du", "free", "uptime", "uname", "hostname", "whoami", "id",
		"ps", "top", "htop", "netstat", "ss", "ip", "ifconfig", "ping",
		"dig", "nslookup", "curl", "wget", "date", "cal", "env", "printenv",
		"which", "whereis", "file", "stat", "lsof", "mount", "lsblk",
		"journalctl", "systemctl status", "docker ps", "docker logs",
		"kubectl get", "kubectl describe", "kubectl logs",
	}
	for _, cmd := range readOnlyCommands {
		p.AddRule(Rule{
			Kind:        RuleKindCommand,
			Pattern:     cmd,
			Decision:    DecisionAllow,
			Description: "Common read-only command",
			Priority:    10,
		})
	}

	// Forbid dangerous commands.
	dangerousCommands := []string{
		"rm -rf /", "mkfs", "dd if=/dev/zero", ":(){ :|:& };:",
		"chmod -R 777 /", "chown -R", "> /dev/sda",
	}
	for _, cmd := range dangerousCommands {
		p.AddRule(Rule{
			Kind:        RuleKindCommand,
			Pattern:     cmd,
			Decision:    DecisionForbidden,
			Description: "Dangerous command blocked",
			Priority:    100,
		})
	}

	// Protect sensitive paths.
	protectedPaths := []string{
		"/etc/shadow", "/etc/passwd", "/etc/sudoers",
		"~/.ssh/", "/root/.ssh/",
		".git/", ".codex/", ".agents/",
	}
	for _, path := range protectedPaths {
		p.AddRule(Rule{
			Kind:        RuleKindPath,
			Pattern:     path,
			Decision:    DecisionForbidden,
			Description: "Protected path",
			Priority:    90,
		})
	}

	return p
}

// AddRule adds a rule to the policy.
func (p *Policy) AddRule(rule Rule) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rules = append(p.rules, rule)
}

// AddAmendment adds a runtime amendment to the policy.
func (p *Policy) AddAmendment(amendment Amendment) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.amendments = append(p.amendments, amendment)
}

// EvaluateCommand evaluates a shell command against the policy.
func (p *Policy) EvaluateCommand(command string) Evaluation {
	p.mu.RLock()
	defer p.mu.RUnlock()

	command = strings.TrimSpace(command)
	if command == "" {
		return Evaluation{Decision: DecisionForbidden, Reason: "empty command"}
	}

	// Check amendments first (session-scoped overrides).
	for i := len(p.amendments) - 1; i >= 0; i-- {
		a := p.amendments[i]
		if a.Rule.Kind == RuleKindCommand && matchPattern(command, a.Rule.Pattern) {
			return Evaluation{
				Decision:    a.Rule.Decision,
				MatchedRule: &a.Rule,
				Reason:      fmt.Sprintf("session amendment: %s", a.Reason),
			}
		}
	}

	// Evaluate rules sorted by priority (highest first).
	sorted := make([]Rule, len(p.rules))
	copy(sorted, p.rules)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority > sorted[j].Priority
	})

	for _, rule := range sorted {
		if rule.Kind != RuleKindCommand {
			continue
		}
		if matchPattern(command, rule.Pattern) {
			r := rule
			return Evaluation{
				Decision:    rule.Decision,
				MatchedRule: &r,
				Reason:      rule.Description,
			}
		}
	}

	return Evaluation{
		Decision: p.defaultDecision,
		Reason:   "no matching rule, using default policy",
	}
}

// EvaluatePath evaluates a file path against the policy.
func (p *Policy) EvaluatePath(path string) Evaluation {
	p.mu.RLock()
	defer p.mu.RUnlock()

	path = strings.TrimSpace(path)

	sorted := make([]Rule, len(p.rules))
	copy(sorted, p.rules)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority > sorted[j].Priority
	})

	for _, rule := range sorted {
		if rule.Kind != RuleKindPath {
			continue
		}
		if matchPattern(path, rule.Pattern) {
			r := rule
			return Evaluation{
				Decision:    rule.Decision,
				MatchedRule: &r,
				Reason:      rule.Description,
			}
		}
	}

	return Evaluation{
		Decision: p.defaultDecision,
		Reason:   "no matching path rule",
	}
}

// EvaluateNetwork evaluates a network access request.
func (p *Policy) EvaluateNetwork(host string) Evaluation {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for i := len(p.amendments) - 1; i >= 0; i-- {
		a := p.amendments[i]
		if a.Rule.Kind == RuleKindNetwork && matchPattern(host, a.Rule.Pattern) {
			return Evaluation{
				Decision:    a.Rule.Decision,
				MatchedRule: &a.Rule,
				Reason:      fmt.Sprintf("session amendment: %s", a.Reason),
			}
		}
	}

	sorted := make([]Rule, len(p.rules))
	copy(sorted, p.rules)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority > sorted[j].Priority
	})

	for _, rule := range sorted {
		if rule.Kind != RuleKindNetwork {
			continue
		}
		if matchPattern(host, rule.Pattern) {
			r := rule
			return Evaluation{
				Decision:    rule.Decision,
				MatchedRule: &r,
				Reason:      rule.Description,
			}
		}
	}

	return Evaluation{
		Decision: p.defaultDecision,
		Reason:   "no matching network rule",
	}
}

// matchPattern checks if the input matches the pattern.
// Supports prefix matching and simple glob (*).
func matchPattern(input, pattern string) bool {
	input = strings.ToLower(strings.TrimSpace(input))
	pattern = strings.ToLower(strings.TrimSpace(pattern))

	if pattern == "*" {
		return true
	}

	// Glob: pattern ends with *
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(input, prefix)
	}

	// Glob: pattern starts with *
	if strings.HasPrefix(pattern, "*") {
		suffix := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(input, suffix)
	}

	// Prefix match for commands.
	return strings.HasPrefix(input, pattern)
}

// ---------- Persistence ----------

// PolicyFile is the on-disk format for a policy file.
type PolicyFile struct {
	DefaultDecision Decision `json:"default_decision" yaml:"default_decision"`
	Rules           []Rule   `json:"rules" yaml:"rules"`
}

// LoadFromFile loads a policy from a JSON or YAML file.
func LoadFromFile(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var pf PolicyFile
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &pf); err != nil {
			return nil, fmt.Errorf("parse yaml policy: %w", err)
		}
	default:
		if err := json.Unmarshal(data, &pf); err != nil {
			return nil, fmt.Errorf("parse json policy: %w", err)
		}
	}

	p := NewPolicy(pf.DefaultDecision)
	if p.defaultDecision == "" {
		p.defaultDecision = DecisionPrompt
	}
	for _, r := range pf.Rules {
		p.AddRule(r)
	}
	return p, nil
}

// SaveToFile saves the policy to a JSON file.
func (p *Policy) SaveToFile(path string) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	pf := PolicyFile{
		DefaultDecision: p.defaultDecision,
		Rules:           p.rules,
	}
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Rules returns a copy of all rules.
func (p *Policy) Rules() []Rule {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Rule, len(p.rules))
	copy(out, p.rules)
	return out
}

// Amendments returns a copy of all amendments.
func (p *Policy) Amendments() []Amendment {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Amendment, len(p.amendments))
	copy(out, p.amendments)
	return out
}

// ClearAmendments removes all runtime amendments (e.g., on session end).
func (p *Policy) ClearAmendments() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.amendments = nil
}

// ClearSessionAmendments removes amendments for a specific session.
func (p *Policy) ClearSessionAmendments(sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var kept []Amendment
	for _, a := range p.amendments {
		if a.SessionID != sessionID {
			kept = append(kept, a)
		}
	}
	p.amendments = kept
}
