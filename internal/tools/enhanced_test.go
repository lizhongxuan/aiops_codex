package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

func TestRegisterToolSuggest(t *testing.T) {
	reg := NewToolRegistry()
	RegisterToolSuggestTool(reg)

	entry, ok := reg.Get("tool_suggest")
	if !ok {
		t.Fatal("tool_suggest not registered")
	}
	if entry.Handler == nil {
		t.Fatal("tool_suggest handler is nil")
	}
}

func TestHandleToolSuggest_EmptyQuery(t *testing.T) {
	reg := NewToolRegistry()
	RegisterToolSuggestTool(reg)

	tc := newTestToolContext(".")
	call := bifrost.ToolCall{ID: "1"}
	args := map[string]interface{}{"query": ""}

	_, err := handleToolSuggest(context.Background(), tc, call, args)
	if err == nil {
		t.Error("expected error for empty query")
	}
}

func TestHandleListDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("hello"), 0644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)
	os.WriteFile(filepath.Join(dir, "subdir", "file2.txt"), []byte("world"), 0644)

	tc := newTestToolContext(dir)
	call := bifrost.ToolCall{ID: "1"}
	args := map[string]interface{}{
		"path":      ".",
		"max_depth": float64(2),
	}

	result, err := handleListDir(context.Background(), tc, call, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !contains(result, "file1.txt") {
		t.Errorf("expected file1.txt in output, got: %s", result)
	}
	if !contains(result, "subdir") {
		t.Errorf("expected subdir in output, got: %s", result)
	}
}

func TestHandleListDir_MaxDepthLimit(t *testing.T) {
	tc := newTestToolContext(t.TempDir())
	call := bifrost.ToolCall{ID: "1"}
	args := map[string]interface{}{
		"path":      ".",
		"max_depth": float64(100),
	}

	_, err := handleListDir(context.Background(), tc, call, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleViewImage_NotAnImage(t *testing.T) {
	dir := t.TempDir()
	txtFile := filepath.Join(dir, "test.txt")
	os.WriteFile(txtFile, []byte("not an image"), 0644)

	tc := newTestToolContext(dir)
	call := bifrost.ToolCall{ID: "1"}
	args := map[string]interface{}{"path": "test.txt"}

	_, err := handleViewImage(context.Background(), tc, call, args)
	if err == nil {
		t.Error("expected error for non-image file")
	}
}

func TestHandleViewImage_EmptyPath(t *testing.T) {
	tc := newTestToolContext(".")
	call := bifrost.ToolCall{ID: "1"}
	args := map[string]interface{}{"path": ""}

	_, err := handleViewImage(context.Background(), tc, call, args)
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestHandleRequestUserInput(t *testing.T) {
	tc := newTestToolContext(".")
	call := bifrost.ToolCall{ID: "1"}
	args := map[string]interface{}{
		"title": "Test Form",
		"questions": []interface{}{
			map[string]interface{}{
				"id":         "q1",
				"text":       "What is your name?",
				"field_type": "text",
				"required":   true,
			},
		},
	}

	result, err := handleRequestUserInput(context.Background(), tc, call, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var req UserInputRequest
	if err := json.Unmarshal([]byte(result), &req); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if req.Title != "Test Form" {
		t.Errorf("expected title 'Test Form', got %q", req.Title)
	}
	if len(req.Questions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(req.Questions))
	}
	if req.Questions[0].ID != "q1" {
		t.Errorf("expected question id 'q1', got %q", req.Questions[0].ID)
	}
}

func TestHandleRequestPermissions_Empty(t *testing.T) {
	tc := newTestToolContext(".")
	call := bifrost.ToolCall{ID: "1"}
	args := map[string]interface{}{}

	_, err := handleRequestPermissions(context.Background(), tc, call, args)
	if err == nil {
		t.Error("expected error for empty permissions")
	}
}

func TestHandleRequestPermissions_Valid(t *testing.T) {
	tc := newTestToolContext(".")
	call := bifrost.ToolCall{ID: "1"}
	args := map[string]interface{}{
		"filesystem": []interface{}{
			map[string]interface{}{
				"path":   "/tmp/data",
				"mode":   "read",
				"reason": "need to read config",
			},
		},
		"reason": "testing",
	}

	result, err := handleRequestPermissions(context.Background(), tc, call, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(result, "/tmp/data") {
		t.Errorf("expected path in result, got: %s", result)
	}
}

func TestDynamicToolRegistration(t *testing.T) {
	reg := NewToolRegistry()

	err := reg.RegisterDynamic(DynamicToolSpec{
		Name:        "my_dynamic_tool",
		Description: "A test dynamic tool",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry, ok := reg.Get("my_dynamic_tool")
	if !ok {
		t.Fatal("dynamic tool not registered")
	}
	if entry.Description != "A test dynamic tool" {
		t.Errorf("unexpected description: %s", entry.Description)
	}

	if !reg.UnregisterDynamic("my_dynamic_tool") {
		t.Error("expected UnregisterDynamic to return true")
	}
	if _, ok := reg.Get("my_dynamic_tool"); ok {
		t.Error("tool should be unregistered")
	}
}

func TestDynamicToolRegistration_InvalidName(t *testing.T) {
	reg := NewToolRegistry()
	err := reg.RegisterDynamic(DynamicToolSpec{
		Name:        "",
		Description: "no name",
	})
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestDynamicToolRegistration_InvalidDescription(t *testing.T) {
	reg := NewToolRegistry()
	err := reg.RegisterDynamic(DynamicToolSpec{
		Name:        "test",
		Description: "",
	})
	if err == nil {
		t.Error("expected error for empty description")
	}
}

func TestHandleAgentJobs(t *testing.T) {
	tc := newTestToolContext(".")
	call := bifrost.ToolCall{ID: "1"}
	args := map[string]interface{}{
		"jobs": []interface{}{
			map[string]interface{}{"id": "job1", "command": "echo hello"},
			map[string]interface{}{"id": "job2", "command": "echo world"},
		},
		"concurrency": float64(2),
	}

	result, err := handleAgentJobs(context.Background(), tc, call, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var results []JobResult
	if err := json.Unmarshal([]byte(result), &results); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Status != "success" {
			t.Errorf("expected success for job %s, got %s", r.ID, r.Status)
		}
	}
}

func TestHandleAgentJobs_EmptyJobs(t *testing.T) {
	tc := newTestToolContext(".")
	call := bifrost.ToolCall{ID: "1"}
	args := map[string]interface{}{
		"jobs": []interface{}{},
	}

	_, err := handleAgentJobs(context.Background(), tc, call, args)
	if err == nil {
		t.Error("expected error for empty jobs")
	}
}

func TestHandleShellCommand(t *testing.T) {
	tc := newTestToolContext(".")
	call := bifrost.ToolCall{ID: "1"}
	args := map[string]interface{}{
		"command": "echo hello",
	}

	result, err := handleShellCommand(context.Background(), tc, call, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var res ShellCommandResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", res.ExitCode)
	}
	if !contains(res.Stdout, "hello") {
		t.Errorf("expected 'hello' in stdout, got: %s", res.Stdout)
	}
}

func TestHandleShellCommand_EmptyCommand(t *testing.T) {
	tc := newTestToolContext(".")
	call := bifrost.ToolCall{ID: "1"}
	args := map[string]interface{}{"command": ""}

	_, err := handleShellCommand(context.Background(), tc, call, args)
	if err == nil {
		t.Error("expected error for empty command")
	}
}

func TestHandleUnifiedExec(t *testing.T) {
	tc := newTestToolContext(".")
	call := bifrost.ToolCall{ID: "1"}
	args := map[string]interface{}{
		"command": "echo",
		"args":    []interface{}{"unified", "exec"},
	}

	result, err := handleUnifiedExec(context.Background(), tc, call, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var res UnifiedExecResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", res.ExitCode)
	}
	if !contains(res.Stdout, "unified") {
		t.Errorf("expected 'unified' in stdout, got: %s", res.Stdout)
	}
}

func TestHandleUnifiedExec_WithStdin(t *testing.T) {
	tc := newTestToolContext(".")
	call := bifrost.ToolCall{ID: "1"}
	args := map[string]interface{}{
		"command": "cat",
		"stdin":   "hello from stdin",
	}

	result, err := handleUnifiedExec(context.Background(), tc, call, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var res UnifiedExecResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if !contains(res.Stdout, "hello from stdin") {
		t.Errorf("expected stdin content in stdout, got: %s", res.Stdout)
	}
}

func TestHandleJSReplReset(t *testing.T) {
	globalJSRepl.mu.Lock()
	globalJSRepl.history = []string{"var x = 1;"}
	globalJSRepl.mu.Unlock()

	tc := newTestToolContext(".")
	call := bifrost.ToolCall{ID: "1"}
	args := map[string]interface{}{}

	result, err := handleJSReplReset(context.Background(), tc, call, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(result, "reset") {
		t.Errorf("expected 'reset' in result, got: %s", result)
	}

	globalJSRepl.mu.Lock()
	histLen := len(globalJSRepl.history)
	globalJSRepl.mu.Unlock()
	if histLen != 0 {
		t.Errorf("expected empty history after reset, got %d entries", histLen)
	}
}

func TestRegisterAllEnhancedTools(t *testing.T) {
	reg := NewToolRegistry()
	RegisterToolSuggestTool(reg)
	RegisterListDirTool(reg)
	RegisterViewImageTool(reg)
	RegisterRequestUserInputTool(reg)
	RegisterRequestPermissionsTool(reg)
	RegisterAgentJobsTool(reg)
	RegisterCodeModeTool(reg)
	RegisterUnifiedExecTool(reg)
	RegisterShellCommandTool(reg)
	RegisterJSReplTool(reg)

	expectedTools := []string{
		"tool_suggest", "list_dir", "view_image",
		"request_user_input", "request_permissions",
		"agent_jobs", "code_mode", "unified_exec",
		"shell_command", "js_repl", "js_repl_reset",
	}

	for _, name := range expectedTools {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("expected tool %q to be registered", name)
		}
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
