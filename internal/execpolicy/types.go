// Package execpolicy provides a rule-based command execution policy engine
// inspired by Codex's ExecPolicy (Starlark rules). It evaluates commands
// against configurable allow/deny/prompt rules to determine whether execution
// should be auto-approved, require user approval, or be forbidden.
package execpolicy

// Decision is the outcome of evaluating a command against the policy.
type Decision string

const (
	DecisionAllow     Decision = "allow"     // Auto-approve, no user prompt needed.
	DecisionPrompt    Decision = "prompt"    // Requires user approval.
	DecisionForbidden Decision = "forbidden" // Blocked, cannot be executed.
)

// RuleKind classifies what a rule matches against.
type RuleKind string

const (
	RuleKindCommand RuleKind = "command" // Matches shell commands.
	RuleKindPath    RuleKind = "path"    // Matches file paths.
	RuleKindNetwork RuleKind = "network" // Matches network access.
)

// Rule is a single policy rule.
type Rule struct {
	// Kind is what this rule applies to.
	Kind RuleKind `json:"kind" yaml:"kind"`
	// Pattern is a glob or prefix pattern to match against.
	Pattern string `json:"pattern" yaml:"pattern"`
	// Decision is the outcome when this rule matches.
	Decision Decision `json:"decision" yaml:"decision"`
	// Description explains why this rule exists.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// Priority controls evaluation order (higher = evaluated first).
	Priority int `json:"priority,omitempty" yaml:"priority,omitempty"`
}

// Evaluation is the result of evaluating a command/path/network request.
type Evaluation struct {
	Decision    Decision `json:"decision"`
	MatchedRule *Rule    `json:"matched_rule,omitempty"`
	Reason      string   `json:"reason,omitempty"`
}

// Amendment is a runtime modification to the policy (e.g., user approved a
// command pattern during a session).
type Amendment struct {
	Rule      Rule   `json:"rule"`
	SessionID string `json:"session_id,omitempty"`
	Reason    string `json:"reason,omitempty"`
}
