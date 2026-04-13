package execpolicy

import (
	"fmt"
	"strings"
)

// ConditionalRule supports boolean expression conditions for complex matching.
type ConditionalRule struct {
	Condition   string   `json:"condition"` // Boolean expression (e.g., "env:CI=true")
	Decision    Decision `json:"decision"`
	Description string   `json:"description,omitempty"`
}

// NetworkRule evaluates host, port, and protocol for network access decisions.
type NetworkRule struct {
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	Protocol    string   `json:"protocol"` // "tcp", "udp", "https", "*"
	Decision    Decision `json:"decision"`
	Description string   `json:"description,omitempty"`
}

// CompositeRule supports AND/OR logic for combining conditions.
type CompositeRule struct {
	Operator    string `json:"operator"` // "and", "or"
	Rules       []Rule `json:"rules"`
	Description string `json:"description,omitempty"`
}

// EvaluateConditional evaluates a conditional rule against the current environment.
func EvaluateConditional(rule ConditionalRule, env map[string]string) Decision {
	if evaluateCondition(rule.Condition, env) {
		return rule.Decision
	}
	return ""
}

// evaluateCondition parses and evaluates a simple boolean expression.
// Supported formats:
//   - "env:KEY=VALUE" — checks if environment variable KEY equals VALUE
//   - "env:KEY" — checks if environment variable KEY is set
//   - "!env:KEY" — checks if environment variable KEY is NOT set
func evaluateCondition(condition string, env map[string]string) bool {
	condition = strings.TrimSpace(condition)

	// Negation
	if strings.HasPrefix(condition, "!") {
		return !evaluateCondition(condition[1:], env)
	}

	// Environment variable check
	if strings.HasPrefix(condition, "env:") {
		envExpr := condition[4:]
		if idx := strings.Index(envExpr, "="); idx >= 0 {
			key := envExpr[:idx]
			value := envExpr[idx+1:]
			return env[key] == value
		}
		_, exists := env[key(envExpr)]
		return exists
	}

	return false
}

func key(s string) string { return strings.TrimSpace(s) }

// EvaluateNetworkRule evaluates a network access request against a NetworkRule.
func EvaluateNetworkRule(rule NetworkRule, host string, port int, protocol string) Evaluation {
	if !matchNetworkHost(host, rule.Host) {
		return Evaluation{Decision: "", Reason: "host mismatch"}
	}
	if rule.Port != 0 && rule.Port != port {
		return Evaluation{Decision: "", Reason: "port mismatch"}
	}
	if rule.Protocol != "" && rule.Protocol != "*" && !strings.EqualFold(rule.Protocol, protocol) {
		return Evaluation{Decision: "", Reason: "protocol mismatch"}
	}

	return Evaluation{
		Decision: rule.Decision,
		Reason:   rule.Description,
	}
}

// matchNetworkHost checks if a host matches a network rule host pattern.
func matchNetworkHost(host, pattern string) bool {
	if pattern == "*" {
		return true
	}
	host = strings.ToLower(host)
	pattern = strings.ToLower(pattern)

	if host == pattern {
		return true
	}

	// Wildcard subdomain: *.example.com
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // .example.com
		return strings.HasSuffix(host, suffix)
	}

	return false
}

// EvaluateComposite evaluates a composite rule using AND/OR logic.
func EvaluateComposite(rule CompositeRule, command string) Evaluation {
	switch strings.ToLower(rule.Operator) {
	case "and":
		return evaluateAnd(rule.Rules, command)
	case "or":
		return evaluateOr(rule.Rules, command)
	default:
		return Evaluation{
			Decision: DecisionForbidden,
			Reason:   fmt.Sprintf("unknown composite operator: %s", rule.Operator),
		}
	}
}

func evaluateAnd(rules []Rule, command string) Evaluation {
	for _, r := range rules {
		if r.Kind != RuleKindCommand {
			continue
		}
		if !matchPattern(command, r.Pattern) {
			return Evaluation{Decision: "", Reason: "AND condition not met"}
		}
	}
	if len(rules) > 0 {
		return Evaluation{Decision: rules[0].Decision, Reason: "all AND conditions met"}
	}
	return Evaluation{Decision: "", Reason: "no rules"}
}

func evaluateOr(rules []Rule, command string) Evaluation {
	for _, r := range rules {
		if r.Kind != RuleKindCommand {
			continue
		}
		if matchPattern(command, r.Pattern) {
			return Evaluation{Decision: r.Decision, Reason: r.Description}
		}
	}
	return Evaluation{Decision: "", Reason: "no OR conditions met"}
}

// EnhancedEvaluateNetwork evaluates a network access request with host, port, and protocol.
func (p *Policy) EnhancedEvaluateNetwork(host string, port int, protocol string) Evaluation {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Check amendments first
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

	// Evaluate network rules
	for _, rule := range p.rules {
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
