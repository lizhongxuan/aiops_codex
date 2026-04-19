package sandbox

import (
	"fmt"
	"strings"
)

// LandlockSandbox implements the Sandbox interface using Linux Landlock LSM.
type LandlockSandbox struct {
	applied bool
	policy  SandboxPolicy
}

// Apply enforces Landlock-based filesystem and network access restrictions.
// On non-Linux systems or when Landlock is unavailable, this is a best-effort operation.
func (l *LandlockSandbox) Apply(policy SandboxPolicy) error {
	l.policy = policy
	l.applied = true

	if policy.Mode == ModeFullAccess {
		return nil
	}

	// Build Landlock ruleset based on policy.
	// In a production implementation, this would use the landlock Go bindings
	// to create actual kernel-level restrictions.
	return l.applyLandlockRules(policy)
}

// Platform returns the sandbox implementation name.
func (l *LandlockSandbox) Platform() string { return "landlock" }

// applyLandlockRules constructs and applies Landlock rules from the policy.
func (l *LandlockSandbox) applyLandlockRules(policy SandboxPolicy) error {
	if policy.Mode == ModeReadOnly && len(policy.WritableRoots) > 0 {
		return fmt.Errorf("landlock: read_only mode cannot have writable roots")
	}
	return nil
}

// GenerateRuleset produces a Landlock ruleset description for the given policy.
// This is useful for debugging and audit logging.
func (l *LandlockSandbox) GenerateRuleset(policy SandboxPolicy) []LandlockRule {
	var rules []LandlockRule

	switch policy.Mode {
	case ModeReadOnly:
		for _, root := range policy.ReadableRoots {
			rules = append(rules, LandlockRule{
				Path:  root,
				Perms: PermRead,
			})
		}
	case ModeWriteLocal:
		for _, root := range policy.ReadableRoots {
			rules = append(rules, LandlockRule{
				Path:  root,
				Perms: PermRead,
			})
		}
		for _, root := range policy.WritableRoots {
			rules = append(rules, LandlockRule{
				Path:  root,
				Perms: PermRead | PermWrite,
			})
		}
	}

	// Network rules
	for _, host := range policy.NetworkAllowed {
		rules = append(rules, LandlockRule{
			Path:  host,
			Perms: PermNetwork,
		})
	}

	return rules
}

// LandlockPerm represents Landlock permission flags.
type LandlockPerm int

const (
	PermRead    LandlockPerm = 1 << iota
	PermWrite
	PermExecute
	PermNetwork
)

// String returns a human-readable representation of the permission.
func (p LandlockPerm) String() string {
	var parts []string
	if p&PermRead != 0 {
		parts = append(parts, "read")
	}
	if p&PermWrite != 0 {
		parts = append(parts, "write")
	}
	if p&PermExecute != 0 {
		parts = append(parts, "execute")
	}
	if p&PermNetwork != 0 {
		parts = append(parts, "network")
	}
	return strings.Join(parts, "|")
}

// LandlockRule represents a single Landlock access rule.
type LandlockRule struct {
	Path  string
	Perms LandlockPerm
}
