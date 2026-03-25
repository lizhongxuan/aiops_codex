package config

import (
	"strings"
	"testing"
)

func TestValidAgentBootstrapTokenSupportsRotationList(t *testing.T) {
	cfg := Config{
		HostAgentBootstrapToken:  "current-token",
		HostAgentBootstrapTokens: []string{"current-token", "previous-token"},
	}

	if !cfg.ValidAgentBootstrapToken("current-token") {
		t.Fatalf("expected current token to be accepted")
	}
	if !cfg.ValidAgentBootstrapToken("previous-token") {
		t.Fatalf("expected previous token to be accepted during rotation")
	}
	if cfg.ValidAgentBootstrapToken("wrong-token") {
		t.Fatalf("expected wrong token to be rejected")
	}
}

func TestAgentHostAllowedUsesAllowlistWhenConfigured(t *testing.T) {
	cfg := Config{
		AllowedAgentHostIDs: []string{"linux-01", "linux-02"},
	}

	if !cfg.AgentHostAllowed("linux-01") {
		t.Fatalf("expected allowed host id to pass")
	}
	if cfg.AgentHostAllowed("linux-03") {
		t.Fatalf("expected unknown host id to be rejected")
	}
}

func TestBootstrapTokensDoesNotAppendDefaultFallbackWhenRotationListIsConfigured(t *testing.T) {
	t.Setenv("HOST_AGENT_BOOTSTRAP_TOKENS", "rotated-a,rotated-b")
	t.Setenv("HOST_AGENT_BOOTSTRAP_TOKEN", "")

	tokens := bootstrapTokens()
	if len(tokens) != 2 {
		t.Fatalf("expected only rotation tokens, got %v", tokens)
	}
	for _, token := range tokens {
		if token == "change-me" {
			t.Fatalf("expected default token to be excluded when rotation list is configured")
		}
	}
}

func TestAgentSourceAllowedUsesCIDRAllowlist(t *testing.T) {
	cfg := Config{
		AllowedAgentCIDRs: []string{"100.64.0.0/10", "10.0.0.0/8"},
	}

	if !cfg.AgentSourceAllowed("100.100.1.8:18090") {
		t.Fatalf("expected tailscale source to be allowed")
	}
	if !cfg.AgentSourceAllowed("10.20.30.40:18090") {
		t.Fatalf("expected intranet source to be allowed")
	}
	if cfg.AgentSourceAllowed("8.8.8.8:18090") {
		t.Fatalf("expected public source to be rejected")
	}
}

func TestValidateHostAgentSecurityProduction(t *testing.T) {
	cfg := Config{
		GRPCAddr:                 "100.64.0.12:18090",
		GRPCTLSCertFile:          "/certs/server.pem",
		GRPCTLSKeyFile:           "/certs/server-key.pem",
		GRPCTLSClientCAFile:      "/certs/ca.pem",
		HostAgentBootstrapTokens: []string{"rotated-token"},
		AllowedAgentHostIDs:      []string{"linux-01"},
		AllowedAgentCIDRs:        []string{"100.64.0.0/10"},
		HostAgentSecurityProfile: "production",
	}

	if err := cfg.ValidateHostAgentSecurity(); err != nil {
		t.Fatalf("expected production config to pass, got %v", err)
	}

	cfg.GRPCAddr = "0.0.0.0:18090"
	if err := cfg.ValidateHostAgentSecurity(); err == nil || !strings.Contains(err.Error(), "explicit private/VPN address") {
		t.Fatalf("expected wildcard bind rejection, got %v", err)
	}

	cfg.GRPCAddr = "100.64.0.12:18090"
	cfg.GRPCTLSClientCAFile = ""
	if err := cfg.ValidateHostAgentSecurity(); err == nil || !strings.Contains(err.Error(), "AIOPS_GRPC_TLS_CLIENT_CA_FILE") {
		t.Fatalf("expected mTLS requirement, got %v", err)
	}
}
