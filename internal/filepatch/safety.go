// Package filepatch implements a unified diff parser and applier.
package filepatch

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SafetyCheck is the result of assessing a patch against sandbox policy.
type SafetyCheck int

const (
	// SafetyAutoApprove indicates the patch is safe and can be applied without user confirmation.
	SafetyAutoApprove SafetyCheck = iota
	// SafetyAskUser indicates the patch requires explicit user approval.
	SafetyAskUser
	// SafetyReject indicates the patch violates policy and must be blocked.
	SafetyReject
)

// String returns a human-readable representation of the SafetyCheck value.
func (s SafetyCheck) String() string {
	switch s {
	case SafetyAutoApprove:
		return "auto_approve"
	case SafetyAskUser:
		return "ask_user"
	case SafetyReject:
		return "reject"
	default:
		return "unknown"
	}
}

// SandboxMode describes the sandbox enforcement level.
type SandboxMode string

const (
	SandboxReadOnly   SandboxMode = "read_only"
	SandboxWriteLocal SandboxMode = "write_local"
	SandboxFullAccess SandboxMode = "full_access"
)

// SandboxPolicy defines the active sandbox constraints for a session.
type SandboxPolicy struct {
	Mode           SandboxMode
	WritableRoots  []string
	ReadableRoots  []string
	NetworkAllowed []string
	NetworkDenied  []string
}

// SafetyError provides a descriptive reason for patch rejection.
type SafetyError struct {
	Path   string
	Reason string
}

func (e *SafetyError) Error() string {
	return fmt.Sprintf("patch safety violation: %s — %s", e.Path, e.Reason)
}

// AssessSafety evaluates a patch against sandbox policy and writable roots.
// It rejects patches targeting files outside writable paths or the project directory.
// If any single file change violates the policy, the entire patch is rejected.
//
// Requirements: Req-1.2, Req-29.1, Req-29.2, Req-29.3, Req-42.1, Req-42.2, Req-42.3
func AssessSafety(action *PatchAction, policy SandboxPolicy, writableRoots []string) (SafetyCheck, error) {
	if action == nil || len(action.Changes) == 0 {
		return SafetyAutoApprove, nil
	}

	// Full access mode auto-approves everything.
	if policy.Mode == SandboxFullAccess {
		return SafetyAutoApprove, nil
	}

	// Read-only mode rejects all write operations.
	if policy.Mode == SandboxReadOnly {
		if len(action.Changes) > 0 {
			path := changePaths(action.Changes[0])
			return SafetyReject, &SafetyError{
				Path:   path,
				Reason: "sandbox is in read-only mode; all file modifications are denied",
			}
		}
	}

	for _, change := range action.Changes {
		paths := allAffectedPaths(&change)
		for _, p := range paths {
			// Reject absolute paths or path traversal attempts outside project.
			if err := validateProjectScope(p); err != nil {
				return SafetyReject, &SafetyError{Path: p, Reason: err.Error()}
			}

			// Check write permission for target paths.
			if isWriteTarget(&change, p) {
				if !isWithinAnyRoot(p, writableRoots) {
					return SafetyReject, &SafetyError{
						Path:   p,
						Reason: "path is outside writable roots",
					}
				}
				if !isWithinAnyRoot(p, policy.WritableRoots) {
					return SafetyReject, &SafetyError{
						Path:   p,
						Reason: "path violates sandbox writable policy",
					}
				}
			}

			// Check read permission for source paths (e.g., rename source, modify source).
			if isReadSource(&change, p) {
				if !isReadable(p, policy) {
					return SafetyReject, &SafetyError{
						Path:   p,
						Reason: "path is not readable under sandbox policy",
					}
				}
			}
		}
	}

	// If we reach here in write_local mode, all paths are within allowed roots.
	return SafetyAutoApprove, nil
}

// validateProjectScope ensures a path does not escape the project directory.
// Paths must be relative and must not use ".." to traverse above the project root.
func validateProjectScope(p string) error {
	if filepath.IsAbs(p) {
		return fmt.Errorf("absolute paths are not allowed; must be relative to project root")
	}
	cleaned := filepath.Clean(p)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path traverses outside the project directory")
	}
	return nil
}

// isWithinAnyRoot checks if a path falls within at least one of the given root directories.
func isWithinAnyRoot(p string, roots []string) bool {
	if len(roots) == 0 {
		return false
	}
	cleaned := filepath.Clean(p)
	for _, root := range roots {
		rootCleaned := filepath.Clean(root)
		// A root of "." means the entire project directory is allowed.
		if rootCleaned == "." {
			return true
		}
		if cleaned == rootCleaned || strings.HasPrefix(cleaned, rootCleaned+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// isReadable checks if a path is readable under the sandbox policy.
// Readable paths include writable roots (write implies read) and explicit readable roots.
func isReadable(p string, policy SandboxPolicy) bool {
	if isWithinAnyRoot(p, policy.WritableRoots) {
		return true
	}
	if isWithinAnyRoot(p, policy.ReadableRoots) {
		return true
	}
	// If no readable roots are configured, allow reads from anywhere in project scope.
	if len(policy.ReadableRoots) == 0 && len(policy.WritableRoots) == 0 {
		return true
	}
	return false
}

// allAffectedPaths returns all file paths referenced by a FileChange.
func allAffectedPaths(fc *FileChange) []string {
	paths := make(map[string]struct{})
	if fc.OldPath != "" {
		paths[fc.OldPath] = struct{}{}
	}
	if fc.NewPath != "" {
		paths[fc.NewPath] = struct{}{}
	}
	result := make([]string, 0, len(paths))
	for p := range paths {
		result = append(result, p)
	}
	return result
}

// isWriteTarget returns true if the path is a write target for the given change.
func isWriteTarget(fc *FileChange, p string) bool {
	switch fc.Mode {
	case ModeCreate:
		return p == fc.NewPath
	case ModeDelete:
		return p == fc.OldPath
	case ModeRename:
		// Both old (delete) and new (create) are write targets.
		return p == fc.OldPath || p == fc.NewPath
	case ModeModify:
		target := fc.NewPath
		if target == "" {
			target = fc.OldPath
		}
		return p == target
	}
	return false
}

// isReadSource returns true if the path is a read source for the given change.
func isReadSource(fc *FileChange, p string) bool {
	switch fc.Mode {
	case ModeModify:
		// Modify reads the existing file.
		target := fc.NewPath
		if target == "" {
			target = fc.OldPath
		}
		return p == target
	case ModeRename:
		// Rename reads the source file.
		return p == fc.OldPath
	case ModeDelete:
		// Delete reads the file to verify existence.
		return p == fc.OldPath
	}
	return false
}

// changePaths returns the primary path for a file change (for error messages).
func changePaths(fc FileChange) string {
	if fc.NewPath != "" {
		return fc.NewPath
	}
	return fc.OldPath
}
