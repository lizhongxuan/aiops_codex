package server

import (
	"context"
	"testing"
)

type testToolHandler struct {
	descriptor ToolDescriptor
	result     ToolExecutionResult
}

func (h testToolHandler) Descriptor() ToolDescriptor {
	return h.descriptor.Clone()
}

func (h testToolHandler) Execute(context.Context, ToolInvocation) (ToolExecutionResult, error) {
	return h.result.Clone(), nil
}

type testUnifiedTool struct {
	name        string
	description string
	callResult  ToolCallResult
	handlerSeen ToolInvocation
	callCount   int
}

func (t *testUnifiedTool) Name() string {
	return t.name
}

func (t *testUnifiedTool) Aliases() []string {
	return []string{"alias-" + t.name}
}

func (t *testUnifiedTool) Description(ToolDescriptionContext) string {
	return t.description
}

func (t *testUnifiedTool) InputSchema() map[string]any {
	return map[string]any{"type": "object"}
}

func (t *testUnifiedTool) Call(_ context.Context, req ToolCallRequest) (ToolCallResult, error) {
	t.callCount++
	t.handlerSeen = req.Invocation.Clone()
	return t.callResult.Clone(), nil
}

func (t *testUnifiedTool) CheckPermissions(context.Context, ToolCallRequest) (PermissionResult, error) {
	return PermissionResult{Allowed: true, RequiresApproval: false}, nil
}

func (t *testUnifiedTool) IsConcurrencySafe(ToolCallRequest) bool {
	return true
}

func (t *testUnifiedTool) IsReadOnly(ToolCallRequest) bool {
	return true
}

func (t *testUnifiedTool) IsDestructive(ToolCallRequest) bool {
	return false
}

func (t *testUnifiedTool) Display() ToolDisplayAdapter {
	return nil
}

func TestToolHandlerRegistryRegisterAndLookup(t *testing.T) {
	reg := NewToolHandlerRegistry()
	handler := testToolHandler{
		descriptor: ToolDescriptor{
			Name:                      "read_file",
			Domain:                    "filesystem",
			RequiresApproval:          false,
			IsReadOnly:                true,
			SupportsStreamingProgress: false,
			ProjectionHints:           []string{"card"},
		},
		result: ToolExecutionResult{
			Status: ToolRunStatusCompleted,
		},
	}

	if err := reg.Register(handler); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	gotHandler, ok := reg.Get("  read_file ")
	if !ok {
		t.Fatalf("expected handler to be found")
	}

	gotDesc := gotHandler.Descriptor()
	if gotDesc.Name != "read_file" || gotDesc.Domain != "filesystem" || !gotDesc.IsReadOnly {
		t.Fatalf("unexpected descriptor: %#v", gotDesc)
	}

	desc, ok := reg.Descriptor("read_file")
	if !ok {
		t.Fatalf("expected descriptor lookup to succeed")
	}
	desc.ProjectionHints[0] = "changed"
	if regDesc, _ := reg.Descriptor("read_file"); regDesc.ProjectionHints[0] != "card" {
		t.Fatalf("descriptor lookup should return a copy: %#v", regDesc)
	}

	if reg.Len() != 1 {
		t.Fatalf("expected registry length 1, got %d", reg.Len())
	}
}

func TestToolHandlerRegistryRejectsInvalidRegistration(t *testing.T) {
	reg := NewToolHandlerRegistry()

	if err := reg.Register(nil); err == nil {
		t.Fatalf("expected nil handler to be rejected")
	}

	if err := reg.Register(testToolHandler{}); err == nil {
		t.Fatalf("expected empty descriptor name to be rejected")
	}

	handler := testToolHandler{descriptor: ToolDescriptor{Name: "execute_command"}}
	if err := reg.Register(handler); err != nil {
		t.Fatalf("register handler: %v", err)
	}
	if err := reg.Register(handler); err == nil {
		t.Fatalf("expected duplicate handler to be rejected")
	}
}

func TestToolHandlerFuncAdapter(t *testing.T) {
	var called bool
	handler := ToolHandlerFunc{
		Desc: ToolDescriptor{Name: "query_state"},
		Fn: func(_ context.Context, inv ToolInvocation) (ToolExecutionResult, error) {
			called = true
			return ToolExecutionResult{
				InvocationID: inv.InvocationID,
				Status:       ToolRunStatusCompleted,
				OutputText:   "ok",
			}, nil
		},
	}

	res, err := handler.Execute(context.Background(), ToolInvocation{InvocationID: "inv-1"})
	if err != nil {
		t.Fatalf("execute handler: %v", err)
	}
	if !called {
		t.Fatalf("expected function handler to be called")
	}
	if res.InvocationID != "inv-1" || res.OutputText != "ok" {
		t.Fatalf("unexpected execution result: %#v", res)
	}
	if handler.Descriptor().Name != "query_state" {
		t.Fatalf("unexpected descriptor returned by adapter")
	}
}

func TestToolHandlerRegistryDispatchCategoryUsesDescriptorHints(t *testing.T) {
	reg := NewToolHandlerRegistry()
	reg.MustRegister(testToolHandler{descriptor: ToolDescriptor{
		Name:             "test.approval.tool",
		RequiresApproval: true,
	}})
	reg.MustRegister(testToolHandler{descriptor: ToolDescriptor{
		Name:       "test.readonly.tool",
		IsReadOnly: true,
	}})

	if got := reg.DispatchCategory("test.approval.tool", toolCategoryMutation); got != toolCategoryApproval {
		t.Fatalf("expected descriptor approval category, got %q", got)
	}
	if got := reg.DispatchCategory("test.readonly.tool", toolCategoryMutation); got != toolCategoryReadonly {
		t.Fatalf("expected descriptor readonly category, got %q", got)
	}
	if got := reg.DispatchCategory("unknown.blocking.tool", toolCategoryBlocking); got != toolCategoryBlocking {
		t.Fatalf("expected explicit blocking category to win, got %q", got)
	}
}

func TestToolHandlerRegistryRegisterUnifiedToolExposesLegacyHandlerView(t *testing.T) {
	reg := NewToolHandlerRegistry()
	tool := scriptedUnifiedTool{
		name:    "web_search",
		aliases: []string{"search_web"},
		callFn: func(_ context.Context, req ToolCallRequest) (ToolCallResult, error) {
			return ToolCallResult{
				Output: "ok",
				DisplayOutput: &ToolDisplayPayload{
					Summary: "searched web",
				},
				StructuredContent: map[string]any{
					"query": req.Input["query"],
				},
			}, nil
		},
	}

	if err := reg.RegisterUnifiedTool(tool); err != nil {
		t.Fatalf("register unified tool: %v", err)
	}

	handler, ok := reg.Get("search_web")
	if !ok {
		t.Fatal("expected handler lookup by alias to succeed")
	}

	result, err := handler.Execute(context.Background(), ToolInvocation{
		InvocationID: "inv-search",
		SessionID:    "sess-search",
		ToolName:     "search_web",
		Arguments: map[string]any{
			"query": "btc price",
		},
	})
	if err != nil {
		t.Fatalf("execute unified handler adapter: %v", err)
	}
	if result.OutputText != "ok" {
		t.Fatalf("expected output text to be preserved, got %#v", result)
	}
	display, ok := result.ProjectionPayload["display"].(map[string]any)
	if !ok {
		t.Fatalf("expected display payload to be projected, got %#v", result.ProjectionPayload)
	}
	if got := getStringAny(display, "summary"); got != "searched web" {
		t.Fatalf("expected display summary, got %#v", display)
	}
}

func TestToolHandlerRegistryRegisterLegacyHandlerExposesUnifiedView(t *testing.T) {
	reg := NewToolHandlerRegistry()
	handler := testToolHandler{
		descriptor: ToolDescriptor{
			Name:             "write_file",
			RequiresApproval: true,
			IsReadOnly:       false,
		},
		result: ToolExecutionResult{
			Status:     ToolRunStatusCompleted,
			OutputText: "patched",
		},
	}

	if err := reg.Register(handler); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	unified, ok := reg.GetUnified("write_file")
	if !ok {
		t.Fatal("expected unified lookup to succeed for legacy handler")
	}

	permission, err := unified.CheckPermissions(context.Background(), ToolCallRequest{
		Invocation: ToolInvocation{ToolName: "write_file"},
	})
	if err != nil {
		t.Fatalf("check permissions: %v", err)
	}
	if permission.Allowed || !permission.RequiresApproval {
		t.Fatalf("expected approval-required compatibility permission, got %#v", permission)
	}
	if unified.IsReadOnly(ToolCallRequest{}) {
		t.Fatalf("expected write_file adapter to remain mutable")
	}
	if unified.IsConcurrencySafe(ToolCallRequest{}) {
		t.Fatalf("expected mutable legacy handler to be non-concurrency-safe")
	}

	result, err := unified.Call(context.Background(), ToolCallRequest{
		Invocation: ToolInvocation{
			InvocationID: "inv-write",
			SessionID:    "sess-write",
			ToolName:     "write_file",
		},
		Input: map[string]any{
			"path": "/tmp/app.conf",
		},
	})
	if err != nil {
		t.Fatalf("call legacy unified adapter: %v", err)
	}
	if output, _ := result.Output.(string); output != "patched" {
		t.Fatalf("expected string output to be preserved, got %#v", result.Output)
	}
}

func TestToolHandlerRegistryRegisterUnifiedTool(t *testing.T) {
	reg := NewToolHandlerRegistry()
	tool := &testUnifiedTool{
		name:        "unified_read_file",
		description: "Read a file",
		callResult: ToolCallResult{
			Output: "ok",
			StructuredContent: map[string]any{
				"path": "/tmp/a.txt",
			},
		},
	}

	if err := reg.RegisterUnifiedTool(tool); err != nil {
		t.Fatalf("register unified tool: %v", err)
	}

	gotHandler, ok := reg.Get(" unified_read_file ")
	if !ok {
		t.Fatalf("expected handler adapter to be found")
	}

	result, err := gotHandler.Execute(context.Background(), ToolInvocation{
		InvocationID: "inv-1",
		ToolName:     "unified_read_file",
		Arguments: map[string]any{
			"path": "/tmp/a.txt",
		},
	})
	if err != nil {
		t.Fatalf("execute handler adapter: %v", err)
	}
	if result.OutputText != "ok" || result.OutputData["path"] != "/tmp/a.txt" {
		t.Fatalf("unexpected converted execution result: %#v", result)
	}
	if tool.callCount != 1 {
		t.Fatalf("expected unified tool call count 1, got %d", tool.callCount)
	}
	if got := tool.handlerSeen.Arguments["path"]; got != "/tmp/a.txt" {
		t.Fatalf("expected invocation arguments to be forwarded, got %#v", got)
	}

	desc, ok := reg.Descriptor("unified_read_file")
	if !ok {
		t.Fatalf("expected descriptor lookup to succeed")
	}
	if desc.Name != "unified_read_file" || desc.DisplayLabel != "Read a file" {
		t.Fatalf("unexpected descriptor from unified tool: %#v", desc)
	}

	gotUnified, ok := reg.GetUnified("unified_read_file")
	if !ok {
		t.Fatalf("expected unified tool lookup to succeed")
	}
	if gotUnified != tool {
		t.Fatalf("expected original unified tool to be returned")
	}

	listed := reg.ListDescriptors()
	if len(listed) != 1 || listed[0].Name != "unified_read_file" {
		t.Fatalf("unexpected descriptor list: %#v", listed)
	}
}

func TestToolHandlerRegistryLegacyHandlerAdaptsToUnifiedTool(t *testing.T) {
	reg := NewToolHandlerRegistry()
	handler := testToolHandler{
		descriptor: ToolDescriptor{
			Name:         "legacy_query_state",
			DisplayLabel: "Legacy query",
			IsReadOnly:   true,
		},
		result: ToolExecutionResult{
			OutputText: "legacy-ok",
		},
	}

	if err := reg.Register(handler); err != nil {
		t.Fatalf("register legacy handler: %v", err)
	}

	desc, unified, ok := reg.LookupUnified("legacy_query_state")
	if !ok {
		t.Fatalf("expected unified adapter lookup to succeed")
	}
	if desc.Name != "legacy_query_state" || !desc.IsReadOnly {
		t.Fatalf("unexpected descriptor from legacy adapter: %#v", desc)
	}

	result, err := unified.Call(context.Background(), ToolCallRequest{
		Invocation: ToolInvocation{
			InvocationID: "inv-legacy",
			ToolName:     "legacy_query_state",
			Arguments: map[string]any{
				"query": "status",
			},
		},
	})
	if err != nil {
		t.Fatalf("call unified adapter: %v", err)
	}
	if result.Output != "legacy-ok" {
		t.Fatalf("unexpected unified result output: %#v", result)
	}

	gotHandler, ok := reg.Get("legacy_query_state")
	if !ok {
		t.Fatalf("expected legacy handler lookup to succeed")
	}
	gotResult, err := gotHandler.Execute(context.Background(), ToolInvocation{InvocationID: "inv-legacy"})
	if err != nil {
		t.Fatalf("execute legacy handler adapter: %v", err)
	}
	if gotResult.OutputText != "legacy-ok" {
		t.Fatalf("unexpected handler result: %#v", gotResult)
	}
}
