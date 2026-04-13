package agentloop

import (
	"context"
	"fmt"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

func TestRegisterAndGet(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(ToolEntry{
		Name:        "alpha",
		Description: "alpha tool",
		Parameters:  map[string]interface{}{"type": "object"},
	})

	e, ok := reg.Get("alpha")
	if !ok {
		t.Fatal("expected to find tool alpha")
	}
	if e.Name != "alpha" {
		t.Fatalf("expected name alpha, got %s", e.Name)
	}
	if e.Description != "alpha tool" {
		t.Fatalf("expected description 'alpha tool', got %s", e.Description)
	}

	_, ok = reg.Get("nonexistent")
	if ok {
		t.Fatal("expected not to find nonexistent tool")
	}
}

func TestDefinitionsReturnsSortedList(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(ToolEntry{Name: "charlie", Description: "c"})
	reg.Register(ToolEntry{Name: "alpha", Description: "a"})
	reg.Register(ToolEntry{Name: "bravo", Description: "b"})

	defs := reg.Definitions(nil)
	if len(defs) != 3 {
		t.Fatalf("expected 3 definitions, got %d", len(defs))
	}
	expected := []string{"alpha", "bravo", "charlie"}
	for i, d := range defs {
		if d.Function.Name != expected[i] {
			t.Errorf("defs[%d] name = %s, want %s", i, d.Function.Name, expected[i])
		}
		if d.Type != "function" {
			t.Errorf("defs[%d] type = %s, want function", i, d.Type)
		}
	}
}

func TestDefinitionsFiltersEnabledTools(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(ToolEntry{Name: "alpha", Description: "a"})
	reg.Register(ToolEntry{Name: "bravo", Description: "b"})

	defs := reg.Definitions([]string{"bravo"})
	if len(defs) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(defs))
	}
	if defs[0].Function.Name != "bravo" {
		t.Fatalf("expected bravo, got %s", defs[0].Function.Name)
	}
}

func TestDispatchCallsHandler(t *testing.T) {
	reg := NewToolRegistry()
	called := false
	reg.Register(ToolEntry{
		Name: "my_tool",
		Handler: func(ctx context.Context, session *Session, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
			called = true
			v, _ := args["key"].(string)
			return "result:" + v, nil
		},
	})

	result, err := reg.Dispatch(context.Background(), nil, bifrost.ToolCall{ID: "call-1"}, "my_tool", map[string]interface{}{"key": "val"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("handler was not called")
	}
	if result != "result:val" {
		t.Fatalf("expected 'result:val', got %q", result)
	}
}

func TestDispatchUnknownToolReturnsError(t *testing.T) {
	reg := NewToolRegistry()
	_, err := reg.Dispatch(context.Background(), nil, bifrost.ToolCall{ID: "call-1"}, "unknown", nil)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
	expected := `tool "unknown" not found`
	if err.Error() != expected {
		t.Fatalf("expected error %q, got %q", expected, err.Error())
	}
}

func TestDispatchNilHandlerReturnsError(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(ToolEntry{Name: "no_handler"})
	_, err := reg.Dispatch(context.Background(), nil, bifrost.ToolCall{ID: "call-1"}, "no_handler", nil)
	if err == nil {
		t.Fatal("expected error for nil handler")
	}
}

func TestNamesReturnsSortedList(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(ToolEntry{Name: "zulu"})
	reg.Register(ToolEntry{Name: "alpha"})
	reg.Register(ToolEntry{Name: "mike"})

	names := reg.Names()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	expected := []string{"alpha", "mike", "zulu"}
	for i, n := range names {
		if n != expected[i] {
			t.Errorf("names[%d] = %s, want %s", i, n, expected[i])
		}
	}
}

func TestRegisterRemoteHostTools(t *testing.T) {
	reg := NewToolRegistry()
	RegisterRemoteHostTools(reg)

	expected := []string{
		"execute_command",
		"execute_readonly_query",
		"list_files",
		"read_file",
		"search_files",
		"write_file",
	}
	names := reg.Names()
	if len(names) != len(expected) {
		t.Fatalf("expected %d remote tools, got %d: %v", len(expected), len(names), names)
	}
	for i, n := range names {
		if n != expected[i] {
			t.Errorf("names[%d] = %s, want %s", i, n, expected[i])
		}
	}

	// Verify approval flags.
	for _, name := range []string{"execute_command", "write_file"} {
		e, _ := reg.Get(name)
		if !e.RequiresApproval {
			t.Errorf("%s should require approval", name)
		}
	}
	for _, name := range []string{"execute_readonly_query", "list_files", "read_file", "search_files"} {
		e, _ := reg.Get(name)
		if !e.IsReadOnly {
			t.Errorf("%s should be read-only", name)
		}
	}
}

func TestRegisterCorootTools(t *testing.T) {
	reg := NewToolRegistry()
	RegisterCorootTools(reg)

	expected := []string{
		"coroot_incident_timeline",
		"coroot_list_services",
		"coroot_rca_report",
		"coroot_service_alerts",
		"coroot_service_metrics",
		"coroot_service_overview",
		"coroot_topology",
	}
	names := reg.Names()
	if len(names) != len(expected) {
		t.Fatalf("expected %d coroot tools, got %d: %v", len(expected), len(names), names)
	}
	for i, n := range names {
		if n != expected[i] {
			t.Errorf("names[%d] = %s, want %s", i, n, expected[i])
		}
	}
}

func TestRegisterWorkspaceTools(t *testing.T) {
	reg := NewToolRegistry()
	RegisterWorkspaceTools(reg)

	expected := []string{
		"ask_user_question",
		"enter_plan_mode",
		"exit_plan_mode",
		"orchestrator_dispatch_tasks",
		"query_ai_server_state",
		"readonly_host_inspect",
		"request_approval",
		"update_plan",
	}
	names := reg.Names()
	if len(names) != len(expected) {
		t.Fatalf("expected %d workspace tools, got %d: %v", len(expected), len(names), names)
	}
	for i, n := range names {
		if n != expected[i] {
			t.Errorf("names[%d] = %s, want %s", i, n, expected[i])
		}
	}
}

func TestHandlerError(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(ToolEntry{
		Name: "fail_tool",
		Handler: func(ctx context.Context, session *Session, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
			return "", fmt.Errorf("something went wrong")
		},
	})

	_, err := reg.Dispatch(context.Background(), nil, bifrost.ToolCall{ID: "call-1"}, "fail_tool", nil)
	if err == nil {
		t.Fatal("expected error from handler")
	}
	if err.Error() != "something went wrong" {
		t.Fatalf("unexpected error: %v", err)
	}
}
