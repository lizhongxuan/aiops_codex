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

// RegisterCodeModeTool registers the code_mode tool.
func RegisterCodeModeTool(reg *ToolRegistry) {
	reg.Register(ToolEntry{
		Name:        "code_mode",
		Description: "Execute code in a specified language and return structured JSON results including stdout, stderr, and exit code.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"language": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"python", "bash", "sh", "node"},
					"description": "Programming language or interpreter to use.",
				},
				"code": map[string]interface{}{
					"type":        "string",
					"description": "Code to execute.",
				},
				"timeout_sec": map[string]interface{}{
					"type":        "integer",
					"description": "Execution timeout in seconds (default 30, max 120).",
				},
			},
			"required":             []string{"language", "code"},
			"additionalProperties": false,
		},
		Handler:          handleCodeMode,
		RequiresApproval: true,
	})
}

// CodeModeResult is the structured result of code execution.
type CodeModeResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Duration string `json:"duration"`
	Error    string `json:"error,omitempty"`
}

func handleCodeMode(ctx context.Context, session *Session, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
	language, _ := args["language"].(string)
	code, _ := args["code"].(string)

	if strings.TrimSpace(language) == "" {
		return "", fmt.Errorf("code_mode requires 'language' argument")
	}
	if strings.TrimSpace(code) == "" {
		return "", fmt.Errorf("code_mode requires non-empty 'code' argument")
	}

	timeoutSec := 30
	if t, ok := args["timeout_sec"].(float64); ok && t > 0 {
		timeoutSec = int(t)
	}
	if timeoutSec > 120 {
		timeoutSec = 120
	}

	// Determine interpreter.
	var interpreter string
	var interpreterArgs []string
	switch language {
	case "python":
		interpreter = "python3"
		interpreterArgs = []string{"-c", code}
	case "bash", "sh":
		interpreter = language
		interpreterArgs = []string{"-c", code}
	case "node":
		interpreter = "node"
		interpreterArgs = []string{"-e", code}
	default:
		return "", fmt.Errorf("code_mode: unsupported language %q", language)
	}

	cwd := session.Cwd()
	if cwd == "" {
		cwd = "."
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(execCtx, interpreter, interpreterArgs...)
	cmd.Dir = cwd

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := time.Since(start)

	result := CodeModeResult{
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
		return "", fmt.Errorf("code_mode: %w", marshalErr)
	}
	return string(out), nil
}
