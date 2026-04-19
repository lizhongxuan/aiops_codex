// Package sandbox provides OS-level process sandboxing with platform-specific
// implementations (Landlock on Linux, Seatbelt on macOS).
package sandbox

import "runtime"

// SandboxMode describes the sandbox enforcement level.
type SandboxMode string

const (
	ModeReadOnly   SandboxMode = "read_only"
	ModeWriteLocal SandboxMode = "write_local"
	ModeFullAccess SandboxMode = "full_access"
)

// SandboxPolicy defines the active sandbox constraints for a session.
type SandboxPolicy struct {
	Mode           SandboxMode `json:"mode"`
	WritableRoots  []string    `json:"writable_roots,omitempty"`
	ReadableRoots  []string    `json:"readable_roots,omitempty"`
	NetworkAllowed []string    `json:"network_allowed,omitempty"` // Host:port patterns
	NetworkDenied  []string    `json:"network_denied,omitempty"`
}

// Sandbox is the platform abstraction for OS-level process sandboxing.
type Sandbox interface {
	// Apply enforces sandbox restrictions based on the given policy.
	Apply(policy SandboxPolicy) error
	// Platform returns the sandbox implementation name.
	Platform() string
}

// NewSandbox returns the platform-appropriate sandbox implementation.
func NewSandbox() Sandbox {
	switch runtime.GOOS {
	case "linux":
		return &LandlockSandbox{}
	case "darwin":
		return &SeatbeltSandbox{}
	default:
		return &NoopSandbox{}
	}
}

// NoopSandbox is a no-op sandbox for unsupported platforms.
type NoopSandbox struct{}

func (n *NoopSandbox) Apply(_ SandboxPolicy) error { return nil }
func (n *NoopSandbox) Platform() string            { return "noop" }
