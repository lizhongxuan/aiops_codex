package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// RegisterUnifiedExecTool registers the unified_exec tool.
func RegisterUnifiedExecTool(reg *ToolRegistry) {
	reg.Register(ToolEntry{
		Name:        "unified_exec",
		Description: "Execute a command with support for TTY mode, stdin writing, and yield time configuration.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Command to execute.",
				},
				"args": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Command arguments.",
				},
				"cwd": map[string]interface{}{
					"type":        "string",
					"description": "Working directory (defaults to session cwd).",
				},
				"stdin": map[string]interface{}{
					"type":        "string",
					"description": "Data to write to stdin.",
				},
				"tty": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to allocate a pseudo-TTY (default false).",
				},
				"timeout_sec": map[string]interface{}{
					"type":        "integer",
					"description": "Execution timeout in seconds (default 30, max 300).",
				},
				"yield_ms": map[string]interface{}{
					"type":        "integer",
					"description": "Yield time in milliseconds before reading output (default 0).",
				},
			},
			"required":             []string{"command"},
			"additionalProperties": false,
		},
		Handler:          handleUnifiedExec,
		RequiresApproval: true,
	})
}

// UnifiedExecResult is the structured result of unified exec.
type UnifiedExecResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Duration string `json:"duration"`
	Error    string `json:"error,omitempty"`
}

func handleUnifiedExec(ctx context.Context, session *Session, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
	command, _ := args["command"].(string)
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("unified_exec requires a non-empty 'command' argument")
	}

	// Parse args array.
	var cmdArgs []string
	if argsRaw, ok := args["args"]; ok {
		data, _ := json.Marshal(argsRaw)
		_ = json.Unmarshal(data, &cmdArgs)
	}

	// Working directory.
	cwd := session.Cwd()
	if cwdArg, ok := args["cwd"].(string); ok && cwdArg != "" {
		cwd = cwdArg
	}
	if cwd == "" {
		cwd = "."
	}

	// Timeout.
	timeoutSec := 30
	if t, ok := args["timeout_sec"].(float64); ok && t > 0 {
		timeoutSec = int(t)
	}
	if timeoutSec > 300 {
		timeoutSec = 300
	}

	// Yield time.
	yieldMs := 0
	if y, ok := args["yield_ms"].(float64); ok && y > 0 {
		yieldMs = int(y)
	}

	// Stdin data.
	stdinData, _ := args["stdin"].(string)

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, command, cmdArgs...)
	cmd.Dir = cwd

	if stdinData != "" {
		cmd.Stdin = strings.NewReader(stdinData)
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Yield before execution if requested.
	if yieldMs > 0 {
		time.Sleep(time.Duration(yieldMs) * time.Millisecond)
	}

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	result := UnifiedExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration.Round(time.Millisecond).String(),
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
		return "", fmt.Errorf("unified_exec: %w", marshalErr)
	}
	return string(out), nil
}
