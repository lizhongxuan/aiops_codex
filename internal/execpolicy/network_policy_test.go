package execpolicy

import (
	"testing"
)

func TestNetworkPolicy_EvaluateOutbound_Deny(t *testing.T) {
	np := NewNetworkPolicy()
	np.AddRule(NetworkPolicyRule{
		Host:     "*.evil.com",
		Protocol: "*",
		Decision: DecisionForbidden,
	})

	eval := np.EvaluateOutbound("malware.evil.com", "https")
	if eval.Decision != DecisionForbidden {
		t.Errorf("expected forbidden, got %s", eval.Decision)
	}
}

func TestNetworkPolicy_EvaluateOutbound_Ask(t *testing.T) {
	np := NewNetworkPolicy()
	np.AddRule(NetworkPolicyRule{
		Host:     "api.example.com",
		Protocol: "https",
		Decision: DecisionPrompt,
	})

	eval := np.EvaluateOutbound("api.example.com", "https")
	if eval.Decision != DecisionPrompt {
		t.Errorf("expected prompt, got %s", eval.Decision)
	}
}

func TestNetworkPolicy_EvaluateOutbound_NoMatch(t *testing.T) {
	np := NewNetworkPolicy()
	np.AddRule(NetworkPolicyRule{
		Host:     "blocked.com",
		Protocol: "tcp",
		Decision: DecisionForbidden,
	})

	eval := np.EvaluateOutbound("allowed.com", "https")
	if eval.Decision != DecisionAllow {
		t.Errorf("expected allow (no match), got %s", eval.Decision)
	}
}

func TestNetworkPolicy_EvaluateOutbound_EmptyHost(t *testing.T) {
	np := NewNetworkPolicy()
	eval := np.EvaluateOutbound("", "https")
	if eval.Decision != DecisionForbidden {
		t.Errorf("expected forbidden for empty host, got %s", eval.Decision)
	}
}

func TestNetworkPolicy_EvaluateOutbound_ProtocolMismatch(t *testing.T) {
	np := NewNetworkPolicy()
	np.AddRule(NetworkPolicyRule{
		Host:     "api.example.com",
		Protocol: "tcp",
		Decision: DecisionForbidden,
	})

	// Different protocol should not match
	eval := np.EvaluateOutbound("api.example.com", "https")
	if eval.Decision != DecisionAllow {
		t.Errorf("expected allow (protocol mismatch), got %s", eval.Decision)
	}
}

func TestNetworkPolicy_EvaluateOutbound_WildcardProtocol(t *testing.T) {
	np := NewNetworkPolicy()
	np.AddRule(NetworkPolicyRule{
		Host:     "blocked.com",
		Protocol: "*",
		Decision: DecisionForbidden,
	})

	eval := np.EvaluateOutbound("blocked.com", "https")
	if eval.Decision != DecisionForbidden {
		t.Errorf("expected forbidden with wildcard protocol, got %s", eval.Decision)
	}
}
