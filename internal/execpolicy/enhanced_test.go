package execpolicy

import (
	"testing"
)

func TestEvaluateConditional(t *testing.T) {
	env := map[string]string{
		"CI":   "true",
		"HOME": "/home/user",
	}

	tests := []struct {
		name      string
		condition string
		expected  Decision
	}{
		{"env_equals_match", "env:CI=true", DecisionAllow},
		{"env_equals_no_match", "env:CI=false", ""},
		{"env_exists", "env:HOME", DecisionAllow},
		{"env_not_exists", "env:MISSING", ""},
		{"negation_exists", "!env:MISSING", DecisionAllow},
		{"negation_not_exists", "!env:CI", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := ConditionalRule{
				Condition: tt.condition,
				Decision:  DecisionAllow,
			}
			got := EvaluateConditional(rule, env)
			if got != tt.expected {
				t.Errorf("EvaluateConditional(%q) = %q, want %q", tt.condition, got, tt.expected)
			}
		})
	}
}

func TestEvaluateNetworkRule(t *testing.T) {
	tests := []struct {
		name     string
		rule     NetworkRule
		host     string
		port     int
		protocol string
		wantDec  Decision
	}{
		{
			"exact_match",
			NetworkRule{Host: "api.example.com", Port: 443, Protocol: "https", Decision: DecisionAllow},
			"api.example.com", 443, "https",
			DecisionAllow,
		},
		{
			"wildcard_host",
			NetworkRule{Host: "*.example.com", Port: 0, Protocol: "*", Decision: DecisionAllow},
			"api.example.com", 8080, "tcp",
			DecisionAllow,
		},
		{
			"port_mismatch",
			NetworkRule{Host: "api.example.com", Port: 443, Protocol: "https", Decision: DecisionAllow},
			"api.example.com", 80, "https",
			"",
		},
		{
			"protocol_mismatch",
			NetworkRule{Host: "api.example.com", Port: 443, Protocol: "https", Decision: DecisionAllow},
			"api.example.com", 443, "tcp",
			"",
		},
		{
			"host_mismatch",
			NetworkRule{Host: "api.example.com", Port: 443, Protocol: "https", Decision: DecisionForbidden},
			"other.com", 443, "https",
			"",
		},
		{
			"all_hosts",
			NetworkRule{Host: "*", Port: 0, Protocol: "*", Decision: DecisionForbidden},
			"anything.com", 9999, "udp",
			DecisionForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eval := EvaluateNetworkRule(tt.rule, tt.host, tt.port, tt.protocol)
			if eval.Decision != tt.wantDec {
				t.Errorf("EvaluateNetworkRule() decision = %q, want %q", eval.Decision, tt.wantDec)
			}
		})
	}
}

func TestEvaluateComposite_AND(t *testing.T) {
	rule := CompositeRule{
		Operator: "and",
		Rules: []Rule{
			{Kind: RuleKindCommand, Pattern: "rm", Decision: DecisionForbidden},
			{Kind: RuleKindCommand, Pattern: "rm -rf", Decision: DecisionForbidden},
		},
	}

	// "rm -rf /tmp" matches both patterns (prefix match)
	eval := EvaluateComposite(rule, "rm -rf /tmp")
	if eval.Decision != DecisionForbidden {
		t.Errorf("AND composite: expected forbidden, got %q", eval.Decision)
	}

	// "ls" doesn't match "rm" pattern
	eval = EvaluateComposite(rule, "ls")
	if eval.Decision != "" {
		t.Errorf("AND composite: expected empty decision for non-match, got %q", eval.Decision)
	}
}

func TestEvaluateComposite_OR(t *testing.T) {
	rule := CompositeRule{
		Operator: "or",
		Rules: []Rule{
			{Kind: RuleKindCommand, Pattern: "rm", Decision: DecisionForbidden, Description: "rm blocked"},
			{Kind: RuleKindCommand, Pattern: "dd", Decision: DecisionForbidden, Description: "dd blocked"},
		},
	}

	// "rm -rf" matches first rule
	eval := EvaluateComposite(rule, "rm -rf")
	if eval.Decision != DecisionForbidden {
		t.Errorf("OR composite: expected forbidden for rm, got %q", eval.Decision)
	}

	// "dd if=/dev/zero" matches second rule
	eval = EvaluateComposite(rule, "dd if=/dev/zero")
	if eval.Decision != DecisionForbidden {
		t.Errorf("OR composite: expected forbidden for dd, got %q", eval.Decision)
	}

	// "ls" matches neither
	eval = EvaluateComposite(rule, "ls")
	if eval.Decision != "" {
		t.Errorf("OR composite: expected empty for ls, got %q", eval.Decision)
	}
}

func TestEvaluateComposite_InvalidOperator(t *testing.T) {
	rule := CompositeRule{
		Operator: "xor",
		Rules:    []Rule{{Kind: RuleKindCommand, Pattern: "ls", Decision: DecisionAllow}},
	}
	eval := EvaluateComposite(rule, "ls")
	if eval.Decision != DecisionForbidden {
		t.Errorf("invalid operator should return forbidden, got %q", eval.Decision)
	}
}

func TestEnhancedEvaluateNetwork(t *testing.T) {
	p := NewPolicy(DecisionPrompt)
	p.AddRule(Rule{
		Kind:        RuleKindNetwork,
		Pattern:     "api.example.com",
		Decision:    DecisionAllow,
		Description: "allow api",
		Priority:    10,
	})
	p.AddRule(Rule{
		Kind:        RuleKindNetwork,
		Pattern:     "evil.com",
		Decision:    DecisionForbidden,
		Description: "block evil",
		Priority:    20,
	})

	t.Run("allowed_host", func(t *testing.T) {
		eval := p.EnhancedEvaluateNetwork("api.example.com", 443, "https")
		if eval.Decision != DecisionAllow {
			t.Errorf("expected allow, got %q", eval.Decision)
		}
	})

	t.Run("forbidden_host", func(t *testing.T) {
		eval := p.EnhancedEvaluateNetwork("evil.com", 80, "tcp")
		if eval.Decision != DecisionForbidden {
			t.Errorf("expected forbidden, got %q", eval.Decision)
		}
	})

	t.Run("unknown_host_default", func(t *testing.T) {
		eval := p.EnhancedEvaluateNetwork("unknown.com", 443, "https")
		if eval.Decision != DecisionPrompt {
			t.Errorf("expected prompt (default), got %q", eval.Decision)
		}
	})

	t.Run("amendment_override", func(t *testing.T) {
		p.AddAmendment(Amendment{
			Rule: Rule{
				Kind:     RuleKindNetwork,
				Pattern:  "evil.com",
				Decision: DecisionAllow,
			},
			SessionID: "test-session",
			Reason:    "user approved",
		})
		eval := p.EnhancedEvaluateNetwork("evil.com", 80, "tcp")
		if eval.Decision != DecisionAllow {
			t.Errorf("expected allow via amendment, got %q", eval.Decision)
		}
	})
}
