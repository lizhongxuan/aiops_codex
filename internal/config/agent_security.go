package config

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

var privateAgentSourcePrefixes = []netip.Prefix{
	mustParsePrefix("10.0.0.0/8"),
	mustParsePrefix("172.16.0.0/12"),
	mustParsePrefix("192.168.0.0/16"),
	mustParsePrefix("100.64.0.0/10"),
	mustParsePrefix("169.254.0.0/16"),
	mustParsePrefix("127.0.0.0/8"),
	mustParsePrefix("fc00::/7"),
	mustParsePrefix("fe80::/10"),
	mustParsePrefix("::1/128"),
}

func (c Config) ValidateHostAgentSecurity() error {
	if _, err := parseCIDROrIPList(c.AllowedAgentCIDRs); err != nil {
		return fmt.Errorf("parse HOST_AGENT_ALLOWED_CIDRS: %w", err)
	}

	switch normalizeHostAgentSecurityProfile(c.HostAgentSecurityProfile) {
	case "development":
		return nil
	case "production":
	default:
		return fmt.Errorf("unknown HOST_AGENT_SECURITY_PROFILE %q", c.HostAgentSecurityProfile)
	}

	if strings.TrimSpace(c.GRPCTLSCertFile) == "" || strings.TrimSpace(c.GRPCTLSKeyFile) == "" {
		return fmt.Errorf("HOST_AGENT_SECURITY_PROFILE=production requires AIOPS_GRPC_TLS_CERT_FILE and AIOPS_GRPC_TLS_KEY_FILE")
	}
	if strings.TrimSpace(c.GRPCTLSClientCAFile) == "" {
		return fmt.Errorf("HOST_AGENT_SECURITY_PROFILE=production requires AIOPS_GRPC_TLS_CLIENT_CA_FILE to enforce mTLS")
	}
	if len(c.AllowedAgentHostIDs) == 0 {
		return fmt.Errorf("HOST_AGENT_SECURITY_PROFILE=production requires HOST_AGENT_ALLOWED_HOST_IDS")
	}
	if len(c.AllowedAgentCIDRs) == 0 {
		return fmt.Errorf("HOST_AGENT_SECURITY_PROFILE=production requires HOST_AGENT_ALLOWED_CIDRS")
	}
	if c.UsesDefaultBootstrapToken() {
		return fmt.Errorf("HOST_AGENT_SECURITY_PROFILE=production requires rotating away from bootstrap token change-me")
	}

	bindHost := grpcBindHost(c.GRPCAddr)
	if grpcBindHostWildcard(bindHost) {
		return fmt.Errorf("HOST_AGENT_SECURITY_PROFILE=production requires AIOPS_GRPC_ADDR to bind to an explicit private/VPN address instead of %q", c.GRPCAddr)
	}
	if bindIP, ok := parseIPLiteral(bindHost); ok && !isPrivateOrVPNIP(bindIP) {
		return fmt.Errorf("HOST_AGENT_SECURITY_PROFILE=production rejects public gRPC bind address %q", bindHost)
	}

	return nil
}

func (c Config) AgentSourceAllowed(remoteAddr string) bool {
	prefixes, err := parseCIDROrIPList(c.AllowedAgentCIDRs)
	if err != nil || len(prefixes) == 0 {
		return err == nil
	}

	addr, ok := parseRemoteIP(remoteAddr)
	if !ok {
		return false
	}
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func (c Config) UsesDefaultBootstrapToken() bool {
	for _, token := range c.effectiveBootstrapTokens() {
		if strings.TrimSpace(token) == "change-me" {
			return true
		}
	}
	return false
}

func normalizeHostAgentSecurityProfile(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "development", "dev":
		return "development"
	case "production", "prod":
		return "production"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func grpcBindHost(addr string) string {
	value := strings.TrimSpace(addr)
	if value == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(value)
	if err == nil {
		return host
	}
	return value
}

func grpcBindHostWildcard(host string) bool {
	switch strings.TrimSpace(host) {
	case "", "0.0.0.0", "::", "[::]":
		return true
	default:
		return false
	}
}

func parseRemoteIP(remoteAddr string) (netip.Addr, bool) {
	host := strings.TrimSpace(remoteAddr)
	if value, _, err := net.SplitHostPort(host); err == nil {
		host = value
	}
	return parseIPLiteral(host)
}

func parseIPLiteral(host string) (netip.Addr, bool) {
	value := strings.TrimSpace(host)
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	if zone := strings.IndexByte(value, '%'); zone >= 0 {
		value = value[:zone]
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func isPrivateOrVPNIP(addr netip.Addr) bool {
	for _, prefix := range privateAgentSourcePrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func parseCIDROrIPList(values []string) ([]netip.Prefix, error) {
	if len(values) == 0 {
		return nil, nil
	}
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, raw := range values {
		prefix, err := parseCIDROrIP(raw)
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}

func parseCIDROrIP(value string) (netip.Prefix, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return netip.Prefix{}, fmt.Errorf("empty cidr")
	}
	if strings.Contains(raw, "/") {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("invalid cidr %q: %w", raw, err)
		}
		return prefix, nil
	}
	addr, ok := parseIPLiteral(raw)
	if !ok {
		return netip.Prefix{}, fmt.Errorf("invalid cidr or ip %q", raw)
	}
	bits := 32
	if addr.Is6() {
		bits = 128
	}
	return netip.PrefixFrom(addr, bits), nil
}

func mustParsePrefix(value string) netip.Prefix {
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		panic(err)
	}
	return prefix
}
