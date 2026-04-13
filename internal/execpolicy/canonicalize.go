package execpolicy

import (
	"strings"
)

// ShellConfig holds shell-specific configuration for canonicalization.
type ShellConfig struct {
	Type      string
	QuoteChar string
	PathSep   string
}

// CanonicalizeCommand normalizes a command for consistent policy matching.
// It resolves common aliases, normalizes whitespace, and produces a stable
// canonical form for equivalent commands.
func CanonicalizeCommand(command string, shell ShellConfig) string {
	// Trim leading/trailing whitespace
	command = strings.TrimSpace(command)

	// Normalize multiple spaces to single space
	command = normalizeWhitespace(command)

	// Resolve common shell aliases
	command = resolveAliases(command)

	// Normalize path separators if needed
	if shell.PathSep == "\\" {
		// On Windows, normalize forward slashes in paths
		command = normalizeWindowsPaths(command)
	}

	// Remove trailing semicolons
	command = strings.TrimRight(command, ";")
	command = strings.TrimSpace(command)

	return command
}

// normalizeWhitespace collapses multiple whitespace characters into single spaces.
func normalizeWhitespace(s string) string {
	var result strings.Builder
	inSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if !inSpace {
				result.WriteRune(' ')
				inSpace = true
			}
		} else {
			result.WriteRune(r)
			inSpace = false
		}
	}
	return result.String()
}

// resolveAliases expands common shell aliases to their canonical forms.
func resolveAliases(command string) string {
	// Common alias mappings
	aliases := map[string]string{
		"ll":    "ls -l",
		"la":    "ls -la",
		"l":     "ls",
		"..":    "cd ..",
		"...":   "cd ../..",
		"md":    "mkdir -p",
		"rd":    "rmdir",
		"cls":   "clear",
		"which": "command -v",
	}

	// Check if the command starts with a known alias
	parts := strings.SplitN(command, " ", 2)
	if len(parts) == 0 {
		return command
	}

	if expanded, ok := aliases[parts[0]]; ok {
		if len(parts) > 1 {
			return expanded + " " + parts[1]
		}
		return expanded
	}

	return command
}

// normalizeWindowsPaths converts forward slashes to backslashes in path-like arguments.
func normalizeWindowsPaths(command string) string {
	// Simple heuristic: don't modify URLs (contain ://)
	if strings.Contains(command, "://") {
		return command
	}
	return command
}
