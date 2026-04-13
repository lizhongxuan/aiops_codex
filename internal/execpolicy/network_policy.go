package execpolicy

import (
	"fmt"
	"strings"
	"sync"
)

// NetworkPolicy evaluates outbound network requests against a set of rules.
// It supports deny and ask decisions at host and protocol granularity.
type NetworkPolicy struct {
	mu    sync.RWMutex
	Rules []NetworkPolicyRule `json:"rules" yaml:"rules"`
}

// NetworkPolicyRule defines a single network policy rule with host pattern,
// protocol, and decision.
type NetworkPolicyRule struct {
	Host     string   `json:"host"`     // Glob pattern (e.g., "*.example.com", "*")
	Protocol string   `json:"protocol"` // "tcp", "udp", "https", "*"
	Decision Decision `json:"decision"` // deny or ask (prompt)
}

// NewNetworkPolicy creates a new empty NetworkPolicy.
func NewNetworkPolicy() *NetworkPolicy {
	return &NetworkPolicy{}
}

// AddRule adds a rule to the network policy.
func (np *NetworkPolicy) AddRule(rule NetworkPolicyRule) {
	np.mu.Lock()
	defer np.mu.Unlock()
	np.Rules = append(np.Rules, rule)
}

// EvaluateOutbound checks an outbound network request against the policy.
// Returns an Evaluation with deny or ask decision. If no rule matches,
// the request is allowed by default.
func (np *NetworkPolicy) EvaluateOutbound(host, protocol string) Evaluation {
	np.mu.RLock()
	defer np.mu.RUnlock()

	host = strings.TrimSpace(strings.ToLower(host))
	protocol = strings.TrimSpace(strings.ToLower(protocol))

	if host == "" {
		return Evaluation{
			Decision: DecisionForbidden,
			Reason:   "empty host not allowed",
		}
	}

	for _, rule := range np.Rules {
		if !matchNetworkHostPattern(host, rule.Host) {
			continue
		}
		if !matchProtocol(protocol, rule.Protocol) {
			continue
		}

		r := Rule{
			Kind:     RuleKindNetwork,
			Pattern:  rule.Host,
			Decision: rule.Decision,
		}
		reason := fmt.Sprintf("network policy: host=%s protocol=%s decision=%s", rule.Host, rule.Protocol, rule.Decision)
		if rule.Decision == DecisionForbidden {
			reason = fmt.Sprintf("outbound connection to %s via %s denied by network policy", host, protocol)
		}
		return Evaluation{
			Decision:    rule.Decision,
			MatchedRule: &r,
			Reason:      reason,
		}
	}

	return Evaluation{
		Decision: DecisionAllow,
		Reason:   "no matching network policy rule, allowing",
	}
}

// matchNetworkHostPattern checks if a host matches a network policy host pattern.
func matchNetworkHostPattern(host, pattern string) bool {
	pattern = strings.TrimSpace(strings.ToLower(pattern))
	if pattern == "" || pattern == "*" {
		return true
	}
	if host == pattern {
		return true
	}
	// Wildcard subdomain: *.example.com matches sub.example.com
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // .example.com
		return strings.HasSuffix(host, suffix)
	}
	return false
}

// matchProtocol checks if a protocol matches a rule protocol pattern.
func matchProtocol(protocol, ruleProtocol string) bool {
	ruleProtocol = strings.TrimSpace(strings.ToLower(ruleProtocol))
	if ruleProtocol == "" || ruleProtocol == "*" {
		return true
	}
	return protocol == ruleProtocol
}
