package server

import (
	"context"
	"fmt"
)

type scriptedUnifiedTool struct {
	name              string
	aliases           []string
	description       string
	inputSchema       map[string]any
	callFn            func(context.Context, ToolCallRequest) (ToolCallResult, error)
	permissionFn      func(context.Context, ToolCallRequest) (PermissionResult, error)
	isConcurrencySafe func(ToolCallRequest) bool
	isReadOnly        func(ToolCallRequest) bool
	isDestructive     func(ToolCallRequest) bool
	displayAdapter    ToolDisplayAdapter
}

func (t scriptedUnifiedTool) Name() string {
	return t.name
}

func (t scriptedUnifiedTool) Aliases() []string {
	return append([]string(nil), t.aliases...)
}

func (t scriptedUnifiedTool) Description(_ ToolDescriptionContext) string {
	return t.description
}

func (t scriptedUnifiedTool) InputSchema() map[string]any {
	return cloneNestedAnyMap(t.inputSchema)
}

func (t scriptedUnifiedTool) Call(ctx context.Context, req ToolCallRequest) (ToolCallResult, error) {
	if t.callFn != nil {
		return t.callFn(ctx, req)
	}
	return ToolCallResult{
		Output: fmt.Sprintf("called %s", req.Invocation.ToolName),
	}, nil
}

func (t scriptedUnifiedTool) CheckPermissions(ctx context.Context, req ToolCallRequest) (PermissionResult, error) {
	if t.permissionFn != nil {
		return t.permissionFn(ctx, req)
	}
	return PermissionResult{Allowed: true}, nil
}

func (t scriptedUnifiedTool) IsConcurrencySafe(req ToolCallRequest) bool {
	if t.isConcurrencySafe != nil {
		return t.isConcurrencySafe(req)
	}
	return false
}

func (t scriptedUnifiedTool) IsReadOnly(req ToolCallRequest) bool {
	if t.isReadOnly != nil {
		return t.isReadOnly(req)
	}
	return false
}

func (t scriptedUnifiedTool) IsDestructive(req ToolCallRequest) bool {
	if t.isDestructive != nil {
		return t.isDestructive(req)
	}
	return false
}

func (t scriptedUnifiedTool) Display() ToolDisplayAdapter {
	return t.displayAdapter
}

type testDisplayAdapter struct {
	usePayload      *ToolDisplayPayload
	progressPayload *ToolDisplayPayload
	resultPayload   *ToolDisplayPayload
}

func (a testDisplayAdapter) RenderUse(ToolCallRequest) *ToolDisplayPayload {
	if a.usePayload == nil {
		return nil
	}
	cloned := a.usePayload.Clone()
	return &cloned
}

func (a testDisplayAdapter) RenderProgress(ToolProgressEvent) *ToolDisplayPayload {
	if a.progressPayload == nil {
		return nil
	}
	cloned := a.progressPayload.Clone()
	return &cloned
}

func (a testDisplayAdapter) RenderResult(ToolCallResult) *ToolDisplayPayload {
	if a.resultPayload == nil {
		return nil
	}
	cloned := a.resultPayload.Clone()
	return &cloned
}
