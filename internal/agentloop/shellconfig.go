package agentloop

import (
	"os"
	"runtime"
	"strings"
)

// ShellType identifies the shell type.
type ShellType string

const (
	ShellBash       ShellType = "bash"
	ShellZsh        ShellType = "zsh"
	ShellSh         ShellType = "sh"
	ShellPowerShell ShellType = "powershell"
	ShellCmd        ShellType = "cmd"
)

// ShellConfig holds shell-specific configuration for tool execution.
type ShellConfig struct {
	Type      ShellType `json:"type"`
	Flags     []string  `json:"flags,omitempty"`
	QuoteChar string    `json:"quote_char"`
	PathSep   string    `json:"path_sep"`
}

// DetectShell identifies the user's shell and returns appropriate configuration.
func DetectShell() ShellConfig {
	if runtime.GOOS == "windows" {
		// Check for PowerShell
		if ps := os.Getenv("PSModulePath"); ps != "" {
			return ShellConfig{
				Type:      ShellPowerShell,
				Flags:     []string{"-NoProfile", "-Command"},
				QuoteChar: "\"",
				PathSep:   "\\",
			}
		}
		return ShellConfig{
			Type:      ShellCmd,
			Flags:     []string{"/C"},
			QuoteChar: "\"",
			PathSep:   "\\",
		}
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		return ShellConfig{
			Type:      ShellSh,
			Flags:     []string{"-c"},
			QuoteChar: "'",
			PathSep:   "/",
		}
	}

	// Extract shell name from path
	parts := strings.Split(shell, "/")
	shellName := parts[len(parts)-1]

	switch shellName {
	case "zsh":
		return ShellConfig{
			Type:      ShellZsh,
			Flags:     []string{"-c"},
			QuoteChar: "'",
			PathSep:   "/",
		}
	case "bash":
		return ShellConfig{
			Type:      ShellBash,
			Flags:     []string{"-c"},
			QuoteChar: "'",
			PathSep:   "/",
		}
	default:
		return ShellConfig{
			Type:      ShellSh,
			Flags:     []string{"-c"},
			QuoteChar: "'",
			PathSep:   "/",
		}
	}
}
