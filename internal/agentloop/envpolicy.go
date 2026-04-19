package agentloop

import (
	"os"
	"strings"
)

// EnvPolicy controls which environment variables are passed to tool commands.
type EnvPolicy string

const (
	EnvPolicyAll  EnvPolicy = "all"  // Pass all env vars
	EnvPolicyNone EnvPolicy = "none" // Pass no env vars
	EnvPolicyCore EnvPolicy = "core" // Pass only safe vars (exclude sensitive)
)

// SensitivePatterns are substrings that indicate a sensitive environment variable.
// Variables containing these patterns are excluded under the "core" policy.
var SensitivePatterns = []string{
	"KEY",
	"SECRET",
	"TOKEN",
	"PASSWORD",
	"CREDENTIAL",
}

// FilterEnv applies the environment variable policy to os.Environ().
// Under "all" policy, all variables are returned.
// Under "none" policy, no variables are returned.
// Under "core" policy, variables matching sensitive patterns are excluded.
func FilterEnv(policy EnvPolicy) []string {
	switch policy {
	case EnvPolicyNone:
		return nil
	case EnvPolicyAll:
		return os.Environ()
	case EnvPolicyCore:
		return filterSensitive(os.Environ())
	default:
		// Default to core for safety
		return filterSensitive(os.Environ())
	}
}

// filterSensitive removes environment variables that match sensitive patterns.
func filterSensitive(env []string) []string {
	var filtered []string
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 0 {
			continue
		}
		name := strings.ToUpper(parts[0])
		if isSensitive(name) {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

// isSensitive checks if an environment variable name matches any sensitive pattern.
func isSensitive(name string) bool {
	for _, pattern := range SensitivePatterns {
		if strings.Contains(name, pattern) {
			return true
		}
	}
	return false
}
