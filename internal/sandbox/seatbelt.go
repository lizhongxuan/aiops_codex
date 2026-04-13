package sandbox

import (
	"fmt"
	"strings"
)

// SeatbeltSandbox implements the Sandbox interface using macOS sandbox-exec (Seatbelt).
type SeatbeltSandbox struct {
	applied bool
	policy  SandboxPolicy
	profile string
}

// Apply enforces Seatbelt-based sandbox profiles for command execution.
func (s *SeatbeltSandbox) Apply(policy SandboxPolicy) error {
	s.policy = policy
	s.profile = s.GenerateProfile(policy)
	s.applied = true

	if policy.Mode == ModeFullAccess {
		return nil
	}

	// In production, this would invoke sandbox-exec with the generated profile.
	return nil
}

// Platform returns the sandbox implementation name.
func (s *SeatbeltSandbox) Platform() string { return "seatbelt" }

// Profile returns the generated Seatbelt profile string.
func (s *SeatbeltSandbox) Profile() string { return s.profile }

// GenerateProfile produces a sandbox-exec profile from the SandboxPolicy.
func (s *SeatbeltSandbox) GenerateProfile(policy SandboxPolicy) string {
	var sb strings.Builder

	sb.WriteString("(version 1)\n")

	switch policy.Mode {
	case ModeReadOnly:
		sb.WriteString("(deny default)\n")
		sb.WriteString("(allow process-exec)\n")
		sb.WriteString("(allow process-fork)\n")
		sb.WriteString("(allow sysctl-read)\n")
		sb.WriteString("(allow mach-lookup)\n")
		// Allow reads to specified roots
		for _, root := range policy.ReadableRoots {
			sb.WriteString(fmt.Sprintf("(allow file-read* (subpath %q))\n", root))
		}
		// Deny all writes
		sb.WriteString("(deny file-write*)\n")

	case ModeWriteLocal:
		sb.WriteString("(deny default)\n")
		sb.WriteString("(allow process-exec)\n")
		sb.WriteString("(allow process-fork)\n")
		sb.WriteString("(allow sysctl-read)\n")
		sb.WriteString("(allow mach-lookup)\n")
		// Allow reads
		for _, root := range policy.ReadableRoots {
			sb.WriteString(fmt.Sprintf("(allow file-read* (subpath %q))\n", root))
		}
		// Allow writes to writable roots only
		for _, root := range policy.WritableRoots {
			sb.WriteString(fmt.Sprintf("(allow file-read* (subpath %q))\n", root))
			sb.WriteString(fmt.Sprintf("(allow file-write* (subpath %q))\n", root))
		}

	case ModeFullAccess:
		sb.WriteString("(allow default)\n")
		return sb.String()
	}

	// Network rules
	if len(policy.NetworkDenied) > 0 {
		sb.WriteString("(deny network*)\n")
	} else if len(policy.NetworkAllowed) > 0 {
		sb.WriteString("(deny network*)\n")
		for _, host := range policy.NetworkAllowed {
			sb.WriteString(fmt.Sprintf("(allow network* (remote ip %q))\n", host))
		}
	} else {
		sb.WriteString("(allow network*)\n")
	}

	return sb.String()
}

// SeatbeltExecArgs returns the command-line arguments needed to run a command
// under the generated Seatbelt profile.
func (s *SeatbeltSandbox) SeatbeltExecArgs(command string) []string {
	if s.profile == "" || s.policy.Mode == ModeFullAccess {
		return []string{"sh", "-c", command}
	}
	return []string{"sandbox-exec", "-p", s.profile, "sh", "-c", command}
}
