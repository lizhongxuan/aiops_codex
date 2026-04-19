package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// jsReplState holds accumulated state for the JS REPL session.
type jsReplState struct {
	mu      sync.Mutex
	history []string
}

var globalJSRepl = &jsReplState{}

// RegisterJSReplTool registers the js_repl and js_repl_reset tools.
func RegisterJSReplTool(reg *ToolRegistry) {
	reg.Register(ToolEntry{
		Name:        "js_repl",
		Description: "Execute JavaScript code using Node.js and return the result. Maintains execution history within the session.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"code":        map[string]interface{}{"type": "string", "description": "JavaScript code to execute."},
				"timeout_sec": map[string]interface{}{"type": "integer", "description": "Execution timeout in seconds (default 10, max 60)."},
			},
			"required":             []string{"code"},
			"additionalProperties": false,
		},
		Handler:          handleJSRepl,
		RequiresApproval: true,
	})

	reg.Register(ToolEntry{
		Name:        "js_repl_reset",
		Description: "Reset the JavaScript REPL session, clearing all accumulated state and history.",
		Parameters: map[string]interface{}{
			"type":                 "object",
			"properties":          map[string]interface{}{},
			"additionalProperties": false,
		},
		Handler:    handleJSReplReset,
		IsReadOnly: true,
	})
}

// JSReplResult is the structured result of JS execution.
type JSReplResult struct {
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
	Duration string `json:"duration"`
}

func handleJSRepl(ctx context.Context, tc ToolContext, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
	code, _ := args["code"].(string)
	if strings.TrimSpace(code) == "" {
		return "", fmt.Errorf("js_repl requires non-empty 'code' argument")
	}

	timeoutSec := 10
	if t, ok := args["timeout_sec"].(float64); ok && t > 0 {
		timeoutSec = int(t)
	}
	if timeoutSec > 60 {
		timeoutSec = 60
	}

	globalJSRepl.mu.Lock()
	globalJSRepl.history = append(globalJSRepl.history, code)
	fullCode := strings.Join(globalJSRepl.history, "\n")
	globalJSRepl.mu.Unlock()

	cwd := tc.Cwd()
	if cwd == "" {
		cwd = "."
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "node", "-e", fullCode)
	cmd.Dir = cwd

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	result := JSReplResult{
		Output:   stdout.String(),
		Duration: duration.Round(time.Millisecond).String(),
	}

	if err != nil {
		result.Error = stderr.String()
		if result.Error == "" {
			result.Error = err.Error()
		}
		globalJSRepl.mu.Lock()
		if len(globalJSRepl.history) > 0 {
			globalJSRepl.history = globalJSRepl.history[:len(globalJSRepl.history)-1]
		}
		globalJSRepl.mu.Unlock()
	}

	out, marshalErr := json.MarshalIndent(result, "", "  ")
	if marshalErr != nil {
		return "", fmt.Errorf("js_repl: %w", marshalErr)
	}
	return string(out), nil
}

func handleJSReplReset(ctx context.Context, tc ToolContext, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
	globalJSRepl.mu.Lock()
	globalJSRepl.history = nil
	globalJSRepl.mu.Unlock()
	return fmt.Sprintf("JavaScript REPL session reset. History cleared."), nil
}
