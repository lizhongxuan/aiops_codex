package agentloop

import (
	"fmt"
	"os"
	"strings"
)

// ShellSnapshot captures the state of a shell environment for later restoration.
type ShellSnapshot struct {
	Cwd     string            `json:"cwd"`
	Env     map[string]string `json:"env"`
	History []string          `json:"history"`
}

// CaptureSnapshot captures the current shell state including working directory
// and environment variables.
func CaptureSnapshot() (*ShellSnapshot, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("capture snapshot: get cwd: %w", err)
	}

	envVars := make(map[string]string)
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			envVars[parts[0]] = parts[1]
		}
	}

	return &ShellSnapshot{
		Cwd:     cwd,
		Env:     envVars,
		History: nil, // History is populated by the caller if needed
	}, nil
}

// RestoreSnapshot restores a previously captured shell state.
// It changes the working directory and sets environment variables.
func RestoreSnapshot(snap *ShellSnapshot) error {
	if snap == nil {
		return fmt.Errorf("restore snapshot: nil snapshot")
	}

	if snap.Cwd != "" {
		if err := os.Chdir(snap.Cwd); err != nil {
			return fmt.Errorf("restore snapshot: chdir to %s: %w", snap.Cwd, err)
		}
	}

	// Restore environment variables
	for key, value := range snap.Env {
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("restore snapshot: setenv %s: %w", key, err)
		}
	}

	return nil
}
