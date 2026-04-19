package sandbox

import (
	"context"
	"fmt"
	"sync"
)

// NetworkApprovalRequest represents a network access approval request.
type NetworkApprovalRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"` // "tcp", "udp", "https"
}

// cacheKey returns a unique key for caching approval decisions.
func (r NetworkApprovalRequest) cacheKey() string {
	return fmt.Sprintf("%s:%d/%s", r.Host, r.Port, r.Protocol)
}

// NetworkApprovalManager evaluates network access requests against policy
// and caches approved host+protocol combinations.
type NetworkApprovalManager struct {
	mu    sync.RWMutex
	cache map[string]bool // cacheKey → approved

	// Policy is the sandbox policy to evaluate against.
	Policy SandboxPolicy

	// ApprovalFunc is called when policy requires user approval.
	// If nil, access is denied by default.
	ApprovalFunc func(ctx context.Context, req NetworkApprovalRequest) (bool, error)
}

// NewNetworkApprovalManager creates a new NetworkApprovalManager.
func NewNetworkApprovalManager(policy SandboxPolicy) *NetworkApprovalManager {
	return &NetworkApprovalManager{
		cache:  make(map[string]bool),
		Policy: policy,
	}
}

// EvaluateNetworkAccess checks network policy and requests approval if needed.
// Approved host+protocol combinations are cached for subsequent requests.
func (m *NetworkApprovalManager) EvaluateNetworkAccess(ctx context.Context, req NetworkApprovalRequest) error {
	if req.Host == "" {
		return fmt.Errorf("network request must specify a host")
	}

	// Check if explicitly denied
	for _, denied := range m.Policy.NetworkDenied {
		if matchHost(req.Host, denied) {
			return fmt.Errorf("network access to %s is denied by policy", req.Host)
		}
	}

	// Check if explicitly allowed
	for _, allowed := range m.Policy.NetworkAllowed {
		if matchHost(req.Host, allowed) {
			return nil
		}
	}

	// If no explicit allow/deny lists, allow by default
	if len(m.Policy.NetworkAllowed) == 0 && len(m.Policy.NetworkDenied) == 0 {
		return nil
	}

	// Check cache
	key := req.cacheKey()
	m.mu.RLock()
	if approved, ok := m.cache[key]; ok {
		m.mu.RUnlock()
		if approved {
			return nil
		}
		return fmt.Errorf("network access to %s previously denied", req.Host)
	}
	m.mu.RUnlock()

	// Request approval
	if m.ApprovalFunc == nil {
		return fmt.Errorf("network access to %s requires approval but no approval handler configured", req.Host)
	}

	approved, err := m.ApprovalFunc(ctx, req)
	if err != nil {
		return fmt.Errorf("network approval failed: %w", err)
	}

	// Cache the decision
	m.mu.Lock()
	m.cache[key] = approved
	m.mu.Unlock()

	if !approved {
		return fmt.Errorf("network access to %s denied by user", req.Host)
	}
	return nil
}

// ClearCache removes all cached approval decisions.
func (m *NetworkApprovalManager) ClearCache() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache = make(map[string]bool)
}

// CacheSize returns the number of cached decisions.
func (m *NetworkApprovalManager) CacheSize() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.cache)
}

// matchHost checks if a host matches a pattern (supports wildcard prefix).
func matchHost(host, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if host == pattern {
		return true
	}
	// Wildcard subdomain matching: *.example.com matches foo.example.com
	if len(pattern) > 2 && pattern[:2] == "*." {
		suffix := pattern[1:] // .example.com
		if len(host) > len(suffix) && host[len(host)-len(suffix):] == suffix {
			return true
		}
	}
	return false
}
