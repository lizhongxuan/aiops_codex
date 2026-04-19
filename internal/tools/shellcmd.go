package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// ShellBackend configures which shell is used for command execution.
type ShellBackend struct {
	Shell string   // e.g. "bash", "sh", "zsh", "powershell"
	Args  []string // e.g. ["-c"] for bash
}

// DefaultShellBackend returns the default shell backend for the current OS.
func DefaultShellBackend() ShellBackend {
	if runtime.GOOS == "windows" {
		return ShellBackend{Shell: "cmd", Args: []string{"/C"}}
	}
	return ShellBackend{Shell: "bash", Args: []string{"-c"}}
}

// sessionShellBackend is the package-level configurable shell backend.
var sessionShellBackend = DefaultShellBackend()

// SetShellBackend configures the shell backend used by shell_command.
func SetShellBackend(backend ShellBackend) {
	sessionShellBackend = backend
}

// RegisterShellCommandTool registers the shell_command tool.
func RegisterShellCommandTool(reg *ToolRegistry) {
	reg.Register(ToolEntry{
		Name:        "shell_command",
		Description: "Execute a shell command through the configured shell backend.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Shell command to execute.",
				},
				"cwd": map[string]interface{}{
					"type":        "string",
					"description": "Working directory (defaults to session cwd).",
				},
				"timeout_sec": map[string]interface{}{
					"type":        "integer",
					"description": "Execution timeout in seconds (default 30, max 300).",
				},
				"env": map[string]interface{}{
					"type":        "object",
					"description": "Additional environment variables.",
				},
			},
			"required":             []string{"command"},
			"additionalProperties": false,
		},
		Handler:          handleShellCommand,
		RequiresApproval: true,
	})
}

// ShellCommandResult is the structured result of a shell command.
type ShellCommandResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Duration string `json:"duration"`
	Shell    string `json:"shell"`
	Error    string `json:"error,omitempty"`
}

func handleShellCommand(ctx context.Context, tc ToolContext, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
	command, _ := args["command"].(string)
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("shell_command requires a non-empty 'command' argument")
	}

	cwd := tc.Cwd()
	if cwdArg, ok := args["cwd"].(string); ok && cwdArg != "" {
		cwd = cwdArg
	}
	if cwd == "" {
		cwd = "."
	}

	timeoutSec := 30
	if t, ok := args["timeout_sec"].(float64); ok && t > 0 {
		timeoutSec = int(t)
	}
	if timeoutSec > 300 {
		timeoutSec = 300
	}

	backend := sessionShellBackend
	shellArgs := append(backend.Args, command)

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, backend.Shell, shellArgs...)
	cmd.Dir = cwd

	// Set additional environment variables.
	if envRaw, ok := args["env"]; ok {
		if envMap, ok := envRaw.(map[string]interface{}); ok {
			for k, v := range envMap {
				if vs, ok := v.(string); ok {
					cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, vs))
				}
			}
		}
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	result := ShellCommandResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration.Round(time.Millisecond).String(),
		Shell:    backend.Shell,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
			result.Error = err.Error()
		}
	}

	out, marshalErr := json.MarshalIndent(result, "", "  ")
	if marshalErr != nil {
		return "", fmt.Errorf("shell_command: %w", marshalErr)
	}
	return string(out), nil
}
